package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

// Global debug flag
var debug bool

func main() {
	// Load .env file first (ignore error if not found)
	loadDotEnv(".env")

	// CLI flags override .env and env vars
	port := flag.String("port", getVal("PORT", "8006"), "listen port")
	baseURL := flag.String("url", getVal("OPENAI_BASE_URL", "http://localhost:11434"), "OpenAI-compatible backend URL")
	apiKey := flag.String("key", getVal("OPENAI_API_KEY", ""), "API key for backend")
	skipThinking := flag.Bool("skip-thinking", false, "skip reasoning/thinking blocks")
	modelMapStr := flag.String("model-map", getVal("MODEL_MAP", ""), "model mapping: client_model:backend_model,...")
	debugFlag := flag.Bool("debug", false, "enable debug logging")
	fg := flag.Bool("fg", false, "run in foreground (default: run as background daemon)")
	flag.Parse()

	debug = *debugFlag

	// If not running in foreground and not already a daemon, fork to background
	if !*fg && os.Getenv("_ANTHROPIC_PROXY_DAEMON") == "" {
		daemonize()
		return
	}

	// Setup logging to file when running as daemon
	if os.Getenv("_ANTHROPIC_PROXY_DAEMON") != "" {
		logFile := setupDaemonLog()
		if logFile != nil {
			defer logFile.Close()
		}
		writePIDFile()
	}

	// Parse model mapping
	modelMap := parseModelMap(*modelMapStr)
	if len(modelMap) > 0 {
		log.Printf("Model mapping:")
		for from, to := range modelMap {
			log.Printf("  %s → %s", from, to)
		}
	}

	http.HandleFunc("/v1/models", makeModelsHandler(*baseURL, *apiKey))
	http.HandleFunc("/v1/messages", makeHandler(*baseURL, *apiKey, *skipThinking, modelMap))

	log.Printf("anthropic-proxy listening on :%s → %s", *port, *baseURL)
	log.Fatal(http.ListenAndServe(":"+*port, nil))
}

// daemonize re-launches the process in the background
func daemonize() {
	execPath, err := os.Executable()
	if err != nil {
		log.Fatalf("failed to get executable path: %v", err)
	}

	// Build args, append marker env
	args := os.Args[1:]

	// Find the log file path
	logDir := filepath.Dir(execPath)
	logPath := filepath.Join(logDir, "anthropic-proxy.log")

	// Open log file for daemon output
	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		// Fallback: try current directory
		logFile, err = os.OpenFile("anthropic-proxy.log", os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
		if err != nil {
			fmt.Fprintf(os.Stderr, "failed to open log file: %v\n", err)
			os.Exit(1)
		}
	}

	// Re-exec self with _ANTHROPIC_PROXY_DAEMON=1
	env := append(os.Environ(), "_ANTHROPIC_PROXY_DAEMON=1")

	proc, err := os.StartProcess(execPath, append([]string{execPath}, args...), &os.ProcAttr{
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
	fmt.Printf("PID file: %s\n", filepath.Join(filepath.Dir(execPath), "anthropic-proxy.pid"))
	fmt.Printf("Stop:     kill %d\n", proc.Pid)
	proc.Release()
}

// setupDaemonLog redirects log output to file
func setupDaemonLog() *os.File {
	logPath := "anthropic-proxy.log"
	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return nil
	}
	log.SetOutput(logFile)
	return logFile
}

// writePIDFile writes the current PID to a file
func writePIDFile() {
	pidPath := "anthropic-proxy.pid"
	err := os.WriteFile(pidPath, []byte(fmt.Sprintf("%d\n", os.Getpid())), 0644)
	if err != nil {
		log.Printf("warning: failed to write PID file: %v", err)
	}
}

// parseModelMap parses "claude-opus-4-8:mimo-v2.5-pro,claude-sonnet-4-6:laguna-m.1" into a map
func parseModelMap(s string) map[string]string {
	m := make(map[string]string)
	if s == "" {
		return m
	}
	for _, pair := range strings.Split(s, ",") {
		pair = strings.TrimSpace(pair)
		if pair == "" {
			continue
		}
		parts := strings.SplitN(pair, ":", 2)
		if len(parts) != 2 {
			continue
		}
		from := strings.TrimSpace(parts[0])
		to := strings.TrimSpace(parts[1])
		if from != "" && to != "" {
			m[from] = to
		}
	}
	return m
}

// getVal checks env var first, then returns fallback
func getVal(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

// loadDotEnv reads a .env file and sets env vars (simple KEY=VALUE, ignores comments and blanks)
func loadDotEnv(path string) {
	f, err := os.Open(path)
	if err != nil {
		return
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 {
			continue
		}
		key := strings.TrimSpace(parts[0])
		val := strings.TrimSpace(parts[1])
		// Remove surrounding quotes if any
		if len(val) >= 2 && (val[0] == '"' && val[len(val)-1] == '"' || val[0] == '\'' && val[len(val)-1] == '\'') {
			val = val[1 : len(val)-1]
		}
		// Only set if not already set (env vars take precedence)
		if os.Getenv(key) == "" {
			os.Setenv(key, val)
		}
	}
}

// makeModelsHandler creates a handler that proxies GET /v1/models to the backend
func makeModelsHandler(baseURL, apiKey string) http.HandlerFunc {
	client := &http.Client{Timeout: 30 * time.Second}
	base := strings.TrimRight(baseURL, "/")
	if strings.HasSuffix(base, "/v1") {
		base = base[:len(base)-3]
	}
	modelsURL := base + "/v1/models"

	return func(w http.ResponseWriter, r *http.Request) {
		log.Printf("→ GET %s", modelsURL)

		proxyReq, err := http.NewRequest("GET", modelsURL, nil)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "api_error", err.Error())
			return
		}

		if apiKey != "" {
			proxyReq.Header.Set("Authorization", "Bearer "+apiKey)
		}

		resp, err := client.Do(proxyReq)
		if err != nil {
			writeError(w, http.StatusBadGateway, "api_error", "backend error: "+err.Error())
			return
		}
		defer resp.Body.Close()

		body, err := io.ReadAll(resp.Body)
		if err != nil {
			writeError(w, http.StatusBadGateway, "api_error", "failed to read backend response")
			return
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(resp.StatusCode)
		w.Write(body)
	}
}

func makeHandler(baseURL, apiKey string, skipThinking bool, modelMap map[string]string) http.HandlerFunc {
	client := &http.Client{Timeout: 5 * time.Minute}
	base := strings.TrimRight(baseURL, "/")
	if strings.HasSuffix(base, "/v1") {
		base = base[:len(base)-3]
	}
	targetURL := base + "/v1/chat/completions"

	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, `{"type":"error","error":{"type":"invalid_request_error","message":"method not allowed"}}`, http.StatusMethodNotAllowed)
			return
		}

		body, err := io.ReadAll(r.Body)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid_request_error", "failed to read body")
			return
		}
		defer r.Body.Close()

		var anthropicReq AnthropicRequest
		if err := json.Unmarshal(body, &anthropicReq); err != nil {
			writeError(w, http.StatusBadRequest, "invalid_request_error", "invalid JSON: "+err.Error())
			return
		}

		// Apply model mapping
		if mapped, ok := modelMap[anthropicReq.Model]; ok {
			if debug {
				log.Printf("Model mapping: %s → %s", anthropicReq.Model, mapped)
			}
			anthropicReq.Model = mapped
		}

		openaiReq, thinkingEnabled := TranslateRequest(&anthropicReq)

		reqBody, _ := json.Marshal(openaiReq)
		if debug {
			log.Printf("→ %s %s model=%s stream=%v thinking=%v", r.Method, targetURL, openaiReq.Model, openaiReq.Stream, thinkingEnabled)
		}

		proxyReq, err := http.NewRequest("POST", targetURL, bytes.NewReader(reqBody))
		if err != nil {
			writeError(w, http.StatusInternalServerError, "api_error", err.Error())
			return
		}
		proxyReq.Header.Set("Content-Type", "application/json")

		// Always use configured API key for backend
		if apiKey != "" {
			proxyReq.Header.Set("Authorization", "Bearer "+apiKey)
		}

		resp, err := client.Do(proxyReq)
		if err != nil {
			writeError(w, http.StatusBadGateway, "api_error", "backend error: "+err.Error())
			return
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(resp.StatusCode)
			io.Copy(w, resp.Body)
			return
		}

		if anthropicReq.Stream {
			shouldSkipThinking := skipThinking || !thinkingEnabled
			streamResponse(w, resp, shouldSkipThinking)
		} else {
			nonStreamResponse(w, resp)
		}
	}
}

func writeError(w http.ResponseWriter, status int, errType, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"type":  "error",
		"error": map[string]string{"type": errType, "message": message},
	})
}

func nonStreamResponse(w http.ResponseWriter, resp *http.Response) {
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		writeError(w, http.StatusBadGateway, "api_error", "failed to read backend response")
		return
	}

	// Trim whitespace/padding that some backends prepend
	body = bytes.TrimSpace(body)

	var openaiResp OpenAIResponse
	if err := json.Unmarshal(body, &openaiResp); err != nil {
		writeError(w, http.StatusBadGateway, "api_error", "failed to parse backend response")
		return
	}

	anthropicResp := TranslateResponse(&openaiResp)

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("anthropic-version", "2023-06-01")
	w.Header().Set("request-id", fmt.Sprintf("req-%d", time.Now().UnixNano()))
	json.NewEncoder(w).Encode(anthropicResp)
}

func streamResponse(w http.ResponseWriter, resp *http.Response, skipThinking bool) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, "api_error", "streaming not supported")
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("anthropic-version", "2023-06-01")
	w.Header().Set("request-id", fmt.Sprintf("req-%d", time.Now().UnixNano()))

	// Ratelimit headers (dummy values)
	w.Header().Set("anthropic-ratelimit-requests-limit", "1000")
	w.Header().Set("anthropic-ratelimit-requests-remaining", "999")
	w.Header().Set("anthropic-ratelimit-requests-reset", "2026-01-01T00:00:00Z")
	w.Header().Set("anthropic-ratelimit-tokens-limit", "100000")
	w.Header().Set("anthropic-ratelimit-tokens-remaining", "99999")
	w.Header().Set("anthropic-ratelimit-tokens-reset", "2026-01-01T00:00:00Z")

	// Retry headers
	w.Header().Set("retry-after", "1")

	emit := func(event string, data interface{}) {
		j, _ := json.Marshal(data)
		if debug {
			log.Printf("← SSE event=%s data=%s", event, string(j))
		}
		fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event, string(j))
		flusher.Flush()
	}

	translator := NewStreamTranslator(emit, skipThinking)

	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		data := strings.TrimPrefix(line, "data: ")
		if data == "[DONE]" {
			break
		}

		var chunk OpenAIChunk
		if err := json.Unmarshal([]byte(data), &chunk); err != nil {
			log.Printf("skip unparseable chunk: %v", err)
			continue
		}
		translator.ProcessChunk(&chunk)
	}

	if err := scanner.Err(); err != nil {
		log.Printf("stream read error: %v", err)
	}
}
