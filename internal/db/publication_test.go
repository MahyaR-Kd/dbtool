package db

import (
	"dbtool/internal/backupstate"
	"dbtool/internal/types"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestDumpStagesInEmptyDirectoryAndPublishesOnlySuccess(t *testing.T) {
	if os.Getenv("DBTOOL_DUMP_PROBE") == "1" {
		RunDump(types.Config{Name: "app", Host: "127.0.0.1", Port: "1", User: "test"}, "")
		return
	}
	for _, fail := range []string{"0", "1"} {
		t.Run("failure="+fail, func(t *testing.T) {
			home := t.TempDir()
			work := filepath.Join(home, "backups")
			os.Mkdir(filepath.Join(home, ".dbtool"), 0700)
			settingsJSON := `{"work_dir":"` + work + `","storage_type":"local"}`
			// Settings uses snake_case JSON fields.
			os.WriteFile(filepath.Join(home, ".dbtool", "dbtool.settings"), []byte(settingsJSON), 0600)
			bin := t.TempDir()
			script := `#!/bin/sh
if [ "$1" = "--version" ]; then echo 'mydumper v1.0.5'; exit 0; fi
while [ "$#" -gt 0 ]; do
 if [ "$1" = "-o" ]; then shift; output="$1"; fi
 shift
done
if [ -n "$(ls -A "$output")" ]; then echo 'directory is not empty' >&2; exit 2; fi
echo complete > "$output/metadata"
exit "$DBTOOL_DUMP_FAIL"
`
			if err := os.WriteFile(filepath.Join(bin, "mydumper"), []byte(script), 0700); err != nil {
				t.Fatal(err)
			}
			cmd := exec.Command(os.Args[0], "-test.run=^TestDumpStagesInEmptyDirectoryAndPublishesOnlySuccess$")
			cmd.Env = append(os.Environ(), "HOME="+home, "PATH="+bin+":"+os.Getenv("PATH"), "DBTOOL_DUMP_PROBE=1", "DBTOOL_DUMP_FAIL="+fail)
			output, err := cmd.CombinedOutput()
			if fail == "0" && err != nil {
				t.Fatalf("dump failed: %v %s", err, output)
			}
			if fail == "1" && err == nil {
				t.Fatal("failed dump reported success")
			}
			published, _ := filepath.Glob(filepath.Join(work, "app_*"))
			if fail == "1" && len(published) != 0 {
				t.Fatalf("failed dump published: %v", published)
			}
			if fail == "0" && (len(published) != 1 || !backupstate.IsComplete(published[0])) {
				t.Fatalf("dump not published: %v %s", published, output)
			}
		})
	}
}
