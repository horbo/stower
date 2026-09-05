package mainpanel

import (
	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"fmt"
	"github.com/horbo/stower/internal/dotfiles"
	"github.com/horbo/stower/internal/tui/components"
	"github.com/horbo/stower/internal/tui/styles"
	"strings"
)

type RestorePlan struct {
	plan          dotfiles.RestorePlan
	width, height int
	vp            viewport.Model
	st            styles.Styles
}

func NewRestorePlan(st styles.Styles) *RestorePlan {
	vp := viewport.New()
	vp.FillHeight = false
	return &RestorePlan{st: st, vp: vp}
}
func (p *RestorePlan) SetPlan(plan dotfiles.RestorePlan) {
	p.plan = plan
	p.vp.SetYOffset(0)
	p.render()
}
func (p *RestorePlan) SetSize(w, h int) {
	p.width, p.height = w, h
	p.vp.SetWidth(max(0, w))
	p.vp.SetHeight(max(0, h))
	p.render()
}
func (p *RestorePlan) Update(msg tea.Msg) tea.Cmd { vp, cmd := p.vp.Update(msg); p.vp = vp; return cmd }
func (p *RestorePlan) Title() string              { return "Restore plan: " + p.plan.Package }
func (p *RestorePlan) Counter() string {
	return fmt.Sprintf("%s · %d blocked", plural(len(p.plan.Moves), "move", "moves"), len(p.plan.Blocked))
}
func (p *RestorePlan) View() string {
	if p.width <= 0 || p.height <= 0 {
		return ""
	}
	return p.vp.View()
}
func (p *RestorePlan) Keys() []key.Binding {
	keys := []key.Binding{key.NewBinding(key.WithKeys("j", "k"), key.WithHelp("j/k", "scroll")), key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "back"))}
	if p.plan.Runnable() {
		keys = append(keys, key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "restore")))
	}
	return keys
}
func (p *RestorePlan) render() {
	var lines []string
	if p.plan.Fatal != nil {
		lines = append(lines, "✘ "+p.plan.Fatal.Error())
	}
	for _, entry := range p.plan.Entries {
		lines = append(lines, fmt.Sprintf("%s/%s → %s  %s %s", p.plan.Package, entry.PkgRel, entry.TargetPath(p.plan.Paths), stateGlyph(entry.State), entry.State))
	}
	lines = append(lines, "", "Then: remove empty "+p.plan.RemoveDir)
	for _, blocked := range p.plan.Blocked {
		lines = append(lines, "✘ "+blocked.Path, "  "+blocked.Reason+"; fix in Issues first")
	}
	for i, line := range lines {
		lines[i] = components.Truncate(line, p.width)
	}
	p.vp.SetContent(strings.Join(lines, "\n"))
}
