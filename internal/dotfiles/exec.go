package dotfiles

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/kamilhorbowicz/stower/internal/stow"
)

type EventKind int

const (
	StepStarted EventKind = iota
	OutputLine
	StepDone
	StepFailed
	Rollback
	PackageDone
	PackageFailed
)

func (k EventKind) String() string {
	switch k {
	case StepStarted:
		return "step-started"
	case OutputLine:
		return "output"
	case StepDone:
		return "step-done"
	case StepFailed:
		return "step-failed"
	case Rollback:
		return "rollback"
	case PackageDone:
		return "package-done"
	case PackageFailed:
		return "package-failed"
	default:
		return "unknown"
	}
}

type Event struct {
	Kind    EventKind
	Package string
	Message string
	Err     error
}

type Runner interface {
	DryRunRestow(pkg string) stow.Result
	Restow(pkgs ...string) stow.Result
	Unstow(pkg string) stow.Result
}

type PackageFailure struct {
	Package string
	Err     error
}

type Summary struct {
	Succeeded []string
	Skipped   []string
	Failed    []PackageFailure
}

func (s Summary) OK() bool {
	return len(s.Failed) == 0
}

func (s Summary) Err() error {
	if len(s.Failed) == 0 {
		return nil
	}
	errs := make([]error, 0, len(s.Failed))
	for _, failure := range s.Failed {
		errs = append(errs, fmt.Errorf("%s: %w", failure.Package, failure.Err))
	}
	return errors.Join(errs...)
}

func Execute(ctx context.Context, plan AdoptPlan, runner Runner, events chan<- Event) Summary {
	em := emitter{ctx: ctx, events: events}
	var summary Summary
	if plan.Fatal != nil {
		for _, pkg := range plan.Packages {
			summary.Failed = append(summary.Failed, PackageFailure{Package: pkg.Package, Err: plan.Fatal})
			em.send(Event{Kind: PackageFailed, Package: pkg.Package, Err: plan.Fatal})
		}
		return summary
	}
	for _, pkg := range plan.Packages {
		if len(pkg.Moves) == 0 {
			summary.Skipped = append(summary.Skipped, pkg.Package)
			continue
		}
		if err := adoptPackage(ctx, pkg, runner, em); err != nil {
			summary.Failed = append(summary.Failed, PackageFailure{Package: pkg.Package, Err: err})
			em.send(Event{Kind: PackageFailed, Package: pkg.Package, Err: err})
			continue
		}
		summary.Succeeded = append(summary.Succeeded, pkg.Package)
		em.send(Event{Kind: PackageDone, Package: pkg.Package})
	}
	return summary
}

func ExecuteRestore(ctx context.Context, plan RestorePlan, runner Runner, events chan<- Event) Summary {
	em := emitter{ctx: ctx, events: events}
	var summary Summary
	switch {
	case plan.Fatal != nil:
		summary.Failed = append(summary.Failed, PackageFailure{Package: plan.Package, Err: plan.Fatal})
		em.send(Event{Kind: PackageFailed, Package: plan.Package, Err: plan.Fatal})
		return summary
	case len(plan.Blocked) > 0:
		err := fmt.Errorf("the plan is blocked by %d conflicting entries", len(plan.Blocked))
		summary.Failed = append(summary.Failed, PackageFailure{Package: plan.Package, Err: err})
		em.send(Event{Kind: PackageFailed, Package: plan.Package, Err: err})
		return summary
	}
	if err := restorePackage(ctx, plan, runner, em); err != nil {
		summary.Failed = append(summary.Failed, PackageFailure{Package: plan.Package, Err: err})
		em.send(Event{Kind: PackageFailed, Package: plan.Package, Err: err})
		return summary
	}
	summary.Succeeded = append(summary.Succeeded, plan.Package)
	em.send(Event{Kind: PackageDone, Package: plan.Package})
	return summary
}

type emitter struct {
	ctx    context.Context
	events chan<- Event
}

func (e emitter) send(event Event) {
	if e.events == nil {
		return
	}
	if e.ctx == nil {
		e.events <- event
		return
	}
	select {
	case e.events <- event:
	case <-e.ctx.Done():
	}
}

func (e emitter) result(pkg string, result stow.Result) {
	e.send(Event{Kind: OutputLine, Package: pkg, Message: result.Command()})
	for _, line := range result.OutputLines() {
		e.send(Event{Kind: OutputLine, Package: pkg, Message: line})
	}
}

type journal struct {
	moves []Move
	dirs  []string
}

func adoptPackage(ctx context.Context, plan PackageAdopt, runner Runner, em emitter) error {
	pkg := plan.Package
	var undo journal
	stowed := false

	fail := func(step string, err error) error {
		em.send(Event{Kind: StepFailed, Package: pkg, Message: step, Err: err})
		if stowed {
			rollbackStow(pkg, runner, em)
		}
		rollbackMoves(&undo, pkg, em)
		return err
	}

	step := "create package directories"
	em.send(Event{Kind: StepStarted, Package: pkg, Message: step})
	if err := ctx.Err(); err != nil {
		return fail(step, err)
	}
	for _, dir := range plan.CreateDirs {
		created, err := mkdirAllTracked(dir)
		undo.dirs = append(undo.dirs, created...)
		if err != nil {
			return fail(step, err)
		}
	}
	em.send(Event{Kind: StepDone, Package: pkg, Message: step})

	for _, move := range plan.Moves {
		step = fmt.Sprintf("mv %s %s", move.From, move.To)
		em.send(Event{Kind: StepStarted, Package: pkg, Message: step})
		if err := ctx.Err(); err != nil {
			return fail(step, err)
		}
		if _, err := os.Lstat(move.To); err == nil {
			return fail(step, fmt.Errorf("%w: %s", ErrDestinationExists, move.To))
		}
		if err := os.Rename(move.From, move.To); err != nil {
			return fail(step, err)
		}
		undo.moves = append(undo.moves, move)
		em.send(Event{Kind: StepDone, Package: pkg, Message: step})
	}

	step = "stow dry run"
	em.send(Event{Kind: StepStarted, Package: pkg, Message: step})
	dry := runner.DryRunRestow(pkg)
	em.result(pkg, dry)
	if dry.Err != nil {
		return fail(step, dry.Err)
	}
	em.send(Event{Kind: StepDone, Package: pkg, Message: step})

	step = "stow restow"
	em.send(Event{Kind: StepStarted, Package: pkg, Message: step})
	stowed = true
	result := runner.Restow(pkg)
	em.result(pkg, result)
	if result.Err != nil {
		return fail(step, result.Err)
	}
	em.send(Event{Kind: StepDone, Package: pkg, Message: step})

	if plan.RemoveNestedGit {
		step = "remove nested .git directories"
		em.send(Event{Kind: StepStarted, Package: pkg, Message: step})
		for _, move := range plan.Moves {
			if err := removeNestedGit(move.To, pkg, em); err != nil {
				em.send(Event{Kind: StepFailed, Package: pkg, Message: step, Err: err})
				return nil
			}
		}
		em.send(Event{Kind: StepDone, Package: pkg, Message: step})
	}
	return nil
}

func restorePackage(ctx context.Context, plan RestorePlan, runner Runner, em emitter) error {
	pkg := plan.Package

	step := "stow unstow"
	em.send(Event{Kind: StepStarted, Package: pkg, Message: step})
	unstow := runner.Unstow(pkg)
	em.result(pkg, unstow)
	if unstow.Err != nil {
		em.send(Event{Kind: StepFailed, Package: pkg, Message: step, Err: unstow.Err})
		return unstow.Err
	}
	em.send(Event{Kind: StepDone, Package: pkg, Message: step})

	var undo journal
	fail := func(step string, err error) error {
		em.send(Event{Kind: StepFailed, Package: pkg, Message: step, Err: err})
		rollbackMoves(&undo, pkg, em)
		rollbackRestow(pkg, runner, em)
		return err
	}

	for _, move := range plan.Moves {
		step = fmt.Sprintf("mv %s %s", move.From, move.To)
		em.send(Event{Kind: StepStarted, Package: pkg, Message: step})
		if err := ctx.Err(); err != nil {
			return fail(step, err)
		}
		if _, err := os.Lstat(move.To); err == nil {
			return fail(step, fmt.Errorf("%w: %s", ErrDestinationExists, move.To))
		}
		created, err := mkdirAllTracked(filepath.Dir(move.To))
		undo.dirs = append(undo.dirs, created...)
		if err != nil {
			return fail(step, err)
		}
		if err := os.Rename(move.From, move.To); err != nil {
			return fail(step, err)
		}
		undo.moves = append(undo.moves, move)
		em.send(Event{Kind: StepDone, Package: pkg, Message: step})
	}

	step = "remove " + plan.RemoveDir
	em.send(Event{Kind: StepStarted, Package: pkg, Message: step})
	removeEmptyTree(plan.RemoveDir)
	if _, err := os.Lstat(plan.RemoveDir); err == nil {
		em.send(Event{
			Kind:    OutputLine,
			Package: pkg,
			Message: plan.RemoveDir + " still contains files and was kept",
		})
	}
	em.send(Event{Kind: StepDone, Package: pkg, Message: step})
	return nil
}

func rollbackMoves(undo *journal, pkg string, em emitter) {
	for i := len(undo.moves) - 1; i >= 0; i-- {
		move := undo.moves[i]
		message := fmt.Sprintf("mv %s %s", move.To, move.From)
		err := os.Rename(move.To, move.From)
		em.send(Event{Kind: Rollback, Package: pkg, Message: message, Err: err})
	}
	undo.moves = nil

	for i := len(undo.dirs) - 1; i >= 0; i-- {
		dir := undo.dirs[i]
		if err := os.Remove(dir); err == nil {
			em.send(Event{Kind: Rollback, Package: pkg, Message: "rmdir " + dir})
		}
	}
	undo.dirs = nil
}

func rollbackStow(pkg string, runner Runner, em emitter) {
	result := runner.Unstow(pkg)
	em.send(Event{Kind: Rollback, Package: pkg, Message: result.Command(), Err: result.Err})
}

func rollbackRestow(pkg string, runner Runner, em emitter) {
	result := runner.Restow(pkg)
	em.send(Event{Kind: Rollback, Package: pkg, Message: result.Command(), Err: result.Err})
}

func mkdirAllTracked(dir string) ([]string, error) {
	var missing []string
	for path := filepath.Clean(dir); ; {
		if _, err := os.Stat(path); err == nil {
			break
		}
		missing = append(missing, path)
		parent := filepath.Dir(path)
		if parent == path {
			break
		}
		path = parent
	}
	created := make([]string, 0, len(missing))
	for i := len(missing) - 1; i >= 0; i-- {
		created = append(created, missing[i])
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return created, err
	}
	return created, nil
}

func removeEmptyTree(root string) {
	entries, err := os.ReadDir(root)
	if err != nil {
		return
	}
	for _, entry := range entries {
		if entry.IsDir() {
			removeEmptyTree(filepath.Join(root, entry.Name()))
		}
	}
	_ = os.Remove(root)
}

func removeNestedGit(root, pkg string, em emitter) error {
	return filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !entry.IsDir() || entry.Name() != ".git" {
			return nil
		}
		if err := os.RemoveAll(path); err != nil {
			return err
		}
		em.send(Event{Kind: OutputLine, Package: pkg, Message: "rm -r " + path})
		return filepath.SkipDir
	})
}
