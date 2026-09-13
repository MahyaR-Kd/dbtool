package db

import (
	"os"
	"path/filepath"
	"testing"
)

// TestMydumperMetadataComplete guards a real false-failure: mydumper 1.0.5
// increments an internal error counter (and so exits non-zero) for a
// completely benign warning — e.g. "Couldn't get master position" when the
// DB user lacks SUPER/BINLOG MONITOR privilege, which is expected for a
// restricted backup account — even when the dump itself completed with
// every table written successfully (github.com/mydumper/mydumper#1300,
// confirmed against mydumper's own source: src/common.c's m_log()
// increments `errors` whenever log_fun_1 != m_message, regardless of
// whether that's a warning or a hard error). mydumper's own "metadata"
// file (renamed from "metadata.partial" only on genuine completion,
// unconditionally on the error counter) is what RunDump uses to tell a
// real failure apart from this false one.
func TestMydumperMetadataComplete(t *testing.T) {
	t.Run("metadata file present", func(t *testing.T) {
		dir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, "metadata"), []byte("# Finished dump at: ...\n"), 0644); err != nil {
			t.Fatal(err)
		}
		if !mydumperMetadataComplete(dir) {
			t.Error("expected true when metadata file exists")
		}
	})

	t.Run("only metadata.partial present (real failure)", func(t *testing.T) {
		dir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, "metadata.partial"), []byte("..."), 0644); err != nil {
			t.Fatal(err)
		}
		if mydumperMetadataComplete(dir) {
			t.Error("expected false when only metadata.partial exists (dump never finished)")
		}
	})

	t.Run("no metadata file at all", func(t *testing.T) {
		dir := t.TempDir()
		if mydumperMetadataComplete(dir) {
			t.Error("expected false when neither metadata file exists")
		}
	})

	t.Run("nonexistent directory", func(t *testing.T) {
		if mydumperMetadataComplete(filepath.Join(t.TempDir(), "does-not-exist")) {
			t.Error("expected false for a nonexistent directory")
		}
	})
}
