package tui

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"memoryelaine/internal/database"
)

type viewMode int

const (
	modeTable viewMode = iota
	modeDetail
)

type Model struct {
	mode    viewMode
	reader  *database.LogReader
	filter  database.QueryFilter
	entries []database.LogEntry
	total   int64
	cursor  int
	detail  *database.LogEntry
	scroll  int
	err     error
	width   int
	height  int
	quit    bool
}

type logsLoadedMsg struct {
	entries []database.LogEntry
	total   int64
}
type logDetailMsg struct{ entry *database.LogEntry }
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
		m.mode = modeDetail
		m.scroll = 0
		return m, nil

	case errMsg:
		m.err = msg.err
		return m, nil
	}
	return m, nil
}

func (m Model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.mode == modeDetail {
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
	if m.mode == modeDetail && m.detail != nil {
		return m.detailView()
	}
	return m.tableView()
}

var (
	headerStyle  = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("12"))
	cursorStyle  = lipgloss.NewStyle().Background(lipgloss.Color("236")).Bold(true)
	statusOK     = lipgloss.NewStyle().Foreground(lipgloss.Color("10"))
	statusWarn   = lipgloss.NewStyle().Foreground(lipgloss.Color("11"))
	statusErr    = lipgloss.NewStyle().Foreground(lipgloss.Color("9"))
	helpStyle    = lipgloss.NewStyle().Faint(true)
	titleStyle   = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("12")).MarginBottom(1)
)

func (m Model) tableView() string {
	var b strings.Builder

	filterStr := "all"
	if m.filter.StatusCode != nil {
		filterStr = strconv.Itoa(*m.filter.StatusCode) + "xx"
	}
	title := fmt.Sprintf("memoryelaine logs — %d total — filter: %s", m.total, filterStr)
	b.WriteString(titleStyle.Render(title))
	b.WriteString("\n")

	header := fmt.Sprintf("%-6s %-12s %-6s %-30s %-6s %-10s %-10s", "ID", "TIME", "METHOD", "PATH", "STATUS", "DURATION", "RESP SIZE")
	b.WriteString(headerStyle.Render(header))
	b.WriteString("\n")

	if m.err != nil {
		b.WriteString(fmt.Sprintf("Error: %v\n", m.err))
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
	b.WriteString(fmt.Sprintf("\nPage %d/%d", page, pages))
	b.WriteString("\n")
	b.WriteString(helpStyle.Render("j/k:navigate  enter:detail  r:refresh  n/p:page  f:filter  q:quit"))

	return b.String()
}

func (m Model) detailView() string {
	e := m.detail
	var b strings.Builder

	b.WriteString(titleStyle.Render(fmt.Sprintf("Log #%d", e.ID)))
	b.WriteString("\n")

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
		truncStr(e.ReqBody, 2000),
		"",
		"─── Response Headers ───",
		fmtStrPtr(e.RespHeadersJSON),
		"",
		fmt.Sprintf("─── Response Body (%d bytes%s) ───", e.RespBytes, truncLabel(e.RespTruncated)),
		truncStr(fmtStrPtr(e.RespBody), 2000),
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

	b.WriteString(helpStyle.Render("esc/q:back  j/k:scroll"))
	return b.String()
}

func (m Model) loadLogs() tea.Msg {
	entries, err := m.reader.Query(m.filter)
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
		return logDetailMsg{entry}
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

func truncLabel(t bool) string {
	if t {
		return ", TRUNCATED"
	}
	return ""
}

func fmtMs(ms int64) string      { return time.UnixMilli(ms).Format("2006-01-02 15:04:05") }
func fmtMsPtr(ms *int64) string  { if ms != nil { return fmtMs(*ms) }; return "—" }
func fmtDur(ms *int64) string    { if ms != nil { return fmt.Sprintf("%dms", *ms) }; return "—" }
func fmtStrPtr(s *string) string { if s != nil { return *s }; return "—" }
func fmtStatusPtr(s *int) string { if s != nil { return strconv.Itoa(*s) }; return "—" }
