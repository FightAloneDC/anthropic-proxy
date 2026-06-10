package handler

import (
	"bytes"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"strings"
)

// DirectForwardHandler forwards OpenAI-compatible endpoints directly to the configured backend path.
func (h *Handler) DirectForwardHandler(targetPath string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeOpenAIError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}

		target := h.defaultTarget(targetPath)
		if target == nil {
			writeOpenAIError(w, http.StatusBadGateway, "no backend configured")
			return
		}

		var body []byte
		var err error
		contentType := r.Header.Get("Content-Type")
		if h.router.Multi() && strings.HasPrefix(contentType, "application/json") {
			body, err = io.ReadAll(io.LimitReader(r.Body, maxRequestBodySize))
			if err != nil {
				writeOpenAIError(w, http.StatusBadRequest, "failed to read body")
				return
			}
			var reqCheck struct {
				Model string `json:"model"`
			}
			if json.Unmarshal(body, &reqCheck) == nil && reqCheck.Model != "" {
				if mapped, ok := h.cfg.GetModelMap()[reqCheck.Model]; ok {
					var payload map[string]interface{}
					if json.Unmarshal(body, &payload) == nil {
						payload["model"] = mapped
						if mappedBody, err := json.Marshal(payload); err == nil {
							body = mappedBody
							reqCheck.Model = mapped
						}
					}
				}
				targets, err := h.modelTargets(reqCheck.Model, targetPath)
				if err != nil {
					writeBackendError(w, "openai", err)
					return
				}
				if h.cfg.Proxy.Debug {
					log.Printf("← %s %s (direct forward)", r.Method, r.URL.Path)
					log.Printf("→ %s %s model=%s", r.Method, targets[0].url, reqCheck.Model)
				}
				resp, err := h.doBackendTargets(targets, r.Method, body, requestID(r), true)
				if err != nil {
					writeBackendError(w, "openai", err)
					return
				}
				defer resp.Body.Close()
				for key, values := range resp.Header {
					for _, value := range values {
						w.Header().Add(key, value)
					}
				}
				w.WriteHeader(resp.StatusCode)
				io.Copy(w, resp.Body)
				return
			}
		}
		if body != nil {
			r.Body = io.NopCloser(bytes.NewReader(body))
		}

		if h.cfg.Proxy.Debug {
			log.Printf("← %s %s (direct forward)", r.Method, r.URL.Path)
			log.Printf("→ %s %s", r.Method, target.url)
		}

		proxyReq, err := http.NewRequest(r.Method, target.url, r.Body)
		if err != nil {
			writeOpenAIError(w, http.StatusInternalServerError, err.Error())
			return
		}

		copyForwardHeader(proxyReq.Header, r.Header, "Content-Type")
		copyForwardHeader(proxyReq.Header, r.Header, "Accept")
		copyForwardHeader(proxyReq.Header, r.Header, "User-Agent")
		proxyReq.Header.Set("x-request-id", requestID(r))

		if target.apiKey != "" {
			proxyReq.Header.Set("Authorization", "Bearer "+target.apiKey)
		}

		resp, err := h.client.Do(proxyReq)
		if err != nil {
			writeOpenAIError(w, http.StatusBadGateway, "backend error: "+err.Error())
			return
		}
		defer resp.Body.Close()

		for key, values := range resp.Header {
			for _, value := range values {
				w.Header().Add(key, value)
			}
		}
		w.WriteHeader(resp.StatusCode)
		io.Copy(w, resp.Body)
	}
}

func copyForwardHeader(dst, src http.Header, key string) {
	for _, value := range src.Values(key) {
		dst.Add(key, value)
	}
}

func writeOpenAIError(w http.ResponseWriter, status int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"error": map[string]string{
			"message": message,
			"type":    "invalid_request_error",
		},
	})
}
