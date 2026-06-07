package translator

import (
	"reflect"
	"testing"

	"anthropic-proxy/internal/types"
)

func TestTranslateGeminiEmbedContentRequest(t *testing.T) {
	req := &types.GeminiEmbedContentRequest{
		Content:              types.GeminiContent{Parts: []types.GeminiPart{{Text: "hello"}, {Text: "world"}}},
		OutputDimensionality: 768,
		TaskType:             "RETRIEVAL_DOCUMENT",
		Title:                "ignored",
	}

	got := TranslateGeminiEmbedContentRequest(req, "embedding-model")

	if got.Model != "embedding-model" {
		t.Fatalf("model = %q", got.Model)
	}
	if got.Input != "hello\nworld" {
		t.Fatalf("input = %#v", got.Input)
	}
	if got.Dimensions != 768 {
		t.Fatalf("dimensions = %d", got.Dimensions)
	}
}

func TestTranslateGeminiEmbedContentResponse(t *testing.T) {
	resp := &types.OpenAIEmbeddingsResponse{Data: []types.OpenAIEmbeddingObject{{Embedding: []float64{0.1, 0.2, 0.3}}}}

	got := TranslateGeminiEmbedContentResponse(resp)

	want := []float64{0.1, 0.2, 0.3}
	if !reflect.DeepEqual(got.Embedding.Values, want) {
		t.Fatalf("values = %#v", got.Embedding.Values)
	}
}

func TestTranslateGeminiEmbedContentResponseEmptyData(t *testing.T) {
	got := TranslateGeminiEmbedContentResponse(&types.OpenAIEmbeddingsResponse{})

	if len(got.Embedding.Values) != 0 {
		t.Fatalf("values = %#v", got.Embedding.Values)
	}
}
