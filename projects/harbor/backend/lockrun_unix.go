//go:build !windows

package backend

import (
	"errors"
	"os"
	"path/filepath"
	"syscall"
)

// lockRun takes the lock in a run's folder, which the run's process holds
// for as long as the run goes. It returns nil, and no error, when another
// process holds it, or this one through another file. The lock is the
// operating system's, so it is let go when the process ends however it
// ends.
func lockRun(dir string) (*os.File, error) {
	f, err := os.OpenFile(filepath.Join(dir, "lock"), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		f.Close()
		if errors.Is(err, syscall.EWOULDBLOCK) {
			return nil, nil
		}
		return nil, err
	}
	return f, nil
}
