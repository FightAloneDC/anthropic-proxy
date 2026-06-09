package handler

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"sort"

	"anthropic-proxy/internal/reliability"
)

func (h *Handler) doBackendRequest(req *http.Request, body []byte, retryable bool) (*http.Response, error) {
	if h.executor == nil {
		return h.client.Do(req)
	}
	return h.executor.Do(req, body, retryable)
}

func (h *Handler) doStreamingBackendRequest(req *http.Request) (*http.Response, error) {
	if h.breaker != nil && !h.breaker.Allow() {
		return nil, reliability.ErrCircuitOpen
	}
	resp, err := h.client.Do(req)
	if err != nil {
		if h.breaker != nil {
			h.breaker.RecordFailure(err.Error())
		}
		return nil, err
	}
	if resp.StatusCode >= 500 || resp.StatusCode == http.StatusTooManyRequests {
		if h.breaker != nil {
			h.breaker.RecordFailure(resp.Status)
		}
	} else if h.breaker != nil {
		h.breaker.RecordSuccess()
	}
	return resp, nil
}

func writeBackendError(w http.ResponseWriter, family string, err error) {
	status := http.StatusBadGateway
	message := "backend error: " + err.Error()
	if errors.Is(err, reliability.ErrCircuitOpen) {
		status = http.StatusServiceUnavailable
		message = err.Error()
	}
	switch family {
	case "responses":
		writeResponsesError(w, status, "api_error", message)
	case "gemini":
		writeGeminiError(w, status, message)
	case "openai":
		writeOpenAIError(w, status, message)
	default:
		writeError(w, status, "api_error", message)
	}
}

func (h *Handler) mergeModelsResponses(bodies [][]byte) []byte {
	merged := map[string]interface{}{
		"object": "list",
		"data":   []interface{}{},
	}
	data := make([]interface{}, 0)
	seen := map[string]bool{}
	for _, body := range bodies {
		var payload map[string]interface{}
		if err := json.Unmarshal(body, &payload); err != nil {
			continue
		}
		items, ok := payload["data"].([]interface{})
		if !ok {
			continue
		}
		for _, item := range items {
			model, ok := item.(map[string]interface{})
			if !ok {
				continue
			}
			id, ok := model["id"].(string)
			if !ok || seen[id] {
				continue
			}
			seen[id] = true
			data = append(data, item)
		}
	}
	merged["data"] = data
	body, err := json.Marshal(merged)
	if err != nil {
		return []byte(`{"object":"list","data":[]}`)
	}
	return body
}

func (h *Handler) enrichModelsResponse(body []byte) []byte {
	modelMap := h.cfg.GetModelMap()
	if len(modelMap) == 0 {
		return body
	}
	var payload map[string]interface{}
	if err := json.Unmarshal(body, &payload); err != nil {
		return body
	}
	data, ok := payload["data"].([]interface{})
	if !ok {
		return body
	}
	seen := map[string]bool{}
	for _, item := range data {
		model, ok := item.(map[string]interface{})
		if !ok {
			continue
		}
		if id, ok := model["id"].(string); ok {
			seen[id] = true
		}
	}
	aliases := make([]string, 0, len(modelMap))
	for alias := range modelMap {
		aliases = append(aliases, alias)
	}
	sort.Strings(aliases)
	for _, alias := range aliases {
		if seen[alias] {
			continue
		}
		data = append(data, map[string]interface{}{
			"id":       alias,
			"object":   "model",
			"owned_by": "anthropic-proxy",
		})
	}
	payload["data"] = data
	mapped, err := json.Marshal(payload)
	if err != nil {
		return body
	}
	return mapped
}

func resetBody(req *http.Request, body []byte) {
	req.Body = io.NopCloser(bytes.NewReader(body))
	req.ContentLength = int64(len(body))
}
