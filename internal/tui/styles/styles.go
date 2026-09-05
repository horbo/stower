package styles

import "charm.land/lipgloss/v2"

type Styles struct {
	Border       lipgloss.Style
	BorderActive lipgloss.Style
	Title        lipgloss.Style
	TitleActive  lipgloss.Style
	Counter      lipgloss.Style
	Accent       lipgloss.Style
	Dim          lipgloss.Style
	OK           lipgloss.Style
	Warn         lipgloss.Style
	Error        lipgloss.Style
	Selected     lipgloss.Style
	Header       lipgloss.Style
	KeyName      lipgloss.Style
	TabActive    lipgloss.Style
	TabInactive  lipgloss.Style
}

func Default() Styles {
	accent := lipgloss.Color("6")
	dim := lipgloss.Color("8")
	return Styles{
		Border:       lipgloss.NewStyle().Foreground(dim),
		BorderActive: lipgloss.NewStyle().Foreground(accent),
		Title:        lipgloss.NewStyle(),
		TitleActive:  lipgloss.NewStyle().Foreground(accent).Bold(true),
		Counter:      lipgloss.NewStyle().Foreground(dim),
		Accent:       lipgloss.NewStyle().Foreground(accent),
		Dim:          lipgloss.NewStyle().Foreground(dim),
		OK:           lipgloss.NewStyle().Foreground(lipgloss.Color("2")),
		Warn:         lipgloss.NewStyle().Foreground(lipgloss.Color("3")),
		Error:        lipgloss.NewStyle().Foreground(lipgloss.Color("1")),
		Selected:     lipgloss.NewStyle().Reverse(true),
		Header:       lipgloss.NewStyle().Foreground(dim).Bold(true),
		KeyName:      lipgloss.NewStyle().Foreground(accent),
		TabActive:    lipgloss.NewStyle().Foreground(accent).Bold(true),
		TabInactive:  lipgloss.NewStyle().Foreground(dim),
	}
}
