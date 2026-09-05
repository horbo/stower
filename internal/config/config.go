package config

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

const (
	EnvDotfiles = "STOWER_DOTFILES"
	EnvNoMouse  = "STOWER_NO_MOUSE"
)

type Paths struct {
	Target   string
	Dotfiles string
}

func Resolve(flagDotfiles, flagTarget string, env func(string) string) (Paths, error) {
	if env == nil {
		env = os.Getenv
	}
	home := env("HOME")

	dotfiles := flagDotfiles
	if dotfiles == "" {
		dotfiles = env(EnvDotfiles)
	}
	if dotfiles == "" {
		if home == "" {
			return Paths{}, errors.New("cannot determine the default dotfiles directory: HOME is not set")
		}
		dotfiles = filepath.Join(home, "dotfiles")
	}

	target := flagTarget
	if target == "" {
		target = home
	}
	if target == "" {
		return Paths{}, errors.New("cannot determine the target directory: HOME is not set")
	}

	resolvedTarget, err := normalize(target, home)
	if err != nil {
		return Paths{}, fmt.Errorf("target %s: %w", target, err)
	}
	resolvedDotfiles, err := normalize(dotfiles, home)
	if err != nil {
		return Paths{}, fmt.Errorf("dotfiles %s: %w", dotfiles, err)
	}
	if err := checkAccessible(resolvedDotfiles); err != nil {
		return Paths{}, fmt.Errorf("dotfiles %s: %w", resolvedDotfiles, err)
	}

	return Paths{Target: resolvedTarget, Dotfiles: resolvedDotfiles}, nil
}

func MouseEnabled(flagNoMouse bool, env func(string) string) bool {
	if flagNoMouse {
		return false
	}
	if env == nil {
		env = os.Getenv
	}
	switch strings.ToLower(strings.TrimSpace(env(EnvNoMouse))) {
	case "", "0", "false", "no", "off":
		return true
	default:
		return false
	}
}

func normalize(path, home string) (string, error) {
	expanded, err := expandTilde(path, home)
	if err != nil {
		return "", err
	}
	abs, err := filepath.Abs(expanded)
	if err != nil {
		return "", err
	}
	if _, err := os.Stat(abs); err != nil {
		return abs, nil
	}
	resolved, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return "", err
	}
	return resolved, nil
}

func expandTilde(path, home string) (string, error) {
	if path == "" {
		return "", errors.New("path is empty")
	}
	if path != "~" && !strings.HasPrefix(path, "~"+string(filepath.Separator)) {
		return path, nil
	}
	if home == "" {
		return "", errors.New("cannot expand ~: HOME is not set")
	}
	if path == "~" {
		return home, nil
	}
	return filepath.Join(home, path[2:]), nil
}

func checkAccessible(path string) error {
	for dir := path; ; {
		info, err := os.Stat(dir)
		if err == nil {
			if !info.IsDir() {
				return fmt.Errorf("%s is not a directory", dir)
			}
			return nil
		}
		if !errors.Is(err, fs.ErrNotExist) {
			return fmt.Errorf("%s is not accessible: %w", dir, err)
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return errors.New("no accessible parent directory")
		}
		dir = parent
	}
}
