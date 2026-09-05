package dotfiles

import (
	"path/filepath"
	"reflect"
	"testing"
)

func TestBuildAdoptPlanGroupsByPackage(t *testing.T) {
	paths := newPaths(t)
	writeFile(t, filepath.Join(paths.Target, ".zshrc"), "zsh")
	writeFile(t, filepath.Join(paths.Target, ".config", "foo", "conf"), "foo")
	writeFile(t, filepath.Join(paths.Target, ".claude", "settings.json"), "claude")

	plan := BuildAdoptPlan(paths, Staging{
		filepath.Join(paths.Target, ".zshrc"):                   "zsh",
		filepath.Join(paths.Target, ".config", "foo"):           "foo",
		filepath.Join(paths.Target, ".claude", "settings.json"): "claude",
	})

	if plan.Fatal != nil {
		t.Fatalf("plan.Fatal = %v", plan.Fatal)
	}
	if len(plan.Packages) != 3 {
		t.Fatalf("packages = %d, want 3", len(plan.Packages))
	}
	names := []string{plan.Packages[0].Package, plan.Packages[1].Package, plan.Packages[2].Package}
	if want := []string{"claude", "foo", "zsh"}; !reflect.DeepEqual(names, want) {
		t.Errorf("package order = %v, want %v", names, want)
	}

	wantMoves := map[string]Move{
		"zsh": {
			From: filepath.Join(paths.Target, ".zshrc"),
			To:   filepath.Join(paths.Dotfiles, "zsh", "dot-zshrc"),
		},
		"foo": {
			From: filepath.Join(paths.Target, ".config", "foo"),
			To:   filepath.Join(paths.Dotfiles, "foo", "dot-config", "foo"),
		},
		"claude": {
			From: filepath.Join(paths.Target, ".claude", "settings.json"),
			To:   filepath.Join(paths.Dotfiles, "claude", "dot-claude", "settings.json"),
		},
	}
	for _, pkg := range plan.Packages {
		if len(pkg.Moves) != 1 {
			t.Fatalf("%s moves = %d, want 1", pkg.Package, len(pkg.Moves))
		}
		if got := pkg.Moves[0]; got != wantMoves[pkg.Package] {
			t.Errorf("%s move = %+v, want %+v", pkg.Package, got, wantMoves[pkg.Package])
		}
	}

	zsh := plan.Packages[2]
	wantLink := Link{
		TargetPath: filepath.Join(paths.Target, ".zshrc"),
		LinkText:   filepath.Join("..", "dotfiles", "zsh", "dot-zshrc"),
	}
	if got := zsh.ExpectedLinks[0]; got != wantLink {
		t.Errorf("expected link = %+v, want %+v", got, wantLink)
	}
	if plan.MoveCount() != 3 || !plan.Runnable() {
		t.Errorf("MoveCount = %d, Runnable = %v", plan.MoveCount(), plan.Runnable())
	}
}

func TestBuildAdoptPlanBlocksAndWarns(t *testing.T) {
	paths := newPaths(t)
	writeFile(t, filepath.Join(paths.Target, ".zshrc"), "zsh")
	writeFile(t, filepath.Join(paths.Dotfiles, "zsh", "dot-zshrc"), "already there")
	writeFile(t, filepath.Join(paths.Target, ".vimrc"), "vim")
	writeFile(t, filepath.Join(paths.Target, ".repo", ".git", "config"), "git")
	symlink(t, "../dotfiles/zsh/dot-zshrc", filepath.Join(paths.Target, ".link"))

	plan := BuildAdoptPlan(paths, Staging{
		filepath.Join(paths.Target, ".zshrc"): "zsh",
		filepath.Join(paths.Target, ".link"):  "zsh",
		filepath.Join(paths.Target, ".vimrc"): ".hidden",
		filepath.Join(paths.Target, ".repo"):  "repo",
	})

	byName := map[string]PackageAdopt{}
	for _, pkg := range plan.Packages {
		byName[pkg.Package] = pkg
	}

	if got := byName["zsh"]; len(got.Blocked) != 2 || len(got.Moves) != 0 {
		t.Errorf("zsh blocked = %d, moves = %d, want 2 and 0", len(got.Blocked), len(got.Moves))
	}
	if got := byName[".hidden"]; len(got.Blocked) != 1 {
		t.Errorf(".hidden blocked = %d, want 1", len(got.Blocked))
	}
	repo := byName["repo"]
	if len(repo.Moves) != 1 || len(repo.Warnings) != 1 {
		t.Fatalf("repo moves = %d, warnings = %d, want 1 and 1", len(repo.Moves), len(repo.Warnings))
	}
	if repo.RemoveNestedGit {
		t.Error("RemoveNestedGit is on by default, want it off")
	}
	if plan.BlockedCount() != 3 {
		t.Errorf("BlockedCount = %d, want 3", plan.BlockedCount())
	}
}

func TestBuildAdoptPlanDeduplicatesDescendants(t *testing.T) {
	paths := newPaths(t)
	writeFile(t, filepath.Join(paths.Target, ".config", "foo", "conf"), "x")

	plan := BuildAdoptPlan(paths, Staging{
		filepath.Join(paths.Target, ".config"):                "cfg",
		filepath.Join(paths.Target, ".config", "foo"):         "cfg",
		filepath.Join(paths.Target, ".config", "foo", "conf"): "cfg",
	})

	if plan.MoveCount() != 1 {
		t.Fatalf("MoveCount = %d, want 1", plan.MoveCount())
	}
	if want := filepath.Join(paths.Target, ".config"); plan.Packages[0].Moves[0].From != want {
		t.Errorf("move From = %q, want %q", plan.Packages[0].Moves[0].From, want)
	}
	if len(plan.Messages) != 2 {
		t.Errorf("messages = %v, want 2 of them", plan.Messages)
	}
}

func TestBuildRestorePlan(t *testing.T) {
	paths := newPaths(t)
	writeFile(t, filepath.Join(paths.Dotfiles, "zsh", "dot-zshrc"), "x")
	writeFile(t, filepath.Join(paths.Dotfiles, "zsh", "dot-vimrc"), "y")
	symlink(t, "../dotfiles/zsh/dot-zshrc", filepath.Join(paths.Target, ".zshrc"))

	plan := BuildRestorePlan(paths, "zsh")
	if plan.Fatal != nil {
		t.Fatalf("plan.Fatal = %v", plan.Fatal)
	}
	if len(plan.Moves) != 2 || len(plan.Blocked) != 0 || !plan.Runnable() {
		t.Fatalf("moves = %d, blocked = %d, runnable = %v", len(plan.Moves), len(plan.Blocked), plan.Runnable())
	}
	want := Move{
		From: filepath.Join(paths.Dotfiles, "zsh", "dot-vimrc"),
		To:   filepath.Join(paths.Target, ".vimrc"),
	}
	if plan.Moves[0] != want {
		t.Errorf("move = %+v, want %+v", plan.Moves[0], want)
	}
	if want := filepath.Join(paths.Dotfiles, "zsh"); plan.RemoveDir != want {
		t.Errorf("RemoveDir = %q, want %q", plan.RemoveDir, want)
	}
}

func TestBuildRestorePlanBlockedByConflict(t *testing.T) {
	paths := newPaths(t)
	writeFile(t, filepath.Join(paths.Dotfiles, "zsh", "dot-zshrc"), "repo")
	writeFile(t, filepath.Join(paths.Target, ".zshrc"), "mine")

	plan := BuildRestorePlan(paths, "zsh")
	if len(plan.Blocked) != 1 || plan.Runnable() {
		t.Fatalf("blocked = %d, runnable = %v, want 1 and false", len(plan.Blocked), plan.Runnable())
	}
}

func TestBuildRestorePlanUnknownPackage(t *testing.T) {
	paths := newPaths(t)
	plan := BuildRestorePlan(paths, "missing")
	if plan.Fatal == nil || plan.Runnable() {
		t.Error("BuildRestorePlan: want a fatal error for a package that does not exist")
	}
}
