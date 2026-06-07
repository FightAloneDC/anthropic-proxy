package translator

import (
	"fmt"

	"anthropic-proxy/internal/types"
)

// TranslateResponsesRequest converts an OpenAI Responses API request to Chat Completions format.
// If prevMessages is non-nil, they are prepended before the input messages.
// Returns the Chat Completions request and whether reasoning is enabled.
func TranslateResponsesRequest(req *types.ResponsesRequest, prevMessages []types.OpenAIMsg) (*types.OpenAIRequest, bool) {
	oai := &types.OpenAIRequest{
		Model:       req.Model,
		MaxTokens:   req.MaxOutputTokens,
		Temperature: req.Temperature,
		TopP:        req.TopP,
		Stream:      req.Stream,
	}

	if req.Stream && req.StreamOptions != nil {
		oai.StreamOptions = req.StreamOptions
	} else if req.Stream {
		oai.StreamOptions = &types.StreamOptions{IncludeUsage: true}
	}

	reasoningEnabled := req.Reasoning != nil

	// Instructions → system message
	if req.Instructions != "" {
		oai.Messages = append(oai.Messages, types.OpenAIMsg{
			Role:    "system",
			Content: req.Instructions,
		})
	}

	// Previous messages (from previous_response_id)
	oai.Messages = append(oai.Messages, prevMessages...)

	// Input → messages
	oai.Messages = append(oai.Messages, translateResponsesInput(req.Input)...)

	// Tools (flat → nested function wrapper)
	// Skip non-function tools (web_search, bash, etc.) as they have no Chat Completions equivalent
	for _, t := range req.Tools {
		if t.Type != "function" || t.Name == "" {
			continue
		}
		oai.Tools = append(oai.Tools, types.OpenAITool{
			Type: "function",
			Function: types.ToolFunction{
				Name:        t.Name,
				Description: t.Description,
				Parameters:  t.Parameters,
			},
		})
	}

	// Tool choice (mostly same format)
	if req.ToolChoice != nil {
		oai.ToolChoice = translateResponsesToolChoice(req.ToolChoice)
	}

	// Text format → response_format
	if req.Text != nil && req.Text.Format != nil {
		oai.ResponseFormat = translateResponseFormat(req.Text.Format)
	}

	// User
	if req.User != "" {
		oai.User = req.User
	}

	return oai, reasoningEnabled
}

// translateResponsesInput converts Responses API input to Chat Completions messages.
func translateResponsesInput(input interface{}) []types.OpenAIMsg {
	switch v := input.(type) {
	case string:
		// Simple string input → single user message
		return []types.OpenAIMsg{{Role: "user", Content: v}}

	case []interface{}:
		return translateInputItems(v)

	default:
		return nil
	}
}

// translateInputItems converts an array of input items to messages.
func translateInputItems(items []interface{}) []types.OpenAIMsg {
	var messages []types.OpenAIMsg

	for _, item := range items {
		m, ok := item.(map[string]interface{})
		if !ok {
			continue
		}

		itemType, _ := m["type"].(string)

		switch itemType {
		case "message":
			msgs := translateInputMessage(m)
			messages = append(messages, msgs...)

		case "function_call":
			// function_call → assistant message with tool_calls
			msg := types.OpenAIMsg{Role: "assistant"}
			callID, _ := m["call_id"].(string)
			name, _ := m["name"].(string)
			args, _ := m["arguments"].(string)
			msg.ToolCalls = []types.ToolCall{
				{
					ID:   callID,
					Type: "function",
					Function: types.FunctionCall{
						Name:      name,
						Arguments: args,
					},
				},
			}
			messages = append(messages, msg)

		case "function_call_output":
			// function_call_output → tool message
			callID, _ := m["call_id"].(string)
			output, _ := m["output"].(string)
			messages = append(messages, types.OpenAIMsg{
				Role:       "tool",
				ToolCallID: callID,
				Content:    output,
			})
		}
	}

	// Merge consecutive assistant messages with tool_calls
	return mergeAssistantToolCalls(messages)
}

// translateInputMessage converts a message input item to OpenAI messages.
func translateInputMessage(m map[string]interface{}) []types.OpenAIMsg {
	role, _ := m["role"].(string)
	if role == "developer" {
		role = "system"
	}

	content := m["content"]

	switch c := content.(type) {
	case string:
		return []types.OpenAIMsg{{Role: role, Content: c}}

	case []interface{}:
		return translateMessageContentBlocks(role, c)

	default:
		return []types.OpenAIMsg{{Role: role}}
	}
}

// translateMessageContentBlocks converts content blocks to OpenAI messages.
func translateMessageContentBlocks(role string, blocks []interface{}) []types.OpenAIMsg {
	msg := types.OpenAIMsg{Role: role}
	var contentParts []interface{}

	for _, block := range blocks {
		b, ok := block.(map[string]interface{})
		if !ok {
			continue
		}

		blockType, _ := b["type"].(string)

		switch blockType {
		case "input_text", "output_text":
			text, _ := b["text"].(string)
			if text != "" {
				contentParts = append(contentParts, map[string]interface{}{
					"type": "text",
					"text": text,
				})
			}

		case "input_image":
			imageURL, _ := b["image_url"].(string)
			if imageURL == "" {
				imageURL, _ = b["file_data"].(string)
			}
			detail, _ := b["detail"].(string)
			if imageURL != "" {
				urlObj := map[string]string{"url": imageURL}
				if detail != "" {
					urlObj["detail"] = detail
				}
				contentParts = append(contentParts, map[string]interface{}{
					"type":      "image_url",
					"image_url": urlObj,
				})
			}
		}
	}

	if len(contentParts) == 0 {
		return []types.OpenAIMsg{msg}
	}

	// If only one text part, use simple string content
	if len(contentParts) == 1 {
		if p, ok := contentParts[0].(map[string]interface{}); ok && p["type"] == "text" {
			msg.Content = p["text"]
			return []types.OpenAIMsg{msg}
		}
	}

	msg.Content = contentParts
	return []types.OpenAIMsg{msg}
}

// mergeAssistantToolCalls merges consecutive assistant messages that have tool_calls
// into a single assistant message with all tool_calls combined.
func mergeAssistantToolCalls(messages []types.OpenAIMsg) []types.OpenAIMsg {
	if len(messages) == 0 {
		return messages
	}

	var result []types.OpenAIMsg
	var pendingToolCalls []types.ToolCall
	var pendingAssistantContent interface{}

	for _, msg := range messages {
		if msg.Role == "assistant" && len(msg.ToolCalls) > 0 {
			// Accumulate tool calls
			pendingToolCalls = append(pendingToolCalls, msg.ToolCalls...)
			if msg.Content != nil {
				pendingAssistantContent = msg.Content
			}
		} else {
			// Flush pending assistant with tool_calls
			if len(pendingToolCalls) > 0 {
				assistantMsg := types.OpenAIMsg{
					Role:      "assistant",
					ToolCalls: pendingToolCalls,
				}
				if pendingAssistantContent != nil {
					assistantMsg.Content = pendingAssistantContent
				}
				result = append(result, assistantMsg)
				pendingToolCalls = nil
				pendingAssistantContent = nil
			}
			result = append(result, msg)
		}
	}

	// Flush remaining
	if len(pendingToolCalls) > 0 {
		assistantMsg := types.OpenAIMsg{
			Role:      "assistant",
			ToolCalls: pendingToolCalls,
		}
		if pendingAssistantContent != nil {
			assistantMsg.Content = pendingAssistantContent
		}
		result = append(result, assistantMsg)
	}

	return result
}

// translateResponsesToolChoice converts Responses API tool_choice to Chat Completions format.
func translateResponsesToolChoice(tc interface{}) interface{} {
	switch v := tc.(type) {
	case string:
		// "none", "auto", "required" are the same
		return v
	case map[string]interface{}:
		tcType, _ := v["type"].(string)
		name, _ := v["name"].(string)
		if tcType == "function" && name != "" {
			return map[string]interface{}{
				"type":     "function",
				"function": map[string]string{"name": name},
			}
		}
	}
	return tc
}

// translateResponseFormat converts Responses API text.format to Chat Completions response_format.
func translateResponseFormat(format *types.TextFormat) interface{} {
	switch format.Type {
	case "json_schema":
		result := map[string]interface{}{
			"type": "json_schema",
			"json_schema": map[string]interface{}{
				"name":   format.Name,
				"schema": format.Schema,
			},
		}
		if format.Strict != nil {
			result["json_schema"].(map[string]interface{})["strict"] = *format.Strict
		}
		return result
	case "json_object":
		return map[string]string{"type": "json_object"}
	default:
		return map[string]string{"type": "text"}
	}
}

// TranslateResponsesResponse converts a Chat Completions response to Responses API format.
func TranslateResponsesResponse(resp *types.OpenAIResponse, requestID string) *types.ResponsesResponse {
	ar := &types.ResponsesResponse{
		ID:        requestID,
		Object:    "response",
		CreatedAt: resp.Created,
		Model:     resp.Model,
		Status:    "completed",
		Output:    []types.ResponseOutputItem{},
	}

	if len(resp.Choices) > 0 {
		ch := resp.Choices[0]

		// Map finish_reason to status
		if ch.FinishReason == "length" {
			ar.Status = "incomplete"
			ar.IncompleteDetails = map[string]string{"reason": "max_output_tokens"}
		}

		// Tool calls → function_call output items
		if len(ch.Message.ToolCalls) > 0 {
			for _, tc := range ch.Message.ToolCalls {
				ar.Output = append(ar.Output, types.ResponseOutputItem{
					Type:      "function_call",
					CallID:    tc.ID,
					Name:      tc.Function.Name,
					Arguments: tc.Function.Arguments,
					Status:    "completed",
				})
			}
		}

		// Text content → message output item
		// Fall back to reasoning_content if content is empty (reasoning models)
		text := ""
		if ch.Message.Content != nil {
			if t, ok := ch.Message.Content.(string); ok {
				text = t
			}
		}
		if text == "" && ch.Message.ReasoningContent != "" {
			text = ch.Message.ReasoningContent
		}
		if text != "" {
			ar.Output = append(ar.Output, types.ResponseOutputItem{
				Type:   "message",
				ID:     fmt.Sprintf("msg_%s", requestID),
				Role:   "assistant",
				Status: "completed",
				Content: []types.OutputContentBlock{
					{Type: "output_text", Text: text},
				},
			})
		}
	}

	// Usage
	if resp.Usage != nil {
		ar.Usage = &types.ResponsesUsage{
			InputTokens:  resp.Usage.PromptTokens,
			OutputTokens: resp.Usage.CompletionTokens,
			TotalTokens:  resp.Usage.TotalTokens,
		}
		if resp.Usage.PromptCacheHitTokens != nil {
			ar.Usage.InputTokensDetails = &types.InputTokensDetails{
				CachedTokens: *resp.Usage.PromptCacheHitTokens,
			}
		}
	}

	return ar
}

// StoredResponseToMessages converts a stored Responses API response back into
// Chat Completions messages for use with previous_response_id.
func StoredResponseToMessages(resp *types.ResponsesResponse) []types.OpenAIMsg {
	var messages []types.OpenAIMsg

	for _, item := range resp.Output {
		switch item.Type {
		case "message":
			if item.Role == "assistant" {
				text := extractOutputText(item.Content)
				if text != "" {
					messages = append(messages, types.OpenAIMsg{
						Role:    "assistant",
						Content: text,
					})
				}
			}

		case "function_call":
			messages = append(messages, types.OpenAIMsg{
				Role: "assistant",
				ToolCalls: []types.ToolCall{
					{
						ID:   item.CallID,
						Type: "function",
						Function: types.FunctionCall{
							Name:      item.Name,
							Arguments: item.Arguments,
						},
					},
				},
			})
		}
	}

	return messages
}

// extractOutputText extracts text from output content blocks.
func extractOutputText(content interface{}) string {
	switch c := content.(type) {
	case string:
		return c
	case []interface{}:
		for _, block := range c {
			if b, ok := block.(map[string]interface{}); ok {
				if b["type"] == "output_text" {
					if text, ok := b["text"].(string); ok {
						return text
					}
				}
			}
		}
	case []types.OutputContentBlock:
		for _, block := range c {
			if block.Type == "output_text" {
				return block.Text
			}
		}
	}
	return ""
}

// GenerateResponseID generates a response ID for the proxy.
func GenerateResponseID() string {
	return fmt.Sprintf("resp_%d", idCounter())
}

var idSeq int64

func idCounter() int64 {
	idSeq++
	return idSeq
}

// StoredResponseToOutputText extracts all output text from a stored response
// for the Responses API output_text convenience field.
func StoredResponseToOutputText(resp *types.ResponsesResponse) string {
	var texts []string
	for _, item := range resp.Output {
		if item.Type == "message" {
			text := extractOutputText(item.Content)
			if text != "" {
				texts = append(texts, text)
			}
		}
	}
	result := ""
	for _, t := range texts {
		result += t
	}
	return result
}
