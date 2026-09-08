package gitx

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	neturl "net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

type Repository struct {
	Path   string
	GitDir string
	Head   string
	URL    string
	Dirty  bool
	Reason string
}

type Submodule struct {
	Name string
	Path string
	URL  string
}

func runContext(ctx context.Context, dir string, args ...string) (string, error) {
	cmd, err := command(dir, args...)
	if err != nil {
		return "", err
	}
	cmd = exec.CommandContext(ctx, cmd.Args[0], cmd.Args[1:]...)
	cmd.Env = append(os.Environ(), "GIT_OPTIONAL_LOCKS=0")
	var out, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &stderr
	if err := cmd.Run(); err != nil {
		return out.String(), fmt.Errorf("git %s: %w%s", strings.Join(args, " "), err, detail(stderr.String()))
	}
	return out.String(), nil
}

func InspectRepository(ctx context.Context, path string) Repository {
	r := Repository{Path: path}
	fail := func(reason string) Repository { r.Reason = reason; return r }
	info, err := os.Lstat(filepath.Join(path, ".git"))
	if err != nil || !info.IsDir() {
		return fail("conversion requires a repository with its own .git directory")
	}
	r.GitDir = filepath.Join(path, ".git")
	for _, name := range []string{"HEAD", "config", "index", "objects", "refs"} {
		info, err := os.Lstat(filepath.Join(r.GitDir, name))
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			return fail(err.Error())
		}
		if info.Mode()&fs.ModeSymlink != 0 {
			return fail("symbolic links in Git metadata are not supported")
		}
	}

	top, err := runContext(ctx, path, "rev-parse", "--show-toplevel")
	actual, resolveErr := filepath.EvalSymlinks(path)
	if err != nil || resolveErr != nil || strings.TrimSpace(top) != actual {
		return fail("not a standalone Git repository")
	}
	gd, err := runContext(ctx, path, "rev-parse", "--absolute-git-dir")
	if err != nil {
		return fail(err.Error())
	}
	r.GitDir = strings.TrimSpace(gd)
	for _, name := range []string{"commondir", "objects/info/alternates", "worktrees"} {
		if _, err := os.Lstat(filepath.Join(r.GitDir, name)); !errors.Is(err, fs.ErrNotExist) {
			return fail("worktrees and shared Git metadata are not supported")
		}
	}
	if out, _ := runContext(ctx, path, "config", "--get", "core.worktree"); strings.TrimSpace(out) != "" {
		return fail("external work trees are not supported")
	}
	head, err := runContext(ctx, path, "rev-parse", "--verify", "HEAD^{commit}")
	if err != nil {
		return fail("repository has no commit")
	}
	r.Head = strings.TrimSpace(head)
	if out, err := runContext(ctx, path, "ls-files", "--stage", "-z"); err != nil {
		return fail(err.Error())
	} else {
		for _, record := range strings.Split(out, "\x00") {
			if strings.HasPrefix(record, "160000 ") {
				return fail("repositories containing submodules are not supported")
			}
		}
	}
	err = filepath.WalkDir(path, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if err = ctx.Err(); err != nil {
			return err
		}
		if p == filepath.Join(path, ".git") {
			return filepath.SkipDir
		}
		if p != path && d.Name() == ".git" {
			return errors.New("repositories containing other repositories are not supported")
		}
		return nil
	})
	if err != nil {
		return fail(err.Error())
	}
	remote, _ := runContext(ctx, path, "config", "--get", "remote.origin.url")
	r.URL = strings.TrimSpace(remote)
	if r.URL != "" && !strings.Contains(r.URL, ":") && !filepath.IsAbs(r.URL) {
		r.URL = filepath.Join(path, r.URL)
	}
	status, err := runContext(ctx, path, "status", "--porcelain", "--untracked-files=all")
	if err != nil {
		return fail(err.Error())
	}
	r.Dirty = status != ""
	return r
}

const maxSubmoduleURL = 2048

var submoduleURLSchemes = []string{"https", "ssh", "git"}

func ValidateSubmoduleURL(raw string) error {
	if strings.TrimSpace(raw) == "" {
		return errors.New("a repository URL is required")
	}
	if raw != strings.TrimSpace(raw) {
		return errors.New("the repository URL must not start or end with whitespace")
	}
	if len(raw) > maxSubmoduleURL {
		return fmt.Errorf("the repository URL must not exceed %d bytes", maxSubmoduleURL)
	}
	for _, r := range raw {
		if r < 0x20 || r == 0x7f {
			return errors.New("the repository URL contains control characters")
		}
	}
	if strings.HasPrefix(raw, "-") {
		return errors.New("the repository URL must not start with a dash")
	}
	if strings.Contains(raw, "::") {
		return errors.New("remote helpers are not allowed in a repository URL")
	}
	if scheme, rest, ok := strings.Cut(raw, "://"); ok {
		return validateSubmoduleSchemeURL(scheme, rest)
	}
	if filepath.IsAbs(raw) {
		return nil
	}
	host, path, ok := strings.Cut(raw, ":")
	if !ok || strings.Contains(host, "/") {
		return errors.New("use https://, ssh://, git://, git@host:path or an absolute local path")
	}
	if err := validateSubmoduleHost(host); err != nil {
		return err
	}
	if path == "" {
		return errors.New("the repository URL has no path")
	}
	return nil
}

func validateSubmoduleSchemeURL(scheme, rest string) error {
	lower := strings.ToLower(scheme)
	allowed := false
	for _, s := range submoduleURLSchemes {
		if lower == s {
			allowed = true
		}
	}
	if !allowed {
		return fmt.Errorf("unsupported URL scheme %q; use https, ssh or git", scheme)
	}
	parsed, err := neturl.Parse(lower + "://" + rest)
	if err != nil {
		return errors.New("invalid repository URL")
	}
	if parsed.User != nil {
		if _, ok := parsed.User.Password(); ok {
			return errors.New("remove the credentials from the repository URL")
		}
	}
	if err := validateSubmoduleHost(parsed.Hostname()); err != nil {
		return err
	}
	if strings.Trim(parsed.EscapedPath(), "/") == "" {
		return errors.New("the repository URL has no path")
	}
	return nil
}

func validateSubmoduleHost(host string) error {
	if host == "" {
		return errors.New("the repository URL has no host")
	}
	if strings.HasPrefix(host, "-") {
		return errors.New("the repository host must not start with a dash")
	}
	return nil
}

func CheckSubmoduleMetadata(ctx context.Context, dir string) error {
	if !IsRepo(dir) {
		return errors.New("dotfiles must be the root of a Git repository")
	}
	info, err := os.Lstat(filepath.Join(dir, ".git"))
	if err != nil || !info.IsDir() {
		return errors.New("dotfiles must have its own .git directory")
	}
	for _, name := range []string{".gitmodules", ".git/config", ".git/index"} {
		if info, err := os.Lstat(filepath.Join(dir, name)); err == nil && !info.Mode().IsRegular() {
			return fmt.Errorf("unsafe Git metadata: %s", name)
		} else if err != nil && !errors.Is(err, fs.ErrNotExist) {
			return err
		}
	}
	out, err := runContext(ctx, dir, "ls-files", "--unmerged", "-z")
	if err != nil {
		return err
	}
	if out != "" {
		return errors.New("resolve index conflicts first")
	}
	out, err = runContext(ctx, dir, "diff", "--name-only", "--", ".gitmodules")
	if err != nil {
		return err
	}
	if out != "" {
		return errors.New("commit or stage existing .gitmodules changes first")
	}
	out, err = runContext(ctx, dir, "ls-files", "--others", "--exclude-standard", "--", ".gitmodules")
	if err != nil {
		return err
	}
	if out != "" {
		return errors.New("commit existing .gitmodules first")
	}
	if err := checkPhantomGitlinks(ctx, dir); err != nil {
		return err
	}
	return checkStagedModules(ctx, dir)
}

func checkPhantomGitlinks(ctx context.Context, dir string) error {
	links, err := IndexGitlinks(ctx, dir)
	if err != nil {
		return err
	}
	if len(links) == 0 {
		return nil
	}
	modules, err := ListSubmodules(ctx, dir)
	if err != nil {
		return err
	}
	for _, link := range links {
		registered := false
		for _, m := range modules {
			if filepath.ToSlash(m.Path) == link {
				registered = true
			}
		}
		if !registered {
			return fmt.Errorf("%s is recorded as a Git link but is missing from .gitmodules; content under that path is invisible to Git", link)
		}
	}
	return nil
}

func ListSubmodules(ctx context.Context, dir string) ([]Submodule, error) {
	file := filepath.Join(dir, ".gitmodules")
	info, err := os.Lstat(file)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, errors.New(".gitmodules must be a regular file")
	}
	data, err := readConfigContent(file)
	if err != nil {
		return nil, err
	}
	if err = rejectConfigIncludes(data); err != nil {
		return nil, err
	}
	out, err := runContext(ctx, dir, "config", "--null", "--file", file, "--get-regexp", `^submodule\..*\.path$`)
	if err != nil {
		if out != "" || !isNoMatchingKeys(err) {
			return nil, err
		}
		return nil, nil
	}
	var result []Submodule
	for _, record := range strings.Split(out, "\x00") {
		if record == "" {
			continue
		}
		key, path, ok := strings.Cut(record, "\n")
		if !ok {
			return nil, errors.New("invalid .gitmodules entry")
		}
		name := strings.TrimSuffix(strings.TrimPrefix(key, "submodule."), ".path")
		if err := safeRelative(path); err != nil {
			return nil, err
		}
		if err := safeRelative(name); err != nil {
			return nil, err
		}
		url, err := runContext(ctx, dir, "config", "--file", file, "--get", "submodule."+name+".url")
		if err != nil {
			return nil, err
		}
		result = append(result, Submodule{Name: name, Path: path, URL: strings.TrimSpace(url)})
	}
	return result, nil
}

const maxConfigContent = 1 << 20

func readConfigContent(path string) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, maxConfigContent+1))
	if err != nil {
		return nil, err
	}
	if len(data) > maxConfigContent {
		return nil, fmt.Errorf("%s is larger than %d bytes", filepath.Base(path), maxConfigContent)
	}
	return data, nil
}

func rejectConfigIncludes(data []byte) error {
	for _, line := range strings.Split(string(data), "\n") {
		trimmed := strings.TrimSpace(line)
		if !strings.HasPrefix(trimmed, "[") {
			continue
		}
		section := strings.ToLower(strings.TrimPrefix(trimmed, "["))
		if strings.HasPrefix(section, "include]") || strings.HasPrefix(section, "include ") ||
			strings.HasPrefix(section, "includeif]") || strings.HasPrefix(section, "includeif ") ||
			strings.HasPrefix(section, "includeif\"") {
			return errors.New("include directives in .gitmodules are not supported")
		}
	}
	return nil
}

func isNoMatchingKeys(err error) bool {
	var exit *exec.ExitError
	return errors.As(err, &exit) && exit.ExitCode() == 1
}

func safeRelative(path string) error {
	if !filepath.IsLocal(path) || path == "." || strings.ContainsAny(path, "\x00\r\n") || strings.HasPrefix(path, "-") {
		return fmt.Errorf("invalid submodule path: %q", path)
	}
	for _, part := range strings.Split(filepath.ToSlash(path), "/") {
		if strings.EqualFold(part, ".git") || part == ".." {
			return fmt.Errorf("unsafe submodule path: %q", path)
		}
	}
	return nil
}

func safeParents(root, path string) error {
	root = filepath.Clean(root)
	path = filepath.Clean(path)
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return err
	}
	if err = safeRelative(rel); err != nil {
		return err
	}
	reached := false
	for p := filepath.Dir(path); ; p = filepath.Dir(p) {
		if p == root {
			reached = true
			break
		}
		info, err := os.Lstat(p)
		if err != nil && !errors.Is(err, fs.ErrNotExist) {
			return err
		}
		if err == nil && !info.IsDir() {
			return fmt.Errorf("unsafe parent: %s", p)
		}
		if p == filepath.Dir(p) {
			break
		}
	}
	if !reached {
		return fmt.Errorf("path is outside %s: %s", root, path)
	}
	return nil
}

func IndexGitlinks(ctx context.Context, dir string) ([]string, error) {
	out, err := runContext(ctx, dir, "ls-files", "--stage", "-z")
	if err != nil {
		return nil, err
	}
	var links []string
	for _, record := range strings.Split(out, "\x00") {
		if !strings.HasPrefix(record, "160000 ") {
			continue
		}
		_, path, ok := strings.Cut(record, "\t")
		if !ok {
			return nil, errors.New("invalid index entry")
		}
		links = append(links, path)
	}
	return links, nil
}

func isPathWithin(path, other string) bool {
	return path == other ||
		strings.HasPrefix(path, other+"/") ||
		strings.HasPrefix(other, path+"/")
}

func ValidateSubmoduleDestination(ctx context.Context, dir, rel string) error {
	if err := safeRelative(rel); err != nil {
		return err
	}
	if err := safeParents(dir, filepath.Join(dir, rel)); err != nil {
		return err
	}
	if err := safeParents(filepath.Join(dir, ".git"), filepath.Join(dir, ".git", "modules", rel)); err != nil {
		return err
	}
	out, err := runContext(ctx, dir, "ls-files", "--stage", "-z", "--", ":(literal)"+filepath.ToSlash(rel))
	if err != nil {
		return err
	}
	if out != "" {
		return fmt.Errorf("submodule destination is already tracked: %s", rel)
	}
	modules, err := ListSubmodules(ctx, dir)
	if err != nil {
		return err
	}
	slashed := filepath.ToSlash(rel)
	for _, m := range modules {
		if isPathWithin(slashed, filepath.ToSlash(m.Path)) || isPathWithin(slashed, filepath.ToSlash(m.Name)) {
			return fmt.Errorf("submodule already registered: %s", m.Path)
		}
	}
	if out, _ := runContext(ctx, dir, "config", "--get-regexp", `^submodule\.`); out != "" {
		for _, name := range configuredSubmoduleNames(out) {
			if isPathWithin(slashed, name) {
				return fmt.Errorf("submodule configuration already exists: %s", name)
			}
		}
	}
	links, err := IndexGitlinks(ctx, dir)
	if err != nil {
		return err
	}
	for _, link := range links {
		if isPathWithin(slashed, link) {
			return fmt.Errorf("a Git link already covers this path: %s", link)
		}
	}
	return checkLeftoverModuleDir(dir, rel)
}

func checkLeftoverModuleDir(dir, rel string) error {
	moduleDir := filepath.Join(dir, ".git", "modules", rel)
	info, err := os.Lstat(moduleDir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return fmt.Errorf("unsafe submodule metadata: %s", moduleDir)
	}
	if _, err := os.Lstat(filepath.Join(moduleDir, "HEAD")); errors.Is(err, fs.ErrNotExist) {
		return nil
	} else if err != nil {
		return err
	}
	return fmt.Errorf("leftover submodule metadata from an earlier registration: remove %s first", moduleDir)
}

func configuredSubmoduleNames(out string) []string {
	var names []string
	for _, line := range strings.Split(out, "\n") {
		key, _, ok := strings.Cut(line, " ")
		if !ok || !strings.HasPrefix(key, "submodule.") {
			continue
		}
		key = strings.TrimPrefix(key, "submodule.")
		last := strings.LastIndex(key, ".")
		if last <= 0 {
			continue
		}
		names = append(names, key[:last])
	}
	return names
}

type metadataFile struct {
	path   string
	data   []byte
	mode   fs.FileMode
	exists bool
}
type SubmoduleTransaction struct {
	dir         string
	recoveryDir string
	files       []metadataFile
	undo        []func() error
}

func BeginSubmoduleTransaction(dir string) (*SubmoduleTransaction, error) {
	backup, err := os.MkdirTemp(filepath.Join(dir, ".git"), "stower-rollback-")
	if err != nil {
		return nil, err
	}
	t := &SubmoduleTransaction{dir: dir, recoveryDir: backup}
	complete := false
	defer func() {
		if !complete {
			os.RemoveAll(backup)
		}
	}()
	for _, name := range []string{".gitmodules", ".git/config", ".git/index"} {
		path := filepath.Join(dir, name)
		f := metadataFile{path: path}
		info, err := os.Lstat(path)
		if err == nil {
			if !info.Mode().IsRegular() {
				return nil, fmt.Errorf("unsafe metadata: %s", path)
			}
			f.exists = true
			f.mode = info.Mode()
			f.data, err = os.ReadFile(path)
		}
		if err != nil && !errors.Is(err, fs.ErrNotExist) {
			return nil, err
		}
		if f.exists {
			if err := os.WriteFile(filepath.Join(backup, filepath.Base(name)), f.data, 0600); err != nil {
				return nil, err
			}
		}
		t.files = append(t.files, f)
	}
	complete = true
	return t, nil
}

func (t *SubmoduleTransaction) Register(ctx context.Context, rel, url, head string) error {
	if err := ValidateSubmoduleURL(url); err != nil {
		return err
	}
	if err := ValidateSubmoduleDestination(ctx, t.dir, rel); err != nil {
		return err
	}
	path := filepath.Join(t.dir, rel)
	original := InspectRepository(ctx, path)
	if original.Reason != "" {
		return errors.New(original.Reason)
	}
	if original.Head != head {
		return errors.New("repository HEAD changed; rebuild the plan")
	}
	config := filepath.Join(path, ".git", "config")
	content, err := os.ReadFile(config)
	if err != nil {
		return err
	}
	info, err := os.Stat(config)
	if err != nil {
		return err
	}
	moduleDir := filepath.Join(t.dir, ".git", "modules", rel)
	t.undo = append(t.undo, func() error {
		gitPath := filepath.Join(path, ".git")
		data, readErr := os.ReadFile(gitPath)
		absorbed := (readErr == nil && strings.HasPrefix(string(data), "gitdir: ")) || errors.Is(readErr, fs.ErrNotExist)
		if absorbed {
			if readErr == nil {
				if err := os.Remove(gitPath); err != nil {
					return err
				}
			}
			if err := os.Rename(moduleDir, gitPath); err != nil {
				return fmt.Errorf("metadata retained at %s: %w", moduleDir, err)
			}
		}
		if gitInfo, err := os.Lstat(gitPath); err != nil || !gitInfo.IsDir() {
			return err
		}
		return os.WriteFile(filepath.Join(gitPath, "config"), content, info.Mode())
	})
	if _, err := runMutation(ctx, t.dir, "submodule", "add", "--name", filepath.ToSlash(rel), "--", url, filepath.ToSlash(rel)); err != nil {
		return err
	}
	_, err = runMutation(ctx, t.dir, "submodule", "absorbgitdirs", "--", filepath.ToSlash(rel))
	return err
}

func (t *SubmoduleTransaction) Detach(ctx context.Context, m Submodule) error {
	path := filepath.Join(t.dir, m.Path)
	gd, err := ValidateRestorableSubmodule(ctx, t.dir, m)
	if err != nil {
		return err
	}
	marker := filepath.Join(path, ".git")
	info, err := os.Lstat(marker)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		original, err := os.ReadFile(marker)
		if err != nil {
			return err
		}
		temp, err := os.MkdirTemp(path, ".stower-git-")
		if err != nil {
			return err
		}
		moved := false
		defer func() {
			if !moved {
				os.RemoveAll(temp)
			}
		}()
		if err = copyGitMetadata(ctx, gd, temp); err != nil {
			return err
		}
		if err = ctx.Err(); err != nil {
			return err
		}
		if err = os.Remove(marker); err != nil {
			return err
		}
		if err = os.Rename(temp, marker); err != nil {
			os.WriteFile(marker, original, info.Mode())
			return err
		}
		moved = true
		t.undo = append(t.undo, func() error {
			if err := os.RemoveAll(marker); err != nil {
				return err
			}
			return os.WriteFile(marker, original, info.Mode())
		})
		if _, err = runMutation(ctx, t.dir, "config", "--file", filepath.Join(marker, "config"), "--unset-all", "core.worktree"); err != nil {
			return err
		}
	}
	if _, err = runMutation(ctx, t.dir, "update-index", "--force-remove", "--", filepath.ToSlash(m.Path)); err != nil {
		return err
	}
	if _, err = runMutation(ctx, t.dir, "config", "--file", filepath.Join(t.dir, ".gitmodules"), "--remove-section", "submodule."+m.Name); err != nil {
		return err
	}
	if out, _ := runContext(ctx, t.dir, "config", "--get-regexp", `^submodule\.`); strings.Contains(out, "submodule."+m.Name+".") {
		if _, err = runMutation(ctx, t.dir, "config", "--remove-section", "submodule."+m.Name); err != nil {
			return err
		}
	}
	modulesFile := filepath.Join(t.dir, ".gitmodules")
	data, err := os.ReadFile(modulesFile)
	if err != nil {
		return err
	}
	if len(bytes.TrimSpace(data)) == 0 {
		if err := os.Remove(modulesFile); err != nil {
			return err
		}
	}
	_, err = runMutation(ctx, t.dir, "add", "-A", "--", ".gitmodules")
	return err
}

func ValidateRestorableSubmodule(ctx context.Context, dir string, m Submodule) (string, error) {
	if err := safeRelative(m.Path); err != nil {
		return "", err
	}
	if err := safeRelative(m.Name); err != nil {
		return "", err
	}
	path := filepath.Join(dir, m.Path)
	if err := safeParents(dir, path); err != nil {
		return "", err
	}
	staged, err := runContext(ctx, dir, "ls-files", "--stage", "-z", "--", ":(literal)"+filepath.ToSlash(m.Path))
	if err != nil {
		return "", err
	}
	if !strings.HasPrefix(staged, "160000 ") {
		return "", errors.New("submodule has no matching gitlink")
	}
	gd, err := runContext(ctx, path, "rev-parse", "--absolute-git-dir")
	if err != nil {
		return "", errors.New("submodule must be initialized before Restore")
	}
	gd = strings.TrimSpace(gd)
	actual, err := filepath.EvalSymlinks(gd)
	if err != nil {
		return "", err
	}
	canonicalDir, err := filepath.EvalSymlinks(dir)
	if err != nil {
		return "", err
	}
	expected := filepath.Join(canonicalDir, ".git", "modules", m.Name)
	embedded := filepath.Join(canonicalDir, m.Path, ".git")
	if actual != expected && actual != embedded {
		return "", errors.New("external or shared submodule metadata is not supported")
	}
	for _, name := range []string{"commondir", "objects/info/alternates", "worktrees"} {
		if _, err := os.Lstat(filepath.Join(gd, name)); !errors.Is(err, fs.ErrNotExist) {
			return "", errors.New("shared submodule metadata is not supported")
		}
	}
	out, err := runContext(ctx, path, "ls-files", "--stage", "-z")
	if err != nil {
		return "", err
	}
	for _, record := range strings.Split(out, "\x00") {
		if strings.HasPrefix(record, "160000 ") {
			return "", errors.New("nested submodules are not supported")
		}
	}
	return gd, nil
}

func (t *SubmoduleTransaction) Rollback() error {
	var errs []error
	for i := len(t.undo) - 1; i >= 0; i-- {
		if err := t.undo[i](); err != nil {
			errs = append(errs, err)
		}
	}
	for _, f := range t.files {
		var err error
		if f.exists {
			err = os.WriteFile(f.path, f.data, f.mode)
		} else {
			err = os.Remove(f.path)
			if errors.Is(err, fs.ErrNotExist) {
				err = nil
			}
		}
		if err != nil {
			errs = append(errs, fmt.Errorf("restore %s: %w", f.path, err))
		}
	}
	if len(errs) > 0 {
		return fmt.Errorf("Git rollback incomplete; original metadata retained at %s: %w", t.recoveryDir, errors.Join(errs...))
	}
	return t.Close()
}

func (t *SubmoduleTransaction) Close() error {
	if err := os.RemoveAll(t.recoveryDir); err != nil {
		return fmt.Errorf("Git recovery files retained at %s: %w", t.recoveryDir, err)
	}
	return nil
}

func runMutation(ctx context.Context, dir string, args ...string) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	out, err := runContext(context.WithoutCancel(ctx), dir, args...)
	return out, errors.Join(err, ctx.Err())
}
