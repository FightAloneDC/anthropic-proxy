package types

// ============================================================
// OpenAI Responses API types
// ============================================================

// ResponsesRequest represents a POST /v1/responses request.
type ResponsesRequest struct {
	Model             string          `json:"model"`
	Input             interface{}     `json:"input"` // string or []InputItem
	Instructions      string          `json:"instructions,omitempty"`
	MaxOutputTokens   int             `json:"max_output_tokens,omitempty"`
	Temperature       *float64        `json:"temperature,omitempty"`
	TopP              *float64        `json:"top_p,omitempty"`
	Stream            bool            `json:"stream,omitempty"`
	StreamOptions     *StreamOptions  `json:"stream_options,omitempty"`
	Tools             []ResponsesTool `json:"tools,omitempty"`
	ToolChoice        interface{}     `json:"tool_choice,omitempty"`
	ParallelToolCalls *bool           `json:"parallel_tool_calls,omitempty"`
	Reasoning         *ReasoningConfig `json:"reasoning,omitempty"`
	Text              *TextConfig     `json:"text,omitempty"`
	Metadata          interface{}     `json:"metadata,omitempty"`
	User              string          `json:"user,omitempty"`
	PreviousResponseID string         `json:"previous_response_id,omitempty"`
}

// ReasoningConfig controls reasoning behavior for o-series models.
type ReasoningConfig struct {
	Effort  string `json:"effort,omitempty"`  // "low", "medium", "high"
	Summary string `json:"summary,omitempty"` // "auto", "detailed", "none"
}

// TextConfig controls text output format.
type TextConfig struct {
	Format   *TextFormat `json:"format,omitempty"`
	Verbosity string    `json:"verbosity,omitempty"`
}

// TextFormat specifies the output format.
type TextFormat struct {
	Type       string      `json:"type"` // "text", "json_schema", "json_object"
	Name       string      `json:"name,omitempty"`
	Schema     interface{} `json:"schema,omitempty"`
	Strict     *bool       `json:"strict,omitempty"`
}

// InputItem represents a single item in the input array.
type InputItem struct {
	Type    string      `json:"type"` // "message", "function_call", "function_call_output"
	Role    string      `json:"role,omitempty"`
	Content interface{} `json:"content,omitempty"`
	// For function_call / custom_tool_call
	CallID    string `json:"call_id,omitempty"`
	Name      string `json:"name,omitempty"`
	Arguments string `json:"arguments,omitempty"`
	// For custom_tool_call (Codex "exec" etc.)
	Input string `json:"input,omitempty"`
	// For function_call_output / custom_tool_call_output
	Output string `json:"output,omitempty"`
	// For message with status
	Status string `json:"status,omitempty"`
}

// InputContentBlock represents a content block in a message input.
type InputContentBlock struct {
	Type     string `json:"type"` // "input_text", "input_image", "input_file"
	Text     string `json:"text,omitempty"`
	ImageURL string `json:"image_url,omitempty"`
	Detail   string `json:"detail,omitempty"` // "auto", "low", "high"
	FileID   string `json:"file_id,omitempty"`
	FileData string `json:"file_data,omitempty"`
	Filename string `json:"filename,omitempty"`
}

// ResponsesTool represents a tool definition in Responses API format (flat).
type ResponsesTool struct {
	Type        string      `json:"type"` // "function"
	Name        string      `json:"name"`
	Description string      `json:"description,omitempty"`
	Parameters  interface{} `json:"parameters,omitempty"`
	Strict      *bool       `json:"strict,omitempty"`
}

// ResponsesResponse represents a /v1/responses response object.
type ResponsesResponse struct {
	ID                string               `json:"id"`
	Object            string               `json:"object"`
	CreatedAt         int64                `json:"created_at"`
	Model             string               `json:"model"`
	Status            string               `json:"status"`
	Output            []ResponseOutputItem `json:"output"`
	Usage             *ResponsesUsage      `json:"usage,omitempty"`
	IncompleteDetails interface{}          `json:"incomplete_details,omitempty"`
	Error             interface{}          `json:"error,omitempty"`
}

// ResponseOutputItem represents an item in the output array.
//
// Codex/OpenAI Responses clients require:
//   - message items: content is a Vec (not omitted while streaming starts)
//   - function_call items: arguments is always present (may be "")
// Empty OutputContentBlock.Text must still serialize as "text":""
// (ContentItem::OutputText requires the field).
//
// Arguments is a pointer so omitempty drops it on message items, while
// function_call can send a non-nil pointer to "" (still serialized).
//
// Input is used by custom_tool_call (Codex "exec" etc.); omitempty keeps it
// off message/function_call items.
type ResponseOutputItem struct {
	Type    string               `json:"type"` // "message", "function_call", or "custom_tool_call"
	ID      string               `json:"id,omitempty"`
	Role    string               `json:"role,omitempty"`
	Content []OutputContentBlock `json:"content,omitempty"`
	// Status is not part of Codex ResponseItem::Message, but harmless if present.
	Status string `json:"status,omitempty"`
	// For function_call / custom_tool_call
	CallID    string  `json:"call_id,omitempty"`
	Name      string  `json:"name,omitempty"`
	Arguments *string `json:"arguments,omitempty"`
	// For custom_tool_call — free-form string payload (not JSON arguments)
	Input string `json:"input,omitempty"`
}

// OutputContentBlock represents a content block in output.
// Text is never omitempty: serde ContentItem::OutputText requires "text".
type OutputContentBlock struct {
	Type string `json:"type"` // "output_text", "refusal"
	Text string `json:"text"`
}

// ResponsesUsage represents token usage in Responses API format.
type ResponsesUsage struct {
	InputTokens         int                  `json:"input_tokens"`
	InputTokensDetails  *InputTokensDetails  `json:"input_tokens_details,omitempty"`
	OutputTokens        int                  `json:"output_tokens"`
	OutputTokensDetails *OutputTokensDetails `json:"output_tokens_details,omitempty"`
	TotalTokens         int                  `json:"total_tokens"`
}

// InputTokensDetails contains details about input token usage.
type InputTokensDetails struct {
	CachedTokens int `json:"cached_tokens"`
}

// OutputTokensDetails contains details about output token usage.
type OutputTokensDetails struct {
	ReasoningTokens int `json:"reasoning_tokens"`
}

// ============================================================
// Responses API SSE event types
// ============================================================

// ResponsesEvent is the base type for all Responses API streaming events.
type ResponsesEvent struct {
	Type string `json:"type"`
}

// ResponseCreatedEvent is emitted when a response is created.
type ResponseCreatedEvent struct {
	Type     string              `json:"type"` // "response.created"
	Response *ResponsesResponse  `json:"response"`
}

// ResponseCompletedEvent is emitted when a response is complete.
type ResponseCompletedEvent struct {
	Type     string              `json:"type"` // "response.completed"
	Response *ResponsesResponse  `json:"response"`
}

// ResponseOutputItemAddedEvent is emitted when an output item starts.
type ResponseOutputItemAddedEvent struct {
	Type        string              `json:"type"` // "response.output_item.added"
	OutputIndex int                 `json:"output_index"`
	Item        *ResponseOutputItem `json:"item"`
}

// ResponseOutputItemDoneEvent is emitted when an output item is complete.
type ResponseOutputItemDoneEvent struct {
	Type        string              `json:"type"` // "response.output_item.done"
	OutputIndex int                 `json:"output_index"`
	Item        *ResponseOutputItem `json:"item"`
}

// ResponseContentPartAddedEvent is emitted when a content part starts.
type ResponseContentPartAddedEvent struct {
	Type         string              `json:"type"` // "response.content_part.added"
	OutputIndex  int                 `json:"output_index"`
	ContentIndex int                 `json:"content_index"`
	ItemID       string              `json:"item_id,omitempty"`
	Part         *OutputContentBlock `json:"part"`
}

// ResponseContentPartDoneEvent is emitted when a content part is done.
type ResponseContentPartDoneEvent struct {
	Type         string              `json:"type"` // "response.content_part.done"
	OutputIndex  int                 `json:"output_index"`
	ContentIndex int                 `json:"content_index"`
	ItemID       string              `json:"item_id,omitempty"`
	Part         *OutputContentBlock `json:"part"`
}

// ResponseOutputTextDeltaEvent streams text content.
type ResponseOutputTextDeltaEvent struct {
	Type         string `json:"type"` // "response.output_text.delta"
	OutputIndex  int    `json:"output_index"`
	ContentIndex int    `json:"content_index"`
	ItemID       string `json:"item_id,omitempty"`
	Delta        string `json:"delta"`
}

// ResponseOutputTextDoneEvent signals text content is done.
type ResponseOutputTextDoneEvent struct {
	Type         string `json:"type"` // "response.output_text.done"
	OutputIndex  int    `json:"output_index"`
	ContentIndex int    `json:"content_index"`
	ItemID       string `json:"item_id,omitempty"`
	Text         string `json:"text"`
}

// ResponseFunctionCallArgumentsDeltaEvent streams function call arguments.
type ResponseFunctionCallArgumentsDeltaEvent struct {
	Type        string `json:"type"` // "response.function_call_arguments.delta"
	OutputIndex int    `json:"output_index"`
	ItemID      string `json:"item_id,omitempty"`
	Delta       string `json:"delta"`
}

// ResponseFunctionCallArgumentsDoneEvent signals function call arguments are done.
type ResponseFunctionCallArgumentsDoneEvent struct {
	Type        string `json:"type"` // "response.function_call_arguments.done"
	OutputIndex int    `json:"output_index"`
	ItemID      string `json:"item_id,omitempty"`
	Arguments   string `json:"arguments"`
}

// ResponseCustomToolCallInputDeltaEvent streams a partial custom tool input.
type ResponseCustomToolCallInputDeltaEvent struct {
	Type        string `json:"type"` // "response.custom_tool_call_input.delta"
	OutputIndex int    `json:"output_index"`
	ItemID      string `json:"item_id,omitempty"`
	Delta       string `json:"delta"`
}

// ResponseCustomToolCallInputDoneEvent signals custom tool input is complete.
type ResponseCustomToolCallInputDoneEvent struct {
	Type        string `json:"type"` // "response.custom_tool_call_input.done"
	OutputIndex int    `json:"output_index"`
	ItemID      string `json:"item_id,omitempty"`
	Input       string `json:"input"`
}

// ResponseReasoningTextDeltaEvent streams reasoning text.
type ResponseReasoningTextDeltaEvent struct {
	Type         string `json:"type"` // "response.reasoning_text.delta"
	OutputIndex  int    `json:"output_index"`
	ContentIndex int    `json:"content_index"`
	ItemID       string `json:"item_id,omitempty"`
	Delta        string `json:"delta"`
}

// ResponseReasoningTextDoneEvent signals reasoning text is done.
type ResponseReasoningTextDoneEvent struct {
	Type         string `json:"type"` // "response.reasoning_text.done"
	OutputIndex  int    `json:"output_index"`
	ContentIndex int    `json:"content_index"`
	ItemID       string `json:"item_id,omitempty"`
}
