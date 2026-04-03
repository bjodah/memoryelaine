package jsonellipsis

import (
	"bytes"
	"encoding/json"
	"strings"
	"unicode/utf8"
)

var DefaultKeys = map[string]bool{
	"prompt":    true,
	"content":   true,
	"text":      true,
	"arguments": true,
	"input":     true,
	"output":    true,
}

const DefaultLimit = 60
const DefaultMinDepth = 2

// Transform rewrites JSON string values for scan-oriented display.
//
// The transform preserves JSON structure semantically, but it does not promise
// byte-for-byte preservation of the original source formatting.
//
// Parameters:
//   - src:      raw JSON bytes
//   - limit:    maximum visible rune count for truncated JSON string values
//   - keys:     optional case-insensitive set of object keys whose values may be
//     truncated even at shallow depth; nil means all string values are
//     eligible
//   - minDepth: minimum nesting depth for truncation to apply unless the key is
//     listed in keys
//
// Returns:
//   - transformed JSON bytes
//   - changed=true if at least one string value was modified
//   - error if src is not valid JSON
func Transform(src []byte, limit int, keys map[string]bool, minDepth int) ([]byte, bool, error) {
	dec := json.NewDecoder(bytes.NewReader(src))
	dec.UseNumber()

	var raw any
	if err := dec.Decode(&raw); err != nil {
		return nil, false, err
	}

	changed := false
	result := walk(raw, "", keys, limit, minDepth, 0, &changed)

	out, err := json.Marshal(result)
	if err != nil {
		return nil, false, err
	}
	return out, changed, nil
}

func walk(v any, parentKey string, keys map[string]bool, limit, minDepth, depth int, changed *bool) any {
	switch val := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(val))
		for k, child := range val {
			out[k] = walk(child, k, keys, limit, minDepth, depth+1, changed)
		}
		return out
	case []any:
		out := make([]any, len(val))
		for i, child := range val {
			out[i] = walk(child, parentKey, keys, limit, minDepth, depth, changed)
		}
		return out
	case string:
		if eligible(parentKey, keys, depth, minDepth) && utf8.RuneCountInString(val) > limit {
			*changed = true
			return truncateRunes(val, limit) + "..."
		}
		return val
	default:
		return val
	}
}

func eligible(parentKey string, keys map[string]bool, depth, minDepth int) bool {
	if keys != nil && keys[strings.ToLower(parentKey)] {
		return true
	}
	return depth >= minDepth
}

func truncateRunes(s string, n int) string {
	count := 0
	for i := range s {
		if count == n {
			return s[:i]
		}
		count++
	}
	return s
}
