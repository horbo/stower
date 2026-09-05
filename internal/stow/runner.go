package stow

import (
	"bytes"
	"errors"
	"fmt"
	"os/exec"
	"regexp"
	"strings"
)

var (
	ErrConflict       = errors.New("stow reported conflicts")
	ErrInvalidPackage = errors.New("invalid stow package name")
	ErrNoPackages     = errors.New("no stow package given")
)

var packageNameRE = regexp.MustCompile(`^[A-Za-z0-9._-]+$`)

type Result struct {
	Args      []string
	Stdout    string
	Stderr    string
	Conflicts []string
	Err       error
}

func (r Result) Command() string {
	return strings.Join(r.Args, " ")
}

func (r Result) HasConflicts() bool {
	return len(r.Conflicts) > 0
}

func (r Result) OutputLines() []string {
	var lines []string
	for _, chunk := range []string{r.Stderr, r.Stdout} {
		for _, line := range strings.Split(chunk, "\n") {
			if strings.TrimSpace(line) != "" {
				lines = append(lines, line)
			}
		}
	}
	return lines
}

type Runner struct {
	Bin      string
	Dotfiles string
	Target   string
}

func (r Runner) DryRunRestow(pkg string) Result {
	return r.run([]string{"--dotfiles", "-n", "-v", "-R"}, pkg)
}

func (r Runner) Restow(pkgs ...string) Result {
	return r.run([]string{"--dotfiles", "-v", "-R"}, pkgs...)
}

func (r Runner) Unstow(pkg string) Result {
	return r.run([]string{"--dotfiles", "-v", "-D"}, pkg)
}

func (r Runner) run(flags []string, pkgs ...string) Result {
	bin := r.Bin
	if bin == "" {
		bin = "stow"
	}

	args := make([]string, 0, len(flags)+4+len(pkgs))
	args = append(args, flags...)
	args = append(args, "-d", r.Dotfiles, "-t", r.Target)
	args = append(args, pkgs...)
	result := Result{Args: append([]string{bin}, args...)}

	if len(pkgs) == 0 {
		result.Err = ErrNoPackages
		return result
	}
	for _, pkg := range pkgs {
		if err := ValidatePackage(pkg); err != nil {
			result.Err = err
			return result
		}
	}

	cmd := exec.Command(bin, args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	runErr := cmd.Run()

	result.Stdout = stdout.String()
	result.Stderr = stderr.String()
	result.Conflicts = ParseConflicts(result.Stderr)

	if runErr != nil {
		if len(result.Conflicts) > 0 {
			result.Err = fmt.Errorf("%w: %s", ErrConflict, strings.Join(result.Conflicts, "; "))
		} else {
			result.Err = fmt.Errorf("%s: %w%s", result.Command(), runErr, detail(result.Stderr))
		}
	}
	return result
}

func ValidatePackage(pkg string) error {
	switch {
	case pkg == "":
		return fmt.Errorf("%w: the name is empty", ErrInvalidPackage)
	case !packageNameRE.MatchString(pkg):
		return fmt.Errorf("%w: %q must match [A-Za-z0-9._-]+", ErrInvalidPackage, pkg)
	case strings.HasPrefix(pkg, "-"), pkg == ".", pkg == "..":
		return fmt.Errorf("%w: %q", ErrInvalidPackage, pkg)
	}
	return nil
}

func ParseConflicts(stderr string) []string {
	var conflicts []string
	for _, line := range strings.Split(stderr, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "* ") {
			conflicts = append(conflicts, strings.TrimSpace(trimmed[2:]))
		}
	}
	return conflicts
}

func detail(stderr string) string {
	trimmed := strings.TrimSpace(stderr)
	if trimmed == "" {
		return ""
	}
	return ": " + strings.Join(strings.Split(trimmed, "\n"), "; ")
}
