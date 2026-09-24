// Package atomicfile persists files without truncating the previous version.
package atomicfile

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

func Write(path string, data []byte, mode os.FileMode) error {
	f, err := os.CreateTemp(filepath.Dir(path), ".dbtool-write-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	defer f.Close()
	if err = f.Chmod(mode); err != nil {
		return err
	}
	if _, err = f.Write(data); err != nil {
		return err
	}
	if err = f.Sync(); err != nil {
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	if err = os.Rename(f.Name(), path); err != nil {
		return err
	}
	d, err := os.Open(filepath.Dir(path))
	if err != nil {
		return err
	}
	defer d.Close()
	return d.Sync()
}

// Entry is one file in a recoverable update, relative to the journal directory.
type Entry struct {
	Name string
	Data []byte
}

// Recover rolls a journaled update forward after an interrupted write.
func Recover(journal string) error {
	data, err := os.ReadFile(journal)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	var entries []Entry
	if err := json.Unmarshal(data, &entries); err != nil {
		return err
	}
	for _, e := range entries {
		if e.Name == "." || filepath.Base(e.Name) != e.Name {
			return fmt.Errorf("invalid journal entry %q", e.Name)
		}
		if err := Write(filepath.Join(filepath.Dir(journal), e.Name), e.Data, 0600); err != nil {
			return err
		}
	}
	if err := os.Remove(journal); err != nil {
		return err
	}
	d, err := os.Open(filepath.Dir(journal))
	if err != nil {
		return err
	}
	defer d.Close()
	return d.Sync()
}

// WriteBatch durably stages the complete update before replacing any file.
// A failure after staging leaves the journal for the next reader to recover.
func WriteBatch(journal string, entries []Entry) error {
	if err := Recover(journal); err != nil {
		return err
	}
	data, err := json.Marshal(entries)
	if err != nil {
		return err
	}
	if err := Write(journal, data, 0600); err != nil {
		return err
	}
	return Recover(journal)
}
