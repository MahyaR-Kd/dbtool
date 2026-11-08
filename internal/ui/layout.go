package ui

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/lipgloss/table"
	"github.com/charmbracelet/x/ansi"
)

// Title prints a command header: "◆ title  subtitle", followed by a blank
// line. Use it once at the top of a long-running or multi-step command.
func Title(title, subtitle string) {
	line := Accent(Icon(IconBrand)) + " " + Bold(title)
	if subtitle != "" {
		line += "  " + Muted(subtitle)
	}
	write(Stdout, line+"\n\n")
}

// Section prints a divider with a heading, used to group related prompts or
// output:  "── Connection ───────────".
func Section(title string) {
	width := Width()
	if width > 60 {
		width = 60
	}
	dash := "─"
	if ASCII() {
		dash = "-"
	}
	head := dash + dash + " " + title + " "
	rest := width - ansi.StringWidth(head)
	if rest < 3 {
		rest = 3
	}
	write(Stdout, "\n"+Muted(dash+dash)+" "+Accent(title)+" "+Muted(strings.Repeat(dash, rest))+"\n")
}

// KV is one key/value row for KeyValues.
type KV struct {
	Key   string
	Value string
}

// KeyValues renders aligned "key  value" rows, keys dimmed, indented by two
// spaces. Rows with an empty Value are skipped.
func KeyValues(rows ...KV) string {
	w := 0
	for _, r := range rows {
		if r.Value == "" {
			continue
		}
		if l := ansi.StringWidth(r.Key); l > w {
			w = l
		}
	}
	var b strings.Builder
	for _, r := range rows {
		if r.Value == "" {
			continue
		}
		pad := strings.Repeat(" ", w-ansi.StringWidth(r.Key))
		fmt.Fprintf(&b, "  %s%s  %s\n", Muted(r.Key), pad, r.Value)
	}
	return b.String()
}

// PrintKeyValues prints KeyValues(rows...).
func PrintKeyValues(rows ...KV) { write(Stdout, KeyValues(rows...)) }

// Badge kinds.
const (
	BadgeNeutral = iota
	BadgeAccent
	BadgeSuccess
	BadgeWarn
	BadgeError
	BadgeInfo
	BadgeSync
)

// Badge renders a short label (a job type, a state) in a kind-specific
// color, bold, e.g. "dump" in blue.
func Badge(text string, kind int) string {
	var c lipgloss.TerminalColor = ColorMuted
	switch kind {
	case BadgeAccent:
		c = ColorAccent
	case BadgeSuccess:
		c = ColorSuccess
	case BadgeWarn:
		c = ColorWarn
	case BadgeError:
		c = ColorError
	case BadgeInfo:
		c = ColorInfo
	case BadgeSync:
		c = ColorSync
	}
	return fg(c).Bold(true).Render(text)
}

// OnOff renders a boolean state as "● on" / "○ off" (green / dimmed).
func OnOff(on bool) string {
	if on {
		return Green(Icon(IconOn) + " on")
	}
	return Muted(Icon(IconOff) + " off")
}

// Status renders an enabled/configured state with a custom label:
// "● label" when on, "○ label" dimmed when off.
func Status(on bool, label string) string {
	if on {
		return Green(Icon(IconOn)) + " " + label
	}
	return Muted(Icon(IconOff) + " " + label)
}

// Table renders rows under a bold header with a single rule beneath it — no
// heavy grid, so it stays readable when copied out of the terminal.
func Table(headers []string, rows [][]string) string {
	border := lipgloss.NormalBorder()
	if ASCII() {
		border = lipgloss.ASCIIBorder()
	}
	t := table.New().
		Border(border).
		BorderTop(false).BorderBottom(false).BorderLeft(false).BorderRight(false).
		BorderColumn(false).BorderRow(false).BorderHeader(true).
		BorderStyle(fg(ColorMuted)).
		Headers(headers...).
		Rows(rows...).
		StyleFunc(func(row, col int) lipgloss.Style {
			s := renderer.NewStyle().PaddingRight(3)
			if col == 0 {
				s = s.PaddingLeft(2)
			}
			if row == table.HeaderRow {
				return s.Bold(true).Foreground(ColorAccent)
			}
			return s
		})
	return t.Render() + "\n"
}

// PrintTable prints Table(headers, rows).
func PrintTable(headers []string, rows [][]string) { write(Stdout, Table(headers, rows)) }

// Box renders lines inside a rounded border with a colored title on top —
// used for end-of-command summaries.
func Box(title string, kind int, lines ...string) string {
	var c lipgloss.TerminalColor = ColorAccent
	icon := Icon(IconBrand)
	switch kind {
	case BadgeSuccess:
		c, icon = ColorSuccess, Icon(IconOK)
	case BadgeWarn:
		c, icon = ColorWarn, Icon(IconWarn)
	case BadgeError:
		c, icon = ColorError, Icon(IconFail)
	}
	border := lipgloss.RoundedBorder()
	if ASCII() {
		border = lipgloss.ASCIIBorder()
	}
	head := fg(c).Bold(true).Render(icon + " " + title)
	body := strings.TrimRight(strings.Join(lines, "\n"), "\n")
	content := head
	if body != "" {
		content += "\n\n" + body
	}
	maxW := Width() - 2
	style := renderer.NewStyle().
		Border(border).
		BorderForeground(c).
		Padding(0, 2)
	if lipgloss.Width(content)+6 > maxW && maxW > 20 {
		style = style.Width(maxW - 2)
	}
	return style.Render(content) + "\n"
}

// PrintBox prints Box(title, kind, lines...).
func PrintBox(title string, kind int, lines ...string) { write(Stdout, Box(title, kind, lines...)) }

// ---------------------------------------------------------------- formatting

// Duration formats d compactly: "850ms", "4.2s", "1m28s", "2h03m".
func Duration(d time.Duration) string {
	switch {
	case d < time.Second:
		return fmt.Sprintf("%dms", d.Milliseconds())
	case d < 10*time.Second:
		return fmt.Sprintf("%.1fs", d.Seconds())
	case d < time.Minute:
		return fmt.Sprintf("%ds", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%dm%02ds", int(d.Minutes()), int(d.Seconds())%60)
	default:
		return fmt.Sprintf("%dh%02dm", int(d.Hours()), int(d.Minutes())%60)
	}
}

// Bytes formats n as a human-readable size: "512 B", "12.3 MB".
func Bytes(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for m := n / unit; m >= unit; m /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(n)/float64(div), "KMGTPE"[exp])
}

// DirSize returns the total size of regular files under dir (0 on error).
func DirSize(dir string) int64 {
	var total int64
	_ = filepath.Walk(dir, func(_ string, info os.FileInfo, err error) error {
		if err == nil && info.Mode().IsRegular() {
			total += info.Size()
		}
		return nil
	})
	return total
}

// Plural returns "1 table" / "3 tables".
func Plural(n int, singular, plural string) string {
	if n == 1 {
		return fmt.Sprintf("%d %s", n, singular)
	}
	return fmt.Sprintf("%d %s", n, plural)
}

// ShortPath replaces the user's home directory prefix with "~" for display.
func ShortPath(p string) string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" || home == "/" {
		return p
	}
	if p == home {
		return "~"
	}
	if strings.HasPrefix(p, home+string(os.PathSeparator)) {
		return "~" + p[len(home):]
	}
	return p
}
