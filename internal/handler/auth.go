package handler

import (
	"net/http"
	"strings"
)

// AuthMiddleware returns an http.HandlerFunc that validates API keys when auth is enabled.
func (h *Handler) AuthMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !h.cfg.Auth.Enabled {
			next(w, r)
			return
		}

		apiKey := extractAPIKey(r)
		if apiKey == "" {
			writeError(w, http.StatusUnauthorized, "authentication_error", "missing API key")
			return
		}

		if !isValidKey(apiKey, h.cfg.Auth.Keys) {
			writeError(w, http.StatusUnauthorized, "authentication_error", "invalid API key")
			return
		}

		next(w, r)
	}
}

// extractAPIKey gets the API key from request headers.
// Supports both X-Api-Key (Anthropic) and Authorization: Bearer (OpenAI/Gemini).
func extractAPIKey(r *http.Request) string {
	// Anthropic style
	if key := r.Header.Get("X-Api-Key"); key != "" {
		return key
	}

	// OpenAI/Gemini style
	if auth := r.Header.Get("Authorization"); auth != "" {
		if strings.HasPrefix(auth, "Bearer ") {
			return strings.TrimPrefix(auth, "Bearer ")
		}
	}

	return ""
}

// isValidKey checks if the given key is in the allowed list.
func isValidKey(key string, allowed []string) bool {
	for _, k := range allowed {
		if k == key {
			return true
		}
	}
	return false
}
