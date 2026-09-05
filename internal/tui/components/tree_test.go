package components

import (
	"errors"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/horbo/stower/internal/tui/styles"
)

func fakeLoader(calls *int) Loader {
	tree := map[string][]Node{
		"/t": {
			{Path: "/t/config", Name: "config", IsDir: true, Selectable: true, Expandable: true},
			{Path: "/t/bin", Name: "bin", IsDir: true, Selectable: true, Expandable: true},
			{Path: "/t/managed", Name: "managed", IsDir: true, Badge: "[zsh]"},
			{Path: "/t/zshrc", Name: "zshrc", Selectable: true},
		},
		"/t/config": {
			{Path: "/t/config/nvim", Name: "nvim", IsDir: true, Selectable: true, Expandable: true},
			{Path: "/t/config/gh", Name: "gh", IsDir: true, Selectable: true, Expandable: true},
		},
		"/t/config/nvim": {
			{Path: "/t/config/nvim/init.lua", Name: "init.lua", Selectable: true},
		},
		"/t/bin": {},
	}
	return func(path string) ([]Node, error) {
		if calls != nil {
			*calls++
		}
		nodes, ok := tree[path]
		if !ok {
			return nil, errors.New("cannot read " + path)
		}
		return nodes, nil
	}
}

func newTestTree(t *testing.T, calls *int) *Tree {
	t.Helper()
	tree := NewTree(styles.Styles{})
	tree.SetLoader(fakeLoader(calls))
	tree.SetRoot(Node{Path: "/t", Name: "~", IsDir: true, Expandable: true})
	tree.SetSize(40, 10)
	return tree
}

func paths(tree *Tree) []string {
	out := make([]string, 0, tree.Len())
	for i := 0; i < tree.Len(); i++ {
		out = append(out, tree.rows[i].Node.Path)
	}
	return out
}

func press(tree *Tree, keys ...string) {
	for _, k := range keys {
		switch k {
		case "enter":
			tree.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
		case "esc":
			tree.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
		case "left":
			tree.Update(tea.KeyPressMsg{Code: tea.KeyLeft})
		case "right":
			tree.Update(tea.KeyPressMsg{Code: tea.KeyRight})
		default:
			tree.Update(tea.KeyPressMsg{Code: []rune(k)[0], Text: k})
		}
	}
}

func TestTreeFlattensTheExpandedRootOnly(t *testing.T) {
	tree := newTestTree(t, nil)
	want := []string{"/t", "/t/config", "/t/bin", "/t/managed", "/t/zshrc"}
	if got := paths(tree); !equal(got, want) {
		t.Fatalf("flattened rows = %v, want %v", got, want)
	}
	if depth := tree.rows[1].Depth; depth != 1 {
		t.Fatalf("child depth = %d, want 1", depth)
	}
}

func TestTreeExpandAndCollapse(t *testing.T) {
	calls := 0
	tree := newTestTree(t, &calls)
	press(tree, "j", "right")
	want := []string{"/t", "/t/config", "/t/config/nvim", "/t/config/gh", "/t/bin", "/t/managed", "/t/zshrc"}
	if got := paths(tree); !equal(got, want) {
		t.Fatalf("after expand rows = %v, want %v", got, want)
	}

	loaded := calls
	press(tree, "left", "right")
	if calls != loaded {
		t.Fatalf("re-expanding reloaded the children: %d calls, want %d", calls, loaded)
	}

	press(tree, "left")
	want = []string{"/t", "/t/config", "/t/bin", "/t/managed", "/t/zshrc"}
	if got := paths(tree); !equal(got, want) {
		t.Fatalf("after collapse rows = %v, want %v", got, want)
	}
}

func TestTreeCollapseOnALeafJumpsToTheParent(t *testing.T) {
	tree := newTestTree(t, nil)
	press(tree, "j", "right", "j")
	if row, _ := tree.Selected(); row.Node.Path != "/t/config/nvim" {
		t.Fatalf("cursor is on %q", row.Node.Path)
	}
	press(tree, "left")
	if row, _ := tree.Selected(); row.Node.Path != "/t/config" {
		t.Fatalf("collapse on a collapsed child moved to %q, want /t/config", row.Node.Path)
	}
}

func TestTreeDoesNotExpandNonExpandableNodes(t *testing.T) {
	tree := newTestTree(t, nil)
	press(tree, "j", "j", "j")
	row, _ := tree.Selected()
	if row.Node.Path != "/t/managed" {
		t.Fatalf("cursor is on %q, want /t/managed", row.Node.Path)
	}
	press(tree, "right")
	if tree.Len() != 5 {
		t.Fatalf("a non-expandable node was expanded: %v", paths(tree))
	}
}

func TestTreeFilterAndCursorClamping(t *testing.T) {
	tree := newTestTree(t, nil)
	press(tree, "G")
	if tree.Cursor() != tree.Len()-1 {
		t.Fatalf("G left the cursor at %d of %d", tree.Cursor(), tree.Len())
	}

	press(tree, "/", "C", "O", "N")
	if !tree.Filtering() {
		t.Fatal("/ did not start filtering")
	}
	if got := paths(tree); !equal(got, []string{"/t/config"}) {
		t.Fatalf("filtered rows = %v, want [/t/config]", got)
	}
	if tree.Cursor() != 0 {
		t.Fatalf("the cursor was not clamped into the filtered list: %d", tree.Cursor())
	}

	press(tree, "enter")
	if tree.Filtering() {
		t.Fatal("enter did not leave the filter input")
	}
	if tree.Filter() != "CON" {
		t.Fatalf("filter = %q, want CON", tree.Filter())
	}

	press(tree, "esc")
	if tree.Filter() != "" {
		t.Fatalf("esc did not clear the filter: %q", tree.Filter())
	}
	if tree.Len() != 5 {
		t.Fatalf("rows after clearing the filter = %v", paths(tree))
	}
}

func TestTreeKeepsTheCursorOnTheSameNodeAcrossRebuilds(t *testing.T) {
	tree := newTestTree(t, nil)
	press(tree, "j", "j")
	before, _ := tree.Selected()
	tree.Apply(func(node *Node) {
		if node.Path == "/t/zshrc" {
			node.Badge = "→ zsh"
		}
	})
	after, _ := tree.Selected()
	if before.Node.Path != after.Node.Path {
		t.Fatalf("cursor moved from %q to %q", before.Node.Path, after.Node.Path)
	}
}

func TestTreeReportsLoadErrors(t *testing.T) {
	tree := NewTree(styles.Styles{})
	tree.SetLoader(fakeLoader(nil))
	tree.SetRoot(Node{Path: "/missing", Name: "~", IsDir: true, Expandable: true})
	tree.SetSize(40, 10)
	if tree.LoadError("/missing") == nil {
		t.Fatal("a failing loader did not record an error")
	}
	if !strings.Contains(ansi.Strip(tree.View()), errorGlyph) {
		t.Fatalf("the error glyph is missing from the view: %q", ansi.Strip(tree.View()))
	}
}

func TestTreeViewFitsTheWidth(t *testing.T) {
	tree := newTestTree(t, nil)
	tree.SetSize(24, 3)
	press(tree, "j", "right")
	lines := strings.Split(tree.View(), "\n")
	if len(lines) != 3 {
		t.Fatalf("view has %d lines, want 3", len(lines))
	}
	for i, line := range lines {
		if got := ansi.StringWidth(line); got != 24 {
			t.Fatalf("line %d has width %d: %q", i, got, ansi.Strip(line))
		}
	}
}

func equal(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
