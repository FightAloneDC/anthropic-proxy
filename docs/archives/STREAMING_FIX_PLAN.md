# Plan: Fix Streaming Content Loss

## Problem

Streaming responses (Anthropic Messages API + OpenAI Responses API) return metadata events but NO content. Non-streaming responses work correctly.

**Symptom:**
```
event: message_start
data: {"type":"message_start",...}          ← metadata OK

event: message_delta
data: {"type":"message_delta",...}          ← NO content_block events before this

event: message_stop
data: {"type":"message_stop"}
```

Expected: `content_block_start`, `content_block_delta`, `content_block_stop` events should appear between `message_start` and `message_delta`.

## Root Cause

The backend sends ALL content in a **single chunk** with `finish_reason: null`, then immediately sends `[DONE]`:

```
data: {"id":"chatcmpl-xxx","choices":[{"delta":{"content":"Hello! ..."},"finish_reason":null}]}
data: [DONE]
```

**The bug:** The `StreamNormalizer` buffers content in `contentBuffer` via `processContentChunk()`, but `Flush()` is never called after the scanner loop ends. The buffered content is silently lost.

**Why it affects both translators:**

| Translator | File | Flush called? |
|---|---|---|
| Gemini | `gemini_stream.go` | Yes (`streamTranslator.Flush()`) |
| Anthropic | `handler.go` `streamResponse()` | **No** |
| Responses API | `handler.go` `responsesStreamResponse()` | **No** |

The Gemini stream translator works because it calls `Flush()`. The other two don't.

## Affected Files

1. `internal/handler/handler.go` - `streamResponse()` and `responsesStreamResponse()`
2. `internal/translator/stream.go` - `StreamTranslator` needs `Flush()` method
3. `internal/translator/responses_stream.go` - `ResponsesStreamTranslator` needs `Flush()` method

## Fix Plan

### Patch 1: Add `Flush()` to `StreamTranslator` (`stream.go`)

Add a `Flush()` method that:
1. Flushes the normalizer's content buffer (emits any remaining text)
2. If a content block was opened but not closed, closes it
3. If the stream ended without a `finish_reason` chunk, emits final `message_delta` + `message_stop`

```go
// Flush emits any remaining buffered content and closes open blocks.
// Call this when the stream ends (scanner loop exits).
func (st *StreamTranslator) Flush() {
    // Flush normalizer buffer (emits content via handleNormalizedContent)
    st.normalizer.Flush()

    // If stream ended without finish_reason, finalize
    if !st.finished {
        if st.blockOpen {
            st.closeBlock()
        }
        st.emitMessageDelta("end_turn", nil)
        st.emit("message_stop", types.EventMessageStop{Type: "message_stop"})
        st.finished = true
    }
}
```

### Patch 2: Add `Flush()` to `ResponsesStreamTranslator` (`responses_stream.go`)

Add a `Flush()` method that:
1. Flushes the normalizer's content buffer
2. If the stream ended without `finish_reason`, calls `finish(nil)`

```go
// Flush emits any remaining buffered content and finalizes if needed.
func (st *ResponsesStreamTranslator) Flush() {
    st.normalizer.Flush()
    if !st.finished {
        st.finish(nil)
    }
}
```

### Patch 3: Call `Flush()` in `streamResponse()` (`handler.go`)

After the scanner loop in `streamResponse()`, add:

```go
// After scanner loop:
translator.Flush()
```

Current code:
```go
for scanner.Scan() {
    // ... parse chunks ...
    translator.ProcessChunk(&chunk)
}
if err := scanner.Err(); err != nil {
    log.Printf("stream read error: %v", err)
}
// ← ADD: translator.Flush()
```

### Patch 4: Call `Flush()` in `responsesStreamResponse()` (`handler.go`)

After the scanner loop in `responsesStreamResponse()`, add:

```go
// After scanner loop:
streamTranslator.Flush()
```

Current code:
```go
for scanner.Scan() {
    // ... parse chunks ...
    streamTranslator.ProcessChunk(&chunk)
}
if err := scanner.Err(); err != nil {
    log.Printf("stream read error: %v", err)
}
// ← ADD: streamTranslator.Flush()
```

### Patch 5: Ensure `Flush()` handles `inThinking` state (`stream.go`)

If the stream ends while inside a thinking tag, `Flush()` must close the thinking block properly. The normalizer's `Flush()` only flushes `contentBuffer`, but if `inThinkTag` is true, the `tagBuffer` content should be emitted as reasoning.

Add to `StreamNormalizer.Flush()` in `normalize.go`:

```go
func (sn *StreamNormalizer) Flush() {
    // If still inside a thinking tag, emit buffered reasoning
    if sn.inThinkTag && sn.tagBuffer != "" {
        if !sn.hasReasoning {
            sn.flushContentBuffer()
            sn.emitReasoning(sn.tagBuffer)
        }
        sn.tagBuffer = ""
        sn.inThinkTag = false
    }
    sn.flushContentBuffer()
}
```

## Verification

After applying patches, verify with:

```bash
# Test 1: Anthropic streaming
curl -s -N http://localhost:8006/anthropic/v1/messages \
  -H "Content-Type: application/json" \
  -H "x-api-key: sk-dummy" \
  -H "anthropic-version: 2023-06-01" \
  -d '{"model":"claude-sonnet-4-6","max_tokens":100,"stream":true,"messages":[{"role":"user","content":"Say hello."}]}'

# Expected: message_start → content_block_start → content_block_delta (×N) → content_block_stop → message_delta → message_stop

# Test 2: Responses API streaming
curl -s -N http://localhost:8006/openai/v1/responses \
  -H "Content-Type: application/json" \
  -d '{"model":"claude-sonnet-4-6","input":"Say hello.","stream":true,"max_output_tokens":100}'

# Expected: response.created → response.output_item.added → response.content_part.added → response.output_text.delta (×N) → ... → response.completed

# Test 3: Anthropic non-streaming (regression)
curl -s http://localhost:8006/anthropic/v1/messages \
  -H "Content-Type: application/json" \
  -H "x-api-key: sk-dummy" \
  -H "anthropic-version: 2023-06-01" \
  -d '{"model":"claude-sonnet-4-6","max_tokens":100,"messages":[{"role":"user","content":"Say hello."}]}'

# Expected: content array with text block
```

## Impact Assessment

| Endpoint | Before | After |
|---|---|---|
| Anthropic streaming | No content | Content present |
| Anthropic non-streaming | Works | No change (regression safe) |
| Responses API streaming | No output | Output present |
| Responses API non-streaming | Works | No change (regression safe) |
| Gemini streaming | Works | No change (already has Flush) |
| OpenAI direct forward | Works | No change (separate path) |

## Risk

Low. The fix adds a `Flush()` call at end-of-stream, which is a standard pattern (already used by Gemini translator). No changes to request translation, model mapping, or non-streaming paths.
