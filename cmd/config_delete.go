package cmd

import (
	"fmt"

	"dbtool/internal/config"
	"dbtool/internal/interactivelist"

	"github.com/spf13/cobra"
)

var configDeleteName string

var deleteCmd = &cobra.Command{
	Use:   "delete",
	Short: "Delete a config",
	Run: func(cmd *cobra.Command, args []string) {
		configs := config.Load()

		if len(configs) == 0 {
			fmt.Println("No configs found")
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
				fmt.Println("Config not found:", configDeleteName)
				return
			}
			configs = append(configs[:found], configs[found+1:]...)
			config.Overwrite(configs)
			fmt.Println("Deleted successfully")
			return
		}

		options := make([]string, len(configs))
		for i, c := range configs {
			options[i] = fmt.Sprintf("%s (%s:%s)", c.Name, c.Host, c.Port)
		}

		idx, err := interactivelist.SelectOne("Select config to delete", options)
		if err != nil {
			fmt.Println("Invalid selection")
			return
		}

		configs = append(configs[:idx], configs[idx+1:]...)
		config.Overwrite(configs)

		fmt.Println("Deleted successfully")
	},
}

func init() {
	deleteCmd.Flags().StringVar(&configDeleteName, "name", "", "Config name to delete (non-interactive)")
	configCmd.AddCommand(deleteCmd)
}
