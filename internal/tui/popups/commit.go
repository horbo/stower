package popups

import (
	"strings"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/textinput"
	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"

	"github.com/horbo/stower/internal/tui/components"
	"github.com/horbo/stower/internal/tui/styles"
)

type CommitMsg struct {
	Subject string
}

type CommitSkippedMsg struct{}

type CommitCancelledMsg struct{}

type commitKeyMap struct {
	Commit key.Binding
	Skip   key.Binding
	Edit   key.Binding
	Cancel key.Binding
	Up     key.Binding
	Down   key.Binding
}

func defaultCommitKeyMap() commitKeyMap {
	return commitKeyMap{
		Commit: key.NewBinding(key.WithKeys("enter")),
		Skip:   key.NewBinding(key.WithKeys("s")),
		Edit:   key.NewBinding(key.WithKeys("e", "i")),
		Cancel: key.NewBinding(key.WithKeys("esc")),
		Up:     key.NewBinding(key.WithKeys("k", "up")),
		Down:   key.NewBinding(key.WithKeys("j", "down")),
	}
}

const messagePrompt = "message: "

type Commit struct {
	packages []string
	lines    []string
	failed   string
	editing  bool
	width    int
	height   int
	st       styles.Styles
	keys     commitKeyMap
	input    textinput.Model
	vp       viewport.Model
}

func NewCommit(st styles.Styles) *Commit {
	input := textinput.New()
	input.Prompt = ""
	input.Placeholder = "commit subject"
	vp := viewport.New()
	vp.FillHeight = false
	return &Commit{st: st, keys: defaultCommitKeyMap(), input: input, vp: vp}
}

func (c *Commit) Open(packages, lines []string, subject string) {
	c.packages = packages
	c.lines = lines
	c.failed = ""
	c.editing = false
	c.input.SetValue(subject)
	c.input.Blur()
	c.vp.SetYOffset(0)
	c.render()
}

func (c *Commit) Packages() []string {
	return c.packages
}

func (c *Commit) Subject() string {
	return strings.TrimSpace(c.input.Value())
}

func (c *Commit) SetSize(w, h int) {
	c.width = w
	c.height = h
	inner := components.InnerWidth(w)
	c.input.SetWidth(max(1, inner-components.Width(messagePrompt)))
	c.vp.SetWidth(max(0, inner))
	c.vp.SetHeight(max(0, components.InnerHeight(h)-4))
	c.render()
}

func (c *Commit) Update(msg tea.Msg) tea.Cmd {
	pressed, ok := msg.(tea.KeyPressMsg)
	if !ok {
		return nil
	}
	if c.editing {
		switch {
		case key.Matches(pressed, c.keys.Cancel):
			c.editing = false
			c.input.Blur()
			return nil
		case key.Matches(pressed, c.keys.Commit):
			return c.commit()
		}
		input, cmd := c.input.Update(pressed)
		c.input = input
		c.failed = ""
		return cmd
	}
	switch {
	case key.Matches(pressed, c.keys.Commit):
		return c.commit()
	case key.Matches(pressed, c.keys.Skip):
		return func() tea.Msg { return CommitSkippedMsg{} }
	case key.Matches(pressed, c.keys.Cancel):
		return func() tea.Msg { return CommitCancelledMsg{} }
	case key.Matches(pressed, c.keys.Edit):
		c.editing = true
		c.failed = ""
		cmd := c.input.Focus()
		c.input.CursorEnd()
		return cmd
	}
	vp, cmd := c.vp.Update(pressed)
	c.vp = vp
	return cmd
}

func (c *Commit) Click(x, y int) tea.Cmd {
	if c.editing || y != c.messageRow() {
		return nil
	}
	c.editing = true
	c.failed = ""
	cmd := c.input.Focus()
	c.input.CursorEnd()
	return cmd
}

func (c *Commit) Scroll(delta int) {
	c.vp.SetYOffset(c.vp.YOffset() + delta)
}

func (c *Commit) messageRow() int {
	return strings.Count(c.vp.View(), "\n") + 1
}

func (c *Commit) commit() tea.Cmd {
	subject := c.Subject()
	if subject == "" {
		c.failed = "the commit subject is empty"
		return nil
	}
	c.editing = false
	c.input.Blur()
	return func() tea.Msg { return CommitMsg{Subject: subject} }
}

func (c *Commit) Title() string {
	return "Commit"
}

func (c *Commit) View() string {
	frame := components.Frame{Title: c.Title(), Counter: "enter commit · s skip · esc cancel", Focused: true, Styles: c.st}
	inner := components.InnerWidth(c.width)
	prompt := c.st.Dim.Render(messagePrompt)
	if c.editing {
		prompt = c.st.Accent.Render(messagePrompt)
	}
	body := []string{c.vp.View(), components.Fit(prompt+c.input.View(), inner)}
	if c.failed != "" {
		body = append(body, c.st.Error.Render(components.Truncate("✘ "+c.failed, inner)))
	} else {
		body = append(body, c.st.Dim.Render(components.Truncate("enter commit · e edit · s skip · esc cancel", inner)))
	}
	return frame.Render(c.width, c.height, strings.Join(body, "\n"))
}

func (c *Commit) render() {
	inner := components.InnerWidth(c.width)
	if inner <= 0 {
		c.vp.SetContent("")
		return
	}
	lines := make([]string, 0, len(c.lines)+2)
	if len(c.packages) > 0 {
		lines = append(lines, c.st.Dim.Render(components.Truncate(strings.Join(c.packages, ", "), inner)))
		lines = append(lines, "")
	}
	for _, line := range c.lines {
		lines = append(lines, components.Truncate(line, inner))
	}
	if len(c.lines) == 0 {
		lines = append(lines, c.st.Dim.Render(components.Truncate("(no changes)", inner)))
	}
	c.vp.SetContent(strings.Join(lines, "\n"))
}
