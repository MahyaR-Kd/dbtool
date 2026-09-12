// Package secureinput reads passwords from the terminal without echoing
// them in plain text: each typed character is masked with '*' instead.
package secureinput

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strings"

	"golang.org/x/term"
)

// stdinReader is a single buffered reader over os.Stdin, shared across
// every non-interactive (piped) ReadPassword call in this process.
//
// Wrapping os.Stdin in a fresh bufio.Reader on every call is a classic Go
// footgun: bufio.NewReader eagerly reads a whole chunk from the underlying
// source, not just up to the next newline, so a fresh reader on a second
// call would create an empty buffer while the pipe's bytes for that second
// line were already drained into — and lost with — the first call's
// (now-discarded) buffer. That silently broke any flow needing two
// sequential piped password reads (e.g. "create" / "confirm" prompts, or a
// sync job's source+destination passwords) — the second read would see
// only whatever came after what the first read had already consumed.
var stdinReader = bufio.NewReader(os.Stdin)

// ReadPassword prints prompt, then reads a password from stdin.
//
// When stdin is an interactive terminal, input is masked: each typed
// character echoes a '*', and Backspace/Delete erase the previous star.
// Ctrl+C aborts and returns an error.
//
// When stdin is not a terminal (piped input — scripts, tests, automation),
// there is no terminal to control, so it falls back to reading a plain line
// with no masking; this preserves existing non-interactive workflows.
func ReadPassword(prompt string) (string, error) {
	fmt.Print(prompt)

	fd := int(os.Stdin.Fd())
	if !term.IsTerminal(fd) {
		return readLine(stdinReader)
	}

	oldState, err := term.MakeRaw(fd)
	if err != nil {
		// Could not switch to raw mode — degrade gracefully rather than
		// failing the command outright.
		return readLine(stdinReader)
	}
	defer term.Restore(fd, oldState) //nolint:errcheck // best-effort terminal restore

	return readMasked(os.Stdin, os.Stdout)
}

// ResolveSecret returns flagValue if non-empty (the caller passed it via a
// CLI flag). Otherwise, if envVar names a set, non-empty environment
// variable, its value is used. Otherwise it prompts interactively via
// ReadPassword. This lets a single "password source" support scripted flag
// use, env-var injection from a secrets manager, and interactive masked
// entry without the caller juggling all three itself.
func ResolveSecret(flagValue, envVar, prompt string) (string, error) {
	if flagValue != "" {
		return flagValue, nil
	}
	if envVar != "" {
		if v := os.Getenv(envVar); v != "" {
			return v, nil
		}
	}
	return ReadPassword(prompt)
}

// readLine reads one line from an already-buffered reader, trimming the
// trailing newline. It returns io.EOF explicitly (rather than swallowing it
// into an empty-string success) when the stream is exhausted with no data
// at all, so a caller doing multiple sequential reads — e.g. credvault's
// "create password" / "confirm password" pair — can detect that input ran
// out and stop retrying instead of looping forever on what would otherwise
// look like a legitimately empty answer.
func readLine(r *bufio.Reader) (string, error) {
	line, err := r.ReadString('\n')
	if err == io.EOF && line == "" {
		return "", io.EOF
	}
	if err != nil && err != io.EOF {
		return "", err
	}
	return strings.TrimRight(line, "\r\n"), nil
}

// readMasked reads raw bytes from r (already in terminal raw mode) until
// Enter, echoing '*' to w for each accepted character.
//
// Every line ending here writes "\r\n", not "\n". term.MakeRaw disables
// output processing (OPOST) along with input processing, so the terminal
// no longer auto-translates a bare "\n" into "\r\n" the way it does in
// normal (cooked) mode — a plain "\n" only moves the cursor down a row
// without returning it to column 0. Without the explicit "\r", the cursor
// would still be sitting at whatever column the last '*' was echoed to,
// and the next line printed after this function returns would start
// indented from there instead of at the left margin.
func readMasked(r io.Reader, w io.Writer) (string, error) {
	var buf []byte
	one := make([]byte, 1)

	for {
		n, err := r.Read(one)
		if n == 0 {
			if err == io.EOF {
				break
			}
			if err != nil {
				fmt.Fprint(w, "\r\n")
				return "", err
			}
			continue
		}

		switch b := one[0]; b {
		case '\r', '\n':
			fmt.Fprint(w, "\r\n")
			return string(buf), nil
		case 3: // Ctrl+C
			fmt.Fprint(w, "\r\n")
			return "", fmt.Errorf("input aborted")
		case 127, 8: // Backspace / Delete
			if len(buf) > 0 {
				buf = buf[:len(buf)-1]
				fmt.Fprint(w, "\b \b")
			}
		default:
			buf = append(buf, b)
			fmt.Fprint(w, "*")
		}
	}

	fmt.Fprint(w, "\r\n")
	return string(buf), nil
}
