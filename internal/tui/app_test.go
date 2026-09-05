package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/horbo/stower/internal/config"
)

func newTestModel(t *testing.T) tea.Model {
	t.Helper()
	root := t.TempDir()
	dotfilesDir := filepath.Join(root, "dotfiles")
	mkdir(t, filepath.Join(dotfilesDir, "zsh"))
	mkdir(t, filepath.Join(dotfilesDir, "misc"))
	write(t, filepath.Join(dotfilesDir, "zsh", "dot-zshrc"), "a")
	write(t, filepath.Join(dotfilesDir, "misc", "dot-bar"), "b")
	if err := os.Symlink(filepath.Join(dotfilesDir, "zsh", "dot-zshrc"), filepath.Join(root, ".zshrc")); err != nil {
		t.Fatal(err)
	}

	model := New(config.Paths{Target: root, Dotfiles: dotfilesDir}, "2.4.1")
	updated, _ := model.Update(model.Init()())
	return updated
}

func mkdir(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatal(err)
	}
}

func write(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func resize(t *testing.T, model tea.Model, w, h int) (tea.Model, string) {
	t.Helper()
	updated, _ := model.Update(tea.WindowSizeMsg{Width: w, Height: h})
	return updated, updated.View().Content
}

func press(t *testing.T, model tea.Model, keys ...string) tea.Model {
	t.Helper()
	for _, k := range keys {
		model, _ = model.Update(keyMsg(k))
	}
	return model
}

func keyMsg(k string) tea.KeyPressMsg {
	switch k {
	case "enter":
		return tea.KeyPressMsg{Code: tea.KeyEnter}
	case "esc":
		return tea.KeyPressMsg{Code: tea.KeyEscape}
	case "tab":
		return tea.KeyPressMsg{Code: tea.KeyTab}
	default:
		return tea.KeyPressMsg{Code: []rune(k)[0], Text: k}
	}
}

func assertScreen(t *testing.T, view string, w, h int) {
	t.Helper()
	lines := strings.Split(view, "\n")
	if len(lines) != h {
		t.Fatalf("%dx%d: view has %d lines", w, h, len(lines))
	}
	for i, line := range lines {
		if got := ansi.StringWidth(line); got > w {
			t.Fatalf("%dx%d: line %d is %d columns wide: %q", w, h, i, got, ansi.Strip(line))
		}
	}
}

func TestViewLandscape(t *testing.T) {
	model, view := resize(t, newTestModel(t), 100, 30)
	assertScreen(t, view, 100, 30)
	plain := ansi.Strip(view)
	for _, want := range []string{"[0] Status", "[1] Packages", "[2] Home", "[3] Staged", "[4] Issues",
		"✔ zsh", "✘ misc", "Package: misc", "dot-bar", "missing", "1 issue"} {
		if !strings.Contains(plain, want) {
			t.Fatalf("landscape view does not contain %q:\n%s", want, plain)
		}
	}
	_ = model
}

func TestViewPortrait(t *testing.T) {
	model, view := resize(t, newTestModel(t), 70, 18)
	assertScreen(t, view, 70, 18)
	plain := ansi.Strip(view)
	if !strings.Contains(plain, "[0]Status [1]Packages [2]Home [3]Staged [4]Issues") {
		t.Fatalf("portrait view has no tab strip:\n%s", plain)
	}
	if strings.Contains(plain, "[3] Staged") {
		t.Fatalf("portrait view must show only the focused side panel:\n%s", plain)
	}
	_ = model
}

func TestViewTooSmall(t *testing.T) {
	_, view := resize(t, newTestModel(t), 50, 10)
	if strings.TrimSpace(ansi.Strip(view)) != "terminal too small" {
		t.Fatalf("50x10 view = %q", ansi.Strip(view))
	}
}

func TestPanelSelectionAndModes(t *testing.T) {
	model, _ := resize(t, newTestModel(t), 100, 30)

	model = press(t, model, "3")
	if focused := model.(Model).focus; focused != Staged {
		t.Fatalf("after 3 the focus is %s", focused)
	}
	model = press(t, model, "tab")
	if focused := model.(Model).focus; focused != Issues {
		t.Fatalf("after tab the focus is %s", focused)
	}

	model = press(t, model, "1", "j")
	if selected, _ := model.(Model).packages.Selected(); selected.Name != "zsh" {
		t.Fatalf("after j the selection is %q", selected.Name)
	}
	view := model.View().Content
	if !strings.Contains(ansi.Strip(view), "Package: zsh") {
		t.Fatal("the main panel did not follow the packages cursor")
	}

	model = press(t, model, "+")
	if mode := model.(Model).mode; mode != ModeHalf {
		t.Fatalf("after + the mode is %s", mode)
	}
	assertScreen(t, model.View().Content, 100, 30)
	model = press(t, model, "+")
	if mode := model.(Model).mode; mode != ModeFullscreen {
		t.Fatalf("after ++ the mode is %s", mode)
	}
	assertScreen(t, model.View().Content, 100, 30)
}

func TestKeysPopup(t *testing.T) {
	model, _ := resize(t, newTestModel(t), 100, 30)
	model = press(t, model, "?")
	if model.(Model).popup != popupKeys {
		t.Fatal("? did not open the keys popup")
	}
	view := model.View().Content
	assertScreen(t, view, 100, 30)
	plain := ansi.Strip(view)
	for _, want := range []string{"Keys", "esc close", "0-4", "panels", "Global"} {
		if !strings.Contains(plain, want) {
			t.Fatalf("keys popup does not contain %q:\n%s", want, plain)
		}
	}
	model = press(t, model, "esc")
	if model.(Model).popup != popupNone {
		t.Fatal("esc did not close the keys popup")
	}
}

func TestEnterAndEscapeMainFocus(t *testing.T) {
	model, _ := resize(t, newTestModel(t), 100, 30)
	model = press(t, model, "enter")
	if !model.(Model).mainFocused {
		t.Fatal("enter did not move the focus into main")
	}
	model = press(t, model, "esc")
	if model.(Model).mainFocused {
		t.Fatal("esc did not return the focus to the side panel")
	}
}
