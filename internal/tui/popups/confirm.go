package popups

import (
	"strings"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"

	"github.com/horbo/stower/internal/tui/components"
	"github.com/horbo/stower/internal/tui/styles"
)

type ConfirmedMsg struct {
	Action string
}

type ConfirmCancelledMsg struct {
	Action string
}

type confirmKeyMap struct {
	Confirm key.Binding
	Cancel  key.Binding
}

func defaultConfirmKeyMap() confirmKeyMap {
	return confirmKeyMap{
		Confirm: key.NewBinding(key.WithKeys("y", "enter")),
		Cancel:  key.NewBinding(key.WithKeys("n", "esc")),
	}
}

const (
	confirmHint = "y / enter confirm"
	cancelHint  = "n / esc cancel"
	hintGap     = "    "
)

type Confirm struct {
	action string
	title  string
	body   []string
	undo   string
	width  int
	height int
	st     styles.Styles
	keys   confirmKeyMap
	vp     viewport.Model
}

func NewConfirm(st styles.Styles) *Confirm {
	vp := viewport.New()
	vp.FillHeight = false
	return &Confirm{st: st, keys: defaultConfirmKeyMap(), vp: vp}
}

func (c *Confirm) Open(action, title string, body []string, undo string) {
	c.action = action
	c.title = title
	c.body = body
	c.undo = undo
	c.vp.SetYOffset(0)
	c.render()
}

func (c *Confirm) Action() string {
	return c.action
}

func (c *Confirm) SetSize(w, h int) {
	c.width = w
	c.height = h
	c.vp.SetWidth(max(0, components.InnerWidth(w)))
	c.vp.SetHeight(max(0, components.InnerHeight(h)-1))
	c.render()
}

func (c *Confirm) Update(msg tea.Msg) tea.Cmd {
	pressed, ok := msg.(tea.KeyPressMsg)
	if !ok {
		return nil
	}
	action := c.action
	switch {
	case key.Matches(pressed, c.keys.Confirm):
		return func() tea.Msg { return ConfirmedMsg{Action: action} }
	case key.Matches(pressed, c.keys.Cancel):
		return func() tea.Msg { return ConfirmCancelledMsg{Action: action} }
	}
	vp, cmd := c.vp.Update(pressed)
	c.vp = vp
	return cmd
}

func (c *Confirm) Click(x, y int) tea.Cmd {
	if y != c.promptRow() {
		return nil
	}
	action := c.action
	switch {
	case x >= 0 && x < components.Width(confirmHint):
		return func() tea.Msg { return ConfirmedMsg{Action: action} }
	case x >= components.Width(confirmHint+hintGap) && x < components.Width(confirmHint+hintGap+cancelHint):
		return func() tea.Msg { return ConfirmCancelledMsg{Action: action} }
	}
	return nil
}

func (c *Confirm) Scroll(delta int) {
	c.vp.SetYOffset(c.vp.YOffset() + delta)
}

func (c *Confirm) promptRow() int {
	return strings.Count(c.vp.View(), "\n") + 1
}

func (c *Confirm) Title() string {
	if c.title == "" {
		return "Confirm"
	}
	return c.title
}

func (c *Confirm) View() string {
	frame := components.Frame{Title: c.Title(), Counter: "y confirm · n cancel", Focused: true, Styles: c.st}
	inner := components.InnerWidth(c.width)
	prompt := c.st.Accent.Render(components.Truncate(confirmHint+hintGap+cancelHint, inner))
	return frame.Render(c.width, c.height, c.vp.View()+"\n"+prompt)
}

func (c *Confirm) render() {
	inner := components.InnerWidth(c.width)
	if inner <= 0 {
		c.vp.SetContent("")
		return
	}
	lines := make([]string, 0, len(c.body)+3)
	for _, line := range c.body {
		lines = append(lines, components.Truncate(line, inner))
	}
	if c.undo != "" {
		lines = append(lines, "")
		lines = append(lines, c.st.Dim.Render(components.Truncate("undo: "+c.undo, inner)))
	}
	c.vp.SetContent(strings.Join(lines, "\n"))
}
