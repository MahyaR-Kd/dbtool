package cmd

import (
	"github.com/spf13/cobra"
)

var jobCmd = &cobra.Command{
	Use:   "job",
	Short: "Manage scheduled dump/restore jobs",
}

func init() {
	if installedInPath {
		rootCmd.AddCommand(jobCmd)
	}
}
