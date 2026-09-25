package tui

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/horbo/stower/internal/config"
	"github.com/horbo/stower/internal/doctor"
	"github.com/horbo/stower/internal/tui/popups"
)

func TestInvisibleFixConvertsGitlink(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed")
	}
	requireStow(t)
	t.Setenv("GIT_CONFIG_GLOBAL", filepath.Join(t.TempDir(), "absent"))
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", root)
	dotfilesDir := filepath.Join(root, "dotfiles")
	mkdir(t, dotfilesDir)
	nestedGitCommand(t, dotfilesDir, "init")
	plugin := filepath.Join(dotfilesDir, "zsh", "dot-oh-my-zsh", "plugins", "remote")
	mkdir(t, plugin)
	write(t, filepath.Join(dotfilesDir, "zsh", "dot-zshrc"), "z\n")
	write(t, filepath.Join(plugin, "remote.zsh"), "r\n")
	nestedGitCommand(t, plugin, "init")
	nestedGitCommand(t, plugin, "add", ".")
	nestedGitCommand(t, plugin, "commit", "-m", "initial")
	nestedGitCommand(t, plugin, "remote", "add", "origin", "https://example.invalid/remote.git")
	nestedGitCommand(t, dotfilesDir, "add", ".")
	nestedGitCommand(t, dotfilesDir, "commit", "-m", "initial")

	model := New(config.Paths{Target: root, Dotfiles: dotfilesDir}, "2.4.1")
	var updated tea.Model = declineGitInit(deliverStagingCmd(t, model, model.Init()))
	updated, _ = updated.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	m := press(t, updated, "4").(Model)
	for i := 0; i < 10; i++ {
		if issue, _ := m.issues.Selected(); issue.State == doctor.Invisible {
			break
		}
		m = press(t, m, "j").(Model)
	}
	if issue, ok := m.issues.Selected(); !ok || issue.State != doctor.Invisible || !issue.Fixable {
		t.Fatalf("selected issue = %+v", issue)
	}
	updated, cmd := m.Update(keyMsg("f"))
	if cmd == nil {
		t.Fatal("f did not load the Git links")
	}
	updated, _ = updated.Update(cmd())
	m = updated.(Model)
	if m.popup != popupRepositories || !m.gitlinkFix {
		t.Fatalf("popup = %v, want the Git links popup", m.popup)
	}
	view := ansi.Strip(m.View().Content)
	for _, want := range []string{"Git links in zsh", "Convert to submodule"} {
		if !strings.Contains(view, want) {
			t.Fatalf("popup does not contain %q:\n%s", want, view)
		}
	}
	updated, cmd = m.Update(keyMsg("enter"))
	updated, _ = updated.Update(cmd())
	m = updated.(Model)
	if m.popup != popupConfirm {
		t.Fatalf("popup = %v, want the confirmation", m.popup)
	}
	view = ansi.Strip(m.View().Content)
	if !strings.Contains(view, "Fix invisible zsh") || !strings.Contains(view, "plugins/remote → Convert to submodule") {
		t.Fatalf("confirmation:\n%s", view)
	}
	updated, cmd = m.Update(popups.ConfirmedMsg{Action: actionFix})
	m = completeOperation(t, updated.(Model), cmd)
	modules, err := os.ReadFile(filepath.Join(dotfilesDir, ".gitmodules"))
	if err != nil || !strings.Contains(string(modules), "url = https://example.invalid/remote.git") {
		t.Fatalf(".gitmodules = %q, %v", modules, err)
	}
	updated, _ = m.Update(refreshCmd(m.paths)())
	m = updated.(Model)
	for _, info := range m.pkgs {
		for _, entry := range info.report.Entries {
			if entry.State == doctor.Invisible {
				t.Fatalf("%s: %+v", info.name, entry)
			}
		}
	}
}

func TestOrphanedFixRemovesGitmodulesEntry(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed")
	}
	requireStow(t)
	t.Setenv("GIT_CONFIG_GLOBAL", filepath.Join(t.TempDir(), "absent"))
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", root)
	dotfilesDir := filepath.Join(root, "dotfiles")
	mkdir(t, dotfilesDir)
	nestedGitCommand(t, dotfilesDir, "init")
	mkdir(t, filepath.Join(dotfilesDir, "zsh"))
	write(t, filepath.Join(dotfilesDir, "zsh", "dot-zshrc"), "z\n")
	nestedGitCommand(t, dotfilesDir, "config", "--file", ".gitmodules", "submodule.gone.path", "zsh/dot-oh-my-zsh/plugins/gone")
	nestedGitCommand(t, dotfilesDir, "config", "--file", ".gitmodules", "submodule.gone.url", "https://example.invalid/gone.git")
	nestedGitCommand(t, dotfilesDir, "add", ".")
	nestedGitCommand(t, dotfilesDir, "commit", "-m", "initial")

	model := New(config.Paths{Target: root, Dotfiles: dotfilesDir}, "2.4.1")
	var updated tea.Model = declineGitInit(deliverStagingCmd(t, model, model.Init()))
	updated, _ = updated.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	updated, _ = updated.Update(refreshCmd(config.Paths{Target: root, Dotfiles: dotfilesDir})())
	m := press(t, updated, "4").(Model)
	for i := 0; i < 10; i++ {
		if issue, _ := m.issues.Selected(); issue.State == doctor.Orphaned {
			break
		}
		m = press(t, m, "j").(Model)
	}
	if issue, ok := m.issues.Selected(); !ok || issue.State != doctor.Orphaned || !issue.Fixable {
		t.Fatalf("selected issue = %+v", issue)
	}
	m = press(t, m, "f").(Model)
	if m.popup != popupConfirm {
		t.Fatalf("popup = %v, want the confirmation", m.popup)
	}
	view := ansi.Strip(m.View().Content)
	if !strings.Contains(view, "Fix orphaned zsh") || !strings.Contains(view, "zsh/dot-oh-my-zsh/plugins/gone") {
		t.Fatalf("confirmation:\n%s", view)
	}
	updated, cmd := m.Update(popups.ConfirmedMsg{Action: actionFix})
	m = completeOperation(t, updated.(Model), cmd)
	if _, err := os.Lstat(filepath.Join(dotfilesDir, ".gitmodules")); !os.IsNotExist(err) {
		t.Fatalf(".gitmodules still exists: %v", err)
	}
	updated, _ = m.Update(refreshCmd(m.paths)())
	m = updated.(Model)
	for _, info := range m.pkgs {
		for _, entry := range info.report.Entries {
			if entry.State == doctor.Orphaned {
				t.Fatalf("%s: %+v", info.name, entry)
			}
		}
	}
}
