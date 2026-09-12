package cmd

import "github.com/spf13/cobra"

var settingCmd = &cobra.Command{
	Use:   "setting",
	Short: "Manage dbtool settings (storage, working directory, proxy)",
}

func init() {
	if installedInPath {
		rootCmd.AddCommand(settingCmd)
	}
}
