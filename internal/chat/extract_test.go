package chat

import (
	"encoding/json"
	"testing"
)

func TestIsChatPath(t *testing.T) {
	tests := []struct {
		path string
		want bool
	}{
		{"/v1/chat/completions", true},
		{"/chat/completions", true},
		{"/v1/completions", false},
		{"/v1/embeddings", false},
		{"", false},
	}
	for _, tt := range tests {
		if got := IsChatPath(tt.path); got != tt.want {
			t.Errorf("IsChatPath(%q) = %v, want %v", tt.path, got, tt.want)
		}
	}
}

func TestParseMessages(t *testing.T) {
	body := `{"model":"gpt-4","messages":[{"role":"system","content":"You are helpful"},{"role":"user","content":"Hello"}]}`
	msgs, err := ParseMessages(body)
	if err != nil {
		t.Fatalf("ParseMessages error: %v", err)
	}
	if len(msgs) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(msgs))
	}
	if msgs[0].Role != "system" {
		t.Errorf("msgs[0].Role = %q, want system", msgs[0].Role)
	}
	if msgs[1].Role != "user" {
		t.Errorf("msgs[1].Role = %q, want user", msgs[1].Role)
	}
}

func TestParseMessages_InvalidJSON(t *testing.T) {
	_, err := ParseMessages("not json")
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

func TestExtractRequestText(t *testing.T) {
	msgs := []Message{
		{Role: "system", Content: json.RawMessage(`"You are helpful"`)},
		{Role: "user", Content: json.RawMessage(`"What is 2+2?"`)},
	}
	text := ExtractRequestText(msgs)
	expected := "system: You are helpful\nuser: What is 2+2?\n"
	if text != expected {
		t.Errorf("ExtractRequestText = %q, want %q", text, expected)
	}
}

func TestExtractRequestText_MultimodalContent(t *testing.T) {
	content := `[{"type":"text","text":"Describe this image"},{"type":"image_url","image_url":{"url":"http://example.com/img.png"}}]`
	msgs := []Message{
		{Role: "user", Content: json.RawMessage(content)},
	}
	text := ExtractRequestText(msgs)
	expected := "user: Describe this image\n"
	if text != expected {
		t.Errorf("ExtractRequestText multimodal = %q, want %q", text, expected)
	}
}

func TestExtractRequestText_ToolCallOnlySkipped(t *testing.T) {
	msgs := []Message{
		{Role: "assistant", ToolCalls: json.RawMessage(`[{"id":"call_1","function":{"name":"get_weather","arguments":"{}"}}]`)},
	}
	text := ExtractRequestText(msgs)
	if text != "" {
		t.Errorf("expected empty text for tool-call-only message, got %q", text)
	}
}

func TestGetMessageComplexity_ToolCalls(t *testing.T) {
	m := Message{
		Role:      "assistant",
		ToolCalls: json.RawMessage(`[{"id":"call_1"}]`),
	}
	isComplex, reason := GetMessageComplexity(m)
	if !isComplex {
		t.Error("expected tool_calls message to be complex")
	}
	if reason != "tool_calls" {
		t.Errorf("expected reason 'tool_calls', got %q", reason)
	}
}

func TestGetMessageComplexity_FunctionCall(t *testing.T) {
	m := Message{
		Role:         "assistant",
		FunctionCall: json.RawMessage(`{"name":"get_weather"}`),
	}
	isComplex, reason := GetMessageComplexity(m)
	if !isComplex {
		t.Error("expected function_call message to be complex")
	}
	if reason != "function_call" {
		t.Errorf("expected reason 'function_call', got %q", reason)
	}
}

func TestGetMessageComplexity_Multimodal(t *testing.T) {
	m := Message{
		Role:    "user",
		Content: json.RawMessage(`[{"type":"text","text":"Describe"},{"type":"image_url","image_url":{"url":"http://example.com/img.png"}}]`),
	}
	isComplex, reason := GetMessageComplexity(m)
	if !isComplex {
		t.Error("expected multimodal message to be complex")
	}
	if reason != "multimodal:image_url" {
		t.Errorf("expected reason 'multimodal:image_url', got %q", reason)
	}
}

func TestGetMessageComplexity_PlainText(t *testing.T) {
	m := Message{
		Role:    "user",
		Content: json.RawMessage(`"Hello"`),
	}
	isComplex, _ := GetMessageComplexity(m)
	if isComplex {
		t.Error("plain text message should not be complex")
	}
}

func TestGetMessageComplexity_TextOnlyArray(t *testing.T) {
	m := Message{
		Role:    "user",
		Content: json.RawMessage(`[{"type":"text","text":"Hello"}]`),
	}
	isComplex, _ := GetMessageComplexity(m)
	if isComplex {
		t.Error("text-only array should not be flagged as complex")
	}
}

func TestExtractAssistantResponse_NonStreaming(t *testing.T) {
	resp := `{"choices":[{"message":{"content":"Hello there!"}}]}`
	text := ExtractAssistantResponse(resp)
	if text != "Hello there!" {
		t.Errorf("expected 'Hello there!', got %q", text)
	}
}

func TestExtractAssistantResponse_Empty(t *testing.T) {
	text := ExtractAssistantResponse("")
	if text != "" {
		t.Errorf("expected empty, got %q", text)
	}
}

func TestExtractAssistantResponse_NoChoices(t *testing.T) {
	text := ExtractAssistantResponse(`{"choices":[]}`)
	if text != "" {
		t.Errorf("expected empty, got %q", text)
	}
}
