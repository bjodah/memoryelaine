package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"memoryelaine/internal/database"
	"memoryelaine/internal/streamview"
)

func makeTestModel(entry *database.LogEntry, svResult streamview.Result) Model {
	m := Model{
		mode:   modeDetail,
		detail: entry,
		streamView: streamViewState{
			mode:   streamview.ModeRaw,
			result: svResult,
		},
		width:  120,
		height: 40,
	}
	m.recomputeDetailBodies()
	return m
}

func sampleEntry() *database.LogEntry {
	body := "data: raw SSE data\n\n"
	return &database.LogEntry{
		ID:             1,
		TsStart:        1700000000000,
		ClientIP:       "127.0.0.1",
		RequestMethod:  "POST",
		RequestPath:    "/v1/chat/completions",
		UpstreamURL:    "https://api.openai.com/v1/chat/completions",
		ReqHeadersJSON: "{}",
		ReqBody:        `{"model":"gpt-4"}`,
		ReqBytes:       17,
		RespBody:       &body,
		RespBytes:      int64(len(body)),
	}
}

func TestDetailView_DefaultsToRaw(t *testing.T) {
	m := makeTestModel(sampleEntry(), streamview.Result{
		RawBody:            "data: raw SSE data\n\n",
		AssembledBody:      "Hello",
		AssembledAvailable: true,
		Reason:             streamview.ReasonSupported,
	})

	if m.streamView.mode != streamview.ModeRaw {
		t.Errorf("expected default mode %q, got %q", streamview.ModeRaw, m.streamView.mode)
	}

	output := m.View()
	if !strings.Contains(output, "Stream View: Raw [press v to toggle]") {
		t.Error("expected raw mode indicator in output")
	}
}

func TestDetailView_ToggleToAssembled(t *testing.T) {
	m := makeTestModel(sampleEntry(), streamview.Result{
		RawBody:            "data: raw SSE data\n\n",
		AssembledBody:      "Hello world",
		AssembledAvailable: true,
		Reason:             streamview.ReasonSupported,
	})

	// Press v to toggle
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'v'}})
	m = updated.(Model)

	if m.streamView.mode != streamview.ModeAssembled {
		t.Errorf("expected mode %q after toggle, got %q", streamview.ModeAssembled, m.streamView.mode)
	}

	output := m.View()
	if !strings.Contains(output, "Stream View: Assembled") {
		t.Error("expected assembled mode indicator in output")
	}
	if !strings.Contains(output, "Hello world") {
		t.Error("expected assembled body in output")
	}
}

func TestDetailView_ToggleBackToRaw(t *testing.T) {
	m := makeTestModel(sampleEntry(), streamview.Result{
		RawBody:            "data: raw SSE data\n\n",
		AssembledBody:      "Hello",
		AssembledAvailable: true,
		Reason:             streamview.ReasonSupported,
	})

	// Toggle to assembled
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'v'}})
	m = updated.(Model)
	// Toggle back to raw
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'v'}})
	m = updated.(Model)

	if m.streamView.mode != streamview.ModeRaw {
		t.Errorf("expected mode %q after double toggle, got %q", streamview.ModeRaw, m.streamView.mode)
	}
}

func TestDetailView_ToggleDoesNothingWhenUnavailable(t *testing.T) {
	m := makeTestModel(sampleEntry(), streamview.Result{
		RawBody:            "some data",
		AssembledAvailable: false,
		Reason:             streamview.ReasonNotSSE,
	})

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'v'}})
	m = updated.(Model)

	if m.streamView.mode != streamview.ModeRaw {
		t.Errorf("expected mode to remain %q, got %q", streamview.ModeRaw, m.streamView.mode)
	}
}

func TestDetailView_PartialParseWarning(t *testing.T) {
	m := makeTestModel(sampleEntry(), streamview.Result{
		RawBody:            "data: partial stream\n\n",
		AssembledBody:      "partial text",
		AssembledAvailable: true,
		Reason:             streamview.ReasonPartialParse,
	})

	// Toggle to assembled
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'v'}})
	m = updated.(Model)

	output := m.View()
	if !strings.Contains(output, "Assembled (partial parse)") {
		t.Error("expected partial parse warning in output")
	}
	if !strings.Contains(output, "partial text") {
		t.Error("expected partial assembled text in output")
	}
}

func TestDetailView_TruncatedShowsReason(t *testing.T) {
	m := makeTestModel(sampleEntry(), streamview.Result{
		RawBody:            "truncated data",
		AssembledAvailable: false,
		Reason:             streamview.ReasonTruncated,
	})

	output := m.View()
	if !strings.Contains(output, "assembled unavailable: truncated") {
		t.Error("expected truncated reason in output")
	}
}

func TestDetailView_AssembledTextRendered(t *testing.T) {
	m := makeTestModel(sampleEntry(), streamview.Result{
		RawBody:            "data: SSE chunks\n\n",
		AssembledBody:      "The complete response text",
		AssembledAvailable: true,
		Reason:             streamview.ReasonSupported,
	})

	// Toggle to assembled
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'v'}})
	m = updated.(Model)

	output := m.View()
	if !strings.Contains(output, "The complete response text") {
		t.Error("expected assembled text in rendered output")
	}
	if strings.Contains(output, "data: SSE chunks") {
		t.Error("should not show raw SSE data in assembled mode")
	}
}

func TestEllipsizeBody_JSON(t *testing.T) {
	body := `{"prompt":"` + strings.Repeat("a", 200) + `","model":"gpt-4"}`
	result := ellipsizeBody(body, 10000)
	if strings.Contains(result, strings.Repeat("a", 200)) {
		t.Error("expected long prompt value to be ellipsized")
	}
	if !strings.Contains(result, "...") {
		t.Error("expected ellipsis marker in output")
	}
	if !strings.Contains(result, "gpt-4") {
		t.Error("expected short values to be preserved")
	}
}

func TestEllipsizeBody_NonJSON(t *testing.T) {
	body := "plain text body"
	result := ellipsizeBody(body, 10000)
	if result != body {
		t.Errorf("expected non-JSON to pass through unchanged, got %q", result)
	}
}

func TestEllipsizeBody_NoChanges(t *testing.T) {
	body := `{"model":"gpt-4"}`
	result := ellipsizeBody(body, 10000)
	if !strings.Contains(result, "gpt-4") {
		t.Error("expected short JSON to pass through unchanged")
	}
	if strings.Contains(result, "...") {
		t.Error("expected no ellipsis for short values")
	}
}
