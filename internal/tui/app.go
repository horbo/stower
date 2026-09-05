package tui

import (
	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

type Model struct {
	quit  key.Binding
	style lipgloss.Style
}

func New() Model {
	return Model{
		quit:  key.NewBinding(key.WithKeys("q"), key.WithHelp("q", "quit")),
		style: lipgloss.NewStyle().Bold(true),
	}
}

func (m Model) Init() tea.Cmd {
	return nil
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if pressed, ok := msg.(tea.KeyPressMsg); ok && key.Matches(pressed, m.quit) {
		return m, tea.Quit
	}
	return m, nil
}

func (m Model) View() tea.View {
	return tea.NewView(m.style.Render("stower"))
}
