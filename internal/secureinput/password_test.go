package secureinput

import (
	"bufio"
	"bytes"
	"io"
	"strings"
	"testing"
)

func TestReadMaskedBasicInput(t *testing.T) {
	in := strings.NewReader("hunter2\n")
	var out bytes.Buffer

	got, err := readMasked(in, &out)
	if err != nil {
		t.Fatalf("readMasked: %v", err)
	}
	if got != "hunter2" {
		t.Errorf("got %q, want %q", got, "hunter2")
	}
	if !strings.Contains(out.String(), "*******") {
		t.Errorf("expected 7 stars echoed, got output %q", out.String())
	}
}

// TestReadMaskedEmitsCRLFNotBareLF is a regression test for a real bug: the
// terminal is in raw mode (term.MakeRaw) while this runs, which disables
// output processing (OPOST) — the flag responsible for a terminal
// auto-translating a bare '\n' into '\r\n'. Emitting just "\n" after Enter
// (e.g. via fmt.Fprintln) left the cursor sitting at whatever column the
// last '*' was echoed to, so the next line printed by the caller after
// ReadPassword returns started indented from there instead of at the left
// margin. Every line-ending write here must include an explicit '\r'.
func TestReadMaskedEmitsCRLFNotBareLF(t *testing.T) {
	tests := []struct {
		name string
		in   string
	}{
		{"enter", "hunter2\n"},
		{"ctrl-c abort", "abc\x03"},
		{"eof without newline", "partial"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var out bytes.Buffer
			_, _ = readMasked(strings.NewReader(tc.in), &out)

			got := out.String()
			if !strings.HasSuffix(got, "\r\n") {
				t.Errorf("output %q does not end with \\r\\n (cursor would not return to column 0 in raw mode)", got)
			}
			if strings.Contains(strings.TrimSuffix(got, "\r\n"), "\n") {
				t.Errorf("output %q contains a bare \\n elsewhere — every line end must be \\r\\n in raw mode", got)
			}
		})
	}
}

func TestReadMaskedBackspaceErasesChar(t *testing.T) {
	// Types "abcX", backspaces the X, types "d" -> "abcd"
	in := strings.NewReader("abcX\x7fd\n")
	var out bytes.Buffer

	got, err := readMasked(in, &out)
	if err != nil {
		t.Fatalf("readMasked: %v", err)
	}
	if got != "abcd" {
		t.Errorf("got %q, want %q", got, "abcd")
	}
}

func TestReadMaskedBackspaceOnEmptyBufferIsNoop(t *testing.T) {
	in := strings.NewReader("\x7f\x7fok\n")
	var out bytes.Buffer

	got, err := readMasked(in, &out)
	if err != nil {
		t.Fatalf("readMasked: %v", err)
	}
	if got != "ok" {
		t.Errorf("got %q, want %q", got, "ok")
	}
}

func TestReadMaskedCtrlCAborts(t *testing.T) {
	in := strings.NewReader("abc\x03")
	var out bytes.Buffer

	_, err := readMasked(in, &out)
	if err == nil {
		t.Fatal("expected an error when input is aborted with Ctrl+C")
	}
}

func TestReadMaskedEmptyInput(t *testing.T) {
	in := strings.NewReader("\n")
	var out bytes.Buffer

	got, err := readMasked(in, &out)
	if err != nil {
		t.Fatalf("readMasked: %v", err)
	}
	if got != "" {
		t.Errorf("got %q, want empty string", got)
	}
}

func TestReadMaskedEOFWithoutNewline(t *testing.T) {
	// No trailing newline (e.g. piped input without one) should still return
	// what was typed rather than hanging or erroring.
	in := strings.NewReader("partial")
	var out bytes.Buffer

	got, err := readMasked(in, &out)
	if err != nil {
		t.Fatalf("readMasked: %v", err)
	}
	if got != "partial" {
		t.Errorf("got %q, want %q", got, "partial")
	}
}

func TestReadLineTrimsNewline(t *testing.T) {
	got, err := readLine(bufio.NewReader(strings.NewReader("mypassword\n")))
	if err != nil {
		t.Fatalf("readLine: %v", err)
	}
	if got != "mypassword" {
		t.Errorf("got %q, want %q", got, "mypassword")
	}
}

func TestReadLine_EOFWithNoDataReturnsEOFError(t *testing.T) {
	// This is the exact condition that used to cause an infinite retry loop
	// in credvault.setup(): once piped input is exhausted, further reads
	// must surface as an error, not silently succeed with "".
	_, err := readLine(bufio.NewReader(strings.NewReader("")))
	if err != io.EOF {
		t.Errorf("got %v, want io.EOF", err)
	}
}

func TestReadLine_PartialLineWithoutTrailingNewlineSucceeds(t *testing.T) {
	// Piped input whose last line has no trailing \n should still return
	// that data, not an error — only a truly empty read is EOF-as-error.
	got, err := readLine(bufio.NewReader(strings.NewReader("noNewlineAtEnd")))
	if err != nil {
		t.Fatalf("readLine: %v", err)
	}
	if got != "noNewlineAtEnd" {
		t.Errorf("got %q, want %q", got, "noNewlineAtEnd")
	}
}

// TestReadLine_SharedReaderAcrossMultipleCallsDoesNotLoseData is a direct
// regression test for a real bug: ReadPassword used to wrap os.Stdin in a
// brand new bufio.Reader on every call. bufio.NewReader eagerly buffers a
// whole chunk from the underlying source, not just up to the next newline,
// so when two lines ("pw1\npw2\n") were available in one read, the first
// call's fresh reader would silently drain BOTH lines into its own buffer,
// return only the first, and then get garbage-collected — permanently
// losing the second line. A second call's brand new reader would then see
// nothing, immediately hitting an unexpected EOF. Reusing ONE *bufio.Reader
// across sequential calls (as ReadPassword now does via the package-level
// stdinReader) is what makes multi-prompt piped input — e.g. credvault's
// "create" + "confirm" master password prompts — actually work.
func TestReadLine_SharedReaderAcrossMultipleCallsDoesNotLoseData(t *testing.T) {
	shared := bufio.NewReader(strings.NewReader("first-line\nsecond-line\nthird-line\n"))

	for i, want := range []string{"first-line", "second-line", "third-line"} {
		got, err := readLine(shared)
		if err != nil {
			t.Fatalf("call %d: readLine: %v", i, err)
		}
		if got != want {
			t.Errorf("call %d: got %q, want %q", i, got, want)
		}
	}
}

func TestResolveSecretPrefersFlagValue(t *testing.T) {
	t.Setenv("DBTOOL_TEST_SECRET", "from-env")

	got, err := ResolveSecret("from-flag", "DBTOOL_TEST_SECRET", "unused prompt: ")
	if err != nil {
		t.Fatalf("ResolveSecret: %v", err)
	}
	if got != "from-flag" {
		t.Errorf("got %q, want %q", got, "from-flag")
	}
}

func TestResolveSecretFallsBackToEnv(t *testing.T) {
	t.Setenv("DBTOOL_TEST_SECRET", "from-env")

	got, err := ResolveSecret("", "DBTOOL_TEST_SECRET", "unused prompt: ")
	if err != nil {
		t.Fatalf("ResolveSecret: %v", err)
	}
	if got != "from-env" {
		t.Errorf("got %q, want %q", got, "from-env")
	}
}
