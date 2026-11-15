package job

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A job saved by an older version with a schedule the current parser
// rejects must not block migrating its plaintext password.
func TestMigrationKeepsJobWithLegacySchedule(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	path := jobFilePath()
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	legacy := "old|dump|@daily|src|plain-pass|||false\n"
	if err := os.WriteFile(path, []byte(legacy), 0600); err != nil {
		t.Fatal(err)
	}

	jobs := Load()
	if len(jobs) != 1 || jobs[0].Schedule != "@daily" || jobs[0].SrcPassword != "plain-pass" {
		t.Fatalf("loaded %+v", jobs)
	}
	data, _ := os.ReadFile(path)
	if strings.Contains(string(data), "plain-pass") {
		t.Fatalf("password still plaintext after migration: %s", data)
	}
	if _, needs := load(); needs {
		t.Fatal("migration did not complete")
	}
}
