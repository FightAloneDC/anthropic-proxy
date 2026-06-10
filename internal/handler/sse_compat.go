package handler

import (
	"bufio"
	"bytes"
	"encoding/json"
	"strings"

	"anthropic-proxy/internal/types"
)

// isSSEResponse detects if a body looks like an SSE stream instead of JSON.
func isSSEResponse(body []byte) bool {
	trimmed := bytes.TrimSpace(body)
	if len(trimmed) == 0 {
		return false
	}
	// SSE lines start with "data:", "event:", or "id:"
	return bytes.HasPrefix(trimmed, []byte("data:")) ||
		bytes.HasPrefix(trimmed, []byte("event:")) ||
		bytes.HasPrefix(trimmed, []byte("id:"))
}

// parseSSEToOpenAIResponse buffers an SSE stream body and accumulates chunks
// into a single OpenAIResponse. This handles the edge case where the backend
// returns SSE format even when stream=false was requested.
func parseSSEToOpenAIResponse(body []byte) (*types.OpenAIResponse, bool) {
	scanner := bufio.NewScanner(bytes.NewReader(body))
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	var lastChunk *types.OpenAIChunk
	chunks := 0

	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		data := strings.TrimPrefix(line, "data: ")
		if data == "[DONE]" {
			break
		}

		var chunk types.OpenAIChunk
		if err := json.Unmarshal([]byte(data), &chunk); err != nil {
			continue
		}
		chunks++
		lastChunk = &chunk
	}

	if chunks == 0 || lastChunk == nil {
		return nil, false
	}

	// Accumulate chunks into a single response
	resp := &types.OpenAIResponse{
		ID:    lastChunk.ID,
		Model: lastChunk.Model,
	}

	// Walk all chunks again to accumulate content and tool calls
	scanner2 := bufio.NewScanner(bytes.NewReader(body))
	scanner2.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	var textContent strings.Builder
	var reasoningContent strings.Builder
	toolCallMap := map[int]*types.ToolCall{}
	var toolCallOrder []int
	var finishReason string
	var usage *types.OpenAIUsage

	for scanner2.Scan() {
		line := scanner2.Text()
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		data := strings.TrimPrefix(line, "data: ")
		if data == "[DONE]" {
			break
		}

		var chunk types.OpenAIChunk
		if err := json.Unmarshal([]byte(data), &chunk); err != nil {
			continue
		}

		if chunk.Usage != nil {
			usage = chunk.Usage
		}

		if len(chunk.Choices) == 0 {
			continue
		}
		ch := chunk.Choices[0]

		if ch.Delta.Content != "" {
			textContent.WriteString(ch.Delta.Content)
		}
		if ch.Delta.Reasoning != "" {
			reasoningContent.WriteString(ch.Delta.Reasoning)
		}

		for _, tc := range ch.Delta.ToolCalls {
			if _, exists := toolCallMap[tc.Index]; !exists {
				toolCallOrder = append(toolCallOrder, tc.Index)
				toolCallMap[tc.Index] = &types.ToolCall{
					ID:   tc.ID,
					Type: "function",
					Function: types.FunctionCall{
						Name: tc.Function.Name,
					},
				}
			}
			if tc.Function.Name != "" {
				toolCallMap[tc.Index].Function.Name = tc.Function.Name
			}
			if tc.ID != "" {
				toolCallMap[tc.Index].ID = tc.ID
			}
			if tc.Function.Arguments != "" {
				toolCallMap[tc.Index].Function.Arguments += tc.Function.Arguments
			}
		}

		if ch.FinishReason != nil {
			finishReason = *ch.FinishReason
		}
	}

	msg := types.OpenAIMsg{}
	if textContent.Len() > 0 {
		msg.Content = textContent.String()
	}
	if reasoningContent.Len() > 0 {
		msg.ReasoningContent = reasoningContent.String()
	}
	for _, idx := range toolCallOrder {
		if tc, ok := toolCallMap[idx]; ok {
			msg.ToolCalls = append(msg.ToolCalls, *tc)
		}
	}

	resp.Choices = []types.Choice{{
		Index:        0,
		Message:      msg,
		FinishReason: finishReason,
	}}
	resp.Usage = usage

	return resp, true
}
