package translator

import (
	"regexp"
	"strings"
)

// Regex patterns for thinking tags
var (
	// Matches <think>...</think> or <thinking>...</thinking> (non-greedy)
	thinkingTagRegex = regexp.MustCompile(`(?s)<think>.*?</think>|<thinking>.*?</thinking>`)
	// Matches standalone closing tags
	closingTagRegex = regexp.MustCompile(`(?s)</think>|</thinking>`)
	// Matches standalone opening tags
	openingTagRegex = regexp.MustCompile(`(?s)<think>|<thinking>`)
)

// NormalizeContent extracts thinking tags from content and returns
// the cleaned content and extracted reasoning.
// Handles both `<think>` and `<thinking>` tags.
//
// Case 1: If reasoningContent is already provided by backend,
// only remove thinking tags from content.
//
// Case 2: If reasoningContent is empty,
// extract thinking tags to reasoningContent and clean content.
func NormalizeContent(content string, reasoningContent string) (cleanContent string, cleanReasoning string) {
	if content == "" {
		return content, reasoningContent
	}

	// Extract thinking tags from content
	extractedReasoning, remainingContent := extractThinkingTags(content)

	if reasoningContent != "" {
		// Case 1: Backend already has reasoning_content
		// Just remove thinking tags from content
		return remainingContent, reasoningContent
	}

	// Case 2: No reasoning_content from backend
	// Use extracted reasoning from thinking tags
	return remainingContent, extractedReasoning
}

// extractThinkingTags extracts content between thinking tags using regex.
// Returns extracted reasoning and remaining content without tags.
func extractThinkingTags(content string) (reasoning string, cleanContent string) {
	// Find all thinking tag blocks
	matches := thinkingTagRegex.FindAllString(content, -1)
	if len(matches) == 0 {
		// No complete tags - check for standalone tags
		cleanContent = removeStandaloneTags(content)
		return "", cleanContent
	}

	// Extract reasoning from each match
	var reasoningParts []string
	for _, match := range matches {
		// Remove opening and closing tags to get content
		text := match
		text = strings.TrimPrefix(text, "<think>")
		text = strings.TrimPrefix(text, "<thinking>")
		text = strings.TrimSuffix(text, "</think>")
		text = strings.TrimSuffix(text, "</thinking>")
		text = strings.TrimSpace(text)
		if text != "" {
			reasoningParts = append(reasoningParts, text)
		}
	}

	// Remove all thinking tag blocks from content
	cleanContent = thinkingTagRegex.ReplaceAllString(content, "")
	// Also remove any standalone tags
	cleanContent = removeStandaloneTags(cleanContent)
	cleanContent = strings.TrimSpace(cleanContent)

	if len(reasoningParts) > 0 {
		reasoning = strings.Join(reasoningParts, "\n")
	}

	return reasoning, cleanContent
}

// removeStandaloneTags removes any standalone thinking tags.
func removeStandaloneTags(content string) string {
	content = closingTagRegex.ReplaceAllString(content, "")
	content = openingTagRegex.ReplaceAllString(content, "")
	return strings.TrimSpace(content)
}

// StreamNormalizer processes streaming chunks for thinking tags.
// It maintains state across chunks to handle tags split across chunk boundaries.
type StreamNormalizer struct {
	inThinkTag     bool
	tagType        string // "think" or "thinking"
	tagBuffer      string // buffered content when inside a thinking tag
	emitContent    func(content string)
	emitReasoning  func(reasoning string)
	contentBuffer  string // buffer content to ensure reasoning is emitted first
	hasReasoning   bool   // track if we've seen reasoning in this stream
	reasoningAccum string // accumulated reasoning to detect duplicates
}

// NewStreamNormalizer creates a new stream normalizer.
func NewStreamNormalizer(
	emitContent func(string),
	emitReasoning func(string),
) *StreamNormalizer {
	return &StreamNormalizer{
		emitContent:   emitContent,
		emitReasoning: emitReasoning,
	}
}

// flushContentBuffer emits any buffered content.
func (sn *StreamNormalizer) flushContentBuffer() {
	if sn.contentBuffer != "" {
		sn.emitContent(sn.contentBuffer)
		sn.contentBuffer = ""
	}
}

// ProcessChunk processes a single chunk and emits normalized content/reasoning.
func (sn *StreamNormalizer) ProcessChunk(content string, reasoningContent string) {
	if content == "" && reasoningContent == "" {
		return
	}

	// If backend provides reasoning_content, emit it immediately
	if reasoningContent != "" {
		sn.hasReasoning = true
		sn.reasoningAccum += reasoningContent
		sn.emitReasoning(reasoningContent)
	}

	// Process content through stateful parser (handles thinking tags)
	if content != "" {
		sn.processContentChunk(content)
	}
}

// processContentChunk handles the stateful parsing of thinking tags in content chunks.
func (sn *StreamNormalizer) processContentChunk(chunk string) {
	remaining := chunk

	for len(remaining) > 0 {
		if sn.inThinkTag {
			// Look for closing tag
			var endTag string
			if sn.tagType == "think" {
				endTag = "</think>"
			} else {
				endTag = "</thinking>"
			}

			endIdx := strings.Index(remaining, endTag)
			if endIdx == -1 {
				// No closing tag in this chunk - buffer everything
				sn.tagBuffer += remaining
				return
			}

			// Found closing tag
			sn.tagBuffer += remaining[:endIdx]
			sn.hasReasoning = true
			// Flush any buffered content before emitting reasoning
			sn.flushContentBuffer()
			sn.emitReasoning(sn.tagBuffer)
			sn.tagBuffer = ""
			sn.inThinkTag = false
			remaining = remaining[endIdx+len(endTag):]
		} else {
			// Remove standalone closing tags first
			if closingTagRegex.MatchString(remaining) {
				// Find first standalone closing tag
				loc := closingTagRegex.FindStringIndex(remaining)
				if loc != nil && loc[0] == 0 {
					// Closing tag at start - remove it
					remaining = remaining[loc[1]:]
					continue
				} else if loc != nil {
					// Buffer content before closing tag
					sn.contentBuffer += remaining[:loc[0]]
					remaining = remaining[loc[1]:]
					continue
				}
			}

			// Look for opening tags
			thinkStart := strings.Index(remaining, "<think>")
			thinkTagStart := strings.Index(remaining, "<thinking>")

			var tagType string
			var startIdx int

			if thinkStart == -1 && thinkTagStart == -1 {
				// No tags found - buffer content
				sn.contentBuffer += remaining
				return
			} else if thinkStart == -1 {
				tagType = "thinking"
				startIdx = thinkTagStart
			} else if thinkTagStart == -1 {
				tagType = "think"
				startIdx = thinkStart
			} else if thinkStart < thinkTagStart {
				tagType = "think"
				startIdx = thinkStart
			} else {
				tagType = "thinking"
				startIdx = thinkTagStart
			}

			// Buffer content before tag
			if startIdx > 0 {
				sn.contentBuffer += remaining[:startIdx]
			}

			// Check if closing tag is in same chunk
			var endTag string
			if tagType == "think" {
				endTag = "</think>"
			} else {
				endTag = "</thinking>"
			}

			tagContent := remaining[startIdx+len("<"+tagType+">"):]
			endIdx := strings.Index(tagContent, endTag)

			if endIdx == -1 {
				// Closing tag not in this chunk
				sn.inThinkTag = true
				sn.tagType = tagType
				sn.tagBuffer = tagContent
				return
			}

			// Both tags in same chunk
			sn.hasReasoning = true
			// Flush any buffered content before emitting reasoning
			sn.flushContentBuffer()
			sn.emitReasoning(tagContent[:endIdx])
			remaining = tagContent[endIdx+len(endTag):]
		}
	}
}

// Flush should be called at the end of stream to emit any remaining buffered content.
func (sn *StreamNormalizer) Flush() {
	sn.flushContentBuffer()
}
