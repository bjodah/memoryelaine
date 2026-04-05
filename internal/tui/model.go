package tui

import (
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"memoryelaine/internal/chat"
	"memoryelaine/internal/database"
	"memoryelaine/internal/jsonellipsis"
	"memoryelaine/internal/streamview"
)

type viewMode int

const (
	modeTable viewMode = iota
	modeDetail
	modeThread
)

type Model struct {
	mode                viewMode
	reader              *database.LogReader
	filter              database.QueryFilter
	entries             []database.LogSummary
	total               int64
	cursor              int
	detail              *database.LogEntry
	streamView          streamViewState
	detailReqBody       string // precomputed ellipsized request body
	detailRespBody      string // precomputed ellipsized response body
	scroll              int
	thread              []threadMessage
	threadScroll        int
	threadLogID         int64
	threadIndex         int
	threadTotal         int
	reasoningFolded     bool
	exportPrefixPending bool
	savePromptActive    bool
	savePromptPath      string
	savePromptKind      exportKind
	detailStatus        string
	err                 error
	width               int
	height              int
	quit                bool
}

type streamViewState struct {
	mode   streamview.Mode
	result streamview.Result
}

type threadMessage struct {
	Role    string
	Content string
	LogID   int64
}

type exportKind int

const (
	exportReqRaw exportKind = iota
	exportRespRaw
	exportAssembledContent
	exportAssembledReasoning
)

type logsLoadedMsg struct {
	entries []database.LogSummary
	total   int64
}
type logDetailMsg struct {
	entry      *database.LogEntry
	streamView streamview.Result
}
type threadLoadedMsg struct {
	messages           []threadMessage
	logID              int64
	selectedEntryIndex int
	totalEntries       int
}
type errMsg struct{ err error }

func initialModel(reader *database.LogReader) Model {
	return Model{
		mode:   modeTable,
		reader: reader,
		filter: database.DefaultQueryFilter(),
	}
}

func (m Model) Init() tea.Cmd {
	return m.loadLogs
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil

	case tea.KeyMsg:
		return m.handleKey(msg)

	case logsLoadedMsg:
		m.entries = msg.entries
		m.total = msg.total
		m.err = nil
		if m.cursor >= len(m.entries) && len(m.entries) > 0 {
			m.cursor = len(m.entries) - 1
		}
		return m, nil

	case logDetailMsg:
		m.detail = msg.entry
		m.streamView = streamViewState{
			mode:   defaultDetailMode(msg.streamView),
			result: msg.streamView,
		}
		m.mode = modeDetail
		m.scroll = 0
		m.reasoningFolded = true
		m.exportPrefixPending = false
		m.savePromptActive = false
		m.savePromptPath = ""
		m.detailStatus = ""
		m.recomputeDetailBodies()
		return m, nil

	case threadLoadedMsg:
		m.thread = msg.messages
		m.threadLogID = msg.logID
		m.threadIndex = msg.selectedEntryIndex
		m.threadTotal = msg.totalEntries
		m.threadScroll = 0
		return m, nil

	case errMsg:
		m.err = msg.err
		return m, nil
	}
	return m, nil
}

func (m Model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.mode == modeThread {
		switch msg.String() {
		case "esc", "q":
			m.mode = modeDetail
			m.thread = nil
			return m, nil
		case "j", "down":
			m.threadScroll++
			return m, nil
		case "k", "up":
			if m.threadScroll > 0 {
				m.threadScroll--
			}
			return m, nil
		}
		return m, nil
	}

	if m.mode == modeDetail {
		if m.savePromptActive {
			switch msg.Type {
			case tea.KeyEsc:
				m.savePromptActive = false
				m.detailStatus = "Export canceled"
				return m, nil
			case tea.KeyEnter:
				if err := m.commitExport(); err != nil {
					m.detailStatus = fmt.Sprintf("Export failed: %v", err)
				} else {
					m.detailStatus = fmt.Sprintf("Saved to %s", m.savePromptPath)
				}
				m.savePromptActive = false
				return m, nil
			case tea.KeyBackspace:
				if len(m.savePromptPath) > 0 {
					runes := []rune(m.savePromptPath)
					m.savePromptPath = string(runes[:len(runes)-1])
				}
				return m, nil
			case tea.KeyRunes:
				m.savePromptPath += string(msg.Runes)
				return m, nil
			}
			return m, nil
		}
		if m.exportPrefixPending {
			m.exportPrefixPending = false
			switch msg.String() {
			case "b":
				return m.startExportPrompt(exportReqRaw), nil
			case "B":
				return m.startExportPrompt(exportRespRaw), nil
			case "c":
				if !m.streamView.result.AssembledAvailable {
					m.detailStatus = "Assembled export unavailable"
					return m, nil
				}
				return m.startExportPrompt(exportAssembledContent), nil
			case "R":
				if !m.streamView.result.AssembledAvailable {
					m.detailStatus = "Assembled export unavailable"
					return m, nil
				}
				return m.startExportPrompt(exportAssembledReasoning), nil
			default:
				m.detailStatus = ""
				return m, nil
			}
		}
		switch msg.String() {
		case "esc", "q":
			m.mode = modeTable
			m.detail = nil
			return m, nil
		case "j", "down":
			m.scroll++
			return m, nil
		case "k", "up":
			if m.scroll > 0 {
				m.scroll--
			}
			return m, nil
		case "v":
			if m.streamView.result.AssembledAvailable {
				if m.streamView.mode == streamview.ModeRaw {
					m.streamView.mode = streamview.ModeAssembled
				} else {
					m.streamView.mode = streamview.ModeRaw
				}
				m.recomputeDetailBodies()
			}
			return m, nil
		case "z":
			if m.streamView.mode == streamview.ModeAssembled && m.streamView.result.HasReasoning {
				m.reasoningFolded = !m.reasoningFolded
				m.recomputeDetailBodies()
			}
			return m, nil
		case "x":
			m.exportPrefixPending = true
			m.detailStatus = "Export: b=req raw, B=resp raw, c=assembled content, R=assembled reasoning"
			return m, nil
		case "c":
			e := m.detail
			if e != nil && chat.IsChatPath(e.RequestPath) && !e.ReqTruncated {
				m.mode = modeThread
				return m, m.loadThread(m.detail.ID)
			}
			return m, nil
		}
		return m, nil
	}

	// Table mode
	switch msg.String() {
	case "q", "ctrl+c":
		m.quit = true
		return m, tea.Quit
	case "j", "down":
		if m.cursor < len(m.entries)-1 {
			m.cursor++
		}
		return m, nil
	case "k", "up":
		if m.cursor > 0 {
			m.cursor--
		}
		return m, nil
	case "enter":
		if len(m.entries) > 0 {
			return m, m.loadDetail(m.entries[m.cursor].ID)
		}
	case "r":
		return m, m.loadLogs
	case "n":
		if m.filter.Offset+m.filter.Limit < int(m.total) {
			m.filter.Offset += m.filter.Limit
			return m, m.loadLogs
		}
	case "p":
		if m.filter.Offset >= m.filter.Limit {
			m.filter.Offset -= m.filter.Limit
			return m, m.loadLogs
		}
	case "f":
		// Cycle status filter: all → 200 → 4xx → 5xx → all
		switch {
		case m.filter.StatusCode == nil:
			s := 200
			m.filter.StatusCode = &s
		case *m.filter.StatusCode == 200:
			s := 400
			m.filter.StatusCode = &s
		case *m.filter.StatusCode == 400:
			s := 500
			m.filter.StatusCode = &s
		default:
			m.filter.StatusCode = nil
		}
		m.filter.Offset = 0
		m.cursor = 0
		return m, m.loadLogs
	}
	return m, nil
}

func (m Model) View() string {
	if m.quit {
		return ""
	}
	if m.mode == modeThread && m.thread != nil {
		return m.threadView()
	}
	if m.mode == modeDetail && m.detail != nil {
		return m.detailView()
	}
	return m.tableView()
}

var (
	headerStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("12"))
	cursorStyle = lipgloss.NewStyle().Background(lipgloss.Color("236")).Bold(true)
	statusOK    = lipgloss.NewStyle().Foreground(lipgloss.Color("10"))
	statusWarn  = lipgloss.NewStyle().Foreground(lipgloss.Color("11"))
	statusErr   = lipgloss.NewStyle().Foreground(lipgloss.Color("9"))
	helpStyle   = lipgloss.NewStyle().Faint(true)
	titleStyle  = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("12")).MarginBottom(1)
)

func (m Model) tableView() string {
	var b strings.Builder

	filterStr := "all"
	if m.filter.StatusCode != nil {
		filterStr = strconv.Itoa(*m.filter.StatusCode)
	}
	title := fmt.Sprintf("memoryelaine logs — %d total — filter: %s", m.total, filterStr)
	b.WriteString(titleStyle.Render(title))
	b.WriteString("\n")

	header := fmt.Sprintf("%-6s %-12s %-6s %-30s %-6s %-10s %-10s", "ID", "TIME", "METHOD", "PATH", "STATUS", "DURATION", "RESP SIZE")
	b.WriteString(headerStyle.Render(header))
	b.WriteString("\n")

	if m.err != nil {
		fmt.Fprintf(&b, "Error: %v\n", m.err)
	}

	for i, e := range m.entries {
		status := "—"
		if e.StatusCode != nil {
			status = strconv.Itoa(*e.StatusCode)
		}
		dur := "—"
		if e.DurationMs != nil {
			dur = fmt.Sprintf("%dms", *e.DurationMs)
		}
		t := time.UnixMilli(e.TsStart).Format("15:04:05")
		trunc := ""
		if e.RespTruncated {
			trunc = "⚠"
		}

		line := fmt.Sprintf("%-6d %-12s %-6s %-30s %-6s %-10s %-9d%s",
			e.ID, t, e.RequestMethod, truncStr(e.RequestPath, 30),
			colorStatus(status), dur, e.RespBytes, trunc)

		if i == m.cursor {
			b.WriteString(cursorStyle.Render(line))
		} else {
			b.WriteString(line)
		}
		b.WriteString("\n")
	}

	page := m.filter.Offset/m.filter.Limit + 1
	pages := int(m.total)/m.filter.Limit + 1
	fmt.Fprintf(&b, "\nPage %d/%d", page, pages)
	b.WriteString("\n")
	b.WriteString(helpStyle.Render("j/k:navigate  enter:detail  r:refresh  n/p:page  f:filter  q:quit"))

	return b.String()
}

func (m Model) detailView() string {
	e := m.detail
	var b strings.Builder

	b.WriteString(titleStyle.Render(fmt.Sprintf("Log #%d", e.ID)))
	b.WriteString("\n")

	// Stream view status line
	svStatus := m.streamViewStatusLine()

	lines := []string{
		fmt.Sprintf("Time:     %s → %s", fmtMs(e.TsStart), fmtMsPtr(e.TsEnd)),
		fmt.Sprintf("Duration: %s", fmtDur(e.DurationMs)),
		fmt.Sprintf("Client:   %s", e.ClientIP),
		fmt.Sprintf("Method:   %s", e.RequestMethod),
		fmt.Sprintf("Path:     %s", e.RequestPath),
		fmt.Sprintf("Upstream: %s", e.UpstreamURL),
		fmt.Sprintf("Status:   %s", fmtStatusPtr(e.StatusCode)),
		fmt.Sprintf("Error:    %s", fmtStrPtr(e.Error)),
		"",
		"─── Request Headers ───",
		e.ReqHeadersJSON,
		"",
		fmt.Sprintf("─── Request Body (%d bytes%s) ───", e.ReqBytes, truncLabel(e.ReqTruncated)),
		m.detailReqBody,
		"",
		"─── Response Headers ───",
		fmtStrPtr(e.RespHeadersJSON),
		"",
		fmt.Sprintf("─── Response Body (%d bytes%s) ───", e.RespBytes, truncLabel(e.RespTruncated)),
		svStatus,
		m.detailRespBody,
	}
	if m.detailStatus != "" {
		lines = append(lines, "", "Status: "+m.detailStatus)
	}

	viewH := m.height - 3
	if viewH < 5 {
		viewH = 20
	}
	if m.scroll >= len(lines)-viewH {
		m.scroll = len(lines) - viewH
	}
	if m.scroll < 0 {
		m.scroll = 0
	}

	end := m.scroll + viewH
	if end > len(lines) {
		end = len(lines)
	}

	for _, l := range lines[m.scroll:end] {
		b.WriteString(l)
		b.WriteString("\n")
	}

	if m.savePromptActive {
		b.WriteString("\n")
		b.WriteString(helpStyle.Render("Save path: " + m.savePromptPath))
		b.WriteString("\n")
		b.WriteString(helpStyle.Render("enter:save  esc:cancel"))
		return b.String()
	}
	b.WriteString(helpStyle.Render("esc/q:back  j/k:scroll  v:stream view  z:toggle reasoning  x:b/B/c/R export  c:conversation"))
	return b.String()
}

// recomputeDetailBodies precomputes the ellipsized display strings for the
// current detail entry so that View() never performs expensive JSON transforms.
func (m *Model) recomputeDetailBodies() {
	e := m.detail
	if e == nil {
		return
	}
	m.detailReqBody = ellipsizeBody(e.ReqBody, 10000)

	respBodyContent := fmtStrPtr(e.RespBody)
	if m.streamView.mode == streamview.ModeAssembled && m.streamView.result.AssembledAvailable {
		respBodyContent = m.assembledDisplayBody()
	}
	m.detailRespBody = ellipsizeBody(respBodyContent, 10000)
}

func (m Model) assembledDisplayBody() string {
	var b strings.Builder
	sv := m.streamView.result
	if sv.HasReasoning {
		if m.reasoningFolded {
			b.WriteString("[Reasoning]\n  (folded, press z to expand)\n\n")
		} else {
			b.WriteString("[Reasoning]\n")
			b.WriteString(truncStr(sv.ReasoningBody, 10000))
			b.WriteString("\n\n")
		}
	}
	b.WriteString("[Content]\n")
	if sv.HasContent {
		b.WriteString(truncStr(sv.ContentBody, 10000))
	} else {
		b.WriteString("(content missing)")
	}
	return b.String()
}

func (m Model) streamViewStatusLine() string {
	sv := m.streamView
	if !sv.result.AssembledAvailable {
		reason := string(sv.result.Reason)
		if reason == "" {
			return ""
		}
		return fmt.Sprintf("Stream View: Raw (assembled unavailable: %s)", reason)
	}
	if sv.mode == streamview.ModeAssembled {
		if sv.result.Reason == streamview.ReasonPartialParse {
			return "Stream View: Assembled (partial parse)"
		}
		if !sv.result.HasContent {
			return "Stream View: Assembled (content missing)"
		}
		return "Stream View: Assembled"
	}
	return "Stream View: Raw [press v to toggle]"
}

func (m Model) loadLogs() tea.Msg {
	entries, err := m.reader.QuerySummaries(m.filter)
	if err != nil {
		return errMsg{err}
	}
	total, err := m.reader.Count(m.filter)
	if err != nil {
		return errMsg{err}
	}
	return logsLoadedMsg{entries, total}
}

func (m Model) loadDetail(id int64) tea.Cmd {
	return func() tea.Msg {
		entry, err := m.reader.GetByID(id)
		if err != nil {
			return errMsg{err}
		}
		sv := streamview.Build(entry)
		return logDetailMsg{entry: entry, streamView: sv}
	}
}

func colorStatus(s string) string {
	if strings.HasPrefix(s, "2") {
		return statusOK.Render(s)
	}
	if strings.HasPrefix(s, "4") {
		return statusWarn.Render(s)
	}
	if strings.HasPrefix(s, "5") {
		return statusErr.Render(s)
	}
	return s
}

func truncStr(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "…"
}

func ellipsizeBody(s string, maxLen int) string {
	if len(s) > 0 && (s[0] == '{' || s[0] == '[') {
		if transformed, changed, err := jsonellipsis.Transform(
			[]byte(s), jsonellipsis.DefaultLimit, jsonellipsis.DefaultKeys, jsonellipsis.DefaultMinDepth,
		); err == nil && changed {
			s = string(transformed)
		}
	}
	return truncStr(s, maxLen)
}

func truncLabel(t bool) string {
	if t {
		return ", TRUNCATED"
	}
	return ""
}

func fmtMs(ms int64) string { return time.UnixMilli(ms).Format("2006-01-02 15:04:05") }
func fmtMsPtr(ms *int64) string {
	if ms != nil {
		return fmtMs(*ms)
	}
	return "—"
}
func fmtDur(ms *int64) string {
	if ms != nil {
		return fmt.Sprintf("%dms", *ms)
	}
	return "—"
}
func fmtStrPtr(s *string) string {
	if s != nil {
		return *s
	}
	return "—"
}
func fmtStatusPtr(s *int) string {
	if s != nil {
		return strconv.Itoa(*s)
	}
	return "—"
}

func defaultDetailMode(sv streamview.Result) streamview.Mode {
	if sv.AssembledAvailable {
		return streamview.ModeAssembled
	}
	return streamview.ModeRaw
}

func (m Model) startExportPrompt(kind exportKind) Model {
	m.savePromptActive = true
	m.savePromptKind = kind
	content := m.exportContentForKind(kind)
	m.savePromptPath = defaultExportFilename(kind, content)
	m.detailStatus = ""
	return m
}

func (m Model) commitExport() error {
	content := m.exportContentForKind(m.savePromptKind)
	return os.WriteFile(m.savePromptPath, []byte(content), 0644)
}

func (m Model) exportContentForKind(kind exportKind) string {
	if m.detail == nil {
		return ""
	}
	switch kind {
	case exportReqRaw:
		return m.detail.ReqBody
	case exportRespRaw:
		return fmtStrPtr(m.detail.RespBody)
	case exportAssembledReasoning:
		return m.streamView.result.ReasoningBody
	case exportAssembledContent:
		return m.streamView.result.ContentBody
	default:
		return ""
	}
}

func defaultExportFilename(kind exportKind, content string) string {
	ext := "txt"
	if isJSONContent(content) {
		ext = "json"
	}
	switch kind {
	case exportReqRaw:
		return "request-body." + ext
	case exportRespRaw:
		return "response-body-parts." + ext
	case exportAssembledReasoning:
		return "response-reasoning-content." + ext
	case exportAssembledContent:
		return "response-body-assembled." + ext
	default:
		return "export." + ext
	}
}

func isJSONContent(s string) bool {
	trimmed := strings.TrimSpace(s)
	if trimmed == "" {
		return false
	}
	if !strings.HasPrefix(trimmed, "{") && !strings.HasPrefix(trimmed, "[") {
		return false
	}
	return json.Valid([]byte(trimmed))
}

func (m Model) loadThread(id int64) tea.Cmd {
	return func() tea.Msg {
		chain, err := m.reader.GetThreadToSelected(id)
		if err != nil || len(chain) == 0 {
			return errMsg{err}
		}
		selected := chain[len(chain)-1]
		msgs, err := chat.ParseMessages(selected.ReqBody)
		if err != nil {
			return errMsg{err}
		}

		threadMsgs := chat.BuildThreadMessages(msgs, toThreadEntries(chain), func(id int64) string {
			// Find the entry in the chain to avoid re-fetching from DB.
			var entry *database.LogEntry
			for i := range chain {
				if chain[i].ID == id {
					entry = &chain[i]
					break
				}
			}
			if entry == nil {
				return ""
			}
			sv := streamview.Build(entry)
			if sv.AssembledAvailable {
				return sv.AssembledBody
			}
			return ""
		})

		var result []threadMessage
		for _, tm := range threadMsgs {
			result = append(result, threadMessage{
				Role:    tm.Role,
				Content: tm.Content,
				LogID:   tm.LogID,
			})
		}

		return threadLoadedMsg{
			messages:           result,
			logID:              id,
			selectedEntryIndex: len(chain) - 1,
			totalEntries:       len(chain),
		}
	}
}

func toThreadEntries(chain []database.LogEntry) []chat.ThreadEntry {
	res := make([]chat.ThreadEntry, len(chain))
	for i := range chain {
		res[i] = chain[i]
	}
	return res
}

func (m Model) threadView() string {
	var b strings.Builder

	title := fmt.Sprintf("Conversation to Log #%d (turn %d of %d)", m.threadLogID, m.threadIndex+1, m.threadTotal)
	b.WriteString(titleStyle.Render(title))
	b.WriteString("\n\n")

	var lines []string
	for _, msg := range m.thread {
		roleLabel := msg.Role
		var style lipgloss.Style
		switch msg.Role {
		case "user":
			style = lipgloss.NewStyle().Foreground(lipgloss.Color("12"))
		case "assistant":
			style = lipgloss.NewStyle().Foreground(lipgloss.Color("10"))
		case "system", "developer":
			style = lipgloss.NewStyle().Faint(true)
		default:
			style = lipgloss.NewStyle()
		}
		header := style.Bold(true).Render(fmt.Sprintf("── %s (Log #%d) ──", roleLabel, msg.LogID))
		lines = append(lines, header)
		for _, l := range strings.Split(msg.Content, "\n") {
			lines = append(lines, style.Render(l))
		}
		lines = append(lines, "")
	}

	viewH := m.height - 5
	if viewH < 5 {
		viewH = 20
	}
	scroll := m.threadScroll
	if scroll >= len(lines)-viewH {
		scroll = len(lines) - viewH
	}
	if scroll < 0 {
		scroll = 0
	}
	end := scroll + viewH
	if end > len(lines) {
		end = len(lines)
	}
	for _, l := range lines[scroll:end] {
		b.WriteString(l)
		b.WriteString("\n")
	}

	b.WriteString(helpStyle.Render("esc/q:back  j/k:scroll"))
	return b.String()
}
