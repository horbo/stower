package dotfiles

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/kamilhorbowicz/stower/internal/config"
)

func newPaths(t *testing.T) config.Paths {
	t.Helper()
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatalf("EvalSymlinks: %v", err)
	}
	paths := config.Paths{
		Target:   filepath.Join(root, "target"),
		Dotfiles: filepath.Join(root, "dotfiles"),
	}
	mkdir(t, paths.Target)
	mkdir(t, paths.Dotfiles)
	return paths
}

func mkdir(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatalf("MkdirAll(%q): %v", path, err)
	}
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	mkdir(t, filepath.Dir(path))
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile(%q): %v", path, err)
	}
}

func symlink(t *testing.T, oldname, newname string) {
	t.Helper()
	mkdir(t, filepath.Dir(newname))
	if err := os.Symlink(oldname, newname); err != nil {
		t.Fatalf("Symlink(%q, %q): %v", oldname, newname, err)
	}
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile(%q): %v", path, err)
	}
	return string(content)
}

func isSymlink(t *testing.T, path string) bool {
	t.Helper()
	info, err := os.Lstat(path)
	if err != nil {
		t.Fatalf("Lstat(%q): %v", path, err)
	}
	return info.Mode()&os.ModeSymlink != 0
}

func exists(path string) bool {
	_, err := os.Lstat(path)
	return err == nil
}

func newPathsAt(t *testing.T, target, dotfiles string) config.Paths {
	t.Helper()
	mkdir(t, target)
	mkdir(t, dotfiles)
	return config.Paths{Target: target, Dotfiles: dotfiles}
}
