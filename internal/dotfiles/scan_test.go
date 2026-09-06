package dotfiles

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/horbo/stower/internal/config"
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

	writeFile(t, filepath.Join(pkgRoot, "dot-inputrc"), "absolute")
	symlink(t, filepath.Join(pkgRoot, "dot-inputrc"), filepath.Join(paths.Target, ".inputrc"))

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
		{PkgRel: "dot-inputrc", TargetRel: ".inputrc", State: Conflict},
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
	symlink(t, filepath.Join(paths.Dotfiles, "zsh", "dot-missing"), filepath.Join(paths.Target, ".broken-abs"))
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
		{name: "broken absolute link", path: ".broken-abs"},
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

func TestInspectLink(t *testing.T) {
	paths := newPaths(t)
	writeFile(t, filepath.Join(paths.Dotfiles, "zsh", "dot-zshrc"), "x")
	writeFile(t, filepath.Join(paths.Target, "plain"), "z")
	writeFile(t, filepath.Join(paths.Target, "elsewhere", "file"), "z")

	absolute := filepath.Join(paths.Dotfiles, "zsh", "dot-zshrc")
	symlink(t, "../dotfiles/zsh/dot-zshrc", filepath.Join(paths.Target, ".zshrc"))
	symlink(t, absolute, filepath.Join(paths.Target, ".zshrc-abs"))
	symlink(t, "elsewhere/file", filepath.Join(paths.Target, "foreign"))
	symlink(t, "../dotfiles/zsh/dot-missing", filepath.Join(paths.Target, ".broken"))
	symlink(t, "../dotfiles", filepath.Join(paths.Target, "root"))

	tests := []struct {
		name   string
		path   string
		want   LinkInfo
		wantOK bool
	}{
		{
			name:   "relative link as stow writes it",
			path:   ".zshrc",
			want:   LinkInfo{Target: "../dotfiles/zsh/dot-zshrc", Package: "zsh"},
			wantOK: true,
		},
		{
			name:   "absolute link",
			path:   ".zshrc-abs",
			want:   LinkInfo{Target: absolute, Package: "zsh"},
			wantOK: true,
		},
		{
			name:   "link outside dotfiles",
			path:   "foreign",
			want:   LinkInfo{Target: "elsewhere/file"},
			wantOK: true,
		},
		{
			name:   "broken link into a package",
			path:   ".broken",
			want:   LinkInfo{Target: "../dotfiles/zsh/dot-missing", Package: "zsh", Dangling: true},
			wantOK: true,
		},
		{
			name:   "link to the dotfiles root",
			path:   "root",
			want:   LinkInfo{Target: "../dotfiles"},
			wantOK: true,
		},
		{name: "regular file", path: "plain"},
		{name: "missing path", path: "nope"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, ok := InspectLink(paths, filepath.Join(paths.Target, test.path))
			if ok != test.wantOK {
				t.Fatalf("InspectLink(%q) ok = %v, want %v", test.path, ok, test.wantOK)
			}
			if !reflect.DeepEqual(got, test.want) {
				t.Errorf("InspectLink(%q) = %+v, want %+v", test.path, got, test.want)
			}
		})
	}
}

func TestInspectLinkThroughSymlinkedDotfilesRoot(t *testing.T) {
	paths := newPaths(t)
	real := filepath.Join(filepath.Dir(paths.Dotfiles), "real-dotfiles")
	if err := os.Remove(paths.Dotfiles); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(real, "zsh", "dot-zshrc"), "x")
	symlink(t, real, paths.Dotfiles)
	symlink(t, "../dotfiles/zsh/dot-zshrc", filepath.Join(paths.Target, ".zshrc"))
	symlink(t, "../dotfiles/zsh/dot-missing", filepath.Join(paths.Target, ".broken"))

	got, ok := InspectLink(paths, filepath.Join(paths.Target, ".zshrc"))
	want := LinkInfo{Target: "../dotfiles/zsh/dot-zshrc", Package: "zsh"}
	if !ok || !reflect.DeepEqual(got, want) {
		t.Errorf("InspectLink(.zshrc) = (%+v, %v), want (%+v, true)", got, ok, want)
	}

	got, ok = InspectLink(paths, filepath.Join(paths.Target, ".broken"))
	want = LinkInfo{Target: "../dotfiles/zsh/dot-missing", Package: "zsh", Dangling: true}
	if !ok || !reflect.DeepEqual(got, want) {
		t.Errorf("InspectLink(.broken) = (%+v, %v), want (%+v, true)", got, ok, want)
	}
}

func TestStowOwns(t *testing.T) {
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}

	inside := newPathsAt(t, filepath.Join(root, "home"), filepath.Join(root, "home", "dotfiles"))
	writeFile(t, filepath.Join(inside.Dotfiles, "pkg", "dot-zshrc"), "x")
	writeFile(t, filepath.Join(inside.Dotfiles, "pkg", "dot-config", "nvim", "init.lua"), "x")
	writeFile(t, filepath.Join(inside.Dotfiles, "pkg", "dot-config", "gh", "hosts.yml"), "x")
	writeFile(t, filepath.Join(inside.Dotfiles, "pkg", "dot-inputrc"), "x")
	symlink(t, filepath.Join(inside.Dotfiles, "pkg", "dot-inputrc"), filepath.Join(inside.Target, ".inputrc"))
	symlink(t, "dotfiles/pkg/dot-zshrc", filepath.Join(inside.Target, ".zshrc"))
	symlink(t, "../dotfiles/pkg/dot-config/nvim", filepath.Join(inside.Target, ".config", "nvim"))
	symlink(t, "../../dotfiles/pkg/dot-config/gh", filepath.Join(inside.Target, ".config", "gh"))

	linkedRoot := filepath.Join(root, "linked")
	real := newPathsAt(t, linkedRoot, filepath.Join(linkedRoot, "Projects", "dotfiles"))
	writeFile(t, filepath.Join(real.Dotfiles, "pkg", "dot-zshrc"), "x")
	symlink(t, filepath.Join("Projects", "dotfiles"), filepath.Join(linkedRoot, "dotfiles"))
	symlink(t, "dotfiles/pkg/dot-zshrc", filepath.Join(linkedRoot, ".zshrc"))

	outside := newPathsAt(t, filepath.Join(root, "outside", "target"), filepath.Join(root, "outside", "dotfiles"))
	writeFile(t, filepath.Join(outside.Dotfiles, "pkg", "dot-zshrc"), "x")
	symlink(t, "../dotfiles/pkg/dot-zshrc", filepath.Join(outside.Target, ".zshrc"))

	missing := config.Paths{Target: inside.Target, Dotfiles: filepath.Join(root, "gone")}

	tests := []struct {
		name   string
		paths  config.Paths
		pkgRel string
		want   bool
	}{
		{name: "absolute link", paths: inside, pkgRel: "dot-inputrc"},
		{name: "relative link at depth one", paths: inside, pkgRel: "dot-zshrc", want: true},
		{name: "relative link at depth two", paths: inside, pkgRel: "dot-config/nvim", want: true},
		{name: "wrong relative depth", paths: inside, pkgRel: "dot-config/gh"},
		{name: "written through a symlinked dotfiles path", paths: real, pkgRel: "dot-zshrc"},
		{name: "dotfiles outside the target", paths: outside, pkgRel: "dot-zshrc", want: true},
		{name: "missing dotfiles root", paths: missing, pkgRel: "dot-zshrc"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			entry := Entry{PkgRel: test.pkgRel, TargetRel: PackageToTarget(test.pkgRel)}
			if got := StowOwns(test.paths, "pkg", entry); got != test.want {
				t.Errorf("StowOwns(%q) = %v, want %v", test.pkgRel, got, test.want)
			}
		})
	}
}
