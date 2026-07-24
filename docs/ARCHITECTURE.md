# Architecture

How **anthropic-proxy** works today: packages, request path, multi-backend routing, response store, and reliability.

## Role

Universal translator proxy. Clients speak **Anthropic Messages**, **OpenAI Responses**, **Gemini generateContent**, or **OpenAI Chat Completions**. The proxy always talks to an **OpenAI-compatible Chat Completions** backend (`/v1/chat/completions`, `/v1/models`).

```
Client (Anthropic | Responses | Gemini | Chat)
        │
        ▼
   anthropic-proxy  (:8006 by default)
        │  model map · auth · rate limit · translate
        ▼
   OpenAI-compatible backend(s)
```

The proxy does **not** invent background traffic. Every `POST` in the log is a client (e.g. Codex agent loop) calling the proxy.

## Packages

| Path | Responsibility |
|------|----------------|
| `cmd/anthropic-proxy/` | CLI: `start`, config load, TLS, daemon |
| `internal/handler/` | HTTP routes, auth, rate limit, forward, stream glue |
| `internal/translator/` | Anthropic / Responses / Gemini / Chat ↔ Chat Completions |
| `internal/types/` | Wire JSON structs |
| `internal/backend/` | Multi-backend router (model match, weight, priority) |
| `internal/reliability/` | Retry, circuit breaker, failover helpers |
| `internal/store/` | `previous_response_id` response store (memory or file) |
| `internal/config/` | YAML config load + defaults |
| `internal/tls/` | Cert load / self-signed generation |
| `internal/daemon/` | Background process helpers |

## High-level request flow

1. **Middleware** — optional API key (`auth`), optional rate limit.
2. **Parse** client body into the surface-specific request type.
3. **Model mapping** — `models[].from` → `models[].to` (client name → backend name).
4. **Backend selection** — single `backend` or multi `backends[]` by mapped model patterns + load-balance strategy.
5. **Translate request** → `types.OpenAIRequest` (Chat Completions).
6. **Optional overrides** — `backends[].overrides.drop_fields` strips JSON keys some providers reject.
7. **Forward** with retry / circuit breaker / failover (non-stream and eligible stream errors).
8. **Translate response** (stream SSE or non-stream JSON) back to the client API.
9. **Responses path only** — store completed turn for `previous_response_id` when store is enabled.

Streaming keeps a **stateful** translator per request so thinking tags, tool-call deltas, and (Responses) custom tool events stay consistent across chunks.

## Multi-backend

- Config: `backend` (single) and/or `backends` (list with `name`, `url`, `api_key`, `models` globs, `weight`, `priority`, `enabled`).
- After model mapping, the router picks a backend whose `models` patterns match the **backend-facing** model name.
- Strategies (config): `round_robin`, `weighted`, `least_connections`.
- Failover: status codes in `failover_status_codes` (default 429/502/503/504); optional stream error pattern list.
- Public model lists (`/anthropic/v1/models`, `/openai/v1/models`, etc.) can aggregate backends when multi-backend is configured.

## Response store (`previous_response_id`)

Used by the **OpenAI Responses** path so multi-turn clients (Codex) can send only the new input + `previous_response_id`.

- Backends: `memory` or `file` (`proxy.store_backend`, `proxy.store_file`).
- TTL / max entries: `store_ttl`, `store_max_entries`.
- On complete response, messages (and tool state needed to rebuild context) are stored under the response id.
- Next request with `previous_response_id` prepends stored messages before new input.

## Reliability knobs

Configured under `proxy` in YAML (see [CONFIGURATION.md](./CONFIGURATION.md)):

- Retry with exponential backoff  
- Circuit breaker per backend  
- Failover across matching backends  
- Optional active backend health checks  
- Metrics endpoint when `metrics_enabled`  

## Thinking / reasoning

- Many backends emit `<think>` / `<thinking>` (or similar) in content or reasoning fields.
- Translators **normalize** thinking into the client-facing shape (Anthropic thinking blocks, Responses reasoning items, Gemini thoughts, etc.).
- `proxy.skip_thinking: true` drops thinking from the client-visible stream/body (does not invent thinking).
- Stateful stream parsers handle tags split across SSE chunks.

## Custom tools (Codex)

Codex `type: "custom"` tools (notably `exec`) are **not** native Chat Completions tools. The Responses translator:

1. Forwards them as free-form functions with an `input` string schema.
2. Tracks names in a `customTools` map for the request.
3. On the way back, rewrites those function calls to `custom_tool_call` + `input` and emits `response.custom_tool_call_input.*` stream events.

Details: [RESPONSES_API.md](./RESPONSES_API.md).

## Observability

- Request logs: method, path, status, latency (`log_format` text/json, `log_level`).
- `proxy.debug` increases verbosity (SSE event dumps, mapping notes).
- Health: `GET /health` (and optional backend check).
- Metrics: when enabled, Prometheus-style endpoint (see handler registration in code).

## What the proxy does **not** do

- Does not schedule or re-fire model calls on its own.
- Does not implement Codex’s agent loop; it only translates and relays.
- Does not require Anthropic/OpenAI/Gemini upstream credentials — only the OpenAI-compatible backend’s key.
