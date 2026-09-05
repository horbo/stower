package panels

import (
	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"

	"github.com/horbo/stower/internal/tui/components"
	"github.com/horbo/stower/internal/tui/styles"
)

type Staged struct {
	width  int
	height int
	st     styles.Styles
}

func NewStaged(st styles.Styles) *Staged {
	return &Staged{st: st}
}

func (p *Staged) SetSize(w, h int) {
	p.width = w
	p.height = h
}

func (p *Staged) Update(tea.Msg) tea.Cmd {
	return nil
}

func (p *Staged) View() string {
	return p.st.Dim.Render(components.Truncate("not implemented yet", p.width))
}

func (p *Staged) Title() string {
	return "[3] Staged"
}

func (p *Staged) Counter() string {
	return ""
}

func (p *Staged) Keys() []key.Binding {
	return nil
}
