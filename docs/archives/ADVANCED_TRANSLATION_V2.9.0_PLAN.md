# v2.9.0 — Advanced Translation

**Status: Completed**
**Priority: Medium**

---

## Overview

Extend the proxy's translation capabilities to support advanced features across all 4 API families: Anthropic Messages, OpenAI Chat Completions, OpenAI Responses, and Gemini generateContent.

**Key architectural insight:** OpenAI `/chat/completions` is a near-pass-through handler (body forwarded as raw bytes with model mapping + drop_fields). Anthropic and Gemini handlers do full translation via typed structs, so new fields require struct + translator changes.

---

## 1. Extended Thinking / Budget Tokens

### Goal
Forward thinking/reasoning configuration to backends that support it, and properly handle reasoning content in responses.

### Current State

| API | Request | Response |
|-----|---------|----------|
| Anthropic | `thinking` consumed by proxy, NOT forwarded. Only affects `max_tokens` bump. | Streaming: `reasoning` → `thinking_delta` ✅. Non-streaming: `reasoning_content` → thinking blocks ❌ (not handled). |
| OpenAI /chat/completions | Pass-through — `reasoning_effort` etc. already forwarded | Pass-through — `reasoning_content` already returned to client |
| OpenAI /responses | `reasoning` struct exists but values (`effort`, `summary`) NOT forwarded to backend | Streaming: `reasoning` → `response.reasoning_text` ✅. Non-streaming: falls back to `reasoning_content` if no `content` |
| Gemini | `thought` parts NOT in type | Streaming: `reasoning` dropped. Non-streaming: `reasoning_content` → text part only if no `content` |

### Changes

#### Anthropic (`internal/translator/request.go`, `internal/types/anthropic.go`)
- [x] Add `Reasoning` field to `OpenAIRequest` (or reuse existing) for `reasoning_effort` / `budget_tokens` mapping
- [x] In `TranslateRequest`: when `thinking.type == "enabled"` with `budget_tokens`, forward as `reasoning_effort` or `max_budget_tokens` to backend (configurable mapping strategy)
- [x] In `TranslateResponse` (non-streaming): extract `reasoning_content` from `OpenAIMsg` into thinking content blocks

#### OpenAI /responses (`internal/translator/responses.go`)
- [x] Forward `reasoning.effort` to backend as `reasoning_effort` in the translated Chat Completions request

#### Gemini (`internal/types/anthropic.go`, `internal/translator/gemini_response.go`, `gemini_stream.go`)
- [x] Add `Thought` field to Gemini content part type
- [x] In response translation: map `reasoning_content` to `thought` parts
- [x] In stream translation: handle `delta.reasoning` → `thought` parts

---

## 2. Response Format / Structured Output

### Goal
Forward structured output configuration so backends can enforce JSON output.

### Current State

| API | Request | Response |
|-----|---------|----------|
| Anthropic | No `response_format` field in `AnthropicRequest` | N/A |
| OpenAI /chat/completions | Pass-through — `response_format` already forwarded | Pass-through |
| OpenAI /responses | `text.format` → `response_format` ✅ | N/A |
| Gemini | `responseMimeType=application/json` → `json_object` ✅. `responseSchema` ❌ (dropped) | N/A |

### Changes

#### Anthropic (`internal/types/anthropic.go`, `internal/translator/request.go`)
- [x] Add `ResponseFormat` field to `AnthropicRequest` (or use passthrough approach)
- [x] In `TranslateRequest`: forward as OpenAI `response_format`

#### Gemini (`internal/types/anthropic.go`, `internal/translator/gemini_request.go`)
- [x] Add `ResponseSchema` to `GeminiGenerationConfig` type
- [x] In `TranslateGeminiRequest`: when `responseMimeType == "application/json"` and `responseSchema` present, forward as `response_format: {type: "json_schema", json_schema: {schema: responseSchema}}`

---

## 3. Image & File Support

### Goal
Support file attachments (PDF, etc.) in addition to images.

### Current State

| API | Request |
|-----|---------|
| Anthropic | Image `base64` ✅, image `url` ✅, file ❌ |
| OpenAI /chat/completions | Pass-through — `image_url` and `file` content types already forwarded |
| OpenAI /responses | `input_image` → `image_url` ✅, `input_file` ❌ |
| Gemini | `inlineData` ✅, `fileData` ✅ |

### Changes

#### Anthropic (`internal/types/anthropic.go`, `internal/translator/request.go`)
- [x] Add `File` content block type to `AnthropicContentBlock` (with `media_type`, `source.type`, `source.data`/`source.url`)
- [x] In `translateMessages`: handle file blocks → OpenAI multipart content (as `file` type or `image_url` with data URI depending on backend support)

#### OpenAI /responses (`internal/translator/responses.go`)
- [x] In `translateInputItems`: handle `input_file` type → Chat Completions multipart content
- [x] Map `file_data` (base64) and `file_id` references

---

## 4. Prompt Caching

### Goal
Forward cache control markers to backends and map cache token usage in responses.

### Current State

| API | Request | Response |
|-----|---------|----------|
| Anthropic | `cache_control` markers NOT in type | Cache usage mapped: `prompt_cache_hit_tokens` → `cache_read_input_tokens` ✅, `prompt_cache_miss_tokens` → `cache_creation_input_tokens` ✅ |
| OpenAI /chat/completions | Pass-through — cache hints already forwarded | Pass-through — cache usage already returned |
| OpenAI /responses | N/A | `prompt_cache_hit_tokens` → `input_tokens_details.cached_tokens` ✅ (non-stream only) |
| Gemini | `cachedContent` NOT in type | Cache usage NOT mapped |

### Changes

#### Anthropic (`internal/types/anthropic.go`, `internal/translator/request.go`)
- [x] Add `CacheControl` field to `AnthropicContentBlock` (or to system/messages)
- [x] In `translateMessages`: preserve `cache_control` markers in the forwarded request
- [x] Decision: forward as-is to backend (if backend supports Anthropic cache format) or translate to OpenAI cache format

#### OpenAI /responses (`internal/translator/responses_stream.go`)
- [x] Map `prompt_cache_hit_tokens` in streaming usage chunks → `input_tokens_details.cached_tokens`

#### Gemini (`internal/types/anthropic.go`, `internal/translator/gemini_request.go`)
- [x] Add `CachedContent` field to `GeminiRequest`
- [x] In `TranslateGeminiRequest`: forward cache reference to backend

---

## Implementation Order

1. **Extended Thinking / Budget Tokens** — highest impact, most requested feature
2. **Response Format / Structured Output** — moderate effort, high value for structured use cases
3. **Prompt Caching** — mostly type additions + forwarding
4. **Image & File Support** — most complex, may need backend-specific handling

---

## Files to Modify

| File | Changes |
|------|---------|
| `internal/types/anthropic.go` | Add new fields to `AnthropicRequest`, `AnthropicContentBlock`, `AnthropicResponse` |
| `internal/types/openai.go` | Add `ReasoningEffort` or similar to `OpenAIRequest` if needed |
| `internal/types/responses.go` | Add `input_file` handling in types |
| `internal/translator/request.go` | Extended thinking forwarding, response_format, cache_control, file blocks |
| `internal/translator/response.go` | Non-streaming reasoning_content → thinking blocks |
| `internal/translator/stream.go` | (minimal — streaming thinking already works) |
| `internal/translator/responses.go` | reasoning.effort forwarding, input_file support |
| `internal/translator/responses_stream.go` | Cache tokens in streaming |
| `internal/translator/gemini_request.go` | responseSchema forwarding, cachedContent |
| `internal/translator/gemini_response.go` | reasoning_content → thought parts |
| `internal/translator/gemini_stream.go` | delta.reasoning → thought parts |

---

## Non-Goals (v2.9.0)

- Backend-specific optimization (e.g., vLLM vs SGLang vs TGI differences)
- Prompt caching negotiation (auto-detect backend cache support)
- Image generation response translation
- Audio/file response translation
