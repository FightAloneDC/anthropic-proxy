package translator

import (
	"encoding/json"

	"anthropic-proxy/internal/types"
)

// TranslateGeminiResponse converts an OpenAI Chat Completions response to Gemini format.
func TranslateGeminiResponse(resp *types.OpenAIResponse) *types.GeminiResponse {
	gr := &types.GeminiResponse{}

	for _, ch := range resp.Choices {
		candidate := types.GeminiCandidate{
			Index:        ch.Index,
			FinishReason: MapGeminiFinishReason(ch.FinishReason),
			Content: &types.GeminiContent{
				Role:  "model",
				Parts: []types.GeminiPart{},
			},
		}

		// Get content and reasoning
		var textContent string
		if text, ok := ch.Message.Content.(string); ok {
			textContent = text
		}
		reasoningContent := ch.Message.ReasoningContent

		// Normalize thinking tags from content
		cleanContent, cleanReasoning := NormalizeContent(textContent, reasoningContent)
		textContent = cleanContent
		reasoningContent = cleanReasoning

		if textContent != "" {
			candidate.Content.Parts = append(candidate.Content.Parts, types.GeminiPart{Text: textContent})
		}
		if reasoningContent != "" {
			candidate.Content.Parts = append(candidate.Content.Parts, types.GeminiPart{Text: reasoningContent, Thought: true})
		}

		for _, tc := range ch.Message.ToolCalls {
			candidate.Content.Parts = append(candidate.Content.Parts, types.GeminiPart{
				FunctionCall: openAIToolCallToGemini(&tc),
			})
		}

		gr.Candidates = append(gr.Candidates, candidate)
	}

	if resp.Usage != nil {
		gr.UsageMetadata = &types.GeminiUsageMetadata{
			PromptTokenCount:     resp.Usage.PromptTokens,
			CandidatesTokenCount: resp.Usage.CompletionTokens,
			TotalTokenCount:      resp.Usage.TotalTokens,
		}
	}

	return gr
}

// MapGeminiFinishReason converts OpenAI finish reasons to Gemini finish reasons.
func MapGeminiFinishReason(reason string) string {
	switch reason {
	case "stop", "tool_calls":
		return "STOP"
	case "length":
		return "MAX_TOKENS"
	case "content_filter":
		return "SAFETY"
	default:
		return "STOP"
	}
}

func openAIToolCallToGemini(tc *types.ToolCall) *types.GeminiFunctionCall {
	var args interface{}
	if err := json.Unmarshal([]byte(tc.Function.Arguments), &args); err != nil || args == nil {
		args = map[string]interface{}{}
	}
	return &types.GeminiFunctionCall{Name: tc.Function.Name, Args: args}
}
