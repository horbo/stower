package popups

import (
	"strings"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"

	"github.com/horbo/stower/internal/dotfiles"
	"github.com/horbo/stower/internal/tui/components"
	"github.com/horbo/stower/internal/tui/styles"
)

type AssignedMsg struct {
	Path    string
	Package string
}

type AssignCancelledMsg struct{}

type assignKeyMap struct {
	Up      key.Binding
	Down    key.Binding
	Confirm key.Binding
	Cancel  key.Binding
}

func defaultAssignKeyMap() assignKeyMap {
	return assignKeyMap{
		Up:      key.NewBinding(key.WithKeys("up")),
		Down:    key.NewBinding(key.WithKeys("down")),
		Confirm: key.NewBinding(key.WithKeys("enter")),
		Cancel:  key.NewBinding(key.WithKeys("esc")),
	}
}

type Assign struct {
	title    string
	subtitle string
	path     string
	packages []string
	cursor   int
	failed   string
	width    int
	height   int
	st       styles.Styles
	keys     assignKeyMap
	input    textinput.Model
}

func NewAssign(st styles.Styles) *Assign {
	input := textinput.New()
	input.Prompt = ""
	input.Placeholder = "name"
	return &Assign{st: st, keys: defaultAssignKeyMap(), input: input}
}

func (a *Assign) Open(title, subtitle, path string, packages []string, current string) tea.Cmd {
	a.reset(title, subtitle, path, packages)
	a.cursor = len(packages)
	for i, pkg := range packages {
		if pkg == current {
			a.cursor = i
		}
	}
	if a.cursor == len(packages) {
		return a.focusInput()
	}
	a.input.Blur()
	return nil
}

func (a *Assign) OpenRename(title, subtitle string, packages []string, current string) tea.Cmd {
	a.reset(title, subtitle, "", packages)
	a.input.SetValue(current)
	a.cursor = len(packages)
	return a.focusInput()
}

func (a *Assign) reset(title, subtitle, path string, packages []string) {
	a.title = title
	a.subtitle = subtitle
	a.path = path
	a.packages = packages
	a.failed = ""
	a.input.SetValue("")
}

func (a *Assign) Path() string {
	return a.path
}

func (a *Assign) SetSize(w, h int) {
	a.width = w
	a.height = h
	a.input.SetWidth(max(1, components.InnerWidth(w)-components.Width(newPackagePrompt)))
}

func (a *Assign) Update(msg tea.Msg) tea.Cmd {
	pressed, ok := msg.(tea.KeyPressMsg)
	if !ok {
		return nil
	}
	switch {
	case key.Matches(pressed, a.keys.Cancel):
		a.input.Blur()
		return func() tea.Msg { return AssignCancelledMsg{} }
	case key.Matches(pressed, a.keys.Confirm):
		return a.confirm()
	case key.Matches(pressed, a.keys.Up):
		return a.move(-1)
	case key.Matches(pressed, a.keys.Down):
		return a.move(1)
	}
	if !a.onInput() {
		return nil
	}
	input, cmd := a.input.Update(pressed)
	a.input = input
	a.failed = ""
	return cmd
}

func (a *Assign) Click(x, y int) tea.Cmd {
	first := a.firstRow()
	input := first + max(len(a.packages), 1) + 1
	switch {
	case y == input:
		if a.onInput() {
			return nil
		}
		return a.move(len(a.packages) - a.cursor)
	case y >= first && y < first+len(a.packages):
		row := y - first
		if row == a.cursor {
			return a.confirm()
		}
		return a.move(row - a.cursor)
	}
	return nil
}

func (a *Assign) firstRow() int {
	if a.subtitle != "" {
		return 2
	}
	return 0
}

func (a *Assign) confirm() tea.Cmd {
	name := a.selection()
	if err := dotfiles.ValidatePackageName(name); err != nil {
		a.failed = err.Error()
		return nil
	}
	path, pkg := a.path, name
	a.input.Blur()
	return func() tea.Msg { return AssignedMsg{Path: path, Package: pkg} }
}

func (a *Assign) selection() string {
	if a.onInput() {
		return strings.TrimSpace(a.input.Value())
	}
	return a.packages[a.cursor]
}

func (a *Assign) onInput() bool {
	return a.cursor >= len(a.packages)
}

func (a *Assign) move(delta int) tea.Cmd {
	next := a.cursor + delta
	if next < 0 {
		next = 0
	}
	if next > len(a.packages) {
		next = len(a.packages)
	}
	a.cursor = next
	a.failed = ""
	if a.onInput() {
		return a.focusInput()
	}
	a.input.Blur()
	return nil
}

func (a *Assign) focusInput() tea.Cmd {
	cmd := a.input.Focus()
	a.input.CursorEnd()
	return cmd
}

func (a *Assign) Title() string {
	if a.title == "" {
		return "Assign"
	}
	return a.title
}

func (a *Assign) View() string {
	frame := components.Frame{Title: a.Title(), Counter: "esc cancel", Focused: true, Styles: a.st}
	return frame.Render(a.width, a.height, strings.Join(a.lines(), "\n"))
}

const newPackagePrompt = "new package: "

func (a *Assign) lines() []string {
	inner := components.InnerWidth(a.width)
	if inner <= 0 {
		return nil
	}
	lines := make([]string, 0, len(a.packages)+5)
	if a.subtitle != "" {
		lines = append(lines, a.st.Dim.Render(components.Truncate(a.subtitle, inner)))
		lines = append(lines, "")
	}
	for i, pkg := range a.packages {
		line := components.Fit("  "+pkg, inner)
		if i == a.cursor {
			line = a.st.Selected.Render(line)
		}
		lines = append(lines, line)
	}
	if len(a.packages) == 0 {
		lines = append(lines, a.st.Dim.Render(components.Fit("  (no packages yet)", inner)))
	}
	lines = append(lines, "")

	prompt := newPackagePrompt
	if a.onInput() {
		prompt = a.st.Accent.Render(prompt)
	} else {
		prompt = a.st.Dim.Render(prompt)
	}
	lines = append(lines, components.Fit(prompt+a.input.View(), inner))
	if a.failed != "" {
		lines = append(lines, a.st.Error.Render(components.Truncate("✘ "+a.failed, inner)))
	}
	return lines
}
