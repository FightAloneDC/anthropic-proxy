package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"anthropic-proxy/internal/config"
	"anthropic-proxy/internal/store"
	"anthropic-proxy/internal/types"
)

func TestGeminiGenerateContentHandler(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			t.Fatalf("backend path = %q", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer backend-key" {
			t.Fatalf("authorization = %q", r.Header.Get("Authorization"))
		}

		var req types.OpenAIRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode backend request: %v", err)
		}
		if req.Model != "backend-gemini" {
			t.Fatalf("model = %q", req.Model)
		}
		if req.Stream {
			t.Fatalf("stream = true")
		}
		if len(req.Messages) != 2 || req.Messages[0].Role != "system" || req.Messages[1].Content != "hello" {
			t.Fatalf("messages = %#v", req.Messages)
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(types.OpenAIResponse{
			Model: "backend-gemini",
			Choices: []types.Choice{{
				Index: 0,
				Message: types.OpenAIMsg{
					Role:    "assistant",
					Content: "hi from backend",
				},
				FinishReason: "stop",
			}},
			Usage: &types.OpenAIUsage{PromptTokens: 3, CompletionTokens: 4, TotalTokens: 7},
		})
	}))
	defer backend.Close()

	h := newTestHandler(backend.URL + "/v1")
	req := httptest.NewRequest(http.MethodPost, "/gemini/v1beta/models/gemini-2.5-pro:generateContent", strings.NewReader(`{
		"systemInstruction":{"parts":[{"text":"be helpful"}]},
		"contents":[{"role":"user","parts":[{"text":"hello"}]}]
	}`))
	req.Header.Set("Authorization", "Bearer client-key")
	rec := httptest.NewRecorder()

	h.GeminiHandler(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s", rec.Code, rec.Body.String())
	}
	var resp types.GeminiResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if got := resp.Candidates[0].Content.Parts[0].Text; got != "hi from backend" {
		t.Fatalf("text = %q", got)
	}
	if resp.UsageMetadata == nil || resp.UsageMetadata.TotalTokenCount != 7 {
		t.Fatalf("usage = %#v", resp.UsageMetadata)
	}
}

func TestGeminiStreamGenerateContentHandler(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			t.Fatalf("backend path = %q", r.URL.Path)
		}
		var req types.OpenAIRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode backend request: %v", err)
		}
		if !req.Stream || req.StreamOptions == nil || !req.StreamOptions.IncludeUsage {
			t.Fatalf("stream request = %#v", req)
		}

		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("data: {\"id\":\"chatcmpl-1\",\"model\":\"backend-gemini\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"hello\"}}]}\n\n"))
		_, _ = w.Write([]byte("data: {\"id\":\"chatcmpl-1\",\"model\":\"backend-gemini\",\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"stop\"}]}\n\n"))
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer backend.Close()

	h := newTestHandler(backend.URL)
	req := httptest.NewRequest(http.MethodPost, "/gemini/v1beta/models/gemini-2.5-pro:streamGenerateContent", strings.NewReader(`{
		"contents":[{"role":"user","parts":[{"text":"hello"}]}]
	}`))
	rec := httptest.NewRecorder()

	h.GeminiHandler(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if !strings.Contains(body, `"text":"hello"`) {
		t.Fatalf("stream body missing text event: %s", body)
	}
	if !strings.Contains(body, `"finishReason":"STOP"`) {
		t.Fatalf("stream body missing finish event: %s", body)
	}
}

func TestGeminiEmbedContentHandler(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/embeddings" {
			t.Fatalf("backend path = %q", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer backend-key" {
			t.Fatalf("authorization = %q", r.Header.Get("Authorization"))
		}

		var req types.OpenAIEmbeddingsRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode backend request: %v", err)
		}
		if req.Model != "backend-embedding" || req.Input != "hello\nworld" || req.Dimensions != 768 {
			t.Fatalf("embedding request = %#v", req)
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(types.OpenAIEmbeddingsResponse{
			Data: []types.OpenAIEmbeddingObject{{Embedding: []float64{0.1, 0.2}, Index: 0}},
		})
	}))
	defer backend.Close()

	h := newTestHandler(backend.URL + "/v1")
	req := httptest.NewRequest(http.MethodPost, "/gemini/v1beta/models/text-embedding-3-small:embedContent", strings.NewReader(`{
		"content":{"parts":[{"text":"hello"},{"text":"world"}]},
		"outputDimensionality":768
	}`))
	rec := httptest.NewRecorder()

	h.GeminiHandler(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s", rec.Code, rec.Body.String())
	}
	var resp types.GeminiEmbedContentResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(resp.Embedding.Values) != 2 || resp.Embedding.Values[0] != 0.1 || resp.Embedding.Values[1] != 0.2 {
		t.Fatalf("embedding = %#v", resp.Embedding.Values)
	}
}

func TestGeminiHandlerUnknownEndpoint(t *testing.T) {
	h := newTestHandler("http://backend.example/v1")
	req := httptest.NewRequest(http.MethodPost, "/gemini/v1beta/models/gemini-2.5-pro:unknown", strings.NewReader(`{}`))
	rec := httptest.NewRecorder()

	h.GeminiHandler(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d body = %s", rec.Code, rec.Body.String())
	}
}

func newTestHandler(backendURL string) *Handler {
	return New(&config.Config{
		Backend: config.BackendConfig{URL: backendURL, APIKey: "backend-key"},
		Models: []config.ModelMap{
			{From: "gemini-2.5-pro", To: "backend-gemini"},
			{From: "text-embedding-3-small", To: "backend-embedding"},
		},
	}, store.New(0, 0))
}
