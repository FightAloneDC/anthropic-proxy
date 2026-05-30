package main

import (
	"encoding/json"
	"fmt"
	"strings"
)

// TranslateRequest converts an Anthropic Messages API request to OpenAI Chat Completions format.
// Returns the OpenAI request and whether thinking is enabled.
func TranslateRequest(req *AnthropicRequest) (*OpenAIRequest, bool) {
	oai := &OpenAIRequest{
		Model:       req.Model,
		MaxTokens:   req.MaxTokens,
		Temperature: req.Temperature,
		TopP:        req.TopP,
		Stream:      req.Stream,
	}
	if req.Stream {
		oai.StreamOptions = &StreamOptions{IncludeUsage: true}
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
		oai.Tools = append(oai.Tools, OpenAITool{
			Type: "function",
			Function: ToolFunction{
				Name:        t.Name,
				Description: t.Description,
				Parameters:  t.InputSchema,
			},
		})
	}

	return oai, thinkingEnabled
}

func translateSystem(sys interface{}) OpenAIMsg {
	msg := OpenAIMsg{Role: "system"}
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
func translateMessages(msg AnthropicMsg) []OpenAIMsg {
	var result []OpenAIMsg

	switch content := msg.Content.(type) {
	case string:
		result = append(result, OpenAIMsg{Role: msg.Role, Content: content})

	case []interface{}:
		oaiMsg := OpenAIMsg{Role: msg.Role}
		var toolMessages []OpenAIMsg

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
				oaiMsg.ToolCalls = append(oaiMsg.ToolCalls, ToolCall{
					ID:   b["id"].(string),
					Type: "function",
					Function: FunctionCall{
						Name:      b["name"].(string),
						Arguments: string(inputJSON),
					},
				})

			case "tool_result":
				toolMsg := OpenAIMsg{
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

// TranslateResponse converts an OpenAI Chat Completions response to Anthropic Messages format.
func TranslateResponse(resp *OpenAIResponse) *AnthropicResponse {
	ar := &AnthropicResponse{
		ID:      resp.ID,
		Type:    "message",
		Role:    "assistant",
		Model:   resp.Model,
		Content: []ContentBlock{},
	}

	if len(resp.Choices) > 0 {
		ch := resp.Choices[0]

		// Finish reason → stop reason
		ar.StopReason = mapFinishReason(ch.FinishReason)

		// Text content
		if ch.Message.Content != nil {
			if text, ok := ch.Message.Content.(string); ok && text != "" {
				ar.Content = append(ar.Content, ContentBlock{Type: "text", Text: text})
			}
		}

		// Tool calls
		for _, tc := range ch.Message.ToolCalls {
			var input interface{}
			json.Unmarshal([]byte(tc.Function.Arguments), &input)
			ar.Content = append(ar.Content, ContentBlock{
				Type:  "tool_use",
				ID:    tc.ID,
				Name:  tc.Function.Name,
				Input: input,
			})
		}
	}

	// Usage
	if resp.Usage != nil {
		ar.Usage = AnthropicUsage{
			InputTokens:  resp.Usage.PromptTokens,
			OutputTokens: resp.Usage.CompletionTokens,
		}
	}

	return ar
}

func mapFinishReason(reason string) string {
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

// ============================================================
// StreamTranslator: translates OpenAI SSE chunks → Anthropic SSE events
// ============================================================

type StreamTranslator struct {
	started       bool
	blockOpen     bool
	blockIndex    int
	finished      bool
	inThinking    bool
	skipThinking  bool
	emit          func(event string, data interface{})
}

func NewStreamTranslator(emit func(event string, data interface{}), skipThinking bool) *StreamTranslator {
	return &StreamTranslator{emit: emit, skipThinking: skipThinking}
}

func (st *StreamTranslator) ProcessChunk(chunk *OpenAIChunk) {
	// Skip keepalive chunks
	if strings.Contains(chunk.ID, "keepalive") {
		return
	}

	// Usage-only chunk (empty choices) — finalize if we haven't already
	if len(chunk.Choices) == 0 {
		if chunk.Usage != nil && !st.finished {
			if st.blockOpen {
				st.closeBlock()
			}
			st.emitMessageDelta("end_turn", chunk.Usage)
			st.emit("message_stop", EventMessageStop{Type: "message_stop"})
			st.finished = true
		}
		return
	}

	ch := chunk.Choices[0]

	// First chunk: emit message_start
	if !st.started {
		st.started = true
		st.emit("message_start", EventMessageStart{
			Type: "message_start",
			Message: AnthropicResponse{
				ID:      chunk.ID,
				Type:    "message",
				Role:    "assistant",
				Model:   chunk.Model,
				Content: []ContentBlock{},
				Usage:   AnthropicUsage{OutputTokens: 1},
			},
		})
	}

	// Tool calls in this chunk
	if len(ch.Delta.ToolCalls) > 0 {
		for _, tc := range ch.Delta.ToolCalls {
			if tc.ID != "" {
				if st.blockOpen {
					st.closeBlock()
				}
				st.blockOpen = true
				st.emit("content_block_start", EventContentBlockStart{
					Type:  "content_block_start",
					Index: st.blockIndex,
					ContentBlock: ContentBlock{
						Type:  "tool_use",
						ID:    tc.ID,
						Name:  tc.Function.Name,
						Input: map[string]interface{}{},
					},
				})
			}
			if tc.Function.Arguments != "" {
				st.emit("content_block_delta", EventContentBlockDelta{
					Type:  "content_block_delta",
					Index: st.blockIndex,
					Delta: InputJSONDelta{Type: "input_json_delta", PartialJSON: tc.Function.Arguments},
				})
			}
		}
		return
	}

	// Reasoning → thinking blocks (skip if configured)
	if ch.Delta.Reasoning != "" {
		if st.skipThinking {
			return
		}
		if !st.inThinking {
			// Close any open block first
			if st.blockOpen {
				st.closeBlock()
			}
			st.inThinking = true
			st.blockOpen = true
			st.emit("content_block_start", EventContentBlockStart{
				Type:  "content_block_start",
				Index: st.blockIndex,
				ContentBlock: ContentBlock{Type: "thinking", Text: ""},
			})
		}
		st.emit("content_block_delta", EventContentBlockDelta{
			Type:  "content_block_delta",
			Index: st.blockIndex,
			Delta: ThinkingDelta{Type: "thinking_delta", Thinking: ch.Delta.Reasoning},
		})
		return
	}

	// Text content (skip empty strings from reasoning-only chunks)
	if ch.Delta.Content != "" {
		// Close thinking block if we were in one
		if st.inThinking {
			if st.blockOpen {
				st.closeBlock()
			}
			st.inThinking = false
		}
		if !st.blockOpen {
			st.blockOpen = true
			st.emit("content_block_start", EventContentBlockStart{
				Type:  "content_block_start",
				Index: st.blockIndex,
				ContentBlock: ContentBlock{Type: "text", Text: ""},
			})
		}
		st.emit("content_block_delta", EventContentBlockDelta{
			Type:  "content_block_delta",
			Index: st.blockIndex,
			Delta: TextDelta{Type: "text_delta", Text: ch.Delta.Content},
		})
		return
	}

	// Finish
	if ch.FinishReason != nil && !st.finished {
		if st.blockOpen {
			st.closeBlock()
		}
		st.emitMessageDelta(mapFinishReason(*ch.FinishReason), chunk.Usage)
		st.emit("message_stop", EventMessageStop{Type: "message_stop"})
		st.finished = true
	}
}

func (st *StreamTranslator) closeBlock() {
	st.emit("content_block_stop", EventContentBlockStop{
		Type:  "content_block_stop",
		Index: st.blockIndex,
	})
	st.blockIndex++
	st.blockOpen = false
}

func (st *StreamTranslator) emitMessageDelta(stopReason string, usage *OpenAIUsage) {
	u := AnthropicUsage{}
	if usage != nil {
		u.OutputTokens = usage.CompletionTokens
	}
	st.emit("message_delta", EventMessageDelta{
		Type:  "message_delta",
		Delta: MessageDelta{StopReason: stopReason},
		Usage: u,
	})
}
