# Streaming 500 Regression Fix Plan

## Goal

Fix the Claude/Anthropic streaming request failure introduced by the observability middleware added during the roadmap phase updates. The proxy should preserve the original streaming behavior while still recording request IDs, logs, and metrics.

## Problem Summary

Claude clients commonly send `stream: true` to `/anthropic/v1/messages`. The active repo now registers that route through:

```text
h.Observe("/anthropic/v1/messages", h.RateLimit(h.MessagesHandler))
```

`Observe` wraps the original `http.ResponseWriter` with `statusRecorder`. The wrapper records HTTP status codes, but it does not preserve optional interfaces implemented by the original writer, especially `http.Flusher`.

`MessagesHandler` eventually calls `streamResponse` for streaming requests. `streamResponse` requires:

```go
flusher, ok := w.(http.Flusher)
```

Because the wrapper does not implement `Flush()`, this check fails and the proxy returns `500` with a streaming-not-supported error before streaming the backend response.

## Root Cause

`internal/handler/observability.go` introduces `statusRecorder` but only implements:

- `WriteHeader(status int)`
- `Write([]byte)`

It does not forward `Flush()` to the wrapped `ResponseWriter`. This strips streaming support from handlers wrapped by `Observe`.

## Implementation Plan

1. Update `statusRecorder` in `internal/handler/observability.go` to implement `http.Flusher`.
2. In `Flush()`, type-assert the wrapped `ResponseWriter` to `http.Flusher` and call its `Flush()` when available.
3. Ensure `Flush()` marks the response as `200 OK` if no status has been written yet, matching normal `ResponseWriter.Write` behavior.
4. Add a regression test in `internal/handler/observability_test.go` proving that a handler wrapped by `Observe` still sees `http.Flusher`.
5. Run focused Go tests for `internal/handler`.
6. Optionally run broader `go test ./...` if focused tests pass.

## Expected Change Scope

Files expected to change:

- `internal/handler/observability.go`
- `internal/handler/observability_test.go`

Plan document:

- `docs/STREAMING_500_REGRESSION_FIX_PLAN.md`

## Verification

Focused verification:

```text
go test ./internal/handler
```

Broader verification:

```text
go test ./...
```

## Non-Goals

- No git commit.
- No changes to translator behavior.
- No changes to backend retry/circuit breaker semantics unless testing reveals a separate confirmed bug.
- No broad refactor of observability middleware.
