package handler

import (
	"encoding/json"
	"net/http"
	"strconv"
)

func (h *Handler) HealthHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeOpenAIError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	backendConfigured := len(h.router.Backends()) > 0
	status := "ok"
	if !backendConfigured {
		status = "error"
	}

	storeStats := h.store.Stats()
	storeBody := map[string]interface{}{
		"backend":     storeStats.Backend,
		"entries":     storeStats.Entries,
		"max_entries": storeStats.MaxEntries,
		"ttl_seconds": storeStats.TTLSeconds,
	}
	if storeStats.Path != "" {
		storeBody["path"] = storeStats.Path
	}

	healthSnapshot := h.health.Snapshot()
	backendBody := map[string]interface{}{
		"monitoring_enabled": healthSnapshot.Enabled,
		"healthy":            healthSnapshot.Healthy,
		"circuit":            healthSnapshot.Circuit,
	}
	if healthSnapshot.LastSuccess != "" {
		backendBody["last_success"] = healthSnapshot.LastSuccess
	}
	if healthSnapshot.LastFailure != "" {
		backendBody["last_failure"] = healthSnapshot.LastFailure
	}
	if healthSnapshot.LastError != "" {
		backendBody["last_error"] = healthSnapshot.LastError
	}

	backendsBody := make([]map[string]interface{}, 0, len(h.router.Backends()))
	for _, runtime := range h.router.Backends() {
		backendsBody = append(backendsBody, map[string]interface{}{
			"name":     runtime.Backend.Name,
			"url":      runtime.Backend.URL,
			"enabled":  runtime.Backend.Enabled,
			"models":   runtime.Backend.Models,
			"active":   runtime.Active(),
			"priority": runtime.Backend.Priority,
			"weight":   runtime.Backend.Weight,
		})
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status":             status,
		"backend_configured": backendConfigured,
		"backend":            backendBody,
		"backends":           backendsBody,
		"store":              storeBody,
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
	output := h.metrics.Prometheus()
	storeStats := h.store.Stats()
	output += "# HELP anthropic_proxy_store_entries Stored Responses API entries.\n"
	output += "# TYPE anthropic_proxy_store_entries gauge\n"
	output += "anthropic_proxy_store_entries{backend=\"" + storeStats.Backend + "\"} " + strconv.Itoa(storeStats.Entries) + "\n"
	if h.health != nil {
		snapshot := h.health.Snapshot()
		state := string(snapshot.Circuit.State)
		output += "# HELP anthropic_proxy_circuit_breaker_state Circuit breaker state.\n"
		output += "# TYPE anthropic_proxy_circuit_breaker_state gauge\n"
		output += "anthropic_proxy_circuit_breaker_state{state=\"" + state + "\"} 1\n"
	}
	for _, runtime := range h.router.Backends() {
		output += "# HELP anthropic_proxy_backend_active_requests Active backend requests.\n"
		output += "# TYPE anthropic_proxy_backend_active_requests gauge\n"
		output += "anthropic_proxy_backend_active_requests{backend=\"" + runtime.Backend.Name + "\"} " + strconv.FormatInt(runtime.Active(), 10) + "\n"
	}
	w.Write([]byte(output))
}
