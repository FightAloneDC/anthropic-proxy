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

	return oai, thinkingEnabled
}

func translateSystem(sys interface{}) types.OpenAIMsg {
	msg := types.OpenAIMsg{Role: "system"}
	switch s := sys.(type) {
	case string:
		msg.Content = s
	case []interface{}:
		var texts []string
		for _, block := range s {
			if b, ok := block.(map[string]interface{}); ok && b["type"] == "text" {
				texts = append(texts, b["text"].(string))
			}
		}
		msg.Content = strings.Join(texts, "\n")
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
				existing, _ := oaiMsg.Content.(string)
				if existing != "" {
					oaiMsg.Content = existing + b["text"].(string)
				} else {
					oaiMsg.Content = b["text"].(string)
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
