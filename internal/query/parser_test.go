package query

import (
	"strings"
	"testing"
	"time"
)

// --- Parse tests ---

func TestParse_BasicTerms(t *testing.T) {
	tests := []struct {
		input string
		kind  TermKind
		value string
	}{
		{"status:200", TermStatus, "200"},
		{"method:POST", TermMethod, "POST"},
		{"path:/v1/chat/completions", TermPath, "/v1/chat/completions"},
		{"is:error", TermIsFlag, "error"},
		{"is:req-truncated", TermIsFlag, "req-truncated"},
		{"is:resp-truncated", TermIsFlag, "resp-truncated"},
		{"has:req", TermHasFlag, "req"},
		{"has:resp", TermHasFlag, "resp"},
		{"text:explicit", TermText, "explicit"},
	}
	for _, tc := range tests {
		terms, err := Parse(tc.input)
		if err != nil {
			t.Errorf("Parse(%q) error: %v", tc.input, err)
			continue
		}
		if len(terms) != 1 {
			t.Errorf("Parse(%q) got %d terms, want 1", tc.input, len(terms))
			continue
		}
		if terms[0].Kind != tc.kind {
			t.Errorf("Parse(%q) kind = %d, want %d", tc.input, terms[0].Kind, tc.kind)
		}
		if terms[0].Value != tc.value {
			t.Errorf("Parse(%q) value = %q, want %q", tc.input, terms[0].Value, tc.value)
		}
		if terms[0].Negated {
			t.Errorf("Parse(%q) negated = true, want false", tc.input)
		}
	}
}

func TestParse_BareWord(t *testing.T) {
	terms, err := Parse("bare")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(terms) != 1 || terms[0].Kind != TermText || terms[0].Value != "bare" {
		t.Fatalf("got %+v, want TermText/bare", terms)
	}
}

func TestParse_QuotedPhrases(t *testing.T) {
	terms, err := Parse(`"hello world"`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(terms) != 1 {
		t.Fatalf("got %d terms, want 1", len(terms))
	}
	if terms[0].Kind != TermText || terms[0].Value != "hello world" {
		t.Fatalf("got %+v, want TermText/'hello world'", terms[0])
	}
}

func TestParse_QuotedWithEscapedQuote(t *testing.T) {
	terms, err := Parse(`"hello \"world\""`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(terms) != 1 || terms[0].Value != `hello "world"` {
		t.Fatalf("got %+v", terms)
	}
}

func TestParse_Negation(t *testing.T) {
	tests := []struct {
		input string
		kind  TermKind
		value string
	}{
		{"-status:200", TermStatus, "200"},
		{`-"excluded phrase"`, TermText, "excluded phrase"},
		{"-method:GET", TermMethod, "GET"},
	}
	for _, tc := range tests {
		terms, err := Parse(tc.input)
		if err != nil {
			t.Errorf("Parse(%q) error: %v", tc.input, err)
			continue
		}
		if len(terms) != 1 || !terms[0].Negated {
			t.Errorf("Parse(%q) got %+v, expected negated", tc.input, terms)
			continue
		}
		if terms[0].Kind != tc.kind || terms[0].Value != tc.value {
			t.Errorf("Parse(%q) = {kind:%d val:%q}, want {kind:%d val:%q}",
				tc.input, terms[0].Kind, terms[0].Value, tc.kind, tc.value)
		}
	}
}

func TestParse_StatusWildcard(t *testing.T) {
	for _, v := range []string{"2xx", "3xx", "4xx", "5xx"} {
		terms, err := Parse("status:" + v)
		if err != nil {
			t.Errorf("Parse(status:%s) error: %v", v, err)
			continue
		}
		if terms[0].Kind != TermStatus || terms[0].Value != v {
			t.Errorf("got %+v, want TermStatus/%s", terms[0], v)
		}
	}
}

func TestParse_TimeTerms(t *testing.T) {
	tests := []struct {
		input string
		kind  TermKind
		value string
	}{
		{"since:24h", TermSince, "24h"},
		{"since:30m", TermSince, "30m"},
		{"since:7d", TermSince, "7d"},
		{"since:60s", TermSince, "60s"},
		{"until:24h", TermUntil, "24h"},
		{"since:2026-03-01T10:00:00Z", TermSince, "2026-03-01T10:00:00Z"},
		{"until:2026-03-01T10:00:00Z", TermUntil, "2026-03-01T10:00:00Z"},
	}
	for _, tc := range tests {
		terms, err := Parse(tc.input)
		if err != nil {
			t.Errorf("Parse(%q) error: %v", tc.input, err)
			continue
		}
		if len(terms) != 1 || terms[0].Kind != tc.kind || terms[0].Value != tc.value {
			t.Errorf("Parse(%q) = %+v, want kind=%d val=%q", tc.input, terms, tc.kind, tc.value)
		}
	}
}

func TestParse_InvalidTerms(t *testing.T) {
	tests := []struct {
		input   string
		wantMsg string
	}{
		{"status:abc", "invalid status value"},
		{"is:unknown", "unknown is-flag"},
		{"has:unknown", "unknown has-flag"},
		{"unknown:value", "unknown query prefix"},
	}
	for _, tc := range tests {
		_, err := Parse(tc.input)
		if err == nil {
			t.Errorf("Parse(%q) expected error, got nil", tc.input)
			continue
		}
		pe, ok := err.(*ParseError)
		if !ok {
			t.Errorf("Parse(%q) error type = %T, want *ParseError", tc.input, err)
			continue
		}
		if !strings.Contains(pe.Message, tc.wantMsg) {
			t.Errorf("Parse(%q) message = %q, want substring %q", tc.input, pe.Message, tc.wantMsg)
		}
		if pe.Token == "" {
			t.Errorf("Parse(%q) ParseError.Token is empty", tc.input)
		}
	}
}

func TestParse_EmptyInput(t *testing.T) {
	terms, err := Parse("")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(terms) != 0 {
		t.Fatalf("expected empty slice, got %+v", terms)
	}

	terms, err = Parse("   ")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(terms) != 0 {
		t.Fatalf("expected empty slice, got %+v", terms)
	}
}

func TestParse_MultipleTerms(t *testing.T) {
	terms, err := Parse(`status:5xx method:POST since:24h "hello world" -status:200`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(terms) != 5 {
		t.Fatalf("got %d terms, want 5", len(terms))
	}
	// status:5xx
	if terms[0].Kind != TermStatus || terms[0].Value != "5xx" || terms[0].Negated {
		t.Errorf("term 0 = %+v", terms[0])
	}
	// method:POST
	if terms[1].Kind != TermMethod || terms[1].Value != "POST" || terms[1].Negated {
		t.Errorf("term 1 = %+v", terms[1])
	}
	// since:24h
	if terms[2].Kind != TermSince || terms[2].Value != "24h" || terms[2].Negated {
		t.Errorf("term 2 = %+v", terms[2])
	}
	// "hello world"
	if terms[3].Kind != TermText || terms[3].Value != "hello world" || terms[3].Negated {
		t.Errorf("term 3 = %+v", terms[3])
	}
	// -status:200
	if terms[4].Kind != TermStatus || terms[4].Value != "200" || !terms[4].Negated {
		t.Errorf("term 4 = %+v", terms[4])
	}
}

// --- ToSQL tests ---

func TestToSQL_BasicTerms(t *testing.T) {
	terms := []Term{
		{Kind: TermStatus, Value: "200"},
		{Kind: TermMethod, Value: "POST"},
	}
	where, args := ToSQL(terms)
	if !strings.Contains(where, "status_code = ?") {
		t.Errorf("where missing status condition: %s", where)
	}
	if !strings.Contains(where, "UPPER(request_method) = UPPER(?)") {
		t.Errorf("where missing method condition: %s", where)
	}
	if len(args) != 2 {
		t.Errorf("args count = %d, want 2", len(args))
	}
	if args[0] != 200 {
		t.Errorf("args[0] = %v, want 200", args[0])
	}
	if args[1] != "POST" {
		t.Errorf("args[1] = %v, want POST", args[1])
	}
}

func TestToSQL_StatusWildcard(t *testing.T) {
	terms := []Term{{Kind: TermStatus, Value: "4xx"}}
	where, args := ToSQL(terms)
	if !strings.Contains(where, "status_code BETWEEN ? AND ?") {
		t.Errorf("where = %s, want BETWEEN", where)
	}
	if len(args) != 2 || args[0] != 400 || args[1] != 499 {
		t.Errorf("args = %v, want [400 499]", args)
	}
}

func TestToSQL_TextTermsCombined(t *testing.T) {
	terms := []Term{
		{Kind: TermText, Value: "hello"},
		{Kind: TermText, Value: "world"},
	}
	where, args := ToSQL(terms)
	// Should be a single FTS MATCH with both terms
	matchCount := strings.Count(where, "MATCH ?")
	if matchCount != 1 {
		t.Errorf("expected 1 MATCH clause, got %d in: %s", matchCount, where)
	}
	if len(args) != 1 {
		t.Fatalf("args count = %d, want 1", len(args))
	}
	matchVal := args[0].(string)
	if !strings.Contains(matchVal, "hello") || !strings.Contains(matchVal, "world") {
		t.Errorf("match expr = %q, want both hello and world", matchVal)
	}
}

func TestToSQL_TextQuotedPhrase(t *testing.T) {
	terms := []Term{
		{Kind: TermText, Value: "hello world"},
	}
	_, args := ToSQL(terms)
	matchVal := args[0].(string)
	// Phrases with spaces should be quoted for FTS5
	if matchVal != `"hello world"` {
		t.Errorf("match expr = %q, want %q", matchVal, `"hello world"`)
	}
}

func TestToSQL_Negation(t *testing.T) {
	terms := []Term{
		{Kind: TermStatus, Value: "200", Negated: true},
	}
	where, args := ToSQL(terms)
	if !strings.Contains(where, "NOT (status_code = ?)") {
		t.Errorf("where = %s, want NOT wrapped", where)
	}
	if len(args) != 1 || args[0] != 200 {
		t.Errorf("args = %v", args)
	}
}

func TestToSQL_NegatedText(t *testing.T) {
	terms := []Term{
		{Kind: TermText, Value: "excluded", Negated: true},
	}
	where, args := ToSQL(terms)
	if !strings.Contains(where, "NOT (id IN") {
		t.Errorf("where = %s, want NOT wrapped FTS", where)
	}
	if len(args) != 1 || args[0] != "excluded" {
		t.Errorf("args = %v", args)
	}
}

func TestToSQL_NoTerms(t *testing.T) {
	where, args := ToSQL(nil)
	if where != "" {
		t.Errorf("where = %q, want empty", where)
	}
	if args != nil {
		t.Errorf("args = %v, want nil", args)
	}
}

func TestToSQL_IsFlag(t *testing.T) {
	terms := []Term{
		{Kind: TermIsFlag, Value: "error"},
		{Kind: TermIsFlag, Value: "req-truncated"},
		{Kind: TermIsFlag, Value: "resp-truncated"},
	}
	where, args := ToSQL(terms)
	if !strings.Contains(where, "error IS NOT NULL AND error != ''") {
		t.Errorf("missing error condition in: %s", where)
	}
	if !strings.Contains(where, "req_truncated = 1") {
		t.Errorf("missing req_truncated condition in: %s", where)
	}
	if !strings.Contains(where, "resp_truncated = 1") {
		t.Errorf("missing resp_truncated condition in: %s", where)
	}
	// is-flags produce no args
	if len(args) != 0 {
		t.Errorf("args = %v, want empty", args)
	}
}

func TestToSQL_HasFlag(t *testing.T) {
	terms := []Term{
		{Kind: TermHasFlag, Value: "req"},
		{Kind: TermHasFlag, Value: "resp"},
	}
	where, _ := ToSQL(terms)
	if !strings.Contains(where, "req_bytes > 0") {
		t.Errorf("missing req_bytes condition in: %s", where)
	}
	if !strings.Contains(where, "resp_bytes > 0") {
		t.Errorf("missing resp_bytes condition in: %s", where)
	}
}

func TestToSQL_PathTerm(t *testing.T) {
	terms := []Term{{Kind: TermPath, Value: "/v1/chat/completions"}}
	where, args := ToSQL(terms)
	if !strings.Contains(where, "request_path = ?") {
		t.Errorf("where = %s", where)
	}
	if len(args) != 1 || args[0] != "/v1/chat/completions" {
		t.Errorf("args = %v", args)
	}
}

func TestToSQL_TimeSince(t *testing.T) {
	terms := []Term{{Kind: TermSince, Value: "24h"}}
	before := time.Now().Add(-24 * time.Hour).UnixMilli()
	where, args := ToSQL(terms)
	after := time.Now().Add(-24 * time.Hour).UnixMilli()

	if !strings.Contains(where, "ts_start >= ?") {
		t.Errorf("where = %s", where)
	}
	if len(args) != 1 {
		t.Fatalf("args count = %d", len(args))
	}
	ms := args[0].(int64)
	if ms < before-1000 || ms > after+1000 {
		t.Errorf("time arg %d not in expected range [%d, %d]", ms, before-1000, after+1000)
	}
}

func TestToSQL_TimeUntil(t *testing.T) {
	terms := []Term{{Kind: TermUntil, Value: "2026-03-01T10:00:00Z"}}
	where, args := ToSQL(terms)
	if !strings.Contains(where, "ts_start <= ?") {
		t.Errorf("where = %s", where)
	}
	expected, _ := time.Parse(time.RFC3339, "2026-03-01T10:00:00Z")
	if args[0] != expected.UnixMilli() {
		t.Errorf("args[0] = %v, want %d", args[0], expected.UnixMilli())
	}
}

func TestToSQL_NoStringInterpolation(t *testing.T) {
	// SQL injection attempt — should be safely parameterized
	terms, err := Parse(`"; DROP TABLE`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	where, args := ToSQL(terms)
	// The dangerous string must never appear literally in the WHERE clause
	if strings.Contains(where, "DROP") {
		t.Fatalf("SQL injection: DROP found in where clause: %s", where)
	}
	// It should appear only as a parameterized arg in the FTS MATCH
	if len(args) == 0 {
		t.Fatal("expected args, got none")
	}
	// Verify ? count matches args length
	qCount := strings.Count(where, "?")
	if qCount != len(args) {
		t.Errorf("? count = %d but len(args) = %d", qCount, len(args))
	}
}

func TestToSQL_ParameterCountMatchesPlaceholders(t *testing.T) {
	terms, err := Parse(`status:5xx method:POST hello "world wide" -status:200 since:1h is:error has:req path:/api`)
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}
	where, args := ToSQL(terms)
	qCount := strings.Count(where, "?")
	if qCount != len(args) {
		t.Errorf("placeholder count %d != args count %d\nwhere: %s\nargs: %v", qCount, len(args), where, args)
	}
}

func TestSanitizeFTS5Token(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{`hello`, `hello`},
		{`he said "hello`, `he said hello`},
		{`test*`, `test`},
		{`(group)`, `group`},
		{`OR`, ``},
		{`AND`, ``},
		{`NOT`, ``},
		{`NEAR`, ``},
		{`normal`, `normal`},
		{`he^llo`, `hello`},
	}
	for _, tt := range tests {
		got := sanitizeFTS5Token(tt.input)
		if got != tt.want {
			t.Errorf("sanitizeFTS5Token(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestBuildFTSMatch_Sanitization(t *testing.T) {
	// Unmatched quotes should be stripped, not cause FTS5 errors
	result := buildFTSMatch([]string{`he said "hello`})
	if strings.Contains(result, `"`) && strings.Count(result, `"`)%2 != 0 {
		t.Errorf("buildFTSMatch produced unbalanced quotes: %q", result)
	}
	// FTS5 keywords as search terms should be dropped
	result2 := buildFTSMatch([]string{"OR", "hello"})
	if strings.Contains(result2, "OR") {
		t.Errorf("buildFTSMatch should drop FTS5 keyword OR: %q", result2)
	}
}
