package cmd

import (
	"fmt"

	"dbtool/internal/config"
	"dbtool/internal/interactivelist"
	"dbtool/internal/types"
	"github.com/spf13/cobra"
)

func cloneConfig(source types.Config) types.Config {
	copy := source
	copy.IgnoredSchemas = append([]string(nil), source.IgnoredSchemas...)
	if source.IgnoredTables != nil {
		copy.IgnoredTables = make(map[string][]string, len(source.IgnoredTables))
		for schema, tables := range source.IgnoredTables {
			copy.IgnoredTables[schema] = append([]string(nil), tables...)
		}
	}
	return copy
}

func availableCopyName(source string, configs []types.Config) string {
	used := make(map[string]bool, len(configs))
	for _, c := range configs {
		used[c.Name] = true
	}
	base := source + "-copy"
	if !used[base] {
		return base
	}
	for n := 2; ; n++ {
		candidate := fmt.Sprintf("%s-%d", base, n)
		if !used[candidate] {
			return candidate
		}
	}
}

var configDuplicateCmd = &cobra.Command{
	Use: "duplicate", Short: "Copy a config, edit its fields, and save it as a new config",
	Run: func(cmd *cobra.Command, args []string) {
		configs := config.Load()
		if len(configs) == 0 {
			fmt.Println("No configs found")
			return
		}
		options := make([]string, len(configs))
		for i, c := range configs {
			options[i] = fmt.Sprintf("%s (%s:%s)", c.Name, c.Host, c.Port)
		}
		idx, err := interactivelist.SelectOne("Select config to duplicate", options)
		if err != nil {
			fmt.Println("Invalid selection")
			return
		}
		copy := cloneConfig(configs[idx])
		copy.Name = availableCopyName(copy.Name, configs)
		copy = config.EditInteractive(copy)
		if copy.Name == "" {
			fmt.Println("Config name cannot be empty.")
			return
		}
		for _, existing := range configs {
			if existing.Name == copy.Name {
				fmt.Printf("A config named %q already exists.\n", copy.Name)
				return
			}
		}
		if err := config.Save(copy); err != nil {
			fmt.Println("Failed to save config:", err)
			return
		}
		fmt.Printf("Duplicated as %q.\n", copy.Name)
	},
}

func init() { configCmd.AddCommand(configDuplicateCmd) }
