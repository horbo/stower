package config

import (
	"errors"
	"os/exec"
	"testing"
)

func TestParseStowVersion(t *testing.T) {
	tests := []struct {
		name    string
		out     string
		want    string
		wantErr bool
	}{
		{name: "gnu stow 2.4.1", out: "stow (GNU Stow) version 2.4.1\n", want: "2.4.1"},
		{name: "two components", out: "stow (GNU Stow) version 2.4\n", want: "2.4"},
		{name: "v prefix", out: "stow (GNU Stow) version v2.4.1\n", want: "2.4.1"},
		{name: "trailing copyright", out: "stow (GNU Stow) version 2.3.1\nCopyright (C) 2019\n", want: "2.3.1"},
		{name: "no version", out: "command not found\n", wantErr: true},
		{name: "empty", out: "", wantErr: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := parseStowVersion(test.out)
			if test.wantErr {
				if err == nil {
					t.Fatalf("parseStowVersion(%q) = %q, want an error", test.out, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("parseStowVersion(%q): %v", test.out, err)
			}
			if got != test.want {
				t.Errorf("parseStowVersion(%q) = %q, want %q", test.out, got, test.want)
			}
		})
	}
}

func TestStowBinaryMissing(t *testing.T) {
	t.Setenv("PATH", t.TempDir())

	_, _, err := StowBinary()
	if !errors.Is(err, ErrStowNotFound) {
		t.Fatalf("StowBinary error = %v, want %v", err, ErrStowNotFound)
	}
	if want := "stow not found in PATH; install it with: brew install stow"; err.Error() != want {
		t.Errorf("error = %q, want %q", err.Error(), want)
	}
}

func TestStowBinaryFound(t *testing.T) {
	if _, err := exec.LookPath("stow"); err != nil {
		t.Skip("stow is not installed")
	}

	path, version, err := StowBinary()
	if err != nil {
		t.Fatalf("StowBinary: %v", err)
	}
	if path == "" {
		t.Error("path is empty")
	}
	if version == "" {
		t.Error("version is empty")
	}
}
