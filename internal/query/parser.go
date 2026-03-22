package query

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// TermKind identifies the type of a parsed query term.
type TermKind int

const (
	TermText    TermKind = iota // bare word or "quoted phrase" or text:value
	TermStatus                  // status:200 or status:4xx
	TermMethod                  // method:POST
	TermPath                    // path:/v1/chat/completions
	TermSince                   // since:24h or since:2026-03-01T10:00:00Z
	TermUntil                   // until:24h
	TermIsFlag                  // is:req-truncated, is:resp-truncated, is:error
	TermHasFlag                 // has:req, has:resp
)

// Term is a single parsed query term.
type Term struct {
	Kind    TermKind
	Value   string
	Negated bool
}

// ParseError represents a query parse failure with position info.
type ParseError struct {
	Message  string `json:"message"`
	Position int    `json:"position"`
	Token    string `json:"token"`
}

func (e *ParseError) Error() string {
	return e.Message
}

// tokenize splits the input on whitespace, respecting double-quoted strings
// and escaped quotes. It returns tokens and their byte positions in the input.
func tokenize(input string) (tokens []string, positions []int) {
	i := 0
	for i < len(input) {
		// skip whitespace
		for i < len(input) && (input[i] == ' ' || input[i] == '\t') {
			i++
		}
		if i >= len(input) {
			break
		}

		start := i
		var b strings.Builder

		// Check for negation prefix before a quoted string
		if input[i] == '-' && i+1 < len(input) && input[i+1] == '"' {
			b.WriteByte('-')
			i++ // skip '-', now pointing at '"'
		}

		if i < len(input) && input[i] == '"' {
			// quoted string — include the surrounding quotes in the token
			b.WriteByte('"')
			i++ // skip opening quote
			for i < len(input) && input[i] != '"' {
				if input[i] == '\\' && i+1 < len(input) && input[i+1] == '"' {
					b.WriteByte('"')
					i += 2
				} else {
					b.WriteByte(input[i])
					i++
				}
			}
			if i < len(input) {
				b.WriteByte('"') // closing quote
				i++              // skip closing quote
			}
		} else {
			// unquoted token — read until whitespace
			for i < len(input) && input[i] != ' ' && input[i] != '\t' {
				b.WriteByte(input[i])
				i++
			}
		}

		tok := b.String()
		if tok != "" {
			tokens = append(tokens, tok)
			positions = append(positions, start)
		}
	}
	return tokens, positions
}

var validIsFlags = map[string]bool{
	"req-truncated":  true,
	"resp-truncated": true,
	"error":          true,
}

var validHasFlags = map[string]bool{
	"req":  true,
	"resp": true,
}

var knownPrefixes = map[string]TermKind{
	"text":   TermText,
	"status": TermStatus,
	"method": TermMethod,
	"path":   TermPath,
	"since":  TermSince,
	"until":  TermUntil,
	"is":     TermIsFlag,
	"has":    TermHasFlag,
}

// validStatusValue returns true for exact numeric codes or Nxx wildcard patterns.
func validStatusValue(v string) bool {
	if _, err := strconv.Atoi(v); err == nil {
		return true
	}
	if len(v) == 3 && v[0] >= '1' && v[0] <= '9' &&
		v[1] == 'x' && v[2] == 'x' {
		return true
	}
	return false
}

// validTimeValue returns true if the value is a relative duration or RFC3339 timestamp.
func validTimeValue(v string) bool {
	if _, err := time.Parse(time.RFC3339, v); err == nil {
		return true
	}
	if len(v) < 2 {
		return false
	}
	unit := v[len(v)-1]
	numStr := v[:len(v)-1]
	if _, err := strconv.ParseFloat(numStr, 64); err != nil {
		return false
	}
	switch unit {
	case 's', 'm', 'h', 'd':
		return true
	}
	return false
}

// Parse tokenizes and parses a query string into a slice of Terms.
func Parse(input string) ([]Term, error) {
	tokens, positions := tokenize(input)
	if len(tokens) == 0 {
		return nil, nil
	}

	terms := make([]Term, 0, len(tokens))
	for i, tok := range tokens {
		pos := positions[i]
		negated := false
		raw := tok

		// Handle negation prefix
		if strings.HasPrefix(tok, "-") && len(tok) > 1 {
			negated = true
			tok = tok[1:]
		}

		// Check for quoted phrase — strip exactly the surrounding quotes
		if strings.HasPrefix(tok, "\"") {
			val := strings.TrimSuffix(tok[1:], "\"")
			terms = append(terms, Term{Kind: TermText, Value: val, Negated: negated})
			continue
		}

		// Check for key:value
		if idx := strings.IndexByte(tok, ':'); idx > 0 {
			key := tok[:idx]
			val := tok[idx+1:]

			kind, known := knownPrefixes[key]
			if !known {
				return nil, &ParseError{
					Message:  fmt.Sprintf("unknown query prefix %q", key),
					Position: pos,
					Token:    raw,
				}
			}

			if val == "" {
				return nil, &ParseError{
					Message:  fmt.Sprintf("missing value for %q", key),
					Position: pos,
					Token:    raw,
				}
			}

			switch kind {
			case TermStatus:
				if !validStatusValue(val) {
					return nil, &ParseError{
						Message:  fmt.Sprintf("invalid status value %q (expected number or Nxx pattern)", val),
						Position: pos,
						Token:    raw,
					}
				}
			case TermIsFlag:
				if !validIsFlags[val] {
					return nil, &ParseError{
						Message:  fmt.Sprintf("unknown is-flag %q (expected req-truncated, resp-truncated, or error)", val),
						Position: pos,
						Token:    raw,
					}
				}
			case TermHasFlag:
				if !validHasFlags[val] {
					return nil, &ParseError{
						Message:  fmt.Sprintf("unknown has-flag %q (expected req or resp)", val),
						Position: pos,
						Token:    raw,
					}
				}
			case TermSince, TermUntil:
				if !validTimeValue(val) {
					return nil, &ParseError{
						Message:  fmt.Sprintf("invalid time value %q", val),
						Position: pos,
						Token:    raw,
					}
				}
			}

			terms = append(terms, Term{Kind: kind, Value: val, Negated: negated})
			continue
		}

		// Bare word → text search
		terms = append(terms, Term{Kind: TermText, Value: tok, Negated: negated})
	}
	return terms, nil
}

// parseTimeArg converts a relative duration string (e.g. "24h", "30m") or
// RFC3339 timestamp into a Unix-millisecond timestamp.
func parseTimeArg(s string) (int64, error) {
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t.UnixMilli(), nil
	}
	s = strings.TrimSpace(s)
	if len(s) < 2 {
		return 0, fmt.Errorf("invalid time %q", s)
	}
	unit := s[len(s)-1]
	numStr := s[:len(s)-1]
	num, err := strconv.ParseFloat(numStr, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid time %q", s)
	}
	var d time.Duration
	switch unit {
	case 's':
		d = time.Duration(num * float64(time.Second))
	case 'm':
		d = time.Duration(num * float64(time.Minute))
	case 'h':
		d = time.Duration(num * float64(time.Hour))
	case 'd':
		d = time.Duration(num * 24 * float64(time.Hour))
	default:
		return 0, fmt.Errorf("unknown time unit %q", string(unit))
	}
	return time.Now().Add(-d).UnixMilli(), nil
}
