// Package ui is dbtool's single source of truth for how terminal output
// looks: the color palette, the icon set, status messages (success / warn /
// error / info), layout helpers (titles, sections, key/value lists, tables,
// summary boxes) and the live elements (progress bars and stage spinners).
//
// Every command prints through this package instead of calling fmt directly,
// so the whole tool shares one visual vocabulary and one set of rules for
// when styling is turned off:
//
//   - colors are disabled when stdout is not a terminal (pipes, cron, CI),
//     when the NO_COLOR environment variable is set (https://no-color.org),
//     or when --no-color is passed;
//   - DBTOOL_COLOR=always forces colors on even when piped;
//   - icons fall back to plain ASCII when TERM=dumb or DBTOOL_ASCII=1.
//
// Errors and warnings go to stderr; everything else goes to stdout.
package ui

import (
	"io"
	"os"
	"strings"
	"sync"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
	"golang.org/x/term"
)

// Stdout and Stderr are the writers every ui function prints to. They are
// variables so tests can capture output.
var (
	Stdout io.Writer = os.Stdout
	Stderr io.Writer = os.Stderr
)

// exit is os.Exit, swappable in tests.
var (
	osExit = os.Exit
	exit   = osExit
)

var (
	cfgMu        sync.RWMutex
	forceNoColor bool
	asciiIcons   = detectASCII()
	renderer     = lipgloss.NewRenderer(os.Stdout)
)

func init() {
	applyColorProfile()
}

// SetNoColor turns colors off (true) or back to automatic detection (false).
// Wired to the global --no-color flag.
func SetNoColor(off bool) {
	cfgMu.Lock()
	forceNoColor = off
	cfgMu.Unlock()
	applyColorProfile()
}

// SetASCII forces the plain-ASCII icon set on or off.
func SetASCII(on bool) {
	cfgMu.Lock()
	asciiIcons = on
	cfgMu.Unlock()
}

// ColorEnabled reports whether ANSI styling is currently emitted.
func ColorEnabled() bool {
	cfgMu.RLock()
	off := forceNoColor
	cfgMu.RUnlock()
	if off {
		return false
	}
	if _, ok := os.LookupEnv("NO_COLOR"); ok {
		return false
	}
	switch strings.ToLower(os.Getenv("DBTOOL_COLOR")) {
	case "always", "1", "true", "yes":
		return true
	case "never", "0", "false", "no":
		return false
	}
	return IsTerminal()
}

// IsTerminal reports whether stdout is an interactive terminal. Live
// elements (spinners, progress bars) only animate when it is.
func IsTerminal() bool {
	if f, ok := Stdout.(*os.File); ok {
		return term.IsTerminal(int(f.Fd()))
	}
	return false
}

// IsInteractive reports whether both stdin and stdout are terminals — the
// condition for showing interactive forms and pickers.
func IsInteractive() bool {
	return term.IsTerminal(int(os.Stdin.Fd())) && term.IsTerminal(int(os.Stdout.Fd()))
}

// Width returns the terminal width, or 80 when it cannot be determined.
func Width() int {
	if f, ok := Stdout.(*os.File); ok {
		if w, _, err := term.GetSize(int(f.Fd())); err == nil && w > 0 {
			return w
		}
	}
	return 80
}

func applyColorProfile() {
	if ColorEnabled() {
		renderer.SetColorProfile(termenv.EnvColorProfile())
		if renderer.ColorProfile() == termenv.Ascii {
			// DBTOOL_COLOR=always with a pipe: termenv would still detect
			// "no colors", so pick a sane default explicitly.
			renderer.SetColorProfile(termenv.ANSI256)
		}
	} else {
		renderer.SetColorProfile(termenv.Ascii)
	}
	// Decide light vs dark ourselves instead of letting lipgloss ask the
	// terminal (an OSC 11 query that blocks until the terminal answers —
	// some terminals, multiplexers and CI shells never do).
	dark := darkBackground()
	renderer.SetHasDarkBackground(dark)
	lipgloss.SetHasDarkBackground(dark)
	// Keep lipgloss's default renderer (used by the huh form theme and the
	// dashboard) in sync, so --no-color applies there too.
	lipgloss.SetColorProfile(renderer.ColorProfile())
}

// darkBackground reports whether to use the dark-background palette:
// DBTOOL_THEME=light|dark wins, then the COLORFGBG convention ("fg;bg",
// set by many terminals), and otherwise dark — the common default.
func darkBackground() bool {
	switch strings.ToLower(os.Getenv("DBTOOL_THEME")) {
	case "light":
		return false
	case "dark":
		return true
	}
	if v := os.Getenv("COLORFGBG"); v != "" {
		parts := strings.Split(v, ";")
		switch parts[len(parts)-1] {
		case "7", "15":
			return false
		}
	}
	return true
}

func detectASCII() bool {
	if v := strings.ToLower(os.Getenv("DBTOOL_ASCII")); v == "1" || v == "true" || v == "yes" {
		return true
	}
	return os.Getenv("TERM") == "dumb"
}

// Renderer exposes the lipgloss renderer the package styles with, so other
// UI code (forms, the dashboard) can build styles that honor the same
// color rules.
func Renderer() *lipgloss.Renderer { return renderer }

// ---------------------------------------------------------------- palette

// Palette colors. Adaptive colors pick a shade that reads well on both light
// and dark terminal backgrounds.
var (
	ColorAccent  = lipgloss.AdaptiveColor{Light: "#5B4BDB", Dark: "#9D8CFF"}
	ColorSuccess = lipgloss.AdaptiveColor{Light: "#1A7F37", Dark: "#3FB950"}
	ColorWarn    = lipgloss.AdaptiveColor{Light: "#9A6700", Dark: "#D29922"}
	ColorError   = lipgloss.AdaptiveColor{Light: "#CF222E", Dark: "#F85149"}
	ColorInfo    = lipgloss.AdaptiveColor{Light: "#0969DA", Dark: "#58A6FF"}
	ColorMuted   = lipgloss.AdaptiveColor{Light: "#6E7781", Dark: "#8B949E"}
	ColorText    = lipgloss.AdaptiveColor{Light: "#1F2328", Dark: "#E6EDF3"}
	ColorSync    = lipgloss.AdaptiveColor{Light: "#8250DF", Dark: "#BC8CFF"}
)

func fg(c lipgloss.TerminalColor) lipgloss.Style { return renderer.NewStyle().Foreground(c) }

// Inline styling helpers. Each returns s unchanged when colors are off.

// Bold renders s in bold.
func Bold(s string) string { return renderer.NewStyle().Bold(true).Render(s) }

// Muted renders s dimmed — for secondary details, durations, hints.
func Muted(s string) string { return fg(ColorMuted).Render(s) }

// Accent renders s in the brand accent color.
func Accent(s string) string { return fg(ColorAccent).Bold(true).Render(s) }

// Green renders s in the success color.
func Green(s string) string { return fg(ColorSuccess).Render(s) }

// Yellow renders s in the warning color.
func Yellow(s string) string { return fg(ColorWarn).Render(s) }

// Red renders s in the error color.
func Red(s string) string { return fg(ColorError).Render(s) }

// Blue renders s in the info color.
func Blue(s string) string { return fg(ColorInfo).Render(s) }

// Code renders a command or path the user may want to copy.
func Code(s string) string { return fg(ColorInfo).Render(s) }

// ---------------------------------------------------------------- icons

// Icon names used across dbtool.
const (
	IconOK      = "ok"
	IconFail    = "fail"
	IconWarn    = "warn"
	IconInfo    = "info"
	IconArrow   = "arrow"
	IconBullet  = "bullet"
	IconPending = "pending"
	IconBrand   = "brand"
	IconHint    = "hint"
	IconKey     = "key"
	IconQuest   = "question"
	IconOn      = "on"
	IconOff     = "off"
)

var unicodeIcons = map[string]string{
	IconOK:      "✔",
	IconFail:    "✖",
	IconWarn:    "⚠",
	IconInfo:    "ℹ",
	IconArrow:   "➜",
	IconBullet:  "•",
	IconPending: "○",
	IconBrand:   "◆",
	IconHint:    "›",
	IconKey:     "🔑",
	IconQuest:   "?",
	IconOn:      "●",
	IconOff:     "○",
}

var asciiIconSet = map[string]string{
	IconOK:      "[ok]",
	IconFail:    "[x]",
	IconWarn:    "[!]",
	IconInfo:    "[i]",
	IconArrow:   "->",
	IconBullet:  "*",
	IconPending: "o",
	IconBrand:   "*",
	IconHint:    ">",
	IconKey:     "[key]",
	IconQuest:   "?",
	IconOn:      "[on]",
	IconOff:     "[off]",
}

// Icon returns the glyph for name in the active icon set.
func Icon(name string) string {
	cfgMu.RLock()
	ascii := asciiIcons
	cfgMu.RUnlock()
	if ascii {
		return asciiIconSet[name]
	}
	return unicodeIcons[name]
}

// ASCII reports whether the plain-ASCII icon set is active.
func ASCII() bool {
	cfgMu.RLock()
	defer cfgMu.RUnlock()
	return asciiIcons
}

var spinnerFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}
var asciiSpinnerFrames = []string{"|", "/", "-", "\\"}

func spinnerFrame(i int) string {
	if ASCII() {
		return asciiSpinnerFrames[i%len(asciiSpinnerFrames)]
	}
	return spinnerFrames[i%len(spinnerFrames)]
}
