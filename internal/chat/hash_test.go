package chat

import (
	"encoding/json"
	"testing"
)

func TestHashMessages_Deterministic(t *testing.T) {
	msgs := []Message{
		{Role: "system", Content: json.RawMessage(`"You are helpful"`)},
		{Role: "user", Content: json.RawMessage(`"Hello"`)},
	}
	h1, err := HashMessages(msgs)
	if err != nil {
		t.Fatalf("HashMessages error: %v", err)
	}
	h2, err := HashMessages(msgs)
	if err != nil {
		t.Fatalf("HashMessages error: %v", err)
	}
	if h1 != h2 {
		t.Errorf("hashes differ for identical input: %s vs %s", h1, h2)
	}
	if len(h1) != 64 {
		t.Errorf("expected 64 hex chars, got %d", len(h1))
	}
}

func TestHashMessages_DifferentContent(t *testing.T) {
	msgs1 := []Message{{Role: "user", Content: json.RawMessage(`"Hello"`)}}
	msgs2 := []Message{{Role: "user", Content: json.RawMessage(`"World"`)}}

	h1, _ := HashMessages(msgs1)
	h2, _ := HashMessages(msgs2)
	if h1 == h2 {
		t.Error("hashes should differ for different content")
	}
}

func TestHashPrefix(t *testing.T) {
	msgs := []Message{
		{Role: "system", Content: json.RawMessage(`"System"`)},
		{Role: "user", Content: json.RawMessage(`"Hello"`)},
		{Role: "assistant", Content: json.RawMessage(`"Hi"`)},
	}

	h1, err := HashPrefix(msgs, 1)
	if err != nil {
		t.Fatalf("HashPrefix(1) error: %v", err)
	}
	h2, err := HashPrefix(msgs, 2)
	if err != nil {
		t.Fatalf("HashPrefix(2) error: %v", err)
	}
	if h1 == h2 {
		t.Error("prefix hashes with different lengths should differ")
	}

	// Prefix hash should match full hash of same slice.
	hFull, _ := HashMessages(msgs[:2])
	if h2 != hFull {
		t.Error("HashPrefix(2) should equal HashMessages of first 2")
	}
}

func TestHashPrefix_ZeroGuard(t *testing.T) {
	msgs := []Message{{Role: "user", Content: json.RawMessage(`"Hi"`)}}
	_, err := HashPrefix(msgs, 0)
	if err == nil {
		t.Error("expected error for prefix_len 0")
	}
}

func TestHashPrefix_NegativeGuard(t *testing.T) {
	msgs := []Message{{Role: "user", Content: json.RawMessage(`"Hi"`)}}
	_, err := HashPrefix(msgs, -1)
	if err == nil {
		t.Error("expected error for negative prefix_len")
	}
}

func TestHashPrefix_ExceedsCount(t *testing.T) {
	msgs := []Message{{Role: "user", Content: json.RawMessage(`"Hi"`)}}
	_, err := HashPrefix(msgs, 5)
	if err == nil {
		t.Error("expected error for prefix_len exceeding message count")
	}
}

func TestComplexHash_ToolCalls(t *testing.T) {
	// Two messages with identical role but different tool_calls should hash differently.
	m1 := Message{
		Role:      "assistant",
		ToolCalls: json.RawMessage(`[{"id":"call_1","function":{"name":"get_weather","arguments":"{\"city\":\"NYC\"}"}}]`),
	}
	m2 := Message{
		Role:      "assistant",
		ToolCalls: json.RawMessage(`[{"id":"call_2","function":{"name":"get_time","arguments":"{}"}}]`),
	}

	h1, _ := HashMessages([]Message{m1})
	h2, _ := HashMessages([]Message{m2})
	if h1 == h2 {
		t.Error("messages with different tool_calls should hash differently due to ComplexHash")
	}
}

func TestComplexHash_FunctionCall(t *testing.T) {
	m1 := Message{
		Role:         "assistant",
		FunctionCall: json.RawMessage(`{"name":"get_weather","arguments":"{}"}`),
	}
	m2 := Message{
		Role:         "assistant",
		FunctionCall: json.RawMessage(`{"name":"get_time","arguments":"{}"}`),
	}
	h1, _ := HashMessages([]Message{m1})
	h2, _ := HashMessages([]Message{m2})
	if h1 == h2 {
		t.Error("messages with different function_call should hash differently")
	}
}

// Tests for review finding #1: multimodal content collision
func TestComplexHash_MultimodalContent(t *testing.T) {
	// Two messages with different image URLs but same role should hash differently
	m1 := Message{
		Role:    "user",
		Content: json.RawMessage(`[{"type":"text","text":"Describe"},{"type":"image_url","image_url":{"url":"http://example.com/cat.jpg"}}]`),
	}
	m2 := Message{
		Role:    "user",
		Content: json.RawMessage(`[{"type":"text","text":"Describe"},{"type":"image_url","image_url":{"url":"http://example.com/dog.jpg"}}]`),
	}
	h1, _ := HashMessages([]Message{m1})
	h2, _ := HashMessages([]Message{m2})
	if h1 == h2 {
		t.Error("messages with different image URLs should hash differently due to ComplexHash on multimodal content")
	}
}

func TestComplexHash_TextOnlyArray(t *testing.T) {
	// Text-only array content - should still get ComplexHash but hash must be stable
	m := Message{
		Role:    "user",
		Content: json.RawMessage(`[{"type":"text","text":"Hello"}]`),
	}
	h1, _ := HashMessages([]Message{m})
	h2, _ := HashMessages([]Message{m})
	if h1 != h2 {
		t.Error("same text-only array message should hash identically")
	}
}

func TestIsComplexContent(t *testing.T) {
	tests := []struct {
		name string
		raw  json.RawMessage
		want bool
	}{
		{"nil", nil, false},
		{"empty", json.RawMessage{}, false},
		{"plain string", json.RawMessage(`"hello"`), false},
		{"array", json.RawMessage(`[{"type":"text","text":"hi"}]`), true},
		{"multimodal array", json.RawMessage(`[{"type":"image_url","image_url":{}}]`), true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isComplexContent(tt.raw)
			if got != tt.want {
				t.Errorf("isComplexContent(%q) = %v, want %v", string(tt.raw), got, tt.want)
			}
		})
	}
}
