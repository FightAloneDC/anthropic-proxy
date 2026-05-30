package handler

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"

	"anthropic-proxy/internal/config"
	"anthropic-proxy/internal/translator"
	"anthropic-proxy/internal/types"
)

// Handler holds the HTTP handlers for the proxy
type Handler struct {
	cfg    *config.Config
	client *http.Client
}

// New creates a new Handler instance
func New(cfg *config.Config) *Handler {
	return &Handler{
		cfg: cfg,
		client: &http.Client{
			Timeout: 5 * time.Minute,
		},
	}
}

// ModelsHandler handles GET /v1/models
func (h *Handler) ModelsHandler(w http.ResponseWriter, r *http.Request) {
	base := strings.TrimRight(h.cfg.Backend.URL, "/")
	if strings.HasSuffix(base, "/v1") {
		base = base[:len(base)-3]
	}
	modelsURL := base + "/v1/models"

	log.Printf("→ GET %s", modelsURL)

	proxyReq, err := http.NewRequest("GET", modelsURL, nil)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "api_error", err.Error())
		return
	}

	if h.cfg.Backend.APIKey != "" {
		proxyReq.Header.Set("Authorization", "Bearer "+h.cfg.Backend.APIKey)
	}

	resp, err := h.client.Do(proxyReq)
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

// MessagesHandler handles POST /v1/messages
func (h *Handler) MessagesHandler(w http.ResponseWriter, r *http.Request) {
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

	var anthropicReq types.AnthropicRequest
	if err := json.Unmarshal(body, &anthropicReq); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request_error", "invalid JSON: "+err.Error())
		return
	}

	// Apply model mapping
	modelMap := h.cfg.GetModelMap()
	if mapped, ok := modelMap[anthropicReq.Model]; ok {
		if h.cfg.Proxy.Debug {
			log.Printf("Model mapping: %s → %s", anthropicReq.Model, mapped)
		}
		anthropicReq.Model = mapped
	}

	openaiReq, thinkingEnabled := translator.TranslateRequest(&anthropicReq)

	base := strings.TrimRight(h.cfg.Backend.URL, "/")
	if strings.HasSuffix(base, "/v1") {
		base = base[:len(base)-3]
	}
	targetURL := base + "/v1/chat/completions"

	reqBody, _ := json.Marshal(openaiReq)
	if h.cfg.Proxy.Debug {
		log.Printf("→ %s %s model=%s stream=%v thinking=%v", r.Method, targetURL, openaiReq.Model, openaiReq.Stream, thinkingEnabled)
	}

	proxyReq, err := http.NewRequest("POST", targetURL, bytes.NewReader(reqBody))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "api_error", err.Error())
		return
	}
	proxyReq.Header.Set("Content-Type", "application/json")

	// Always use configured API key for backend
	if h.cfg.Backend.APIKey != "" {
		proxyReq.Header.Set("Authorization", "Bearer "+h.cfg.Backend.APIKey)
	}

	resp, err := h.client.Do(proxyReq)
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
		shouldSkipThinking := h.cfg.Proxy.SkipThinking || !thinkingEnabled
		h.streamResponse(w, resp, shouldSkipThinking)
	} else {
		h.nonStreamResponse(w, resp)
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

func (h *Handler) nonStreamResponse(w http.ResponseWriter, resp *http.Response) {
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		writeError(w, http.StatusBadGateway, "api_error", "failed to read backend response")
		return
	}

	// Trim whitespace/padding that some backends prepend
	body = bytes.TrimSpace(body)

	var openaiResp types.OpenAIResponse
	if err := json.Unmarshal(body, &openaiResp); err != nil {
		writeError(w, http.StatusBadGateway, "api_error", "failed to parse backend response")
		return
	}

	anthropicResp := translator.TranslateResponse(&openaiResp)

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("anthropic-version", "2023-06-01")
	w.Header().Set("request-id", fmt.Sprintf("req-%d", time.Now().UnixNano()))
	json.NewEncoder(w).Encode(anthropicResp)
}

func (h *Handler) streamResponse(w http.ResponseWriter, resp *http.Response, skipThinking bool) {
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
		if h.cfg.Proxy.Debug {
			log.Printf("← SSE event=%s data=%s", event, string(j))
		}
		fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event, string(j))
		flusher.Flush()
	}

	translator := translator.NewStreamTranslator(emit, skipThinking)

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

		var chunk types.OpenAIChunk
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
