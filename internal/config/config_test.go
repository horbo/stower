package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func envFrom(values map[string]string) func(string) string {
	return func(name string) string { return values[name] }
}

func TestResolveDotfilesPrecedence(t *testing.T) {
	home := t.TempDir()

	tests := []struct {
		name         string
		flagDotfiles string
		envDotfiles  string
		want         string
	}{
		{
			name:         "flag beats env and default",
			flagDotfiles: filepath.Join(home, "from-flag"),
			envDotfiles:  filepath.Join(home, "from-env"),
			want:         filepath.Join(home, "from-flag"),
		},
		{
			name:        "env beats default",
			envDotfiles: filepath.Join(home, "from-env"),
			want:        filepath.Join(home, "from-env"),
		},
		{
			name: "default is ~/dotfiles",
			want: filepath.Join(home, "dotfiles"),
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			env := envFrom(map[string]string{"HOME": home, EnvDotfiles: test.envDotfiles})
			paths, err := Resolve(test.flagDotfiles, "", env)
			if err != nil {
				t.Fatalf("Resolve: %v", err)
			}
			if paths.Dotfiles != test.want {
				t.Errorf("Dotfiles = %q, want %q", paths.Dotfiles, test.want)
			}
		})
	}
}

func TestResolveTargetPrecedence(t *testing.T) {
	home := t.TempDir()
	other := t.TempDir()
	env := envFrom(map[string]string{"HOME": home})

	paths, err := Resolve("", other, env)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if paths.Target != evalSymlinks(t, other) {
		t.Errorf("Target = %q, want %q", paths.Target, evalSymlinks(t, other))
	}

	paths, err = Resolve("", "", env)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if paths.Target != evalSymlinks(t, home) {
		t.Errorf("Target = %q, want %q", paths.Target, evalSymlinks(t, home))
	}
}

func TestResolveExpandsTilde(t *testing.T) {
	home := t.TempDir()
	env := envFrom(map[string]string{"HOME": home})

	paths, err := Resolve("~/repo/dots", "~", env)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if want := filepath.Join(home, "repo", "dots"); paths.Dotfiles != want {
		t.Errorf("Dotfiles = %q, want %q", paths.Dotfiles, want)
	}
	if want := evalSymlinks(t, home); paths.Target != want {
		t.Errorf("Target = %q, want %q", paths.Target, want)
	}
}

func TestResolveTildeWithoutHome(t *testing.T) {
	env := envFrom(map[string]string{})
	if _, err := Resolve("~/dots", "/tmp", env); err == nil {
		t.Fatal("Resolve: want an error when HOME is not set")
	}
}

func TestResolveWithoutHomeAndWithoutFlags(t *testing.T) {
	env := envFrom(map[string]string{})
	if _, err := Resolve("", "", env); err == nil {
		t.Fatal("Resolve: want an error when HOME is not set")
	}
}

func TestResolveCanonicalisesSymlinks(t *testing.T) {
	root := evalSymlinks(t, t.TempDir())
	real := filepath.Join(root, "real")
	if err := os.MkdirAll(filepath.Join(real, "inner"), 0o755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "link")
	if err := os.Symlink(real, link); err != nil {
		t.Fatal(err)
	}

	env := envFrom(map[string]string{"HOME": root})
	paths, err := Resolve(filepath.Join(link, "inner"), link, env)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if want := filepath.Join(real, "inner"); paths.Dotfiles != want {
		t.Errorf("Dotfiles = %q, want %q", paths.Dotfiles, want)
	}
	if paths.Target != real {
		t.Errorf("Target = %q, want %q", paths.Target, real)
	}
}

func TestResolveKeepsMissingPathUnresolved(t *testing.T) {
	root := evalSymlinks(t, t.TempDir())
	env := envFrom(map[string]string{"HOME": root})

	missing := filepath.Join(root, "not-there")
	paths, err := Resolve(missing, root, env)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if paths.Dotfiles != missing {
		t.Errorf("Dotfiles = %q, want %q", paths.Dotfiles, missing)
	}
}

func TestResolveRejectsDotfilesUnderNonDirectory(t *testing.T) {
	root := evalSymlinks(t, t.TempDir())
	file := filepath.Join(root, "file")
	if err := os.WriteFile(file, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	env := envFrom(map[string]string{"HOME": root})

	if _, err := Resolve(file, root, env); err == nil {
		t.Error("Resolve: want an error for a dotfiles path that is a regular file")
	}
	if _, err := Resolve(filepath.Join(file, "pkg"), root, env); err == nil {
		t.Error("Resolve: want an error for a dotfiles path nested inside a regular file")
	}
}

func TestResolveRejectsUnreadableDotfilesParent(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("running as root ignores directory permissions")
	}
	root := evalSymlinks(t, t.TempDir())
	locked := filepath.Join(root, "locked")
	if err := os.Mkdir(locked, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(locked, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(locked, 0o755) })

	env := envFrom(map[string]string{"HOME": root})
	_, err := Resolve(filepath.Join(locked, "dots"), root, env)
	if err == nil {
		t.Fatal("Resolve: want an error for an unreadable parent directory")
	}
	if !strings.Contains(err.Error(), "not accessible") {
		t.Errorf("error = %v, want it to mention that the path is not accessible", err)
	}
}

func TestResolveMakesRelativePathsAbsolute(t *testing.T) {
	root := evalSymlinks(t, t.TempDir())
	env := envFrom(map[string]string{"HOME": root})

	paths, err := Resolve("relative/dots", root, env)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if !filepath.IsAbs(paths.Dotfiles) {
		t.Errorf("Dotfiles = %q, want an absolute path", paths.Dotfiles)
	}
}

func TestMouseEnabled(t *testing.T) {
	tests := []struct {
		name string
		flag bool
		env  string
		want bool
	}{
		{name: "enabled by default", want: true},
		{name: "flag disables", flag: true, want: false},
		{name: "env 1 disables", env: "1", want: false},
		{name: "env true disables", env: "TRUE", want: false},
		{name: "env 0 keeps it enabled", env: "0", want: true},
		{name: "env off keeps it enabled", env: " off ", want: true},
		{name: "flag wins over env", flag: true, env: "0", want: false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			env := envFrom(map[string]string{EnvNoMouse: test.env})
			if got := MouseEnabled(test.flag, env); got != test.want {
				t.Fatalf("MouseEnabled(%v, %q) = %v, want %v", test.flag, test.env, got, test.want)
			}
		})
	}
}

func evalSymlinks(t *testing.T, path string) string {
	t.Helper()
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		t.Fatalf("EvalSymlinks(%q): %v", path, err)
	}
	return resolved
}
