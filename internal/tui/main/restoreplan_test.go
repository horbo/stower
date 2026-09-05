package mainpanel

import (
	"strings"
	"testing"

	"github.com/horbo/stower/internal/config"
	"github.com/horbo/stower/internal/dotfiles"
	"github.com/horbo/stower/internal/tui/styles"
)

func newPlanPanel(t *testing.T, plan dotfiles.RestorePlan) *RestorePlan {
	t.Helper()
	p := NewRestorePlan(styles.Default())
	p.SetSize(100, 20)
	p.SetPlan(plan)
	return p
}

func TestRestorePlanFatalShowsOnlyTheError(t *testing.T) {
	paths := config.Paths{Target: "/home/kamil", Dotfiles: "/home/kamil/dotfiles"}
	plan := dotfiles.BuildRestorePlan(paths, "missing")
	if plan.Fatal == nil {
		t.Fatal("BuildRestorePlan: want a fatal error for a package that does not exist")
	}
	if plan.Partial() {
		t.Error("a whole-package plan reports itself as partial after a failed walk")
	}
	view := newPlanPanel(t, plan).View()
	if !strings.Contains(view, "✘ ") {
		t.Fatalf("the fatal error is missing:\n%s", view)
	}
	for _, unwanted := range []string{"Then:", "moves back", "stays linked"} {
		if strings.Contains(view, unwanted) {
			t.Errorf("a fatal plan renders %q:\n%s", unwanted, view)
		}
	}

	entryPlan := dotfiles.BuildEntryRestorePlan(paths, "missing", []string{"dot-zshrc"})
	if entryPlan.Fatal == nil {
		t.Fatal("BuildEntryRestorePlan: want a fatal error for a package that does not exist")
	}
	if strings.Contains(newPlanPanel(t, entryPlan).View(), "Then:") {
		t.Error("a fatal entry plan renders a Then: line")
	}
}

func TestRestorePlanPartialAndWholeView(t *testing.T) {
	paths := config.Paths{Target: "/home/kamil", Dotfiles: "/home/kamil/dotfiles"}
	entries := []dotfiles.Entry{
		{PkgRel: "dot-zshrc", TargetRel: ".zshrc", State: dotfiles.Linked},
		{PkgRel: "dot-zprofile", TargetRel: ".zprofile", State: dotfiles.Linked},
	}
	partial := dotfiles.RestorePlan{
		Paths:    paths,
		Package:  "zsh",
		Entries:  entries,
		Selected: []string{"dot-zshrc"},
		Moves:    []dotfiles.Move{{From: "/home/kamil/dotfiles/zsh/dot-zshrc", To: "/home/kamil/.zshrc"}},
	}
	view := newPlanPanel(t, partial).View()
	for _, want := range []string{"↩ zsh/dot-zshrc", "1 entry moves back", "1 entry stays linked", "Then: relink 1 entry with stow and keep zsh"} {
		if !strings.Contains(view, want) {
			t.Errorf("partial plan does not contain %q:\n%s", want, view)
		}
	}

	whole := partial
	whole.Selected = []string{"dot-zshrc", "dot-zprofile"}
	whole.RemoveDir = "/home/kamil/dotfiles/zsh"
	view = newPlanPanel(t, whole).View()
	if !strings.Contains(view, "Then: remove empty /home/kamil/dotfiles/zsh") {
		t.Errorf("whole plan does not remove the package directory:\n%s", view)
	}
	if strings.Contains(view, "↩") {
		t.Errorf("whole plan marks entries as partial:\n%s", view)
	}

	blocked := partial
	blocked.Blocked = []dotfiles.Blocked{{Path: "/home/kamil/.zshrc", Package: "zsh", Reason: "replaced"}}
	if !strings.Contains(newPlanPanel(t, blocked).View(), "Issues") {
		t.Error("a blocked plan does not point to Issues")
	}
}
