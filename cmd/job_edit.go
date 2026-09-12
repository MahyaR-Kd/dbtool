package cmd

import (
	"fmt"

	"dbtool/internal/interactivelist"
	"dbtool/internal/job"

	"github.com/spf13/cobra"
)

var jobEditName string
var jobEditNewName string
var jobEditType string
var jobEditSchedule string
var jobEditSrcConfig string
var jobEditSrcPass string
var jobEditDstConfig string
var jobEditDstPass string
var jobEditOverwriteTables bool

var jobEditCmd = &cobra.Command{
	Use:   "edit",
	Short: "Edit an existing scheduled job",
	Run: func(cmd *cobra.Command, args []string) {
		jobs := job.Load()

		if len(jobs) == 0 {
			fmt.Println("No jobs found")
			return
		}

		if jobEditName != "" {
			found := -1
			for i := range jobs {
				if jobs[i].Name == jobEditName {
					found = i
					break
				}
			}
			if found == -1 {
				fmt.Println("Job not found:", jobEditName)
				return
			}

			updated := jobs[found]
			if cmd.Flags().Changed("new-name") {
				updated.Name = jobEditNewName
			}
			if cmd.Flags().Changed("type") {
				updated.Type = jobEditType
			}
			if cmd.Flags().Changed("schedule") {
				updated.Schedule = jobEditSchedule
			}
			if cmd.Flags().Changed("src-config") {
				updated.SrcConfigName = jobEditSrcConfig
			}
			if cmd.Flags().Changed("src-pass") {
				updated.SrcPassword = jobEditSrcPass
			}
			if cmd.Flags().Changed("dst-config") {
				updated.DstConfigName = jobEditDstConfig
			}
			if cmd.Flags().Changed("dst-pass") {
				updated.DstPassword = jobEditDstPass
			}
			if cmd.Flags().Changed("overwrite-tables") {
				updated.OverwriteTables = jobEditOverwriteTables
			}

			if updated.Name == "" || updated.Type == "" || updated.Schedule == "" {
				fmt.Println("Job name, type, and schedule cannot be empty.")
				return
			}
			switch updated.Type {
			case "dump":
				if updated.SrcConfigName == "" || updated.SrcPassword == "" {
					fmt.Println("Dump jobs require source config and source password.")
					return
				}
			case "restore":
				if updated.DstConfigName == "" || updated.DstPassword == "" || updated.SrcConfigName == "" {
					fmt.Println("Restore jobs require destination config/password and source config.")
					return
				}
			case "sync":
				if updated.SrcConfigName == "" || updated.SrcPassword == "" || updated.DstConfigName == "" || updated.DstPassword == "" {
					fmt.Println("Sync jobs require source+destination configs/passwords.")
					return
				}
			default:
				fmt.Println("Unknown type. Use dump, restore, or sync.")
				return
			}

			jobs[found] = updated
			job.Overwrite(jobs)
			fmt.Println("Updated successfully")
			return
		}

		options := make([]string, len(jobs))
		for i, j := range jobs {
			options[i] = fmt.Sprintf("%s [%s] schedule=%q", j.Name, j.Type, j.Schedule)
		}

		idx, err := interactivelist.SelectOne("Select job to edit", options)
		if err != nil {
			fmt.Println("Invalid selection")
			return
		}

		updated := job.EditInteractive(jobs[idx])
		jobs[idx] = updated
		job.Overwrite(jobs)

		fmt.Println("Updated successfully")
	},
}

func init() {
	jobEditCmd.Flags().StringVar(&jobEditName, "name", "", "Job name to edit (non-interactive)")
	jobEditCmd.Flags().StringVar(&jobEditNewName, "new-name", "", "New job name (non-interactive)")
	jobEditCmd.Flags().StringVar(&jobEditType, "type", "", "New job type: dump|restore|sync (non-interactive)")
	jobEditCmd.Flags().StringVar(&jobEditSchedule, "schedule", "", "New cron schedule (non-interactive)")
	jobEditCmd.Flags().StringVar(&jobEditSrcConfig, "src-config", "", "New source config name (non-interactive)")
	jobEditCmd.Flags().StringVar(&jobEditSrcPass, "src-pass", "", "New source password (non-interactive)")
	jobEditCmd.Flags().StringVar(&jobEditDstConfig, "dst-config", "", "New destination config name (non-interactive)")
	jobEditCmd.Flags().StringVar(&jobEditDstPass, "dst-pass", "", "New destination password (non-interactive)")
	jobEditCmd.Flags().BoolVar(&jobEditOverwriteTables, "overwrite-tables", false, "Set overwrite-tables for myloader (non-interactive)")
	jobCmd.AddCommand(jobEditCmd)
}
