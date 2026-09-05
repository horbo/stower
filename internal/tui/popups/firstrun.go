package popups

import (
	"strings"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"

	"github.com/horbo/stower/internal/tui/components"
	"github.com/horbo/stower/internal/tui/styles"
)

type FirstRunAppliedMsg struct {
	Create    bool
	GitInit   bool
	GitIgnore bool
}

type FirstRunCancelledMsg struct {
	Offer bool
}

type firstRunKeyMap struct {
	Apply  key.Binding
	Cancel key.Binding
	Toggle key.Binding
	Up     key.Binding
	Down   key.Binding
}

func defaultFirstRunKeyMap() firstRunKeyMap {
	return firstRunKeyMap{
		Apply:  key.NewBinding(key.WithKeys("enter")),
		Cancel: key.NewBinding(key.WithKeys("esc")),
		Toggle: key.NewBinding(key.WithKeys("space")),
		Up:     key.NewBinding(key.WithKeys("k", "up")),
		Down:   key.NewBinding(key.WithKeys("j", "down")),
	}
}

const firstRunOptionRow = 3

type FirstRun struct {
	dir       string
	offer     bool
	gitFound  bool
	gitInit   bool
	gitIgnore bool
	cursor    int
	width     int
	height    int
	st        styles.Styles
	keys      firstRunKeyMap
}

func NewFirstRun(st styles.Styles) *FirstRun {
	return &FirstRun{st: st, keys: defaultFirstRunKeyMap()}
}

func (f *FirstRun) OpenMissing(dir string, gitFound bool) {
	f.dir = dir
	f.offer = false
	f.gitFound = gitFound
	f.gitInit = gitFound
	f.gitIgnore = true
	f.cursor = 0
}

func (f *FirstRun) OpenOffer(dir string) {
	f.dir = dir
	f.offer = true
	f.gitFound = true
	f.gitInit = true
	f.gitIgnore = false
	f.cursor = 0
}

func (f *FirstRun) Offer() bool {
	return f.offer
}

func (f *FirstRun) SetSize(w, h int) {
	f.width = w
	f.height = h
}

func (f *FirstRun) Update(msg tea.Msg) tea.Cmd {
	pressed, ok := msg.(tea.KeyPressMsg)
	if !ok {
		return nil
	}
	offer := f.offer
	switch {
	case key.Matches(pressed, f.keys.Cancel), offer && pressed.String() == "n":
		return func() tea.Msg { return FirstRunCancelledMsg{Offer: offer} }
	case key.Matches(pressed, f.keys.Apply), offer && pressed.String() == "y":
		applied := FirstRunAppliedMsg{Create: !offer, GitInit: f.gitInit, GitIgnore: f.gitIgnore}
		return func() tea.Msg { return applied }
	case key.Matches(pressed, f.keys.Up):
		f.move(-1)
	case key.Matches(pressed, f.keys.Down):
		f.move(1)
	case key.Matches(pressed, f.keys.Toggle):
		f.toggle()
	}
	return nil
}

func (f *FirstRun) Click(x, y int) tea.Cmd {
	if f.offer {
		return nil
	}
	row := y - firstRunOptionRow
	if row < 0 || row > 2 {
		return nil
	}
	if row != f.cursor {
		f.cursor = row
		return nil
	}
	f.toggle()
	return nil
}

func (f *FirstRun) move(delta int) {
	if f.offer {
		return
	}
	f.cursor = min(2, max(0, f.cursor+delta))
}

func (f *FirstRun) toggle() {
	if f.offer {
		return
	}
	switch f.cursor {
	case 1:
		if f.gitFound {
			f.gitInit = !f.gitInit
		}
	case 2:
		f.gitIgnore = !f.gitIgnore
	}
}

func (f *FirstRun) Title() string {
	if f.offer {
		return "Git"
	}
	return "First run"
}

func (f *FirstRun) View() string {
	counter := "enter apply · esc quit"
	if f.offer {
		counter = "enter yes · esc no"
	}
	frame := components.Frame{Title: f.Title(), Counter: counter, Focused: true, Styles: f.st}
	return frame.Render(f.width, f.height, strings.Join(f.lines(), "\n"))
}

func (f *FirstRun) lines() []string {
	inner := components.InnerWidth(f.width)
	if inner <= 0 {
		return nil
	}
	if f.offer {
		return []string{
			components.Truncate("The dotfiles directory is not a git repository:", inner),
			f.st.Dim.Render(components.TruncateLeft(f.dir, inner)),
			"",
			components.Truncate("Run git init here?", inner),
			"",
			f.st.Dim.Render(components.Truncate("enter yes · esc no, not this session", inner)),
		}
	}
	lines := []string{
		components.Truncate("The dotfiles directory does not exist:", inner),
		f.st.Dim.Render(components.TruncateLeft(f.dir, inner)),
		"",
	}
	rows := []struct {
		checked  bool
		disabled bool
		label    string
	}{
		{true, true, "create " + f.dir},
		{f.gitInit, !f.gitFound, "git init"},
		{f.gitIgnore, false, "add .gitignore (.DS_Store)"},
	}
	for i, row := range rows {
		box := "[ ] "
		if row.checked {
			box = "[x] "
		}
		label := row.label
		switch {
		case i == 0:
			label += "  (required)"
		case row.disabled:
			label += "  (git not found)"
		}
		line := components.Fit(box+label, inner)
		if i == f.cursor {
			line = f.st.Selected.Render(line)
		} else if row.disabled {
			line = f.st.Dim.Render(line)
		}
		lines = append(lines, line)
	}
	lines = append(lines, "", f.st.Dim.Render(components.Truncate("space toggle · enter apply · esc quit", inner)))
	return lines
}
