# Memory Bug & Crash/Panic Audit — anthropic-proxy

**Date:** 2026-06-11
**Scope:** All Go source files under `internal/` and `cmd/`
**Severity Levels:** CRITICAL, HIGH, MEDIUM, LOW
**Status:** All 13 bugs patched and verified (build + test + race detector pass)

---

## Executive Summary

| Severity | Count |
|----------|-------|
| CRITICAL | 0 |
| HIGH     | 5 |
| MEDIUM   | 3 |
| LOW      | 5 |
| **Total** | **13** |

---

## HIGH Severity

### BUG-01: Rate Limiter Bucket Map Grows Unbounded (Memory Leak)

**File:** `internal/reliability/ratelimit.go:38-68`
**Type:** Memory leak

The `RateLimiter` maintains a `map[string]*bucket` keyed by client IP. The `Cleanup()` method (line 71) exists to evict stale entries, but it is **never called** from anywhere in the codebase.

In `handler.go:77`, the limiter is created:
```go
limiter: reliability.NewRateLimiter(cfg.Proxy.RateLimitEnabled, ...)
```

No goroutine, ticker, or periodic call invokes `limiter.Cleanup()`.

**Impact:** Under sustained traffic from many distinct client IPs, the `buckets` map grows without bound. Over hours/days of operation, this causes increasing memory consumption and eventual OOM on a memory-constrained system.

**Fix:** Start a background goroutine in `NewRateLimiter` or in `Handler.New` that calls `Cleanup()` periodically (e.g., every `interval` duration).

---

### BUG-02: Response ID Counter Has Data Race (Panic/Crash)

**File:** `internal/translator/responses.go:485-490`
**Type:** Race condition

```go
var idSeq int64

func idCounter() int64 {
    idSeq++
    return idSeq
}
```

`idSeq` is a package-level `int64` incremented without synchronization. When multiple HTTP request goroutines call `GenerateResponseID()` concurrently, this is a **data race**. Under Go's race detector, this will abort the program. Even without the race detector, it can produce duplicate IDs or corrupt the value.

**Impact:** Duplicate response IDs cause `previous_response_id` lookups to return wrong responses. With `-race`, the process crashes immediately.

**Fix:** Use `atomic.AddInt64(&idSeq, 1)` or a `sync.Mutex`.

---

### BUG-03: Unbounded `io.ReadAll(r.Body)` — OOM via Malicious Request (Crash)

**File:** `internal/handler/handler.go:168`, `handler.go:370`, `handler.go:552`, `handler.go:653`
**Type:** Resource exhaustion / crash

All HTTP handlers read the entire request body into memory without a size limit:

```go
body, err := io.ReadAll(r.Body)
```

A malicious client can send a multi-gigabyte request body, consuming all available memory and crashing the proxy with OOM.

**Affected handlers:**
- `MessagesHandler` (Anthropic)
- `ResponsesHandler` (OpenAI Responses)
- `ChatCompletionsHandler` (OpenAI Chat)
- `GeminiHandler` (Gemini)

**Impact:** Single malicious request crashes the entire proxy process.

**Fix:** Use `io.LimitReader(r.Body, maxBodySize)` with a configurable limit (e.g., 10 MB).

---

### BUG-04: Unbounded `io.ReadAll(resp.Body)` on Backend Responses (Crash)

**File:** `internal/handler/handler.go:251`, `handler.go:444`, `handler.go:776`, `handler.go:799`
**Type:** Resource exhaustion / crash

Non-streaming response handlers read the entire backend response body without limit:

```go
body, err := io.ReadAll(resp.Body)
```

A misbehaving or compromised backend could return a multi-gigabyte response, causing OOM.

**Impact:** Backend misbehavior crashes the proxy.

**Fix:** Use `io.LimitReader(resp.Body, maxResponseSize)` with a configurable limit (e.g., 100 MB).

---

### BUG-05: Unsafe Type Assertions in Message Translation (Panic)

**File:** `internal/translator/request.go:111`, `request.go:156`, `request.go:186`, `request.go:230-231`, `request.go:242`
**Type:** Nil pointer dereference / panic

Multiple type assertions on `map[string]interface{}` values are performed without the two-value check form:

```go
// Line 111 — translateSystem
text := b["text"].(string)                    // panics if "text" is nil or not string

// Line 156 — translateMessages (text block)
text := b["text"].(string)                    // panics if "text" is nil or not string

// Line 186 — translateMessages (image block)
imageURL = source["url"].(string)             // panics if "url" is nil or not string

// Lines 230-231 — translateMessages (tool_use block)
b["id"].(string)                              // panics if "id" is nil
b["name"].(string)                            // panics if "name" is nil

// Line 242 — translateMessages (tool_result block)
b["tool_use_id"].(string)                     // panics if "tool_use_id" is nil
```

If a client sends a malformed Anthropic request where a content block has `type: "text"` but the `text` field is missing or null, the proxy panics and the request goroutine crashes. While Go's HTTP server recovers from panics, it kills the goroutine and returns a 500 with no useful error message.

**Impact:** Malformed requests cause panics. Under Go 1.21+, panics in goroutines kill the process unless recovered.

**Fix:** Use the two-value form: `text, ok := b["text"].(string)` and handle the missing case.

---

## MEDIUM Severity

### BUG-06: Tool Call Arguments Index Out of Bounds (Panic)

**File:** `internal/translator/responses_stream.go:129-130`
**Type:** Index out of bounds / panic

```go
if tc.Function.Arguments != "" {
    idx := len(st.toolCalls) - 1
    st.toolCalls[idx].arguments += tc.Function.Arguments
```

If a backend sends a chunk with `Function.Arguments` but no preceding chunk with `tc.ID` (which adds the entry to `st.toolCalls`), then `st.toolCalls` is empty and `idx` becomes `-1`, causing a panic.

Some backends may send argument deltas before the tool call ID in edge cases.

**Impact:** Panic on specific backend response patterns.

**Fix:** Check `len(st.toolCalls) > 0` before accessing `st.toolCalls[idx]`.

---

### BUG-07: `GeminiStreamTranslator` Tool Call Maps Never Reset (Logic Bug)

**File:** `internal/translator/gemini_stream.go:59,62`
**Type:** State corruption

`toolCallNames` and `toolCallArgs` maps are populated during streaming but never cleared. While a new `GeminiStreamTranslator` is created per request, if the same translator instance were ever reused (which the current architecture prevents), stale tool call data would leak across requests.

More critically, within a single stream, if `FinishReason` is set on an intermediate chunk (some backends do this), the maps are iterated and emitted but never cleared, leading to duplicate tool call emissions.

**Impact:** Duplicate tool call events in Gemini streaming responses.

**Fix:** Clear the maps after iterating them in the `FinishReason` handler block (line 68-83).

---

### BUG-08: `Metrics` Maps Grow Unbounded (Slow Memory Leak)

**File:** `internal/handler/observability.go:120-151`
**Type:** Slow memory leak

The `requests`, `errors`, `durationCount`, and `durationSum` maps in `Metrics` grow with each unique `endpoint+method+status` combination. There is no rotation, compaction, or TTL mechanism.

In practice, the number of unique routes is bounded (~15 routes), so this is unlikely to be a problem in normal operation. However, if dynamic endpoint paths are introduced or if the endpoint normalization is bypassed, the maps could grow.

**Impact:** Negligible under current architecture; risk increases with route changes.

**Fix:** Consider periodic reset or use fixed-size arrays indexed by route ID.

---

## LOW Severity

### BUG-09: Health Monitor Goroutine Never Stopped (Goroutine Leak)

**File:** `internal/handler/handler.go:69`
**Type:** Goroutine leak

`healthMonitor.Start()` launches a background goroutine. The `Stop()` method exists but is never called during shutdown. The goroutine runs until the process exits.

**Impact:** One leaked goroutine per process lifetime. Negligible in practice since it dies with the process.

**Fix:** Call `healthMonitor.Stop()` in a shutdown hook or `defer` in `runServer`.

---

### BUG-10: Store Cleanup Goroutines Never Stopped (Goroutine Leak)

**File:** `internal/store/store.go:53`, `internal/store/file.go:50`
**Type:** Goroutine leak

Both `ResponseStore` (memory) and `FileStore` launch cleanup goroutines via `go s.cleanup(...)`. Neither store provides a way to stop these goroutines. `ResponseStore.Close()` is a no-op; `FileStore.Close()` only compacts the file.

**Impact:** Two leaked goroutines per process lifetime. Die with the process.

**Fix:** Add a `stop` channel or `context.Context` to the store constructors and respect it in the cleanup loop.

---

### BUG-11: File Store Temp File Left on Crash (Data Integrity)

**File:** `internal/store/file.go:208-239`
**Type:** Data integrity

`compactLocked` writes to `fs.path + ".tmp"` and renames. If the process crashes during the write (after `OpenFile` but before `Rename`), the `.tmp` file is left behind. On next startup, the original file is loaded correctly, but the orphaned `.tmp` file wastes disk space.

**Impact:** Minor disk space leak. No data loss due to atomic rename semantics.

**Fix:** On startup, check for and remove orphaned `.tmp` files.

---

### BUG-12: `parseSSEToOpenAIResponse` Double-Scans Body (Performance)

**File:** `internal/handler/sse_compat.go:27-152`
**Type:** Performance

The function scans the SSE body twice — once to find the last chunk (for ID/model) and once to accumulate content. The entire body is already in memory (from `io.ReadAll`), so this doubles processing time for large SSE responses.

**Impact:** ~2x CPU and memory churn for non-streaming SSE fallback responses.

**Fix:** Single-pass accumulation that tracks last chunk ID/model alongside content.

---

### BUG-13: `os.Rename` Across Filesystems Fails Silently (File Store)

**File:** `internal/store/file.go:239`
**Type:** Data integrity

`os.Rename(tmp, fs.path)` fails if `tmp` and `fs.path` are on different filesystems (e.g., if `/tmp` is a mount point). The error is returned but the caller in `cleanup` discards it with `_ = fs.compactLocked()`.

**Impact:** Silent failure of compaction; store file becomes stale.

**Fix:** Use `io.Copy` + `os.Remove` as a fallback when `Rename` fails, or ensure both paths are on the same filesystem.

---

## Recommendations Summary

### Immediate Fixes (HIGH)
1. Add `atomic.AddInt64` to `idCounter()` in `responses.go`
2. Add `io.LimitReader` to all `io.ReadAll(r.Body)` calls
3. Add `io.LimitReader` to all `io.ReadAll(resp.Body)` calls in non-streaming handlers
4. Convert all unsafe type assertions to two-value form in `request.go`
5. Start `RateLimiter.Cleanup()` in a background goroutine

### Short-term Fixes (MEDIUM)
6. Add bounds check before `st.toolCalls[idx]` in `responses_stream.go`
7. Clear `toolCallNames`/`toolCallArgs` after emitting tool calls in `gemini_stream.go`

### Nice-to-have (LOW)
8. Add shutdown hooks for `HealthMonitor` and store cleanup goroutines
9. Clean up orphaned `.tmp` files on startup
10. Optimize `parseSSEToOpenAIResponse` to single-pass
