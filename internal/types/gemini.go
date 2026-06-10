package types

// ============================================================
// Gemini API types
// ============================================================

type GeminiRequest struct {
	Contents          []GeminiContent         `json:"contents"`
	SystemInstruction *GeminiContent          `json:"systemInstruction,omitempty"`
	Tools             []GeminiTool            `json:"tools,omitempty"`
	GenerationConfig  *GeminiGenerationConfig `json:"generationConfig,omitempty"`
	SafetySettings    []GeminiSafetySetting   `json:"safetySettings,omitempty"`
	CachedContent     string                  `json:"cachedContent,omitempty"`
}

type GeminiContent struct {
	Role  string       `json:"role,omitempty"`
	Parts []GeminiPart `json:"parts"`
}

type GeminiPart struct {
	Text             string                  `json:"text,omitempty"`
	Thought          bool                    `json:"thought,omitempty"`
	InlineData       *GeminiInlineData       `json:"inlineData,omitempty"`
	FileData         *GeminiFileData         `json:"fileData,omitempty"`
	FunctionCall     *GeminiFunctionCall     `json:"functionCall,omitempty"`
	FunctionResponse *GeminiFunctionResponse `json:"functionResponse,omitempty"`
}

type GeminiInlineData struct {
	MimeType string `json:"mimeType"`
	Data     string `json:"data"`
}

type GeminiFileData struct {
	MimeType string `json:"mimeType,omitempty"`
	FileURI  string `json:"fileUri"`
}

type GeminiFunctionCall struct {
	Name string      `json:"name"`
	Args interface{} `json:"args,omitempty"`
}

type GeminiFunctionResponse struct {
	Name     string      `json:"name"`
	Response interface{} `json:"response"`
}

type GeminiTool struct {
	FunctionDeclarations []GeminiFunctionDeclaration `json:"functionDeclarations,omitempty"`
}

type GeminiFunctionDeclaration struct {
	Name        string      `json:"name"`
	Description string      `json:"description,omitempty"`
	Parameters  interface{} `json:"parameters,omitempty"`
}

type GeminiGenerationConfig struct {
	Temperature      *float64    `json:"temperature,omitempty"`
	TopP             *float64    `json:"topP,omitempty"`
	TopK             *int        `json:"topK,omitempty"`
	MaxOutputTokens  int         `json:"maxOutputTokens,omitempty"`
	StopSequences    []string    `json:"stopSequences,omitempty"`
	ResponseMIMEType string      `json:"responseMimeType,omitempty"`
	ResponseSchema   interface{} `json:"responseSchema,omitempty"`
}

type GeminiSafetySetting struct {
	Category  string `json:"category"`
	Threshold string `json:"threshold"`
}

type GeminiResponse struct {
	Candidates     []GeminiCandidate    `json:"candidates,omitempty"`
	UsageMetadata  *GeminiUsageMetadata `json:"usageMetadata,omitempty"`
	PromptFeedback interface{}          `json:"promptFeedback,omitempty"`
}

type GeminiCandidate struct {
	Content       *GeminiContent `json:"content,omitempty"`
	FinishReason  string         `json:"finishReason,omitempty"`
	Index         int            `json:"index,omitempty"`
	SafetyRatings interface{}    `json:"safetyRatings,omitempty"`
}

type GeminiUsageMetadata struct {
	PromptTokenCount     int `json:"promptTokenCount,omitempty"`
	CandidatesTokenCount int `json:"candidatesTokenCount,omitempty"`
	TotalTokenCount      int `json:"totalTokenCount,omitempty"`
}

type GeminiEmbedContentRequest struct {
	Content              GeminiContent `json:"content"`
	TaskType             string        `json:"taskType,omitempty"`
	Title                string        `json:"title,omitempty"`
	OutputDimensionality int           `json:"outputDimensionality,omitempty"`
}

type GeminiEmbedContentResponse struct {
	Embedding GeminiEmbedding `json:"embedding"`
}

type GeminiEmbedding struct {
	Values []float64 `json:"values"`
}
