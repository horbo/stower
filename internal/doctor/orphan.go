package doctor

import (
	"context"
	"errors"
	"path/filepath"
	"strings"

	"github.com/horbo/stower/internal/config"
	"github.com/horbo/stower/internal/dotfiles"
	"github.com/horbo/stower/internal/gitx"
)

func RemoveOrphanedSubmodules(ctx context.Context, paths config.Paths, pkg string, events chan<- dotfiles.Event) dotfiles.Summary {
	var gitPaths []string
	summary := transaction(ctx, pkg, "remove stale .gitmodules entries", events, func() error {
		if err := dotfiles.ValidatePackageName(pkg); err != nil {
			return err
		}
		orphans, err := orphanedSubmodules(paths)
		if err != nil {
			return err
		}
		var selected []gitx.Submodule
		for _, m := range orphans {
			path := filepath.ToSlash(m.Path)
			if path == pkg || strings.HasPrefix(path, pkg+"/") {
				selected = append(selected, m)
			}
		}
		if len(selected) == 0 {
			return errors.New("no stale .gitmodules entries; refresh first")
		}
		if err := gitx.CheckGitmodulesClean(ctx, paths.Dotfiles); err != nil {
			return err
		}
		tx, err := gitx.BeginSubmoduleTransaction(paths.Dotfiles)
		if err != nil {
			return err
		}
		for _, m := range selected {
			emit(events, dotfiles.Event{Kind: dotfiles.OutputLine, Package: pkg, Message: "git config --file .gitmodules --remove-section submodule." + m.Name})
			err := ctx.Err()
			if err == nil {
				err = tx.Unregister(ctx, m)
			}
			if err != nil {
				if rollbackErr := tx.Rollback(); rollbackErr != nil {
					return errors.Join(err, rollbackErr)
				}
				emit(events, dotfiles.Event{Kind: dotfiles.Rollback, Package: pkg, Message: "restore the Git index and .gitmodules"})
				return err
			}
		}
		if err := tx.Close(); err != nil {
			emit(events, dotfiles.Event{Kind: dotfiles.OutputLine, Package: pkg, Message: "warning: " + err.Error()})
		}
		gitPaths = []string{".gitmodules"}
		return nil
	})
	summary.GitPaths = gitPaths
	return summary
}
