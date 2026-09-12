package cmd

import (
	"fmt"

	"dbtool/internal/config"
	"dbtool/internal/interactivelist"

	"github.com/spf13/cobra"
)

var configEditName string
var configEditNewName string
var configEditHost string
var configEditPort string
var configEditUser string
var configEditRetentionDays int
var configEditSSH bool
var configEditSSHHost string
var configEditSSHUser string
var configEditSSHPort string
var configEditIgnoredSchemas string
var configEditIgnoredTables string
var configEditNoLocks bool
var configEditPassword string

var configEditCmd = &cobra.Command{
	Use:   "edit",
	Short: "Edit an existing database connection config",
	Run: func(cmd *cobra.Command, args []string) {
		configs := config.Load()

		if len(configs) == 0 {
			fmt.Println("No configs found")
			return
		}

		if configEditName != "" {
			found := -1
			for i := range configs {
				if configs[i].Name == configEditName {
					found = i
					break
				}
			}
			if found == -1 {
				fmt.Println("Config not found:", configEditName)
				return
			}

			updated := configs[found]
			if configEditNewName != "" {
				updated.Name = configEditNewName
			}
			if cmd.Flags().Changed("host") {
				updated.Host = configEditHost
			}
			if cmd.Flags().Changed("port") {
				updated.Port = configEditPort
			}
			if cmd.Flags().Changed("user") {
				updated.User = configEditUser
			}
			if configEditRetentionDays >= 0 {
				updated.RetentionDays = configEditRetentionDays
			}
			if cmd.Flags().Changed("ssh") {
				updated.SSH = configEditSSH
				if !configEditSSH {
					updated.SSHHost = ""
					updated.SSHUser = ""
					updated.SSHPort = ""
				}
			}
			if cmd.Flags().Changed("ssh-host") {
				updated.SSHHost = configEditSSHHost
			}
			if cmd.Flags().Changed("ssh-user") {
				updated.SSHUser = configEditSSHUser
			}
			if cmd.Flags().Changed("ssh-port") {
				updated.SSHPort = configEditSSHPort
			}
			if cmd.Flags().Changed("ignored-schemas") {
				updated.IgnoredSchemas = parseCommaSeparated(configEditIgnoredSchemas)
			}
			if cmd.Flags().Changed("ignored-tables") {
				updated.IgnoredTables = parseIgnoredTablesFlag(configEditIgnoredTables)
			}
			if cmd.Flags().Changed("no-locks") {
				updated.NoLocks = configEditNoLocks
			}
			if cmd.Flags().Changed("password") {
				updated.Password = configEditPassword
			}

			if updated.SSH && (updated.SSHHost == "" || updated.SSHUser == "" || updated.SSHPort == "") {
				fmt.Println("SSH configs require SSH host, user, and port.")
				return
			}
			configs[found] = updated
			config.Overwrite(configs)
			fmt.Println("Updated successfully")
			return
		}

		options := make([]string, len(configs))
		for i, c := range configs {
			options[i] = fmt.Sprintf("%s (%s:%s)", c.Name, c.Host, c.Port)
		}

		idx, err := interactivelist.SelectOne("Select config to edit", options)
		if err != nil {
			fmt.Println("Invalid selection")
			return
		}

		updated := config.EditInteractive(configs[idx])
		configs[idx] = updated
		config.Overwrite(configs)

		fmt.Println("Updated successfully")
	},
}

func init() {
	configEditCmd.Flags().StringVar(&configEditName, "name", "", "Config name to edit (non-interactive)")
	configEditCmd.Flags().StringVar(&configEditNewName, "new-name", "", "New config name (non-interactive)")
	configEditCmd.Flags().StringVar(&configEditHost, "host", "", "New database host (non-interactive)")
	configEditCmd.Flags().StringVar(&configEditPort, "port", "", "New database port (non-interactive)")
	configEditCmd.Flags().StringVar(&configEditUser, "user", "", "New database user (non-interactive)")
	configEditCmd.Flags().IntVar(&configEditRetentionDays, "retention-days", -1, "New dump retention days (non-interactive)")
	configEditCmd.Flags().BoolVar(&configEditSSH, "ssh", false, "Enable/disable SSH (non-interactive)")
	configEditCmd.Flags().StringVar(&configEditSSHHost, "ssh-host", "", "New SSH host (non-interactive)")
	configEditCmd.Flags().StringVar(&configEditSSHUser, "ssh-user", "", "New SSH user (non-interactive)")
	configEditCmd.Flags().StringVar(&configEditSSHPort, "ssh-port", "", "New SSH port (non-interactive)")
	configEditCmd.Flags().StringVar(&configEditIgnoredSchemas, "ignored-schemas", "", "New comma-separated ignored schemas, empty clears (non-interactive)")
	configEditCmd.Flags().StringVar(&configEditIgnoredTables, "ignored-tables", "", "Per-schema ignored tables in format schema1:tbl1,tbl2;schema2:tbl3, empty clears (non-interactive)")
	configEditCmd.Flags().BoolVar(&configEditNoLocks, "no-locks", false, "Enable/disable --no-locks for mydumper (non-interactive)")
	configEditCmd.Flags().StringVar(&configEditPassword, "password", "", "New DB password to save, encrypted with your master password; empty clears it (non-interactive)")
	configCmd.AddCommand(configEditCmd)
}
