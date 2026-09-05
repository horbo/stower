package stow

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/horbo/stower/internal/config"
)

func TestParseConflicts(t *testing.T) {
	stderr := "Ignoring an absolute symlink: .config/bar => /etc/hosts\n" +
		"WARNING! stowing pkg would cause conflicts:\n" +
		"  * cannot stow ../dotfiles/pkg/dot-zshrc over existing target .zshrc since neither a link nor a directory and --adopt not specified\n" +
		"  * existing target is not owned by stow: .config/bar\n" +
		"All operations aborted.\n"

	want := []string{
		"cannot stow ../dotfiles/pkg/dot-zshrc over existing target .zshrc since neither a link nor a directory and --adopt not specified",
		"existing target is not owned by stow: .config/bar",
	}
	if got := ParseConflicts(stderr); !reflect.DeepEqual(got, want) {
		t.Errorf("ParseConflicts:\n got %q\nwant %q", got, want)
	}

	if got := ParseConflicts("LINK: .zshrc => ../dotfiles/pkg/dot-zshrc\n"); got != nil {
		t.Errorf("ParseConflicts on clean output = %q, want none", got)
	}
}

func TestValidatePackage(t *testing.T) {
	for _, pkg := range []string{"zsh", "my_pkg-2.0", ".hidden"} {
		if err := ValidatePackage(pkg); err != nil {
			t.Errorf("ValidatePackage(%q) = %v, want nil", pkg, err)
		}
	}
	for _, pkg := range []string{"", "-d", "a/b", "..", "a b", "a;b", "--target=/etc"} {
		if err := ValidatePackage(pkg); !errors.Is(err, ErrInvalidPackage) {
			t.Errorf("ValidatePackage(%q) = %v, want %v", pkg, err, ErrInvalidPackage)
		}
	}
}

func TestRunnerArguments(t *testing.T) {
	runner := Runner{Bin: filepath.Join(t.TempDir(), "missing-stow"), Dotfiles: "/d", Target: "/t"}

	tests := []struct {
		name string
		run  func() Result
		want []string
	}{
		{
			name: "dry run restow",
			run:  func() Result { return runner.DryRunRestow("zsh") },
			want: []string{runner.Bin, "--dotfiles", "-n", "-v", "-R", "-d", "/d", "-t", "/t", "zsh"},
		},
		{
			name: "restow",
			run:  func() Result { return runner.Restow("zsh", "nvim") },
			want: []string{runner.Bin, "--dotfiles", "-v", "-R", "-d", "/d", "-t", "/t", "zsh", "nvim"},
		},
		{
			name: "unstow",
			run:  func() Result { return runner.Unstow("zsh") },
			want: []string{runner.Bin, "--dotfiles", "-v", "-D", "-d", "/d", "-t", "/t", "zsh"},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			result := test.run()
			if !reflect.DeepEqual(result.Args, test.want) {
				t.Errorf("Args = %q, want %q", result.Args, test.want)
			}
			if result.Err == nil {
				t.Error("Err = nil, want an error for a missing binary")
			}
		})
	}
}

func TestRunnerRejectsPackageNames(t *testing.T) {
	runner := Runner{Bin: "stow", Dotfiles: "/d", Target: "/t"}

	if err := runner.Restow("--target=/etc").Err; !errors.Is(err, ErrInvalidPackage) {
		t.Errorf("Err = %v, want %v", err, ErrInvalidPackage)
	}
	if err := runner.Restow().Err; !errors.Is(err, ErrNoPackages) {
		t.Errorf("Err = %v, want %v", err, ErrNoPackages)
	}
}

func TestRunnerConflictResult(t *testing.T) {
	bin := fakeStow(t, 1, "WARNING! stowing zsh would cause conflicts:\n  * existing target is not owned by stow: .zshrc\nAll operations aborted.")
	runner := Runner{Bin: bin, Dotfiles: "/d", Target: "/t"}

	result := runner.DryRunRestow("zsh")
	if !result.HasConflicts() || len(result.Conflicts) != 1 {
		t.Fatalf("Conflicts = %q, want one of them", result.Conflicts)
	}
	if !errors.Is(result.Err, ErrConflict) {
		t.Errorf("Err = %v, want %v", result.Err, ErrConflict)
	}
	if !strings.Contains(result.Command(), "--dotfiles -n -v -R") {
		t.Errorf("Command() = %q", result.Command())
	}
}

func TestRunnerPlainFailure(t *testing.T) {
	bin := fakeStow(t, 2, "stow: ERROR: The stow directory /d does not contain package zsh")
	runner := Runner{Bin: bin, Dotfiles: "/d", Target: "/t"}

	result := runner.Restow("zsh")
	if result.HasConflicts() {
		t.Errorf("Conflicts = %q, want none", result.Conflicts)
	}
	if errors.Is(result.Err, ErrConflict) {
		t.Errorf("Err = %v, want a plain error", result.Err)
	}
	if !strings.Contains(result.Err.Error(), "does not contain package zsh") {
		t.Errorf("Err = %v, want the stderr in the message", result.Err)
	}
}

func TestRunnerSuccess(t *testing.T) {
	bin := fakeStow(t, 0, "LINK: .zshrc => ../dotfiles/zsh/dot-zshrc")
	runner := Runner{Bin: bin, Dotfiles: "/d", Target: "/t"}

	result := runner.Restow("zsh")
	if result.Err != nil {
		t.Fatalf("Err = %v, want nil", result.Err)
	}
	want := []string{"LINK: .zshrc => ../dotfiles/zsh/dot-zshrc"}
	if got := result.OutputLines(); !reflect.DeepEqual(got, want) {
		t.Errorf("OutputLines = %q, want %q", got, want)
	}
}

func TestRealStowWritesEverythingToStderr(t *testing.T) {
	requireStow(t)
	root := t.TempDir()
	target := filepath.Join(root, "target")
	dotfiles := filepath.Join(root, "dotfiles")
	if err := os.MkdirAll(filepath.Join(dotfiles, "zsh"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(target, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dotfiles, "zsh", "dot-zshrc"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	runner := Runner{Bin: "stow", Dotfiles: dotfiles, Target: target}
	result := runner.Restow("zsh")
	if result.Err != nil {
		t.Fatalf("Restow: %v", result.Err)
	}
	if result.Stdout != "" {
		t.Errorf("Stdout = %q, want stow to write only to stderr", result.Stdout)
	}
	if !strings.Contains(result.Stderr, "LINK: .zshrc") {
		t.Errorf("Stderr = %q, want a LINK line", result.Stderr)
	}
}

func fakeStow(t *testing.T, code int, message string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "fake-stow")
	script := "#!/bin/sh\ncat >&2 <<'EOF'\n" + message + "\nEOF\nexit " + strconv.Itoa(code) + "\n"
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

func requireStow(t *testing.T) string {
	t.Helper()
	bin, version, err := config.StowBinary()
	if err != nil {
		t.Skip("stow is not installed")
	}
	if !stowSupportsDotfiles(version) {
		t.Skipf("stow %s is too old: these tests need stow 2.4 or newer", version)
	}
	return bin
}

func stowSupportsDotfiles(version string) bool {
	parts := strings.SplitN(version, ".", 3)
	if len(parts) < 2 {
		return false
	}
	major, err := strconv.Atoi(parts[0])
	if err != nil {
		return false
	}
	minor, err := strconv.Atoi(parts[1])
	if err != nil {
		return false
	}
	return major > 2 || (major == 2 && minor >= 4)
}

func TestRestowExcludingArguments(t *testing.T) {
	runner := Runner{Bin: filepath.Join(t.TempDir(), "missing-stow"), Dotfiles: "/d", Target: "/t"}

	result := runner.RestowExcluding("p", []string{"dot-bar", "dot-config/app", "plain/dot-keep"})
	want := []string{runner.Bin, "--dotfiles", "-v", "-R",
		"--ignore=^dot-bar$", "--ignore=^\\.config/app$", "--ignore=^plain/dot-keep$",
		"-d", "/d", "-t", "/t", "p"}
	if !reflect.DeepEqual(result.Args, want) {
		t.Errorf("Args = %q, want %q", result.Args, want)
	}

	for _, bad := range []string{"", ".", "..", "../x", "/abs"} {
		if err := runner.RestowExcluding("p", []string{bad}).Err; err == nil {
			t.Errorf("RestowExcluding(%q) accepted an invalid entry", bad)
		}
	}
}

func TestRestowExcludingWithRealStow(t *testing.T) {
	bin := requireStow(t)
	root := t.TempDir()
	dotfiles := filepath.Join(root, "dotfiles")
	target := filepath.Join(root, "home")
	pkg := filepath.Join(dotfiles, "p")
	for _, dir := range []string{filepath.Join(pkg, "dot-config", "app"), filepath.Join(target, ".config", "app")} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	files := map[string]string{
		filepath.Join(pkg, "dot-bar"):                   "repo bar",
		filepath.Join(pkg, "plain"):                     "repo plain",
		filepath.Join(pkg, "dot-config", "app", "conf"): "repo conf",
		filepath.Join(target, ".bar"):                   "target bar",
		filepath.Join(target, ".config", "app", "conf"): "target conf",
	}
	for path, content := range files {
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	runner := Runner{Bin: bin, Dotfiles: dotfiles, Target: target}
	result := runner.RestowExcluding("p", []string{"dot-bar", "dot-config/app"})
	if result.Err != nil {
		t.Fatalf("Err = %v; stderr:\n%s", result.Err, result.Stderr)
	}
	if result.HasConflicts() {
		t.Fatalf("Conflicts = %q, want none", result.Conflicts)
	}

	for _, path := range []string{filepath.Join(target, ".bar"), filepath.Join(target, ".config", "app")} {
		info, err := os.Lstat(path)
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode()&os.ModeSymlink != 0 {
			t.Errorf("%s became a symlink; the excluded entry was stowed", path)
		}
	}
	got, err := os.ReadFile(filepath.Join(target, ".bar"))
	if err != nil || string(got) != "target bar" {
		t.Errorf(".bar content = %q, %v; want the untouched target copy", got, err)
	}
	info, err := os.Lstat(filepath.Join(target, "plain"))
	if err != nil {
		t.Fatalf("plain was not linked: %v", err)
	}
	if info.Mode()&os.ModeSymlink == 0 {
		t.Errorf("plain is not a symlink; unaffected entries must be restowed")
	}
}
