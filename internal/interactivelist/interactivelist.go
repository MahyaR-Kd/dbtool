// Package interactivelist provides an fzf-like, arrow-key-driven picker for
// choosing one or more items from a list — built into the dbtool binary via
// github.com/ktr0731/go-fuzzyfinder, so no external fzf install is needed.
//
// When stdin/stdout aren't an interactive terminal (piped input, scripts,
// automation), a full-screen picker has nothing to draw on, so both
// SelectOne and SelectMulti fall back to the classic "print a numbered
// list, type a number" prompt instead.
//
// Note: go-fuzzyfinder always renders using the entire current terminal
// height — there is no option to cap it (checked its full option list:
// WithMode, WithPreviewWindow, WithHotReload(Lock), WithCursorPosition,
// WithPromptString, WithHeader, WithContext, WithQuery, WithSelectOne,
// WithPreselected — nothing sizes the picker). It anchors the list to the
// bottom rows and leaves everything above blank, so on a tall terminal
// window a short list leaves a large empty gap above it. Two upstream
// feature requests ask for exactly this (github.com/ktr0731/go-fuzzyfinder
// issues #261 and #134), both open/unresolved as of this writing. This is
// a known cosmetic side effect, not a bug — kept as-is deliberately,
// preferring the picker's arrow-key/fuzzy-search UX at every list length
// over avoiding the gap.
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

	"github.com/ktr0731/go-fuzzyfinder"
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

	if !isInteractiveTerminal() {
		return selectOneFallback(os.Stdin, prompt, options)
	}

	idx, err := fuzzyfinder.Find(options, func(i int) string { return options[i] },
		fuzzyfinder.WithPromptString(prompt+"> "))
	if err != nil {
		if errors.Is(err, fuzzyfinder.ErrAbort) {
			return 0, ErrCanceled
		}
		return 0, err
	}
	return idx, nil
}

// SelectMulti presents options under prompt with Tab to toggle a
// selection and Enter to confirm (matching fzf -m), and returns the chosen
// indices (0-based), sorted ascending. A nil, non-error result means
// nothing was selected. preselected may be nil (nothing pre-checked).
func SelectMulti(prompt string, options []string, preselected PreselectedFunc) ([]int, error) {
	if len(options) == 0 {
		return nil, nil
	}

	if !isInteractiveTerminal() {
		return selectMultiFallback(os.Stdin, prompt, options, preselected)
	}

	opts := []fuzzyfinder.Option{fuzzyfinder.WithPromptString(prompt + "> ")}
	if preselected != nil {
		opts = append(opts, fuzzyfinder.WithPreselected(func(i int) bool { return preselected(i) }))
	}

	idxs, err := fuzzyfinder.FindMulti(options, func(i int) string { return options[i] }, opts...)
	if err != nil {
		if errors.Is(err, fuzzyfinder.ErrAbort) {
			return nil, ErrCanceled
		}
		return nil, err
	}
	sort.Ints(idxs)
	return idxs, nil
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
