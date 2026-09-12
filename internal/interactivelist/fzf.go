package interactivelist

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
)

// fzfPath is the binary looked up on PATH; a var so tests can point it at a
// fake fzf script instead of requiring the real thing to be installed.
var fzfPath = "fzf"

// fzfAvailable reports whether the real fzf binary can be found on PATH.
func fzfAvailable() bool {
	_, err := exec.LookPath(fzfPath)
	return err == nil
}

// runFzf drives the real fzf binary as a subprocess to pick from options.
// fzf opens the controlling terminal itself for its UI (it doesn't need
// stdin to be a terminal for that), so this works even though options are
// piped in and the result is read back from stdout.
//
// Each option is sent as "<index>\t<text>" so fzf can hand back which item
// was chosen by index regardless of duplicate display text: --with-nth
// only changes what's shown/matched against, while the line fzf prints on
// accept is still the original "<index>\t<text>" — so the index is parsed
// back out of that in Go (see the field.Cut below) instead of asking fzf
// to do the extraction via --accept-nth, which isn't available in the
// older fzf versions still shipped by several Linux distros' package
// managers; --delimiter/--with-nth have existed since fzf's earliest
// releases, so this works everywhere. --height=~100% is the other key
// piece: it renders fzf inline, sized to the input (never exceeding the
// terminal), instead of swapping the whole terminal into an alternate
// full-screen buffer the way a plain --height-less picker does. That
// swap-and-restore is what made the picker feel like a jarring jump next
// to dbtool's plain text prompts; sizing to content and staying inline
// removes it.
func runFzf(prompt string, options []string, multi bool, preselected PreselectedFunc) ([]int, error) {
	args := []string{
		"--height=~100%",
		"--layout=reverse",
		"--prompt=" + prompt + "> ",
		"--delimiter=\t",
		"--with-nth=2",
	}

	if multi {
		args = append(args, "--multi")
		if bind := preselectBind(options, preselected); bind != "" {
			args = append(args, "--bind", bind)
		}
	}

	cmd := exec.Command(fzfPath, args...)

	var in bytes.Buffer
	for i, o := range options {
		fmt.Fprintf(&in, "%d\t%s\n", i, o)
	}
	cmd.Stdin = &in

	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = os.Stderr

	err := cmd.Run()
	if err != nil {
		var exitErr *exec.ExitError
		// fzf exits 130 on Esc/Ctrl-C/Ctrl-G, 1 when Enter is pressed with
		// no match/selection — both mean "the user didn't pick anything".
		if errors.As(err, &exitErr) && (exitErr.ExitCode() == 130 || exitErr.ExitCode() == 1) {
			return nil, ErrCanceled
		}
		return nil, fmt.Errorf("run fzf: %w", err)
	}

	var idxs []int
	for _, line := range strings.Split(strings.TrimRight(out.String(), "\n"), "\n") {
		if line == "" {
			continue
		}
		field, _, _ := strings.Cut(line, "\t")
		idx, convErr := strconv.Atoi(field)
		if convErr != nil {
			continue
		}
		idxs = append(idxs, idx)
	}
	if len(idxs) == 0 {
		return nil, ErrCanceled
	}

	// fzf's --height mode erases its own UI on exit, leaving no trace of
	// the question or the answer behind — unlike a typed prompt, where the
	// terminal's own line echo keeps "? Label: value" visible in the
	// scrollback. Print that same line ourselves so a picker answer looks
	// just as permanent as a typed one before the next question appears.
	printAnswer(prompt, describeChoices(options, idxs))
	return idxs, nil
}

// describeChoices renders the chosen option(s) as a single display string:
// the option text itself for one choice, comma-joined for several.
func describeChoices(options []string, idxs []int) string {
	chosen := make([]string, len(idxs))
	for i, idx := range idxs {
		chosen[i] = options[idx]
	}
	return strings.Join(chosen, ", ")
}

// printAnswer prints a completed "? prompt answer" line in the same style
// as every other prompt's label, so it reads as a permanent record of what
// was asked and chosen rather than a still-open question.
func printAnswer(prompt, answer string) {
	marker := style(ansiCyan+ansiBold, "?")
	label := style(ansiBold, prompt)
	fmt.Printf("%s %s %s\n", marker, label, answer)
}

// preselectBind builds a `--bind start:pos(N)+select+pos(M)+select+...`
// string that pre-checks every option preselected reports true for, the
// way fzf's own docs recommend auto-selecting specific lines at startup
// (pos() addresses the match list by 1-based position, which equals the
// original option order before any query has been typed). Returns "" when
// nothing should start pre-checked.
func preselectBind(options []string, preselected PreselectedFunc) string {
	if preselected == nil {
		return ""
	}
	var b strings.Builder
	b.WriteString("start:")
	any := false
	for i := range options {
		if !preselected(i) {
			continue
		}
		if any {
			b.WriteString("+")
		}
		fmt.Fprintf(&b, "pos(%d)+select", i+1)
		any = true
	}
	if !any {
		return ""
	}
	b.WriteString("+first")
	return b.String()
}
