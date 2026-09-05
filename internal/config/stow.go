package config

import (
	"errors"
	"fmt"
	"os/exec"
	"regexp"
)

var ErrStowNotFound = errors.New("stow not found in PATH; install it with: brew install stow")

var stowVersionRE = regexp.MustCompile(`version\s+v?([0-9]+(?:\.[0-9]+)*)`)

func StowBinary() (string, string, error) {
	path, err := exec.LookPath("stow")
	if err != nil {
		return "", "", ErrStowNotFound
	}
	out, err := exec.Command(path, "--version").Output()
	if err != nil {
		return "", "", fmt.Errorf("running %s --version: %w", path, err)
	}
	version, err := parseStowVersion(string(out))
	if err != nil {
		return "", "", err
	}
	return path, version, nil
}

func parseStowVersion(out string) (string, error) {
	match := stowVersionRE.FindStringSubmatch(out)
	if match == nil {
		return "", fmt.Errorf("cannot parse stow version from %q", out)
	}
	return match[1], nil
}
