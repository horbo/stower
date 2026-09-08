package tui

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/horbo/stower/internal/config"
	"github.com/horbo/stower/internal/dotfiles"
	"github.com/horbo/stower/internal/tui/popups"
)

func nestedGitCommand(t *testing.T, dir string, args ...string) {
	t.Helper()
	full := append([]string{"-C", dir, "-c", "user.name=Test", "-c", "user.email=test@example.invalid",
		"-c", "commit.gpgsign=false"}, args...)
	if out, err := exec.Command("git", full...).CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

func newNestedRepositoryModel(t *testing.T) (tea.Model, config.Paths, string) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed")
	}
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

	source := filepath.Join(root, ".config", "repo")
	mkdir(t, source)
	write(t, filepath.Join(source, "tracked"), "x\n")
	nestedGitCommand(t, source, "init")
	nestedGitCommand(t, source, "add", ".")
	nestedGitCommand(t, source, "commit", "-m", "initial")

	paths := config.Paths{Target: root, Dotfiles: dotfilesDir}
	model := New(paths, "2.4.1")
	var updated tea.Model = deliverStagingCmd(t, model, model.Init())
	updated = declineGitInit(updated)
	updated, _ = updated.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	return updated, paths, source
}

func stageNested(t *testing.T, m Model, source string) Model {
	t.Helper()
	return deliverAll(t, m, m.stage(source, "editor")).(Model)
}

func TestRepositoryChoiceRevalidatesPlan(t *testing.T) {
	model, paths, source := newNestedRepositoryModel(t)
	m := stageNested(t, model.(Model), source)
	if len(m.plan.Packages) != 1 || len(m.plan.Packages[0].Repositories) != 1 {
		t.Fatalf("plan: %+v", m.plan)
	}
	if got := m.plan.Packages[0].Repositories[0].Info.Reason; got != "" {
		t.Fatalf("the nested repository is not convertible: %s", got)
	}

	m.focus = Staged
	m.mainFocused = true
	choices := map[string]dotfiles.RepositoryChoice{
		source: {Action: dotfiles.ConvertRepository, URL: ""},
	}
	updated, cmd := m.Update(popups.RepositoriesChosenMsg{Choices: choices})
	m = deliverAll(t, updated, cmd).(Model)

	if m.popup != popupNone {
		t.Fatalf("popup stayed open: %v", m.popup)
	}
	if m.plan.Packages[0].Repositories[0].Choice.Action != dotfiles.ConvertRepository {
		t.Fatal("the choice was not applied to the rebuilt plan")
	}
	view := ansi.Strip(m.View().Content)
	if !strings.Contains(view, "a repository URL is required") {
		t.Fatalf("the plan does not show the conversion error:\n%s", view)
	}
	if m.popup == popupConfirm {
		t.Fatal("the error only appeared through Apply")
	}
	if paths.Dotfiles == "" {
		t.Fatal("paths are not set")
	}
}

func TestRepositoryChoiceConflictAppearsAfterClosingThePopup(t *testing.T) {
	model, paths, source := newNestedRepositoryModel(t)
	m := stageNested(t, model.(Model), source)
	rel := filepath.Join("editor", "dot-config", "repo")
	modules := "[submodule \"" + filepath.ToSlash(rel) + "\"]\n\tpath = " + filepath.ToSlash(rel) +
		"\n\turl = https://example.invalid/repo.git\n"
	write(t, filepath.Join(paths.Dotfiles, ".gitmodules"), modules)
	nestedGitCommand(t, paths.Dotfiles, "add", ".gitmodules")
	nestedGitCommand(t, paths.Dotfiles, "commit", "-m", "modules")

	m.focus = Staged
	m.mainFocused = true
	choices := map[string]dotfiles.RepositoryChoice{
		source: {Action: dotfiles.ConvertRepository, URL: "https://example.invalid/repo.git"},
	}
	updated, cmd := m.Update(popups.RepositoriesChosenMsg{Choices: choices})
	m = deliverAll(t, updated, cmd).(Model)

	if got := m.plan.Packages[0].Repositories[0].Conflict; got == "" {
		t.Fatal("the destination conflict was not detected on the plan path")
	}
	view := ansi.Strip(m.View().Content)
	if !strings.Contains(view, "already registered") {
		t.Fatalf("the plan does not show the conflict:\n%s", view)
	}
}

func TestApplyWarningsAppearInLog(t *testing.T) {
	model, _, source := newNestedRepositoryModel(t)
	m := stageNested(t, model.(Model), source)
	m.mainLog.Start("apply")
	m.mainLog.Finish(dotfiles.Summary{
		Succeeded: []string{"editor"},
		Warnings: []dotfiles.PackageFailure{
			{Package: "editor", Err: errors.New("refusing to remove non-directory Git metadata")},
		},
	}, false)
	lines := strings.Join(m.mainLog.Lines(), "\n")
	if !strings.Contains(lines, "⚠ editor: refusing to remove non-directory Git metadata") {
		t.Fatalf("the warning is missing from the log:\n%s", lines)
	}
	if !strings.Contains(lines, "✔") {
		t.Fatalf("the success line is missing:\n%s", lines)
	}
	if _, err := os.Lstat(source); err != nil {
		t.Fatal(err)
	}
}
