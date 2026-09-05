package panels

import (
	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"

	"github.com/horbo/stower/internal/tui/components"
	"github.com/horbo/stower/internal/tui/styles"
)

type Issues struct {
	width  int
	height int
	st     styles.Styles
}

func NewIssues(st styles.Styles) *Issues {
	return &Issues{st: st}
}

func (p *Issues) SetSize(w, h int) {
	p.width = w
	p.height = h
}

func (p *Issues) Update(tea.Msg) tea.Cmd {
	return nil
}

func (p *Issues) View() string {
	return p.st.Dim.Render(components.Truncate("not implemented yet", p.width))
}

func (p *Issues) Title() string {
	return "[4] Issues"
}

func (p *Issues) Counter() string {
	return ""
}

func (p *Issues) Keys() []key.Binding {
	return nil
}
