# Streaming Thinking Tags + Tool Calls Bug Report

**Date:** 2026-06-20
**Version:** v2.2.2+
**Branch:** fix/stream-thinking-tool-calls
**Commit:** 69becb3
**Author:** anthropic-proxy development team

---

## Executive Summary

A critical bug was discovered in the streaming translation layer where tool calls were silently dropped when a backend provider sent both thinking-tagged content and tool_calls in the same SSE chunk. Additionally, a second manifestation caused thinking content to appear after `message_stop` events, corrupting the event stream for downstream clients.

This document details the full investigation, root cause analysis, fix applied, and remaining open questions.

---

## Problem Statement

### Symptom 1: Tool Calls Lost (Original Report)

When using models that emit thinking tags (e.g., `tokenrouter/MiniMax-M3`, `kr/claude-sonnet-4.5-thinking`), the AI agent would:
- Receive and display the chat response correctly
- **Completely ignore tool calling actions** — no tool_use blocks in the output
- Stop responding as if no tools were available

### Symptom 2: Thinking Mixed Into Content (After First Patch Attempt)

After an initial fix attempt, a second issue emerged:
- Thinking/reasoning content was merged with the actual response content
- `<think>...</think>` tags or their extracted text appeared in the `content` field
- The `reasoning` field was empty or missing

### Symptom 3: Thinking Emitted After message_stop (Root Cause Symptom)

Raw event inspection revealed the actual stream ordering:
```
event: content_block_start     → tool_use (index 0) ✅
event: content_block_delta     → tool arguments     ✅
event: content_block_stop                             ✅
event: message_delta           → stop_reason: tool_use ✅
event: message_stop                                   ✅
event: content_block_start     → thinking (index 1)   ❌ AFTER message_stop!
event: content_block_delta     → thinking content      ❌ AFTER message_stop!
```

---

## Root Cause Analysis

### The Backend Behavior

Backend providers like MiniMax-M3 send thinking content and tool_calls in a specific streaming pattern:

```
Chunk 1: {"delta": {"content": "<think>The user is asking"}}
Chunk 2: {"delta": {"content": " about the weather"}}
Chunk 3: {"delta": {"content": "</think>\n\n", "tool_calls": [...]}}
Chunk 4: {"delta": {"tool_calls": [...]}}
Chunk 5: {"finish_reason": "tool_calls"}
```

**Critical observation:** Chunk 3 contains BOTH:
- Content with the closing `</think>` tag
- `tool_calls` array with the first tool call

This is the key pattern that triggers the bug.

### The Bug in Code

In `internal/translator/stream.go` (StreamTranslator) and `internal/translator/responses_stream.go` (ResponsesStreamTranslator), the original code had:

```go
// Tool calls in this chunk
if len(ch.Delta.ToolCalls) > 0 {
    for _, tc := range ch.Delta.ToolCalls {
        // ... emit tool_use blocks
    }
    return  // ← BUG: Early return skips normalizer!
}

// Normalizer never reaches here when tool_calls are present
st.normalizer.ProcessChunk(ch.Delta.Content, ch.Delta.Reasoning)
```

### Data Flow Trace (Before Fix)

```
Backend Chunk 3: content="<think>...</think>\n\n" + tool_calls=[...]

StreamTranslator.ProcessChunk():
  ├─ ch.Delta.ToolCalls is non-empty
  │   ├─ Emit content_block_start (tool_use)
  │   ├─ Emit content_block_delta (tool arguments)
  │   └─ return ← NORMALIZER SKIPPED!
  │
  └─ normalizer.ProcessChunk() NEVER CALLED
      └─ Closing tag </think> never seen by normalizer
          └─ normalizer.inThinkTag remains TRUE
              └─ tagBuffer contains thinking content

Later: StreamTranslator.Flush() called after scanner loop
  ├─ normalizer.Flush()
  │   ├─ inThinkTag=true, tagBuffer has content
  │   ├─ Emit thinking content ← AFTER message_stop!
  │   └─ Set inThinkTag=false
  └─ finished=true, skip finalization
```

### Why This Was Hard to Catch

1. **Works without tool_calls:** When there are no tool_calls, the normalizer processes all content chunks correctly. Thinking tags are extracted and emitted in the right order.

2. **Works with separate chunks:** When content and tool_calls arrive in different chunks (common with GPT models), the normalizer processes content first, then tool_calls are handled separately.

3. **Only fails with same-chunk content+tool_calls:** The bug only manifests when a backend sends both content (with thinking tags) and tool_calls in the exact same SSE chunk. This is a provider-specific behavior (MiniMax-M3, some Claude thinking models).

4. **Different API paths have different code:** The Chat Completions direct forward path (`ChatCompletionsHandler`) uses `StreamNormalizer` directly. The Anthropic API path uses `StreamTranslator`. The Responses API path uses `ResponsesStreamTranslator`. Each had the same bug independently.

---

## Affected Models and Providers

| Model | Provider | Thinking Tag Format | Bug Trigger |
|-------|----------|--------------------:|-------------|
| `tokenrouter/MiniMax-M3` | TokenRouter | `<think>...</think>` | ✅ Same-chunk content+tool_calls |
| `kr/claude-sonnet-4.5-thinking` | Kiro | `<thinking>...</thinking>` | ✅ Split tags across chunks |
| `kr/claude-sonnet-4.5-thinking-agentic` | Kiro | `<thinking>...</thinking>` | ✅ Split tags across chunks |
| `kr/claude-haiku-4.5-thinking` | Kiro | `<think>...</think>` | ✅ Tag-only emission |
| `kr/claude-haiku-4.5-thinking-agentic` | Kiro | `<think>...</think>` | ✅ Tag-only emission |

### Backend Patterns (from api-analysis-v1.md)

**Pattern 1: Dual Field Emission (MiniMax-M3)**
```json
{"delta": {"content": "<think>reasoning", "reasoning": "reasoning"}}
{"delta": {"content": "</think>\n\n", "tool_calls": [...]}}
```
Backend sends identical content in both `reasoning` and `content` fields, with thinking tags wrapping the content field.

**Pattern 2: Tag-Only Emission (Claude Haiku 4.5 Thinking)**
```json
{"delta": {"content": "<think>reasoning</think>\n\nresponse"}}
```
Backend sends thinking content only in `content` field with `<think>` tags, no `reasoning` field.

**Pattern 3: Split Tag Emission (Claude Sonnet 4.5 Thinking)**
```json
{"delta": {"content": "<thinking"}}
{"delta": {"content": ">\nreasoning"}}
{"delta": {"content": "\n</thinking>\n\nresponse"}}
```
Opening tag split across multiple chunks, requiring stateful parsing.

---

## Fix Applied

### Changes Summary

| File | Change |
|------|--------|
| `internal/translator/stream.go` | Move normalizer BEFORE tool_calls, remove early `return` |
| `internal/translator/responses_stream.go` | Same: normalizer first, remove early `return` |
| `internal/handler/handler.go` | ChatCompletionsHandler: normalize content, forward tool_calls separately |

### stream.go (Anthropic API Path)

```go
// BEFORE (buggy):
if len(ch.Delta.ToolCalls) > 0 {
    // ... handle tool calls
    return  // ← skips normalizer
}
st.normalizer.ProcessChunk(ch.Delta.Content, ch.Delta.Reasoning)

// AFTER (fixed):
st.normalizer.ProcessChunk(ch.Delta.Content, ch.Delta.Reasoning)  // ← always runs
if len(ch.Delta.ToolCalls) > 0 {
    // ... handle tool calls
    // no return
}
```

### responses_stream.go (Responses API Path)

```go
// BEFORE (buggy):
if len(ch.Delta.ToolCalls) > 0 {
    st.handleToolCalls(ch.Delta.ToolCalls)
    return  // ← skips normalizer
}
st.normalizer.ProcessChunk(ch.Delta.Content, ch.Delta.Reasoning)

// AFTER (fixed):
st.normalizer.ProcessChunk(ch.Delta.Content, ch.Delta.Reasoning)  // ← always runs
if len(ch.Delta.ToolCalls) > 0 {
    st.handleToolCalls(ch.Delta.ToolCalls)
    // no return
}
```

### handler.go (Chat Completions Direct Forward Path)

```go
// BEFORE (buggy):
normalizer.ProcessChunk(ch.Delta.Content, ch.Delta.Reasoning)
// tool_calls completely ignored!

// AFTER (fixed):
normalizer.ProcessChunk(ch.Delta.Content, ch.Delta.Reasoning)
if len(ch.Delta.ToolCalls) > 0 {
    normalizer.Flush()
    // Forward tool_calls as separate chunk (without raw content)
    tcChunk := types.OpenAIChunk{...Delta: types.Delta{ToolCalls: ch.Delta.ToolCalls}}
    j, _ := json.Marshal(tcChunk)
    fmt.Fprintf(w, "data: %s\n\n", string(j))
}
```

### Event Ordering After Fix

```
event: message_start           ✅
event: content_block_start     → thinking (index 0) ✅
event: content_block_delta     → thinking content    ✅
event: content_block_stop                             ✅
event: content_block_start     → text (index 1)      ✅
event: content_block_delta     → clean content        ✅
event: content_block_stop                             ✅
event: content_block_start     → tool_use (index 2)   ✅
event: content_block_delta     → tool arguments       ✅
event: content_block_stop                             ✅
event: message_delta           → stop_reason: tool_use ✅
event: message_stop                                   ✅
```

---

## Normalization Flow Reference

### Non-Streaming (`NormalizeContent` in normalize.go)

Uses regex to extract complete thinking tags from the full response content.

```
Input:  content="<think>reasoning</think>actual response"
        reasoning_content=""

Output: content="actual response"
        reasoning_content="reasoning"
```

### Streaming (`StreamNormalizer` in normalize.go)

Stateful parser that handles tags split across chunk boundaries.

**State:**
- `inThinkTag` — currently inside a thinking block
- `tagBuffer` — content buffered inside a thinking tag
- `hasReasoning` — backend already provides `reasoning_content` field
- `openTagBuffer` — buffer for incomplete opening tags

**Flow per chunk:**
```
1. Prepend openTagBuffer to chunk
2. Loop while remaining content:
   a. If inThinkTag:
      - Find closing tag
      - If found → emit reasoning (unless hasReasoning=true), reset state
      - If not found → buffer everything, return
   b. If not inThinkTag:
      - Skip standalone closing tags
      - Find opening tag
      - If not found → emit content directly
      - If found → emit content before tag, find closing tag
        - Closing found → emit reasoning, continue
        - Closing not found → enter inThinkTag, buffer
```

### API-Specific Translators

| API Path | Translator | Normalizer Usage |
|----------|-----------|------------------|
| `/anthropic/v1/messages` (stream) | `StreamTranslator` | `StreamNormalizer` → emit Anthropic SSE events |
| `/anthropic/v1/messages` (non-stream) | `TranslateResponse` | `NormalizeContent` regex |
| `/openai/v1/chat/completions` (stream) | `ChatCompletionsHandler` | `StreamNormalizer` → emit OpenAI chunks |
| `/openai/v1/chat/completions` (non-stream) | `ChatCompletionsHandler` | `NormalizeContent` regex |
| `/openai/v1/responses` (stream) | `ResponsesStreamTranslator` | `StreamNormalizer` → emit Responses SSE events |
| `/openai/v1/responses` (non-stream) | `TranslateResponsesResponse` | `NormalizeContent` regex |
| `/gemini/v1beta/models/...` (stream) | `GeminiStreamTranslator` | `StreamNormalizer` → emit Gemini SSE events |
| `/gemini/v1beta/models/...` (non-stream) | `TranslateGeminiResponse` | `NormalizeContent` regex |

---

## Open Questions

### 1. Responses API + Codex Tool Calling Failures (CONFIRMED - Upstream Issue)

**Observation:** When using OpenAI Codex with non-GPT models via model mapping, tool calling sometimes fails through the Responses API path, but works consistently through the Chat Completions path (OpenCode).

**Confirmed Root Cause (upstream):** This is NOT a bug in our proxy. It is caused by 9router's request normalization in `open-sse/executors/codex.js`. The `normalizeCodexTools()` function filters tool types against an allowlist (`function`, `namespace`, and `CODEX_HOSTED_TOOL_TYPES`), which causes `tool_search` and `custom` tool types to be dropped from the outgoing request before forwarding to the upstream provider.

When `tool_search` is removed, Codex deferred tools (app automation, thread-management, `multi_agent_v1`) cannot be discovered by the model, making them appear missing in the session.

**Reference:** [9router/9router#1907](https://github.com/decolua/9router/issues/1907)

**Impact on our project:** None. This is a 9router-level issue. Our proxy correctly forwards the request as-is; the filtering happens upstream before our layer. No code changes are needed in anthropic-proxy.

**Status:** Upstream issue open. No action required on our side.

### 2. Provider-Specific Chunk Boundaries

Different providers send content and tool_calls in different chunk patterns:

| Provider | Content + ToolCalls | Split Tags | Dual Fields |
|----------|--------------------:|:----------:|:-----------:|
| MiniMax-M3 | Same chunk | No | Yes (`reasoning` field) |
| Claude Thinking (kr/) | Separate chunks | Yes | No |
| GPT Original | Separate chunks | No | No |
| Unknown providers | ??? | ??? | ??? |

The fix handles all known patterns, but new providers may introduce new edge cases.

### 3. Non-Streaming Responses API Normalization

`TranslateResponsesResponse` in `responses.go` handles tool calls. If a backend returns `arguments` as a JSON object instead of a string, `json.Unmarshal` may fail silently, causing tool calls to be dropped in non-streaming mode.

---

## Testing Performed

### Manual Tests

| Test | Endpoint | Model | Result |
|------|----------|-------|--------|
| Streaming no tools | `/openai/v1/chat/completions` | MiniMax-M3 | ✅ Thinking extracted |
| Non-streaming no tools | `/openai/v1/chat/completions` | MiniMax-M3 | ✅ Thinking extracted |
| Streaming with tools | `/openai/v1/chat/completions` | MiniMax-M3 | ✅ Thinking + tool_calls |
| Streaming with tools | `/anthropic/v1/messages` | MiniMax-M3 | ✅ Thinking before tool_use |
| Client test (OpenCode) | Various | MiniMax-M3 | ✅ Working correctly |

### Raw SSE Captures

**Backend direct (MiniMax-M3 streaming with tools):**
```
Chunk 1: content="<think>The user is asking"  (no tool_calls)
Chunk 2: content=" about the weather"       (no tool_calls)
Chunk 3: content="</think>\n\n" + tool_calls=[{id:"call_...", name:"get_weather"}]
Chunk 4: tool_calls=[{arguments:"}"}]
Chunk 5: finish_reason="tool_calls"
```

**Proxy output (after fix):**
```
event: message_start
event: content_block_start  → thinking (extracted from <think> tags)
event: content_block_delta  → thinking content
event: content_block_stop
event: content_block_start  → text (clean content "\n\n")
event: content_block_delta  → text content
event: content_block_stop
event: content_block_start  → tool_use
event: content_block_delta  → tool arguments
event: content_block_stop
event: message_delta        → stop_reason: tool_use
event: message_stop
```

---

## Recommendations

### For Backend Providers

1. Use standard `reasoning` or `reasoning_content` fields for thinking content
2. Avoid sending content and tool_calls in the same SSE chunk when possible
3. If thinking tags must be used in content, ensure complete tags in each chunk

### For Proxy Developers (us)

1. Always process content through normalizer before handling tool_calls
2. Never use early `return` after tool_calls that skips content normalization
3. Add test cases for same-chunk content+tool_calls patterns
4. Consider adding SSE event ordering validation in debug mode

### For 9router (Upstream - Issue #1907)

1. Update `normalizeCodexTools()` to preserve `tool_search` and `custom` tool types for Codex Responses requests
2. Add regression coverage for tool types that survive request normalization: `function`, `namespace`, `tool_search`, `custom`, and hosted tools (`web_search`, `image_generation`, `mcp`, `local_shell`, `code_interpreter`, `computer`)

### For Client Developers

1. Handle out-of-order events gracefully (thinking after message_stop)
2. Validate that tool_use blocks appear before message_delta
3. Log raw SSE events for debugging when tool calls are missing

---

## Related Documents

- `docs/api-analysis-v1.md` — Backend API response analysis
- `docs/proxy-normalization-approach-v1.md` — Normalization design decisions
- `docs/BACKEND_THINKING_BUG_REPORT.md` — Original thinking tag bug report (v2.2.2)
- `docs/STREAMING_FIX_PLAN.md` — Previous streaming content loss fix
- [9router#1907](https://github.com/decolua/9router/issues/1907) — Codex Responses proxy drops tool_search during request normalization (confirmed upstream issue)

---

## Changelog

| Date | Version | Change |
|------|---------|--------|
| 2026-06-20 | v2.2.2+ | Fix thinking+tool_calls same-chunk bug in StreamTranslator, ResponsesStreamTranslator, and ChatCompletionsHandler |
| 2026-06-20 | v2.2.2+ | Confirm Codex Responses tool calling failure is upstream issue (9router#1907), not our proxy |
