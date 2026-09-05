package tui

import (
	"os"
	"strings"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/horbo/stower/internal/config"
	"github.com/horbo/stower/internal/dotfiles"
	"github.com/horbo/stower/internal/tui/components"
	mainpanel "github.com/horbo/stower/internal/tui/main"
	"github.com/horbo/stower/internal/tui/panels"
	"github.com/horbo/stower/internal/tui/popups"
	"github.com/horbo/stower/internal/tui/styles"
)

const (
	popupMaxWidth = 70
	popupMargin   = 4
)

type packageInfo struct {
	name    string
	entries []dotfiles.Entry
	err     error
}

type refreshedMsg struct {
	pkgs []packageInfo
	err  error
}

type keyMap struct {
	Quit      key.Binding
	ForceQuit key.Binding
	Help      key.Binding
	NextPanel key.Binding
	ModeNext  key.Binding
	ModePrev  key.Binding
	Refresh   key.Binding
	Back      key.Binding
	Enter     key.Binding
	Panels    key.Binding
	byPanel   [SidePanelCount]key.Binding
}

func defaultKeyMap() keyMap {
	m := keyMap{
		Quit:      key.NewBinding(key.WithKeys("q"), key.WithHelp("q", "quit")),
		ForceQuit: key.NewBinding(key.WithKeys("ctrl+c"), key.WithHelp("ctrl+c", "quit")),
		Help:      key.NewBinding(key.WithKeys("?"), key.WithHelp("?", "keys")),
		NextPanel: key.NewBinding(key.WithKeys("tab"), key.WithHelp("tab", "next panel")),
		ModeNext:  key.NewBinding(key.WithKeys("+"), key.WithHelp("+", "wider")),
		ModePrev:  key.NewBinding(key.WithKeys("_"), key.WithHelp("_", "narrower")),
		Refresh:   key.NewBinding(key.WithKeys("R"), key.WithHelp("R", "refresh")),
		Back:      key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "back")),
		Enter:     key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "focus main")),
		Panels:    key.NewBinding(key.WithKeys("0", "1", "2", "3", "4"), key.WithHelp("0-4", "panels")),
	}
	for _, p := range sidePanels {
		m.byPanel[p] = key.NewBinding(key.WithKeys(string(rune('0' + int(p)))))
	}
	return m
}

type Model struct {
	paths       config.Paths
	home        string
	stowVersion string
	st          styles.Styles
	keys        keyMap

	width  int
	height int
	layout Layout

	focus       PanelID
	mainFocused bool
	mode        ScreenMode

	side      [SidePanelCount]Panel
	status    *panels.Status
	packages  *panels.Packages
	homePanel *panels.Home
	staged    *panels.Staged
	issues    *panels.Issues
	main      *mainpanel.Package

	keysPopup *popups.Keys
	popupOpen bool

	pkgs    []packageInfo
	loadErr error
}

func New(paths config.Paths, stowVersion string) Model {
	st := styles.Default()
	home := os.Getenv("HOME")
	m := Model{
		paths:       paths,
		home:        home,
		stowVersion: stowVersion,
		st:          st,
		keys:        defaultKeyMap(),
		focus:       Packages,
		mode:        ModeNormal,
		status:      panels.NewStatus(paths, home, stowVersion, st),
		packages:    panels.NewPackages(st),
		homePanel:   panels.NewHome(st),
		staged:      panels.NewStaged(st),
		issues:      panels.NewIssues(st),
		main:        mainpanel.NewPackage(paths, home, st),
		keysPopup:   popups.NewKeys(st),
	}
	m.side = [SidePanelCount]Panel{m.status, m.packages, m.homePanel, m.staged, m.issues}
	return m
}

func (m Model) Init() tea.Cmd {
	return refreshCmd(m.paths)
}

func refreshCmd(paths config.Paths) tea.Cmd {
	return func() tea.Msg {
		names, err := dotfiles.ListPackages(paths)
		if err != nil {
			return refreshedMsg{err: err}
		}
		infos := make([]packageInfo, 0, len(names))
		for _, name := range names {
			entries, walkErr := dotfiles.WalkPackage(paths, name)
			infos = append(infos, packageInfo{name: name, entries: entries, err: walkErr})
		}
		return refreshedMsg{pkgs: infos}
	}
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.relayout()
		return m, nil
	case refreshedMsg:
		m.pkgs = msg.pkgs
		m.loadErr = msg.err
		m.packages.SetPackages(packageItems(msg.pkgs))
		m.syncMain()
		return m, nil
	case tea.KeyPressMsg:
		return m.handleKey(msg)
	}
	return m, m.focusedPanel().Update(msg)
}

func packageItems(pkgs []packageInfo) []panels.Package {
	items := make([]panels.Package, 0, len(pkgs))
	for _, info := range pkgs {
		linked := true
		for _, entry := range info.entries {
			if entry.State != dotfiles.Linked {
				linked = false
				break
			}
		}
		items = append(items, panels.Package{Name: info.name, Linked: linked, Failed: info.err != nil})
	}
	return items
}

func (m Model) handleKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	if key.Matches(msg, m.keys.ForceQuit) {
		return m, tea.Quit
	}
	if m.popupOpen {
		switch {
		case key.Matches(msg, m.keys.Back, m.keys.Help, m.keys.Quit):
			m.popupOpen = false
			return m, nil
		}
		return m, m.keysPopup.Update(msg)
	}

	switch {
	case key.Matches(msg, m.keys.Quit):
		return m, tea.Quit
	case key.Matches(msg, m.keys.Help):
		m.popupOpen = true
		m.keysPopup.SetSections(m.keySections())
		m.relayout()
		return m, nil
	case key.Matches(msg, m.keys.ModeNext):
		m.mode = m.mode.Next()
		m.relayout()
		return m, nil
	case key.Matches(msg, m.keys.ModePrev):
		m.mode = m.mode.Prev()
		m.relayout()
		return m, nil
	case key.Matches(msg, m.keys.Refresh):
		return m, refreshCmd(m.paths)
	case key.Matches(msg, m.keys.NextPanel):
		m.focus = nextSide(m.focus)
		m.mainFocused = false
		m.relayout()
		m.syncMain()
		return m, nil
	case key.Matches(msg, m.keys.Back):
		if m.mainFocused {
			m.mainFocused = false
			m.relayout()
		}
		return m, nil
	case key.Matches(msg, m.keys.Enter):
		if !m.mainFocused && m.focus == Packages {
			if _, ok := m.packages.Selected(); ok {
				m.mainFocused = true
				m.relayout()
			}
		}
		return m, nil
	}

	for _, p := range sidePanels {
		if key.Matches(msg, m.keys.byPanel[p]) {
			m.focus = p
			m.mainFocused = false
			m.relayout()
			m.syncMain()
			return m, nil
		}
	}

	cmd := m.focusedPanel().Update(msg)
	m.syncMain()
	return m, cmd
}

func nextSide(focus PanelID) PanelID {
	if !focus.IsSide() || focus == Issues {
		return Status
	}
	return focus + 1
}

func (m Model) focusedPanel() Panel {
	if m.mainFocused {
		return m.main
	}
	if m.focus.IsSide() {
		return m.side[m.focus]
	}
	return m.main
}

func (m *Model) layoutFocus() PanelID {
	if m.mainFocused {
		return Main
	}
	return m.focus
}

func (m *Model) relayout() {
	m.layout = Compute(m.width, m.height, m.layoutFocus(), m.mode)
	if m.layout.TooSmall {
		return
	}
	for _, p := range sidePanels {
		rect := m.layout.Side[p]
		if rect.Empty() || m.layout.Collapsed[p] {
			m.side[p].SetSize(components.InnerWidth(rect.Width), 0)
			continue
		}
		m.side[p].SetSize(components.InnerWidth(rect.Width), components.InnerHeight(rect.Height))
	}
	if m.layout.Main.Empty() {
		m.main.SetSize(0, 0)
	} else {
		m.main.SetSize(components.InnerWidth(m.layout.Main.Width), components.InnerHeight(m.layout.Main.Height))
	}
	if m.popupOpen {
		rect := m.popupRect()
		m.keysPopup.SetSize(rect.Width, rect.Height)
	}
}

func (m *Model) syncMain() {
	selected, ok := m.packages.Selected()
	if !ok {
		m.main.SetPackage("", nil, nil)
		return
	}
	for _, info := range m.pkgs {
		if info.name == selected.Name {
			m.main.SetPackage(info.name, info.entries, info.err)
			return
		}
	}
	m.main.SetPackage(selected.Name, nil, nil)
}

func (m Model) popupRect() Rect {
	w := min(m.width-popupMargin, popupMaxWidth)
	h := m.height - popupMargin
	if w < 1 || h < 1 {
		return Rect{}
	}
	return Rect{X: (m.width - w) / 2, Y: (m.height - h) / 2, Width: w, Height: h}
}

func (m Model) View() tea.View {
	view := tea.NewView(m.render())
	view.AltScreen = true
	return view
}

func (m Model) render() string {
	if m.width <= 0 || m.height <= 0 {
		return ""
	}
	if m.layout.TooSmall {
		return m.st.Dim.Render("terminal too small")
	}

	layers := []*lipgloss.Layer{lipgloss.NewLayer(blank(m.width, m.height))}
	for _, p := range sidePanels {
		rect := m.layout.Side[p]
		if rect.Empty() {
			continue
		}
		layers = append(layers, layerAt(rect, m.renderSide(p, rect)))
	}
	if !m.layout.Main.Empty() {
		layers = append(layers, layerAt(m.layout.Main, m.renderMain(m.layout.Main)))
	}
	if !m.layout.KeyBar.Empty() {
		layers = append(layers, layerAt(m.layout.KeyBar, m.renderKeyBar(m.layout.KeyBar.Width)))
	}
	if m.popupOpen {
		if rect := m.popupRect(); !rect.Empty() {
			layers = append(layers, layerAt(rect, m.keysPopup.View()).Z(1))
		}
	}
	return lipgloss.NewCompositor(layers...).Render()
}

func layerAt(rect Rect, content string) *lipgloss.Layer {
	return lipgloss.NewLayer(content).X(rect.X).Y(rect.Y)
}

func blank(w, h int) string {
	line := strings.Repeat(" ", w)
	lines := make([]string, h)
	for i := range lines {
		lines[i] = line
	}
	return strings.Join(lines, "\n")
}

func (m Model) renderSide(p PanelID, rect Rect) string {
	panel := m.side[p]
	frame := components.Frame{
		Title:   panel.Title(),
		Counter: panel.Counter(),
		Focused: !m.mainFocused && m.focus == p,
		Styles:  m.st,
	}
	if m.layout.Collapsed[p] {
		return frame.RenderCollapsed(rect.Width)
	}
	return frame.Render(rect.Width, rect.Height, panel.View())
}

func (m Model) renderMain(rect Rect) string {
	frame := components.Frame{
		Title:   m.main.Title(),
		Counter: m.main.Counter(),
		Focused: m.mainFocused,
		Styles:  m.st,
	}
	return frame.Render(rect.Width, rect.Height, m.main.View())
}

func (m Model) renderKeyBar(width int) string {
	if m.loadErr != nil {
		return components.Fit(" "+m.st.Error.Render("cannot read the dotfiles directory: "+m.loadErr.Error()), width)
	}
	if m.layout.TabStrip {
		return components.Fit(" "+m.tabStrip()+"   "+m.shortKeys(), width)
	}
	return components.Fit(" "+m.keyBarText(), width)
}

func (m Model) tabStrip() string {
	parts := make([]string, 0, SidePanelCount)
	for _, p := range sidePanels {
		label := "[" + string(rune('0'+int(p))) + "]" + p.String()
		if !m.mainFocused && m.focus == p {
			parts = append(parts, m.st.TabActive.Render(label))
			continue
		}
		parts = append(parts, m.st.TabInactive.Render(label))
	}
	return strings.Join(parts, " ")
}

func (m Model) shortKeys() string {
	return m.st.KeyName.Render("?") + " keys  " + m.st.KeyName.Render("+") + " mode  " + m.st.KeyName.Render("q") + " quit"
}

func (m Model) keyBarText() string {
	parts := make([]string, 0, 8)
	for _, binding := range m.contextKeys() {
		help := binding.Help()
		if !binding.Enabled() || help.Key == "" {
			continue
		}
		parts = append(parts, m.st.KeyName.Render(help.Key)+" "+help.Desc)
	}
	for _, binding := range m.globalKeys() {
		help := binding.Help()
		if help.Key == "" {
			continue
		}
		parts = append(parts, m.st.KeyName.Render(help.Key)+" "+help.Desc)
	}
	return strings.Join(parts, "  ")
}

func (m Model) contextKeys() []key.Binding {
	keys := append([]key.Binding{}, m.focusedPanel().Keys()...)
	if !m.mainFocused && m.focus == Packages {
		keys = append(keys, m.keys.Enter)
	}
	return keys
}

func (m Model) globalKeys() []key.Binding {
	return []key.Binding{
		m.keys.Panels,
		m.keys.NextPanel,
		m.keys.ModeNext,
		m.keys.Refresh,
		m.keys.Help,
		m.keys.Quit,
	}
}

func (m Model) keySections() []popups.Section {
	title := m.main.Title()
	if !m.mainFocused {
		title = m.side[m.focus].Title()
	}
	return []popups.Section{
		{Title: title, Bindings: m.contextKeys()},
		{Title: "Global", Bindings: append(m.globalKeys(), m.keys.Back, m.keys.ModePrev, m.keys.ForceQuit)},
	}
}
