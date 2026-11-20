package app

import (
	"errors"
	"os"

	"golang.org/x/sys/windows"
)

func tryReputationLock(f *os.File) (bool, error) {
	err := windows.LockFileEx(windows.Handle(f.Fd()), windows.LOCKFILE_EXCLUSIVE_LOCK|windows.LOCKFILE_FAIL_IMMEDIATELY, 0, 1, 0, &windows.Overlapped{})
	if errors.Is(err, windows.ERROR_LOCK_VIOLATION) {
		return false, nil
	}
	return err == nil, err
}

// Windows does not support flushing directory handles with FlushFileBuffers.
func syncReputationDir(string) error { return nil }
