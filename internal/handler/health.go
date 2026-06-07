package handler

import (
	"encoding/json"
	"net/http"
)

func (h *Handler) HealthHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeOpenAIError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	backendConfigured := h.cfg.Backend.URL != ""
	status := "ok"
	if !backendConfigured {
		status = "error"
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status":             status,
		"backend_configured": backendConfigured,
		"store": map[string]interface{}{
			"backend":     "memory",
			"entries":     h.store.Stats().Entries,
			"max_entries": h.store.Stats().MaxEntries,
			"ttl_seconds": h.store.Stats().TTLSeconds,
		},
	})
}

func (h *Handler) MetricsHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeOpenAIError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if h.cfg.Proxy.MetricsEnabled != nil && !*h.cfg.Proxy.MetricsEnabled {
		http.NotFound(w, r)
		return
	}
	id := requestID(r)
	w.Header().Set("x-request-id", id)
	w.Header().Set("Content-Type", "text/plain; version=0.0.4")
	w.Write([]byte(h.metrics.Prometheus()))
}
