package popups

import (
	"strings"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"

	"github.com/horbo/stower/internal/tui/components"
	"github.com/horbo/stower/internal/tui/styles"
)

type Section struct {
	Title    string
	Bindings []key.Binding
}

type Keys struct {
	sections []Section
	width    int
	height   int
	st       styles.Styles
	vp       viewport.Model
}

func NewKeys(st styles.Styles) *Keys {
	return &Keys{st: st, vp: viewport.New()}
}

func (k *Keys) SetSections(sections []Section) {
	k.sections = sections
	k.vp.SetYOffset(0)
	k.render()
}

func (k *Keys) SetSize(w, h int) {
	k.width = w
	k.height = h
	k.vp.SetWidth(max(0, components.InnerWidth(w)))
	k.vp.SetHeight(max(0, components.InnerHeight(h)))
	k.render()
}

func (k *Keys) Update(msg tea.Msg) tea.Cmd {
	vp, cmd := k.vp.Update(msg)
	k.vp = vp
	return cmd
}

func (k *Keys) Title() string {
	return "Keys"
}

func (k *Keys) View() string {
	frame := components.Frame{Title: k.Title(), Counter: "esc close", Focused: true, Styles: k.st}
	return frame.Render(k.width, k.height, k.vp.View())
}

func (k *Keys) render() {
	inner := components.InnerWidth(k.width)
	if inner <= 0 {
		k.vp.SetContent("")
		return
	}
	nameWidth := 0
	for _, section := range k.sections {
		for _, binding := range section.Bindings {
			if !binding.Enabled() || binding.Help().Key == "" {
				continue
			}
			nameWidth = max(nameWidth, components.Width(binding.Help().Key))
		}
	}
	var lines []string
	for i, section := range k.sections {
		if i > 0 {
			lines = append(lines, "")
		}
		lines = append(lines, k.st.Header.Render(components.Truncate(section.Title, inner)))
		for _, binding := range section.Bindings {
			help := binding.Help()
			if !binding.Enabled() || help.Key == "" {
				continue
			}
			name := components.Fit(help.Key, nameWidth)
			lines = append(lines, components.Truncate(k.st.KeyName.Render(name)+"  "+help.Desc, inner))
		}
	}
	if len(lines) == 0 {
		lines = append(lines, k.st.Dim.Render("no keys"))
	}
	k.vp.SetContent(strings.Join(lines, "\n"))
}
