# anthropic-proxy 🔄

Universal API translator proxy. Translates between **Anthropic Messages API**, **OpenAI Responses API**, **Gemini generateContent API**, and **OpenAI Chat Completions API** — allowing Anthropic, OpenAI Responses, Gemini, or OpenAI Chat clients to connect to any OpenAI-compatible backend.

```
Anthropic SDK        →  /anthropic/v1/messages                       ─┐
OpenAI Responses SDK →  /openai/v1/responses                          ├→  translate  →  backend /v1/chat/completions
Gemini SDK/API       →  /gemini/v1beta/models/{model}:generateContent ─┘
OpenAI Chat SDK      →  /openai/v1/chat/completions                   ──  direct forward
```

## Features

- **Anthropic Messages API** — full translation to/from Chat Completions
- **OpenAI Responses API** — full translation to/from Chat Completions
- **Gemini generateContent API** — translation to/from Chat Completions
- **Gemini embedContent API** — translation to/from OpenAI Embeddings
- **Multi-modal forwarding** — embeddings, rerank, speech, transcription, and image generation
- **Direct forward** — `/openai/v1/chat/completions` pass-through
- **Streaming** — SSE streaming for all endpoints
- **Tool calling** — bidirectional function/tool translation
- **Reasoning/thinking blocks** — maps `reasoning` ↔ `thinking` blocks
- **Model mapping** — remap model names to backend equivalents
- **`previous_response_id`** — memory or file-backed response store for conversation continuity
- **Persistence & reliability** — optional file store, retry, circuit breaker, health state, and rate limiting
- **Cache token forwarding** — maps cache hit/miss tokens between formats
- **Daemon mode** — run as background process with start/stop/restart/status

## Quick Start

```bash
# Create config
cp config.example.yaml config.yaml
# Edit config.yaml with your backend URL and API key

# Build for current platform
make

# Run in foreground
./build/anthropic-proxy-linux-amd64 start -fg

# Run as daemon
./build/anthropic-proxy-linux-amd64 start
```

## Build

```bash
# Build for current platform (auto-detects OS/arch)
make

# Build for current platform and run
make run

# Build for ALL platforms (Linux, macOS, FreeBSD, OpenBSD, Windows)
make all

# Build for specific platform/arch
make build-linux-amd64
make build-linux-arm64
make build-darwin-arm64
make build-windows-amd64

# Clean build artifacts
make clean
```

### Supported Platforms

| OS | Architectures |
|----|---------------|
| Linux | amd64, arm64, arm, 386 |
| macOS (Darwin) | amd64, arm64 |
| FreeBSD | amd64, arm64, arm, 386 |
| OpenBSD | amd64, arm, 386 |
| Windows | amd64, arm64, 386 |

Build output: `build/anthropic-proxy-{os}-{arch}` (Windows gets `.exe` suffix)

Build flags: `CGO_ENABLED=0`, static binary, stripped symbols (`-s -w`), trimmed paths (`-trimpath`)

## CLI Commands

```
Usage: anthropic-proxy <command> [flags]

Commands:
  start       start proxy as daemon (default, use -fg for foreground)
  stop        stop running daemon
  restart     restart daemon
  status      show daemon status

Flags (for start/restart):
  -config string    path to config file (default "config.yaml")
  -port int         listen port (overrides config)
  -url string       backend URL (overrides config)
  -key string       backend API key (overrides config)
  -skip-thinking    skip thinking blocks (overrides config)
  -debug            enable debug logging (overrides config)
  -log-file string  write logs to this file path (overrides config)
  -fg               run in foreground (not as daemon)
```

## Configuration

Priority: **CLI flags > YAML config > environment variables**

### YAML config (recommended)

```yaml
server:
  port: 8006
  # fg: false  # Run in foreground (default: daemon mode)
  # log: "./debug-output/proxy.log"  # Optional log file path

backend:
  url: "https://your-openai-compatible-backend.com/v1"
  api_key: "your-api-key"

proxy:
  skip_thinking: false     # Skip reasoning/thinking blocks
  debug: false             # Enable debug logging
  log_format: "text"        # text or json
  log_level: "info"         # debug, info, warn, error
  metrics_enabled: true
  health_backend_check: false
  store_backend: "memory"   # memory or file
  store_file: "./data/responses.jsonl"
  store_ttl: 3600          # Response store TTL in seconds (default: 3600)
  store_max_entries: 1000  # Max stored responses (default: 1000)
  backend_health_enabled: false
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

# Model mapping: client model → backend model
models:
  - from: "claude-opus-4-8"
    to: "your-backend-model"
  - from: "claude-sonnet-4-6"
    to: "your-backend-model"
```

### Environment variables

```bash
PORT=8006
OPENAI_BASE_URL=https://your-backend.com/v1
OPENAI_API_KEY=your-api-key
MODEL_MAP=claude-opus-4-8:your-model,claude-sonnet-4-6:your-model
```

## Endpoints

| Endpoint | Method | Description |
|----------|--------|-------------|
| `/anthropic/v1/messages` | POST | Anthropic Messages API (translated) |
| `/anthropic/v1/models` | GET | List models (forwarded to backend) |
| `/openai/v1/responses` | POST | OpenAI Responses API (translated) |
| `/openai/v1/chat/completions` | POST | OpenAI Chat Completions (direct forward) |
| `/openai/v1/models` | GET | List models (forwarded to backend) |
| `/openai/v1/embeddings` | POST | OpenAI Embeddings (direct forward) |
| `/openai/v1/rerank` | POST | Rerank API (direct forward) |
| `/openai/v1/audio/speech` | POST | Text-to-speech (direct forward) |
| `/openai/v1/audio/transcriptions` | POST | Speech-to-text (direct forward) |
| `/openai/v1/images/generations` | POST | Image generation (direct forward) |
| `/gemini/v1beta/models/{model}:generateContent` | POST | Gemini generateContent API (translated) |
| `/gemini/v1beta/models/{model}:streamGenerateContent` | POST | Gemini streaming generateContent API (translated) |
| `/gemini/v1beta/models/{model}:embedContent` | POST | Gemini embedding API (translated) |
| `/health` | GET | Health check |
| `/metrics` | GET | Prometheus-style metrics |

### SDK Configuration

**Anthropic SDK:**
```python
import anthropic
client = anthropic.Anthropic(
    base_url="http://localhost:8006/anthropic",
    api_key="dummy"  # proxy uses its own configured key
)
```

**OpenAI SDK (Responses API):**
```python
import openai
client = openai.OpenAI(
    base_url="http://localhost:8006/openai",
    api_key="dummy"
)
response = client.responses.create(
    model="your-model",
    input="Hello!"
)
```

**OpenAI SDK (Chat Completions):**
```python
import openai
client = openai.OpenAI(
    base_url="http://localhost:8006/openai",
    api_key="dummy"
)
response = client.chat.completions.create(
    model="your-model",
    messages=[{"role": "user", "content": "Hello!"}]
)
```

**Gemini API:** use `http://localhost:8006/gemini` as the API root. The proxy ignores the incoming client key and uses the configured backend key.

## API Examples

### Anthropic Messages — Non-streaming

```bash
curl http://localhost:8006/anthropic/v1/messages \
  -H "Content-Type: application/json" \
  -H "x-api-key: sk-dummy" \
  -H "anthropic-version: 2023-06-01" \
  -d '{
    "model": "claude-sonnet-4-6",
    "max_tokens": 200,
    "messages": [{"role": "user", "content": "Hello!"}]
  }'
```

Response:
```json
{
  "id": "gen-xxx",
  "type": "message",
  "role": "assistant",
  "model": "your-model",
  "content": [{"type": "text", "text": "Hello! How can I help?"}],
  "stop_reason": "end_turn",
  "usage": {"input_tokens": 10, "output_tokens": 15}
}
```

### Anthropic Messages — Streaming

```bash
curl -N http://localhost:8006/anthropic/v1/messages \
  -H "Content-Type: application/json" \
  -H "x-api-key: sk-dummy" \
  -H "anthropic-version: 2023-06-01" \
  -d '{
    "model": "claude-sonnet-4-6",
    "max_tokens": 200,
    "stream": true,
    "messages": [{"role": "user", "content": "Hello!"}]
  }'
```

SSE events:
```
event: message_start
data: {"type":"message_start","message":{...}}

event: content_block_start
data: {"type":"content_block_start","index":0,"content_block":{"type":"text"}}

event: content_block_delta
data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"Hello!"}}

event: content_block_stop
data: {"type":"content_block_stop","index":0}

event: message_delta
data: {"type":"message_delta","delta":{"stop_reason":"end_turn"}}

event: message_stop
data: {"type":"message_stop"}
```

### OpenAI Responses — Non-streaming

```bash
curl http://localhost:8006/openai/v1/responses \
  -H "Content-Type: application/json" \
  -d '{
    "model": "your-model",
    "input": "Hello!",
    "max_output_tokens": 200
  }'
```

Response:
```json
{
  "id": "resp_1",
  "object": "response",
  "created_at": 1780568046,
  "model": "your-model",
  "status": "completed",
  "output": [
    {
      "type": "message",
      "id": "msg_resp_1",
      "role": "assistant",
      "content": [{"type": "output_text", "text": "Hello! How can I help?"}],
      "status": "completed"
    }
  ],
  "usage": {"input_tokens": 10, "output_tokens": 15, "total_tokens": 25}
}
```

### OpenAI Responses — Streaming

```bash
curl -N http://localhost:8006/openai/v1/responses \
  -H "Content-Type: application/json" \
  -d '{
    "model": "your-model",
    "input": "Hello!",
    "stream": true,
    "max_output_tokens": 200
  }'
```

SSE events:
```
event: response.created
data: {"type":"response.created","response":{...}}

event: response.output_item.added
data: {"type":"response.output_item.added","index":0,"item":{...}}

event: response.content_part.added
data: {"type":"response.content_part.added","output_index":0,"content_index":0,"part":{"type":"output_text"}}

event: response.output_text.delta
data: {"type":"response.output_text.delta","output_index":0,"content_index":0,"delta":"Hello!"}

event: response.output_text.done
data: {"type":"response.output_text.done","output_index":0,"content_index":0,"text":"Hello!"}

event: response.content_part.done
data: {"type":"response.content_part.done",...}

event: response.output_item.done
data: {"type":"response.output_item.done",...}

event: response.completed
data: {"type":"response.completed","response":{...}}
```

### With Tools

**Anthropic format:**
```bash
curl http://localhost:8006/anthropic/v1/messages \
  -H "Content-Type: application/json" \
  -H "x-api-key: sk-dummy" \
  -H "anthropic-version: 2023-06-01" \
  -d '{
    "model": "claude-sonnet-4-6",
    "max_tokens": 200,
    "tools": [{
      "name": "get_weather",
      "description": "Get weather for a location",
      "input_schema": {
        "type": "object",
        "properties": {"location": {"type": "string"}},
        "required": ["location"]
      }
    }],
    "messages": [{"role": "user", "content": "Weather in Jakarta?"}]
  }'
```

**Responses format:**
```bash
curl http://localhost:8006/openai/v1/responses \
  -H "Content-Type: application/json" \
  -d '{
    "model": "your-model",
    "input": "Weather in Jakarta?",
    "tools": [{
      "type": "function",
      "name": "get_weather",
      "description": "Get weather for a location",
      "parameters": {
        "type": "object",
        "properties": {"location": {"type": "string"}},
        "required": ["location"]
      }
    }],
    "max_output_tokens": 200
  }'
```

### Previous Response ID (Conversation Continuity)

```bash
# First request
curl http://localhost:8006/openai/v1/responses \
  -H "Content-Type: application/json" \
  -d '{"model": "your-model", "input": "My name is Alex"}'
# Returns: {"id": "resp_1", ...}

# Follow-up with context
curl http://localhost:8006/openai/v1/responses \
  -H "Content-Type: application/json" \
  -d '{
    "model": "your-model",
    "input": "What is my name?",
    "previous_response_id": "resp_1"
  }'
# Returns: {"id": "resp_2", "output": [{"type":"message","content":[{"type":"output_text","text":"Your name is Alex."}]}]}
```

### Reasoning / Thinking Blocks

For reasoning models (MiMo, DeepSeek R1, o-series), the proxy automatically maps:

**Anthropic format** — `thinking` blocks:
```
event: content_block_start
data: {"type":"content_block_start","index":0,"content_block":{"type":"thinking"}}

event: content_block_delta
data: {"type":"content_block_delta","index":0,"delta":{"type":"thinking_delta","thinking":"Let me think..."}}
```

**Responses format** — `reasoning_text` events:
```
event: response.output_item.added
data: {"type":"response.output_item.added","index":0,"item":{"type":"message","role":"assistant"}}

event: response.reasoning_text.delta
data: {"type":"response.reasoning_text.delta","output_index":0,"content_index":0,"delta":"Let me think..."}
```

### Gemini generateContent — Non-streaming

```bash
curl http://localhost:8006/gemini/v1beta/models/gemini-2.5-pro:generateContent \
  -H "Content-Type: application/json" \
  -d '{
    "contents": [
      {"role": "user", "parts": [{"text": "Hello!"}]}
    ],
    "generationConfig": {"maxOutputTokens": 200}
  }'
```

### Gemini generateContent — Streaming

```bash
curl -N http://localhost:8006/gemini/v1beta/models/gemini-2.5-pro:streamGenerateContent \
  -H "Content-Type: application/json" \
  -d '{
    "contents": [
      {"role": "user", "parts": [{"text": "Hello!"}]}
    ],
    "generationConfig": {"maxOutputTokens": 200}
  }'
```

### Gemini embedContent

```bash
curl http://localhost:8006/gemini/v1beta/models/text-embedding-3-small:embedContent \
  -H "Content-Type: application/json" \
  -d '{
    "content": {"parts": [{"text": "Hello!"}]},
    "outputDimensionality": 768
  }'
```

### OpenAI Embeddings — Direct forward

```bash
curl http://localhost:8006/openai/v1/embeddings \
  -H "Content-Type: application/json" \
  -d '{"model":"text-embedding-3-small","input":"Hello!"}'
```

### OpenAI Audio Speech — Direct forward

```bash
curl http://localhost:8006/openai/v1/audio/speech \
  -H "Content-Type: application/json" \
  -d '{"model":"tts-1","voice":"alloy","input":"Hello!"}' \
  --output speech.mp3
```

### OpenAI Audio Transcriptions — Direct forward

```bash
curl http://localhost:8006/openai/v1/audio/transcriptions \
  -F model=whisper-1 \
  -F file=@audio.mp3
```

### OpenAI Image Generation — Direct forward

```bash
curl http://localhost:8006/openai/v1/images/generations \
  -H "Content-Type: application/json" \
  -d '{"model":"dall-e-3","prompt":"a small red robot","size":"1024x1024"}'
```

## Translation Mapping

### Anthropic ↔ Chat Completions

**Request:**

| Anthropic | Chat Completions |
|-----------|------------------|
| `system` (string/array) | `messages[0]` with `role: "system"` |
| `messages[].content` blocks | `messages[].content` string/parts |
| `tool_use` blocks | `tool_calls` array |
| `tool_result` blocks | `role: "tool"` messages |
| `tool_choice` | `tool_choice` (auto/any/tool/none) |
| `stop_sequences` | `stop` |
| `tools[].input_schema` | `tools[].function.parameters` |
| `top_k` | `top_k` |
| `metadata.user_id` | `user` |
| `thinking.budget_tokens` | adjusts `max_tokens` |

**Response:**

| Chat Completions | Anthropic |
|------------------|-----------|
| `choices[0].message.content` | `content[{type:"text"}]` |
| `choices[0].message.tool_calls` | `content[{type:"tool_use"}]` |
| `finish_reason: "stop"` | `stop_reason: "end_turn"` |
| `finish_reason: "length"` | `stop_reason: "max_tokens"` |
| `finish_reason: "tool_calls"` | `stop_reason: "tool_use"` |
| `reasoning` (streaming) | `thinking` blocks |
| `usage.prompt_tokens` | `usage.input_tokens` |
| `usage.completion_tokens` | `usage.output_tokens` |
| `usage.prompt_cache_hit_tokens` | `usage.cache_read_input_tokens` |

### Responses ↔ Chat Completions

**Request:**

| Responses | Chat Completions |
|-----------|------------------|
| `input` (string) | `messages[{role:"user", content:string}]` |
| `input` (items) | `messages[]` with role/content mapping |
| `instructions` | `messages[0]` with `role: "system"` |
| `max_output_tokens` | `max_tokens` |
| `tools[]` (flat) | `tools[]` (nested `function` wrapper) |
| `tool_choice` | `tool_choice` |
| `text.format` | `response_format` |
| `previous_response_id` | prepends stored messages |

**Response:**

| Chat Completions | Responses |
|------------------|-----------|
| `choices[0].message.content` | `output[{type:"message", content:[{type:"output_text"}]}]` |
| `choices[0].message.tool_calls` | `output[{type:"function_call"}]` |
| `finish_reason: "stop"` | `status: "completed"` |
| `finish_reason: "length"` | `status: "incomplete"` |
| `usage.prompt_tokens` | `usage.input_tokens` |
| `usage.completion_tokens` | `usage.output_tokens` |

### Gemini ↔ Chat Completions

**Request:**

| Gemini | Chat Completions |
|--------|------------------|
| URL `{model}` | `model` |
| `systemInstruction.parts[].text` | `messages[0]` with `role: "system"` |
| `contents[].role: "user"` | `role: "user"` |
| `contents[].role: "model"` | `role: "assistant"` |
| `parts[].text` | text content |
| `parts[].inlineData` | `image_url` data URI |
| `parts[].fileData.fileUri` | `image_url` URL |
| `tools[].functionDeclarations[]` | `tools[].function` |
| `generationConfig.maxOutputTokens` | `max_tokens` |
| `generationConfig.temperature` | `temperature` |
| `generationConfig.topP` | `top_p` |
| `generationConfig.topK` | `top_k` |
| `safetySettings` | accepted but ignored |

**Response:**

| Chat Completions | Gemini |
|------------------|--------|
| `choices[].message.content` | `candidates[].content.parts[].text` |
| `choices[].message.tool_calls` | `parts[].functionCall` |
| `finish_reason: "stop"` | `finishReason: "STOP"` |
| `finish_reason: "length"` | `finishReason: "MAX_TOKENS"` |
| `usage.prompt_tokens` | `usageMetadata.promptTokenCount` |
| `usage.completion_tokens` | `usageMetadata.candidatesTokenCount` |

### Gemini embedContent ↔ OpenAI Embeddings

| Gemini | OpenAI Embeddings |
|--------|-------------------|
| URL `{model}` | `model` |
| `content.parts[].text` | `input` string |
| multiple text parts | newline-joined `input` |
| `outputDimensionality` | `dimensions` |
| `taskType` | accepted but ignored |
| `title` | accepted but ignored |

| OpenAI Embeddings | Gemini |
|-------------------|--------|
| `data[0].embedding` | `embedding.values` |

## Project Structure

```
anthropic-proxy/
├── cmd/anthropic-proxy/
│   └── main.go              # Entry point, CLI, route registration
├── internal/
│   ├── config/
│   │   └── config.go        # YAML + env + CLI config loading
│   ├── daemon/
│   │   ├── daemon.go        # Daemon management (state dir, log, PID)
│   │   ├── daemon_unix.go   # Unix daemonize
│   │   ├── daemon_windows.go
│   │   ├── stop.go          # Stop/status via PID file
│   │   └── stop_unix.go
│   ├── handler/
│   │   ├── handler.go       # HTTP handlers for translated endpoints
│   │   └── forward.go       # Direct-forward handlers for OpenAI-compatible endpoints
│   ├── store/
│   │   └── store.go         # In-memory response store (TTL-based)
│   ├── translator/
│   │   ├── request.go       # Anthropic → Chat Completions request
│   │   ├── response.go      # Chat Completions → Anthropic response
│   │   ├── stream.go        # Chat Completions SSE → Anthropic SSE
│   │   ├── responses.go     # Responses ↔ Chat Completions translator
│   │   ├── responses_stream.go  # Responses SSE stream translator
│   │   ├── gemini_request.go    # Gemini → Chat Completions request
│   │   ├── gemini_response.go   # Chat Completions → Gemini response
│   │   ├── gemini_stream.go     # Chat Completions SSE → Gemini SSE
│   │   └── gemini_embeddings.go # Gemini embedContent ↔ OpenAI Embeddings
│   └── types/
│       ├── anthropic.go     # Anthropic API types
│       ├── openai.go        # OpenAI Chat Completions types
│       ├── responses.go     # OpenAI Responses API types
│       ├── gemini.go        # Gemini API types
│       └── embeddings.go    # OpenAI Embeddings API types
├── docs/
│   ├── ARCHITECTURE.md      # Internal design, translation patterns
│   └── TROUBLESHOOTING.md   # Known issues, fixes, debugging tips
├── build/                   # Build output (gitignored)
├── config.example.yaml      # Example configuration
├── go.mod
├── Makefile                 # Cross-platform build system
└── README.md
```

## Tested Backends

| Backend | Status |
|---------|--------|
| MiMo (Xiaomi) | ✅ Tested |
| Ollama | ✅ Should work |
| OpenAI | ✅ Should work |
| Any OpenAI-compatible | ✅ Should work |

## Documentation

- [Architecture](docs/ARCHITECTURE.md) — how the proxy works, translation patterns, data flow
- [Troubleshooting](docs/TROUBLESHOOTING.md) — known issues, fixes, debugging tips
- [Roadmap](docs/ROADMAP.md) — future development plan, features, milestones

## License

MIT
