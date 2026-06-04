# Troubleshooting

Known issues, fixes, and debugging tips for anthropic-proxy.

## Table of Contents

- [Codex / OpenAI Responses API Issues](#codex--openai-responses-api-issues)
- [Anthropic API Issues](#anthropic-api-issues)
- [General Issues](#general-issues)
- [Debugging Tips](#debugging-tips)

---

## Codex / OpenAI Responses API Issues

### `tools[N] is missing function.name`

**Symptom:**
```json
{"error":{"code":"400","message":"Param Incorrect","param":"tools[9] is missing function.name","type":""}}
```

**Cause:** Codex (and other Responses API clients) may send built-in tools like `web_search_20250305`, `bash`, `computer_use`, etc. These are not `"function"` type tools and don't have a `name` field at the function level. The translator was converting ALL tools to Chat Completions format, creating empty function names.

**Fix:** Non-function tools (type != `"function"` or name == `""`) are now skipped during translation. These tools have no Chat Completions equivalent.

**Affected tool types:**
- `web_search_20250305`, `web_search_20260209`
- `bash_20250124`
- `computer_use_*`
- `code_execution_*`
- `text_editor_*`
- `memory_*`

**File:** `internal/translator/responses.go` — `TranslateResponsesRequest()` tools loop

---

### `messages[N] assistant must provide content, reasoning_content or tool_calls`

**Symptom:**
```json
{"error":{"code":"400","message":"Param Incorrect","param":"messages[4] assistant must provide content, reasoning_content or tool_calls","type":""}}
```

**Cause:** Two issues working together:

1. **`output_text` not handled in input translator:** When Codex sends conversation history, assistant messages from previous Responses API responses use `output_text` content type (not `input_text`). The translator only handled `input_text`, silently dropping the content.

2. **`reasoning_content` field missing from `OpenAIMsg`:** Backend returns `reasoning_content` for reasoning models (MiMo, DeepSeek R1, o-series), but the `OpenAIMsg` struct didn't have this field, so it was dropped during JSON unmarshaling. When the response was converted to Responses format, the assistant message had no content.

**Fix (two parts):**

1. **Input translator** — handle both `input_text` and `output_text` content types:
   ```go
   case "input_text", "output_text":
       // both map to Chat Completions "text" content type
   ```

2. **OpenAI types** — added `ReasoningContent` field to `OpenAIMsg`:
   ```go
   type OpenAIMsg struct {
       Role             string      `json:"role"`
       Content          interface{} `json:"content,omitempty"`
       ReasoningContent string      `json:"reasoning_content,omitempty"`
       ToolCalls        []ToolCall  `json:"tool_calls,omitempty"`
       ToolCallID       string      `json:"tool_call_id,omitempty"`
   }
   ```

3. **Response translator** — fallback to `reasoning_content` when `content` is empty:
   ```go
   if text == "" && ch.Message.ReasoningContent != "" {
       text = ch.Message.ReasoningContent
   }
   ```

**Files:**
- `internal/types/openai.go` — `OpenAIMsg` struct
- `internal/translator/responses.go` — `translateMessageContentBlocks()` and `TranslateResponsesResponse()`

---

### Multi-turn conversation fails (but single-turn works)

**Symptom:** First message works fine, but follow-up messages fail with errors about invalid messages.

**Cause:** Codex sends full conversation history in the `input` array (not using `previous_response_id`). Each previous assistant response is included as a message with `output_text` content type. If the translator doesn't handle `output_text`, the assistant messages become empty.

**Fix:** See the `output_text` fix above. The translator now handles both `input_text` and `output_text` content types.

---

### Model metadata warning

**Symptom:**
```
⚠ Model metadata for `mimo-v2.5-pro` not found. Defaulting to fallback metadata.
```

**Cause:** Codex has built-in model metadata for known models (GPT-4o, Claude, etc.). Custom/unknown models trigger this warning.

**Impact:** Cosmetic only. Codex still works, just with reduced metadata awareness.

**Workaround:** Use a known model name if possible, or ignore the warning.

---

## Anthropic API Issues

### `anthropic-version` header not echoed

**Symptom:** Client expects `anthropic-version` in response but gets default.

**Cause:** The proxy reads `anthropic-version` from the request and echoes it back. If the client doesn't send it, the proxy defaults to `2023-06-01`.

**Fix:** Ensure client sends `anthropic-version: 2023-06-01` header.

---

### `X-Api-Key` ignored

**Symptom:** Client sends API key but proxy ignores it.

**Cause:** By design. The proxy always uses its configured `backend.api_key` for outbound requests. The incoming `X-Api-Key` is only logged (masked) for debugging.

**This is intentional.** The proxy is a single point of auth — clients don't need real backend keys.

---

## General Issues

### Port already in use

**Symptom:**
```
Server failed: listen tcp :8006: bind: address already in use
```

**Fix:**
```bash
./anthropic-proxy status     # Check if already running
./anthropic-proxy stop       # Stop existing instance
./anthropic-proxy start -fg  # Start fresh
```

---

### Backend connection refused

**Symptom:**
```json
{"type":"error","error":{"type":"api_error","message":"backend error: dial tcp ...: connect: connection refused"}}
```

**Fix:** Check `backend.url` in config.yaml. Ensure backend is running and accessible.

---

### Previous response not found

**Symptom:**
```json
{"type":"error","error":{"type":"not_found","message":"previous response not found: resp_999"}}
```

**Cause:** The response ID doesn't exist in the store (expired, never stored, or server restarted).

**Fix:** Response store is in-memory only. Responses are lost on server restart. Increase `store_ttl` if needed.

---

## Debugging Tips

### Enable debug logging

```bash
./anthropic-proxy start -fg -debug
```

This logs:
- All incoming requests with headers (API keys masked)
- All outbound requests to backend
- All SSE events (streaming)
- Model mapping decisions

### Check what Codex sends

Enable debug logging and send a message from Codex. The log will show the full request body, including the `input` array with conversation history.

### Test endpoints manually

```bash
# Test Responses API
curl -s http://localhost:8006/openai/v1/responses \
  -H "Content-Type: application/json" \
  -d '{"model":"your-model","input":"hello","max_output_tokens":100}'

# Test Anthropic API
curl -s http://localhost:8006/anthropic/v1/messages \
  -H "Content-Type: application/json" \
  -H "x-api-key: sk-dummy" \
  -H "anthropic-version: 2023-06-01" \
  -d '{"model":"claude-sonnet-4-6","max_tokens":100,"messages":[{"role":"user","content":"hello"}]}'

# Test direct forward
curl -s http://localhost:8006/openai/v1/chat/completions \
  -H "Content-Type: application/json" \
  -d '{"model":"your-model","messages":[{"role":"user","content":"hello"}],"max_tokens":100}'

# Test models
curl -s http://localhost:8006/openai/v1/models
```

### Common log patterns

| Log Pattern | Meaning |
|-------------|---------|
| `← POST /openai/v1/responses` | Incoming Responses API request |
| `→ POST http://backend/v1/chat/completions` | Outbound to backend |
| `← SSE event=response.created` | Streaming started |
| `← SSE event=response.completed` | Streaming finished |
| `skip unparseable chunk` | Backend sent malformed SSE (usually harmless) |
