package paths

import (
	"dbtool/internal/atomicfile"
	"dbtool/internal/filelock"
	"os"
	"path/filepath"
)

// DbtoolDir returns the path to the ~/.dbtool directory and creates it if it
// does not yet exist.  All dbtool data files (log, conf, jobs, settings) are
// stored inside this directory.
func DbtoolDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(home, ".dbtool")
	if err := os.MkdirAll(dir, 0700); err != nil {
		return "", err
	}
	release, err := filelock.Acquire(dir, "rotation-recovery", true)
	if err != nil {
		return "", err
	}
	err = atomicfile.Recover(filepath.Join(dir, ".rotation-journal"))
	release()
	if err != nil {
		return "", err
	}
	return dir, nil
}
