# Hardening & Observability Phase 3 Plan

This document describes the implementation plan for Roadmap Phase 3, interpreted as `v2.5.0 — Hardening & Observability` from `docs/ROADMAP.md`.

## Goal

Improve production readiness by adding request validation, structured request logging, request ID propagation, metrics, and health checks without changing the existing proxy behavior for valid requests.

Primary goals:

- Reject malformed requests before translation with API-appropriate error responses.
- Add consistent request IDs across logs and responses.
- Add structured logging while preserving simple debug behavior.
- Add optional log-to-file support for foreground and daemon debugging.
- Add in-memory request metrics.
- Add `/health` and optional `/metrics` utility endpoints.
- Keep implementation dependency-light and aligned with the standard-library `net/http` style.

## Current Project Context

The project currently supports:

```text
/anthropic/v1/messages                                → translated to /v1/chat/completions
/anthropic/v1/models                                  → forwarded to /v1/models
/openai/v1/responses                                  → translated to /v1/chat/completions
/openai/v1/chat/completions                           → direct forward to /v1/chat/completions
/openai/v1/models                                     → forwarded to /v1/models
/openai/v1/embeddings                                 → direct forward to /v1/embeddings
/openai/v1/rerank                                     → direct forward to /v1/rerank
/openai/v1/audio/speech                               → direct forward to /v1/audio/speech
/openai/v1/audio/transcriptions                       → direct forward to /v1/audio/transcriptions
/openai/v1/images/generations                         → direct forward to /v1/images/generations
/gemini/v1beta/models/{model}:generateContent         → translated to /v1/chat/completions
/gemini/v1beta/models/{model}:streamGenerateContent   → translated to /v1/chat/completions
/gemini/v1beta/models/{model}:embedContent            → translated to /v1/embeddings
```

Current observations:

- Logging uses raw `log.Printf` across handlers and daemon code.
- Request IDs exist for Anthropic and Responses responses, but are not consistently propagated.
- No explicit request validation layer exists.
- No `/health` or `/metrics` endpoint exists.
- Direct-forward endpoints intentionally avoid JSON parsing to preserve multipart/binary behavior.
- Existing tests include translator unit tests and handler tests with `httptest`.

## Scope

### In scope

- Request validation for translated JSON endpoints.
- Structured logging helper with log levels.
- Optional log-to-file support via config and CLI flag.
- Request ID generation and propagation.
- In-memory metrics collection.
- `GET /health` endpoint.
- `GET /metrics` endpoint in Prometheus text format.
- Tests for validation, request IDs, health, metrics, and logging-safe behavior.
- Documentation updates.

### Out of scope

- Persistent metrics store.
- External observability integrations.
- OpenTelemetry.
- Prometheus client dependency.
- Distributed tracing.
- Circuit breaker or backend health monitor loop; those belong to Phase 4 persistence/reliability.
- Rate limiting.
- Retrying backend requests.
- Request validation for binary/multipart direct-forward endpoints beyond method/path checks.

## Design Principles

1. **Validate only system boundaries.**
   Validate user/client request fields before translation, but do not add defensive validation for impossible internal states.

2. **Preserve valid request behavior.**
   Existing valid requests should continue producing the same backend request and client response.

3. **Use API-native error envelopes.**
   Anthropic, OpenAI Responses, Gemini, and OpenAI-compatible routes should return errors in the closest matching format.

4. **Keep observability simple and local.**
   Use in-memory counters/histograms and standard-library HTTP output.

5. **Avoid new dependencies.**
   Implement structured logs and Prometheus text output manually unless a future phase requires a dedicated library.

6. **Do not parse direct-forward bodies.**
   Direct-forward multimodal routes should keep body passthrough behavior.

## Proposed Files

Likely new files:

```text
internal/handler/validation.go
internal/handler/validation_test.go
internal/handler/observability.go
internal/handler/observability_test.go
internal/handler/health.go
internal/handler/health_test.go
internal/handler/metrics.go
internal/handler/metrics_test.go
```

Likely edited files:

```text
cmd/anthropic-proxy/main.go
internal/config/config.go
internal/handler/handler.go
internal/handler/forward.go
README.md
docs/ARCHITECTURE.md
docs/ROADMAP.md
docs/TESTING.md
config.example.yaml
```

Optional file if config grows:

```text
internal/config/observability.go
```

Use this only if config helpers become too large for `config.go`.

## Configuration Plan

Extend config with optional observability settings:

```yaml
server:
  port: 8006
  fg: false
  log: ""                 # optional log file path; empty keeps current stdout/stderr or daemon default

proxy:
  skip_thinking: false
  debug: false
  store_ttl: 3600
  store_max_entries: 1000
  log_format: "text"        # text|json
  log_level: "info"         # debug|info|warn|error
  metrics_enabled: true
  health_backend_check: false
```

Add a CLI override:

```text
-log-file string    write logs to this file path (overrides server.log)
```

Example:

```bash
./build/anthropic-proxy-linux-amd64 start -fg -debug -log-file ./debug-output/proxy.log
```

Environment variables can be added as optional follow-up if the project needs parity with existing `OPENAI_BASE_URL` and `OPENAI_API_KEY` behavior.

Suggested defaults:

| Field | Default | Notes |
|---|---|---|
| `log_format` | `text` | Keep current operator experience by default |
| `log_level` | `info` | Debug logs only when explicitly requested |
| `server.log` | empty | Foreground logs stay on stdout/stderr; daemon uses existing default unless set |
| `metrics_enabled` | `true` | `/metrics` available by default |
| `health_backend_check` | `false` | Avoid making `/health` depend on backend availability unless enabled |

## Request Validation Plan

### Anthropic Messages

Validate before translation:

- `model` is required.
- `max_tokens` must be greater than zero.
- `messages` must contain at least one item.
- each message role must be `user` or `assistant`.
- `temperature`, if present, should be in a reasonable boundary such as `0 <= temperature <= 2`.
- `top_p`, if present, should satisfy `0 <= top_p <= 1`.
- `top_k`, if present, should be non-negative.

Error format:

```json
{
  "type": "error",
  "error": {
    "type": "invalid_request_error",
    "message": "model is required"
  }
}
```

### OpenAI Responses

Validate before translation:

- `model` is required.
- `input` is required.
- `max_output_tokens`, if present, must be non-negative.
- `temperature`, if present, should satisfy `0 <= temperature <= 2`.
- `top_p`, if present, should satisfy `0 <= top_p <= 1`.
- `previous_response_id`, if present, remains validated by existing store lookup.

Error format should use existing Responses-style errors:

```json
{
  "type": "error",
  "error": {
    "type": "invalid_request_error",
    "message": "model is required"
  }
}
```

### Gemini generateContent / streamGenerateContent

Validate before translation:

- path model is required; this is already part of path parsing.
- `contents` must contain at least one item.
- each content should contain at least one part.
- `generationConfig.maxOutputTokens`, if present, must be non-negative.
- `generationConfig.temperature`, if present, should satisfy `0 <= temperature <= 2`.
- `generationConfig.topP`, if present, should satisfy `0 <= topP <= 1`.
- `generationConfig.topK`, if present, should be non-negative.

Gemini error format:

```json
{
  "error": {
    "code": "400",
    "message": "contents is required",
    "status": "Bad Request"
  }
}
```

### Gemini embedContent

Validate before translation:

- path model is required.
- `content.parts` must include at least one text part.
- `outputDimensionality`, if present, must be non-negative.

### Direct-forward OpenAI-compatible endpoints

Do not parse body. Keep method-only validation:

- reject non-POST with OpenAI-style error.
- forward backend errors unchanged.

## Request ID Plan

Add helper:

```go
func requestID(r *http.Request) string
```

Behavior:

1. If inbound `x-request-id` exists, use it.
2. Else if inbound `request-id` exists, use it.
3. Else generate `req-{UnixNano}` or similar.

Response headers:

| Endpoint family | Header |
|---|---|
| Anthropic | `request-id` |
| OpenAI Responses | `x-request-id` |
| OpenAI direct-forward | `x-request-id` |
| Gemini | `x-request-id` |
| `/health`, `/metrics` | `x-request-id` |

Backend propagation:

- Set `x-request-id` on outbound backend requests.
- For Anthropic translated requests, also preserve `request-id` response header for client compatibility.

Tests:

- inbound request ID is reused.
- generated request ID is present when inbound missing.
- outbound backend request includes `x-request-id`.

## Structured Logging Plan

Add a lightweight logger helper in `internal/handler/observability.go` or a small package-local helper.

Suggested API:

```go
type Logger struct {
    format string
    level  string
}

func (l *Logger) Info(msg string, fields map[string]interface{})
func (l *Logger) Debug(msg string, fields map[string]interface{})
func (l *Logger) Warn(msg string, fields map[string]interface{})
func (l *Logger) Error(msg string, fields map[string]interface{})
```

Text format example:

```text
level=info msg="request completed" request_id=req-123 method=POST path=/gemini/v1beta/models/gemini-2.5-pro:generateContent status=200 latency_ms=12
```

JSON format example:

```json
{"level":"info","msg":"request completed","request_id":"req-123","method":"POST","path":"/openai/v1/responses","status":200,"latency_ms":12}
```

Required fields for request logs:

- `request_id`
- `method`
- `path`
- `status`
- `latency_ms`
- `endpoint_family`
- `backend_status`, when applicable
- `error_type`, when applicable

Sensitive data rules:

- Never log raw API keys.
- Keep existing key masking behavior if logging incoming keys.
- Do not log request/response bodies by default.

### Log-to-file support

Add explicit log file support for debugging:

- config key: `server.log`
- CLI flag: `-log-file string`
- CLI flag overrides YAML config.
- If a log file is configured, write all application logs to that file.
- If no log file is configured and the process runs in foreground, keep logs on the default process output.
- If daemon mode is used and no explicit log file is configured, keep the existing daemon log behavior.
- If the configured log file parent directory does not exist, create it with safe local permissions.
- Open the log file in append mode so repeated debug sessions do not overwrite earlier logs.
- Do not implement log rotation in Phase 3.

Example config:

```yaml
server:
  port: 8006
  fg: true
  log: "./debug-output/proxy.log"

proxy:
  debug: true
  log_format: "json"
  log_level: "debug"
```

Example CLI:

```bash
./build/anthropic-proxy-linux-amd64 start -fg -debug -log-file ./debug-output/proxy.log
```

Implementation notes:

- Reuse or extend existing daemon log setup where possible.
- Ensure `defer logFile.Close()` happens in the same lifecycle that opens the file.
- If opening the requested log file fails, fail startup loudly rather than silently falling back.
- Keep `-debug` as behavior control and `-log-file` as destination control.

## Metrics Plan

Add in-memory metrics with mutex/atomic safety.

Suggested metrics:

```text
anthropic_proxy_requests_total{endpoint="...",method="...",status="..."}
anthropic_proxy_request_duration_seconds_bucket{endpoint="...",le="..."}
anthropic_proxy_request_duration_seconds_sum{endpoint="..."}
anthropic_proxy_request_duration_seconds_count{endpoint="..."}
anthropic_proxy_errors_total{endpoint="...",type="..."}
anthropic_proxy_active_requests
anthropic_proxy_backend_requests_total{endpoint="...",status="..."}
```

Latency buckets:

```text
0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10
```

Implementation options:

1. Wrap handlers with middleware that records method/path/status/duration.
2. Use a response writer wrapper to capture status code.
3. For backend status, record it inside handler after backend response is received.

Preferred approach:

- Add a `Metrics` struct to `Handler`.
- Add `ObserveHTTP` middleware wrapper used during route registration.
- Add direct helper calls for backend status/error metrics where backend responses are handled.

## `/metrics` Endpoint Plan

Register:

```go
http.HandleFunc("/metrics", h.MetricsHandler)
```

Behavior:

- `GET` only.
- Return `404` or `503` if `metrics_enabled` is false. Prefer `404` to hide disabled endpoint.
- Content-Type: `text/plain; version=0.0.4`.
- Output Prometheus-compatible text exposition.

Example:

```text
# HELP anthropic_proxy_requests_total Total HTTP requests handled by the proxy.
# TYPE anthropic_proxy_requests_total counter
anthropic_proxy_requests_total{endpoint="/gemini/v1beta/models/:action",method="POST",status="200"} 12
```

Endpoint label normalization:

- Avoid using raw model names as labels.
- Normalize Gemini paths to `/gemini/v1beta/models/{model}:generateContent`, `/gemini/v1beta/models/{model}:streamGenerateContent`, or `/gemini/v1beta/models/{model}:embedContent`.

## `/health` Endpoint Plan

Register:

```go
http.HandleFunc("/health", h.HealthHandler)
```

Behavior:

- `GET` only.
- Return JSON.
- Include proxy status, version-like state if available, backend configuration presence, and store status.
- Do not expose backend API key.

Default response without backend check:

```json
{
  "status": "ok",
  "backend_configured": true,
  "store": {
    "backend": "memory",
    "entries": 3,
    "max_entries": 1000
  }
}
```

If `health_backend_check` is enabled:

- Attempt lightweight `GET {backend}/v1/models` or configured backend model endpoint.
- Use a short timeout.
- Return degraded status if backend check fails.

Suggested statuses:

| Status | Meaning |
|---|---|
| `ok` | proxy is running and config is valid |
| `degraded` | proxy running but backend check failed |
| `error` | invalid local state |

## Store Introspection Plan

`/health` needs store entry count and max entries. Current `store.ResponseStore` may need a small method such as:

```go
func (s *ResponseStore) Stats() StoreStats
```

Potential type:

```go
type StoreStats struct {
    Entries    int `json:"entries"`
    MaxEntries int `json:"max_entries"`
    TTLSeconds int `json:"ttl_seconds"`
}
```

If TTL is not currently stored in a way that is easy to expose, expose only entries/max entries in Phase 3.

## Handler Integration Plan

### Route registration

Update `cmd/anthropic-proxy/main.go`:

```go
http.HandleFunc("/health", h.HealthHandler)
http.HandleFunc("/metrics", h.MetricsHandler)
```

Wrap existing handlers if middleware is implemented:

```go
http.HandleFunc("/anthropic/v1/messages", h.Observe("/anthropic/v1/messages", h.MessagesHandler))
```

Avoid changing route semantics.

### Handler struct

Extend `Handler`:

```go
type Handler struct {
    cfg     *config.Config
    client  *http.Client
    store   *store.ResponseStore
    metrics *Metrics
    logger  *Logger
}
```

Keep constructor backwards-compatible:

```go
func New(cfg *config.Config, s *store.ResponseStore) *Handler
```

## Test Plan

### Validation tests

Add tests for:

- Anthropic missing `model` returns 400.
- Anthropic missing `messages` returns 400.
- Responses missing `model` returns 400.
- Responses missing `input` returns 400.
- Gemini empty `contents` returns 400.
- Gemini `embedContent` without text parts returns 400.
- Invalid `temperature`, `top_p`, `top_k`, and max token values where applicable.

### Request ID tests

Add tests for:

- inbound `x-request-id` appears in client response.
- generated request ID appears when inbound missing.
- outbound backend request gets `x-request-id`.
- Anthropic still returns `request-id` header.

### Health tests

Add tests for:

- `GET /health` returns `200` and `status: ok` with backend configured.
- `GET /health` does not expose API key.
- non-GET returns `405`.
- store stats are included.

### Metrics tests

Add tests for:

- observed handler increments request count.
- status code label is captured correctly.
- `/metrics` returns Prometheus text.
- active request gauge increments/decrements around a request.
- disabled metrics behavior if config supports disabling.

### Logging tests

Prefer unit tests for formatting instead of brittle global log capture:

- text formatter includes required fields.
- JSON formatter emits valid JSON.
- log level filtering works.
- API keys are not present in output.
- `-log-file` overrides `server.log`.
- configured log file is opened in append mode.
- missing log parent directory is created.
- log file open failures return startup errors.

## Documentation Updates

Update:

```text
README.md
docs/ARCHITECTURE.md
docs/ROADMAP.md
docs/TESTING.md
config.example.yaml
```

Documentation should include:

- `/health` endpoint example.
- `/metrics` endpoint example.
- observability config keys.
- `server.log` config and `-log-file` CLI examples.
- request ID behavior.
- validation behavior.
- note that direct-forward endpoints avoid body parsing.

## Implementation Order

1. Add validation helpers and tests.
2. Integrate validation into translated JSON handlers.
3. Add request ID helper and tests.
4. Propagate request IDs to response headers and backend requests.
5. Add logger helper and tests.
6. Add log-to-file config/CLI support and tests.
7. Replace request-level `log.Printf` calls in handlers with logger helper.
8. Add metrics struct and tests.
9. Add middleware/observe wrapper for route metrics.
10. Add `/metrics` endpoint and tests.
11. Add store stats helper if needed.
12. Add `/health` endpoint and tests.
13. Update config example and docs.
14. Run `gofmt`.
15. Run `go test ./...`.
16. Run `make`.
17. Commit after verification if requested.

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

Manual checks after starting the proxy:

```bash
curl http://localhost:8006/health
curl http://localhost:8006/metrics
curl -H "x-request-id: req-test-123" http://localhost:8006/health -i
./build/anthropic-proxy-linux-amd64 start -fg -debug -log-file ./debug-output/proxy.log
```

Then verify the log file exists and receives startup/request logs:

```bash
ls -l ./debug-output/proxy.log
```

## Risks and Decisions

### Risk: validation rejects requests that used to pass

Decision: validate only required fields and clear numeric bounds from public API semantics. Avoid over-validating nested polymorphic content in the first pass.

### Risk: metrics label cardinality

Decision: normalize dynamic paths and never use raw model names, request IDs, or user input in labels.

### Risk: logging sensitive data

Decision: never log request bodies or raw keys. Keep existing `maskKey` pattern if API key visibility is needed for debug logs.

### Risk: `/health` backend check slows down operations

Decision: make backend health check optional and disabled by default.

### Risk: log files grow without bounds

Decision: do not implement log rotation in Phase 3. Document that `server.log` / `-log-file` is intended for debugging or external log management.

### Risk: handler refactor grows too large

Decision: keep helpers package-local in `internal/handler` and avoid a broad router/middleware framework.

## Approval Gate for Implementation

This document is only the Phase 3 implementation plan. Actual implementation should begin only after explicit user approval.
