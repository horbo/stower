package panels

import (
	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"

	"github.com/horbo/stower/internal/tui/components"
	"github.com/horbo/stower/internal/tui/styles"
)

type Home struct {
	width  int
	height int
	st     styles.Styles
}

func NewHome(st styles.Styles) *Home {
	return &Home{st: st}
}

func (p *Home) SetSize(w, h int) {
	p.width = w
	p.height = h
}

func (p *Home) Update(tea.Msg) tea.Cmd {
	return nil
}

func (p *Home) View() string {
	return p.st.Dim.Render(components.Truncate("not implemented yet", p.width))
}

func (p *Home) Title() string {
	return "[2] Home"
}

func (p *Home) Counter() string {
	return ""
}

func (p *Home) Keys() []key.Binding {
	return nil
}
