package translator

import (
	"encoding/json"
	"fmt"
	"strings"

	"anthropic-proxy/internal/types"
)

// TranslateRequest converts an Anthropic Messages API request to OpenAI Chat Completions format.
// Returns the OpenAI request and whether thinking is enabled.
func TranslateRequest(req *types.AnthropicRequest) (*types.OpenAIRequest, bool) {
	oai := &types.OpenAIRequest{
		Model:       req.Model,
		MaxTokens:   req.MaxTokens,
		Temperature: req.Temperature,
		TopP:        req.TopP,
		TopK:        req.TopK,
		Stream:      req.Stream,
	}
	if req.Stream {
		oai.StreamOptions = &types.StreamOptions{IncludeUsage: true}
	}

	// Check if thinking is enabled
	// Default: true (include thinking blocks)
	// Only disabled if explicitly set to "disabled"
	thinkingEnabled := true
	if req.Thinking != nil && req.Thinking.Type == "disabled" {
		thinkingEnabled = false
	}

	// When thinking is enabled with budget_tokens, ensure max_tokens is sufficient.
	// Anthropic requires max_tokens > budget_tokens.
	if thinkingEnabled && req.Thinking != nil && req.Thinking.BudgetTokens != nil {
		budget := *req.Thinking.BudgetTokens
		if oai.MaxTokens <= budget {
			oai.MaxTokens = budget + 1
		}
		// Map budget_tokens to reasoning_effort for backends that support it
		switch {
		case budget <= 1024:
			oai.ReasoningEffort = "low"
		case budget <= 4096:
			oai.ReasoningEffort = "medium"
		default:
			oai.ReasoningEffort = "high"
		}
	} else if thinkingEnabled && req.Thinking != nil && req.Thinking.Type == "enabled" {
		// thinking enabled without explicit budget_tokens → default to high effort
		oai.ReasoningEffort = "high"
	}

	// System → system message
	if req.System != nil {
		oai.Messages = append(oai.Messages, translateSystem(req.System))
	}

	// Messages
	for _, msg := range req.Messages {
		oai.Messages = append(oai.Messages, translateMessages(msg)...)
	}

	// Stop sequences
	if len(req.StopSequences) > 0 {
		oai.Stop = req.StopSequences
	}

	// Tools
	for _, t := range req.Tools {
		oai.Tools = append(oai.Tools, types.OpenAITool{
			Type: "function",
			Function: types.ToolFunction{
				Name:        t.Name,
				Description: t.Description,
				Parameters:  t.InputSchema,
			},
		})
	}

	// Tool choice
	if req.ToolChoice != nil {
		oai.ToolChoice = translateToolChoice(req.ToolChoice)
	}

	// Metadata → user
	if req.Metadata != nil && req.Metadata.UserID != "" {
		oai.User = req.Metadata.UserID
	}

	// Response format
	if req.ResponseFormat != nil {
		oai.ResponseFormat = req.ResponseFormat
	}

	return oai, thinkingEnabled
}

func translateSystem(sys interface{}) types.OpenAIMsg {
	msg := types.OpenAIMsg{Role: "system"}
	switch s := sys.(type) {
	case string:
		msg.Content = s
	case []interface{}:
		var texts []string
		var hasCacheControl bool
		var parts []interface{}
		for _, block := range s {
			if b, ok := block.(map[string]interface{}); ok && b["type"] == "text" {
				text := b["text"].(string)
				texts = append(texts, text)
				if cacheControl, ok := b["cache_control"]; ok && cacheControl != nil {
					hasCacheControl = true
					parts = append(parts, map[string]interface{}{
						"type":          "text",
						"text":          text,
						"cache_control": cacheControl,
					})
				} else if hasCacheControl {
					parts = append(parts, map[string]interface{}{"type": "text", "text": text})
				}
			}
		}
		if hasCacheControl {
			msg.Content = parts
		} else {
			msg.Content = strings.Join(texts, "\n")
		}
	}
	return msg
}

// translateMessages converts one Anthropic message into zero or more OpenAI messages.
// A single Anthropic message with tool_result blocks may produce multiple OpenAI messages
// (assistant message + tool messages).
func translateMessages(msg types.AnthropicMsg) []types.OpenAIMsg {
	var result []types.OpenAIMsg

	switch content := msg.Content.(type) {
	case string:
		result = append(result, types.OpenAIMsg{Role: msg.Role, Content: content})

	case []interface{}:
		oaiMsg := types.OpenAIMsg{Role: msg.Role}
		var toolMessages []types.OpenAIMsg

		for _, block := range content {
			b, ok := block.(map[string]interface{})
			if !ok {
				continue
			}

			switch b["type"] {
			case "text":
				text := b["text"].(string)
				if cacheControl, ok := b["cache_control"]; ok && cacheControl != nil {
					// Switch to multi-part format to preserve cache_control
					if existing, ok := oaiMsg.Content.(string); ok && existing != "" {
						oaiMsg.Content = []interface{}{map[string]interface{}{"type": "text", "text": existing}}
					}
					if oaiMsg.Content == nil {
						oaiMsg.Content = []interface{}{}
					}
					oaiMsg.Content = append(oaiMsg.Content.([]interface{}), map[string]interface{}{
						"type":          "text",
						"text":          text,
						"cache_control": cacheControl,
					})
				} else {
					existing, _ := oaiMsg.Content.(string)
					if existing != "" {
						oaiMsg.Content = existing + text
					} else {
						oaiMsg.Content = text
					}
				}

			case "image":
				if source, ok := b["source"].(map[string]interface{}); ok {
					var imageURL string
					switch source["type"] {
					case "base64":
						imageURL = fmt.Sprintf("data:%s;base64,%s", source["media_type"], source["data"])
					case "url":
						imageURL = source["url"].(string)
					}
					if imageURL != "" {
						if oaiMsg.Content == nil {
							oaiMsg.Content = []interface{}{}
						}
						oaiMsg.Content = append(oaiMsg.Content.([]interface{}), map[string]interface{}{
							"type":      "image_url",
							"image_url": map[string]string{"url": imageURL},
						})
					}
				}

			case "file":
				if source, ok := b["source"].(map[string]interface{}); ok {
					if oaiMsg.Content == nil {
						oaiMsg.Content = []interface{}{}
					}
					switch source["type"] {
					case "base64":
						mediaType, _ := source["media_type"].(string)
						data, _ := source["data"].(string)
						if data != "" {
							oaiMsg.Content = append(oaiMsg.Content.([]interface{}), map[string]interface{}{
								"type": "file",
								"file": map[string]string{
									"file_data": fmt.Sprintf("data:%s;base64,%s", mediaType, data),
								},
							})
						}
					case "url":
						url, _ := source["url"].(string)
						if url != "" {
							oaiMsg.Content = append(oaiMsg.Content.([]interface{}), map[string]interface{}{
								"type": "file",
								"file": map[string]string{"file_url": url},
							})
						}
					}
				}

			case "tool_use":
				inputJSON, _ := json.Marshal(b["input"])
				oaiMsg.ToolCalls = append(oaiMsg.ToolCalls, types.ToolCall{
					ID:   b["id"].(string),
					Type: "function",
					Function: types.FunctionCall{
						Name:      b["name"].(string),
						Arguments: string(inputJSON),
					},
				})

			case "tool_result":
				toolMsg := types.OpenAIMsg{
					Role:       "tool",
					ToolCallID: b["tool_use_id"].(string),
				}
				switch c := b["content"].(type) {
				case string:
					toolMsg.Content = c
				case nil:
					toolMsg.Content = ""
				default:
					j, _ := json.Marshal(c)
					toolMsg.Content = string(j)
				}
				toolMessages = append(toolMessages, toolMsg)
			}
		}

		// Only add assistant message if it has content or tool calls
		if oaiMsg.Content != nil || len(oaiMsg.ToolCalls) > 0 {
			result = append(result, oaiMsg)
		}
		result = append(result, toolMessages...)
	}

	return result
}

// translateToolChoice converts Anthropic tool_choice to OpenAI tool_choice format.
//
// Anthropic formats:
//
//	{"type": "auto"}                             → "auto"
//	{"type": "any"}                              → "required"
//	{"type": "tool", "name": "get_weather"}      → {"type": "function", "function": {"name": "get_weather"}}
//	{"type": "none"}                             → "none"
func translateToolChoice(tc interface{}) interface{} {
	m, ok := tc.(map[string]interface{})
	if !ok {
		return tc
	}
	switch m["type"] {
	case "auto":
		return "auto"
	case "any":
		return "required"
	case "none":
		return "none"
	case "tool":
		if name, ok := m["name"].(string); ok {
			return map[string]interface{}{
				"type":     "function",
				"function": map[string]string{"name": name},
			}
		}
	}
	return tc
}
