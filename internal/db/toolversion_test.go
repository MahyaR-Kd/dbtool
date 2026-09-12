package db

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// isolateHome points HOME (and USERPROFILE, for Windows) at a fresh temp
// dir so the logger's writes never touch the real ~/.dbtool on the machine
// running the tests.
func isolateHome(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("USERPROFILE", dir)
	return dir
}

// writeFakeVersionScript creates an executable shell script at
// <dir>/<name> that prints output when invoked with --version, simulating
// mydumper/myloader's --version behavior without needing the real binaries.
func writeFakeVersionScript(t *testing.T, dir, name, output string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	script := "#!/bin/sh\necho '" + output + "'\n"
	if err := os.WriteFile(path, []byte(script), 0755); err != nil {
		t.Fatalf("write fake script: %v", err)
	}
	return path
}

func TestDetectMydumperVersion_RecordsAndParsesVersion(t *testing.T) {
	home := isolateHome(t)
	scriptDir := t.TempDir()
	script := writeFakeVersionScript(t, scriptDir, "fake-mydumper", "mydumper 1.0.5-1, built against MySQL 8.0.36")

	v := detectMydumperVersion(script)

	if !v.OK {
		t.Fatalf("expected OK=true, got %+v", v)
	}
	if v.Major != 1 || v.Minor != 0 || v.Patch != 5 {
		t.Errorf("got %d.%d.%d, want 1.0.5", v.Major, v.Minor, v.Patch)
	}

	logPath := filepath.Join(home, ".dbtool", "dbtool.log")
	data, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("read log file: %v", err)
	}
	log := string(data)
	if !strings.Contains(log, "mydumper 1.0.5-1, built against MySQL 8.0.36") {
		t.Errorf("log file does not contain the expected version line, got:\n%s", log)
	}
	if !strings.Contains(log, script) {
		t.Errorf("log file does not mention the tool path %q, got:\n%s", script, log)
	}
}

func TestDetectMydumperVersion_OldVersion(t *testing.T) {
	isolateHome(t)
	scriptDir := t.TempDir()
	script := writeFakeVersionScript(t, scriptDir, "fake-mydumper", "mydumper 0.10.0, built against MySQL 8.0.36")

	v := detectMydumperVersion(script)

	if !v.OK || v.Major != 0 || v.Minor != 10 || v.Patch != 0 {
		t.Errorf("got %+v, want 0.10.0 OK=true", v)
	}
}

func TestDetectMydumperVersion_MissingBinaryDoesNotPanic(t *testing.T) {
	isolateHome(t)
	v := detectMydumperVersion(filepath.Join(t.TempDir(), "does-not-exist"))
	if v.OK {
		t.Errorf("expected OK=false for a missing binary, got %+v", v)
	}
}

func TestParseToolVersion(t *testing.T) {
	tests := []struct {
		name  string
		line  string
		re    string // "mydumper" or "myloader"
		want  toolVersion
		notOK bool
	}{
		{
			name: "mydumper 1.0.5 with build suffix",
			line: "mydumper 1.0.5-1, built against MySQL 8.0.36",
			re:   "mydumper",
			want: toolVersion{Major: 1, Minor: 0, Patch: 5, OK: true},
		},
		{
			name: "mydumper 0.10.0",
			line: "mydumper 0.10.0, built against MySQL 8.0.36",
			re:   "mydumper",
			want: toolVersion{Major: 0, Minor: 10, Patch: 0, OK: true},
		},
		{
			// Real-world case: the apt-packaged 1.0.5-1 release prefixes
			// the version with a literal "v", also has an "with SSL
			// support" suffix, and "against" MySQL 8.0.46 — this exact
			// line is what caused version detection to silently fail and
			// fall back to the removed --no-locks flag in production.
			name: "mydumper v-prefixed version with SSL suffix",
			line: "mydumper v1.0.5-1, built against MySQL 8.0.46 with SSL support",
			re:   "mydumper",
			want: toolVersion{Major: 1, Minor: 0, Patch: 5, OK: true},
		},
		{
			name: "myloader 1.0.5",
			line: "myloader 1.0.5-1, built against MySQL 8.0.36",
			re:   "myloader",
			want: toolVersion{Major: 1, Minor: 0, Patch: 5, OK: true},
		},
		{
			name: "myloader v-prefixed version",
			line: "myloader v1.0.5-1, built against MySQL 8.0.46 with SSL support",
			re:   "myloader",
			want: toolVersion{Major: 1, Minor: 0, Patch: 5, OK: true},
		},
		{
			name:  "unparseable garbage",
			line:  "not a version string at all",
			re:    "mydumper",
			notOK: true,
		},
		{
			name:  "empty string",
			line:  "",
			re:    "mydumper",
			notOK: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			re := mydumperVersionRe
			if tc.re == "myloader" {
				re = myloaderVersionRe
			}
			got := parseToolVersion(tc.line, re)
			if tc.notOK {
				if got.OK {
					t.Errorf("got OK=true for %q, want false: %+v", tc.line, got)
				}
				return
			}
			if got.Major != tc.want.Major || got.Minor != tc.want.Minor || got.Patch != tc.want.Patch || got.OK != tc.want.OK {
				t.Errorf("parseToolVersion(%q) = %+v, want %+v", tc.line, got, tc.want)
			}
		})
	}
}

func TestLockModeArgs(t *testing.T) {
	tests := []struct {
		name string
		v    toolVersion
		want []string
	}{
		{"mydumper 1.0.5 uses new flag", toolVersion{Major: 1, Minor: 0, Patch: 5, OK: true}, []string{"--sync-thread-lock-mode", "NO_LOCK"}},
		{"mydumper 1.0.0 uses new flag", toolVersion{Major: 1, Minor: 0, Patch: 0, OK: true}, []string{"--sync-thread-lock-mode", "NO_LOCK"}},
		{"mydumper 0.10.0 uses old flag", toolVersion{Major: 0, Minor: 10, Patch: 0, OK: true}, []string{"--no-locks"}},
		{"mydumper 2.0.0 uses new flag", toolVersion{Major: 2, Minor: 0, Patch: 0, OK: true}, []string{"--sync-thread-lock-mode", "NO_LOCK"}},
		{"unknown version falls back to old flag", toolVersion{OK: false}, []string{"--no-locks"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := lockModeArgs(tc.v)
			if !equalArgs(got, tc.want) {
				t.Errorf("lockModeArgs(%+v) = %v, want %v", tc.v, got, tc.want)
			}
		})
	}
}

func TestOverwriteTablesArgs(t *testing.T) {
	tests := []struct {
		name string
		v    toolVersion
		want []string
	}{
		{"myloader 1.0.5 uses new flag", toolVersion{Major: 1, Minor: 0, Patch: 5, OK: true}, []string{"--drop-table"}},
		{"myloader 0.10.0 uses old flag", toolVersion{Major: 0, Minor: 10, Patch: 0, OK: true}, []string{"--overwrite-tables"}},
		{"unknown version falls back to old flag", toolVersion{OK: false}, []string{"--overwrite-tables"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := overwriteTablesArgs(tc.v)
			if !equalArgs(got, tc.want) {
				t.Errorf("overwriteTablesArgs(%+v) = %v, want %v", tc.v, got, tc.want)
			}
		})
	}
}

func equalArgs(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
