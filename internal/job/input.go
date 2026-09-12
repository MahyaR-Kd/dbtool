package job

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"dbtool/internal/secureinput"
)

// AskInteractive prompts the user to fill in a new JobConfig interactively.
func AskInteractive() Config {
	reader := bufio.NewReader(os.Stdin)

	j := Config{}

	fmt.Print("Job Name: ")
	j.Name = readLine(reader)

	fmt.Print("Type (dump/restore/sync): ")
	j.Type = readLine(reader)

	fmt.Print("Schedule (cron expression, e.g. \"0 2 * * *\"): ")
	j.Schedule = readLine(reader)

	switch j.Type {
	case "dump":
		fmt.Print("Source Config Name (saved DB config to dump from): ")
		j.SrcConfigName = readLine(reader)
		j.SrcPassword = askPassword("Source DB Password: ")

	case "restore":
		fmt.Print("Destination Config Name (saved DB config to restore into): ")
		j.DstConfigName = readLine(reader)
		j.DstPassword = askPassword("Destination DB Password: ")
		fmt.Print("Source Config Name (used to find latest dump folder, e.g. the name of the source DB config): ")
		j.SrcConfigName = readLine(reader)
		fmt.Print("Overwrite existing tables? (y/N): ")
		j.OverwriteTables = strings.ToLower(readLine(reader)) == "y"

	case "sync":
		fmt.Print("Source Config Name (saved DB config to dump from): ")
		j.SrcConfigName = readLine(reader)
		j.SrcPassword = askPassword("Source DB Password: ")
		fmt.Print("Destination Config Name (saved DB config to restore into): ")
		j.DstConfigName = readLine(reader)
		j.DstPassword = askPassword("Destination DB Password: ")
		fmt.Print("Overwrite existing tables? (y/N): ")
		j.OverwriteTables = strings.ToLower(readLine(reader)) == "y"

	default:
		fmt.Println("Unknown type. Use dump, restore, or sync.")
	}

	return j
}

// askPassword prompts for a password with masked (starred) input. On error
// (e.g. Ctrl+C) it reports the problem and returns an empty password rather
// than aborting the whole interactive flow.
func askPassword(prompt string) string {
	pass, err := secureinput.ReadPassword(prompt)
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

	readField := func(prompt, current string) string {
		fmt.Printf("%s [%s]: ", prompt, current)
		val := readLine(reader)
		if val == "" {
			return current
		}
		return val
	}

	// readPassword prompts for a password with masked (starred) input,
	// without ever displaying the current value. Pressing Enter (empty
	// input) keeps the existing password unchanged.
	readPassword := func(prompt, current string) string {
		val := askPassword(fmt.Sprintf("%s [press Enter to keep current]: ", prompt))
		if val == "" {
			return current
		}
		return val
	}

	j.Name = readField("Job Name", j.Name)
	j.Type = readField("Type (dump/restore/sync)", j.Type)
	j.Schedule = readField("Schedule (cron expression, e.g. \"0 2 * * *\")", j.Schedule)

	switch j.Type {
	case "dump":
		j.SrcConfigName = readField("Source Config Name", j.SrcConfigName)
		j.SrcPassword = readPassword("Source DB Password", j.SrcPassword)

	case "restore":
		j.DstConfigName = readField("Destination Config Name", j.DstConfigName)
		j.DstPassword = readPassword("Destination DB Password", j.DstPassword)
		j.SrcConfigName = readField("Source Config Name", j.SrcConfigName)
		j.OverwriteTables = readBool(reader, "Overwrite existing tables?", j.OverwriteTables)

	case "sync":
		j.SrcConfigName = readField("Source Config Name", j.SrcConfigName)
		j.SrcPassword = readPassword("Source DB Password", j.SrcPassword)
		j.DstConfigName = readField("Destination Config Name", j.DstConfigName)
		j.DstPassword = readPassword("Destination DB Password", j.DstPassword)
		j.OverwriteTables = readBool(reader, "Overwrite existing tables?", j.OverwriteTables)

	default:
		fmt.Println("Unknown type. Use dump, restore, or sync.")
	}

	return j
}

func readLine(r *bufio.Reader) string {
	s, _ := r.ReadString('\n')
	return strings.TrimSpace(s)
}

// readBool shows the current bool value and accepts y/n (or Enter to keep current).
func readBool(r *bufio.Reader, prompt string, current bool) bool {
	cur := "N"
	if current {
		cur = "Y"
	}
	fmt.Printf("%s [%s] (y/N): ", prompt, cur)
	val := strings.ToLower(readLine(r))
	if val == "" {
		return current
	}
	return val == "y" || val == "yes"
}
