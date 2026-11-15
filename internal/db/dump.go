package db

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/MahyaR-Kd/dbtool/internal/backupstate"
	"github.com/MahyaR-Kd/dbtool/internal/connection"
	"github.com/MahyaR-Kd/dbtool/internal/filelock"
	"github.com/MahyaR-Kd/dbtool/internal/logger"
	"github.com/MahyaR-Kd/dbtool/internal/paths"
	"github.com/MahyaR-Kd/dbtool/internal/progress"
	"github.com/MahyaR-Kd/dbtool/internal/rotate"
	"github.com/MahyaR-Kd/dbtool/internal/s3store"
	"github.com/MahyaR-Kd/dbtool/internal/settings"
	"github.com/MahyaR-Kd/dbtool/internal/telegram"
	"github.com/MahyaR-Kd/dbtool/internal/types"
	"github.com/MahyaR-Kd/dbtool/internal/ui"
)

// isTableSchemaFile returns true when name is a mydumper table-level schema
// file (e.g. "mydb.users-schema.sql.gz").  Database-level files such as
// "mydb-schema-create.sql.gz" are excluded because they do not contain a "."
// before the "-schema.sql" marker.
func isTableSchemaFile(name string) bool {
	idx := strings.Index(name, "-schema.sql")
	if idx < 0 {
		return false
	}
	return strings.Contains(name[:idx], ".")
}

// countDumpedTables counts how many table schema files mydumper has written to
// dir so far.  Each table produces exactly one <schema>.<table>-schema.sql[.gz]
// file, so this gives the number of tables whose schema has been dumped.
func countDumpedTables(dir string) int {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return 0
	}
	n := 0
	for _, e := range entries {
		if !e.IsDir() && isTableSchemaFile(e.Name()) {
			n++
		}
	}
	return n
}

// countTablesToProcess queries the target database to determine the total number
// of tables that will be included in the dump (i.e. all tables minus ignored
// schemas and ignored individual tables).  It returns 0 on any error so that
// callers can fall back gracefully to a spinner.
func countTablesToProcess(cfg types.Config, pass string) int {
	schemas, err := FetchSchemas(cfg.Host, cfg.Port, cfg.User, pass)
	if err != nil {
		logger.Debug("pre-count: FetchSchemas failed: %v", err)
		return 0
	}

	ignoredSchema := map[string]bool{}
	for _, s := range cfg.IgnoredSchemas {
		ignoredSchema[strings.ToLower(s)] = true
	}

	total := 0
	for _, schema := range schemas {
		if ignoredSchema[strings.ToLower(schema)] {
			continue
		}
		tables, err := FetchTables(cfg.Host, cfg.Port, cfg.User, pass, schema)
		if err != nil {
			logger.Debug("pre-count: FetchTables(%s) failed: %v", schema, err)
			continue
		}
		ignoredTable := map[string]bool{}
		for _, t := range cfg.IgnoredTables[schema] {
			ignoredTable[t] = true
		}
		for _, t := range tables {
			if !ignoredTable[t] {
				total++
			}
		}
	}
	logger.Debug("pre-count: %d tables to dump", total)
	return total
}

// commonPaths lists directories that are searched for executables in addition
// to the directories found in the current PATH. Cron jobs typically run with a
// minimal PATH (e.g. /usr/bin:/bin), so tools installed in /usr/local/bin
// would otherwise not be found.
var commonPaths = []string{
	"/usr/bin",
	"/usr/local/bin",
	"/usr/sbin",
	"/usr/local/sbin",
	"/bin",
	"/sbin",
}

// findExecutable returns the full path to name if it can be found either via
// exec.LookPath (which searches $PATH) or in one of the well-known
// commonPaths. This is necessary because cron jobs run with a restricted PATH.
func findExecutable(name string) (string, error) {
	// Reject names that contain path separators to prevent directory traversal.
	if strings.ContainsRune(name, '/') {
		return "", fmt.Errorf("executable name must not contain path separators: %q", name)
	}
	if p, err := exec.LookPath(name); err == nil {
		return p, nil
	}
	for _, dir := range commonPaths {
		candidate := filepath.Join(dir, name)
		info, err := os.Stat(candidate)
		if err != nil || info.IsDir() {
			continue
		}
		if info.Mode()&0111 != 0 {
			return candidate, nil
		}
	}
	return "", fmt.Errorf("%q not found in PATH or common directories", name)
}

// buildExclusionRegex builds mydumper's --regex value that excludes fully
// ignored schemas (e.g. ^(?!(s1(\.|$)|s2(\.|$)))) and individually ignored
// tables (e.g. ^(?!(app\.logs$|app\.audit$))) from a dump. Returns "" when
// there's nothing to exclude.
//
// mydumper evaluates this regex against "schema.table" when checking
// whether to dump a table, but against the bare "schema" (no dot) when
// deciding whether to write that schema's own CREATE DATABASE statement
// (see mydumper's check_regex in src/regex.c: it builds "db.table" only
// when a table name is given, otherwise just "db"). A schema exclusion
// pattern of "schema\." alone never matches that bare form — the schema's
// tables would be correctly skipped, but its CREATE DATABASE statement
// would still leak into the dump. "(\.|$)" matches both forms.
func buildExclusionRegex(ignoredSchemas []string, ignoredTables map[string][]string) string {
	exclusions := exclusionPatterns(ignoredSchemas, ignoredTables)
	if len(exclusions) == 0 {
		return ""
	}
	return fmt.Sprintf(`^(?!(%s))`, strings.Join(exclusions, "|"))
}

// exclusionPatterns returns the individual alternation branches used inside
// buildExclusionRegex's negative lookahead, sorted for deterministic output
// regardless of ignoredTables' map iteration order. Split out from
// buildExclusionRegex so tests can check "does this subject match any
// exclusion branch" directly — Go's stdlib regexp (RE2) can't compile the
// lookahead-wrapped pattern itself, since RE2 deliberately doesn't support
// lookahead (mydumper's own PCRE2-based matching does).
func exclusionPatterns(ignoredSchemas []string, ignoredTables map[string][]string) []string {
	var exclusions []string

	for _, s := range ignoredSchemas {
		exclusions = append(exclusions, regexp.QuoteMeta(s)+`(\.|$)`)
	}

	for schema, tables := range ignoredTables {
		for _, table := range tables {
			exclusions = append(exclusions, regexp.QuoteMeta(schema)+`\.`+regexp.QuoteMeta(table)+`$`)
		}
	}

	sort.Strings(exclusions)
	return exclusions
}

// redactPasswordArg returns a copy of a mydumper/myloader argv (built with
// the "-p", "<password>" pair used throughout this package) with the
// password value replaced by a fixed placeholder, so logging the command
// line for debugging never writes a plaintext DB password to the log
// file.
func redactPasswordArg(args []string) []string {
	redacted := make([]string, len(args))
	copy(redacted, args)
	for i, a := range redacted {
		if a == "-p" && i+1 < len(redacted) {
			redacted[i+1] = "****"
		}
	}
	return redacted
}

// mydumperMetadataComplete reports whether outDir contains a finished
// mydumper metadata file. mydumper renames metadata.partial to metadata only
// on genuine completion, independent of the internal error counter that
// drives its exit code (mydumper/mydumper#1300), so its presence is a
// reliable completion signal even when the exit status says otherwise.
func mydumperMetadataComplete(outDir string) bool {
	info, err := os.Stat(filepath.Join(outDir, "metadata"))
	return err == nil && !info.IsDir()
}

// classifyMydumperLine reports whether a stderr line is a GLib WARNING, and
// whether it signals a real failure: CRITICAL/ERROR, or the WARNING mydumper
// emits when a table's data query fails ("Failed to execute query" — the
// table's rows are then silently missing).
func classifyMydumperLine(line string) (warn, severe bool) {
	severe = strings.Contains(line, "CRITICAL **") || strings.Contains(line, "ERROR **") || strings.Contains(line, "Failed to execute query")
	warn = strings.Contains(line, "WARNING **")
	return warn, severe
}

// mydumperExitIsBenign reports whether a non-zero mydumper exit can be
// treated as success: the metadata file confirms completion, at least one
// WARNING explains the bumped error counter (mydumper/mydumper#1300), and
// nothing severe was logged. A non-zero exit with no explanation at all —
// or with a metadata file but a real error — stays a failure.
func mydumperExitIsBenign(outDir string, sawWarning, sawSevere bool) bool {
	return mydumperMetadataComplete(outDir) && sawWarning && !sawSevere
}

func RunDump(cfg types.Config, pass string) string {

	logger.Info("starting dump for config %q (host=%s port=%s user=%s)", cfg.Name, cfg.Host, cfg.Port, cfg.User)

	s := settings.Load()
	started := time.Now()
	printDumpHeader(cfg, s)

	cfg, cleanup := connection.ApplyTunnels(cfg, s)
	defer cleanup()

	// ---------------- output dir ----------------
	prep := ui.StartTask("Preparing")
	if err := os.MkdirAll(s.WorkDir, 0755); err != nil {
		logger.Error("failed to create work directory %q: %v", s.WorkDir, err)
		prep.Fail(fmt.Errorf("cannot create work directory: %w", err))
		os.Exit(1)
	}

	outDir := filepath.Join(s.WorkDir, fmt.Sprintf("%s_%s",
		cfg.Name,
		time.Now().Format("2006-01-02_150405"),
	))

	lockDir, err := paths.DbtoolDir()
	if err != nil {
		prep.Fail(fmt.Errorf("dump lock failed: %w", err))
		os.Exit(1)
	}
	release, err := filelock.Acquire(lockDir, "dump:"+s.WorkDir+":"+cfg.Name, false)
	if err != nil {
		prep.Fail(fmt.Errorf("dump blocked: %w", err))
		ui.Hint("Another dump of this config is already running.")
		os.Exit(1)
	}
	defer release()
	// mydumper requires an empty output directory. Stage outside the published
	// naming convention, then atomically rename only after successful completion.
	finalDir := outDir
	if _, err := os.Lstat(finalDir); !os.IsNotExist(err) {
		prep.Fail(fmt.Errorf("dump directory already exists or cannot be checked: %s", finalDir))
		os.Exit(1)
	}
	outDir, err = os.MkdirTemp(s.WorkDir, ".dbtool-pending-")
	if err != nil {
		prep.Fail(fmt.Errorf("cannot create staging directory: %w", err))
		os.Exit(1)
	}

	logger.Debug("dump output directory: %s", outDir)

	// ---------------- tool selection ----------------
	mydumperPath, err := findExecutable("mydumper")
	if err != nil {
		logger.Error("mydumper not found in PATH or common directories")
		prep.Fail(fmt.Errorf("mydumper is not installed"))
		ui.Hint("Install it with %s, or see https://github.com/mydumper/mydumper/releases", ui.Code("dbtool install"))
		os.Exit(1)
	}
	logger.Debug("using mydumper at %s", mydumperPath)
	mydumperVersion := detectMydumperVersion(mydumperPath)
	prep.Done(toolLabel("mydumper", mydumperVersion))

	// ---------------- mydumper ----------------
	args := []string{"-h", cfg.Host, "-P", cfg.Port, "-u", cfg.User}
	if pass != "" {
		args = append(args, "-p", pass)
	}

	args = append(args,
		"-o", outDir,
		"-t", "8",
		"--compress",
		"-v", "3",
	)
	if cfg.NoLocks {
		lockArgs := lockModeArgs(mydumperVersion)
		args = append(args, lockArgs...)
		logger.Debug("mydumper: no-locks requested (user lacks RELOAD/BACKUP_ADMIN privilege) -> %s", strings.Join(lockArgs, " "))
	}

	if regex := buildExclusionRegex(cfg.IgnoredSchemas, cfg.IgnoredTables); regex != "" {
		args = append(args, "--regex", regex)
		logger.Debug("mydumper regex: %s", regex)
		if len(cfg.IgnoredSchemas) > 0 {
			logger.Debug("dump ignoring schemas: %s", strings.Join(cfg.IgnoredSchemas, ", "))
		}
		if len(cfg.IgnoredTables) > 0 {
			for schema, tables := range cfg.IgnoredTables {
				logger.Debug("dump ignoring tables in %s: %s", schema, strings.Join(tables, ", "))
			}
		}
	}

	cmd := exec.Command(mydumperPath, args...)
	logger.Debug("dump command: %s %s", mydumperPath, strings.Join(redactPasswordArg(args), " "))
	stdout, _ := cmd.StdoutPipe()
	stderr, _ := cmd.StderrPipe()

	if err := cmd.Start(); err != nil {
		logger.Error("failed to start mydumper: %v", err)
		ui.Fatal("Failed to start mydumper: %v", err)
	}

	// ---------------- progress tracking ----------------
	// Pre-count tables so we can show a real progress bar with ETA from the
	// start, rather than an indefinite spinner for the entire dump duration.
	totalTables := countTablesToProcess(cfg, pass)

	var (
		barMu      sync.Mutex
		lastBarVal int // highest value ever passed to bar.Set; prevents backward jumps
	)

	done := make(chan struct{})

	// A total of 0 (pre-count failed) starts the bar in spinner mode; it is
	// switched to a real bar in place if mydumper reports "[x/y]" below.
	bar := progress.New(totalTables, "Dumping "+cfg.Name)

	// Goroutine: animate the spinner or update the real bar every 500 ms by
	// counting table-schema files that mydumper has written to the output dir.
	go func() {
		ticker := time.NewTicker(500 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				barMu.Lock()
				if bar.Total() > 0 {
					if n := countDumpedTables(outDir); n > lastBarVal {
						lastBarVal = n
						_ = bar.Set(n)
					}
				}
				barMu.Unlock()
			case <-done:
				return
			}
		}
	}()

	// Goroutine: parse mydumper verbose (-v 3) stderr lines.  Each "Thread N
	// dumping schema for …" line corresponds to one table being processed.  We
	// count them to drive the real bar; if the pre-count DB query failed we use
	// the first [X/Y] line (legacy format) to upgrade the spinner instead.
	stderrDone := make(chan struct{})
	var sawWarning, sawSevere atomic.Bool
	go func() {
		defer close(stderrDone)
		var stderrCount int
		scanner := bufio.NewScanner(stderr)
		for scanner.Scan() {
			line := scanner.Text()
			logger.Debug("[mydumper stderr] %s", line)

			warn, severe := classifyMydumperLine(line)
			if warn {
				sawWarning.Store(true)
			}
			if severe {
				sawSevere.Store(true)
			}

			if progress.ParseDumpingTable(line) {
				barMu.Lock()
				stderrCount++
				if stderrCount > lastBarVal {
					lastBarVal = stderrCount
					_ = bar.Set(stderrCount)
				}
				barMu.Unlock()
				continue
			}

			cur, tot, ok := progress.ParseTableProgress(line)
			if !ok {
				continue
			}

			barMu.Lock()
			if bar.Total() <= 0 {
				// Upgrade: turn the indefinite spinner into a real bar.
				bar.SetTotal(tot)
			}
			if cur > lastBarVal {
				lastBarVal = cur
				_ = bar.Set(cur)
			}
			barMu.Unlock()
		}
	}()

	stdoutDone := make(chan struct{})
	go func() {
		defer close(stdoutDone)
		scanner := bufio.NewScanner(stdout)
		for scanner.Scan() {
			logger.Debug("[mydumper stdout] %s", scanner.Text())
		}
	}()

	<-stdoutDone
	<-stderrDone
	err = cmd.Wait()
	close(done)

	_ = bar.Finish()
	dumpedTables := bar.Current()
	if t := bar.Total(); t > dumpedTables {
		dumpedTables = t
	}
	dumpElapsed := bar.Elapsed()

	if err != nil {
		if mydumperExitIsBenign(outDir, sawWarning.Load(), sawSevere.Load()) {
			logger.Warn("mydumper exited with a non-zero status for config %q, but its metadata file confirms the dump completed — likely a non-fatal MySQL warning (e.g. insufficient privilege to read binlog position) incorrectly driving the exit code; see https://github.com/mydumper/mydumper/issues/1300: %v", cfg.Name, err)
			ui.Warn("mydumper reported a non-zero exit status, but the dump completed successfully")
			ui.Hint("A known mydumper quirk — see %s for details.", ui.Code("dbtool log"))
		} else {
			logger.Error("dump failed for config %q: %v", cfg.Name, err)
			ui.Error("Dump failed: %v", err)
			ui.Hint("mydumper's full output is in the log — run %s", ui.Code("dbtool log --level debug"))
			os.Exit(1)
		}
	}

	if !mydumperMetadataComplete(outDir) {
		ui.Fatal("Dump failed: mydumper did not produce completed metadata")
	}
	ui.Success("Dumped %s  %s", ui.Plural(dumpedTables, "table", "tables"), ui.Muted(ui.Duration(dumpElapsed)))
	logger.Info("dump completed for config %q: %s", cfg.Name, outDir)

	verify := ui.StartTask("Verifying dump")
	if err := backupstate.Begin(outDir); err != nil {
		verify.Fail(fmt.Errorf("cannot mark pending dump: %w", err))
		os.Exit(1)
	}
	// Patch known dump-portability issues before this dump is retained or
	// uploaded — see PatchDumpDir for what it does and doesn't touch.
	PatchDumpDir(outDir)

	// Safety net: mydumper (especially older versions) can silently drop a
	// column from a table's INSERT statements — e.g. a known issue where
	// MySQL 8's DEFAULT_GENERATED marking on DEFAULT/ON UPDATE
	// CURRENT_TIMESTAMP columns gets misread as a true GENERATED column and
	// excluded. When that happens the real value is never captured at all,
	// and restoring silently substitutes the column's DEFAULT instead. Warn
	// loudly if this dump shows that pattern, without failing the dump.
	issues := ValidateDump(outDir)
	if len(issues) > 0 {
		verify.Warn(fmt.Sprintf("%s may have lost column data", ui.Plural(len(issues), "table", "tables")))
	} else {
		verify.Done("")
	}
	ReportValidationIssues(issues)
	if err := backupstate.Finish(outDir); err != nil {
		ui.Fatal("Cannot publish dump: %v", err)
	}
	if err := os.Rename(outDir, finalDir); err != nil {
		ui.Fatal("Cannot publish dump directory: %v", err)
	}
	outDir = finalDir
	dumpSize := ui.DirSize(outDir)
	ui.Success("Saved  %s", ui.Muted(ui.ShortPath(outDir)+"  ("+ui.Bytes(dumpSize)+")"))
	location := ui.ShortPath(outDir)
	var warnings []string
	if len(issues) > 0 {
		warnings = append(warnings, fmt.Sprintf("%s may have lost column data (see above)", ui.Plural(len(issues), "table", "tables")))
	}

	// ---------------- S3 upload ----------------
	// The local copy is kept until after Telegram delivery below (which reads
	// outDir from disk) and is only removed once both are done.
	if s.S3Enabled() {
		upload := ui.StartTask("Uploading to S3")
		logger.Info("uploading dump to S3 bucket %q prefix %q", s.S3.Bucket, s.S3.Prefix)
		if err := s3store.UploadDir(s.S3, outDir); err != nil {
			logger.Error("S3 upload failed: %v", err)
			upload.Fail(err)
			ui.Hint("Check your S3 settings with %s", ui.Code("dbtool setting s3 test"))
			os.Exit(1)
		}
		logger.Info("S3 upload completed for %s", outDir)
		dumpDirName := filepath.Base(outDir)
		s3Path := dumpDirName
		if s.S3.Prefix != "" {
			s3Path = strings.TrimSuffix(s.S3.Prefix, "/") + "/" + dumpDirName
		}
		location = fmt.Sprintf("s3://%s/%s/", s.S3.Bucket, s3Path)
		upload.Done(location)
	}

	// Rotate old dump directories based on the configured retention period.
	if s.S3Enabled() {
		// In S3 mode rotate old S3 dumps; local dumps are handled below.
		rotate.RotateOldS3Dumps(s.S3, cfg.Name, cfg.RetentionDays)
	} else {
		rotate.RotateOldDumps(s.WorkDir, cfg.Name, cfg.RetentionDays)
	}

	// ---------------- Telegram delivery ----------------
	// Runs after the dump has been persisted (S3 upload above, or already on
	// local disk), regardless of which storage backend is configured.
	// Delivery failures are logged and reported but never fail the dump — the
	// local/S3 copy is already safe by this point.
	if s.Telegram.Enabled() && !cfg.TelegramDisabled {
		tg := ui.StartTask("Sending to Telegram")
		if err := telegram.SendDump(s.Telegram, outDir); err != nil {
			logger.Error("telegram delivery failed for %q: %v", outDir, err)
			tg.Warn(err.Error())
			warnings = append(warnings, "Telegram delivery failed: "+err.Error())
		} else {
			logger.Info("telegram delivery completed for %s", outDir)
			tg.Done("")
		}
	}

	// ---------------- cleanup local copy (S3 mode only) ----------------
	if s.S3Enabled() {
		logger.Debug("removing local dump dir after S3 upload: %s", outDir)
		if err := os.RemoveAll(outDir); err != nil {
			logger.Error("failed to remove local dump dir %s after S3 upload: %v", outDir, err)
			ui.Warn("Could not remove local dump dir %s: %v", outDir, err)
		}
	}

	printDumpSummary(cfg, location, dumpedTables, dumpSize, time.Since(started), warnings)
	return outDir
}

// printDumpHeader prints the "◆ Dump · name" title with where the dump comes
// from and where it is going.
func printDumpHeader(cfg types.Config, s settings.Settings) {
	ui.Title("Dump · "+cfg.Name, "")
	route := "direct"
	if cfg.SSH {
		route = "SSH tunnel via " + cfg.SSHUser + "@" + cfg.SSHHost
	}
	if s.Proxy.Enabled() {
		route += " + SOCKS5 proxy"
	}
	dest := ui.ShortPath(s.WorkDir)
	if s.S3Enabled() {
		dest = "s3://" + s.S3.Bucket + "/" + strings.TrimSuffix(s.S3.Prefix, "/")
	}
	tg := ""
	if s.Telegram.Enabled() && !cfg.TelegramDisabled {
		tg = "yes"
	}
	ui.PrintKeyValues(
		ui.KV{Key: "Source", Value: fmt.Sprintf("%s@%s:%s", cfg.User, cfg.Host, cfg.Port)},
		ui.KV{Key: "Route", Value: route},
		ui.KV{Key: "Destination", Value: dest},
		ui.KV{Key: "Telegram", Value: tg},
	)
	ui.Blank()
}

// printDumpSummary prints the end-of-dump box.
func printDumpSummary(cfg types.Config, location string, tables int, size int64, took time.Duration, warnings []string) {
	kind, title := ui.BadgeSuccess, "Dump complete"
	if len(warnings) > 0 {
		kind, title = ui.BadgeWarn, "Dump complete with warnings"
	}
	lines := []string{strings.TrimRight(ui.KeyValues(
		ui.KV{Key: "Config", Value: cfg.Name},
		ui.KV{Key: "Location", Value: location},
		ui.KV{Key: "Tables", Value: fmt.Sprint(tables)},
		ui.KV{Key: "Size", Value: ui.Bytes(size)},
		ui.KV{Key: "Took", Value: ui.Duration(took)},
	), "\n")}
	for _, w := range warnings {
		lines = append(lines, ui.Yellow(ui.Icon(ui.IconWarn)+" "+w))
	}
	ui.Blank()
	ui.PrintBox(title, kind, lines...)
}

// toolLabel renders "mydumper 0.16.9" for the Preparing stage.
func toolLabel(name string, v toolVersion) string {
	if v.OK {
		return fmt.Sprintf("%s %d.%d.%d", name, v.Major, v.Minor, v.Patch)
	}
	return name
}
