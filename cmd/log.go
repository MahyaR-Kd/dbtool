package cmd

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"dbtool/internal/logger"

	"github.com/spf13/cobra"
)

var logFollowFlag bool
var logLevelFlag string

var logCmd = &cobra.Command{
	Use:   "log",
	Short: "Show dbtool log entries",
	Long: `Show entries from the dbtool log file (~/.dbtool/dbtool.log).

Examples:
  dbtool log                      # print all log entries
  dbtool log --level info         # show only INFO entries and above
  dbtool log -f                   # follow log output (like tail -f)
  dbtool log -f --level warn      # follow, showing WARN and above`,
	RunE: func(cmd *cobra.Command, args []string) error {
		var minLevel *logger.Level
		if logLevelFlag != "" {
			lvl, ok := logger.ParseLevel(logLevelFlag)
			if !ok {
				return fmt.Errorf("unknown level %q; valid values: debug, info, warn, error", logLevelFlag)
			}
			minLevel = &lvl
		}

		path := logger.LogFile()

		if err := printLog(path, minLevel); err != nil {
			return err
		}

		if logFollowFlag {
			return followLog(path, minLevel)
		}

		return nil
	},
}

// levelOfLine extracts the log level from a formatted log line.
// Expected format: "2006-01-02 15:04:05 [LEVEL] message"
// Returns LevelDebug and false if the line cannot be parsed.
func levelOfLine(line string) (logger.Level, bool) {
	// Find the opening bracket after the timestamp (19 chars + space = index 20)
	open := strings.Index(line, " [")
	if open < 0 {
		return logger.LevelDebug, false
	}
	rest := line[open+2:]
	close := strings.Index(rest, "]")
	if close < 0 {
		return logger.LevelDebug, false
	}
	tag := rest[:close]
	lvl, ok := logger.ParseLevel(tag)
	return lvl, ok
}

// shouldShow returns true when the line's level is at or above minLevel.
func shouldShow(line string, minLevel *logger.Level) bool {
	if minLevel == nil {
		return true
	}
	lvl, ok := levelOfLine(line)
	if !ok {
		return true // show unparseable lines by default
	}
	return lvl >= *minLevel
}

// printLog prints existing log file content filtered by minLevel.
func printLog(path string, minLevel *logger.Level) error {
	f, err := os.Open(path)
	if os.IsNotExist(err) {
		fmt.Println("No log file found. Run some dbtool commands to generate logs.")
		return nil
	}
	if err != nil {
		return fmt.Errorf("cannot open log file: %w", err)
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		if shouldShow(line, minLevel) {
			fmt.Println(line)
		}
	}
	return scanner.Err()
}

// followLog tails the log file, printing new lines as they are appended.
// It blocks until interrupted (Ctrl+C).
func followLog(path string, minLevel *logger.Level) error {
	f, err := openOrWait(path)
	if err != nil {
		return err
	}
	defer f.Close()

	// Seek to end so we only show new entries.
	if _, err := f.Seek(0, io.SeekEnd); err != nil {
		return fmt.Errorf("cannot seek log file: %w", err)
	}

	fmt.Println("--- following log (Ctrl+C to stop) ---")

	reader := bufio.NewReader(f)
	for {
		line, err := reader.ReadString('\n')
		if len(line) > 0 {
			trimmed := strings.TrimRight(line, "\n")
			if shouldShow(trimmed, minLevel) {
				fmt.Println(trimmed)
			}
		}
		if err != nil {
			if err == io.EOF {
				time.Sleep(300 * time.Millisecond)
				continue
			}
			return fmt.Errorf("error reading log file: %w", err)
		}
	}
}

// openOrWait opens the log file, waiting up to ~10 s if it does not yet exist.
func openOrWait(path string) (*os.File, error) {
	for i := 0; i < 20; i++ {
		f, err := os.Open(path)
		if err == nil {
			return f, nil
		}
		if !os.IsNotExist(err) {
			return nil, fmt.Errorf("cannot open log file: %w", err)
		}
		if i == 0 {
			fmt.Println("Waiting for log file to appear...")
		}
		time.Sleep(500 * time.Millisecond)
	}
	return nil, fmt.Errorf("log file %s not found", path)
}

func init() {
	logCmd.Flags().BoolVarP(&logFollowFlag, "follow", "f", false, "Follow log output in real time (like tail -f)")
	logCmd.Flags().StringVar(&logLevelFlag, "level", "", "Minimum log level to show (debug, info, warn, error)")
	if installedInPath {
		rootCmd.AddCommand(logCmd)
	}
}
