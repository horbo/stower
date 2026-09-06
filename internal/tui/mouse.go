package tui

import (
	tea "charm.land/bubbletea/v2"

	"github.com/horbo/stower/internal/tui/components"
)

const wheelRows = 3

type clickable interface {
	Click(x, y int) tea.Cmd
}

type scrollable interface {
	Scroll(delta int)
}

func (r Rect) Contains(x, y int) bool {
	return x >= r.X && x < r.X+r.Width && y >= r.Y && y < r.Y+r.Height
}

func panelAt(l Layout, x, y int) (PanelID, bool) {
	if l.TooSmall {
		return Main, false
	}
	for _, p := range hitOrder {
		rect := l.Rect(p)
		if !rect.Empty() && rect.Contains(x, y) {
			return p, true
		}
	}
	return Main, false
}

func contentRow(rect Rect, y int) (int, bool) {
	row := y - rect.Y - 1
	if row < 0 || row >= max(0, rect.Height-2) {
		return 0, false
	}
	return row, true
}

func contentColumn(rect Rect, x int) int {
	pad := 2
	if rect.Width < 6 {
		pad = 1
	}
	return x - rect.X - pad
}

func (m Model) handleMouse(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	if !m.mouse || m.exec != nil || m.layout.TooSmall {
		return m, nil
	}
	mouse := msg.Mouse()
	if m.popup != popupNone {
		return m.mousePopup(msg, mouse)
	}
	switch msg.(type) {
	case tea.MouseClickMsg:
		if mouse.Button != tea.MouseLeft {
			return m, nil
		}
		return m.clickAt(mouse.X, mouse.Y)
	case tea.MouseWheelMsg:
		return m.wheelAt(msg, mouse)
	}
	return m, nil
}

func (m Model) clickAt(x, y int) (tea.Model, tea.Cmd) {
	panel, ok := panelAt(m.layout, x, y)
	if !ok {
		return m, nil
	}
	switch {
	case panel == KeyBar:
		return m.clickKeyBar(x)
	case panel == Main:
		return m.clickMain(x, y)
	default:
		return m.clickSide(panel, x, y)
	}
}

func (m Model) clickSide(p PanelID, x, y int) (tea.Model, tea.Cmd) {
	rect := m.layout.Rect(p)
	focused := m.focus == p && !m.mainFocused
	var cmd tea.Cmd
	if panel, ok := m.side[p].(clickable); ok {
		if row, inside := contentRow(rect, y); inside {
			cmd = panel.Click(contentColumn(rect, x), row)
		}
	}
	if !focused {
		cmd = nil
		m.closeLog()
		m.focus = p
		m.mainFocused = false
		m.relayout()
	}
	return m, tea.Batch(cmd, m.syncMain())
}

func (m Model) clickMain(x, y int) (tea.Model, tea.Cmd) {
	rect := m.layout.Main
	focused := m.mainFocused
	var cmd tea.Cmd
	if panel, ok := m.mainPanel().(clickable); ok {
		if row, inside := contentRow(rect, y); inside {
			cmd = panel.Click(contentColumn(rect, x), row)
		}
	}
	if !focused {
		cmd = nil
		if m.canFocusMain() {
			m.mainFocused = true
			m.relayout()
		}
	}
	return m, cmd
}

func (m Model) clickKeyBar(x int) (tea.Model, tea.Cmd) {
	item, ok := barItemAt(m.keyBarItems(), x, m.layout.KeyBar.Width)
	if !ok {
		return m, nil
	}
	press, ok := keyPress(item.key)
	if !ok {
		return m, nil
	}
	return m.handleKey(press)
}

func (m Model) wheelAt(msg tea.Msg, mouse tea.Mouse) (tea.Model, tea.Cmd) {
	delta, ok := wheelDelta(mouse.Button)
	if !ok {
		return m, nil
	}
	p, ok := panelAt(m.layout, mouse.X, mouse.Y)
	if !ok || p == KeyBar {
		return m, nil
	}
	panel := m.mainPanel()
	if p.IsSide() {
		panel = m.side[p]
	}
	if scroller, ok := panel.(scrollable); ok {
		scroller.Scroll(delta)
		return m, m.syncMain()
	}
	return m, panel.Update(msg)
}

func wheelDelta(button tea.MouseButton) (int, bool) {
	switch button {
	case tea.MouseWheelUp:
		return -wheelRows, true
	case tea.MouseWheelDown:
		return wheelRows, true
	default:
		return 0, false
	}
}

func (m Model) mousePopup(msg tea.Msg, mouse tea.Mouse) (tea.Model, tea.Cmd) {
	rect := m.popupRect()
	if rect.Empty() {
		return m, nil
	}
	popup := m.popupPanel()
	if !rect.Contains(mouse.X, mouse.Y) {
		if _, ok := msg.(tea.MouseClickMsg); !ok || mouse.Button != tea.MouseLeft || m.popup == popupFirstRun {
			return m, nil
		}
		return m.handleKey(tea.KeyPressMsg{Code: tea.KeyEscape})
	}
	switch msg.(type) {
	case tea.MouseClickMsg:
		if mouse.Button != tea.MouseLeft {
			return m, nil
		}
		row, inside := contentRow(rect, mouse.Y)
		if !inside {
			return m, nil
		}
		if target, ok := popup.(clickable); ok {
			return m, target.Click(contentColumn(rect, mouse.X), row)
		}
	case tea.MouseWheelMsg:
		delta, ok := wheelDelta(mouse.Button)
		if !ok {
			return m, nil
		}
		if target, ok := popup.(scrollable); ok {
			target.Scroll(delta)
		}
	}
	return m, nil
}

func (m Model) popupPanel() any {
	switch m.popup {
	case popupKeys:
		return m.keysPopup
	case popupAssign:
		return m.assignPopup
	case popupConfirm:
		return m.confirmPopup
	case popupError:
		return m.errorPopup
	case popupFix:
		return m.fixPopup
	case popupCommit:
		return m.commitPopup
	case popupFirstRun:
		return m.firstRunPopup
	}
	return nil
}

type barItem struct {
	key   string
	desc  string
	panel PanelID
	tab   bool
	gap   int
	x     int
	width int
}

func (b barItem) label() string {
	if b.tab {
		return "[" + b.key + "]" + b.desc
	}
	return b.key + " " + b.desc
}

func (m Model) keyBarItems() []barItem {
	if m.exec != nil || m.flash != "" || m.loadErr != nil {
		return nil
	}
	items := make([]barItem, 0, 12)
	if m.layout.TabStrip {
		for i, p := range sidePanels {
			gap := 1
			if i == 0 {
				gap = 0
			}
			items = append(items, barItem{key: string(rune('0' + int(p))), desc: p.String(), panel: p, tab: true, gap: gap})
		}
		for i, short := range [3]barItem{{key: "?", desc: "keys"}, {key: "+", desc: "mode"}, {key: "q", desc: "quit"}} {
			short.gap = 2
			if i == 0 {
				short.gap = 3
			}
			items = append(items, short)
		}
		return positionBar(items)
	}
	for _, binding := range m.contextKeys() {
		help := binding.Help()
		if !binding.Enabled() || help.Key == "" {
			continue
		}
		items = append(items, barItem{key: help.Key, desc: help.Desc, gap: 2})
	}
	for _, binding := range m.globalKeys() {
		help := binding.Help()
		if help.Key == "" {
			continue
		}
		items = append(items, barItem{key: help.Key, desc: help.Desc, gap: 2})
	}
	if len(items) > 0 {
		items[0].gap = 0
	}
	return positionBar(items)
}

func positionBar(items []barItem) []barItem {
	x := 1
	for i := range items {
		x += items[i].gap
		items[i].x = x
		items[i].width = components.Width(items[i].label())
		x += items[i].width
	}
	return items
}

func barItemAt(items []barItem, x, width int) (barItem, bool) {
	for _, item := range items {
		if item.x+item.width > width {
			break
		}
		if x >= item.x && x < item.x+item.width {
			return item, true
		}
	}
	return barItem{}, false
}

func keyPress(name string) (tea.KeyPressMsg, bool) {
	switch name {
	case "enter":
		return tea.KeyPressMsg{Code: tea.KeyEnter}, true
	case "esc":
		return tea.KeyPressMsg{Code: tea.KeyEscape}, true
	case "tab":
		return tea.KeyPressMsg{Code: tea.KeyTab}, true
	case "space":
		return tea.KeyPressMsg{Code: tea.KeySpace}, true
	case "ctrl+r":
		return tea.KeyPressMsg{Code: 'r', Mod: tea.ModCtrl}, true
	}
	if runes := []rune(name); len(runes) == 1 {
		return tea.KeyPressMsg{Code: runes[0], Text: name}, true
	}
	return tea.KeyPressMsg{}, false
}
