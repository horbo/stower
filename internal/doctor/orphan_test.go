package doctor

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/horbo/stower/internal/config"
)

func orphanFixture(t *testing.T) config.Paths {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed")
	}
	t.Setenv("GIT_CONFIG_GLOBAL", filepath.Join(t.TempDir(), "absent"))
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	paths, runner := fixture(t)
	doctorGit(t, paths.Dotfiles, "init")
	put(t, filepath.Join(paths.Dotfiles, "zsh", "dot-oh-my-zsh", "plugins", "kept", "kept.zsh"), "k\n")
	put(t, filepath.Join(paths.Dotfiles, "vim", "dot-vimrc"), "v\n")
	for name, path := range map[string]string{"gone": "zsh/dot-oh-my-zsh/plugins/gone", "kept": "zsh/dot-oh-my-zsh/plugins/kept", "vim": "vim/dot-vim/pack"} {
		doctorGit(t, paths.Dotfiles, "config", "--file", ".gitmodules", "submodule."+name+".path", path)
		doctorGit(t, paths.Dotfiles, "config", "--file", ".gitmodules", "submodule."+name+".url", "https://example.invalid/"+name+".git")
	}
	doctorGit(t, paths.Dotfiles, "config", "submodule.gone.url", "https://example.invalid/gone.git")
	doctorGit(t, paths.Dotfiles, "add", ".")
	doctorGit(t, paths.Dotfiles, "commit", "-m", "initial")
	for _, pkg := range []string{"zsh", "vim"} {
		if result := runner.Restow(pkg); result.Err != nil {
			t.Fatal(result.Err)
		}
	}
	return paths
}

func TestOrphanedSubmoduleIssue(t *testing.T) {
	paths := orphanFixture(t)
	issue := issueFor(t, paths, "zsh", Orphaned)
	want := []string{"zsh/dot-oh-my-zsh/plugins/gone", "zsh/dot-oh-my-zsh/plugins/kept"}
	if !issue.Fixable || !reflect.DeepEqual(issue.Modules, want) {
		t.Fatalf("issue: %+v", issue)
	}
	plan := BuildRestorePlan(paths, "zsh")
	if len(plan.Blocked) != 1 || plan.Blocked[0].Path != filepath.Join(paths.Dotfiles, want[1]) {
		t.Fatalf("blocked: %+v", plan.Blocked)
	}
	if summary := RemoveOrphanedSubmodules(context.Background(), paths, "zsh", nil); !summary.OK() {
		t.Fatal(summary.Err())
	}
	if plan := BuildRestorePlan(paths, "zsh"); !plan.Runnable() {
		t.Fatalf("restore after fix: %+v", plan.Blocked)
	}
}

func TestRemoveOrphanedSubmodules(t *testing.T) {
	paths := orphanFixture(t)
	summary := RemoveOrphanedSubmodules(context.Background(), paths, "zsh", nil)
	if !summary.OK() {
		t.Fatal(summary.Err())
	}
	if !reflect.DeepEqual(summary.GitPaths, []string{".gitmodules"}) {
		t.Fatalf("GitPaths: %v", summary.GitPaths)
	}
	modules := doctorGit(t, paths.Dotfiles, "config", "--file", ".gitmodules", "--get-regexp", `\.path$`)
	if strings.Contains(modules, "zsh/") || !strings.Contains(modules, "vim/dot-vim/pack") {
		t.Fatalf(".gitmodules: %q", modules)
	}
	if out, err := exec.Command("git", "-C", paths.Dotfiles, "config", "--get", "submodule.gone.url").Output(); err == nil {
		t.Fatalf("local config kept: %q", out)
	}
	if staged := doctorGit(t, paths.Dotfiles, "diff", "--cached", "--name-only"); staged != ".gitmodules\n" {
		t.Fatalf("staged: %q", staged)
	}
	if got := readText(t, filepath.Join(paths.Dotfiles, "zsh", "dot-oh-my-zsh", "plugins", "kept", "kept.zsh")); got != "k\n" {
		t.Fatal(got)
	}
	for _, issue := range InspectPackage(paths, "zsh").Entries {
		if issue.State == Orphaned {
			t.Fatalf("still orphaned: %+v", issue)
		}
	}
	issueFor(t, paths, "vim", Orphaned)
}

func TestRemoveOrphanedSubmodulesRemovesEmptyGitmodules(t *testing.T) {
	paths := orphanFixture(t)
	for _, pkg := range []string{"zsh", "vim"} {
		if summary := RemoveOrphanedSubmodules(context.Background(), paths, pkg, nil); !summary.OK() {
			t.Fatal(summary.Err())
		}
	}
	if _, err := os.Lstat(filepath.Join(paths.Dotfiles, ".gitmodules")); !os.IsNotExist(err) {
		t.Fatalf(".gitmodules still exists: %v", err)
	}
	if status := doctorGit(t, paths.Dotfiles, "status", "--porcelain", "--", ".gitmodules"); status != "D  .gitmodules\n" {
		t.Fatalf("status: %q", status)
	}
}

func TestRemoveOrphanedSubmodulesRefusesDirtyGitmodules(t *testing.T) {
	paths := orphanFixture(t)
	file := filepath.Join(paths.Dotfiles, ".gitmodules")
	doctorGit(t, paths.Dotfiles, "config", "--file", ".gitmodules", "submodule.extra.path", "zsh/extra")
	before := readText(t, file)
	summary := RemoveOrphanedSubmodules(context.Background(), paths, "zsh", nil)
	if summary.OK() || !strings.Contains(summary.Err().Error(), ".gitmodules") {
		t.Fatalf("summary: %+v", summary)
	}
	if after := readText(t, file); after != before {
		t.Fatalf(".gitmodules changed:\n%s", after)
	}
}

func readText(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}
