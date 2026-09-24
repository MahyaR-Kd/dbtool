// Package filelock provides locks shared by independent dbtool processes.
package filelock

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
)

var ErrLocked = errors.New("operation is already running")

// Acquire locks a resource until release. Lock files must not be removed:
// deleting them allows another process to lock a different inode.
func Acquire(dir, resource string, wait bool) (func(), error) {
	sum := sha256.Sum256([]byte(resource))
	f, err := os.OpenFile(filepath.Join(dir, fmt.Sprintf(".lock-%x", sum)), os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, err
	}
	flags := syscall.LOCK_EX
	if !wait {
		flags |= syscall.LOCK_NB
	}
	if err := syscall.Flock(int(f.Fd()), flags); err != nil {
		f.Close()
		if errors.Is(err, syscall.EWOULDBLOCK) {
			return nil, ErrLocked
		}
		return nil, err
	}
	return func() { _ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN); _ = f.Close() }, nil
}
