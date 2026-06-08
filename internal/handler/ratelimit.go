package handler

import "net/http"

func (h *Handler) RateLimit(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if h.limiter == nil || !h.limiter.Enabled() || r.URL.Path == "/health" || r.URL.Path == "/metrics" {
			next(w, r)
			return
		}
		if !h.limiter.Allow(r) {
			w.Header().Set("Retry-After", "60")
			writeOpenAIError(w, http.StatusTooManyRequests, "rate limit exceeded")
			return
		}
		next(w, r)
	}
}
