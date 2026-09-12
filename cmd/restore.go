package cmd

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"dbtool/internal/config"
	"dbtool/internal/db"
	"dbtool/internal/deps"
	"dbtool/internal/interactivelist"
	"dbtool/internal/logger"
	"dbtool/internal/s3store"
	"dbtool/internal/settings"
	"dbtool/internal/types"
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
			if s.StorageType == settings.StorageS3 {
				// S3 mode: list available dumps from S3 and let the user pick one.
				selectedDir, err := selectDumpFromS3(s)
				if err != nil {
					fmt.Println("Error:", err)
					os.Exit(1)
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
		if s.StorageType == settings.StorageS3 && dir == "" {
			logger.Debug("cleaning up S3 temp restore dir: %s", restoreDir)
			if err := os.RemoveAll(restoreDir); err != nil {
				logger.Error("failed to remove temp restore dir %s: %v", restoreDir, err)
				fmt.Printf("Warning: failed to remove temp restore directory %s — please clean it up manually.\n", restoreDir)
			}
		}
	},
}

// selectDumpFromS3 lists dumps stored in S3, lets the user pick one, downloads
// it to a temp directory, and returns the local path.
func selectDumpFromS3(s settings.Settings) (string, error) {
	fmt.Println("Fetching available dumps from S3…")
	dumps, err := s3store.ListDumps(s.S3)
	if err != nil {
		return "", fmt.Errorf("list S3 dumps: %w", err)
	}
	if len(dumps) == 0 {
		return "", fmt.Errorf("no dumps found in S3 bucket %q (prefix %q)", s.S3.Bucket, s.S3.Prefix)
	}

	// Sort descending so the newest dump appears first.
	sort.Sort(sort.Reverse(sort.StringSlice(dumps)))

	idx, err := interactivelist.SelectOne("Select dump (newest first)", dumps)
	if err != nil {
		return "", fmt.Errorf("invalid selection: %w", err)
	}

	chosen := dumps[idx]
	tmpDir, err := os.MkdirTemp("", restoreTempDirPrefix)
	if err != nil {
		return "", fmt.Errorf("create temp dir: %w", err)
	}

	fmt.Printf("Downloading %q from S3...\n", chosen)
	logger.Info("downloading S3 dump %q to %s", chosen, tmpDir)
	localDir, err := s3store.DownloadDump(s.S3, chosen, tmpDir)
	if err != nil {
		if rmErr := os.RemoveAll(tmpDir); rmErr != nil {
			logger.Error("failed to remove temp dir %s after download error: %v", tmpDir, rmErr)
			fmt.Printf("Warning: failed to remove temp directory %s -- please clean it up manually.\n", tmpDir)
		}
		return "", fmt.Errorf("download from S3: %w", err)
	}
	fmt.Printf("Downloaded to: %s\n", localDir)
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
		fmt.Println("No configs found. Add a config first.")
		os.Exit(1)
	}

	configOptions := make([]string, len(configs))
	for i, c := range configs {
		configOptions[i] = fmt.Sprintf("%s (%s:%s)", c.Name, c.Host, c.Port)
	}

	cfgIdx, err := interactivelist.SelectOne("Select source config to browse dumps from", configOptions)
	if err != nil {
		fmt.Println("Invalid selection")
		os.Exit(1)
	}
	sourceCfgName := configs[cfgIdx].Name

	// Step 2: list dumps for the chosen source config.
	entries, err := os.ReadDir(s.WorkDir)
	if err != nil {
		fmt.Printf("Cannot read work directory %q: %v\n", s.WorkDir, err)
		os.Exit(1)
	}

	prefix := sourceCfgName + "_"
	var dumps []string
	for _, e := range entries {
		if e.IsDir() && strings.HasPrefix(e.Name(), prefix) {
			dumps = append(dumps, e.Name())
		}
	}

	if len(dumps) == 0 {
		fmt.Printf("No dumps found for config %q in %s\n", sourceCfgName, s.WorkDir)
		os.Exit(1)
	}

	// Sort descending so the newest dump appears first.
	sort.Sort(sort.Reverse(sort.StringSlice(dumps)))

	idx, err := interactivelist.SelectOne(fmt.Sprintf("Select dump for %q (newest first)", sourceCfgName), dumps)
	if err != nil {
		fmt.Println("Invalid selection")
		os.Exit(1)
	}

	return filepath.Join(s.WorkDir, dumps[idx])
}

// askOverwriteTables asks the user whether to pass --overwrite-tables to myloader.
func askOverwriteTables() bool {
	reader := bufio.NewReader(os.Stdin)
	fmt.Print("Overwrite existing tables? (y/N): ")
	input, _ := reader.ReadString('\n')
	input = strings.TrimSpace(strings.ToLower(input))
	return input == "y" || input == "yes"
}

func init() {
	restoreCmd.Flags().StringVar(&password, "pass", "", "Database password")
	restoreCmd.Flags().StringVar(&restoreConfigName, "name", "", "Config name (skip interactive selection)")
	restoreCmd.Flags().StringVar(&dir, "dir", "", "Dump directory (skip interactive selection; for S3 mode, omit to select from S3)")
	if installedInPath {
		rootCmd.AddCommand(restoreCmd)
	}
}
