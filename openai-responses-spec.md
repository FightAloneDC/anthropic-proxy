# OpenAI Responses API Specification (Proxy Reference)

Extracted from the official OpenAI API documentation for building an Anthropic Messages API to OpenAI Responses API proxy translator.

---

## 1. Endpoint

```
POST /v1/responses
```

Returns a `Response` object (non-streaming) or an SSE stream of `ResponseStreamEvent` objects (streaming).

---

## 2. Request Body Parameters

### Core Parameters

| Parameter | Type | Required | Description |
|---|---|---|---|
| `model` | `string` | Yes | Model ID (e.g., `gpt-4o`, `o3`, `gpt-4.1`) |
| `input` | `string \| InputItem[]` | Yes | Text string or array of input items (messages, tool outputs, etc.) |
| `instructions` | `string` | No | System/developer message inserted into context. Not carried over with `previous_response_id`. |
| `max_output_tokens` | `integer` | No | Upper bound for generated tokens (includes visible + reasoning tokens) |
| `temperature` | `number` | No | Sampling temperature, 0-2. Default varies by model. |
| `top_p` | `number` | No | Nucleus sampling. Alternative to temperature; don't use both. |

### Streaming Parameters

| Parameter | Type | Required | Description |
|---|---|---|---|
| `stream` | `boolean` | No | If `true`, response is streamed as SSE events |
| `stream_options` | `object` | No | Only set when `stream: true`. Has sub-field `include_obfuscation: boolean` (default `true`; set `false` to reduce bandwidth). |

### Tool Parameters

| Parameter | Type | Required | Description |
|---|---|---|---|
| `tools` | `Tool[]` | No | Array of tools the model may call (functions, built-in tools, MCP tools) |
| `tool_choice` | `string \| object` | No | Controls tool selection. See tool_choice section below. |
| `parallel_tool_calls` | `boolean` | No | Whether to allow parallel tool calls. |

### Reasoning Parameters (o-series and gpt-5 models)

| Parameter | Type | Required | Description |
|---|---|---|---|
| `reasoning` | `object` | No | Configuration for reasoning models. Contains: |
| `reasoning.effort` | `string` | No | `"none"`, `"minimal"`, `"low"`, `"medium"`, `"high"`, `"xhigh"`. gpt-5.1 defaults to `none`; earlier models default to `medium`. |
| `reasoning.summary` | `string` | No | `"auto"`, `"concise"`, `"detailed"`. Enables reasoning summary output. |

### Response Format / Structured Output

| Parameter | Type | Required | Description |
|---|---|---|---|
| `text` | `object` | No | Text output configuration |
| `text.format` | `object` | No | Output format. See text.format section below. |
| `text.verbosity` | `string` | No | `"low"`, `"medium"`, `"high"`. Constrains response verbosity. |

### Conversation & State

| Parameter | Type | Required | Description |
|---|---|---|---|
| `previous_response_id` | `string` | No | ID of previous response for multi-turn conversations. Cannot be used with `conversation`. |
| `conversation` | `string \| object` | No | Conversation ID. Items auto-added. Cannot be used with `previous_response_id`. |

### Other Parameters

| Parameter | Type | Required | Description |
|---|---|---|---|
| `store` | `boolean` | No | Whether to store the response for later retrieval. |
| `metadata` | `map[string]string` | No | Up to 16 key-value pairs (keys max 64 chars, values max 512 chars). |
| `user` | `string` | No | End-user identifier (being replaced by `safety_identifier` and `prompt_cache_key`). |
| `truncation` | `string` | No | `"auto"` or `"disabled"` (default). Auto drops early items to fit context. |
| `include` | `string[]` | No | Extra data to include (e.g., `"message.output_text.logprobs"`, `"reasoning.encrypted_content"`). |
| `service_tier` | `string` | No | `"auto"`, `"default"`, `"flex"`, `"priority"` |
| `background` | `boolean` | No | Run response in background. |

---

## 3. Input Item Types

The `input` parameter accepts either a plain string (equivalent to a user message) or an array of items.

### EasyInputMessage (simplest form)

```json
{
  "role": "user" | "assistant" | "system" | "developer",
  "content": "string or content array"
}
```

### Message Input (more explicit)

```json
{
  "type": "message",
  "role": "user" | "system" | "developer",
  "content": [
    { "type": "input_text", "text": "Hello" },
    { "type": "input_image", "image_url": "https://...", "detail": "auto" },
    { "type": "input_file", "file_id": "..." }
  ]
}
```

### FunctionCallOutput (tool result)

```json
{
  "type": "function_call_output",
  "call_id": "call_abc123",
  "output": "string result or content array"
}
```

### Input Content Types

| Type | Fields | Description |
|---|---|---|
| `input_text` | `text` | Text input |
| `input_image` | `image_url`, `detail`, `file_id` | Image input. `detail`: `"low"`, `"high"`, `"auto"`, `"original"` |
| `input_file` | `file_data`, `file_id`, `file_url`, `filename`, `detail` | File/PDF input |

---

## 4. Response Object

Returned by non-streaming requests, or embedded in `response.created` and `response.completed` streaming events.

```json
{
  "id": "resp_abc123",
  "object": "response",
  "created_at": 1712345678,
  "model": "gpt-4o",
  "status": "completed",
  "output": [...],
  "output_text": "aggregated text (SDK convenience)",
  "usage": {
    "input_tokens": 100,
    "input_tokens_details": { "cached_tokens": 50 },
    "output_tokens": 200,
    "output_tokens_details": { "reasoning_tokens": 50 },
    "total_tokens": 300
  },
  "instructions": "system prompt",
  "temperature": 1.0,
  "top_p": 1.0,
  "max_output_tokens": 4096,
  "tools": [...],
  "tool_choice": "auto",
  "parallel_tool_calls": true,
  "reasoning": { "effort": "medium", "summary": "auto" },
  "text": { "format": { "type": "text" }, "verbosity": "medium" },
  "previous_response_id": "resp_prev",
  "metadata": {},
  "truncation": "disabled",
  "store": true,
  "incomplete_details": null,
  "error": null,
  "completed_at": 1712345680
}
```

### Response Top-Level Fields

| Field | Type | Description |
|---|---|---|
| `id` | `string` | Unique response ID |
| `object` | `"response"` | Always `"response"` |
| `created_at` | `number` | Unix timestamp (seconds) |
| `model` | `string` | Model used |
| `status` | `string` | `"completed"`, `"failed"`, `"in_progress"`, `"cancelled"`, `"queued"`, `"incomplete"` |
| `output` | `ResponseOutputItem[]` | Array of output items (messages, tool calls, etc.) |
| `output_text` | `string` | SDK convenience: aggregated text from all `output_text` items |
| `usage` | `ResponseUsage` | Token usage details |
| `error` | `ResponseError \| null` | Error if generation failed |
| `incomplete_details` | `object \| null` | Why response is incomplete (`reason`: `"max_output_tokens"` or `"content_filter"`) |
| `completed_at` | `number \| null` | Unix timestamp when completed |

### ResponseOutputItem Types

The `output` array contains one or more of these item types:

#### ResponseOutputMessage (text response)

```json
{
  "id": "msg_abc123",
  "type": "message",
  "role": "assistant",
  "status": "completed",
  "content": [
    {
      "type": "output_text",
      "text": "Hello! How can I help?",
      "annotations": [],
      "logprobs": null
    }
  ]
}
```

| Field | Type | Description |
|---|---|---|
| `id` | `string` | Unique message ID |
| `type` | `"message"` | Always `"message"` |
| `role` | `"assistant"` | Always `"assistant"` for output |
| `status` | `string` | `"completed"`, `"in_progress"`, `"incomplete"` |
| `content` | `array` | Array of `ResponseOutputText` or `ResponseOutputRefusal` |

#### ResponseOutputText

```json
{
  "type": "output_text",
  "text": "The answer is 42.",
  "annotations": [...],
  "logprobs": [...]
}
```

#### ResponseOutputRefusal

```json
{
  "type": "refusal",
  "refusal": "I cannot help with that request."
}
```

#### FunctionCall (tool use)

```json
{
  "id": "fc_abc123",
  "type": "function_call",
  "call_id": "call_abc123",
  "name": "get_weather",
  "arguments": "{\"location\":\"San Francisco\"}",
  "status": "completed"
}
```

| Field | Type | Description |
|---|---|---|
| `id` | `string` | Unique ID of the function call |
| `type` | `"function_call"` | Always `"function_call"` |
| `call_id` | `string` | Call ID for referencing in tool outputs |
| `name` | `string` | Function name |
| `arguments` | `string` | JSON string of arguments |
| `status` | `string` | `"completed"`, `"in_progress"`, `"incomplete"` |

---

## 5. ResponseUsage Object

```json
{
  "input_tokens": 100,
  "input_tokens_details": {
    "cached_tokens": 50
  },
  "output_tokens": 200,
  "output_tokens_details": {
    "reasoning_tokens": 50
  },
  "total_tokens": 300
}
```

| Field | Type | Description |
|---|---|---|
| `input_tokens` | `number` | Number of input tokens |
| `input_tokens_details.cached_tokens` | `number` | Tokens retrieved from cache |
| `output_tokens` | `number` | Number of output tokens |
| `output_tokens_details.reasoning_tokens` | `number` | Reasoning tokens (for reasoning models) |
| `total_tokens` | `number` | Total tokens used |

---

## 6. Tools and Function Calling

### Defining Tools

Tools are defined in the `tools` array. The most common type is `function`:

```json
{
  "tools": [
    {
      "type": "function",
      "name": "get_weather",
      "description": "Get the current weather for a location",
      "parameters": {
        "type": "object",
        "properties": {
          "location": {
            "type": "string",
            "description": "City and country"
          }
        },
        "required": ["location"],
        "additionalProperties": false
      },
      "strict": true
    }
  ]
}
```

| Field | Type | Required | Description |
|---|---|---|---|
| `type` | `"function"` | Yes | Always `"function"` for custom tools |
| `name` | `string` | Yes | Function name |
| `description` | `string` | No | Description for the model |
| `parameters` | `object` | No | JSON Schema describing parameters |
| `strict` | `boolean` | No | Enforce strict parameter validation (default `true`) |

### tool_choice Options

| Value | Type | Description |
|---|---|---|
| `"none"` | `string` | Model will not call any tool |
| `"auto"` | `string` | Model can pick between message or tool call (default) |
| `"required"` | `string` | Model must call one or more tools |
| `{"type": "function", "name": "get_weather"}` | `object` | Force a specific function call |

Advanced tool_choice variants:
- `ToolChoiceAllowed`: `{ "type": "allowed_tools", "mode": "auto"|"required", "tools": [...] }` -- constrain to specific tools
- `ToolChoiceTypes`: `{ "type": "file_search" }` -- force a built-in tool type

### Tool Call Flow (Multi-Turn)

1. **Request** with `tools` and `input` messages
2. **Response** contains `FunctionCall` items in `output`
3. **Follow-up request** with previous `FunctionCallOutput` items in `input`:

```json
{
  "model": "gpt-4o",
  "input": [
    { "role": "user", "content": "What's the weather in SF?" },
    {
      "type": "function_call",
      "call_id": "call_abc123",
      "name": "get_weather",
      "arguments": "{\"location\":\"San Francisco\"}"
    },
    {
      "type": "function_call_output",
      "call_id": "call_abc123",
      "output": "{\"temperature\": 65, \"condition\": \"sunny\"}"
    }
  ],
  "tools": [...]
}
```

---

## 7. text.format (Structured Output)

### Plain Text (default)

```json
{ "text": { "format": { "type": "text" } } }
```

### JSON Schema (Structured Outputs)

```json
{
  "text": {
    "format": {
      "type": "json_schema",
      "name": "weather_response",
      "schema": {
        "type": "object",
        "properties": {
          "temperature": { "type": "number" },
          "condition": { "type": "string" }
        },
        "required": ["temperature", "condition"],
        "additionalProperties": false
      },
      "strict": true,
      "description": "Weather information"
    }
  }
}
```

### JSON Object (legacy, not recommended)

```json
{ "text": { "format": { "type": "json_object" } } }
```

---

## 8. Streaming Events

When `stream: true`, the response is delivered as Server-Sent Events (SSE). Each event has:

```
event: <event.type>
data: <JSON object with "type" field matching event name>
```

### Event Lifecycle (Typical Non-Tool-Use Stream)

```
1. response.created              -- Response object created (status: "in_progress")
2. response.in_progress          -- Response is being processed
3. response.output_item.added    -- New output item added (message or tool call)
4. response.content_part.added   -- New content part added to output item
5. response.output_text.delta    -- Incremental text chunk
   ... (repeated for each chunk) ...
6. response.output_text.done     -- Text content finalized
7. response.content_part.done    -- Content part finalized
8. response.output_item.done     -- Output item finalized
9. response.completed            -- Full Response object (status: "completed")
```

### Core Streaming Events (for proxy translation)

#### response.created

```json
{
  "type": "response.created",
  "sequence_number": 0,
  "response": { /* full Response object with status "in_progress" */ }
}
```

#### response.in_progress

```json
{
  "type": "response.in_progress",
  "sequence_number": 1,
  "response": { /* Response object */ }
}
```

#### response.output_item.added

```json
{
  "type": "response.output_item.added",
  "sequence_number": 2,
  "output_index": 0,
  "item": {
    "id": "msg_abc123",
    "type": "message",
    "role": "assistant",
    "status": "in_progress",
    "content": []
  }
}
```

#### response.content_part.added

```json
{
  "type": "response.content_part.added",
  "sequence_number": 3,
  "output_index": 0,
  "content_index": 0,
  "item_id": "msg_abc123",
  "part": {
    "type": "output_text",
    "text": ""
  }
}
```

#### response.output_text.delta (the main text streaming event)

```json
{
  "type": "response.output_text.delta",
  "sequence_number": 4,
  "output_index": 0,
  "content_index": 0,
  "item_id": "msg_abc123",
  "delta": "Hello",
  "logprobs": null
}
```

#### response.output_text.done

```json
{
  "type": "response.output_text.done",
  "sequence_number": 5,
  "output_index": 0,
  "content_index": 0,
  "item_id": "msg_abc123",
  "text": "Hello! How can I help you today?"
}
```

#### response.content_part.done

```json
{
  "type": "response.content_part.done",
  "sequence_number": 6,
  "output_index": 0,
  "content_index": 0,
  "item_id": "msg_abc123",
  "part": {
    "type": "output_text",
    "text": "Hello! How can I help you today?",
    "annotations": []
  }
}
```

#### response.output_item.done

```json
{
  "type": "response.output_item.done",
  "sequence_number": 7,
  "output_index": 0,
  "item": {
    "id": "msg_abc123",
    "type": "message",
    "role": "assistant",
    "status": "completed",
    "content": [
      {
        "type": "output_text",
        "text": "Hello! How can I help you today?",
        "annotations": []
      }
    ]
  }
}
```

#### response.completed (terminal event)

```json
{
  "type": "response.completed",
  "sequence_number": 8,
  "response": { /* full Response object with status "completed" and usage */ }
}
```

### Function Call Streaming Events

When the model calls a tool, instead of text events you get:

#### response.output_item.added (with function_call item)

```json
{
  "type": "response.output_item.added",
  "output_index": 0,
  "item": {
    "id": "fc_abc123",
    "type": "function_call",
    "call_id": "call_abc123",
    "name": "get_weather",
    "arguments": "",
    "status": "in_progress"
  }
}
```

#### response.function_call_arguments.delta

```json
{
  "type": "response.function_call_arguments.delta",
  "output_index": 0,
  "item_id": "fc_abc123",
  "delta": "{\"location\":"
}
```

#### response.function_call_arguments.done

```json
{
  "type": "response.function_call_arguments.done",
  "output_index": 0,
  "item_id": "fc_abc123",
  "name": "get_weather",
  "arguments": "{\"location\":\"San Francisco\"}"
}
```

#### response.output_item.done (with completed function_call)

```json
{
  "type": "response.output_item.done",
  "output_index": 0,
  "item": {
    "id": "fc_abc123",
    "type": "function_call",
    "call_id": "call_abc123",
    "name": "get_weather",
    "arguments": "{\"location\":\"San Francisco\"}",
    "status": "completed"
  }
}
```

### Reasoning Model Streaming Events

For reasoning models (o-series, gpt-5 with reasoning enabled):

#### response.reasoning_text.delta

```json
{
  "type": "response.reasoning_text.delta",
  "output_index": 0,
  "content_index": 0,
  "item_id": "rs_abc123",
  "delta": "Let me think about this..."
}
```

#### response.reasoning_text.done

```json
{
  "type": "response.reasoning_text.done",
  "output_index": 0,
  "content_index": 0,
  "item_id": "rs_abc123",
  "text": "Let me think about this... The answer is 42."
}
```

#### response.reasoning_summary_text.delta

```json
{
  "type": "response.reasoning_summary_text.delta",
  "output_index": 0,
  "summary_index": 0,
  "item_id": "rs_abc123",
  "delta": "Summary of reasoning..."
}
```

### Refusal Streaming Events

#### response.refusal.delta

```json
{
  "type": "response.refusal.delta",
  "output_index": 0,
  "content_index": 0,
  "item_id": "msg_abc123",
  "delta": "I'm sorry"
}
```

### Error / Failure Events

#### response.error

```json
{
  "type": "response.error",
  "sequence_number": 5,
  "code": "server_error",
  "message": "An error occurred"
}
```

#### response.failed

```json
{
  "type": "response.failed",
  "sequence_number": 5,
  "response": { /* Response object with status "failed" and error */ }
}
```

#### response.incomplete

```json
{
  "type": "response.incomplete",
  "response": { /* Response object with status "incomplete" */ }
}
```

### Complete List of Streaming Event Types

**Lifecycle events:**
- `response.created`
- `response.in_progress`
- `response.completed`
- `response.failed`
- `response.incomplete`
- `response.queued`
- `response.cancelled` (referenced but not in main spec)
- `response.error`

**Output item events:**
- `response.output_item.added`
- `response.output_item.done`

**Content part events:**
- `response.content_part.added`
- `response.content_part.done`

**Text output events:**
- `response.output_text.delta`
- `response.output_text.done`

**Refusal events:**
- `response.refusal.delta`
- `response.refusal.done`

**Function call events:**
- `response.function_call_arguments.delta`
- `response.function_call_arguments.done`

**Reasoning events:**
- `response.reasoning_text.delta`
- `response.reasoning_text.done`
- `response.reasoning_summary_text.delta`
- `response.reasoning_summary_text.done`
- `response.reasoning_summary_part.added`
- `response.reasoning_summary_part.done`

**Audio events:**
- `response.audio.delta`
- `response.audio.done`
- `response.audio.transcript.delta`
- `response.audio.transcript.done`

**Web search events:**
- `response.web_search_call.in_progress`
- `response.web_search_call.searching`
- `response.web_search_call.completed`

**File search events:**
- `response.file_search_call.in_progress`
- `response.file_search_call.searching`
- `response.file_search_call.completed`

**Code interpreter events:**
- `response.code_interpreter_call.in_progress`
- `response.code_interpreter_call.interpreting`
- `response.code_interpreter_call.completed`
- `response.code_interpreter_call_code.delta`
- `response.code_interpreter_call_code.done`

**Image generation events:**
- `response.image_gen_call.in_progress`
- `response.image_gen_call.generating`
- `response.image_gen_call.completed`
- `response.image_gen_call.partial_image`

**MCP events:**
- `response.mcp_call.in_progress`
- `response.mcp_call.completed`
- `response.mcp_call.failed`
- `response.mcp_call.arguments.delta`
- `response.mcp_call.arguments.done`
- `response.mcp_list_tools.in_progress`
- `response.mcp_list_tools.completed`
- `response.mcp_list_tools.failed`

**Custom tool events:**
- `response.custom_tool_call_input.delta`
- `response.custom_tool_call_input.done`

**Annotation events:**
- `response.output_text.annotation.added`

---

## 9. Key Differences from Chat Completions API

For proxy builders migrating from the Chat Completions API, note these Responses API differences:

1. **Endpoint**: `POST /v1/responses` (not `/v1/chat/completions`)
2. **Input format**: `input` array uses typed items (`type: "message"`) rather than `messages` array
3. **Output format**: `output` array of `ResponseOutputItem` objects (not `choices[].message`)
4. **System prompt**: Uses `instructions` parameter (not a `system` role message)
5. **Tool calls**: Output as `FunctionCall` items in `output` (not `choices[].message.tool_calls`)
6. **Tool results**: Input as `FunctionCallOutput` items (not `role: "tool"` messages)
7. **Streaming**: Events are typed (`response.output_text.delta`) with `output_index` and `item_id` (not `choices[].delta`)
8. **Reasoning**: Exposed via `reasoning` parameter and `response.reasoning_text.delta` events (not `reasoning_content`)
9. **Status field**: Response has a `status` field (`completed`, `failed`, `incomplete`, etc.)
10. **Stateful conversations**: `previous_response_id` replaces manually managing message history
11. **Token usage**: `input_tokens_details.cached_tokens` and `output_tokens_details.reasoning_tokens` (not `prompt_tokens_details`)
12. **Function call arguments**: Streamed as `response.function_call_arguments.delta` with `delta` field (not `choices[].delta.tool_calls[].function.arguments`)
