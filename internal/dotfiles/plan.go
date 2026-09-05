package dotfiles

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"

	"github.com/horbo/stower/internal/config"
)

type Staging map[string]string

type Move struct {
	From string
	To   string
}

type Link struct {
	TargetPath string
	LinkText   string
}

type Blocked struct {
	Path    string
	Package string
	Reason  string
}

type Warning struct {
	Path    string
	Package string
	Message string
}

type PackageAdopt struct {
	Package         string
	Moves           []Move
	CreateDirs      []string
	ExpectedLinks   []Link
	Warnings        []Warning
	Blocked         []Blocked
	RemoveNestedGit bool
}

type AdoptPlan struct {
	Paths    config.Paths
	Packages []PackageAdopt
	Messages []string
	Fatal    error
}

func (p AdoptPlan) MoveCount() int {
	count := 0
	for _, pkg := range p.Packages {
		count += len(pkg.Moves)
	}
	return count
}

func (p AdoptPlan) BlockedCount() int {
	count := 0
	for _, pkg := range p.Packages {
		count += len(pkg.Blocked)
	}
	return count
}

func (p AdoptPlan) Runnable() bool {
	return p.Fatal == nil && p.MoveCount() > 0
}

func BuildAdoptPlan(paths config.Paths, staging Staging) AdoptPlan {
	plan := AdoptPlan{Paths: paths}
	kept, messages := DedupeStaging(staging)
	plan.Messages = messages
	if err := CheckSameDevice(paths); err != nil {
		plan.Fatal = err
	}

	byPackage := make(map[string][]string, len(kept))
	for path, pkg := range kept {
		byPackage[pkg] = append(byPackage[pkg], path)
	}
	names := make([]string, 0, len(byPackage))
	for pkg := range byPackage {
		names = append(names, pkg)
	}
	sort.Strings(names)

	for _, pkg := range names {
		staged := byPackage[pkg]
		sort.Strings(staged)
		plan.Packages = append(plan.Packages, buildPackageAdopt(paths, pkg, staged))
	}
	return plan
}

func buildPackageAdopt(paths config.Paths, pkg string, staged []string) PackageAdopt {
	out := PackageAdopt{Package: pkg}
	if err := ValidatePackageName(pkg); err != nil {
		for _, path := range staged {
			out.Blocked = append(out.Blocked, Blocked{Path: path, Package: pkg, Reason: err.Error()})
		}
		return out
	}

	for _, path := range staged {
		if err := ValidateStagingPath(paths, path); err != nil {
			out.Blocked = append(out.Blocked, Blocked{Path: path, Package: pkg, Reason: err.Error()})
			continue
		}
		rel, err := RelToTarget(paths, path)
		if err != nil {
			out.Blocked = append(out.Blocked, Blocked{Path: path, Package: pkg, Reason: err.Error()})
			continue
		}
		dest := filepath.Join(paths.Dotfiles, TargetToPackage(pkg, rel))
		if _, err := os.Lstat(dest); err == nil {
			out.Blocked = append(out.Blocked, Blocked{
				Path:    path,
				Package: pkg,
				Reason:  fmt.Sprintf("%s: %s", ErrDestinationExists, dest),
			})
			continue
		} else if !errors.Is(err, fs.ErrNotExist) {
			out.Blocked = append(out.Blocked, Blocked{Path: path, Package: pkg, Reason: err.Error()})
			continue
		}

		out.Moves = append(out.Moves, Move{From: path, To: dest})
		out.CreateDirs = appendDir(out.CreateDirs, filepath.Dir(dest))
		out.ExpectedLinks = append(out.ExpectedLinks, expectedLink(path, dest))

		nested, err := HasNestedGit(path)
		if err == nil && nested {
			out.Warnings = append(out.Warnings, Warning{
				Path:    path,
				Package: pkg,
				Message: "contains a nested .git directory; git would treat it as an embedded repository",
			})
		}
	}
	return out
}

type RestorePlan struct {
	Paths     config.Paths
	Package   string
	Entries   []Entry
	Selected  []string
	Moves     []Move
	Blocked   []Blocked
	RemoveDir string
	Fatal     error
}

func (p RestorePlan) Runnable() bool {
	return p.Fatal == nil && len(p.Blocked) == 0 && len(p.Moves) > 0
}

func (p RestorePlan) Partial() bool {
	return p.RemoveDir == ""
}

func BuildRestorePlan(paths config.Paths, pkg string) RestorePlan {
	return BuildEntryRestorePlan(paths, pkg, nil)
}

func BuildEntryRestorePlan(paths config.Paths, pkg string, pkgRels []string) RestorePlan {
	whole := pkgRels == nil
	plan := RestorePlan{Paths: paths, Package: pkg}
	if whole {
		plan.RemoveDir = filepath.Join(paths.Dotfiles, pkg)
	}
	entries, err := WalkPackage(paths, pkg)
	if err != nil {
		plan.Fatal = err
		return plan
	}
	plan.Entries = entries

	selected := make(map[string]bool, len(pkgRels))
	if !whole {
		linkPoints := make(map[string]bool, len(entries))
		for _, entry := range entries {
			linkPoints[entry.PkgRel] = true
		}
		for _, rel := range pkgRels {
			clean := filepath.Clean(rel)
			if !linkPoints[clean] {
				plan.Blocked = append(plan.Blocked, Blocked{
					Path:    filepath.Join(paths.Dotfiles, pkg, clean),
					Package: pkg,
					Reason:  fmt.Sprintf("%s is not a link point of %s", clean, pkg),
				})
				continue
			}
			selected[clean] = true
		}
	}
	if !whole && len(entries) > 0 && len(selected) == len(entries) {
		plan.RemoveDir = filepath.Join(paths.Dotfiles, pkg)
	}

	for _, entry := range entries {
		if !whole && !selected[entry.PkgRel] {
			continue
		}
		from := entry.PackagePath(paths, pkg)
		to := entry.TargetPath(paths)
		if entry.State == Conflict {
			plan.Blocked = append(plan.Blocked, Blocked{
				Path:    to,
				Package: pkg,
				Reason:  fmt.Sprintf("%s is not a link into %s; fix it first", to, from),
			})
			continue
		}
		plan.Selected = append(plan.Selected, entry.PkgRel)
		plan.Moves = append(plan.Moves, Move{From: from, To: to})
	}
	if err := CheckSameDevice(paths); err != nil {
		plan.Fatal = err
	}
	return plan
}

func expectedLink(targetPath, dest string) Link {
	text, err := filepath.Rel(filepath.Dir(targetPath), dest)
	if err != nil {
		text = dest
	}
	return Link{TargetPath: targetPath, LinkText: text}
}

func appendDir(dirs []string, dir string) []string {
	for _, existing := range dirs {
		if existing == dir {
			return dirs
		}
	}
	return append(dirs, dir)
}
