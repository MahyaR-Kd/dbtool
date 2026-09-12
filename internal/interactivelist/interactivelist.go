// Package interactivelist provides an arrow-key/fuzzy-search picker for
// choosing one or more items from a list, by driving the real fzf binary
// (see fzf.go) as a subprocess with --height so it renders inline — sized
// to the list, never swapping the terminal into a full-screen alternate
// buffer the way a plain fullscreen picker does. That swap-and-restore is
// what made earlier attempts (an embedded picker library with no height
// option) feel like a jarring jump next to dbtool's plain text prompts.
//
// When stdin/stdout aren't an interactive terminal (piped input, scripts,
// automation) or fzf isn't installed, SelectOne and SelectMulti fall back
// to the classic "print a numbered list, type a number" prompt instead —
// fzf is a UX nicety here, never a hard requirement to run dbtool.
package interactivelist

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"sort"
	"strconv"
	"strings"

	"golang.org/x/term"
)

// ErrCanceled is returned when the user aborts selection: Esc/Ctrl+C in the
// interactive picker, or an empty/invalid line in the plain-text fallback.
var ErrCanceled = errors.New("selection canceled")

// PreselectedFunc reports whether the option at index i should start
// pre-selected (checked) in a SelectMulti picker — used when editing an
// existing choice, so the user sees their current selection and can just
// press Enter to keep it, or toggle individual items to change it. This is
// honored in both the full-screen picker and the compact fallback.
type PreselectedFunc func(i int) bool

// SelectOne presents options under prompt and returns the chosen index
// (0-based) into options. Returns ErrCanceled if the user aborts.
func SelectOne(prompt string, options []string) (int, error) {
	if len(options) == 0 {
		return 0, errors.New("no options to select from")
	}

	if !isInteractiveTerminal() || !fzfAvailable() {
		return selectOneFallback(os.Stdin, prompt, options)
	}

	idxs, err := runFzf(prompt, options, false, nil)
	if err != nil {
		return 0, err
	}
	return idxs[0], nil
}

// SelectMulti presents options under prompt with Tab to toggle a
// selection and Enter to confirm, and returns the chosen indices
// (0-based), sorted ascending. A nil, non-error result means nothing was
// selected. preselected may be nil (nothing pre-checked).
func SelectMulti(prompt string, options []string, preselected PreselectedFunc) ([]int, error) {
	if len(options) == 0 {
		return nil, nil
	}

	if !isInteractiveTerminal() || !fzfAvailable() {
		return selectMultiFallback(os.Stdin, prompt, options, preselected)
	}

	idxs, err := runFzf(prompt, options, true, preselected)
	if err != nil {
		return nil, err
	}
	sort.Ints(idxs)
	return idxs, nil
}

// Confirm asks a yes/no question as a plain typed "(y/N)" / "(Y/n)" line,
// styled like every other text prompt (see PromptLabel) — deliberately
// NOT the full-screen arrow-key picker SelectOne uses. A full-screen
// picker for a plain yes/no answer felt like a jarring visual jump next
// to the surrounding inline text prompts, so booleans stay a simple typed
// answer instead; the arrow-key picker is reserved for actual lists.
// defaultYes is used when the line is empty or unparseable, so callers
// get a plain bool with no error to handle.
func Confirm(prompt string, defaultYes bool) bool {
	return confirmText(os.Stdin, prompt, defaultYes)
}

func confirmText(r io.Reader, prompt string, defaultYes bool) bool {
	suffix := "(y/N)"
	if defaultYes {
		suffix = "(Y/n)"
	}
	marker := style(ansiCyan+ansiBold, "?")
	label := style(ansiBold, prompt)
	fmt.Printf("%s %s %s: ", marker, label, style(ansiDim, suffix))

	reader := bufio.NewReader(r)
	line, _ := reader.ReadString('\n')
	line = strings.TrimSpace(strings.ToLower(line))
	if line == "" {
		return defaultYes
	}
	return line == "y" || line == "yes"
}

// ANSI styling shared by every plain text prompt in dbtool, so a typed
// field doesn't look out of place next to the full-screen arrow-key
// picker. Disabled automatically when stdout isn't a real terminal, so
// piped output and test fixtures never see raw escape codes.
const (
	ansiReset = "\033[0m"
	ansiBold  = "\033[1m"
	ansiCyan  = "\033[36m"
	ansiDim   = "\033[2m"
)

func styleEnabled() bool {
	return term.IsTerminal(int(os.Stdout.Fd()))
}

func style(code, s string) string {
	if !styleEnabled() {
		return s
	}
	return code + s + ansiReset
}

// PromptLabel renders label in the style shared by every prompt in
// dbtool: a cyan "?" marker and a bold label. If current is non-empty,
// it's appended dimmed in brackets as the value Enter will keep — the
// same convention the arrow-key picker uses for a pre-selected item.
func PromptLabel(label, current string) string {
	marker := style(ansiCyan+ansiBold, "?")
	l := style(ansiBold, label)
	if current != "" {
		return fmt.Sprintf("%s %s %s: ", marker, l, style(ansiDim, "["+current+"]"))
	}
	return fmt.Sprintf("%s %s: ", marker, l)
}

// Text prompts for a free-text value under label, styled via PromptLabel,
// reading a line from r. If current is non-empty, pressing Enter with no
// input keeps it. r is read from directly (no reader of its own), so
// callers can thread one shared *bufio.Reader through a whole sequence of
// prompts without losing buffered-ahead input between calls.
func Text(r *bufio.Reader, label, current string) string {
	fmt.Print(PromptLabel(label, current))
	line, _ := r.ReadString('\n')
	line = strings.TrimSpace(line)
	if line == "" {
		return current
	}
	return line
}

func isInteractiveTerminal() bool {
	return term.IsTerminal(int(os.Stdin.Fd())) && term.IsTerminal(int(os.Stdout.Fd()))
}

func selectOneFallback(r io.Reader, prompt string, options []string) (int, error) {
	fmt.Println(prompt)
	for i, o := range options {
		fmt.Printf("  %d) %s\n", i+1, o)
	}
	fmt.Print("Select: ")

	reader := bufio.NewReader(r)
	line, _ := reader.ReadString('\n')
	idx, err := strconv.Atoi(strings.TrimSpace(line))
	if err != nil || idx < 1 || idx > len(options) {
		return 0, ErrCanceled
	}
	return idx - 1, nil
}

// selectMultiFallback mirrors SelectMulti's full-screen behavior in plain
// text: preselected items are marked and kept if the user presses Enter
// without typing anything, matching the "see current selection, adjust or
// accept" experience the picker gives via WithPreselected.
func selectMultiFallback(r io.Reader, prompt string, options []string, preselected PreselectedFunc) ([]int, error) {
	fmt.Println(prompt)

	var current []int
	for i, o := range options {
		mark := " "
		if preselected != nil && preselected(i) {
			mark = "x"
			current = append(current, i)
		}
		fmt.Printf("  [%s] %d) %s\n", mark, i+1, o)
	}
	fmt.Print("Select (comma-separated numbers, or press Enter to keep the current selection): ")

	reader := bufio.NewReader(r)
	line, _ := reader.ReadString('\n')
	line = strings.TrimSpace(line)
	if line == "" {
		return current, nil
	}

	seen := map[int]bool{}
	var idxs []int
	for _, part := range strings.Split(line, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		n, err := strconv.Atoi(part)
		if err != nil || n < 1 || n > len(options) {
			fmt.Printf("  Skipping invalid selection: %q\n", part)
			continue
		}
		if !seen[n] {
			seen[n] = true
			idxs = append(idxs, n-1)
		}
	}
	sort.Ints(idxs)
	return idxs, nil
}
