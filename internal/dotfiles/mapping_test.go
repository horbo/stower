package dotfiles

import "testing"

func TestTargetToPackage(t *testing.T) {
	tests := []struct {
		name      string
		pkg       string
		relTarget string
		want      string
	}{
		{name: "top level dotfile", pkg: "zsh", relTarget: ".zshrc", want: "zsh/dot-zshrc"},
		{name: "dot directory", pkg: "nvim", relTarget: ".config/nvim", want: "nvim/dot-config/nvim"},
		{name: "file below a dot directory", pkg: "claude", relTarget: ".claude/settings.json", want: "claude/dot-claude/settings.json"},
		{name: "deeper literal dot is kept", pkg: "nvim", relTarget: ".config/nvim/.gitignore", want: "nvim/dot-config/nvim/.gitignore"},
		{name: "deeper dot directory is kept", pkg: "nvim", relTarget: ".config/.inner/file", want: "nvim/dot-config/.inner/file"},
		{name: "non dot top level entry", pkg: "bin", relTarget: "bin/tool", want: "bin/bin/tool"},
		{name: "trailing separator", pkg: "nvim", relTarget: ".config/nvim/", want: "nvim/dot-config/nvim"},
		{name: "empty relative path", pkg: "zsh", relTarget: "", want: "zsh"},
		{name: "dot relative path", pkg: "zsh", relTarget: ".", want: "zsh"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := TargetToPackage(test.pkg, test.relTarget); got != test.want {
				t.Errorf("TargetToPackage(%q, %q) = %q, want %q", test.pkg, test.relTarget, got, test.want)
			}
		})
	}
}

func TestPackageToTarget(t *testing.T) {
	tests := []struct {
		name   string
		relPkg string
		want   string
	}{
		{name: "top level dotfile", relPkg: "dot-zshrc", want: ".zshrc"},
		{name: "dot directory", relPkg: "dot-config/nvim", want: ".config/nvim"},
		{name: "file below a dot directory", relPkg: "dot-claude/settings.json", want: ".claude/settings.json"},
		{name: "deeper dot- is not translated", relPkg: "dot-config/dot-inner", want: ".config/dot-inner"},
		{name: "deeper literal dot is kept", relPkg: "dot-config/nvim/.gitignore", want: ".config/nvim/.gitignore"},
		{name: "non dot top level entry", relPkg: "bin/tool", want: "bin/tool"},
		{name: "bare dot- prefix is not translated", relPkg: "dot-", want: "dot-"},
		{name: "empty", relPkg: "", want: ""},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := PackageToTarget(test.relPkg); got != test.want {
				t.Errorf("PackageToTarget(%q) = %q, want %q", test.relPkg, got, test.want)
			}
		})
	}
}

func TestMappingRoundTrip(t *testing.T) {
	relTargets := []string{
		".zshrc",
		".config/nvim",
		".config/nvim/.gitignore",
		".claude/settings.json",
		"bin/tool",
		".config/.inner/file",
	}

	for _, relTarget := range relTargets {
		t.Run(relTarget, func(t *testing.T) {
			relPkg := TargetToPackage("pkg", relTarget)
			if PackageOf(relPkg) != "pkg" {
				t.Fatalf("PackageOf(%q) = %q, want %q", relPkg, PackageOf(relPkg), "pkg")
			}
			inPackage := relPkg[len("pkg/"):]
			if got := PackageToTarget(inPackage); got != relTarget {
				t.Errorf("PackageToTarget(%q) = %q, want %q", inPackage, got, relTarget)
			}
		})
	}
}

func TestPackageOf(t *testing.T) {
	tests := []struct {
		name        string
		relDotfiles string
		want        string
	}{
		{name: "package and entry", relDotfiles: "zsh/dot-zshrc", want: "zsh"},
		{name: "package only", relDotfiles: "zsh", want: "zsh"},
		{name: "deep entry", relDotfiles: "nvim/dot-config/nvim/init.lua", want: "nvim"},
		{name: "empty", relDotfiles: "", want: ""},
		{name: "dot", relDotfiles: ".", want: ""},
		{name: "outside", relDotfiles: "../elsewhere", want: ""},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := PackageOf(test.relDotfiles); got != test.want {
				t.Errorf("PackageOf(%q) = %q, want %q", test.relDotfiles, got, test.want)
			}
		})
	}
}
