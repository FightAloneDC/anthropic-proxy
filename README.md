# anthropic-proxy 🔄

Universal API translator proxy. Translates between **Anthropic Messages API**, **OpenAI Responses API**, and **OpenAI Chat Completions API** — allowing any Anthropic or OpenAI Responses SDK client to connect to any OpenAI-compatible backend.

```
Anthropic SDK        →  /anthropic/v1/messages      ─┐
                                                      ├→  translate  →  backend /v1/chat/completions
OpenAI Responses SDK →  /openai/v1/responses         ─┘
OpenAI Chat SDK      →  /openai/v1/chat/completions  ──  direct forward
```

## Features

- **Anthropic Messages API** — full translation to/from Chat Completions
- **OpenAI Responses API** — full translation to/from Chat Completions
- **Direct forward** — `/openai/v1/chat/completions` pass-through
- **Streaming** — SSE streaming for all endpoints
- **Tool calling** — bidirectional function/tool translation
- **Reasoning/thinking blocks** — maps `reasoning` ↔ `thinking` blocks
- **Model mapping** — remap model names to backend equivalents
- **`previous_response_id`** — in-memory response store for conversation continuity
- **Cache token forwarding** — maps cache hit/miss tokens between formats
- **Daemon mode** — run as background process with start/stop/restart/status

## Quick Start

```bash
# Build
go build -o anthropic-proxy ./cmd/anthropic-proxy

# Create config
cp config.example.yaml config.yaml
# Edit config.yaml with your backend URL and API key

# Run in foreground
./anthropic-proxy start -fg

# Run as daemon
./anthropic-proxy start
```

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
  -fg               run in foreground (not as daemon)
```

## Configuration

Priority: **CLI flags > YAML config > environment variables**

### YAML config (recommended)

```yaml
server:
  port: 8006
  # fg: false  # Run in foreground (default: daemon mode)

backend:
  url: "https://your-openai-compatible-backend.com/v1"
  api_key: "your-api-key"

proxy:
  skip_thinking: false     # Skip reasoning/thinking blocks
  debug: false             # Enable debug logging
  store_ttl: 3600          # Response store TTL in seconds (default: 3600)
  store_max_entries: 1000  # Max stored responses (default: 1000)

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
│   │   └── handler.go       # HTTP handlers for all endpoints
│   ├── store/
│   │   └── store.go         # In-memory response store (TTL-based)
│   ├── translator/
│   │   ├── request.go       # Anthropic → Chat Completions request
│   │   ├── response.go      # Chat Completions → Anthropic response
│   │   ├── stream.go        # Chat Completions SSE → Anthropic SSE
│   │   ├── responses.go     # Responses ↔ Chat Completions translator
│   │   └── responses_stream.go  # Responses SSE stream translator
│   └── types/
│       ├── anthropic.go     # Anthropic API types
│       ├── openai.go        # OpenAI Chat Completions types
│       └── responses.go     # OpenAI Responses API types
├── config.example.yaml      # Example configuration
├── go.mod
├── Makefile
└── README.md
```

## Tested Backends

| Backend | Status |
|---------|--------|
| MiMo (Xiaomi) | ✅ Tested |
| Ollama | ✅ Should work |
| OpenAI | ✅ Should work |
| Any OpenAI-compatible | ✅ Should work |

## License

MIT
