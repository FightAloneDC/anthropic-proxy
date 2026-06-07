package translator

import (
	"strings"

	"anthropic-proxy/internal/types"
)

// TranslateGeminiEmbedContentRequest converts Gemini embedContent to OpenAI embeddings format.
func TranslateGeminiEmbedContentRequest(req *types.GeminiEmbedContentRequest, model string) *types.OpenAIEmbeddingsRequest {
	oai := &types.OpenAIEmbeddingsRequest{
		Model:      model,
		Input:      normalizeEmbeddingInput(geminiTextParts(req.Content.Parts)),
		Dimensions: req.OutputDimensionality,
	}
	return oai
}

// TranslateGeminiEmbedContentResponse converts OpenAI embeddings response to Gemini embedContent format.
func TranslateGeminiEmbedContentResponse(resp *types.OpenAIEmbeddingsResponse) *types.GeminiEmbedContentResponse {
	gr := &types.GeminiEmbedContentResponse{}
	if len(resp.Data) > 0 {
		gr.Embedding.Values = resp.Data[0].Embedding
	}
	return gr
}

func normalizeEmbeddingInput(text string) string {
	return strings.TrimSpace(text)
}
