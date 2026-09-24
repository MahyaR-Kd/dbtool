// Package backupstate distinguishes published backups from interrupted work.
package backupstate

import (
	"dbtool/internal/atomicfile"
	"os"
	"path/filepath"
)

const Pending = "dbtool.pending"
const Complete = "dbtool.complete"

func Begin(dir string) error {
	return atomicfile.Write(filepath.Join(dir, Pending), []byte("pending\n"), 0600)
}
func Finish(dir string) error {
	if err := atomicfile.Write(filepath.Join(dir, Complete), []byte("complete\n"), 0600); err != nil {
		return err
	}
	return os.Remove(filepath.Join(dir, Pending))
}
func IsComplete(dir string) bool {
	if _, err := os.Lstat(filepath.Join(dir, Pending)); !os.IsNotExist(err) {
		return false
	}
	if info, err := os.Stat(filepath.Join(dir, Complete)); err == nil && info.Mode().IsRegular() {
		return true
	}
	// Older backups predate dbtool's publication markers.
	info, err := os.Stat(filepath.Join(dir, "metadata"))
	return err == nil && info.Mode().IsRegular()
}
