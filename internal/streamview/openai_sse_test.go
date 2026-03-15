package streamview

import (
	"strings"
	"testing"
)

// --- Chat completions parser tests ---

func TestChatCompletions_MultipleTextDeltas(t *testing.T) {
	body := makeSSEBody(
		`{"choices":[{"index":0,"delta":{"role":"assistant"}}]}`,
		`{"choices":[{"index":0,"delta":{"content":"Hello"}}]}`,
		`{"choices":[{"index":0,"delta":{"content":" world"}}]}`,
		`{"choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}`,
		"[DONE]",
	)
	r := parseChatCompletionsSSE(body)
	if !r.available {
		t.Errorf("expected available, reason=%q", r.reason)
	}
	if r.text != "Hello world" {
		t.Errorf("expected %q, got %q", "Hello world", r.text)
	}
	if r.reason != ReasonSupported {
		t.Errorf("expected reason %q, got %q", ReasonSupported, r.reason)
	}
}

func TestChatCompletions_CRLFFraming(t *testing.T) {
	body := "data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\"A\"}}]}\r\n\r\n" +
		"data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\"B\"}}]}\r\n\r\n" +
		"data: [DONE]\r\n\r\n"

	r := parseChatCompletionsSSE(body)
	if !r.available {
		t.Errorf("expected available, reason=%q", r.reason)
	}
	if r.text != "AB" {
		t.Errorf("expected %q, got %q", "AB", r.text)
	}
}

func TestChatCompletions_DoneHandling(t *testing.T) {
	body := makeSSEBody(
		`{"choices":[{"index":0,"delta":{"content":"ok"}}]}`,
		"[DONE]",
	)
	r := parseChatCompletionsSSE(body)
	if !r.available || r.text != "ok" {
		t.Errorf("expected available with text %q, got available=%v text=%q", "ok", r.available, r.text)
	}
}

func TestChatCompletions_RoleOnlyDeltaSkipped(t *testing.T) {
	body := makeSSEBody(
		`{"choices":[{"index":0,"delta":{"role":"assistant"}}]}`,
		`{"choices":[{"index":0,"delta":{"content":"text"}}]}`,
		"[DONE]",
	)
	r := parseChatCompletionsSSE(body)
	if !r.available || r.text != "text" {
		t.Errorf("expected text %q, got %q", "text", r.text)
	}
}

func TestChatCompletions_EmptyChoices_UsageChunk(t *testing.T) {
	body := makeSSEBody(
		`{"choices":[{"index":0,"delta":{"content":"hi"}}]}`,
		`{"choices":[],"usage":{"prompt_tokens":5,"completion_tokens":1}}`,
		"[DONE]",
	)
	r := parseChatCompletionsSSE(body)
	if !r.available || r.text != "hi" {
		t.Errorf("expected text %q, got available=%v text=%q", "hi", r.available, r.text)
	}
}

func TestChatCompletions_NullContent(t *testing.T) {
	body := makeSSEBody(
		`{"choices":[{"index":0,"delta":{"content":null}}]}`,
		`{"choices":[{"index":0,"delta":{"content":"real"}}]}`,
		"[DONE]",
	)
	r := parseChatCompletionsSSE(body)
	if !r.available || r.text != "real" {
		t.Errorf("expected text %q, got %q", "real", r.text)
	}
}

func TestChatCompletions_MultiChoice(t *testing.T) {
	body := makeSSEBody(
		`{"choices":[{"index":0,"delta":{"content":"A"}},{"index":1,"delta":{"content":"B"}}]}`,
		"[DONE]",
	)
	r := parseChatCompletionsSSE(body)
	if r.available {
		t.Error("expected unavailable for multi-choice")
	}
	if r.reason != ReasonUnsupportedMultiChoice {
		t.Errorf("expected reason %q, got %q", ReasonUnsupportedMultiChoice, r.reason)
	}
}

func TestChatCompletions_ToolCalls(t *testing.T) {
	body := makeSSEBody(
		`{"choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"call_1","type":"function","function":{"name":"get_weather","arguments":""}}]}}]}`,
		"[DONE]",
	)
	r := parseChatCompletionsSSE(body)
	if r.available {
		t.Error("expected unavailable for tool call stream")
	}
	if r.reason != ReasonUnsupportedToolCallStream {
		t.Errorf("expected reason %q, got %q", ReasonUnsupportedToolCallStream, r.reason)
	}
}

func TestChatCompletions_FunctionCall(t *testing.T) {
	body := makeSSEBody(
		`{"choices":[{"index":0,"delta":{"function_call":{"name":"fn","arguments":"{}"}}}]}`,
		"[DONE]",
	)
	r := parseChatCompletionsSSE(body)
	if r.available {
		t.Error("expected unavailable for function call stream")
	}
	if r.reason != ReasonUnsupportedToolCallStream {
		t.Errorf("expected reason %q, got %q", ReasonUnsupportedToolCallStream, r.reason)
	}
}

func TestChatCompletions_NullToolCalls_Ignored(t *testing.T) {
	body := makeSSEBody(
		`{"choices":[{"index":0,"delta":{"content":"ok","tool_calls":null}}]}`,
		"[DONE]",
	)
	r := parseChatCompletionsSSE(body)
	if !r.available || r.text != "ok" {
		t.Errorf("expected text %q, got available=%v text=%q", "ok", r.available, r.text)
	}
}

func TestChatCompletions_EmptyToolCallsArray_Ignored(t *testing.T) {
	body := makeSSEBody(
		`{"choices":[{"index":0,"delta":{"content":"ok","tool_calls":[]}}]}`,
		"[DONE]",
	)
	r := parseChatCompletionsSSE(body)
	if !r.available || r.text != "ok" {
		t.Errorf("expected text %q, got available=%v text=%q", "ok", r.available, r.text)
	}
}

func TestChatCompletions_InvalidJSONBeforeText(t *testing.T) {
	body := makeSSEBody(
		`{invalid json}`,
		"[DONE]",
	)
	r := parseChatCompletionsSSE(body)
	if r.available {
		t.Error("expected unavailable")
	}
	if r.reason != ReasonParseFailed {
		t.Errorf("expected reason %q, got %q", ReasonParseFailed, r.reason)
	}
}

func TestChatCompletions_InvalidJSONAfterText(t *testing.T) {
	body := makeSSEBody(
		`{"choices":[{"index":0,"delta":{"content":"partial"}}]}`,
		`{broken`,
		"[DONE]",
	)
	r := parseChatCompletionsSSE(body)
	if !r.available {
		t.Error("expected available with partial parse")
	}
	if r.text != "partial" {
		t.Errorf("expected text %q, got %q", "partial", r.text)
	}
	if r.reason != ReasonPartialParse {
		t.Errorf("expected reason %q, got %q", ReasonPartialParse, r.reason)
	}
}

func TestChatCompletions_TextlessStream(t *testing.T) {
	body := makeSSEBody(
		`{"choices":[{"index":0,"delta":{"role":"assistant"}}]}`,
		`{"choices":[],"usage":{"prompt_tokens":5}}`,
		"[DONE]",
	)
	r := parseChatCompletionsSSE(body)
	if r.available {
		t.Error("expected unavailable for textless stream")
	}
	if r.reason != ReasonNoTextContent {
		t.Errorf("expected reason %q, got %q", ReasonNoTextContent, r.reason)
	}
}

func TestChatCompletions_EmptySSEPaylines(t *testing.T) {
	// Body with comment lines and empty blocks
	body := ": keep-alive\n\ndata: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\"hi\"}}]}\n\ndata: [DONE]\n\n"
	r := parseChatCompletionsSSE(body)
	if !r.available || r.text != "hi" {
		t.Errorf("expected text %q, got available=%v text=%q", "hi", r.available, r.text)
	}
}

// --- Completions parser tests ---

func TestCompletions_MultipleTextChunks(t *testing.T) {
	body := makeSSEBody(
		`{"choices":[{"index":0,"text":"Hello"}]}`,
		`{"choices":[{"index":0,"text":" world"}]}`,
		"[DONE]",
	)
	r := parseCompletionsSSE(body)
	if !r.available || r.text != "Hello world" {
		t.Errorf("expected %q, got available=%v text=%q", "Hello world", r.available, r.text)
	}
	if r.reason != ReasonSupported {
		t.Errorf("expected reason %q, got %q", ReasonSupported, r.reason)
	}
}

func TestCompletions_NullText(t *testing.T) {
	body := makeSSEBody(
		`{"choices":[{"index":0,"text":null}]}`,
		`{"choices":[{"index":0,"text":"ok"}]}`,
		"[DONE]",
	)
	r := parseCompletionsSSE(body)
	if !r.available || r.text != "ok" {
		t.Errorf("expected text %q, got %q", "ok", r.text)
	}
}

func TestCompletions_EmptyChoices(t *testing.T) {
	body := makeSSEBody(
		`{"choices":[{"index":0,"text":"hi"}]}`,
		`{"choices":[]}`,
		"[DONE]",
	)
	r := parseCompletionsSSE(body)
	if !r.available || r.text != "hi" {
		t.Errorf("expected text %q, got available=%v text=%q", "hi", r.available, r.text)
	}
}

func TestCompletions_MultiChoice(t *testing.T) {
	body := makeSSEBody(
		`{"choices":[{"index":0,"text":"A"},{"index":1,"text":"B"}]}`,
		"[DONE]",
	)
	r := parseCompletionsSSE(body)
	if r.available {
		t.Error("expected unavailable for multi-choice")
	}
	if r.reason != ReasonUnsupportedMultiChoice {
		t.Errorf("expected reason %q, got %q", ReasonUnsupportedMultiChoice, r.reason)
	}
}

func TestCompletions_InvalidJSONBeforeText(t *testing.T) {
	body := makeSSEBody(`not json`, "[DONE]")
	r := parseCompletionsSSE(body)
	if r.available {
		t.Error("expected unavailable")
	}
	if r.reason != ReasonParseFailed {
		t.Errorf("expected reason %q, got %q", ReasonParseFailed, r.reason)
	}
}

func TestCompletions_InvalidJSONAfterText(t *testing.T) {
	body := makeSSEBody(
		`{"choices":[{"index":0,"text":"partial"}]}`,
		`{broken`,
		"[DONE]",
	)
	r := parseCompletionsSSE(body)
	if !r.available {
		t.Error("expected available with partial parse")
	}
	if r.text != "partial" {
		t.Errorf("expected text %q, got %q", "partial", r.text)
	}
	if r.reason != ReasonPartialParse {
		t.Errorf("expected reason %q, got %q", ReasonPartialParse, r.reason)
	}
}

func TestCompletions_TextlessStream(t *testing.T) {
	body := makeSSEBody(
		`{"choices":[{"index":0,"text":null}]}`,
		"[DONE]",
	)
	r := parseCompletionsSSE(body)
	if r.available {
		t.Error("expected unavailable for textless stream")
	}
	if r.reason != ReasonNoTextContent {
		t.Errorf("expected reason %q, got %q", ReasonNoTextContent, r.reason)
	}
}

// --- SSE event splitting tests ---

func TestSplitSSEEvents_Basic(t *testing.T) {
	body := "data: hello\n\ndata: world\n\n"
	events := splitSSEEvents(body)
	if len(events) != 2 {
		t.Fatalf("expected 2 events, got %d", len(events))
	}
	if events[0] != "hello" {
		t.Errorf("expected %q, got %q", "hello", events[0])
	}
	if events[1] != "world" {
		t.Errorf("expected %q, got %q", "world", events[1])
	}
}

func TestSplitSSEEvents_CRLF(t *testing.T) {
	body := "data: hello\r\n\r\ndata: world\r\n\r\n"
	events := splitSSEEvents(body)
	if len(events) != 2 {
		t.Fatalf("expected 2 events, got %d", len(events))
	}
	if events[0] != "hello" {
		t.Errorf("expected %q, got %q", "hello", events[0])
	}
}

func TestSplitSSEEvents_CommentLines(t *testing.T) {
	body := ": comment\n\ndata: value\n\n"
	events := splitSSEEvents(body)
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	if events[0] != "value" {
		t.Errorf("expected %q, got %q", "value", events[0])
	}
}

func TestSplitSSEEvents_NoTrailingNewline(t *testing.T) {
	body := "data: only"
	events := splitSSEEvents(body)
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	if events[0] != "only" {
		t.Errorf("expected %q, got %q", "only", events[0])
	}
}

func TestSplitSSEEvents_NoLeadingSpace(t *testing.T) {
	body := "data:nospace\n\n"
	events := splitSSEEvents(body)
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	if events[0] != "nospace" {
		t.Errorf("expected %q, got %q", "nospace", events[0])
	}
}

// --- Realistic fixture tests ---

func TestRealisticChatCompletionStream(t *testing.T) {
	body := strings.Join([]string{
		`data: {"id":"chatcmpl-abc","object":"chat.completion.chunk","created":1700000000,"model":"gpt-4","choices":[{"index":0,"delta":{"role":"assistant","content":""},"logprobs":null,"finish_reason":null}]}`,
		``,
		`data: {"id":"chatcmpl-abc","object":"chat.completion.chunk","created":1700000000,"model":"gpt-4","choices":[{"index":0,"delta":{"content":"Hello"},"logprobs":null,"finish_reason":null}]}`,
		``,
		`data: {"id":"chatcmpl-abc","object":"chat.completion.chunk","created":1700000000,"model":"gpt-4","choices":[{"index":0,"delta":{"content":"!"},"logprobs":null,"finish_reason":null}]}`,
		``,
		`data: {"id":"chatcmpl-abc","object":"chat.completion.chunk","created":1700000000,"model":"gpt-4","choices":[{"index":0,"delta":{},"logprobs":null,"finish_reason":"stop"}]}`,
		``,
		`data: [DONE]`,
		``,
	}, "\n")

	r := parseChatCompletionsSSE(body)
	if !r.available {
		t.Errorf("expected available, reason=%q", r.reason)
	}
	if r.text != "Hello!" {
		t.Errorf("expected %q, got %q", "Hello!", r.text)
	}
}

func TestRealisticCompletionStream(t *testing.T) {
	body := strings.Join([]string{
		`data: {"id":"cmpl-xyz","object":"text_completion","created":1700000000,"choices":[{"text":"The","index":0,"logprobs":null,"finish_reason":null}],"model":"gpt-3.5-turbo-instruct"}`,
		``,
		`data: {"id":"cmpl-xyz","object":"text_completion","created":1700000000,"choices":[{"text":" answer","index":0,"logprobs":null,"finish_reason":null}],"model":"gpt-3.5-turbo-instruct"}`,
		``,
		`data: {"id":"cmpl-xyz","object":"text_completion","created":1700000000,"choices":[{"text":" is 42","index":0,"logprobs":null,"finish_reason":"stop"}],"model":"gpt-3.5-turbo-instruct"}`,
		``,
		`data: [DONE]`,
		``,
	}, "\n")

	r := parseCompletionsSSE(body)
	if !r.available {
		t.Errorf("expected available, reason=%q", r.reason)
	}
	if r.text != "The answer is 42" {
		t.Errorf("expected %q, got %q", "The answer is 42", r.text)
	}
}
