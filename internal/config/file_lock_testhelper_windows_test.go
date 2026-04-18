//go:build windows

package config

import (
	"fmt"
	"os"

	"golang.org/x/sys/windows"
)

func lockTestFile(file *os.File) error {
	var overlapped windows.Overlapped
	if err := windows.LockFileEx(windows.Handle(file.Fd()), windows.LOCKFILE_EXCLUSIVE_LOCK, 0, 1, 0, &overlapped); err != nil {
		return fmt.Errorf("lock file ex: %w", err)
	}
	return nil
}

func unlockTestFile(file *os.File) error {
	var overlapped windows.Overlapped
	if err := windows.UnlockFileEx(windows.Handle(file.Fd()), 0, 1, 0, &overlapped); err != nil {
		return fmt.Errorf("unlock file ex: %w", err)
	}
	return nil
}
