package streamview

import (
	"encoding/json"
	"net/http"
	"strings"

	"memoryelaine/internal/database"
)

// Mode represents the active view mode for a response body.
type Mode string

const (
	ModeRaw       Mode = "raw"
	ModeAssembled Mode = "assembled"
)

// AvailabilityReason is a machine-stable reason explaining whether
// assembled mode is available, partially available, or unavailable.
type AvailabilityReason string

const (
	ReasonSupported                 AvailabilityReason = "supported"
	ReasonPartialParse              AvailabilityReason = "partial_parse"
	ReasonUnsupportedPath           AvailabilityReason = "unsupported_path"
	ReasonUnsupportedMultiChoice    AvailabilityReason = "unsupported_multi_choice"
	ReasonUnsupportedToolCallStream AvailabilityReason = "unsupported_tool_call_stream"
	ReasonNoTextContent             AvailabilityReason = "no_text_content"
	ReasonMissingBody               AvailabilityReason = "missing_body"
	ReasonTruncated                 AvailabilityReason = "truncated"
	ReasonNotSSE                    AvailabilityReason = "not_sse"
	ReasonParseFailed               AvailabilityReason = "parse_failed"
)

// Result contains the raw body and, when available, the assembled body
// derived from a supported SSE stream.
type Result struct {
	RawBody            string
	AssembledBody      string
	AssembledAvailable bool
	Reason             AvailabilityReason
}

var supportedPaths = map[string]bool{
	"/v1/chat/completions": true,
	"/v1/completions":      true,
}

// Build inspects a log entry and derives stream-view metadata.
func Build(entry *database.LogEntry) Result {
	raw := ""
	if entry.RespBody != nil {
		raw = *entry.RespBody
	}

	result := Result{RawBody: raw}

	if entry.RespBody == nil || *entry.RespBody == "" {
		result.Reason = ReasonMissingBody
		return result
	}

	if entry.RespTruncated {
		result.Reason = ReasonTruncated
		return result
	}

	if !supportedPaths[entry.RequestPath] {
		result.Reason = ReasonUnsupportedPath
		return result
	}

	// Use Content-Type header as an additional signal when available.
	if entry.RespHeadersJSON != nil && *entry.RespHeadersJSON != "" {
		var headers http.Header
		if err := json.Unmarshal([]byte(*entry.RespHeadersJSON), &headers); err == nil {
			ct := headers.Get("Content-Type")
			if ct != "" && !strings.Contains(strings.ToLower(ct), "text/event-stream") {
				result.Reason = ReasonNotSSE
				return result
			}
		}
	}

	if !looksLikeSSE(raw) {
		result.Reason = ReasonNotSSE
		return result
	}

	var pr parseResult
	switch entry.RequestPath {
	case "/v1/chat/completions":
		pr = parseChatCompletionsSSE(raw)
	case "/v1/completions":
		pr = parseCompletionsSSE(raw)
	}

	result.AssembledBody = pr.text
	result.AssembledAvailable = pr.available
	result.Reason = pr.reason

	return result
}

// looksLikeSSE returns true if the body contains at least one SSE data: line.
func looksLikeSSE(body string) bool {
	for _, line := range strings.Split(body, "\n") {
		line = strings.TrimRight(line, "\r")
		if strings.HasPrefix(line, "data:") {
			return true
		}
	}
	return false
}
