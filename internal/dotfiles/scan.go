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
			if ResolvesTo(targetPath, filepath.Join(paths.Dotfiles, pkg, pkgRel)) && StowOwns(paths, pkg, entry) {
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

type LinkInfo struct {
	Target   string
	Package  string
	Dangling bool
}

func InspectLink(paths config.Paths, targetPath string) (LinkInfo, bool) {
	info, err := os.Lstat(targetPath)
	if err != nil || info.Mode()&fs.ModeSymlink == 0 {
		return LinkInfo{}, false
	}
	dest, err := os.Readlink(targetPath)
	if err != nil {
		return LinkInfo{}, false
	}
	link := LinkInfo{Target: dest}
	if _, err := os.Stat(targetPath); err != nil {
		link.Dangling = true
	}
	link.Package = linkPackage(paths, targetPath, dest)
	return link, true
}

func ManagedBy(paths config.Paths, targetPath string) (string, bool) {
	info, ok := InspectLink(paths, targetPath)
	if !ok || info.Dangling || info.Package == "" {
		return "", false
	}
	return info.Package, true
}

func ResolvesTo(link, want string) bool {
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

func StowOwns(paths config.Paths, pkg string, entry Entry) bool {
	dest, err := os.Readlink(entry.TargetPath(paths))
	if err != nil || filepath.IsAbs(dest) {
		return false
	}
	target, err := filepath.EvalSymlinks(paths.Target)
	if err != nil {
		return false
	}
	repo, err := filepath.EvalSymlinks(paths.Dotfiles)
	if err != nil {
		return false
	}
	stowRel, err := filepath.Rel(target, repo)
	if err != nil {
		return false
	}
	return filepath.Join(filepath.Dir(entry.TargetRel), dest) == filepath.Join(stowRel, pkg, entry.PkgRel)
}

func linkPackage(paths config.Paths, targetPath, dest string) string {
	if resolved, err := resolveLink(targetPath); err == nil {
		root, err := filepath.EvalSymlinks(paths.Dotfiles)
		if err != nil {
			return ""
		}
		return packageUnder(root, resolved)
	}
	lexical := dest
	if !filepath.IsAbs(lexical) {
		lexical = filepath.Join(filepath.Dir(targetPath), lexical)
	}
	lexical = filepath.Clean(lexical)
	if pkg := packageUnder(paths.Dotfiles, lexical); pkg != "" {
		return pkg
	}
	root, err := filepath.EvalSymlinks(paths.Dotfiles)
	if err != nil {
		return ""
	}
	return packageUnder(root, lexical)
}

func packageUnder(root, dest string) string {
	rel, err := filepath.Rel(root, dest)
	if err != nil {
		return ""
	}
	if rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return ""
	}
	return PackageOf(rel)
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
