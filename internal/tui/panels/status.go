package panels

import (
	"fmt"
	"strings"

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
	arrow := " " + s.st.Accent.Render("→") + " "
	for _, suffix := range []string{"  stow " + version + staged + issues, staged + issues, staged, issues, ""} {
		room := s.width - components.Width(suffix) - 3
		if room < 8 {
			continue
		}
		left, right := share(room, components.Width(dotfiles), components.Width(target))
		line := components.TruncateLeft(dotfiles, left) + arrow + components.TruncateLeft(target, right)
		if strings.HasPrefix(suffix, "  stow ") {
			return line + "  " + s.st.Dim.Render("stow "+version) + s.st.Accent.Render(staged+issues)
		}
		return line + s.st.Accent.Render(suffix)
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
	return []key.Binding{key.NewBinding(key.WithKeys("R"), key.WithHelp("R", "restow all"))}
}
