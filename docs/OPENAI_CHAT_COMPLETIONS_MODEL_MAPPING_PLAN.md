# OpenAI Chat Completions Model Mapping Plan

## Goal

Allow OpenAI-compatible agents that use this proxy with:

```text
base_url = http://localhost:8006/openai/v1
```

to benefit from the existing `models` mapping configuration when they call:

```text
POST /openai/v1/chat/completions
```

## Problem Summary

The proxy currently applies model mapping in translated endpoints such as:

- `/anthropic/v1/messages`
- `/openai/v1/responses`
- `/gemini/v1beta/models/{model}:generateContent`

However, `/openai/v1/chat/completions` is a direct-forward endpoint. It reads the request body only to detect `stream`, then forwards the original body unchanged to the backend. Because the body is forwarded unchanged, configured model mappings are skipped.

This means an OpenAI-compatible agent using model aliases such as `claude-sonnet-4-6` will send that alias directly to the backend instead of the mapped backend model such as `mimo-v2.5-pro`.

## Root Cause

`ChatCompletionsHandler` in `internal/handler/handler.go` does not modify the `model` field before constructing the backend request. Existing model mapping logic is duplicated in translated handlers but absent from this direct-forward path.

## Implementation Plan

1. In `ChatCompletionsHandler`, after reading the body, unmarshal only the `model` and `stream` fields into a small local struct.
2. Look up `model` in `h.cfg.GetModelMap()`.
3. If a mapping exists, update the JSON request body so `model` is replaced with the backend model.
4. Preserve all other request fields exactly as sent by the client.
5. Keep direct-forward behavior unchanged when no mapping exists or when the body cannot be parsed.
6. Add a regression test proving `/openai/v1/chat/completions` forwards the mapped model to the backend.
7. Run focused handler tests and full Go tests.

## Expected Change Scope

Files expected to change:

- `internal/handler/handler.go`
- an existing handler test file or a new focused test file under `internal/handler`

Plan document:

- `docs/OPENAI_CHAT_COMPLETIONS_MODEL_MAPPING_PLAN.md`

## Verification

Focused verification:

```text
go test ./internal/handler
```

Broader verification:

```text
go test ./...
```

## Non-Goals

- No git commit.
- No changes to Anthropic, Responses, or Gemini translation logic.
- No changes to backend URL routing.
- No broad refactor of direct-forward endpoints.
