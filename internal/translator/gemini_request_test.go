package translator

import (
	"testing"

	"anthropic-proxy/internal/types"
)

func TestTranslateGeminiRequestTextAndSystem(t *testing.T) {
	temp := 0.7
	topP := 0.9
	req := &types.GeminiRequest{
		SystemInstruction: &types.GeminiContent{Parts: []types.GeminiPart{{Text: "be helpful"}}},
		Contents: []types.GeminiContent{{
			Role:  "user",
			Parts: []types.GeminiPart{{Text: "hello"}},
		}},
		GenerationConfig: &types.GeminiGenerationConfig{
			Temperature:     &temp,
			TopP:            &topP,
			MaxOutputTokens: 128,
			StopSequences:   []string{"stop"},
		},
	}

	got := TranslateGeminiRequest(req, "gemini-2.5-pro", false)

	if got.Model != "gemini-2.5-pro" {
		t.Fatalf("model = %q", got.Model)
	}
	if len(got.Messages) != 2 {
		t.Fatalf("messages len = %d", len(got.Messages))
	}
	if got.Messages[0].Role != "system" || got.Messages[0].Content != "be helpful" {
		t.Fatalf("system message = %#v", got.Messages[0])
	}
	if got.Messages[1].Role != "user" || got.Messages[1].Content != "hello" {
		t.Fatalf("user message = %#v", got.Messages[1])
	}
	if got.MaxTokens != 128 || got.Temperature != &temp || got.TopP != &topP {
		t.Fatalf("generation config not mapped: %#v", got)
	}
}

func TestTranslateGeminiRequestImageAndTools(t *testing.T) {
	req := &types.GeminiRequest{
		Contents: []types.GeminiContent{{
			Role: "user",
			Parts: []types.GeminiPart{
				{Text: "describe"},
				{InlineData: &types.GeminiInlineData{MimeType: "image/png", Data: "abc"}},
			},
		}},
		Tools: []types.GeminiTool{{
			FunctionDeclarations: []types.GeminiFunctionDeclaration{{
				Name:        "get_weather",
				Description: "Get weather",
				Parameters:  map[string]interface{}{"type": "object"},
			}},
		}},
	}

	got := TranslateGeminiRequest(req, "gemini-2.5-pro", true)

	if !got.Stream || got.StreamOptions == nil || !got.StreamOptions.IncludeUsage {
		t.Fatalf("stream options not set: %#v", got.StreamOptions)
	}
	parts, ok := got.Messages[0].Content.([]interface{})
	if !ok || len(parts) != 2 {
		t.Fatalf("content parts = %#v", got.Messages[0].Content)
	}
	if len(got.Tools) != 1 || got.Tools[0].Function.Name != "get_weather" {
		t.Fatalf("tools = %#v", got.Tools)
	}
}

func TestTranslateGeminiRequestFunctionCallAndResponse(t *testing.T) {
	req := &types.GeminiRequest{Contents: []types.GeminiContent{
		{
			Role: "model",
			Parts: []types.GeminiPart{{FunctionCall: &types.GeminiFunctionCall{
				Name: "get_weather",
				Args: map[string]interface{}{"city": "Jakarta"},
			}}},
		},
		{
			Role: "user",
			Parts: []types.GeminiPart{{FunctionResponse: &types.GeminiFunctionResponse{
				Name:     "get_weather",
				Response: map[string]interface{}{"temp": 30},
			}}},
		},
	}}

	got := TranslateGeminiRequest(req, "gemini-2.5-pro", false)

	if len(got.Messages) != 2 {
		t.Fatalf("messages len = %d", len(got.Messages))
	}
	if got.Messages[0].Role != "assistant" || len(got.Messages[0].ToolCalls) != 1 {
		t.Fatalf("assistant tool call = %#v", got.Messages[0])
	}
	if got.Messages[1].Role != "tool" || got.Messages[1].Content == "" {
		t.Fatalf("tool message = %#v", got.Messages[1])
	}
}
