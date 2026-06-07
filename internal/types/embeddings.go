package types

// ============================================================
// OpenAI Embeddings API types
// ============================================================

type OpenAIEmbeddingsRequest struct {
	Model      string      `json:"model"`
	Input      interface{} `json:"input"`
	Dimensions int         `json:"dimensions,omitempty"`
	User       string      `json:"user,omitempty"`
}

type OpenAIEmbeddingsResponse struct {
	Object string                  `json:"object"`
	Data   []OpenAIEmbeddingObject `json:"data"`
	Model  string                  `json:"model,omitempty"`
	Usage  *OpenAIEmbeddingUsage   `json:"usage,omitempty"`
}

type OpenAIEmbeddingObject struct {
	Object    string    `json:"object"`
	Embedding []float64 `json:"embedding"`
	Index     int       `json:"index"`
}

type OpenAIEmbeddingUsage struct {
	PromptTokens int `json:"prompt_tokens"`
	TotalTokens  int `json:"total_tokens"`
}
