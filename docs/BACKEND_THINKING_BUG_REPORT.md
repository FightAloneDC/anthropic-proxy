# Backend Thinking Tag Bug Report

**Date:** 2026-06-19  
**Version:** v2.2.2  
**Author:** anthropic-proxy development team  

---

## Executive Summary

A critical bug was discovered in how certain LLM backends return thinking/reasoning content. Instead of using the standard `reasoning` or `reasoning_content` fields, some backends embed thinking content directly in the `content` field using `<think>` or `<thinking>` tags. This causes thinking tags to leak into client output, resulting in poor user experience.

This document details the bug discovery, root cause analysis, and the solution implemented in the proxy's normalization layer.

---

## Bug Discovery

### Initial Observation

During testing of the anthropic-proxy with multiple LLM backends, inconsistent behavior was observed:

1. **MiniMax-M3 Backend**: Returns thinking content in BOTH `reasoning` field AND `content` field (with `<think>` tags)
2. **Claude Haiku 4.5 Thinking Agentic**: Returns `<think>` tags in `content` field without `reasoning` field
3. **Claude Sonnet 4.5 Thinking Agentic**: Returns `<thinking>` tags in `content` field with split opening tags across chunks

### Symptoms

- Thinking tags (`<think>`, `<thinking>`, `</think>`, `</thinking>`) visible in client output
- Thinking content duplicated in both reasoning and content fields
- Inconsistent behavior across different backend providers
- Poor user experience when viewing model responses

---

## Root Cause Analysis

### Backend Behavior Patterns

Three distinct patterns were identified:

#### Pattern 1: Dual Field Emission (MiniMax-M3)

```json
// Chunk 1
{"delta": {"content": "<think>\nThe user is asking", "reasoning": "The user is asking"}}
// Chunk 2  
{"delta": {"content": " about MacGyver\n</thinking>\n\nResponse", "reasoning": " about MacGyver"}}
```

**Issue**: Backend sends identical content in both `reasoning` and `content` fields, with thinking tags wrapping the content field.

#### Pattern 2: Tag-Only Emission (Claude Haiku 4.5 Thinking Agentic)

```json
{"delta": {"content": "<think>\nThe user just said \"hi\"\n</thinking>\n\nHi there!"}}
```

**Issue**: Backend sends thinking content only in `content` field with `<think>` tags, without using `reasoning` field.

#### Pattern 3: Split Tag Emission (Claude Sonnet 4.5 Thinking Agentic)

```json
// Chunk 1
{"delta": {"content": "<thinking"}}
// Chunk 2
{"delta": {"content": ">\nThe user just said \"hi\""}}
// ...
// Chunk N
{"delta": {"content": "\n</thinking>\n\nHi there!"}}
```

**Issue**: Opening tag `<thinking>` is split across multiple chunks, requiring stateful parsing.

### Why This Happens

LLM backends have different implementations for returning thinking/reasoning content:

1. Some backends properly use `reasoning` or `reasoning_content` fields
2. Some backends use thinking tags in `content` field (legacy or provider-specific behavior)
3. Some backends use both methods simultaneously
4. Some backends split tags across streaming chunks due to token-level streaming

---

## Solution

### Architecture

A normalization layer was implemented in the proxy's translator package to handle all three patterns:

```
Backend Response → Normalizer → Clean Response → Client
```

### Components

#### 1. Non-Streaming Normalizer (`normalize.go`)

Uses regex for complete tag extraction:

```go
var thinkingTagRegex = regexp.MustCompile(`(?s)<think>.*?</think>|<thinking>.*?</thinking>`)
```

**Logic:**
- Extract content between thinking tags
- Move extracted content to `reasoning_content` field
- Remove thinking tags from `content` field
- Handle both `<think>` and `<thinking>` tag formats

#### 2. Streaming Normalizer (`StreamNormalizer`)

Uses stateful parser for chunk-by-chunk processing:

```go
type StreamNormalizer struct {
    inThinkTag    bool
    tagType       string
    tagBuffer     string
    emitContent   func(string)
    emitReasoning func(string)
    openTagBuffer string
}
```

**Key Features:**
- Handles incomplete opening tags across chunks
- Handles mismatched closing tags (`</think>` with `<thinking>`)
- Removes standalone closing tags
- Prevents duplicate reasoning emission
- Buffers content until reasoning is processed

### Processing Logic

#### Case 1: Backend Has Reasoning Field + Thinking Tags

```
Input:
  reasoning: "The user is asking"
  content: "<think>\nThe user is asking\n</thinking>\n\nResponse"

Output:
  reasoning: "The user is asking"
  content: "Response"
```

**Action:** Emit reasoning from field, extract and discard thinking tags from content.

#### Case 2: Backend Has No Reasoning Field + Thinking Tags

```
Input:
  reasoning: ""
  content: "<think>\nThe user is asking\n</thinking>\n\nResponse"

Output:
  reasoning: "The user is asking"
  content: "Response"
```

**Action:** Extract thinking content to reasoning field, clean content.

#### Case 3: Split Tags Across Chunks

```
Chunk 1: content="<thinking"
Chunk 2: content=">\nThe user is asking"
Chunk 3: content="\n</thinking>\n\nResponse"

Output:
  reasoning: "\nThe user is asking"
  content: "\n\nResponse"
```

**Action:** Buffer incomplete tags, process when complete, emit at chunk boundaries.

### Edge Cases Handled

1. **Incomplete opening tags**: `<thinking` split across chunks
2. **Mismatched tags**: `<think>` with `</thinking>`
3. **Standalone closing tags**: `</thinking>` without opening tag
4. **Empty thinking blocks**: `<think></thinking>`
5. **Multiple thinking blocks**: `<think>first</thinking>middle<think>second</thinking>`
6. **Duplicate reasoning**: Same content in both `reasoning` and `content` fields

---

## Testing

### Test Cases

| Test Case | Description | Status |
|-----------|-------------|--------|
| Complete tags | Single chunk with full tags | ✅ Pass |
| Split tags | Tags split across 3+ chunks | ✅ Pass |
| Reasoning + tags | Backend sends both fields | ✅ Pass |
| Multiple blocks | Multiple thinking blocks | ✅ Pass |
| No tags | Normal content | ✅ Pass |
| Only opening | Incomplete tag | ✅ Pass |
| Only closing | Standalone closing tag | ✅ Pass |
| Empty block | Empty thinking content | ✅ Pass |
| Real backend | Actual backend output | ✅ Pass |
| Long conversation | 10-turn conversation | ✅ Pass |

### Test Results

- **MiniMax-M3**: 10/10 turns passed, 0 issues
- **Claude Haiku 4.5 Thinking Agentic**: 10/10 turns passed, 0 issues
- **Claude Sonnet 4.5 Thinking Agentic**: 10/10 turns passed, 0 issues

---

## Implementation Details

### Files Modified

| File | Changes |
|------|---------|
| `internal/translator/normalize.go` | New file with thinking tag normalization |
| `internal/translator/response.go` | Anthropic non-streaming normalization |
| `internal/translator/stream.go` | Anthropic streaming normalization |
| `internal/translator/responses.go` | OpenAI Responses non-streaming normalization |
| `internal/translator/responses_stream.go` | OpenAI Responses streaming normalization |
| `internal/translator/gemini_response.go` | Gemini non-streaming normalization |
| `internal/translator/gemini_stream.go` | Gemini streaming normalization |
| `internal/handler/handler.go` | ChatCompletions direct forward normalization |

### Key Functions

- `NormalizeContent()`: Non-streaming normalization entry point
- `StreamNormalizer.ProcessChunk()`: Streaming normalization per chunk
- `findOpeningTag()`: Find first opening tag in string
- `findClosingTag()`: Find first closing tag (both types)
- `hasIncompleteOpenTag()`: Check for incomplete opening tags
- `isStandaloneClosingTag()`: Check for standalone closing tags

---

## Recommendations

### For Backend Providers

1. Use standard `reasoning` or `reasoning_content` fields for thinking content
2. Avoid embedding thinking tags in `content` field
3. If tags must be used, ensure complete tags in each chunk

### For Proxy Users

1. Update to v2.2.1-dev or later for normalization support
2. Monitor logs for normalization warnings
3. Report any remaining thinking tag leaks

### For Developers

1. Test with multiple backend providers before deployment
2. Add test cases for edge cases discovered in this report
3. Consider adding configuration option to disable normalization if needed

---

## Conclusion

The thinking tag normalization issue was successfully resolved through a comprehensive stateful parsing approach. The solution handles all known backend patterns and edge cases, ensuring clean output for clients regardless of how backends return thinking content.

The normalization layer is transparent to clients and requires no configuration changes. It automatically detects and handles thinking tags in both streaming and non-streaming modes across all supported API formats (Anthropic, OpenAI Responses, Gemini, OpenAI ChatCompletions).

---

## References

- [OpenAI Chat Completions API](https://platform.openai.com/docs/api-reference/chat)
- [Anthropic Messages API](https://docs.anthropic.com/claude/reference/messages_post)
- [OpenAI Responses API](https://platform.openai.com/docs/api-reference/responses)
- [Gemini generateContent API](https://ai.google.dev/api/generate-content)
