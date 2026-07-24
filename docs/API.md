# Public API

HTTP surface exposed by anthropic-proxy. Default listen: `:8006` (config `server.port`). TLS optional via `server.tls`.

Auth (when `auth.enabled: true`): send a configured key as `x-api-key` or `Authorization: Bearer <key>`.

## Routes (summary)

Exact registration lives in `internal/handler` / `cmd`. Typical map:

| Method | Path | Client surface |
|--------|------|----------------|
| POST | `/v1/messages` | Anthropic Messages |
| POST | `/anthropic/v1/messages` | Anthropic (prefixed) |
| GET | `/v1/models`, `/anthropic/v1/models` | Model list (mapped / aggregated) |
| POST | `/v1/responses` | OpenAI Responses |
| POST | `/openai/v1/responses` | OpenAI Responses (prefixed) |
| GET | `/openai/v1/models` | OpenAI-style model list |
| POST | `/v1/chat/completions` | Passthrough-style Chat Completions (+ thinking normalize) |
| POST | `/openai/v1/chat/completions` | Same, prefixed |
| POST | `/v1beta/models/{model}:generateContent` | Gemini |
| POST | `/v1beta/models/{model}:streamGenerateContent` | Gemini stream |
| GET | `/health` | Liveness / optional backend probe |
| GET | metrics path (when enabled) | Metrics |

If a path 404s, check the running binary’s route table — prefer code over this table when they disagree.

## Anthropic Messages

- Request/response shapes follow Anthropic Messages API (system, messages, tools, tool_choice, stream).
- Mapped to Chat Completions: system → system message; `tool_use`/`tool_result` ↔ `tool_calls` / `role: tool`; `input_schema` ↔ function parameters.
- Streaming: Anthropic SSE events (`message_start`, `content_block_*`, `message_delta`, `message_stop`, etc.).
- Thinking blocks when the backend produces thinking content and `skip_thinking` is false.

## OpenAI Responses

Primary integration for **Codex** and Responses-API clients. Full behavior: [RESPONSES_API.md](./RESPONSES_API.md).

- `instructions` + `input` (+ optional `previous_response_id`) → Chat Completions messages.
- Tools: function tools + Codex **custom** tools (`exec`, etc.).
- Stream: Responses SSE (`response.created`, `response.output_text.delta`, tool events, `response.completed`, …).

## Gemini generateContent

- Translates Gemini request bodies to Chat Completions and back.
- Supports stream and non-stream generateContent-style paths used by Gemini clients.

## Chat Completions

- Minimal translation: model mapping, optional field drops, thinking tag normalization on stream/non-stream.
- Useful when the client already speaks OpenAI Chat but the backend needs mapping/failover.

## Model mapping

Config `models`:

```yaml
models:
  - from: "gpt-5.6-terra"
    to: "gcli/grok-4.5"
```

Client sends `from`; backend receives `to`. Unmapped models pass through as-is (subject to backend routing rules).

## Errors

- Client validation / unknown body → 4xx from the proxy.
- Upstream failures may be retried or failed over; final error is mapped to a client-shaped error when possible.
- Stream mid-flight failures depend on failover settings and whether the stream already committed bytes to the client.
