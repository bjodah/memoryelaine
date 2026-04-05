package streamview

import (
	"testing"

	"memoryelaine/internal/database"
)

func ptr(s string) *string { return &s }

func TestBuild_MissingBody(t *testing.T) {
	entry := &database.LogEntry{RequestPath: "/v1/chat/completions"}
	r := Build(entry)
	if r.AssembledAvailable {
		t.Error("expected assembled unavailable")
	}
	if r.Reason != ReasonMissingBody {
		t.Errorf("expected reason %q, got %q", ReasonMissingBody, r.Reason)
	}
}

func TestBuild_EmptyBody(t *testing.T) {
	entry := &database.LogEntry{RequestPath: "/v1/chat/completions", RespBody: ptr("")}
	r := Build(entry)
	if r.AssembledAvailable {
		t.Error("expected assembled unavailable")
	}
	if r.Reason != ReasonMissingBody {
		t.Errorf("expected reason %q, got %q", ReasonMissingBody, r.Reason)
	}
}

func TestBuild_Truncated(t *testing.T) {
	body := "data: {}\n\n"
	entry := &database.LogEntry{
		RequestPath:   "/v1/chat/completions",
		RespBody:      &body,
		RespTruncated: true,
	}
	r := Build(entry)
	if r.AssembledAvailable {
		t.Error("expected assembled unavailable")
	}
	if r.Reason != ReasonTruncated {
		t.Errorf("expected reason %q, got %q", ReasonTruncated, r.Reason)
	}
}

func TestBuild_UnsupportedPath(t *testing.T) {
	body := "data: {}\n\n"
	entry := &database.LogEntry{
		RequestPath: "/v1/embeddings",
		RespBody:    &body,
	}
	r := Build(entry)
	if r.AssembledAvailable {
		t.Error("expected assembled unavailable")
	}
	if r.Reason != ReasonUnsupportedPath {
		t.Errorf("expected reason %q, got %q", ReasonUnsupportedPath, r.Reason)
	}
}

func TestBuild_NotSSE(t *testing.T) {
	body := `{"id":"chatcmpl-1","choices":[{"message":{"content":"hello"}}]}`
	entry := &database.LogEntry{
		RequestPath: "/v1/chat/completions",
		RespBody:    &body,
	}
	r := Build(entry)
	if r.AssembledAvailable {
		t.Error("expected assembled unavailable")
	}
	if r.Reason != ReasonNotSSE {
		t.Errorf("expected reason %q, got %q", ReasonNotSSE, r.Reason)
	}
}

func TestBuild_ContentTypeNotSSE(t *testing.T) {
	body := "data: {\"choices\":[]}\n\n"
	headers := `{"Content-Type":["application/json"]}`
	entry := &database.LogEntry{
		RequestPath:     "/v1/chat/completions",
		RespBody:        &body,
		RespHeadersJSON: &headers,
	}
	r := Build(entry)
	if r.AssembledAvailable {
		t.Error("expected assembled unavailable")
	}
	if r.Reason != ReasonNotSSE {
		t.Errorf("expected reason %q, got %q", ReasonNotSSE, r.Reason)
	}
}

func TestBuild_ContentTypeEventStream(t *testing.T) {
	body := makeSSEBody(
		`{"choices":[{"index":0,"delta":{"content":"hi"}}]}`,
		"[DONE]",
	)
	headers := `{"Content-Type":["text/event-stream"]}`
	entry := &database.LogEntry{
		RequestPath:     "/v1/chat/completions",
		RespBody:        &body,
		RespHeadersJSON: &headers,
	}
	r := Build(entry)
	if !r.AssembledAvailable {
		t.Errorf("expected assembled available, reason=%q", r.Reason)
	}
	if r.AssembledBody != "hi" {
		t.Errorf("expected assembled body %q, got %q", "hi", r.AssembledBody)
	}
}

func TestBuild_ContentTypeAbsentFallsThroughToBody(t *testing.T) {
	body := makeSSEBody(
		`{"choices":[{"index":0,"delta":{"content":"ok"}}]}`,
		"[DONE]",
	)
	entry := &database.LogEntry{
		RequestPath: "/v1/chat/completions",
		RespBody:    &body,
	}
	r := Build(entry)
	if !r.AssembledAvailable {
		t.Errorf("expected assembled available, reason=%q", r.Reason)
	}
	if r.AssembledBody != "ok" {
		t.Errorf("expected assembled body %q, got %q", "ok", r.AssembledBody)
	}
}

func TestBuild_RawBodyAlwaysSet(t *testing.T) {
	body := "not SSE at all"
	entry := &database.LogEntry{
		RequestPath: "/v1/chat/completions",
		RespBody:    &body,
	}
	r := Build(entry)
	if r.RawBody != body {
		t.Errorf("expected raw body %q, got %q", body, r.RawBody)
	}
}

func TestBuild_ChatCompletionsDelegates(t *testing.T) {
	body := makeSSEBody(
		`{"choices":[{"index":0,"delta":{"content":"Hello"}}]}`,
		`{"choices":[{"index":0,"delta":{"content":" world"}}]}`,
		"[DONE]",
	)
	entry := &database.LogEntry{
		RequestPath: "/v1/chat/completions",
		RespBody:    &body,
	}
	r := Build(entry)
	if !r.AssembledAvailable {
		t.Errorf("expected assembled available, reason=%q", r.Reason)
	}
	if r.AssembledBody != "Hello world" {
		t.Errorf("expected %q, got %q", "Hello world", r.AssembledBody)
	}
	if r.Reason != ReasonSupported {
		t.Errorf("expected reason %q, got %q", ReasonSupported, r.Reason)
	}
	if !r.HasContent {
		t.Error("expected content flag to be true")
	}
	if r.HasReasoning {
		t.Error("expected reasoning flag to be false")
	}
}

func TestBuild_CompletionsDelegates(t *testing.T) {
	body := makeSSEBody(
		`{"choices":[{"index":0,"text":"Hello"}]}`,
		`{"choices":[{"index":0,"text":" world"}]}`,
		"[DONE]",
	)
	entry := &database.LogEntry{
		RequestPath: "/v1/completions",
		RespBody:    &body,
	}
	r := Build(entry)
	if !r.AssembledAvailable {
		t.Errorf("expected assembled available, reason=%q", r.Reason)
	}
	if r.AssembledBody != "Hello world" {
		t.Errorf("expected %q, got %q", "Hello world", r.AssembledBody)
	}
	if !r.HasContent {
		t.Error("expected content flag to be true")
	}
}

func TestBuild_ReasoningOnlyStillAvailable(t *testing.T) {
	body := makeSSEBody(
		`{"choices":[{"index":0,"delta":{"reasoning_content":"#"}}]}`,
		"[DONE]",
	)
	entry := &database.LogEntry{
		RequestPath: "/v1/chat/completions",
		RespBody:    &body,
	}
	r := Build(entry)
	if !r.AssembledAvailable {
		t.Errorf("expected assembled available, reason=%q", r.Reason)
	}
	if !r.HasReasoning {
		t.Error("expected reasoning flag to be true")
	}
	if r.HasContent {
		t.Error("expected content flag to be false")
	}
	if r.ReasoningBody != "#" {
		t.Errorf("expected reasoning body %q, got %q", "#", r.ReasoningBody)
	}
	if r.AssembledBody != "" {
		t.Errorf("expected empty flattened assembled body, got %q", r.AssembledBody)
	}
}

// makeSSEBody creates an SSE body from data payloads.
func makeSSEBody(events ...string) string {
	var sb []string
	for _, e := range events {
		sb = append(sb, "data: "+e)
	}
	return joinEvents(sb...)
}

func joinEvents(lines ...string) string {
	return joinWith(lines, "\n\n") + "\n\n"
}

func joinWith(lines []string, sep string) string {
	result := ""
	for i, l := range lines {
		if i > 0 {
			result += sep
		}
		result += l
	}
	return result
}
