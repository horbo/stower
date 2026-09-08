package dotfiles

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/horbo/stower/internal/config"
	"github.com/horbo/stower/internal/gitx"
)

func repoCommand(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir, "-c", "user.name=Test", "-c", "user.email=test@example.invalid", "-c", "protocol.file.allow=always"}, args...)...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return string(out)
}

func setupSubmodule(t *testing.T) (config.Paths, string) {
	t.Helper()
	requireStow(t)
	t.Setenv("GIT_CONFIG_GLOBAL", filepath.Join(t.TempDir(), "absent"))
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	paths := newPaths(t)
	repoCommand(t, paths.Dotfiles, "init")
	source := filepath.Join(paths.Target, ".config", "repo")
	writeFile(t, filepath.Join(source, "tracked"), "initial\n")
	writeFile(t, filepath.Join(source, ".gitignore"), "ignored\n")
	repoCommand(t, source, "init")
	repoCommand(t, source, "add", ".")
	repoCommand(t, source, "commit", "-m", "initial")
	repoCommand(t, source, "remote", "add", "origin", "https://example.invalid/repo.git")
	return paths, source
}

func conversionPlan(t *testing.T, paths config.Paths, source, pkg string) AdoptPlan {
	t.Helper()
	plan := BuildAdoptPlan(paths, Staging{source: pkg})
	if plan.Fatal != nil || plan.BlockedCount() != 0 {
		t.Fatalf("plan: %+v", plan)
	}
	for i := range plan.Packages {
		for j := range plan.Packages[i].Repositories {
			plan.Packages[i].Repositories[j].Choice.Action = ConvertRepository
		}
	}
	return plan
}

func repositoryState(t *testing.T, path string) []string {
	t.Helper()
	var state []string
	for _, args := range [][]string{{"rev-parse", "HEAD"}, {"for-each-ref"}, {"remote", "-v"}, {"diff", "--binary"}, {"diff", "--cached", "--binary"}, {"status", "--porcelain", "--untracked-files=all"}} {
		state = append(state, repoCommand(t, path, args...))
	}
	return state
}

func TestSubmoduleRoundTrip(t *testing.T) {
	for _, commit := range []bool{false, true} {
		t.Run(map[bool]string{false: "pending", true: "committed"}[commit], func(t *testing.T) {
			paths, source := setupSubmodule(t)
			writeFile(t, filepath.Join(source, "tracked"), "staged\n")
			repoCommand(t, source, "add", "tracked")
			writeFile(t, filepath.Join(source, "tracked"), "unstaged\n")
			writeFile(t, filepath.Join(source, "untracked"), "keep\n")
			writeFile(t, filepath.Join(source, "ignored"), "ignored\n")
			before := repositoryState(t, source)
			plan := conversionPlan(t, paths, source, "editor")
			summary := Execute(context.Background(), plan, newRunner(paths), nil)
			if !summary.OK() {
				t.Fatal(summary.Err())
			}
			if !reflect.DeepEqual(summary.GitPaths, []string{".gitmodules"}) {
				t.Fatalf("GitPaths: %v", summary.GitPaths)
			}
			if after := repositoryState(t, source); !reflect.DeepEqual(before, after) {
				t.Fatalf("conversion changed repository:\nbefore %q\nafter %q", before, after)
			}
			dest := plan.Packages[0].Repositories[0].Destination
			info, err := os.Lstat(filepath.Join(dest, ".git"))
			if err != nil || !info.Mode().IsRegular() {
				t.Fatalf("expected absorbed .git: %v", err)
			}
			if commit {
				repoCommand(t, paths.Dotfiles, "commit", "-m", "adopt")
			}
			restore := BuildRestorePlan(paths, "editor")
			if !restore.Runnable() {
				t.Fatalf("restore: %+v", restore)
			}
			summary = ExecuteRestore(context.Background(), restore, newRunner(paths), nil)
			if !summary.OK() {
				t.Fatal(summary.Err())
			}
			if after := repositoryState(t, source); !reflect.DeepEqual(before, after) {
				t.Fatalf("restore changed repository:\nbefore %q\nafter %q", before, after)
			}
			if got := readFile(t, filepath.Join(source, "ignored")); got != "ignored\n" {
				t.Fatal(got)
			}
			info, err = os.Lstat(filepath.Join(source, ".git"))
			if err != nil || !info.IsDir() {
				t.Fatalf("expected standalone .git: %v", err)
			}
			if out := repoCommand(t, paths.Dotfiles, "ls-files", "--stage"); strings.Contains(out, "160000") {
				t.Fatal(out)
			}
			modules, err := gitx.ListSubmodules(context.Background(), paths.Dotfiles)
			if err != nil || len(modules) != 0 {
				t.Fatalf("modules: %v %v", modules, err)
			}
		})
	}
}

type failingGit struct {
	repositoryGit
	register bool
	detach   bool
	rollback bool
}
type failingTransaction struct {
	GitTransaction
	register bool
	detach   bool
	rollback bool
}

func (g failingGit) Begin(dir string) (GitTransaction, error) {
	tx, err := g.repositoryGit.Begin(dir)
	return failingTransaction{tx, g.register, g.detach, g.rollback}, err
}
func (g failingTransaction) Rollback() error {
	err := g.GitTransaction.Rollback()
	if g.rollback {
		return errors.Join(err, errors.New("injected rollback failure"))
	}
	return err
}
func (g failingTransaction) Register(ctx context.Context, rel, url, head string) error {
	if err := g.GitTransaction.Register(ctx, rel, url, head); err != nil {
		return err
	}
	if g.register {
		return errors.New("injected register failure")
	}
	return nil
}
func (g failingTransaction) Detach(ctx context.Context, m gitx.Submodule) error {
	if err := g.GitTransaction.Detach(ctx, m); err != nil {
		return err
	}
	if g.detach {
		return errors.New("injected detach failure")
	}
	return nil
}

func TestSubmoduleConversionRollback(t *testing.T) {
	paths, source := setupSubmodule(t)
	before := repositoryState(t, source)
	configBefore := readFile(t, filepath.Join(source, ".git", "config"))
	plan := conversionPlan(t, paths, source, "editor")
	summary := ExecuteWithGit(context.Background(), plan, newRunner(paths), failingGit{register: true}, nil)
	if summary.OK() {
		t.Fatal("expected failure")
	}
	if after := repositoryState(t, source); !reflect.DeepEqual(before, after) {
		t.Fatalf("rollback changed repo: %q %q", before, after)
	}
	if got := readFile(t, filepath.Join(source, ".git", "config")); got != configBefore {
		t.Fatal("config changed")
	}
	if exists(filepath.Join(paths.Dotfiles, ".gitmodules")) {
		t.Fatal(".gitmodules was not rolled back")
	}
}

func TestSubmoduleRollbackFailureKeepsHomeEntries(t *testing.T) {
	paths, source := setupSubmodule(t)
	plan := conversionPlan(t, paths, source, "editor")
	destination := plan.Packages[0].Repositories[0].Destination
	summary := ExecuteWithGit(context.Background(), plan, newRunner(paths), failingGit{register: true, rollback: true}, nil)
	if summary.OK() {
		t.Fatal("expected failure")
	}
	if got := summary.Err().Error(); !strings.Contains(got, "injected rollback failure") {
		t.Fatalf("error does not report the rollback failure: %v", got)
	}
	info, err := os.Lstat(source)
	if err != nil || !info.IsDir() {
		t.Fatalf("source directory was not restored: %v", err)
	}
	if got := readFile(t, filepath.Join(source, "tracked")); got != "initial\n" {
		t.Fatalf("tracked file: %q", got)
	}
	if exists(destination) {
		t.Fatalf("destination was kept: %s", destination)
	}
}

func TestRemoveGitFailureIsReportedAsWarning(t *testing.T) {
	paths, source := setupSubmodule(t)
	second := filepath.Join(filepath.Dir(source), "other")
	repoCommand(t, filepath.Dir(source), "clone", source, second)
	if err := os.RemoveAll(filepath.Join(source, ".git")); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(source, ".git"), "gitdir: /nowhere\n")
	plan := BuildAdoptPlan(paths, Staging{filepath.Dir(source): "editor"})
	if plan.Fatal != nil || plan.BlockedCount() != 0 {
		t.Fatalf("plan: %+v", plan)
	}
	if len(plan.Packages) != 1 || len(plan.Packages[0].Repositories) != 2 {
		t.Fatalf("repositories: %+v", plan.Packages)
	}
	for i := range plan.Packages[0].Repositories {
		plan.Packages[0].Repositories[i].Choice.Action = RemoveRepositoryGit
	}
	summary := Execute(context.Background(), plan, newRunner(paths), nil)
	if !summary.OK() {
		t.Fatal(summary.Err())
	}
	if len(summary.Succeeded) != 1 || summary.Succeeded[0] != "editor" {
		t.Fatalf("summary: %+v", summary)
	}
	if len(summary.Warnings) != 1 || summary.Warnings[0].Package != "editor" {
		t.Fatalf("warnings: %+v", summary.Warnings)
	}
	for _, r := range plan.Packages[0].Repositories {
		removed := !exists(filepath.Join(r.Destination, ".git"))
		if filepath.Base(r.Source) == "other" && !removed {
			t.Fatalf(".git of the healthy repository was kept: %s", r.Destination)
		}
	}
}

func TestSubmoduleRestoreRollback(t *testing.T) {
	paths, source := setupSubmodule(t)
	plan := conversionPlan(t, paths, source, "editor")
	if summary := Execute(context.Background(), plan, newRunner(paths), nil); !summary.OK() {
		t.Fatal(summary.Err())
	}
	before := repositoryState(t, source)
	modules := readFile(t, filepath.Join(paths.Dotfiles, ".gitmodules"))
	summary := ExecuteRestoreWithGit(context.Background(), BuildRestorePlan(paths, "editor"), newRunner(paths), failingGit{detach: true}, nil)
	if summary.OK() {
		t.Fatal("expected failure")
	}
	if after := repositoryState(t, source); !reflect.DeepEqual(before, after) {
		t.Fatalf("rollback changed repo: %q %q", before, after)
	}
	if got := readFile(t, filepath.Join(paths.Dotfiles, ".gitmodules")); got != modules {
		t.Fatal(".gitmodules changed")
	}
}

func TestSubmoduleRestoreUnfolded(t *testing.T) {
	paths, source := setupSubmodule(t)
	before := repositoryState(t, source)
	plan := conversionPlan(t, paths, source, "editor")
	runner := newRunner(paths)
	if summary := Execute(context.Background(), plan, runner, nil); !summary.OK() {
		t.Fatal(summary.Err())
	}
	if result := runner.Unstow("editor"); result.Err != nil {
		t.Fatal(result.Err)
	}
	if err := os.MkdirAll(source, 0755); err != nil {
		t.Fatal(err)
	}
	if result := runner.Restow("editor"); result.Err != nil {
		t.Fatal(result.Err)
	}
	restore := BuildRestorePlan(paths, "editor")
	if !restore.Runnable() {
		t.Fatalf("restore: %+v", restore)
	}
	if len(restore.Moves) != 1 || restore.Moves[0].To != source {
		t.Fatalf("submodule was split: %+v", restore.Moves)
	}
	partial := BuildEntryRestorePlan(paths, "editor", []string{"dot-config/repo/tracked"})
	if partial.Runnable() {
		t.Fatal("partial submodule restore was allowed")
	}
	if summary := ExecuteRestore(context.Background(), restore, runner, nil); !summary.OK() {
		t.Fatal(summary.Err())
	}
	if after := repositoryState(t, source); !reflect.DeepEqual(before, after) {
		t.Fatalf("restore changed repo: %q %q", before, after)
	}
}

func TestSubmoduleRestoreAfterClone(t *testing.T) {
	paths, source := setupSubmodule(t)
	remote := filepath.Join(t.TempDir(), "remote.git")
	repoCommand(t, paths.Target, "clone", "--bare", source, remote)
	repoCommand(t, source, "remote", "set-url", "origin", remote)
	plan := conversionPlan(t, paths, source, "editor")
	if summary := Execute(context.Background(), plan, newRunner(paths), nil); !summary.OK() {
		t.Fatal(summary.Err())
	}
	repoCommand(t, paths.Dotfiles, "commit", "-m", "adopt")
	clonePaths := newPaths(t)
	repoCommand(t, clonePaths.Target, "clone", "--recurse-submodules", paths.Dotfiles, clonePaths.Dotfiles)
	runner := newRunner(clonePaths)
	if result := runner.Restow("editor"); result.Err != nil {
		t.Fatal(result.Err)
	}
	restore := BuildRestorePlan(clonePaths, "editor")
	if !restore.Runnable() {
		t.Fatalf("restore: %+v", restore)
	}
	if summary := ExecuteRestore(context.Background(), restore, runner, nil); !summary.OK() {
		t.Fatal(summary.Err())
	}
	restored := filepath.Join(clonePaths.Target, ".config", "repo")
	if got := strings.TrimSpace(repoCommand(t, restored, "rev-parse", "HEAD")); got != plan.Packages[0].Repositories[0].Info.Head {
		t.Fatal(got)
	}
	if out := repoCommand(t, restored, "status", "--porcelain"); out != "" {
		t.Fatal(out)
	}
}

func TestSubmoduleMultipleRepositoriesAndMixedActions(t *testing.T) {
	paths, source := setupSubmodule(t)
	parent := filepath.Dir(source)
	second := filepath.Join(parent, "other")
	repoCommand(t, parent, "clone", source, second)
	third := filepath.Join(parent, "plain")
	repoCommand(t, parent, "clone", source, third)
	plan := conversionPlan(t, paths, parent, "editor")
	for i := range plan.Packages[0].Repositories {
		r := &plan.Packages[0].Repositories[i]
		if r.Source == third {
			r.Choice.Action = RemoveRepositoryGit
		}
	}
	if summary := Execute(context.Background(), plan, newRunner(paths), nil); !summary.OK() {
		t.Fatal(summary.Err())
	}
	modules, err := gitx.ListSubmodules(context.Background(), paths.Dotfiles)
	if err != nil || len(modules) != 2 {
		t.Fatalf("modules: %v %v", modules, err)
	}
	if exists(filepath.Join(paths.Dotfiles, "editor", "dot-config", "plain", ".git")) {
		t.Fatal("selected .git was kept")
	}
	restore := BuildRestorePlan(paths, "editor")
	if summary := ExecuteRestore(context.Background(), restore, newRunner(paths), nil); !summary.OK() {
		t.Fatal(summary.Err())
	}
	for _, path := range []string{source, second} {
		if !gitx.IsRepo(path) {
			t.Fatalf("not standalone: %s", path)
		}
	}
}

func TestSubmoduleBlockedConversionLeavesFiles(t *testing.T) {
	for _, kind := range []string{"no-url", "no-head", "nested", "metadata-change"} {
		t.Run(kind, func(t *testing.T) {
			paths, source := setupSubmodule(t)
			switch kind {
			case "no-url":
				repoCommand(t, source, "remote", "remove", "origin")
			case "no-head":
				repoCommand(t, source, "checkout", "--orphan", "empty")
			case "nested":
				nested := filepath.Join(source, "nested")
				writeFile(t, filepath.Join(nested, "file"), "x")
				repoCommand(t, nested, "init")
			case "metadata-change":
				writeFile(t, filepath.Join(paths.Dotfiles, ".gitmodules"), "unrelated\n")
			}
			plan := conversionPlan(t, paths, source, "editor")
			summary := Execute(context.Background(), plan, newRunner(paths), nil)
			if summary.OK() {
				t.Fatal("expected blocked conversion")
			}
			info, err := os.Lstat(source)
			if err != nil || !info.IsDir() {
				t.Fatal("source moved")
			}
		})
	}
}

func TestSubmoduleStowFailuresRollback(t *testing.T) {
	for _, step := range []string{"dry-run", "restow"} {
		t.Run(step, func(t *testing.T) {
			paths, source := setupSubmodule(t)
			before := repositoryState(t, source)
			runner := &fakeRunner{}
			if step == "dry-run" {
				runner.dryRunErr = errors.New("injected dry run failure")
			} else {
				runner.restowErr = errors.New("injected restow failure")
			}
			summary := Execute(context.Background(), conversionPlan(t, paths, source, "editor"), runner, nil)
			if summary.OK() {
				t.Fatal("expected failure")
			}
			if after := repositoryState(t, source); !reflect.DeepEqual(before, after) {
				t.Fatalf("rollback changed repository: %q %q", before, after)
			}
			if out := repoCommand(t, paths.Dotfiles, "ls-files", "--stage"); out != "" {
				t.Fatal(out)
			}
		})
	}
}

type cancelGit struct {
	repositoryGit
	cancel context.CancelFunc
}
type cancelTransaction struct {
	GitTransaction
	cancel context.CancelFunc
}

func (g cancelGit) Begin(dir string) (GitTransaction, error) {
	tx, err := g.repositoryGit.Begin(dir)
	return cancelTransaction{tx, g.cancel}, err
}
func (g cancelTransaction) Register(ctx context.Context, rel, url, head string) error {
	if err := g.GitTransaction.Register(ctx, rel, url, head); err != nil {
		return err
	}
	g.cancel()
	return ctx.Err()
}

func TestSubmoduleCancellationRollback(t *testing.T) {
	paths, source := setupSubmodule(t)
	before := repositoryState(t, source)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	summary := ExecuteWithGit(ctx, conversionPlan(t, paths, source, "editor"), newRunner(paths), cancelGit{cancel: cancel}, nil)
	if !errors.Is(summary.Err(), context.Canceled) {
		t.Fatalf("expected cancellation: %v", summary.Err())
	}
	if after := repositoryState(t, source); !reflect.DeepEqual(before, after) {
		t.Fatalf("rollback changed repository: %q %q", before, after)
	}
}

func TestSubmoduleLaterPackageFailureKeepsEarlierSuccess(t *testing.T) {
	paths, source := setupSubmodule(t)
	second := filepath.Join(paths.Target, ".config", "second")
	repoCommand(t, filepath.Dir(source), "clone", source, second)
	plan := BuildAdoptPlan(paths, Staging{source: "aaa", second: "zzz"})
	for i := range plan.Packages {
		for j := range plan.Packages[i].Repositories {
			plan.Packages[i].Repositories[j].Choice.Action = ConvertRepository
		}
	}
	summary := ExecuteWithGit(context.Background(), plan, newRunner(paths), failSecondGit{}, nil)
	if len(summary.Succeeded) != 1 || summary.Succeeded[0] != "aaa" || len(summary.Failed) != 1 {
		t.Fatalf("summary: %+v", summary)
	}
	modules, err := gitx.ListSubmodules(context.Background(), paths.Dotfiles)
	if err != nil || len(modules) != 1 || !strings.HasPrefix(modules[0].Path, "aaa/") {
		t.Fatalf("modules: %v %v", modules, err)
	}
	info, err := os.Lstat(filepath.Join(second, ".git"))
	if err != nil || !info.IsDir() {
		t.Fatalf("second source changed: %v", err)
	}
}

func TestSubmodulePartialRestoreKeepsOtherRepository(t *testing.T) {
	paths, source := setupSubmodule(t)
	second := filepath.Join(paths.Target, ".config", "other")
	repoCommand(t, filepath.Dir(source), "clone", source, second)
	plan := BuildAdoptPlan(paths, Staging{source: "editor", second: "editor"})
	for i := range plan.Packages[0].Repositories {
		plan.Packages[0].Repositories[i].Choice.Action = ConvertRepository
	}
	runner := newRunner(paths)
	if summary := Execute(context.Background(), plan, runner, nil); !summary.OK() {
		t.Fatal(summary.Err())
	}
	restore := BuildEntryRestorePlan(paths, "editor", []string{"dot-config/repo"})
	if !restore.Runnable() || !restore.Partial() {
		t.Fatalf("restore: %+v", restore)
	}
	if summary := ExecuteRestore(context.Background(), restore, runner, nil); !summary.OK() {
		t.Fatal(summary.Err())
	}
	modules, err := gitx.ListSubmodules(context.Background(), paths.Dotfiles)
	if err != nil || len(modules) != 1 || modules[0].Path != "editor/dot-config/other" {
		t.Fatalf("modules: %v %v", modules, err)
	}
	if !gitx.IsRepo(source) {
		t.Fatal("restored repository is not standalone")
	}
	if !isSymlink(t, second) {
		t.Fatal("other repository was unlinked")
	}
}

type failSecondGit struct{ repositoryGit }
type failSecondTransaction struct{ GitTransaction }

func (g failSecondGit) Begin(dir string) (GitTransaction, error) {
	tx, err := g.repositoryGit.Begin(dir)
	return failSecondTransaction{tx}, err
}
func (g failSecondTransaction) Register(ctx context.Context, rel, url, head string) error {
	if err := g.GitTransaction.Register(ctx, rel, url, head); err != nil {
		return err
	}
	if strings.HasPrefix(rel, "zzz/") {
		return errors.New("injected second package failure")
	}
	return nil
}

func TestAdoptPlanDoesNotValidateKeptRepositories(t *testing.T) {
	paths, source := setupSubmodule(t)
	if err := os.RemoveAll(filepath.Join(paths.Dotfiles, ".git")); err != nil {
		t.Fatal(err)
	}
	plan := BuildAdoptPlan(paths, Staging{source: "editor"})
	if len(plan.Packages) != 1 || len(plan.Packages[0].Repositories) != 1 {
		t.Fatalf("plan: %+v", plan)
	}
	r := plan.Packages[0].Repositories[0]
	if r.Choice.Action != KeepRepository {
		t.Fatalf("default action: %v", r.Choice.Action)
	}
	if r.Conflict != "" || r.Info.Reason != "" {
		t.Fatalf("kept repository was validated: conflict=%q reason=%q", r.Conflict, r.Info.Reason)
	}
	if err := r.ConversionError(); err != nil {
		t.Fatalf("conversion error before a choice: %v", err)
	}
}

func TestValidateRepositoryChoicesFlagsRegisteredDestination(t *testing.T) {
	paths, source := setupSubmodule(t)
	plan := conversionPlan(t, paths, source, "editor")
	rel, err := filepath.Rel(paths.Dotfiles, plan.Packages[0].Repositories[0].Destination)
	if err != nil {
		t.Fatal(err)
	}
	modules := "[submodule \"" + filepath.ToSlash(rel) + "\"]\n\tpath = " + filepath.ToSlash(rel) +
		"\n\turl = https://example.invalid/repo.git\n"
	writeFile(t, filepath.Join(paths.Dotfiles, ".gitmodules"), modules)
	repoCommand(t, paths.Dotfiles, "add", ".gitmodules")
	repoCommand(t, paths.Dotfiles, "commit", "-m", "modules")

	ValidateRepositoryChoices(context.Background(), &plan)
	r := plan.Packages[0].Repositories[0]
	if r.Conflict == "" {
		t.Fatal("the registered destination was accepted")
	}
	if r.Info.Reason != "" {
		t.Fatalf("validation overwrote Info.Reason: %q", r.Info.Reason)
	}
	if err := r.ConversionError(); err == nil || err.Error() != r.Conflict {
		t.Fatalf("conversion error: %v", err)
	}
	for i := range plan.Packages[0].Repositories {
		plan.Packages[0].Repositories[i].Choice.Action = KeepRepository
	}
	ValidateRepositoryChoices(context.Background(), &plan)
	if got := plan.Packages[0].Repositories[0].Conflict; got != "" {
		t.Fatalf("conflict kept after switching to Keep: %q", got)
	}
}

func TestNestedRepositoryAllowedActions(t *testing.T) {
	tests := []struct {
		name string
		r    NestedRepository
		want []RepositoryAction
	}{
		{
			name: "full repository",
			r:    NestedRepository{Info: gitx.Repository{GitDir: "/tmp/repo/.git"}},
			want: []RepositoryAction{KeepRepository, RemoveRepositoryGit, ConvertRepository},
		},
		{
			name: "git is a file",
			r:    NestedRepository{Info: gitx.Repository{Reason: "conversion requires a repository with its own .git directory"}},
			want: []RepositoryAction{KeepRepository},
		},
		{
			name: "repository with a reason",
			r:    NestedRepository{Info: gitx.Repository{GitDir: "/tmp/repo/.git", Reason: "repository has no commit"}},
			want: []RepositoryAction{KeepRepository, RemoveRepositoryGit},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := test.r.AllowedActions(); !reflect.DeepEqual(got, test.want) {
				t.Fatalf("actions = %v, want %v", got, test.want)
			}
		})
	}
}

func TestSubmoduleStateIgnoresGitignore(t *testing.T) {
	paths, source := setupSubmodule(t)
	plan := conversionPlan(t, paths, source, "editor")
	if summary := Execute(context.Background(), plan, newRunner(paths), nil); !summary.OK() {
		t.Fatal(summary.Err())
	}
	entries, err := WalkPackage(paths, "editor")
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, entry := range entries {
		if !entry.Submodule {
			continue
		}
		found = true
		if entry.State != Linked {
			t.Fatalf("submodule state = %v, want Linked", entry.State)
		}
	}
	if !found {
		t.Fatalf("no submodule entry: %+v", entries)
	}
}

func TestKeepRepositoryDefaultPreservesGit(t *testing.T) {
	paths, source := setupSubmodule(t)
	before := repositoryState(t, source)
	plan := BuildAdoptPlan(paths, Staging{source: "editor"})
	if len(plan.Packages) != 1 || len(plan.Packages[0].Repositories) != 1 {
		t.Fatalf("missing repository: %+v", plan)
	}
	repository := plan.Packages[0].Repositories[0]
	if repository.Choice.Action != KeepRepository {
		t.Fatalf("default action: %v", repository.Choice.Action)
	}
	summary := Execute(context.Background(), plan, newRunner(paths), nil)
	if !summary.OK() {
		t.Fatal(summary.Err())
	}
	if after := repositoryState(t, source); !reflect.DeepEqual(before, after) {
		t.Fatalf("Keep changed repository: %q %q", before, after)
	}
	info, err := os.Lstat(filepath.Join(repository.Destination, ".git"))
	if err != nil || !info.IsDir() {
		t.Fatalf("Keep converted or removed .git: %v", err)
	}
	if exists(filepath.Join(paths.Dotfiles, ".gitmodules")) || len(summary.GitPaths) != 0 {
		t.Fatal("Keep registered a submodule")
	}
}
