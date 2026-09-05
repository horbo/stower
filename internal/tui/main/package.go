package mainpanel

import (
	"fmt"
	"strings"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"

	"github.com/horbo/stower/internal/config"
	"github.com/horbo/stower/internal/doctor"
	"github.com/horbo/stower/internal/dotfiles"
	"github.com/horbo/stower/internal/tui/components"
	"github.com/horbo/stower/internal/tui/styles"
)

const (
	columnGap = 2
	minColumn = 8
)

type Package struct {
	paths   config.Paths
	home    string
	name    string
	entries []dotfiles.Entry
	reports []doctor.Issue
	cursor  int
	err     error
	loaded  bool
	width   int
	height  int
	st      styles.Styles
	vp      viewport.Model
}

func NewPackage(paths config.Paths, home string, st styles.Styles) *Package {
	vp := viewport.New()
	vp.FillHeight = false
	return &Package{paths: paths, home: home, st: st, vp: vp}
}

func (p *Package) SetPackage(name string, entries []dotfiles.Entry, err error) {
	if p.loaded && name == p.name && err == nil && p.err == nil && sameEntries(entries, p.entries) {
		return
	}
	p.name = name
	p.reports = nil
	p.cursor = 0
	p.entries = entries
	p.err = err
	p.loaded = true
	p.vp.SetYOffset(0)
	p.render()
}

func sameEntries(a, b []dotfiles.Entry) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func (p *Package) SetSize(w, h int) {
	if w == p.width && h == p.height {
		return
	}
	p.width = w
	p.height = h
	p.vp.SetWidth(max(0, w))
	p.vp.SetHeight(max(0, h))
	p.render()
}

func (p *Package) Update(msg tea.Msg) tea.Cmd {
	if k, ok := msg.(tea.KeyPressMsg); ok {
		switch k.String() {
		case "j", "down":
			p.cursor = min(max(0, len(p.entries)-1), p.cursor+1)
		case "k", "up":
			p.cursor = max(0, p.cursor-1)
		case "g", "home":
			p.cursor = 0
		case "G", "end":
			p.cursor = max(0, len(p.entries)-1)
		}
		p.render()
	}

	vp, cmd := p.vp.Update(msg)
	p.vp = vp
	return cmd
}

func (p *Package) View() string {
	if p.width <= 0 || p.height <= 0 {
		return ""
	}
	return p.vp.View()
}

func (p *Package) Title() string {
	if p.name == "" {
		return "Package"
	}
	return "Package: " + p.name
}

func (p *Package) Counter() string {
	if p.name == "" || p.err != nil {
		return ""
	}
	return fmt.Sprintf("%d", len(p.entries))
}

func (p *Package) Keys() []key.Binding {
	return []key.Binding{
		key.NewBinding(key.WithKeys("f"), key.WithHelp("f", "fix")),
		key.NewBinding(key.WithKeys("D"), key.WithHelp("D", "diff")),
		key.NewBinding(key.WithKeys("j", "k"), key.WithHelp("j/k", "select entry")),
		key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "back")),
	}
}

func (p *Package) render() {
	p.vp.SetContent(strings.Join(p.lines(), "\n"))
}

func (p *Package) lines() []string {
	if p.width <= 0 {
		return nil
	}
	if p.name == "" {
		return []string{p.st.Dim.Render("no package selected")}
	}
	if p.err != nil {
		return []string{p.st.Error.Render(components.Truncate("cannot read package: "+p.err.Error(), p.width))}
	}
	if len(p.entries) == 0 {
		return []string{p.st.Dim.Render("the package is empty")}
	}

	targets := make([]string, len(p.entries))
	states := make([]string, len(p.entries))
	for i, entry := range p.entries {
		targets[i] = components.DisplayPath(entry.TargetPath(p.paths), p.home)
		states[i] = stateGlyph(entry.State) + " " + entry.State.String()
		if i < len(p.reports) {
			states[i] = p.reports[i].Glyph() + " " + string(p.reports[i].State)
		}
	}

	stateWidth := components.Width("STATE")
	for _, s := range states {
		stateWidth = max(stateWidth, components.Width(s))
	}
	entryWidth := components.Width("ENTRY")
	targetWidth := components.Width("TARGET")
	for i, entry := range p.entries {
		entryWidth = max(entryWidth, components.Width(entryName(entry)))
		targetWidth = max(targetWidth, components.Width(targets[i]))
	}

	free := p.width - stateWidth - 2*columnGap
	if free < 2*minColumn {
		stateWidth = max(1, p.width/4)
		free = p.width - stateWidth - 2*columnGap
	}
	if free < 2 {
		free = 2
	}
	if entryWidth+targetWidth > free {
		entryWidth = max(minColumn, free*45/100)
		if entryWidth > free-minColumn {
			entryWidth = max(1, free-minColumn)
		}
	}
	targetWidth = max(1, free-entryWidth)

	lines := make([]string, 0, len(p.entries)+3)
	header := components.Fit("ENTRY", entryWidth) + strings.Repeat(" ", columnGap) +
		components.Fit("TARGET", targetWidth) + strings.Repeat(" ", columnGap) + "STATE"
	lines = append(lines, p.st.Header.Render(components.Fit(header, p.width)))
	for i, entry := range p.entries {
		line := row(entryWidth, targetWidth, stateWidth, entryName(entry), targets[i], states[i])
		if i == p.cursor {
			lines = append(lines, p.st.Selected.Render(components.Fit(line, p.width)))
		} else {
			lines = append(lines, p.styleRow(entry.State, components.Fit(line, p.width)))
		}
	}
	lines = append(lines, "")
	lines = append(lines, p.st.Dim.Render(components.Truncate(p.summary(), p.width)))
	return lines
}

func (p *Package) styleRow(state dotfiles.State, line string) string {
	switch state {
	case dotfiles.Linked:
		return line
	case dotfiles.Unlinked:
		return p.st.Warn.Render(line)
	default:
		return p.st.Error.Render(line)
	}
}

func (p *Package) summary() string {
	if len(p.reports) > 0 {
		counts := map[doctor.State]int{}
		for _, item := range p.reports {
			counts[item.State]++
		}
		parts := []string{plural(len(p.reports), "entry", "entries")}
		for _, state := range []doctor.State{doctor.OK, doctor.Missing, doctor.Replaced, doctor.Foreign, doctor.Unnormalized} {
			if counts[state] > 0 {
				parts = append(parts, fmt.Sprintf("%d %s", counts[state], state))
			}
		}
		return strings.Join(parts, "  ")
	}

	var linked, unlinked, conflict int
	for _, entry := range p.entries {
		switch entry.State {
		case dotfiles.Linked:
			linked++
		case dotfiles.Unlinked:
			unlinked++
		default:
			conflict++
		}
	}
	parts := []string{plural(len(p.entries), "entry", "entries"), fmt.Sprintf("%d linked", linked)}
	if unlinked > 0 {
		parts = append(parts, fmt.Sprintf("%d unlinked", unlinked))
	}
	if conflict > 0 {
		parts = append(parts, fmt.Sprintf("%d conflict", conflict))
	}
	return strings.Join(parts, "  ")
}

func plural(n int, singular, many string) string {
	if n == 1 {
		return fmt.Sprintf("%d %s", n, singular)
	}
	return fmt.Sprintf("%d %s", n, many)
}

func entryName(entry dotfiles.Entry) string {
	if entry.IsDir {
		return entry.PkgRel + "/"
	}
	return entry.PkgRel
}

func stateGlyph(state dotfiles.State) string {
	switch state {
	case dotfiles.Linked:
		return "✔"
	case dotfiles.Unlinked:
		return "✘"
	default:
		return "⚠"
	}
}

func row(entryWidth, targetWidth, stateWidth int, entry, target, state string) string {
	gap := strings.Repeat(" ", columnGap)
	return components.Fit(entry, entryWidth) + gap + components.FitLeft(target, targetWidth) + gap + components.Fit(state, stateWidth)
}

func (p *Package) SetReport(report doctor.PackageReport) {
	if report.Package != p.name {
		p.cursor = 0
	}
	entries := make([]dotfiles.Entry, 0, len(report.Entries))
	for _, item := range report.Entries {
		entries = append(entries, item.Entry)
	}
	p.SetPackage(report.Package, entries, report.Err)
	p.reports = report.Entries
	p.cursor = min(p.cursor, max(0, len(entries)-1))
	p.render()
}
func (p *Package) SelectedIssue() (doctor.Issue, bool) {
	if p.cursor < 0 || p.cursor >= len(p.reports) {
		return doctor.Issue{}, false
	}
	return p.reports[p.cursor], true
}
