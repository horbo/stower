package popups

import (
	"strings"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"

	"github.com/horbo/stower/internal/tui/components"
	"github.com/horbo/stower/internal/tui/styles"
)

type ErrorClosedMsg struct{}

type Error struct {
	title  string
	text   string
	width  int
	height int
	st     styles.Styles
	close  key.Binding
	vp     viewport.Model
}

func NewError(st styles.Styles) *Error {
	vp := viewport.New()
	vp.FillHeight = false
	return &Error{
		st:    st,
		close: key.NewBinding(key.WithKeys("enter", "esc", "q")),
		vp:    vp,
	}
}

func (e *Error) Open(title, text string) {
	e.title = title
	e.text = text
	e.vp.SetYOffset(0)
	e.render()
}

func (e *Error) SetSize(w, h int) {
	e.width = w
	e.height = h
	e.vp.SetWidth(max(0, components.InnerWidth(w)))
	e.vp.SetHeight(max(0, components.InnerHeight(h)))
	e.render()
}

func (e *Error) Update(msg tea.Msg) tea.Cmd {
	pressed, ok := msg.(tea.KeyPressMsg)
	if !ok {
		return nil
	}
	if key.Matches(pressed, e.close) {
		return func() tea.Msg { return ErrorClosedMsg{} }
	}
	vp, cmd := e.vp.Update(pressed)
	e.vp = vp
	return cmd
}

func (e *Error) Scroll(delta int) {
	e.vp.SetYOffset(e.vp.YOffset() + delta)
}

func (e *Error) Title() string {
	if e.title == "" {
		return "Error"
	}
	return e.title
}

func (e *Error) View() string {
	frame := components.Frame{Title: e.Title(), Counter: "esc close", Focused: true, Styles: e.st}
	return frame.Render(e.width, e.height, e.vp.View())
}

func (e *Error) render() {
	inner := components.InnerWidth(e.width)
	if inner <= 0 {
		e.vp.SetContent("")
		return
	}
	var lines []string
	for _, line := range strings.Split(e.text, "\n") {
		lines = append(lines, e.st.Error.Render(components.Truncate(line, inner)))
	}
	e.vp.SetContent(strings.Join(lines, "\n"))
}
