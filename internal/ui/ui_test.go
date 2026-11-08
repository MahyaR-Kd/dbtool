package ui

import (
	"bytes"
	"errors"
	"strings"
	"testing"
	"time"
)

// capture redirects Stdout/Stderr to buffers with colors off and Unicode
// icons on, restoring everything afterwards.
func capture(t *testing.T) (out, errOut *bytes.Buffer) {
	t.Helper()
	out, errOut = &bytes.Buffer{}, &bytes.Buffer{}
	prevOut, prevErr := Stdout, Stderr
	Stdout, Stderr = out, errOut
	SetNoColor(true)
	SetASCII(false)
	t.Cleanup(func() {
		Stdout, Stderr = prevOut, prevErr
		SetNoColor(false)
	})
	return out, errOut
}

func TestStatusMessagesRouteAndFormat(t *testing.T) {
	out, errOut := capture(t)

	Success("Saved %s", "prod")
	Info("hello")
	Step("next")
	Warn("careful")
	Error("broken: %v", errors.New("boom"))

	wantOut := "✔ Saved prod\nℹ hello\n➜ next\n"
	if out.String() != wantOut {
		t.Fatalf("stdout = %q, want %q", out.String(), wantOut)
	}
	wantErr := "⚠ careful\n✖ broken: boom\n"
	if errOut.String() != wantErr {
		t.Fatalf("stderr = %q, want %q", errOut.String(), wantErr)
	}
}

func TestNoANSIWhenColorDisabled(t *testing.T) {
	out, _ := capture(t)
	Title("Dump", "prod")
	Section("Connection")
	PrintKeyValues(KV{"Host", "db:3306"}, KV{"Empty", ""}, KV{"User", "root"})
	PrintTable([]string{"NAME", "HOST"}, [][]string{{"prod", "db:3306"}})
	PrintBox("Done", BadgeSuccess, "line one")
	Println(Badge("dump", BadgeInfo), OnOff(true), Bold("b"), Accent("a"))
	if strings.Contains(out.String(), "\x1b[") {
		t.Fatalf("output contains ANSI escapes with colors disabled:\n%q", out.String())
	}
	if strings.Contains(out.String(), "Empty") {
		t.Fatal("KeyValues should skip empty values")
	}
}

func TestKeyValuesAlign(t *testing.T) {
	capture(t)
	got := KeyValues(KV{"A", "1"}, KV{"Long key", "2"})
	want := "  A         1\n  Long key  2\n"
	if got != want {
		t.Fatalf("KeyValues = %q, want %q", got, want)
	}
}

func TestASCIIIcons(t *testing.T) {
	out, _ := capture(t)
	SetASCII(true)
	defer SetASCII(false)
	Success("ok")
	if out.String() != "[ok] ok\n" {
		t.Fatalf("ascii success = %q", out.String())
	}
}

func TestTaskNonTerminalPrintsOnlyResult(t *testing.T) {
	out, errOut := capture(t)
	tk := StartTask("Uploading")
	tk.Update("Uploading to S3")
	tk.Done("s3://b/k")
	tk.Done("ignored second call")

	if got := out.String(); !strings.HasPrefix(got, "✔ Uploading to S3  s3://b/k") || strings.Count(got, "\n") != 1 {
		t.Fatalf("task output = %q", got)
	}

	tk2 := StartTask("Sending")
	tk2.Fail(errors.New("nope"))
	if !strings.Contains(errOut.String(), "✖ Sending  nope") {
		t.Fatalf("fail output = %q", errOut.String())
	}
}

func TestBarNeverGoesBackwards(t *testing.T) {
	capture(t)
	b := NewBar(10, "Dumping", "tables")
	_ = b.Set(5)
	_ = b.Set(3)
	if b.Current() != 5 {
		t.Fatalf("current = %d, want 5", b.Current())
	}
	_ = b.Add(2)
	if b.Current() != 7 {
		t.Fatalf("current = %d, want 7", b.Current())
	}
	b.SetTotal(20)
	if b.Total() != 20 {
		t.Fatalf("total = %d", b.Total())
	}
	view := b.render(100)
	if !strings.Contains(view, "7/20 tables") || !strings.Contains(view, "35%") {
		t.Fatalf("render = %q", view)
	}
	_ = b.Finish()
	_ = b.Finish()
}

func TestBarUnknownTotal(t *testing.T) {
	capture(t)
	b := NewBar(-1, "Restoring", "tables")
	_ = b.Set(4)
	if v := b.render(80); !strings.Contains(v, "4 tables") {
		t.Fatalf("render = %q", v)
	}
	_ = b.Finish()
}

func TestFormatting(t *testing.T) {
	cases := map[time.Duration]string{
		500 * time.Millisecond:  "500ms",
		4200 * time.Millisecond: "4.2s",
		42 * time.Second:        "42s",
		88 * time.Second:        "1m28s",
		125 * time.Minute:       "2h05m",
	}
	for d, want := range cases {
		if got := Duration(d); got != want {
			t.Errorf("Duration(%v) = %q, want %q", d, got, want)
		}
	}
	if got := Bytes(512); got != "512 B" {
		t.Errorf("Bytes(512) = %q", got)
	}
	if got := Bytes(12_900_000); got != "12.3 MB" {
		t.Errorf("Bytes = %q", got)
	}
	if Plural(1, "table", "tables") != "1 table" || Plural(3, "table", "tables") != "3 tables" {
		t.Error("Plural")
	}
}

func TestFatalExits(t *testing.T) {
	_, errOut := capture(t)
	code := -1
	exit = func(c int) { code = c }
	defer func() { exit = osExit }()
	Fatal("bad %d", 1)
	if code != 1 || !strings.Contains(errOut.String(), "✖ bad 1") {
		t.Fatalf("code=%d stderr=%q", code, errOut.String())
	}
}
