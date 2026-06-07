package handler

import (
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

		base := strings.TrimRight(h.cfg.Backend.URL, "/")
		if strings.HasSuffix(base, "/v1") {
			base = base[:len(base)-3]
		}
		targetURL := base + targetPath

		if h.cfg.Proxy.Debug {
			log.Printf("← %s %s (direct forward)", r.Method, r.URL.Path)
			log.Printf("→ %s %s", r.Method, targetURL)
		}

		proxyReq, err := http.NewRequest(r.Method, targetURL, r.Body)
		if err != nil {
			writeOpenAIError(w, http.StatusInternalServerError, err.Error())
			return
		}

		copyForwardHeader(proxyReq.Header, r.Header, "Content-Type")
		copyForwardHeader(proxyReq.Header, r.Header, "Accept")
		copyForwardHeader(proxyReq.Header, r.Header, "User-Agent")
		proxyReq.Header.Set("x-request-id", requestID(r))

		if h.cfg.Backend.APIKey != "" {
			proxyReq.Header.Set("Authorization", "Bearer "+h.cfg.Backend.APIKey)
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
