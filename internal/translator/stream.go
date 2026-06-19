package translator

import (
	"strings"

	"anthropic-proxy/internal/types"
)

// StreamTranslator translates OpenAI SSE chunks → Anthropic SSE events
type StreamTranslator struct {
	started      bool
	blockOpen    bool
	blockIndex   int
	finished     bool
	inThinking   bool
	skipThinking bool
	lastUsage    *types.OpenAIUsage
	emit         func(event string, data interface{})
	normalizer   *StreamNormalizer
}

// Flush emits any remaining buffered content and closes open blocks.
// Call this when the stream ends (scanner loop exits).
func (st *StreamTranslator) Flush() {
	st.normalizer.Flush()

	if !st.finished {
		if st.blockOpen {
			st.closeBlock()
		}
		st.emitMessageDelta("end_turn", st.lastUsage)
		st.emit("message_stop", types.EventMessageStop{Type: "message_stop"})
		st.finished = true
	}
}

// NewStreamTranslator creates a new stream translator
func NewStreamTranslator(emit func(event string, data interface{}), skipThinking bool) *StreamTranslator {
	st := &StreamTranslator{emit: emit, skipThinking: skipThinking}
	st.normalizer = NewStreamNormalizer(
		func(content string) { st.handleNormalizedContent(content) },
		func(reasoning string) { st.handleNormalizedReasoning(reasoning) },
	)
	return st
}

// ProcessChunk processes a single OpenAI chunk and emits Anthropic events
func (st *StreamTranslator) ProcessChunk(chunk *types.OpenAIChunk) {
	// Skip keepalive chunks
	if strings.Contains(chunk.ID, "keepalive") {
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

	// First chunk: emit message_start
	if !st.started {
		st.started = true
		st.emit("message_start", types.EventMessageStart{
			Type: "message_start",
			Message: types.AnthropicResponse{
				ID:      chunk.ID,
				Type:    "message",
				Role:    "assistant",
				Model:   chunk.Model,
				Content: []types.ContentBlock{},
				Usage:   types.AnthropicUsage{OutputTokens: 1},
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
				st.emit("content_block_start", types.EventContentBlockStart{
					Type:  "content_block_start",
					Index: st.blockIndex,
					ContentBlock: types.ContentBlock{
						Type:  "tool_use",
						ID:    tc.ID,
						Name:  tc.Function.Name,
						Input: map[string]interface{}{},
					},
				})
			}
			if tc.Function.Arguments != "" {
				st.emit("content_block_delta", types.EventContentBlockDelta{
					Type:  "content_block_delta",
					Index: st.blockIndex,
					Delta: types.InputJSONDelta{Type: "input_json_delta", PartialJSON: tc.Function.Arguments},
				})
			}
		}
		return
	}

	// Use normalizer to handle thinking tags in content
	st.normalizer.ProcessChunk(ch.Delta.Content, ch.Delta.Reasoning)

	// Finish
	if ch.FinishReason != nil && !st.finished {
		if st.blockOpen {
			st.closeBlock()
		}
		st.emitMessageDelta(MapFinishReason(*ch.FinishReason), chunk.Usage)
		st.emit("message_stop", types.EventMessageStop{Type: "message_stop"})
		st.finished = true
	}
}

// handleNormalizedContent handles content emitted by the normalizer
func (st *StreamTranslator) handleNormalizedContent(content string) {
	if content == "" {
		return
	}

	// Close thinking block if we were in one
	if st.inThinking {
		if st.blockOpen {
			st.closeBlock()
		}
		st.inThinking = false
	}

	if !st.blockOpen {
		st.blockOpen = true
		st.emit("content_block_start", types.EventContentBlockStart{
			Type:  "content_block_start",
			Index: st.blockIndex,
			ContentBlock: types.ContentBlock{Type: "text", Text: ""},
		})
	}
	st.emit("content_block_delta", types.EventContentBlockDelta{
		Type:  "content_block_delta",
		Index: st.blockIndex,
		Delta: types.TextDelta{Type: "text_delta", Text: content},
	})
}

// handleNormalizedReasoning handles reasoning emitted by the normalizer
func (st *StreamTranslator) handleNormalizedReasoning(reasoning string) {
	if reasoning == "" || st.skipThinking {
		return
	}

	if !st.inThinking {
		// Close any open block first
		if st.blockOpen {
			st.closeBlock()
		}
		st.inThinking = true
		st.blockOpen = true
		st.emit("content_block_start", types.EventContentBlockStart{
			Type:  "content_block_start",
			Index: st.blockIndex,
			ContentBlock: types.ContentBlock{Type: "thinking", Text: ""},
		})
	}
	st.emit("content_block_delta", types.EventContentBlockDelta{
		Type:  "content_block_delta",
		Index: st.blockIndex,
		Delta: types.ThinkingDelta{Type: "thinking_delta", Thinking: reasoning},
	})
}

func (st *StreamTranslator) closeBlock() {
	st.emit("content_block_stop", types.EventContentBlockStop{
		Type:  "content_block_stop",
		Index: st.blockIndex,
	})
	st.blockIndex++
	st.blockOpen = false
}

func (st *StreamTranslator) emitMessageDelta(stopReason string, usage *types.OpenAIUsage) {
	u := types.AnthropicUsage{}
	if usage != nil {
		u.OutputTokens = usage.CompletionTokens
		if usage.PromptCacheHitTokens != nil {
			u.CacheReadInputTokens = *usage.PromptCacheHitTokens
		}
		if usage.PromptCacheMissTokens != nil {
			u.CacheCreationInputTokens = *usage.PromptCacheMissTokens
		}
	}
	st.emit("message_delta", types.EventMessageDelta{
		Type:  "message_delta",
		Delta: types.MessageDelta{StopReason: stopReason},
		Usage: u,
	})
}
