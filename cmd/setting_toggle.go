package cmd

import (
	"os"
	"strings"

	"dbtool/internal/logger"
	"dbtool/internal/settings"
	"dbtool/internal/ui"

	"github.com/spf13/cobra"
)

func enabledLabel(enabled bool) string {
	if enabled {
		return "enabled"
	}
	return "disabled"
}

// enabledStatus renders an enabled/disabled state with the shared ui
// on/off vocabulary ("● enabled" / "○ disabled").
func enabledStatus(enabled bool) string {
	return ui.Status(enabled, enabledLabel(enabled))
}

// splitRunHint splits a message of the form "Something is missing. Run
// 'dbtool …' first." into the message and the suggested command, so the
// command can be shown as a separate hint line. ok is false when msg holds
// no such suggestion.
func splitRunHint(msg string) (text, command string, ok bool) {
	i := strings.Index(msg, " Run '")
	if i < 0 {
		return msg, "", false
	}
	rest := msg[i+len(" Run '"):]
	j := strings.Index(rest, "'")
	if j < 0 {
		return msg, "", false
	}
	return msg[:i], rest[:j], true
}

// warnWithHint prints msg as a warning; a trailing "Run 'cmd' first."
// suggestion is moved onto its own hint line with the command highlighted.
func warnWithHint(msg string) {
	text, command, ok := splitRunHint(msg)
	ui.Warn("%s", text)
	if ok {
		ui.Hint("Run %s first", ui.Code(command))
	}
}

// fatalWithHint is warnWithHint for errors that end the command with exit 1.
func fatalWithHint(msg string) {
	text, command, ok := splitRunHint(msg)
	ui.Error("%s", text)
	if ok {
		ui.Hint("Run %s first", ui.Code(command))
	}
	os.Exit(1)
}

func featureToggleCommand(use, short string, update func(*settings.Settings) (bool, string)) *cobra.Command {
	return &cobra.Command{Use: use, Short: short, Run: func(cmd *cobra.Command, args []string) {
		s := settings.Load()
		ok, message := update(&s)
		if !ok {
			warnWithHint(message)
			return
		}
		if err := settings.Save(s); err != nil {
			logger.Error("failed to save settings: %v", err)
			ui.Fatal("Could not save settings: %v", err)
		}
		ui.Success("%s", message)
	}}
}
