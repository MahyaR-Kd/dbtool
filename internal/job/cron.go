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
	"dbtool/internal/filelock"
	"dbtool/internal/logger"
	"dbtool/internal/paths"
	"dbtool/internal/s3store"
	"dbtool/internal/settings"
)

// cronField supports lists, ranges, and positive steps with bounded values.
func cronField(field string, val, min, max int) (bool, error) {
	matched := false
	for _, term := range strings.Split(field, ",") {
		parts := strings.Split(term, "/")
		if len(parts) > 2 {
			return false, fmt.Errorf("invalid cron field %q", field)
		}
		step := 1
		if len(parts) == 2 {
			n, err := strconv.Atoi(parts[1])
			if err != nil || n <= 0 {
				return false, fmt.Errorf("invalid cron step %q", term)
			}
			step = n
		}
		lo, hi := min, max
		if parts[0] != "*" {
			bounds := strings.Split(parts[0], "-")
			if len(bounds) > 2 {
				return false, fmt.Errorf("invalid cron range %q", term)
			}
			n, err := strconv.Atoi(bounds[0])
			if err != nil {
				return false, fmt.Errorf("invalid cron value %q", term)
			}
			lo = n
			hi = n
			if len(bounds) == 2 {
				n, err := strconv.Atoi(bounds[1])
				if err != nil {
					return false, fmt.Errorf("invalid cron value %q", term)
				}
				hi = n
			} else if len(parts) == 2 {
				hi = max
			}
		}
		if lo < min || hi > max || lo > hi {
			return false, fmt.Errorf("cron field out of range: %q", term)
		}
		if val >= lo && val <= hi && (val-lo)%step == 0 {
			matched = true
		}
	}
	return matched, nil
}

func ValidateSchedule(schedule string) error {
	fields := strings.Fields(schedule)
	if len(fields) != 5 {
		return fmt.Errorf("schedule must contain five cron fields")
	}
	limits := [][2]int{{0, 59}, {0, 23}, {1, 31}, {1, 12}, {0, 7}}
	for i, field := range fields {
		if _, err := cronField(field, limits[i][0], limits[i][0], limits[i][1]); err != nil {
			return err
		}
	}
	return nil
}

func MatchSchedule(schedule string, t time.Time) bool {
	if ValidateSchedule(schedule) != nil {
		return false
	}
	fields := strings.Fields(schedule)
	minute, _ := cronField(fields[0], t.Minute(), 0, 59)
	hour, _ := cronField(fields[1], t.Hour(), 0, 23)
	day, _ := cronField(fields[2], t.Day(), 1, 31)
	month, _ := cronField(fields[3], int(t.Month()), 1, 12)
	weekday, _ := cronField(fields[4], int(t.Weekday()), 0, 7)
	if t.Weekday() == time.Sunday {
		sunday, _ := cronField(fields[4], 7, 0, 7)
		weekday = weekday || sunday
	}
	// Cron combines restricted day-of-month and day-of-week with OR.
	days := day && weekday
	if !strings.HasPrefix(fields[2], "*") && !strings.HasPrefix(fields[4], "*") {
		days = day || weekday
	}
	return minute && hour && month && days
}

// RunJob executes a single job (dump, restore, or sync) using its stored configs and passwords.
func RunJob(j Config) {
	dir, err := paths.DbtoolDir()
	if err != nil {
		logger.Error("job lock directory: %v", err)
		return
	}
	release, err := filelock.Acquire(dir, "job:"+j.Name, false)
	if err != nil {
		logger.Warn("skipping job %q: %v", j.Name, err)
		return
	}
	defer release()
	if err := ValidateSchedule(j.Schedule); err != nil {
		logger.Error("invalid schedule for job %q: %v", j.Name, err)
		return
	}
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

		if s.S3Enabled() {
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

		if s.S3Enabled() {
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
