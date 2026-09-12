package paths

import (
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
	return dir, nil
}
