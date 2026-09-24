package db

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"dbtool/internal/backupstate"
	"dbtool/internal/types"
)

func TestSQLModePatchPreservesRowsAndComments(t *testing.T) {
	body := "INSERT INTO `logs` VALUES ('NO_AUTO_CREATE_USER', 'SET SQL_MODE=\"NO_AUTO_CREATE_USER\"');\n-- NO_AUTO_CREATE_USER\n"
	for _, header := range []string{"", "/*!40101 SET SQL_MODE='STRICT_TRANS_TABLES'*/;\n", "/*!40101 SET SQL_MODE='NO_AUTO_CREATE_USER,STRICT_TRANS_TABLES'*/;\n"} {
		got, _ := stripRemovedSQLModeValues([]byte(header + body))
		if !strings.HasSuffix(string(got), body) {
			t.Fatalf("row contents altered: %s", got)
		}
		if strings.Contains(string(got), "SQL_MODE='NO_AUTO_CREATE_USER") {
			t.Fatal("removed mode retained")
		}
	}
}

func TestLatestDumpSkipsPendingBackup(t *testing.T) {
	dir := t.TempDir()
	old := filepath.Join(dir, "app_2026-09-15_010000")
	newer := filepath.Join(dir, "app_2026-09-16_010000")
	for _, d := range []string{old, newer} {
		if err := os.Mkdir(d, 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(d, "metadata"), nil, 0600); err != nil {
			t.Fatal(err)
		}
	}
	if err := backupstate.Begin(newer); err != nil {
		t.Fatal(err)
	}
	if got := FindLatestDumpDir(dir, "app"); got != old {
		t.Fatalf("selected %s", got)
	}
	if err := backupstate.Finish(newer); err != nil {
		t.Fatal(err)
	}
	if got := FindLatestDumpDir(dir, "app"); got != newer {
		t.Fatalf("selected %s", got)
	}
}

func TestValidateDoubleQuotedDump(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "app.users-schema.sql"), []byte(`CREATE TABLE "users" ("id" int, "display,name" text);`), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "app.users.00000.sql"), []byte(`INSERT INTO "users" ("id") VALUES (1);`), 0600); err != nil {
		t.Fatal(err)
	}
	issues := ValidateDump(dir)
	if len(issues) != 1 || len(issues[0].Missing) != 1 || issues[0].Missing[0] != "display,name" {
		t.Fatalf("issues: %+v", issues)
	}
}

func fakeLoader(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "myloader")
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"+body), 0700); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestRestoreCompletionDoesNotMaskFailure(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	loader := fakeLoader(t, "echo 'Errors found: Data: 1' >&2\necho 'Restore completed' >&2\nexit 1\n")
	err, _ := runMyloaderOnce(types.Config{Name: "test"}, loader, nil, t.TempDir(), "")
	if err == nil {
		t.Fatal("failed restore reported success")
	}
}

func TestReadOnlyRestoreDoesNotRestartWithoutOverwrite(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	count := filepath.Join(t.TempDir(), "attempts")
	t.Setenv("REVIEW_ATTEMPTS", count)
	loader := fakeLoader(t, "echo attempt >> \"$REVIEW_ATTEMPTS\"\necho 'running with the --read-only option' >&2\nexit 1\n")
	if err := restoreOneDatabase(types.Config{Name: "test"}, "", loader, nil, t.TempDir(), ""); err == nil {
		t.Fatal("expected failure")
	}
	data, err := os.ReadFile(count)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "attempt\n" {
		t.Fatalf("unsafe retries: %s", data)
	}
	for _, args := range [][]string{{"--overwrite-tables"}, {"--drop-table"}, {"--drop-table=DROP"}} {
		if !restoreCanRestart(args) {
			t.Fatalf("overwrite not recognized: %v", args)
		}
	}
	if restoreCanRestart([]string{"--drop-table=NONE"}) {
		t.Fatal("NONE does not permit restart")
	}
}

func TestSQLModePatchStopsBeforeMultilineRow(t *testing.T) {
	original := "/* documentation\nSET SQL_MODE='NO_AUTO_CREATE_USER';\n*/\nINSERT INTO `logs` VALUES ('row\nSET SQL_MODE=\"NO_AUTO_CREATE_USER\";\nend');\n"
	got, changed := stripRemovedSQLModeValues([]byte(original))
	if changed || string(got) != original {
		t.Fatalf("non-preamble SQL altered: %s", got)
	}
}
