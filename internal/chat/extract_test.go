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
