package doctor

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/horbo/stower/internal/config"
	"github.com/horbo/stower/internal/dotfiles"
)

func gitlinkFixture(t *testing.T) (config.Paths, map[string]string) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed")
	}
	t.Setenv("GIT_CONFIG_GLOBAL", filepath.Join(t.TempDir(), "absent"))
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	paths, runner := fixture(t)
	doctorGit(t, paths.Dotfiles, "init")
	plugins := filepath.Join(paths.Dotfiles, "zsh", "dot-oh-my-zsh", "plugins")
	put(t, filepath.Join(paths.Dotfiles, "zsh", "dot-zshrc"), "z\n")
	sources := map[string]string{}
	for _, name := range []string{"remote", "local"} {
		dir := filepath.Join(plugins, name)
		put(t, filepath.Join(dir, name+".zsh"), name+"\n")
		doctorGit(t, dir, "init")
		doctorGit(t, dir, "add", ".")
		doctorGit(t, dir, "commit", "-m", "initial")
		sources[name] = dir
	}
	doctorGit(t, sources["remote"], "remote", "add", "origin", "https://example.invalid/remote.git")
	doctorGit(t, paths.Dotfiles, "add", ".")
	doctorGit(t, paths.Dotfiles, "commit", "-m", "initial")
	sha := strings.TrimSpace(doctorGit(t, sources["remote"], "rev-parse", "HEAD"))
	doctorGit(t, paths.Dotfiles, "update-index", "--add", "--cacheinfo", "160000,"+sha+",zsh/dot-oh-my-zsh/plugins/gone")
	doctorGit(t, paths.Dotfiles, "commit", "-m", "gone")
	sources["gone"] = filepath.Join(plugins, "gone")
	if result := runner.Restow("zsh"); result.Err != nil {
		t.Fatal(result.Err)
	}
	return paths, sources
}

func TestInspectGitlinksDefaults(t *testing.T) {
	paths, sources := gitlinkFixture(t)
	issue := issueFor(t, paths, "zsh", Invisible)
	if !issue.Fixable || len(issue.Gitlinks) != 3 {
		t.Fatalf("issue: %+v", issue)
	}
	rows, err := InspectGitlinks(context.Background(), paths, "zsh")
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]dotfiles.RepositoryAction{
		sources["remote"]: dotfiles.ConvertRepository,
		sources["local"]:  dotfiles.KeepRepository,
		sources["gone"]:   dotfiles.RemoveGitlink,
	}
	if len(rows) != len(want) {
		t.Fatalf("rows: %+v", rows)
	}
	for _, row := range rows {
		if row.Choice.Action != want[row.Source] {
			t.Fatalf("%s: action %v, want %v", row.Source, row.Choice.Action, want[row.Source])
		}
	}
	if _, err := InspectGitlinks(context.Background(), paths, "other"); err == nil {
		t.Fatal("a package without Git links returned rows")
	}
}

func TestRepairGitlinks(t *testing.T) {
	paths, sources := gitlinkFixture(t)
	choices := map[string]dotfiles.RepositoryChoice{
		sources["remote"]: {Action: dotfiles.ConvertRepository, URL: "https://example.invalid/remote.git"},
		sources["local"]:  {Action: dotfiles.RemoveRepositoryGit},
		sources["gone"]:   {Action: dotfiles.RemoveGitlink},
	}
	summary := RepairGitlinks(context.Background(), paths, "zsh", choices, nil)
	if len(summary.Failed) != 0 || len(summary.Succeeded) != 1 {
		t.Fatalf("summary: %+v", summary)
	}
	if len(summary.GitPaths) != 1 || summary.GitPaths[0] != ".gitmodules" {
		t.Fatalf("git paths: %v", summary.GitPaths)
	}
	for _, item := range InspectPackage(paths, "zsh").Entries {
		if item.State == Invisible {
			t.Fatalf("still invisible: %+v", item)
		}
	}
	modules, err := os.ReadFile(filepath.Join(paths.Dotfiles, ".gitmodules"))
	if err != nil || !strings.Contains(string(modules), "path = zsh/dot-oh-my-zsh/plugins/remote") {
		t.Fatalf(".gitmodules: %q %v", modules, err)
	}
	if _, err := os.Stat(filepath.Join(sources["local"], ".git")); !os.IsNotExist(err) {
		t.Fatalf("local .git was kept: %v", err)
	}
	assertText(t, filepath.Join(sources["local"], "local.zsh"), "local\n")
	doctorGit(t, paths.Dotfiles, "add", "-A", "--", "zsh", ".gitmodules")
	staged := doctorGit(t, paths.Dotfiles, "ls-files", "--stage", "--", "zsh")
	if !strings.Contains(staged, "zsh/dot-oh-my-zsh/plugins/local/local.zsh") || strings.Contains(staged, "plugins/gone") {
		t.Fatalf("index after commit staging:\n%s", staged)
	}
}

func TestRepairGitlinksRollsBack(t *testing.T) {
	paths, sources := gitlinkFixture(t)
	before := doctorGit(t, paths.Dotfiles, "ls-files", "--stage")
	choices := map[string]dotfiles.RepositoryChoice{
		sources["gone"]:   {Action: dotfiles.RemoveGitlink},
		sources["remote"]: {Action: dotfiles.ConvertRepository, URL: "https://example.invalid/remote.git"},
	}
	if err := os.MkdirAll(filepath.Join(paths.Dotfiles, ".git", "modules", "zsh", "dot-oh-my-zsh", "plugins", "remote"), 0755); err != nil {
		t.Fatal(err)
	}
	put(t, filepath.Join(paths.Dotfiles, ".git", "modules", "zsh", "dot-oh-my-zsh", "plugins", "remote", "HEAD"), "ref: refs/heads/main\n")
	summary := RepairGitlinks(context.Background(), paths, "zsh", choices, nil)
	if len(summary.Failed) != 1 || !strings.Contains(summary.Failed[0].Err.Error(), "leftover submodule metadata") {
		t.Fatalf("summary: %+v", summary)
	}
	if after := doctorGit(t, paths.Dotfiles, "ls-files", "--stage"); after != before {
		t.Fatalf("index changed:\n%s\nwant:\n%s", after, before)
	}
	if _, err := os.Stat(filepath.Join(paths.Dotfiles, ".gitmodules")); !os.IsNotExist(err) {
		t.Fatalf(".gitmodules left behind: %v", err)
	}
	if summary.GitPaths != nil {
		t.Fatalf("git paths after failure: %v", summary.GitPaths)
	}
}

func TestRepairGitlinksRejectsUnavailableAction(t *testing.T) {
	paths, sources := gitlinkFixture(t)
	summary := RepairGitlinks(context.Background(), paths, "zsh", map[string]dotfiles.RepositoryChoice{
		sources["gone"]: {Action: dotfiles.ConvertRepository, URL: "https://example.invalid/gone.git"},
	}, nil)
	if len(summary.Failed) != 1 {
		t.Fatalf("summary: %+v", summary)
	}
	summary = RepairGitlinks(context.Background(), paths, "zsh", map[string]dotfiles.RepositoryChoice{}, nil)
	if len(summary.Failed) != 1 || !strings.Contains(summary.Failed[0].Err.Error(), "nothing to do") {
		t.Fatalf("summary: %+v", summary)
	}
}
