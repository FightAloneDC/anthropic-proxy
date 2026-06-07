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

	got, _ := TranslateResponsesRequest(req, nil)

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

	got, _ := TranslateResponsesRequest(req, nil)

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
