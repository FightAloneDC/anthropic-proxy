package reliability

import (
	"bytes"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestBackendExecutorRetriesTransientStatus(t *testing.T) {
	attempts := 0
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		if attempts == 1 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer backend.Close()

	req, err := http.NewRequest(http.MethodPost, backend.URL, bytes.NewReader([]byte(`{"model":"m"}`)))
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	executor := &BackendExecutor{
		Client: http.DefaultClient,
		Config: RetryConfig{Enabled: true, MaxAttempts: 2, Sleep: func(time.Duration) {}},
	}
	resp, err := executor.Do(req, []byte(`{"model":"m"}`), true)
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK || attempts != 2 {
		t.Fatalf("status=%d attempts=%d", resp.StatusCode, attempts)
	}
}

func TestBackendExecutorDoesNotRetryNonTransientStatus(t *testing.T) {
	attempts := 0
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		w.WriteHeader(http.StatusBadRequest)
	}))
	defer backend.Close()

	req, err := http.NewRequest(http.MethodPost, backend.URL, bytes.NewReader([]byte(`{}`)))
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	executor := &BackendExecutor{Client: http.DefaultClient, Config: RetryConfig{Enabled: true, MaxAttempts: 3, Sleep: func(time.Duration) {}}}
	resp, err := executor.Do(req, []byte(`{}`), true)
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest || attempts != 1 {
		t.Fatalf("status=%d attempts=%d", resp.StatusCode, attempts)
	}
}

func TestBackendExecutorCircuitOpen(t *testing.T) {
	cb := NewCircuitBreaker(true, 1, time.Hour)
	cb.RecordFailure("fail")
	executor := &BackendExecutor{Client: http.DefaultClient, Breaker: cb}
	req, err := http.NewRequest(http.MethodPost, "http://example.invalid", bytes.NewReader([]byte(`{}`)))
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	_, err = executor.Do(req, []byte(`{}`), true)
	if !errors.Is(err, ErrCircuitOpen) {
		t.Fatalf("err = %v", err)
	}
}
