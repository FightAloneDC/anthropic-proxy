# Multi-Modal Proxy Phase 2 Plan

This document describes the implementation plan for Roadmap Phase 2, interpreted as `v2.4.0 — Multi-Modal Proxy` from `docs/ROADMAP.md`.

## Goal

Extend `anthropic-proxy` beyond chat-style LLM translation so it can proxy common multi-modal OpenAI-compatible endpoints and translate the Gemini embedding endpoint to the OpenAI embeddings format.

Primary goals:

- Add direct forwarding for OpenAI-compatible multi-modal endpoints.
- Add Gemini `embedContent` translation to OpenAI embeddings.
- Keep the implementation aligned with the existing standard-library `net/http` routing style.
- Avoid new dependencies unless absolutely required.
- Preserve configured backend API key behavior.
- Keep request/response bodies streaming where forwarding large or binary payloads.

## Current Project Context

Phase 1 added translated Gemini generateContent endpoints. The project currently has these major paths:

```text
/anthropic/v1/messages                                → translated to /v1/chat/completions
/anthropic/v1/models                                  → forwarded to /v1/models
/openai/v1/responses                                  → translated to /v1/chat/completions
/openai/v1/chat/completions                           → direct forward to /v1/chat/completions
/openai/v1/models                                     → forwarded to /v1/models
/gemini/v1beta/models/{model}:generateContent         → translated to /v1/chat/completions
/gemini/v1beta/models/{model}:streamGenerateContent   → translated to /v1/chat/completions
```

Relevant existing structure:

```text
cmd/anthropic-proxy/main.go   # CLI entry point and route registration
internal/handler/handler.go   # HTTP handlers and backend forwarding
internal/translator/          # request/response/stream translators
internal/types/               # API structs
internal/config/              # config and model mapping
```

## Scope

### In scope

Add direct forwarding endpoints:

```text
POST /openai/v1/embeddings
POST /openai/v1/rerank
POST /openai/v1/audio/speech
POST /openai/v1/audio/transcriptions
POST /openai/v1/images/generations
```

Add translated Gemini embedding endpoint:

```text
POST /gemini/v1beta/models/{model}:embedContent
```

Improve Responses API image support where needed:

```text
input_image → OpenAI Chat Completions image_url content
base64 and URL image sources
```

### Out of scope

- Native Gemini image generation.
- Gemini speech APIs.
- Native safety enforcement.
- External web search provider integration.
- `/health` and `/metrics`; those are planned for the next hardening/observability phase.
- Persistent storage.
- Multi-backend load balancing.
- Any third-party router or multipart helper dependency.

## Design Principles

1. **Forward when formats are already OpenAI-compatible.**
   Multi-modal OpenAI endpoints should be direct passthroughs to the configured backend path.

2. **Translate only where client API differs.**
   Gemini `embedContent` needs translation because the request/response shape differs from OpenAI embeddings.

3. **Preserve request body behavior.**
   Multipart requests such as audio transcription should be proxied without full JSON parsing.

4. **Use the configured backend key.**
   Incoming client keys remain ignored, matching existing project behavior.

5. **Avoid premature abstraction.**
   A small direct-forward helper is acceptable if it replaces repeated forwarding code across multiple endpoints.

## Route Plan

### OpenAI-compatible direct forward routes

Register routes in `cmd/anthropic-proxy/main.go`:

```go
http.HandleFunc("/openai/v1/embeddings", h.DirectForwardHandler("/v1/embeddings"))
http.HandleFunc("/openai/v1/rerank", h.DirectForwardHandler("/v1/rerank"))
http.HandleFunc("/openai/v1/audio/speech", h.DirectForwardHandler("/v1/audio/speech"))
http.HandleFunc("/openai/v1/audio/transcriptions", h.DirectForwardHandler("/v1/audio/transcriptions"))
http.HandleFunc("/openai/v1/images/generations", h.DirectForwardHandler("/v1/images/generations"))
```

If the existing code style makes function-returning handlers awkward, add explicit methods instead:

```go
EmbeddingsHandler
RerankHandler
AudioSpeechHandler
AudioTranscriptionsHandler
ImageGenerationsHandler
```

Preferred approach: create one small reusable forward helper to avoid duplicating backend URL/header/stream copying logic.

### Gemini embedding route

Use the existing Gemini prefix route or extend the Gemini handler path parser:

```text
/gemini/v1beta/models/{model}:embedContent
```

This route should:

1. Validate `POST`.
2. Extract `{model}`.
3. Apply model mapping.
4. Decode Gemini embedding request.
5. Translate to OpenAI embeddings request.
6. Forward to backend `/v1/embeddings`.
7. Translate OpenAI embeddings response to Gemini embedding response.

## Direct Forward Design

### Supported methods

Most Phase 2 routes are `POST` only. The helper should reject unsupported methods with an OpenAI-style JSON error envelope.

### Header handling

Forward selected client headers:

- `Content-Type`
- `Accept`
- `User-Agent`, optional

Overwrite auth header:

```text
Authorization: Bearer {configured backend.api_key}
```

Do not forward incoming `Authorization` or API-key headers to the backend.

### Body handling

For direct forwarding:

- do not JSON-decode request bodies;
- pass `r.Body` directly to backend request;
- preserve multipart bodies for audio transcription;
- copy backend response status, headers, and body to the client.

### Streaming/binary responses

`/openai/v1/audio/speech` may return audio bytes or chunked audio. The proxy should copy response headers and stream the response body with `io.Copy`.

## Gemini Embeddings Translation

### Gemini request shape

Support common Gemini `embedContent` request:

```json
{
  "content": {
    "parts": [
      {"text": "hello"}
    ]
  },
  "taskType": "RETRIEVAL_DOCUMENT",
  "title": "optional title",
  "outputDimensionality": 768
}
```

Also consider batch support only if the project explicitly needs it later. For this phase, keep the endpoint single-input unless the user requests batch embeddings.

### OpenAI embeddings request

Translate to:

```json
{
  "model": "mapped-backend-model",
  "input": "hello",
  "dimensions": 768
}
```

Mapping:

| Gemini | OpenAI Embeddings |
|---|---|
| URL `{model}` | `model` |
| `content.parts[].text` | `input` string |
| multiple text parts | joined with newline |
| `outputDimensionality` | `dimensions` |
| `taskType` | accepted but ignored |
| `title` | accepted but ignored |

### OpenAI embeddings response

Expected OpenAI shape:

```json
{
  "object": "list",
  "data": [
    {"object": "embedding", "embedding": [0.1, 0.2], "index": 0}
  ],
  "model": "backend-model",
  "usage": {"prompt_tokens": 3, "total_tokens": 3}
}
```

Translate to Gemini shape:

```json
{
  "embedding": {
    "values": [0.1, 0.2]
  }
}
```

If backend returns multiple embeddings, use the first item for `embedContent`.

## Responses API Vision Follow-up

Roadmap says image-to-text vision is already mostly supported via image content blocks, but Phase 2 should verify and harden Responses `input_image` support.

Expected behavior:

| Responses input block | Chat Completions |
|---|---|
| `input_image.image_url` | `image_url.url` |
| `input_image.file_data` | data URL if already base64 data |
| `input_image.detail` | `image_url.detail` if backend supports it |

Implementation should first inspect current `TranslateResponsesRequest()` behavior. If already sufficient, add tests only. If incomplete, make the smallest translator fix.

## Proposed Files

Likely new files:

```text
internal/types/embeddings.go
internal/translator/gemini_embeddings.go
internal/translator/gemini_embeddings_test.go
```

Likely edited files:

```text
cmd/anthropic-proxy/main.go
internal/handler/handler.go
internal/translator/responses.go
internal/translator/responses_test.go
README.md
docs/ARCHITECTURE.md
docs/ROADMAP.md
```

Optional file if handler grows too large:

```text
internal/handler/forward.go
```

Use this only if it improves readability without introducing abstraction churn.

## Implementation Order

1. Add or refactor a direct-forward helper for OpenAI-compatible passthrough routes.
2. Register OpenAI multi-modal direct-forward routes.
3. Add Gemini embedding types.
4. Add Gemini embedding request/response translator tests.
5. Implement Gemini embedding translator.
6. Extend Gemini handler path parsing for `:embedContent`.
7. Forward translated Gemini embeddings to backend `/v1/embeddings`.
8. Inspect Responses API `input_image` support.
9. Add or update Responses vision tests.
10. Apply the smallest Responses vision fix if needed.
11. Update README and architecture docs.
12. Update roadmap status when complete.
13. Run verification.

## Test Plan

### Unit tests

Add translator tests for Gemini embeddings:

- single text part → OpenAI embeddings input string;
- multiple text parts joined by newline;
- `outputDimensionality` → `dimensions`;
- model mapping supplied by handler path, not request body;
- OpenAI embedding response → Gemini embedding response;
- empty backend `data` handling.

Add or update Responses vision tests:

- `input_image.image_url` maps to Chat Completions `image_url` content;
- base64/file data is preserved in a backend-compatible format;
- mixed text + image content remains ordered.

### Handler tests

If lightweight handler tests are added, cover:

- direct forward path builds the correct backend URL;
- direct forward uses configured backend key;
- Gemini `embedContent` path parsing;
- unsupported Gemini route returns error.

### Manual smoke tests

Embeddings:

```bash
curl http://localhost:8006/openai/v1/embeddings \
  -H "Content-Type: application/json" \
  -d '{"model":"text-embedding-3-small","input":"hello"}'
```

Gemini embeddings:

```bash
curl http://localhost:8006/gemini/v1beta/models/text-embedding-3-small:embedContent \
  -H "Content-Type: application/json" \
  -d '{"content":{"parts":[{"text":"hello"}]}}'
```

Audio speech:

```bash
curl http://localhost:8006/openai/v1/audio/speech \
  -H "Content-Type: application/json" \
  -d '{"model":"tts-1","voice":"alloy","input":"Hello"}' \
  --output speech.mp3
```

Audio transcription:

```bash
curl http://localhost:8006/openai/v1/audio/transcriptions \
  -F model=whisper-1 \
  -F file=@audio.mp3
```

Image generation:

```bash
curl http://localhost:8006/openai/v1/images/generations \
  -H "Content-Type: application/json" \
  -d '{"model":"dall-e-3","prompt":"a small red robot","size":"1024x1024"}'
```

## Verification Plan

Run formatting:

```bash
gofmt -w <changed-go-files>
```

Run tests:

```bash
go test ./...
```

Run build:

```bash
make
```

If tests or build fail, report the relevant failure output, fix the issue, and rerun the relevant command.

## Documentation Updates

Update:

```text
README.md
docs/ARCHITECTURE.md
docs/ROADMAP.md
```

Documentation should include:

- new OpenAI-compatible multi-modal endpoints;
- Gemini `embedContent` endpoint;
- example curl commands;
- direct-forward behavior;
- note that incoming client keys are ignored;
- note that Gemini `taskType` and `title` are accepted but ignored in Phase 2.

## Risks and Decisions

### Risk: backend endpoint support varies

Some OpenAI-compatible backends may not support TTS, STT, image generation, reranking, or embeddings.

Decision: direct-forward backend errors unchanged so clients see the backend's native error.

### Risk: audio and image responses may be binary or large

Decision: avoid JSON parsing for direct-forward endpoints and use streaming body copy.

### Risk: Gemini embedding variants differ

Decision: start with single `embedContent`; defer batch embedding support unless requested.

### Risk: handler file growth

Decision: if `handler.go` becomes too large, move direct-forward helper to `internal/handler/forward.go` while keeping package-local behavior simple.

## Approval Gate for Implementation

This document is only the Phase 2 implementation plan. Actual implementation should begin only after explicit user approval.
