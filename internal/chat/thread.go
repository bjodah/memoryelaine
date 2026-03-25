package chat

import (
	"encoding/json"
	"fmt"
)

// ThreadMessage represents one message in a reconstructed conversation thread.
type ThreadMessage struct {
	Role       string `json:"role"`
	Content    string `json:"content"`
	LogID      int64  `json:"log_id"`
	IsComplex  bool   `json:"is_complex"`
	Complexity string `json:"complexity,omitempty"`
}

// ThreadEntry interface allows BuildThreadMessages to work with both
// database.LogEntry and other types.
type ThreadEntry interface {
	GetID() int64
	GetRequestPath() string
	GetReqBody() string
	IsReqTruncated() bool
	GetRespBody() *string
	GetRespText() *string
	GetParentPrefixLen() *int
}

// BuildThreadMessages performs backward attribution: walk the ancestor chain
// from the selected entry (last) back toward the root, using parent_prefix_len
// to determine which messages each entry contributed.
// sseExtractor is optional and used to assemble SSE responses if RespText is nil.
func BuildThreadMessages(msgs []Message, chain []ThreadEntry, sseExtractor func(id int64) string) []ThreadMessage {
	if len(msgs) == 0 || len(chain) == 0 {
		return nil
	}

	selected := chain[len(chain)-1]

	// Each message gets attributed to a log entry.
	attribution := make([]int64, len(msgs))
	// Default: attribute all to root.
	rootID := chain[0].GetID()
	for i := range attribution {
		attribution[i] = rootID
	}

	// Walk backward from the selected entry through the chain, attributing
	// message ranges.
	cursor := len(msgs) // end of range (exclusive)
	for i := len(chain) - 1; i >= 0; i-- {
		entry := chain[i]
		start := 0
		if ppl := entry.GetParentPrefixLen(); ppl != nil && *ppl > 0 {
			start = *ppl
		}
		// Clamp to valid range.
		if start > cursor {
			start = cursor
		}
		for j := start; j < cursor; j++ {
			if j < len(attribution) {
				attribution[j] = entry.GetID()
			}
		}
		cursor = start
		if cursor <= 0 {
			break
		}
	}

	// Build the output.
	result := make([]ThreadMessage, 0, len(msgs)+1)
	for i, m := range msgs {
		isComplex, complexity := GetMessageComplexity(m)
		content := ExtractContentString(m.Content)
		logID := attribution[i]

		if content == "" && isComplex {
			content = fmt.Sprintf("[Complex message: %s - view raw log #%d]", complexity, logID)
		} else if content == "" && !isComplex {
			content = fmt.Sprintf("[Empty message - view raw log #%d]", logID)
		}

		result = append(result, ThreadMessage{
			Role:       m.Role,
			Content:    content,
			LogID:      logID,
			IsComplex:  isComplex,
			Complexity: complexity,
		})
	}

	// Append the assistant's response from the selected entry if available.
	if rt := selected.GetRespText(); rt != nil && *rt != "" {
		result = append(result, ThreadMessage{
			Role:    "assistant",
			Content: *rt,
			LogID:   selected.GetID(),
		})
	} else if rb := selected.GetRespBody(); rb != nil && *rb != "" {
		// Fallback 1: Try non-streaming JSON extraction.
		if rt := ExtractAssistantResponse(*rb); rt != "" {
			result = append(result, ThreadMessage{
				Role:    "assistant",
				Content: rt,
				LogID:   selected.GetID(),
			})
		} else if sseExtractor != nil {
			// Fallback 2: Try SSE extraction via the provided callback.
			if assembled := sseExtractor(selected.GetID()); assembled != "" {
				result = append(result, ThreadMessage{
					Role:    "assistant",
					Content: assembled,
					LogID:   selected.GetID(),
				})
			}
		} else {
			// Fallback 3: Check if it's complex (e.g. tool calls only).
			var resp struct {
				Choices []struct {
					Message Message `json:"message"`
				} `json:"choices"`
			}
			if err := json.Unmarshal([]byte(*rb), &resp); err == nil && len(resp.Choices) > 0 {
				m := resp.Choices[0].Message
				if isComplex, complexity := GetMessageComplexity(m); isComplex {
					result = append(result, ThreadMessage{
						Role:       "assistant",
						Content:    fmt.Sprintf("[Complex message: %s - view raw log #%d]", complexity, selected.GetID()),
						LogID:      selected.GetID(),
						IsComplex:  true,
						Complexity: complexity,
					})
				}
			}
		}
	}

	return result
}
