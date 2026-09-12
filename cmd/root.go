package cmd

import (
	"os"
	"os/exec"
	"path/filepath"

	"github.com/spf13/cobra"
)

var rootCmd = &cobra.Command{
	Use:   "dbtool",
	Short: "Database backup & restore CLI",
	Long:  "A powerful CLI for MySQL dump/restore with SSH tunneling support",
}

func Execute() {
	cobra.CheckErr(rootCmd.Execute())
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
}
