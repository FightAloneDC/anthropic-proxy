package translator

import (
	"testing"

	"anthropic-proxy/internal/types"
)

func TestTranslateResponsesRequestInputImageURL(t *testing.T) {
	req := &types.ResponsesRequest{
		Model: "vision-model",
		Input: []interface{}{
			map[string]interface{}{
				"type": "message",
				"role": "user",
				"content": []interface{}{
					map[string]interface{}{"type": "input_text", "text": "describe"},
					map[string]interface{}{"type": "input_image", "image_url": "https://example.com/image.png", "detail": "high"},
				},
			},
		},
	}

	got, _, _ := TranslateResponsesRequest(req, nil)

	parts, ok := got.Messages[0].Content.([]interface{})
	if !ok || len(parts) != 2 {
		t.Fatalf("content = %#v", got.Messages[0].Content)
	}
	imagePart := parts[1].(map[string]interface{})
	imageURL := imagePart["image_url"].(map[string]string)
	if imageURL["url"] != "https://example.com/image.png" || imageURL["detail"] != "high" {
		t.Fatalf("image_url = %#v", imageURL)
	}
}

func TestTranslateResponsesRequestInputImageFileData(t *testing.T) {
	req := &types.ResponsesRequest{
		Model: "vision-model",
		Input: []interface{}{
			map[string]interface{}{
				"type": "message",
				"role": "user",
				"content": []interface{}{
					map[string]interface{}{"type": "input_image", "file_data": "data:image/png;base64,abc"},
				},
			},
		},
	}

	got, _, _ := TranslateResponsesRequest(req, nil)

	parts, ok := got.Messages[0].Content.([]interface{})
	if !ok || len(parts) != 1 {
		t.Fatalf("content = %#v", got.Messages[0].Content)
	}
	imagePart := parts[0].(map[string]interface{})
	imageURL := imagePart["image_url"].(map[string]string)
	if imageURL["url"] != "data:image/png;base64,abc" {
		t.Fatalf("image_url = %#v", imageURL)
	}
}

// Codex embeds tools as input items of type "additional_tools" (not top-level tools[]).
// Dropping them leaves the model with no shell/read/write tools, so it only emits
// "I'll check workspace instructions..." text and never acts.
func TestTranslateResponsesRequestAdditionalTools(t *testing.T) {
	req := &types.ResponsesRequest{
		Model: "gpt-test",
		Input: []interface{}{
			map[string]interface{}{
				"type": "message",
				"role": "user",
				"content": []interface{}{
					map[string]interface{}{"type": "input_text", "text": "hi kawan"},
				},
			},
			map[string]interface{}{
				"type": "additional_tools",
				"tools": []interface{}{
					map[string]interface{}{
						"type":        "function",
						"name":        "shell",
						"description": "run shell",
						"parameters": map[string]interface{}{
							"type": "object",
							"properties": map[string]interface{}{
								"command": map[string]interface{}{"type": "array", "items": map[string]interface{}{"type": "string"}},
							},
						},
					},
					map[string]interface{}{
						// custom tools must be ignored — not mappable to Chat Completions
						"type":        "custom",
						"name":        "exec",
						"description": "run js",
						"format":      map[string]interface{}{"type": "grammar", "syntax": "lark"},
					},
					map[string]interface{}{
						"type":        "namespace",
						"name":        "collaboration",
						"description": "agents",
						"tools": []interface{}{
							map[string]interface{}{
								"type":        "function",
								"name":        "list_agents",
								"description": "list",
								"parameters":   map[string]interface{}{"type": "object", "properties": map[string]interface{}{}},
							},
						},
					},
				},
			},
		},
		ToolChoice: "auto",
	}

	got, _, customTools := TranslateResponsesRequest(req, nil)

	if len(got.Messages) != 1 {
		t.Fatalf("messages = %#v", got.Messages)
	}
	if got.Messages[0].Role != "user" {
		t.Fatalf("role = %s", got.Messages[0].Role)
	}

	names := map[string]bool{}
	for _, tool := range got.Tools {
		names[tool.Function.Name] = true
	}
	// Custom tools are forwarded as free-form functions so the model can call them;
	// the response path rewrites them back to custom_tool_call + input.
	if !names["exec"] {
		t.Fatalf("custom tool exec must be forwarded as free-form function: %#v", names)
	}
	if !customTools["exec"] {
		t.Fatalf("exec must be marked custom: %#v", customTools)
	}
	for _, want := range []string{"shell", "collaboration__list_agents", "exec"} {
		if !names[want] {
			t.Fatalf("missing tool %q in %#v", want, names)
		}
	}
	if len(got.Tools) != 3 {
		t.Fatalf("tools count = %d names=%v", len(got.Tools), names)
	}
}

func TestExtractCustomToolInput(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{`{"input":"ls -la"}`, "ls -la"},
		{`{"input":{"cmd":"ls"}}`, `{"cmd":"ls"}`},
		{`not-json raw`, "not-json raw"},
		{`{"other":1}`, `{"other":1}`},
		{"", ""},
	}
	for _, c := range cases {
		if got := extractCustomToolInput(c.in); got != c.want {
			t.Errorf("extractCustomToolInput(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestTranslateResponsesResponseCustomTool(t *testing.T) {
	resp := &types.OpenAIResponse{
		ID:      "chatcmpl-1",
		Object:  "chat.completion",
		Created: 1,
		Model:   "gpt-test",
		Choices: []types.Choice{
			{
				Index: 0,
				Message: types.OpenAIMsg{
					Role: "assistant",
					ToolCalls: []types.ToolCall{
						{
							ID:   "call_1",
							Type: "function",
							Function: types.FunctionCall{
								Name:      "exec",
								Arguments: `{"input":"pwd"}`,
							},
						},
					},
				},
				FinishReason: "tool_calls",
			},
		},
	}
	out := TranslateResponsesResponse(resp, "resp_1", map[string]bool{"exec": true})
	if len(out.Output) != 1 {
		t.Fatalf("output len = %d", len(out.Output))
	}
	item := out.Output[0]
	if item.Type != "custom_tool_call" {
		t.Fatalf("type = %s", item.Type)
	}
	if item.Input != "pwd" {
		t.Fatalf("input = %q", item.Input)
	}
	if item.Arguments != nil {
		t.Fatalf("arguments should be nil for custom tool, got %#v", item.Arguments)
	}
	if item.Name != "exec" || item.CallID != "call_1" {
		t.Fatalf("item = %#v", item)
	}
}
