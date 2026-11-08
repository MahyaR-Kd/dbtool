package cmd

import (
	"strconv"
	"strings"

	"dbtool/internal/config"
	"dbtool/internal/interactivelist"
	"dbtool/internal/types"
	"dbtool/internal/ui"

	"github.com/spf13/cobra"
)

var configAddName string
var configAddHost string
var configAddPort string
var configAddUser string
var configAddRetentionDays int
var configAddSSH bool
var configAddSSHHost string
var configAddSSHUser string
var configAddSSHPort string
var configAddIgnoredSchemas string
var configAddIgnoredTables string
var configAddNoLocks bool
var configAddPassword string
var configAddSendTelegram bool

var configAddCmd = &cobra.Command{
	Use:   "add",
	Short: "Add a new database connection config",
	Run: func(cmd *cobra.Command, args []string) {
		hasNonInteractiveInput := configAddName != "" ||
			configAddHost != "" ||
			configAddPort != "" ||
			configAddUser != "" ||
			configAddRetentionDays >= 0 ||
			configAddSSH ||
			configAddSSHHost != "" ||
			configAddSSHUser != "" ||
			configAddSSHPort != "" ||
			configAddIgnoredSchemas != "" ||
			configAddIgnoredTables != "" ||
			configAddPassword != "" ||
			cmd.Flags().Changed("send-telegram") ||
			cmd.Flags().Changed("no-locks")

		var c types.Config
		if hasNonInteractiveInput {
			if configAddName == "" || configAddHost == "" || configAddPort == "" || configAddUser == "" {
				ui.Error("For non-interactive mode, --name, --host, --port, and --user are required.")
				return
			}
			if configAddSSH && (configAddSSHHost == "" || configAddSSHUser == "" || configAddSSHPort == "") {
				ui.Error("When --ssh is set, --ssh-host, --ssh-user, and --ssh-port are required.")
				return
			}
			if !configAddSSH && (configAddSSHHost != "" || configAddSSHUser != "" || configAddSSHPort != "") {
				ui.Error("--ssh-host, --ssh-user, and --ssh-port can only be used with --ssh.")
				return
			}

			c = types.Config{
				Name:             configAddName,
				Host:             configAddHost,
				Port:             configAddPort,
				User:             configAddUser,
				SSH:              configAddSSH,
				SSHHost:          configAddSSHHost,
				SSHUser:          configAddSSHUser,
				SSHPort:          configAddSSHPort,
				IgnoredSchemas:   parseCommaSeparated(configAddIgnoredSchemas),
				IgnoredTables:    parseIgnoredTablesFlag(configAddIgnoredTables),
				NoLocks:          configAddNoLocks,
				Password:         configAddPassword,
				TelegramDisabled: !configAddSendTelegram,
			}
			if configAddRetentionDays >= 0 {
				c.RetentionDays = configAddRetentionDays
			}
		} else {
			c = config.AskInteractive()
			if !confirmSaveConfig(c) {
				return
			}
		}

		if err := config.Save(c); err != nil {
			ui.Error("Failed to save configs: %v", err)
			return
		}
		configSaved(c.Name)
	},
}

// confirmSaveConfig shows a review box for c and asks whether to save it.
// Used only by the interactive add/edit/duplicate forms, never by the
// flag-driven paths. On "no" it prints "Not saved" and returns false.
func confirmSaveConfig(c types.Config) bool {
	ui.Blank()
	ui.PrintBox("Review", ui.BadgeAccent, configReview(c))
	if !interactivelist.Confirm("Save this config?", true) {
		ui.Warn("Not saved")
		return false
	}
	return true
}

// configReview renders c as key/value rows for the review box. The
// password is never shown — only whether one is saved.
func configReview(c types.Config) string {
	password := ui.Muted("not saved")
	if c.Password != "" {
		password = "saved"
	}
	telegram := ""
	if c.TelegramDisabled {
		telegram = "off for this config"
	}
	noLocks := ""
	if c.NoLocks {
		noLocks = "yes (--no-locks)"
	}
	ignores := ignoreDetail(c)
	return ui.KeyValues(
		ui.KV{Key: "Name", Value: ui.Bold(c.Name)},
		ui.KV{Key: "Server", Value: c.User + "@" + c.Host + ":" + c.Port},
		ui.KV{Key: "Route", Value: configRoute(c)},
		ui.KV{Key: "Retention", Value: configRetention(c.RetentionDays)},
		ui.KV{Key: "Skip locks", Value: noLocks},
		ui.KV{Key: "Telegram", Value: telegram},
		ui.KV{Key: "Password", Value: password},
		ui.KV{Key: "Ignores", Value: ignores},
	)
}

// configSaved prints the success line for a saved config, plus the obvious
// next step.
func configSaved(name string) {
	ui.Success("Config %s saved", ui.Bold(name))
	ui.Hint("Back it up now with %s", ui.Code("dbtool dump --name "+shellQuote(name)))
}

// shellQuote quotes s for display in a suggested command line when it
// contains anything a shell would split or expand.
func shellQuote(s string) string {
	if s != "" && strings.IndexFunc(s, func(r rune) bool {
		return !(r == '-' || r == '_' || r == '.' || r == '/' || r == ':' || r == '@' ||
			(r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9'))
	}) < 0 {
		return s
	}
	return strconv.Quote(s)
}

func parseCommaSeparated(input string) []string {
	input = strings.TrimSpace(input)
	if input == "" {
		return nil
	}
	parts := strings.Split(input, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

// parseIgnoredTablesFlag parses the --ignored-tables flag value of the form
// "schema1:tbl1,tbl2;schema2:tbl3" into a map[schema][]table.
func parseIgnoredTablesFlag(input string) map[string][]string {
	input = strings.TrimSpace(input)
	if input == "" {
		return nil
	}
	result := make(map[string][]string)
	for _, entry := range strings.Split(input, ";") {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}
		idx := strings.Index(entry, ":")
		if idx < 0 {
			continue
		}
		schema := strings.TrimSpace(entry[:idx])
		tablesRaw := strings.TrimSpace(entry[idx+1:])
		if schema == "" || tablesRaw == "" {
			continue
		}
		var tables []string
		for _, t := range strings.Split(tablesRaw, ",") {
			t = strings.TrimSpace(t)
			if t != "" {
				tables = append(tables, t)
			}
		}
		if len(tables) > 0 {
			result[schema] = tables
		}
	}
	if len(result) == 0 {
		return nil
	}
	return result
}

func init() {
	configAddCmd.Flags().StringVar(&configAddName, "name", "", "Config name (non-interactive)")
	configAddCmd.Flags().StringVar(&configAddHost, "host", "", "Database host (non-interactive)")
	configAddCmd.Flags().StringVar(&configAddPort, "port", "", "Database port (non-interactive)")
	configAddCmd.Flags().StringVar(&configAddUser, "user", "", "Database user (non-interactive)")
	configAddCmd.Flags().IntVar(&configAddRetentionDays, "retention-days", -1, "Dump retention days, 0=keep forever (non-interactive)")
	configAddCmd.Flags().BoolVar(&configAddSSH, "ssh", false, "Enable SSH tunnel (non-interactive)")
	configAddCmd.Flags().StringVar(&configAddSSHHost, "ssh-host", "", "SSH host (non-interactive)")
	configAddCmd.Flags().StringVar(&configAddSSHUser, "ssh-user", "", "SSH user (non-interactive)")
	configAddCmd.Flags().StringVar(&configAddSSHPort, "ssh-port", "", "SSH port (non-interactive)")
	configAddCmd.Flags().StringVar(&configAddIgnoredSchemas, "ignored-schemas", "", "Comma-separated schemas to ignore (non-interactive)")
	configAddCmd.Flags().StringVar(&configAddIgnoredTables, "ignored-tables", "", "Per-schema ignored tables in format schema1:tbl1,tbl2;schema2:tbl3 (non-interactive)")
	configAddCmd.Flags().BoolVar(&configAddNoLocks, "no-locks", false, "Pass --no-locks to mydumper (use when DB user lacks RELOAD privilege) (non-interactive)")
	configAddCmd.Flags().StringVar(&configAddPassword, "password", "", "DB password to save, encrypted with your master password (optional, non-interactive)")
	configAddCmd.Flags().BoolVar(&configAddSendTelegram, "send-telegram", true, "Send this config's dumps to Telegram when globally enabled")
	configCmd.AddCommand(configAddCmd)
}
