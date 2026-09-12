package cmd

import (
	"dbtool/internal/config"
	"dbtool/internal/db"
	"dbtool/internal/logger"
	"dbtool/internal/types"
	"github.com/spf13/cobra"
)

var password string
var name string

var dumpCmd = &cobra.Command{
	Use:   "dump",
	Short: "Dump database",
	Run: func(cmd *cobra.Command, args []string) {
		var cfg types.Config

		if name != "" {
			logger.Debug("dump: using named config %q", name)
			cfg = config.SelectByName(name)
		} else {
			cfg = config.SelectInteractive()
		}

		pass := config.AskPassword(password, "DBTOOL_DB_PASS", cfg)
		logger.Info("dump command invoked for config %q", cfg.Name)
		_ = db.RunDump(cfg, pass)
	},
}

func init() {
	dumpCmd.Flags().StringVar(&password, "pass", "", "Database password")
	dumpCmd.Flags().StringVar(&name, "name", "", "Config name (non-interactive)")
	if installedInPath {
		rootCmd.AddCommand(dumpCmd)
	}
}
