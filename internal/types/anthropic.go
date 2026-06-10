package types

// ============================================================
// Anthropic Messages API types
// ============================================================

type AnthropicRequest struct {
	Model          string             `json:"model"`
	MaxTokens      int                `json:"max_tokens"`
	Messages       []AnthropicMsg     `json:"messages"`
	System         interface{}        `json:"system,omitempty"`
	Stream         bool               `json:"stream,omitempty"`
	Temperature    *float64           `json:"temperature,omitempty"`
	TopP           *float64           `json:"top_p,omitempty"`
	TopK           *int               `json:"top_k,omitempty"`
	StopSequences  []string           `json:"stop_sequences,omitempty"`
	Tools          []AnthropicTool    `json:"tools,omitempty"`
	ToolChoice     interface{}        `json:"tool_choice,omitempty"`
	Thinking       *ThinkingConfig    `json:"thinking,omitempty"`
	Metadata       *AnthropicMetadata `json:"metadata,omitempty"`
	ResponseFormat interface{}        `json:"response_format,omitempty"`
}

type ThinkingConfig struct {
	Type         string `json:"type"`                    // "enabled", "disabled", or "adaptive"
	BudgetTokens *int   `json:"budget_tokens,omitempty"` // required when type="enabled"
}

type AnthropicMetadata struct {
	UserID string `json:"user_id,omitempty"`
}

type AnthropicMsg struct {
	Role    string      `json:"role"`
	Content interface{} `json:"content"` // string or []ContentBlock
}

type ContentBlock struct {
	Type         string      `json:"type"`
	Text         string      `json:"text,omitempty"`
	ID           string      `json:"id,omitempty"`
	Name         string      `json:"name,omitempty"`
	Input        interface{} `json:"input,omitempty"`
	ToolUseID    string      `json:"tool_use_id,omitempty"`
	Content      interface{} `json:"content,omitempty"`
	IsError      bool        `json:"is_error,omitempty"`
	Source       interface{} `json:"source,omitempty"`
	Thinking     string      `json:"thinking,omitempty"`
	Signature    string      `json:"signature,omitempty"`
	CacheControl interface{} `json:"cache_control,omitempty"`
}

type AnthropicTool struct {
	Name        string      `json:"name"`
	Description string      `json:"description,omitempty"`
	Type        string      `json:"type,omitempty"` // "custom" (optional)
	InputSchema interface{} `json:"input_schema"`
}

// Anthropic response (non-streaming)
type AnthropicResponse struct {
	ID           string         `json:"id"`
	Type         string         `json:"type"`
	Role         string         `json:"role"`
	Model        string         `json:"model"`
	Content      []ContentBlock `json:"content"`
	StopReason   string         `json:"stop_reason"`
	StopSequence *string        `json:"stop_sequence"`
	Usage        AnthropicUsage `json:"usage"`
}

type AnthropicUsage struct {
	InputTokens              int `json:"input_tokens"`
	OutputTokens             int `json:"output_tokens"`
	CacheCreationInputTokens int `json:"cache_creation_input_tokens,omitempty"`
	CacheReadInputTokens     int `json:"cache_read_input_tokens,omitempty"`
}

// Anthropic SSE event types
type EventMessageStart struct {
	Type    string            `json:"type"`
	Message AnthropicResponse `json:"message"`
}

type EventContentBlockStart struct {
	Type         string       `json:"type"`
	Index        int          `json:"index"`
	ContentBlock ContentBlock `json:"content_block"`
}

type EventContentBlockDelta struct {
	Type  string      `json:"type"`
	Index int         `json:"index"`
	Delta interface{} `json:"delta"`
}

type TextDelta struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type InputJSONDelta struct {
	Type        string `json:"type"`
	PartialJSON string `json:"partial_json"`
}

type ThinkingDelta struct {
	Type     string `json:"type"`
	Thinking string `json:"thinking"`
}

type EventContentBlockStop struct {
	Type  string `json:"type"`
	Index int    `json:"index"`
}

type EventMessageDelta struct {
	Type  string         `json:"type"`
	Delta MessageDelta   `json:"delta"`
	Usage AnthropicUsage `json:"usage"`
}

type MessageDelta struct {
	StopReason   string  `json:"stop_reason"`
	StopSequence *string `json:"stop_sequence"`
}

type EventMessageStop struct {
	Type string `json:"type"`
}
