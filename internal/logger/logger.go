package logger

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"dbtool/internal/paths"
)

// Level represents the severity of a log entry.
type Level int

const (
	LevelDebug Level = iota
	LevelInfo
	LevelWarn
	LevelError
)

var levelNames = map[Level]string{
	LevelDebug: "DEBUG",
	LevelInfo:  "INFO",
	LevelWarn:  "WARN",
	LevelError: "ERROR",
}

var levelOrder = map[string]Level{
	"DEBUG": LevelDebug,
	"INFO":  LevelInfo,
	"WARN":  LevelWarn,
	"ERROR": LevelError,
}

// ParseLevel converts a string (case-insensitive) to a Level.
// Returns (LevelInfo, false) when the string is not recognized.
func ParseLevel(s string) (Level, bool) {
	lvl, ok := levelOrder[strings.ToUpper(s)]
	return lvl, ok
}

// LevelName returns the name tag for a level (e.g. "INFO").
func LevelName(l Level) string {
	return levelNames[l]
}

func logFilePath() string {
	dir, err := paths.DbtoolDir()
	if err != nil {
		return filepath.Join(os.ExpandEnv("$HOME"), ".dbtool", "dbtool.log")
	}
	return filepath.Join(dir, "dbtool.log")
}

// LogFile returns the path to the log file.
func LogFile() string {
	return logFilePath()
}

func write(level Level, msg string) {
	f, err := os.OpenFile(logFilePath(), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		fmt.Fprintf(os.Stderr, "dbtool: cannot open log file %s: %v\n", logFilePath(), err)
		return
	}
	defer f.Close()

	line := fmt.Sprintf("%s [%s] %s\n",
		time.Now().Format("2006-01-02 15:04:05"),
		levelNames[level],
		msg,
	)
	_, _ = f.WriteString(line)
}

// Debug logs a debug-level message.
func Debug(format string, args ...any) {
	write(LevelDebug, fmt.Sprintf(format, args...))
}

// Info logs an info-level message.
func Info(format string, args ...any) {
	write(LevelInfo, fmt.Sprintf(format, args...))
}

// Warn logs a warning-level message.
func Warn(format string, args ...any) {
	write(LevelWarn, fmt.Sprintf(format, args...))
}

// Error logs an error-level message.
func Error(format string, args ...any) {
	write(LevelError, fmt.Sprintf(format, args...))
}
