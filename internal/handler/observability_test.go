package handler

import (
	"bytes"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestRequestIDPrefersInboundHeader(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	req.Header.Set("x-request-id", "req-inbound")

	if got := requestID(req); got != "req-inbound" {
		t.Fatalf("requestID = %q", got)
	}
}

func TestObserveSetsRequestIDAndMetrics(t *testing.T) {
	h := newTestHandler("http://backend.example/v1")
	req := httptest.NewRequest(http.MethodGet, "/observed", nil)
	req.Header.Set("x-request-id", "req-observed")
	rec := httptest.NewRecorder()

	h.Observe("/observed", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("x-request-id") != "req-observed" {
			t.Fatalf("request header id = %q", r.Header.Get("x-request-id"))
		}
		w.WriteHeader(http.StatusCreated)
	})(rec, req)

	if rec.Header().Get("x-request-id") != "req-observed" {
		t.Fatalf("response id = %q", rec.Header().Get("x-request-id"))
	}
	metrics := h.metrics.Prometheus()
	if !strings.Contains(metrics, `anthropic_proxy_requests_total{endpoint="/observed",method="GET",status="201"} 1`) {
		t.Fatalf("metrics = %s", metrics)
	}
}

func TestObservePreservesFlusher(t *testing.T) {
	h := newTestHandler("http://backend.example/v1")
	req := httptest.NewRequest(http.MethodGet, "/stream", nil)
	rec := httptest.NewRecorder()

	h.Observe("/stream", func(w http.ResponseWriter, r *http.Request) {
		flusher, ok := w.(http.Flusher)
		if !ok {
			t.Fatalf("observed response writer does not implement http.Flusher")
		}
		_, _ = w.Write([]byte("data: hello\n\n"))
		flusher.Flush()
	})(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	if body := rec.Body.String(); body != "data: hello\n\n" {
		t.Fatalf("body = %q", body)
	}
}

func TestLoggerJSONAndLevelFiltering(t *testing.T) {
	var buf bytes.Buffer
	oldOutput := log.Writer()
	log.SetOutput(&buf)
	defer log.SetOutput(oldOutput)

	logger := NewLogger("json", "warn")
	logger.Info("hidden", map[string]interface{}{"api_key": "should-not-appear"})
	logger.Error("visible", map[string]interface{}{"request_id": "req-1"})

	output := buf.String()
	if strings.Contains(output, "should-not-appear") {
		t.Fatalf("filtered log leaked fields: %s", output)
	}
	if !strings.Contains(output, `"msg":"visible"`) || !strings.Contains(output, `"request_id":"req-1"`) {
		t.Fatalf("output = %s", output)
	}
}

func TestMetricsPrometheus(t *testing.T) {
	metrics := NewMetrics()
	metrics.IncActive()
	metrics.Observe("/health", http.MethodGet, http.StatusOK, 10*time.Millisecond)
	metrics.DecActive()

	output := metrics.Prometheus()
	if !strings.Contains(output, "anthropic_proxy_requests_total") {
		t.Fatalf("output = %s", output)
	}
	if !strings.Contains(output, `endpoint="/health"`) {
		t.Fatalf("output = %s", output)
	}
	if !strings.Contains(output, "anthropic_proxy_active_requests 0") {
		t.Fatalf("output = %s", output)
	}
}
