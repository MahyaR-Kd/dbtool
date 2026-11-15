package cmd

import (
	"github.com/MahyaR-Kd/dbtool/internal/config"
	"github.com/MahyaR-Kd/dbtool/internal/interactivelist"
	"github.com/MahyaR-Kd/dbtool/internal/ui"

	"github.com/spf13/cobra"
)

var configDeleteName string

var deleteCmd = &cobra.Command{
	Use:   "delete",
	Short: "Delete a config",
	Run: func(cmd *cobra.Command, args []string) {
		configs := config.Load()

		if len(configs) == 0 {
			ui.Empty("No configs yet", "Create one with "+ui.Code("dbtool config add"))
			return
		}

		if configDeleteName != "" {
			found := -1
			for i := range configs {
				if configs[i].Name == configDeleteName {
					found = i
					break
				}
			}
			if found == -1 {
				ui.Error("Config not found: %s", configDeleteName)
				return
			}
			configs = append(configs[:found], configs[found+1:]...)
			if err := config.Overwrite(configs); err != nil {
				ui.Error("Failed to save configs: %v", err)
				return
			}
			ui.Success("Config %s deleted", ui.Bold(configDeleteName))
			return
		}

		options := make([]string, len(configs))
		for i, c := range configs {
			options[i] = configOption(c)
		}

		idx, err := interactivelist.SelectOne("Choose a config to delete", options)
		if err != nil {
			ui.Error("Invalid selection")
			return
		}

		deleted := configs[idx].Name
		configs = append(configs[:idx], configs[idx+1:]...)
		if err := config.Overwrite(configs); err != nil {
			ui.Error("Failed to save configs: %v", err)
			return
		}

		ui.Success("Config %s deleted", ui.Bold(deleted))
	},
}

func init() {
	deleteCmd.Flags().StringVar(&configDeleteName, "name", "", "Config name to delete (non-interactive)")
	configCmd.AddCommand(deleteCmd)
}
