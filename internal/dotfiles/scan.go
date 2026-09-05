package dotfiles

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/horbo/stower/internal/config"
)

type State int

const (
	Linked State = iota
	Unlinked
	Conflict
)

func (s State) String() string {
	switch s {
	case Linked:
		return "linked"
	case Unlinked:
		return "unlinked"
	case Conflict:
		return "conflict"
	default:
		return "unknown"
	}
}

type Entry struct {
	PkgRel    string
	TargetRel string
	IsDir     bool
	State     State
}

func (e Entry) PackagePath(paths config.Paths, pkg string) string {
	return filepath.Join(paths.Dotfiles, pkg, e.PkgRel)
}

func (e Entry) TargetPath(paths config.Paths) string {
	return filepath.Join(paths.Target, e.TargetRel)
}

func ListPackages(paths config.Paths) ([]string, error) {
	entries, err := os.ReadDir(paths.Dotfiles)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	var names []string
	for _, entry := range entries {
		if !entry.IsDir() || strings.HasPrefix(entry.Name(), ".") {
			continue
		}
		names = append(names, entry.Name())
	}
	return names, nil
}

func WalkPackage(paths config.Paths, pkg string) ([]Entry, error) {
	if err := ValidatePackageName(pkg); err != nil {
		return nil, err
	}
	root := filepath.Join(paths.Dotfiles, pkg)
	info, err := os.Stat(root)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("%s is not a directory", root)
	}
	var entries []Entry
	if err := walkPackageDir(paths, pkg, "", &entries); err != nil {
		return nil, err
	}
	return entries, nil
}

func walkPackageDir(paths config.Paths, pkg, relPkg string, out *[]Entry) error {
	dir := filepath.Join(paths.Dotfiles, pkg, relPkg)
	children, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	for _, child := range children {
		pkgRel := filepath.Join(relPkg, child.Name())
		entry := Entry{
			PkgRel:    pkgRel,
			TargetRel: PackageToTarget(pkgRel),
			IsDir:     child.IsDir(),
		}
		targetPath := filepath.Join(paths.Target, entry.TargetRel)

		info, err := os.Lstat(targetPath)
		switch {
		case errors.Is(err, fs.ErrNotExist):
			entry.State = Unlinked
		case err != nil:
			return err
		case info.Mode()&fs.ModeSymlink != 0:
			if pointsAt(targetPath, filepath.Join(paths.Dotfiles, pkg, pkgRel)) {
				entry.State = Linked
			} else {
				entry.State = Conflict
			}
		case info.IsDir() && child.IsDir():
			if err := walkPackageDir(paths, pkg, pkgRel, out); err != nil {
				return err
			}
			continue
		default:
			entry.State = Conflict
		}
		*out = append(*out, entry)
	}
	return nil
}

func ManagedBy(paths config.Paths, targetPath string) (string, bool) {
	info, err := os.Lstat(targetPath)
	if err != nil || info.Mode()&fs.ModeSymlink == 0 {
		return "", false
	}
	dest, err := resolveLink(targetPath)
	if err != nil {
		return "", false
	}
	root, err := filepath.EvalSymlinks(paths.Dotfiles)
	if err != nil {
		return "", false
	}
	rel, err := filepath.Rel(root, dest)
	if err != nil {
		return "", false
	}
	if rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", false
	}
	pkg := PackageOf(rel)
	if pkg == "" {
		return "", false
	}
	return pkg, true
}

func pointsAt(link, want string) bool {
	dest, err := resolveLink(link)
	if err != nil {
		return false
	}
	resolved, err := filepath.EvalSymlinks(want)
	if err != nil {
		return false
	}
	return dest == resolved
}

func resolveLink(link string) (string, error) {
	dest, err := os.Readlink(link)
	if err != nil {
		return "", err
	}
	if !filepath.IsAbs(dest) {
		dest = filepath.Join(filepath.Dir(link), dest)
	}
	return filepath.EvalSymlinks(dest)
}
