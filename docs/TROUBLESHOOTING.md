# Troubleshooting

## Proxy vs client traffic

| Symptom | Likely cause |
|---------|----------------|
| Continuous `POST /openai/v1/responses` while you “idle” | **Client agent loop** (Codex turn still running), not proxy cron |
| Traffic resumes right after proxy restart | Client reconnected; process still alive |
| Only stops when Codex is cancelled/killed | Confirms client-driven loop |

Proxy never schedules model calls by itself.

## Codex / custom tools

| Symptom | Check |
|---------|--------|
| Codex ignores or fails on `exec` | Response must use `custom_tool_call` + `input`, not only `function_call` + `arguments` |
| No custom rewrite | Log should show `customTools=map[exec:true]` on forward |
| Missing stream events | Expect `response.custom_tool_call_input.delta` / `.done` |
| Tool args look double-encoded | `extractCustomToolInput` should unwrap `{"input":"..."}` |

See [RESPONSES_API.md](./RESPONSES_API.md).

## Thinking / tags

| Symptom | Check |
|---------|--------|
| Raw `<think>` in client UI | Stream thinking normalizer path; backend tag style |
| Want no thinking | `proxy.skip_thinking: true` |
| Split tags across chunks | Stateful stream parser — fixed in current code; retest if regression |

## Multi-turn / store

| Symptom | Check |
|---------|--------|
| Context lost next turn | `previous_response_id` + store backend; TTL expiry; max entries eviction |
| File store empty | `store_backend: file`, path writable, process CWD |

## Backend / failover

| Symptom | Check |
|---------|--------|
| Always same backend fails | Circuit breaker open; health check; model glob mismatch after mapping |
| No failover on 401/403 | Default failover codes may omit them — extend `failover_status_codes` intentionally |
| Stream errors not failed over | Add `failover_stream_error_patterns` for provider-specific messages |
| Extra fields rejected by provider | `backends[].overrides.drop_fields` |

## Auth / rate limit

| Symptom | Check |
|---------|--------|
| 401 from proxy | `auth.enabled` and client key vs `auth.keys` |
| 429 from proxy | `rate_limit_*` settings |

## Local debug

1. `proxy.debug: true` and/or `log_level: debug`.  
2. Confirm model mapping line in logs (`Model mapping: X → Y`).  
3. Confirm upstream URL and status.  
4. For SSE, log event types (`response.*` / Anthropic events).  
5. Do **not** kill production `:8006` casually; use another port for test binaries.

## Build / test

```bash
go test ./...
go build -o build/anthropic-proxy-linux-amd64 ./cmd/anthropic-proxy/
```

If tests fail after a Responses change, update call sites for `TranslateResponsesRequest` triple return and stream constructor `customTools` argument.
