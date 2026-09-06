package mainpanel

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/horbo/stower/internal/config"
	"github.com/horbo/stower/internal/tui/styles"
)

func newEntryPaths(t *testing.T) config.Paths {
	t.Helper()
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatalf("EvalSymlinks: %v", err)
	}
	paths := config.Paths{
		Target:   filepath.Join(root, "home"),
		Dotfiles: filepath.Join(root, "dotfiles"),
	}
	makeDir(t, paths.Target)
	makeDir(t, filepath.Join(paths.Dotfiles, "misc"))
	return paths
}

func makeDir(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatalf("MkdirAll(%q): %v", path, err)
	}
}

func makeFile(t *testing.T, path, content string) {
	t.Helper()
	makeDir(t, filepath.Dir(path))
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile(%q): %v", path, err)
	}
}

func makeLink(t *testing.T, oldname, newname string) {
	t.Helper()
	makeDir(t, filepath.Dir(newname))
	if err := os.Symlink(oldname, newname); err != nil {
		t.Fatalf("Symlink(%q, %q): %v", oldname, newname, err)
	}
}

func newEntryPanel(t *testing.T, paths config.Paths, path, pkg string) *HomeEntry {
	t.Helper()
	h := NewHomeEntry(paths, paths.Target, styles.Default())
	h.SetSize(100, 20)
	h.SetEntry(path, pkg, false)
	h.SetFacts(path, InspectContext(context.Background(), paths, path))
	return h
}

func TestHomeEntryManagedSymlinkShowsItsPackage(t *testing.T) {
	paths := newEntryPaths(t)
	makeFile(t, filepath.Join(paths.Dotfiles, "misc", "dot-bar"), "bar\n")
	link := filepath.Join(paths.Target, ".bar")
	makeLink(t, "../dotfiles/misc/dot-bar", link)

	view := newEntryPanel(t, paths, link, "").View()
	for _, want := range []string{"✔ already in package misc", "→ ../dotfiles/misc/dot-bar"} {
		if !strings.Contains(view, want) {
			t.Errorf("a managed symlink does not render %q:\n%s", want, view)
		}
	}
	for _, unwanted := range []string{"cannot be staged", "path is a symlink"} {
		if strings.Contains(view, unwanted) {
			t.Errorf("a managed symlink renders %q:\n%s", unwanted, view)
		}
	}
}

func TestHomeEntryForeignSymlinkCannotBeStaged(t *testing.T) {
	paths := newEntryPaths(t)
	makeFile(t, filepath.Join(filepath.Dir(paths.Target), "elsewhere", "thing"), "thing\n")
	link := filepath.Join(paths.Target, ".foreign")
	makeLink(t, "../elsewhere/thing", link)

	view := newEntryPanel(t, paths, link, "").View()
	for _, want := range []string{"→ ../elsewhere/thing", "a symlink cannot be staged"} {
		if !strings.Contains(view, want) {
			t.Errorf("a foreign symlink does not render %q:\n%s", want, view)
		}
	}
	if strings.Contains(view, "already in package") {
		t.Errorf("a foreign symlink claims a package:\n%s", view)
	}
}

func TestHomeEntryDanglingPackageSymlinkWarns(t *testing.T) {
	paths := newEntryPaths(t)
	link := filepath.Join(paths.Target, ".gone")
	makeLink(t, "../dotfiles/misc/dot-gone", link)

	view := newEntryPanel(t, paths, link, "").View()
	for _, want := range []string{"⚠ already in package misc (dangling link)", "→ ../dotfiles/misc/dot-gone"} {
		if !strings.Contains(view, want) {
			t.Errorf("a dangling package symlink does not render %q:\n%s", want, view)
		}
	}
	if strings.Contains(view, "cannot be staged") {
		t.Errorf("a dangling package symlink renders %q:\n%s", "cannot be staged", view)
	}
}

func TestHomeEntryRegularFileKeepsTheDestinationBlock(t *testing.T) {
	paths := newEntryPaths(t)
	file := filepath.Join(paths.Target, ".zshrc")
	makeFile(t, file, "export EDITOR=vi\n")

	view := newEntryPanel(t, paths, file, "zsh").View()
	for _, want := range []string{"Would become:  zsh/dot-zshrc", "Expected link: ", "export EDITOR=vi"} {
		if !strings.Contains(view, want) {
			t.Errorf("a regular file does not render %q:\n%s", want, view)
		}
	}
	for _, unwanted := range []string{"cannot be staged", "already in package"} {
		if strings.Contains(view, unwanted) {
			t.Errorf("a regular file renders %q:\n%s", unwanted, view)
		}
	}
}
