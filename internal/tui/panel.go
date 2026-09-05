package tui

import (
	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
)

type PanelID int

const (
	Status PanelID = iota
	Packages
	Home
	Staged
	Issues
	Main
	KeyBar
)

const SidePanelCount = 5

var sidePanels = [SidePanelCount]PanelID{Status, Packages, Home, Staged, Issues}

var hitOrder = [SidePanelCount + 2]PanelID{Status, Packages, Home, Staged, Issues, Main, KeyBar}

func (p PanelID) IsSide() bool {
	return p >= Status && p <= Issues
}

func (p PanelID) String() string {
	switch p {
	case Status:
		return "Status"
	case Packages:
		return "Packages"
	case Home:
		return "Home"
	case Staged:
		return "Staged"
	case Issues:
		return "Issues"
	case Main:
		return "Main"
	case KeyBar:
		return "KeyBar"
	default:
		return "unknown"
	}
}

type Panel interface {
	SetSize(w, h int)
	Update(msg tea.Msg) tea.Cmd
	View() string
	Title() string
	Counter() string
	Keys() []key.Binding
}
