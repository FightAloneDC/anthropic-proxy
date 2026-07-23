package translator

import (
	"fmt"
	"time"

	"anthropic-proxy/internal/types"
)

// ResponsesStreamTranslator translates OpenAI Chat Completions SSE chunks
// into OpenAI Responses API SSE events.
type ResponsesStreamTranslator struct {
	started        bool
	finished       bool
	finalResp      *types.ResponsesResponse
	responseID     string
	model          string
	createdAt      int64
	outputIndex    int
	contentIndex   int
	hasText        bool
	hasToolCalls   bool
	textAccum      string
	reasoningAccum string
	toolCalls      []toolCallState
	customTools    map[string]bool
	inReasoning    bool
	lastUsage      *types.OpenAIUsage
	emit           func(event string, data interface{})
	normalizer     *StreamNormalizer
}

type toolCallState struct {
	id        string
	name      string
	arguments string
	// For custom tools: track how much of the unwrapped input we already streamed
	// so we can emit input.delta as raw args arrive.
	inputEmitted int
	isCustom     bool
}

// Flush emits any remaining buffered content and finalizes if needed.
func (st *ResponsesStreamTranslator) Flush() {
	st.normalizer.Flush()
	if !st.finished {
		st.finish(st.lastUsage)
	}
}

// NewResponsesStreamTranslator creates a new Responses API stream translator.
// customTools lists tool names that must be emitted as custom_tool_call.
func NewResponsesStreamTranslator(emit func(event string, data interface{}), responseID string, customTools map[string]bool) *ResponsesStreamTranslator {
	st := &ResponsesStreamTranslator{
		emit:        emit,
		responseID:  responseID,
		customTools: customTools,
	}
	st.normalizer = NewStreamNormalizer(
		func(content string) { st.handleNormalizedContent(content) },
		func(reasoning string) { st.handleNormalizedReasoning(reasoning) },
	)
	return st
}

// ProcessChunk processes a single OpenAI chunk and emits Responses API events.
func (st *ResponsesStreamTranslator) ProcessChunk(chunk *types.OpenAIChunk) {
	// Skip keepalive chunks
	if contains(chunk.ID, "keepalive") {
		return
	}

	// Usage-only chunk (empty choices) — store usage, defer finalization to Flush()
	if len(chunk.Choices) == 0 {
		if chunk.Usage != nil {
			st.lastUsage = chunk.Usage
		}
		return
	}

	ch := chunk.Choices[0]

	// First chunk: emit response.created
	if !st.started {
		st.started = true
		st.model = chunk.Model
		st.createdAt = chunk.Created
		if st.createdAt == 0 {
			st.createdAt = time.Now().Unix()
		}
		st.emit("response.created", types.ResponseCreatedEvent{
			Type: "response.created",
			Response: &types.ResponsesResponse{
				ID:        st.responseID,
				Object:    "response",
				CreatedAt: st.createdAt,
				Model:     chunk.Model,
				Status:    "in_progress",
				Output:    []types.ResponseOutputItem{},
			},
		})
	}

	// Process content through normalizer FIRST (extracts thinking tags).
	// Must happen before tool_calls because backend may send both in the same chunk.
	st.normalizer.ProcessChunk(ch.Delta.Content, ch.Delta.Reasoning)

	// Tool calls
	if len(ch.Delta.ToolCalls) > 0 {
		st.handleToolCalls(ch.Delta.ToolCalls)
	}

	// Finish
	if ch.FinishReason != nil && !st.finished {
		st.finish(chunk.Usage)
	}
}

// handleNormalizedContent handles content emitted by the normalizer
func (st *ResponsesStreamTranslator) handleNormalizedContent(content string) {
	if content == "" {
		return
	}

	// Close reasoning if we were in it
	if st.inReasoning {
		st.inReasoning = false
		st.emit("response.reasoning_text.done", types.ResponseReasoningTextDoneEvent{
			Type:         "response.reasoning_text.done",
			OutputIndex:  st.outputIndex,
			ContentIndex: st.contentIndex,
		})
		st.contentIndex++
	}

	st.handleText(content)
}

// handleNormalizedReasoning handles reasoning emitted by the normalizer
func (st *ResponsesStreamTranslator) handleNormalizedReasoning(reasoning string) {
	if reasoning == "" {
		return
	}

	st.handleReasoning(reasoning)
}

func (st *ResponsesStreamTranslator) messageItemID() string {
	return fmt.Sprintf("msg_%s", st.responseID)
}

func (st *ResponsesStreamTranslator) functionCallItemID(callID string) string {
	if callID == "" {
		return fmt.Sprintf("fc_%s", st.responseID)
	}
	return fmt.Sprintf("fc_%s", callID)
}

func (st *ResponsesStreamTranslator) customToolCallItemID(callID string) string {
	if callID == "" {
		return fmt.Sprintf("ctc_%s", st.responseID)
	}
	return fmt.Sprintf("ctc_%s", callID)
}

func (st *ResponsesStreamTranslator) isCustomTool(name string) bool {
	return st.customTools != nil && st.customTools[name]
}

func (st *ResponsesStreamTranslator) handleToolCalls(toolCalls []types.ToolCallDelta) {
	// Close reasoning if we were in it
	if st.inReasoning {
		st.inReasoning = false
	}

	for _, tc := range toolCalls {
		if tc.ID != "" {
			// New tool call
			st.hasToolCalls = true
			isCustom := st.isCustomTool(tc.Function.Name)
			st.toolCalls = append(st.toolCalls, toolCallState{
				id:       tc.ID,
				name:     tc.Function.Name,
				isCustom: isCustom,
			})

			idx := len(st.toolCalls) - 1
			if isCustom {
				itemID := st.customToolCallItemID(tc.ID)
				// Codex ResponseItem::CustomToolCall requires name/call_id/input.
				st.emit("response.output_item.added", types.ResponseOutputItemAddedEvent{
					Type:        "response.output_item.added",
					OutputIndex: idx + st.outputIndexOffset(),
					Item: &types.ResponseOutputItem{
						Type:   "custom_tool_call",
						ID:     itemID,
						CallID: tc.ID,
						Name:   tc.Function.Name,
						Input:  "",
						Status: "in_progress",
					},
				})
			} else {
				itemID := st.functionCallItemID(tc.ID)
				// Codex ResponseItem::FunctionCall requires name/call_id/arguments.
				st.emit("response.output_item.added", types.ResponseOutputItemAddedEvent{
					Type:        "response.output_item.added",
					OutputIndex: idx + st.outputIndexOffset(),
					Item: &types.ResponseOutputItem{
						Type:      "function_call",
						ID:        itemID,
						CallID:    tc.ID,
						Name:      tc.Function.Name,
						Arguments: strPtr(""),
						Status:    "in_progress",
					},
				})
			}
		}
		if tc.Function.Arguments != "" {
			// Stream arguments / custom input
			if len(st.toolCalls) > 0 {
				idx := len(st.toolCalls) - 1
				st.toolCalls[idx].arguments += tc.Function.Arguments

				if st.toolCalls[idx].isCustom {
					// Emit progressive raw input as it becomes extractable.
					// While args are partial JSON we may not unwrap yet; finish()
					// always emits input.done with the final value.
					input := extractCustomToolInput(st.toolCalls[idx].arguments)
					if len(input) > st.toolCalls[idx].inputEmitted {
						delta := input[st.toolCalls[idx].inputEmitted:]
						st.toolCalls[idx].inputEmitted = len(input)
						itemID := st.customToolCallItemID(st.toolCalls[idx].id)
						st.emit("response.custom_tool_call_input.delta", types.ResponseCustomToolCallInputDeltaEvent{
							Type:        "response.custom_tool_call_input.delta",
							OutputIndex: idx + st.outputIndexOffset(),
							ItemID:      itemID,
							Delta:       delta,
						})
					}
				} else {
					itemID := st.functionCallItemID(st.toolCalls[idx].id)
					st.emit("response.function_call_arguments.delta", types.ResponseFunctionCallArgumentsDeltaEvent{
						Type:        "response.function_call_arguments.delta",
						OutputIndex: idx + st.outputIndexOffset(),
						ItemID:      itemID,
						Delta:       tc.Function.Arguments,
					})
				}
			}
		}
	}
}

func (st *ResponsesStreamTranslator) handleReasoning(reasoning string) {
	if !st.inReasoning {
		st.inReasoning = true
		itemID := st.messageItemID()
		// Codex ResponseItem::Message requires content: Vec (cannot omit).
		st.emit("response.output_item.added", types.ResponseOutputItemAddedEvent{
			Type:        "response.output_item.added",
			OutputIndex: st.outputIndex,
			Item: &types.ResponseOutputItem{
				Type:   "message",
				ID:     itemID,
				Role:   "assistant",
				Status: "in_progress",
				Content: []types.OutputContentBlock{
					{Type: "output_text", Text: ""},
				},
			},
		})

		st.emit("response.content_part.added", types.ResponseContentPartAddedEvent{
			Type:         "response.content_part.added",
			OutputIndex:  st.outputIndex,
			ContentIndex: st.contentIndex,
			ItemID:       itemID,
			Part:         &types.OutputContentBlock{Type: "output_text", Text: ""},
		})
	}

	st.reasoningAccum += reasoning
	st.emit("response.reasoning_text.delta", types.ResponseReasoningTextDeltaEvent{
		Type:         "response.reasoning_text.delta",
		OutputIndex:  st.outputIndex,
		ContentIndex: st.contentIndex,
		ItemID:       st.messageItemID(),
		Delta:        reasoning,
	})
}

func (st *ResponsesStreamTranslator) handleText(text string) {
	// Close reasoning if we were in it
	if st.inReasoning {
		st.inReasoning = false
		st.emit("response.reasoning_text.done", types.ResponseReasoningTextDoneEvent{
			Type:         "response.reasoning_text.done",
			OutputIndex:  st.outputIndex,
			ContentIndex: st.contentIndex,
			ItemID:       st.messageItemID(),
		})
		st.contentIndex++
	}

	if !st.hasText {
		st.hasText = true
		itemID := st.messageItemID()
		// Emit output_item.added for message.
		// Codex only sets active_item when this parses as ResponseItem::Message,
		// which requires content (even if text is still empty while streaming).
		st.emit("response.output_item.added", types.ResponseOutputItemAddedEvent{
			Type:        "response.output_item.added",
			OutputIndex: st.outputIndex,
			Item: &types.ResponseOutputItem{
				Type:   "message",
				ID:     itemID,
				Role:   "assistant",
				Status: "in_progress",
				Content: []types.OutputContentBlock{
					{Type: "output_text", Text: ""},
				},
			},
		})

		// Emit content_part.added
		st.emit("response.content_part.added", types.ResponseContentPartAddedEvent{
			Type:         "response.content_part.added",
			OutputIndex:  st.outputIndex,
			ContentIndex: st.contentIndex,
			ItemID:       itemID,
			Part:         &types.OutputContentBlock{Type: "output_text", Text: ""},
		})
	}

	st.textAccum += text
	st.emit("response.output_text.delta", types.ResponseOutputTextDeltaEvent{
		Type:         "response.output_text.delta",
		OutputIndex:  st.outputIndex,
		ContentIndex: st.contentIndex,
		ItemID:       st.messageItemID(),
		Delta:        text,
	})
}

func (st *ResponsesStreamTranslator) finish(usage *types.OpenAIUsage) {
	// Close any open text block
	if st.hasText {
		itemID := st.messageItemID()
		st.emit("response.output_text.done", types.ResponseOutputTextDoneEvent{
			Type:         "response.output_text.done",
			OutputIndex:  st.outputIndex,
			ContentIndex: st.contentIndex,
			ItemID:       itemID,
			Text:         st.textAccum,
		})

		st.emit("response.content_part.done", types.ResponseContentPartDoneEvent{
			Type:         "response.content_part.done",
			OutputIndex:  st.outputIndex,
			ContentIndex: st.contentIndex,
			ItemID:       itemID,
			Part: &types.OutputContentBlock{
				Type: "output_text",
				Text: st.textAccum,
			},
		})

		st.emit("response.output_item.done", types.ResponseOutputItemDoneEvent{
			Type:        "response.output_item.done",
			OutputIndex: st.outputIndex,
			Item: &types.ResponseOutputItem{
				Type:   "message",
				ID:     itemID,
				Role:   "assistant",
				Status: "completed",
				Content: []types.OutputContentBlock{
					{Type: "output_text", Text: st.textAccum},
				},
			},
		})

		st.outputIndex++
	}

	// Close reasoning if still open
	if st.inReasoning {
		st.emit("response.reasoning_text.done", types.ResponseReasoningTextDoneEvent{
			Type:         "response.reasoning_text.done",
			OutputIndex:  st.outputIndex,
			ContentIndex: st.contentIndex,
			ItemID:       st.messageItemID(),
		})
		st.inReasoning = false
	}

	// Close tool calls
	for i, tc := range st.toolCalls {
		idx := i + st.outputIndexOffset()
		if tc.isCustom {
			itemID := st.customToolCallItemID(tc.id)
			input := extractCustomToolInput(tc.arguments)
			// Emit any remaining input that wasn't streamed yet
			if len(input) > tc.inputEmitted {
				delta := input[tc.inputEmitted:]
				st.emit("response.custom_tool_call_input.delta", types.ResponseCustomToolCallInputDeltaEvent{
					Type:        "response.custom_tool_call_input.delta",
					OutputIndex: idx,
					ItemID:      itemID,
					Delta:       delta,
				})
			}
			st.emit("response.custom_tool_call_input.done", types.ResponseCustomToolCallInputDoneEvent{
				Type:        "response.custom_tool_call_input.done",
				OutputIndex: idx,
				ItemID:      itemID,
				Input:       input,
			})
			st.emit("response.output_item.done", types.ResponseOutputItemDoneEvent{
				Type:        "response.output_item.done",
				OutputIndex: idx,
				Item: &types.ResponseOutputItem{
					Type:   "custom_tool_call",
					ID:     itemID,
					CallID: tc.id,
					Name:   tc.name,
					Input:  input,
					Status: "completed",
				},
			})
			continue
		}
		itemID := st.functionCallItemID(tc.id)
		st.emit("response.function_call_arguments.done", types.ResponseFunctionCallArgumentsDoneEvent{
			Type:        "response.function_call_arguments.done",
			OutputIndex: idx,
			ItemID:      itemID,
			Arguments:   tc.arguments,
		})

		st.emit("response.output_item.done", types.ResponseOutputItemDoneEvent{
			Type:        "response.output_item.done",
			OutputIndex: idx,
			Item: &types.ResponseOutputItem{
				Type:      "function_call",
				ID:        itemID,
				CallID:    tc.id,
				Name:      tc.name,
				Arguments: strPtr(tc.arguments),
				Status:    "completed",
			},
		})
	}

	// Build final response for response.completed
	createdAt := st.createdAt
	if createdAt == 0 {
		createdAt = time.Now().Unix()
	}
	resp := &types.ResponsesResponse{
		ID:        st.responseID,
		Object:    "response",
		CreatedAt: createdAt,
		Model:     st.model,
		Status:    "completed",
		Output:    st.buildOutput(),
		Usage:     st.buildUsage(usage),
	}

	st.finalResp = resp
	st.emit("response.completed", types.ResponseCompletedEvent{
		Type:     "response.completed",
		Response: resp,
	})

	st.finished = true
}

func (st *ResponsesStreamTranslator) outputIndexOffset() int {
	offset := 0
	if st.hasText {
		offset++
	}
	return offset
}

func (st *ResponsesStreamTranslator) buildOutput() []types.ResponseOutputItem {
	output := []types.ResponseOutputItem{}

	if st.hasText {
		output = append(output, types.ResponseOutputItem{
			Type:   "message",
			ID:     fmt.Sprintf("msg_%s", st.responseID),
			Role:   "assistant",
			Status: "completed",
			Content: []types.OutputContentBlock{
				{Type: "output_text", Text: st.textAccum},
			},
		})
	}

	for _, tc := range st.toolCalls {
		if tc.isCustom {
			output = append(output, types.ResponseOutputItem{
				Type:   "custom_tool_call",
				CallID: tc.id,
				Name:   tc.name,
				Input:  extractCustomToolInput(tc.arguments),
				Status: "completed",
			})
			continue
		}
		output = append(output, types.ResponseOutputItem{
			Type:      "function_call",
			CallID:    tc.id,
			Name:      tc.name,
			Arguments: strPtr(tc.arguments),
			Status:    "completed",
		})
	}

	return output
}

func strPtr(s string) *string {
	return &s
}

func (st *ResponsesStreamTranslator) buildUsage(usage *types.OpenAIUsage) *types.ResponsesUsage {
	if usage == nil {
		return nil
	}
	u := &types.ResponsesUsage{
		InputTokens:  usage.PromptTokens,
		OutputTokens: usage.CompletionTokens,
		TotalTokens:  usage.TotalTokens,
	}
	if usage.PromptCacheHitTokens != nil {
		u.InputTokensDetails = &types.InputTokensDetails{
			CachedTokens: *usage.PromptCacheHitTokens,
		}
	}
	return u
}

// FinalResponse returns the accumulated ResponsesResponse from the stream.
func (st *ResponsesStreamTranslator) FinalResponse() *types.ResponsesResponse {
	return st.finalResp
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsSubstr(s, substr))
}

func containsSubstr(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
