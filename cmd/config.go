package cmd

import (
	"github.com/spf13/cobra"
)

var configCmd = &cobra.Command{
	Use:   "config",
	Short: "Manage database connection configs",
}

func init() {
	if installedInPath {
		rootCmd.AddCommand(configCmd)
	}
}
