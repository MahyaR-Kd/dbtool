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
	"time"

	"dbtool/internal/connection"
	"dbtool/internal/logger"
	"dbtool/internal/progress"
	"dbtool/internal/rotate"
	"dbtool/internal/s3store"
	"dbtool/internal/settings"
	"dbtool/internal/telegram"
	"dbtool/internal/types"

	pb "github.com/schollz/progressbar/v3"
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

func RunDump(cfg types.Config, pass string) string {

	logger.Info("starting dump for config %q (host=%s port=%s user=%s)", cfg.Name, cfg.Host, cfg.Port, cfg.User)

	s := settings.Load()

	cfg, cleanup := connection.ApplyTunnels(cfg, s)
	defer cleanup()

	// ---------------- output dir ----------------
	if err := os.MkdirAll(s.WorkDir, 0755); err != nil {
		logger.Error("failed to create work directory %q: %v", s.WorkDir, err)
		fmt.Println("Failed to create work directory:", err)
		os.Exit(1)
	}

	outDir := filepath.Join(s.WorkDir, fmt.Sprintf("%s_%s",
		cfg.Name,
		time.Now().Format("2006-01-02_150405"),
	))

	logger.Debug("dump output directory: %s", outDir)

	// ---------------- tool selection ----------------
	mydumperPath, err := findExecutable("mydumper")
	if err != nil {
		logger.Error("mydumper not found in PATH or common directories")
		fmt.Println("Error: mydumper is not available. Please install mydumper.")
		os.Exit(1)
	}
	logger.Debug("using mydumper at %s", mydumperPath)
	mydumperVersion := detectMydumperVersion(mydumperPath)

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
	logger.Debug("dump command: %s", strings.Join(cmd.Args, " "))
	stdout, _ := cmd.StdoutPipe()
	stderr, _ := cmd.StderrPipe()

	if err := cmd.Start(); err != nil {
		logger.Error("failed to start mydumper: %v", err)
		fmt.Println("Failed:", err)
		os.Exit(1)
	}

	// ---------------- progress tracking ----------------
	// Pre-count tables so we can show a real progress bar with ETA from the
	// start, rather than an indefinite spinner for the entire dump duration.
	totalTables := countTablesToProcess(cfg, pass)

	var (
		barMu      sync.Mutex
		realBar    *pb.ProgressBar
		spinner    *pb.ProgressBar
		lastBarVal int // highest value ever passed to realBar.Set; prevents backward jumps
	)

	done := make(chan struct{})

	if totalTables > 0 {
		realBar = progress.New(totalTables, "Dumping…")
	} else {
		spinner = progress.SpinnerBar("Dumping…")
	}

	// Goroutine: animate the spinner or update the real bar every 500 ms by
	// counting table-schema files that mydumper has written to the output dir.
	go func() {
		ticker := time.NewTicker(500 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				barMu.Lock()
				if realBar != nil {
					if n := countDumpedTables(outDir); n > lastBarVal {
						lastBarVal = n
						_ = realBar.Set(n)
					}
				} else if spinner != nil {
					_ = spinner.Add(0)
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
	go func() {
		var stderrCount int
		scanner := bufio.NewScanner(stderr)
		for scanner.Scan() {
			line := scanner.Text()
			logger.Debug("[mydumper stderr] %s", line)

			if progress.ParseDumpingTable(line) {
				barMu.Lock()
				stderrCount++
				if realBar != nil && stderrCount > lastBarVal {
					lastBarVal = stderrCount
					_ = realBar.Set(stderrCount)
				}
				barMu.Unlock()
				continue
			}

			cur, tot, ok := progress.ParseTableProgress(line)
			if !ok {
				continue
			}

			barMu.Lock()
			if spinner != nil {
				// Upgrade: replace the indefinite spinner with a real bar.
				_ = spinner.Finish()
				fmt.Println()
				spinner = nil
				realBar = progress.New(tot, "Dumping…")
			}
			if realBar != nil && cur > lastBarVal {
				lastBarVal = cur
				_ = realBar.Set(cur)
			}
			barMu.Unlock()
		}
	}()

	go func() {
		scanner := bufio.NewScanner(stdout)
		for scanner.Scan() {
			logger.Debug("[mydumper stdout] %s", scanner.Text())
		}
	}()

	err = cmd.Wait()
	close(done)

	barMu.Lock()
	if realBar != nil {
		_ = realBar.Finish()
	} else if spinner != nil {
		_ = spinner.Finish()
	}
	barMu.Unlock()
	fmt.Println()

	if err != nil {
		logger.Error("dump failed for config %q: %v", cfg.Name, err)
		fmt.Println("Dump FAILED:", err)
		os.Exit(1)
	}

	logger.Info("dump completed for config %q: %s", cfg.Name, outDir)

	// mydumper can preserve ANSI-style double-quoted identifiers from the
	// source server. Convert table schema files to MySQL's portable backtick
	// form before this dump is retained or uploaded.
	PatchDumpDir(outDir)
	fmt.Println("Dump completed:", outDir)

	// Safety net: mydumper (especially older versions) can silently drop a
	// column from a table's INSERT statements — e.g. a known issue where
	// MySQL 8's DEFAULT_GENERATED marking on DEFAULT/ON UPDATE
	// CURRENT_TIMESTAMP columns gets misread as a true GENERATED column and
	// excluded. When that happens the real value is never captured at all,
	// and restoring silently substitutes the column's DEFAULT instead. Warn
	// loudly if this dump shows that pattern, without failing the dump.
	ReportValidationIssues(ValidateDump(outDir))

	// Rotate old dump directories based on the configured retention period.
	if s.StorageType == settings.StorageS3 {
		// In S3 mode rotate old S3 dumps; local dumps are handled below.
		rotate.RotateOldS3Dumps(s.S3, cfg.Name, cfg.RetentionDays)
	} else {
		rotate.RotateOldDumps(s.WorkDir, cfg.Name, cfg.RetentionDays)
	}

	// ---------------- S3 upload ----------------
	// The local copy is kept until after Telegram delivery below (which reads
	// outDir from disk) and is only removed once both are done.
	if s.StorageType == settings.StorageS3 {
		fmt.Println("Uploading dump to S3…")
		logger.Info("uploading dump to S3 bucket %q prefix %q", s.S3.Bucket, s.S3.Prefix)
		if err := s3store.UploadDir(s.S3, outDir); err != nil {
			logger.Error("S3 upload failed: %v", err)
			fmt.Println("S3 upload FAILED:", err)
			os.Exit(1)
		}
		logger.Info("S3 upload completed for %s", outDir)
		dumpDirName := filepath.Base(outDir)
		s3Path := dumpDirName
		if s.S3.Prefix != "" {
			s3Path = strings.TrimSuffix(s.S3.Prefix, "/") + "/" + dumpDirName
		}
		fmt.Printf("S3 upload completed: s3://%s/%s/\n", s.S3.Bucket, s3Path)
	}

	// ---------------- Telegram delivery ----------------
	// Runs after the dump has been persisted (S3 upload above, or already on
	// local disk), regardless of which storage backend is configured.
	// Delivery failures are logged and reported but never fail the dump — the
	// local/S3 copy is already safe by this point.
	if s.Telegram.Enabled() {
		fmt.Println("Sending dump to Telegram…")
		if err := telegram.SendDump(s.Telegram, outDir); err != nil {
			logger.Error("telegram delivery failed for %q: %v", outDir, err)
			fmt.Println("Warning: failed to send dump to Telegram:", err)
		} else {
			logger.Info("telegram delivery completed for %s", outDir)
			fmt.Println("Telegram delivery completed.")
		}
	}

	// ---------------- cleanup local copy (S3 mode only) ----------------
	if s.StorageType == settings.StorageS3 {
		logger.Debug("removing local dump dir after S3 upload: %s", outDir)
		if err := os.RemoveAll(outDir); err != nil {
			logger.Error("failed to remove local dump dir %s after S3 upload: %v", outDir, err)
			fmt.Printf("Warning: could not remove local dump dir %s: %v\n", outDir, err)
		}
	}

	return outDir
}
