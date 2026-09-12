package job

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"dbtool/internal/config"
	"dbtool/internal/db"
	"dbtool/internal/logger"
	"dbtool/internal/s3store"
	"dbtool/internal/settings"
)

// matchField returns true when the cron field matches val.
// Supports: *, */n, a-b, a,b,c, and exact numbers.
func matchField(field string, val int) bool {
	if field == "*" {
		return true
	}

	if strings.HasPrefix(field, "*/") {
		n, err := strconv.Atoi(field[2:])
		if err != nil {
			return false
		}
		return val%n == 0
	}

	if strings.Contains(field, "-") {
		parts := strings.SplitN(field, "-", 2)
		a, e1 := strconv.Atoi(parts[0])
		b, e2 := strconv.Atoi(parts[1])
		if e1 != nil || e2 != nil {
			return false
		}
		return val >= a && val <= b
	}

	if strings.Contains(field, ",") {
		for _, p := range strings.Split(field, ",") {
			n, err := strconv.Atoi(strings.TrimSpace(p))
			if err != nil {
				continue
			}
			if n == val {
				return true
			}
		}
		return false
	}

	n, err := strconv.Atoi(field)
	if err != nil {
		return false
	}
	return n == val
}

// MatchSchedule returns true when the 5-field cron expression matches t.
// Fields: minute hour day-of-month month day-of-week
func MatchSchedule(schedule string, t time.Time) bool {
	fields := strings.Fields(schedule)
	if len(fields) != 5 {
		return false
	}

	return matchField(fields[0], t.Minute()) &&
		matchField(fields[1], t.Hour()) &&
		matchField(fields[2], t.Day()) &&
		matchField(fields[3], int(t.Month())) &&
		matchField(fields[4], int(t.Weekday()))
}

// RunJob executes a single job (dump, restore, or sync) using its stored configs and passwords.
func RunJob(j Config) {
	fmt.Printf("[schedule] running job %q (%s)\n", j.Name, j.Type)
	logger.Info("[schedule] running job %q (type=%s)", j.Name, j.Type)

	s := settings.Load()

	switch j.Type {
	case "dump":
		srcCfg := config.SelectByName(j.SrcConfigName)
		db.RunDump(srcCfg, j.SrcPassword)

	case "restore":
		dstCfg := config.SelectByName(j.DstConfigName)
		var dir string
		var tempDir string // non-empty when we downloaded from S3 and must clean up

		if s.StorageType == settings.StorageS3 {
			// Find the latest dump for this config in S3 and download it locally.
			dumpName, err := s3store.FindLatestDump(s.S3, j.SrcConfigName)
			if dumpName == "" || err != nil {
				if err != nil {
					logger.Warn("[schedule] job %q: S3 lookup error: %v", j.Name, err)
					fmt.Printf("[schedule] job %q: S3 lookup error: %v\n", j.Name, err)
				} else {
					logger.Warn("[schedule] job %q: no S3 dump found for prefix %q", j.Name, j.SrcConfigName)
					fmt.Printf("[schedule] job %q: no S3 dump found for prefix %q\n", j.Name, j.SrcConfigName)
				}
				return
			}
			tmp, err := os.MkdirTemp("", "dbtool-sched-restore-*")
			if err != nil {
				logger.Warn("[schedule] job %q: cannot create temp dir: %v", j.Name, err)
				fmt.Printf("[schedule] job %q: cannot create temp dir: %v\n", j.Name, err)
				return
			}
			localDir, err := s3store.DownloadDump(s.S3, dumpName, tmp)
			if err != nil {
				logger.Warn("[schedule] job %q: S3 download error: %v", j.Name, err)
				fmt.Printf("[schedule] job %q: S3 download error: %v\n", j.Name, err)
				_ = os.RemoveAll(tmp)
				return
			}
			dir = localDir
			tempDir = tmp
		} else {
			dir = db.FindLatestDumpDir(s.WorkDir, j.SrcConfigName)
			if dir == "" {
				logger.Warn("[schedule] job %q: no dump directory found for prefix %q", j.Name, j.SrcConfigName)
				fmt.Printf("[schedule] job %q: no dump directory found for prefix %q\n", j.Name, j.SrcConfigName)
				return
			}
		}

		logger.Info("[schedule] job %q: restoring from %q", j.Name, dir)
		fmt.Printf("[schedule] job %q: restoring from %q\n", j.Name, dir)
		db.RunRestore(dstCfg, j.DstPassword, dir, j.OverwriteTables)

		if tempDir != "" {
			logger.Debug("[schedule] cleaning up S3 temp restore dir: %s", tempDir)
			if err := os.RemoveAll(tempDir); err != nil {
				logger.Error("[schedule] failed to remove temp dir %s: %v", tempDir, err)
				fmt.Printf("[schedule] warning: failed to remove temp dir %s — please clean it up manually.\n", tempDir)
			}
		}

	case "sync":
		srcCfg := config.SelectByName(j.SrcConfigName)
		dstCfg := config.SelectByName(j.DstConfigName)

		if s.StorageType == settings.StorageS3 {
			// Dump uploads to S3 and removes the local copy; download fresh for restore.
			db.RunDump(srcCfg, j.SrcPassword)

			dumpName, err := s3store.FindLatestDump(s.S3, j.SrcConfigName)
			if dumpName == "" || err != nil {
				if err != nil {
					logger.Warn("[schedule] job %q: S3 lookup after dump error: %v", j.Name, err)
					fmt.Printf("[schedule] job %q: S3 lookup after dump error: %v\n", j.Name, err)
				} else {
					logger.Warn("[schedule] job %q: no S3 dump found after dump for prefix %q", j.Name, j.SrcConfigName)
					fmt.Printf("[schedule] job %q: no S3 dump found after dump for prefix %q\n", j.Name, j.SrcConfigName)
				}
				return
			}
			tmp, err := os.MkdirTemp("", "dbtool-sched-sync-*")
			if err != nil {
				logger.Warn("[schedule] job %q: cannot create temp dir: %v", j.Name, err)
				fmt.Printf("[schedule] job %q: cannot create temp dir: %v\n", j.Name, err)
				return
			}
			localDir, err := s3store.DownloadDump(s.S3, dumpName, tmp)
			if err != nil {
				logger.Warn("[schedule] job %q: S3 download error: %v", j.Name, err)
				fmt.Printf("[schedule] job %q: S3 download error: %v\n", j.Name, err)
				_ = os.RemoveAll(tmp)
				return
			}
			logger.Info("[schedule] job %q: restoring %q into destination", j.Name, localDir)
			fmt.Printf("[schedule] job %q: restoring %q into destination\n", j.Name, localDir)
			db.RunRestore(dstCfg, j.DstPassword, localDir, j.OverwriteTables)
			logger.Debug("[schedule] cleaning up S3 temp sync dir: %s", tmp)
			if err := os.RemoveAll(tmp); err != nil {
				logger.Error("[schedule] failed to remove temp dir %s: %v", tmp, err)
				fmt.Printf("[schedule] warning: failed to remove temp dir %s — please clean it up manually.\n", tmp)
			}
		} else {
			outDir := db.RunDump(srcCfg, j.SrcPassword)
			logger.Info("[schedule] job %q: restoring %q into destination", j.Name, outDir)
			fmt.Printf("[schedule] job %q: restoring %q into destination\n", j.Name, outDir)
			db.RunRestore(dstCfg, j.DstPassword, outDir, j.OverwriteTables)
		}

	default:
		logger.Warn("[schedule] unknown job type %q for job %q", j.Type, j.Name)
		fmt.Printf("[schedule] unknown job type %q for job %q\n", j.Type, j.Name)
	}

	logger.Info("[schedule] job %q (%s) finished", j.Name, j.Type)
}

// running tracks which job names are currently executing to avoid overlapping runs.
var (
	runningMu sync.Mutex
	running   = map[string]bool{}
)

// RunScheduler starts an infinite loop that checks every minute whether any
// saved jobs are due and runs them in separate goroutines.
func RunScheduler() {
	jobs := Load()

	if len(jobs) == 0 {
		fmt.Println("No jobs configured. Use 'dbtool job' to add one.")
		return
	}

	fmt.Printf("Scheduler started with %d job(s). Press Ctrl+C to stop.\n", len(jobs))

	// sleep until the next full minute boundary
	now := time.Now()
	time.Sleep(time.Until(now.Truncate(time.Minute).Add(time.Minute)))

	for {
		t := time.Now().Truncate(time.Minute)

		// reload jobs each tick so changes take effect without restart
		jobs = Load()

		for _, j := range jobs {
			if MatchSchedule(j.Schedule, t) {
				j := j // capture loop variable

				runningMu.Lock()
				alreadyRunning := running[j.Name]
				if !alreadyRunning {
					running[j.Name] = true
				}
				runningMu.Unlock()

				if alreadyRunning {
					logger.Warn("[schedule] job %q still running at %s, skipping", j.Name, t.Format("15:04"))
					fmt.Printf("[schedule] job %q still running, skipping\n", j.Name)
					continue
				}

				go func() {
					defer func() {
						runningMu.Lock()
						delete(running, j.Name)
						runningMu.Unlock()
					}()
					RunJob(j)
				}()
			}
		}

		// sleep until the next minute
		time.Sleep(time.Until(t.Add(time.Minute)))
	}
}
