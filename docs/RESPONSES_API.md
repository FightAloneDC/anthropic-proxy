# OpenAI Responses API (Codex)

How the proxy implements `/openai/v1/responses` and `/v1/responses` against Chat Completions backends.

Code: `internal/translator/responses.go`, `responses_stream.go`, `internal/handler` Responses handlers, `internal/store`, `internal/types/responses.go`.

## Request translation

`TranslateResponsesRequest(req, prevMessages) → (OpenAIRequest, reasoningEnabled, customTools)`

| Responses field | Chat Completions |
|-----------------|------------------|
| `model` | mapped model |
| `instructions` | system (or leading system message) |
| `input` string | user message |
| `input` item list | messages / tool history (see below) |
| `tools` (function) | `tools[].type=function` |
| `tools` (custom) | free-form function + name recorded in `customTools` |
| `tool_choice` | best-effort map to Chat tool_choice |
| `stream` | stream flag |
| `previous_response_id` | resolved via store → `prevMessages` prepended |
| reasoning / effort hints | may enable reasoning-related backend behavior when present |

### Input item types (high level)

- `message` — role + content parts (text, input_image, etc.) → OpenAI messages  
- `function_call` / `custom_tool_call` → assistant message with `tool_calls`  
- `function_call_output` / `custom_tool_call_output` → `role: tool` message  
- Nested / namespaced tool lists (`additional_tools`, etc.) — extracted when present; custom names join `customTools`

### Custom tools (Codex `exec`)

Codex registers tools like:

```json
{ "type": "custom", "name": "exec", ... }
```

Chat Completions has no `custom` tool type. The proxy:

1. Emits a function tool roughly:

```json
{
  "type": "function",
  "function": {
    "name": "exec",
    "parameters": {
      "type": "object",
      "properties": { "input": { "type": "string" } },
      "required": ["input"],
      "additionalProperties": true
    }
  }
}
```

2. Puts `"exec": true` in `customTools` for the response path.
3. On the model’s tool call, unwraps arguments with `extractCustomToolInput`:
   - Prefer JSON object `{"input": "..."}` → use the string  
   - Else use raw arguments string  

## Non-stream response

`TranslateResponsesResponse(openaiResp, responseID, customTools)` builds a Responses object:

- Assistant text → `output[]` item `type: "message"` with `output_text`  
- Tool calls whose names are in `customTools` → `type: "custom_tool_call"` with **`input`** (not `arguments`)  
- Other tool calls → `type: "function_call"` with arguments  
- Reasoning/thinking (when present and not skipped) → reasoning-style items  

`ResponseOutputItem` includes optional `Input` for custom tool calls.

## Streaming

`NewResponsesStreamTranslator(emit, responseID, customTools)` maps Chat Completions SSE chunks to Responses events.

Typical sequence for a custom tool call:

1. `response.created`  
2. `response.output_item.added` — `item.type = "custom_tool_call"`, `name`, `call_id`, `status: in_progress`  
3. `response.custom_tool_call_input.delta` — incremental `input` text  
4. `response.custom_tool_call_input.done` — full `input`  
5. `response.output_item.done` — completed custom_tool_call  
6. `response.completed` — full `response.output` snapshot + usage  

Text path uses `response.output_text.delta` / content part events; function tools use function-call argument deltas (not custom_*).

Reasoning path (when enabled): reasoning item open/delta/done events; thinking tags from the backend are normalized rather than stripped (unless `skip_thinking`).

## `previous_response_id`

1. Client completes a turn → proxy stores conversation messages under `response.id`.  
2. Next turn: client sends new `input` + `previous_response_id`.  
3. Handler loads store → `TranslateResponsesRequest(..., prevMessages)`.  

Store config: [CONFIGURATION.md](./CONFIGURATION.md) (`store_backend`, `store_file`, `store_ttl`, `store_max_entries`).

## Client behavior notes (not proxy bugs)

- **Codex agent loop**: after a tool result, Codex immediately POSTs again. Idle terminal ≠ idle agent.  
- **Restarting the proxy** only drops in-flight connections; a running Codex process reconnects and continues.  
- **`// @exec: { "yield_time_ms": ... }`** inside `input` is Codex exec metadata, not a proxy feature.  
- Long multi-tool turns (memory rewrite, large `exec`) produce many consecutive 200s — expected relay, not self-driven traffic.

## Verification checklist

- Log line contains `customTools=map[exec:true]` when Codex sends custom tools.  
- SSE includes `custom_tool_call` + `input`, not only `function_call` + `arguments` for `exec`.  
- Events include `response.custom_tool_call_input.delta` / `.done`.  
- Multi-turn with `previous_response_id` keeps tool history coherent.

## Related types

- `types.ResponsesRequest`, `ResponsesResponse`, `ResponseOutputItem`  
- Stream event structs for text, function args, custom tool input, completed payload  
