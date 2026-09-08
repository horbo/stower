package dotfiles

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/horbo/stower/internal/stow"
)

type fakeRunner struct {
	dryRunErr error
	restowErr error
	unstowErr error
	calls     []string
}

func (f *fakeRunner) DryRunRestow(pkg string) stow.Result {
	f.calls = append(f.calls, "dry-run "+pkg)
	return stow.Result{Args: []string{"stow", "-n", pkg}, Err: f.dryRunErr}
}

func (f *fakeRunner) Restow(pkgs ...string) stow.Result {
	f.calls = append(f.calls, "restow "+pkgs[0])
	return stow.Result{Args: append([]string{"stow", "-R"}, pkgs...), Err: f.restowErr}
}

func (f *fakeRunner) Unstow(pkg string) stow.Result {
	f.calls = append(f.calls, "unstow "+pkg)
	return stow.Result{Args: []string{"stow", "-D", pkg}, Err: f.unstowErr}
}

func (f *fakeRunner) RestowExcluding(pkg string, entries []string) stow.Result {
	f.calls = append(f.calls, "restow-excluding "+pkg+" "+strings.Join(entries, ","))
	return stow.Result{Args: []string{"stow", "-R", pkg}, Err: f.restowErr}
}

func (f *fakeRunner) DryRunRestowExcluding(pkg string, entries []string) stow.Result {
	f.calls = append(f.calls, "dry-run-excluding "+pkg+" "+strings.Join(entries, ","))
	return stow.Result{Args: []string{"stow", "-n", "-R", pkg}, Err: f.dryRunErr}
}

func collect(t *testing.T, run func(chan Event) Summary) (Summary, []Event) {
	t.Helper()
	events := make(chan Event, 256)
	summary := run(events)
	close(events)
	var collected []Event
	for event := range events {
		collected = append(collected, event)
	}
	return summary, collected
}

func kinds(events []Event) map[EventKind]int {
	counts := map[EventKind]int{}
	for _, event := range events {
		counts[event.Kind]++
	}
	return counts
}

func TestExecuteMovesAndStows(t *testing.T) {
	paths := newPaths(t)
	writeFile(t, filepath.Join(paths.Target, ".zshrc"), "zsh")
	plan := BuildAdoptPlan(paths, Staging{filepath.Join(paths.Target, ".zshrc"): "zsh"})
	runner := &fakeRunner{}

	summary, events := collect(t, func(events chan Event) Summary {
		return Execute(context.Background(), plan, runner, events)
	})

	if !summary.OK() || len(summary.Succeeded) != 1 {
		t.Fatalf("summary = %+v, want one success", summary)
	}
	if exists(filepath.Join(paths.Target, ".zshrc")) {
		t.Error("the source path still exists after the move")
	}
	if got := readFile(t, filepath.Join(paths.Dotfiles, "zsh", "dot-zshrc")); got != "zsh" {
		t.Errorf("moved content = %q, want %q", got, "zsh")
	}
	if want := []string{"dry-run zsh", "restow zsh"}; len(runner.calls) != 2 || runner.calls[0] != want[0] || runner.calls[1] != want[1] {
		t.Errorf("stow calls = %v, want %v", runner.calls, want)
	}
	if counts := kinds(events); counts[PackageDone] != 1 || counts[Rollback] != 0 || counts[StepFailed] != 0 {
		t.Errorf("event kinds = %v, want one PackageDone and no failures", counts)
	}
}

func TestExecuteRollsBackOnDryRunConflict(t *testing.T) {
	paths := newPaths(t)
	writeFile(t, filepath.Join(paths.Target, ".zshrc"), "zsh")
	writeFile(t, filepath.Join(paths.Target, ".config", "foo", "conf"), "foo")
	plan := BuildAdoptPlan(paths, Staging{
		filepath.Join(paths.Target, ".zshrc"):         "one",
		filepath.Join(paths.Target, ".config", "foo"): "one",
	})
	runner := &fakeRunner{dryRunErr: stow.ErrConflict}

	summary, events := collect(t, func(events chan Event) Summary {
		return Execute(context.Background(), plan, runner, events)
	})

	if summary.OK() || len(summary.Failed) != 1 {
		t.Fatalf("summary = %+v, want one failure", summary)
	}
	if !errors.Is(summary.Err(), stow.ErrConflict) {
		t.Errorf("summary.Err() = %v, want it to wrap %v", summary.Err(), stow.ErrConflict)
	}
	if got := readFile(t, filepath.Join(paths.Target, ".zshrc")); got != "zsh" {
		t.Errorf(".zshrc = %q, want it back in the target", got)
	}
	if got := readFile(t, filepath.Join(paths.Target, ".config", "foo", "conf")); got != "foo" {
		t.Errorf(".config/foo/conf = %q, want it back in the target", got)
	}
	if exists(filepath.Join(paths.Dotfiles, "one")) {
		t.Error("the package directory was left behind after the rollback")
	}
	if counts := kinds(events); counts[Rollback] == 0 || counts[PackageFailed] != 1 {
		t.Errorf("event kinds = %v, want rollbacks and one PackageFailed", counts)
	}
	if len(runner.calls) != 1 {
		t.Errorf("stow calls = %v, want only the dry run", runner.calls)
	}
}

func TestExecuteRollsBackOnRestowFailure(t *testing.T) {
	paths := newPaths(t)
	writeFile(t, filepath.Join(paths.Target, ".zshrc"), "zsh")
	plan := BuildAdoptPlan(paths, Staging{filepath.Join(paths.Target, ".zshrc"): "zsh"})
	runner := &fakeRunner{restowErr: errors.New("boom")}

	summary, _ := collect(t, func(events chan Event) Summary {
		return Execute(context.Background(), plan, runner, events)
	})

	if summary.OK() {
		t.Fatal("summary is OK, want a failure")
	}
	if got := readFile(t, filepath.Join(paths.Target, ".zshrc")); got != "zsh" {
		t.Errorf(".zshrc = %q, want it back in the target", got)
	}
	if exists(filepath.Join(paths.Dotfiles, "zsh")) {
		t.Error("the package directory was left behind after the rollback")
	}
	want := []string{"dry-run zsh", "restow zsh", "unstow zsh"}
	if len(runner.calls) != 3 {
		t.Fatalf("stow calls = %v, want %v", runner.calls, want)
	}
	if runner.calls[2] != want[2] {
		t.Errorf("stow calls = %v, want %v", runner.calls, want)
	}
}

func TestExecuteIsOneTransactionPerPackage(t *testing.T) {
	paths := newPaths(t)
	writeFile(t, filepath.Join(paths.Target, ".zshrc"), "zsh")
	writeFile(t, filepath.Join(paths.Target, ".vimrc"), "vim")
	plan := BuildAdoptPlan(paths, Staging{
		filepath.Join(paths.Target, ".zshrc"): "aaa",
		filepath.Join(paths.Target, ".vimrc"): "bbb",
	})
	runner := &failingRunner{failFor: "bbb"}

	summary, _ := collect(t, func(events chan Event) Summary {
		return Execute(context.Background(), plan, runner, events)
	})

	if len(summary.Succeeded) != 1 || summary.Succeeded[0] != "aaa" {
		t.Errorf("succeeded = %v, want [aaa]", summary.Succeeded)
	}
	if len(summary.Failed) != 1 || summary.Failed[0].Package != "bbb" {
		t.Errorf("failed = %+v, want bbb", summary.Failed)
	}
	if exists(filepath.Join(paths.Target, ".zshrc")) {
		t.Error("aaa was rolled back, want it committed")
	}
	if !exists(filepath.Join(paths.Target, ".vimrc")) {
		t.Error("bbb was not rolled back")
	}
	if exists(filepath.Join(paths.Dotfiles, "bbb")) {
		t.Error("the bbb package directory was left behind")
	}
}

type failingRunner struct {
	failFor string
}

func (f *failingRunner) DryRunRestow(pkg string) stow.Result {
	if pkg == f.failFor {
		return stow.Result{Args: []string{"stow", pkg}, Err: stow.ErrConflict}
	}
	return stow.Result{Args: []string{"stow", pkg}}
}

func (f *failingRunner) Restow(pkgs ...string) stow.Result {
	return stow.Result{Args: append([]string{"stow"}, pkgs...)}
}

func (f *failingRunner) Unstow(pkg string) stow.Result {
	return stow.Result{Args: []string{"stow", pkg}}
}

func (f *failingRunner) RestowExcluding(pkg string, entries []string) stow.Result {
	return stow.Result{Args: []string{"stow", pkg}}
}

func (f *failingRunner) DryRunRestowExcluding(pkg string, entries []string) stow.Result {
	return f.DryRunRestow(pkg)
}

func TestExecuteFatalPlan(t *testing.T) {
	paths := newPaths(t)
	writeFile(t, filepath.Join(paths.Target, ".zshrc"), "zsh")
	plan := BuildAdoptPlan(paths, Staging{filepath.Join(paths.Target, ".zshrc"): "zsh"})
	plan.Fatal = ErrCrossDevice
	runner := &fakeRunner{}

	summary, _ := collect(t, func(events chan Event) Summary {
		return Execute(context.Background(), plan, runner, events)
	})

	if summary.OK() {
		t.Fatal("summary is OK, want a failure")
	}
	if len(runner.calls) != 0 {
		t.Errorf("stow calls = %v, want none", runner.calls)
	}
	if !exists(filepath.Join(paths.Target, ".zshrc")) {
		t.Error("the target file was touched despite the fatal plan error")
	}
}

func TestExecuteRemovesNestedGit(t *testing.T) {
	paths := newPaths(t)
	writeFile(t, filepath.Join(paths.Target, ".repo", ".git", "config"), "git")
	writeFile(t, filepath.Join(paths.Target, ".repo", "file"), "x")
	plan := BuildAdoptPlan(paths, Staging{filepath.Join(paths.Target, ".repo"): "repo"})
	plan.Packages[0].Repositories[0].Choice.Action = RemoveRepositoryGit

	summary, _ := collect(t, func(events chan Event) Summary {
		return Execute(context.Background(), plan, &fakeRunner{}, events)
	})

	if !summary.OK() {
		t.Fatalf("summary = %+v, want a success", summary)
	}
	if exists(filepath.Join(paths.Dotfiles, "repo", "dot-repo", ".git")) {
		t.Error("the nested .git directory was kept")
	}
	if !exists(filepath.Join(paths.Dotfiles, "repo", "dot-repo", "file")) {
		t.Error("the moved file is missing")
	}
}

func TestExecuteRestoreMovesBack(t *testing.T) {
	paths := newPaths(t)
	writeFile(t, filepath.Join(paths.Dotfiles, "zsh", "dot-zshrc"), "zsh")
	writeFile(t, filepath.Join(paths.Dotfiles, "zsh", "dot-config", "gh", "hosts.yml"), "gh")
	symlink(t, "../dotfiles/zsh/dot-zshrc", filepath.Join(paths.Target, ".zshrc"))

	plan := BuildRestorePlan(paths, "zsh")
	runner := &unstowingRunner{links: []string{filepath.Join(paths.Target, ".zshrc")}}

	summary, events := collect(t, func(events chan Event) Summary {
		return ExecuteRestore(context.Background(), plan, runner, events)
	})

	if !summary.OK() {
		t.Fatalf("summary = %+v, want a success", summary)
	}
	if got := readFile(t, filepath.Join(paths.Target, ".zshrc")); got != "zsh" {
		t.Errorf(".zshrc = %q, want %q", got, "zsh")
	}
	if isSymlink(t, filepath.Join(paths.Target, ".zshrc")) {
		t.Error(".zshrc is still a symlink")
	}
	if got := readFile(t, filepath.Join(paths.Target, ".config", "gh", "hosts.yml")); got != "gh" {
		t.Errorf("hosts.yml = %q, want %q", got, "gh")
	}
	if exists(plan.RemoveDir) {
		t.Errorf("%s was kept, want it removed", plan.RemoveDir)
	}
	if counts := kinds(events); counts[PackageDone] != 1 || counts[Rollback] != 0 {
		t.Errorf("event kinds = %v, want one PackageDone and no rollback", counts)
	}
}

type unstowingRunner struct {
	links []string
}

func (u *unstowingRunner) DryRunRestow(pkg string) stow.Result {
	return stow.Result{Args: []string{"stow", "-n", pkg}}
}

func (u *unstowingRunner) Restow(pkgs ...string) stow.Result {
	return stow.Result{Args: append([]string{"stow", "-R"}, pkgs...)}
}

func (u *unstowingRunner) Unstow(pkg string) stow.Result {
	for _, link := range u.links {
		_ = os.Remove(link)
	}
	return stow.Result{Args: []string{"stow", "-D", pkg}}
}

func (u *unstowingRunner) RestowExcluding(pkg string, entries []string) stow.Result {
	return stow.Result{Args: []string{"stow", "-R", pkg}}
}

func (u *unstowingRunner) DryRunRestowExcluding(pkg string, entries []string) stow.Result {
	return stow.Result{Args: []string{"stow", "-n", "-R", pkg}}
}

func TestExecuteRestoreBlockedPlan(t *testing.T) {
	paths := newPaths(t)
	writeFile(t, filepath.Join(paths.Dotfiles, "zsh", "dot-zshrc"), "repo")
	writeFile(t, filepath.Join(paths.Target, ".zshrc"), "mine")

	plan := BuildRestorePlan(paths, "zsh")
	runner := &fakeRunner{}
	summary, _ := collect(t, func(events chan Event) Summary {
		return ExecuteRestore(context.Background(), plan, runner, events)
	})

	if summary.OK() {
		t.Fatal("summary is OK, want a failure for a blocked plan")
	}
	if len(runner.calls) != 0 {
		t.Errorf("stow calls = %v, want none", runner.calls)
	}
	if got := readFile(t, filepath.Join(paths.Target, ".zshrc")); got != "mine" {
		t.Errorf(".zshrc = %q, want it untouched", got)
	}
}

func TestExecuteRestoreRollsBackOnCollision(t *testing.T) {
	paths := newPaths(t)
	writeFile(t, filepath.Join(paths.Dotfiles, "zsh", "dot-aaa"), "a")
	writeFile(t, filepath.Join(paths.Dotfiles, "zsh", "dot-bbb"), "b")

	plan := BuildRestorePlan(paths, "zsh")
	writeFile(t, filepath.Join(paths.Target, ".bbb"), "in the way")

	summary, events := collect(t, func(events chan Event) Summary {
		return ExecuteRestore(context.Background(), plan, &fakeRunner{}, events)
	})

	if summary.OK() {
		t.Fatal("summary is OK, want a failure")
	}
	if !errors.Is(summary.Err(), ErrDestinationExists) {
		t.Errorf("summary.Err() = %v, want it to wrap %v", summary.Err(), ErrDestinationExists)
	}
	if got := readFile(t, filepath.Join(paths.Dotfiles, "zsh", "dot-aaa")); got != "a" {
		t.Errorf("dot-aaa = %q, want it rolled back into the package", got)
	}
	if got := readFile(t, filepath.Join(paths.Target, ".bbb")); got != "in the way" {
		t.Errorf(".bbb = %q, want it untouched", got)
	}
	if exists(filepath.Join(paths.Target, ".aaa")) {
		t.Error(".aaa was left in the target after the rollback")
	}
	if counts := kinds(events); counts[Rollback] == 0 {
		t.Errorf("event kinds = %v, want rollback events", counts)
	}
}

func TestExecuteWithoutEventChannel(t *testing.T) {
	paths := newPaths(t)
	writeFile(t, filepath.Join(paths.Target, ".zshrc"), "zsh")
	plan := BuildAdoptPlan(paths, Staging{filepath.Join(paths.Target, ".zshrc"): "zsh"})

	if summary := Execute(context.Background(), plan, &fakeRunner{}, nil); !summary.OK() {
		t.Fatalf("summary = %+v, want a success", summary)
	}
}

func TestExecuteCancelledContext(t *testing.T) {
	paths := newPaths(t)
	writeFile(t, filepath.Join(paths.Target, ".zshrc"), "zsh")
	plan := BuildAdoptPlan(paths, Staging{filepath.Join(paths.Target, ".zshrc"): "zsh"})

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	summary := Execute(ctx, plan, &fakeRunner{}, nil)

	if summary.OK() {
		t.Fatal("summary is OK, want a failure on a cancelled context")
	}
	if !exists(filepath.Join(paths.Target, ".zshrc")) {
		t.Error("the target file was moved despite the cancelled context")
	}
}
