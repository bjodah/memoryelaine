package streamview

import (
	"encoding/json"
	"strings"
)

// parseResult is the internal result of endpoint-specific SSE parsing.
type parseResult struct {
	text      string
	content   string
	reasoning string
	available bool
	reason    AvailabilityReason
}

// splitSSEEvents splits raw SSE text into individual events, returning
// the concatenated data: payload for each event block.
func splitSSEEvents(body string) []string {
	body = strings.ReplaceAll(body, "\r\n", "\n")

	var payloads []string
	for _, block := range strings.Split(body, "\n\n") {
		block = strings.TrimSpace(block)
		if block == "" {
			continue
		}

		var dataLines []string
		for _, line := range strings.Split(block, "\n") {
			if strings.HasPrefix(line, "data:") {
				payload := strings.TrimPrefix(line, "data:")
				if len(payload) > 0 && payload[0] == ' ' {
					payload = payload[1:]
				}
				dataLines = append(dataLines, payload)
			}
		}

		if len(dataLines) > 0 {
			payloads = append(payloads, strings.Join(dataLines, "\n"))
		}
	}
	return payloads
}

// --- /v1/chat/completions ---

type chatCompletionChunk struct {
	Choices []chatCompletionChoice `json:"choices"`
}

type chatCompletionChoice struct {
	Delta chatCompletionDelta `json:"delta"`
}

type chatCompletionDelta struct {
	Content          *string         `json:"content"`
	ReasoningContent *string         `json:"reasoning_content,omitempty"`
	ToolCalls        json.RawMessage `json:"tool_calls,omitempty"`
	FunctionCall     json.RawMessage `json:"function_call,omitempty"`
}

func parseChatCompletionsSSE(body string) parseResult {
	events := splitSSEEvents(body)
	var content strings.Builder
	var reasoning strings.Builder

	for _, data := range events {
		if data == "[DONE]" {
			continue
		}

		var chunk chatCompletionChunk
		if err := json.Unmarshal([]byte(data), &chunk); err != nil {
			if content.Len() > 0 || reasoning.Len() > 0 {
				return parseResult{
					text:      content.String(),
					content:   content.String(),
					reasoning: reasoning.String(),
					available: true,
					reason:    ReasonPartialParse,
				}
			}
			return parseResult{reason: ReasonParseFailed}
		}

		if len(chunk.Choices) == 0 {
			continue
		}

		if len(chunk.Choices) > 1 {
			return parseResult{reason: ReasonUnsupportedMultiChoice}
		}

		delta := chunk.Choices[0].Delta

		if isNonEmptyJSONValue(delta.ToolCalls) {
			return parseResult{reason: ReasonUnsupportedToolCallStream}
		}
		if isNonEmptyJSONValue(delta.FunctionCall) {
			return parseResult{reason: ReasonUnsupportedToolCallStream}
		}

		if delta.Content != nil {
			content.WriteString(*delta.Content)
		}
		if delta.ReasoningContent != nil {
			reasoning.WriteString(*delta.ReasoningContent)
		}
	}

	if content.Len() == 0 && reasoning.Len() == 0 {
		return parseResult{reason: ReasonNoTextContent}
	}

	return parseResult{
		text:      content.String(),
		content:   content.String(),
		reasoning: reasoning.String(),
		available: true,
		reason:    ReasonSupported,
	}
}

// --- /v1/completions ---

type completionChunk struct {
	Choices []completionChoice `json:"choices"`
}

type completionChoice struct {
	Text *string `json:"text"`
}

func parseCompletionsSSE(body string) parseResult {
	events := splitSSEEvents(body)
	var assembled strings.Builder

	for _, data := range events {
		if data == "[DONE]" {
			continue
		}

		var chunk completionChunk
		if err := json.Unmarshal([]byte(data), &chunk); err != nil {
			if assembled.Len() > 0 {
				return parseResult{
					text:      assembled.String(),
					content:   assembled.String(),
					available: true,
					reason:    ReasonPartialParse,
				}
			}
			return parseResult{reason: ReasonParseFailed}
		}

		if len(chunk.Choices) == 0 {
			continue
		}

		if len(chunk.Choices) > 1 {
			return parseResult{reason: ReasonUnsupportedMultiChoice}
		}

		if chunk.Choices[0].Text != nil {
			assembled.WriteString(*chunk.Choices[0].Text)
		}
	}

	if assembled.Len() == 0 {
		return parseResult{reason: ReasonNoTextContent}
	}

	return parseResult{
		text:      assembled.String(),
		content:   assembled.String(),
		available: true,
		reason:    ReasonSupported,
	}
}

// isNonEmptyJSONValue returns true if the raw JSON message is present and
// is neither null nor an empty array.
func isNonEmptyJSONValue(raw json.RawMessage) bool {
	if raw == nil {
		return false
	}
	s := strings.TrimSpace(string(raw))
	return s != "" && s != "null" && s != "[]"
}
