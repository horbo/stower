package tui

import (
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

type editorCall struct {
	argv []string
	path string
}

func editorModel(t *testing.T) Model {
	t.Helper()
	m, _ := resize(t, newTestModel(t), 100, 30)
	return m.(Model)
}

func fakeEditorBinary(t *testing.T, name string) {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, name), []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("VISUAL", "")
	t.Setenv("EDITOR", name+" --wait")
}

func recordEditor(m Model, mutate func()) (Model, *[]editorCall) {
	calls := &[]editorCall{}
	m.execEditor = func(argv []string, path string) tea.Cmd {
		*calls = append(*calls, editorCall{argv: argv, path: path})
		return func() tea.Msg {
			if mutate != nil {
				mutate()
			}
			return editorFinishedMsg{}
		}
	}
	return m, calls
}

func focusHomeEntry(t *testing.T, m Model, name string) Model {
	t.Helper()
	m = press(t, m, "2").(Model)
	for i := 0; i < 20; i++ {
		if node, ok := m.homePanel.Selected(); ok && filepath.Base(node.Path) == name {
			return m
		}
		m = press(t, m, "j").(Model)
	}
	t.Fatalf("%s is not reachable in the Home tree", name)
	return m
}

func exitError(t *testing.T) error {
	t.Helper()
	err := exec.Command("sh", "-c", "exit 3").Run()
	if err == nil {
		t.Fatal("sh -c 'exit 3' unexpectedly succeeded")
	}
	return err
}

func onlyCall(t *testing.T, calls *[]editorCall) editorCall {
	t.Helper()
	if len(*calls) != 1 {
		t.Fatalf("the editor was started %d times, want once", len(*calls))
	}
	return (*calls)[0]
}

func TestOpenInEditorFromHome(t *testing.T) {
	fakeEditorBinary(t, "fake-editor")
	m := focusHomeEntry(t, editorModel(t), ".zshrc")
	m, calls := recordEditor(m, nil)

	if _, cmd := m.Update(keyMsg("o")); cmd == nil {
		t.Fatal("o returned no command")
	}
	call := onlyCall(t, calls)
	if want := []string{"fake-editor", "--wait"}; !reflect.DeepEqual(call.argv, want) {
		t.Fatalf("argv = %#v, want %#v", call.argv, want)
	}
	if want := filepath.Join(m.paths.Target, ".zshrc"); call.path != want {
		t.Fatalf("path = %q, want the target copy %q", call.path, want)
	}
}

func TestOpenInEditorFromPackageContext(t *testing.T) {
	fakeEditorBinary(t, "fake-editor")
	m := focusPackage(t, editorModel(t), "misc")

	withRepo, repoCalls := recordEditor(m, nil)
	if _, cmd := withRepo.Update(keyMsg("o")); cmd == nil {
		t.Fatal("o returned no command")
	}
	if got, want := onlyCall(t, repoCalls).path, filepath.Join(m.paths.Dotfiles, "misc", "dot-bar"); got != want {
		t.Fatalf("o opened %q, want the repository copy %q", got, want)
	}

	withTarget, targetCalls := recordEditor(m, nil)
	if _, cmd := withTarget.Update(keyMsg("O")); cmd == nil {
		t.Fatal("O returned no command")
	}
	if got, want := onlyCall(t, targetCalls).path, filepath.Join(m.paths.Target, ".bar"); got != want {
		t.Fatalf("O opened %q, want the target copy %q", got, want)
	}
}

func TestOpenInEditorFromIssues(t *testing.T) {
	fakeEditorBinary(t, "fake-editor")
	m := press(t, editorModel(t), "4").(Model)
	if _, ok := m.issues.Selected(); !ok {
		t.Fatal("the Issues panel is empty")
	}

	withRepo, repoCalls := recordEditor(m, nil)
	if _, cmd := withRepo.Update(keyMsg("o")); cmd == nil {
		t.Fatal("o returned no command")
	}
	if got, want := onlyCall(t, repoCalls).path, filepath.Join(m.paths.Dotfiles, "misc", "dot-bar"); got != want {
		t.Fatalf("o opened %q, want the repository copy %q", got, want)
	}

	withTarget, targetCalls := recordEditor(m, nil)
	if _, cmd := withTarget.Update(keyMsg("O")); cmd == nil {
		t.Fatal("O returned no command")
	}
	if got, want := onlyCall(t, targetCalls).path, filepath.Join(m.paths.Target, ".bar"); got != want {
		t.Fatalf("O opened %q, want the target copy %q", got, want)
	}
}

func TestOpenInEditorRescansAfterAtomicSave(t *testing.T) {
	fakeEditorBinary(t, "fake-editor")
	m := focusPackage(t, editorModel(t), "zsh")
	link := filepath.Join(m.paths.Target, ".zshrc")

	m, _ = recordEditor(m, func() {
		if err := os.Remove(link); err != nil {
			t.Error(err)
			return
		}
		if err := os.WriteFile(link, []byte("saved by the editor\n"), 0o644); err != nil {
			t.Error(err)
		}
	})

	updated, cmd := m.Update(keyMsg("O"))
	if cmd == nil {
		t.Fatal("O returned no command")
	}
	final := deliverAll(t, updated, cmd).(Model)

	if final.issues.Len() != 2 {
		t.Fatalf("Issues holds %d entries after the editor exited, want 2", final.issues.Len())
	}
	plain := ansi.Strip(final.View().Content)
	if !strings.Contains(plain, "zsh") || !strings.Contains(plain, "replaced") {
		t.Fatalf("zsh is not reported as replaced after the editor exited:\n%s", plain)
	}
}

func TestOpenInEditorWithoutEditor(t *testing.T) {
	m := focusPackage(t, editorModel(t), "misc")
	m, calls := recordEditor(m, nil)

	t.Setenv("PATH", t.TempDir())
	t.Setenv("VISUAL", "")
	t.Setenv("EDITOR", "")

	updated, _ := m.Update(keyMsg("o"))
	if len(*calls) != 0 {
		t.Fatalf("the editor was started %d times without an editor set", len(*calls))
	}
	if flash := updated.(Model).flash; flash != "no editor: set $EDITOR or $VISUAL" {
		t.Fatalf("flash = %q", flash)
	}
}

func TestOpenInEditorFailureShowsExitCode(t *testing.T) {
	updated, _ := editorModel(t).Update(editorFinishedMsg{err: exitError(t)})
	if flash := updated.(Model).flash; !strings.Contains(flash, "exited with code 3") {
		t.Fatalf("flash = %q, want the exit code", flash)
	}
	if updated.(Model).popup != popupNone {
		t.Fatal("a failing editor opened a popup")
	}
}

func TestOpenInEditorIgnoredWhilePopupIsOpen(t *testing.T) {
	fakeEditorBinary(t, "fake-editor")
	m := focusPackage(t, editorModel(t), "misc")
	m, calls := recordEditor(m, nil)

	m = press(t, m, "?").(Model)
	if m.popup != popupKeys {
		t.Fatal("? did not open the keys popup")
	}
	press(t, m, "o")
	if len(*calls) != 0 {
		t.Fatal("o started the editor while a popup was open")
	}
}

func TestOpenInEditorIgnoredWhileOperationRuns(t *testing.T) {
	fakeEditorBinary(t, "fake-editor")
	m := focusPackage(t, editorModel(t), "misc")
	m, calls := recordEditor(m, nil)

	m.exec = &execution{title: "Apply"}
	press(t, m, "o")
	if len(*calls) != 0 {
		t.Fatal("o started the editor while an operation was running")
	}
}

func TestEditorKeysInKeyBar(t *testing.T) {
	m := focusPackage(t, editorModel(t), "misc")
	var help []string
	for _, binding := range m.contextKeys() {
		help = append(help, binding.Help().Key)
	}
	joined := strings.Join(help, " ")
	if !strings.Contains(joined, "o") || !strings.Contains(joined, "O") {
		t.Fatalf("the Package context key bar has no editor keys: %v", help)
	}

	staged := press(t, m, "esc", "3").(Model)
	for _, binding := range staged.contextKeys() {
		if binding.Help().Key == "o" || binding.Help().Key == "O" {
			t.Fatalf("the Staged context advertises %q", binding.Help().Key)
		}
	}
}
