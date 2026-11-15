package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/MahyaR-Kd/dbtool/internal/config"
	"github.com/MahyaR-Kd/dbtool/internal/db"
	"github.com/MahyaR-Kd/dbtool/internal/deps"
	"github.com/MahyaR-Kd/dbtool/internal/interactivelist"
	"github.com/MahyaR-Kd/dbtool/internal/logger"
	"github.com/MahyaR-Kd/dbtool/internal/s3store"
	"github.com/MahyaR-Kd/dbtool/internal/settings"
	"github.com/MahyaR-Kd/dbtool/internal/types"
	"github.com/MahyaR-Kd/dbtool/internal/ui"

	"github.com/spf13/cobra"
)

var dir string
var restoreConfigName string

var restoreCmd = &cobra.Command{
	Use:   "restore",
	Short: "Restore database",
	Run: func(cmd *cobra.Command, args []string) {
		deps.Require("myloader", "mysql")

		var cfg types.Config
		if restoreConfigName != "" {
			cfg = config.SelectByName(restoreConfigName)
		} else {
			cfg = config.Select()
		}
		pass := config.AskPassword(password, "DBTOOL_DB_PASS", cfg)

		s := settings.Load()

		restoreDir := dir
		if restoreDir == "" {
			if s.S3Enabled() {
				// S3 mode: list available dumps from S3 and let the user pick one.
				selectedDir, err := selectDumpFromS3(s)
				if err != nil {
					ui.Fatal("%v", err)
				}
				restoreDir = selectedDir
			} else {
				// Local mode: list dumps in the work directory.
				restoreDir = selectDumpDir()
			}
		}

		overwriteTables := askOverwriteTables()

		logger.Info("restore command invoked for config %q (dir=%s overwrite-tables=%v)", cfg.Name, restoreDir, overwriteTables)
		db.RunRestore(cfg, pass, restoreDir, overwriteTables)

		// If we downloaded from S3 to a temp dir, clean it up.
		if s.S3Enabled() && dir == "" {
			logger.Debug("cleaning up S3 temp restore dir: %s", restoreDir)
			if err := os.RemoveAll(restoreDir); err != nil {
				logger.Error("failed to remove temp restore dir %s: %v", restoreDir, err)
				ui.Warn("Failed to remove temp restore directory %s — please clean it up manually.", restoreDir)
			}
		}
	},
}

// selectDumpFromS3 lists dumps stored in S3, lets the user pick one, downloads
// it to a temp directory, and returns the local path.
func selectDumpFromS3(s settings.Settings) (string, error) {
	task := ui.StartTask("Fetching available dumps from S3")
	dumps, err := s3store.ListDumps(s.S3)
	if err != nil {
		task.Fail(nil) // the caller reports err
	} else {
		task.Done(ui.Plural(len(dumps), "dump", "dumps"))
	}
	if err != nil {
		return "", fmt.Errorf("list S3 dumps: %w", err)
	}
	if len(dumps) == 0 {
		return "", fmt.Errorf("no dumps found in S3 bucket %q (prefix %q)", s.S3.Bucket, s.S3.Prefix)
	}

	// Sort descending so the newest dump appears first.
	sort.Sort(sort.Reverse(sort.StringSlice(dumps)))

	idx, err := interactivelist.SelectOne("Choose a dump (newest first)", dumps)
	if err != nil {
		return "", fmt.Errorf("invalid selection: %w", err)
	}

	chosen := dumps[idx]
	tmpDir, err := os.MkdirTemp("", restoreTempDirPrefix)
	if err != nil {
		return "", fmt.Errorf("create temp dir: %w", err)
	}

	task = ui.StartTask("Downloading %s from S3", ui.Bold(chosen))
	logger.Info("downloading S3 dump %q to %s", chosen, tmpDir)
	localDir, err := s3store.DownloadDump(s.S3, chosen, tmpDir)
	if err != nil {
		task.Fail(nil) // the caller reports err
		if rmErr := os.RemoveAll(tmpDir); rmErr != nil {
			logger.Error("failed to remove temp dir %s after download error: %v", tmpDir, rmErr)
			ui.Warn("Failed to remove temp directory %s — please clean it up manually.", tmpDir)
		}
		return "", fmt.Errorf("download from S3: %w", err)
	}
	task.Done(localDir)
	return localDir, nil
}

// selectDumpDir first asks the user to choose a source config (whose dumps to
// browse), then lists that config's dump directories inside the configured work
// directory sorted newest-first, and prompts the user to pick one.
func selectDumpDir() string {
	s := settings.Load()

	// Step 1: pick a source config to browse dumps from.
	configs := config.Load()
	if len(configs) == 0 {
		ui.Error("No configs yet — add a config first.")
		ui.Hint("Create one with %s", ui.Code("dbtool config add"))
		os.Exit(1)
	}

	configOptions := make([]string, len(configs))
	for i, c := range configs {
		configOptions[i] = configOption(c)
	}

	cfgIdx, err := interactivelist.SelectOne("Choose a source config to browse dumps from", configOptions)
	if err != nil {
		ui.Fatal("Invalid selection")
	}
	sourceCfgName := configs[cfgIdx].Name

	// Step 2: list dumps for the chosen source config.
	entries, err := os.ReadDir(s.WorkDir)
	if err != nil {
		ui.Fatal("Cannot read work directory %q: %v", s.WorkDir, err)
	}

	prefix := sourceCfgName + "_"
	var dumps []string
	for _, e := range entries {
		if e.IsDir() && strings.HasPrefix(e.Name(), prefix) {
			dumps = append(dumps, e.Name())
		}
	}

	if len(dumps) == 0 {
		ui.Error("No dumps found for config %q in %s", sourceCfgName, s.WorkDir)
		ui.Hint("Create one with %s", ui.Code("dbtool dump --name "+shellQuote(sourceCfgName)))
		os.Exit(1)
	}

	// Sort descending so the newest dump appears first.
	sort.Sort(sort.Reverse(sort.StringSlice(dumps)))

	// Labels carry the dump's size and age; the index still maps back into
	// dumps, so the returned path is unchanged.
	labels := make([]string, len(dumps))
	now := time.Now()
	for i, d := range dumps {
		labels[i] = dumpOptionLabel(filepath.Join(s.WorkDir, d), d, now)
	}

	idx, err := interactivelist.SelectOne(fmt.Sprintf("Choose a dump of %q (newest first)", sourceCfgName), labels)
	if err != nil {
		ui.Fatal("Invalid selection")
	}

	return filepath.Join(s.WorkDir, dumps[idx])
}

// dumpOptionLabel renders a dump directory for the picker:
// "name  12.3 MB · 3h ago" (size and age dimmed, omitted if unreadable).
func dumpOptionLabel(path, name string, now time.Time) string {
	var meta []string
	if size := ui.DirSize(path); size > 0 {
		meta = append(meta, ui.Bytes(size))
	}
	if info, err := os.Stat(path); err == nil {
		meta = append(meta, ago(now.Sub(info.ModTime())))
	}
	if len(meta) == 0 {
		return name
	}
	return name + "  " + ui.Muted(strings.Join(meta, " · "))
}

// ago renders an elapsed duration compactly: "just now", "5m ago",
// "3h ago", "2d ago".
func ago(d time.Duration) string {
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	default:
		return fmt.Sprintf("%dd ago", int(d.Hours()/24))
	}
}

// askOverwriteTables asks the user whether to pass --overwrite-tables to myloader.
func askOverwriteTables() bool {
	return interactivelist.Confirm("Overwrite existing tables?", false)
}

func init() {
	restoreCmd.Flags().StringVar(&password, "pass", "", "Database password")
	restoreCmd.Flags().StringVar(&restoreConfigName, "name", "", "Config name (skip interactive selection)")
	restoreCmd.Flags().StringVar(&dir, "dir", "", "Dump directory (skip interactive selection; for S3 mode, omit to select from S3)")
	if installedInPath {
		rootCmd.AddCommand(restoreCmd)
	}
}
