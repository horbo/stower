package config

import (
	"errors"
	"os"
	"os/exec"
	"strings"
)

const (
	EnvVisual = "VISUAL"
	EnvEditor = "EDITOR"
)

const fallbackEditor = "vi"

var ErrNoEditor = errors.New("no editor: set $EDITOR or $VISUAL")

func Editor(env func(string) string) ([]string, error) {
	if env == nil {
		env = os.Getenv
	}
	for _, candidate := range []string{env(EnvVisual), env(EnvEditor), fallbackEditor} {
		argv := splitCommand(candidate)
		if len(argv) == 0 || argv[0] == "" {
			continue
		}
		if _, err := exec.LookPath(argv[0]); err != nil {
			continue
		}
		return argv, nil
	}
	return nil, ErrNoEditor
}

func splitCommand(value string) []string {
	var argv []string
	var current strings.Builder
	started := false
	quote := rune(0)
	escaped := false
	for _, r := range value {
		switch {
		case escaped:
			current.WriteRune(r)
			escaped = false
		case quote == '\'':
			if r == '\'' {
				quote = 0
			} else {
				current.WriteRune(r)
			}
		case quote == '"':
			switch r {
			case '"':
				quote = 0
			case '\\':
				escaped = true
			default:
				current.WriteRune(r)
			}
		case r == '\'' || r == '"':
			quote = r
			started = true
		case r == '\\':
			escaped = true
			started = true
		case r == ' ' || r == '\t' || r == '\n' || r == '\r':
			if started {
				argv = append(argv, current.String())
				current.Reset()
				started = false
			}
		default:
			current.WriteRune(r)
			started = true
		}
	}
	if started {
		argv = append(argv, current.String())
	}
	return argv
}
