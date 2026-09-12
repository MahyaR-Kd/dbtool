package cmd

import (
	"fmt"
	"sort"
	"strings"

	"dbtool/internal/config"
	"dbtool/internal/types"

	"github.com/spf13/cobra"
)

var listCmd = &cobra.Command{
	Use:   "list",
	Short: "List saved database configs",
	Run: func(cmd *cobra.Command, args []string) {
		configs := config.Load()
		if len(configs) == 0 {
			fmt.Println("No configs found")
			return
		}

		for i, c := range configs {
			if i > 0 {
				fmt.Println()
			}
			printConfigEntry(i+1, c)
		}
	},
}

// printConfigEntry renders one config as a short block: a header line, then
// only the detail lines that actually apply to it — a config with nothing
// special set (no SSH, no ignore rules, no saved password, default
// retention) prints as just two lines.
func printConfigEntry(n int, c types.Config) {
	fmt.Printf("%d) %s\n", n, c.Name)

	host := fmt.Sprintf("   %s:%s", c.Host, c.Port)
	if c.SSH {
		host += " (via SSH tunnel)"
	}
	fmt.Println(host)

	var status []string
	if c.RetentionDays > 0 {
		status = append(status, fmt.Sprintf("retention: %d day(s)", c.RetentionDays))
	} else {
		status = append(status, "retention: forever")
	}
	if c.NoLocks {
		status = append(status, "no-locks")
	}
	// c.Password is checked only for presence, never decrypted here — listing
	// configs must never prompt for the master password.
	if c.Password != "" {
		status = append(status, "password saved")
	}
	fmt.Printf("   %s\n", strings.Join(status, " · "))

	if len(c.IgnoredSchemas) > 0 {
		fmt.Printf("   ignoring schema(s): %s\n", strings.Join(c.IgnoredSchemas, ", "))
	}
	if len(c.IgnoredTables) > 0 {
		schemas := make([]string, 0, len(c.IgnoredTables))
		for schema := range c.IgnoredTables {
			schemas = append(schemas, schema)
		}
		sort.Strings(schemas)
		parts := make([]string, 0, len(schemas))
		for _, schema := range schemas {
			parts = append(parts, fmt.Sprintf("%s: %s", schema, strings.Join(c.IgnoredTables[schema], ", ")))
		}
		fmt.Printf("   ignoring table(s) — %s\n", strings.Join(parts, "; "))
	}
}

func init() {
	configCmd.AddCommand(listCmd)
}
