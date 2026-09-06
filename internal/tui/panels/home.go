package panels

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"

	"github.com/horbo/stower/internal/config"
	"github.com/horbo/stower/internal/dotfiles"
	"github.com/horbo/stower/internal/tui/components"
	"github.com/horbo/stower/internal/tui/styles"
)

type StageRequestMsg struct {
	Path  string
	IsDir bool
}

type UnstageRequestMsg struct {
	Path string
}

type homeKeyMap struct {
	Stage   key.Binding
	Unstage key.Binding
}

func defaultHomeKeyMap() homeKeyMap {
	return homeKeyMap{
		Stage:   key.NewBinding(key.WithKeys("space"), key.WithHelp("space", "stage")),
		Unstage: key.NewBinding(key.WithKeys("u"), key.WithHelp("u", "unstage")),
	}
}

type Home struct {
	paths   config.Paths
	home    string
	tree    *components.Tree
	staging dotfiles.Staging
	width   int
	height  int
	st      styles.Styles
	keys    homeKeyMap
}

func NewHome(paths config.Paths, home string, st styles.Styles) *Home {
	p := &Home{
		paths:   paths,
		home:    home,
		tree:    components.NewTree(st),
		staging: dotfiles.Staging{},
		st:      st,
		keys:    defaultHomeKeyMap(),
	}
	p.tree.SetContextLoader(p.loadContext)
	p.tree.SetRoot(components.Node{
		Path:       paths.Target,
		Name:       components.DisplayPath(paths.Target, home),
		IsDir:      true,
		Expandable: true,
	})
	p.applyBadges()
	return p
}

func (p *Home) SetStaging(staging dotfiles.Staging) {
	p.staging = staging
	p.applyBadges()
}

func (p *Home) Reload() tea.Cmd {
	cmd := p.tree.ReloadAsync()
	p.applyBadges()
	return cmd
}

func (p *Home) Selected() (components.Node, bool) {
	row, ok := p.tree.Selected()
	return row.Node, ok
}

func (p *Home) CapturesInput() bool {
	return p.tree.Filtering()
}

func (p *Home) SetSize(w, h int) {
	p.width = w
	p.height = h
	p.tree.SetSize(w, h)
}

func (p *Home) Update(msg tea.Msg) tea.Cmd {
	if pressed, ok := msg.(tea.KeyPressMsg); ok && !p.tree.Filtering() {
		node, hasNode := p.Selected()
		switch {
		case key.Matches(pressed, p.keys.Stage):
			if !hasNode || !node.Selectable {
				return nil
			}
			return func() tea.Msg { return StageRequestMsg{Path: node.Path, IsDir: node.IsDir} }
		case key.Matches(pressed, p.keys.Unstage):
			if !hasNode {
				return nil
			}
			return func() tea.Msg { return UnstageRequestMsg{Path: node.Path} }
		}
	}
	cmd := p.tree.Update(msg)
	if _, ok := msg.(components.TreeLoadedMsg); ok {
		p.applyBadges()
	}
	return cmd
}

func (p *Home) View() string {
	return p.tree.View()
}

func (p *Home) Title() string {
	return "[2] Home"
}

func (p *Home) Counter() string {
	if p.tree.Len() == 0 {
		return ""
	}
	if filter := p.tree.Filter(); filter != "" {
		return fmt.Sprintf("/%s %d", filter, p.tree.Len())
	}
	return fmt.Sprintf("%d of %d", p.tree.Cursor()+1, p.tree.Len())
}

func (p *Home) Keys() []key.Binding {
	return []key.Binding{
		p.keys.Stage,
		p.keys.Unstage,
		key.NewBinding(key.WithKeys("left", "right"), key.WithHelp("←→", "fold")),
		key.NewBinding(key.WithKeys("/"), key.WithHelp("/", "filter")),
		key.NewBinding(key.WithKeys("j", "k"), key.WithHelp("j/k", "move")),
	}
}

func (p *Home) applyBadges() {
	p.tree.Apply(func(node *components.Node) {
		if pkg, ok := p.staging[node.Path]; ok {
			node.Badge = "→ " + pkg
			node.BadgeStyle = p.st.Accent
			return
		}
		if node.Managed != "" {
			node.Badge = "[" + node.Managed + "]"
			node.BadgeStyle = p.st.Dim
			return
		}
		node.Badge = ""
		node.BadgeStyle = p.st.Dim
	})
}

func (p *Home) loadContext(ctx context.Context, path string) ([]components.Node, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(path)
	if err != nil {
		return nil, err
	}
	nodes := make([]components.Node, 0, len(entries))
	for _, entry := range entries {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		full := filepath.Join(path, entry.Name())
		if full == p.paths.Dotfiles {
			continue
		}
		node := components.Node{Path: full, Name: entry.Name(), IsDir: entry.IsDir()}
		switch {
		case entry.Type()&fs.ModeSymlink != 0:
			if info, err := os.Stat(full); err == nil && info.IsDir() {
				node.IsDir = true
			}
			if pkg, ok := dotfiles.ManagedBy(p.paths, full); ok {
				node.Managed = pkg
			}
		default:
			node.Selectable = true
			node.Expandable = entry.IsDir()
		}
		nodes = append(nodes, node)
	}
	sortNodes(nodes)
	return nodes, nil
}

func sortNodes(nodes []components.Node) {
	sort.SliceStable(nodes, func(i, j int) bool {
		if nodes[i].IsDir != nodes[j].IsDir {
			return nodes[i].IsDir
		}
		left, right := strings.ToLower(nodes[i].Name), strings.ToLower(nodes[j].Name)
		if left != right {
			return left < right
		}
		return nodes[i].Name < nodes[j].Name
	})
}
