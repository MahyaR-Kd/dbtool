package job

import (
	"bufio"
	"fmt"
	"os"

	"dbtool/internal/interactivelist"
	"dbtool/internal/secureinput"
)

// jobTypes lists every valid job Type value, in the order offered by the
// arrow-key picker.
var jobTypes = []string{"dump", "restore", "sync"}

// AskInteractive prompts the user to fill in a new JobConfig interactively.
func AskInteractive() Config {
	reader := bufio.NewReader(os.Stdin)

	j := Config{}

	j.Name = interactivelist.Text(reader, "Job Name", "")
	j.Type = selectJobType("")
	j.Schedule = interactivelist.Text(reader, "Schedule (cron expression, e.g. \"0 2 * * *\")", "")

	switch j.Type {
	case "dump":
		j.SrcConfigName = interactivelist.Text(reader, "Source Config Name (saved DB config to dump from)", "")
		j.SrcPassword = askPassword("Source DB Password")

	case "restore":
		j.DstConfigName = interactivelist.Text(reader, "Destination Config Name (saved DB config to restore into)", "")
		j.DstPassword = askPassword("Destination DB Password")
		j.SrcConfigName = interactivelist.Text(reader, "Source Config Name (used to find latest dump folder, e.g. the name of the source DB config)", "")
		j.OverwriteTables = interactivelist.Confirm("Overwrite existing tables?", false)

	case "sync":
		j.SrcConfigName = interactivelist.Text(reader, "Source Config Name (saved DB config to dump from)", "")
		j.SrcPassword = askPassword("Source DB Password")
		j.DstConfigName = interactivelist.Text(reader, "Destination Config Name (saved DB config to restore into)", "")
		j.DstPassword = askPassword("Destination DB Password")
		j.OverwriteTables = interactivelist.Confirm("Overwrite existing tables?", false)

	default:
		fmt.Println("Type selection canceled — no type set.")
	}

	return j
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
		fmt.Println("Error reading password:", err)
		return ""
	}
	return pass
}

// EditInteractive prompts the user to update each field of an existing JobConfig.
// Pressing Enter on any field keeps its current value.
func EditInteractive(j Config) Config {
	reader := bufio.NewReader(os.Stdin)

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

	j.Name = interactivelist.Text(reader, "Job Name", j.Name)
	j.Type = selectJobType(j.Type)
	j.Schedule = interactivelist.Text(reader, "Schedule (cron expression, e.g. \"0 2 * * *\")", j.Schedule)

	switch j.Type {
	case "dump":
		j.SrcConfigName = interactivelist.Text(reader, "Source Config Name", j.SrcConfigName)
		j.SrcPassword = readPassword("Source DB Password", j.SrcPassword)

	case "restore":
		j.DstConfigName = interactivelist.Text(reader, "Destination Config Name", j.DstConfigName)
		j.DstPassword = readPassword("Destination DB Password", j.DstPassword)
		j.SrcConfigName = interactivelist.Text(reader, "Source Config Name", j.SrcConfigName)
		j.OverwriteTables = interactivelist.Confirm("Overwrite existing tables?", j.OverwriteTables)

	case "sync":
		j.SrcConfigName = interactivelist.Text(reader, "Source Config Name", j.SrcConfigName)
		j.SrcPassword = readPassword("Source DB Password", j.SrcPassword)
		j.DstConfigName = interactivelist.Text(reader, "Destination Config Name", j.DstConfigName)
		j.DstPassword = readPassword("Destination DB Password", j.DstPassword)
		j.OverwriteTables = interactivelist.Confirm("Overwrite existing tables?", j.OverwriteTables)

	default:
		fmt.Println("Unknown type. Use dump, restore, or sync.")
	}

	return j
}
