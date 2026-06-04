# Roadmap

Development plan for anthropic-proxy — features, improvements, and milestones.

## Current State (v2.2.0)

- ✅ Anthropic Messages API → Chat Completions translation
- ✅ OpenAI Responses API → Chat Completions translation
- ✅ Direct forward (Chat Completions)
- ✅ Streaming (SSE) for all endpoints
- ✅ Tool calling (bidirectional)
- ✅ Reasoning/thinking blocks
- ✅ Model mapping
- ✅ Previous response ID (in-memory store)
- ✅ CLI subcommands (start/stop/restart/status)
- ✅ Cross-platform builds (5 OS × multiple arch)
- ✅ Debug logging

---

## v2.3.0 — Gemini API Support

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
- `safetySettings` — pass through or map

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

## v2.4.0 — Multi-Modal Proxy

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

## v2.5.0 — Hardening & Observability

**Priority: Medium**

### Request Validation
- Validate required fields before translation (model, input, messages)
- Return proper error responses for malformed requests
- Validate max_tokens ranges, temperature bounds

### Structured Logging
- Replace raw `log.Printf` with structured logging (JSON format option)
- Request ID propagation through entire request lifecycle
- Log levels: debug, info, warn, error

### Metrics
- Request count per endpoint
- Latency histograms (p50, p95, p99)
- Error rate by type
- Active connections gauge
- Optional Prometheus `/metrics` endpoint

### Health Check
- `GET /health` endpoint
- Backend connectivity check
- Store status (entries count, memory usage)

---

## v2.6.0 — Persistence & Reliability

**Priority: High**

### Persistent Response Store
- Option to use file-based or Redis store for `previous_response_id`
- Survives server restarts
- Config: `store_backend: memory|file|redis`

### Backend Health Monitoring
- Periodic health checks to backend
- Circuit breaker pattern — stop forwarding if backend is down
- Auto-recovery when backend comes back

### Retry Logic
- Configurable retry on backend errors (5xx, timeout)
- Exponential backoff
- Max retry count

### Rate Limiting
- Optional rate limiting on proxy side
- Per-IP or per-API-key limits
- Configurable via YAML

---

## v2.7.0 — Multi-Backend & Load Balancing

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
