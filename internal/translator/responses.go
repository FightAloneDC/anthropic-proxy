package translator

import (
	"encoding/json"
	"fmt"
	"sync/atomic"

	"anthropic-proxy/internal/types"
)

// TranslateResponsesRequest converts an OpenAI Responses API request to Chat Completions format.
// If prevMessages is non-nil, they are prepended before the input messages.
// Returns the Chat Completions request, whether reasoning is enabled, and the set of
// custom tool names (Codex "exec" etc.) that must be emitted as custom_tool_call.
func TranslateResponsesRequest(req *types.ResponsesRequest, prevMessages []types.OpenAIMsg) (*types.OpenAIRequest, bool, map[string]bool) {
	oai := &types.OpenAIRequest{
		Model:       req.Model,
		MaxTokens:   req.MaxOutputTokens,
		Temperature: req.Temperature,
		TopP:        req.TopP,
		Stream:      req.Stream,
	}
	customTools := map[string]bool{}

	if req.Stream && req.StreamOptions != nil {
		oai.StreamOptions = req.StreamOptions
	} else if req.Stream {
		oai.StreamOptions = &types.StreamOptions{IncludeUsage: true}
	}

	reasoningEnabled := req.Reasoning != nil

	// Forward reasoning.effort to backend
	if req.Reasoning != nil && req.Reasoning.Effort != "" {
		oai.ReasoningEffort = req.Reasoning.Effort
	}

	// Instructions → system message
	if req.Instructions != "" {
		oai.Messages = append(oai.Messages, types.OpenAIMsg{
			Role:    "system",
			Content: req.Instructions,
		})
	}

	// Previous messages (from previous_response_id)
	oai.Messages = append(oai.Messages, prevMessages...)

	// Input → messages (+ tools embedded as additional_tools items, used by Codex)
	msgs, inputTools, inputCustom := translateResponsesInputWithTools(req.Input)
	oai.Messages = append(oai.Messages, msgs...)
	for name := range inputCustom {
		customTools[name] = true
	}

	// Tools from top-level request field (flat → nested function wrapper)
	// Skip non-function/non-custom tools (web_search, bash, etc.)
	for _, t := range req.Tools {
		if tool, isCustom := responsesToolToOpenAI(t.Type, t.Name, t.Description, t.Parameters); tool != nil {
			oai.Tools = append(oai.Tools, *tool)
			if isCustom {
				customTools[t.Name] = true
			}
		}
	}
	// Tools from input[].type == "additional_tools" (Codex)
	oai.Tools = append(oai.Tools, inputTools...)
	// Deduplicate by function name (top-level wins over later duplicates)
	oai.Tools = dedupeOpenAITools(oai.Tools)

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

	return oai, reasoningEnabled, customTools
}

// translateResponsesInput converts Responses API input to Chat Completions messages.
func translateResponsesInput(input interface{}) []types.OpenAIMsg {
	msgs, _, _ := translateResponsesInputWithTools(input)
	return msgs
}

// translateResponsesInputWithTools converts Responses input and also extracts
// tools from Codex-style input items (type=additional_tools / namespace).
// The third return value is the set of custom tool names found in those items.
func translateResponsesInputWithTools(input interface{}) ([]types.OpenAIMsg, []types.OpenAITool, map[string]bool) {
	switch v := input.(type) {
	case string:
		return []types.OpenAIMsg{{Role: "user", Content: v}}, nil, nil
	case []interface{}:
		return translateInputItems(v)
	default:
		return nil, nil, nil
	}
}

// translateInputItems converts an array of input items to messages and tools.
func translateInputItems(items []interface{}) ([]types.OpenAIMsg, []types.OpenAITool, map[string]bool) {
	var messages []types.OpenAIMsg
	var tools []types.OpenAITool
	customTools := map[string]bool{}

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

		case "function_call", "custom_tool_call":
			// function_call / custom_tool_call → assistant message with tool_calls.
			// Custom tools use "input" (raw string); function tools use "arguments" (JSON).
			msg := types.OpenAIMsg{Role: "assistant"}
			callID, _ := m["call_id"].(string)
			name, _ := m["name"].(string)
			args, _ := m["arguments"].(string)
			if itemType == "custom_tool_call" {
				customTools[name] = true
				if input, ok := m["input"].(string); ok && input != "" {
					// Wrap raw custom input as {"input":...} so Chat Completions
					// backends that expect JSON function args still accept it.
					// On the way back we extract via extractCustomToolInput.
					b, _ := json.Marshal(map[string]string{"input": input})
					args = string(b)
				}
			}
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

		case "function_call_output", "custom_tool_call_output":
			// function/custom tool output → tool message
			callID, _ := m["call_id"].(string)
			output, _ := m["output"].(string)
			messages = append(messages, types.OpenAIMsg{
				Role:       "tool",
				ToolCallID: callID,
				Content:    output,
			})

		case "additional_tools":
			// Codex embeds tools in input instead of top-level tools[]
			extracted, names := extractAdditionalTools(m["tools"])
			tools = append(tools, extracted...)
			for name := range names {
				customTools[name] = true
			}

		case "namespace":
			// Nested namespace tools (collaboration.*, etc.)
			extracted, names := extractNamespaceTools(m)
			tools = append(tools, extracted...)
			for name := range names {
				customTools[name] = true
			}
		}
	}

	// Merge consecutive assistant messages with tool_calls
	return mergeAssistantToolCalls(messages), tools, customTools
}

// extractAdditionalTools converts tools from an additional_tools input item.
// Function tools are forwarded as-is. Custom tools (Codex "exec" etc.) are
// approximated as Chat Completions functions with a free-form {"input": string}
// schema; the response path rewrites them back to custom_tool_call + input.
func extractAdditionalTools(raw interface{}) ([]types.OpenAITool, map[string]bool) {
	arr, ok := raw.([]interface{})
	if !ok {
		return nil, nil
	}
	var out []types.OpenAITool
	custom := map[string]bool{}
	for _, item := range arr {
		m, ok := item.(map[string]interface{})
		if !ok {
			continue
		}
		typ, _ := m["type"].(string)
		name, _ := m["name"].(string)
		desc, _ := m["description"].(string)
		switch typ {
		case "function", "custom":
			if tool, isCustom := responsesToolToOpenAI(typ, name, desc, m["parameters"]); tool != nil {
				out = append(out, *tool)
				if isCustom {
					custom[name] = true
				}
			}
		case "namespace":
			extracted, names := extractNamespaceTools(m)
			out = append(out, extracted...)
			for n := range names {
				custom[n] = true
			}
		}
	}
	return out, custom
}

// extractNamespaceTools flattens namespace.tools into function tools named "ns__child".
func extractNamespaceTools(m map[string]interface{}) ([]types.OpenAITool, map[string]bool) {
	ns, _ := m["name"].(string)
	raw, ok := m["tools"].([]interface{})
	if !ok {
		return nil, nil
	}
	var out []types.OpenAITool
	custom := map[string]bool{}
	for _, item := range raw {
		t, ok := item.(map[string]interface{})
		if !ok {
			continue
		}
		typ, _ := t["type"].(string)
		name, _ := t["name"].(string)
		desc, _ := t["description"].(string)
		if (typ != "function" && typ != "custom") || name == "" {
			continue
		}
		fullName := name
		if ns != "" {
			fullName = ns + "__" + name
		}
		if tool, isCustom := responsesToolToOpenAI(typ, fullName, desc, t["parameters"]); tool != nil {
			out = append(out, *tool)
			if isCustom {
				custom[fullName] = true
			}
		}
	}
	return out, custom
}

// responsesToolToOpenAI maps a Responses-style tool to Chat Completions tools[].
// Custom tools are forwarded as free-form functions; isCustom=true so the
// response path can emit custom_tool_call with an "input" field.
func responsesToolToOpenAI(typ, name, desc string, params interface{}) (*types.OpenAITool, bool) {
	if name == "" {
		return nil, false
	}
	switch typ {
	case "function":
		return &types.OpenAITool{
			Type: "function",
			Function: types.ToolFunction{
				Name:        name,
				Description: desc,
				Parameters:  params,
			},
		}, false
	case "custom":
		// Free-form string input: backends that only support JSON function
		// args still get a usable schema. extractCustomToolInput unwraps this
		// on the way back to Responses/Codex.
		if params == nil {
			params = map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"input": map[string]interface{}{
						"type":        "string",
						"description": "Raw custom tool input",
					},
				},
				"required":             []string{"input"},
				"additionalProperties": false,
			}
		}
		return &types.OpenAITool{
			Type: "function",
			Function: types.ToolFunction{
				Name:        name,
				Description: desc,
				Parameters:  params,
			},
		}, true
	default:
		return nil, false
	}
}

// extractCustomToolInput converts Chat Completions function arguments into the
// raw string expected by Responses custom_tool_call.input.
// Accepts: plain string, {"input":"..."}, or any other JSON (returned as-is).
func extractCustomToolInput(args string) string {
	if args == "" {
		return ""
	}
	var obj map[string]interface{}
	if err := json.Unmarshal([]byte(args), &obj); err != nil {
		// Not JSON — treat the whole string as the input (some backends
		// emit free-form text for custom tools).
		return args
	}
	if v, ok := obj["input"]; ok {
		switch t := v.(type) {
		case string:
			return t
		default:
			b, err := json.Marshal(t)
			if err != nil {
				return args
			}
			return string(b)
		}
	}
	// Model returned some other JSON shape — keep it as the input string.
	return args
}

// dedupeOpenAITools keeps the first tool for each function name.
func dedupeOpenAITools(tools []types.OpenAITool) []types.OpenAITool {
	if len(tools) == 0 {
		return tools
	}
	seen := make(map[string]struct{}, len(tools))
	out := make([]types.OpenAITool, 0, len(tools))
	for _, t := range tools {
		name := t.Function.Name
		if name == "" {
			continue
		}
		if _, ok := seen[name]; ok {
			continue
		}
		seen[name] = struct{}{}
		out = append(out, t)
	}
	return out
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

		case "input_file":
			fileData, _ := b["file_data"].(string)
			fileID, _ := b["file_id"].(string)
			filename, _ := b["filename"].(string)
			if fileData != "" {
				fileObj := map[string]string{"file_data": fileData}
				if filename != "" {
					fileObj["filename"] = filename
				}
				contentParts = append(contentParts, map[string]interface{}{
					"type": "file",
					"file": fileObj,
				})
			} else if fileID != "" {
				fileObj := map[string]string{"file_id": fileID}
				if filename != "" {
					fileObj["filename"] = filename
				}
				contentParts = append(contentParts, map[string]interface{}{
					"type": "file",
					"file": fileObj,
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
// customTools lists tool names that must be emitted as custom_tool_call (with "input")
// rather than function_call (with "arguments").
func TranslateResponsesResponse(resp *types.OpenAIResponse, requestID string, customTools map[string]bool) *types.ResponsesResponse {
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

		// Tool calls → function_call or custom_tool_call output items
		if len(ch.Message.ToolCalls) > 0 {
			for _, tc := range ch.Message.ToolCalls {
				name := tc.Function.Name
				if customTools[name] {
					ar.Output = append(ar.Output, types.ResponseOutputItem{
						Type:   "custom_tool_call",
						CallID: tc.ID,
						Name:   name,
						Input:  extractCustomToolInput(tc.Function.Arguments),
						Status: "completed",
					})
					continue
				}
				args := tc.Function.Arguments
				ar.Output = append(ar.Output, types.ResponseOutputItem{
					Type:      "function_call",
					CallID:    tc.ID,
					Name:      name,
					Arguments: &args,
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

		// Normalize thinking tags from content
		cleanContent, cleanReasoning := NormalizeContent(text, ch.Message.ReasoningContent)
		text = cleanContent
		reasoningContent := cleanReasoning

		if text == "" && reasoningContent != "" {
			text = reasoningContent
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

		case "function_call", "custom_tool_call":
			args := ""
			if item.Type == "custom_tool_call" {
				if item.Input != "" {
					b, _ := json.Marshal(map[string]string{"input": item.Input})
					args = string(b)
				}
			} else if item.Arguments != nil {
				args = *item.Arguments
			}
			messages = append(messages, types.OpenAIMsg{
				Role: "assistant",
				ToolCalls: []types.ToolCall{
					{
						ID:   item.CallID,
						Type: "function",
						Function: types.FunctionCall{
							Name:      item.Name,
							Arguments: args,
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
	return atomic.AddInt64(&idSeq, 1)
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
