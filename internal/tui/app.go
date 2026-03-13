package tui

import (
	"fmt"

	tea "github.com/charmbracelet/bubbletea"

	"memoryelaine/internal/database"
)

// Run starts the TUI application. Blocks until the user quits.
func Run(reader *database.LogReader) error {
	m := initialModel(reader)
	p := tea.NewProgram(m, tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		return fmt.Errorf("TUI error: %w", err)
	}
	return nil
}
