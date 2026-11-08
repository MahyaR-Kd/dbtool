package cmd

import (
	"time"

	"dbtool/internal/job"
	"dbtool/internal/ui"

	"github.com/spf13/cobra"
)

var jobListCmd = &cobra.Command{
	Use:   "list",
	Short: "List saved scheduled jobs",
	Run: func(cmd *cobra.Command, args []string) {
		jobs := job.Load()
		if len(jobs) == 0 {
			ui.Empty("No jobs yet", "Create one with "+ui.Code("dbtool job add"))
			return
		}

		ui.PrintTable(jobHeaders, jobRows(jobs, time.Now()))
		ui.Blank()
		ui.Hint("%s · manage them with %s", ui.Plural(len(jobs), "job", "jobs"),
			ui.Code("dbtool job add | edit | delete"))
	},
}

func init() {
	jobCmd.AddCommand(jobListCmd)
}
