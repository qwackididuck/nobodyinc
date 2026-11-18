package projects

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
)

// lockFile is the file a running project daemon holds locked in the
// project's state directory.
const (
	lockFile     = "daemon.lock"
	lockFilePerm = 0o600
)

// ErrLocked is returned by Lock when another daemon runs the project.
var ErrLocked = errors.New("another Nobody Inc daemon is running this project")

// Lock makes sure only one daemon runs a project: it takes an exclusive lock
// on <stateDir>/daemon.lock and keeps it until the returned function is
// called. The lock is the operating system's (flock): it goes away with the
// process, so a daemon that crashed never leaves the project locked.
func Lock(stateDir string) (func(), error) {
	f, err := os.OpenFile(filepath.Join(stateDir, lockFile), os.O_CREATE|os.O_RDWR, lockFilePerm)
	if err != nil {
		return nil, fmt.Errorf("open the project lock: %w", err)
	}

	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		_ = f.Close() // the lock failed; closing cannot make it worse

		if errors.Is(err, syscall.EWOULDBLOCK) {
			return nil, ErrLocked
		}

		return nil, fmt.Errorf("lock the project: %w", err)
	}

	return func() {
		_ = f.Close() // closing releases the lock; nothing else to do on error
	}, nil
}
