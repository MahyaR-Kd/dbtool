package cmd

import (
	"fmt"
	"strings"
	"text/template"

	"dbtool/internal/config"
	"dbtool/internal/job"
	"dbtool/internal/settings"
	"dbtool/internal/ui"

	"github.com/spf13/cobra"
)

// Command groups shown in "dbtool --help".
const (
	groupBackup = "backup"
	groupManage = "manage"
	groupSystem = "system"
)

var commandGroupOf = map[string]string{
	"dump":     groupBackup,
	"restore":  groupBackup,
	"validate": groupBackup,
	"decrypt":  groupBackup,
	"config":   groupManage,
	"job":      groupManage,
	"schedule": groupManage,
	"ui":       groupManage,
	"setting":  groupSystem,
	"log":      groupSystem,
	"install":  groupSystem,
}

// assignCommandGroups files each top-level command under a help group.
// Done at startup rather than in each command's own file so the grouping
// lives in one place; commands not in the map land in "Additional Commands".
func assignCommandGroups() {
	groups := []*cobra.Group{
		{ID: groupBackup, Title: "Backup & restore"},
		{ID: groupManage, Title: "Configs & jobs"},
		{ID: groupSystem, Title: "Settings & maintenance"},
	}
	used := map[string]bool{groupSystem: true} // "help" always lives there
	for _, c := range rootCmd.Commands() {
		if g, ok := commandGroupOf[c.Name()]; ok && c.GroupID == "" {
			c.GroupID = g
		}
		if c.IsAvailableCommand() {
			used[c.GroupID] = true
		}
	}
	// Only declare groups that have something in them, so a not-yet-
	// installed binary (which hides most commands) shows no empty headings.
	for _, g := range groups {
		if used[g.ID] && !rootCmd.ContainsGroup(g.ID) {
			rootCmd.AddGroup(g)
		}
	}
	for _, c := range rootCmd.Commands() {
		if c.GroupID != "" && !rootCmd.ContainsGroup(c.GroupID) {
			c.GroupID = ""
		}
	}
	rootCmd.SetHelpCommandGroupID(groupSystem)
}

// setupHelp installs the colored help/usage template on root (inherited by
// every subcommand). Colors vanish automatically when piped or --no-color.
func setupHelp(root *cobra.Command) {
	cobra.AddTemplateFuncs(template.FuncMap{
		"heading": func(s string) string { return ui.Accent(s) },
		"cmdName": func(s string, pad int) string { return ui.Blue(fmt.Sprintf("%-*s", pad, s)) },
		"muted":   ui.Muted,
		"bold":    ui.Bold,
		"brand":   func() string { return ui.Accent(ui.Icon(ui.IconBrand)) },
		"flagUsages": func(s string) string {
			return colorFlags(s)
		},
	})
	root.SetUsageTemplate(usageTemplate)
	root.SetHelpTemplate(helpTemplate)
}

// colorFlags highlights the flag names in pflag's pre-formatted usage block.
func colorFlags(block string) string {
	lines := strings.Split(strings.TrimRight(block, "\n"), "\n")
	for i, l := range lines {
		trimmed := strings.TrimLeft(l, " ")
		if !strings.HasPrefix(trimmed, "-") {
			continue
		}
		indent := l[:len(l)-len(trimmed)]
		// Flag spec ends at the first run of 2+ spaces.
		if idx := strings.Index(trimmed, "   "); idx > 0 {
			lines[i] = indent + ui.Blue(trimmed[:idx]) + ui.Muted("") + trimmed[idx:]
		} else {
			lines[i] = indent + ui.Blue(trimmed)
		}
	}
	return strings.Join(lines, "\n")
}

const helpTemplate = `{{with (or .Long .Short)}}{{brand}} {{bold .}}

{{end}}{{if or .Runnable .HasSubCommands}}{{.UsageString}}{{end}}`

const usageTemplate = `{{heading "Usage"}}{{if .Runnable}}
  {{.UseLine}}{{end}}{{if .HasAvailableSubCommands}}
  {{.CommandPath}} {{muted "<command> [flags]"}}{{end}}{{if gt (len .Aliases) 0}}

{{heading "Aliases"}}
  {{.NameAndAliases}}{{end}}{{if .HasExample}}

{{heading "Examples"}}
{{.Example}}{{end}}{{if .HasAvailableSubCommands}}{{$cmds := .Commands}}{{if eq (len .Groups) 0}}

{{heading "Commands"}}{{range $cmds}}{{if (or .IsAvailableCommand (eq .Name "help"))}}
  {{cmdName .Name .NamePadding}} {{.Short}}{{end}}{{end}}{{else}}{{range $group := .Groups}}

{{heading .Title}}{{range $cmds}}{{if (and (eq .GroupID $group.ID) (or .IsAvailableCommand (eq .Name "help")))}}
  {{cmdName .Name .NamePadding}} {{.Short}}{{end}}{{end}}{{end}}{{if not .AllChildCommandsHaveGroup}}

{{heading "Other"}}{{range $cmds}}{{if (and (eq .GroupID "") (or .IsAvailableCommand (eq .Name "help")))}}
  {{cmdName .Name .NamePadding}} {{.Short}}{{end}}{{end}}{{end}}{{end}}{{end}}{{if .HasAvailableLocalFlags}}

{{heading "Flags"}}
{{flagUsages .LocalFlags.FlagUsages}}{{end}}{{if .HasAvailableInheritedFlags}}

{{heading "Global flags"}}
{{flagUsages .InheritedFlags.FlagUsages}}{{end}}{{if .HasHelpSubCommands}}

{{heading "Additional help topics"}}{{range .Commands}}{{if .IsAdditionalHelpTopicCommand}}
  {{cmdName .CommandPath .CommandPathPadding}} {{.Short}}{{end}}{{end}}{{end}}{{if .HasAvailableSubCommands}}

{{muted (printf "Run \"%s <command> --help\" for more about a command." .CommandPath)}}{{end}}
`

// printWelcome is what a bare "dbtool" prints: a banner, a one-line status
// of what is configured, and the handful of commands people use most.
func printWelcome() {
	ui.Println(ui.Accent(ui.Icon(ui.IconBrand)+" dbtool") + "  " + ui.Muted(Version))
	ui.Println(ui.Muted("  MySQL dump & restore · SSH tunnels · scheduled jobs · S3 & Telegram"))
	ui.Blank()

	if !installedInPath {
		ui.Warn("dbtool is not installed on your PATH yet — most commands are hidden until it is")
		ui.Hint("Run %s to install it and set up shell completion", ui.Code("./dbtool install"))
		ui.Blank()
		return
	}

	configs := config.Load()
	jobs := job.Load()
	s := settings.Load()

	storage := "local · " + s.WorkDir
	if s.S3Enabled() {
		storage = "s3://" + s.S3.Bucket + "/" + strings.TrimSuffix(s.S3.Prefix, "/")
	}
	sched := ""
	if len(jobs) > 0 {
		current, _ := readCrontab()
		sched = ui.Status(strings.Contains(current, schedulerTickCmd), "background scheduling")
	}
	ui.PrintKeyValues(
		ui.KV{Key: "Configs", Value: fmt.Sprint(len(configs))},
		ui.KV{Key: "Jobs", Value: fmt.Sprint(len(jobs))},
		ui.KV{Key: "Storage", Value: storage},
		ui.KV{Key: "Telegram", Value: ui.OnOff(s.Telegram.Enabled())},
		ui.KV{Key: "Scheduler", Value: sched},
	)
	ui.Blank()

	type entry struct{ cmd, what string }
	var steps []entry
	if len(configs) == 0 {
		steps = append(steps, entry{"dbtool config add", "Save your first database connection"})
	}
	steps = append(steps,
		entry{"dbtool dump", "Dump a database"},
		entry{"dbtool restore", "Restore a dump into a database"},
		entry{"dbtool job add", "Schedule a recurring dump / restore / sync"},
		entry{"dbtool ui", "Open the interactive dashboard"},
	)
	ui.Println(ui.Accent("Get started"))
	for _, e := range steps {
		ui.Printf("  %s  %s\n", ui.Blue(fmt.Sprintf("%-18s", e.cmd)), e.what)
	}
	ui.Blank()
	ui.Hint("Run %s for every command", ui.Code("dbtool --help"))
}
