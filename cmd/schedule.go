package cmd

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"dbtool/internal/logger"
	"github.com/spf13/cobra"
)

const schedulerTickCmd = "_scheduler_tick"

var scheduleCmd = &cobra.Command{
	Use:   "schedule",
	Short: "Enable or disable background scheduling via crontab",
}

var scheduleEnableCmd = &cobra.Command{
	Use:   "enable",
	Short: "Install a crontab entry to run jobs every minute in the background",
	RunE: func(cmd *cobra.Command, args []string) error {
		bin, err := os.Executable()
		if err != nil {
			return fmt.Errorf("cannot determine executable path: %w", err)
		}

		entry := fmt.Sprintf("* * * * * %s %s", bin, schedulerTickCmd)

		current, _ := readCrontab()
		if strings.Contains(current, entry) {
			fmt.Println("Scheduler is already enabled.")
			return nil
		}

		// Remove any stale dbtool scheduler line first
		cleaned := removeDbtoolLine(current)
		newCrontab := cleaned
		if newCrontab != "" && !strings.HasSuffix(newCrontab, "\n") {
			newCrontab += "\n"
		}
		newCrontab += entry + "\n"

		if err := writeCrontab(newCrontab); err != nil {
			logger.Error("failed to update crontab for scheduler enable: %v", err)
			return fmt.Errorf("failed to update crontab: %w", err)
		}

		logger.Info("scheduler enabled (cron entry: %s)", entry)
		fmt.Println("Scheduler enabled. Jobs will run every minute in the background.")
		return nil
	},
}

var scheduleDisableCmd = &cobra.Command{
	Use:   "disable",
	Short: "Remove the dbtool crontab entry to stop background scheduling",
	RunE: func(cmd *cobra.Command, args []string) error {
		current, err := readCrontab()
		if err != nil {
			fmt.Println("No crontab found; scheduler is already disabled.")
			return nil
		}

		cleaned := removeDbtoolLine(current)
		if cleaned == current {
			fmt.Println("Scheduler is already disabled.")
			return nil
		}

		if err := writeCrontab(cleaned); err != nil {
			logger.Error("failed to update crontab for scheduler disable: %v", err)
			return fmt.Errorf("failed to update crontab: %w", err)
		}

		logger.Info("scheduler disabled")
		fmt.Println("Scheduler disabled.")
		return nil
	},
}

// readCrontab returns the current crontab contents for the running user.
func readCrontab() (string, error) {
	out, err := exec.Command("crontab", "-l").Output()
	if err != nil {
		// crontab -l exits non-zero when no crontab exists — treat as empty
		return "", nil
	}
	return string(out), nil
}

// writeCrontab replaces the user crontab with content.
func writeCrontab(content string) error {
	cmd := exec.Command("crontab", "-")
	cmd.Stdin = bytes.NewBufferString(content)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// removeDbtoolLine strips any line that contains the dbtool scheduler marker.
func removeDbtoolLine(crontab string) string {
	var lines []string
	for _, line := range strings.Split(crontab, "\n") {
		if strings.Contains(line, schedulerTickCmd) {
			continue
		}
		lines = append(lines, line)
	}
	result := strings.Join(lines, "\n")
	// Avoid trailing blank lines accumulating over multiple enable/disable cycles
	result = strings.TrimRight(result, "\n")
	if result != "" {
		result += "\n"
	}
	return result
}

func init() {
	scheduleCmd.AddCommand(scheduleEnableCmd)
	scheduleCmd.AddCommand(scheduleDisableCmd)
	if installedInPath {
		rootCmd.AddCommand(scheduleCmd)
	}
}
