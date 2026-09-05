package doctor

import (
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"

	"github.com/horbo/stower/internal/config"
	"github.com/horbo/stower/internal/dotfiles"
)

func Diff(paths config.Paths, issue Issue) (string, error) {
	if err := dotfiles.ValidatePackageName(issue.Package); err != nil {
		return "", err
	}
	if !filepath.IsLocal(issue.Entry.PkgRel) || issue.Entry.PkgRel == "." {
		return "", fmt.Errorf("invalid entry path")
	}
	repo := filepath.Join(paths.Dotfiles, issue.Package, issue.Entry.PkgRel)
	target := filepath.Join(paths.Target, dotfiles.PackageToTarget(issue.Entry.PkgRel))
	if err := safeParents(paths.Dotfiles, repo); err != nil {
		return "", err
	}
	if err := safeParents(paths.Target, target); err != nil {
		return "", err
	}
	bin, err := exec.LookPath("git")
	if err != nil {
		return "git not available for diff", nil
	}
	cmd := exec.Command(bin, "diff", "--no-index", "--no-color", "--no-ext-diff", "--no-textconv", "--", repo, target)
	output, err := cmd.CombinedOutput()
	var exit *exec.ExitError
	if err != nil && !(errors.As(err, &exit) && exit.ExitCode() == 1) {
		return string(output), err
	}
	if len(output) == 0 {
		return "no differences", nil
	}
	return string(output), nil
}
