package dotfiles

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/horbo/stower/internal/config"
	"github.com/horbo/stower/internal/gitx"
)

type RepositoryAction int

const (
	KeepRepository RepositoryAction = iota
	RemoveRepositoryGit
	ConvertRepository
)

func (a RepositoryAction) String() string {
	switch a {
	case RemoveRepositoryGit:
		return "Remove .git"
	case ConvertRepository:
		return "Convert to submodule"
	default:
		return "Keep repository"
	}
}

type RepositoryChoice struct {
	Action RepositoryAction
	URL    string
}

type NestedRepository struct {
	Source      string
	Destination string
	Info        gitx.Repository
	Choice      RepositoryChoice
	Conflict    string
}

func (r NestedRepository) AllowedActions() []RepositoryAction {
	actions := []RepositoryAction{KeepRepository}
	if r.Info.GitDir != "" {
		actions = append(actions, RemoveRepositoryGit)
	}
	if r.Info.Reason == "" {
		actions = append(actions, ConvertRepository)
	}
	return actions
}

type GitTransaction interface {
	Register(context.Context, string, string, string) error
	Detach(context.Context, gitx.Submodule) error
	Rollback() error
	Close() error
}

type GitOperations interface {
	Check(context.Context, string) error
	Begin(string) (GitTransaction, error)
}

type repositoryGit struct{}

func (repositoryGit) Check(ctx context.Context, dir string) error {
	return gitx.CheckSubmoduleMetadata(ctx, dir)
}
func (repositoryGit) Begin(dir string) (GitTransaction, error) {
	return gitx.BeginSubmoduleTransaction(dir)
}

func scanRepositories(ctx context.Context, root, dest string) ([]NestedRepository, error) {
	var found []NestedRepository
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if err = ctx.Err(); err != nil {
			return err
		}
		if path == root || d.Name() != ".git" {
			return nil
		}
		source := filepath.Dir(path)
		rel, err := filepath.Rel(root, source)
		if err != nil {
			return err
		}
		info := gitx.Repository{Path: source, Reason: "conversion requires a repository with its own .git directory"}
		if d.IsDir() {
			info = gitx.InspectRepository(ctx, source)
		}
		found = append(found, NestedRepository{Source: source, Destination: filepath.Join(dest, rel), Info: info, Choice: RepositoryChoice{URL: info.URL}})
		return filepath.SkipDir
	})
	for i := range found {
		for j := range found {
			if i != j && isInside(found[j].Source, found[i].Source) {
				found[i].Info.Reason = "repositories inside other repositories are not supported"
				found[j].Info.Reason = "repositories inside other repositories are not supported"
			}
		}
	}
	return found, err
}

func (r NestedRepository) ConversionError() error {
	if r.Info.Reason != "" {
		return errors.New(r.Info.Reason)
	}
	if err := gitx.ValidateSubmoduleURL(r.Choice.URL); err != nil {
		return err
	}
	if r.Conflict != "" {
		return errors.New(r.Conflict)
	}
	return nil
}

func ApplyRepositoryChoices(plan *AdoptPlan, choices map[string]RepositoryChoice) {
	for i := range plan.Packages {
		p := &plan.Packages[i]
		for j := range p.Repositories {
			r := &p.Repositories[j]
			if choice, ok := choices[r.Source]; ok {
				r.Choice = choice
			}
		}
	}
}

func repositoryPlanError(ctx context.Context, paths config.Paths, p PackageAdopt) error {
	for _, r := range p.Repositories {
		if r.Choice.Action != ConvertRepository {
			continue
		}
		if err := r.ConversionError(); err != nil {
			return fmt.Errorf("%s: %w", r.Source, err)
		}
		current := gitx.InspectRepository(ctx, r.Source)
		if current.Reason != "" {
			return fmt.Errorf("%s: %s", r.Source, current.Reason)
		}
		if current.Head != r.Info.Head {
			return fmt.Errorf("%s: HEAD changed; rebuild the plan", r.Source)
		}
		rel, err := filepath.Rel(paths.Dotfiles, r.Destination)
		if err != nil {
			return err
		}
		if err = gitx.ValidateSubmoduleDestination(ctx, paths.Dotfiles, rel); err != nil {
			return err
		}
	}
	return nil
}

func hasConversions(p PackageAdopt) bool {
	for _, r := range p.Repositories {
		if r.Choice.Action == ConvertRepository {
			return true
		}
	}
	return false
}

func planRestoreRepositories(plan *RestorePlan) {
	modules, err := gitx.ListSubmodules(context.Background(), plan.Paths.Dotfiles)
	if err != nil {
		plan.Fatal = err
		return
	}
	for _, m := range modules {
		root := filepath.Join(plan.Paths.Dotfiles, m.Path)
		covered, split := false, false
		for _, move := range plan.Moves {
			if move.From == root || isInside(move.From, root) {
				covered = true
			}
			if isInside(root, move.From) {
				split = true
			}
		}
		if split && !covered {
			plan.Blocked = append(plan.Blocked, Blocked{Package: plan.Package, Path: root, Reason: "restore the entire submodule, not individual files"})
			continue
		}
		if !covered {
			continue
		}
		if _, err := gitx.ValidateRestorableSubmodule(context.Background(), plan.Paths.Dotfiles, m); err != nil {
			plan.Blocked = append(plan.Blocked, Blocked{Package: plan.Package, Path: root, Reason: err.Error()})
			continue
		}
		plan.Submodules = append(plan.Submodules, m)
	}
}

func removeSelectedGit(p PackageAdopt, em emitter) error {
	var errs []error
	for _, r := range p.Repositories {
		if r.Choice.Action != RemoveRepositoryGit {
			continue
		}
		path := filepath.Join(r.Destination, ".git")
		info, err := os.Lstat(path)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		if !info.IsDir() {
			errs = append(errs, fmt.Errorf("refusing to remove non-directory Git metadata: %s", path))
			continue
		}
		if err = os.RemoveAll(path); err != nil {
			errs = append(errs, err)
			continue
		}
		em.send(Event{Kind: OutputLine, Package: p.Package, Message: "remove " + path})
	}
	return errors.Join(errs...)
}

func ValidateRepositoryChoices(ctx context.Context, plan *AdoptPlan) {
	checked := false
	var sharedErr error
	for i := range plan.Packages {
		if ctx.Err() != nil {
			return
		}
		for j := range plan.Packages[i].Repositories {
			if ctx.Err() != nil {
				return
			}
			r := &plan.Packages[i].Repositories[j]
			r.Conflict = ""
			if r.Choice.Action != ConvertRepository || r.Info.Reason != "" {
				continue
			}
			if !checked {
				sharedErr = gitx.CheckSubmoduleMetadata(ctx, plan.Paths.Dotfiles)
				checked = true
			}
			if sharedErr != nil {
				r.Conflict = sharedErr.Error()
				continue
			}
			rel, err := filepath.Rel(plan.Paths.Dotfiles, r.Destination)
			if err == nil {
				err = gitx.ValidateSubmoduleDestination(ctx, plan.Paths.Dotfiles, rel)
			}
			if err != nil {
				r.Conflict = err.Error()
			}
		}
	}
}
