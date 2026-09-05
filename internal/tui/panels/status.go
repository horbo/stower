package panels

import (
	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"

	"github.com/horbo/stower/internal/config"
	"github.com/horbo/stower/internal/tui/components"
	"github.com/horbo/stower/internal/tui/styles"
)

type Status struct {
	paths       config.Paths
	home        string
	stowVersion string
	width       int
	height      int
	st          styles.Styles
}

func NewStatus(paths config.Paths, home, stowVersion string, st styles.Styles) *Status {
	return &Status{paths: paths, home: home, stowVersion: stowVersion, st: st}
}

func (s *Status) SetSize(w, h int) {
	s.width = w
	s.height = h
}

func (s *Status) Update(tea.Msg) tea.Cmd {
	return nil
}

func (s *Status) View() string {
	dotfiles := components.DisplayPath(s.paths.Dotfiles, s.home)
	target := components.DisplayPath(s.paths.Target, s.home)
	version := s.stowVersion
	if version == "" {
		version = "unknown"
	}
	arrow := " " + s.st.Accent.Render("→") + " "
	room := s.width - components.Width("  stow "+version) - 3
	if room < 8 {
		return components.Truncate(dotfiles+arrow+target, s.width)
	}
	left, right := share(room, components.Width(dotfiles), components.Width(target))
	return components.TruncateLeft(dotfiles, left) + arrow + components.TruncateLeft(target, right) +
		"  " + s.st.Dim.Render("stow "+version)
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
	return nil
}
