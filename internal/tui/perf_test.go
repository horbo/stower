package tui

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"charm.land/bubbles/v2/spinner"
	tea "charm.land/bubbletea/v2"

	"github.com/horbo/stower/internal/config"
	"github.com/horbo/stower/internal/dotfiles"
	"github.com/horbo/stower/internal/tui/components"
	mainpanel "github.com/horbo/stower/internal/tui/main"
)

func perfMkdir(tb testing.TB, path string) {
	tb.Helper()
	if err := os.MkdirAll(path, 0o755); err != nil {
		tb.Fatal(err)
	}
}

func perfWrite(tb testing.TB, path string) {
	tb.Helper()
	if err := os.WriteFile(path, nil, 0o644); err != nil {
		tb.Fatal(err)
	}
}

func deliverSync(tb testing.TB, m *Model, cmd tea.Cmd) {
	tb.Helper()
	if cmd == nil {
		return
	}
	msg := cmd()
	if batch, ok := msg.(tea.BatchMsg); ok {
		for _, leaf := range batch {
			deliverSync(tb, m, leaf)
		}
		return
	}
	switch msg.(type) {
	case spinner.TickMsg, flashExpiredMsg:
		return
	}
	updated, next := m.Update(msg)
	*m = updated.(Model)
	deliverSync(tb, m, next)
}

func newFlatHomeModel(tb testing.TB, n int) Model {
	tb.Helper()
	root := tb.TempDir()
	dotfilesDir := filepath.Join(root, "dotfiles")
	perfMkdir(tb, dotfilesDir)
	big := filepath.Join(root, "big")
	perfMkdir(tb, big)
	for i := 0; i < n; i++ {
		perfWrite(tb, filepath.Join(big, fmt.Sprintf("file-%05d", i)))
	}
	paths := config.Paths{Target: big, Dotfiles: dotfilesDir}
	m := declineGitInit(New(paths, "2.4.1")).(Model)
	deliverSync(tb, &m, m.homePanel.Reload())
	m.focus = Home
	return m
}

func buildDeepChainTree(tb testing.TB, root string, depth, breadth int) {
	tb.Helper()
	dir := root
	for level := 0; level < depth; level++ {
		perfMkdir(tb, dir)
		for i := 0; i < breadth-1; i++ {
			perfWrite(tb, filepath.Join(dir, fmt.Sprintf("file-%02d-%02d", level, i)))
		}
		dir = filepath.Join(dir, fmt.Sprintf("sub-%02d", level))
	}
	perfMkdir(tb, dir)
	perfWrite(tb, filepath.Join(dir, "leaf"))
}

func newDeepHomeModel(tb testing.TB, depth, breadth, expandLevels int) Model {
	tb.Helper()
	root := tb.TempDir()
	dotfilesDir := filepath.Join(root, "dotfiles")
	perfMkdir(tb, dotfilesDir)
	deep := filepath.Join(root, "deep")
	buildDeepChainTree(tb, deep, depth, breadth)
	paths := config.Paths{Target: deep, Dotfiles: dotfilesDir}
	m := declineGitInit(New(paths, "2.4.1")).(Model)
	deliverSync(tb, &m, m.homePanel.Reload())
	m.focus = Home

	for level := 0; level < expandLevels; level++ {
		updated, _ := m.Update(tea.KeyPressMsg{Code: []rune("j")[0], Text: "j"})
		m = updated.(Model)
		row, ok := m.homePanel.Selected()
		if !ok || !row.Expandable {
			break
		}
		updated, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyRight})
		m = updated.(Model)
		deliverSync(tb, &m, cmd)
	}
	return m
}

func BenchmarkHomeCursorMoveFlat(b *testing.B) {
	m := newFlatHomeModel(b, 5000)
	m.inspectHomeEntry = func(context.Context, string) mainpanel.EntryFacts { return mainpanel.EntryFacts{} }
	msg := tea.KeyPressMsg{Code: []rune("j")[0], Text: "j"}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		updated, _ := m.Update(msg)
		m = updated.(Model)
	}
}

func BenchmarkHomeCursorMoveDeep(b *testing.B) {
	m := newDeepHomeModel(b, 6, 8, 4)
	m.inspectHomeEntry = func(context.Context, string) mainpanel.EntryFacts { return mainpanel.EntryFacts{} }
	msg := tea.KeyPressMsg{Code: []rune("j")[0], Text: "j"}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		updated, _ := m.Update(msg)
		m = updated.(Model)
	}
}

func TestHomeCursorMoveDoesNotScan(t *testing.T) {
	m := newFlatHomeModel(t, 5000)
	var inspectCalls int
	m.inspectHomeEntry = func(context.Context, string) mainpanel.EntryFacts {
		inspectCalls++
		return mainpanel.EntryFacts{}
	}
	var planCalls int
	m.buildAdoptPlan = func(context.Context, config.Paths, dotfiles.Staging) dotfiles.AdoptPlan {
		planCalls++
		return dotfiles.AdoptPlan{}
	}

	updated, cmd := m.Update(tea.KeyPressMsg{Code: []rune("j")[0], Text: "j"})
	m = updated.(Model)
	if inspectCalls != 0 {
		t.Fatalf("Update(j) called inspectHomeEntry synchronously %d times, want 0", inspectCalls)
	}
	if planCalls != 0 {
		t.Fatalf("Update(j) called buildAdoptPlan synchronously %d times, want 0", planCalls)
	}
	if cmd == nil {
		return
	}
	msg := cmd()
	batch, ok := msg.(tea.BatchMsg)
	if !ok {
		if _, ok := msg.(components.TreeLoadedMsg); ok {
			t.Fatal("Update(j) triggered a synchronous tree load")
		}
		return
	}
	for _, leaf := range batch {
		if leaf == nil {
			continue
		}
		if _, ok := leaf().(components.TreeLoadedMsg); ok {
			t.Fatal("Update(j) scheduled a tree load")
		}
	}
}
