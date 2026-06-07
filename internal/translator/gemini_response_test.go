package translator

import (
	"testing"

	"anthropic-proxy/internal/types"
)

func TestTranslateGeminiResponseTextUsageAndFinishReason(t *testing.T) {
	resp := &types.OpenAIResponse{
		Model: "backend-model",
		Choices: []types.Choice{{
			Index: 0,
			Message: types.OpenAIMsg{
				Role:    "assistant",
				Content: "hello",
			},
			FinishReason: "length",
		}},
		Usage: &types.OpenAIUsage{PromptTokens: 3, CompletionTokens: 4, TotalTokens: 7},
	}

	got := TranslateGeminiResponse(resp)

	if len(got.Candidates) != 1 {
		t.Fatalf("candidates len = %d", len(got.Candidates))
	}
	candidate := got.Candidates[0]
	if candidate.FinishReason != "MAX_TOKENS" {
		t.Fatalf("finish reason = %q", candidate.FinishReason)
	}
	if candidate.Content.Role != "model" || candidate.Content.Parts[0].Text != "hello" {
		t.Fatalf("content = %#v", candidate.Content)
	}
	if got.UsageMetadata == nil || got.UsageMetadata.TotalTokenCount != 7 {
		t.Fatalf("usage = %#v", got.UsageMetadata)
	}
}

func TestTranslateGeminiResponseToolCall(t *testing.T) {
	resp := &types.OpenAIResponse{Choices: []types.Choice{{
		Message: types.OpenAIMsg{ToolCalls: []types.ToolCall{{
			ID:   "call_1",
			Type: "function",
			Function: types.FunctionCall{
				Name:      "get_weather",
				Arguments: `{"city":"Jakarta"}`,
			},
		}}},
		FinishReason: "tool_calls",
	}}}

	got := TranslateGeminiResponse(resp)

	part := got.Candidates[0].Content.Parts[0]
	if part.FunctionCall == nil || part.FunctionCall.Name != "get_weather" {
		t.Fatalf("function call = %#v", part.FunctionCall)
	}
	if got.Candidates[0].FinishReason != "STOP" {
		t.Fatalf("finish reason = %q", got.Candidates[0].FinishReason)
	}
}
