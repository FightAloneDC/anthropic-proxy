package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"anthropic-proxy/internal/types"
)

func TestValidateAnthropicRequest(t *testing.T) {
	valid := &types.AnthropicRequest{
		Model:     "claude-sonnet-4-6",
		MaxTokens: 100,
		Messages:  []types.AnthropicMsg{{Role: "user", Content: "hello"}},
	}
	if err := validateAnthropicRequest(valid); err != nil {
		t.Fatalf("valid request error = %v", err)
	}

	missingModel := *valid
	missingModel.Model = ""
	if err := validateAnthropicRequest(&missingModel); err == nil || !strings.Contains(err.Error(), "model") {
		t.Fatalf("missing model error = %v", err)
	}

	badRole := *valid
	badRole.Messages = []types.AnthropicMsg{{Role: "system", Content: "hello"}}
	if err := validateAnthropicRequest(&badRole); err == nil || !strings.Contains(err.Error(), "role") {
		t.Fatalf("bad role error = %v", err)
	}
}

func TestValidateResponsesRequest(t *testing.T) {
	valid := &types.ResponsesRequest{Model: "model", Input: "hello"}
	if err := validateResponsesRequest(valid); err != nil {
		t.Fatalf("valid request error = %v", err)
	}

	missingInput := &types.ResponsesRequest{Model: "model"}
	if err := validateResponsesRequest(missingInput); err == nil || !strings.Contains(err.Error(), "input") {
		t.Fatalf("missing input error = %v", err)
	}
}

func TestValidateGeminiRequest(t *testing.T) {
	valid := &types.GeminiRequest{Contents: []types.GeminiContent{{Parts: []types.GeminiPart{{Text: "hello"}}}}}
	if err := validateGeminiRequest(valid); err != nil {
		t.Fatalf("valid request error = %v", err)
	}

	emptyContents := &types.GeminiRequest{}
	if err := validateGeminiRequest(emptyContents); err == nil || !strings.Contains(err.Error(), "contents") {
		t.Fatalf("empty contents error = %v", err)
	}
}

func TestValidateGeminiEmbedContentRequest(t *testing.T) {
	valid := &types.GeminiEmbedContentRequest{Content: types.GeminiContent{Parts: []types.GeminiPart{{Text: "hello"}}}}
	if err := validateGeminiEmbedContentRequest(valid); err != nil {
		t.Fatalf("valid request error = %v", err)
	}

	missingText := &types.GeminiEmbedContentRequest{Content: types.GeminiContent{Parts: []types.GeminiPart{{}}}}
	if err := validateGeminiEmbedContentRequest(missingText); err == nil || !strings.Contains(err.Error(), "text") {
		t.Fatalf("missing text error = %v", err)
	}
}

func TestMessagesHandlerForwardsWithoutPreTranslationValidation(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload map[string]interface{}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		if payload["model"] != "backend-gemini" {
			t.Fatalf("model = %#v", payload["model"])
		}
		messages, ok := payload["messages"].([]interface{})
		if !ok || len(messages) != 1 {
			t.Fatalf("messages = %#v", payload["messages"])
		}
		message := messages[0].(map[string]interface{})
		if message["role"] != "system" {
			t.Fatalf("role = %#v", message["role"])
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"chatcmpl-test","object":"chat.completion","created":1,"model":"backend-gemini","choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}]}`))
	}))
	defer backend.Close()

	h := newTestHandler(backend.URL + "/v1")
	req := httptest.NewRequest(http.MethodPost, "/anthropic/v1/messages", strings.NewReader(`{"model":"gemini-2.5-pro","max_tokens":100,"messages":[{"role":"system","content":"hello"}]}`))
	rec := httptest.NewRecorder()

	h.MessagesHandler(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s", rec.Code, rec.Body.String())
	}
}

func TestResponsesHandlerForwardsWithoutPreTranslationValidation(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload map[string]interface{}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		if payload["model"] != "backend-gemini" {
			t.Fatalf("model = %#v", payload["model"])
		}
		if payload["messages"] != nil {
			t.Fatalf("messages = %#v", payload["messages"])
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"chatcmpl-test","object":"chat.completion","created":1,"model":"backend-gemini","choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}]}`))
	}))
	defer backend.Close()

	h := newTestHandler(backend.URL + "/v1")
	req := httptest.NewRequest(http.MethodPost, "/openai/v1/responses", strings.NewReader(`{"model":"gemini-2.5-pro"}`))
	rec := httptest.NewRecorder()

	h.ResponsesHandler(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s", rec.Code, rec.Body.String())
	}
}

func TestGeminiHandlerValidationError(t *testing.T) {
	h := newTestHandler("http://backend.example/v1")
	req := httptest.NewRequest(http.MethodPost, "/gemini/v1beta/models/gemini-2.5-pro:generateContent", strings.NewReader(`{"contents":[]}`))
	rec := httptest.NewRecorder()

	h.GeminiHandler(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d body = %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "contents") {
		t.Fatalf("body = %s", rec.Body.String())
	}
}
