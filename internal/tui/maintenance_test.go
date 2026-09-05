package tui

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/horbo/stower/internal/doctor"
	"github.com/horbo/stower/internal/dotfiles"
	"github.com/horbo/stower/internal/stow"
	"github.com/horbo/stower/internal/tui/popups"
)

func completeOperation(t *testing.T, m Model, cmd tea.Cmd) Model {
	t.Helper()
	batch := cmd().(tea.BatchMsg)
	messages := make(chan tea.Msg, 1)
	go func() { messages <- batch[0]() }()
	for {
		select {
		case msg := <-messages:
			updated, next := m.Update(msg)
			m = updated.(Model)
			if m.exec == nil {
				if next != nil {
					updated, _ = m.Update(next())
					m = updated.(Model)
				}
				return m
			}
			go func() { messages <- next() }()
		case <-time.After(10 * time.Second):
			t.Fatal("operation did not finish")
		}
	}
}

func TestRestoreUI(t *testing.T) {
	m := runApply(t, prepareApply(t))
	m = press(t, m, "esc", "1").(Model)
	for i := 0; i < len(m.pkgs); i++ {
		selected, _ := m.packages.Selected()
		if selected.Name == "misc" {
			break
		}
		m = press(t, m, "j").(Model)
	}
	m = press(t, m, "r").(Model)
	if !m.restoreOpen || !strings.Contains(m.View().Content, "Restore plan: misc") {
		t.Fatal("no restore plan")
	}
	m = press(t, m, "enter").(Model)
	if m.popup != popupConfirm {
		t.Fatal("restore confirmation missing")
	}
	updated, cmd := m.Update(popups.ConfirmedMsg{Action: actionRestore})
	m = completeOperation(t, updated.(Model), cmd)
	path := filepath.Join(m.paths.Target, ".bar")
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() {
		t.Fatalf("restored path: %v %v", info, err)
	}
	if _, err := os.Stat(filepath.Join(m.paths.Dotfiles, "misc")); !os.IsNotExist(err) {
		t.Fatal("package not removed")
	}
	if !strings.Contains(strings.Join(m.mainLog.Lines(), "\n"), "restored: misc") {
		t.Fatal(m.mainLog.Lines())
	}
}

func TestIssuesFixAndDiffUI(t *testing.T) {
	m := runApply(t, prepareApply(t))
	path := filepath.Join(m.paths.Target, ".bar")
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	write(t, path, "replacement\n")
	updated, _ := m.Update(refreshCmd(m.paths)())
	m = updated.(Model)
	m = press(t, m, "4").(Model)
	for i := 0; i < 10; i++ {
		issue, _ := m.issues.Selected()
		if issue.Package == "misc" {
			break
		}
		m = press(t, m, "j").(Model)
	}
	updated, cmd := m.Update(keyMsg("D"))
	m = updated.(Model)
	if !m.diffOpen {
		t.Fatal("D did not open diff")
	}
	updated, _ = m.Update(cmd())
	m = updated.(Model)
	if !strings.Contains(m.mainDiff.View(), "+replacement") {
		t.Fatal(m.mainDiff.View())
	}
	m = press(t, m, "esc", "f").(Model)
	if m.popup != popupFix {
		t.Fatal("f did not open fix popup")
	}
	for _, size := range [][2]int{{60, 16}, {70, 18}, {100, 30}} {
		updated, view := resize(t, m, size[0], size[1])
		assertScreen(t, view, size[0], size[1])
		m = updated.(Model)
	}
	updated, cmd = m.Update(popups.FixChosenMsg{Action: doctor.KeepTarget})
	m = completeOperation(t, updated.(Model), cmd)
	data, err := os.ReadFile(filepath.Join(m.paths.Dotfiles, "misc", "dot-bar"))
	if err != nil || string(data) != "replacement\n" {
		t.Fatal("target version not kept")
	}
	for _, info := range m.pkgs {
		if info.name == "misc" {
			for _, entry := range info.report.Entries {
				if entry.State != doctor.OK {
					t.Fatal(entry)
				}
			}
		}
	}
}

func TestRestoreBlockedAndRestowConfirmation(t *testing.T) {
	m := runApply(t, prepareApply(t))
	if err := os.Remove(filepath.Join(m.paths.Target, ".bar")); err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(m.paths.Target, ".bar"), "replaced")
	m = press(t, m, "1").(Model)
	for i := 0; i < len(m.pkgs); i++ {
		selected, _ := m.packages.Selected()
		if selected.Name == "misc" {
			break
		}
		m = press(t, m, "j").(Model)
	}
	m = press(t, m, "r", "enter").(Model)
	if m.popup == popupConfirm || m.restorePlan.Runnable() {
		t.Fatal("blocked restore offered confirmation")
	}
	if !strings.Contains(m.View().Content, "Issues") {
		t.Fatal("restore did not point to Issues")
	}
	m = press(t, m, "esc", "0", "R").(Model)
	if m.popup != popupConfirm || !strings.Contains(m.View().Content, "Restow all packages") {
		t.Fatal("restow confirmation missing")
	}
}

func TestPackageEntryDiffAndFixKeys(t *testing.T) {
	m := runApply(t, prepareApply(t))
	m = press(t, m, "1").(Model)
	for i := 0; i < len(m.pkgs); i++ {
		selected, _ := m.packages.Selected()
		if selected.Name == "misc" {
			break
		}
		m = press(t, m, "j").(Model)
	}
	m = press(t, m, "enter").(Model)
	updated, cmd := m.Update(keyMsg("D"))
	m = updated.(Model)
	if cmd == nil || !m.diffOpen {
		t.Fatal("package main D not wired")
	}
	updated, _ = m.Update(cmd())
	m = updated.(Model)
	m = press(t, m, "esc").(Model)
	if !m.mainFocused || m.diffOpen {
		t.Fatal("diff did not return to package")
	}
}

func TestCancelledRestoreDoesNotUnstow(t *testing.T) {
	m := runApply(t, prepareApply(t))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	plan := doctor.BuildRestorePlan(m.paths, "misc")
	summary := dotfiles.ExecuteRestore(ctx, plan, stow.Runner{Target: m.paths.Target, Dotfiles: m.paths.Dotfiles}, nil)
	if summary.OK() {
		t.Fatal("cancelled restore succeeded")
	}
	if _, managed := dotfiles.ManagedBy(m.paths, filepath.Join(m.paths.Target, ".bar")); !managed {
		t.Fatal("cancelled restore removed link")
	}
}
