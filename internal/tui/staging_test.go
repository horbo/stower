package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"charm.land/bubbles/v2/spinner"
	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/horbo/stower/internal/config"
)

func newStagingModel(t *testing.T) (tea.Model, config.Paths) {
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
	write(t, filepath.Join(root, ".bar"), "x\n")
	write(t, filepath.Join(root, "dot-foo"), "x\n")
	mkdir(t, filepath.Join(root, ".config", "foo", ".git"))
	write(t, filepath.Join(root, ".config", "foo", "conf"), "y\n")
	mkdir(t, filepath.Join(root, ".config", "taken"))
	write(t, filepath.Join(root, ".config", "taken", "file"), "z\n")
	mkdir(t, filepath.Join(dotfilesDir, "clash", "dot-config", "taken"))

	paths := config.Paths{Target: root, Dotfiles: dotfilesDir}
	model := New(paths, "2.4.1")
	var updated tea.Model = deliverStagingCmd(t, model, model.Init())
	updated = declineGitInit(updated)
	updated, _ = updated.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	return updated, paths
}

func waitFor(cmd tea.Cmd) tea.Msg {
	done := make(chan tea.Msg, 1)
	go func() { done <- cmd() }()
	select {
	case msg := <-done:
		return msg
	case <-time.After(60 * time.Millisecond):
		return nil
	}
}

func send(t *testing.T, model tea.Model, keys ...string) tea.Model {
	t.Helper()
	for _, k := range keys {
		updated, cmd := model.Update(stagingKeyMsg(k))
		model = updated
		model = deliverStagingCmd(t, model, cmd)
	}
	return model
}

func deliverStagingCmd(t *testing.T, model tea.Model, cmd tea.Cmd) tea.Model {
	t.Helper()
	if cmd == nil {
		return model
	}
	msg := waitFor(cmd)
	if msg == nil {
		return model
	}
	if batch, ok := msg.(tea.BatchMsg); ok {
		for _, next := range batch {
			model = deliverStagingCmd(t, model, next)
		}
		return model
	}
	updated, next := model.Update(msg)
	return deliverStagingCmd(t, updated, next)
}

func waitForLong(cmd tea.Cmd) tea.Msg {
	done := make(chan tea.Msg, 1)
	go func() { done <- cmd() }()
	select {
	case msg := <-done:
		return msg
	case <-time.After(2 * time.Second):
		return nil
	}
}

func deliverAll(t *testing.T, model tea.Model, cmd tea.Cmd) tea.Model {
	t.Helper()
	if cmd == nil {
		return model
	}
	msg := waitForLong(cmd)
	if msg == nil {
		return model
	}
	if batch, ok := msg.(tea.BatchMsg); ok {
		for _, next := range batch {
			model = deliverAll(t, model, next)
		}
		return model
	}
	switch msg.(type) {
	case spinner.TickMsg, flashExpiredMsg:
		return model
	}
	updated, next := model.Update(msg)
	return deliverAll(t, updated, next)
}

func stageAndWait(t *testing.T, m Model, path, pkg string) Model {
	t.Helper()
	return deliverStagingCmd(t, m, m.stage(path, pkg)).(Model)
}

func applyAndWait(t *testing.T, m Model) Model {
	t.Helper()
	updated, cmd := m.apply()
	return deliverStagingCmd(t, updated, cmd).(Model)
}

func stagingKeyMsg(k string) tea.KeyPressMsg {
	switch k {
	case "enter":
		return tea.KeyPressMsg{Code: tea.KeyEnter}
	case "esc":
		return tea.KeyPressMsg{Code: tea.KeyEscape}
	case "space":
		return tea.KeyPressMsg{Code: tea.KeySpace, Text: " "}
	case "backspace":
		return tea.KeyPressMsg{Code: tea.KeyBackspace}
	case "right":
		return tea.KeyPressMsg{Code: tea.KeyRight}
	case "left":
		return tea.KeyPressMsg{Code: tea.KeyLeft}
	case "down":
		return tea.KeyPressMsg{Code: tea.KeyDown}
	case "up":
		return tea.KeyPressMsg{Code: tea.KeyUp}
	default:
		return tea.KeyPressMsg{Code: []rune(k)[0], Text: k}
	}
}

func typeName(t *testing.T, model tea.Model, name string) tea.Model {
	t.Helper()
	keys := make([]string, 0, len(name)+1)
	for _, r := range name {
		keys = append(keys, string(r))
	}
	keys = append(keys, "enter")
	return send(t, model, keys...)
}

func cursorPath(t *testing.T, model tea.Model) string {
	t.Helper()
	node, ok := model.(Model).homePanel.Selected()
	if !ok {
		t.Fatal("the home tree has no selection")
	}
	return node.Path
}

func moveTo(t *testing.T, model tea.Model, path string) tea.Model {
	t.Helper()
	model = send(t, model, "g")
	for i := 0; i < 60; i++ {
		if cursorPath(t, model) == path {
			return model
		}
		model = send(t, model, "j")
	}
	t.Fatalf("%s is not reachable in the home tree", path)
	return model
}

func TestStageFileAndDirectory(t *testing.T) {
	model, paths := newStagingModel(t)
	model = send(t, model, "2")

	model = moveTo(t, model, filepath.Join(paths.Target, ".bar"))
	model = send(t, model, "space")
	model = typeName(t, model, "misc")

	model = moveTo(t, model, filepath.Join(paths.Target, ".config"))
	model = send(t, model, "right")
	model = moveTo(t, model, filepath.Join(paths.Target, ".config", "foo"))
	model = send(t, model, "space")
	model = typeName(t, model, "foo")

	staging := model.(Model).staging
	if got := staging[filepath.Join(paths.Target, ".bar")]; got != "misc" {
		t.Fatalf(".bar is staged into %q, want misc", got)
	}
	if got := staging[filepath.Join(paths.Target, ".config", "foo")]; got != "foo" {
		t.Fatalf(".config/foo is staged into %q, want foo", got)
	}

	plan := model.(Model).plan
	want := map[string]string{
		filepath.Join(paths.Target, ".bar"):           filepath.Join(paths.Dotfiles, "misc", "dot-bar"),
		filepath.Join(paths.Target, ".config", "foo"): filepath.Join(paths.Dotfiles, "foo", "dot-config", "foo"),
	}
	for _, pkg := range plan.Packages {
		for _, move := range pkg.Moves {
			if want[move.From] != move.To {
				t.Fatalf("move %s → %s, want %s", move.From, move.To, want[move.From])
			}
			delete(want, move.From)
		}
	}
	if len(want) != 0 {
		t.Fatalf("the plan is missing moves for %v", want)
	}

	model = send(t, model, "3")
	plain := ansi.Strip(model.View().Content)
	for _, text := range []string{"misc/", "foo/", "~/.bar", "~/.config/foo", "2 items · 2 packages", "2 staged"} {
		if !strings.Contains(plain, text) {
			t.Fatalf("the staged view does not contain %q:\n%s", text, plain)
		}
	}
}

func TestStagingADescendantIsRefused(t *testing.T) {
	model, paths := newStagingModel(t)
	model = send(t, model, "2")
	model = moveTo(t, model, filepath.Join(paths.Target, ".config"))
	model = send(t, model, "right")
	model = moveTo(t, model, filepath.Join(paths.Target, ".config", "foo"))
	model = send(t, model, "space")
	model = typeName(t, model, "foo")

	model = send(t, model, "right")
	model = moveTo(t, model, filepath.Join(paths.Target, ".config", "foo", "conf"))
	model = send(t, model, "space")
	model = typeName(t, model, "other")

	if _, ok := model.(Model).staging[filepath.Join(paths.Target, ".config", "foo", "conf")]; ok {
		t.Fatal("a descendant of a staged directory was staged")
	}
	if flash := model.(Model).flash; !strings.Contains(flash, "already covered by") {
		t.Fatalf("flash = %q, want a message about the covering directory", flash)
	}
}

func TestStagingADirectoryDropsStagedDescendants(t *testing.T) {
	model, paths := newStagingModel(t)
	model = send(t, model, "2")
	model = moveTo(t, model, filepath.Join(paths.Target, ".config"))
	model = send(t, model, "right")
	model = moveTo(t, model, filepath.Join(paths.Target, ".config", "foo"))
	model = send(t, model, "space")
	model = typeName(t, model, "foo")

	model = moveTo(t, model, filepath.Join(paths.Target, ".config"))
	model = send(t, model, "space")
	model = typeName(t, model, "cfg")

	staging := model.(Model).staging
	if len(staging) != 1 {
		t.Fatalf("staging = %v, want only the parent directory", staging)
	}
	if got := staging[filepath.Join(paths.Target, ".config")]; got != "cfg" {
		t.Fatalf(".config is staged into %q, want cfg", got)
	}
	if flash := model.(Model).flash; !strings.Contains(flash, "dropped") {
		t.Fatalf("flash = %q, want a message about the dropped descendants", flash)
	}
}

func TestManagedEntriesCannotBeStagedOrExpanded(t *testing.T) {
	model, paths := newStagingModel(t)
	model = send(t, model, "2")
	model = moveTo(t, model, filepath.Join(paths.Target, ".zshrc"))

	node, _ := model.(Model).homePanel.Selected()
	if node.Selectable || node.Expandable {
		t.Fatalf("the managed entry is selectable=%v expandable=%v", node.Selectable, node.Expandable)
	}
	before := model.(Model).homePanel.Counter()
	model = send(t, model, "space", "right")
	if model.(Model).popup != popupNone {
		t.Fatal("space opened the assign popup for a managed entry")
	}
	if len(model.(Model).staging) != 0 {
		t.Fatal("a managed entry was staged")
	}
	if after := model.(Model).homePanel.Counter(); after != before {
		t.Fatalf("the managed entry was expanded: %s → %s", before, after)
	}
	if !strings.Contains(ansi.Strip(model.View().Content), "[zsh]") {
		t.Fatal("the managed badge is missing from the home tree")
	}
}

func TestNestedGitWarningAndToggle(t *testing.T) {
	model, paths := newStagingModel(t)
	model = send(t, model, "2")
	model = moveTo(t, model, filepath.Join(paths.Target, ".config"))
	model = send(t, model, "right")
	model = moveTo(t, model, filepath.Join(paths.Target, ".config", "foo"))
	model = send(t, model, "space")
	model = typeName(t, model, "foo")

	model = send(t, model, "3")
	plain := ansi.Strip(model.View().Content)
	if !strings.Contains(plain, "nested .git") {
		t.Fatalf("the plan has no nested .git warning:\n%s", plain)
	}
	if !strings.Contains(plain, "[ ] remove .git after move") {
		t.Fatalf("the plan has no toggle:\n%s", plain)
	}

	model = send(t, model, "x")
	if !model.(Model).removeGit["foo"] {
		t.Fatal("x did not toggle remove .git for foo")
	}
	if !strings.Contains(ansi.Strip(model.View().Content), "[x] remove .git after move") {
		t.Fatal("the toggle is not shown as enabled")
	}
	model = send(t, model, "x")
	if model.(Model).removeGit["foo"] {
		t.Fatal("x did not toggle remove .git back off")
	}
}

func TestBlockedWhenTheDestinationExists(t *testing.T) {
	model, paths := newStagingModel(t)
	model = send(t, model, "2")
	model = moveTo(t, model, filepath.Join(paths.Target, ".config"))
	model = send(t, model, "right")
	model = moveTo(t, model, filepath.Join(paths.Target, ".config", "taken"))
	model = send(t, model, "space")
	model = typeName(t, model, "clash")

	if blocked := model.(Model).plan.BlockedCount(); blocked != 1 {
		t.Fatalf("the plan has %d blocked entries, want 1", blocked)
	}
	model = send(t, model, "3")
	plain := ansi.Strip(model.View().Content)
	if !strings.Contains(plain, "destination already exists") {
		t.Fatalf("the blocked reason is missing from the plan:\n%s", plain)
	}
}

func TestAssignRejectsAnInvalidPackageName(t *testing.T) {
	model, paths := newStagingModel(t)
	model = send(t, model, "2")
	model = moveTo(t, model, filepath.Join(paths.Target, ".bar"))
	model = send(t, model, "space")
	model = typeName(t, model, ".hidden")

	if model.(Model).popup != popupAssign {
		t.Fatal("an invalid package name closed the assign popup")
	}
	if !strings.Contains(ansi.Strip(model.View().Content), "must not start with a dot") {
		t.Fatalf("the assign popup shows no validation error:\n%s", ansi.Strip(model.View().Content))
	}
	model = send(t, model, "esc")
	if len(model.(Model).staging) != 0 {
		t.Fatal("the entry was staged despite the invalid name")
	}
}

func TestMainPanelFollowsTheFocusedSidePanel(t *testing.T) {
	model, paths := newStagingModel(t)
	model = send(t, model, "2")
	model = moveTo(t, model, filepath.Join(paths.Target, ".bar"))
	if title := model.(Model).mainPanel().Title(); title != "Home: ~/.bar" {
		t.Fatalf("with Home focused the main title is %q", title)
	}
	model = send(t, model, "3")
	if title := model.(Model).mainPanel().Title(); title != "Staged plan" {
		t.Fatalf("with Staged focused the main title is %q", title)
	}
	model = send(t, model, "1")
	if title := model.(Model).mainPanel().Title(); !strings.HasPrefix(title, "Package") {
		t.Fatalf("with Packages focused the main title is %q", title)
	}
}

func TestHomeFilterCapturesKeys(t *testing.T) {
	model, _ := newStagingModel(t)
	model = send(t, model, "2", "/", "c", "o", "n")
	if !model.(Model).homePanel.CapturesInput() {
		t.Fatal("/ did not start the filter")
	}
	plain := ansi.Strip(model.View().Content)
	if !strings.Contains(plain, ".config/") {
		t.Fatalf("the filtered tree does not show .config:\n%s", plain)
	}
	model = send(t, model, "esc")
	if model.(Model).homePanel.CapturesInput() {
		t.Fatal("esc did not leave the filter")
	}
}

func TestStagedPanelUnstageAndRename(t *testing.T) {
	model, paths := newStagingModel(t)
	model = send(t, model, "2")
	model = moveTo(t, model, filepath.Join(paths.Target, ".bar"))
	model = send(t, model, "space")
	model = typeName(t, model, "misc")

	model = send(t, model, "3", "e")
	if model.(Model).popup != popupAssign {
		t.Fatal("e did not open the rename popup")
	}
	model = send(t, model, "backspace", "backspace", "backspace", "backspace")
	model = typeName(t, model, "tools")
	if got := model.(Model).staging[filepath.Join(paths.Target, ".bar")]; got != "tools" {
		t.Fatalf("after the rename the entry is in %q, want tools", got)
	}
	if model.(Model).renaming != "" {
		t.Fatal("the rename state was not cleared")
	}

	model = send(t, model, "j", "u")
	if len(model.(Model).staging) != 0 {
		t.Fatalf("u did not unstage the entry: %v", model.(Model).staging)
	}
	if !strings.Contains(ansi.Strip(model.View().Content), "(nothing staged)") {
		t.Fatal("the staged panel does not show the empty state")
	}
}

func TestCancellingARenameDoesNotAffectTheNextAssignment(t *testing.T) {
	model, paths := newStagingModel(t)
	model = send(t, model, "2")
	model = moveTo(t, model, filepath.Join(paths.Target, ".bar"))
	model = send(t, model, "space")
	model = typeName(t, model, "misc")

	model = send(t, model, "3", "e", "esc")
	if model.(Model).renaming != "" {
		t.Fatal("esc did not clear the rename state")
	}

	model = send(t, model, "2")
	model = moveTo(t, model, filepath.Join(paths.Target, ".config"))
	model = send(t, model, "space")
	model = typeName(t, model, "cfg")
	staging := model.(Model).staging
	if got := staging[filepath.Join(paths.Target, ".config")]; got != "cfg" {
		t.Fatalf(".config is staged into %q, want cfg", got)
	}
	if got := staging[filepath.Join(paths.Target, ".bar")]; got != "misc" {
		t.Fatalf(".bar changed package to %q", got)
	}
}

func TestStagedAndHomeViewsFitEverySize(t *testing.T) {
	model, paths := newStagingModel(t)
	model = send(t, model, "2")
	model = moveTo(t, model, filepath.Join(paths.Target, ".config"))
	model = send(t, model, "right")
	model = moveTo(t, model, filepath.Join(paths.Target, ".config", "foo"))
	model = send(t, model, "space")
	model = typeName(t, model, "foo")

	sizes := [][2]int{{60, 16}, {70, 18}, {80, 24}, {100, 30}, {200, 50}}
	for _, focus := range []string{"2", "3"} {
		model = send(t, model, focus)
		for _, size := range sizes {
			updated, view := resize(t, model, size[0], size[1])
			assertScreen(t, view, size[0], size[1])
			model = updated
			for _, mode := range []string{"+", "+"} {
				model = send(t, model, mode)
				assertScreen(t, model.View().Content, size[0], size[1])
			}
			model = send(t, model, "+")
		}
	}
}

func TestAssignPopupFitsTheScreen(t *testing.T) {
	model, paths := newStagingModel(t)
	model = send(t, model, "2")
	model = moveTo(t, model, filepath.Join(paths.Target, ".bar"))
	model = send(t, model, "space")
	if model.(Model).popup != popupAssign {
		t.Fatal("space did not open the assign popup")
	}
	for _, size := range [][2]int{{60, 16}, {70, 18}, {100, 30}} {
		updated, view := resize(t, model, size[0], size[1])
		assertScreen(t, view, size[0], size[1])
		plain := ansi.Strip(view)
		for _, want := range []string{"Assign to a package", "new package:", "clash", "zsh", "esc cancel"} {
			if !strings.Contains(plain, want) {
				t.Fatalf("%dx%d assign popup does not contain %q:\n%s", size[0], size[1], want, plain)
			}
		}
		model = updated
	}
}

func TestStagingADotPrefixedEntryIsRejected(t *testing.T) {
	model, paths := newStagingModel(t)
	model = send(t, model, "2")
	model = moveTo(t, model, filepath.Join(paths.Target, "dot-foo"))
	model = send(t, model, "space")

	if model.(Model).popup == popupAssign {
		t.Fatal("the assign popup opened for a dot- prefixed entry")
	}
	if len(model.(Model).staging) != 0 {
		t.Fatal("a dot- prefixed entry was staged")
	}
	if flash := model.(Model).flash; !strings.Contains(flash, "dot-") {
		t.Fatalf("flash = %q, want the dot- rejection", flash)
	}
	if !strings.Contains(ansi.Strip(model.View().Content), "dot-") {
		t.Fatal("the rejection is not visible in the key bar")
	}
}
