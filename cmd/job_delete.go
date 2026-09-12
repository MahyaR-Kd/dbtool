package cmd

import (
	"fmt"

	"dbtool/internal/interactivelist"
	"dbtool/internal/job"
	"github.com/spf13/cobra"
)

var jobDeleteName string

var jobDeleteCmd = &cobra.Command{
	Use:   "delete",
	Short: "Delete a scheduled job",
	Run: func(cmd *cobra.Command, args []string) {
		jobs := job.Load()

		if len(jobs) == 0 {
			fmt.Println("No jobs found")
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
				fmt.Println("Job not found:", jobDeleteName)
				return
			}
			jobs = append(jobs[:found], jobs[found+1:]...)
			job.Overwrite(jobs)
			fmt.Println("Deleted successfully")
			return
		}

		options := make([]string, len(jobs))
		for i, j := range jobs {
			options[i] = fmt.Sprintf("%s [%s] schedule=%q", j.Name, j.Type, j.Schedule)
		}

		idx, err := interactivelist.SelectOne("Select job to delete", options)
		if err != nil {
			fmt.Println("Invalid selection")
			return
		}

		jobs = append(jobs[:idx], jobs[idx+1:]...)
		job.Overwrite(jobs)

		fmt.Println("Deleted successfully")
	},
}

func init() {
	jobDeleteCmd.Flags().StringVar(&jobDeleteName, "name", "", "Job name to delete (non-interactive)")
	jobCmd.AddCommand(jobDeleteCmd)
}
