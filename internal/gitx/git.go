package gitx

import (
	"bytes"
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

var (
	ErrNoDirectory     = errors.New("no directory given")
	ErrInvalidPathspec = errors.New("invalid path")
	ErrInvalidSubject  = errors.New("invalid commit subject")
	ErrNothingToCommit = errors.New("nothing to commit")
)

func Available() bool {
	_, err := exec.LookPath("git")
	return err == nil
}

func IsRepo(dir string) bool {
	out, err := run(dir, "rev-parse", "--show-toplevel")
	if err != nil {
		return false
	}
	root, err := filepath.EvalSymlinks(strings.TrimSpace(out))
	if err != nil {
		return false
	}
	self, err := filepath.EvalSymlinks(dir)
	if err != nil {
		return false
	}
	return root == self
}

func Init(dir string) error {
	_, err := run(dir, "init")
	return err
}

func Porcelain(dir string, pkgs ...string) ([]string, error) {
	args := []string{"-c", "core.quotepath=false", "status", "--porcelain", "--untracked-files=all"}
	if len(pkgs) > 0 {
		args = append(args, "--")
		for _, pkg := range pkgs {
			if err := validatePathspec(pkg); err != nil {
				return nil, err
			}
			args = append(args, pkg)
		}
	}
	out, err := run(dir, args...)
	if err != nil {
		return nil, err
	}
	var lines []string
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimRight(line, "\r")
		if strings.TrimSpace(line) != "" {
			lines = append(lines, line)
		}
	}
	return lines, nil
}

func DirtyPaths(dir, pkg string) ([]string, error) {
	lines, err := Porcelain(dir, pkg)
	if err != nil {
		return nil, err
	}
	paths := make([]string, 0, len(lines))
	for _, line := range lines {
		if path := StatusPath(line); path != "" {
			paths = append(paths, path)
		}
	}
	return paths, nil
}

func StatusPath(line string) string {
	if len(line) < 4 {
		return ""
	}
	path := line[3:]
	if i := strings.Index(path, " -> "); i >= 0 {
		path = path[i+len(" -> "):]
	}
	if strings.HasPrefix(path, `"`) {
		if unquoted, err := strconv.Unquote(path); err == nil {
			return unquoted
		}
	}
	return path
}

func AddAndCommit(dir string, pkgs []string, subject string) error {
	if len(pkgs) == 0 {
		return fmt.Errorf("%w: no packages given", ErrInvalidPathspec)
	}
	if err := validateSubject(subject); err != nil {
		return err
	}
	args := make([]string, 0, len(pkgs)+3)
	args = append(args, "add", "-A", "--")
	for _, pkg := range pkgs {
		if err := validatePathspec(pkg); err != nil {
			return err
		}
		args = append(args, pkg)
	}
	if _, err := run(dir, args...); err != nil {
		return err
	}
	staged, err := hasStagedChanges(dir)
	if err != nil {
		return err
	}
	if !staged {
		return ErrNothingToCommit
	}
	_, err = run(dir, "commit", "-m", subject)
	return err
}

func hasStagedChanges(dir string) (bool, error) {
	cmd, err := command(dir, "diff", "--cached", "--quiet")
	if err != nil {
		return false, err
	}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	runErr := cmd.Run()
	if runErr == nil {
		return false, nil
	}
	var exit *exec.ExitError
	if errors.As(runErr, &exit) && exit.ExitCode() == 1 {
		return true, nil
	}
	return false, fmt.Errorf("git diff --cached: %w%s", runErr, detail(stderr.String()))
}

func command(dir string, args ...string) (*exec.Cmd, error) {
	if strings.TrimSpace(dir) == "" {
		return nil, ErrNoDirectory
	}
	full := make([]string, 0, len(args)+2)
	full = append(full, "-C", dir)
	full = append(full, args...)
	return exec.Command("git", full...), nil
}

func run(dir string, args ...string) (string, error) {
	cmd, err := command(dir, args...)
	if err != nil {
		return "", err
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if runErr := cmd.Run(); runErr != nil {
		return stdout.String(), fmt.Errorf("git %s: %w%s", strings.Join(args, " "), runErr, detail(stderr.String()))
	}
	return stdout.String(), nil
}

func detail(stderr string) string {
	trimmed := strings.TrimSpace(stderr)
	if trimmed == "" {
		return ""
	}
	return ": " + strings.Join(strings.Split(trimmed, "\n"), "; ")
}

func validatePathspec(name string) error {
	switch {
	case name == "":
		return fmt.Errorf("%w: the name is empty", ErrInvalidPathspec)
	case !filepath.IsLocal(name):
		return fmt.Errorf("%w: %q is not a local path", ErrInvalidPathspec, name)
	case strings.ContainsRune(name, filepath.Separator), strings.ContainsRune(name, '/'):
		return fmt.Errorf("%w: %q is not a top-level entry", ErrInvalidPathspec, name)
	}
	return nil
}

func validateSubject(subject string) error {
	if strings.TrimSpace(subject) == "" {
		return fmt.Errorf("%w: the subject is empty", ErrInvalidSubject)
	}
	if strings.ContainsAny(subject, "\n\r") {
		return fmt.Errorf("%w: the subject must be a single line", ErrInvalidSubject)
	}
	return nil
}
