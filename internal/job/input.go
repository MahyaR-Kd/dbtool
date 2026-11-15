package job

import (
	"bufio"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/MahyaR-Kd/dbtool/internal/interactivelist"
	"github.com/MahyaR-Kd/dbtool/internal/secureinput"
	"github.com/MahyaR-Kd/dbtool/internal/ui"
)

// jobTypes lists every valid job Type value, in the order offered by the
// arrow-key picker.
var jobTypes = []string{"dump", "restore", "sync"}

// AskInteractive prompts the user to fill in a new JobConfig interactively.
func AskInteractive() Config {
	reader := secureinput.Stdin()

	j := Config{}

	ui.Section("Job")
	j.Name = interactivelist.TextValidated(reader, "Job Name", "", uniqueJobName(""))
	j.Type = selectJobType("")
	j.Schedule = askSchedule(reader, "")

	switch j.Type {
	case "dump":
		ui.Section("Source")
		j.SrcConfigName = interactivelist.Text(reader, "Source Config Name (saved DB config to dump from)", "")
		j.SrcPassword = askPassword("Source DB Password")

	case "restore":
		ui.Section("Destination")
		j.DstConfigName = interactivelist.Text(reader, "Destination Config Name (saved DB config to restore into)", "")
		j.DstPassword = askPassword("Destination DB Password")
		ui.Section("Source")
		j.SrcConfigName = interactivelist.Text(reader, "Source Config Name (used to find latest dump folder, e.g. the name of the source DB config)", "")
		ui.Section("Options")
		j.OverwriteTables = interactivelist.Confirm("Overwrite existing tables?", false)

	case "sync":
		ui.Section("Source")
		j.SrcConfigName = interactivelist.Text(reader, "Source Config Name (saved DB config to dump from)", "")
		j.SrcPassword = askPassword("Source DB Password")
		ui.Section("Destination")
		j.DstConfigName = interactivelist.Text(reader, "Destination Config Name (saved DB config to restore into)", "")
		j.DstPassword = askPassword("Destination DB Password")
		ui.Section("Options")
		j.OverwriteTables = interactivelist.Confirm("Overwrite existing tables?", false)

	default:
		ui.Warn("Type selection canceled — no type set.")
	}

	return j
}

// scheduleLabel is the prompt for a job's cron schedule.
const scheduleLabel = "Schedule (cron expression, e.g. \"0 2 * * *\")"

// askSchedule prompts for a cron schedule, refusing invalid expressions,
// then shows when it will next fire.
func askSchedule(reader *bufio.Reader, current string) string {
	schedule := interactivelist.TextValidated(reader, scheduleLabel, current, ValidateSchedule)
	if next := DescribeNext(schedule, time.Now()); next != "" {
		ui.Hint("Next run: %s", next)
	}
	return schedule
}

// uniqueJobName validates a job name: non-empty, not used by another saved
// job. keep is the name the form started with (when editing).
func uniqueJobName(keep string) interactivelist.Validator {
	taken := make(map[string]bool)
	for _, j := range Load() {
		if j.Name != keep {
			taken[j.Name] = true
		}
	}
	return func(name string) error {
		if err := interactivelist.Required(name); err != nil {
			return err
		}
		if strings.Contains(name, "|") {
			return errors.New("the name can't contain \"|\"")
		}
		if taken[name] {
			return fmt.Errorf("a job named %q already exists", name)
		}
		return nil
	}
}

// selectJobType offers jobTypes through the arrow-key picker, ordered so
// current (if it's a known type) starts on top — matching the "press
// Enter to keep it" feel of every other field. Canceling keeps current.
func selectJobType(current string) string {
	options := jobTypes
	if current != "" {
		reordered := make([]string, 0, len(jobTypes))
		reordered = append(reordered, current)
		for _, t := range jobTypes {
			if t != current {
				reordered = append(reordered, t)
			}
		}
		options = reordered
	}

	idx, err := interactivelist.SelectOne("Type", options)
	if err != nil {
		return current
	}
	return options[idx]
}

// askPassword prompts for a password with masked (starred) input. On error
// (e.g. Ctrl+C) it reports the problem and returns an empty password rather
// than aborting the whole interactive flow.
func askPassword(label string) string {
	pass, err := secureinput.ReadPassword(interactivelist.PromptLabel(label, ""))
	if err != nil {
		ui.Error("Could not read password: %v", err)
		return ""
	}
	return pass
}

// EditInteractive prompts the user to update each field of an existing JobConfig.
// Pressing Enter on any field keeps its current value.
func EditInteractive(j Config) Config {
	reader := secureinput.Stdin()

	// readPassword prompts for a password with masked (starred) input,
	// without ever displaying the current value. Pressing Enter (empty
	// input) keeps the existing password unchanged.
	readPassword := func(prompt, current string) string {
		val := askPassword(prompt + " (press Enter to keep current)")
		if val == "" {
			return current
		}
		return val
	}

	ui.Section("Job")
	j.Name = interactivelist.TextValidated(reader, "Job Name", j.Name, uniqueJobName(j.Name))
	j.Type = selectJobType(j.Type)
	j.Schedule = askSchedule(reader, j.Schedule)

	switch j.Type {
	case "dump":
		ui.Section("Source")
		j.SrcConfigName = interactivelist.Text(reader, "Source Config Name", j.SrcConfigName)
		j.SrcPassword = readPassword("Source DB Password", j.SrcPassword)

	case "restore":
		ui.Section("Destination")
		j.DstConfigName = interactivelist.Text(reader, "Destination Config Name", j.DstConfigName)
		j.DstPassword = readPassword("Destination DB Password", j.DstPassword)
		ui.Section("Source")
		j.SrcConfigName = interactivelist.Text(reader, "Source Config Name", j.SrcConfigName)
		ui.Section("Options")
		j.OverwriteTables = interactivelist.Confirm("Overwrite existing tables?", j.OverwriteTables)

	case "sync":
		ui.Section("Source")
		j.SrcConfigName = interactivelist.Text(reader, "Source Config Name", j.SrcConfigName)
		j.SrcPassword = readPassword("Source DB Password", j.SrcPassword)
		ui.Section("Destination")
		j.DstConfigName = interactivelist.Text(reader, "Destination Config Name", j.DstConfigName)
		j.DstPassword = readPassword("Destination DB Password", j.DstPassword)
		ui.Section("Options")
		j.OverwriteTables = interactivelist.Confirm("Overwrite existing tables?", j.OverwriteTables)

	default:
		ui.Warn("Unknown type. Use dump, restore, or sync.")
	}

	return j
}
