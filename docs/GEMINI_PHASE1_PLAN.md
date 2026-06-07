# Gemini API Support Phase 1 Plan

This document describes the implementation plan for Roadmap Phase 1, interpreted as `v2.3.0 — Gemini API Support` from `docs/ROADMAP.md`.

## Goal

Add Gemini-compatible API endpoints that translate Gemini requests to the existing OpenAI Chat Completions backend format, then translate backend responses back into Gemini format.

Target endpoints:

```text
POST /gemini/v1beta/models/{model}:generateContent
POST /gemini/v1beta/models/{model}:streamGenerateContent
```

Target flow:

```text
Gemini client
→ anthropic-proxy Gemini endpoint
→ translate to OpenAI Chat Completions
→ backend /v1/chat/completions
→ translate response back to Gemini
→ Gemini client
```

## Current Project Context

The project already supports three request paths:

```text
/anthropic/v1/messages        → translated to chat/completions
/anthropic/v1/models          → forwarded to backend
/openai/v1/responses          → translated to chat/completions
/openai/v1/chat/completions   → direct forward
/openai/v1/models             → forwarded to backend
```

Relevant existing structure:

```text
cmd/anthropic-proxy/main.go   # CLI entry point and route registration
internal/handler/handler.go   # HTTP handlers and backend forwarding
internal/translator/          # Request/response/stream translators
internal/types/               # API request/response structs
internal/config/              # YAML/env/CLI config loading
internal/store/               # in-memory previous_response_id store
```

The Gemini implementation should follow these existing conventions:

- no new router dependency;
- use standard library `net/http` route handling;
- translate everything to backend `/v1/chat/completions`;
- keep unsupported features accepted-but-ignored when there is no Chat Completions equivalent;
- use configured backend API key for outbound requests;
- preserve the existing model mapping behavior.

## Scope

### In scope

- Add Gemini non-streaming endpoint.
- Add Gemini streaming endpoint.
- Translate Gemini text content to OpenAI Chat Completions messages.
- Translate Gemini image parts to OpenAI vision content.
- Translate Gemini function declarations to OpenAI tools.
- Translate Gemini function calls and function responses.
- Translate backend OpenAI responses to Gemini candidates.
- Translate backend OpenAI streaming chunks to Gemini streaming output.
- Apply existing model mapping from config.
- Add unit tests for Gemini translators.
- Update documentation.
- Run project verification commands.

### Out of scope

- Gemini embeddings.
- Gemini `embedContent`.
- Native Gemini safety enforcement.
- File upload APIs.
- Persistent response store.
- Metrics or health check endpoints.
- Incoming Gemini API key validation.
- Adding third-party router/framework dependencies.

## Proposed Files

New files:

```text
internal/types/gemini.go
internal/translator/gemini_request.go
internal/translator/gemini_response.go
internal/translator/gemini_stream.go
internal/translator/gemini_request_test.go
internal/translator/gemini_response_test.go
internal/translator/gemini_stream_test.go
```

Existing files to edit:

```text
cmd/anthropic-proxy/main.go
internal/handler/handler.go
README.md
docs/ARCHITECTURE.md
docs/ROADMAP.md
```

## Route Design

Use a single prefix handler, consistent with the project's standard library approach:

```go
http.HandleFunc("/gemini/v1beta/models/", h.GeminiHandler)
```

The handler parses the suffix after `/gemini/v1beta/models/`:

```text
{model}:generateContent
{model}:streamGenerateContent
```

Examples:

```text
/gemini/v1beta/models/gemini-2.5-pro:generateContent
/gemini/v1beta/models/gemini-2.5-pro:streamGenerateContent
```

Handler responsibilities:

1. Validate `POST` method.
2. Parse model and action from path.
3. Decode Gemini request JSON.
4. Apply configured model mapping.
5. Translate Gemini request to OpenAI Chat Completions request.
6. Forward to backend `/v1/chat/completions`.
7. Translate backend response to Gemini response.
8. Stream translated chunks for streaming requests.

## Gemini Request Type Support

Support the common Gemini request shape:

```json
{
  "contents": [],
  "systemInstruction": {},
  "tools": [],
  "generationConfig": {},
  "safetySettings": []
}
```

### Request mapping

| Gemini | OpenAI Chat Completions |
|---|---|
| URL `{model}` | `model` |
| `systemInstruction.parts[].text` | system message |
| `contents[].role = user` | `role: user` |
| `contents[].role = model` | `role: assistant` |
| `parts[].text` | text content |
| `parts[].inlineData` | `image_url` data URI |
| `parts[].fileData.fileUri` | `image_url` URL |
| `parts[].functionCall` | assistant `tool_calls` |
| `parts[].functionResponse` | tool message |
| `generationConfig.temperature` | `temperature` |
| `generationConfig.topP` | `top_p` |
| `generationConfig.topK` | `top_k` |
| `generationConfig.maxOutputTokens` | `max_tokens` |
| `tools[].functionDeclarations[]` | `tools[].function` |

### Safety settings

`SafetySettings` should be accepted in the Gemini request type but not forwarded to Chat Completions, because there is no direct equivalent in the current backend contract.

Document behavior:

```text
safetySettings are accepted but ignored by the proxy.
```

## Gemini Response Type Support

Translate OpenAI Chat Completions response to Gemini response:

```json
{
  "candidates": [
    {
      "content": {
        "role": "model",
        "parts": []
      },
      "finishReason": "STOP"
    }
  ],
  "usageMetadata": {}
}
```

### Response mapping

| OpenAI Chat Completions | Gemini |
|---|---|
| `choices[0].message.content` | `candidates[0].content.parts[].text` |
| `choices[0].message.tool_calls` | `parts[].functionCall` |
| `finish_reason = stop` | `finishReason = STOP` |
| `finish_reason = length` | `finishReason = MAX_TOKENS` |
| `finish_reason = tool_calls` | `finishReason = STOP` |
| `usage.prompt_tokens` | `usageMetadata.promptTokenCount` |
| `usage.completion_tokens` | `usageMetadata.candidatesTokenCount` |
| `usage.total_tokens` | `usageMetadata.totalTokenCount` |

If backend content is empty but `reasoning_content` is present, reuse the existing project behavior and surface usable text where appropriate.

## Streaming Design

Endpoint:

```text
POST /gemini/v1beta/models/{model}:streamGenerateContent
```

The handler forwards to backend with:

```json
{
  "stream": true
}
```

The backend OpenAI SSE chunks should be translated to Gemini-compatible SSE data events.

Example text event:

```text
data: {"candidates":[{"content":{"role":"model","parts":[{"text":"Hello"}]}}]}
```

Example final event:

```text
data: {"candidates":[{"finishReason":"STOP"}],"usageMetadata":{"promptTokenCount":10,"candidatesTokenCount":15,"totalTokenCount":25}}
```

Streaming translator should maintain state for:

- response started;
- accumulated tool call arguments;
- final finish reason;
- usage metadata if present in stream chunks.

## Tool Calling

### Gemini tools to OpenAI tools

Gemini:

```json
{
  "functionDeclarations": [
    {
      "name": "get_weather",
      "description": "Get weather",
      "parameters": {}
    }
  ]
}
```

OpenAI:

```json
{
  "type": "function",
  "function": {
    "name": "get_weather",
    "description": "Get weather",
    "parameters": {}
  }
}
```

### OpenAI tool call to Gemini function call

OpenAI assistant tool call should become:

```json
{
  "functionCall": {
    "name": "get_weather",
    "args": {}
  }
}
```

### Gemini function response to OpenAI tool message

Gemini `functionResponse` should become an OpenAI `tool` message.

If Gemini does not provide a stable tool call ID, generate or derive a deterministic internal ID for the translated Chat Completions message sequence.

## Model Mapping

Use the existing config model mapping behavior.

Example:

```yaml
models:
  - from: "gemini-2.5-pro"
    to: "backend-model-name"
```

The path model is the source model. If mapping exists, send the mapped model to the backend.

## Test Plan

The repository currently has no `*_test.go` files. Add translator unit tests as the initial safety net.

### Request translator tests

- Text-only user message.
- `systemInstruction` conversion.
- Multi-turn `user` and `model` conversation.
- `generationConfig` mapping.
- `inlineData` image conversion to data URI.
- `fileData.fileUri` image conversion to URL image content.
- Function declarations to OpenAI tools.
- Gemini `functionCall` to assistant tool call.
- Gemini `functionResponse` to tool message.

### Response translator tests

- Text response to Gemini candidate.
- Tool call response to Gemini `functionCall` part.
- Finish reason mapping.
- Usage metadata mapping.
- Empty or partial backend response handling.

### Stream translator tests

- Text delta event.
- Finish event.
- Usage chunk handling.
- Tool call argument accumulation, if feasible within simple unit tests.

## Verification Plan

After implementation, run:

```bash
go test ./...
```

Then run the project build command:

```bash
make
```

If either command fails, capture the relevant output, fix the issue, and rerun the relevant command. Do not claim success unless the commands pass.

## Documentation Updates

Update:

```text
README.md
docs/ARCHITECTURE.md
docs/ROADMAP.md
```

Documentation should include:

- new Gemini endpoints;
- SDK/base URL guidance if applicable;
- `curl` example for `generateContent`;
- `curl` example for `streamGenerateContent`;
- summary of Gemini to Chat Completions mapping;
- note that `safetySettings` are accepted but ignored.

## Implementation Order

1. Add Gemini types.
2. Add Gemini request translator tests.
3. Implement Gemini request translator.
4. Add Gemini response translator tests.
5. Implement Gemini response translator.
6. Add Gemini stream translator tests.
7. Implement Gemini stream translator.
8. Add Gemini handler and route registration.
9. Update documentation.
10. Run `go test ./...`.
11. Run `make`.
12. Report results and any limitations.

## Approval Gate for Implementation

This document is only the implementation plan. Actual implementation should begin only after explicit user approval.
