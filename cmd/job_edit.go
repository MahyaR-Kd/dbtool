package cmd

import (
	"dbtool/internal/interactivelist"
	"dbtool/internal/job"
	"dbtool/internal/ui"

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
			ui.Empty("No jobs yet", "Create one with "+ui.Code("dbtool job add"))
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
				ui.Error("Job not found: %s", jobEditName)
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
				ui.Error("Job name, type, and schedule cannot be empty.")
				return
			}
			if err := job.ValidateSchedule(updated.Schedule); err != nil {
				ui.Error("Invalid schedule %q: %v", updated.Schedule, err)
				return
			}
			switch updated.Type {
			case "dump":
				if updated.SrcConfigName == "" || updated.SrcPassword == "" {
					ui.Error("Dump jobs require source config and source password.")
					return
				}
			case "restore":
				if updated.DstConfigName == "" || updated.DstPassword == "" || updated.SrcConfigName == "" {
					ui.Error("Restore jobs require destination config/password and source config.")
					return
				}
			case "sync":
				if updated.SrcConfigName == "" || updated.SrcPassword == "" || updated.DstConfigName == "" || updated.DstPassword == "" {
					ui.Error("Sync jobs require source+destination configs/passwords.")
					return
				}
			default:
				ui.Error("Unknown type. Use dump, restore, or sync.")
				return
			}

			jobs[found] = updated
			if err := job.Overwrite(jobs); err != nil {
				ui.Error("Failed to save jobs: %v", err)
				return
			}
			ui.Success("Job %s updated", ui.Bold(updated.Name))
			return
		}

		options := make([]string, len(jobs))
		for i, j := range jobs {
			options[i] = jobOption(j)
		}

		idx, err := interactivelist.SelectOne("Choose a job to edit", options)
		if err != nil {
			ui.Error("Invalid selection")
			return
		}

		updated := job.EditInteractive(jobs[idx])
		if !confirmSaveJob(updated) {
			return
		}
		jobs[idx] = updated
		if err := job.Overwrite(jobs); err != nil {
			ui.Error("Failed to save jobs: %v", err)
			return
		}

		jobSaved(updated)
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
