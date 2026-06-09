package handler

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"anthropic-proxy/internal/config"
	"anthropic-proxy/internal/store"
)

func TestChatCompletionsHandlerRoutesByMappedModel(t *testing.T) {
	wrong := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatalf("wrong backend received request")
	}))
	defer wrong.Close()

	right := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			t.Fatalf("path = %q", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer right-key" {
			t.Fatalf("authorization = %q", r.Header.Get("Authorization"))
		}
		var payload map[string]interface{}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if payload["model"] != "deepseek-chat" {
			t.Fatalf("model = %#v", payload["model"])
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer right.Close()

	h := newMultiBackendTestHandler(wrong.URL, right.URL)
	req := httptest.NewRequest(http.MethodPost, "/openai/v1/chat/completions", strings.NewReader(`{"model":"claude-sonnet-4-6","messages":[{"role":"user","content":"hi"}]}`))
	rec := httptest.NewRecorder()

	h.ChatCompletionsHandler(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s", rec.Code, rec.Body.String())
	}
}

func TestChatCompletionsHandlerFailsOverRetryableStatus(t *testing.T) {
	primaryHits := 0
	primary := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		primaryHits++
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer primary.Close()

	secondaryHits := 0
	secondary := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		secondaryHits++
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer secondary.Close()

	h := New(&config.Config{
		Backends: []config.BackendConfig{
			{Name: "primary", URL: primary.URL, Models: []string{"mimo-*"}, Weight: 1},
			{Name: "secondary", URL: secondary.URL, Models: []string{"mimo-*"}, Weight: 1},
		},
		Proxy: config.ProxyConfig{LoadBalanceStrategy: "round_robin", FailoverEnabled: true, RetryMaxAttempts: 1},
	}, store.New(0, 0))
	h.executor.Config.Enabled = false
	req := httptest.NewRequest(http.MethodPost, "/openai/v1/chat/completions", strings.NewReader(`{"model":"mimo-v2","messages":[{"role":"user","content":"hi"}]}`))
	rec := httptest.NewRecorder()

	h.ChatCompletionsHandler(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s", rec.Code, rec.Body.String())
	}
	if primaryHits != 1 || secondaryHits != 1 {
		t.Fatalf("hits primary=%d secondary=%d", primaryHits, secondaryHits)
	}
}

func TestDirectForwardHandlerRoutesJSONByModel(t *testing.T) {
	wrong := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatalf("wrong backend received request")
	}))
	defer wrong.Close()

	right := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/embeddings" {
			t.Fatalf("path = %q", r.URL.Path)
		}
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("read body: %v", err)
		}
		if !strings.Contains(string(body), `"model":"deepseek-embedding"`) {
			t.Fatalf("body = %s", body)
		}
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer right.Close()

	h := newMultiBackendTestHandler(wrong.URL, right.URL)
	req := httptest.NewRequest(http.MethodPost, "/openai/v1/embeddings", strings.NewReader(`{"model":"embedding-client","input":"hi"}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	h.DirectForwardHandler("/v1/embeddings")(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s", rec.Code, rec.Body.String())
	}
}

func TestModelsHandlerMergesBackendsAndKeepsPublicEndpoint(t *testing.T) {
	first := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/models" {
			t.Fatalf("first path = %q", r.URL.Path)
		}
		_, _ = w.Write([]byte(`{"object":"list","data":[{"id":"mimo-v2","object":"model"}]}`))
	}))
	defer first.Close()

	second := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/models" {
			t.Fatalf("second path = %q", r.URL.Path)
		}
		_, _ = w.Write([]byte(`{"object":"list","data":[{"id":"deepseek-chat","object":"model"}]}`))
	}))
	defer second.Close()

	h := newMultiBackendTestHandler(first.URL, second.URL)
	for _, path := range []string{"/anthropic/v1/models", "/openai/v1/models"} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		rec := httptest.NewRecorder()
		h.ModelsHandler(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("%s status = %d body = %s", path, rec.Code, rec.Body.String())
		}
		body := rec.Body.String()
		for _, id := range []string{"mimo-v2", "deepseek-chat", "claude-sonnet-4-6", "embedding-client"} {
			if !strings.Contains(body, id) {
				t.Fatalf("%s body missing %s: %s", path, id, body)
			}
		}
	}
}

func newMultiBackendTestHandler(mimoURL, deepseekURL string) *Handler {
	return New(&config.Config{
		Backends: []config.BackendConfig{
			{Name: "mimo", URL: mimoURL + "/v1", APIKey: "mimo-key", Models: []string{"mimo-*"}, Weight: 1},
			{Name: "deepseek", URL: deepseekURL + "/v1", APIKey: "right-key", Models: []string{"deepseek-*"}, Weight: 1},
		},
		Models: []config.ModelMap{
			{From: "claude-sonnet-4-6", To: "deepseek-chat"},
			{From: "embedding-client", To: "deepseek-embedding"},
		},
		Proxy: config.ProxyConfig{LoadBalanceStrategy: "round_robin", FailoverEnabled: true},
	}, store.New(0, 0))
}
