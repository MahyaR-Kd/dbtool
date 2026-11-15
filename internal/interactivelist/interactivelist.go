// Package interactivelist provides every interactive prompt dbtool shows:
// single and multi-select pickers with type-to-filter, yes/no confirms and
// free-text inputs with validation.
//
// In a terminal they are built on huh (github.com/charmbracelet/huh), so
// they need no external binary (the old picker shelled out to fzf). huh
// renders inline, sized to the list, instead of switching the terminal to a
// full-screen buffer — so a picker does not jump away from the surrounding
// prompts. Once a prompt is answered it is replaced by a permanent
// "✔ Label  answer" line, so the scrollback shows what was asked and chosen.
//
// When stdin/stdout are not a terminal (piped input, scripts, automation)
// every prompt falls back to a plain line-based version — numbered lists,
// "(y/N)" and "Label [current]:" lines — so scripted use keeps working
// exactly as before.
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

	"github.com/MahyaR-Kd/dbtool/internal/secureinput"
	"github.com/MahyaR-Kd/dbtool/internal/ui"

	"github.com/charmbracelet/huh"
)

// ErrCanceled is returned when the user aborts selection: Ctrl+C in the
// interactive picker, or an empty/invalid line in the plain-text fallback.
var ErrCanceled = errors.New("selection canceled")

// PreselectedFunc reports whether the option at index i should start
// pre-selected (checked) in a SelectMulti picker — used when editing an
// existing choice, so the user sees their current selection and can just
// press Enter to keep it, or toggle individual items to change it. This is
// honored in both the interactive picker and the plain fallback.
type PreselectedFunc func(i int) bool

// interactive reports whether to show the huh-based prompts. A variable so
// tests can force the fallback path.
var interactive = ui.IsInteractive

// maxListHeight caps how many rows a picker shows at once; longer lists
// scroll (and can be filtered by typing).
const maxListHeight = 12

func listHeight(n int) int {
	// Title + filter line + options; huh needs a little headroom.
	h := n + 2
	if h > maxListHeight+2 {
		h = maxListHeight + 2
	}
	return h
}

// SelectOne presents options under prompt and returns the chosen index
// (0-based) into options. Returns ErrCanceled if the user aborts.
func SelectOne(prompt string, options []string) (int, error) {
	if len(options) == 0 {
		return 0, errors.New("no options to select from")
	}
	if !interactive() {
		return selectOneFallback(secureinput.Stdin(), prompt, options)
	}

	opts := make([]huh.Option[int], len(options))
	for i, o := range options {
		opts[i] = huh.NewOption(o, i)
	}
	choice := 0
	field := huh.NewSelect[int]().
		Title(prompt).
		Options(opts...).
		Value(&choice)
	// Size to the list (title + options), capped for long lists, which
	// scroll — "/" filters them.
	field = field.Height(listHeight(len(options)))
	if err := runField(field); err != nil {
		return 0, err
	}
	printAnswer(prompt, options[choice])
	return choice, nil
}

// SelectMulti presents options under prompt with Space/x to toggle a
// selection and Enter to confirm, and returns the chosen indices
// (0-based), sorted ascending. A nil, non-error result means nothing was
// selected. preselected may be nil (nothing pre-checked).
func SelectMulti(prompt string, options []string, preselected PreselectedFunc) ([]int, error) {
	if len(options) == 0 {
		return nil, nil
	}
	if !interactive() {
		return selectMultiFallback(secureinput.Stdin(), prompt, options, preselected)
	}

	opts := make([]huh.Option[int], len(options))
	var chosen []int
	for i, o := range options {
		opts[i] = huh.NewOption(o, i)
		if preselected != nil && preselected(i) {
			chosen = append(chosen, i)
		}
	}
	// Value must come after Options: huh then checks the preselected items
	// without scrolling the list to the first one (Option.Selected would
	// hide every item above it until the user scrolls up).
	field := huh.NewMultiSelect[int]().
		Title(prompt).
		Options(opts...).
		Filterable(true).
		Value(&chosen)
	field = field.Height(listHeight(len(options)) + 1)
	if err := runField(field); err != nil {
		return nil, err
	}
	if len(chosen) == 0 {
		printAnswer(prompt, ui.Muted("none"))
		return nil, nil
	}
	sort.Ints(chosen)
	names := make([]string, len(chosen))
	for i, idx := range chosen {
		names[i] = options[idx]
	}
	printAnswer(prompt, summarize(names))
	return chosen, nil
}

// summarize joins names for the answer line, collapsing long lists to
// "a, b, c +7 more" so the line stays readable.
func summarize(names []string) string {
	const show = 4
	if len(names) <= show {
		return strings.Join(names, ", ")
	}
	return strings.Join(names[:show], ", ") + ui.Muted(fmt.Sprintf(" +%d more", len(names)-show))
}

// Confirm asks a yes/no question. defaultYes is the pre-selected answer
// (and the answer for an empty or unparseable line in the plain fallback),
// so callers get a plain bool with no error to handle. Ctrl+C exits.
func Confirm(prompt string, defaultYes bool) bool {
	if !interactive() {
		return confirmText(secureinput.Stdin(), prompt, defaultYes)
	}
	answer := defaultYes
	field := huh.NewConfirm().
		Title(prompt).
		Affirmative("Yes").
		Negative("No").
		Value(&answer)
	if err := runField(field); err != nil {
		abort()
	}
	if answer {
		printAnswer(prompt, "yes")
	} else {
		printAnswer(prompt, "no")
	}
	return answer
}

func confirmText(r io.Reader, prompt string, defaultYes bool) bool {
	suffix := "(y/N)"
	if defaultYes {
		suffix = "(Y/n)"
	}
	fmt.Printf("%s %s %s: ", questionMark(), ui.Bold(prompt), ui.Muted(suffix))

	reader := asBuffered(r)
	line, _ := reader.ReadString('\n')
	line = strings.TrimSpace(strings.ToLower(line))
	if line == "" {
		return defaultYes
	}
	return line == "y" || line == "yes"
}

// asBuffered reuses r if it is already buffered (the shared stdin reader),
// so no read-ahead is lost between prompts.
func asBuffered(r io.Reader) *bufio.Reader {
	if br, ok := r.(*bufio.Reader); ok {
		return br
	}
	return bufio.NewReader(r)
}

func questionMark() string { return ui.Accent(ui.Icon(ui.IconQuest)) }

// PromptLabel renders label in the style shared by every typed prompt in
// dbtool: an accent "?" marker and a bold label. If current is non-empty,
// it's appended dimmed in brackets as the value Enter will keep.
func PromptLabel(label, current string) string {
	l := ui.Bold(label)
	if current != "" {
		return fmt.Sprintf("%s %s %s: ", questionMark(), l, ui.Muted("["+current+"]"))
	}
	return fmt.Sprintf("%s %s: ", questionMark(), l)
}

// Text prompts for a free-text value under label. If current is non-empty,
// it is shown as a placeholder and submitting an empty value keeps it. r is only used by the plain fallback, read from directly (no reader
// of its own), so callers can thread one shared *bufio.Reader through a
// whole sequence of prompts without losing buffered-ahead input.
func Text(r *bufio.Reader, label, current string) string {
	return TextValidated(r, label, current, nil)
}

// Validator checks a typed value and returns a user-facing error if it is
// not acceptable. Validators receive the value after trimming, and after an
// empty value has been replaced by the current one.
type Validator func(string) error

// TextValidated is Text with validation: in a terminal the error is shown
// inline under the field and Enter is refused until it is fixed; in the
// plain fallback the prompt repeats until the value passes (or input ends,
// in which case the current value is returned).
func TextValidated(r *bufio.Reader, label, current string, validate Validator) string {
	if !interactive() {
		return textFallback(r, label, current, validate)
	}

	// The current value is shown as a dimmed placeholder (not pre-filled
	// text, which typing would append to): Enter on an empty input keeps it.
	value := ""
	field := huh.NewInput().
		Title(label).
		Placeholder(current).
		Value(&value)
	if validate != nil {
		field = field.Validate(func(s string) error {
			s = strings.TrimSpace(s)
			if s == "" {
				s = current
			}
			return validate(s)
		})
	}
	if err := runField(field); err != nil {
		abort()
	}
	value = strings.TrimSpace(value)
	if value == "" {
		value = current
	}
	shown := value
	if shown == "" {
		shown = ui.Muted("(empty)")
	}
	printAnswer(label, shown)
	return value
}

func textFallback(r *bufio.Reader, label, current string, validate Validator) string {
	if r == nil {
		r = secureinput.Stdin()
	}
	for {
		fmt.Print(PromptLabel(label, current))
		line, err := r.ReadString('\n')
		line = strings.TrimSpace(line)
		if line == "" {
			line = current
		}
		if validate == nil {
			return line
		}
		verr := validate(line)
		if verr == nil {
			return line
		}
		if err != nil { // input exhausted — don't loop forever
			return current
		}
		ui.Error("%v", verr)
	}
}

// Common validators.

// Required rejects an empty value.
func Required(s string) error {
	if s == "" {
		return errors.New("a value is required")
	}
	return nil
}

// Port accepts a TCP port number (1-65535).
func Port(s string) error {
	n, err := strconv.Atoi(s)
	if err != nil || n < 1 || n > 65535 {
		return errors.New("enter a port number between 1 and 65535")
	}
	return nil
}

// OptionalPort accepts an empty value or a valid port.
func OptionalPort(s string) error {
	if s == "" {
		return nil
	}
	return Port(s)
}

// NonNegativeInt accepts 0 or a positive integer.
func NonNegativeInt(s string) error {
	n, err := strconv.Atoi(s)
	if err != nil || n < 0 {
		return errors.New("enter a whole number, 0 or greater")
	}
	return nil
}

// runField runs a single huh field as an inline form with dbtool's theme.
func runField(field huh.Field) error {
	form := huh.NewForm(huh.NewGroup(field)).
		WithTheme(Theme()).
		WithShowHelp(true).
		WithWidth(formWidth(ui.Width()))
	err := form.Run()
	if errors.Is(err, huh.ErrUserAborted) {
		return ErrCanceled
	}
	return err
}

// formWidth keeps prompts narrower than the terminal. huh pads every line
// to the form width, so a full-width form wraps as soon as the terminal
// draws a glyph like "❯" or "┃" two columns wide (macOS Terminal does for
// East Asian ambiguous-width characters). Each redraw then lands a row off
// and leaves duplicate lines behind.
func formWidth(termWidth int) int {
	return max(20, min(termWidth-4, 80))
}

// abort handles Ctrl+C on a prompt that has no way to return an error
// (Text/Confirm): the old line-based prompts died with SIGINT, so exit the
// same way (status 130) instead of silently continuing with a default.
func abort() {
	ui.Warn("Canceled")
	os.Exit(130)
}

// printAnswer leaves a permanent "✔ prompt  answer" line once a prompt has
// been answered, since huh erases its inline UI on exit.
func printAnswer(prompt, answer string) {
	ui.Printf("%s %s  %s\n", ui.Green(ui.Icon(ui.IconOK)), prompt, ui.Blue(answer))
}

func selectOneFallback(r io.Reader, prompt string, options []string) (int, error) {
	fmt.Println(prompt)
	for i, o := range options {
		fmt.Printf("  %d) %s\n", i+1, o)
	}
	fmt.Print("Select: ")

	reader := asBuffered(r)
	line, _ := reader.ReadString('\n')
	idx, err := strconv.Atoi(strings.TrimSpace(line))
	if err != nil || idx < 1 || idx > len(options) {
		return 0, ErrCanceled
	}
	return idx - 1, nil
}

// selectMultiFallback mirrors SelectMulti in plain text: preselected items
// are marked and kept if the user presses Enter without typing anything.
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

	reader := asBuffered(r)
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
