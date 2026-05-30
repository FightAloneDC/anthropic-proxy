//go:build windows

package daemon

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
)

// Daemonize on Windows: start as background process (no fork, uses START)
func Daemonize() {
	// Get state directory for logs/PID
	stateDir, err := getStateDir()
	if err != nil {
		// Fallback to current directory on Windows
		stateDir = "."
	}

	logPath := filepath.Join(stateDir, logFileName)
	pidPath := filepath.Join(stateDir, pidFileName)

	// Open log file for daemon output
	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to open log file: %v\n", err)
		os.Exit(1)
	}
	defer logFile.Close()

	// Write PID file
	err = os.WriteFile(pidPath, []byte(fmt.Sprintf("%d\n", os.Getpid())), 0644)
	if err != nil {
		log.Printf("warning: failed to write PID file: %v", err)
	}

	// On Windows, redirect stdout/stderr to log file and run in foreground
	// True daemon mode requires Windows Services API
	fmt.Printf("anthropic-proxy started (PID %d)\n", os.Getpid())
	fmt.Printf("Log file: %s\n", logPath)
	fmt.Printf("PID file: %s\n", pidPath)
	fmt.Printf("Note: On Windows, running in foreground mode\n")
	fmt.Printf("Stop:     taskkill /PID %d /F\n", os.Getpid())

	// Set daemon flag and redirect output
	os.Setenv("_ANTHROPIC_PROXY_DAEMON", "1")
	os.Setenv("_ANTHROPIC_PROXY_STATE_DIR", stateDir)
	log.SetOutput(logFile)

	// Block forever (keep running)
	select {}
}
