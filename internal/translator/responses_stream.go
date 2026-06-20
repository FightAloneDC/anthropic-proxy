package translator

import (
	"fmt"

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
	outputIndex    int
	contentIndex   int
	hasText        bool
	hasToolCalls   bool
	textAccum      string
	reasoningAccum string
	toolCalls      []toolCallState
	inReasoning    bool
	lastUsage      *types.OpenAIUsage
	emit           func(event string, data interface{})
	normalizer     *StreamNormalizer
}

type toolCallState struct {
	id        string
	name      string
	arguments string
}

// Flush emits any remaining buffered content and finalizes if needed.
func (st *ResponsesStreamTranslator) Flush() {
	st.normalizer.Flush()
	if !st.finished {
		st.finish(st.lastUsage)
	}
}

// NewResponsesStreamTranslator creates a new Responses API stream translator.
func NewResponsesStreamTranslator(emit func(event string, data interface{}), responseID string) *ResponsesStreamTranslator {
	st := &ResponsesStreamTranslator{
		emit:       emit,
		responseID: responseID,
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
		st.emit("response.created", types.ResponseCreatedEvent{
			Type: "response.created",
			Response: &types.ResponsesResponse{
				ID:        st.responseID,
				Object:    "response",
				CreatedAt: chunk.Created,
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

func (st *ResponsesStreamTranslator) handleToolCalls(toolCalls []types.ToolCallDelta) {
	// Close reasoning if we were in it
	if st.inReasoning {
		st.inReasoning = false
	}

	for _, tc := range toolCalls {
		if tc.ID != "" {
			// New tool call
			st.hasToolCalls = true
			st.toolCalls = append(st.toolCalls, toolCallState{
				id:   tc.ID,
				name: tc.Function.Name,
			})

			// Emit output_item.added for function_call
			st.emit("response.output_item.added", types.ResponseOutputItemAddedEvent{
				Type:  "response.output_item.added",
				Index: len(st.toolCalls) - 1 + st.outputIndexOffset(),
				Item: &types.ResponseOutputItem{
					Type:   "function_call",
					CallID: tc.ID,
					Name:   tc.Function.Name,
					Status: "in_progress",
				},
			})
		}
		if tc.Function.Arguments != "" {
			// Stream arguments
			if len(st.toolCalls) > 0 {
				idx := len(st.toolCalls) - 1
				st.toolCalls[idx].arguments += tc.Function.Arguments

				st.emit("response.function_call_arguments.delta", types.ResponseFunctionCallArgumentsDeltaEvent{
					Type:        "response.function_call_arguments.delta",
					OutputIndex: idx + st.outputIndexOffset(),
					Delta:       tc.Function.Arguments,
				})
			}
		}
	}
}

func (st *ResponsesStreamTranslator) handleReasoning(reasoning string) {
	if !st.inReasoning {
		st.inReasoning = true
		st.emit("response.output_item.added", types.ResponseOutputItemAddedEvent{
			Type:  "response.output_item.added",
			Index: st.outputIndex,
			Item: &types.ResponseOutputItem{
				Type:   "message",
				Role:   "assistant",
				Status: "in_progress",
			},
		})

		st.emit("response.content_part.added", types.ResponseContentPartAddedEvent{
			Type:         "response.content_part.added",
			OutputIndex:  st.outputIndex,
			ContentIndex: st.contentIndex,
			Part:         &types.OutputContentBlock{Type: "output_text"},
		})
	}

	st.reasoningAccum += reasoning
	st.emit("response.reasoning_text.delta", types.ResponseReasoningTextDeltaEvent{
		Type:         "response.reasoning_text.delta",
		OutputIndex:  st.outputIndex,
		ContentIndex: st.contentIndex,
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
		})
		st.contentIndex++
	}

	if !st.hasText {
		st.hasText = true
		// Emit output_item.added for message
		st.emit("response.output_item.added", types.ResponseOutputItemAddedEvent{
			Type:  "response.output_item.added",
			Index: st.outputIndex,
			Item: &types.ResponseOutputItem{
				Type:   "message",
				ID:     fmt.Sprintf("msg_%s", st.responseID),
				Role:   "assistant",
				Status: "in_progress",
			},
		})

		// Emit content_part.added
		st.emit("response.content_part.added", types.ResponseContentPartAddedEvent{
			Type:         "response.content_part.added",
			OutputIndex:  st.outputIndex,
			ContentIndex: st.contentIndex,
			Part:         &types.OutputContentBlock{Type: "output_text"},
		})
	}

	st.textAccum += text
	st.emit("response.output_text.delta", types.ResponseOutputTextDeltaEvent{
		Type:         "response.output_text.delta",
		OutputIndex:  st.outputIndex,
		ContentIndex: st.contentIndex,
		Delta:        text,
	})
}

func (st *ResponsesStreamTranslator) finish(usage *types.OpenAIUsage) {
	// Close any open text block
	if st.hasText {
		st.emit("response.output_text.done", types.ResponseOutputTextDoneEvent{
			Type:         "response.output_text.done",
			OutputIndex:  st.outputIndex,
			ContentIndex: st.contentIndex,
			Text:         st.textAccum,
		})

		st.emit("response.content_part.done", types.ResponseContentPartDoneEvent{
			Type:         "response.content_part.done",
			OutputIndex:  st.outputIndex,
			ContentIndex: st.contentIndex,
			Part: &types.OutputContentBlock{
				Type: "output_text",
				Text: st.textAccum,
			},
		})

		st.emit("response.output_item.done", types.ResponseOutputItemDoneEvent{
			Type:  "response.output_item.done",
			Index: st.outputIndex,
			Item: &types.ResponseOutputItem{
				Type:   "message",
				ID:     fmt.Sprintf("msg_%s", st.responseID),
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
		})
		st.inReasoning = false
	}

	// Close tool calls
	for i, tc := range st.toolCalls {
		idx := i + st.outputIndexOffset()
		st.emit("response.function_call_arguments.done", types.ResponseFunctionCallArgumentsDoneEvent{
			Type:        "response.function_call_arguments.done",
			OutputIndex: idx,
			Arguments:   tc.arguments,
		})

		st.emit("response.output_item.done", types.ResponseOutputItemDoneEvent{
			Type:  "response.output_item.done",
			Index: idx,
			Item: &types.ResponseOutputItem{
				Type:      "function_call",
				CallID:    tc.id,
				Name:      tc.name,
				Arguments: tc.arguments,
				Status:    "completed",
			},
		})
	}

	// Build final response for response.completed
	resp := &types.ResponsesResponse{
		ID:        st.responseID,
		Object:    "response",
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
		output = append(output, types.ResponseOutputItem{
			Type:      "function_call",
			CallID:    tc.id,
			Name:      tc.name,
			Arguments: tc.arguments,
			Status:    "completed",
		})
	}

	return output
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
