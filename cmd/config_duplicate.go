package cmd

import (
	"fmt"

	"github.com/MahyaR-Kd/dbtool/internal/config"
	"github.com/MahyaR-Kd/dbtool/internal/interactivelist"
	"github.com/MahyaR-Kd/dbtool/internal/types"
	"github.com/MahyaR-Kd/dbtool/internal/ui"

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
			ui.Empty("No configs yet", "Create one with "+ui.Code("dbtool config add"))
			return
		}
		options := make([]string, len(configs))
		for i, c := range configs {
			options[i] = configOption(c)
		}
		idx, err := interactivelist.SelectOne("Choose a config to duplicate", options)
		if err != nil {
			ui.Error("Invalid selection")
			return
		}
		copy := cloneConfig(configs[idx])
		copy.Name = availableCopyName(copy.Name, configs)
		copy = config.EditInteractive(copy)
		if copy.Name == "" {
			ui.Error("Config name cannot be empty.")
			return
		}
		for _, existing := range configs {
			if existing.Name == copy.Name {
				ui.Error("A config named %q already exists.", copy.Name)
				return
			}
		}
		if !confirmSaveConfig(copy) {
			return
		}
		if err := config.Save(copy); err != nil {
			ui.Error("Failed to save config: %v", err)
			return
		}
		ui.Success("Duplicated %s as %s", ui.Bold(configs[idx].Name), ui.Bold(copy.Name))
		ui.Hint("Back it up now with %s", ui.Code("dbtool dump --name "+shellQuote(copy.Name)))
	},
}

func init() { configCmd.AddCommand(configDuplicateCmd) }
