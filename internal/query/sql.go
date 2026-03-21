package query

import (
	"fmt"
	"strconv"
	"strings"
)

// ToSQL converts parsed terms into a WHERE clause and parameterized args.
// Returns empty string and nil args if there are no terms.
// All user values are passed as ? parameters — never interpolated into the SQL string.
func ToSQL(terms []Term) (where string, args []interface{}) {
	if len(terms) == 0 {
		return "", nil
	}

	// Collect text terms separately to combine into a single FTS MATCH.
	var positiveTexts []string
	var negativeTexts []string
	var conditions []string

	for _, t := range terms {
		switch t.Kind {
		case TermText:
			if t.Negated {
				negativeTexts = append(negativeTexts, t.Value)
			} else {
				positiveTexts = append(positiveTexts, t.Value)
			}

		case TermStatus:
			cond, cArgs := statusCondition(t)
			conditions = append(conditions, cond)
			args = append(args, cArgs...)

		case TermMethod:
			cond := wrap("UPPER(request_method) = UPPER(?)", t.Negated)
			conditions = append(conditions, cond)
			args = append(args, t.Value)

		case TermPath:
			cond := wrap("request_path = ?", t.Negated)
			conditions = append(conditions, cond)
			args = append(args, t.Value)

		case TermSince:
			ms, _ := parseTimeArg(t.Value) // already validated by parser
			cond := wrap("ts_start >= ?", t.Negated)
			conditions = append(conditions, cond)
			args = append(args, ms)

		case TermUntil:
			ms, _ := parseTimeArg(t.Value)
			cond := wrap("ts_start <= ?", t.Negated)
			conditions = append(conditions, cond)
			args = append(args, ms)

		case TermIsFlag:
			cond := isFlagCondition(t)
			conditions = append(conditions, cond)

		case TermHasFlag:
			cond := hasFlagCondition(t)
			conditions = append(conditions, cond)
		}
	}

	// Build the combined FTS condition for positive text terms.
	if len(positiveTexts) > 0 {
		matchExpr := buildFTSMatch(positiveTexts)
		cond := "id IN (SELECT rowid FROM openai_logs_fts WHERE openai_logs_fts MATCH ?)"
		conditions = append([]string{cond}, conditions...)
		args = append([]interface{}{matchExpr}, args...)
	}

	// Each negated text term becomes its own NOT(FTS MATCH).
	for _, nt := range negativeTexts {
		matchExpr := buildFTSMatch([]string{nt})
		cond := "NOT (id IN (SELECT rowid FROM openai_logs_fts WHERE openai_logs_fts MATCH ?))"
		conditions = append(conditions, cond)
		args = append(args, matchExpr)
	}

	where = strings.Join(conditions, " AND ")
	return where, args
}

// buildFTSMatch creates the FTS5 MATCH expression. Multiple terms are
// space-separated which FTS5 treats as implicit AND. Phrases containing
// spaces are wrapped in double quotes for FTS5.
func buildFTSMatch(texts []string) string {
	parts := make([]string, 0, len(texts))
	for _, t := range texts {
		if strings.ContainsAny(t, " \t") {
			parts = append(parts, fmt.Sprintf(`"%s"`, t))
		} else {
			parts = append(parts, t)
		}
	}
	return strings.Join(parts, " ")
}

func statusCondition(t Term) (string, []interface{}) {
	v := t.Value
	// Wildcard pattern like 4xx, 5xx
	if len(v) == 3 && v[1] == 'x' && v[2] == 'x' {
		base, _ := strconv.Atoi(string(v[0]) + "00")
		cond := wrap("status_code BETWEEN ? AND ?", t.Negated)
		return cond, []interface{}{base, base + 99}
	}
	// Exact status code
	code, _ := strconv.Atoi(v)
	cond := wrap("status_code = ?", t.Negated)
	return cond, []interface{}{code}
}

func isFlagCondition(t Term) string {
	var expr string
	switch t.Value {
	case "req-truncated":
		expr = "req_truncated = 1"
	case "resp-truncated":
		expr = "resp_truncated = 1"
	case "error":
		expr = "error IS NOT NULL AND error != ''"
	}
	return wrap(expr, t.Negated)
}

func hasFlagCondition(t Term) string {
	var expr string
	switch t.Value {
	case "req":
		expr = "req_bytes > 0"
	case "resp":
		expr = "resp_bytes > 0"
	}
	return wrap(expr, t.Negated)
}

// wrap optionally wraps an expression in NOT (...).
func wrap(expr string, negated bool) string {
	if negated {
		return "NOT (" + expr + ")"
	}
	return expr
}
