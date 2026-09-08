package gitx

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

func checkStagedModules(ctx context.Context, dir string) error {
	changed, err := runContext(ctx, dir, "diff", "--cached", "--name-only", "--", ".gitmodules")
	if err != nil {
		return err
	}
	if changed == "" {
		return nil
	}
	before, err := moduleConfigBlob(ctx, dir, "HEAD:.gitmodules")
	if err != nil {
		return err
	}
	after, err := moduleConfigBlob(ctx, dir, ":.gitmodules")
	if err != nil {
		return err
	}
	names := map[string]bool{}
	for key, value := range before {
		if after[key] != value {
			names[key] = true
		}
	}
	for key, value := range after {
		if before[key] != value {
			names[key] = true
		}
	}
	if len(names) == 0 {
		return errors.New("commit existing .gitmodules edits first")
	}
	for key := range names {
		if !strings.HasPrefix(key, "submodule.") {
			return errors.New("commit existing .gitmodules edits first")
		}
		name, field, ok := strings.Cut(strings.TrimPrefix(key, "submodule."), "\x00")
		if !ok || field != "path" && field != "url" {
			return errors.New("commit existing .gitmodules edits first")
		}
		pathKey := "submodule." + name + "\x00path"
		oldPath, newPath := before[pathKey], after[pathKey]
		if oldPath != "" && newPath != "" {
			return errors.New("commit existing submodule configuration changes first")
		}
		path := newPath
		if path == "" {
			path = oldPath
		}
		if err := safeRelative(path); err != nil {
			return err
		}
		staged, err := runContext(ctx, dir, "ls-files", "--stage", "-z", "--", ":(literal)"+path)
		if err != nil {
			return err
		}
		old, _ := runContext(ctx, dir, "ls-tree", "-z", "HEAD", "--", ":(literal)"+path)
		if newPath != "" && (!strings.HasPrefix(staged, "160000 ") || old != "") {
			return fmt.Errorf("commit existing .gitmodules changes for %s first", path)
		}
		if newPath == "" && (staged != "" || !strings.HasPrefix(old, "160000 ")) {
			return fmt.Errorf("commit existing .gitmodules changes for %s first", path)
		}
	}
	return nil
}

func moduleConfigBlob(ctx context.Context, dir, blob string) (map[string]string, error) {
	result := map[string]string{}
	exists, err := moduleBlobExists(ctx, dir, blob)
	if err != nil {
		return nil, err
	}
	if !exists {
		return result, nil
	}
	content, err := runContext(ctx, dir, "cat-file", "blob", blob)
	if err != nil {
		return nil, err
	}
	if err = rejectConfigIncludes([]byte(content)); err != nil {
		return nil, err
	}
	out, err := runContext(ctx, dir, "config", "--null", "--blob", blob, "--list")
	if err != nil {
		if isNoMatchingKeys(err) && out == "" {
			return result, nil
		}
		return nil, err
	}
	for _, record := range strings.Split(out, "\x00") {
		if record == "" {
			continue
		}
		key, value, _ := strings.Cut(record, "\n")
		if strings.HasPrefix(key, "submodule.") {
			index := strings.LastIndex(key, ".")
			key = key[:index] + "\x00" + key[index+1:]
		}
		if _, exists := result[key]; exists {
			return nil, errors.New("duplicate submodule configuration")
		}
		result[key] = value
	}
	return result, nil
}

func moduleBlobExists(ctx context.Context, dir, blob string) (bool, error) {
	rev, path, ok := strings.Cut(blob, ":")
	if !ok {
		return false, fmt.Errorf("invalid blob reference: %q", blob)
	}
	if rev == "" {
		out, err := runContext(ctx, dir, "ls-files", "--stage", "-z", "--", ":(literal)"+path)
		return out != "", err
	}
	if _, err := runContext(ctx, dir, "rev-parse", "--verify", "--quiet", rev+"^{commit}"); err != nil {
		if ctx.Err() != nil {
			return false, ctx.Err()
		}
		return false, nil
	}
	out, err := runContext(ctx, dir, "ls-tree", "-z", rev, "--", ":(literal)"+path)
	if err != nil {
		return false, err
	}
	return out != "", nil
}
