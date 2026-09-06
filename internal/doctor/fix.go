package doctor

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/horbo/stower/internal/config"
	"github.com/horbo/stower/internal/dotfiles"
	"github.com/horbo/stower/internal/stow"
)

type Action string

const (
	KeepTarget Action = "keep target"
	KeepRepo   Action = "keep repo"
	Normalize  Action = "normalize"
	Restow     Action = "restow"
	Relink     Action = "relink"
)

func emit(events chan<- dotfiles.Event, event dotfiles.Event) {
	if events != nil {
		events <- event
	}
}

func output(events chan<- dotfiles.Event, pkg string, result stow.Result) {
	emit(events, dotfiles.Event{Kind: dotfiles.OutputLine, Package: pkg, Message: result.Command()})
	for _, line := range result.OutputLines() {
		emit(events, dotfiles.Event{Kind: dotfiles.OutputLine, Package: pkg, Message: line})
	}
}

func transaction(ctx context.Context, pkg, title string, events chan<- dotfiles.Event, work func() error) dotfiles.Summary {
	emit(events, dotfiles.Event{Kind: dotfiles.StepStarted, Package: pkg, Message: title})
	err := ctx.Err()
	if err == nil {
		err = work()
	}
	if err != nil {
		emit(events, dotfiles.Event{Kind: dotfiles.StepFailed, Package: pkg, Message: title, Err: err})
		emit(events, dotfiles.Event{Kind: dotfiles.PackageFailed, Package: pkg, Err: err})
		return dotfiles.Summary{Failed: []dotfiles.PackageFailure{{Package: pkg, Err: err}}}
	}
	emit(events, dotfiles.Event{Kind: dotfiles.StepDone, Package: pkg, Message: title})
	emit(events, dotfiles.Event{Kind: dotfiles.PackageDone, Package: pkg})
	return dotfiles.Summary{Succeeded: []string{pkg}}
}

func RestowPackages(ctx context.Context, packages []string, runner dotfiles.Runner, events chan<- dotfiles.Event) dotfiles.Summary {
	var summary dotfiles.Summary
	for _, pkg := range packages {
		one := transaction(ctx, pkg, "stow restow", events, func() error {
			if err := dotfiles.ValidatePackageName(pkg); err != nil {
				return err
			}
			result := runner.DryRunRestow(pkg)
			output(events, pkg, result)
			if result.Err != nil {
				return result.Err
			}
			if err := ctx.Err(); err != nil {
				return err
			}
			result = runner.Restow(pkg)
			output(events, pkg, result)
			return result.Err
		})
		summary.Succeeded = append(summary.Succeeded, one.Succeeded...)
		summary.Failed = append(summary.Failed, one.Failed...)
	}
	return summary
}

func Fix(ctx context.Context, paths config.Paths, issue Issue, action Action, runner dotfiles.Runner, events chan<- dotfiles.Event) dotfiles.Summary {
	return transaction(ctx, issue.Package, string(action)+" "+issue.Entry.PkgRel, events, func() error {
		if err := dotfiles.ValidatePackageName(issue.Package); err != nil {
			return err
		}
		if !filepath.IsLocal(issue.Entry.PkgRel) || issue.Entry.PkgRel == "." {
			return fmt.Errorf("invalid entry path")
		}
		report := InspectPackage(paths, issue.Package)
		if report.Err != nil {
			return report.Err
		}
		var current *Issue
		for _, entry := range report.Entries {
			if entry.Entry.PkgRel == issue.Entry.PkgRel && entry.State == issue.State {
				found := entry
				current = &found
				break
			}
		}
		if current == nil || !current.Fixable {
			return fmt.Errorf("issue changed or is report only; refresh first")
		}
		issue = *current
		if action == Restow && issue.State == Missing {
			result := runner.DryRunRestow(issue.Package)
			output(events, issue.Package, result)
			if result.Err != nil {
				return result.Err
			}
			if err := ctx.Err(); err != nil {
				return err
			}
			result = runner.Restow(issue.Package)
			output(events, issue.Package, result)
			return result.Err
		}
		if action == Normalize && issue.State == Unnormalized {
			return normalize(ctx, paths, issue, runner, events)
		}
		if action == Relink && issue.State == Unowned {
			return relink(ctx, paths, issue, runner, events)
		}
		if issue.State != Replaced || (action != KeepTarget && action != KeepRepo) {
			return fmt.Errorf("invalid fix %q for %s", action, issue.State)
		}
		return replace(ctx, paths, issue, action, runner, events)
	})
}

type backupFix struct {
	paths    config.Paths
	issue    Issue
	runner   dotfiles.Runner
	events   chan<- dotfiles.Event
	original string
	move     func() error
	undoMove func() error
}

func (f backupFix) run(ctx context.Context) error {
	report := InspectPackage(f.paths, f.issue.Package)
	if report.Err != nil {
		return report.Err
	}
	var excluded []string
	for _, item := range report.Entries {
		if item.State != OK {
			excluded = append(excluded, item.Entry.PkgRel)
		}
	}
	if err := dotfiles.CheckSameDevice(f.paths); err != nil {
		return err
	}
	backup, err := os.MkdirTemp(f.paths.Dotfiles, ".stower-backup-")
	if err != nil {
		return err
	}
	saved := filepath.Join(backup, "original")
	if err := os.Rename(f.original, saved); err != nil {
		os.Remove(backup)
		return err
	}
	emit(f.events, dotfiles.Event{Kind: dotfiles.OutputLine, Package: f.issue.Package, Message: "backup " + f.original + " → " + saved})
	attempted := false
	rollback := func(cause error) error {
		if attempted {
			result := f.runner.Unstow(f.issue.Package)
			output(f.events, f.issue.Package, result)
			if result.Err != nil {
				return errors.Join(cause, fmt.Errorf("rollback unstow failed; backup retained at %s: %w", backup, result.Err))
			}
		}
		if f.undoMove != nil {
			if err := f.undoMove(); err != nil {
				return errors.Join(cause, fmt.Errorf("rollback failed; backup at %s: %w", backup, err))
			}
		}
		if err := os.Rename(saved, f.original); err != nil {
			return errors.Join(cause, fmt.Errorf("rollback failed; backup at %s: %w", backup, err))
		}
		os.Remove(backup)
		emit(f.events, dotfiles.Event{Kind: dotfiles.Rollback, Package: f.issue.Package, Message: "restore original target and repository"})
		if attempted {
			result := f.runner.RestowExcluding(f.issue.Package, excluded)
			output(f.events, f.issue.Package, result)
			return errors.Join(cause, result.Err)
		}
		return cause
	}
	if f.move != nil {
		if err := f.move(); err != nil {
			return rollback(err)
		}
	}
	if err := ctx.Err(); err != nil {
		return rollback(err)
	}
	dry := f.runner.DryRunRestow(f.issue.Package)
	output(f.events, f.issue.Package, dry)
	if dry.Err != nil {
		return rollback(dry.Err)
	}
	if err := ctx.Err(); err != nil {
		return rollback(err)
	}
	attempted = true
	result := f.runner.Restow(f.issue.Package)
	output(f.events, f.issue.Package, result)
	if result.Err != nil {
		return rollback(result.Err)
	}
	if err := ctx.Err(); err != nil {
		return rollback(err)
	}
	if err := os.RemoveAll(backup); err != nil {
		return fmt.Errorf("fix applied but backup cleanup failed at %s: %w", backup, err)
	}
	return nil
}

func replace(ctx context.Context, paths config.Paths, issue Issue, action Action, runner dotfiles.Runner, events chan<- dotfiles.Event) error {
	repo, target := issue.Entry.PackagePath(paths, issue.Package), issue.Entry.TargetPath(paths)
	if err := dotfiles.ValidateStagingPath(paths, target); err != nil {
		return err
	}
	if err := safeParents(paths.Dotfiles, repo); err != nil {
		return err
	}
	if err := safeParents(paths.Target, target); err != nil {
		return err
	}
	fix := backupFix{paths: paths, issue: issue, runner: runner, events: events, original: target}
	if action == KeepTarget {
		fix.original = repo
		movedTarget := false
		fix.move = func() error {
			if err := os.Rename(target, repo); err != nil {
				return err
			}
			movedTarget = true
			return nil
		}
		fix.undoMove = func() error {
			if !movedTarget {
				return nil
			}
			return os.Rename(repo, target)
		}
	}
	return fix.run(ctx)
}

func relink(ctx context.Context, paths config.Paths, issue Issue, runner dotfiles.Runner, events chan<- dotfiles.Event) error {
	target := issue.Entry.TargetPath(paths)
	if err := safeParents(paths.Target, target); err != nil {
		return err
	}
	return backupFix{paths: paths, issue: issue, runner: runner, events: events, original: target}.run(ctx)
}

func normalize(ctx context.Context, paths config.Paths, issue Issue, runner dotfiles.Runner, events chan<- dotfiles.Event) error {
	rel := issue.Entry.PkgRel
	if filepath.Dir(rel) != "." || !strings.HasPrefix(rel, ".") {
		return fmt.Errorf("nested normalization is report only")
	}
	from := issue.Entry.PackagePath(paths, issue.Package)
	to := filepath.Join(paths.Dotfiles, issue.Package, "dot-"+strings.TrimPrefix(rel, "."))
	if err := safeParents(paths.Dotfiles, from); err != nil {
		return err
	}
	if _, err := os.Lstat(to); !errors.Is(err, os.ErrNotExist) {
		if err == nil {
			return fmt.Errorf("%w: %s", dotfiles.ErrDestinationExists, to)
		}
		return err
	}
	unstow := runner.Unstow(issue.Package)
	output(events, issue.Package, unstow)
	if unstow.Err != nil {
		return unstow.Err
	}
	renamed, attempted := false, false
	rollback := func(cause error) error {
		if attempted {
			result := runner.Unstow(issue.Package)
			output(events, issue.Package, result)
			if result.Err != nil {
				return errors.Join(cause, result.Err)
			}
		}
		if renamed {
			if err := os.Rename(to, from); err != nil {
				return errors.Join(cause, err)
			}
		}
		result := runner.Restow(issue.Package)
		output(events, issue.Package, result)
		emit(events, dotfiles.Event{Kind: dotfiles.Rollback, Package: issue.Package, Message: "restore original package names and links", Err: result.Err})
		return errors.Join(cause, result.Err)
	}
	if err := ctx.Err(); err != nil {
		return rollback(err)
	}
	if err := os.Rename(from, to); err != nil {
		return rollback(err)
	}
	renamed = true
	dry := runner.DryRunRestow(issue.Package)
	output(events, issue.Package, dry)
	if dry.Err != nil {
		return rollback(dry.Err)
	}
	if err := ctx.Err(); err != nil {
		return rollback(err)
	}
	attempted = true
	result := runner.Restow(issue.Package)
	output(events, issue.Package, result)
	if result.Err != nil {
		return rollback(result.Err)
	}
	if err := ctx.Err(); err != nil {
		return rollback(err)
	}
	return nil
}
