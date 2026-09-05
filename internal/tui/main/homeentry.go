package mainpanel

import (
	"bufio"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"

	"github.com/horbo/stower/internal/config"
	"github.com/horbo/stower/internal/dotfiles"
	"github.com/horbo/stower/internal/tui/components"
	"github.com/horbo/stower/internal/tui/styles"
)

const (
	CountCap     = 2000
	previewLines = 20
	listedNames  = 20
	previewBytes = 64 * 1024
)

type entryFacts struct {
	path      string
	exists    bool
	err       error
	isDir     bool
	isLink    bool
	size      int64
	files     int
	capped    bool
	nestedGit bool
	names     []string
	preview   []string
	binary    bool
}

type HomeEntry struct {
	paths  config.Paths
	home   string
	path   string
	pkg    string
	staged bool
	facts  entryFacts
	width  int
	height int
	st     styles.Styles
	vp     viewport.Model
}

func NewHomeEntry(paths config.Paths, home string, st styles.Styles) *HomeEntry {
	vp := viewport.New()
	vp.FillHeight = false
	return &HomeEntry{paths: paths, home: home, st: st, vp: vp}
}

func (h *HomeEntry) SetEntry(path, pkg string, staged bool) {
	if path != h.facts.path {
		h.facts = inspect(path)
		h.vp.SetYOffset(0)
	}
	h.path = path
	h.pkg = pkg
	h.staged = staged
	h.render()
}

func (h *HomeEntry) SetSize(width, height int) {
	if width == h.width && height == h.height {
		return
	}
	h.width = width
	h.height = height
	h.vp.SetWidth(max(0, width))
	h.vp.SetHeight(max(0, height))
	h.render()
}

func (h *HomeEntry) Update(msg tea.Msg) tea.Cmd {
	vp, cmd := h.vp.Update(msg)
	h.vp = vp
	return cmd
}

func (h *HomeEntry) View() string {
	if h.width <= 0 || h.height <= 0 {
		return ""
	}
	return h.vp.View()
}

func (h *HomeEntry) Title() string {
	if h.path == "" {
		return "Home"
	}
	return "Home: " + components.DisplayPath(h.path, h.home)
}

func (h *HomeEntry) Counter() string {
	if !h.facts.exists || !h.facts.isDir {
		return ""
	}
	if h.facts.capped {
		return fmt.Sprintf("%d+ files", CountCap)
	}
	return plural(h.facts.files, "file", "files")
}

func (h *HomeEntry) Keys() []key.Binding {
	return []key.Binding{
		key.NewBinding(key.WithKeys("j", "k"), key.WithHelp("j/k", "scroll")),
		key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "back")),
	}
}

func (h *HomeEntry) render() {
	h.vp.SetContent(strings.Join(h.lines(), "\n"))
}

func (h *HomeEntry) lines() []string {
	if h.width <= 0 {
		return nil
	}
	if h.path == "" {
		return []string{h.st.Dim.Render("no entry selected")}
	}
	if h.facts.err != nil {
		return []string{h.st.Error.Render(components.Truncate("cannot read the entry: "+h.facts.err.Error(), h.width))}
	}

	lines := []string{h.st.Dim.Render(components.Truncate(h.summary(), h.width))}
	if h.facts.nestedGit {
		lines = append(lines, h.st.Warn.Render(components.Truncate("⚠ contains .git/", h.width)))
	}
	if h.facts.isLink {
		lines = append(lines, h.st.Dim.Render(components.Truncate("a symlink cannot be staged", h.width)))
	}
	lines = append(lines, "")
	lines = append(lines, h.destination()...)
	lines = append(lines, "")
	lines = append(lines, h.body()...)
	return lines
}

func (h *HomeEntry) summary() string {
	parts := []string{kindOf(h.facts)}
	if h.facts.isDir {
		count := plural(h.facts.files, "file", "files")
		if h.facts.capped {
			count = fmt.Sprintf("%d+ files", CountCap)
		}
		parts = append(parts, count)
	} else if !h.facts.isLink {
		parts = append(parts, humanSize(h.facts.size))
	}
	if h.staged {
		parts = append(parts, "staged → "+h.pkg)
	}
	return strings.Join(parts, " · ")
}

func kindOf(facts entryFacts) string {
	switch {
	case !facts.exists:
		return "missing"
	case facts.isLink:
		return "symlink"
	case facts.isDir:
		return "directory"
	default:
		return "file"
	}
}

func (h *HomeEntry) destination() []string {
	pkg := h.pkg
	if pkg == "" {
		pkg = "<package>"
	}
	rel, err := dotfiles.RelToTarget(h.paths, h.path)
	if err != nil {
		return []string{h.st.Dim.Render(components.Truncate("outside the target directory", h.width))}
	}
	if err := dotfiles.ValidateStagingPath(h.paths, h.path); err != nil {
		return []string{h.st.Error.Render(components.Truncate("✘ "+err.Error(), h.width))}
	}
	pkgRel := dotfiles.TargetToPackage(pkg, rel)
	dest := filepath.Join(h.paths.Dotfiles, pkgRel)
	link, linkErr := filepath.Rel(filepath.Dir(h.path), dest)
	if linkErr != nil {
		link = dest
	}
	suffix := ""
	if h.facts.isDir {
		suffix = "/"
	}
	return []string{
		components.Truncate("Would become:  "+pkgRel+suffix, h.width),
		components.Truncate("Expected link: "+components.DisplayPath(h.path, h.home)+" → "+link, h.width),
	}
}

func (h *HomeEntry) body() []string {
	if !h.facts.exists {
		return nil
	}
	if h.facts.isDir {
		if len(h.facts.names) == 0 {
			return []string{h.st.Dim.Render("(empty directory)")}
		}
		lines := make([]string, 0, len(h.facts.names))
		for _, name := range h.facts.names {
			lines = append(lines, components.Truncate(name, h.width))
		}
		if h.facts.capped || h.facts.files > len(h.facts.names) {
			lines = append(lines, h.st.Dim.Render("…"))
		}
		return lines
	}
	if h.facts.binary {
		return []string{h.st.Dim.Render("(binary file)")}
	}
	lines := make([]string, 0, len(h.facts.preview))
	for _, line := range h.facts.preview {
		lines = append(lines, components.Truncate(strings.ReplaceAll(line, "\t", "    "), h.width))
	}
	return lines
}

func inspect(path string) entryFacts {
	facts := entryFacts{path: path}
	if path == "" {
		return facts
	}
	info, err := os.Lstat(path)
	if err != nil {
		facts.err = err
		return facts
	}
	facts.exists = true
	facts.isLink = info.Mode()&fs.ModeSymlink != 0
	facts.isDir = info.IsDir()
	facts.size = info.Size()
	if facts.isLink {
		return facts
	}
	if facts.isDir {
		facts.files, facts.capped = countFiles(path, CountCap)
		facts.names = listNames(path, listedNames)
		nested, err := dotfiles.HasNestedGit(path)
		facts.nestedGit = err == nil && nested
		return facts
	}
	facts.preview, facts.binary = head(path, previewLines)
	return facts
}

func countFiles(root string, limit int) (int, bool) {
	count := 0
	capped := false
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			if path == root {
				return err
			}
			return nil
		}
		if path == root || entry.IsDir() {
			return nil
		}
		count++
		if count >= limit {
			capped = true
			return filepath.SkipAll
		}
		return nil
	})
	if err != nil {
		return count, capped
	}
	return count, capped
}

func listNames(root string, limit int) []string {
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil
	}
	names := make([]string, 0, min(limit, len(entries)))
	for i, entry := range entries {
		if i >= limit {
			break
		}
		name := entry.Name()
		if entry.IsDir() {
			name += "/"
		}
		names = append(names, name)
	}
	return names
}

func head(path string, limit int) ([]string, bool) {
	file, err := os.Open(path)
	if err != nil {
		return nil, false
	}
	defer file.Close()

	reader := bufio.NewReader(file)
	probe, _ := reader.Peek(512)
	if !utf8.Valid(probe) || strings.ContainsRune(string(probe), 0) {
		return nil, true
	}

	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 0, 4096), previewBytes)
	lines := make([]string, 0, limit)
	for len(lines) < limit && scanner.Scan() {
		lines = append(lines, scanner.Text())
	}
	return lines, false
}

func humanSize(size int64) string {
	const unit = 1024
	if size < unit {
		return fmt.Sprintf("%d B", size)
	}
	value := float64(size)
	units := []string{"KB", "MB", "GB", "TB"}
	for _, name := range units {
		value /= unit
		if value < unit {
			return fmt.Sprintf("%.1f %s", value, name)
		}
	}
	return fmt.Sprintf("%.1f PB", value/unit)
}
