# Persistence & Reliability Phase 4 Plan

This document describes the implementation plan and completed implementation checklist for Roadmap Phase 4, interpreted as `v2.6.0 — Persistence & Reliability` from `docs/ROADMAP.md`.

## Implementation Status

Status: completed after implementation and verification.

Completed scope:

- Memory/file response store backend.
- Optional backend health monitor and circuit breaker.
- Retry helper for safe buffered JSON backend requests.
- Optional per-IP rate limiter, disabled by default.
- Model mapping aliases added to `*/models` responses.
- Compatibility guardrails for streaming, Responses, Anthropic, and Chat Completions model mapping.

## Goal

Improve reliability for production usage by making `previous_response_id` storage durable, adding backend health monitoring with circuit breaker behavior, adding safe retry logic for transient backend failures, and adding optional proxy-side rate limiting.

Primary goals:

- Add configurable response store backend: `memory` or `file` in Phase 4.
- Preserve the current in-memory behavior as the default.
- Persist Responses API `previous_response_id` data across process restarts when file store is enabled.
- Track backend health periodically.
- Stop forwarding translated/direct requests when the backend is known unhealthy.
- Retry safe backend requests on transient errors.
- Add optional per-IP rate limiting.
- Keep implementation dependency-light and standard-library based.

## Current Project Context

Current relevant behavior:

```text
/openai/v1/responses → translated to /v1/chat/completions
```

The Responses handler stores completed responses so later requests can use:

```json
{
  "previous_response_id": "resp_..."
}
```

Current store implementation:

```text
internal/store/store.go
```

Current store traits:

- in-memory only;
- TTL-based cleanup;
- max-entry eviction;
- `Store(id, response)` and `Get(id)` API;
- `Stats()` for health output;
- process restart loses all previous responses.

Current reliability traits:

- backend HTTP client has a fixed timeout;
- no backend health monitor loop;
- no circuit breaker;
- no retry helper;
- no proxy-side rate limiter;
- `/health` reports local status but does not yet enforce backend reliability behavior.

## Scope

### In scope

- Store interface abstraction.
- Existing memory store adapted to the interface.
- File-based persistent response store.
- Optional Redis config placeholder only, not Redis implementation in this phase.
- Backend health monitor.
- Circuit breaker state: closed, open, half-open.
- Retry helper for retryable backend failures.
- Optional per-IP token bucket rate limiter.
- Config updates.
- Tests for file store, circuit breaker, retry, and rate limiting.
- Documentation updates.

### Out of scope

- Redis store implementation.
- Multi-backend failover; that belongs to Phase 5 / v2.7.0.
- Distributed rate limiting.
- Persistent rate limit counters.
- Full OpenTelemetry tracing.
- Request queueing.
- Retry for streaming requests after response streaming has started.
- Retrying non-idempotent uploads or binary multipart direct-forward requests by default.

## Design Principles

1. **Preserve default behavior.**
   If no new config is set, existing memory store and forwarding behavior should remain familiar.

2. **Protect validated agent compatibility.**
   Claude, Codex, Pi, and OpenCode behavior that has already been manually validated must remain stable throughout Phase 4. Phase 4 reliability work must not reintroduce the previous Anthropic streaming `500`, OpenAI Chat Completions model-mapping `404`, or pre-translation role validation regressions.

3. **Avoid data loss surprises.**
   File store should write atomically and recover gracefully from corrupt/partial records.

4. **Retry only when safe.**
   Retry transient backend failures for JSON request bodies that are fully buffered before forwarding. Avoid retrying streaming or multipart bodies by default.

5. **Fail fast only when explicitly enabled.**
   Circuit breaker behavior should not change normal request forwarding unless the relevant reliability feature is enabled and has observed confirmed backend failures.

6. **Keep labels and state bounded.**
   Rate limiter state should expire inactive clients. Metrics labels should not include raw IPs unless explicitly needed later.

7. **No new dependencies by default.**
   Use the Go standard library. Redis support can be documented as future work.

## Regression Guardrails

Phase 4 implementation must preserve the currently working proxy behavior before adding new reliability behavior.

Mandatory guardrails:

- `/anthropic/v1/messages` must continue to support streaming through `Observe` and `RateLimit` wrappers without losing `http.Flusher`.
- `/anthropic/v1/messages` must not reject translated-compatible roles through pre-translation proxy validation.
- `/openai/v1/responses` must continue to translate to `/v1/chat/completions` and must not be rejected by strict proxy-side input validation before translation.
- `/openai/v1/chat/completions` must continue to apply configured model mapping even though it is a direct-forward endpoint.
- All `*/models` endpoints must include model aliases from configured model mapping when mappings exist, while preserving backend-provided model entries.
- Direct-forward endpoints must preserve the request body except for intentional, tested model mapping changes.
- New reliability features must be default-off or non-invasive when using existing configuration.
- Streaming requests must not be retried after backend response streaming has started.
- Rate limiting must remain disabled by default and must not apply to `/health` or `/metrics`.

Compatibility tests that must keep passing before Phase 4 is considered complete:

- Anthropic Messages streaming still sees `http.Flusher` after observability wrapping.
- Anthropic Messages forwards translation-compatible role/content shapes instead of returning `400 message role must be user or assistant` before translation.
- OpenAI Responses forwards minimal translated requests without pre-translation `400` rejection.
- OpenAI Chat Completions forwards the mapped backend model for configured aliases.
- `*/models` endpoints include configured model-mapping aliases in their model lists without dropping backend models.
- Existing manually validated agents remain compatible: Claude, Codex, Pi, and OpenCode.

## Safe Rollout Strategy

Implement Phase 4 in small checkpoints instead of one large patch:

1. First checkpoint: store interface and memory-store compatibility only.
2. Second checkpoint: file store and file-store tests, disabled unless configured.
3. Third checkpoint: circuit breaker primitives and tests, not yet wired into request paths by default.
4. Fourth checkpoint: retry helper and tests, applied only to safe buffered JSON requests.
5. Fifth checkpoint: optional rate limiter and tests, disabled by default.
6. Final checkpoint: health/metrics/docs/roadmap updates after all compatibility gates pass.

After each checkpoint:

- Run focused package tests for the touched area.
- Run handler compatibility tests covering the guardrails above.
- Run `go test ./...` before moving to the next checkpoint.
- Do not mark `v2.6.0 — Persistence & Reliability` as completed until all Phase 4 features and compatibility gates pass.

## Configuration Plan

Extend config with reliability settings:

```yaml
proxy:
  store_backend: "memory"       # memory|file|redis-placeholder
  store_file: "./data/responses.jsonl"
  store_ttl: 3600
  store_max_entries: 1000

  backend_health_enabled: true
  backend_health_interval: 30
  backend_health_timeout: 5
  circuit_breaker_enabled: true
  circuit_breaker_failure_threshold: 3
  circuit_breaker_cooldown: 30

  retry_enabled: true
  retry_max_attempts: 3
  retry_initial_backoff_ms: 200
  retry_max_backoff_ms: 2000

  rate_limit_enabled: false
  rate_limit_requests_per_minute: 60
  rate_limit_burst: 20
```

Suggested defaults:

| Field | Default | Notes |
|---|---|---|
| `store_backend` | `memory` | Preserve existing behavior |
| `store_file` | `./data/responses.jsonl` | Used only when `store_backend: file` |
| `backend_health_enabled` | `false` or `true` | Prefer `false` if avoiding behavior change; prefer `true` if Phase 4 is explicitly reliability-focused |
| `circuit_breaker_enabled` | `true` | Only meaningful when health/retry records failures |
| `retry_enabled` | `true` | Applies only to safe buffered JSON requests |
| `rate_limit_enabled` | `false` | Avoid surprising users by default |

Recommended default decisions for implementation:

- `store_backend`: `memory`
- `backend_health_enabled`: `false` by default to avoid startup/network surprises
- `circuit_breaker_enabled`: `true`, but no effect until failures are recorded
- `retry_enabled`: `true`
- `rate_limit_enabled`: `false`

## Store Architecture Plan

### Store interface

Introduce an interface in `internal/store`:

```go
type ResponseStore interface {
    Store(id string, resp *types.ResponsesResponse)
    Get(id string) (*types.ResponsesResponse, bool)
    Len() int
    Stats() Stats
    Close() error
}
```

If `Close()` adds too much churn, use a narrower optional interface:

```go
type Closer interface {
    Close() error
}
```

Then keep handler code depending on `store.ResponseStore` interface instead of concrete memory store.

### Memory store

Rename existing concrete type to avoid naming conflict:

```text
MemoryStore
```

Keep constructor compatibility through:

```go
func New(ttl time.Duration, maxSize int) ResponseStore
func NewMemory(ttl time.Duration, maxSize int) *MemoryStore
```

### File store

Add:

```text
internal/store/file.go
internal/store/file_test.go
```

Suggested file format: JSON Lines.

Each line:

```json
{"id":"resp_123","created_at":"2026-06-07T12:00:00Z","response":{...}}
```

File store behavior:

- Load existing records at startup.
- Skip expired records on load.
- If duplicate IDs exist, keep the newest valid record.
- On `Store`, append one JSON line and `Sync()` the file.
- Enforce max entries by in-memory eviction.
- Periodically compact file to remove expired/evicted records.
- Atomic compaction via write temp file then rename.
- Expose stats with backend type `file`, path, entries, max entries, TTL.

### Store factory

Add:

```go
func NewFromConfig(cfg config.ProxyConfig) (ResponseStore, error)
```

Or keep config package decoupled and pass explicit values:

```go
func NewStore(backend, filePath string, ttl time.Duration, maxSize int) (ResponseStore, error)
```

Preferred approach: avoid importing config into store; build the store in `cmd/anthropic-proxy/main.go` based on config fields.

## Backend Health Monitoring Plan

Add package-local reliability helpers, likely in:

```text
internal/handler/reliability.go
internal/handler/reliability_test.go
```

Or create a focused package:

```text
internal/reliability/
```

Preferred approach: use `internal/reliability` if circuit breaker/retry/rate limiter grows beyond handler-local helpers.

### Health monitor

Behavior:

- Periodically check backend with `GET {backend}/v1/models`.
- Use configured short timeout.
- Track last success, last failure, last error, and current health.
- Update circuit breaker on failures/successes.
- Expose status for `/health`.

Status fields:

```json
{
  "backend": {
    "healthy": true,
    "last_success": "...",
    "last_failure": "...",
    "last_error": "",
    "circuit_state": "closed"
  }
}
```

If health monitor is disabled, `/health` should report:

```json
"backend_monitoring": "disabled"
```

## Circuit Breaker Plan

States:

| State | Behavior |
|---|---|
| `closed` | requests pass normally |
| `open` | requests fail fast with `503` |
| `half-open` | allow one probe request after cooldown |

Open circuit when:

- consecutive failures >= `circuit_breaker_failure_threshold`.

Move from open to half-open when:

- cooldown elapsed.

Move from half-open to closed when:

- probe request succeeds.

Move from half-open to open when:

- probe request fails.

Failure conditions:

- connection errors;
- timeouts;
- backend 5xx;
- optionally 429 if retry config treats it as transient.

Do not count as circuit failures:

- client validation errors;
- backend 4xx except optionally 429;
- local rate limit rejections.

## Retry Plan

Add helper for safe backend requests:

```go
func (h *Handler) doBackendRequest(req *http.Request, body []byte, retryable bool) (*http.Response, error)
```

Behavior:

- Retry connection errors, timeouts, 502, 503, 504, optionally 429.
- Exponential backoff with cap.
- Respect `Retry-After` for 429/503 if present and reasonable.
- Record attempts in debug logs and metrics.
- Record final success/failure in circuit breaker.

Safe retry candidates:

- translated Anthropic messages request;
- translated Responses request;
- translated Gemini generateContent non-streaming request;
- Gemini embedContent request;
- OpenAI direct-forward JSON requests only if body is buffered.

Do not retry by default:

- streaming requests after backend response body starts;
- multipart audio transcription;
- binary body direct-forward requests;
- requests with unknown body replayability.

Implementation strategy:

- Start with translated JSON handlers because bodies are already marshaled to `[]byte`.
- Keep direct-forward retry out of first pass unless body buffering is explicitly added.

## Rate Limiting Plan

Add optional per-IP token bucket limiter.

Suggested file:

```text
internal/handler/ratelimit.go
internal/handler/ratelimit_test.go
```

Behavior:

- Disabled by default.
- Key by client IP extracted from `X-Forwarded-For`, `X-Real-IP`, or `RemoteAddr`.
- Token bucket with configured rate and burst.
- Expire inactive buckets periodically.
- Return `429` with API-appropriate error where possible.
- Add `Retry-After` header.

Middleware approach:

```go
http.HandleFunc("/openai/v1/responses", h.Observe(..., h.RateLimit(h.ResponsesHandler)))
```

Or integrated helper:

```go
h.Wrap(endpoint, handler)
```

Preferred approach for Phase 4:

- Add `h.RateLimit(next)` middleware.
- Apply it to all client-facing API endpoints.
- Do not rate limit `/health` or `/metrics` by default.

## Handler Integration Plan

### Handler struct

Extend handler:

```go
type Handler struct {
    cfg        *config.Config
    client     *http.Client
    store      store.ResponseStore
    metrics    *Metrics
    logger     *Logger
    breaker    *CircuitBreaker
    limiter    *RateLimiter
}
```

### Store construction

Update `cmd/anthropic-proxy/main.go`:

```go
responseStore, err := store.NewStore(cfg.Proxy.StoreBackend, cfg.Proxy.StoreFile, ttl, maxEntries)
if err != nil {
    log.Fatalf("failed to initialize response store: %v", err)
}
defer responseStore.Close()
```

### Backend request execution

Replace direct `h.client.Do(proxyReq)` in translated JSON handlers with a helper that understands retry and circuit breaker state.

First-pass targets:

- `MessagesHandler`
- `ResponsesHandler`
- `GeminiHandler` non-streaming
- `geminiEmbedContent`

Streaming requests can still use direct `h.client.Do()` with circuit pre-check but no retry.

## Error Response Plan

Circuit breaker open:

- Anthropic translated endpoint: Anthropic-style `529` or `503` error. Prefer `503` for HTTP semantics.
- Responses endpoint: Responses-style `503` error.
- Gemini endpoint: Gemini-style `503` error.
- OpenAI direct-forward: OpenAI-style `503` error.

Rate limited:

- return `429`.
- include `Retry-After`.
- use endpoint-family matching error envelope where practical.

Retry exhausted:

- return existing backend error if available;
- otherwise `502 Bad Gateway` with proxy error message.

## Metrics and Observability Additions

Extend metrics with:

```text
anthropic_proxy_backend_health{state="healthy|unhealthy"}
anthropic_proxy_circuit_breaker_state{state="closed|open|half_open"}
anthropic_proxy_backend_retries_total{endpoint="...",result="success|exhausted"}
anthropic_proxy_rate_limited_total{endpoint="..."}
anthropic_proxy_store_entries{backend="memory|file"}
```

Logs should include:

- retry attempts;
- circuit breaker state transitions;
- backend health changes;
- rate limit rejections;
- file store load/compact errors.

Sensitive data rules from Phase 3 still apply.

## Test Plan

### Store tests

- Memory store behavior remains unchanged.
- File store persists records across new instance creation.
- File store skips expired records on load.
- File store keeps newest duplicate record.
- File store enforces max entries.
- File compaction removes expired/evicted entries.
- Corrupt JSON lines are skipped without failing startup, but logged.

### Circuit breaker tests

- Closed state allows requests.
- Consecutive failures open circuit.
- Open circuit fails fast.
- Cooldown moves state to half-open.
- Half-open success closes circuit.
- Half-open failure reopens circuit.

### Retry tests

- Retry on connection error.
- Retry on 502/503/504.
- Do not retry on 400/401/404.
- Stop after max attempts.
- Backoff is applied using injectable sleeper to avoid slow tests.
- Successful retry returns final response.

### Rate limit tests

- Disabled limiter allows requests.
- Enabled limiter allows burst then rejects.
- Buckets are keyed by client IP.
- `X-Forwarded-For` is honored.
- Rejections return `429` and `Retry-After`.

### Handler integration tests

- Responses `previous_response_id` survives file store restart.
- Circuit open returns `503` without contacting backend when circuit breaker behavior is explicitly enabled and open.
- Retry succeeds when first backend attempt returns `503` and second returns `200` for safe buffered JSON requests.
- Rate limit applies to API endpoints only when enabled, but not `/health` or `/metrics`.
- Existing compatibility tests for Anthropic streaming, Anthropic/Responses pre-translation forwarding, and OpenAI Chat Completions model mapping remain green.

## Compatibility Gate

Before merging each Phase 4 checkpoint, run the existing compatibility safety net:

```bash
go test ./internal/handler
```

The safety net must include tests that prevent regressions for:

- Anthropic streaming through observability middleware.
- Anthropic Messages forwarding without strict pre-translation role rejection.
- OpenAI Responses forwarding without strict pre-translation input rejection.
- OpenAI Chat Completions model mapping on direct-forward requests.
- All `*/models` endpoints exposing configured mapping aliases without removing backend models.

A checkpoint should not proceed if any compatibility test fails, even if the new reliability tests pass.

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

- store backend config examples;
- file store path and durability notes;
- backend health monitor behavior;
- circuit breaker states;
- retry config and limitations;
- rate limiting config;
- operational notes for file store backups/permissions.

## Implementation Order

Use the safe rollout checkpoints above as the implementation boundary. Do not combine unrelated checkpoints into one large patch.

1. Confirm the current compatibility baseline with `go test ./internal/handler` and `go test ./...`.
2. Add store interface and adapt memory store without changing default runtime behavior.
3. Run focused store tests, handler compatibility tests, and `go test ./...`.
4. Add file store, factory/wiring, and file-store tests; keep it disabled unless `store_backend: file` is configured.
5. Run focused store tests, handler compatibility tests, and `go test ./...`.
6. Add health monitor and circuit breaker primitives with tests; keep behavior non-invasive by default.
7. Run reliability tests, handler compatibility tests, and `go test ./...`.
8. Add retry helper with injectable backoff/sleeper and tests; apply only to safe buffered JSON request paths.
9. Run retry tests, handler compatibility tests, and `go test ./...`.
10. Add optional rate limiter and tests; keep rate limiting disabled by default and exclude `/health` and `/metrics`.
11. Run rate limit tests, handler compatibility tests, and `go test ./...`.
12. Extend `/health` and `/metrics` with reliability state without exposing secrets.
13. Update `config.example.yaml`, README, architecture, testing docs, and roadmap status.
14. Run `gofmt` on changed Go files.
15. Run `go test ./...`.
16. Run `make`.
17. Mark `v2.6.0 — Persistence & Reliability` as completed only after all features and compatibility gates pass.
18. Commit after verification if requested.

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

Manual checks:

```bash
# file store config
./build/anthropic-proxy-linux-amd64 start -fg -config config.yaml

# health and metrics
curl http://localhost:8006/health
curl http://localhost:8006/metrics

# rate limit smoke test, if enabled
for i in $(seq 1 100); do curl -s -o /dev/null -w "%{http_code}\n" http://localhost:8006/health; done
```

## Risks and Decisions

### Risk: file store grows without bound

Decision: compact periodically and after max-entry eviction. Keep JSONL format for append-safety and inspectability.

### Risk: retry duplicates backend work

Decision: only retry buffered JSON requests and transient backend failures. Do not retry streaming or multipart requests by default.

### Risk: circuit breaker rejects traffic during short backend blips

Decision: make threshold/cooldown configurable and expose current state in `/health`.

### Risk: rate limiting surprises local users

Decision: disabled by default.

### Risk: Redis scope grows too large

Decision: include Redis config placeholder only; implement Redis in a later phase if needed.

## Approval Gate for Implementation

This document is only the Phase 4 implementation plan. Actual implementation should begin only after explicit user approval.
