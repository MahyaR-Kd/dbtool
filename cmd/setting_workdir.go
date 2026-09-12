package cmd

import (
	"fmt"
	"os"
	"path/filepath"

	"dbtool/internal/logger"
	"dbtool/internal/settings"
	"github.com/spf13/cobra"
)

var workdirCmd = &cobra.Command{
	Use:   "workdir",
	Short: "Manage the working directory used for dump output",
}

var workdirGetCmd = &cobra.Command{
	Use:   "get",
	Short: "Print the current working directory for dump output",
	Run: func(cmd *cobra.Command, args []string) {
		s := settings.Load()
		fmt.Println(s.WorkDir)
	},
}

var useDefault bool

var workdirSetCmd = &cobra.Command{
	Use:   "set [path]",
	Short: "Set the working directory for dump output",
	Long: `Set the directory where dump output will be stored.

Examples:
  dbtool workdir set /data/backups
  dbtool workdir set --default   (uses ~/dbtool-backups)`,
	RunE: func(cmd *cobra.Command, args []string) error {
		s := settings.Load()

		if useDefault {
			home, err := os.UserHomeDir()
			if err != nil {
				return fmt.Errorf("cannot determine home directory: %w", err)
			}
			s.WorkDir = filepath.Join(home, "dbtool-backups")
		} else if len(args) == 1 {
			abs, err := filepath.Abs(args[0])
			if err != nil {
				return fmt.Errorf("invalid path: %w", err)
			}
			s.WorkDir = abs
		} else {
			return fmt.Errorf("provide a path argument or use --default")
		}

		if err := settings.Save(s); err != nil {
			logger.Error("failed to save workdir setting: %v", err)
			return fmt.Errorf("failed to save settings: %w", err)
		}

		logger.Info("work directory set to: %s", s.WorkDir)
		fmt.Println("Work directory set to:", s.WorkDir)
		return nil
	},
}

func init() {
	workdirSetCmd.Flags().BoolVar(&useDefault, "default", false, "Reset to the default work directory (~/dbtool-backups)")
	workdirCmd.AddCommand(workdirGetCmd)
	workdirCmd.AddCommand(workdirSetCmd)
	if installedInPath {
		settingCmd.AddCommand(workdirCmd)
	}
}
