package dotfiles

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/horbo/stower/internal/stow"
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
	DryRunRestowExcluding(pkg string, entries []string) stow.Result
	Restow(pkgs ...string) stow.Result
	Unstow(pkg string) stow.Result
	RestowExcluding(pkg string, entries []string) stow.Result
}

type PackageFailure struct {
	Package string
	Err     error
}

type Summary struct {
	Succeeded []string
	Skipped   []string
	Failed    []PackageFailure
	Warnings  []PackageFailure
	GitPaths  []string
}

func (s *Summary) addWarnings(pkg string, warnings []error) {
	for _, warning := range warnings {
		if warning != nil {
			s.Warnings = append(s.Warnings, PackageFailure{Package: pkg, Err: warning})
		}
	}
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
	return ExecuteWithGit(ctx, plan, runner, repositoryGit{}, events)
}

func ExecuteWithGit(ctx context.Context, plan AdoptPlan, runner Runner, git GitOperations, events chan<- Event) Summary {
	em := emitter{events: events}
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
		var err error
		var warnings []error
		if hasConversions(pkg) {
			err = git.Check(ctx, plan.Paths.Dotfiles)
			if err == nil {
				err = repositoryPlanError(ctx, plan.Paths, pkg)
			}
		}
		if err == nil {
			warnings, err = adoptPackage(ctx, pkg, plan.Paths.Dotfiles, runner, git, em)
		}
		summary.addWarnings(pkg.Package, warnings)
		if err != nil {
			summary.Failed = append(summary.Failed, PackageFailure{Package: pkg.Package, Err: err})
			em.send(Event{Kind: PackageFailed, Package: pkg.Package, Err: err})
			continue
		}
		if hasConversions(pkg) {
			summary.GitPaths = []string{".gitmodules"}
		}
		summary.Succeeded = append(summary.Succeeded, pkg.Package)
		em.send(Event{Kind: PackageDone, Package: pkg.Package})
	}
	return summary
}

func ExecuteRestore(ctx context.Context, plan RestorePlan, runner Runner, events chan<- Event) Summary {
	return ExecuteRestoreWithGit(ctx, plan, runner, repositoryGit{}, events)
}

func ExecuteRestoreWithGit(ctx context.Context, plan RestorePlan, runner Runner, git GitOperations, events chan<- Event) Summary {
	em := emitter{events: events}
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
	warnings, err := restorePackage(ctx, plan, runner, git, em)
	summary.addWarnings(plan.Package, warnings)
	if err != nil {
		summary.Failed = append(summary.Failed, PackageFailure{Package: plan.Package, Err: err})
		em.send(Event{Kind: PackageFailed, Package: plan.Package, Err: err})
		return summary
	}
	if len(plan.Submodules) > 0 {
		summary.GitPaths = []string{".gitmodules"}
	}
	summary.Succeeded = append(summary.Succeeded, plan.Package)
	em.send(Event{Kind: PackageDone, Package: plan.Package})
	return summary
}

type emitter struct {
	events chan<- Event
}

func (e emitter) send(event Event) {
	if e.events == nil {
		return
	}
	e.events <- event
}

func (e emitter) result(pkg string, result stow.Result) {
	e.send(Event{Kind: OutputLine, Package: pkg, Message: result.Command()})
	for _, line := range result.OutputLines() {
		e.send(Event{Kind: OutputLine, Package: pkg, Message: line})
	}
}

type removedDirectory struct {
	path string
	mode os.FileMode
}

type journal struct {
	moves       []Move
	dirs        []string
	removedDirs []removedDirectory
}

func adoptPackage(ctx context.Context, plan PackageAdopt, dir string, runner Runner, git GitOperations, em emitter) ([]error, error) {
	pkg := plan.Package
	var undo journal
	stowed := false
	var transaction GitTransaction
	if hasConversions(plan) {
		var err error
		transaction, err = git.Begin(dir)
		if err != nil {
			em.send(Event{Kind: StepFailed, Package: pkg, Message: "begin the Git transaction", Err: err})
			return nil, err
		}
	}

	fail := func(step string, err error) ([]error, error) {
		em.send(Event{Kind: StepFailed, Package: pkg, Message: step, Err: err})
		if stowed {
			rollbackStow(pkg, runner, em)
		}
		if transaction != nil {
			if rollbackErr := transaction.Rollback(); rollbackErr != nil {
				em.send(Event{Kind: Rollback, Package: pkg, Err: rollbackErr})
				err = errors.Join(err, rollbackErr)
			}
		}
		rollbackMoves(&undo, pkg, em)
		return nil, err
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

	for _, repository := range plan.Repositories {
		if repository.Choice.Action != ConvertRepository {
			continue
		}
		step = "convert " + repository.Source + " to submodule"
		em.send(Event{Kind: StepStarted, Package: pkg, Message: step})
		if err := ctx.Err(); err != nil {
			return fail(step, err)
		}
		rel, err := filepath.Rel(dir, repository.Destination)
		if err != nil {
			return fail(step, err)
		}
		if err = transaction.Register(ctx, rel, repository.Choice.URL, repository.Info.Head); err != nil {
			return fail(step, err)
		}
		em.send(Event{Kind: StepDone, Package: pkg, Message: step})
	}
	step = "stow dry run"
	em.send(Event{Kind: StepStarted, Package: pkg, Message: step})
	if err := ctx.Err(); err != nil {
		return fail(step, err)
	}
	dry := runner.DryRunRestow(pkg)
	em.result(pkg, dry)
	if dry.Err != nil {
		return fail(step, dry.Err)
	}
	em.send(Event{Kind: StepDone, Package: pkg, Message: step})

	step = "stow restow"
	em.send(Event{Kind: StepStarted, Package: pkg, Message: step})
	if err := ctx.Err(); err != nil {
		return fail(step, err)
	}
	stowed = true
	result := runner.Restow(pkg)
	em.result(pkg, result)
	if result.Err != nil {
		return fail(step, result.Err)
	}
	em.send(Event{Kind: StepDone, Package: pkg, Message: step})

	if err := ctx.Err(); err != nil {
		return fail(step, err)
	}

	var warnings []error
	if err := closeGitTransaction(transaction, pkg, em); err != nil {
		warnings = append(warnings, err)
	}
	if err := removeSelectedGit(plan, em); err != nil {
		em.send(Event{Kind: StepFailed, Package: pkg, Message: "remove selected .git directories", Err: err})
		warnings = append(warnings, err)
	}
	return warnings, nil
}

func restorePackage(ctx context.Context, plan RestorePlan, runner Runner, git GitOperations, em emitter) ([]error, error) {
	pkg := plan.Package
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	var transaction GitTransaction
	if len(plan.Submodules) > 0 {
		if err := git.Check(ctx, plan.Paths.Dotfiles); err != nil {
			return nil, err
		}
		var err error
		transaction, err = git.Begin(plan.Paths.Dotfiles)
		if err != nil {
			em.send(Event{Kind: StepFailed, Package: pkg, Message: "begin the Git transaction", Err: err})
			return nil, err
		}
	}
	var warnings []error
	closeTransaction := func() {
		if err := closeGitTransaction(transaction, pkg, em); err != nil {
			warnings = append(warnings, err)
		}
	}
	step := "stow unstow"
	em.send(Event{Kind: StepStarted, Package: pkg, Message: step})
	unstow := runner.Unstow(pkg)
	em.result(pkg, unstow)
	if unstow.Err != nil {
		em.send(Event{Kind: StepFailed, Package: pkg, Message: step, Err: unstow.Err})
		closeTransaction()
		return warnings, unstow.Err
	}
	em.send(Event{Kind: StepDone, Package: pkg, Message: step})

	var undo journal
	fail := func(step string, err error) ([]error, error) {
		em.send(Event{Kind: StepFailed, Package: pkg, Message: step, Err: err})
		rollbackMoves(&undo, pkg, em)
		if transaction != nil {
			if rollbackErr := transaction.Rollback(); rollbackErr != nil {
				em.send(Event{Kind: Rollback, Package: pkg, Err: rollbackErr})
				err = errors.Join(err, rollbackErr)
			}
		}
		rollbackRestow(pkg, runner, em)
		return warnings, err
	}

	for _, module := range plan.Submodules {
		step = "restore standalone repository " + module.Path
		em.send(Event{Kind: StepStarted, Package: pkg, Message: step})
		if err := ctx.Err(); err != nil {
			return fail(step, err)
		}
		if err := transaction.Detach(ctx, module); err != nil {
			return fail(step, err)
		}
		em.send(Event{Kind: StepDone, Package: pkg, Message: step})
	}

	for _, move := range plan.Moves {
		step = fmt.Sprintf("mv %s %s", move.From, move.To)
		em.send(Event{Kind: StepStarted, Package: pkg, Message: step})
		if err := ctx.Err(); err != nil {
			return fail(step, err)
		}
		if info, err := os.Lstat(move.To); err == nil {
			submodule := false
			for _, entry := range plan.Entries {
				if entry.Submodule && entry.TargetPath(plan.Paths) == move.To {
					submodule = true
				}
			}
			if !submodule || !info.IsDir() {
				return fail(step, fmt.Errorf("%w: %s", ErrDestinationExists, move.To))
			}
			if err := removeRestoreDirectories(move.To, &undo); err != nil {
				return fail(step, err)
			}
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

	if err := ctx.Err(); err != nil {
		return fail("restore cancelled", err)
	}

	if plan.Partial() {
		step = "stow restow without the restored entries"
		em.send(Event{Kind: StepStarted, Package: pkg, Message: step})
		result := runner.RestowExcluding(pkg, plan.Selected)
		em.result(pkg, result)
		if result.Err != nil {
			return fail(step, result.Err)
		}
		em.send(Event{Kind: StepDone, Package: pkg, Message: step})
		closeTransaction()
		return warnings, nil
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
	closeTransaction()
	return warnings, nil
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
	for i := len(undo.removedDirs) - 1; i >= 0; i-- {
		if err := os.MkdirAll(undo.removedDirs[i].path, undo.removedDirs[i].mode); err != nil {
			em.send(Event{Kind: Rollback, Package: pkg, Err: err})
		}
	}
	undo.removedDirs = nil
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

func removeRestoreDirectories(path string, undo *journal) error {
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	children, err := os.ReadDir(path)
	if err != nil {
		return err
	}
	for _, child := range children {
		if !child.IsDir() {
			return fmt.Errorf("restore destination is not empty: %s", path)
		}
		if err := removeRestoreDirectories(filepath.Join(path, child.Name()), undo); err != nil {
			return err
		}
	}
	if err := os.Remove(path); err != nil {
		return err
	}
	undo.removedDirs = append(undo.removedDirs, removedDirectory{path, info.Mode()})
	return nil
}

func closeGitTransaction(transaction GitTransaction, pkg string, em emitter) error {
	if transaction == nil {
		return nil
	}
	err := transaction.Close()
	if err != nil {
		em.send(Event{Kind: OutputLine, Package: pkg, Message: err.Error()})
	}
	return err
}
