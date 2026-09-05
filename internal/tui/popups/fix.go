package popups

import (
	tea "charm.land/bubbletea/v2"
	"github.com/horbo/stower/internal/doctor"
	"github.com/horbo/stower/internal/tui/components"
	"github.com/horbo/stower/internal/tui/styles"
	"strings"
)

type FixChosenMsg struct{ Action doctor.Action }
type FixCancelledMsg struct{}
type Fix struct {
	issue                 doctor.Issue
	cursor, width, height int
	st                    styles.Styles
}

const fixOptionRow = 2

func NewFix(st styles.Styles) *Fix     { return &Fix{st: st} }
func (p *Fix) Open(issue doctor.Issue) { p.issue = issue; p.cursor = 2 }
func (p *Fix) SetSize(w, h int)        { p.width, p.height = w, h }
func (p *Fix) Update(msg tea.Msg) tea.Cmd {
	k, ok := msg.(tea.KeyPressMsg)
	if !ok {
		return nil
	}
	switch k.String() {
	case "esc":
		return func() tea.Msg { return FixCancelledMsg{} }
	case "j", "down":
		p.cursor = min(2, p.cursor+1)
	case "k", "up":
		p.cursor = max(0, p.cursor-1)
	case "enter":
		if p.cursor == 2 {
			return func() tea.Msg { return FixCancelledMsg{} }
		}
		action := doctor.KeepTarget
		if p.cursor == 1 {
			action = doctor.KeepRepo
		}
		return func() tea.Msg { return FixChosenMsg{Action: action} }
	}
	return nil
}
func (p *Fix) Click(x, y int) tea.Cmd {
	row := y - fixOptionRow
	if row < 0 || row > 2 {
		return nil
	}
	if row != p.cursor {
		p.cursor = row
		return nil
	}
	if row == 2 {
		return func() tea.Msg { return FixCancelledMsg{} }
	}
	action := doctor.KeepTarget
	if row == 1 {
		action = doctor.KeepRepo
	}
	return func() tea.Msg { return FixChosenMsg{Action: action} }
}
func (p *Fix) View() string {
	width := components.InnerWidth(p.width)
	lines := []string{components.Truncate(p.issue.Package+"/"+p.issue.Entry.PkgRel, width), ""}
	for i, text := range []string{"Keep TARGET: replace repo copy with target, then relink", "Keep REPO: discard target copy, then relink", "Cancel: keep both copies unchanged"} {
		line := components.Fit(text, width)
		if i == p.cursor {
			line = p.st.Selected.Render(line)
		}
		lines = append(lines, line)
	}
	lines = append(lines, "", components.Truncate("↑/↓ choose · enter confirm · esc cancel", width))
	frame := components.Frame{Title: "Fix replaced entry", Focused: true, Styles: p.st}
	return frame.Render(p.width, p.height, strings.Join(lines, "\n"))
}
