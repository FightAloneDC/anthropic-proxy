package translator

import (
	"encoding/json"

	"anthropic-proxy/internal/types"
)

// GeminiStreamTranslator translates OpenAI SSE chunks to Gemini SSE response objects.
type GeminiStreamTranslator struct {
	emit          func(data interface{})
	toolCallNames map[int]string
	toolCallArgs  map[int]string
}

// NewGeminiStreamTranslator creates a Gemini stream translator.
func NewGeminiStreamTranslator(emit func(data interface{})) *GeminiStreamTranslator {
	return &GeminiStreamTranslator{
		emit:          emit,
		toolCallNames: map[int]string{},
		toolCallArgs:  map[int]string{},
	}
}

// ProcessChunk processes one OpenAI stream chunk and emits Gemini response objects.
func (st *GeminiStreamTranslator) ProcessChunk(chunk *types.OpenAIChunk) {
	if len(chunk.Choices) == 0 {
		if chunk.Usage != nil {
			st.emit(types.GeminiResponse{UsageMetadata: geminiUsage(chunk.Usage)})
		}
		return
	}

	ch := chunk.Choices[0]

	if ch.Delta.Content != "" {
		st.emit(types.GeminiResponse{Candidates: []types.GeminiCandidate{{
			Index: ch.Index,
			Content: &types.GeminiContent{
				Role:  "model",
				Parts: []types.GeminiPart{{Text: ch.Delta.Content}},
			},
		}}})
	}

	if len(ch.Delta.ToolCalls) > 0 {
		for _, tc := range ch.Delta.ToolCalls {
			if tc.Function.Name != "" {
				st.toolCallNames[tc.Index] = tc.Function.Name
			}
			if tc.Function.Arguments != "" {
				st.toolCallArgs[tc.Index] += tc.Function.Arguments
			}
		}
	}

	if ch.FinishReason != nil {
		for index, name := range st.toolCallNames {
			var args interface{}
			if err := json.Unmarshal([]byte(st.toolCallArgs[index]), &args); err != nil || args == nil {
				args = map[string]interface{}{}
			}
			st.emit(types.GeminiResponse{Candidates: []types.GeminiCandidate{{
				Index: index,
				Content: &types.GeminiContent{
					Role: "model",
					Parts: []types.GeminiPart{{FunctionCall: &types.GeminiFunctionCall{
						Name: name,
						Args: args,
					}}},
				},
			}}})
		}

		st.emit(types.GeminiResponse{Candidates: []types.GeminiCandidate{{
			Index:        ch.Index,
			FinishReason: MapGeminiFinishReason(*ch.FinishReason),
		}}})
	}
}

func geminiUsage(usage *types.OpenAIUsage) *types.GeminiUsageMetadata {
	if usage == nil {
		return nil
	}
	return &types.GeminiUsageMetadata{
		PromptTokenCount:     usage.PromptTokens,
		CandidatesTokenCount: usage.CompletionTokens,
		TotalTokenCount:      usage.TotalTokens,
	}
}
