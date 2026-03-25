package chat

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
)

// CanonicalMessage is the normalized representation of one message used for
// hashing. Only fields that define conversation identity are included.
type CanonicalMessage struct {
	Role        string `json:"role"`
	Content     string `json:"content"`
	ComplexHash string `json:"complex_hash,omitempty"`
}

// canonicalize converts a Message to its canonical form for hashing.
// Tool calls and function calls are hashed via ComplexHash to avoid collisions
// when the text content is empty.
func canonicalize(m Message) CanonicalMessage {
	cm := CanonicalMessage{
		Role:    m.Role,
		Content: ExtractContentString(m.Content),
	}

	// If the message has tool_calls or function_call, compute ComplexHash from
	// their compacted JSON bytes so that otherwise-identical messages with
	// different tool calls produce distinct hashes.
	if len(m.ToolCalls) > 0 {
		cm.ComplexHash = compactHash(m.ToolCalls)
	} else if len(m.FunctionCall) > 0 {
		cm.ComplexHash = compactHash(m.FunctionCall)
	}

	return cm
}

// compactHash returns the hex SHA-256 of the JSON-compacted input bytes.
func compactHash(raw json.RawMessage) string {
	var buf bytes.Buffer
	if err := json.Compact(&buf, raw); err != nil {
		// If compaction fails, hash the raw bytes as-is.
		h := sha256.Sum256(raw)
		return fmt.Sprintf("%x", h)
	}
	h := sha256.Sum256(buf.Bytes())
	return fmt.Sprintf("%x", h)
}

// HashMessages computes a deterministic SHA-256 hex digest over all messages.
// The messages are first canonicalized, then serialized to JSON. The hash
// represents the full conversation identity.
func HashMessages(msgs []Message) (string, error) {
	canonical := make([]CanonicalMessage, len(msgs))
	for i, m := range msgs {
		canonical[i] = canonicalize(m)
	}
	data, err := json.Marshal(canonical)
	if err != nil {
		return "", fmt.Errorf("marshaling canonical messages: %w", err)
	}
	h := sha256.Sum256(data)
	return fmt.Sprintf("%x", h), nil
}

// HashPrefix computes the hash of the first prefixLen messages. Returns an
// error if prefixLen is out of range. Callers MUST ensure prefixLen > 0.
func HashPrefix(msgs []Message, prefixLen int) (string, error) {
	if prefixLen <= 0 {
		return "", fmt.Errorf("prefix_len must be > 0, got %d", prefixLen)
	}
	if prefixLen > len(msgs) {
		return "", fmt.Errorf("prefix_len %d exceeds message count %d", prefixLen, len(msgs))
	}
	return HashMessages(msgs[:prefixLen])
}
