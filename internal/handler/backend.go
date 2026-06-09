package handler

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"strings"
)

func (h *Handler) modelCandidates(model, targetPath string) ([]string, error) {
	limit := h.cfg.Proxy.FailoverMaxBackends
	targets := h.router.Candidates(model, targetPath, limit)
	if len(targets) == 0 {
		return nil, errNoBackendForModel(model)
	}
	urls := make([]string, 0, len(targets))
	for _, target := range targets {
		urls = append(urls, target.URL)
	}
	return urls, nil
}

func (h *Handler) backendRequest(method, url string, body io.Reader, requestID string, apiKey string) (*http.Request, error) {
	req, err := http.NewRequest(method, url, body)
	if err != nil {
		return nil, err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if requestID != "" {
		req.Header.Set("x-request-id", requestID)
	}
	if apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+apiKey)
	}
	return req, nil
}

func (h *Handler) modelTargets(model, targetPath string) ([]*backendTarget, error) {
	limit := h.cfg.Proxy.FailoverMaxBackends
	targets := h.router.Candidates(model, targetPath, limit)
	if len(targets) == 0 {
		return nil, errNoBackendForModel(model)
	}
	out := make([]*backendTarget, 0, len(targets))
	for _, target := range targets {
		out = append(out, &backendTarget{name: target.Runtime.Backend.Name, url: target.URL, apiKey: target.Runtime.Backend.APIKey, dropFields: target.Runtime.Backend.DropFields})
	}
	return out, nil
}

func (h *Handler) defaultTarget(targetPath string) *backendTarget {
	target := h.router.DefaultTarget(targetPath)
	if target == nil {
		return nil
	}
	return &backendTarget{name: target.Runtime.Backend.Name, url: target.URL, apiKey: target.Runtime.Backend.APIKey, dropFields: target.Runtime.Backend.DropFields}
}

type backendTarget struct {
	name       string
	url        string
	apiKey     string
	dropFields []string
}

func applyDropFields(body []byte, fields []string) []byte {
	if len(fields) == 0 {
		return body
	}
	var payload map[string]interface{}
	if err := json.Unmarshal(body, &payload); err != nil {
		return body
	}
	changed := false
	for _, field := range fields {
		if _, ok := payload[field]; ok {
			delete(payload, field)
			changed = true
		}
	}
	if !changed {
		return body
	}
	mapped, err := json.Marshal(payload)
	if err != nil {
		return body
	}
	return mapped
}

func (h *Handler) doBackendTargets(targets []*backendTarget, method string, body []byte, requestID string, retryable bool) (*http.Response, error) {
	if len(targets) == 0 {
		return nil, errNoBackendForModel("")
	}
	var lastErr error
	for i, target := range targets {
		attemptBody := applyDropFields(body, target.dropFields)
		req, err := h.backendRequest(method, target.url, cloneBody(attemptBody), requestID, target.apiKey)
		if err != nil {
			return nil, err
		}
		resp, err := h.doBackendRequest(req, attemptBody, retryable)
		if err != nil {
			lastErr = err
			if !h.cfg.Proxy.FailoverEnabled || i == len(targets)-1 {
				return nil, err
			}
			continue
		}
		if !h.cfg.Proxy.FailoverEnabled || i == len(targets)-1 || !h.isFailoverStatus(resp.StatusCode) {
			return resp, nil
		}
		io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
	}
	return nil, lastErr
}

func (h *Handler) doStreamingBackendTargets(targets []*backendTarget, method string, body []byte, requestID string) (*http.Response, error) {
	if len(targets) == 0 {
		return nil, errNoBackendForModel("")
	}
	var lastErr error
	for i, target := range targets {
		attemptBody := applyDropFields(body, target.dropFields)
		req, err := h.backendRequest(method, target.url, cloneBody(attemptBody), requestID, target.apiKey)
		if err != nil {
			return nil, err
		}
		resp, err := h.doStreamingBackendRequest(req)
		if err != nil {
			lastErr = err
			if !h.cfg.Proxy.FailoverEnabled || i == len(targets)-1 {
				return nil, err
			}
			continue
		}
		if !h.cfg.Proxy.FailoverEnabled || i == len(targets)-1 {
			return resp, nil
		}
		if h.isFailoverStatus(resp.StatusCode) {
			io.Copy(io.Discard, resp.Body)
			resp.Body.Close()
			continue
		}
		shouldFailover, err := h.prepareStreamingResponse(resp)
		if err != nil {
			lastErr = err
			resp.Body.Close()
			continue
		}
		if !shouldFailover {
			return resp, nil
		}
		io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
	}
	return nil, lastErr
}

func cloneBody(body []byte) io.Reader {
	return bytes.NewReader(body)
}

func (h *Handler) isFailoverStatus(status int) bool {
	for _, configured := range h.cfg.Proxy.FailoverStatusCodes {
		if status == configured {
			return true
		}
	}
	return false
}

func (h *Handler) prepareStreamingResponse(resp *http.Response) (bool, error) {
	patterns := h.cfg.Proxy.FailoverStreamErrorPatterns
	if len(patterns) == 0 || resp.Body == nil || resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return false, nil
	}
	const maxInspectBytes = 16 * 1024
	buf := make([]byte, maxInspectBytes)
	n, err := resp.Body.Read(buf)
	if err != nil && err != io.EOF {
		return false, err
	}
	prefix := buf[:n]
	bodyText := string(prefix)
	for _, pattern := range patterns {
		if pattern != "" && strings.Contains(bodyText, pattern) {
			return true, nil
		}
	}
	if n > 0 {
		resp.Body = io.NopCloser(io.MultiReader(bytes.NewReader(prefix), resp.Body))
	}
	return false, nil
}

type noBackendForModelError struct{ model string }

func errNoBackendForModel(model string) error { return noBackendForModelError{model: model} }

func (e noBackendForModelError) Error() string { return "no backend configured for model: " + e.model }
