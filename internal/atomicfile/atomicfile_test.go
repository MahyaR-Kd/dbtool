package atomicfile

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestRecoverInterruptedBatch(t *testing.T) {
	dir := t.TempDir()
	journal := filepath.Join(dir, "journal")
	entries := []Entry{{"conf", []byte("new ciphertext")}, {"masterkey", []byte("new verification")}}
	data, _ := json.Marshal(entries)
	if err := Write(journal, data, 0600); err != nil {
		t.Fatal(err)
	}
	if err := Write(filepath.Join(dir, "conf"), entries[0].Data, 0600); err != nil {
		t.Fatal(err)
	}
	if err := Recover(journal); err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		got, err := os.ReadFile(filepath.Join(dir, e.Name))
		if err != nil || string(got) != string(e.Data) {
			t.Fatalf("%s: %s %v", e.Name, got, err)
		}
	}
	if _, err := os.Stat(journal); !os.IsNotExist(err) {
		t.Fatalf("journal not removed: %v", err)
	}
}

func TestFailedWriteRetainsOriginal(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "conf")
	if err := Write(path, []byte("original"), 0600); err != nil {
		t.Fatal(err)
	}
	// Renaming onto a nonempty directory must fail without replacing it.
	target := filepath.Join(dir, "blocked")
	os.Mkdir(target, 0700)
	os.WriteFile(filepath.Join(target, "original"), []byte("safe"), 0600)
	if err := Write(target, []byte("new"), 0600); err == nil {
		t.Fatal("expected failure")
	}
	got, err := os.ReadFile(filepath.Join(target, "original"))
	if err != nil || string(got) != "safe" {
		t.Fatalf("original lost: %s %v", got, err)
	}
}
