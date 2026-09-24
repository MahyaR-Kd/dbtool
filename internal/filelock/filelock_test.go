package filelock

import (
	"errors"
	"testing"
)

func TestExclusiveLockAndRelease(t *testing.T) {
	dir := t.TempDir()
	release, err := Acquire(dir, "destination", false)
	if err != nil {
		t.Fatal(err)
	}
	if unlock, err := Acquire(dir, "destination", false); !errors.Is(err, ErrLocked) {
		if unlock != nil {
			unlock()
		}
		t.Fatalf("concurrent lock: %v", err)
	}
	release()
	unlock, err := Acquire(dir, "destination", false)
	if err != nil {
		t.Fatal(err)
	}
	unlock()
}
