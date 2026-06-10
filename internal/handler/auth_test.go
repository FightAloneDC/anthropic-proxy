package handler

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"anthropic-proxy/internal/config"
)

func TestAuthMiddleware_Disabled(t *testing.T) {
	cfg := &config.Config{
		Auth: config.AuthConfig{
			Enabled: false,
		},
	}
	h := &Handler{cfg: cfg}

	called := false
	inner := func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	}

	middleware := h.AuthMiddleware(inner)
	req := httptest.NewRequest("POST", "/anthropic/v1/messages", nil)
	w := httptest.NewRecorder()

	middleware(w, req)

	if !called {
		t.Error("expected inner handler to be called when auth disabled")
	}
	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", w.Code)
	}
}

func TestAuthMiddleware_Enabled_ValidKey(t *testing.T) {
	cfg := &config.Config{
		Auth: config.AuthConfig{
			Enabled: true,
			Keys:    []string{"sk-valid-1", "sk-valid-2"},
		},
	}
	h := &Handler{cfg: cfg}

	called := false
	inner := func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	}

	middleware := h.AuthMiddleware(inner)

	tests := []struct {
		name   string
		header string
		value  string
	}{
		{"X-Api-Key", "X-Api-Key", "sk-valid-1"},
		{"Authorization Bearer", "Authorization", "Bearer sk-valid-2"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			called = false
			req := httptest.NewRequest("POST", "/anthropic/v1/messages", nil)
			req.Header.Set(tt.header, tt.value)
			w := httptest.NewRecorder()

			middleware(w, req)

			if !called {
				t.Error("expected inner handler to be called with valid key")
			}
			if w.Code != http.StatusOK {
				t.Errorf("expected status 200, got %d", w.Code)
			}
		})
	}
}

func TestAuthMiddleware_Enabled_MissingKey(t *testing.T) {
	cfg := &config.Config{
		Auth: config.AuthConfig{
			Enabled: true,
			Keys:    []string{"sk-valid-1"},
		},
	}
	h := &Handler{cfg: cfg}

	called := false
	inner := func(w http.ResponseWriter, r *http.Request) {
		called = true
	}

	middleware := h.AuthMiddleware(inner)
	req := httptest.NewRequest("POST", "/anthropic/v1/messages", nil)
	w := httptest.NewRecorder()

	middleware(w, req)

	if called {
		t.Error("expected inner handler to NOT be called when key missing")
	}
	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected status 401, got %d", w.Code)
	}
}

func TestAuthMiddleware_Enabled_InvalidKey(t *testing.T) {
	cfg := &config.Config{
		Auth: config.AuthConfig{
			Enabled: true,
			Keys:    []string{"sk-valid-1"},
		},
	}
	h := &Handler{cfg: cfg}

	called := false
	inner := func(w http.ResponseWriter, r *http.Request) {
		called = true
	}

	middleware := h.AuthMiddleware(inner)
	req := httptest.NewRequest("POST", "/anthropic/v1/messages", nil)
	req.Header.Set("X-Api-Key", "sk-invalid-key")
	w := httptest.NewRecorder()

	middleware(w, req)

	if called {
		t.Error("expected inner handler to NOT be called with invalid key")
	}
	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected status 401, got %d", w.Code)
	}
}

func TestExtractAPIKey(t *testing.T) {
	tests := []struct {
		name   string
		header string
		value  string
		want   string
	}{
		{"X-Api-Key", "X-Api-Key", "sk-test", "sk-test"},
		{"Authorization Bearer", "Authorization", "Bearer sk-test", "sk-test"},
		{"No header", "", "", ""},
		{"Empty Bearer", "Authorization", "Bearer ", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("POST", "/", nil)
			if tt.header != "" {
				req.Header.Set(tt.header, tt.value)
			}
			got := extractAPIKey(req)
			if got != tt.want {
				t.Errorf("extractAPIKey() = %q, want %q", got, tt.want)
			}
		})
	}
}
