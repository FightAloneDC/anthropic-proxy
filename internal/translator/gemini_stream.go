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
	normalizer    *StreamNormalizer
}

// NewGeminiStreamTranslator creates a Gemini stream translator.
func NewGeminiStreamTranslator(emit func(data interface{})) *GeminiStreamTranslator {
	st := &GeminiStreamTranslator{
		emit:          emit,
		toolCallNames: map[int]string{},
		toolCallArgs:  map[int]string{},
	}
	st.normalizer = NewStreamNormalizer(
		func(content string) { st.handleNormalizedContent(content) },
		func(reasoning string) { st.handleNormalizedReasoning(reasoning) },
	)
	return st
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

	// Use normalizer to handle thinking tags in content
	st.normalizer.ProcessChunk(ch.Delta.Content, ch.Delta.Reasoning)

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
		// Flush any buffered content before finish
		st.normalizer.Flush()

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
		st.toolCallNames = map[int]string{}
		st.toolCallArgs = map[int]string{}

		st.emit(types.GeminiResponse{Candidates: []types.GeminiCandidate{{
			Index:        ch.Index,
			FinishReason: MapGeminiFinishReason(*ch.FinishReason),
		}}})
	}
}

// handleNormalizedContent handles content emitted by the normalizer
func (st *GeminiStreamTranslator) handleNormalizedContent(content string) {
	if content == "" {
		return
	}

	st.emit(types.GeminiResponse{Candidates: []types.GeminiCandidate{{
		Content: &types.GeminiContent{
			Role:  "model",
			Parts: []types.GeminiPart{{Text: content}},
		},
	}}})
}

// handleNormalizedReasoning handles reasoning emitted by the normalizer
func (st *GeminiStreamTranslator) handleNormalizedReasoning(reasoning string) {
	if reasoning == "" {
		return
	}

	st.emit(types.GeminiResponse{Candidates: []types.GeminiCandidate{{
		Content: &types.GeminiContent{
			Role:  "model",
			Parts: []types.GeminiPart{{Text: reasoning, Thought: true}},
		},
	}}})
}

// Flush emits any remaining buffered content.
func (st *GeminiStreamTranslator) Flush() {
	st.normalizer.Flush()
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
