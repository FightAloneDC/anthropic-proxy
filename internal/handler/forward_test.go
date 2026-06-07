package handler

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
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
