package main

import (
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"

	"anthropic-proxy/internal/config"
	"anthropic-proxy/internal/daemon"
	"anthropic-proxy/internal/handler"
)

func main() {
	// Check for control flags before loading config
	stopFlag := flag.Bool("stop", false, "stop running daemon")
	statusFlag := flag.Bool("status", false, "show daemon status")
	flag.Parse()

	// Handle --stop command
	if *stopFlag {
		if err := daemon.Stop(); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
		fmt.Println("anthropic-proxy stopped")
		return
	}

	// Handle --status command
	if *statusFlag {
		pid, running, err := daemon.Status()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
		if running {
			fmt.Printf("anthropic-proxy is running (PID %d)\n", pid)
		} else {
			fmt.Printf("anthropic-proxy is not running (stale PID file, PID %d)\n", pid)
			os.Exit(1)
		}
		return
	}

	// Load configuration
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	// If not running in foreground and not already a daemon, fork to background
	if !cfg.Server.Fg && !daemon.IsDaemon() {
		daemon.Daemonize()
		return
	}

	// Setup logging to file when running as daemon
	if daemon.IsDaemon() {
		logFile := daemon.SetupLog()
		if logFile != nil {
			defer logFile.Close()
		}
		daemon.WritePIDFile()
	}

	// Log model mappings
	modelMap := cfg.GetModelMap()
	if len(modelMap) > 0 {
		log.Printf("Model mapping:")
		for from, to := range modelMap {
			log.Printf("  %s → %s", from, to)
		}
	}

	// Create handler
	h := handler.New(cfg)

	// Register routes
	http.HandleFunc("/v1/models", h.ModelsHandler)
	http.HandleFunc("/v1/messages", h.MessagesHandler)

	// Start server
	log.Printf("anthropic-proxy listening on :%d → %s", cfg.Server.Port, cfg.Backend.URL)
	if err := http.ListenAndServe(":"+itoa(cfg.Server.Port), nil); err != nil {
		log.Fatalf("Server failed: %v", err)
		os.Exit(1)
	}
}

func itoa(i int) string {
	return fmt.Sprintf("%d", i)
}
