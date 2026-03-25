package chat

import (
	"encoding/json"
	"strings"
)

// Message represents a single message in an OpenAI chat request.
type Message struct {
	Role         string          `json:"role"`
	Content      json.RawMessage `json:"content,omitempty"`
	Name         *string         `json:"name,omitempty"`
	ToolCalls    json.RawMessage `json:"tool_calls,omitempty"`
	ToolCallID   *string         `json:"tool_call_id,omitempty"`
	FunctionCall json.RawMessage `json:"function_call,omitempty"`
}

// ChatCompletionRequest is the subset of the OpenAI request we need to parse.
type ChatCompletionRequest struct {
	Messages []Message `json:"messages"`
}

// IsChatPath returns true if the request path ends with /chat/completions.
func IsChatPath(path string) bool {
	return strings.HasSuffix(path, "/chat/completions")
}

// ParseMessages extracts the messages array from a raw JSON request body.
// Returns nil, nil if parsing fails (non-chat or malformed).
func ParseMessages(reqBody string) ([]Message, error) {
	var req ChatCompletionRequest
	if err := json.Unmarshal([]byte(reqBody), &req); err != nil {
		return nil, err
	}
	return req.Messages, nil
}

// ExtractRequestText returns the concatenated user-visible text from a chat
// request's messages array. Each message becomes "role: content\n".
// Messages with no string content (e.g. tool_calls-only) are skipped.
func ExtractRequestText(messages []Message) string {
	var sb strings.Builder
	for _, m := range messages {
		text := ExtractContentString(m.Content)
		if text == "" {
			continue
		}
		sb.WriteString(m.Role)
		sb.WriteString(": ")
		sb.WriteString(text)
		sb.WriteByte('\n')
	}
	return sb.String()
}

// ExtractContentString returns the string value of a content field.
// Content can be a plain string or an array of content parts; we concatenate
// all "text" parts for the array form.
func ExtractContentString(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}

	// Try as plain string first.
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return s
	}

	// Try as array of content parts (multimodal).
	var parts []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if err := json.Unmarshal(raw, &parts); err == nil {
		var texts []string
		for _, p := range parts {
			if p.Type == "text" && p.Text != "" {
				texts = append(texts, p.Text)
			}
		}
		return strings.Join(texts, "\n")
	}

	return ""
}

// ExtractAssistantResponse extracts the assistant's text from a non-streaming
// chat completion JSON response body. Returns "" if extraction fails or the
// response has no text content.
func ExtractAssistantResponse(respBody string) string {
	var resp struct {
		Choices []struct {
			Message struct {
				Content json.RawMessage `json:"content"`
			} `json:"message"`
			Delta struct {
				Content json.RawMessage `json:"content"`
			} `json:"delta"`
		} `json:"choices"`
	}
	if err := json.Unmarshal([]byte(respBody), &resp); err != nil {
		return ""
	}
	if len(resp.Choices) == 0 {
		return ""
	}
	// Try message first (non-streaming), then delta (single chunk).
	content := resp.Choices[0].Message.Content
	if len(content) == 0 {
		content = resp.Choices[0].Delta.Content
	}
	return ExtractContentString(content)
}

// GetMessageComplexity returns true and a reason if the message contains
// complex elements like tool calls or non-text content parts.
func GetMessageComplexity(m Message) (bool, string) {
	if len(m.ToolCalls) > 0 {
		return true, "tool_calls"
	}
	if len(m.FunctionCall) > 0 {
		return true, "function_call"
	}

	// Check for non-text multimodal content.
	if len(m.Content) > 0 && m.Content[0] == '[' {
		var parts []struct {
			Type string `json:"type"`
		}
		if err := json.Unmarshal(m.Content, &parts); err == nil {
			for _, p := range parts {
				if p.Type != "text" {
					return true, "multimodal:" + p.Type
				}
			}
		}
	}

	return false, ""
}
