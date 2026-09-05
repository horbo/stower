package components

import (
	"strings"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/horbo/stower/internal/tui/styles"
)

const (
	indentWidth   = 2
	expandedGlyph = "▾ "
	closedGlyph   = "▸ "
	leafGlyph     = "  "
	errorGlyph    = "⚠"
)

type Node struct {
	Path       string
	Name       string
	IsDir      bool
	Badge      string
	BadgeStyle lipgloss.Style
	Selectable bool
	Expandable bool
}

type Loader func(path string) ([]Node, error)

type Row struct {
	Node     Node
	Depth    int
	Expanded bool
}

type treeKeyMap struct {
	Up       key.Binding
	Down     key.Binding
	Top      key.Binding
	Bottom   key.Binding
	Expand   key.Binding
	Collapse key.Binding
	Filter   key.Binding
	Accept   key.Binding
	Cancel   key.Binding
}

func defaultTreeKeyMap() treeKeyMap {
	return treeKeyMap{
		Up:       key.NewBinding(key.WithKeys("k", "up")),
		Down:     key.NewBinding(key.WithKeys("j", "down")),
		Top:      key.NewBinding(key.WithKeys("g", "home")),
		Bottom:   key.NewBinding(key.WithKeys("G", "end")),
		Expand:   key.NewBinding(key.WithKeys("right", "l", "enter")),
		Collapse: key.NewBinding(key.WithKeys("left", "h")),
		Filter:   key.NewBinding(key.WithKeys("/")),
		Accept:   key.NewBinding(key.WithKeys("enter")),
		Cancel:   key.NewBinding(key.WithKeys("esc")),
	}
}

type Tree struct {
	root     Node
	loader   Loader
	children map[string][]Node
	expanded map[string]bool
	errs     map[string]error

	rows   []Row
	cursor int
	offset int

	width  int
	height int

	filter    string
	filtering bool
	input     textinput.Model

	st   styles.Styles
	keys treeKeyMap
}

func NewTree(st styles.Styles) *Tree {
	input := textinput.New()
	input.Prompt = "/"
	input.Placeholder = "filter"
	return &Tree{
		children: map[string][]Node{},
		expanded: map[string]bool{},
		errs:     map[string]error{},
		input:    input,
		st:       st,
		keys:     defaultTreeKeyMap(),
	}
}

func (t *Tree) SetLoader(loader Loader) {
	t.loader = loader
}

func (t *Tree) SetRoot(root Node) {
	if root.Path == t.root.Path {
		t.root = root
		t.rebuild()
		return
	}
	t.root = root
	t.children = map[string][]Node{}
	t.expanded = map[string]bool{}
	t.errs = map[string]error{}
	t.cursor = 0
	t.offset = 0
	if root.Expandable {
		t.expand(root.Path)
	}
	t.rebuild()
}

func (t *Tree) Reload() {
	t.children = map[string][]Node{}
	t.errs = map[string]error{}
	for path, open := range t.expanded {
		if open {
			t.load(path)
		}
	}
	t.rebuild()
}

func (t *Tree) Apply(fn func(node *Node)) {
	fn(&t.root)
	for path, nodes := range t.children {
		for i := range nodes {
			fn(&nodes[i])
		}
		t.children[path] = nodes
	}
	t.rebuild()
}

func (t *Tree) SetSize(w, h int) {
	t.width = w
	t.height = h
	t.clampOffset()
}

func (t *Tree) Len() int {
	return len(t.rows)
}

func (t *Tree) Cursor() int {
	return t.cursor
}

func (t *Tree) Filtering() bool {
	return t.filtering
}

func (t *Tree) Filter() string {
	return t.filter
}

func (t *Tree) Selected() (Row, bool) {
	if t.cursor < 0 || t.cursor >= len(t.rows) {
		return Row{}, false
	}
	return t.rows[t.cursor], true
}

func (t *Tree) LoadError(path string) error {
	return t.errs[path]
}

func (t *Tree) Update(msg tea.Msg) tea.Cmd {
	pressed, ok := msg.(tea.KeyPressMsg)
	if !ok {
		return nil
	}
	if t.filtering {
		return t.updateFilter(pressed)
	}
	switch {
	case key.Matches(pressed, t.keys.Filter):
		t.filtering = true
		t.input.SetValue(t.filter)
		t.input.CursorEnd()
		t.clampOffset()
		return t.input.Focus()
	case key.Matches(pressed, t.keys.Cancel):
		if t.filter != "" {
			t.filter = ""
			t.rebuild()
		}
	case key.Matches(pressed, t.keys.Up):
		t.move(-1)
	case key.Matches(pressed, t.keys.Down):
		t.move(1)
	case key.Matches(pressed, t.keys.Top):
		t.cursor = 0
		t.clampOffset()
	case key.Matches(pressed, t.keys.Bottom):
		t.cursor = max(0, len(t.rows)-1)
		t.clampOffset()
	case key.Matches(pressed, t.keys.Expand):
		t.expandSelected()
	case key.Matches(pressed, t.keys.Collapse):
		t.collapseSelected()
	}
	return nil
}

func (t *Tree) updateFilter(pressed tea.KeyPressMsg) tea.Cmd {
	switch {
	case key.Matches(pressed, t.keys.Accept):
		t.filtering = false
		t.input.Blur()
		t.clampOffset()
		return nil
	case key.Matches(pressed, t.keys.Cancel):
		t.filtering = false
		t.input.Blur()
		t.input.SetValue("")
		t.filter = ""
		t.rebuild()
		return nil
	}
	input, cmd := t.input.Update(pressed)
	t.input = input
	t.filter = input.Value()
	t.rebuild()
	return cmd
}

func (t *Tree) move(delta int) {
	if len(t.rows) == 0 {
		return
	}
	t.cursor = clampIndex(t.cursor+delta, len(t.rows))
	t.clampOffset()
}

func (t *Tree) expandSelected() {
	row, ok := t.Selected()
	if !ok || !row.Node.Expandable {
		return
	}
	if t.expanded[row.Node.Path] {
		if t.cursor+1 < len(t.rows) {
			t.move(1)
		}
		return
	}
	t.expand(row.Node.Path)
	t.rebuild()
}

func (t *Tree) collapseSelected() {
	row, ok := t.Selected()
	if !ok {
		return
	}
	if row.Node.Expandable && t.expanded[row.Node.Path] {
		delete(t.expanded, row.Node.Path)
		t.rebuild()
		return
	}
	for i := t.cursor - 1; i >= 0; i-- {
		if t.rows[i].Depth < row.Depth {
			t.cursor = i
			t.clampOffset()
			return
		}
	}
}

func (t *Tree) Click(y int) {
	if t.filtering && y >= t.visibleHeight() {
		return
	}
	row, ok := RowAt(t.offset, y, len(t.rows))
	if !ok {
		return
	}
	if row == t.cursor {
		t.toggleSelected()
		return
	}
	t.cursor = row
	t.clampOffset()
}

func (t *Tree) Scroll(delta int) {
	t.move(delta)
}

func (t *Tree) toggleSelected() {
	row, ok := t.Selected()
	if !ok || !row.Node.Expandable {
		return
	}
	if t.expanded[row.Node.Path] {
		delete(t.expanded, row.Node.Path)
		t.rebuild()
		return
	}
	t.expand(row.Node.Path)
	t.rebuild()
}

func (t *Tree) expand(path string) {
	t.load(path)
	if t.errs[path] != nil {
		return
	}
	t.expanded[path] = true
}

func (t *Tree) load(path string) {
	if _, ok := t.children[path]; ok {
		return
	}
	if t.loader == nil {
		t.children[path] = nil
		return
	}
	nodes, err := t.loader(path)
	if err != nil {
		t.errs[path] = err
		return
	}
	delete(t.errs, path)
	t.children[path] = nodes
}

func (t *Tree) rebuild() {
	selected := ""
	if row, ok := t.Selected(); ok {
		selected = row.Node.Path
	}
	t.rows = t.rows[:0]
	if t.root.Path != "" {
		t.appendNode(t.root, 0)
	}
	if t.filter != "" {
		t.rows = filterRows(t.rows, t.filter)
	}
	t.cursor = clampIndex(indexOfPath(t.rows, selected, t.cursor), len(t.rows))
	t.clampOffset()
}

func (t *Tree) appendNode(node Node, depth int) {
	open := node.Expandable && t.expanded[node.Path]
	t.rows = append(t.rows, Row{Node: node, Depth: depth, Expanded: open})
	if !open {
		return
	}
	for _, child := range t.children[node.Path] {
		t.appendNode(child, depth+1)
	}
}

func filterRows(rows []Row, filter string) []Row {
	needle := strings.ToLower(filter)
	kept := make([]Row, 0, len(rows))
	for _, row := range rows {
		if strings.Contains(strings.ToLower(row.Node.Name), needle) {
			kept = append(kept, row)
		}
	}
	return kept
}

func indexOfPath(rows []Row, path string, fallback int) int {
	if path == "" {
		return fallback
	}
	for i, row := range rows {
		if row.Node.Path == path {
			return i
		}
	}
	return fallback
}

func clampIndex(i, length int) int {
	if length == 0 {
		return 0
	}
	if i < 0 {
		return 0
	}
	if i >= length {
		return length - 1
	}
	return i
}

func (t *Tree) visibleHeight() int {
	height := t.height
	if t.filtering {
		height--
	}
	return max(0, height)
}

func (t *Tree) clampOffset() {
	height := t.visibleHeight()
	if height <= 0 {
		t.offset = 0
		return
	}
	if t.cursor < t.offset {
		t.offset = t.cursor
	}
	if t.cursor >= t.offset+height {
		t.offset = t.cursor - height + 1
	}
	if limit := max(0, len(t.rows)-height); t.offset > limit {
		t.offset = limit
	}
	if t.offset < 0 {
		t.offset = 0
	}
}

func (t *Tree) View() string {
	if t.width <= 0 {
		return ""
	}
	height := t.visibleHeight()
	var lines []string
	switch {
	case len(t.rows) == 0 && t.filter != "":
		lines = append(lines, t.st.Dim.Render(Truncate("no match for "+t.filter, t.width)))
	case len(t.rows) == 0:
		lines = append(lines, t.st.Dim.Render(Truncate("(empty)", t.width)))
	default:
		end := min(len(t.rows), t.offset+max(height, 1))
		for i := t.offset; i < end; i++ {
			lines = append(lines, t.line(i))
		}
	}
	if t.filtering {
		lines = append(lines, Fit(t.input.View(), t.width))
	}
	return strings.Join(lines, "\n")
}

func (t *Tree) line(i int) string {
	row := t.rows[i]
	glyph := leafGlyph
	if row.Node.Expandable {
		glyph = closedGlyph
		if row.Expanded {
			glyph = expandedGlyph
		}
	}
	name := row.Node.Name
	if row.Node.IsDir && !strings.HasSuffix(name, "/") {
		name += "/"
	}
	left := strings.Repeat(" ", row.Depth*indentWidth) + glyph + name

	badge := row.Node.Badge
	if t.errs[row.Node.Path] != nil {
		badge = errorGlyph
	}
	badgeWidth := 0
	if badge != "" {
		badgeWidth = Width(badge) + 1
	}
	body := Fit(left, max(0, t.width-badgeWidth))

	if i == t.cursor {
		line := body
		if badge != "" {
			line += " " + badge
		}
		return t.st.Selected.Render(Fit(line, t.width))
	}
	if !row.Node.Selectable {
		body = t.st.Dim.Render(body)
	}
	if badge == "" {
		return body
	}
	style := row.Node.BadgeStyle
	if t.errs[row.Node.Path] != nil {
		style = t.st.Warn
	}
	return body + " " + style.Render(badge)
}
