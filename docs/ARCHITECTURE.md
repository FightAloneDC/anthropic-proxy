# Architecture

How the anthropic-proxy works internally — translation patterns, data flow, and design decisions.

## Table of Contents

- [Overview](#overview)
- [Route Structure](#route-structure)
- [Translation Flow](#translation-flow)
- [Anthropic ↔ Chat Completions](#anthropic--chat-completions)
- [Responses ↔ Chat Completions](#responses--chat-completions)
- [Streaming Architecture](#streaming-architecture)
- [State Management](#state-management)
- [Code Structure](#code-structure)

---

## Overview

The proxy is a **universal translator** between three API formats:

```
┌─────────────────────┐
│  Anthropic SDK      │──→ /anthropic/v1/messages
└─────────────────────┘         │
                                ▼
                        ┌───────────────┐         ┌─────────────────────┐
                        │   translator   │────────→│  Backend (OpenAI    │
                        └───────────────┘         │  Chat Completions)  │
                                ▲                 └─────────────────────┘
┌─────────────────────┐         │
│  OpenAI Responses   │──→ /openai/v1/responses
│  SDK / Codex        │
└─────────────────────┘         │
┌─────────────────────┐         │
│  Gemini SDK / API   │──→ /gemini/v1beta/models/{model}:generateContent
└─────────────────────┘
```

**Design principle:** All translation is **stateless** (except `previous_response_id` store). Each request is independently translated and forwarded.

---

## Route Structure

```
/anthropic/v1/messages        → translate to chat/completions
/anthropic/v1/models          → direct forward to backend

/openai/v1/responses          → translate to chat/completions
/openai/v1/chat/completions   → direct forward to backend
/openai/v1/models             → direct forward to backend
/openai/v1/embeddings         → direct forward to backend
/openai/v1/rerank             → direct forward to backend
/openai/v1/audio/speech       → direct forward to backend
/openai/v1/audio/transcriptions → direct forward to backend
/openai/v1/images/generations → direct forward to backend

/gemini/v1beta/models/{model}:generateContent        → translate to chat/completions
/gemini/v1beta/models/{model}:streamGenerateContent  → translate to chat/completions
/gemini/v1beta/models/{model}:embedContent           → translate to embeddings
```

**Naming convention:** `/{provider}/v1/{endpoint}`

SDK configuration:
- Anthropic SDK: `base_url = "http://host:port/anthropic"`
- OpenAI SDK: `base_url = "http://host:port/openai"`
- Gemini API root: `http://host:port/gemini`

---

## Translation Flow

### Anthropic Messages → Chat Completions

```
1. Parse AnthropicRequest
2. Model mapping (from → to)
3. TranslateRequest() → OpenAIRequest
   - system → messages[0] with role:"system"
   - messages[] → messages[] (text, image, tool_use, tool_result)
   - tools[] → tools[] (input_schema → function.parameters)
   - tool_choice → tool_choice (auto/any/tool → auto/required/function)
   - thinking.budget_tokens → adjusts max_tokens
   - metadata.user_id → user
4. Forward to backend /v1/chat/completions
5. TranslateResponse() → AnthropicResponse (or StreamTranslator for SSE)
```

### Chat Completions → Anthropic Messages

```
1. OpenAIResponse → AnthropicResponse
   - choices[0].message.content → content[{type:"text"}]
   - choices[0].message.tool_calls → content[{type:"tool_use"}]
   - finish_reason → stop_reason (stop→end_turn, length→max_tokens, tool_calls→tool_use)
   - usage mapping (prompt_tokens→input_tokens, etc.)
2. For streaming: StreamTranslator processes each chunk
   - delta.content → text blocks
   - delta.reasoning → thinking blocks (unless skipThinking)
   - delta.tool_calls → tool_use blocks
```

---

### Responses API → Chat Completions

```
1. Parse ResponsesRequest
2. Handle previous_response_id (fetch from store, prepend messages)
3. Model mapping
4. TranslateResponsesRequest() → OpenAIRequest
   - input (string) → messages[{role:"user", content:string}]
   - input ([]InputItem) → messages[] with type-based mapping:
     - "message" → role+content mapping
     - "function_call" → assistant message with tool_calls
     - "function_call_output" → role:"tool" message
   - instructions → messages[0] with role:"system"
   - tools[] (flat) → tools[] (nested function wrapper)
   - tool_choice → tool_choice (mostly same format)
   - text.format → response_format
   - max_output_tokens → max_tokens
5. Forward to backend /v1/chat/completions
6. TranslateResponsesResponse() → ResponsesResponse
   - choices[0].message.content → output[{type:"message", content:[{type:"output_text"}]}]
   - choices[0].message.tool_calls → output[{type:"function_call"}]
   - finish_reason → status (stop→completed, length→incomplete)
   - usage mapping (prompt_tokens→input_tokens, etc.)
```

### Chat Completions → Responses API

For streaming, the `ResponsesStreamTranslator` converts chunks:

```
1. First chunk → response.created event
2. delta.content → response.output_text.delta event
3. delta.tool_calls → response.function_call_arguments.delta event
4. delta.reasoning → response.reasoning_text.delta event
5. Finish → response.output_text.done + response.output_item.done + response.completed events
```

### Gemini generateContent → Chat Completions

```
1. Parse model and action from /gemini/v1beta/models/{model}:generateContent
2. Parse GeminiRequest
3. Model mapping
4. TranslateGeminiRequest() → OpenAIRequest
   - systemInstruction → messages[0] with role:"system"
   - contents[].role user/model → user/assistant messages
   - parts[].text → text content
   - parts[].inlineData/fileData → image_url content
   - functionDeclarations → tools[]
   - generationConfig → max_tokens, temperature, top_p, top_k, stop
   - safetySettings → accepted but ignored
5. Forward to backend /v1/chat/completions
6. TranslateGeminiResponse() → GeminiResponse (or GeminiStreamTranslator for SSE)
```

### Chat Completions → Gemini

```
1. OpenAIResponse → GeminiResponse
   - choices[].message.content → candidates[].content.parts[].text
   - choices[].message.tool_calls → candidates[].content.parts[].functionCall
   - finish_reason → finishReason (stop/tool_calls→STOP, length→MAX_TOKENS)
   - usage → usageMetadata
2. For streaming: GeminiStreamTranslator processes each chunk
   - delta.content → Gemini SSE data candidate with text part
   - delta.tool_calls → accumulated functionCall part
   - usage-only chunks → usageMetadata-only event
```

### Gemini embedContent → OpenAI Embeddings

```
1. Parse model from /gemini/v1beta/models/{model}:embedContent
2. Parse GeminiEmbedContentRequest
3. Model mapping
4. TranslateGeminiEmbedContentRequest() → OpenAIEmbeddingsRequest
   - content.parts[].text → input
   - outputDimensionality → dimensions
   - taskType/title → accepted but ignored
5. Forward to backend /v1/embeddings
6. TranslateGeminiEmbedContentResponse() → GeminiEmbedContentResponse
```

### OpenAI-compatible multi-modal direct forward

```
1. Match /openai/v1/{multimodal-endpoint}
2. Build backend /v1/{multimodal-endpoint} URL
3. Preserve request body and selected headers
4. Replace Authorization with configured backend API key
5. Copy backend status, headers, and response body
```

Direct-forward endpoints intentionally avoid JSON parsing so multipart uploads and binary responses keep their backend-native behavior.

---

## Key Translation Challenges

### 1. Input Reshaping (Responses → Chat Completions)

Responses API uses flat typed items:
```json
[
  {"type": "message", "role": "user", "content": [...]},
  {"type": "function_call", "call_id": "call_1", "name": "get_weather", "arguments": "..."},
  {"type": "function_call_output", "call_id": "call_1", "output": "..."}
]
```

Chat Completions requires nested messages:
```json
[
  {"role": "user", "content": "..."},
  {"role": "assistant", "tool_calls": [{"id": "call_1", "type": "function", "function": {"name": "get_weather", "arguments": "..."}}]},
  {"role": "tool", "tool_call_id": "call_1", "content": "..."}
]
```

**Solution:** `translateInputItems()` iterates through items, converting each type. `mergeAssistantToolCalls()` combines consecutive assistant messages with tool_calls into one.

### 2. Content Type Mapping

| Responses API | Chat Completions | Notes |
|---------------|------------------|-------|
| `input_text` | `text` | Type name change |
| `output_text` | `text` | Also handled (for conversation history replay) |
| `input_image` | `image_url` | Structure changes (flat → nested) |

### 3. Tool Definition Nesting

Responses API (flat):
```json
{"type": "function", "name": "get_weather", "description": "...", "parameters": {...}}
```

Chat Completions (nested):
```json
{"type": "function", "function": {"name": "get_weather", "description": "...", "parameters": {...}}
```

**Solution:** Simple wrapper in `TranslateResponsesRequest()`.

### 4. Reasoning Content

Backend returns `reasoning_content` for reasoning models. Two issues:
- `OpenAIMsg` didn't have `ReasoningContent` field → added
- Response translator didn't use `reasoning_content` as fallback → added

### 5. Non-function Tools

Codex sends built-in tools (web_search, bash, etc.) that have no Chat Completions equivalent. These are skipped during translation.

---

## Streaming Architecture

Both Anthropic and Responses streaming use the same pattern:

```
Backend (SSE) → Scanner → Translator → emit callback → Client (SSE)
```

### Callback-based design

The translator doesn't write to `http.ResponseWriter` directly. Instead, it receives an `emit func(event string, data interface{})` callback defined in the handler:

```go
emit := func(event string, data interface{}) {
    j, _ := json.Marshal(data)
    fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event, string(j))
    flusher.Flush()
}
```

### State machine

Both stream translators are stateful structs with:
- `started` — whether the initial event has been emitted
- `blockOpen` / `inThinking` — current block state
- `finished` — whether the final event has been emitted

Each chunk triggers state transitions and emits appropriate events.

---

## State Management

### Response Store (for `previous_response_id`)

**Location:** `internal/store/store.go`

- **In-memory** — no persistence across restarts
- **TTL-based** — entries expire after configurable time (default 1 hour)
- **Max capacity** — FIFO eviction when full (default 1000 entries)
- **Thread-safe** — `sync.RWMutex` for concurrent access
- **Background cleanup** — goroutine runs every TTL/2

**Usage:**
1. After successful non-streaming Responses API response, store it: `store.Store(id, response)`
2. When `previous_response_id` is set in request, fetch: `store.Get(id)`
3. Convert stored response output to messages and prepend to new request

---

## Code Structure

```
internal/
├── config/
│   └── config.go           # YAML + env + CLI config
├── daemon/
│   ├── daemon.go           # Daemon management
│   ├── daemon_unix.go      # Unix fork
│   ├── daemon_windows.go   # Windows stub
│   ├── stop.go             # Stop/status
│   └── stop_unix.go        # Unix kill
├── handler/
│   ├── handler.go          # HTTP handlers for translated endpoints
│   └── forward.go          # Direct-forward handlers
├── store/
│   └── store.go            # In-memory response store
├── translator/
│   ├── request.go          # Anthropic → Chat Completions request
│   ├── response.go         # Chat Completions → Anthropic response
│   ├── stream.go           # Chat Completions SSE → Anthropic SSE
│   ├── responses.go        # Responses ↔ Chat Completions translator
│   ├── responses_stream.go # Responses SSE stream translator
│   ├── gemini_request.go   # Gemini → Chat Completions request
│   ├── gemini_response.go  # Chat Completions → Gemini response
│   ├── gemini_stream.go    # Chat Completions SSE → Gemini SSE
│   └── gemini_embeddings.go # Gemini embedContent ↔ OpenAI Embeddings
└── types/
    ├── anthropic.go        # Anthropic API types
    ├── openai.go           # OpenAI Chat Completions types
    ├── responses.go        # OpenAI Responses API types
    ├── gemini.go           # Gemini API types
    └── embeddings.go       # OpenAI Embeddings API types
```

### Conventions

- **Types:** `Anthropic`, `OpenAI`, `Responses`, and `Gemini` prefixes identify external API formats
- **Polymorphic fields:** Use `interface{}` with type switches at translation time
- **No third-party deps:** Only `gopkg.in/yaml.v3` for config
- **Standard library HTTP:** No router framework, just `net/http`
- **Error format:** Each translated endpoint returns the closest client-facing error envelope for its API format
