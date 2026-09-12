package cmd

import (
	"fmt"
	"time"

	"dbtool/internal/job"
	"dbtool/internal/logger"
	"github.com/spf13/cobra"
)

// schedulerTickCmd is a hidden command invoked by the crontab entry every
// minute. It checks all saved jobs against the current time and runs any that
// are due synchronously (one cron invocation = one tick).
var schedulerTickCommand = &cobra.Command{
	Use:    "_scheduler_tick",
	Short:  "Internal: execute jobs due at the current minute (called by cron)",
	Hidden: true,
	Run: func(cmd *cobra.Command, args []string) {
		jobs := job.Load()
		if len(jobs) == 0 {
			return
		}

		now := time.Now().Truncate(time.Minute)
		logger.Debug("[scheduler_tick] tick at %s, checking %d job(s)", now.Format("2006-01-02 15:04"), len(jobs))

		for _, j := range jobs {
			if job.MatchSchedule(j.Schedule, now) {
				logger.Info("[scheduler_tick] triggering job %q (%s)", j.Name, j.Type)
				fmt.Printf("[scheduler] running job %q (%s)\n", j.Name, j.Type)
				job.RunJob(j)
			}
		}
	},
}

func init() {
	rootCmd.AddCommand(schedulerTickCommand)
}
