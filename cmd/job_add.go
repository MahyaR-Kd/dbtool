package cmd

import (
	"fmt"
	"strings"

	"dbtool/internal/job"
	"dbtool/internal/secureinput"
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
				fmt.Println("For non-interactive mode, --name, --type, and --schedule are required.")
				return
			}

			// Passwords: use the flag if given, else DBTOOL_SRC_PASS /
			// DBTOOL_DST_PASS env vars, else prompt with masked input.
			// Existing scripted --src-pass/--dst-pass usage keeps working
			// unchanged; a missing password now prompts instead of erroring.
			switch j.Type {
			case "dump":
				if j.SrcConfigName == "" {
					fmt.Println("Dump jobs require --src-config.")
					return
				}
				pass, err := secureinput.ResolveSecret(jobAddSrcPass, "DBTOOL_SRC_PASS", "Source DB Password: ")
				if err != nil {
					fmt.Println("Error reading password:", err)
					return
				}
				j.SrcPassword = pass

			case "restore":
				if j.DstConfigName == "" || j.SrcConfigName == "" {
					fmt.Println("Restore jobs require --dst-config and --src-config.")
					return
				}
				pass, err := secureinput.ResolveSecret(jobAddDstPass, "DBTOOL_DST_PASS", "Destination DB Password: ")
				if err != nil {
					fmt.Println("Error reading password:", err)
					return
				}
				j.DstPassword = pass

			case "sync":
				if j.SrcConfigName == "" || j.DstConfigName == "" {
					fmt.Println("Sync jobs require --src-config and --dst-config.")
					return
				}
				srcPass, err := secureinput.ResolveSecret(jobAddSrcPass, "DBTOOL_SRC_PASS", "Source DB Password: ")
				if err != nil {
					fmt.Println("Error reading password:", err)
					return
				}
				j.SrcPassword = srcPass

				dstPass, err := secureinput.ResolveSecret(jobAddDstPass, "DBTOOL_DST_PASS", "Destination DB Password: ")
				if err != nil {
					fmt.Println("Error reading password:", err)
					return
				}
				j.DstPassword = dstPass

			default:
				fmt.Println("Unknown job type. Use dump, restore, or sync.")
				return
			}
		} else {
			j = job.AskInteractive()
		}

		job.Save(j)
		fmt.Println("Saved")
	},
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
