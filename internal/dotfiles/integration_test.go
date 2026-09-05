package dotfiles

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/horbo/stower/internal/config"
	"github.com/horbo/stower/internal/stow"
)

func requireStow(t *testing.T) {
	t.Helper()
	_, version, err := config.StowBinary()
	if err != nil {
		t.Skip("stow is not installed")
	}
	if !stowSupportsDotfiles(version) {
		t.Skipf("stow %s is too old: these tests need stow 2.4 or newer", version)
	}
}

func stowSupportsDotfiles(version string) bool {
	parts := strings.SplitN(version, ".", 3)
	if len(parts) < 2 {
		return false
	}
	major, err := strconv.Atoi(parts[0])
	if err != nil {
		return false
	}
	minor, err := strconv.Atoi(parts[1])
	if err != nil {
		return false
	}
	return major > 2 || (major == 2 && minor >= 4)
}

func newRunner(paths config.Paths) stow.Runner {
	return stow.Runner{Bin: "stow", Dotfiles: paths.Dotfiles, Target: paths.Target}
}

func adopt(t *testing.T, paths config.Paths, staging Staging) Summary {
	t.Helper()
	plan := BuildAdoptPlan(paths, staging)
	if plan.Fatal != nil {
		t.Fatalf("plan.Fatal = %v", plan.Fatal)
	}
	if plan.BlockedCount() != 0 {
		t.Fatalf("plan is blocked: %+v", plan.Packages)
	}
	summary := Execute(context.Background(), plan, newRunner(paths), nil)
	if !summary.OK() {
		t.Fatalf("Execute: %v", summary.Err())
	}
	return summary
}

func TestAdoptSingleFile(t *testing.T) {
	requireStow(t)
	paths := newPaths(t)
	writeFile(t, filepath.Join(paths.Target, ".bar"), "bar\n")

	adopt(t, paths, Staging{filepath.Join(paths.Target, ".bar"): "misc"})

	dest := filepath.Join(paths.Dotfiles, "misc", "dot-bar")
	if got := readFile(t, dest); got != "bar\n" {
		t.Errorf("%s = %q, want %q", dest, got, "bar\n")
	}
	link := filepath.Join(paths.Target, ".bar")
	if !isSymlink(t, link) {
		t.Fatalf("%s is not a symlink", link)
	}
	if text, err := os.Readlink(link); err != nil || text != filepath.Join("..", "dotfiles", "misc", "dot-bar") {
		t.Errorf("readlink %s = %q (%v), want a relative link into the package", link, text, err)
	}
	if pkg, ok := ManagedBy(paths, link); !ok || pkg != "misc" {
		t.Errorf("ManagedBy = (%q, %v), want (misc, true)", pkg, ok)
	}
}

func TestAdoptDirectoryIsFolded(t *testing.T) {
	requireStow(t)
	paths := newPaths(t)
	writeFile(t, filepath.Join(paths.Target, ".config", "foo", "conf"), "foo\n")

	adopt(t, paths, Staging{filepath.Join(paths.Target, ".config", "foo"): "foo"})

	dest := filepath.Join(paths.Dotfiles, "foo", "dot-config", "foo", "conf")
	if got := readFile(t, dest); got != "foo\n" {
		t.Errorf("%s = %q, want %q", dest, got, "foo\n")
	}
	link := filepath.Join(paths.Target, ".config", "foo")
	if !isSymlink(t, link) {
		t.Fatalf("%s is not a symlink", link)
	}
	if isSymlink(t, filepath.Join(paths.Target, ".config")) {
		t.Error(".config was replaced by a link, want it to stay a real directory")
	}
	if got := readFile(t, filepath.Join(link, "conf")); got != "foo\n" {
		t.Errorf("through the link = %q, want %q", got, "foo\n")
	}

	entries, err := WalkPackage(paths, "foo")
	if err != nil {
		t.Fatalf("WalkPackage: %v", err)
	}
	want := []Entry{{PkgRel: "dot-config/foo", TargetRel: ".config/foo", IsDir: true, State: Linked}}
	if len(entries) != 1 || entries[0] != want[0] {
		t.Errorf("WalkPackage = %+v, want %+v", entries, want)
	}
}

func TestAdoptFileKeepsTargetDirectoryUnfolded(t *testing.T) {
	requireStow(t)
	paths := newPaths(t)
	writeFile(t, filepath.Join(paths.Target, ".claude", "settings.json"), "{}\n")
	writeFile(t, filepath.Join(paths.Target, ".claude", "keep.txt"), "keep\n")

	adopt(t, paths, Staging{filepath.Join(paths.Target, ".claude", "settings.json"): "claude"})

	if isSymlink(t, filepath.Join(paths.Target, ".claude")) {
		t.Fatal(".claude is a symlink, want it to stay a real directory")
	}
	link := filepath.Join(paths.Target, ".claude", "settings.json")
	if !isSymlink(t, link) {
		t.Fatalf("%s is not a symlink", link)
	}
	if got := readFile(t, filepath.Join(paths.Target, ".claude", "keep.txt")); got != "keep\n" {
		t.Errorf("keep.txt = %q, want it untouched", got)
	}
	if pkg, ok := ManagedBy(paths, link); !ok || pkg != "claude" {
		t.Errorf("ManagedBy = (%q, %v), want (claude, true)", pkg, ok)
	}
}

func TestRestoreEachAdoptedShape(t *testing.T) {
	requireStow(t)
	paths := newPaths(t)
	writeFile(t, filepath.Join(paths.Target, ".bar"), "bar\n")
	writeFile(t, filepath.Join(paths.Target, ".config", "foo", "conf"), "foo\n")
	writeFile(t, filepath.Join(paths.Target, ".claude", "settings.json"), "{}\n")

	adopt(t, paths, Staging{
		filepath.Join(paths.Target, ".bar"):                     "misc",
		filepath.Join(paths.Target, ".config", "foo"):           "foo",
		filepath.Join(paths.Target, ".claude", "settings.json"): "claude",
	})

	for _, pkg := range []string{"misc", "foo", "claude"} {
		plan := BuildRestorePlan(paths, pkg)
		if !plan.Runnable() {
			t.Fatalf("restore plan for %s is not runnable: fatal=%v blocked=%+v", pkg, plan.Fatal, plan.Blocked)
		}
		summary := ExecuteRestore(context.Background(), plan, newRunner(paths), nil)
		if !summary.OK() {
			t.Fatalf("ExecuteRestore(%s): %v", pkg, summary.Err())
		}
		if exists(filepath.Join(paths.Dotfiles, pkg)) {
			t.Errorf("%s was kept in the dotfiles directory", pkg)
		}
	}

	checks := map[string]string{
		filepath.Join(paths.Target, ".bar"):                     "bar\n",
		filepath.Join(paths.Target, ".config", "foo", "conf"):   "foo\n",
		filepath.Join(paths.Target, ".claude", "settings.json"): "{}\n",
	}
	for path, want := range checks {
		if isSymlink(t, path) {
			t.Errorf("%s is still a symlink", path)
		}
		if got := readFile(t, path); got != want {
			t.Errorf("%s = %q, want %q", path, got, want)
		}
	}
	if left, err := os.ReadDir(paths.Dotfiles); err != nil || len(left) != 0 {
		t.Errorf("dotfiles directory = %v (%v), want it empty", left, err)
	}
}

func TestRestoreOneEntryKeepsTheRestLinked(t *testing.T) {
	requireStow(t)
	paths := newPaths(t)
	writeFile(t, filepath.Join(paths.Target, ".zshrc"), "zshrc\n")
	writeFile(t, filepath.Join(paths.Target, ".zprofile"), "zprofile\n")
	writeFile(t, filepath.Join(paths.Target, ".p10k.zsh"), "p10k\n")

	adopt(t, paths, Staging{
		filepath.Join(paths.Target, ".zshrc"):    "zsh",
		filepath.Join(paths.Target, ".zprofile"): "zsh",
		filepath.Join(paths.Target, ".p10k.zsh"): "zsh",
	})

	plan := BuildEntryRestorePlan(paths, "zsh", []string{"dot-zshrc"})
	if !plan.Runnable() || !plan.Partial() {
		t.Fatalf("plan is not a runnable partial plan: fatal=%v blocked=%+v", plan.Fatal, plan.Blocked)
	}
	if summary := ExecuteRestore(context.Background(), plan, newRunner(paths), nil); !summary.OK() {
		t.Fatalf("ExecuteRestore: %v", summary.Err())
	}

	restored := filepath.Join(paths.Target, ".zshrc")
	if isSymlink(t, restored) {
		t.Error(".zshrc is still a symlink")
	}
	if got := readFile(t, restored); got != "zshrc\n" {
		t.Errorf(".zshrc = %q, want %q", got, "zshrc\n")
	}
	if exists(filepath.Join(paths.Dotfiles, "zsh", "dot-zshrc")) {
		t.Error("dot-zshrc was kept in the package")
	}
	for _, name := range []string{".zprofile", ".p10k.zsh"} {
		link := filepath.Join(paths.Target, name)
		if !isSymlink(t, link) {
			t.Errorf("%s is not a symlink any more", name)
		}
		if pkg, ok := ManagedBy(paths, link); !ok || pkg != "zsh" {
			t.Errorf("ManagedBy(%s) = (%q, %v), want (zsh, true)", name, pkg, ok)
		}
	}
	if !exists(filepath.Join(paths.Dotfiles, "zsh")) {
		t.Fatal("the package directory was removed by a partial restore")
	}

	for _, rel := range []string{"dot-zprofile", "dot-p10k.zsh"} {
		plan := BuildEntryRestorePlan(paths, "zsh", []string{rel})
		if !plan.Runnable() {
			t.Fatalf("restore plan for %s is not runnable: fatal=%v blocked=%+v", rel, plan.Fatal, plan.Blocked)
		}
		if summary := ExecuteRestore(context.Background(), plan, newRunner(paths), nil); !summary.OK() {
			t.Fatalf("ExecuteRestore(%s): %v", rel, summary.Err())
		}
	}
	if exists(filepath.Join(paths.Dotfiles, "zsh")) {
		t.Error("the package directory survived the restore of the last link point")
	}
	for path, want := range map[string]string{
		filepath.Join(paths.Target, ".zprofile"): "zprofile\n",
		filepath.Join(paths.Target, ".p10k.zsh"): "p10k\n",
	} {
		if isSymlink(t, path) {
			t.Errorf("%s is still a symlink", path)
		}
		if got := readFile(t, path); got != want {
			t.Errorf("%s = %q, want %q", path, got, want)
		}
	}
}

func TestExecuteRollsBackWhenStowFails(t *testing.T) {
	paths := newPaths(t)
	writeFile(t, filepath.Join(paths.Target, ".bar"), "bar\n")
	writeFile(t, filepath.Join(paths.Target, ".config", "foo", "conf"), "foo\n")

	script := filepath.Join(t.TempDir(), "fake-stow")
	body := "#!/bin/sh\n" +
		"echo 'WARNING! stowing misc would cause conflicts:' >&2\n" +
		"echo '  * existing target is not owned by stow: .bar' >&2\n" +
		"echo 'All operations aborted.' >&2\n" +
		"exit 1\n"
	if err := os.WriteFile(script, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}

	plan := BuildAdoptPlan(paths, Staging{
		filepath.Join(paths.Target, ".bar"):           "misc",
		filepath.Join(paths.Target, ".config", "foo"): "misc",
	})
	runner := stow.Runner{Bin: script, Dotfiles: paths.Dotfiles, Target: paths.Target}

	summary := Execute(context.Background(), plan, runner, nil)
	if summary.OK() {
		t.Fatal("summary is OK, want a failure")
	}
	if got := readFile(t, filepath.Join(paths.Target, ".bar")); got != "bar\n" {
		t.Errorf(".bar = %q, want it untouched", got)
	}
	if got := readFile(t, filepath.Join(paths.Target, ".config", "foo", "conf")); got != "foo\n" {
		t.Errorf(".config/foo/conf = %q, want it untouched", got)
	}
	if exists(filepath.Join(paths.Dotfiles, "misc")) {
		t.Error("the misc package directory was left behind")
	}
	if left, err := os.ReadDir(paths.Dotfiles); err != nil || len(left) != 0 {
		t.Errorf("dotfiles directory = %v (%v), want it empty", left, err)
	}
}

func TestUnstowSkipsAReplacedLink(t *testing.T) {
	requireStow(t)
	paths := newPaths(t)
	writeFile(t, filepath.Join(paths.Target, ".bar"), "bar\n")
	writeFile(t, filepath.Join(paths.Target, ".baz"), "baz\n")

	adopt(t, paths, Staging{
		filepath.Join(paths.Target, ".bar"): "misc",
		filepath.Join(paths.Target, ".baz"): "misc",
	})

	replaced := filepath.Join(paths.Target, ".bar")
	if err := os.Remove(replaced); err != nil {
		t.Fatal(err)
	}
	writeFile(t, replaced, "mine\n")

	result := newRunner(paths).Unstow("misc")
	if result.Err != nil {
		t.Fatalf("Unstow: %v, want stow to skip the replaced link", result.Err)
	}
	if result.HasConflicts() {
		t.Errorf("Conflicts = %q, want none", result.Conflicts)
	}
	if strings.Contains(result.Stderr, "UNLINK: .bar") {
		t.Errorf("Stderr = %q, want no UNLINK for the replaced link", result.Stderr)
	}
	if !strings.Contains(result.Stderr, "UNLINK: .baz") {
		t.Errorf("Stderr = %q, want the untouched link to be removed", result.Stderr)
	}
	if got := readFile(t, replaced); got != "mine\n" {
		t.Errorf(".bar = %q, want the replacement kept in place", got)
	}
	if got := readFile(t, filepath.Join(paths.Dotfiles, "misc", "dot-bar")); got != "bar\n" {
		t.Errorf("dot-bar = %q, want the package copy kept", got)
	}

	plan := BuildRestorePlan(paths, "misc")
	if len(plan.Blocked) != 1 || plan.Runnable() {
		t.Errorf("restore plan blocked = %+v, runnable = %v, want one blocked entry", plan.Blocked, plan.Runnable())
	}
}

func TestStowTranslatesDotPrefixAtEveryLevel(t *testing.T) {
	requireStow(t)
	paths := newPaths(t)
	writeFile(t, filepath.Join(paths.Dotfiles, "deep", "dot-config", "dot-inner", "file"), "x\n")
	mkdir(t, filepath.Join(paths.Target, ".config"))

	result := newRunner(paths).Restow("deep")
	if result.Err != nil {
		t.Fatalf("Restow: %v", result.Err)
	}

	translated := filepath.Join(paths.Target, ".config", ".inner")
	if !exists(translated) {
		t.Fatalf("stow 2.4.1 did not translate dot- below the first component; stderr = %q", result.Stderr)
	}
	if exists(filepath.Join(paths.Target, ".config", "dot-inner")) {
		t.Error("stow kept the literal dot-inner name, which contradicts the observed 2.4.1 behaviour")
	}
	if got := PackageToTarget("dot-config/dot-inner"); got != filepath.Join(".config", "dot-inner") {
		t.Errorf("PackageToTarget = %q, want the first-component-only mapping from DESIGN.md", got)
	}
}
