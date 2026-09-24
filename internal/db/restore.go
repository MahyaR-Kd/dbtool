package db

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"dbtool/internal/connection"
	"dbtool/internal/filelock"
	"dbtool/internal/logger"
	"dbtool/internal/paths"
	"dbtool/internal/progress"
	"dbtool/internal/settings"
	"dbtool/internal/types"

	pb "github.com/schollz/progressbar/v3"
)

// myloaderRestoreCompletedMarker is the message myloader logs unconditionally
// as the very last thing it does in main() — after schema, data, checksums,
// and cleanup have all finished — right before computing its exit code from
// its internal error counter (myloader.c: `exit_code = errors ? ... `). That
// counter is bumped by any warning-level MySQL response, the same
// non-fatal-warnings-drive-exit-code bug as mydumper/mydumper#1300, so this
// marker is a reliable completion signal even when the exit status says
// otherwise.
const myloaderRestoreCompletedMarker = "Restore completed"

// myloaderErrorsFoundMarker starts the summary myloader prints (print_errors
// in myloader.c) just before "Restore completed" whenever any schema, data,
// index, or other real error was counted. If it appears, the completion
// marker does not mean the restore was clean, so the exit status stands.
const myloaderErrorsFoundMarker = "Errors found:"

// isMyloaderRestoreCompletedLine reports whether a line of myloader's stderr
// is (or contains) its unconditional completion message.
func isMyloaderRestoreCompletedLine(line string) bool {
	return strings.Contains(line, myloaderRestoreCompletedMarker)
}

// myloaderReadOnlyErrorMarker is the substring of the MySQL error message
// (ERROR 1290) myloader logs verbatim when the destination server has
// read_only or super_read_only enabled and rejects a write statement.
// RunRestore treats this specially — see restoreBackoff — instead of just
// failing outright, since it can be a slow managed-MySQL failover or
// maintenance event rather than a real, permanent misconfiguration.
const myloaderReadOnlyErrorMarker = "running with the --read-only option"

// isMyloaderReadOnlyErrorLine reports whether a line of myloader's stderr
// output is a read-only rejection from the destination MySQL server.
func isMyloaderReadOnlyErrorLine(line string) bool {
	return strings.Contains(line, myloaderReadOnlyErrorMarker)
}

// restoreBackoff is how long restoreOneDatabase waits before each successive
// retry of a database's restore after it fails because the destination was
// read-only: 1, 2, 4, 8, 16 minutes, doubling each time — len(restoreBackoff)+1
// is the most attempts any one database gets. A real, sustained failover on
// a managed MySQL instance can take several minutes to resolve; a fixed
// short timeout (or myloader's own single, zero-delay retry — see
// restore_data_in_gstring_by_statement in src/myloader/myloader_restore.c)
// gives up long before that.
var restoreBackoff = []time.Duration{
	1 * time.Minute,
	2 * time.Minute,
	4 * time.Minute,
	8 * time.Minute,
	16 * time.Minute,
}

// myloaderRaceWorkaroundFile is the filename myloader treats as a leftover
// artifact of a previously interrupted --stream dump: if present, its
// initialize_process_file_type() (src/myloader/myloader_process_file_type.c)
// starts myloader's file-type classification pool with a single thread
// instead of its default 4, purely because it exists — its content is
// never read for this check.
//
// That single-threading side effect is also, incidentally, the only
// available way to work around a genuine, still-open race condition in
// that same pool. myloader guarantees the "metadata" file is classified
// as METADATA_GLOBAL and dequeued before any SCHEMA_TABLE item (the queue
// is kept sorted by file type — file_type_push()/file_type_cmp() in the
// same file), but with multiple concurrent workers that only orders items
// already sitting in the queue: it does not stop an idle worker from
// grabbing a SCHEMA_TABLE job the instant it's classified, while a
// different worker is still busy inside process_metadata_global_filename()
// (myloader_process.c) actually parsing the dump's own metadata and
// setting identifier_quote_character_str from it. A dump whose metadata
// declares "quote-character = DOUBLE_QUOTE" can lose that race: a schema
// file gets checked against the compiled-in BACKTICK default before
// metadata has flipped identifier_quote_character_str to double-quote,
// and myloader aborts with "Identifier quote character (`) not found...".
//
// Confirmed against myloader's own source — both the exact myloader
// version this crash was reproduced on (v1.0.5-1) and current upstream
// master still have this race unchanged; it is a different, still-unfixed
// bug from the superficially similar, already-fixed
// mydumper/mydumper#1934/#1936 ("forgot to initialize
// identifier_quote_character_str"), whose fix is present in both.
//
// A dump whose metadata declares BACKTICK never needs
// identifier_quote_character_str to change from its compiled-in default
// at all, so it can never lose this race and doesn't need the workaround
// — see dumpMetadataQuoteCharacterIsDoubleQuote and its caller below.
const myloaderRaceWorkaroundFile = "metadata.partial.0"

// applyMyloaderQuoteCharacterRaceWorkaround creates myloaderRaceWorkaroundFile
// in dir when (and only when) its dump metadata declares double-quoted
// identifiers — see myloaderRaceWorkaroundFile for why. A no-op for any
// other dump, and leaves an already-present file (e.g. a genuine leftover
// from an interrupted --stream dump, which forces the same single-threaded
// behavior on its own) untouched.
func applyMyloaderQuoteCharacterRaceWorkaround(dir string) {
	if !dumpMetadataQuoteCharacterIsDoubleQuote(dir) {
		return
	}
	path := filepath.Join(dir, myloaderRaceWorkaroundFile)
	if _, err := os.Stat(path); err == nil {
		return
	}
	if err := os.WriteFile(path, nil, 0644); err != nil { // #nosec G306 -- placeholder file, content is never read
		logger.Debug("applyMyloaderQuoteCharacterRaceWorkaround: failed to create %s: %v", path, err)
	}
}

// countRestoredTables returns the number of table-schema files found in dir
// belonging to db (or every table-schema file in dir, if db is "") — the
// number of tables a myloader invocation scoped to db (via --source-db)
// will restore.
func countRestoredTables(dir, db string) int {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return 0
	}
	n := 0
	for _, e := range entries {
		if e.IsDir() || !isTableSchemaFile(e.Name()) {
			continue
		}
		if db != "" && !strings.HasPrefix(e.Name(), db+".") {
			continue
		}
		n++
	}
	return n
}

// dumpSchemaCreateSuffixes are the file extensions mydumper writes a
// per-database "<db>-schema-create.sql" file with — exactly one per source
// database in the dump, regardless of its own compression setting.
var dumpSchemaCreateSuffixes = []string{"-schema-create.sql", "-schema-create.sql.gz", "-schema-create.sql.zst"}

// listDumpDatabases returns the distinct source database names present in
// dir, sorted for a deterministic restore order — see RunRestore for why it
// restores one database at a time instead of the whole dump in one
// myloader invocation. Returns nil if dir has no recognizable
// "<db>-schema-create.sql[.gz|.zst]" files (e.g. a dump taken with
// --no-schemas, or from a mydumper old enough not to write them),  letting
// RunRestore fall back to a single whole-dump invocation.
func listDumpDatabases(dir string) []string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var dbs []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		for _, suffix := range dumpSchemaCreateSuffixes {
			if strings.HasSuffix(name, suffix) {
				dbs = append(dbs, strings.TrimSuffix(name, suffix))
				break
			}
		}
	}
	sort.Strings(dbs)
	return dbs
}

func RunRestore(cfg types.Config, pass, dir string, overwriteTables bool) {
	lockDir, err := paths.DbtoolDir()
	if err != nil {
		fmt.Println("Restore lock failed:", err)
		os.Exit(1)
	}
	release, err := filelock.Acquire(lockDir, "restore:"+cfg.Host+":"+cfg.Port, false)
	if err != nil {
		fmt.Println("Restore blocked:", err)
		os.Exit(1)
	}
	defer release()

	logger.Info("starting restore into config %q (dir=%s overwrite-tables=%v)", cfg.Name, dir, overwriteTables)

	// Log the configured destination before ApplyTunnels rewrites cfg.Host/
	// cfg.Port to the local tunnel endpoint (127.0.0.1:<port>) — this is the
	// only place the actual remote target (what an SSH tunnel forwards to,
	// or the plain host:port with no tunnel) is visible at all. A restore
	// that deterministically fails against one config but not others is as
	// likely to be this address pointing at the wrong physical server as
	// anything on the MySQL side.
	if cfg.SSH {
		logger.Debug("restore target for config %q: db=%s:%s via SSH tunnel %s@%s:%s", cfg.Name, cfg.Host, cfg.Port, cfg.SSHUser, cfg.SSHHost, cfg.SSHPort)
	} else {
		logger.Debug("restore target for config %q: db=%s:%s (no SSH tunnel)", cfg.Name, cfg.Host, cfg.Port)
	}

	s := settings.Load()

	cfg, cleanup := connection.ApplyTunnels(cfg, s)
	defer cleanup()

	logger.Debug("restore target for config %q resolved to local endpoint %s:%s", cfg.Name, cfg.Host, cfg.Port)

	myloaderPath, err := findExecutable("myloader")
	if err != nil {
		logger.Error("myloader not found in PATH or common directories")
		fmt.Println("Error: myloader is not available. Please install myloader.")
		os.Exit(1)
	}
	myloaderVersion := detectMyloaderVersion(myloaderPath)

	// Re-run the same dump-portability patches before myloader runs, as a
	// safety net for a dump that never went through them at dump time (one
	// taken by an older dbtool version, or downloaded from S3/Telegram) —
	// see PatchDumpDir for what it does and doesn't touch.
	PatchDumpDir(dir)

	// Safety net: warn before loading data if this dump shows the same
	// column-dropping pattern checked for at dump time (see ValidateDump).
	// Restoring from an already-taken dump (downloaded from S3, or simply
	// dumped a while ago with an older mydumper) never went through that
	// check, so it's worth re-running here before the data actually lands.
	ReportValidationIssues(ValidateDump(dir))

	// Work around a still-open myloader race condition for ANSI-quoted
	// (double-quote) dumps — see myloaderRaceWorkaroundFile.
	applyMyloaderQuoteCharacterRaceWorkaround(dir)

	args := []string{"-h", cfg.Host, "-P", cfg.Port, "-u", cfg.User}
	if pass != "" {
		args = append(args, "-p", pass)
	}

	args = append(args, "-d", dir, "-v", "3")

	if overwriteTables {
		overwriteArgs := overwriteTablesArgs(myloaderVersion)
		args = append(args, overwriteArgs...)
		// Concurrent DROP/CREATE operations on foreign-key-related tables can
		// deadlock on MySQL metadata locks, even with foreign_key_checks=0.
		// Serialize schema workers while retaining parallel data loading.
		args = append(args, "--max-threads-for-schema-creation=1")
		logger.Debug("myloader: overwrite-tables requested -> %s", strings.Join(overwriteArgs, " "))
		logger.Debug("myloader: serializing schema changes to avoid overwrite metadata-lock deadlocks")
	}

	logger.Debug("myloader base command: %s %s", myloaderPath, strings.Join(redactPasswordArg(args), " "))

	// Restore one source database per myloader invocation (via --source-db)
	// instead of the whole dump in one shot. myloader has no crash-safe
	// checkpoint to resume from (its --resume mechanism only writes a
	// marker on a graceful Ctrl+C shutdown, never on the g_critical abort a
	// read-only destination triggers — see myloader_restore_job.c's
	// "Writing resume.partial file" handler), so a database that already
	// finished loading must never be re-run: dropping into that granularity
	// is the only way to "resume from the failed part" rather than restart
	// the whole restore after a failure well into it. A dump without the
	// per-database schema-create files this relies on (see
	// listDumpDatabases) falls back to one whole-dump invocation, matching
	// the old, pre-batching behavior.
	databases := listDumpDatabases(dir)
	if len(databases) == 0 {
		databases = []string{""}
	}

	for i, dbName := range databases {
		dbArgs := args
		if dbName != "" {
			dbArgs = append(append([]string{}, args...), "-s", dbName)
			fmt.Printf("Restoring database %q (%d of %d)…\n", dbName, i+1, len(databases))
			logger.Info("restoring database %q (%d of %d) for config %q", dbName, i+1, len(databases), cfg.Name)
		}

		if err := restoreOneDatabase(cfg, pass, myloaderPath, dbArgs, dir, dbName); err != nil {
			logger.Error("restore failed for config %q: %v", cfg.Name, err)
			fmt.Println("Restore FAILED:", err)
			os.Exit(1)
		}
	}

	logger.Info("restore completed for config %q", cfg.Name)
}

// restoreOneDatabase runs myloader against dbArgs — already scoped to one
// source database via "-s", or the whole dump if dbName is "" — retrying
// with exponential backoff (see restoreBackoff) when it fails specifically
// because the destination was read-only. Returns nil on success, or the final
// error once retries are exhausted or a non-read-only failure occurs.
func restoreOneDatabase(cfg types.Config, pass, myloaderPath string, dbArgs []string, dir, dbName string) error {
	maxAttempts := len(restoreBackoff) + 1

	var lastErr error
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		if attempt > 1 {
			logger.Info("retrying restore for config %q (attempt %d of %d)", cfg.Name, attempt, maxAttempts)
		}

		var readOnlyFailure bool
		lastErr, readOnlyFailure = runMyloaderOnce(cfg, myloaderPath, dbArgs, dir, dbName)
		if lastErr == nil {
			return nil
		}

		if !readOnlyFailure || !restoreCanRestart(dbArgs) || attempt == maxAttempts {
			return lastErr
		}

		delay := restoreBackoff[attempt-1]
		logger.Warn("restore failed for config %q because the destination briefly went read-only mid-restore; retrying in %s: %v", cfg.Name, delay, lastErr)
		fmt.Printf("Warning: destination went read-only mid-restore. Retrying in %s…\n", delay)
		if id, checkErr := checkDestinationServer(cfg, pass); checkErr == nil {
			logger.Debug("destination check before retry for config %q: @@hostname=%s @@server_id=%s read_only=%s", cfg.Name, id.hostname, id.serverID, orNone(id.readOnlyVar))
		}
		time.Sleep(delay)
	}
	return lastErr
}

// runMyloaderOnce runs a single myloader invocation against dir (scoped to
// dbName's tables if dbName is non-empty, matching the -s in args — dbName
// is only needed here to size the progress bar), driving the progress bar
// and log capture. A nil error means success — including the false-failure
// case where myloader exited non-zero but its own "Restore completed" marker
// confirms it actually finished (see isMyloaderRestoreCompletedLine).
// A non-nil error's readOnly return is true when myloader aborted
// specifically because the destination rejected a write with
// "--read-only option" — see isMyloaderReadOnlyErrorLine — which
// restoreOneDatabase uses to decide whether retrying is worth it.
func runMyloaderOnce(cfg types.Config, myloaderPath string, args []string, dir, dbName string) (err error, readOnly bool) {
	cmd := exec.Command(myloaderPath, args...)

	stderr, _ := cmd.StderrPipe()
	stdout, _ := cmd.StdoutPipe()

	if startErr := cmd.Start(); startErr != nil {
		logger.Error("restore failed to start for config %q: %v", cfg.Name, startErr)
		return fmt.Errorf("failed to start myloader: %w", startErr), false
	}

	// ---------------- progress tracking ----------------
	// Count table files in the dump directory so we can show a real bar.
	totalTables := countRestoredTables(dir, dbName)
	label := "Restoring…"
	if dbName != "" {
		label = fmt.Sprintf("Restoring %s…", dbName)
	}

	var (
		barMu   sync.Mutex
		realBar *pb.ProgressBar
		spinner *pb.ProgressBar
	)

	done := make(chan struct{})

	if totalTables > 0 {
		realBar = progress.New(totalTables, label)
	} else {
		spinner = progress.SpinnerBar(label)
	}

	// Goroutine: animate the spinner or advance the real bar every 500 ms.
	go func() {
		ticker := time.NewTicker(500 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				barMu.Lock()
				if spinner != nil {
					_ = spinner.Add(0)
				}
				barMu.Unlock()
			case <-done:
				return
			}
		}
	}()

	stdoutDone := make(chan struct{})
	go func() {
		defer close(stdoutDone)
		scanner := bufio.NewScanner(stdout)
		for scanner.Scan() {
			logger.Debug("[myloader stdout] %s", scanner.Text())
		}
	}()

	var restoreCompleted, errorsReported atomic.Bool
	var readOnlyDetected atomic.Bool
	stderrDone := make(chan struct{})

	// Goroutine: parse myloader verbose (-v 3) stderr lines.  Each "Thread N
	// restoring …" line corresponds to one table chunk being loaded.  We count
	// them to drive the real bar; the legacy [X/Y] handler is kept as fallback.
	go func() {
		defer close(stderrDone)
		var stderrCount int
		scanner := bufio.NewScanner(stderr)
		for scanner.Scan() {
			line := scanner.Text()
			logger.Debug("[myloader stderr] %s", line)

			if isMyloaderRestoreCompletedLine(line) {
				restoreCompleted.Store(true)
			}
			if strings.Contains(line, myloaderErrorsFoundMarker) {
				errorsReported.Store(true)
			}
			if isMyloaderReadOnlyErrorLine(line) {
				readOnlyDetected.Store(true)
			}

			if progress.ParseRestoringTable(line) {
				barMu.Lock()
				stderrCount++
				if realBar != nil {
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
				realBar = progress.New(tot, label)
			}
			if realBar != nil {
				_ = realBar.Set(cur)
			}
			barMu.Unlock()
		}
	}()

	<-stdoutDone
	<-stderrDone
	waitErr := cmd.Wait()
	close(done)

	barMu.Lock()
	if realBar != nil {
		_ = realBar.Finish()
	} else if spinner != nil {
		_ = spinner.Finish()
	}
	barMu.Unlock()
	fmt.Println()

	if waitErr == nil {
		return nil, false
	}

	if restoreCompleted.Load() && !errorsReported.Load() {
		logger.Warn("myloader exited with a non-zero status for config %q, but it logged %q — likely a non-fatal MySQL warning incorrectly driving the exit code, the same class of bug as mydumper/mydumper#1300 on the restore side: %v", cfg.Name, myloaderRestoreCompletedMarker, waitErr)
		fmt.Println("Warning: myloader reported a non-zero exit status, but the restore completed successfully (a known myloader quirk — see dbtool.log for details).")
		return nil, false
	}

	return waitErr, readOnlyDetected.Load()
}

// Restarting a partially committed database is safe only when tables are dropped.
func restoreCanRestart(args []string) bool {
	for _, arg := range args {
		if arg == "--overwrite-tables" || arg == "--drop-table" || arg == "--drop-table=DROP" {
			return true
		}
	}
	return false
}
