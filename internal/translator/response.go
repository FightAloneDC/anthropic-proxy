package translator

import (
	"encoding/json"

	"anthropic-proxy/internal/types"
)

// TranslateResponse converts an OpenAI Chat Completions response to Anthropic Messages format.
func TranslateResponse(resp *types.OpenAIResponse) *types.AnthropicResponse {
	ar := &types.AnthropicResponse{
		ID:      resp.ID,
		Type:    "message",
		Role:    "assistant",
		Model:   resp.Model,
		Content: []types.ContentBlock{},
	}

	if len(resp.Choices) > 0 {
		ch := resp.Choices[0]

		// Finish reason → stop reason
		ar.StopReason = MapFinishReason(ch.FinishReason)

		// Text content
		if ch.Message.Content != nil {
			if text, ok := ch.Message.Content.(string); ok && text != "" {
				ar.Content = append(ar.Content, types.ContentBlock{Type: "text", Text: text})
			}
		}

		// Tool calls
		for _, tc := range ch.Message.ToolCalls {
			var input interface{}
			json.Unmarshal([]byte(tc.Function.Arguments), &input)
			ar.Content = append(ar.Content, types.ContentBlock{
				Type:  "tool_use",
				ID:    tc.ID,
				Name:  tc.Function.Name,
				Input: input,
			})
		}
	}

	// Usage
	if resp.Usage != nil {
		ar.Usage = types.AnthropicUsage{
			InputTokens:  resp.Usage.PromptTokens,
			OutputTokens: resp.Usage.CompletionTokens,
		}
		// Map cache token fields if present (e.g. from vLLM backends)
		if resp.Usage.PromptCacheHitTokens != nil {
			ar.Usage.CacheReadInputTokens = *resp.Usage.PromptCacheHitTokens
		}
		if resp.Usage.PromptCacheMissTokens != nil {
			ar.Usage.CacheCreationInputTokens = *resp.Usage.PromptCacheMissTokens
		}
	}

	return ar
}

// MapFinishReason converts OpenAI finish reason to Anthropic stop reason
func MapFinishReason(reason string) string {
	switch reason {
	case "stop":
		return "end_turn"
	case "length":
		return "max_tokens"
	case "tool_calls":
		return "tool_use"
	default:
		return "end_turn"
	}
}
