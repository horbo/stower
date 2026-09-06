package components

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

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

type invocation struct {
	ctx     context.Context
	release chan struct{}
}

type loadGate struct {
	mu    sync.Mutex
	chans map[string]chan *invocation
	nodes map[string][]Node
	errs  map[string]error
}

func newLoadGate() *loadGate {
	return &loadGate{
		chans: map[string]chan *invocation{},
		nodes: map[string][]Node{},
		errs:  map[string]error{},
	}
}

func (g *loadGate) channel(path string) chan *invocation {
	g.mu.Lock()
	defer g.mu.Unlock()
	ch, ok := g.chans[path]
	if !ok {
		ch = make(chan *invocation, 8)
		g.chans[path] = ch
	}
	return ch
}

func (g *loadGate) setResult(path string, nodes []Node, err error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.nodes[path] = nodes
	g.errs[path] = err
}

func (g *loadGate) loader() ContextLoader {
	return func(ctx context.Context, path string) ([]Node, error) {
		inv := &invocation{ctx: ctx, release: make(chan struct{})}
		g.channel(path) <- inv
		select {
		case <-inv.release:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
		g.mu.Lock()
		nodes, err := g.nodes[path], g.errs[path]
		g.mu.Unlock()
		return nodes, err
	}
}

func (g *loadGate) next(t *testing.T, path string) *invocation {
	t.Helper()
	select {
	case inv := <-g.channel(path):
		return inv
	case <-time.After(2 * time.Second):
		t.Fatalf("no load was requested for %s", path)
		return nil
	}
}

func (g *loadGate) allow(inv *invocation) {
	close(inv.release)
}

func (g *loadGate) waitCancelled(t *testing.T, inv *invocation) {
	t.Helper()
	select {
	case <-inv.ctx.Done():
	case <-time.After(2 * time.Second):
		t.Fatal("the load context was not cancelled")
	}
}

func startLoad(t *testing.T, cmd tea.Cmd) chan tea.Msg {
	t.Helper()
	if cmd == nil {
		t.Fatal("expand did not return a command")
	}
	msg := cmd()
	batch, ok := msg.(tea.BatchMsg)
	if !ok || len(batch) == 0 {
		t.Fatalf("cmd result = %T, want tea.BatchMsg", msg)
	}
	out := make(chan tea.Msg, len(batch))
	for _, leaf := range batch {
		leaf := leaf
		if leaf == nil {
			continue
		}
		go func() { out <- leaf() }()
	}
	return out
}

func recvLoaded(t *testing.T, ch chan tea.Msg) TreeLoadedMsg {
	t.Helper()
	for {
		select {
		case msg := <-ch:
			if loaded, ok := msg.(TreeLoadedMsg); ok {
				return loaded
			}
		case <-time.After(2 * time.Second):
			t.Fatal("no TreeLoadedMsg arrived")
			return TreeLoadedMsg{}
		}
	}
}

func newAsyncTree(g *loadGate) *Tree {
	tree := NewTree(styles.Styles{})
	tree.SetContextLoader(g.loader())
	tree.SetRoot(Node{Path: "/t", Name: "~", IsDir: true, Expandable: true})
	tree.SetSize(40, 10)
	return tree
}

func TestTreeAsyncExpandShowsSpinnerThenChildren(t *testing.T) {
	g := newLoadGate()
	g.setResult("/t", []Node{
		{Path: "/t/config", Name: "config", IsDir: true, Selectable: true, Expandable: true},
	}, nil)
	g.setResult("/t/config", []Node{
		{Path: "/t/config/nvim", Name: "nvim", Selectable: true},
	}, nil)

	tree := newAsyncTree(g)

	ch := startLoad(t, tree.ReloadAsync())
	rootInv := g.next(t, "/t")
	g.allow(rootInv)
	tree.Update(recvLoaded(t, ch))

	if got := paths(tree); !equal(got, []string{"/t", "/t/config"}) {
		t.Fatalf("rows after root load = %v", got)
	}

	press(tree, "j")
	spinnerCmd := tree.expandSelected()
	if spinner := tree.spin.View(); !strings.Contains(ansi.Strip(tree.View()), spinner) {
		t.Fatalf("expanding a folder did not show the spinner: %q", ansi.Strip(tree.View()))
	}

	ch = startLoad(t, spinnerCmd)
	configInv := g.next(t, "/t/config")
	g.allow(configInv)
	tree.Update(recvLoaded(t, ch))

	want := []string{"/t", "/t/config", "/t/config/nvim"}
	if got := paths(tree); !equal(got, want) {
		t.Fatalf("rows after expand = %v, want %v", got, want)
	}
}

func TestTreeIgnoresStaleOrForeignLoadResults(t *testing.T) {
	g := newLoadGate()
	g.setResult("/t", nil, nil)
	tree := newAsyncTree(g)

	ch := startLoad(t, tree.ReloadAsync())
	inv := g.next(t, "/t")

	staleMsg := TreeLoadedMsg{Tree: tree, Path: "/t", Generation: 9999, Nodes: []Node{{Path: "/t/x", Name: "x"}}}
	tree.Update(staleMsg)
	if _, ok := tree.children["/t"]; ok {
		t.Fatal("a stale generation was accepted")
	}

	foreign := NewTree(styles.Styles{})
	foreignMsg := TreeLoadedMsg{
		Tree:       foreign,
		Path:       "/t",
		Generation: tree.inflight["/t"].generation,
		Nodes:      []Node{{Path: "/t/y", Name: "y"}},
	}
	tree.Update(foreignMsg)
	if _, ok := tree.children["/t"]; ok {
		t.Fatal("a foreign tree pointer was accepted")
	}

	g.allow(inv)
	tree.Update(recvLoaded(t, ch))
	if got := paths(tree); !equal(got, []string{"/t"}) {
		t.Fatalf("rows after the correct load = %v", got)
	}
}

func TestTreeCollapseCancelsPendingLoad(t *testing.T) {
	g := newLoadGate()
	g.setResult("/t", []Node{
		{Path: "/t/config", Name: "config", IsDir: true, Selectable: true, Expandable: true},
	}, nil)
	g.setResult("/t/config", []Node{{Path: "/t/config/nvim", Name: "nvim", Selectable: true}}, nil)

	tree := newAsyncTree(g)
	ch := startLoad(t, tree.ReloadAsync())
	rootInv := g.next(t, "/t")
	g.allow(rootInv)
	tree.Update(recvLoaded(t, ch))

	press(tree, "j")
	cmd := tree.expandSelected()
	ch = startLoad(t, cmd)
	inv := g.next(t, "/t/config")

	press(tree, "left")
	if len(tree.inflight) != 0 {
		t.Fatalf("inflight = %v, want empty right after collapse", tree.inflight)
	}
	g.waitCancelled(t, inv)

	late := recvLoaded(t, ch)
	tree.Update(late)

	if len(tree.inflight) != 0 {
		t.Fatal("the late result re-added an inflight load")
	}
	if got := paths(tree); !equal(got, []string{"/t", "/t/config"}) {
		t.Fatalf("rows after the late result = %v, want the collapsed tree", got)
	}
	if _, ok := tree.children["/t/config"]; ok {
		t.Fatal("the late result stored children for a collapsed folder")
	}
}

func TestTreeExpandsMultipleFoldersConcurrently(t *testing.T) {
	g := newLoadGate()
	g.setResult("/t", []Node{
		{Path: "/t/a", Name: "a", IsDir: true, Selectable: true, Expandable: true},
		{Path: "/t/b", Name: "b", IsDir: true, Selectable: true, Expandable: true},
		{Path: "/t/c", Name: "c", IsDir: true, Selectable: true, Expandable: true},
	}, nil)
	g.setResult("/t/a", []Node{{Path: "/t/a/1", Name: "1", Selectable: true}}, nil)
	g.setResult("/t/b", []Node{{Path: "/t/b/1", Name: "1", Selectable: true}}, nil)
	g.setResult("/t/c", []Node{{Path: "/t/c/1", Name: "1", Selectable: true}}, nil)

	tree := newAsyncTree(g)
	ch := startLoad(t, tree.ReloadAsync())
	rootInv := g.next(t, "/t")
	g.allow(rootInv)
	tree.Update(recvLoaded(t, ch))

	press(tree, "j")
	chA := startLoad(t, tree.expandSelected())
	press(tree, "j")
	chB := startLoad(t, tree.expandSelected())
	press(tree, "j")
	chC := startLoad(t, tree.expandSelected())

	if len(tree.inflight) != 3 {
		t.Fatalf("inflight = %v, want 3 concurrent loads", tree.inflight)
	}

	invA := g.next(t, "/t/a")
	invB := g.next(t, "/t/b")
	invC := g.next(t, "/t/c")
	g.allow(invC)
	g.allow(invA)
	g.allow(invB)

	tree.Update(recvLoaded(t, chC))
	tree.Update(recvLoaded(t, chA))
	tree.Update(recvLoaded(t, chB))

	want := []string{"/t", "/t/a", "/t/a/1", "/t/b", "/t/b/1", "/t/c", "/t/c/1"}
	if got := paths(tree); !equal(got, want) {
		t.Fatalf("rows after concurrent expand = %v, want %v", got, want)
	}
	if len(tree.inflight) != 0 {
		t.Fatal("inflight loads remained after every result arrived")
	}
}

func TestTreeReloadAsyncDuringLoadCancelsAndReloadsRoot(t *testing.T) {
	g := newLoadGate()
	g.setResult("/t", []Node{{Path: "/t/old", Name: "old", Selectable: true}}, nil)

	tree := newAsyncTree(g)
	ch1 := startLoad(t, tree.ReloadAsync())
	inv1 := g.next(t, "/t")

	ch2 := startLoad(t, tree.ReloadAsync())
	g.waitCancelled(t, inv1)
	if len(tree.inflight) != 1 {
		t.Fatalf("inflight = %v, want exactly the new root load", tree.inflight)
	}

	g.setResult("/t", []Node{{Path: "/t/new", Name: "new", Selectable: true}}, nil)
	inv2 := g.next(t, "/t")
	g.allow(inv2)
	tree.Update(recvLoaded(t, ch2))

	if got := paths(tree); !equal(got, []string{"/t", "/t/new"}) {
		t.Fatalf("rows after reload = %v, want the fresh root children", got)
	}

	stale := recvLoaded(t, ch1)
	tree.Update(stale)
	if got := paths(tree); !equal(got, []string{"/t", "/t/new"}) {
		t.Fatalf("a stale root result changed the tree: %v", got)
	}
}

func TestTreeAsyncLoadErrorIsReported(t *testing.T) {
	g := newLoadGate()
	failure := errors.New("boom")
	g.setResult("/t", nil, failure)

	tree := newAsyncTree(g)
	ch := startLoad(t, tree.ReloadAsync())
	inv := g.next(t, "/t")
	g.allow(inv)
	tree.Update(recvLoaded(t, ch))

	if tree.LoadError("/t") == nil {
		t.Fatal("a failing async loader did not record an error")
	}
	if !strings.Contains(ansi.Strip(tree.View()), errorGlyph) {
		t.Fatalf("the error glyph is missing from the view: %q", ansi.Strip(tree.View()))
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
