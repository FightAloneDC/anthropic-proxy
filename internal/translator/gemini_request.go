package translator

import (
	"encoding/json"
	"fmt"
	"strings"

	"anthropic-proxy/internal/types"
)

// TranslateGeminiRequest converts a Gemini generateContent request to OpenAI Chat Completions format.
func TranslateGeminiRequest(req *types.GeminiRequest, model string, stream bool) *types.OpenAIRequest {
	oai := &types.OpenAIRequest{
		Model:  model,
		Stream: stream,
	}
	if stream {
		oai.StreamOptions = &types.StreamOptions{IncludeUsage: true}
	}

	if req.GenerationConfig != nil {
		oai.MaxTokens = req.GenerationConfig.MaxOutputTokens
		oai.Temperature = req.GenerationConfig.Temperature
		oai.TopP = req.GenerationConfig.TopP
		oai.TopK = req.GenerationConfig.TopK
		if len(req.GenerationConfig.StopSequences) > 0 {
			oai.Stop = req.GenerationConfig.StopSequences
		}
		if req.GenerationConfig.ResponseMIMEType == "application/json" {
			if req.GenerationConfig.ResponseSchema != nil {
				oai.ResponseFormat = map[string]interface{}{
					"type": "json_schema",
					"json_schema": map[string]interface{}{
						"name":   "response",
						"schema": req.GenerationConfig.ResponseSchema,
					},
				}
			} else {
				oai.ResponseFormat = map[string]string{"type": "json_object"}
			}
		}
	}

	if req.SystemInstruction != nil {
		if text := geminiTextParts(req.SystemInstruction.Parts); text != "" {
			oai.Messages = append(oai.Messages, types.OpenAIMsg{Role: "system", Content: text})
		}
	}

	for _, content := range req.Contents {
		oai.Messages = append(oai.Messages, translateGeminiContent(content)...)
	}

	for _, tool := range req.Tools {
		for _, fn := range tool.FunctionDeclarations {
			oai.Tools = append(oai.Tools, types.OpenAITool{
				Type: "function",
				Function: types.ToolFunction{
					Name:        fn.Name,
					Description: fn.Description,
					Parameters:  fn.Parameters,
				},
			})
		}
	}

	return oai
}

func translateGeminiContent(content types.GeminiContent) []types.OpenAIMsg {
	role := geminiRoleToOpenAI(content.Role)
	oaiMsg := types.OpenAIMsg{Role: role}
	var toolMessages []types.OpenAIMsg
	var textParts []string
	var multiParts []interface{}

	for i, part := range content.Parts {
		if part.Text != "" {
			textParts = append(textParts, part.Text)
			multiParts = append(multiParts, map[string]interface{}{"type": "text", "text": part.Text})
		}

		if part.InlineData != nil && part.InlineData.Data != "" {
			multiParts = append(multiParts, map[string]interface{}{
				"type": "image_url",
				"image_url": map[string]string{
					"url": fmt.Sprintf("data:%s;base64,%s", part.InlineData.MimeType, part.InlineData.Data),
				},
			})
		}

		if part.FileData != nil && part.FileData.FileURI != "" {
			multiParts = append(multiParts, map[string]interface{}{
				"type":      "image_url",
				"image_url": map[string]string{"url": part.FileData.FileURI},
			})
		}

		if part.FunctionCall != nil {
			args, _ := json.Marshal(part.FunctionCall.Args)
			if string(args) == "null" {
				args = []byte("{}")
			}
			oaiMsg.ToolCalls = append(oaiMsg.ToolCalls, types.ToolCall{
				ID:   fmt.Sprintf("call_%s_%d", part.FunctionCall.Name, i),
				Type: "function",
				Function: types.FunctionCall{
					Name:      part.FunctionCall.Name,
					Arguments: string(args),
				},
			})
		}

		if part.FunctionResponse != nil {
			body, _ := json.Marshal(part.FunctionResponse.Response)
			if string(body) == "null" {
				body = []byte("{}")
			}
			toolMessages = append(toolMessages, types.OpenAIMsg{
				Role:       "tool",
				ToolCallID: fmt.Sprintf("call_%s_%d", part.FunctionResponse.Name, i),
				Content:    string(body),
			})
		}
	}

	if len(multiParts) > len(textParts) {
		oaiMsg.Content = multiParts
	} else if len(textParts) > 0 {
		oaiMsg.Content = strings.Join(textParts, "\n")
	}

	var result []types.OpenAIMsg
	if oaiMsg.Content != nil || len(oaiMsg.ToolCalls) > 0 {
		result = append(result, oaiMsg)
	}
	result = append(result, toolMessages...)
	return result
}

func geminiRoleToOpenAI(role string) string {
	switch role {
	case "model":
		return "assistant"
	case "function":
		return "tool"
	case "user", "assistant", "system", "tool":
		return role
	default:
		return "user"
	}
}

func geminiTextParts(parts []types.GeminiPart) string {
	var texts []string
	for _, part := range parts {
		if part.Text != "" {
			texts = append(texts, part.Text)
		}
	}
	return strings.Join(texts, "\n")
}
