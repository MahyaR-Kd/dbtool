package db

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	"dbtool/internal/connection"
	"dbtool/internal/logger"
	"dbtool/internal/progress"
	"dbtool/internal/settings"
	"dbtool/internal/types"

	pb "github.com/schollz/progressbar/v3"
)

// countRestoredTables returns the number of table-schema files found in dir,
// which equals the total number of tables in the dump directory.
func countRestoredTables(dir string) int {
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

func RunRestore(cfg types.Config, pass, dir string, overwriteTables bool) {

	logger.Info("starting restore into config %q (dir=%s overwrite-tables=%v)", cfg.Name, dir, overwriteTables)

	s := settings.Load()

	cfg, cleanup := connection.ApplyTunnels(cfg, s)
	defer cleanup()

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

	args := []string{"-h", cfg.Host, "-P", cfg.Port, "-u", cfg.User}
	if pass != "" {
		args = append(args, "-p", pass)
	}

	args = append(args, "-d", dir, "-v", "3")

	if overwriteTables {
		overwriteArgs := overwriteTablesArgs(myloaderVersion)
		args = append(args, overwriteArgs...)
		logger.Debug("myloader: overwrite-tables requested -> %s", strings.Join(overwriteArgs, " "))
	}

	cmd := exec.Command(myloaderPath, args...)

	stderr, _ := cmd.StderrPipe()
	stdout, _ := cmd.StdoutPipe()

	if err := cmd.Start(); err != nil {
		logger.Error("restore failed to start for config %q: %v", cfg.Name, err)
		fmt.Println("Failed:", err)
		os.Exit(1)
	}

	// ---------------- progress tracking ----------------
	// Count table files in the dump directory so we can show a real bar.
	totalTables := countRestoredTables(dir)

	var (
		barMu   sync.Mutex
		realBar *pb.ProgressBar
		spinner *pb.ProgressBar
	)

	done := make(chan struct{})

	if totalTables > 0 {
		realBar = progress.New(totalTables, "Restoring…")
	} else {
		spinner = progress.SpinnerBar("Restoring…")
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

	go func() {
		scanner := bufio.NewScanner(stdout)
		for scanner.Scan() {
			logger.Debug("[myloader stdout] %s", scanner.Text())
		}
	}()

	// Goroutine: parse myloader verbose (-v 3) stderr lines.  Each "Thread N
	// restoring …" line corresponds to one table chunk being loaded.  We count
	// them to drive the real bar; the legacy [X/Y] handler is kept as fallback.
	go func() {
		var stderrCount int
		scanner := bufio.NewScanner(stderr)
		for scanner.Scan() {
			line := scanner.Text()
			logger.Debug("[myloader stderr] %s", line)

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
				realBar = progress.New(tot, "Restoring…")
			}
			if realBar != nil {
				_ = realBar.Set(cur)
			}
			barMu.Unlock()
		}
	}()

	if err := cmd.Wait(); err != nil {
		close(done)
		logger.Error("restore failed for config %q: %v", cfg.Name, err)
		fmt.Println("Command failed:", err)
		os.Exit(1)
	}

	close(done)

	barMu.Lock()
	if realBar != nil {
		_ = realBar.Finish()
	} else if spinner != nil {
		_ = spinner.Finish()
	}
	barMu.Unlock()
	fmt.Println()

	logger.Info("restore completed for config %q", cfg.Name)
}
