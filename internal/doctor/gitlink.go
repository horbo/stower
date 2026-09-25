package doctor

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/horbo/stower/internal/config"
	"github.com/horbo/stower/internal/dotfiles"
	"github.com/horbo/stower/internal/gitx"
)

func InspectGitlinks(ctx context.Context, paths config.Paths, pkg string) ([]dotfiles.NestedRepository, error) {
	if err := dotfiles.ValidatePackageName(pkg); err != nil {
		return nil, err
	}
	links, err := phantomGitlinks(paths)
	if err != nil {
		return nil, err
	}
	var rows []dotfiles.NestedRepository
	for _, link := range links {
		if !strings.HasPrefix(link, pkg+"/") {
			continue
		}
		row, err := inspectGitlink(ctx, paths, link)
		if err != nil {
			return nil, err
		}
		rows = append(rows, row)
	}
	if len(rows) == 0 {
		return nil, errors.New("no Git links to repair; refresh first")
	}
	return rows, nil
}

func inspectGitlink(ctx context.Context, paths config.Paths, link string) (dotfiles.NestedRepository, error) {
	rel := filepath.FromSlash(link)
	source := filepath.Join(paths.Dotfiles, rel)
	if err := safeParents(paths.Dotfiles, source); err != nil {
		return dotfiles.NestedRepository{}, err
	}
	row := dotfiles.NestedRepository{Source: source, Destination: rel, Gitlink: true}
	info, err := os.Lstat(filepath.Join(source, ".git"))
	switch {
	case errors.Is(err, fs.ErrNotExist):
		row.NoGit = true
		row.Info = gitx.Repository{Path: source, Reason: "the directory has no Git repository"}
		if _, statErr := os.Lstat(source); errors.Is(statErr, fs.ErrNotExist) {
			row.Info.Reason = "the directory is missing"
		}
		row.Choice.Action = dotfiles.RemoveGitlink
	case err != nil:
		return row, err
	case !info.IsDir():
		row.Info = gitx.Repository{Path: source, Reason: "conversion requires a repository with its own .git directory"}
	default:
		row.Info = gitx.InspectRepository(ctx, source)
		row.Choice.URL = row.Info.URL
		if row.Info.Reason == "" && gitx.ValidateSubmoduleURL(row.Info.URL) == nil {
			row.Choice.Action = dotfiles.ConvertRepository
		}
	}
	return row, nil
}

func RepairGitlinks(ctx context.Context, paths config.Paths, pkg string, choices map[string]dotfiles.RepositoryChoice, events chan<- dotfiles.Event) dotfiles.Summary {
	var gitPaths []string
	summary := transaction(ctx, pkg, "repair Git links", events, func() error {
		rows, err := InspectGitlinks(ctx, paths, pkg)
		if err != nil {
			return err
		}
		phantoms, err := phantomGitlinks(paths)
		if err != nil {
			return err
		}
		var selected []dotfiles.NestedRepository
		for _, row := range rows {
			choice, ok := choices[row.Source]
			if !ok || choice.Action == dotfiles.KeepRepository {
				continue
			}
			if !slices.Contains(row.AllowedActions(), choice.Action) {
				return fmt.Errorf("%s: %s is not available; refresh first", row.Destination, choice.Action)
			}
			row.Choice = choice
			if choice.Action == dotfiles.ConvertRepository {
				if err := row.ConversionError(); err != nil {
					return fmt.Errorf("%s: %w", row.Destination, err)
				}
			}
			selected = append(selected, row)
		}
		if len(selected) == 0 {
			return errors.New("every Git link is set to Keep; nothing to do")
		}
		if err := gitx.CheckSubmoduleMetadataExcept(ctx, paths.Dotfiles, phantoms); err != nil {
			return err
		}
		tx, err := gitx.BeginSubmoduleTransaction(paths.Dotfiles)
		if err != nil {
			return err
		}
		rollback := func(cause error) error {
			if err := tx.Rollback(); err != nil {
				return errors.Join(cause, err)
			}
			emit(events, dotfiles.Event{Kind: dotfiles.Rollback, Package: pkg, Message: "restore the Git index and .gitmodules"})
			return cause
		}
		var removals, registered []string
		for _, row := range selected {
			if err := ctx.Err(); err != nil {
				return rollback(err)
			}
			rel := filepath.ToSlash(row.Destination)
			emit(events, dotfiles.Event{Kind: dotfiles.OutputLine, Package: pkg, Message: "git rm --cached " + rel})
			if err := tx.Untrack(ctx, row.Destination); err != nil {
				return rollback(err)
			}
			switch row.Choice.Action {
			case dotfiles.ConvertRepository:
				emit(events, dotfiles.Event{Kind: dotfiles.OutputLine, Package: pkg, Message: "git submodule add " + row.Choice.URL + " " + rel})
				if err := tx.Register(ctx, row.Destination, row.Choice.URL, row.Info.Head); err != nil {
					return rollback(err)
				}
				registered = []string{".gitmodules"}
			case dotfiles.RemoveRepositoryGit:
				removals = append(removals, filepath.Join(row.Source, ".git"))
			}
		}
		if err := ctx.Err(); err != nil {
			return rollback(err)
		}
		if err := tx.Close(); err != nil {
			emit(events, dotfiles.Event{Kind: dotfiles.OutputLine, Package: pkg, Message: "warning: " + err.Error()})
		}
		for _, gitDir := range removals {
			emit(events, dotfiles.Event{Kind: dotfiles.OutputLine, Package: pkg, Message: "remove " + gitDir})
			if err := os.RemoveAll(gitDir); err != nil {
				emit(events, dotfiles.Event{Kind: dotfiles.OutputLine, Package: pkg, Message: "warning: " + err.Error()})
			}
		}
		gitPaths = registered
		return nil
	})
	summary.GitPaths = gitPaths
	return summary
}
