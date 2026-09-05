package panels

import (
	"fmt"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/spinner"
	tea "charm.land/bubbletea/v2"

	"github.com/horbo/stower/internal/config"
	"github.com/horbo/stower/internal/tui/components"
	"github.com/horbo/stower/internal/tui/styles"
)

type Status struct {
	paths       config.Paths
	home        string
	stowVersion string
	staged      int
	issues      int
	repo        bool
	dirty       int
	running     bool
	width       int
	height      int
	st          styles.Styles
	spin        spinner.Model
}

func NewStatus(paths config.Paths, home, stowVersion string, st styles.Styles) *Status {
	return &Status{
		paths:       paths,
		home:        home,
		stowVersion: stowVersion,
		st:          st,
		spin:        spinner.New(spinner.WithSpinner(spinner.MiniDot)),
	}
}

func (s *Status) SetSize(w, h int) {
	s.width = w
	s.height = h
}

func (s *Status) Update(msg tea.Msg) tea.Cmd {
	tick, ok := msg.(spinner.TickMsg)
	if !ok || !s.running {
		return nil
	}
	spin, cmd := s.spin.Update(tick)
	s.spin = spin
	return cmd
}

func (s *Status) SetRunning(running bool) tea.Cmd {
	if running == s.running {
		return nil
	}
	s.running = running
	if !running {
		return nil
	}
	return s.spin.Tick
}

func (s *Status) Running() bool {
	return s.running
}

func (s *Status) SetStaged(n int) {
	s.staged = n
}

func (s *Status) SetIssues(n int) { s.issues = n }

func (s *Status) SetGit(repo bool, dirty int) {
	s.repo = repo
	s.dirty = dirty
}

func (s *Status) GitSummary() string {
	switch {
	case !s.repo:
		return "no git"
	case s.dirty == 0:
		return "git clean"
	default:
		return fmt.Sprintf("git %d*", s.dirty)
	}
}

func (s *Status) View() string {
	dotfiles := components.DisplayPath(s.paths.Dotfiles, s.home)
	target := components.DisplayPath(s.paths.Target, s.home)
	version := s.stowVersion
	if version == "" {
		version = "unknown"
	}
	staged := ""
	issues := ""
	if s.issues > 0 {
		issues = "  " + plural(s.issues, "issue", "issues")
	}
	switch {
	case s.running:
		staged = "  " + s.spin.View() + " running…"
	case s.staged > 0:
		staged = fmt.Sprintf("  %d staged", s.staged)
	}
	git := s.GitSummary()
	dim := "  " + s.st.Dim.Render("stow "+version) + "  " + s.st.Dim.Render(git)
	arrow := " " + s.st.Accent.Render("→") + " "
	candidates := []struct{ plain, rendered string }{
		{"  stow " + version + "  " + git + staged + issues, dim + s.st.Accent.Render(staged+issues)},
		{"  " + git + staged + issues, "  " + s.st.Dim.Render(git) + s.st.Accent.Render(staged+issues)},
		{staged + issues, s.st.Accent.Render(staged + issues)},
		{staged, s.st.Accent.Render(staged)},
		{issues, s.st.Accent.Render(issues)},
		{"", ""},
	}
	for _, candidate := range candidates {
		room := s.width - components.Width(candidate.plain) - 3
		if room < 8 {
			continue
		}
		left, right := share(room, components.Width(dotfiles), components.Width(target))
		line := components.TruncateLeft(dotfiles, left) + arrow + components.TruncateLeft(target, right)
		return line + candidate.rendered
	}
	return components.Truncate(dotfiles+arrow+target, s.width)
}

func share(room, left, right int) (int, int) {
	if left+right <= room {
		return left, right
	}
	half := room / 2
	switch {
	case left <= half:
		return left, room - left
	case right <= room-half:
		return room - right, right
	default:
		return half, room - half
	}
}

func (s *Status) Title() string {
	return "[0] Status"
}

func (s *Status) Counter() string {
	return ""
}

func (s *Status) Keys() []key.Binding {
	return []key.Binding{
		key.NewBinding(key.WithKeys("c"), key.WithHelp("c", "commit")),
		key.NewBinding(key.WithKeys("R"), key.WithHelp("R", "restow all")),
	}
}
