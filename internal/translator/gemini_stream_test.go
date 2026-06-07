package translator

import (
	"testing"

	"anthropic-proxy/internal/types"
)

func TestGeminiStreamTranslatorTextAndFinish(t *testing.T) {
	var events []types.GeminiResponse
	st := NewGeminiStreamTranslator(func(data interface{}) {
		events = append(events, data.(types.GeminiResponse))
	})
	finish := "stop"

	st.ProcessChunk(&types.OpenAIChunk{Choices: []types.ChunkChoice{{
		Index: 0,
		Delta: types.Delta{Content: "hi"},
	}}})
	st.ProcessChunk(&types.OpenAIChunk{Choices: []types.ChunkChoice{{
		Index:        0,
		FinishReason: &finish,
	}}})

	if len(events) != 2 {
		t.Fatalf("events len = %d", len(events))
	}
	if events[0].Candidates[0].Content.Parts[0].Text != "hi" {
		t.Fatalf("text event = %#v", events[0])
	}
	if events[1].Candidates[0].FinishReason != "STOP" {
		t.Fatalf("finish event = %#v", events[1])
	}
}

func TestGeminiStreamTranslatorUsage(t *testing.T) {
	var events []types.GeminiResponse
	st := NewGeminiStreamTranslator(func(data interface{}) {
		events = append(events, data.(types.GeminiResponse))
	})

	st.ProcessChunk(&types.OpenAIChunk{Usage: &types.OpenAIUsage{PromptTokens: 1, CompletionTokens: 2, TotalTokens: 3}})

	if len(events) != 1 || events[0].UsageMetadata.TotalTokenCount != 3 {
		t.Fatalf("usage event = %#v", events)
	}
}
