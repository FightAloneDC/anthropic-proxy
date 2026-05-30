package daemon

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// GetPIDFilePath returns the path to the PID file
func GetPIDFilePath() string {
	stateDir, err := getStateDir()
	if err != nil {
		return pidFileName
	}
	return filepath.Join(stateDir, pidFileName)
}

// ReadPID reads the PID from the PID file
func ReadPID() (int, error) {
	pidPath := GetPIDFilePath()
	data, err := os.ReadFile(pidPath)
	if err != nil {
		return 0, fmt.Errorf("cannot read PID file %s: %w", pidPath, err)
	}

	pidStr := strings.TrimSpace(string(data))
	pid, err := strconv.Atoi(pidStr)
	if err != nil {
		return 0, fmt.Errorf("invalid PID in file: %s", pidStr)
	}

	return pid, nil
}

// RemovePIDFile removes the PID file
func RemovePIDFile() error {
	pidPath := GetPIDFilePath()
	return os.Remove(pidPath)
}

// Stop kills the running daemon process
func Stop() error {
	pid, err := ReadPID()
	if err != nil {
		return err
	}

	if err := killProcess(pid); err != nil {
		return fmt.Errorf("failed to kill process %d: %w", pid, err)
	}

	// Remove PID file after successful kill
	RemovePIDFile()

	return nil
}

// Status checks if the daemon is running
func Status() (int, bool, error) {
	pid, err := ReadPID()
	if err != nil {
		return 0, false, err
	}

	running, err := isProcessRunning(pid)
	if err != nil {
		return pid, false, err
	}

	return pid, running, nil
}
