package handler

import (
	"fmt"

	"anthropic-proxy/internal/types"
)

func validateAnthropicRequest(req *types.AnthropicRequest) error {
	if req.Model == "" {
		return fmt.Errorf("model is required")
	}
	if req.MaxTokens <= 0 {
		return fmt.Errorf("max_tokens must be greater than 0")
	}
	if len(req.Messages) == 0 {
		return fmt.Errorf("messages is required")
	}
	for _, msg := range req.Messages {
		if msg.Role != "user" && msg.Role != "assistant" {
			return fmt.Errorf("message role must be user or assistant")
		}
	}
	if req.Temperature != nil && (*req.Temperature < 0 || *req.Temperature > 2) {
		return fmt.Errorf("temperature must be between 0 and 2")
	}
	if req.TopP != nil && (*req.TopP < 0 || *req.TopP > 1) {
		return fmt.Errorf("top_p must be between 0 and 1")
	}
	if req.TopK != nil && *req.TopK < 0 {
		return fmt.Errorf("top_k must be non-negative")
	}
	return nil
}

func validateResponsesRequest(req *types.ResponsesRequest) error {
	if req.Model == "" {
		return fmt.Errorf("model is required")
	}
	if req.Input == nil {
		return fmt.Errorf("input is required")
	}
	if req.MaxOutputTokens < 0 {
		return fmt.Errorf("max_output_tokens must be non-negative")
	}
	if req.Temperature != nil && (*req.Temperature < 0 || *req.Temperature > 2) {
		return fmt.Errorf("temperature must be between 0 and 2")
	}
	if req.TopP != nil && (*req.TopP < 0 || *req.TopP > 1) {
		return fmt.Errorf("top_p must be between 0 and 1")
	}
	return nil
}

func validateGeminiRequest(req *types.GeminiRequest) error {
	if len(req.Contents) == 0 {
		return fmt.Errorf("contents is required")
	}
	for _, content := range req.Contents {
		if len(content.Parts) == 0 {
			return fmt.Errorf("content parts is required")
		}
	}
	if req.GenerationConfig != nil {
		cfg := req.GenerationConfig
		if cfg.MaxOutputTokens < 0 {
			return fmt.Errorf("generationConfig.maxOutputTokens must be non-negative")
		}
		if cfg.Temperature != nil && (*cfg.Temperature < 0 || *cfg.Temperature > 2) {
			return fmt.Errorf("generationConfig.temperature must be between 0 and 2")
		}
		if cfg.TopP != nil && (*cfg.TopP < 0 || *cfg.TopP > 1) {
			return fmt.Errorf("generationConfig.topP must be between 0 and 1")
		}
		if cfg.TopK != nil && *cfg.TopK < 0 {
			return fmt.Errorf("generationConfig.topK must be non-negative")
		}
	}
	return nil
}

func validateGeminiEmbedContentRequest(req *types.GeminiEmbedContentRequest) error {
	for _, part := range req.Content.Parts {
		if part.Text != "" {
			if req.OutputDimensionality < 0 {
				return fmt.Errorf("outputDimensionality must be non-negative")
			}
			return nil
		}
	}
	return fmt.Errorf("content.parts must include text")
}
