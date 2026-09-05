package tui

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/horbo/stower/internal/config"
	"github.com/horbo/stower/internal/doctor"
	"github.com/horbo/stower/internal/tui/popups"
)

func clickMsg(x, y int) tea.MouseClickMsg {
	return tea.MouseClickMsg{X: x, Y: y, Button: tea.MouseLeft}
}

func wheelMsg(x, y int, button tea.MouseButton) tea.MouseWheelMsg {
	return tea.MouseWheelMsg{X: x, Y: y, Button: button}
}

func click(t *testing.T, model tea.Model, x, y int) tea.Model {
	t.Helper()
	updated, cmd := model.Update(clickMsg(x, y))
	if cmd == nil {
		return updated
	}
	if msg := cmd(); msg != nil {
		updated, _ = updated.Update(msg)
	}
	return updated
}

func wheel(t *testing.T, model tea.Model, x, y int, button tea.MouseButton) tea.Model {
	t.Helper()
	updated, _ := model.Update(wheelMsg(x, y, button))
	return updated
}

func rowPoint(m Model, p PanelID, row int) (int, int) {
	rect := m.layout.Rect(p)
	return rect.X + 2, rect.Y + 1 + row
}

func TestPanelAt(t *testing.T) {
	landscape := Compute(100, 30, Packages, ModeNormal)
	portrait := Compute(70, 18, Home, ModeNormal)
	tooSmall := Compute(50, 10, Packages, ModeNormal)

	tests := []struct {
		name   string
		layout Layout
		x, y   int
		want   PanelID
		ok     bool
	}{
		{name: "landscape status", layout: landscape, x: 0, y: 0, want: Status, ok: true},
		{name: "landscape packages", layout: landscape, x: 2, y: 4, want: Packages, ok: true},
		{name: "landscape home", layout: landscape, x: 2, y: 11, want: Home, ok: true},
		{name: "landscape staged", layout: landscape, x: 2, y: 18, want: Staged, ok: true},
		{name: "landscape issues", layout: landscape, x: 2, y: 24, want: Issues, ok: true},
		{name: "landscape main", layout: landscape, x: 60, y: 10, want: Main, ok: true},
		{name: "landscape key bar", layout: landscape, x: 5, y: 29, want: KeyBar, ok: true},
		{name: "outside the screen", layout: landscape, x: 120, y: 5},
		{name: "negative", layout: landscape, x: -1, y: -1},
		{name: "portrait side panel", layout: portrait, x: 2, y: 2, want: Home, ok: true},
		{name: "portrait main", layout: portrait, x: 2, y: 10, want: Main, ok: true},
		{name: "portrait key bar", layout: portrait, x: 2, y: 17, want: KeyBar, ok: true},
		{name: "portrait hidden panel", layout: portrait, x: 2, y: 30},
		{name: "too small", layout: tooSmall, x: 1, y: 1},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			panel, ok := panelAt(test.layout, test.x, test.y)
			if ok != test.ok {
				t.Fatalf("panelAt(%d, %d) ok = %v, want %v", test.x, test.y, ok, test.ok)
			}
			if ok && panel != test.want {
				t.Fatalf("panelAt(%d, %d) = %s, want %s", test.x, test.y, panel, test.want)
			}
		})
	}
}

func TestPanelAtCoversEveryCell(t *testing.T) {
	for _, mode := range testModes {
		for _, focus := range testFocus {
			l := Compute(100, 30, focus, mode)
			for y := 0; y < 30; y++ {
				for x := 0; x < 100; x++ {
					if _, ok := panelAt(l, x, y); !ok {
						t.Fatalf("%s/%s: cell %d,%d hits no panel", mode, focus, x, y)
					}
				}
			}
		}
	}
}

func TestClickSelectsPackageAndFocusesMain(t *testing.T) {
	model, _ := resize(t, newTestModel(t), 100, 30)
	m := model.(Model)
	if selected, _ := m.packages.Selected(); selected.Name != "misc" {
		t.Fatalf("the first package is %q", selected.Name)
	}

	x, y := rowPoint(m, Packages, 1)
	model = click(t, model, x, y)
	m = model.(Model)
	if selected, _ := m.packages.Selected(); selected.Name != "zsh" {
		t.Fatalf("after the click the selection is %q", selected.Name)
	}
	if m.mainFocused {
		t.Fatal("the first click must not move the focus into main")
	}
	if !strings.Contains(m.View().Content, "Package: zsh") {
		t.Fatal("the main panel did not follow the click")
	}

	model = click(t, model, x, y)
	if !model.(Model).mainFocused {
		t.Fatal("clicking the highlighted row again did not focus main")
	}
}

func TestClickMovesFocusBetweenPanels(t *testing.T) {
	model, _ := resize(t, newTestModel(t), 100, 30)
	x, root := rowPoint(model.(Model), Home, 0)
	_, second := rowPoint(model.(Model), Home, 1)

	model = click(t, model, x, second)
	m := model.(Model)
	if m.focus != Home || m.mainFocused {
		t.Fatalf("after clicking Home the focus is %s (main %v)", m.focus, m.mainFocused)
	}
	if counter := m.homePanel.Counter(); counter != "2 of 2" {
		t.Fatalf("the click selected %q, want the second row", counter)
	}

	model = click(t, model, x, root)
	m = model.(Model)
	if selected, ok := m.homePanel.Selected(); !ok || selected.Path != m.paths.Target {
		t.Fatal("the click did not select the tree root")
	}
	if counter := m.homePanel.Counter(); counter != "1 of 2" {
		t.Fatalf("the tree shows %q, want the expanded root", counter)
	}

	model = click(t, model, x, root)
	if counter := model.(Model).homePanel.Counter(); counter != "1 of 1" {
		t.Fatalf("clicking the highlighted directory did not collapse it: %q", counter)
	}
	model = click(t, model, x, root)
	if counter := model.(Model).homePanel.Counter(); counter != "1 of 2" {
		t.Fatalf("clicking the highlighted directory did not expand it again: %q", counter)
	}
}

func TestClickCollapsedPanelExpandsIt(t *testing.T) {
	model, _ := resize(t, newTestModel(t), 100, 24)
	m := model.(Model)
	if !m.layout.Collapsed[Issues] {
		t.Fatal("Issues must be collapsed at 100x24 with Packages focused")
	}
	rect := m.layout.Rect(Issues)
	model = click(t, model, rect.X+2, rect.Y)
	m = model.(Model)
	if m.focus != Issues {
		t.Fatalf("clicking the collapsed panel focused %s", m.focus)
	}
	if m.layout.Collapsed[Issues] {
		t.Fatal("the clicked panel is still collapsed")
	}
}

func TestWheelScrollsWithoutChangingFocus(t *testing.T) {
	model, _ := resize(t, newTestModel(t), 100, 30)
	x, y := rowPoint(model.(Model), Home, 0)

	model = wheel(t, model, x, y, tea.MouseWheelDown)
	m := model.(Model)
	if m.focus != Packages || m.mainFocused {
		t.Fatalf("the wheel moved the focus to %s (main %v)", m.focus, m.mainFocused)
	}
	if counter := m.homePanel.Counter(); counter != "2 of 2" {
		t.Fatalf("the wheel did not scroll Home: %q", counter)
	}
	model = wheel(t, model, x, y, tea.MouseWheelUp)
	if counter := model.(Model).homePanel.Counter(); counter != "1 of 2" {
		t.Fatalf("the wheel did not scroll Home back: %q", counter)
	}

	model = wheel(t, model, 60, 10, tea.MouseWheelDown)
	m = model.(Model)
	if m.focus != Packages || m.mainFocused {
		t.Fatal("the wheel over main moved the focus")
	}
	if selected, _ := m.packages.Selected(); selected.Name != "misc" {
		t.Fatalf("the wheel over main changed the package selection to %q", selected.Name)
	}
}

func TestWheelScrollsTheMainViewport(t *testing.T) {
	root := t.TempDir()
	dotfilesDir := filepath.Join(root, "dotfiles")
	mkdir(t, filepath.Join(dotfilesDir, "big"))
	for i := 0; i < 40; i++ {
		write(t, filepath.Join(dotfilesDir, "big", fmt.Sprintf("dot-entry%02d", i)), "x")
	}
	model := New(config.Paths{Target: root, Dotfiles: dotfilesDir}, "2.4.1")
	updated, _ := model.Update(model.Init()())
	base, view := resize(t, declineGitInit(updated), 100, 30)
	if first := lineAt(view, 2); !strings.Contains(first, "dot-entry00") {
		t.Fatalf("the package table does not start at the first entry: %q", first)
	}

	scrolled := wheel(t, base, 60, 10, tea.MouseWheelDown)
	m := scrolled.(Model)
	after := ansi.Strip(m.View().Content)
	if first := lineAt(after, 1); !strings.Contains(first, fmt.Sprintf("dot-entry%02d", wheelRows-1)) {
		t.Fatalf("the wheel did not scroll the package table by %d rows: %q", wheelRows, first)
	}
	if m.mainFocused || m.focus != Packages {
		t.Fatal("the wheel over main changed the focus")
	}
	if !strings.Contains(after, "1 of 1") {
		t.Fatal("the wheel moved the packages cursor")
	}
}

func TestKeyBarClickSendsTheKey(t *testing.T) {
	model, _ := resize(t, newTestModel(t), 100, 30)
	m := model.(Model)
	items := m.keyBarItems()
	if len(items) == 0 {
		t.Fatal("the key bar has no items")
	}
	restore, ok := barItemByKey(items, "r")
	if !ok {
		t.Fatal("the key bar has no restore item")
	}
	model = click(t, model, restore.x, m.layout.KeyBar.Y)
	if !model.(Model).restoreOpen {
		t.Fatal("clicking r restore did not open the restore plan")
	}

	move, ok := barItemByKey(items, "j/k")
	if !ok {
		t.Fatal("the key bar has no j/k item")
	}
	before := model.(Model)
	after := click(t, model, move.x, before.layout.KeyBar.Y).(Model)
	if after.focus != before.focus || after.mainFocused != before.mainFocused {
		t.Fatal("clicking a multi-key label changed the model")
	}
}

func TestKeyBarTabStripSwitchesPanels(t *testing.T) {
	model, _ := resize(t, newTestModel(t), 70, 18)
	m := model.(Model)
	if !m.layout.TabStrip {
		t.Fatal("70x18 must show the tab strip")
	}
	home, ok := barItemByKey(m.keyBarItems(), "2")
	if !ok || !home.tab {
		t.Fatal("the tab strip has no Home tab")
	}
	model = click(t, model, home.x, m.layout.KeyBar.Y)
	if focus := model.(Model).focus; focus != Home {
		t.Fatalf("clicking the Home tab focused %s", focus)
	}

	keys, ok := barItemByKey(model.(Model).keyBarItems(), "?")
	if !ok {
		t.Fatal("the tab strip has no keys item")
	}
	model = click(t, model, keys.x, m.layout.KeyBar.Y)
	if model.(Model).popup != popupKeys {
		t.Fatal("clicking ? keys did not open the keys popup")
	}
}

func TestKeyBarClickIgnoresTruncatedItems(t *testing.T) {
	model, _ := resize(t, newTestModel(t), 70, 18)
	m := model.(Model)
	quit, ok := barItemByKey(m.keyBarItems(), "q")
	if !ok {
		t.Fatal("the key bar has no quit item")
	}
	if quit.x+quit.width <= m.layout.KeyBar.Width {
		t.Fatalf("q quit fits at %dx%d, pick another item for this test", 70, 18)
	}
	updated, cmd := model.Update(clickMsg(quit.x, m.layout.KeyBar.Y))
	if cmd != nil {
		t.Fatal("a click on a truncated key bar item produced a command")
	}
	if updated.(Model).popup != popupNone {
		t.Fatal("a click on a truncated key bar item changed the model")
	}
}

func TestPopupClickOutsideClosesIt(t *testing.T) {
	model, _ := resize(t, newTestModel(t), 100, 30)
	model = press(t, model, "?")
	if model.(Model).popup != popupKeys {
		t.Fatal("? did not open the keys popup")
	}
	model = click(t, model, 0, 0)
	if model.(Model).popup != popupNone {
		t.Fatal("a click outside the popup did not close it")
	}
}

func TestFixPopupClickChoosesKeepTarget(t *testing.T) {
	model, _ := resize(t, newReplacedModel(t), 100, 30)
	model = press(t, model, "4", "f")
	m := model.(Model)
	if m.popup != popupFix {
		t.Fatalf("f opened popup %d, want the fix popup", m.popup)
	}
	x := m.popupRect().X + 2
	y := lineWith(t, model.View().Content, "Keep TARGET")

	updated, cmd := model.Update(clickMsg(x, y))
	if cmd != nil {
		t.Fatal("the first click on an option must only select it")
	}

	_, cmd = updated.Update(clickMsg(x, y))
	if cmd == nil {
		t.Fatal("the second click on Keep TARGET produced no command")
	}
	chosen, ok := cmd().(popups.FixChosenMsg)
	if !ok || chosen.Action != doctor.KeepTarget {
		t.Fatalf("the click produced %#v, want Keep TARGET", chosen)
	}
}

func TestConfirmPopupClickConfirmsAndCancels(t *testing.T) {
	model, _ := resize(t, newTestModel(t), 100, 30)
	model = press(t, model, "R")
	m := model.(Model)
	if m.popup != popupConfirm {
		t.Fatal("R did not open the confirmation")
	}
	rect := m.popupRect()
	y := lineWith(t, model.View().Content, "y / enter confirm")

	_, cmd := model.Update(clickMsg(rect.X+2, y))
	if cmd == nil {
		t.Fatal("clicking the confirm half produced no command")
	}
	if confirmed, ok := cmd().(popups.ConfirmedMsg); !ok || confirmed.Action != actionRestow {
		t.Fatalf("the click produced %#v, want a confirmation", cmd())
	}

	_, cmd = model.Update(clickMsg(rect.X+2+25, y))
	if cmd == nil {
		t.Fatal("clicking the cancel half produced no command")
	}
	if _, ok := cmd().(popups.ConfirmCancelledMsg); !ok {
		t.Fatalf("the click produced %#v, want a cancellation", cmd())
	}
}

func TestAssignPopupClickPicksAPackage(t *testing.T) {
	model, paths := newStagingModel(t)
	model = send(t, model, "2")
	model = moveTo(t, model, filepath.Join(paths.Target, ".bar"))
	model = send(t, model, "space")
	m := model.(Model)
	if m.popup != popupAssign {
		t.Fatal("space did not open the assign popup")
	}
	rect := m.popupRect()
	x, y := rect.X+2, rect.Y+1+3
	if line := lineAt(model.View().Content, y); !strings.Contains(line[rect.X:], "zsh") {
		t.Fatalf("the assign popup does not list zsh at row 3: %q", line)
	}

	model = click(t, model, x, y)
	if model.(Model).popup != popupAssign {
		t.Fatal("the first click closed the assign popup")
	}
	model = click(t, model, x, y)
	m = model.(Model)
	if m.popup != popupNone {
		t.Fatal("the second click did not confirm the package")
	}
	if got := m.staging[filepath.Join(paths.Target, ".bar")]; got != "zsh" {
		t.Fatalf(".bar is staged into %q, want zsh", got)
	}
}

func TestStagedPanelClickSelectsARow(t *testing.T) {
	model, paths := newStagingModel(t)
	model = send(t, model, "2")
	model = moveTo(t, model, filepath.Join(paths.Target, ".bar"))
	model = send(t, model, "space")
	model = typeName(t, model, "misc")

	x, y := rowPoint(model.(Model), Staged, 1)
	model = click(t, model, x, y)
	m := model.(Model)
	if m.focus != Staged {
		t.Fatalf("the click focused %s", m.focus)
	}
	row, ok := m.staged.Selected()
	if !ok || row.Path != filepath.Join(paths.Target, ".bar") {
		t.Fatalf("the click selected %+v, want the staged entry", row)
	}

	_, y = rowPoint(m, Staged, 0)
	model = click(t, model, x, y)
	if row, ok := model.(Model).staged.Selected(); !ok || !row.Group || row.Package != "misc" {
		t.Fatalf("the click selected %+v, want the package group", row)
	}
}

func TestNoMouseIgnoresEveryMouseMessage(t *testing.T) {
	model, _ := resize(t, newTestModel(t).(Model).WithMouse(false), 100, 30)
	if mode := model.View().MouseMode; mode != tea.MouseModeNone {
		t.Fatalf("--no-mouse leaves MouseMode = %v", mode)
	}
	before := model.(Model)
	x, y := rowPoint(before, Packages, 1)

	for _, msg := range []tea.Msg{clickMsg(x, y), wheelMsg(x, y, tea.MouseWheelDown), tea.MouseMotionMsg{X: x, Y: y}, tea.MouseReleaseMsg{X: x, Y: y}} {
		updated, cmd := model.Update(msg)
		if cmd != nil {
			t.Fatalf("%T produced a command with --no-mouse", msg)
		}
		model = updated
	}
	after := model.(Model)
	selected, _ := after.packages.Selected()
	if selected.Name != "misc" || after.focus != before.focus || after.mainFocused {
		t.Fatalf("mouse messages changed the model with --no-mouse: %q %s", selected.Name, after.focus)
	}
}

func TestMouseIsEnabledByDefault(t *testing.T) {
	model, _ := resize(t, newTestModel(t), 100, 30)
	if mode := model.View().MouseMode; mode != tea.MouseModeCellMotion {
		t.Fatalf("MouseMode = %v, want cell motion", mode)
	}
}

func lineAt(view string, y int) string {
	lines := strings.Split(ansi.Strip(view), "\n")
	if y < 0 || y >= len(lines) {
		return ""
	}
	return lines[y]
}

func lineWith(t *testing.T, view, needle string) int {
	t.Helper()
	for i, line := range strings.Split(ansi.Strip(view), "\n") {
		if strings.Contains(line, needle) {
			return i
		}
	}
	t.Fatalf("no line contains %q:\n%s", needle, ansi.Strip(view))
	return 0
}

func barItemByKey(items []barItem, key string) (barItem, bool) {
	for _, item := range items {
		if item.key == key {
			return item, true
		}
	}
	return barItem{}, false
}

func newReplacedModel(t *testing.T) tea.Model {
	t.Helper()
	root := t.TempDir()
	dotfilesDir := filepath.Join(root, "dotfiles")
	mkdir(t, filepath.Join(dotfilesDir, "misc"))
	write(t, filepath.Join(dotfilesDir, "misc", "dot-bar"), "repo")
	write(t, filepath.Join(root, ".bar"), "target")

	model := New(config.Paths{Target: root, Dotfiles: dotfilesDir}, "2.4.1")
	updated, _ := model.Update(model.Init()())
	return declineGitInit(updated)
}
