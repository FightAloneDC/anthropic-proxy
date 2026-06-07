package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestHealthHandler(t *testing.T) {
	h := newTestHandler("http://backend.example/v1")
	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rec := httptest.NewRecorder()

	h.HealthHandler(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s", rec.Code, rec.Body.String())
	}
	var body map[string]interface{}
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode health: %v", err)
	}
	if body["status"] != "ok" || body["backend_configured"] != true {
		t.Fatalf("body = %#v", body)
	}
	if strings.Contains(rec.Body.String(), "backend-key") {
		t.Fatalf("health leaked api key: %s", rec.Body.String())
	}
}

func TestHealthHandlerRejectsNonGet(t *testing.T) {
	h := newTestHandler("http://backend.example/v1")
	req := httptest.NewRequest(http.MethodPost, "/health", nil)
	rec := httptest.NewRecorder()

	h.HealthHandler(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d", rec.Code)
	}
}

func TestMetricsHandler(t *testing.T) {
	h := newTestHandler("http://backend.example/v1")
	h.metrics.Observe("/health", http.MethodGet, http.StatusOK, 0)
	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	req.Header.Set("x-request-id", "req-metrics")
	rec := httptest.NewRecorder()

	h.MetricsHandler(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s", rec.Code, rec.Body.String())
	}
	if rec.Header().Get("x-request-id") != "req-metrics" {
		t.Fatalf("request id = %q", rec.Header().Get("x-request-id"))
	}
	if !strings.Contains(rec.Body.String(), "anthropic_proxy_requests_total") {
		t.Fatalf("metrics = %s", rec.Body.String())
	}
}
