package cmd

import (
	"fmt"
	"os"

	"dbtool/internal/db"
	"github.com/spf13/cobra"
)

var validateDir string

var validateCmd = &cobra.Command{
	Use:   "validate",
	Short: "Check a dump directory for columns silently dropped from INSERT statements",
	Long: `Validate re-runs the same safety-net check that dump and restore already run
automatically: for each table in the dump directory, it compares the
schema's column list against its data file's INSERT statement, and reports
any column that's in the schema but missing from the INSERT (excluding true
GENERATED columns, which mydumper legitimately omits).

This is useful for checking a dump you didn't just take yourself — one
downloaded from S3, or one sitting on disk from a while ago — without
having to run a full restore first. Unlike the automatic check during
dump/restore (which only warns), this command exits non-zero when it finds
an issue, so it can be used as a gate in scripts.

Example:
  dbtool validate --dir /root/dbtool-backups/mydb_2026-09-10_132320`,
	Run: func(cmd *cobra.Command, args []string) {
		if validateDir == "" {
			fmt.Println("--dir is required.")
			os.Exit(1)
		}

		info, err := os.Stat(validateDir)
		if err != nil {
			fmt.Printf("Cannot access %s: %v\n", validateDir, err)
			os.Exit(1)
		}
		if !info.IsDir() {
			fmt.Printf("Not a directory: %s\n", validateDir)
			os.Exit(1)
		}

		issues := db.ValidateDump(validateDir)
		if len(issues) == 0 {
			fmt.Println("No issues found — every table's INSERT statements include all expected columns.")
			return
		}

		db.ReportValidationIssues(issues)
		os.Exit(1)
	},
}

func init() {
	validateCmd.Flags().StringVar(&validateDir, "dir", "", "Dump directory to check (required)")
	if installedInPath {
		rootCmd.AddCommand(validateCmd)
	}
}
