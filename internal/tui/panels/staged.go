package panels

import (
	"fmt"
	"sort"
	"strings"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"

	"github.com/horbo/stower/internal/config"
	"github.com/horbo/stower/internal/dotfiles"
	"github.com/horbo/stower/internal/tui/components"
	"github.com/horbo/stower/internal/tui/styles"
)

type UnstageGroupMsg struct {
	Package string
}

type RenameGroupMsg struct {
	Package string
}

type StagedRow struct {
	Package string
	Path    string
	Group   bool
}

type stagedKeyMap struct {
	Up      key.Binding
	Down    key.Binding
	Top     key.Binding
	Bottom  key.Binding
	Unstage key.Binding
	Rename  key.Binding
}

func defaultStagedKeyMap() stagedKeyMap {
	return stagedKeyMap{
		Up:      key.NewBinding(key.WithKeys("k", "up")),
		Down:    key.NewBinding(key.WithKeys("j", "down")),
		Top:     key.NewBinding(key.WithKeys("g", "home")),
		Bottom:  key.NewBinding(key.WithKeys("G", "end")),
		Unstage: key.NewBinding(key.WithKeys("u"), key.WithHelp("u", "unstage")),
		Rename:  key.NewBinding(key.WithKeys("e"), key.WithHelp("e", "rename package")),
	}
}

type Staged struct {
	paths    config.Paths
	home     string
	rows     []StagedRow
	items    int
	packages int
	cursor   int
	offset   int
	width    int
	height   int
	st       styles.Styles
	keys     stagedKeyMap
}

func NewStaged(paths config.Paths, home string, st styles.Styles) *Staged {
	return &Staged{paths: paths, home: home, st: st, keys: defaultStagedKeyMap()}
}

func (p *Staged) SetStaging(staging dotfiles.Staging) {
	selected := ""
	if row, ok := p.Selected(); ok {
		selected = row.Path + "\x00" + row.Package
	}
	p.rows, p.items, p.packages = stagedRows(staging)
	p.cursor = 0
	for i, row := range p.rows {
		if row.Path+"\x00"+row.Package == selected {
			p.cursor = i
			break
		}
	}
	p.clampOffset()
}

func stagedRows(staging dotfiles.Staging) ([]StagedRow, int, int) {
	byPackage := map[string][]string{}
	for path, pkg := range staging {
		byPackage[pkg] = append(byPackage[pkg], path)
	}
	names := make([]string, 0, len(byPackage))
	for pkg := range byPackage {
		names = append(names, pkg)
	}
	sort.Strings(names)

	rows := make([]StagedRow, 0, len(staging)+len(names))
	for _, pkg := range names {
		paths := byPackage[pkg]
		sort.Strings(paths)
		rows = append(rows, StagedRow{Package: pkg, Group: true})
		for _, path := range paths {
			rows = append(rows, StagedRow{Package: pkg, Path: path})
		}
	}
	return rows, len(staging), len(names)
}

func (p *Staged) Selected() (StagedRow, bool) {
	if p.cursor < 0 || p.cursor >= len(p.rows) {
		return StagedRow{}, false
	}
	return p.rows[p.cursor], true
}

func (p *Staged) SelectedPackage() string {
	row, ok := p.Selected()
	if !ok {
		return ""
	}
	return row.Package
}

func (p *Staged) SetSize(w, h int) {
	p.width = w
	p.height = h
	p.clampOffset()
}

func (p *Staged) Update(msg tea.Msg) tea.Cmd {
	pressed, ok := msg.(tea.KeyPressMsg)
	if !ok {
		return nil
	}
	row, hasRow := p.Selected()
	switch {
	case key.Matches(pressed, p.keys.Up):
		p.move(-1)
	case key.Matches(pressed, p.keys.Down):
		p.move(1)
	case key.Matches(pressed, p.keys.Top):
		p.cursor = 0
		p.clampOffset()
	case key.Matches(pressed, p.keys.Bottom):
		p.cursor = max(0, len(p.rows)-1)
		p.clampOffset()
	case key.Matches(pressed, p.keys.Unstage):
		if !hasRow {
			return nil
		}
		if row.Group {
			return func() tea.Msg { return UnstageGroupMsg{Package: row.Package} }
		}
		return func() tea.Msg { return UnstageRequestMsg{Path: row.Path} }
	case key.Matches(pressed, p.keys.Rename):
		if !hasRow {
			return nil
		}
		return func() tea.Msg { return RenameGroupMsg{Package: row.Package} }
	}
	return nil
}

func (p *Staged) move(delta int) {
	if len(p.rows) == 0 {
		return
	}
	p.cursor += delta
	if p.cursor < 0 {
		p.cursor = 0
	}
	if p.cursor >= len(p.rows) {
		p.cursor = len(p.rows) - 1
	}
	p.clampOffset()
}

func (p *Staged) clampOffset() {
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
	if limit := max(0, len(p.rows)-p.height); p.offset > limit {
		p.offset = limit
	}
	if p.offset < 0 {
		p.offset = 0
	}
}

func (p *Staged) View() string {
	if len(p.rows) == 0 {
		return p.st.Dim.Render(components.Truncate("(nothing staged)", p.width))
	}
	end := min(len(p.rows), p.offset+max(p.height, 1))
	lines := make([]string, 0, end-p.offset)
	for i := p.offset; i < end; i++ {
		lines = append(lines, p.row(i))
	}
	return strings.Join(lines, "\n")
}

func (p *Staged) row(i int) string {
	row := p.rows[i]
	text := "  " + components.DisplayPath(row.Path, p.home)
	if row.Group {
		text = row.Package + "/"
	}
	line := components.Fit(text, p.width)
	if i == p.cursor {
		return p.st.Selected.Render(line)
	}
	if row.Group {
		return p.st.Header.Render(line)
	}
	return line
}

func (p *Staged) Title() string {
	return "[3] Staged"
}

func (p *Staged) Counter() string {
	if p.items == 0 {
		return ""
	}
	return fmt.Sprintf("%s · %s", plural(p.items, "item", "items"), plural(p.packages, "package", "packages"))
}

func plural(n int, one, many string) string {
	if n == 1 {
		return fmt.Sprintf("%d %s", n, one)
	}
	return fmt.Sprintf("%d %s", n, many)
}

func (p *Staged) Keys() []key.Binding {
	if len(p.rows) == 0 {
		return nil
	}
	return []key.Binding{
		p.keys.Unstage,
		p.keys.Rename,
		key.NewBinding(key.WithKeys("x"), key.WithHelp("x", "toggle .git removal")),
		key.NewBinding(key.WithKeys("j", "k"), key.WithHelp("j/k", "move")),
	}
}
