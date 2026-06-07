package main

import (
	"fmt"
	"log"
	"net/http"
	"os"
	"time"

	"anthropic-proxy/internal/config"
	"anthropic-proxy/internal/daemon"
	"anthropic-proxy/internal/handler"
	"anthropic-proxy/internal/store"
)

const usage = `Usage: anthropic-proxy <command> [flags]

Commands:
  start       start proxy as daemon (default, use -fg for foreground)
  stop        stop running daemon
  restart     restart daemon
  status      show daemon status

Flags (for start/restart):
  -config string    path to config file (default "config.yaml")
  -port int         listen port (overrides config)
  -url string       backend URL (overrides config)
  -key string       backend API key (overrides config)
  -skip-thinking    skip thinking blocks (overrides config)
  -debug            enable debug logging (overrides config)
  -fg               run in foreground (not as daemon)
`

func main() {
	// Handle subcommands before flag.Parse()
	if len(os.Args) > 1 {
		// Handle help flags
		if os.Args[1] == "-h" || os.Args[1] == "--help" {
			fmt.Print(usage)
			return
		}

		switch os.Args[1] {
		case "stop":
			if err := daemon.Stop(); err != nil {
				fmt.Fprintf(os.Stderr, "Error: %v\n", err)
				os.Exit(1)
			}
			fmt.Println("anthropic-proxy stopped")
			return

		case "status":
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

		case "restart":
			if err := daemon.Stop(); err != nil {
				fmt.Fprintf(os.Stderr, "Warning: %v\n", err)
			}
			fmt.Println("Restarting anthropic-proxy...")
			// Remove subcommand so flag.Parse() sees only flags
			os.Args = append(os.Args[:1], os.Args[2:]...)

		case "start":
			// Remove subcommand so flag.Parse() sees only flags
			os.Args = append(os.Args[:1], os.Args[2:]...)

		default:
			fmt.Fprintf(os.Stderr, "Unknown command: %s\n\n", os.Args[1])
			fmt.Fprint(os.Stderr, usage)
			os.Exit(1)
		}
	}

	// Load configuration (start/restart path)
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	// If not running in foreground and not already a daemon, fork to background
	if !cfg.Server.Fg && !daemon.IsDaemon() {
		daemon.Daemonize()
		return
	}

	runServer(cfg)
}

func runServer(cfg *config.Config) {
	// Setup logging to file when running as daemon
	if daemon.IsDaemon() {
		logFile := daemon.SetupLog()
		if logFile != nil {
			defer logFile.Close()
		}
		daemon.WritePIDFile()
	}

	// Create response store for previous_response_id support
	ttl := time.Duration(cfg.Proxy.StoreTTL) * time.Second
	if ttl <= 0 {
		ttl = 1 * time.Hour
	}
	maxEntries := cfg.Proxy.StoreMaxEntries
	if maxEntries <= 0 {
		maxEntries = 1000
	}
	responseStore := store.New(ttl, maxEntries)
	log.Printf("Response store: TTL=%v, maxEntries=%d", ttl, maxEntries)

	// Log model mappings
	modelMap := cfg.GetModelMap()
	if len(modelMap) > 0 {
		log.Printf("Model mapping:")
		for from, to := range modelMap {
			log.Printf("  %s → %s", from, to)
		}
	}

	// Create handler
	h := handler.New(cfg, responseStore)

	// Register routes — Anthropic
	http.HandleFunc("/anthropic/v1/messages", h.MessagesHandler)
	http.HandleFunc("/anthropic/v1/models", h.ModelsHandler)

	// Register routes — OpenAI
	http.HandleFunc("/openai/v1/responses", h.ResponsesHandler)
	http.HandleFunc("/openai/v1/chat/completions", h.ChatCompletionsHandler)
	http.HandleFunc("/openai/v1/models", h.ModelsHandler)
	http.HandleFunc("/openai/v1/embeddings", h.DirectForwardHandler("/v1/embeddings"))
	http.HandleFunc("/openai/v1/rerank", h.DirectForwardHandler("/v1/rerank"))
	http.HandleFunc("/openai/v1/audio/speech", h.DirectForwardHandler("/v1/audio/speech"))
	http.HandleFunc("/openai/v1/audio/transcriptions", h.DirectForwardHandler("/v1/audio/transcriptions"))
	http.HandleFunc("/openai/v1/images/generations", h.DirectForwardHandler("/v1/images/generations"))

	// Register routes — Gemini
	http.HandleFunc("/gemini/v1beta/models/", h.GeminiHandler)

	// Start server
	log.Printf("anthropic-proxy listening on :%d → %s", cfg.Server.Port, cfg.Backend.URL)
	if err := http.ListenAndServe(fmt.Sprintf(":%d", cfg.Server.Port), nil); err != nil {
		log.Fatalf("Server failed: %v", err)
		os.Exit(1)
	}
}
