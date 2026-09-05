package doctor

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/horbo/stower/internal/config"
	"github.com/horbo/stower/internal/dotfiles"
)

type State string

const (
	OK           State = "ok"
	Missing      State = "missing"
	Replaced     State = "replaced"
	Foreign      State = "foreign"
	Unnormalized State = "unnormalized"
)

type Issue struct {
	Package string
	Entry   dotfiles.Entry
	State   State
	Detail  string
	Fixable bool
}

func (i Issue) Glyph() string {
	switch i.State {
	case OK:
		return "✔"
	case Unnormalized, Foreign:
		return "⚠"
	default:
		return "✘"
	}
}

type PackageReport struct {
	Package string
	Entries []Issue
	Err     error
}

func Inspect(paths config.Paths) ([]Issue, []PackageReport, error) {
	names, err := dotfiles.ListPackages(paths)
	if err != nil {
		return nil, nil, err
	}
	var issues []Issue
	var reports []PackageReport
	var errs []error
	for _, name := range names {
		report := InspectPackage(paths, name)
		reports = append(reports, report)
		if report.Err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", name, report.Err))
		}
		for _, entry := range report.Entries {
			if entry.State != OK {
				issues = append(issues, entry)
			}
		}
	}
	return issues, reports, errors.Join(errs...)
}

func InspectPackage(paths config.Paths, pkg string) PackageReport {
	report := PackageReport{Package: pkg}
	base, err := dotfiles.WalkPackage(paths, pkg)
	if err != nil {
		report.Err = err
		return report
	}
	root := filepath.Join(paths.Dotfiles, pkg)
	if err := safeParents(paths.Dotfiles, filepath.Join(root, "placeholder")); err != nil {
		report.Err = err
		return report
	}
	states := map[string]dotfiles.State{}
	for _, entry := range base {
		states[entry.PkgRel] = entry.State
	}
	var walk func(string) error
	walk = func(rel string) error {
		children, err := os.ReadDir(filepath.Join(root, rel))
		if err != nil {
			return err
		}
		for _, child := range children {
			pkgRel := filepath.Join(rel, child.Name())
			entry := dotfiles.Entry{PkgRel: pkgRel, TargetRel: dotfiles.PackageToTarget(pkgRel), IsDir: child.IsDir()}
			item := Issue{Package: pkg, Entry: entry, State: OK}
			target := entry.TargetPath(paths)
			info, statErr := os.Lstat(target)
			switch {
			case errors.Is(statErr, fs.ErrNotExist):
				item.State, item.Entry.State, item.Fixable = Missing, dotfiles.Unlinked, true
			case statErr != nil:
				return statErr
			case info.Mode()&fs.ModeSymlink != 0:
				if state, found := states[pkgRel]; !found || state != dotfiles.Linked {
					item.State, item.Entry.State = Foreign, dotfiles.Conflict
				}
			case info.IsDir() && child.IsDir():
				replaced, err := replacedDirectory(paths, pkg, pkgRel, base)
				if err != nil {
					return err
				}
				if !replaced {
					if err := walk(pkgRel); err != nil {
						return err
					}
					continue
				}
				item.State, item.Entry.State, item.Fixable = Replaced, dotfiles.Conflict, true
			default:
				item.State, item.Entry.State, item.Fixable = Replaced, dotfiles.Conflict, true
			}
			item.Detail = string(item.State)
			report.Entries = append(report.Entries, item)
		}
		return nil
	}
	if err := walk(""); err != nil {
		report.Err = err
		return report
	}
	err = filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if path == root {
			return nil
		}
		rel, _ := filepath.Rel(root, path)
		top := filepath.Dir(rel) == "."
		if !(top && strings.HasPrefix(entry.Name(), ".")) && !(!top && strings.HasPrefix(entry.Name(), "dot-")) {
			return nil
		}
		issue := Issue{Package: pkg, Entry: dotfiles.Entry{PkgRel: rel, TargetRel: dotfiles.PackageToTarget(rel), IsDir: entry.IsDir()}, State: Unnormalized, Fixable: top}
		if top {
			issue.Detail = "rename the top-level dot name to dot-"
		} else {
			issue.Detail = "nested dot- component changes stow mapping; report only"
		}
		found := false
		for i := range report.Entries {
			if report.Entries[i].Entry.PkgRel == rel {
				issue.Entry.State = report.Entries[i].Entry.State
				report.Entries[i] = issue
				found = true
				break
			}
		}
		if !found {
			report.Entries = append(report.Entries, issue)
		}
		return nil
	})
	report.Err = err
	return report
}

func replacedDirectory(paths config.Paths, pkg, rel string, entries []dotfiles.Entry) (bool, error) {
	prefix := rel + string(filepath.Separator)
	for _, entry := range entries {
		if strings.HasPrefix(entry.PkgRel, prefix) && entry.State == dotfiles.Linked {
			return false, nil
		}
	}
	children, err := os.ReadDir(filepath.Join(paths.Dotfiles, pkg, rel))
	if err != nil {
		return false, err
	}

	for _, child := range children {
		if !child.IsDir() {
			target := filepath.Join(paths.Target, dotfiles.PackageToTarget(filepath.Join(rel, child.Name())))
			info, err := os.Lstat(target)
			if err == nil && info.Mode()&fs.ModeSymlink == 0 {
				return true, nil
			}
			if err != nil && !errors.Is(err, fs.ErrNotExist) {
				return false, err
			}
		}
	}
	return false, nil
}

func BuildRestorePlan(paths config.Paths, pkg string) dotfiles.RestorePlan {
	plan := dotfiles.BuildRestorePlan(paths, pkg)
	report := InspectPackage(paths, pkg)
	if report.Err != nil {
		plan.Fatal = report.Err
		return plan
	}
	for _, issue := range report.Entries {
		if issue.State == OK || issue.State == Missing {
			continue
		}
		found := false
		for _, blocked := range plan.Blocked {
			if blocked.Path == issue.Entry.TargetPath(paths) {
				found = true
			}
		}
		if !found {
			plan.Blocked = append(plan.Blocked, dotfiles.Blocked{Package: pkg, Path: issue.Entry.TargetPath(paths), Reason: string(issue.State) + "; fix in Issues first"})
		}
	}
	return plan
}

func safeParents(root, path string) error {
	root = filepath.Clean(root)
	path = filepath.Clean(path)
	rel, err := filepath.Rel(root, path)
	if err != nil || rel == "." || !filepath.IsLocal(rel) {
		return fmt.Errorf("path outside root: %s", path)
	}
	parent := filepath.Dir(path)
	for parent != root {
		info, err := os.Lstat(parent)
		if err != nil {
			return err
		}
		if info.Mode()&fs.ModeSymlink != 0 || !info.IsDir() {
			return fmt.Errorf("unsafe parent: %s", parent)
		}
		parent = filepath.Dir(parent)
	}
	return nil
}
