package dotfiles

import (
	"errors"
	"path/filepath"
	"reflect"
	"testing"
)

func TestValidatePackageName(t *testing.T) {
	tests := []struct {
		name    string
		pkg     string
		wantErr bool
	}{
		{name: "simple", pkg: "zsh"},
		{name: "with digits and separators", pkg: "my_pkg-2.0"},
		{name: "empty", pkg: "", wantErr: true},
		{name: "leading dot", pkg: ".git", wantErr: true},
		{name: "leading dash looks like a flag", pkg: "-n", wantErr: true},
		{name: "path separator", pkg: "a/b", wantErr: true},
		{name: "parent directory", pkg: "..", wantErr: true},
		{name: "space", pkg: "my pkg", wantErr: true},
		{name: "shell metacharacter", pkg: "a;rm", wantErr: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := ValidatePackageName(test.pkg)
			if test.wantErr && err == nil {
				t.Errorf("ValidatePackageName(%q) = nil, want an error", test.pkg)
			}
			if !test.wantErr && err != nil {
				t.Errorf("ValidatePackageName(%q) = %v, want nil", test.pkg, err)
			}
		})
	}
}

func TestValidateStagingPath(t *testing.T) {
	paths := newPaths(t)
	writeFile(t, filepath.Join(paths.Target, ".zshrc"), "x")
	writeFile(t, filepath.Join(paths.Dotfiles, "zsh", "dot-zshrc"), "x")
	symlink(t, "../dotfiles/zsh/dot-zshrc", filepath.Join(paths.Target, ".link"))
	mkdir(t, filepath.Join(paths.Target, ".config", "foo"))
	writeFile(t, filepath.Join(paths.Target, "dot-foo"), "x")
	writeFile(t, filepath.Join(paths.Target, "dot-config", "nvim", "init.lua"), "x")
	writeFile(t, filepath.Join(paths.Target, "dotfiles-notes"), "x")
	writeFile(t, filepath.Join(paths.Target, ".config", "dot-deep"), "x")

	tests := []struct {
		name    string
		path    string
		wantErr error
	}{
		{name: "regular file", path: filepath.Join(paths.Target, ".zshrc")},
		{name: "directory", path: filepath.Join(paths.Target, ".config", "foo")},
		{name: "dot prefixed file", path: filepath.Join(paths.Target, "dot-foo"), wantErr: ErrDotPrefixed},
		{name: "inside a dot prefixed directory", path: filepath.Join(paths.Target, "dot-config", "nvim"), wantErr: ErrDotPrefixed},
		{name: "prefix only looks similar", path: filepath.Join(paths.Target, "dotfiles-notes")},
		{name: "dot prefix below the first component", path: filepath.Join(paths.Target, ".config", "dot-deep")},
		{name: "outside the target", path: filepath.Join(filepath.Dir(paths.Target), "elsewhere"), wantErr: ErrOutsideTarget},
		{name: "the target itself", path: paths.Target, wantErr: ErrOutsideTarget},
		{name: "escaping with dot dot", path: filepath.Join(paths.Target, "..", "x"), wantErr: ErrOutsideTarget},
		{name: "relative path", path: ".zshrc", wantErr: ErrOutsideTarget},
		{name: "symlink", path: filepath.Join(paths.Target, ".link"), wantErr: ErrSymlink},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := ValidateStagingPath(paths, test.path)
			if test.wantErr == nil {
				if err != nil {
					t.Errorf("ValidateStagingPath(%q) = %v, want nil", test.path, err)
				}
				return
			}
			if !errors.Is(err, test.wantErr) {
				t.Errorf("ValidateStagingPath(%q) = %v, want %v", test.path, err, test.wantErr)
			}
		})
	}
}

func TestValidateStagingPathInsideDotfiles(t *testing.T) {
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	paths := newPathsAt(t, root, filepath.Join(root, "dotfiles"))
	writeFile(t, filepath.Join(paths.Dotfiles, "zsh", "dot-zshrc"), "x")

	err = ValidateStagingPath(paths, filepath.Join(paths.Dotfiles, "zsh", "dot-zshrc"))
	if !errors.Is(err, ErrInsideDotfiles) {
		t.Errorf("ValidateStagingPath = %v, want %v", err, ErrInsideDotfiles)
	}
}

func TestDedupeStaging(t *testing.T) {
	staging := Staging{
		"/home/u/.config":          "cfg",
		"/home/u/.config/nvim":     "nvim",
		"/home/u/.config/gh/hosts": "gh",
		"/home/u/.config-backup":   "backup",
		"/home/u/.zshrc":           "zsh",
	}

	kept, messages := DedupeStaging(staging)
	want := Staging{
		"/home/u/.config":        "cfg",
		"/home/u/.config-backup": "backup",
		"/home/u/.zshrc":         "zsh",
	}
	if !reflect.DeepEqual(kept, want) {
		t.Errorf("DedupeStaging kept %v, want %v", kept, want)
	}
	if len(messages) != 2 {
		t.Errorf("DedupeStaging messages = %v, want 2 of them", messages)
	}
}

func TestCheckSameDeviceOnOneFilesystem(t *testing.T) {
	paths := newPaths(t)
	if err := CheckSameDevice(paths); err != nil {
		t.Errorf("CheckSameDevice: %v", err)
	}
}

func TestCheckSameDeviceWithMissingDotfiles(t *testing.T) {
	paths := newPaths(t)
	paths.Dotfiles = filepath.Join(paths.Dotfiles, "not", "created", "yet")
	if err := CheckSameDevice(paths); err != nil {
		t.Errorf("CheckSameDevice: %v", err)
	}
}
