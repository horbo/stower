package tui

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/horbo/stower/internal/config"
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

func focusPackage(t *testing.T, m Model, name string) Model {
	t.Helper()
	m = press(t, m, "esc", "1").(Model)
	for i := 0; i < len(m.pkgs); i++ {
		selected, _ := m.packages.Selected()
		if selected.Name == name {
			return press(t, m, "enter").(Model)
		}
		m = press(t, m, "j").(Model)
	}
	t.Fatalf("package %s is not in the Packages panel", name)
	return m
}

func TestPackageEntryRestoreUI(t *testing.T) {
	m := runApply(t, prepareApply(t))
	for _, name := range []string{".other", ".third"} {
		path := filepath.Join(m.paths.Target, name)
		write(t, path, name+"\n")
		m = stageAndWait(t, m, path, "misc")
	}
	m.focus = Staged
	m = runApply(t, applyAndWait(t, m))

	m = focusPackage(t, m, "misc")
	updated, _ := m.Update(stagingKeyMsg("space"))
	m = press(t, updated, "j").(Model)
	updated, _ = m.Update(stagingKeyMsg("space"))
	m = updated.(Model)
	if marked := m.mainPkg.Marked(); len(marked) != 2 {
		t.Fatalf("marked = %v, want two entries", marked)
	}
	if !strings.Contains(ansi.Strip(m.mainPkg.View()), "✓ dot-bar") {
		t.Fatalf("the package table does not mark the entry:\n%s", ansi.Strip(m.mainPkg.View()))
	}

	m = press(t, m, "r").(Model)
	if !m.restoreOpen {
		t.Fatal("r did not open the entry restore plan")
	}
	view := ansi.Strip(m.View().Content)
	for _, want := range []string{"Restore plan: misc", "moves back", "stays linked"} {
		if !strings.Contains(view, want) {
			t.Fatalf("restore plan does not contain %q:\n%s", want, view)
		}
	}

	for _, size := range [][2]int{{60, 16}, {70, 18}, {100, 30}} {
		updated, rendered := resize(t, m, size[0], size[1])
		assertScreen(t, rendered, size[0], size[1])
		m = updated.(Model)
	}

	m = press(t, m, "enter").(Model)
	if m.popup != popupConfirm {
		t.Fatal("entry restore did not ask for confirmation")
	}
	for _, size := range [][2]int{{60, 16}, {70, 18}, {100, 30}} {
		updated, rendered := resize(t, m, size[0], size[1])
		assertScreen(t, rendered, size[0], size[1])
		m = updated.(Model)
	}
	view = ansi.Strip(m.View().Content)
	for _, want := range []string{"misc/dot-bar", "misc/dot-other", "Keep 1 entry linked"} {
		if !strings.Contains(view, want) {
			t.Fatalf("confirmation does not contain %q:\n%s", want, view)
		}
	}

	updated, cmd := m.Update(popups.ConfirmedMsg{Action: actionRestore})
	m = completeOperation(t, updated.(Model), cmd)

	for _, name := range []string{".bar", ".other"} {
		info, err := os.Lstat(filepath.Join(m.paths.Target, name))
		if err != nil || !info.Mode().IsRegular() {
			t.Fatalf("%s was not restored: %v %v", name, info, err)
		}
	}
	if pkg, managed := dotfiles.ManagedBy(m.paths, filepath.Join(m.paths.Target, ".third")); !managed || pkg != "misc" {
		t.Fatalf(".third owner=%q managed=%v, want it still linked", pkg, managed)
	}
	if _, err := os.Stat(filepath.Join(m.paths.Dotfiles, "misc", "dot-third")); err != nil {
		t.Fatalf("the package lost the remaining entry: %v", err)
	}
	if !strings.Contains(strings.Join(m.mainLog.Lines(), "\n"), "restored: misc") {
		t.Fatal(m.mainLog.Lines())
	}
}

func TestPackageEntryRestoreLastEntryRemovesPackage(t *testing.T) {
	m := runApply(t, prepareApply(t))
	m = focusPackage(t, m, "misc")
	m = press(t, m, "r").(Model)
	if !m.restoreOpen || m.restorePlan.Partial() {
		t.Fatalf("the only link point must restore like the whole package: partial=%v", m.restorePlan.Partial())
	}
	m = press(t, m, "enter").(Model)
	updated, cmd := m.Update(popups.ConfirmedMsg{Action: actionRestore})
	m = completeOperation(t, updated.(Model), cmd)
	if _, err := os.Stat(filepath.Join(m.paths.Dotfiles, "misc")); !os.IsNotExist(err) {
		t.Fatalf("the package directory survived the last entry restore: %v", err)
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

func newUnownedModel(t *testing.T) Model {
	t.Helper()
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", root)
	dotfilesDir := filepath.Join(root, "dotfiles")
	mkdir(t, filepath.Join(dotfilesDir, "zsh"))
	write(t, filepath.Join(dotfilesDir, "zsh", "dot-zshrc"), "a\n")
	if err := os.Symlink(filepath.Join(dotfilesDir, "zsh", "dot-zshrc"), filepath.Join(root, ".zshrc")); err != nil {
		t.Fatal(err)
	}
	model := New(config.Paths{Target: root, Dotfiles: dotfilesDir}, "2.4.1")
	updated := declineGitInit(deliverStagingCmd(t, model, model.Init()))
	updated, _ = updated.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	return updated.(Model)
}

func TestUnownedFixUI(t *testing.T) {
	if _, err := exec.LookPath("stow"); err != nil {
		t.Fatal(err)
	}
	m := newUnownedModel(t)
	m = press(t, m, "4").(Model)
	issue, ok := m.issues.Selected()
	if !ok || issue.State != doctor.Unowned || issue.Package != "zsh" {
		t.Fatalf("selected issue = %+v, want an unowned zsh entry", issue)
	}
	for _, size := range [][2]int{{60, 16}, {70, 18}, {100, 30}} {
		updated, rendered := resize(t, m, size[0], size[1])
		assertScreen(t, rendered, size[0], size[1])
		m = updated.(Model)
	}
	m = press(t, m, "f").(Model)
	if m.popup != popupConfirm || m.fixAction != doctor.Relink {
		t.Fatalf("popup=%v action=%q, want the confirm popup with relink", m.popup, m.fixAction)
	}
	for _, size := range [][2]int{{60, 16}, {70, 18}, {100, 30}} {
		updated, rendered := resize(t, m, size[0], size[1])
		assertScreen(t, rendered, size[0], size[1])
		m = updated.(Model)
	}
	view := ansi.Strip(m.View().Content)
	for _, want := range []string{"Fix unowned", "zsh/dot-zshrc", "relink"} {
		if !strings.Contains(view, want) {
			t.Fatalf("confirmation does not contain %q:\n%s", want, view)
		}
	}
	updated, cmd := m.Update(popups.ConfirmedMsg{Action: actionFix})
	m = completeOperation(t, updated.(Model), cmd)
	dest, err := os.Readlink(filepath.Join(m.paths.Target, ".zshrc"))
	if err != nil || dest != filepath.Join("dotfiles", "zsh", "dot-zshrc") {
		t.Fatalf("readlink = %q, %v; want a relative stow link", dest, err)
	}
	if _, ok := m.issues.Selected(); ok {
		t.Fatal("the unowned issue survived the fix")
	}
	for _, info := range m.pkgs {
		for _, entry := range info.report.Entries {
			if entry.State != doctor.OK {
				t.Fatalf("%s: %+v", info.name, entry)
			}
		}
	}
}
