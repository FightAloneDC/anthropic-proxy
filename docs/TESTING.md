# Testing Guide

This document explains how to run the project test suite, what the current tests cover, and how to prepare a local config for manual smoke tests.

## Test Types

The repository currently uses Go tests only. There are two main categories:

1. **Translator unit tests** in `internal/translator/`
   - Validate request/response/stream mapping without starting an HTTP server.
   - Cover Anthropic/OpenAI-compatible translator behavior added for Gemini and multimodal features.

2. **Handler tests** in `internal/handler/`
   - Use `net/http/httptest` backend servers.
   - Validate route handling, backend URL construction, model mapping, backend API key override, SSE translation, direct-forward behavior, and binary passthrough.

The tests do **not** require a real backend API key or internet access.

## Prerequisites

- Go 1.21+
- `make`

Check Go version:

```bash
go version
```

## Run All Tests

From the repository root:

```bash
go test ./...
```

Expected output shape:

```text
?       anthropic-proxy/cmd/anthropic-proxy     [no test files]
ok      anthropic-proxy/internal/handler        ...
ok      anthropic-proxy/internal/translator     ...
?       anthropic-proxy/internal/types          [no test files]
```

## Run Tests Verbosely

```bash
go test -v ./...
```

Run only handler tests:

```bash
go test -v ./internal/handler
```

Run only translator tests:

```bash
go test -v ./internal/translator
```

Run one test by name:

```bash
go test -v ./internal/handler -run TestGeminiGenerateContentHandler
```

## Run Build Verification

The default build command builds for the current host OS/architecture:

```bash
make
```

On Linux amd64, this writes:

```text
build/anthropic-proxy-linux-amd64
```

Run both tests and build before considering changes complete:

```bash
go test ./...
make
```

## What the Current Tests Cover

### Gemini Phase 1

Files:

```text
internal/translator/gemini_request_test.go
internal/translator/gemini_response_test.go
internal/translator/gemini_stream_test.go
internal/handler/gemini_handler_test.go
```

Coverage:

- `systemInstruction` → OpenAI system message.
- Gemini `contents[]` role/content mapping.
- Gemini text and image parts.
- Gemini function declarations → OpenAI tools.
- Gemini function call/function response mapping.
- OpenAI Chat Completions response → Gemini candidates.
- Finish reason mapping.
- Usage metadata mapping.
- OpenAI SSE chunks → Gemini SSE data events.
- `generateContent` handler forwards to `/v1/chat/completions`.
- `streamGenerateContent` handler sends `stream: true` and returns SSE.
- Model mapping is applied before forwarding.
- Backend `Authorization` uses configured `backend.api_key`.

### Multimodal Phase 2

Files:

```text
internal/translator/gemini_embeddings_test.go
internal/translator/responses_test.go
internal/handler/forward_test.go
internal/handler/gemini_handler_test.go
```

Coverage:

- Gemini `embedContent` → OpenAI embeddings request.
- `outputDimensionality` → `dimensions`.
- OpenAI embeddings response → Gemini `embedding.values`.
- Empty OpenAI embeddings response handling.
- Responses API `input_image.image_url` → Chat Completions `image_url`.
- Responses API `input_image.file_data` passthrough.
- OpenAI-compatible direct-forward route path/body/header behavior.
- Direct-forward backend API key override.
- Binary response passthrough for audio-style responses.
- `GET` rejected on direct-forward POST-only endpoints.

### Hardening & Observability Phase 3

Files:

```text
internal/handler/validation_test.go
internal/handler/observability_test.go
internal/handler/health_test.go
cmd/anthropic-proxy/main_test.go
```

Coverage:

- Validation rejects missing/invalid Anthropic, Responses, and Gemini request fields.
- Request ID helper prefers inbound `x-request-id`.
- Observability middleware sets response/request IDs and records metrics.
- Logger level filtering and JSON output work without leaking filtered fields.
- `/health` returns proxy/store status without exposing backend API keys.
- `/metrics` returns Prometheus-style text output.
- Log-file setup creates missing parent directories and appends across sessions.

## Local Manual Smoke Test Config

Automated tests use `httptest` and do not need `config.yaml`. Manual smoke tests need a real OpenAI-compatible backend.

Create a local config:

```bash
cp config.example.yaml config.yaml
```

Example `config.yaml`:

```yaml
server:
  port: 8006
  fg: true

backend:
  url: "http://localhost:11434/v1"
  api_key: "dummy-local-key"

proxy:
  skip_thinking: false
  debug: true
  store_ttl: 3600
  store_max_entries: 1000

models:
  - from: "claude-sonnet-4-6"
    to: "llama3.1"
  - from: "gemini-2.5-pro"
    to: "llama3.1"
  - from: "text-embedding-3-small"
    to: "nomic-embed-text"
```

Notes:

- `backend.url` should point to an OpenAI-compatible `/v1` base URL.
- `backend.api_key` is always used for outbound backend requests.
- Incoming client API keys are ignored by the proxy.
- Model names in requests are mapped through `models` before forwarding.
- `.qwen/` is ignored by git and can exist locally when using Qwen Agent.

## Start the Proxy for Manual Smoke Tests

Build:

```bash
make
```

Run in foreground:

```bash
./build/anthropic-proxy-linux-amd64 start -fg -config config.yaml
```

If your platform is not Linux amd64, use the binary name emitted by `make`, for example:

```text
build/anthropic-proxy-{os}-{arch}
```

## Manual Smoke Test Examples

### Gemini generateContent

```bash
curl http://localhost:8006/gemini/v1beta/models/gemini-2.5-pro:generateContent \
  -H "Content-Type: application/json" \
  -d '{
    "contents": [
      {"role": "user", "parts": [{"text": "Hello!"}]}
    ],
    "generationConfig": {"maxOutputTokens": 100}
  }'
```

### Gemini streamGenerateContent

```bash
curl -N http://localhost:8006/gemini/v1beta/models/gemini-2.5-pro:streamGenerateContent \
  -H "Content-Type: application/json" \
  -d '{
    "contents": [
      {"role": "user", "parts": [{"text": "Hello!"}]}
    ],
    "generationConfig": {"maxOutputTokens": 100}
  }'
```

### Gemini embedContent

```bash
curl http://localhost:8006/gemini/v1beta/models/text-embedding-3-small:embedContent \
  -H "Content-Type: application/json" \
  -d '{
    "content": {"parts": [{"text": "hello"}]},
    "outputDimensionality": 768
  }'
```

### OpenAI Embeddings Direct Forward

```bash
curl http://localhost:8006/openai/v1/embeddings \
  -H "Content-Type: application/json" \
  -d '{"model":"text-embedding-3-small","input":"hello"}'
```

### OpenAI Audio Speech Direct Forward

```bash
curl http://localhost:8006/openai/v1/audio/speech \
  -H "Content-Type: application/json" \
  -d '{"model":"tts-1","voice":"alloy","input":"Hello"}' \
  --output speech.mp3
```

### OpenAI Audio Transcriptions Direct Forward

```bash
curl http://localhost:8006/openai/v1/audio/transcriptions \
  -F model=whisper-1 \
  -F file=@audio.mp3
```

### OpenAI Image Generation Direct Forward

```bash
curl http://localhost:8006/openai/v1/images/generations \
  -H "Content-Type: application/json" \
  -d '{"model":"dall-e-3","prompt":"a small red robot","size":"1024x1024"}'
```

## Troubleshooting Test Failures

### `go test ./...` fails with compile errors

Run formatting first:

```bash
gofmt -w $(git ls-files '*.go')
```

Then rerun:

```bash
go test ./...
```

### Handler tests fail unexpectedly

Handler tests use local `httptest` servers. They should not depend on external services. Check:

- backend path expected by the test;
- configured backend URL normalization with or without `/v1`;
- model mapping in the test config;
- expected backend `Authorization` header.

### Manual smoke test returns backend error

Direct-forward multimodal endpoints intentionally pass backend errors through. Check whether your backend supports the requested endpoint and model.

### Port already in use

Change the server port in `config.yaml`:

```yaml
server:
  port: 8010
```

Or pass a CLI override:

```bash
./build/anthropic-proxy-linux-amd64 start -fg -port 8010
```
