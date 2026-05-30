//go:build !windows

package daemon

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"syscall"
)

// Daemonize re-launches the process in the background (Unix only)
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
