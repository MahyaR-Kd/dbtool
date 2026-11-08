package cmd

import (
	"bufio"
	"errors"
	"os"
	"os/exec"

	"dbtool/internal/dashboard"
	"dbtool/internal/ui"

	"github.com/spf13/cobra"
)

const execWaitCmd = "_exec-wait"

var uiCmd = &cobra.Command{
	Use:     "ui",
	Aliases: []string{"dashboard"},
	Short:   "Open the interactive dashboard (configs, jobs, settings, logs)",
	Long: "Open a full-screen dashboard to browse configs, scheduled jobs, settings and the log,\n" +
		"and run dump / restore / add / edit / delete from one place.",
	Args: cobra.NoArgs,
	Run: func(cmd *cobra.Command, args []string) {
		if !ui.IsInteractive() {
			ui.Fatal("The dashboard needs an interactive terminal")
		}
		self, err := os.Executable()
		if err != nil {
			ui.Fatal("Cannot determine the dbtool executable: %v", err)
		}
		err = dashboard.Run(dashboard.Options{
			Version: Version,
			Command: func(a ...string) *exec.Cmd {
				c := exec.Command(self, append([]string{execWaitCmd}, a...)...)
				c.Stdin, c.Stdout, c.Stderr = os.Stdin, os.Stdout, os.Stderr
				return c
			},
		})
		if err != nil {
			ui.Fatal("Dashboard error: %v", err)
		}
	},
}

// execWait runs a dbtool subcommand and then waits for Enter, so that when
// the dashboard launches a command the user can read its result before the
// dashboard redraws over it. A separate process (rather than calling the
// command in-process) also isolates the dashboard from the os.Exit calls
// commands make on failure.
var execWaitCommand = &cobra.Command{
	Use:                execWaitCmd,
	Hidden:             true,
	DisableFlagParsing: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		self, err := os.Executable()
		if err != nil {
			return err
		}
		ui.Blank()
		c := exec.Command(self, args...)
		c.Stdin, c.Stdout, c.Stderr = os.Stdin, os.Stdout, os.Stderr
		runErr := c.Run()
		ui.Blank()
		ui.Println(ui.Muted("Press Enter to return to the dashboard…"))
		_, _ = bufio.NewReader(os.Stdin).ReadString('\n')
		var exitErr *exec.ExitError
		if errors.As(runErr, &exitErr) {
			os.Exit(exitErr.ExitCode())
		}
		return runErr
	},
}

func init() {
	if installedInPath {
		rootCmd.AddCommand(uiCmd)
	}
	rootCmd.AddCommand(execWaitCommand)
}
