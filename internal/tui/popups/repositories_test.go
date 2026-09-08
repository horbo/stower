package popups

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/horbo/stower/internal/dotfiles"
	"github.com/horbo/stower/internal/gitx"
	"github.com/horbo/stower/internal/tui/styles"
)

func repositoryKey(code rune) tea.KeyPressMsg { return tea.KeyPressMsg{Code: code} }

func TestRepositoriesDraftAndURLEdit(t *testing.T) {
	rows := []dotfiles.NestedRepository{
		{Source: "/temporary/one", Info: gitx.Repository{GitDir: "/temporary/one/.git"}, Choice: dotfiles.RepositoryChoice{URL: "https://example.invalid/one.git"}},
		{Source: "/temporary/two", Info: gitx.Repository{GitDir: "/temporary/two/.git"}},
	}
	p := NewRepositories(styles.Default())
	p.Open(rows)
	p.Update(repositoryKey(' '))
	p.Update(repositoryKey(' '))
	p.Update(repositoryKey('e'))
	p.input.SetValue("https://example.invalid/changed.git")
	p.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	p.Update(repositoryKey('j'))
	p.Update(repositoryKey(' '))
	cmd := p.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	msg := cmd().(RepositoriesChosenMsg)
	if msg.Choices[rows[0].Source].Action != dotfiles.ConvertRepository || msg.Choices[rows[0].Source].URL != "https://example.invalid/changed.git" {
		t.Fatalf("conversion: %+v", msg.Choices)
	}
	if msg.Choices[rows[1].Source].Action != dotfiles.RemoveRepositoryGit {
		t.Fatalf("second choice: %+v", msg.Choices)
	}
	if rows[0].Choice.Action != dotfiles.KeepRepository || rows[0].Choice.URL != "https://example.invalid/one.git" {
		t.Fatal("popup mutated its input")
	}
}

func TestRepositoriesUnsupportedConversionAndCancel(t *testing.T) {
	p := NewRepositories(styles.Default())
	p.Open([]dotfiles.NestedRepository{{Source: "/temporary/repo", Info: gitx.Repository{GitDir: "/temporary/repo/.git", Reason: "worktrees are not supported"}}})
	p.Update(repositoryKey(' '))
	p.Update(repositoryKey(' '))
	if p.rows[0].Choice.Action != dotfiles.KeepRepository {
		t.Fatal("unsupported conversion was selected")
	}
	p.Update(repositoryKey('e'))
	p.input.SetValue("https://example.invalid/ignored.git")
	p.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	if p.rows[0].Choice.URL != "" {
		t.Fatal("cancel saved URL edit")
	}
	cmd := p.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	if _, ok := cmd().(RepositoriesCancelledMsg); !ok {
		t.Fatal("cancel saved choices")
	}
}

func TestRepositoriesPopupKeyMap(t *testing.T) {
	p := NewRepositories(styles.Default())
	want := []string{"k/↑", "j/↓", "space", "e", "enter", "esc"}
	keys := p.Keys()
	if len(keys) != len(want) {
		t.Fatalf("keys = %d, want %d", len(keys), len(want))
	}
	for i, binding := range keys {
		if binding.Help().Key != want[i] {
			t.Fatalf("key %d = %q, want %q", i, binding.Help().Key, want[i])
		}
	}
}

func TestRepositoriesPopupBlocksInvalidURL(t *testing.T) {
	p := NewRepositories(styles.Default())
	p.SetSize(70, 20)
	p.Open([]dotfiles.NestedRepository{
		{Source: "/temporary/one", Info: gitx.Repository{GitDir: "/temporary/one/.git"}},
		{Source: "/temporary/two", Info: gitx.Repository{GitDir: "/temporary/two/.git"}},
	})
	p.Update(repositoryKey('j'))
	p.Update(repositoryKey(' '))
	p.Update(repositoryKey(' '))
	if p.rows[1].Choice.Action != dotfiles.ConvertRepository {
		t.Fatalf("action: %v", p.rows[1].Choice.Action)
	}
	p.Update(repositoryKey('k'))
	if cmd := p.Update(tea.KeyPressMsg{Code: tea.KeyEnter}); cmd != nil {
		t.Fatal("the popup closed with an empty conversion URL")
	}
	if p.cursor != 1 {
		t.Fatalf("cursor = %d, want the offending row", p.cursor)
	}
	if !strings.Contains(p.View(), "a repository URL is required") {
		t.Fatalf("the error is not rendered: %s", p.View())
	}

	p.Update(repositoryKey('e'))
	p.input.SetValue("ext::sh -c id")
	p.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if cmd := p.Update(tea.KeyPressMsg{Code: tea.KeyEnter}); cmd != nil {
		t.Fatal("the popup closed with a remote helper URL")
	}

	p.Update(repositoryKey('e'))
	p.input.SetValue("https://example.invalid/repo.git")
	p.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	cmd := p.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("a valid URL did not close the popup")
	}
	if _, ok := cmd().(RepositoriesChosenMsg); !ok {
		t.Fatal("expected the choices message")
	}
}

func TestRepositoriesPopupUsesAllowedActions(t *testing.T) {
	p := NewRepositories(styles.Default())
	p.SetSize(70, 20)
	p.Open([]dotfiles.NestedRepository{
		{Source: "/temporary/plain", Info: gitx.Repository{Reason: "conversion requires a repository with its own .git directory"}},
	})
	for i := 0; i < 3; i++ {
		p.Update(repositoryKey(' '))
		if p.rows[0].Choice.Action != dotfiles.KeepRepository {
			t.Fatalf("action after %d cycles: %v", i+1, p.rows[0].Choice.Action)
		}
	}
	if !strings.Contains(p.View(), "Only Keep is available") {
		t.Fatalf("space was a silent no-op: %s", p.View())
	}

	p.Open([]dotfiles.NestedRepository{
		{Source: "/temporary/repo", Info: gitx.Repository{GitDir: "/temporary/repo/.git"}},
	})
	want := []dotfiles.RepositoryAction{
		dotfiles.RemoveRepositoryGit, dotfiles.ConvertRepository, dotfiles.KeepRepository,
	}
	for i, action := range want {
		p.Update(repositoryKey(' '))
		if p.rows[0].Choice.Action != action {
			t.Fatalf("cycle %d = %v, want %v", i, p.rows[0].Choice.Action, action)
		}
	}
}

func TestRepositoriesPopupScrollAndClick(t *testing.T) {
	var rows []dotfiles.NestedRepository
	for i := 0; i < 10; i++ {
		rows = append(rows, dotfiles.NestedRepository{
			Source: "/temporary/repo" + string(rune('0'+i)),
			Info:   gitx.Repository{GitDir: "/temporary/repo/.git"},
		})
	}
	p := NewRepositories(styles.Default())
	p.SetSize(70, 12)
	p.Open(rows)
	if p.vp.Height() <= 0 || p.vp.Height() >= len(rows)*repositoryRowHeight {
		t.Fatalf("viewport height = %d, want a window smaller than the list", p.vp.Height())
	}

	for i := 0; i < len(rows)-1; i++ {
		p.Update(repositoryKey('j'))
	}
	if p.cursor != len(rows)-1 {
		t.Fatalf("cursor = %d", p.cursor)
	}
	if p.vp.YOffset() == 0 {
		t.Fatal("the viewport did not follow the cursor")
	}

	before := p.vp.YOffset()
	p.Scroll(-2)
	if p.vp.YOffset() != before-2 {
		t.Fatalf("scroll offset = %d, want %d", p.vp.YOffset(), before-2)
	}

	p.vp.SetYOffset(0)
	p.cursor = 0
	p.render()
	p.vp.SetYOffset(0)
	if cmd := p.Click(3, repositoryRowHeight); cmd != nil {
		t.Fatal("click returned a command")
	}
	if p.cursor != 1 {
		t.Fatalf("click did not move the cursor: %d", p.cursor)
	}
	p.Click(3, repositoryRowHeight)
	if p.rows[1].Choice.Action != dotfiles.RemoveRepositoryGit {
		t.Fatalf("clicking the selected row did not cycle: %v", p.rows[1].Choice.Action)
	}
	if cmd := p.Click(3, 500); cmd != nil {
		t.Fatal("a click outside the list returned a command")
	}
}

func TestRepositoriesKeepDoesNotPromptForConversion(t *testing.T) {
	p := NewRepositories(styles.Default())
	p.SetSize(70, 20)
	rows := []dotfiles.NestedRepository{{Source: "/temporary/repo", Info: gitx.Repository{GitDir: "/temporary/repo/.git", Dirty: true}, Choice: dotfiles.RepositoryChoice{URL: "https://example.invalid/repo.git"}}}
	p.Open(rows)
	view := p.View()
	if !strings.Contains(view, "Action: Keep repository") {
		t.Fatalf("default Keep is not visible: %s", view)
	}
	for _, text := range []string{"URL:", "records HEAD", "Convert to submodule"} {
		if strings.Contains(view, text) {
			t.Fatalf("Keep prompts for conversion with %q: %s", text, view)
		}
	}
	cmd := p.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if msg := cmd().(RepositoriesChosenMsg); msg.Choices[rows[0].Source].Action != dotfiles.KeepRepository {
		t.Fatal("accepting the default changed the action")
	}
	p.Update(repositoryKey(' '))
	p.Update(repositoryKey(' '))
	view = p.View()
	for _, text := range []string{"Action: Convert to submodule", "URL:", "records HEAD"} {
		if !strings.Contains(view, text) {
			t.Fatalf("explicit conversion is missing %q: %s", text, view)
		}
	}
}
