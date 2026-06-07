package handler

import (
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

func TestMessagesHandlerValidationError(t *testing.T) {
	h := newTestHandler("http://backend.example/v1")
	req := httptest.NewRequest(http.MethodPost, "/anthropic/v1/messages", strings.NewReader(`{"max_tokens":100,"messages":[{"role":"user","content":"hello"}]}`))
	rec := httptest.NewRecorder()

	h.MessagesHandler(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d body = %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "model is required") {
		t.Fatalf("body = %s", rec.Body.String())
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
