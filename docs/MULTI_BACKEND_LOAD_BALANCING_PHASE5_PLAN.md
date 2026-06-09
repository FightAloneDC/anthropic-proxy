# Multi-Backend & Load Balancing Phase 5 Plan

This document describes the implementation plan for Roadmap Phase 5, interpreted as `v2.7.0 — Multi-Backend & Load Balancing` from `docs/ROADMAP.md`.

## Implementation Status

Status: completed after implementation and verification.

Completed scope:

- Multiple backend configuration in YAML.
- Backward-compatible single `backend:` configuration.
- Backend selection by requested or mapped model name.
- Optional failover from primary backend to secondary candidates.
- Load balancing across matching backends.
- Per-backend health/circuit/retry integration.
- Backend-specific request translation overrides for narrowly defined compatibility differences.
- Health, metrics, tests, and documentation updates.

## Goal

Allow one proxy instance to route traffic to more than one OpenAI-compatible backend while preserving current behavior for existing single-backend users.

Primary goals:

- Keep the existing `backend.url` and `backend.api_key` config working as the default path.
- Add a `backends:` list for named backend pools.
- Route requests by model pattern after model mapping has been applied.
- Support fallback/failover when a selected backend is unavailable or returns retryable backend errors.
- Support configurable load balancing strategies: `round_robin`, `weighted`, and `least_connections`.
- Preserve translated endpoint behavior for Anthropic Messages, OpenAI Responses, Gemini generateContent, and Gemini embedContent.
- Preserve direct-forward behavior for OpenAI-compatible endpoints including Chat Completions and multi-modal endpoints.
- Keep new behavior dependency-light and standard-library based.

## Current Project Context

Current relevant configuration shape:

```yaml
backend:
  url: "https://url-to-openai-compatible-backend.com/v1"
  api_key: "ogw_live_xxx"

models:
  - from: "claude-opus-4-8"
    to: "mimo-v2.5-pro"
```

Current backend access is centralized only partially:

- `internal/config/config.go` defines a single `BackendConfig` and global `models` mapping.
- `internal/handler/handler.go` stores `cfg.Backend.URL` and `cfg.Backend.APIKey` into health monitoring setup.
- Anthropic Messages, OpenAI Responses, OpenAI Chat Completions, Gemini generateContent, and Gemini embedContent build target URLs directly from `cfg.Backend.URL`.
- `internal/handler/forward.go` directly forwards OpenAI-compatible multi-modal endpoints to `cfg.Backend.URL`.
- `internal/handler/reliability.go` wraps non-streaming JSON requests with retry/circuit behavior and has a separate streaming circuit path.
- `/health` and `/metrics` currently expose one backend health/circuit snapshot.
- `*/models` endpoints query a single backend `/v1/models` and enrich that response with configured model mapping aliases.

Existing reliability features from v2.6 are important foundations but are currently single-backend oriented:

- backend health monitor;
- circuit breaker;
- retry helper;
- rate limiting;
- file/memory response store.

## Scope

### In scope

- Config structs for multiple named backends.
- Backward compatibility adapter from existing `backend:` into an internal backend list.
- Backend model pattern matching with simple glob-like patterns such as `mimo-*`, `deepseek-*`, and `*`.
- Backend selection service that returns a target backend for a request.
- Model-aware routing for all LLM and embedding translation paths.
- Direct-forward routing for OpenAI Chat Completions and multi-modal endpoints.
- Failover for safe buffered JSON requests before a client response is committed.
- Failover for streaming requests only before backend response streaming starts.
- Per-backend circuit breaker and optional health monitor.
- Per-backend retry executor or executor pool.
- Load balancing strategies:
  - `round_robin`;
  - `weighted`;
  - `least_connections`.
- Health output expanded to list configured backend statuses without exposing API keys.
- Metrics labels for backend name, with bounded cardinality from config names only.
- Tests for config parsing, routing, load balancing, failover, health/metrics, and compatibility guardrails.
- Documentation and config example updates.

### Out of scope

- Dynamic backend discovery.
- Runtime config reload.
- Cross-process or distributed load balancing state.
- Backend credentials from external secret managers.
- Full plugin architecture for arbitrary translators.
- Backend-specific response translation beyond currently supported response formats.
- Retrying or failing over multipart/binary direct-forward uploads after request body streaming has begun.
- Request queueing.
- Sticky sessions or per-client affinity.
- Authentication and per-client model permissions; that belongs to v2.8.0.

## Design Principles

1. **Preserve existing behavior by default.**
   A config with only `backend:` must behave like the current v2.6 implementation.

2. **Route after model mapping.**
   Existing `models.from → models.to` mapping must remain active. Backend pattern matching should use the backend-facing model name after mapping, because backend `models` patterns describe what each backend can serve.

3. **Keep compatibility guardrails first.**
   Claude, Codex, Pi, and OpenCode behavior that has already been manually validated must remain stable. Do not reintroduce Anthropic streaming `500`, OpenAI Chat Completions model-mapping `404`, or pre-translation validation regressions.

4. **Fail over only when safe.**
   Buffered JSON requests can be retried or failed over before the response is written. Streaming requests can try alternate backends only before a backend response is accepted; once streaming to the client begins, do not switch backend.

5. **Avoid request body surprises.**
   Direct-forward endpoints should preserve request bodies except for intentional, tested model mapping and backend override transformations.

6. **Bound observability labels.**
   Metrics can label by configured backend name and endpoint, but must not include raw model names unless explicitly bounded by config.

7. **Prefer small internal abstractions.**
   Introduce only the backend routing abstractions needed to remove duplicated `cfg.Backend` URL construction. Avoid broad plugin abstractions in this phase.

## Regression Guardrails

Mandatory compatibility guardrails:

- Existing single-backend config must keep working without adding `backends:`.
- CLI `-url` and `-key` overrides must continue to override the effective single backend.
- Existing environment fallbacks `OPENAI_BASE_URL`, `OPENAI_API_KEY`, and `MODEL_MAP` must remain compatible.
- `/anthropic/v1/messages` must continue to support streaming through `Observe` and `RateLimit` wrappers without losing `http.Flusher`.
- `/anthropic/v1/messages` must continue to forward translation-compatible role/content shapes instead of returning pre-translation role validation errors.
- `/openai/v1/responses` must continue to translate to `/v1/chat/completions` and preserve `previous_response_id` behavior.
- `/openai/v1/chat/completions` must continue to apply configured model mapping even though it is a direct-forward endpoint.
- `/gemini/v1beta/models/{model}:generateContent` and `:streamGenerateContent` must continue applying model mapping before routing.
- `/gemini/v1beta/models/{model}:embedContent` must continue routing to `/v1/embeddings` with mapped model names.
- Multi-modal direct-forward endpoints must not require JSON parsing unless model-based routing is possible and the body can be safely buffered.
- `*/models` endpoints must include model mapping aliases and should aggregate backend model responses when multiple backends are configured.
- New multi-backend behavior must not expose backend API keys in health, metrics, logs, or errors.
- Streaming requests must not be retried or failed over after backend response streaming has started.

Compatibility tests that must pass before Phase 5 is considered complete:

- Anthropic Messages non-streaming routes to the backend selected by mapped model.
- Anthropic Messages streaming preserves `http.Flusher` and routes to selected backend.
- OpenAI Responses routes by mapped model and still stores completed responses.
- OpenAI Chat Completions routes by mapped model and forwards the mapped backend model.
- Gemini generateContent and streamGenerateContent route by mapped model.
- Gemini embedContent routes by mapped embedding model.
- Direct-forward embeddings/rerank/audio/image endpoints preserve path, headers, backend auth, and body.
- Single-backend config tests from v2.6 continue passing unchanged.
- Existing manually validated agents remain compatible: Claude, Codex, Pi, and OpenCode.

## Configuration Plan

### Backward-compatible existing config

Existing config remains valid:

```yaml
backend:
  url: "https://backend-a.com/v1"
  api_key: "key-a"
```

When `backends:` is absent or empty, the application should internally create one backend named `default` from `backend:`.

### New multi-backend config

```yaml
backend:
  url: "https://backend-a.com/v1"
  api_key: "key-a"

backends:
  - name: primary
    url: "https://backend-a.com/v1"
    api_key: "key-a"
    models: ["mimo-*", "gpt-*", "text-embedding-3-*"]
    weight: 2
    priority: 10
    enabled: true

  - name: secondary
    url: "https://backend-b.com/v1"
    api_key: "key-b"
    models: ["deepseek-*", "mimo-*"]
    weight: 1
    priority: 20
    enabled: true

proxy:
  load_balance_strategy: "round_robin"  # round_robin|weighted|least_connections
  failover_enabled: true
  failover_max_backends: 2
```

Recommended config semantics:

| Field | Default | Notes |
|---|---|---|
| `backends[].name` | required for multi-backend | Stable metrics/health label |
| `backends[].url` | required | May include `/v1`; normalize internally |
| `backends[].api_key` | empty | Backend-specific auth |
| `backends[].models` | `["*"]` | Backend model patterns after model mapping |
| `backends[].weight` | `1` | Used by weighted strategy |
| `backends[].priority` | `0` | Lower value preferred for primary ordering, or use documented ascending order |
| `backends[].enabled` | `true` | Disabled backends ignored |
| `proxy.load_balance_strategy` | `round_robin` | Applies among matching healthy backends |
| `proxy.failover_enabled` | `true` | Allows trying alternate matching backends for safe requests |
| `proxy.failover_max_backends` | `0` | `0` means all matching candidates |

### Model mapping interaction

Global mapping remains:

```yaml
models:
  - from: "claude-opus-4-8"
    to: "mimo-v2.5-pro"
```

Routing should use this flow:

```text
client model → global model mapping → backend-facing model → backend model pattern match
```

Example:

```text
client sends: claude-opus-4-8
mapping gives: mimo-v2.5-pro
backend matching uses: mimo-v2.5-pro
selected backend: first healthy backend whose models include mimo-*
```

## Architecture Plan

### Backend package

Add a focused backend package:

```text
internal/backend/backend.go
internal/backend/router.go
internal/backend/pattern.go
internal/backend/balancer.go
internal/backend/backend_test.go
internal/backend/router_test.go
internal/backend/balancer_test.go
```

Suggested core types:

```go
type Backend struct {
    Name     string
    URL      string
    APIKey   string
    Models   []string
    Weight   int
    Priority int
    Enabled  bool
}

type Target struct {
    Backend *Backend
    URL     string
}

type Router struct {
    backends []*BackendRuntime
    strategy Strategy
}
```

`BackendRuntime` can hold runtime-only state:

```go
type BackendRuntime struct {
    Config      Backend
    Breaker     *reliability.CircuitBreaker
    Executor    *reliability.BackendExecutor
    Health      *reliability.HealthMonitor
    ActiveCount int64
}
```

Keep URL normalization in one place:

```text
normalizeBackendBase(url) + targetPath
```

This replaces repeated code currently trimming `/v1` in handlers.

### Pattern matching

Support simple wildcard model patterns only:

- `*` matches all models;
- `mimo-*` prefix match;
- `*-embedding` suffix match if needed;
- exact string match.

Use standard-library matching via `path.Match` only if behavior is clearly documented and tests cover special characters. Otherwise implement minimal `*` handling to avoid surprising glob semantics.

### Backend selection flow

For each request:

1. Determine backend-facing model if possible.
2. Ask router for ordered candidate backends:

   ```text
   candidates = enabled backends matching model pattern
   candidates = candidates whose circuit allows request
   candidates = load-balance order by strategy
   ```

3. Build backend target URL from selected backend and endpoint path.
4. Attach selected backend API key.
5. Execute request through that backend's executor/circuit.
6. On retryable failure and if failover is enabled, try the next candidate when safe.

If no backend matches the model:

- return API-family-specific `502` or `400` with a clear error such as `no backend configured for model: <model>`;
- prefer not to silently fall back to a random backend unless a backend has `models: ["*"]`.

### Load balancing strategies

#### Round-robin

- Maintain per-router atomic index.
- Among matching healthy candidates, rotate selected backend.
- Deterministic enough for tests by exposing or resetting router state in test setup.

#### Weighted

- Expand backend candidates by weight or implement smooth weighted round-robin.
- For simplicity and low backend counts, weighted expansion is acceptable if bounded by config validation.
- Treat weight `<= 0` as `1`.

#### Least-connections

- Track active in-flight requests per backend runtime.
- Increment when a backend attempt starts.
- Decrement when backend response body is closed or after request attempt finishes.
- For streaming, active count should remain incremented until streaming response body is closed.

## Handler Integration Plan

### Centralize backend request creation

Introduce helper methods in `internal/handler`:

```go
func (h *Handler) backendCandidates(model string, path string) ([]*backend.Target, error)
func (h *Handler) newBackendRequest(target *backend.Target, method string, body io.Reader, requestID string) (*http.Request, error)
func (h *Handler) doBackendCandidates(candidates []*backend.Target, method, path string, body []byte, retryable bool) (*http.Response, *backend.Target, error)
func (h *Handler) doStreamingBackendCandidates(candidates []*backend.Target, method, path string, body []byte) (*http.Response, *backend.Target, error)
```

Exact function names can differ, but request path construction and backend auth should become centralized.

### Translated JSON endpoints

Apply routing after model mapping and translation request construction:

- Anthropic Messages:
  - parse Anthropic request;
  - apply global model mapping;
  - translate to Chat Completions;
  - route by `openaiReq.Model` to `/v1/chat/completions`.

- OpenAI Responses:
  - parse Responses request;
  - handle `previous_response_id`;
  - apply global model mapping;
  - translate to Chat Completions;
  - route by `openaiReq.Model` to `/v1/chat/completions`.

- Gemini generateContent:
  - parse model from URL;
  - apply global model mapping;
  - translate to Chat Completions;
  - route by `openaiReq.Model` to `/v1/chat/completions`.

- Gemini embedContent:
  - parse model from URL;
  - apply global model mapping;
  - translate to Embeddings;
  - route by `openaiReq.Model` to `/v1/embeddings`.

### OpenAI Chat Completions

`ChatCompletionsHandler` should continue buffering JSON to apply model mapping and detect `stream`.

Routing flow:

```text
read body → parse model/stream → apply global model mapping in body → route by mapped model → forward to selected backend
```

Keep existing tests for model mapping and add routing-specific tests.

### Direct-forward multi-modal endpoints

Existing `DirectForwardHandler(targetPath)` does not parse JSON and currently streams `r.Body` directly. Multi-backend routing introduces a trade-off because model-based routing usually requires reading the body.

Recommended staged behavior:

1. If only one effective backend exists, keep current streaming direct-forward behavior.
2. If multiple backends exist and request is JSON with a `model` field, buffer body, route by mapped model, then forward buffered body.
3. If multiple backends exist and request is multipart or non-JSON, route to the default backend or the first `models: ["*"]` backend, unless a future endpoint-specific config is added.
4. Do not fail over multipart/binary upload bodies by default.

This preserves current audio transcription and binary behavior while allowing embeddings/rerank/image JSON requests to route by model.

### Models endpoints

Current `ModelsHandler` queries one backend `/v1/models` and enriches aliases.

For multi-backend:

- Query all enabled/healthy backends with `/v1/models`.
- Merge `data[]` entries by `id`.
- Add configured alias models from global `models` mapping.
- Do not include backend API keys in errors.
- If some backend model queries fail, choose one of two documented behaviors:
  - strict: fail the whole models request;
  - tolerant: return models from healthy/responding backends and log failures.

Recommended behavior: tolerant by default, strict only if all queried backends fail.

## Backend-specific Translation Overrides

Roadmap notes that different backends may need slightly different request formats.

Recommended minimal config:

```yaml
backends:
  - name: primary
    url: "https://backend-a.com/v1"
    api_key: "key-a"
    models: ["mimo-*"]
    overrides:
      drop_fields: ["top_k"]
      allow_fields: []
```

Suggested initial scope:

- `drop_fields`: remove top-level JSON request fields before forwarding to that backend.
- Apply only to translated/buffered JSON requests and OpenAI Chat Completions JSON bodies.
- Do not apply to multipart/binary direct-forward endpoints.
- Keep overrides default-empty.

Out of initial override scope:

- arbitrary field renaming;
- response transformations;
- custom scripting;
- endpoint-specific plugin behavior.

This gives a narrow escape hatch for backend incompatibilities without prematurely implementing v3.0 plugin architecture.

## Reliability Integration Plan

Current v2.6 reliability primitives should become per-backend rather than global-only.

### Circuit breaker

- Each backend runtime gets its own circuit breaker.
- A backend with an open circuit should be skipped during candidate selection.
- If all matching backend circuits are open, return API-family-specific `503`.

### Retry

- Each backend runtime gets its own `BackendExecutor`.
- Retry remains within the same backend attempt.
- Failover moves to the next backend after that backend attempt fails with a retryable error/status.
- Preserve current non-streaming JSON retry behavior.

### Health monitor

- Start one health monitor per enabled backend when health monitoring is enabled.
- Health checks target each backend's `/v1/models` or configured health URL if later added.
- `/health` should list backend names and status snapshots.

### Streaming

- Streaming path should consult per-backend circuit before making the request.
- If selected backend returns a retryable status before client streaming starts, failover may try another matching backend.
- Once the proxy starts writing SSE to the client, no backend switch is allowed.

## Health and Metrics Plan

### Health response

Expand `/health` while preserving existing top-level fields where possible:

```json
{
  "status": "ok",
  "backend_configured": true,
  "backend": {
    "monitoring_enabled": true,
    "healthy": true,
    "circuit": {"state":"closed"}
  },
  "backends": [
    {
      "name": "primary",
      "configured": true,
      "enabled": true,
      "healthy": true,
      "circuit": {"state":"closed"},
      "models": ["mimo-*"]
    }
  ],
  "store": {"backend":"memory","entries":0}
}
```

For single-backend configs, `backend` remains meaningful and `backends` contains the synthetic `default` backend.

### Metrics

Add bounded backend labels:

```text
anthropic_proxy_backend_requests_total{backend="primary",endpoint="/openai/v1/chat/completions",status="200"} 12
anthropic_proxy_backend_active_requests{backend="primary"} 1
anthropic_proxy_backend_circuit_breaker_state{backend="primary",state="closed"} 1
```

Keep existing metrics unchanged to avoid breaking dashboards.

## Testing Plan

### Config tests

- Existing single `backend:` config loads unchanged.
- `backends:` config loads multiple enabled backends.
- Empty backend name is rejected or normalized only for synthetic default.
- Duplicate backend names are rejected.
- Missing URL in enabled multi-backend entry is rejected.
- Defaults are applied for models, weight, enabled, failover, and strategy.
- CLI `-url` and `-key` keep overriding the effective backend for single-backend mode.

### Backend router tests

- Exact model match.
- Prefix wildcard match, e.g. `mimo-*`.
- Catch-all `*` match.
- No matching backend returns a clear error.
- Disabled backend is ignored.
- Open circuit backend is skipped.
- Round-robin alternates among candidates.
- Weighted routing approximates configured weights in deterministic sequence.
- Least-connections chooses the candidate with the lowest active count.

### Handler tests

- Anthropic Messages routes by mapped model to backend A.
- Anthropic Messages failover tries backend B if backend A returns `503` before response commit.
- Anthropic streaming routes by mapped model and preserves flusher behavior.
- OpenAI Responses routes by mapped model and still stores response IDs.
- OpenAI Chat Completions applies model mapping before backend selection.
- Gemini generateContent routes by mapped URL model.
- Gemini embedContent routes by mapped embedding model.
- Direct-forward embeddings JSON routes by model when multiple backends exist.
- Direct-forward audio speech preserves binary response.
- Multipart transcription does not attempt unsafe failover.
- Models handler merges model lists across backends and includes aliases.
- Health handler does not leak API keys.
- Metrics include backend label and bounded values.

### Regression tests to keep

Continue running the current v2.6 guardrail tests:

- `TestChatCompletionsHandlerAppliesModelMapping`
- `TestModelsHandlerIncludesModelMappingAliases`
- Anthropic validation/streaming compatibility tests.
- OpenAI Responses validation compatibility tests.
- Gemini generateContent/stream/embed tests.
- Direct-forward body/path/auth tests.
- Reliability retry/circuit/rate-limit tests.

## Safe Rollout Strategy

Implement Phase 5 in small checkpoints:

1. **Config compatibility checkpoint**
   - Add `backends` config structs and defaults.
   - Add internal effective backend list.
   - Preserve current single-backend behavior.
   - Run config and existing handler tests.

2. **Backend router checkpoint**
   - Add backend router, pattern matching, and load balancing primitives.
   - Add unit tests independent of handlers.

3. **Single-backend handler adapter checkpoint**
   - Replace direct `cfg.Backend` URL construction with centralized target helpers.
   - Keep behavior equivalent for single backend.
   - Run all existing tests.

4. **Model-aware routing checkpoint**
   - Wire translated endpoints and Chat Completions to route by mapped model.
   - Add routing tests for Anthropic, Responses, Gemini, and Chat Completions.

5. **Direct-forward routing checkpoint**
   - Add safe JSON model extraction for direct-forward endpoints.
   - Preserve streaming/multipart behavior for non-JSON direct-forward requests.
   - Add direct-forward multi-backend tests.

6. **Failover checkpoint**
   - Add ordered candidate execution and safe failover.
   - Keep streaming failover limited to pre-response attempts.
   - Add retry/failover tests.

7. **Per-backend reliability checkpoint**
   - Move circuit/executor/health monitor state to backend runtime.
   - Update health and metrics.
   - Add backend-labeled health/metrics tests.

8. **Backend overrides checkpoint**
   - Add narrow `drop_fields` override support for buffered JSON requests.
   - Add tests proving override is backend-specific and default-empty.

9. **Docs and final verification checkpoint**
   - Update `config.example.yaml`, `README.md` if needed, and `docs/ROADMAP.md` after implementation is verified.
   - Run full verification.

After each checkpoint:

- Run focused tests for touched packages.
- Run relevant handler regression tests.
- Run `go test ./...` before moving to the next checkpoint.
- Do not mark `v2.7.0 — Multi-Backend & Load Balancing` as completed until all planned features and compatibility gates pass.

## Verification Plan

Recommended verification commands:

```bash
go test ./...
make local
```

Manual validation targets after automated tests pass:

- Claude
- Codex
- Pi
- OpenCode

Manual validation should cover at least:

- Anthropic Messages non-streaming and streaming.
- OpenAI Responses non-streaming and streaming.
- OpenAI Chat Completions model mapping.
- Gemini generateContent and streamGenerateContent.
- Embeddings or Gemini embedContent.
- A multi-backend config where two model families route to different test backends.
- A failover scenario where primary returns `503` and secondary succeeds.

## Documentation Update Plan

Update these files during implementation:

- `config.example.yaml`
  - Add commented `backends:` example.
  - Add load balancing and failover proxy fields.

- `README.md`
  - Add multi-backend configuration section.
  - Explain model routing uses mapped backend model names.
  - Explain failover and strategy behavior.

- `docs/ROADMAP.md`
  - Mark v2.7.0 completed only after verification.

- Optional new doc if README becomes too large:
  - `docs/MULTI_BACKEND_LOAD_BALANCING.md`

## Open Decisions Before Implementation

These should be confirmed before coding if not already decided:

1. Should `backends:` fully override `backend:`, or should `backend:` also be included as a default backend when both are present?
   - Recommended: when `backends:` is non-empty, it is authoritative; keep `backend:` only for backward compatibility and CLI/env fallback.

2. Should no model match return `400` invalid request or `502` backend routing error?
   - Recommended: use API-family-specific error with `502` because the request may be valid but proxy routing cannot satisfy it.

3. Should `/v1/models` aggregation be tolerant or strict when one backend fails?
   - Recommended: tolerant unless all enabled backend model queries fail.

4. Should backend-specific overrides be implemented in the first v2.7 pass or deferred after core routing/failover is stable?
   - Recommended: implement after core routing/failover, with only `drop_fields` initially.

## Completion Criteria

Phase 5 is complete when:

- Existing single-backend config remains compatible.
- Multi-backend config can route different mapped models to different backends.
- Round-robin, weighted, and least-connections strategies have unit coverage.
- Safe failover works for buffered JSON requests.
- Streaming paths do not switch backend after client streaming starts.
- Per-backend health and metrics are visible without leaking secrets.
- `config.example.yaml` and docs describe the new behavior.
- `go test ./...` passes.
- `make local` passes.
- Manual validation with Claude, Codex, Pi, and OpenCode does not show regressions.
