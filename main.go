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
	"strings"
	"time"
)

func main() {
	// Load .env file first (ignore error if not found)
	loadDotEnv(".env")

	// CLI flags override .env and env vars
	port := flag.String("port", getVal("PORT", "8006"), "listen port")
	baseURL := flag.String("url", getVal("OPENAI_BASE_URL", "http://localhost:11434"), "OpenAI-compatible backend URL")
	apiKey := flag.String("key", getVal("OPENAI_API_KEY", ""), "API key for backend")
	skipThinking := flag.Bool("skip-thinking", false, "skip reasoning/thinking blocks")
	flag.Parse()

	http.HandleFunc("/v1/messages", makeHandler(*baseURL, *apiKey, *skipThinking))

	log.Printf("anthropic-proxy listening on :%s → %s", *port, *baseURL)
	log.Fatal(http.ListenAndServe(":"+*port, nil))
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

func makeHandler(baseURL, apiKey string, skipThinking bool) http.HandlerFunc {
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

		openaiReq, thinkingEnabled := TranslateRequest(&anthropicReq)

		reqBody, _ := json.Marshal(openaiReq)
		log.Printf("→ %s %s model=%s stream=%v thinking=%v", r.Method, targetURL, openaiReq.Model, openaiReq.Stream, thinkingEnabled)

		proxyReq, err := http.NewRequest("POST", targetURL, bytes.NewReader(reqBody))
		if err != nil {
			writeError(w, http.StatusInternalServerError, "api_error", err.Error())
			return
		}
		proxyReq.Header.Set("Content-Type", "application/json")

		// Always use configured API key for backend
		// Client can send any key (or none) - proxy handles auth
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
			// Skip thinking if CLI flag is set OR if thinking is disabled in request
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
		log.Printf("← SSE event=%s data=%s", event, string(j))
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
