package ui

import (
	"fmt"
	"strings"
)

// Status messages. Each prints one line: a colored icon followed by the
// formatted text. Multi-line text is indented so continuation lines align
// under the first line's text rather than under the icon.

func statusLine(icon, text string) string {
	lines := strings.Split(strings.TrimRight(text, "\n"), "\n")
	for i := 1; i < len(lines); i++ {
		lines[i] = "  " + lines[i]
	}
	return ensureNewline(icon + " " + strings.Join(lines, "\n"))
}

// Success prints "✔ text" in the success color.
func Success(format string, a ...any) {
	write(Stdout, statusLine(fg(ColorSuccess).Bold(true).Render(Icon(IconOK)), fmt.Sprintf(format, a...)))
}

// Info prints "ℹ text".
func Info(format string, a ...any) {
	write(Stdout, statusLine(fg(ColorInfo).Bold(true).Render(Icon(IconInfo)), fmt.Sprintf(format, a...)))
}

// Step prints "➜ text" — announces an action that is about to happen.
func Step(format string, a ...any) {
	write(Stdout, statusLine(fg(ColorAccent).Bold(true).Render(Icon(IconArrow)), fmt.Sprintf(format, a...)))
}

// Warn prints "⚠ text" to stderr.
func Warn(format string, a ...any) {
	write(Stderr, statusLine(fg(ColorWarn).Bold(true).Render(Icon(IconWarn)), fmt.Sprintf(format, a...)))
}

// Error prints "✖ text" to stderr.
func Error(format string, a ...any) {
	write(Stderr, statusLine(fg(ColorError).Bold(true).Render(Icon(IconFail)), fmt.Sprintf(format, a...)))
}

// Fatal prints an error and exits with status 1.
func Fatal(format string, a ...any) {
	StopLive()
	Error(format, a...)
	exit(1)
}

// Hint prints a dimmed, indented tip line: "  › text".
func Hint(format string, a ...any) {
	write(Stdout, Muted("  "+Icon(IconHint)+" "+fmt.Sprintf(format, a...))+"\n")
}

// Detail prints a dimmed, indented secondary line: "  • text".
func Detail(format string, a ...any) {
	write(Stdout, "  "+Muted(Icon(IconBullet))+" "+fmt.Sprintf(format, a...)+"\n")
}

// Println prints its operands like fmt.Println, keeping any live line intact.
func Println(a ...any) { write(Stdout, fmt.Sprintln(a...)) }

// Printf prints like fmt.Printf, keeping any live line intact.
func Printf(format string, a ...any) { write(Stdout, fmt.Sprintf(format, a...)) }

// Print prints like fmt.Print, keeping any live line intact.
func Print(a ...any) { write(Stdout, fmt.Sprint(a...)) }

// Blank prints an empty line.
func Blank() { write(Stdout, "\n") }

// Empty prints a friendly "nothing here" message with an optional hint on
// how to create the first item, e.g.
//
//	○ No configs yet
//	  › Create one with dbtool config add
func Empty(what, hint string) {
	write(Stdout, Muted(Icon(IconPending))+" "+what+"\n")
	if hint != "" {
		Hint("%s", hint)
	}
}
