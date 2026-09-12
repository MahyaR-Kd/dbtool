package interactivelist

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// captureStdout redirects os.Stdout to a pipe for the duration of fn and
// returns whatever was written to it, so tests can assert on printAnswer's
// output without it landing in the actual test log.
func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	orig := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("create pipe: %v", err)
	}
	os.Stdout = w
	t.Cleanup(func() { os.Stdout = orig })

	fn()

	w.Close()
	os.Stdout = orig
	data, err := io.ReadAll(r)
	if err != nil {
		t.Fatalf("read captured stdout: %v", err)
	}
	return string(data)
}

// writeFakeFzf creates an executable shell script standing in for the real
// fzf binary: it records its argv and stdin to files (so the test can
// assert what runFzf sent it), then prints stdoutContent and exits with
// exitCode — simulating fzf's actual contract without needing a real
// terminal or the real binary.
func writeFakeFzf(t *testing.T, stdoutContent string, exitCode int) (bin, argsFile, stdinFile string) {
	t.Helper()
	dir := t.TempDir()
	bin = filepath.Join(dir, "fake-fzf")
	argsFile = filepath.Join(dir, "args.txt")
	stdinFile = filepath.Join(dir, "stdin.txt")

	script := "#!/bin/sh\n" +
		"printf '%s\\n' \"$*\" > " + shQuote(argsFile) + "\n" +
		"cat > " + shQuote(stdinFile) + "\n" +
		"printf '%s' " + shQuote(stdoutContent) + "\n" +
		"exit " + strconv.Itoa(exitCode) + "\n"

	if err := os.WriteFile(bin, []byte(script), 0755); err != nil {
		t.Fatalf("write fake fzf: %v", err)
	}
	return bin, argsFile, stdinFile
}

func shQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

func withFakeFzf(t *testing.T, stdoutContent string, exitCode int) (argsFile, stdinFile string) {
	t.Helper()
	bin, argsFile, stdinFile := writeFakeFzf(t, stdoutContent, exitCode)
	orig := fzfPath
	fzfPath = bin
	t.Cleanup(func() { fzfPath = orig })
	return argsFile, stdinFile
}

func TestRunFzf_SendsIndexedOptionsAndParsesSelection(t *testing.T) {
	// Real fzf's default output on accept is the full original line, not
	// just the index — --with-nth only changes what's displayed/matched,
	// not what's printed back. runFzf must parse the index back out of
	// this itself (see the comment on --accept-nth in fzf.go).
	argsFile, stdinFile := withFakeFzf(t, "1\tbeta\n", 0)

	idxs, err := runFzf("Pick", []string{"alpha", "beta", "gamma"}, false, nil)
	if err != nil {
		t.Fatalf("runFzf: %v", err)
	}
	if len(idxs) != 1 || idxs[0] != 1 {
		t.Fatalf("got %v, want [1]", idxs)
	}

	stdin, err := os.ReadFile(stdinFile)
	if err != nil {
		t.Fatalf("read stdin capture: %v", err)
	}
	want := "0\talpha\n1\tbeta\n2\tgamma\n"
	if string(stdin) != want {
		t.Errorf("stdin sent to fzf = %q, want %q", string(stdin), want)
	}

	args, err := os.ReadFile(argsFile)
	if err != nil {
		t.Fatalf("read args capture: %v", err)
	}
	for _, want := range []string{"--height=~100%", "--prompt=Pick> ", "--delimiter=\t", "--with-nth=2"} {
		if !strings.Contains(string(args), want) {
			t.Errorf("fzf args missing %q, got: %s", want, args)
		}
	}
	if strings.Contains(string(args), "--multi") {
		t.Errorf("single-select must not pass --multi, got: %s", args)
	}
	// --accept-nth isn't available in the older fzf versions some Linux
	// package managers still ship (this broke on a real machine once
	// already) — the index is parsed from fzf's default full-line output
	// instead, so this flag must never come back.
	if strings.Contains(string(args), "--accept-nth") {
		t.Errorf("must not pass --accept-nth (unsupported by older fzf versions), got: %s", args)
	}
}

func TestRunFzf_PrintsAnswerAfterSelection(t *testing.T) {
	withFakeFzf(t, "1\tbeta\n", 0)

	var idxs []int
	var err error
	out := captureStdout(t, func() {
		idxs, err = runFzf("Pick", []string{"alpha", "beta", "gamma"}, false, nil)
	})
	if err != nil {
		t.Fatalf("runFzf: %v", err)
	}
	if len(idxs) != 1 || idxs[0] != 1 {
		t.Fatalf("got %v, want [1]", idxs)
	}

	// fzf's own UI leaves no trace once it exits, so runFzf must print the
	// question and the chosen answer itself — otherwise, unlike a typed
	// prompt, both vanish from the scrollback once the next question
	// appears.
	if !strings.Contains(out, "Pick") || !strings.Contains(out, "beta") {
		t.Errorf("expected printed answer to contain the prompt and chosen option, got: %q", out)
	}
}

func TestRunFzf_MultiSelectPrintsCommaJoinedAnswer(t *testing.T) {
	withFakeFzf(t, "0\talpha\n2\tgamma\n", 0)

	out := captureStdout(t, func() {
		_, _ = runFzf("Pick", []string{"alpha", "beta", "gamma"}, true, nil)
	})
	if !strings.Contains(out, "alpha, gamma") {
		t.Errorf("expected comma-joined answer \"alpha, gamma\", got: %q", out)
	}
}

func TestRunFzf_MultiSelectPassesFlagAndParsesMultipleLines(t *testing.T) {
	argsFile, _ := withFakeFzf(t, "0\talpha\n2\tgamma\n", 0)

	idxs, err := runFzf("Pick", []string{"alpha", "beta", "gamma"}, true, nil)
	if err != nil {
		t.Fatalf("runFzf: %v", err)
	}
	if len(idxs) != 2 || idxs[0] != 0 || idxs[1] != 2 {
		t.Fatalf("got %v, want [0 2]", idxs)
	}

	args, _ := os.ReadFile(argsFile)
	if !strings.Contains(string(args), "--multi") {
		t.Errorf("multi-select must pass --multi, got: %s", args)
	}
}

func TestRunFzf_PreselectedBuildsStartBind(t *testing.T) {
	argsFile, _ := withFakeFzf(t, "1\tbeta\n", 0)

	preselected := func(i int) bool { return i == 0 || i == 2 }
	_, err := runFzf("Pick", []string{"alpha", "beta", "gamma"}, true, preselected)
	if err != nil {
		t.Fatalf("runFzf: %v", err)
	}

	args, _ := os.ReadFile(argsFile)
	want := "start:pos(1)+select+pos(3)+select+first"
	if !strings.Contains(string(args), want) {
		t.Errorf("fzf args missing preselect bind %q, got: %s", want, args)
	}
}

func TestRunFzf_CancelExitCodes(t *testing.T) {
	for _, code := range []int{130, 1} {
		t.Run(strconv.Itoa(code), func(t *testing.T) {
			withFakeFzf(t, "", code)
			_, err := runFzf("Pick", []string{"alpha"}, false, nil)
			if !errors.Is(err, ErrCanceled) {
				t.Errorf("exit code %d: got err=%v, want ErrCanceled", code, err)
			}
		})
	}
}

func TestRunFzf_UnexpectedExitCodeIsARealError(t *testing.T) {
	withFakeFzf(t, "", 2)
	_, err := runFzf("Pick", []string{"alpha"}, false, nil)
	if err == nil || errors.Is(err, ErrCanceled) {
		t.Errorf("got %v, want a non-ErrCanceled error", err)
	}
}

func TestPreselectBind_NothingPreselected(t *testing.T) {
	if got := preselectBind([]string{"a", "b"}, nil); got != "" {
		t.Errorf("nil preselected: got %q, want empty", got)
	}
	if got := preselectBind([]string{"a", "b"}, func(i int) bool { return false }); got != "" {
		t.Errorf("all-false preselected: got %q, want empty", got)
	}
}

func TestFzfAvailable(t *testing.T) {
	bin, _, _ := writeFakeFzf(t, "", 0)
	orig := fzfPath
	fzfPath = bin
	t.Cleanup(func() { fzfPath = orig })
	if !fzfAvailable() {
		t.Error("expected fzfAvailable to find the fake binary")
	}

	fzfPath = filepath.Join(t.TempDir(), "does-not-exist")
	if fzfAvailable() {
		t.Error("expected fzfAvailable to report false for a missing binary")
	}
}
