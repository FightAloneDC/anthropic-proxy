package handler

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"anthropic-proxy/internal/reliability"
)

func TestDirectForwardHandlerForwardsBodyPathAndBackendAuth(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/embeddings" {
			t.Fatalf("backend path = %q", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer backend-key" {
			t.Fatalf("authorization = %q", r.Header.Get("Authorization"))
		}
		if r.Header.Get("Content-Type") != "application/json" {
			t.Fatalf("content-type = %q", r.Header.Get("Content-Type"))
		}
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("read body: %v", err)
		}
		if string(body) != `{"model":"m","input":"hello"}` {
			t.Fatalf("body = %s", body)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusAccepted)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer backend.Close()

	h := newTestHandler(backend.URL + "/v1")
	req := httptest.NewRequest(http.MethodPost, "/openai/v1/embeddings", strings.NewReader(`{"model":"m","input":"hello"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer client-key")
	rec := httptest.NewRecorder()

	h.DirectForwardHandler("/v1/embeddings")(rec, req)

	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d body = %s", rec.Code, rec.Body.String())
	}
	if rec.Body.String() != `{"ok":true}` {
		t.Fatalf("response body = %s", rec.Body.String())
	}
}

func TestDirectForwardHandlerCopiesBinaryResponse(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/audio/speech" {
			t.Fatalf("backend path = %q", r.URL.Path)
		}
		w.Header().Set("Content-Type", "audio/mpeg")
		_, _ = w.Write([]byte{0x01, 0x02, 0x03})
	}))
	defer backend.Close()

	h := newTestHandler(backend.URL)
	req := httptest.NewRequest(http.MethodPost, "/openai/v1/audio/speech", strings.NewReader(`{"model":"tts-1"}`))
	rec := httptest.NewRecorder()

	h.DirectForwardHandler("/v1/audio/speech")(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	if rec.Header().Get("Content-Type") != "audio/mpeg" {
		t.Fatalf("content-type = %q", rec.Header().Get("Content-Type"))
	}
	body := rec.Body.Bytes()
	if len(body) != 3 || body[0] != 0x01 || body[1] != 0x02 || body[2] != 0x03 {
		t.Fatalf("body = %#v", body)
	}
}

func TestChatCompletionsHandlerAppliesModelMapping(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			t.Fatalf("backend path = %q", r.URL.Path)
		}
		var payload map[string]interface{}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		if payload["model"] != "backend-gemini" {
			t.Fatalf("model = %#v", payload["model"])
		}
		if payload["messages"] == nil {
			t.Fatalf("messages missing: %#v", payload)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer backend.Close()

	h := newTestHandler(backend.URL + "/v1")
	req := httptest.NewRequest(http.MethodPost, "/openai/v1/chat/completions", strings.NewReader(`{"model":"gemini-2.5-pro","messages":[{"role":"user","content":"hello"}]}`))
	rec := httptest.NewRecorder()

	h.ChatCompletionsHandler(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s", rec.Code, rec.Body.String())
	}
	if rec.Body.String() != `{"ok":true}` {
		t.Fatalf("body = %s", rec.Body.String())
	}
}

func TestDirectForwardHandlerRejectsNonPost(t *testing.T) {
	h := newTestHandler("http://backend.example/v1")
	req := httptest.NewRequest(http.MethodGet, "/openai/v1/embeddings", nil)
	rec := httptest.NewRecorder()

	h.DirectForwardHandler("/v1/embeddings")(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d body = %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "method not allowed") {
		t.Fatalf("body = %s", rec.Body.String())
	}
}

func TestModelsHandlerIncludesModelMappingAliases(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/models" {
			t.Fatalf("backend path = %q", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"object":"list","data":[{"id":"backend-gemini","object":"model"}]}`))
	}))
	defer backend.Close()

	h := newTestHandler(backend.URL + "/v1")
	req := httptest.NewRequest(http.MethodGet, "/openai/v1/models", nil)
	rec := httptest.NewRecorder()

	h.ModelsHandler(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s", rec.Code, rec.Body.String())
	}
	var body struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	seen := map[string]bool{}
	for _, model := range body.Data {
		seen[model.ID] = true
	}
	if !seen["backend-gemini"] || !seen["gemini-2.5-pro"] || !seen["text-embedding-3-small"] {
		t.Fatalf("models = %#v", body.Data)
	}
}

func TestChatCompletionsHandlerRetriesTransientBackendError(t *testing.T) {
	attempts := 0
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		if attempts == 1 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer backend.Close()

	h := newTestHandler(backend.URL + "/v1")
	h.executor.Config = reliability.RetryConfig{Enabled: true, MaxAttempts: 2, Sleep: func(time.Duration) {}}
	req := httptest.NewRequest(http.MethodPost, "/openai/v1/chat/completions", strings.NewReader(`{"model":"gemini-2.5-pro","messages":[{"role":"user","content":"hello"}]}`))
	rec := httptest.NewRecorder()

	h.ChatCompletionsHandler(rec, req)

	if rec.Code != http.StatusOK || attempts != 2 {
		t.Fatalf("status=%d attempts=%d body=%s", rec.Code, attempts, rec.Body.String())
	}
}

func TestRateLimitMiddlewareDisabledByDefault(t *testing.T) {
	h := newTestHandler("http://backend.example/v1")
	req := httptest.NewRequest(http.MethodPost, "/openai/v1/chat/completions", strings.NewReader(`{}`))
	rec := httptest.NewRecorder()
	called := 0

	h.RateLimit(func(w http.ResponseWriter, r *http.Request) {
		called++
		w.WriteHeader(http.StatusAccepted)
	})(rec, req)

	if rec.Code != http.StatusAccepted || called != 1 {
		t.Fatalf("status=%d called=%d body=%s", rec.Code, called, rec.Body.String())
	}
}

func TestRateLimitMiddlewareRejectsWhenEnabled(t *testing.T) {
	h := newTestHandler("http://backend.example/v1")
	h.limiter = reliability.NewRateLimiter(true, 60, 1)
	next := h.RateLimit(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusAccepted)
	})

	first := httptest.NewRecorder()
	req1 := httptest.NewRequest(http.MethodPost, "/openai/v1/chat/completions", strings.NewReader(`{}`))
	req1.RemoteAddr = "127.0.0.1:1234"
	next(first, req1)
	if first.Code != http.StatusAccepted {
		t.Fatalf("first status=%d body=%s", first.Code, first.Body.String())
	}

	second := httptest.NewRecorder()
	req2 := httptest.NewRequest(http.MethodPost, "/openai/v1/chat/completions", strings.NewReader(`{}`))
	req2.RemoteAddr = "127.0.0.1:1234"
	next(second, req2)
	if second.Code != http.StatusTooManyRequests {
		t.Fatalf("second status=%d body=%s", second.Code, second.Body.String())
	}
}
