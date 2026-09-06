package doctor

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/horbo/stower/internal/config"
	"github.com/horbo/stower/internal/dotfiles"
	"github.com/horbo/stower/internal/stow"
)

func fixture(t *testing.T) (config.Paths, stow.Runner) {
	t.Helper()
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", root)
	paths := config.Paths{Target: root, Dotfiles: filepath.Join(root, "dotfiles")}
	if err := os.MkdirAll(paths.Dotfiles, 0755); err != nil {
		t.Fatal(err)
	}
	bin, err := exec.LookPath("stow")
	if err != nil {
		t.Fatal(err)
	}
	return paths, stow.Runner{Bin: bin, Target: root, Dotfiles: paths.Dotfiles}
}
func put(t *testing.T, path, text string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(text), 0644); err != nil {
		t.Fatal(err)
	}
}
func assertText(t *testing.T, path, want string) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil || string(data) != want {
		t.Fatalf("%s = %q, %v; want %q", path, data, err, want)
	}
}
func issueFor(t *testing.T, paths config.Paths, pkg string, state State) Issue {
	t.Helper()
	report := InspectPackage(paths, pkg)
	if report.Err != nil {
		t.Fatal(report.Err)
	}
	for _, item := range report.Entries {
		if item.State == state {
			return item
		}
	}
	t.Fatalf("no %s in %+v", state, report)
	return Issue{}
}
func doFix(t *testing.T, paths config.Paths, issue Issue, action Action, runner dotfiles.Runner) dotfiles.Summary {
	t.Helper()
	return Fix(context.Background(), paths, issue, action, runner, nil)
}

func TestInspectStates(t *testing.T) {
	paths, runner := fixture(t)
	for _, pkg := range []string{"healthy", "missing", "replaced", "foreign", "unowned"} {
		put(t, filepath.Join(paths.Dotfiles, pkg, "dot-"+pkg), pkg)
	}
	if result := runner.Restow("healthy", "replaced"); result.Err != nil {
		t.Fatal(result.Err)
	}
	if err := os.Remove(filepath.Join(paths.Target, ".replaced")); err != nil {
		t.Fatal(err)
	}
	put(t, filepath.Join(paths.Target, ".replaced"), "target")
	if err := os.Symlink(filepath.Join(paths.Target, "outside"), filepath.Join(paths.Target, ".foreign")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(paths.Dotfiles, "unowned", "dot-unowned"), filepath.Join(paths.Target, ".unowned")); err != nil {
		t.Fatal(err)
	}
	put(t, filepath.Join(paths.Dotfiles, "raw", ".zshenv"), "raw")
	put(t, filepath.Join(paths.Dotfiles, "nested", "dot-config", "dot-deep"), "nested")
	if result := runner.Restow("nested"); result.Err != nil {
		t.Fatal(result.Err)
	}
	for pkg, state := range map[string]State{"healthy": OK, "missing": Missing, "replaced": Replaced, "foreign": Foreign, "unowned": Unowned, "raw": Unnormalized, "nested": Unnormalized} {
		issueFor(t, paths, pkg, state)
	}
	foreign := issueFor(t, paths, "foreign", Foreign)
	if foreign.Detail != "symlink points elsewhere: "+filepath.Join(paths.Target, "outside")+" (dangling)" || foreign.Fixable {
		t.Fatalf("foreign detail = %q fixable=%v", foreign.Detail, foreign.Fixable)
	}
	unowned := issueFor(t, paths, "unowned", Unowned)
	if unowned.Detail != "link resolves to the package entry but stow will not own it: "+filepath.Join(paths.Dotfiles, "unowned", "dot-unowned") || !unowned.Fixable {
		t.Fatalf("unowned detail = %q fixable=%v", unowned.Detail, unowned.Fixable)
	}
	if plan := BuildRestorePlan(paths, "unowned"); plan.Runnable() {
		t.Fatalf("restore allowed an unowned entry: %+v", plan)
	}
	issue := issueFor(t, paths, "nested", Unnormalized)
	if issue.Fixable {
		t.Fatal("nested dot- is fixable")
	}
	issues, reports, err := Inspect(paths)
	if err != nil || len(issues) != 6 || len(reports) != 7 {
		t.Fatalf("%d issues %d reports err=%v", len(issues), len(reports), err)
	}
}

func TestFoldedDirectoryAndUnfoldedLinks(t *testing.T) {
	paths, runner := fixture(t)
	put(t, filepath.Join(paths.Dotfiles, "foo", "dot-config", "foo", "conf"), "repo")
	if err := os.Mkdir(filepath.Join(paths.Target, ".config"), 0755); err != nil {
		t.Fatal(err)
	}
	if result := runner.Restow("foo"); result.Err != nil {
		t.Fatal(result.Err)
	}
	put(t, filepath.Join(paths.Target, ".config", "foo", "conf"), "edited through link")
	issueFor(t, paths, "foo", OK)
	if err := os.Remove(filepath.Join(paths.Target, ".config", "foo")); err != nil {
		t.Fatal(err)
	}
	put(t, filepath.Join(paths.Target, ".config", "foo", "conf"), "replacement")
	issue := issueFor(t, paths, "foo", Replaced)
	if issue.Entry.PkgRel != "dot-config/foo" {
		t.Fatal(issue.Entry.PkgRel)
	}
	plan := BuildRestorePlan(paths, "foo")
	if plan.Runnable() {
		t.Fatal("restore allowed a replaced directory")
	}
	if summary := doFix(t, paths, issue, KeepTarget, runner); !summary.OK() {
		t.Fatal(summary.Err())
	}
	assertText(t, filepath.Join(paths.Dotfiles, "foo", "dot-config", "foo", "conf"), "replacement")
	issueFor(t, paths, "foo", OK)
}

func TestKeepTargetAndRepo(t *testing.T) {
	for _, action := range []Action{KeepTarget, KeepRepo} {
		t.Run(string(action), func(t *testing.T) {
			paths, runner := fixture(t)
			repo, target := filepath.Join(paths.Dotfiles, "pkg", "dot-file"), filepath.Join(paths.Target, ".file")
			put(t, repo, "repo")
			put(t, target, "target")
			issue := issueFor(t, paths, "pkg", Replaced)
			if result := doFix(t, paths, issue, action, runner); !result.OK() {
				t.Fatal(result.Err())
			}
			want := "repo"
			if action == KeepTarget {
				want = "target"
			}
			assertText(t, repo, want)
			assertText(t, target, want)
			issueFor(t, paths, "pkg", OK)
			backups, _ := filepath.Glob(filepath.Join(paths.Dotfiles, ".stower-backup-*"))
			if len(backups) != 0 {
				t.Fatal(backups)
			}
		})
	}
}

func TestMissingAndNormalize(t *testing.T) {
	paths, runner := fixture(t)
	put(t, filepath.Join(paths.Dotfiles, "pkg", "dot-file"), "repo")
	if summary := doFix(t, paths, issueFor(t, paths, "pkg", Missing), Restow, runner); !summary.OK() {
		t.Fatal(summary.Err())
	}
	issueFor(t, paths, "pkg", OK)
	put(t, filepath.Join(paths.Dotfiles, "raw", ".raw"), "raw")
	if result := runner.Restow("raw"); result.Err != nil {
		t.Fatal(result.Err)
	}
	if summary := doFix(t, paths, issueFor(t, paths, "raw", Unnormalized), Normalize, runner); !summary.OK() {
		t.Fatal(summary.Err())
	}
	assertText(t, filepath.Join(paths.Dotfiles, "raw", "dot-raw"), "raw")
	issueFor(t, paths, "raw", OK)
}

func TestNormalizeCollisionAndReportOnly(t *testing.T) {
	paths, runner := fixture(t)
	put(t, filepath.Join(paths.Dotfiles, "raw", ".raw"), "raw")
	put(t, filepath.Join(paths.Dotfiles, "raw", "dot-raw"), "existing")
	if result := doFix(t, paths, issueFor(t, paths, "raw", Unnormalized), Normalize, runner); result.OK() {
		t.Fatal("collision accepted")
	}
	assertText(t, filepath.Join(paths.Dotfiles, "raw", ".raw"), "raw")
	assertText(t, filepath.Join(paths.Dotfiles, "raw", "dot-raw"), "existing")
	put(t, filepath.Join(paths.Dotfiles, "nested", "dot-config", "dot-x"), "nested")
	if result := doFix(t, paths, issueFor(t, paths, "nested", Unnormalized), Normalize, runner); result.OK() {
		t.Fatal("normalized deeper dot-")
	}
	put(t, filepath.Join(paths.Dotfiles, "foreign", "dot-foreign"), "repo")
	if err := os.Symlink("missing", filepath.Join(paths.Target, ".foreign")); err != nil {
		t.Fatal(err)
	}
	if result := doFix(t, paths, issueFor(t, paths, "foreign", Foreign), KeepRepo, runner); result.OK() {
		t.Fatal("foreign link modified")
	}
}

type failingRunner struct {
	stow.Runner
	dryFail bool
	cancel  context.CancelFunc
}

func (r failingRunner) DryRunRestow(pkg string) stow.Result {
	if r.cancel != nil {
		r.cancel()
	}
	if r.dryFail {
		return stow.Result{Err: errors.New("forced dry-run failure")}
	}
	return r.Runner.DryRunRestow(pkg)
}

func (r failingRunner) DryRunRestowExcluding(pkg string, entries []string) stow.Result {
	if r.cancel != nil {
		r.cancel()
	}
	if r.dryFail {
		return stow.Result{Err: errors.New("forced dry-run failure")}
	}
	return r.Runner.DryRunRestowExcluding(pkg, entries)
}

func TestFixRollback(t *testing.T) {
	for _, action := range []Action{KeepTarget, KeepRepo} {
		t.Run(string(action), func(t *testing.T) {
			paths, runner := fixture(t)
			repo, target := filepath.Join(paths.Dotfiles, "pkg", "dot-file"), filepath.Join(paths.Target, ".file")
			put(t, repo, "repo")
			put(t, target, "target")
			events := make(chan dotfiles.Event, 100)
			result := Fix(context.Background(), paths, issueFor(t, paths, "pkg", Replaced), action, failingRunner{Runner: runner, dryFail: true}, events)
			if result.OK() {
				t.Fatal("forced failure succeeded")
			}
			assertText(t, repo, "repo")
			assertText(t, target, "target")
			close(events)
			rollback := false
			for e := range events {
				if e.Kind == dotfiles.Rollback {
					rollback = true
				}
			}
			if !rollback {
				t.Fatal("no rollback event")
			}
		})
	}
}

func TestCancelledFixRollsBack(t *testing.T) {
	paths, runner := fixture(t)
	repo, target := filepath.Join(paths.Dotfiles, "pkg", "dot-file"), filepath.Join(paths.Target, ".file")
	put(t, repo, "repo")
	put(t, target, "target")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	result := Fix(ctx, paths, issueFor(t, paths, "pkg", Replaced), KeepTarget, failingRunner{Runner: runner, cancel: cancel}, nil)
	if result.OK() {
		t.Fatal("cancelled fix succeeded")
	}
	assertText(t, repo, "repo")
	assertText(t, target, "target")
}

func TestRestoreAndRestow(t *testing.T) {
	paths, runner := fixture(t)
	put(t, filepath.Join(paths.Dotfiles, "pkg", "dot-file"), "repo")
	if summary := RestowPackages(context.Background(), []string{"pkg"}, runner, nil); !summary.OK() {
		t.Fatal(summary.Err())
	}
	plan := BuildRestorePlan(paths, "pkg")
	if !plan.Runnable() {
		t.Fatalf("plan not runnable: %+v", plan)
	}
	if summary := dotfiles.ExecuteRestore(context.Background(), plan, runner, nil); !summary.OK() {
		t.Fatal(summary.Err())
	}
	assertText(t, filepath.Join(paths.Target, ".file"), "repo")
	if _, err := os.Stat(filepath.Join(paths.Dotfiles, "pkg")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("package still exists: %v", err)
	}
}

func TestEntryRestorePlanBlockedByReplaced(t *testing.T) {
	paths, runner := fixture(t)
	put(t, filepath.Join(paths.Dotfiles, "pkg", "dot-one"), "one")
	put(t, filepath.Join(paths.Dotfiles, "pkg", "dot-two"), "two")
	if summary := RestowPackages(context.Background(), []string{"pkg"}, runner, nil); !summary.OK() {
		t.Fatal(summary.Err())
	}

	healthy := BuildEntryRestorePlan(paths, "pkg", []string{"dot-one"})
	if !healthy.Runnable() || !healthy.Partial() {
		t.Fatalf("entry plan not runnable: %+v", healthy)
	}

	replaced := filepath.Join(paths.Target, ".two")
	if err := os.Remove(replaced); err != nil {
		t.Fatal(err)
	}
	put(t, replaced, "mine")
	if state := issueFor(t, paths, "pkg", Replaced).State; state != Replaced {
		t.Fatalf("state = %s, want replaced", state)
	}

	plan := BuildEntryRestorePlan(paths, "pkg", []string{"dot-two"})
	if plan.Runnable() || len(plan.Blocked) == 0 {
		t.Fatalf("a replaced entry was accepted: %+v", plan)
	}
	if other := BuildEntryRestorePlan(paths, "pkg", []string{"dot-one"}); other.Runnable() {
		t.Error("a healthy entry stayed restorable while a sibling is replaced")
	}
	assertText(t, replaced, "mine")
}

func TestEntryRestorePlanBlockedByUnnormalized(t *testing.T) {
	paths, _ := fixture(t)
	put(t, filepath.Join(paths.Dotfiles, "pkg", ".zshrc"), "repo")

	if plan := dotfiles.BuildEntryRestorePlan(paths, "pkg", []string{".zshrc"}); !plan.Runnable() {
		t.Fatalf("the mapping layer blocked an unnormalized entry on its own: %+v", plan)
	}
	plan := BuildEntryRestorePlan(paths, "pkg", []string{".zshrc"})
	if plan.Runnable() || len(plan.Blocked) != 1 {
		t.Fatalf("an unnormalized entry was accepted: %+v", plan)
	}
	if !strings.Contains(plan.Blocked[0].Reason, "Issues") {
		t.Errorf("reason = %q, want it to point to Issues", plan.Blocked[0].Reason)
	}
}

func TestDiffExitOneAndMissingGit(t *testing.T) {
	paths, _ := fixture(t)
	put(t, filepath.Join(paths.Dotfiles, "pkg", "dot-file"), "repo\n")
	put(t, filepath.Join(paths.Target, ".file"), "target\n")
	issue := issueFor(t, paths, "pkg", Replaced)
	text, err := Diff(paths, issue)
	if err != nil || !strings.Contains(text, "-repo") || !strings.Contains(text, "+target") {
		t.Fatalf("diff=%s err=%v", text, err)
	}
	t.Setenv("PATH", t.TempDir())
	text, err = Diff(paths, issue)
	if err != nil || text != "git not available for diff" {
		t.Fatalf("%s %v", text, err)
	}
}

func TestFixRevalidatesAndRejectsTraversal(t *testing.T) {
	paths, runner := fixture(t)
	put(t, filepath.Join(paths.Dotfiles, "pkg", "dot-file"), "repo")
	put(t, filepath.Join(paths.Target, ".file"), "target")
	issue := issueFor(t, paths, "pkg", Replaced)
	if err := os.Remove(filepath.Join(paths.Target, ".file")); err != nil {
		t.Fatal(err)
	}
	if result := doFix(t, paths, issue, KeepTarget, runner); result.OK() {
		t.Fatal("stale issue accepted")
	}
	issue.Entry.PkgRel = "../escape"
	if result := doFix(t, paths, issue, KeepRepo, runner); result.OK() {
		t.Fatal("traversal accepted")
	}
}

type cancelAfterRestow struct {
	stow.Runner
	cancel context.CancelFunc
}

func (r cancelAfterRestow) Restow(pkgs ...string) stow.Result {
	result := r.Runner.Restow(pkgs...)
	r.cancel()
	return result
}

func (r cancelAfterRestow) RestowExcluding(pkg string, entries []string) stow.Result {
	result := r.Runner.RestowExcluding(pkg, entries)
	r.cancel()
	return result
}

func TestFixRollbackPreservesOtherLinks(t *testing.T) {
	for _, action := range []Action{KeepTarget, KeepRepo} {
		t.Run(string(action), func(t *testing.T) {
			paths, runner := fixture(t)
			repo, target := filepath.Join(paths.Dotfiles, "pkg", "dot-config", "foo", "conf"), filepath.Join(paths.Target, ".config", "foo", "conf")
			put(t, repo, "repo")
			put(t, filepath.Join(paths.Dotfiles, "pkg", "dot-other"), "other")
			if result := runner.Restow("pkg"); result.Err != nil {
				t.Fatal(result.Err)
			}
			if err := os.Remove(filepath.Join(paths.Target, ".config")); err != nil {
				t.Fatal(err)
			}
			put(t, target, "target")
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			summary := Fix(ctx, paths, issueFor(t, paths, "pkg", Replaced), action, cancelAfterRestow{Runner: runner, cancel: cancel}, nil)
			if summary.OK() {
				t.Fatal("cancelled fix succeeded")
			}
			assertText(t, repo, "repo")
			assertText(t, target, "target")
			if _, managed := dotfiles.ManagedBy(paths, filepath.Join(paths.Target, ".other")); !managed {
				t.Fatal("rollback lost unaffected link")
			}
		})
	}
}

func TestNormalizeRollback(t *testing.T) {
	paths, runner := fixture(t)
	raw := filepath.Join(paths.Dotfiles, "pkg", ".raw")
	put(t, raw, "original")
	if result := runner.Restow("pkg"); result.Err != nil {
		t.Fatal(result.Err)
	}
	result := doFix(t, paths, issueFor(t, paths, "pkg", Unnormalized), Normalize, failingRunner{Runner: runner, dryFail: true})
	if result.OK() {
		t.Fatal("forced failure succeeded")
	}
	assertText(t, raw, "original")
	if _, managed := dotfiles.ManagedBy(paths, filepath.Join(paths.Target, ".raw")); !managed {
		t.Fatal("normalization rollback did not restore link")
	}
}

func TestUnfoldedDirectoryWithMissingLinks(t *testing.T) {
	paths, runner := fixture(t)
	put(t, filepath.Join(paths.Dotfiles, "pkg", "dot-config", "conf"), "repo")
	if err := os.Mkdir(filepath.Join(paths.Target, ".config"), 0755); err != nil {
		t.Fatal(err)
	}
	issue := issueFor(t, paths, "pkg", Missing)
	if issue.Entry.PkgRel != "dot-config/conf" {
		t.Fatal(issue.Entry.PkgRel)
	}
	if result := doFix(t, paths, issue, Restow, runner); !result.OK() {
		t.Fatal(result.Err())
	}
	issueFor(t, paths, "pkg", OK)
}

type failFirstRestow struct {
	stow.Runner
	failed bool
}

func (r *failFirstRestow) RestowExcluding(pkg string, entries []string) stow.Result {
	if !r.failed {
		r.failed = true
		return stow.Result{Err: errors.New("forced restow failure")}
	}
	return r.Runner.RestowExcluding(pkg, entries)
}

func TestRelinkUnowned(t *testing.T) {
	paths, runner := fixture(t)
	repo := filepath.Join(paths.Dotfiles, "zsh", "dot-zshrc")
	put(t, repo, "zsh")
	target := filepath.Join(paths.Target, ".zshrc")
	if err := os.Symlink(repo, target); err != nil {
		t.Fatal(err)
	}
	if summary := doFix(t, paths, issueFor(t, paths, "zsh", Unowned), Relink, runner); !summary.OK() {
		t.Fatal(summary.Err())
	}
	dest, err := os.Readlink(target)
	if err != nil || dest != filepath.Join("dotfiles", "zsh", "dot-zshrc") {
		t.Fatalf("readlink = %q, %v; want a relative stow link", dest, err)
	}
	assertText(t, target, "zsh")
	if backups, _ := filepath.Glob(filepath.Join(paths.Dotfiles, ".stower-backup-*")); len(backups) != 0 {
		t.Fatal(backups)
	}
	issueFor(t, paths, "zsh", OK)
	if result := runner.Restow("zsh"); result.Err != nil {
		t.Fatalf("stow -R after relink: %v", result.Err)
	}
}

func TestRelinkRollback(t *testing.T) {
	paths, runner := fixture(t)
	repo := filepath.Join(paths.Dotfiles, "zsh", "dot-zshrc")
	put(t, repo, "zsh")
	put(t, filepath.Join(paths.Dotfiles, "zsh", "dot-other"), "other")
	if result := runner.Restow("zsh"); result.Err != nil {
		t.Fatal(result.Err)
	}
	target := filepath.Join(paths.Target, ".zshrc")
	if err := os.Remove(target); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(repo, target); err != nil {
		t.Fatal(err)
	}
	events := make(chan dotfiles.Event, 200)
	summary := Fix(context.Background(), paths, issueFor(t, paths, "zsh", Unowned), Relink, &failFirstRestow{Runner: runner}, events)
	if summary.OK() {
		t.Fatal("forced failure succeeded")
	}
	if err := summary.Err(); err == nil || strings.Contains(err.Error(), "rollback") {
		t.Fatalf("rollback did not complete cleanly: %v", err)
	}
	close(events)
	rolledBack := false
	for e := range events {
		if e.Kind == dotfiles.Rollback {
			rolledBack = true
		}
	}
	if !rolledBack {
		t.Fatal("no rollback event")
	}
	dest, err := os.Readlink(target)
	if err != nil || dest != repo {
		t.Fatalf("readlink = %q, %v; want the original link back", dest, err)
	}
	if backups, _ := filepath.Glob(filepath.Join(paths.Dotfiles, ".stower-backup-*")); len(backups) != 0 {
		t.Fatal(backups)
	}
	if _, managed := dotfiles.ManagedBy(paths, filepath.Join(paths.Target, ".other")); !managed {
		t.Fatal("rollback lost unaffected link")
	}
	issueFor(t, paths, "zsh", Unowned)
}

func TestRelinkRejectedForOtherStates(t *testing.T) {
	paths, runner := fixture(t)
	put(t, filepath.Join(paths.Dotfiles, "pkg", "dot-file"), "repo")
	put(t, filepath.Join(paths.Target, ".file"), "target")
	if result := doFix(t, paths, issueFor(t, paths, "pkg", Replaced), Relink, runner); result.OK() {
		t.Fatal("relink accepted for a replaced entry")
	}
	assertText(t, filepath.Join(paths.Target, ".file"), "target")
	repo := filepath.Join(paths.Dotfiles, "zsh", "dot-zshrc")
	put(t, repo, "zsh")
	if err := os.Symlink(repo, filepath.Join(paths.Target, ".zshrc")); err != nil {
		t.Fatal(err)
	}
	if result := doFix(t, paths, issueFor(t, paths, "zsh", Unowned), KeepRepo, runner); result.OK() {
		t.Fatal("keep repo accepted for an unowned entry")
	}
}

func absoluteLink(t *testing.T, paths config.Paths, pkg, pkgRel string) {
	t.Helper()
	repo := filepath.Join(paths.Dotfiles, pkg, pkgRel)
	put(t, repo, pkgRel)
	target := filepath.Join(paths.Target, dotfiles.PackageToTarget(pkgRel))
	if err := os.Symlink(repo, target); err != nil {
		t.Fatal(err)
	}
}

func stateOf(t *testing.T, paths config.Paths, pkg, pkgRel string) State {
	t.Helper()
	report := InspectPackage(paths, pkg)
	if report.Err != nil {
		t.Fatal(report.Err)
	}
	for _, item := range report.Entries {
		if item.Entry.PkgRel == pkgRel {
			return item.State
		}
	}
	t.Fatalf("no entry %s in %+v", pkgRel, report)
	return OK
}

func issueOf(t *testing.T, paths config.Paths, pkg, pkgRel string) Issue {
	t.Helper()
	report := InspectPackage(paths, pkg)
	if report.Err != nil {
		t.Fatal(report.Err)
	}
	for _, item := range report.Entries {
		if item.Entry.PkgRel == pkgRel {
			return item
		}
	}
	t.Fatalf("no entry %s in %+v", pkgRel, report)
	return Issue{}
}

func assertNoBackup(t *testing.T, paths config.Paths) {
	t.Helper()
	if backups, _ := filepath.Glob(filepath.Join(paths.Dotfiles, ".stower-backup-*")); len(backups) != 0 {
		t.Fatal(backups)
	}
}

func TestRelinkOneOfSeveralUnownedEntries(t *testing.T) {
	paths, runner := fixture(t)
	absoluteLink(t, paths, "zsh", "dot-zshrc")
	absoluteLink(t, paths, "zsh", "dot-zshenv")
	envRepo := filepath.Join(paths.Dotfiles, "zsh", "dot-zshenv")

	if summary := doFix(t, paths, issueOf(t, paths, "zsh", "dot-zshrc"), Relink, runner); !summary.OK() {
		t.Fatal(summary.Err())
	}
	dest, err := os.Readlink(filepath.Join(paths.Target, ".zshrc"))
	if err != nil || dest != filepath.Join("dotfiles", "zsh", "dot-zshrc") {
		t.Fatalf("readlink = %q, %v; want a relative stow link", dest, err)
	}
	if state := stateOf(t, paths, "zsh", "dot-zshrc"); state != OK {
		t.Fatalf("dot-zshrc = %s, want ok", state)
	}
	if state := stateOf(t, paths, "zsh", "dot-zshenv"); state != Unowned {
		t.Fatalf("dot-zshenv = %s, want unowned", state)
	}
	if dest, err := os.Readlink(filepath.Join(paths.Target, ".zshenv")); err != nil || dest != envRepo {
		t.Fatalf("the sibling link changed: %q, %v", dest, err)
	}
	assertNoBackup(t, paths)

	if summary := doFix(t, paths, issueOf(t, paths, "zsh", "dot-zshenv"), Relink, runner); !summary.OK() {
		t.Fatal(summary.Err())
	}
	for _, pkgRel := range []string{"dot-zshrc", "dot-zshenv"} {
		if state := stateOf(t, paths, "zsh", pkgRel); state != OK {
			t.Fatalf("%s = %s, want ok", pkgRel, state)
		}
	}
	assertNoBackup(t, paths)
	if result := runner.Restow("zsh"); result.Err != nil {
		t.Fatalf("stow -R after both relinks: %v", result.Err)
	}
}

func TestRestowMissingNextToUnownedSibling(t *testing.T) {
	paths, runner := fixture(t)
	absoluteLink(t, paths, "zsh", "dot-zshenv")
	put(t, filepath.Join(paths.Dotfiles, "zsh", "dot-zshrc"), "repo")
	envRepo := filepath.Join(paths.Dotfiles, "zsh", "dot-zshenv")

	if state := stateOf(t, paths, "zsh", "dot-zshrc"); state != Missing {
		t.Fatalf("dot-zshrc = %s, want missing", state)
	}
	if summary := doFix(t, paths, issueOf(t, paths, "zsh", "dot-zshrc"), Restow, runner); !summary.OK() {
		t.Fatal(summary.Err())
	}
	if state := stateOf(t, paths, "zsh", "dot-zshrc"); state != OK {
		t.Fatalf("dot-zshrc = %s, want ok", state)
	}
	if state := stateOf(t, paths, "zsh", "dot-zshenv"); state != Unowned {
		t.Fatalf("dot-zshenv = %s, want unowned", state)
	}
	if dest, err := os.Readlink(filepath.Join(paths.Target, ".zshenv")); err != nil || dest != envRepo {
		t.Fatalf("the sibling link changed: %q, %v", dest, err)
	}
}

func TestRelinkRollbackWithUnownedSibling(t *testing.T) {
	paths, runner := fixture(t)
	absoluteLink(t, paths, "zsh", "dot-zshrc")
	absoluteLink(t, paths, "zsh", "dot-zshenv")
	repo := filepath.Join(paths.Dotfiles, "zsh", "dot-zshrc")
	envRepo := filepath.Join(paths.Dotfiles, "zsh", "dot-zshenv")

	summary := Fix(context.Background(), paths, issueOf(t, paths, "zsh", "dot-zshrc"), Relink, &failFirstRestow{Runner: runner}, nil)
	if summary.OK() {
		t.Fatal("forced failure succeeded")
	}
	if err := summary.Err(); err == nil || strings.Contains(err.Error(), "rollback") {
		t.Fatalf("rollback did not complete cleanly: %v", err)
	}
	if dest, err := os.Readlink(filepath.Join(paths.Target, ".zshrc")); err != nil || dest != repo {
		t.Fatalf("readlink = %q, %v; want the original link back", dest, err)
	}
	if dest, err := os.Readlink(filepath.Join(paths.Target, ".zshenv")); err != nil || dest != envRepo {
		t.Fatalf("the sibling link changed: %q, %v", dest, err)
	}
	assertNoBackup(t, paths)
	for _, pkgRel := range []string{"dot-zshrc", "dot-zshenv"} {
		if state := stateOf(t, paths, "zsh", pkgRel); state != Unowned {
			t.Fatalf("%s = %s, want unowned", pkgRel, state)
		}
	}
}
