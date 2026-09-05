package mainpanel

import (
	"fmt"
	"strings"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"

	"github.com/horbo/stower/internal/config"
	"github.com/horbo/stower/internal/dotfiles"
	"github.com/horbo/stower/internal/tui/components"
	"github.com/horbo/stower/internal/tui/styles"
)

type StagedPlan struct {
	paths     config.Paths
	home      string
	plan      dotfiles.AdoptPlan
	highlight string
	width     int
	height    int
	st        styles.Styles
	vp        viewport.Model
}

func NewStagedPlan(paths config.Paths, home string, st styles.Styles) *StagedPlan {
	vp := viewport.New()
	vp.FillHeight = false
	return &StagedPlan{paths: paths, home: home, st: st, vp: vp}
}

func (s *StagedPlan) SetPlan(plan dotfiles.AdoptPlan, highlight string) {
	s.plan = plan
	s.highlight = highlight
	s.render()
}

func (s *StagedPlan) SetSize(width, height int) {
	if width == s.width && height == s.height {
		return
	}
	s.width = width
	s.height = height
	s.vp.SetWidth(max(0, width))
	s.vp.SetHeight(max(0, height))
	s.render()
}

func (s *StagedPlan) Update(msg tea.Msg) tea.Cmd {
	vp, cmd := s.vp.Update(msg)
	s.vp = vp
	return cmd
}

func (s *StagedPlan) View() string {
	if s.width <= 0 || s.height <= 0 {
		return ""
	}
	return s.vp.View()
}

func (s *StagedPlan) Title() string {
	return "Staged plan"
}

func (s *StagedPlan) Counter() string {
	moves := s.plan.MoveCount()
	if moves == 0 && s.plan.BlockedCount() == 0 {
		return ""
	}
	counter := fmt.Sprintf("%d moves", moves)
	if blocked := s.plan.BlockedCount(); blocked > 0 {
		counter += fmt.Sprintf(" · %d blocked", blocked)
	}
	return counter
}

func (s *StagedPlan) Keys() []key.Binding {
	return []key.Binding{
		key.NewBinding(key.WithKeys("j", "k"), key.WithHelp("j/k", "scroll")),
		key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "back")),
	}
}

func (s *StagedPlan) HasWarning(pkg string) bool {
	for _, adopt := range s.plan.Packages {
		if adopt.Package == pkg {
			return len(adopt.Warnings) > 0
		}
	}
	return false
}

func (s *StagedPlan) render() {
	s.vp.SetContent(strings.Join(s.lines(), "\n"))
}

func (s *StagedPlan) lines() []string {
	if s.width <= 0 {
		return nil
	}
	if len(s.plan.Packages) == 0 {
		return []string{s.st.Dim.Render("nothing staged; press space in Home to stage an entry")}
	}

	var lines []string
	if s.plan.Fatal != nil {
		lines = append(lines, s.st.Error.Render(components.Truncate("✘ "+s.plan.Fatal.Error(), s.width)))
		lines = append(lines, "")
	}
	for _, message := range s.plan.Messages {
		lines = append(lines, s.st.Warn.Render(components.Truncate("⚠ "+s.short(message), s.width)))
	}
	for i, adopt := range s.plan.Packages {
		if i > 0 {
			lines = append(lines, "")
		}
		lines = append(lines, s.packageLines(adopt)...)
	}
	return lines
}

func (s *StagedPlan) packageLines(adopt dotfiles.PackageAdopt) []string {
	header := adopt.Package
	if adopt.Package == s.highlight {
		header = "› " + header
	}
	lines := []string{s.st.Header.Render(components.Fit(header, s.width))}

	for _, move := range adopt.Moves {
		from := components.DisplayPath(move.From, s.home)
		lines = append(lines, components.Truncate("  "+from+" → "+s.short(move.To), s.width))
	}
	for _, link := range adopt.ExpectedLinks {
		lines = append(lines, s.st.Dim.Render(components.Truncate(
			"  link "+components.DisplayPath(link.TargetPath, s.home)+" → "+link.LinkText, s.width)))
	}
	if len(adopt.Moves) > 0 {
		lines = append(lines, s.st.Dim.Render(components.Truncate("  $ "+s.stowCommand(adopt.Package), s.width)))
	}
	for _, warning := range adopt.Warnings {
		lines = append(lines, s.st.Warn.Render(components.Truncate(
			"  ⚠ "+components.DisplayPath(warning.Path, s.home), s.width)))
		lines = append(lines, s.st.Warn.Render(components.Truncate("    "+warning.Message, s.width)))
		lines = append(lines, s.toggleLine(adopt))
	}
	for _, blocked := range adopt.Blocked {
		lines = append(lines, s.st.Error.Render(components.Truncate(
			"  ✘ "+components.DisplayPath(blocked.Path, s.home), s.width)))
		lines = append(lines, s.st.Error.Render(components.Truncate("    "+s.short(blocked.Reason), s.width)))
	}
	return lines
}

func (s *StagedPlan) toggleLine(adopt dotfiles.PackageAdopt) string {
	box := "[ ]"
	if adopt.RemoveNestedGit {
		box = "[x]"
	}
	line := components.Truncate("    "+box+" remove .git after move", s.width)
	if adopt.Package == s.highlight {
		return s.st.Accent.Render(line)
	}
	return s.st.Dim.Render(line)
}

func (s *StagedPlan) stowCommand(pkg string) string {
	return fmt.Sprintf("stow --dotfiles -v -R -d %s -t %s %s",
		components.DisplayPath(s.paths.Dotfiles, s.home),
		components.DisplayPath(s.paths.Target, s.home),
		pkg)
}

func (s *StagedPlan) short(text string) string {
	text = strings.ReplaceAll(text, s.paths.Dotfiles+"/", "")
	if s.home != "" {
		text = strings.ReplaceAll(text, s.home+"/", "~/")
	}
	return text
}
