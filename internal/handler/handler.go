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

	"anthropic-proxy/internal/backend"
	"anthropic-proxy/internal/config"
	"anthropic-proxy/internal/reliability"
	"anthropic-proxy/internal/store"
	"anthropic-proxy/internal/translator"
	"anthropic-proxy/internal/types"
)

const (
	maxRequestBodySize  = 10 << 20 // 10 MB
	maxResponseBodySize = 100 << 20 // 100 MB
)

// Handler holds the HTTP handlers for the proxy
type Handler struct {
	cfg      *config.Config
	client   *http.Client
	store    store.Store
	metrics  *Metrics
	logger   *Logger
	breaker  *reliability.CircuitBreaker
	limiter  *reliability.RateLimiter
	health   *reliability.HealthMonitor
	executor *reliability.BackendExecutor
	router   *backend.Router
}

// Shutdown stops background goroutines (health monitor, rate limiter cleanup).
func (h *Handler) Shutdown() {
	if h.health != nil {
		h.health.Stop()
	}
	if h.limiter != nil {
		h.limiter.Stop()
	}
}

// New creates a new Handler instance
func New(cfg *config.Config, s store.Store) *Handler {
	_ = cfg.NormalizeBackends()
	backendURL := cfg.Backend.URL
	backendAPIKey := cfg.Backend.APIKey
	if backends := cfg.EffectiveBackends(); len(backends) > 0 {
		backendURL = backends[0].URL
		backendAPIKey = backends[0].APIKey
	}
	client := &http.Client{Timeout: 5 * time.Minute}
	breaker := reliability.NewCircuitBreaker(
		cfg.Proxy.CircuitBreakerEnabled,
		cfg.Proxy.CircuitBreakerFailureThreshold,
		time.Duration(cfg.Proxy.CircuitBreakerCooldown)*time.Second,
	)
	executor := &reliability.BackendExecutor{
		Client:  client,
		Breaker: breaker,
		Config: reliability.RetryConfig{
			Enabled:        cfg.Proxy.RetryEnabled,
			MaxAttempts:    cfg.Proxy.RetryMaxAttempts,
			InitialBackoff: time.Duration(cfg.Proxy.RetryInitialBackoffMS) * time.Millisecond,
			MaxBackoff:     time.Duration(cfg.Proxy.RetryMaxBackoffMS) * time.Millisecond,
		},
	}
	healthMonitor := reliability.NewHealthMonitor(
		cfg.Proxy.BackendHealthEnabled || cfg.Proxy.HealthBackendCheck,
		backendURL,
		backendAPIKey,
		time.Duration(cfg.Proxy.BackendHealthInterval)*time.Second,
		time.Duration(cfg.Proxy.BackendHealthTimeout)*time.Second,
		breaker,
	)
	healthMonitor.Start()
	return &Handler{
		cfg:      cfg,
		client:   client,
		store:    s,
		metrics:  NewMetrics(),
		logger:   NewLogger(cfg.Proxy.LogFormat, cfg.Proxy.LogLevel),
		breaker:  breaker,
		limiter:  reliability.NewRateLimiter(cfg.Proxy.RateLimitEnabled, cfg.Proxy.RateLimitRequestsPerMinute, cfg.Proxy.RateLimitBurst),
		health:   healthMonitor,
		executor: executor,
		router:   backend.NewRouter(cfg.EffectiveBackends(), cfg.Proxy.LoadBalanceStrategy),
	}
}

// ModelsHandler handles GET /anthropic/v1/models
func (h *Handler) ModelsHandler(w http.ResponseWriter, r *http.Request) {
	if h.cfg.Proxy.Debug {
		apiKey := r.Header.Get("X-Api-Key")
		log.Printf("← %s %s x-api-key=%s", r.Method, r.URL.Path, maskKey(apiKey))
	}

	targets := make([]*backendTarget, 0)
	for _, runtime := range h.router.Backends() {
		targets = append(targets, &backendTarget{
			name:   runtime.Backend.Name,
			url:    backend.JoinURL(runtime.Backend.URL, "/v1/models"),
			apiKey: runtime.Backend.APIKey,
		})
	}
	if len(targets) == 0 {
		writeError(w, http.StatusBadGateway, "api_error", "no backend configured")
		return
	}

	bodies := make([][]byte, 0, len(targets))
	var lastStatus int
	for _, target := range targets {
		if h.cfg.Proxy.Debug {
			log.Printf("→ GET %s backend=%s", target.url, target.name)
		}

		proxyReq, err := http.NewRequest("GET", target.url, nil)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "api_error", err.Error())
			return
		}

		if target.apiKey != "" {
			proxyReq.Header.Set("Authorization", "Bearer "+target.apiKey)
		}

		resp, err := h.client.Do(proxyReq)
		if err != nil {
			lastStatus = http.StatusBadGateway
			continue
		}
		body, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBodySize))
		resp.Body.Close()
		if err != nil {
			lastStatus = http.StatusBadGateway
			continue
		}
		lastStatus = resp.StatusCode
		if resp.StatusCode == http.StatusOK {
			bodies = append(bodies, body)
		}
	}
	if len(bodies) == 0 {
		if lastStatus == 0 {
			lastStatus = http.StatusBadGateway
		}
		writeError(w, lastStatus, "api_error", "failed to fetch backend models")
		return
	}

	body := h.mergeModelsResponses(bodies)
	body = h.enrichModelsResponse(body)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	w.Write(body)
}

// MessagesHandler handles POST /anthropic/v1/messages
func (h *Handler) MessagesHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, `{"type":"error","error":{"type":"invalid_request_error","message":"method not allowed"}}`, http.StatusMethodNotAllowed)
		return
	}

	// Read Anthropic-specific headers
	apiKey := r.Header.Get("X-Api-Key")
	anthropicVersion := r.Header.Get("anthropic-version")

	if h.cfg.Proxy.Debug {
		log.Printf("← %s %s x-api-key=%s anthropic-version=%s", r.Method, r.URL.Path, maskKey(apiKey), anthropicVersion)
	}

	body, err := io.ReadAll(io.LimitReader(r.Body, maxRequestBodySize))
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

	targets, err := h.modelTargets(openaiReq.Model, "/v1/chat/completions")
	if err != nil {
		writeBackendError(w, "anthropic", err)
		return
	}

	reqBody, _ := json.Marshal(openaiReq)
	if h.cfg.Proxy.Debug {
		log.Printf("→ %s %s model=%s stream=%v thinking=%v", r.Method, targets[0].url, openaiReq.Model, openaiReq.Stream, thinkingEnabled)
	}

	var resp *http.Response
	if anthropicReq.Stream {
		resp, err = h.doStreamingBackendTargets(targets, "POST", reqBody, requestID(r))
	} else {
		resp, err = h.doBackendTargets(targets, "POST", reqBody, requestID(r), true)
	}
	if err != nil {
		writeBackendError(w, "anthropic", err)
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
		h.streamResponse(w, resp, anthropicVersion, shouldSkipThinking)
	} else {
		h.nonStreamResponse(w, resp, anthropicVersion)
	}
}

// maskKey returns a masked version of an API key for logging
func maskKey(key string) string {
	if key == "" {
		return "(none)"
	}
	if len(key) <= 8 {
		return "***"
	}
	return key[:4] + "..." + key[len(key)-4:]
}

func writeError(w http.ResponseWriter, status int, errType, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"type":  "error",
		"error": map[string]string{"type": errType, "message": message},
	})
}

func (h *Handler) nonStreamResponse(w http.ResponseWriter, resp *http.Response, anthropicVersion string) {
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBodySize))
	if err != nil {
		writeError(w, http.StatusBadGateway, "api_error", "failed to read backend response")
		return
	}

	// Trim whitespace/padding that some backends prepend
	body = bytes.TrimSpace(body)

	var openaiResp types.OpenAIResponse
	if isSSEResponse(body) {
		// Backend returned SSE format for a non-streaming request — accumulate chunks
		if accumulated, ok := parseSSEToOpenAIResponse(body); ok {
			openaiResp = *accumulated
		} else {
			writeError(w, http.StatusBadGateway, "api_error", "failed to parse backend SSE response")
			return
		}
	} else if err := json.Unmarshal(body, &openaiResp); err != nil {
		writeError(w, http.StatusBadGateway, "api_error", "failed to parse backend response")
		return
	}

	anthropicResp := translator.TranslateResponse(&openaiResp)

	w.Header().Set("Content-Type", "application/json")
	if anthropicVersion != "" {
		w.Header().Set("anthropic-version", anthropicVersion)
	} else {
		w.Header().Set("anthropic-version", "2023-06-01")
	}
	if w.Header().Get("request-id") == "" {
		w.Header().Set("request-id", fmt.Sprintf("req-%d", time.Now().UnixNano()))
	}
	json.NewEncoder(w).Encode(anthropicResp)
}

func (h *Handler) streamResponse(w http.ResponseWriter, resp *http.Response, anthropicVersion string, skipThinking bool) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, "api_error", "streaming not supported")
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	if anthropicVersion != "" {
		w.Header().Set("anthropic-version", anthropicVersion)
	} else {
		w.Header().Set("anthropic-version", "2023-06-01")
	}
	if w.Header().Get("request-id") == "" {
		w.Header().Set("request-id", fmt.Sprintf("req-%d", time.Now().UnixNano()))
	}

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

// ============================================================
// OpenAI Responses API handlers
// ============================================================

// ResponsesHandler handles POST /openai/v1/responses
func (h *Handler) ResponsesHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeResponsesError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
		return
	}

	if h.cfg.Proxy.Debug {
		log.Printf("← %s %s", r.Method, r.URL.Path)
	}

	body, err := io.ReadAll(io.LimitReader(r.Body, maxRequestBodySize))
	if err != nil {
		writeResponsesError(w, http.StatusBadRequest, "invalid_request_error", "failed to read body")
		return
	}
	defer r.Body.Close()

	var responsesReq types.ResponsesRequest
	if err := json.Unmarshal(body, &responsesReq); err != nil {
		writeResponsesError(w, http.StatusBadRequest, "invalid_request_error", "invalid JSON: "+err.Error())
		return
	}

	// Handle previous_response_id
	var prevMessages []types.OpenAIMsg
	if responsesReq.PreviousResponseID != "" {
		if prev, ok := h.store.Get(responsesReq.PreviousResponseID); ok {
			prevMessages = translator.StoredResponseToMessages(prev)
		} else {
			writeResponsesError(w, http.StatusNotFound, "not_found", "previous response not found: "+responsesReq.PreviousResponseID)
			return
		}
	}

	// Apply model mapping
	modelMap := h.cfg.GetModelMap()
	if mapped, ok := modelMap[responsesReq.Model]; ok {
		if h.cfg.Proxy.Debug {
			log.Printf("Model mapping: %s → %s", responsesReq.Model, mapped)
		}
		responsesReq.Model = mapped
	}

	responseID := translator.GenerateResponseID()
	openaiReq, _ := translator.TranslateResponsesRequest(&responsesReq, prevMessages)

	targets, err := h.modelTargets(openaiReq.Model, "/v1/chat/completions")
	if err != nil {
		writeBackendError(w, "responses", err)
		return
	}

	reqBody, _ := json.Marshal(openaiReq)
	if h.cfg.Proxy.Debug {
		log.Printf("→ POST %s model=%s stream=%v", targets[0].url, openaiReq.Model, openaiReq.Stream)
	}

	var resp *http.Response
	if responsesReq.Stream {
		resp, err = h.doStreamingBackendTargets(targets, "POST", reqBody, requestID(r))
	} else {
		resp, err = h.doBackendTargets(targets, "POST", reqBody, requestID(r), true)
	}
	if err != nil {
		writeBackendError(w, "responses", err)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(resp.StatusCode)
		io.Copy(w, resp.Body)
		return
	}

	if responsesReq.Stream {
		h.responsesStreamResponse(w, resp, responseID)
	} else {
		h.responsesNonStreamResponse(w, resp, responseID)
	}
}

func (h *Handler) responsesNonStreamResponse(w http.ResponseWriter, resp *http.Response, responseID string) {
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBodySize))
	if err != nil {
		writeResponsesError(w, http.StatusBadGateway, "api_error", "failed to read backend response")
		return
	}
	body = bytes.TrimSpace(body)

	var openaiResp types.OpenAIResponse
	if isSSEResponse(body) {
		if accumulated, ok := parseSSEToOpenAIResponse(body); ok {
			openaiResp = *accumulated
		} else {
			writeResponsesError(w, http.StatusBadGateway, "api_error", "failed to parse backend SSE response")
			return
		}
	} else if err := json.Unmarshal(body, &openaiResp); err != nil {
		writeResponsesError(w, http.StatusBadGateway, "api_error", "failed to parse backend response")
		return
	}

	responsesResp := translator.TranslateResponsesResponse(&openaiResp, responseID)

	// Store for previous_response_id support
	h.store.Store(responseID, responsesResp)

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("x-request-id", responseID)
	json.NewEncoder(w).Encode(responsesResp)
}

func (h *Handler) responsesStreamResponse(w http.ResponseWriter, resp *http.Response, responseID string) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeResponsesError(w, http.StatusInternalServerError, "api_error", "streaming not supported")
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("x-request-id", responseID)

	emit := func(event string, data interface{}) {
		j, _ := json.Marshal(data)
		if h.cfg.Proxy.Debug {
			log.Printf("← SSE event=%s data=%s", event, string(j))
		}
		fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event, string(j))
		flusher.Flush()
	}

	streamTranslator := translator.NewResponsesStreamTranslator(emit, responseID)

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
		streamTranslator.ProcessChunk(&chunk)
	}

	if err := scanner.Err(); err != nil {
		log.Printf("stream read error: %v", err)
	}

	// Store for previous_response_id support
	if finalResp := streamTranslator.FinalResponse(); finalResp != nil {
		h.store.Store(responseID, finalResp)
	}
}

func writeResponsesError(w http.ResponseWriter, status int, errType, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"type":  "error",
		"error": map[string]string{"type": errType, "message": message},
	})
}

// ============================================================
// OpenAI Chat Completions direct forward
// ============================================================

// ChatCompletionsHandler handles POST /openai/v1/chat/completions — direct forward to backend.
func (h *Handler) ChatCompletionsHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, `{"error":{"message":"method not allowed","type":"invalid_request_error"}}`, http.StatusMethodNotAllowed)
		return
	}

	if h.cfg.Proxy.Debug {
		log.Printf("← %s %s (direct forward)", r.Method, r.URL.Path)
	}

	body, err := io.ReadAll(io.LimitReader(r.Body, maxRequestBodySize))
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]interface{}{"error": map[string]string{"message": "failed to read body"}})
		return
	}
	defer r.Body.Close()

	// Parse to check stream flag and apply model mapping.
	var reqCheck struct {
		Model  string `json:"model"`
		Stream bool   `json:"stream"`
	}
	json.Unmarshal(body, &reqCheck)
	if mapped, ok := h.cfg.GetModelMap()[reqCheck.Model]; ok {
		if h.cfg.Proxy.Debug {
			log.Printf("Model mapping: %s → %s", reqCheck.Model, mapped)
		}
		var payload map[string]interface{}
		if err := json.Unmarshal(body, &payload); err == nil {
			payload["model"] = mapped
			if mappedBody, err := json.Marshal(payload); err == nil {
				body = mappedBody
				reqCheck.Model = mapped
			}
		}
	}

	targets, err := h.modelTargets(reqCheck.Model, "/v1/chat/completions")
	if err != nil {
		writeBackendError(w, "openai", err)
		return
	}

	if h.cfg.Proxy.Debug {
		log.Printf("→ POST %s model=%s stream=%v", targets[0].url, reqCheck.Model, reqCheck.Stream)
	}

	var resp *http.Response
	if reqCheck.Stream {
		resp, err = h.doStreamingBackendTargets(targets, "POST", body, requestID(r))
	} else {
		resp, err = h.doBackendTargets(targets, "POST", body, requestID(r), true)
	}
	if err != nil {
		writeBackendError(w, "openai", err)
		return
	}
	defer resp.Body.Close()

	// Copy response headers
	for key, values := range resp.Header {
		for _, value := range values {
			w.Header().Add(key, value)
		}
	}
	w.WriteHeader(resp.StatusCode)

	if reqCheck.Stream {
		// Stream SSE passthrough
		flusher, ok := w.(http.Flusher)
		if !ok {
			return
		}
		scanner := bufio.NewScanner(resp.Body)
		scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
		for scanner.Scan() {
			line := scanner.Text()
			fmt.Fprintf(w, "%s\n", line)
			if line == "" {
				flusher.Flush()
			}
		}
	} else {
		io.Copy(w, resp.Body)
	}
}

// ============================================================
// Gemini API handlers
// ============================================================

// GeminiHandler handles Gemini generateContent and streamGenerateContent requests.
func (h *Handler) GeminiHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeGeminiError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	model, action, ok := parseGeminiPath(r.URL.Path)
	if !ok {
		writeGeminiError(w, http.StatusNotFound, "unknown Gemini endpoint")
		return
	}
	stream := action == "streamGenerateContent"

	if h.cfg.Proxy.Debug {
		log.Printf("← %s %s model=%s action=%s", r.Method, r.URL.Path, model, action)
	}

	body, err := io.ReadAll(io.LimitReader(r.Body, maxRequestBodySize))
	if err != nil {
		writeGeminiError(w, http.StatusBadRequest, "failed to read body")
		return
	}
	defer r.Body.Close()

	modelMap := h.cfg.GetModelMap()
	if mapped, ok := modelMap[model]; ok {
		if h.cfg.Proxy.Debug {
			log.Printf("Model mapping: %s → %s", model, mapped)
		}
		model = mapped
	}

	if action == "embedContent" {
		h.geminiEmbedContent(w, r, body, model)
		return
	}

	var geminiReq types.GeminiRequest
	if err := json.Unmarshal(body, &geminiReq); err != nil {
		writeGeminiError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}
	if err := validateGeminiRequest(&geminiReq); err != nil {
		writeGeminiError(w, http.StatusBadRequest, err.Error())
		return
	}

	openaiReq := translator.TranslateGeminiRequest(&geminiReq, model, stream)

	targets, err := h.modelTargets(openaiReq.Model, "/v1/chat/completions")
	if err != nil {
		writeBackendError(w, "gemini", err)
		return
	}

	reqBody, _ := json.Marshal(openaiReq)
	if h.cfg.Proxy.Debug {
		log.Printf("→ POST %s model=%s stream=%v", targets[0].url, openaiReq.Model, openaiReq.Stream)
	}

	var resp *http.Response
	if stream {
		resp, err = h.doStreamingBackendTargets(targets, "POST", reqBody, requestID(r))
	} else {
		resp, err = h.doBackendTargets(targets, "POST", reqBody, requestID(r), true)
	}
	if err != nil {
		writeBackendError(w, "gemini", err)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(resp.StatusCode)
		io.Copy(w, resp.Body)
		return
	}

	if stream {
		h.geminiStreamResponse(w, resp)
	} else {
		h.geminiNonStreamResponse(w, resp)
	}
}

func parseGeminiPath(path string) (string, string, bool) {
	const prefix = "/gemini/v1beta/models/"
	if !strings.HasPrefix(path, prefix) {
		return "", "", false
	}
	suffix := strings.TrimPrefix(path, prefix)
	for _, action := range []string{"generateContent", "streamGenerateContent", "embedContent"} {
		marker := ":" + action
		if strings.HasSuffix(suffix, marker) {
			model := strings.TrimSuffix(suffix, marker)
			return model, action, model != ""
		}
	}
	return "", "", false
}

func (h *Handler) geminiEmbedContent(w http.ResponseWriter, r *http.Request, body []byte, model string) {
	var geminiReq types.GeminiEmbedContentRequest
	if err := json.Unmarshal(body, &geminiReq); err != nil {
		writeGeminiError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}
	if err := validateGeminiEmbedContentRequest(&geminiReq); err != nil {
		writeGeminiError(w, http.StatusBadRequest, err.Error())
		return
	}

	openaiReq := translator.TranslateGeminiEmbedContentRequest(&geminiReq, model)

	targets, err := h.modelTargets(openaiReq.Model, "/v1/embeddings")
	if err != nil {
		writeBackendError(w, "gemini", err)
		return
	}

	reqBody, _ := json.Marshal(openaiReq)
	if h.cfg.Proxy.Debug {
		log.Printf("→ POST %s model=%s", targets[0].url, openaiReq.Model)
	}

	resp, err := h.doBackendTargets(targets, "POST", reqBody, requestID(r), true)
	if err != nil {
		writeBackendError(w, "gemini", err)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(resp.StatusCode)
		io.Copy(w, resp.Body)
		return
	}

	respBody, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBodySize))
	if err != nil {
		writeGeminiError(w, http.StatusBadGateway, "failed to read backend response")
		return
	}
	respBody = bytes.TrimSpace(respBody)

	var openaiResp types.OpenAIEmbeddingsResponse
	if isSSEResponse(respBody) {
		writeGeminiError(w, http.StatusBadGateway, "unexpected SSE response from embeddings endpoint")
		return
	} else if err := json.Unmarshal(respBody, &openaiResp); err != nil {
		writeGeminiError(w, http.StatusBadGateway, "failed to parse backend response")
		return
	}

	geminiResp := translator.TranslateGeminiEmbedContentResponse(&openaiResp)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(geminiResp)
}

func (h *Handler) geminiNonStreamResponse(w http.ResponseWriter, resp *http.Response) {
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBodySize))
	if err != nil {
		writeGeminiError(w, http.StatusBadGateway, "failed to read backend response")
		return
	}
	body = bytes.TrimSpace(body)

	var openaiResp types.OpenAIResponse
	if isSSEResponse(body) {
		if accumulated, ok := parseSSEToOpenAIResponse(body); ok {
			openaiResp = *accumulated
		} else {
			writeGeminiError(w, http.StatusBadGateway, "failed to parse backend SSE response")
			return
		}
	} else if err := json.Unmarshal(body, &openaiResp); err != nil {
		writeGeminiError(w, http.StatusBadGateway, "failed to parse backend response")
		return
	}

	geminiResp := translator.TranslateGeminiResponse(&openaiResp)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(geminiResp)
}

func (h *Handler) geminiStreamResponse(w http.ResponseWriter, resp *http.Response) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeGeminiError(w, http.StatusInternalServerError, "streaming not supported")
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	emit := func(data interface{}) {
		j, _ := json.Marshal(data)
		if h.cfg.Proxy.Debug {
			log.Printf("← Gemini SSE data=%s", string(j))
		}
		fmt.Fprintf(w, "data: %s\n\n", string(j))
		flusher.Flush()
	}

	streamTranslator := translator.NewGeminiStreamTranslator(emit)

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
		streamTranslator.ProcessChunk(&chunk)
	}

	if err := scanner.Err(); err != nil {
		log.Printf("stream read error: %v", err)
	}
}

func writeGeminiError(w http.ResponseWriter, status int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"error": map[string]string{
			"code":    fmt.Sprintf("%d", status),
			"message": message,
			"status":  http.StatusText(status),
		},
	})
}
