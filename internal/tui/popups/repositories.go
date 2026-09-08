package popups

import (
	"strings"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/textinput"
	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"

	"github.com/horbo/stower/internal/dotfiles"
	"github.com/horbo/stower/internal/tui/components"
	"github.com/horbo/stower/internal/tui/styles"
)

type RepositoriesChosenMsg struct {
	Choices map[string]dotfiles.RepositoryChoice
}
type RepositoriesCancelledMsg struct{}

type repositoriesKeyMap struct {
	Up      key.Binding
	Down    key.Binding
	Cycle   key.Binding
	EditURL key.Binding
	Confirm key.Binding
	Cancel  key.Binding
}

func defaultRepositoriesKeyMap() repositoriesKeyMap {
	return repositoriesKeyMap{
		Up:      key.NewBinding(key.WithKeys("k", "up"), key.WithHelp("k/↑", "up")),
		Down:    key.NewBinding(key.WithKeys("j", "down"), key.WithHelp("j/↓", "down")),
		Cycle:   key.NewBinding(key.WithKeys(" ", "space"), key.WithHelp("space", "action")),
		EditURL: key.NewBinding(key.WithKeys("e"), key.WithHelp("e", "edit URL")),
		Confirm: key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "save")),
		Cancel:  key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "cancel")),
	}
}

const repositoryRowHeight = 2

type Repositories struct {
	rows          []dotfiles.NestedRepository
	cursor        int
	width, height int
	editing       bool
	problem       string
	input         textinput.Model
	keys          repositoriesKeyMap
	vp            viewport.Model
	st            styles.Styles
}

func NewRepositories(st styles.Styles) *Repositories {
	input := textinput.New()
	input.Prompt = "URL: "
	vp := viewport.New()
	vp.FillHeight = false
	return &Repositories{st: st, input: input, keys: defaultRepositoriesKeyMap(), vp: vp}
}

func (p *Repositories) Open(rows []dotfiles.NestedRepository) {
	p.rows = append([]dotfiles.NestedRepository(nil), rows...)
	p.cursor = 0
	p.editing = false
	p.problem = ""
	p.input.Blur()
	p.vp.SetYOffset(0)
	p.render()
}

func (p *Repositories) SetSize(w, h int) {
	p.width = w
	p.height = h
	p.input.SetWidth(max(1, components.InnerWidth(w)-5))
	p.vp.SetWidth(max(0, components.InnerWidth(w)))
	p.vp.SetHeight(max(0, components.InnerHeight(h)-p.detailHeight()))
	p.render()
}

func (p *Repositories) Title() string {
	return "Git repositories"
}

func (p *Repositories) Keys() []key.Binding {
	return []key.Binding{p.keys.Up, p.keys.Down, p.keys.Cycle, p.keys.EditURL, p.keys.Confirm, p.keys.Cancel}
}

func (p *Repositories) Update(msg tea.Msg) tea.Cmd {
	pressed, ok := msg.(tea.KeyPressMsg)
	if !ok {
		return nil
	}
	if p.editing {
		switch {
		case key.Matches(pressed, p.keys.Cancel):
			p.editing = false
			p.input.Blur()
			p.render()
			return nil
		case key.Matches(pressed, p.keys.Confirm):
			p.rows[p.cursor].Choice.URL = strings.TrimSpace(p.input.Value())
			p.editing = false
			p.problem = ""
			p.input.Blur()
			p.render()
			return nil
		}
		var cmd tea.Cmd
		p.input, cmd = p.input.Update(msg)
		p.render()
		return cmd
	}
	switch {
	case key.Matches(pressed, p.keys.Cancel):
		return func() tea.Msg { return RepositoriesCancelledMsg{} }
	case key.Matches(pressed, p.keys.Confirm):
		return p.confirm()
	case key.Matches(pressed, p.keys.Down):
		p.moveCursor(1)
	case key.Matches(pressed, p.keys.Up):
		p.moveCursor(-1)
	case key.Matches(pressed, p.keys.Cycle):
		p.cycle()
	case key.Matches(pressed, p.keys.EditURL):
		return p.editURL()
	}
	return nil
}

func (p *Repositories) Click(x, y int) tea.Cmd {
	if p.editing || len(p.rows) == 0 {
		return nil
	}
	row := (y + p.vp.YOffset()) / repositoryRowHeight
	if row < 0 || row >= len(p.rows) || y >= p.vp.Height() {
		return nil
	}
	if row != p.cursor {
		p.cursor = row
		p.problem = ""
		p.render()
		return nil
	}
	p.cycle()
	return nil
}

func (p *Repositories) Scroll(delta int) {
	p.vp.SetYOffset(p.vp.YOffset() + delta)
}

func (p *Repositories) View() string {
	inner := components.InnerWidth(p.width)
	frame := components.Frame{Title: p.Title(), Counter: "enter save · esc cancel", Focused: true, Styles: p.st}
	if inner <= 0 {
		return frame.Render(p.width, p.height, "")
	}
	body := append([]string{p.vp.View()}, p.detailLines(inner)...)
	return frame.Render(p.width, p.height, strings.Join(body, "\n"))
}

func (p *Repositories) confirm() tea.Cmd {
	for i, r := range p.rows {
		if r.Choice.Action != dotfiles.ConvertRepository {
			continue
		}
		if err := r.ConversionError(); err != nil {
			p.cursor = i
			p.problem = err.Error()
			p.render()
			return nil
		}
	}
	choices := map[string]dotfiles.RepositoryChoice{}
	for _, r := range p.rows {
		choices[r.Source] = r.Choice
	}
	return func() tea.Msg { return RepositoriesChosenMsg{Choices: choices} }
}

func (p *Repositories) moveCursor(delta int) {
	if len(p.rows) == 0 {
		return
	}
	p.cursor = min(len(p.rows)-1, max(0, p.cursor+delta))
	p.problem = ""
	p.render()
}

func (p *Repositories) cycle() {
	if len(p.rows) == 0 {
		return
	}
	r := &p.rows[p.cursor]
	allowed := r.AllowedActions()
	if len(allowed) < 2 {
		p.problem = p.unavailableReason(*r)
		p.render()
		return
	}
	next := 0
	for i, action := range allowed {
		if action == r.Choice.Action {
			next = (i + 1) % len(allowed)
		}
	}
	r.Choice.Action = allowed[next]
	p.problem = ""
	p.render()
}

func (p *Repositories) unavailableReason(r dotfiles.NestedRepository) string {
	if r.Info.Reason != "" {
		return "Only Keep is available: " + r.Info.Reason
	}
	return "Only Keep is available for this repository."
}

func (p *Repositories) editURL() tea.Cmd {
	if len(p.rows) == 0 || p.rows[p.cursor].Choice.Action != dotfiles.ConvertRepository {
		return nil
	}
	p.editing = true
	p.input.SetValue(p.rows[p.cursor].Choice.URL)
	p.render()
	return p.input.Focus()
}

func (p *Repositories) detailHeight() int {
	return 5
}

func (p *Repositories) detailLines(inner int) []string {
	lines := make([]string, 0, p.detailHeight())
	help := "j/k select · space action"
	if len(p.rows) > 0 {
		r := p.rows[p.cursor]
		switch r.Choice.Action {
		case dotfiles.KeepRepository:
			lines = append(lines, components.Truncate("Keep .git and repository history unchanged.", inner))
		case dotfiles.RemoveRepositoryGit:
			lines = append(lines, p.st.Warn.Render(components.Truncate("Delete .git after Apply; Restore cannot recover its history.", inner)))
		case dotfiles.ConvertRepository:
			help += " · e edit URL · enter save URL"
			if p.editing {
				lines = append(lines, p.input.View())
			} else {
				lines = append(lines, components.Truncate("URL: "+r.Choice.URL, inner))
			}
			if err := r.ConversionError(); err != nil {
				lines = append(lines, p.st.Error.Render(components.Truncate(err.Error(), inner)))
			} else if r.Info.Dirty {
				lines = append(lines, components.Truncate("Local changes stay here; dotfiles records HEAD only.", inner))
			}
		}
	}
	if p.problem != "" {
		lines = append(lines, p.st.Error.Render(components.Truncate(p.problem, inner)))
	}
	for len(lines) < p.detailHeight()-2 {
		lines = append(lines, "")
	}
	lines = append(lines[:p.detailHeight()-2], "", components.Truncate(help, inner))
	return lines
}

func (p *Repositories) render() {
	inner := components.InnerWidth(p.width)
	if inner <= 0 {
		p.vp.SetContent("")
		return
	}
	lines := make([]string, 0, len(p.rows)*repositoryRowHeight)
	for i, r := range p.rows {
		line := components.Fit(components.TruncateLeft(r.Source, inner), inner)
		if i == p.cursor {
			line = p.st.Selected.Render(line)
		}
		lines = append(lines, line, components.Truncate("  Action: "+r.Choice.Action.String(), inner))
	}
	p.vp.SetContent(strings.Join(lines, "\n"))
	p.showCursor()
}

func (p *Repositories) showCursor() {
	height := p.vp.Height()
	if height <= 0 {
		return
	}
	top := p.cursor * repositoryRowHeight
	bottom := top + repositoryRowHeight
	offset := p.vp.YOffset()
	if top < offset {
		p.vp.SetYOffset(top)
		return
	}
	if bottom > offset+height {
		p.vp.SetYOffset(bottom - height)
	}
}
