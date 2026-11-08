package cmd

import (
	"strings"
	"time"

	"dbtool/internal/interactivelist"
	"dbtool/internal/job"
	"dbtool/internal/secureinput"
	"dbtool/internal/ui"

	"github.com/spf13/cobra"
)

var jobAddName string
var jobAddType string
var jobAddSchedule string
var jobAddSrcConfig string
var jobAddSrcPass string
var jobAddDstConfig string
var jobAddDstPass string
var jobAddOverwriteTables bool

var jobAddCmd = &cobra.Command{
	Use:   "add",
	Short: "Add a new scheduled dump/restore job",
	Run: func(cmd *cobra.Command, args []string) {
		hasNonInteractiveInput := jobAddName != "" ||
			jobAddType != "" ||
			jobAddSchedule != "" ||
			jobAddSrcConfig != "" ||
			jobAddSrcPass != "" ||
			jobAddDstConfig != "" ||
			jobAddDstPass != "" ||
			cmd.Flags().Changed("overwrite-tables")

		var j job.Config
		if hasNonInteractiveInput {
			j = job.Config{
				Name:            strings.TrimSpace(jobAddName),
				Type:            strings.TrimSpace(jobAddType),
				Schedule:        strings.TrimSpace(jobAddSchedule),
				SrcConfigName:   strings.TrimSpace(jobAddSrcConfig),
				DstConfigName:   strings.TrimSpace(jobAddDstConfig),
				OverwriteTables: jobAddOverwriteTables,
			}

			if j.Name == "" || j.Type == "" || j.Schedule == "" {
				ui.Error("For non-interactive mode, --name, --type, and --schedule are required.")
				return
			}

			// Passwords: use the flag if given, else DBTOOL_SRC_PASS /
			// DBTOOL_DST_PASS env vars, else prompt with masked input.
			// Existing scripted --src-pass/--dst-pass usage keeps working
			// unchanged; a missing password now prompts instead of erroring.
			switch j.Type {
			case "dump":
				if j.SrcConfigName == "" {
					ui.Error("Dump jobs require --src-config.")
					return
				}
				pass, err := secureinput.ResolveSecret(jobAddSrcPass, "DBTOOL_SRC_PASS", "Source DB Password: ")
				if err != nil {
					ui.Error("Could not read password: %v", err)
					return
				}
				j.SrcPassword = pass

			case "restore":
				if j.DstConfigName == "" || j.SrcConfigName == "" {
					ui.Error("Restore jobs require --dst-config and --src-config.")
					return
				}
				pass, err := secureinput.ResolveSecret(jobAddDstPass, "DBTOOL_DST_PASS", "Destination DB Password: ")
				if err != nil {
					ui.Error("Could not read password: %v", err)
					return
				}
				j.DstPassword = pass

			case "sync":
				if j.SrcConfigName == "" || j.DstConfigName == "" {
					ui.Error("Sync jobs require --src-config and --dst-config.")
					return
				}
				srcPass, err := secureinput.ResolveSecret(jobAddSrcPass, "DBTOOL_SRC_PASS", "Source DB Password: ")
				if err != nil {
					ui.Error("Could not read password: %v", err)
					return
				}
				j.SrcPassword = srcPass

				dstPass, err := secureinput.ResolveSecret(jobAddDstPass, "DBTOOL_DST_PASS", "Destination DB Password: ")
				if err != nil {
					ui.Error("Could not read password: %v", err)
					return
				}
				j.DstPassword = dstPass

			default:
				ui.Error("Unknown job type. Use dump, restore, or sync.")
				return
			}
		} else {
			j = job.AskInteractive()
			if !confirmSaveJob(j) {
				return
			}
		}

		if err := job.Save(j); err != nil {
			ui.Error("Failed to save jobs: %v", err)
			return
		}
		jobSaved(j)
	},
}

// confirmSaveJob shows a review box for j and asks whether to save it.
// Only the interactive add/edit forms use it. On "no" it prints
// "Not saved" and returns false.
func confirmSaveJob(j job.Config) bool {
	ui.Blank()
	ui.PrintBox("Review", ui.BadgeAccent, jobReview(j))
	if !interactivelist.Confirm("Save this job?", true) {
		ui.Warn("Not saved")
		return false
	}
	return true
}

// jobReview renders j as key/value rows. Passwords are never shown, only
// whether one is set for the side that needs it.
func jobReview(j job.Config) string {
	passState := func(p string) string {
		if p != "" {
			return "saved"
		}
		return ui.Yellow("not set")
	}
	var srcPass, dstPass, overwrite string
	switch j.Type {
	case "dump":
		srcPass = passState(j.SrcPassword)
	case "restore":
		dstPass = passState(j.DstPassword)
	case "sync":
		srcPass, dstPass = passState(j.SrcPassword), passState(j.DstPassword)
	}
	if j.Type == "restore" || j.Type == "sync" {
		overwrite = "no"
		if j.OverwriteTables {
			overwrite = "yes"
		}
	}
	schedule := j.Schedule
	if next := job.DescribeNext(j.Schedule, time.Now()); next != "" {
		schedule += "  " + ui.Muted(next)
	}
	return ui.KeyValues(
		ui.KV{Key: "Name", Value: ui.Bold(j.Name)},
		ui.KV{Key: "Type", Value: jobTypeBadge(j.Type)},
		ui.KV{Key: "Schedule", Value: schedule},
		ui.KV{Key: "Flow", Value: jobFlow(j)},
		ui.KV{Key: "Source password", Value: srcPass},
		ui.KV{Key: "Dest. password", Value: dstPass},
		ui.KV{Key: "Overwrite tables", Value: overwrite},
	)
}

// jobSaved prints the success line for a saved job and when it runs next.
func jobSaved(j job.Config) {
	ui.Success("Job %s saved", ui.Bold(j.Name))
	if next := job.DescribeNext(j.Schedule, time.Now()); next != "" {
		ui.Hint("Next run: %s", next)
	}
	ui.Hint("See all jobs with %s", ui.Code("dbtool job list"))
}

func init() {
	jobAddCmd.Flags().StringVar(&jobAddName, "name", "", "Job name (non-interactive)")
	jobAddCmd.Flags().StringVar(&jobAddType, "type", "", "Job type: dump|restore|sync (non-interactive)")
	jobAddCmd.Flags().StringVar(&jobAddSchedule, "schedule", "", "Cron schedule expression (non-interactive)")
	jobAddCmd.Flags().StringVar(&jobAddSrcConfig, "src-config", "", "Source config name (non-interactive)")
	jobAddCmd.Flags().StringVar(&jobAddSrcPass, "src-pass", "", "Source database password (non-interactive; falls back to DBTOOL_SRC_PASS env var, then a masked prompt)")
	jobAddCmd.Flags().StringVar(&jobAddDstConfig, "dst-config", "", "Destination config name (non-interactive)")
	jobAddCmd.Flags().StringVar(&jobAddDstPass, "dst-pass", "", "Destination database password (non-interactive; falls back to DBTOOL_DST_PASS env var, then a masked prompt)")
	jobAddCmd.Flags().BoolVar(&jobAddOverwriteTables, "overwrite-tables", false, "Pass --overwrite-tables to myloader (non-interactive)")
	jobCmd.AddCommand(jobAddCmd)
}
