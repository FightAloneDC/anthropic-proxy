# v2.8.0 — Authentication & Security

## Goals

1. API Key authentication — optional, on/off
2. HTTPS support with auto-redirect

## Design

### API Key Auth

Config (YAML):

```yaml
auth:
  enabled: false
  keys:
    - "sk-client-1"
    - "sk-client-2"
```

Behavior:
- `enabled: false` → skip validation, all requests allowed (default, safe for local dev)
- `enabled: true` → request must include valid key in `X-Api-Key` header (Anthropic) or `Authorization: Bearer <key>` (OpenAI/Gemini)
- If key invalid or missing → return `401 Unauthorized`

### HTTPS + Auto-Redirect

Config (YAML):

```yaml
server:
  tls:
    enabled: false
    cert_file: ""
    key_file: ""
    auto_generate: false
```

Behavior:
- `enabled: false` → plain HTTP (default)
- `enabled: true` → HTTPS + auto-redirect HTTP→HTTPS
- `auto_generate: true` → if cert_file/key_file not exist, auto-generate self-signed cert (10 year expiry, saved to `./certs/`)

## Files to Modify

| File | Change |
|------|--------|
| `internal/config/config.go` | Add `AuthConfig` and `TLSConfig` structs, load from YAML |
| `internal/handler/auth.go` | **New file** — auth middleware (key validation) |
| `cmd/anthropic-proxy/main.go` | Apply auth middleware, handle TLS, auto-redirect |
| `config.example.yaml` | Add auth and tls examples |
| `internal/handler/auth_test.go` | **New file** — tests for auth middleware |

## Implementation Details

### Auth Middleware

```
func (h *Handler) Auth(next http.HandlerFunc) http.HandlerFunc
```

- Check if `auth.enabled` is false → pass through
- Extract key from `X-Api-Key` or `Authorization: Bearer <key>`
- Validate against configured keys list
- Return 401 if invalid

### TLS + Redirect

In `main.go`:
- If `tls.enabled`:
  - If `auto_generate: true` and cert/key files don't exist → generate self-signed cert (10 year expiry, RSA 2048, saved to `./certs/`)
  - Start HTTPS server with cert/key
  - Start HTTP server that redirects to HTTPS
- If not enabled: plain HTTP (current behavior)

## Default Values

| Config | Default |
|--------|---------|
| `auth.enabled` | `false` |
| `auth.keys` | `[]` (empty) |
| `server.tls.enabled` | `false` |
| `server.tls.cert_file` | `""` |
| `server.tls.key_file` | `""` |
| `server.tls.auto_generate` | `false` |

## Scope Exclusions

- Request sanitization (log redaction) — deferred to later
- Per-client permissions (model restrictions per key) — not in this phase

## Verification

1. Run with `auth.enabled: false` → all requests work without key
2. Run with `auth.enabled: true` + keys → valid key passes, invalid returns 401
3. Run with `tls.enabled: false` → HTTP works (current behavior)
4. Run with `tls.enabled: true` + cert/key → HTTPS works, HTTP redirects
5. Existing tests pass (no regression)
