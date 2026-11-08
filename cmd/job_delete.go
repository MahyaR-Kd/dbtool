package cmd

import (
	"dbtool/internal/interactivelist"
	"dbtool/internal/job"
	"dbtool/internal/ui"

	"github.com/spf13/cobra"
)

var jobDeleteName string

var jobDeleteCmd = &cobra.Command{
	Use:   "delete",
	Short: "Delete a scheduled job",
	Run: func(cmd *cobra.Command, args []string) {
		jobs := job.Load()

		if len(jobs) == 0 {
			ui.Empty("No jobs yet", "Create one with "+ui.Code("dbtool job add"))
			return
		}

		if jobDeleteName != "" {
			found := -1
			for i := range jobs {
				if jobs[i].Name == jobDeleteName {
					found = i
					break
				}
			}
			if found == -1 {
				ui.Error("Job not found: %s", jobDeleteName)
				return
			}
			jobs = append(jobs[:found], jobs[found+1:]...)
			if err := job.Overwrite(jobs); err != nil {
				ui.Error("Failed to save jobs: %v", err)
				return
			}
			ui.Success("Job %s deleted", ui.Bold(jobDeleteName))
			return
		}

		options := make([]string, len(jobs))
		for i, j := range jobs {
			options[i] = jobOption(j)
		}

		idx, err := interactivelist.SelectOne("Choose a job to delete", options)
		if err != nil {
			ui.Error("Invalid selection")
			return
		}

		deleted := jobs[idx].Name
		jobs = append(jobs[:idx], jobs[idx+1:]...)
		if err := job.Overwrite(jobs); err != nil {
			ui.Error("Failed to save jobs: %v", err)
			return
		}

		ui.Success("Job %s deleted", ui.Bold(deleted))
	},
}

func init() {
	jobDeleteCmd.Flags().StringVar(&jobDeleteName, "name", "", "Job name to delete (non-interactive)")
	jobCmd.AddCommand(jobDeleteCmd)
}
