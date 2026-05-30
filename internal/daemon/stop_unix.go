//go:build !windows

package daemon

import (
	"fmt"
	"os"
	"syscall"
)

// killProcess sends SIGTERM to the process (Unix)
func killProcess(pid int) error {
	proc, err := os.FindProcess(pid)
	if err != nil {
		return fmt.Errorf("process %d not found: %w", pid, err)
	}

	return proc.Signal(syscall.SIGTERM)
}

// isProcessRunning checks if a process is running (Unix)
func isProcessRunning(pid int) (bool, error) {
	proc, err := os.FindProcess(pid)
	if err != nil {
		return false, nil
	}

	// Send signal 0 to check if process exists
	err = proc.Signal(syscall.Signal(0))
	if err != nil {
		return false, nil
	}

	return true, nil
}
