package dotfiles

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestListPackages(t *testing.T) {
	paths := newPaths(t)
	mkdir(t, filepath.Join(paths.Dotfiles, "zsh"))
	mkdir(t, filepath.Join(paths.Dotfiles, "nvim"))
	mkdir(t, filepath.Join(paths.Dotfiles, ".git"))
	writeFile(t, filepath.Join(paths.Dotfiles, "README.md"), "x")

	got, err := ListPackages(paths)
	if err != nil {
		t.Fatalf("ListPackages: %v", err)
	}
	want := []string{"nvim", "zsh"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("ListPackages = %v, want %v", got, want)
	}
}

func TestListPackagesMissingDotfiles(t *testing.T) {
	paths := newPaths(t)
	if err := os.Remove(paths.Dotfiles); err != nil {
		t.Fatal(err)
	}

	got, err := ListPackages(paths)
	if err != nil {
		t.Fatalf("ListPackages: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("ListPackages = %v, want none", got)
	}
}

func TestWalkPackageStates(t *testing.T) {
	paths := newPaths(t)
	pkgRoot := filepath.Join(paths.Dotfiles, "mixed")

	writeFile(t, filepath.Join(pkgRoot, "dot-zshrc"), "linked")
	symlink(t, "../dotfiles/mixed/dot-zshrc", filepath.Join(paths.Target, ".zshrc"))

	writeFile(t, filepath.Join(pkgRoot, "dot-vimrc"), "unlinked")

	writeFile(t, filepath.Join(pkgRoot, "dot-bashrc"), "conflict")
	writeFile(t, filepath.Join(paths.Target, ".bashrc"), "mine")

	writeFile(t, filepath.Join(pkgRoot, "dot-profile"), "foreign")
	symlink(t, "/etc/hosts", filepath.Join(paths.Target, ".profile"))

	writeFile(t, filepath.Join(pkgRoot, "dot-config", "nvim", "init.lua"), "nvim")
	symlink(t, "../../dotfiles/mixed/dot-config/nvim", filepath.Join(paths.Target, ".config", "nvim"))
	writeFile(t, filepath.Join(pkgRoot, "dot-config", "gh", "hosts.yml"), "gh")

	got, err := WalkPackage(paths, "mixed")
	if err != nil {
		t.Fatalf("WalkPackage: %v", err)
	}

	want := []Entry{
		{PkgRel: "dot-bashrc", TargetRel: ".bashrc", State: Conflict},
		{PkgRel: "dot-config/gh", TargetRel: ".config/gh", IsDir: true, State: Unlinked},
		{PkgRel: "dot-config/nvim", TargetRel: ".config/nvim", IsDir: true, State: Linked},
		{PkgRel: "dot-profile", TargetRel: ".profile", State: Conflict},
		{PkgRel: "dot-vimrc", TargetRel: ".vimrc", State: Unlinked},
		{PkgRel: "dot-zshrc", TargetRel: ".zshrc", State: Linked},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("WalkPackage entries:\n got %+v\nwant %+v", got, want)
	}
}

func TestWalkPackageStopsAtLinkPoints(t *testing.T) {
	paths := newPaths(t)
	writeFile(t, filepath.Join(paths.Dotfiles, "nvim", "dot-config", "nvim", "init.lua"), "x")
	writeFile(t, filepath.Join(paths.Dotfiles, "nvim", "dot-config", "nvim", "lua", "opts.lua"), "y")
	symlink(t, "../dotfiles/nvim/dot-config", filepath.Join(paths.Target, ".config"))

	got, err := WalkPackage(paths, "nvim")
	if err != nil {
		t.Fatalf("WalkPackage: %v", err)
	}
	want := []Entry{{PkgRel: "dot-config", TargetRel: ".config", IsDir: true, State: Linked}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("WalkPackage entries:\n got %+v\nwant %+v", got, want)
	}
}

func TestWalkPackageInvalidName(t *testing.T) {
	paths := newPaths(t)
	if _, err := WalkPackage(paths, "../escape"); err == nil {
		t.Error("WalkPackage: want an error for a name outside the dotfiles directory")
	}
	if _, err := WalkPackage(paths, "missing"); err == nil {
		t.Error("WalkPackage: want an error for a package that does not exist")
	}
}

func TestManagedBy(t *testing.T) {
	paths := newPaths(t)
	writeFile(t, filepath.Join(paths.Dotfiles, "zsh", "dot-zshrc"), "x")
	writeFile(t, filepath.Join(paths.Dotfiles, "nvim", "dot-config", "nvim", "init.lua"), "y")
	writeFile(t, filepath.Join(paths.Target, "plain"), "z")
	writeFile(t, filepath.Join(paths.Target, "elsewhere", "file"), "z")

	symlink(t, "../dotfiles/zsh/dot-zshrc", filepath.Join(paths.Target, ".zshrc"))
	symlink(t, "../../dotfiles/nvim/dot-config/nvim", filepath.Join(paths.Target, ".config", "nvim"))
	symlink(t, filepath.Join(paths.Dotfiles, "zsh", "dot-zshrc"), filepath.Join(paths.Target, ".zshrc-abs"))
	symlink(t, "elsewhere/file", filepath.Join(paths.Target, "foreign"))
	symlink(t, "../dotfiles/zsh/dot-missing", filepath.Join(paths.Target, ".broken"))
	symlink(t, "../dotfiles", filepath.Join(paths.Target, "root"))

	tests := []struct {
		name    string
		path    string
		wantPkg string
		wantOK  bool
	}{
		{name: "relative link as stow writes it", path: ".zshrc", wantPkg: "zsh", wantOK: true},
		{name: "deeper relative link", path: ".config/nvim", wantPkg: "nvim", wantOK: true},
		{name: "absolute link", path: ".zshrc-abs", wantPkg: "zsh", wantOK: true},
		{name: "link outside dotfiles", path: "foreign"},
		{name: "broken link", path: ".broken"},
		{name: "link to the dotfiles root", path: "root"},
		{name: "regular file", path: "plain"},
		{name: "missing path", path: "nope"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			pkg, ok := ManagedBy(paths, filepath.Join(paths.Target, test.path))
			if ok != test.wantOK || pkg != test.wantPkg {
				t.Errorf("ManagedBy(%q) = (%q, %v), want (%q, %v)", test.path, pkg, ok, test.wantPkg, test.wantOK)
			}
		})
	}
}

func TestManagedByThroughSymlinkedDotfilesRoot(t *testing.T) {
	paths := newPaths(t)
	real := filepath.Join(filepath.Dir(paths.Dotfiles), "real-dotfiles")
	if err := os.Remove(paths.Dotfiles); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(real, "zsh", "dot-zshrc"), "x")
	symlink(t, real, paths.Dotfiles)
	symlink(t, "../dotfiles/zsh/dot-zshrc", filepath.Join(paths.Target, ".zshrc"))

	pkg, ok := ManagedBy(paths, filepath.Join(paths.Target, ".zshrc"))
	if !ok || pkg != "zsh" {
		t.Errorf("ManagedBy = (%q, %v), want (%q, true)", pkg, ok, "zsh")
	}
}
