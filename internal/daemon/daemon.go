package daemon

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
)

const (
	appName     = "anthropic-proxy"
	logFileName = "anthropic-proxy.log"
	pidFileName = "anthropic-proxy.pid"
)

// getStateDir returns the state directory for logs and PID files
// Uses $XDG_STATE_HOME/anthropic-proxy or $HOME/.local/state/anthropic-proxy
func getStateDir() (string, error) {
	// Check XDG_STATE_HOME first
	if xdg := os.Getenv("XDG_STATE_HOME"); xdg != "" {
		dir := filepath.Join(xdg, appName)
		return dir, os.MkdirAll(dir, 0755)
	}

	// Fallback to $HOME/.local/state/
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("cannot determine home directory: %w", err)
	}

	dir := filepath.Join(home, ".local", "state", appName)
	return dir, os.MkdirAll(dir, 0755)
}

// SetupLog redirects log output to file when running as daemon
func SetupLog() *os.File {
	stateDir := os.Getenv("_ANTHROPIC_PROXY_STATE_DIR")
	if stateDir == "" {
		stateDir = "."
	}

	logPath := filepath.Join(stateDir, logFileName)
	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return nil
	}
	log.SetOutput(logFile)
	return logFile
}

// WritePIDFile writes the current PID to a file
func WritePIDFile() {
	stateDir := os.Getenv("_ANTHROPIC_PROXY_STATE_DIR")
	if stateDir == "" {
		stateDir = "."
	}

	pidPath := filepath.Join(stateDir, pidFileName)
	err := os.WriteFile(pidPath, []byte(fmt.Sprintf("%d\n", os.Getpid())), 0644)
	if err != nil {
		log.Printf("warning: failed to write PID file: %v", err)
	}
}

// IsDaemon returns true if running as a daemon child process
func IsDaemon() bool {
	return os.Getenv("_ANTHROPIC_PROXY_DAEMON") != ""
}
