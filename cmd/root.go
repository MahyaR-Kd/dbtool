package cmd

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"dbtool/internal/ui"

	"github.com/spf13/cobra"
)

var noColorFlag bool

var rootCmd = &cobra.Command{
	Use:   "dbtool",
	Short: "Database backup & restore CLI",
	Long:  "MySQL dump & restore with SSH tunnels, scheduled jobs, S3 and Telegram delivery.",
	// Bare "dbtool" shows a welcome screen with status and the most common
	// commands instead of the full help.
	Args: cobra.NoArgs,
	Run: func(cmd *cobra.Command, args []string) {
		printWelcome()
	},
}

func Execute() {
	// --no-color must take effect before cobra renders anything (help and
	// usage errors are printed without running any PreRun hook), so look
	// for it directly in the raw arguments as well.
	for _, a := range os.Args[1:] {
		if a == "--" {
			break
		}
		if a == "--no-color" || a == "--no-color=true" {
			ui.SetNoColor(true)
		}
	}
	assignCommandGroups()
	if err := rootCmd.Execute(); err != nil {
		ui.Error("%v", err)
		if strings.Contains(err.Error(), "unknown command") || strings.Contains(err.Error(), "unknown flag") {
			ui.Hint("Run %s to see all commands", ui.Code("dbtool --help"))
		}
		os.Exit(1)
	}
}

// installedInPath is true when the "dbtool" binary found in PATH resolves to
// the same file as the currently running executable.  This guards against the
// case where a different "dbtool" binary happens to exist in PATH while the
// user is running a freshly-built copy that has not yet been installed.
var installedInPath = func() bool {
	found, err := exec.LookPath("dbtool")
	if err != nil {
		return false
	}
	// Resolve symlinks on both sides before comparing.
	foundResolved, err := filepath.EvalSymlinks(found)
	if err != nil {
		return false
	}
	self, err := os.Executable()
	if err != nil {
		return false
	}
	selfResolved, err := filepath.EvalSymlinks(self)
	if err != nil {
		return false
	}
	return foundResolved == selfResolved
}()

func init() {
	rootCmd.CompletionOptions.DisableDefaultCmd = true
	rootCmd.Version = Version
	rootCmd.SetVersionTemplate("dbtool {{.Version}}\n")
	// Errors are printed by Execute in the ui style; cobra's own "Error:"
	// line and the full usage dump on every mistake are just noise.
	rootCmd.SilenceErrors = true
	rootCmd.SilenceUsage = true
	rootCmd.PersistentFlags().BoolVar(&noColorFlag, "no-color", false, "Disable colors (also honored: NO_COLOR env var)")
	rootCmd.PersistentPreRun = func(cmd *cobra.Command, args []string) {
		if noColorFlag {
			ui.SetNoColor(true)
		}
	}
	setupHelp(rootCmd)
}
