# Configuration

Primary file: `config.yaml` (see `config.example.yaml`). Load path is CLI-dependent; default is project / working directory `config.yaml`.

## `server`

| Key | Meaning |
|-----|---------|
| `port` | Listen port (default often 8006) |
| `fg` | Foreground if true (daemon otherwise, CLI-dependent) |
| `log` | Optional log file path |
| `tls.enabled` | Serve HTTPS |
| `tls.cert_file` / `tls.key_file` | Certificate paths |
| `tls.auto_generate` | Self-signed cert if missing |

**Ops note:** production proxy on port **8006** should not be killed casually during tests; use another port for experiments.

## `auth`

| Key | Meaning |
|-----|---------|
| `enabled` | If true, require a valid client key |
| `keys` | Allowed API keys (`x-api-key` or Bearer) |

## `backend` (single)

| Key | Meaning |
|-----|---------|
| `url` | OpenAI-compatible base URL (…/v1) |
| `api_key` | Upstream key |

## `backends` (multi, optional)

List of:

| Key | Meaning |
|-----|---------|
| `name` | Label |
| `url` / `api_key` | Upstream |
| `models` | Glob patterns for **mapped** model names |
| `weight` / `priority` | Load-balance / preference |
| `enabled` | Soft disable |
| `overrides.drop_fields` | JSON field names stripped from outbound Chat Completions body |

When `backends` is set, routing uses the backend-facing model after mapping.

## `proxy`

| Key | Meaning |
|-----|---------|
| `skip_thinking` | Drop thinking/reasoning from client output |
| `debug` | Verbose logging (SSE dumps, etc.) |
| `log_format` | `text` or `json` |
| `log_level` | `debug` / `info` / `warn` / `error` |
| `metrics_enabled` | Metrics endpoint (default true) |
| `health_backend_check` | Health handler probes backend |
| `store_backend` | `memory` or `file` (Responses history) |
| `store_file` | Path for file store |
| `store_ttl` | Entry TTL seconds |
| `store_max_entries` | Cap stored responses |
| `backend_health_*` | Active health interval/timeout |
| `circuit_breaker_*` | Failure threshold + cooldown |
| `retry_*` | Attempts + backoff ms |
| `rate_limit_*` | Per-client rate limit |
| `load_balance_strategy` | `round_robin` / `weighted` / `least_connections` |
| `failover_enabled` | Try next matching backend |
| `failover_max_backends` | 0 = all matching |
| `failover_status_codes` | e.g. 429, 502, 503, 504 |
| `failover_stream_error_patterns` | Substrings that trigger stream failover |

## `models`

```yaml
models:
  - from: "client-model-name"
    to: "backend-model-name"
```

## CLI (typical)

```bash
./build/anthropic-proxy-linux-amd64 start -fg
# or make / go run — see Makefile and README
```

Binary and flags evolve; `start -fg` is the common local path.

## Minimal single-backend example

```yaml
server:
  port: 8006
auth:
  enabled: false
backend:
  url: "http://127.0.0.1:8003/v1"
  api_key: "sk-..."
proxy:
  skip_thinking: false
  debug: false
  store_backend: file
  store_file: "./data/responses.jsonl"
models:
  - from: "gpt-5.6-terra"
    to: "gcli/grok-4.5"
```
