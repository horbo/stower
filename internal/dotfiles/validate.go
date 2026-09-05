package dotfiles

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/kamilhorbowicz/stower/internal/config"
)

var (
	ErrInvalidPackageName = errors.New("invalid package name")
	ErrOutsideTarget      = errors.New("path is outside the target directory")
	ErrInsideDotfiles     = errors.New("path is inside the dotfiles directory")
	ErrSymlink            = errors.New("path is a symlink")
	ErrDestinationExists  = errors.New("destination already exists")
	ErrCrossDevice        = errors.New("target and dotfiles are on different devices")
)

var packageNameRE = regexp.MustCompile(`^[A-Za-z0-9._-]+$`)

func ValidatePackageName(name string) error {
	switch {
	case name == "":
		return fmt.Errorf("%w: the name is empty", ErrInvalidPackageName)
	case !packageNameRE.MatchString(name):
		return fmt.Errorf("%w: %q must match [A-Za-z0-9._-]+", ErrInvalidPackageName, name)
	case strings.HasPrefix(name, "."):
		return fmt.Errorf("%w: %q must not start with a dot", ErrInvalidPackageName, name)
	case strings.HasPrefix(name, "-"):
		return fmt.Errorf("%w: %q must not start with a dash", ErrInvalidPackageName, name)
	}
	return nil
}

func RelToTarget(paths config.Paths, path string) (string, error) {
	rel, err := filepath.Rel(paths.Target, filepath.Clean(path))
	if err != nil || rel == "." || !isRelInside(rel) {
		return "", fmt.Errorf("%w: %s", ErrOutsideTarget, path)
	}
	return rel, nil
}

func ValidateStagingPath(paths config.Paths, targetPath string) error {
	if !filepath.IsAbs(targetPath) {
		return fmt.Errorf("%w: %s is not an absolute path", ErrOutsideTarget, targetPath)
	}
	clean := filepath.Clean(targetPath)
	if _, err := RelToTarget(paths, clean); err != nil {
		return err
	}
	if isInside(paths.Dotfiles, clean) {
		return fmt.Errorf("%w: %s", ErrInsideDotfiles, clean)
	}
	info, err := os.Lstat(clean)
	if err != nil {
		return err
	}
	if info.Mode()&fs.ModeSymlink != 0 {
		return fmt.Errorf("%w: %s", ErrSymlink, clean)
	}
	return nil
}

func DedupeStaging(staging Staging) (Staging, []string) {
	ordered := make([]string, 0, len(staging))
	for path := range staging {
		ordered = append(ordered, filepath.Clean(path))
	}
	sort.Strings(ordered)

	kept := make(Staging, len(staging))
	var messages []string
	for _, path := range ordered {
		if ancestor, ok := ancestorIn(kept, path); ok {
			messages = append(messages, fmt.Sprintf("%s is already covered by %s", path, ancestor))
			continue
		}
		kept[path] = staging[path]
	}
	return kept, messages
}

func HasNestedGit(root string) (bool, error) {
	info, err := os.Lstat(root)
	if err != nil {
		return false, err
	}
	if !info.IsDir() {
		return false, nil
	}
	found := false
	err = filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if path == root {
			return nil
		}
		if entry.Name() == ".git" {
			found = true
			return filepath.SkipAll
		}
		return nil
	})
	if err != nil {
		return false, err
	}
	return found, nil
}

func CheckSameDevice(paths config.Paths) error {
	target, err := existingAncestor(paths.Target)
	if err != nil {
		return err
	}
	dotfiles, err := existingAncestor(paths.Dotfiles)
	if err != nil {
		return err
	}
	same, err := SameDevice(target, dotfiles)
	if err != nil {
		return err
	}
	if !same {
		return fmt.Errorf("%w: %s and %s", ErrCrossDevice, paths.Target, paths.Dotfiles)
	}
	return nil
}

func existingAncestor(path string) (os.FileInfo, error) {
	for dir := filepath.Clean(path); ; {
		info, err := os.Stat(dir)
		if err == nil {
			return info, nil
		}
		if !errors.Is(err, fs.ErrNotExist) {
			return nil, err
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return nil, fmt.Errorf("no existing directory above %s", path)
		}
		dir = parent
	}
}

func ancestorIn(kept Staging, path string) (string, bool) {
	for candidate := range kept {
		if isInside(candidate, path) {
			return candidate, true
		}
	}
	return "", false
}

func isInside(root, path string) bool {
	rel, err := filepath.Rel(filepath.Clean(root), filepath.Clean(path))
	if err != nil || rel == "." {
		return false
	}
	return isRelInside(rel)
}

func isRelInside(rel string) bool {
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}
