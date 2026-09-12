package cmd

import (
	"fmt"
	"strings"

	"dbtool/internal/config"
	"dbtool/internal/types"
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
			cmd.Flags().Changed("no-locks")

		var c types.Config
		if hasNonInteractiveInput {
			if configAddName == "" || configAddHost == "" || configAddPort == "" || configAddUser == "" {
				fmt.Println("For non-interactive mode, --name, --host, --port, and --user are required.")
				return
			}
			if configAddSSH && (configAddSSHHost == "" || configAddSSHUser == "" || configAddSSHPort == "") {
				fmt.Println("When --ssh is set, --ssh-host, --ssh-user, and --ssh-port are required.")
				return
			}
			if !configAddSSH && (configAddSSHHost != "" || configAddSSHUser != "" || configAddSSHPort != "") {
				fmt.Println("--ssh-host, --ssh-user, and --ssh-port can only be used with --ssh.")
				return
			}

			c = types.Config{
				Name:           configAddName,
				Host:           configAddHost,
				Port:           configAddPort,
				User:           configAddUser,
				SSH:            configAddSSH,
				SSHHost:        configAddSSHHost,
				SSHUser:        configAddSSHUser,
				SSHPort:        configAddSSHPort,
				IgnoredSchemas: parseCommaSeparated(configAddIgnoredSchemas),
				IgnoredTables:  parseIgnoredTablesFlag(configAddIgnoredTables),
				NoLocks:        configAddNoLocks,
				Password:       configAddPassword,
			}
			if configAddRetentionDays >= 0 {
				c.RetentionDays = configAddRetentionDays
			}
		} else {
			c = config.AskInteractive()
		}

		config.Save(c)
		fmt.Println("Saved")
	},
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
	configCmd.AddCommand(configAddCmd)
}
