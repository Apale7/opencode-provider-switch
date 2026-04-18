//go:build !windows

package config

import (
	"fmt"
	"os"
	"syscall"
)

func lockTestFile(file *os.File) error {
	if err := syscall.Flock(int(file.Fd()), syscall.LOCK_EX); err != nil {
		return fmt.Errorf("flock lock: %w", err)
	}
	return nil
}

func unlockTestFile(file *os.File) error {
	if err := syscall.Flock(int(file.Fd()), syscall.LOCK_UN); err != nil {
		return fmt.Errorf("flock unlock: %w", err)
	}
	return nil
}
