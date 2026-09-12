package cmd

import (
	"dbtool/internal/job"
	"fmt"

	"github.com/spf13/cobra"
)

var jobListCmd = &cobra.Command{
	Use:   "list",
	Short: "List saved scheduled jobs",
	Run: func(cmd *cobra.Command, args []string) {
		jobs := job.Load()
		if len(jobs) == 0 {
			fmt.Println("No jobs found")
			return
		}
		for i, j := range jobs {
			switch j.Type {
			case "dump":
				fmt.Printf("%d) %s [dump] src=%s schedule=%q\n",
					i+1, j.Name, j.SrcConfigName, j.Schedule)
			case "restore":
				fmt.Printf("%d) %s [restore] src=%s dst=%s schedule=%q\n",
					i+1, j.Name, j.SrcConfigName, j.DstConfigName, j.Schedule)
			case "sync":
				fmt.Printf("%d) %s [sync] src=%s dst=%s schedule=%q\n",
					i+1, j.Name, j.SrcConfigName, j.DstConfigName, j.Schedule)
			default:
				fmt.Printf("%d) %s [%s] schedule=%q\n",
					i+1, j.Name, j.Type, j.Schedule)
			}
		}
	},
}

func init() {
	jobCmd.AddCommand(jobListCmd)
}
