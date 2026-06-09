# Roadmap

Development plan for anthropic-proxy — features, improvements, and milestones.

## Current State (v2.7.0)

- ✅ Anthropic Messages API → Chat Completions translation
- ✅ OpenAI Responses API → Chat Completions translation
- ✅ Gemini generateContent API → Chat Completions translation
- ✅ Gemini embedContent API → OpenAI Embeddings translation
- ✅ OpenAI-compatible multi-modal direct forwarding
- ✅ Request validation for translated JSON endpoints
- ✅ Request ID propagation
- ✅ Structured logging and optional log file output
- ✅ Health check and Prometheus-style metrics
- ✅ Direct forward (Chat Completions)
- ✅ Streaming (SSE) for all translated endpoints
- ✅ Tool calling (bidirectional)
- ✅ Reasoning/thinking blocks
- ✅ Model mapping
- ✅ Multiple backend configuration
- ✅ Model-based backend routing
- ✅ Load balancing and safe failover
- ✅ Public `/anthropic/v1/models` and `/openai/v1/models` compatibility
- ✅ Previous response ID (in-memory store)
- ✅ CLI subcommands (start/stop/restart/status)
- ✅ Cross-platform builds (5 OS × multiple arch)
- ✅ Debug logging

---

## v2.3.0 — Gemini API Support ✅ Completed

**Priority: High**

### Gemini Messages Endpoint
- `POST /gemini/v1beta/models/{model}:generateContent` — Gemini generateContent API
- Translate Gemini request format → Chat Completions
- Translate Chat Completions response → Gemini format
- Support `gemini-2.0-flash`, `gemini-2.5-pro`, etc.

### Gemini Streaming
- `POST /gemini/v1beta/models/{model}:streamGenerateContent` — SSE streaming
- Gemini streaming format → Chat Completions SSE → Gemini SSE events

### Gemini Specific Features
- `systemInstruction` → system message
- `contents[]` → messages[]
- `tools[]` (Gemini function declarations) → Chat Completions tools
- `generationConfig` (temperature, topP, topK, maxOutputTokens) → Chat Completions params
- `safetySettings` — accepted but ignored because Chat Completions has no direct equivalent

### Route Structure (Updated)
```
/anthropic/v1/messages        → translate to chat/completions
/anthropic/v1/models          → direct forward

/openai/v1/responses          → translate to chat/completions
/openai/v1/chat/completions   → direct forward
/openai/v1/models             → direct forward

/gemini/v1beta/models/{model}:generateContent        → translate to chat/completions
/gemini/v1beta/models/{model}:streamGenerateContent  → translate to chat/completions
```

---

## v2.4.0 — Multi-Modal Proxy ✅ Completed

**Priority: High**

### Embeddings
- `POST /openai/v1/embeddings` → forward to backend
- `POST /gemini/v1beta/models/{model}:embedContent` → translate to embeddings
- Support text-embedding-3-small, text-embedding-3-large, etc.

### Reranker
- `POST /openai/v1/rerank` → forward to backend (Cohere-compatible)
- Cross-encoder reranking support

### Text-to-Speech (TTS)
- `POST /openai/v1/audio/speech` → forward to backend
- Support tts-1, tts-1-hd models
- Stream audio response

### Speech-to-Text (STT)
- `POST /openai/v1/audio/transcriptions` → forward to backend
- Support whisper-1, whisper-large models
- File upload (multipart/form-data)

### Image Generation
- `POST /openai/v1/images/generations` → forward to backend
- Support DALL-E compatible endpoints
- Return image URLs or base64

### Image-to-Text (Vision)
- Already supported via image content blocks in messages
- Support `input_image` in Responses API
- Support base64 and URL image sources

### Web Search
- `POST /openai/v1/search` → forward to backend (if supported)
- Or integrate with external search APIs

### Route Structure (Updated)
```
# LLM endpoints (translated)
/anthropic/v1/messages
/openai/v1/responses
/gemini/v1beta/models/{model}:generateContent

# LLM endpoints (direct forward)
/openai/v1/chat/completions

# Multi-modal endpoints (direct forward)
/openai/v1/embeddings
/openai/v1/audio/speech
/openai/v1/audio/transcriptions
/openai/v1/images/generations
/openai/v1/rerank

# Utility endpoints
/openai/v1/models
/health
/metrics
```

---

## v2.5.0 — Hardening & Observability ✅ Completed

**Priority: Medium**

### Request Validation
- Validate required fields before translation (model, input, messages)
- Return proper API-specific error responses for malformed requests
- Validate token and sampling parameter bounds

### Structured Logging
- Request ID propagation through client responses and backend requests
- Log levels: debug, info, warn, error
- Text and JSON log formats
- Optional log file output via `server.log` or `-log-file`

### Metrics
- Request count per endpoint
- Request duration summaries
- Error count by endpoint/status
- Active request gauge
- Prometheus-style `GET /metrics` endpoint

### Health Check
- `GET /health` endpoint
- Backend configuration status
- Store status (entries, max entries, TTL)
- API keys are not exposed in health responses

---

## v2.6.0 — Persistence & Reliability ✅ Completed

**Priority: High**

### Persistent Response Store
- Configurable memory or file-backed store for `previous_response_id`
- File store survives server restarts
- Config: `store_backend: memory|file`

### Backend Health Monitoring
- Optional periodic health checks to backend
- Circuit breaker pattern — stop forwarding if backend is down
- Auto-recovery when backend comes back

### Retry Logic
- Configurable retry on backend errors (429, 502, 503, 504, timeout)
- Exponential backoff
- Max retry count

### Rate Limiting
- Optional rate limiting on proxy side
- Per-IP limits
- Configurable via YAML

### Compatibility Guardrails
- Streaming wrappers preserve `http.Flusher`
- Chat Completions model mapping remains active
- `*/models` endpoints include configured model mapping aliases

---

## v2.7.0 — Multi-Backend & Load Balancing ✅ Completed

**Priority: Medium**

### Multiple Backend Support
- Configure multiple backends in YAML
- Route by model name (e.g., `mimo-*` → backend A, `deepseek-*` → backend B)
- Failover: if primary backend fails, try secondary

```yaml
backends:
  - name: primary
    url: "https://backend-a.com/v1"
    api_key: "key-a"
    models: ["mimo-*"]
  - name: secondary
    url: "https://backend-b.com/v1"
    api_key: "key-b"
    models: ["deepseek-*"]
```

### Load Balancing
- Round-robin across backends
- Weighted distribution
- Least-connections strategy

### Backend-specific Translation
- Different backends may need slightly different request formats
- Per-backend translation overrides (e.g., some backends support `top_k`, some don't)

---

## v2.8.0 — Authentication & Security

**Priority: Medium**

### API Key Authentication
- Optional API key requirement for incoming requests
- Per-client API keys with different permissions
- Validate `X-Api-Key` against configured keys

```yaml
auth:
  enabled: true
  keys:
    - key: "sk-client-1"
      models: ["*"]
    - key: "sk-client-2"
      models: ["claude-*"]
```

### TLS Support
- HTTPS termination in the proxy
- Configurable cert/key paths
- Auto-redirect HTTP → HTTPS

### Request Sanitization
- Strip sensitive headers before logging
- Sanitize request bodies in debug logs
- Configurable log redaction patterns

---

## v2.9.0 — Advanced Translation

**Priority: Medium**

### Extended Thinking / Budget Tokens
- Forward `thinking.budget_tokens` to backend reasoning config
- Map `reasoning.effort` (low/medium/high/xhigh) to backend equivalents
- Thinking block summarization for long reasoning

### Response Format / Structured Output
- Forward `response_format` / `text.format` to backend
- JSON Schema validation
- `json_object` and `json_schema` modes

### Image & File Support
- Forward `input_image` in Responses API
- Forward `input_file` (PDF, etc.) if backend supports
- Base64 and URL image sources

### Prompt Caching
- Forward `cache_control` markers to backend
- Map cache token usage in responses
- Anthropic-specific cache headers

---

## v3.0.0 — Plugin Architecture

**Priority: Low (Long-term)**

### Middleware System
- Pluggable middleware chain (auth, logging, rate limiting, caching)
- Custom middleware via Go plugins or config-driven

```yaml
middleware:
  - name: auth
    config: { ... }
  - name: rate_limit
    config: { ... }
  - name: cache
    config: { ... }
```

### Custom Translators
- Support custom translation logic via config or external scripts
- For backends with non-standard API formats

### Web UI
- Simple dashboard for monitoring
- Request/response viewer
- Configuration editor
- Live log streaming

### gRPC Support
- Backend communication via gRPC
- Useful for high-performance internal services

---

## Ongoing — Documentation & Community

### Documentation
- [ ] API reference (auto-generated from types)
- [ ] Deployment guide (Docker, systemd, Kubernetes)
- [ ] Contributing guide
- [ ] Changelog

### Testing
- [ ] Unit tests for all translators
- [ ] Integration tests with mock backends
- [ ] Streaming test suite
- [ ] Tool calling test suite
- [ ] Edge case tests (empty content, null fields, malformed input)

### CI/CD
- [ ] GitHub Actions for build/test
- [ ] Automated cross-platform builds on release
- [ ] Docker image build and publish
- [ ] Dependabot for dependency updates

### Distribution
- [ ] Docker image (multi-arch)
- [ ] Homebrew formula
- [ ] AUR package
- [ ] Systemd service file
- [ ] GitHub Releases with pre-built binaries

---

## Milestone Summary

| Version | Theme | Key Features |
|---------|-------|--------------|
| v2.3.0 | Gemini API | Gemini generateContent, streaming, function declarations |
| v2.4.0 | Multi-Modal | Embeddings, reranker, TTS, STT, image generation, vision |
| v2.5.0 | Hardening | Validation, structured logging, metrics, health check |
| v2.6.0 | Reliability | Persistent store, circuit breaker, retry, rate limiting |
| v2.7.0 | Multi-Backend | Multiple backends, load balancing, failover |
| v2.8.0 | Security | API key auth, TLS, request sanitization |
| v2.9.0 | Advanced Translation | Extended thinking, structured output, image/file support |
| v3.0.0 | Plugin Architecture | Middleware system, custom translators, web UI |

---

## How to Contribute

Pick any item from the roadmap and open a PR. Issues and feature requests welcome on GitHub.

Repository: https://github.com/FightAloneDC/anthropic-proxy
