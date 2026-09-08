package gitx

import (
	"context"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
)

type metadataReader struct {
	ctx    context.Context
	reader io.Reader
}

func (r metadataReader) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.reader.Read(p)
}

func copyGitMetadata(ctx context.Context, source, destination string) error {
	type directory struct {
		path string
		mode fs.FileMode
	}
	var directories []directory
	err := filepath.WalkDir(source, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(source, path)
		if err != nil {
			return err
		}
		target := filepath.Join(destination, rel)
		if entry.IsDir() {
			if path != source {
				if err := os.Mkdir(target, 0700); err != nil {
					return err
				}
			}
			directories = append(directories, directory{target, info.Mode().Perm()})
			return nil
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("unsupported Git metadata file: %s", path)
		}
		input, err := os.Open(path)
		if err != nil {
			return err
		}
		defer input.Close()
		output, err := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		if err != nil {
			return err
		}
		_, copyErr := io.Copy(output, metadataReader{ctx, input})
		closeErr := output.Close()
		if copyErr != nil {
			return copyErr
		}
		if closeErr != nil {
			return closeErr
		}
		return os.Chmod(target, info.Mode().Perm())
	})
	if err != nil {
		return err
	}
	for i := len(directories) - 1; i >= 0; i-- {
		if err := os.Chmod(directories[i].path, directories[i].mode); err != nil {
			return err
		}
	}
	return nil
}
