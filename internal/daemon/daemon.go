package daemon

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"syscall"
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

// Daemonize re-launches the process in the background
func Daemonize() {
	execPath, err := os.Executable()
	if err != nil {
		log.Fatalf("failed to get executable path: %v", err)
	}

	// Get state directory for logs/PID
	stateDir, err := getStateDir()
	if err != nil {
		log.Fatalf("failed to create state directory: %v", err)
	}

	logPath := filepath.Join(stateDir, logFileName)
	pidPath := filepath.Join(stateDir, pidFileName)

	// Open log file for daemon output
	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to open log file: %v\n", err)
		os.Exit(1)
	}

	// Re-exec self with marker env + state dir
	env := append(os.Environ(),
		"_ANTHROPIC_PROXY_DAEMON=1",
		"_ANTHROPIC_PROXY_STATE_DIR="+stateDir,
	)

	proc, err := os.StartProcess(execPath, append([]string{execPath}, os.Args[1:]...), &os.ProcAttr{
		Dir: ".",
		Env: env,
		Files: []*os.File{
			nil,     // stdin
			logFile, // stdout
			logFile, // stderr
		},
		Sys: &syscall.SysProcAttr{
			Setsid: true, // Detach from terminal
		},
	})
	if err != nil {
		logFile.Close()
		log.Fatalf("failed to daemonize: %v", err)
	}
	logFile.Close()

	fmt.Printf("anthropic-proxy started as daemon (PID %d)\n", proc.Pid)
	fmt.Printf("Log file: %s\n", logPath)
	fmt.Printf("PID file: %s\n", pidPath)
	fmt.Printf("Stop:     kill %d\n", proc.Pid)
	proc.Release()
}

// SetupLog redirects log output to file when running as daemon
func SetupLog() *os.File {
	stateDir := os.Getenv("_ANTHROPIC_PROXY_STATE_DIR")
	if stateDir == "" {
		// Fallback to current directory
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
		// Fallback to current directory
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
