package tui

import (
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
	"github.com/horbo/stower/internal/gitx"
	"github.com/horbo/stower/internal/tui/panels"
	"github.com/horbo/stower/internal/tui/popups"
	"github.com/horbo/stower/internal/tui/styles"
)

func requireGit(t *testing.T) {
	t.Helper()
	if !gitx.Available() {
		t.Skip("git is not installed")
	}
}

func requireStow(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("stow"); err != nil {
		t.Skip("stow is not installed")
	}
}

func isolateGit(t *testing.T) {
	t.Helper()
	for _, variable := range [][2]string{
		{"GIT_CONFIG_GLOBAL", os.DevNull},
		{"GIT_CONFIG_SYSTEM", os.DevNull},
		{"GIT_AUTHOR_NAME", "stower test"},
		{"GIT_AUTHOR_EMAIL", "test@example.invalid"},
		{"GIT_COMMITTER_NAME", "stower test"},
		{"GIT_COMMITTER_EMAIL", "test@example.invalid"},
	} {
		t.Setenv(variable[0], variable[1])
	}
}

func configureRepo(t *testing.T, dir string) {
	t.Helper()
	for _, setting := range [][2]string{
		{"user.name", "stower test"},
		{"user.email", "test@example.invalid"},
		{"commit.gpgsign", "false"},
	} {
		if out, err := exec.Command("git", "-C", dir, "config", setting[0], setting[1]).CombinedOutput(); err != nil {
			t.Fatalf("git config %s: %v: %s", setting[0], err, out)
		}
	}
}

func git(t *testing.T, dir string, args ...string) string {
	t.Helper()
	out, err := exec.Command("git", append([]string{"-C", dir}, args...)...).CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v: %s", strings.Join(args, " "), err, out)
	}
	return string(out)
}

func step(model tea.Model, cmd tea.Cmd) tea.Model {
	if cmd == nil {
		return model
	}
	done := make(chan tea.Msg, 1)
	go func() { done <- cmd() }()
	var msg tea.Msg
	select {
	case msg = <-done:
	case <-time.After(2 * time.Second):
		return model
	}
	if batch, ok := msg.(tea.BatchMsg); ok {
		for _, one := range batch {
			model = step(model, one)
		}
		return model
	}
	if msg == nil {
		return model
	}
	updated, next := model.Update(msg)
	return step(updated, next)
}

func finishOperation(t *testing.T, m Model, cmd tea.Cmd) Model {
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
				return step(m, next).(Model)
			}
			go func() { messages <- next() }()
		case <-time.After(10 * time.Second):
			t.Fatal("operation did not finish")
		}
	}
}

func newFirstRunModel(t *testing.T) (Model, config.Paths) {
	t.Helper()
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", root)
	write(t, filepath.Join(root, ".bar"), "x\n")
	paths := config.Paths{Target: root, Dotfiles: filepath.Join(root, "dotfiles")}
	m := New(paths, "2.4.1")
	if m.popup != popupFirstRun {
		t.Fatal("the first run popup is not shown for a missing dotfiles directory")
	}
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	return updated.(Model), paths
}

func TestFirstRunAdoptsAndCommits(t *testing.T) {
	requireGit(t)
	requireStow(t)
	m, paths := newFirstRunModel(t)

	view := ansi.Strip(m.View().Content)
	for _, want := range []string{"First run", "does not exist", "[x] create", "[x] git init", "[x] add .gitignore"} {
		if !strings.Contains(view, want) {
			t.Fatalf("first run popup does not contain %q:\n%s", want, view)
		}
	}
	for _, size := range [][2]int{{60, 16}, {70, 18}, {100, 30}} {
		updated, view := resize(t, m, size[0], size[1])
		assertScreen(t, view, size[0], size[1])
		m = updated.(Model)
	}
	m = press(t, m, "j", "j", "space").(Model)
	if strings.Contains(ansi.Strip(m.View().Content), "[x] add .gitignore") {
		t.Fatal("space did not clear the .gitignore checkbox")
	}

	updated, cmd := m.Update(keyMsg("enter"))
	m = step(updated, cmd).(Model)
	if m.popup != popupNone {
		t.Fatal("the first run popup stayed open")
	}
	if !gitx.IsRepo(paths.Dotfiles) {
		t.Fatal("the dotfiles directory was not initialised")
	}
	if _, err := os.Stat(filepath.Join(paths.Dotfiles, ".gitignore")); !os.IsNotExist(err) {
		t.Fatalf(".gitignore was created although the checkbox was cleared: %v", err)
	}
	configureRepo(t, paths.Dotfiles)

	m = stageAndWait(t, m, filepath.Join(paths.Target, ".bar"), "misc")
	m.focus = Staged
	m = applyAndWait(t, m)
	if m.popup != popupConfirm {
		t.Fatal("apply did not ask for confirmation")
	}
	updated, cmd = m.Update(popups.ConfirmedMsg{Action: actionApply})
	m = finishOperation(t, updated.(Model), cmd)

	if m.popup != popupCommit {
		t.Fatalf("the commit popup is not open after a successful apply: popup=%d", m.popup)
	}
	view = ansi.Strip(m.View().Content)
	for _, want := range []string{"Commit", "?? misc/dot-bar", "stower: add misc (1 file)"} {
		if !strings.Contains(view, want) {
			t.Fatalf("commit popup does not contain %q:\n%s", want, view)
		}
	}

	updated, cmd = m.Update(keyMsg("enter"))
	m = step(updated, cmd).(Model)
	if m.popup != popupNone {
		t.Fatal("the commit popup stayed open")
	}

	log := strings.TrimSpace(git(t, paths.Dotfiles, "log", "--oneline"))
	if lines := strings.Split(log, "\n"); len(lines) != 1 {
		t.Fatalf("expected exactly one commit, got:\n%s", log)
	}
	if subject := strings.TrimSpace(git(t, paths.Dotfiles, "log", "--format=%s")); subject != "stower: add misc (1 file)" {
		t.Fatalf("commit subject = %q", subject)
	}
	if status := git(t, paths.Dotfiles, "status", "--short"); strings.TrimSpace(status) != "" {
		t.Fatalf("git status is not clean:\n%s", status)
	}
	if summary := m.status.GitSummary(); summary != "git clean" {
		t.Fatalf("status git summary = %q", summary)
	}
	t.Logf("Status: %s", ansi.Strip(m.status.View()))
	t.Logf("Packages:\n%s", ansi.Strip(m.packages.View()))
}

func TestFirstRunGitIgnoreAndQuit(t *testing.T) {
	requireGit(t)
	m, paths := newFirstRunModel(t)
	updated, cmd := m.Update(keyMsg("enter"))
	m = step(updated, cmd).(Model)
	data, err := os.ReadFile(filepath.Join(paths.Dotfiles, ".gitignore"))
	if err != nil || string(data) != ".DS_Store\n" {
		t.Fatalf(".gitignore = %q, %v", data, err)
	}
	if !gitx.IsRepo(paths.Dotfiles) {
		t.Fatal("git init was not run")
	}

	other, _ := newFirstRunModel(t)
	updated, cmd = other.Update(keyMsg("esc"))
	other = updated.(Model)
	quit, next := other.Update(cmd())
	other = quit.(Model)
	if next == nil {
		t.Fatal("esc did not quit the first run popup")
	}
	if _, ok := next().(tea.QuitMsg); !ok {
		t.Fatal("esc did not produce a quit message")
	}
	if _, err := os.Stat(other.paths.Dotfiles); !os.IsNotExist(err) {
		t.Fatalf("esc created the dotfiles directory: %v", err)
	}
}

func TestFirstRunCommitsTheCreatedFiles(t *testing.T) {
	requireGit(t)
	isolateGit(t)
	m, paths := newFirstRunModel(t)

	updated, cmd := m.Update(keyMsg("enter"))
	m = step(updated, cmd).(Model)
	if !gitx.IsRepo(paths.Dotfiles) {
		t.Fatal("the dotfiles directory was not initialised")
	}
	if subject := strings.TrimSpace(git(t, paths.Dotfiles, "log", "--format=%s")); subject != "stower: init" {
		t.Fatalf("initial commit subject = %q", subject)
	}
	if status := git(t, paths.Dotfiles, "status", "--short"); strings.TrimSpace(status) != "" {
		t.Fatalf("git status is not clean after the first run:\n%s", status)
	}
	if summary := m.status.GitSummary(); summary != "git clean" {
		t.Fatalf("status git summary = %q", summary)
	}
	if m.flash != "" {
		t.Fatalf("first run reported %q", m.flash)
	}
}

func TestDeclinedGitInitKeepsStowerWorking(t *testing.T) {
	requireGit(t)
	requireStow(t)
	m := prepareApply(t)
	if !m.gitDeclined || m.git.repo {
		t.Fatal("the git init offer was not declined")
	}
	if summary := m.status.GitSummary(); summary != "no git" {
		t.Fatalf("status git summary = %q", summary)
	}
	m = runApply(t, m)
	if m.popup != popupNone {
		t.Fatalf("a commit popup was offered without a repository: popup=%d", m.popup)
	}
	if !strings.Contains(ansi.Strip(m.View().Content), "no git") {
		t.Fatalf("status does not report the missing repository:\n%s", ansi.Strip(m.View().Content))
	}
	m = press(t, m, "0").(Model)
	updated, cmd := m.Update(keyMsg("c"))
	m = step(updated, cmd).(Model)
	if m.popup != popupNone {
		t.Fatal("c opened a commit popup without a repository")
	}
	if !strings.Contains(m.flash, "not a git repository") {
		t.Fatalf("flash = %q", m.flash)
	}
}

func newRepositoryModel(t *testing.T) (Model, config.Paths) {
	t.Helper()
	requireGit(t)
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", root)
	dotfilesDir := filepath.Join(root, "dotfiles")
	mkdir(t, filepath.Join(dotfilesDir, "zsh"))
	write(t, filepath.Join(dotfilesDir, "zsh", "dot-zshrc"), "a\n")
	if err := os.Symlink(filepath.Join("dotfiles", "zsh", "dot-zshrc"), filepath.Join(root, ".zshrc")); err != nil {
		t.Fatal(err)
	}
	if err := gitx.Init(dotfilesDir); err != nil {
		t.Fatal(err)
	}
	configureRepo(t, dotfilesDir)

	paths := config.Paths{Target: root, Dotfiles: dotfilesDir}
	model := New(paths, "2.4.1")
	if model.popup != popupNone {
		t.Fatal("a repository must not trigger the first run popup")
	}
	updated := deliverStagingCmd(t, model, model.Init())
	updated, _ = updated.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	return updated.(Model), paths
}

func TestGitMarkersAndManualCommit(t *testing.T) {
	m, paths := newRepositoryModel(t)
	if summary := m.status.GitSummary(); summary != "git 1*" {
		t.Fatalf("status git summary = %q", summary)
	}
	status := ansi.Strip(m.status.View())
	packages := ansi.Strip(m.packages.View())
	t.Logf("Status: %s", status)
	t.Logf("Packages: %s", packages)
	if !strings.Contains(status, "git 1*") {
		t.Fatalf("status line does not show the git summary: %s", status)
	}
	if !strings.Contains(packages, "zsh *") {
		t.Fatalf("packages panel does not mark the dirty package: %s", packages)
	}

	updated, cmd := m.Update(keyMsg("c"))
	m = step(updated, cmd).(Model)
	if m.popup != popupCommit {
		t.Fatalf("c did not open the commit popup: popup=%d", m.popup)
	}
	view := ansi.Strip(m.View().Content)
	for _, want := range []string{"?? zsh/dot-zshrc", "stower: update zsh (1 file)"} {
		if !strings.Contains(view, want) {
			t.Fatalf("commit popup does not contain %q:\n%s", want, view)
		}
	}
	for _, size := range [][2]int{{60, 16}, {70, 18}, {100, 30}} {
		updated, rendered := resize(t, m, size[0], size[1])
		assertScreen(t, rendered, size[0], size[1])
		m = updated.(Model)
	}

	updated, cmd = m.Update(keyMsg("s"))
	m = step(updated, cmd).(Model)
	if m.popup != popupNone || m.flash != "commit skipped" {
		t.Fatalf("s did not skip the commit: popup=%d flash=%q", m.popup, m.flash)
	}
	if lines, err := gitx.Porcelain(paths.Dotfiles); err != nil || len(lines) != 1 {
		t.Fatalf("skip committed anyway: %v %v", lines, err)
	}

	m = press(t, m, "1").(Model)
	updated, cmd = m.Update(keyMsg("c"))
	m = step(updated, cmd).(Model)
	if m.popup != popupCommit {
		t.Fatal("c does not work from the Packages panel")
	}
	updated, cmd = m.Update(keyMsg("esc"))
	m = step(updated, cmd).(Model)
	if m.popup != popupNone {
		t.Fatal("esc did not close the commit popup")
	}

	updated, cmd = m.Update(keyMsg("c"))
	m = step(updated, cmd).(Model)
	m = press(t, m, "e").(Model)
	m = press(t, m, "!").(Model)
	updated, cmd = m.Update(keyMsg("enter"))
	m = step(updated, cmd).(Model)
	if subject := strings.TrimSpace(git(t, paths.Dotfiles, "log", "--format=%s")); subject != "stower: update zsh (1 file)!" {
		t.Fatalf("edited commit subject = %q", subject)
	}
	if summary := m.status.GitSummary(); summary != "git clean" {
		t.Fatalf("status git summary after the commit = %q", summary)
	}
	if strings.Contains(ansi.Strip(m.packages.View()), "zsh *") {
		t.Fatalf("the dirty marker survived the commit: %s", ansi.Strip(m.packages.View()))
	}
}

func TestStatusAndPackagesGitLines(t *testing.T) {
	st := styles.Default()
	paths := config.Paths{Target: "/home/kamil", Dotfiles: "/home/kamil/dotfiles"}
	status := panels.NewStatus(paths, "/home/kamil", "2.4.1", st)
	status.SetSize(60, 1)
	for _, tt := range []struct {
		repo  bool
		dirty int
		want  string
	}{
		{true, 1, "~/dotfiles → ~  stow 2.4.1  git 1*"},
		{true, 0, "~/dotfiles → ~  stow 2.4.1  git clean"},
		{false, 0, "~/dotfiles → ~  stow 2.4.1  no git"},
	} {
		status.SetGit(tt.repo, tt.dirty)
		got := ansi.Strip(status.View())
		if got != tt.want {
			t.Errorf("status line = %q, want %q", got, tt.want)
		}
		t.Logf("Status: %s", got)
	}

	packages := panels.NewPackages(st)
	packages.SetSize(24, 3)
	packages.SetPackages([]panels.Package{
		{Name: "claude", Dirty: true},
		{Name: "nvim", Linked: true},
		{Name: "zsh", Linked: true, Dirty: true},
	})
	for _, line := range strings.Split(ansi.Strip(packages.View()), "\n") {
		t.Logf("Packages: %s", strings.TrimRight(line, " "))
	}
	lines := strings.Split(ansi.Strip(packages.View()), "\n")
	for i, want := range []string{"✘ claude *", "✔ nvim", "✔ zsh *"} {
		if strings.TrimRight(lines[i], " ") != want {
			t.Errorf("packages line %d = %q, want %q", i, lines[i], want)
		}
	}
}

func TestCommitChangeSubjects(t *testing.T) {
	m := Model{fixIssue: doctor.Issue{Package: "claude", Entry: dotfiles.Entry{PkgRel: "dot-claude/settings.json"}}}
	tests := []struct {
		title     string
		succeeded []string
		want      string
	}{
		{actionApply, []string{"zsh"}, "stower: add zsh (3 files)"},
		{actionApply, []string{"git", "ghostty"}, "stower: add git, ghostty (3 files)"},
		{actionRestore, []string{"zsh"}, "stower: remove zsh"},
		{actionFix, []string{"claude"}, "stower: fix claude/settings.json"},
		{actionRestow, []string{"zsh", "nvim"}, "stower: restow"},
	}
	for _, tt := range tests {
		change, ok := m.commitChange(tt.title, tt.succeeded)
		if !ok {
			t.Fatalf("%s: no change", tt.title)
		}
		change.Files = 3
		if got := gitx.Subject(change); got != tt.want {
			t.Errorf("%s: subject = %q, want %q", tt.title, got, tt.want)
		}
	}
	if _, ok := m.commitChange("unknown", []string{"zsh"}); ok {
		t.Fatal("an unknown operation produced a commit")
	}
}

func TestEntryRestoreCommitSubjects(t *testing.T) {
	m := Model{restorePlan: dotfiles.RestorePlan{Package: "zsh"}}
	tests := []struct {
		entries []string
		want    string
	}{
		{nil, "stower: remove zsh"},
		{[]string{"dot-zshrc"}, "stower: remove zsh/dot-zshrc"},
		{[]string{"dot-zshrc", "dot-zprofile"}, "stower: remove 2 entries from zsh"},
	}
	for _, tt := range tests {
		m.restoreEntries = tt.entries
		change, ok := m.commitChange(actionRestore, []string{"zsh"})
		if !ok {
			t.Fatalf("%v: no change", tt.entries)
		}
		if got := gitx.Subject(change); got != tt.want {
			t.Errorf("%v: subject = %q, want %q", tt.entries, got, tt.want)
		}
	}
}

func TestCommitAfterRestowAndRestore(t *testing.T) {
	requireStow(t)
	m, paths := newRepositoryModel(t)
	if err := gitx.AddAndCommit(paths.Dotfiles, []string{"zsh"}, "stower: add zsh (1 file)"); err != nil {
		t.Fatal(err)
	}
	updated, _ := m.Update(refreshCmd(paths)())
	m = updated.(Model)

	m = press(t, m, "0").(Model)
	m = press(t, m, "R").(Model)
	if m.popup != popupConfirm {
		t.Fatal("R did not ask for confirmation")
	}
	updated, cmd := m.Update(popups.ConfirmedMsg{Action: actionRestow})
	m = finishOperation(t, updated.(Model), cmd)
	if m.popup != popupNone {
		t.Fatal("a commit popup was offered although nothing changed")
	}

	m = press(t, m, "1").(Model)
	m = press(t, m, "r").(Model)
	if !m.restoreOpen {
		t.Fatal("r did not open the restore plan")
	}
	updated, cmd = m.Update(popups.ConfirmedMsg{Action: actionRestore})
	m = finishOperation(t, updated.(Model), cmd)
	if m.popup != popupCommit {
		t.Fatalf("no commit popup after a restore: popup=%d", m.popup)
	}
	if !strings.Contains(ansi.Strip(m.View().Content), "stower: remove zsh") {
		t.Fatalf("restore subject missing:\n%s", ansi.Strip(m.View().Content))
	}
	updated, cmd = m.Update(keyMsg("enter"))
	m = step(updated, cmd).(Model)
	if subject := strings.Split(strings.TrimSpace(git(t, paths.Dotfiles, "log", "--format=%s")), "\n")[0]; subject != "stower: remove zsh" {
		t.Fatalf("commit subject = %q", subject)
	}
	if status := git(t, paths.Dotfiles, "status", "--short"); strings.TrimSpace(status) != "" {
		t.Fatalf("git status is not clean:\n%s", status)
	}
}
