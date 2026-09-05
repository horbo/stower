package gitx

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func newRepo(t *testing.T) string {
	t.Helper()
	if !Available() {
		t.Skip("git is not installed")
	}
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := Init(dir); err != nil {
		t.Fatal(err)
	}
	for _, setting := range [][2]string{
		{"user.name", "stower test"},
		{"user.email", "test@example.invalid"},
		{"commit.gpgsign", "false"},
	} {
		cmd := exec.Command("git", "-C", dir, "config", setting[0], setting[1])
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git config %s: %v: %s", setting[0], err, out)
		}
	}
	return dir
}

func gitOutput(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v: %s", strings.Join(args, " "), err, out)
	}
	return string(out)
}

func writePackage(t *testing.T, dir, pkg string, names ...string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(dir, pkg), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, name := range names {
		if err := os.WriteFile(filepath.Join(dir, pkg, name), []byte("x\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func TestInitAndIsRepo(t *testing.T) {
	if !Available() {
		t.Skip("git is not installed")
	}
	dir := t.TempDir()
	if IsRepo(dir) {
		t.Fatal("empty directory reported as a repository")
	}
	if err := Init(dir); err != nil {
		t.Fatal(err)
	}
	if !IsRepo(dir) {
		t.Fatal("initialised directory not reported as a repository")
	}
	nested := filepath.Join(dir, "pkg")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatal(err)
	}
	if IsRepo(nested) {
		t.Fatal("a subdirectory of a repository must not be reported as its own repository")
	}
	if IsRepo(filepath.Join(dir, "missing")) {
		t.Fatal("missing directory reported as a repository")
	}
	if IsRepo("") {
		t.Fatal("empty directory name reported as a repository")
	}
}

func TestDirtyPathsAndPorcelain(t *testing.T) {
	dir := newRepo(t)
	writePackage(t, dir, "zsh", "dot-zshrc")
	writePackage(t, dir, "nvim", "dot-config")

	paths, err := DirtyPaths(dir, "zsh")
	if err != nil {
		t.Fatal(err)
	}
	if len(paths) != 1 || paths[0] != "zsh/dot-zshrc" {
		t.Fatalf("dirty paths = %v", paths)
	}
	lines, err := Porcelain(dir, "zsh", "nvim")
	if err != nil {
		t.Fatal(err)
	}
	if len(lines) != 2 {
		t.Fatalf("porcelain = %v", lines)
	}
	for _, line := range lines {
		if !strings.HasPrefix(line, "?? ") {
			t.Fatalf("unexpected porcelain line %q", line)
		}
	}

	if err := AddAndCommit(dir, []string{"zsh", "nvim"}, "stower: add nvim, zsh (2 files)"); err != nil {
		t.Fatal(err)
	}
	paths, err = DirtyPaths(dir, "zsh")
	if err != nil {
		t.Fatal(err)
	}
	if len(paths) != 0 {
		t.Fatalf("committed package still dirty: %v", paths)
	}

	if err := os.Remove(filepath.Join(dir, "zsh", "dot-zshrc")); err != nil {
		t.Fatal(err)
	}
	paths, err = DirtyPaths(dir, "zsh")
	if err != nil {
		t.Fatal(err)
	}
	if len(paths) != 1 || paths[0] != "zsh/dot-zshrc" {
		t.Fatalf("deletion not reported: %v", paths)
	}

	for _, pkg := range []string{"", "..", "../escape", "zsh/dot-zshrc"} {
		if _, err := DirtyPaths(dir, pkg); !errors.Is(err, ErrInvalidPathspec) {
			t.Fatalf("DirtyPaths(%q) error = %v", pkg, err)
		}
	}
}

func TestAddAndCommitCapturesDeletions(t *testing.T) {
	dir := newRepo(t)
	writePackage(t, dir, "zsh", "dot-zshrc", "dot-zprofile")
	if err := AddAndCommit(dir, []string{"zsh"}, "stower: add zsh (2 files)"); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(filepath.Join(dir, "zsh")); err != nil {
		t.Fatal(err)
	}
	if err := AddAndCommit(dir, []string{"zsh"}, "stower: remove zsh"); err != nil {
		t.Fatal(err)
	}
	log := gitOutput(t, dir, "log", "--format=%s")
	if lines := strings.Split(strings.TrimSpace(log), "\n"); len(lines) != 2 || lines[0] != "stower: remove zsh" {
		t.Fatalf("log = %q", log)
	}
	if status := gitOutput(t, dir, "status", "--short"); strings.TrimSpace(status) != "" {
		t.Fatalf("status not clean: %q", status)
	}
}

func TestAddAndCommitRejectsEmptyCommit(t *testing.T) {
	dir := newRepo(t)
	writePackage(t, dir, "zsh", "dot-zshrc")
	if err := AddAndCommit(dir, []string{"zsh"}, "stower: add zsh (1 file)"); err != nil {
		t.Fatal(err)
	}
	if err := AddAndCommit(dir, []string{"zsh"}, "stower: add zsh (1 file)"); !errors.Is(err, ErrNothingToCommit) {
		t.Fatalf("second commit error = %v", err)
	}
	log := strings.TrimSpace(gitOutput(t, dir, "log", "--oneline"))
	if len(strings.Split(log, "\n")) != 1 {
		t.Fatalf("log = %q", log)
	}
}

func TestAddAndCommitRejectsBadInput(t *testing.T) {
	dir := newRepo(t)
	writePackage(t, dir, "zsh", "dot-zshrc")
	if err := AddAndCommit(dir, nil, "stower: add zsh"); !errors.Is(err, ErrInvalidPathspec) {
		t.Fatalf("empty package list error = %v", err)
	}
	if err := AddAndCommit(dir, []string{".."}, "stower: add zsh"); !errors.Is(err, ErrInvalidPathspec) {
		t.Fatalf("traversal error = %v", err)
	}
	for _, subject := range []string{"", "   ", "line one\nline two"} {
		if err := AddAndCommit(dir, []string{"zsh"}, subject); !errors.Is(err, ErrInvalidSubject) {
			t.Fatalf("subject %q error = %v", subject, err)
		}
	}
	if _, err := Porcelain("", "zsh"); !errors.Is(err, ErrNoDirectory) {
		t.Fatalf("empty directory error = %v", err)
	}
}

func TestAddAndCommitSpecialCharacters(t *testing.T) {
	dir := newRepo(t)
	pkg := "weird name; rm -rf $HOME"
	if err := os.MkdirAll(filepath.Join(dir, pkg), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, pkg, "file"), []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	subject := `stower: add "weird name; rm -rf $HOME" (1 file) 100% --amend`
	if err := AddAndCommit(dir, []string{pkg}, subject); err != nil {
		t.Fatal(err)
	}
	if got := strings.TrimSpace(gitOutput(t, dir, "log", "--format=%s")); got != subject {
		t.Fatalf("subject = %q", got)
	}
	if status := gitOutput(t, dir, "status", "--short"); strings.TrimSpace(status) != "" {
		t.Fatalf("status not clean: %q", status)
	}
}

func TestPorcelainQuotedAndRenamedPaths(t *testing.T) {
	dir := newRepo(t)
	writePackage(t, dir, "zsh", "a file")
	if err := AddAndCommit(dir, []string{"zsh"}, "stower: add zsh (1 file)"); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(filepath.Join(dir, "zsh", "a file"), filepath.Join(dir, "zsh", "b file")); err != nil {
		t.Fatal(err)
	}
	gitOutput(t, dir, "add", "-A", "--", "zsh")
	paths, err := DirtyPaths(dir, "zsh")
	if err != nil {
		t.Fatal(err)
	}
	if len(paths) != 1 || paths[0] != "zsh/b file" {
		t.Fatalf("renamed path = %v", paths)
	}
}

func TestStatusPath(t *testing.T) {
	tests := []struct {
		line string
		want string
	}{
		{"?? zsh/dot-zshrc", "zsh/dot-zshrc"},
		{" M zsh/dot-zshrc", "zsh/dot-zshrc"},
		{"R  zsh/old -> zsh/new", "zsh/new"},
		{`?? "zsh/a\tb"`, "zsh/a\tb"},
		{"?? ", ""},
		{"", ""},
		{"??", ""},
	}
	for _, tt := range tests {
		if got := StatusPath(tt.line); got != tt.want {
			t.Errorf("StatusPath(%q) = %q, want %q", tt.line, got, tt.want)
		}
	}
}
