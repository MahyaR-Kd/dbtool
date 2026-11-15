package cmd

import (
	"github.com/MahyaR-Kd/dbtool/internal/config"
	"github.com/MahyaR-Kd/dbtool/internal/ui"

	"github.com/spf13/cobra"
)

var listCmd = &cobra.Command{
	Use:   "list",
	Short: "List saved database configs",
	Run: func(cmd *cobra.Command, args []string) {
		configs := config.Load()
		if len(configs) == 0 {
			ui.Empty("No configs yet", "Create one with "+ui.Code("dbtool config add"))
			return
		}

		ui.PrintTable(configHeaders, configRows(configs))

		// The FLAGS column only counts ignore rules; spell them out below
		// the table so nothing is lost compared to a per-config listing.
		printedDetail := false
		for _, c := range configs {
			detail := ignoreDetail(c)
			if detail == "" {
				continue
			}
			if !printedDetail {
				ui.Blank()
				printedDetail = true
			}
			ui.Detail("%s %s", c.Name, ui.Muted("ignores "+detail))
		}

		ui.Blank()
		ui.Hint("%s · manage them with %s", ui.Plural(len(configs), "config", "configs"),
			ui.Code("dbtool config add | edit | delete"))
	},
}

func init() {
	configCmd.AddCommand(listCmd)
}
