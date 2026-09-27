// Package filelock provides locks shared by independent dbtool processes.
package filelock

import "errors"

var ErrLocked = errors.New("operation is already running")

// Acquire locks a resource until release. Lock files must not be removed:
// deleting them allows another process to lock a different inode.
