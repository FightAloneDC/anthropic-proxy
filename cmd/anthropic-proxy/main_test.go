package main

import (
	"log"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSetupLogFileCreatesParentAndAppends(t *testing.T) {
	tmp := t.TempDir()
	logPath := filepath.Join(tmp, "logs", "proxy.log")
	oldOutput := log.Writer()
	defer log.SetOutput(oldOutput)

	logFile, err := setupLogFile(logPath)
	if err != nil {
		t.Fatalf("setupLogFile error = %v", err)
	}
	log.Print("first")
	logFile.Close()

	logFile, err = setupLogFile(logPath)
	if err != nil {
		t.Fatalf("second setupLogFile error = %v", err)
	}
	log.Print("second")
	logFile.Close()

	content, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("read log file: %v", err)
	}
	if !strings.Contains(string(content), "first") || !strings.Contains(string(content), "second") {
		t.Fatalf("log content = %s", content)
	}
}
