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
	openTagBuffer  string // buffer for incomplete opening tags
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

// findClosingTag finds the first closing tag (either <think> or <thinking>) in the string.
// Returns the index and the length of the closing tag found.
func findClosingTag(s string) (idx int, length int) {
	thinkEnd := strings.Index(s, "</think>")
	thinkTagEnd := strings.Index(s, "</thinking>")

	if thinkEnd == -1 && thinkTagEnd == -1 {
		return -1, 0
	} else if thinkEnd == -1 {
		return thinkTagEnd, len("</thinking>")
	} else if thinkTagEnd == -1 {
		return thinkEnd, len("</think>")
	} else if thinkEnd < thinkTagEnd {
		return thinkEnd, len("</think>")
	}
	return thinkTagEnd, len("</thinking>")
}

// findOpeningTag finds the first opening tag in the string.
// Returns the index, tag type ("think" or "thinking"), and whether it was found.
func findOpeningTag(s string) (idx int, tagType string, found bool) {
	thinkIdx := strings.Index(s, "<think>")
	thinkTagIdx := strings.Index(s, "<thinking>")

	if thinkIdx == -1 && thinkTagIdx == -1 {
		return -1, "", false
	} else if thinkIdx == -1 {
		return thinkTagIdx, "thinking", true
	} else if thinkTagIdx == -1 {
		return thinkIdx, "think", true
	} else if thinkIdx < thinkTagIdx {
		return thinkIdx, "think", true
	}
	return thinkTagIdx, "thinking", true
}

// isStandaloneClosingTag checks if the string starts with a closing tag
// that has no matching opening tag before it.
func isStandaloneClosingTag(s string) (bool, int) {
	thinkEnd := strings.Index(s, "</think>")
	thinkTagEnd := strings.Index(s, "</thinking>")

	var idx int
	var tagLen int

	if thinkEnd == -1 && thinkTagEnd == -1 {
		return false, 0
	} else if thinkEnd == -1 {
		idx = thinkTagEnd
		tagLen = len("</thinking>")
	} else if thinkTagEnd == -1 {
		idx = thinkEnd
		tagLen = len("</think>")
	} else if thinkEnd < thinkTagEnd {
		idx = thinkEnd
		tagLen = len("</think>")
	} else {
		idx = thinkTagEnd
		tagLen = len("</thinking>")
	}

	// Check if there's an opening tag before this closing tag
	thinkStart := strings.Index(s, "<think>")
	thinkTagStart := strings.Index(s, "<thinking>")

	hasOpeningBefore := (thinkStart != -1 && thinkStart < idx) || (thinkTagStart != -1 && thinkTagStart < idx)
	if hasOpeningBefore {
		return false, 0
	}

	return true, idx + tagLen
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
	// Prepend any buffered incomplete opening tag
	remaining := sn.openTagBuffer + chunk
	sn.openTagBuffer = ""

	for len(remaining) > 0 {
		if sn.inThinkTag {
			// Look for ANY closing tag (handle mismatched tags)
			endIdx, endLen := findClosingTag(remaining)
			if endIdx == -1 {
				// No closing tag in this chunk - buffer everything
				sn.tagBuffer += remaining
				return
			}

			// Found closing tag
			sn.tagBuffer += remaining[:endIdx]
			// Only emit reasoning from tags if we haven't already emitted from reasoning field
			if !sn.hasReasoning {
				sn.flushContentBuffer()
				sn.emitReasoning(sn.tagBuffer)
			}
			sn.tagBuffer = ""
			sn.inThinkTag = false
			remaining = remaining[endIdx+endLen:]
		} else {
			// Remove standalone closing tags first
			if standalone, endPos := isStandaloneClosingTag(remaining); standalone {
				remaining = remaining[endPos:]
				continue
			}

			// Look for opening tags
			startIdx, tagType, found := findOpeningTag(remaining)
			if !found {
				// Check if remaining ends with incomplete opening tag
				if hasIncompleteOpenTag(remaining) {
					sn.openTagBuffer = remaining
					return
				}
				// No tags found - emit content immediately
				sn.flushContentBuffer()
				sn.emitContent(remaining)
				return
			}

			// Emit content before tag
			if startIdx > 0 {
				sn.flushContentBuffer()
				sn.emitContent(remaining[:startIdx])
			}

			// Get content after opening tag
			openLen := len("<" + tagType + ">")
			tagContent := remaining[startIdx+openLen:]

			// Look for ANY closing tag (handle mismatched tags)
			endIdx, endLen := findClosingTag(tagContent)

			if endIdx == -1 {
				// Closing tag not in this chunk
				sn.inThinkTag = true
				sn.tagType = tagType
				sn.tagBuffer = tagContent
				return
			}

			// Both tags in same chunk
			// Only emit reasoning from tags if we haven't already emitted from reasoning field
			if !sn.hasReasoning {
				sn.flushContentBuffer()
				sn.emitReasoning(tagContent[:endIdx])
			}
			remaining = tagContent[endIdx+endLen:]
		}
	}
}

// hasIncompleteOpenTag checks if string ends with an incomplete opening tag.
// Examples: "<thin", "<thinking", "<t", "<"
func hasIncompleteOpenTag(s string) bool {
	// Check for incomplete "<thinking>" or "<think>"
	incomplete := []string{
		"<",
		"<t",
		"<th",
		"<thi",
		"<thin",
		"<think",
		"<thinki",
		"<thinkin",
		"<thinking",
	}
	for _, inc := range incomplete {
		if strings.HasSuffix(s, inc) {
			return true
		}
	}
	return false
}

// Flush should be called at the end of stream to emit any remaining buffered content.
func (sn *StreamNormalizer) Flush() {
	if sn.inThinkTag && sn.tagBuffer != "" {
		if !sn.hasReasoning {
			sn.flushContentBuffer()
			sn.emitReasoning(sn.tagBuffer)
		}
		sn.tagBuffer = ""
		sn.inThinkTag = false
	}
	sn.flushContentBuffer()
}
