package panels

import (
	"fmt"
	"strings"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"

	"github.com/horbo/stower/internal/tui/components"
	"github.com/horbo/stower/internal/tui/styles"
)

type Package struct {
	Name   string
	Linked bool
	Failed bool
}

type packagesKeyMap struct {
	Up     key.Binding
	Down   key.Binding
	Top    key.Binding
	Bottom key.Binding
}

func defaultPackagesKeyMap() packagesKeyMap {
	return packagesKeyMap{
		Up:     key.NewBinding(key.WithKeys("k", "up")),
		Down:   key.NewBinding(key.WithKeys("j", "down")),
		Top:    key.NewBinding(key.WithKeys("g", "home")),
		Bottom: key.NewBinding(key.WithKeys("G", "end")),
	}
}

type Packages struct {
	items  []Package
	cursor int
	offset int
	width  int
	height int
	st     styles.Styles
	keys   packagesKeyMap
}

func NewPackages(st styles.Styles) *Packages {
	return &Packages{st: st, keys: defaultPackagesKeyMap()}
}

func (p *Packages) SetPackages(items []Package) {
	p.items = items
	if p.cursor >= len(items) {
		p.cursor = max(0, len(items)-1)
	}
	p.clampOffset()
}

func (p *Packages) SetSize(w, h int) {
	p.width = w
	p.height = h
	p.clampOffset()
}

func (p *Packages) Selected() (Package, bool) {
	if p.cursor < 0 || p.cursor >= len(p.items) {
		return Package{}, false
	}
	return p.items[p.cursor], true
}

func (p *Packages) Cursor() int {
	return p.cursor
}

func (p *Packages) Update(msg tea.Msg) tea.Cmd {
	pressed, ok := msg.(tea.KeyPressMsg)
	if !ok {
		return nil
	}
	switch {
	case key.Matches(pressed, p.keys.Up):
		p.move(-1)
	case key.Matches(pressed, p.keys.Down):
		p.move(1)
	case key.Matches(pressed, p.keys.Top):
		p.cursor = 0
		p.clampOffset()
	case key.Matches(pressed, p.keys.Bottom):
		p.cursor = max(0, len(p.items)-1)
		p.clampOffset()
	}
	return nil
}

func (p *Packages) move(delta int) {
	if len(p.items) == 0 {
		return
	}
	p.cursor += delta
	if p.cursor < 0 {
		p.cursor = 0
	}
	if p.cursor >= len(p.items) {
		p.cursor = len(p.items) - 1
	}
	p.clampOffset()
}

func (p *Packages) clampOffset() {
	if p.height <= 0 {
		p.offset = 0
		return
	}
	if p.cursor < p.offset {
		p.offset = p.cursor
	}
	if p.cursor >= p.offset+p.height {
		p.offset = p.cursor - p.height + 1
	}
	if p.offset > max(0, len(p.items)-p.height) {
		p.offset = max(0, len(p.items)-p.height)
	}
	if p.offset < 0 {
		p.offset = 0
	}
}

func (p *Packages) View() string {
	if len(p.items) == 0 {
		return p.st.Dim.Render(components.Truncate("(no packages)", p.width))
	}
	end := min(len(p.items), p.offset+max(p.height, 1))
	lines := make([]string, 0, end-p.offset)
	for i := p.offset; i < end; i++ {
		lines = append(lines, p.row(i))
	}
	return strings.Join(lines, "\n")
}

func (p *Packages) row(i int) string {
	item := p.items[i]
	glyph, glyphStyle := "✔", p.st.OK
	switch {
	case item.Failed:
		glyph, glyphStyle = "?", p.st.Warn
	case !item.Linked:
		glyph, glyphStyle = "✘", p.st.Error
	}
	line := components.Fit(glyph+" "+item.Name, p.width)
	if i == p.cursor {
		return p.st.Selected.Render(line)
	}
	if !strings.HasPrefix(line, glyph) {
		return line
	}
	return glyphStyle.Render(glyph) + line[len(glyph):]
}

func (p *Packages) Title() string {
	return "[1] Packages"
}

func (p *Packages) Counter() string {
	if len(p.items) == 0 {
		return ""
	}
	return fmt.Sprintf("%d of %d", p.cursor+1, len(p.items))
}

func (p *Packages) Keys() []key.Binding {
	return []key.Binding{
		key.NewBinding(key.WithKeys("j", "k"), key.WithHelp("j/k", "move")),
		key.NewBinding(key.WithKeys("g", "G"), key.WithHelp("g/G", "top/bottom")),
	}
}
