package panels

import (
	"fmt"
	"strings"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"github.com/horbo/stower/internal/doctor"
	"github.com/horbo/stower/internal/tui/components"
	"github.com/horbo/stower/internal/tui/styles"
)

type Issues struct {
	items  []doctor.Issue
	cursor int
	width  int
	height int
	st     styles.Styles
}

func NewIssues(st styles.Styles) *Issues { return &Issues{st: st} }
func (p *Issues) SetIssues(items []doctor.Issue) {
	selected, ok := p.Selected()
	p.items = items
	p.cursor = min(p.cursor, max(0, len(items)-1))
	if ok {
		for i, item := range items {
			if item.Package == selected.Package && item.Entry.PkgRel == selected.Entry.PkgRel {
				p.cursor = i
				break
			}
		}
	}
}
func (p *Issues) Selected() (doctor.Issue, bool) {
	if len(p.items) == 0 {
		return doctor.Issue{}, false
	}
	return p.items[p.cursor], true
}
func (p *Issues) SetSize(w, h int) { p.width, p.height = w, h }
func (p *Issues) Update(msg tea.Msg) tea.Cmd {
	if k, ok := msg.(tea.KeyPressMsg); ok {
		switch k.String() {
		case "j", "down":
			p.cursor = min(max(0, len(p.items)-1), p.cursor+1)
		case "k", "up":
			p.cursor = max(0, p.cursor-1)
		case "g", "home":
			p.cursor = 0
		case "G", "end":
			p.cursor = max(0, len(p.items)-1)
		}
	}
	return nil
}
func (p *Issues) View() string {
	if len(p.items) == 0 {
		return p.st.Dim.Render(components.Truncate("(no issues)", p.width))
	}
	start := max(0, p.cursor-max(1, p.height)+1)
	var lines []string
	for i := start; i < min(len(p.items), start+max(1, p.height)); i++ {
		item := p.items[i]
		line := components.Fit(fmt.Sprintf("%s %s %s %s", item.Glyph(), item.Package, item.Entry.PkgRel, item.State), p.width)
		if i == p.cursor {
			line = p.st.Selected.Render(line)
		} else {
			line = p.st.Warn.Render(line)
		}
		lines = append(lines, line)
	}
	return strings.Join(lines, "\n")
}
func (p *Issues) Title() string { return "[4] Issues" }
func (p *Issues) Counter() string {
	if len(p.items) == 0 {
		return ""
	}
	return fmt.Sprintf("%d of %d", p.cursor+1, len(p.items))
}
func (p *Issues) Keys() []key.Binding {
	return []key.Binding{
		key.NewBinding(key.WithKeys("j", "k"), key.WithHelp("j/k", "move")),
		key.NewBinding(key.WithKeys("f"), key.WithHelp("f", "fix")),
		key.NewBinding(key.WithKeys("D"), key.WithHelp("D", "diff")),
	}
}
