# anthropic-proxy 🔄

Proxy translator **Anthropic Messages API** ↔ **OpenAI Chat Completions API**.

Client ngomong format Anthropic, proxy translate ke OpenAI, forward ke backend, terus translate balik ke format Anthropic. Support streaming, non-streaming, dan reasoning/thinking blocks.

```
Client (Anthropic)  →  Proxy (:8080)  →  OpenAI-compatible backend
              ←  ──  ──┘
```

## Quick Start

```bash
# Build
go build -o anthropic-proxy .

# Bikin .env
cat > .env << 'EOF'
PORT=8080
OPENAI_BASE_URL=https://your-backend.com/v1
OPENAI_API_KEY=your-api-key-here
EOF

# Jalankan
./anthropic-proxy
```

## Konfigurasi

Priority: **CLI flag > env var > .env file > default**

### Option 1: `.env` file (paling simpel)

```env
PORT=8080
OPENAI_BASE_URL=https://api.openai.com/v1
OPENAI_API_KEY=sk-xxxx
```

### Option 2: CLI flags

```bash
./anthropic-proxy -port 9090 -url https://api.openai.com/v1 -key sk-xxxx
```

### Option 3: Env vars (kalau suka export)

```bash
PORT=9090 OPENAI_BASE_URL=https://api.openai.com/v1 ./anthropic-proxy
```

### Semua flag

| Flag | Env var | Default | Description |
|------|---------|---------|-------------|
| `-port` | `PORT` | `8080` | Port listen |
| `-url` | `OPENAI_BASE_URL` | `http://localhost:11434` | URL backend OpenAI-compatible |
| `-key` | `OPENAI_API_KEY` | `-` | API key untuk backend |

## Contoh Request

### Non-streaming

```bash
curl http://localhost:8080/v1/messages \
  -H "Content-Type: application/json" \
  -d '{
    "model": "gpt-4o-mini",
    "max_tokens": 200,
    "messages": [{"role": "user", "content": "Halo!"}]
  }'
```

Response:
```json
{
  "id": "gen-xxx",
  "type": "message",
  "role": "assistant",
  "model": "gpt-4o-mini",
  "content": [
    {"type": "text", "text": "Halo! Ada yang bisa saya bantu?"}
  ],
  "stop_reason": "end_turn",
  "usage": {"input_tokens": 10, "output_tokens": 15}
}
```

### Streaming

```bash
curl -N http://localhost:8080/v1/messages \
  -H "Content-Type: application/json" \
  -d '{
    "model": "gpt-4o-mini",
    "max_tokens": 200,
    "stream": true,
    "messages": [{"role": "user", "content": "Ceritakan tentang Go"}]
  }'
```

Response (SSE):
```
event: message_start
data: {"type":"message_start","message":{...}}

event: content_block_start
data: {"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}

event: content_block_delta
data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"Go"}}

event: content_block_delta
data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":" adalah"}}

...

event: content_block_stop
data: {"type":"content_block_stop","index":0}

event: message_delta
data: {"type":"message_delta","delta":{"stop_reason":"end_turn"}}

event: message_stop
data: {"type":"message_stop"}
```

### Dengan system message

```bash
curl http://localhost:8080/v1/messages \
  -H "Content-Type: application/json" \
  -d '{
    "model": "gpt-4o-mini",
    "max_tokens": 200,
    "system": "Kamu adalah asisten yang helpful dan ramah.",
    "messages": [{"role": "user", "content": "Hi!"}]
  }'
```

### Dengan tools

```bash
curl http://localhost:8080/v1/messages \
  -H "Content-Type: application/json" \
  -d '{
    "model": "gpt-4o-mini",
    "max_tokens": 200,
    "tools": [{
      "name": "get_weather",
      "description": "Get weather for a location",
      "input_schema": {
        "type": "object",
        "properties": {
          "location": {"type": "string"}
        },
        "required": ["location"]
      }
    }],
    "messages": [{"role": "user", "content": "Cuaca di Jakarta?"}]
  }'
```

### Reasoning model (thinking blocks)

Kalau backend pakai reasoning model (kayak MiMo, DeepSeek R1), proxy otomatis map `reasoning` ke Anthropic `thinking` blocks:

```
event: content_block_start
data: {"type":"content_block_start","index":0,"content_block":{"type":"thinking"}}

event: content_block_delta
data: {"type":"content_block_delta","index":0,"delta":{"type":"thinking_delta","thinking":"Let me think..."}}

event: content_block_stop
data: {"type":"content_block_stop","index":0}

event: content_block_start
data: {"type":"content_block_start","index":1,"content_block":{"type":"text"}}

event: content_block_delta
data: {"type":"content_block_delta","index":1,"delta":{"type":"text_delta","text":"The answer is..."}}
```

## Translation Mapping

### Request (Anthropic → OpenAI)

| Anthropic | OpenAI |
|-----------|--------|
| `system` (string/array) | `messages[0]` role `system` |
| `messages[].content` blocks | `messages[].content` string/parts |
| `tool_use` blocks | `tool_calls` array |
| `tool_result` blocks | `role: "tool"` messages |
| `stop_sequences` | `stop` |
| `tools[].input_schema` | `tools[].function.parameters` |
| `temperature` | `temperature` |
| `top_p` | `top_p` |
| `max_tokens` | `max_tokens` |

### Response (OpenAI → Anthropic)

| OpenAI | Anthropic |
|--------|-----------|
| `choices[0].message.content` | `content[{type:"text"}]` |
| `choices[0].message.tool_calls` | `content[{type:"tool_use"}]` |
| `finish_reason: "stop"` | `stop_reason: "end_turn"` |
| `finish_reason: "length"` | `stop_reason: "max_tokens"` |
| `finish_reason: "tool_calls"` | `stop_reason: "tool_use"` |
| `reasoning` (streaming) | `thinking` blocks |
| `usage.prompt_tokens` | `usage.input_tokens` |
| `usage.completion_tokens` | `usage.output_tokens` |

## Tested Backends

| Backend | Status |
|---------|--------|
| [VoidMind](https://voidmind.io) | ✅ Works (reasoning model) |
| Ollama | ✅ Should work |
| OpenAI | ✅ Should work |
| Any OpenAI-compatible | ✅ Should work |

## Project Structure

```
anthropic-proxy/
├── main.go          # HTTP server, config, routing
├── types.go         # Anthropic & OpenAI struct definitions
├── translate.go     # Translation logic (request, response, streaming)
├── .env             # Config (gitignored)
├── go.mod           # Go module
└── README.md        # This file
```

## License

MIT - bikin apa aja terserah 🤙
