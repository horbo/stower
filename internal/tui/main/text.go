package mainpanel

import (
	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"github.com/horbo/stower/internal/tui/components"
	"strings"
)

type Text struct {
	title, content string
	width, height  int
	vp             viewport.Model
}

func NewText() *Text { vp := viewport.New(); vp.FillHeight = false; return &Text{vp: vp} }
func (p *Text) SetText(title, content string) {
	if p.title == title && p.content == content {
		return
	}
	p.title, p.content = title, content
	p.vp.SetYOffset(0)
	p.render()
}
func (p *Text) SetSize(w, h int) {
	p.width, p.height = w, h
	p.vp.SetWidth(max(0, w))
	p.vp.SetHeight(max(0, h))
	p.render()
}
func (p *Text) render() {
	lines := strings.Split(p.content, "\n")
	for i, line := range lines {
		lines[i] = components.Truncate(line, p.width)
	}
	p.vp.SetContent(strings.Join(lines, "\n"))
}
func (p *Text) Update(msg tea.Msg) tea.Cmd { vp, cmd := p.vp.Update(msg); p.vp = vp; return cmd }
func (p *Text) View() string {
	if p.width <= 0 || p.height <= 0 {
		return ""
	}
	return p.vp.View()
}
func (p *Text) Title() string   { return p.title }
func (p *Text) Counter() string { return "" }
func (p *Text) Keys() []key.Binding {
	return []key.Binding{key.NewBinding(key.WithKeys("j", "k"), key.WithHelp("j/k", "scroll")), key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "back"))}
}
