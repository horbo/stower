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
	for _, pkg := range []string{"healthy", "missing", "replaced", "foreign"} {
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
	put(t, filepath.Join(paths.Dotfiles, "raw", ".zshenv"), "raw")
	put(t, filepath.Join(paths.Dotfiles, "nested", "dot-config", "dot-deep"), "nested")
	if result := runner.Restow("nested"); result.Err != nil {
		t.Fatal(result.Err)
	}
	for pkg, state := range map[string]State{"healthy": OK, "missing": Missing, "replaced": Replaced, "foreign": Foreign, "raw": Unnormalized, "nested": Unnormalized} {
		issueFor(t, paths, pkg, state)
	}
	issue := issueFor(t, paths, "nested", Unnormalized)
	if issue.Fixable {
		t.Fatal("nested dot- is fixable")
	}
	issues, reports, err := Inspect(paths)
	if err != nil || len(issues) != 5 || len(reports) != 6 {
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
