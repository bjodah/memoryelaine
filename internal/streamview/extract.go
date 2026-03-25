package streamview

import (
	"memoryelaine/internal/chat"
	"memoryelaine/internal/database"
)

// BestEffortResponseText extracts assistant-visible response text from a log
// entry using stored sidecars first, then JSON parsing, then SSE assembly.
func BestEffortResponseText(entry *database.LogEntry) string {
	if entry == nil {
		return ""
	}
	if entry.RespText != nil && *entry.RespText != "" {
		return *entry.RespText
	}
	if entry.RespBody == nil || *entry.RespBody == "" {
		return ""
	}
	if text := chat.ExtractAssistantResponse(*entry.RespBody); text != "" {
		return text
	}

	result := Build(entry)
	if result.AssembledAvailable {
		return result.AssembledBody
	}
	return ""
}
