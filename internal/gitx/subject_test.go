package gitx

import "testing"

func TestSubject(t *testing.T) {
	tests := []struct {
		name   string
		change Change
		want   string
	}{
		{"add one package", Change{Operation: Add, Packages: []string{"zsh"}, Files: 3}, "stower: add zsh (3 files)"},
		{"add one file", Change{Operation: Add, Packages: []string{"misc"}, Files: 1}, "stower: add misc (1 file)"},
		{"add two packages", Change{Operation: Add, Packages: []string{"git", "ghostty"}, Files: 3}, "stower: add git, ghostty (3 files)"},
		{"add without a count", Change{Operation: Add, Packages: []string{"zsh"}}, "stower: add zsh"},
		{"remove", Change{Operation: Remove, Packages: []string{"zsh"}, Files: 3}, "stower: remove zsh"},
		{"update", Change{Operation: Update, Packages: []string{"zsh", "nvim"}, Files: 2}, "stower: update zsh, nvim (2 files)"},
		{"fix", Change{Operation: Fix, Entry: "claude/settings.json"}, "stower: fix claude/settings.json"},
		{"fix without an entry", Change{Operation: Fix, Packages: []string{"claude"}}, "stower: fix"},
		{"restow", Change{Operation: Restow, Packages: []string{"zsh", "nvim"}, Files: 9}, "stower: restow"},
		{"no packages", Change{Operation: Add, Files: 2}, "stower: add"},
		{"no operation", Change{Packages: []string{"zsh"}, Files: 2}, "stower: update zsh (2 files)"},
		{"empty package names", Change{Operation: Add, Packages: []string{"", "  ", "zsh"}, Files: 1}, "stower: add zsh (1 file)"},
		{"negative count", Change{Operation: Add, Packages: []string{"zsh"}, Files: -1}, "stower: add zsh"},
		{"newline in a package name", Change{Operation: Add, Packages: []string{"zsh\nrm -rf /"}, Files: 1}, "stower: add zsh rm -rf / (1 file)"},
		{"control characters in an entry", Change{Operation: Fix, Entry: "claude/\x00set\rtings"}, "stower: fix claude/set tings"},
		{"tabs collapse", Change{Operation: Add, Packages: []string{"a\t\tb"}, Files: 2}, "stower: add a b (2 files)"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Subject(tt.change)
			if got != tt.want {
				t.Fatalf("Subject() = %q, want %q", got, tt.want)
			}
			if err := validateSubject(got); err != nil {
				t.Fatalf("generated subject is not committable: %v", err)
			}
		})
	}
}
