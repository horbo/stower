package config

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func fakeBinaries(t *testing.T, names ...string) string {
	t.Helper()
	dir := t.TempDir()
	for _, name := range names {
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
			t.Fatalf("WriteFile %s: %v", path, err)
		}
	}
	return dir
}

func TestSplitCommand(t *testing.T) {
	tests := []struct {
		name  string
		value string
		want  []string
	}{
		{name: "empty", value: ""},
		{name: "blank", value: "   \t "},
		{name: "single word", value: "vi", want: []string{"vi"}},
		{name: "surrounding space", value: "  nvim  ", want: []string{"nvim"}},
		{name: "flag", value: "code --wait", want: []string{"code", "--wait"}},
		{name: "collapsed spaces", value: "code   --wait", want: []string{"code", "--wait"}},
		{name: "quoted whole command", value: `"code --wait"`, want: []string{"code --wait"}},
		{name: "quoted argument", value: `nvim -u "~/my config/init.lua"`, want: []string{"nvim", "-u", "~/my config/init.lua"}},
		{name: "single quoted argument", value: `nvim -u '~/my config/init.lua'`, want: []string{"nvim", "-u", "~/my config/init.lua"}},
		{name: "double quotes inside single quotes", value: `sh -c 'echo "hi"'`, want: []string{"sh", "-c", `echo "hi"`}},
		{name: "escaped space", value: `/opt/my\ editor/bin -w`, want: []string{"/opt/my editor/bin", "-w"}},
		{name: "escaped quote inside quotes", value: `nvim -c "echo \"hi\""`, want: []string{"nvim", "-c", `echo "hi"`}},
		{name: "empty quoted argument", value: `nvim ""`, want: []string{"nvim", ""}},
		{name: "unterminated quote", value: `nvim "--flag`, want: []string{"nvim", "--flag"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := splitCommand(test.value); !reflect.DeepEqual(got, test.want) {
				t.Fatalf("splitCommand(%q) = %#v, want %#v", test.value, got, test.want)
			}
		})
	}
}

func TestEditorPrecedence(t *testing.T) {
	t.Setenv("PATH", fakeBinaries(t, "visual-editor", "env-editor", "vi"))

	tests := []struct {
		name   string
		visual string
		editor string
		want   []string
	}{
		{name: "visual wins", visual: "visual-editor", editor: "env-editor", want: []string{"visual-editor"}},
		{name: "editor when visual unset", editor: "env-editor", want: []string{"env-editor"}},
		{name: "editor when visual empty", visual: "   ", editor: "env-editor", want: []string{"env-editor"}},
		{name: "vi when both unset", want: []string{"vi"}},
		{name: "arguments are kept", visual: `env-editor --wait "a b"`, want: []string{"env-editor", "--wait", "a b"}},
		{name: "unknown editor falls back to vi", visual: "no-such-editor", editor: "also-missing", want: []string{"vi"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			env := envFrom(map[string]string{EnvVisual: test.visual, EnvEditor: test.editor})
			got, err := Editor(env)
			if err != nil {
				t.Fatalf("Editor: %v", err)
			}
			if !reflect.DeepEqual(got, test.want) {
				t.Fatalf("Editor = %#v, want %#v", got, test.want)
			}
		})
	}
}

func TestEditorMissing(t *testing.T) {
	t.Setenv("PATH", t.TempDir())

	argv, err := Editor(envFrom(map[string]string{}))
	if !errors.Is(err, ErrNoEditor) {
		t.Fatalf("Editor error = %v, want %v", err, ErrNoEditor)
	}
	if argv != nil {
		t.Errorf("Editor argv = %#v, want nil", argv)
	}
	if want := "no editor: set $EDITOR or $VISUAL"; err.Error() != want {
		t.Errorf("error = %q, want %q", err.Error(), want)
	}
}
