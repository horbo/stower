package tui

import (
	"context"
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/horbo/stower/internal/doctor"
	"github.com/horbo/stower/internal/dotfiles"
)

const (
	actionRestore = "restore"
	actionFix     = "fix"
	actionRestow  = "restow"
)

type gitlinksLoadedMsg struct {
	issue doctor.Issue
	rows  []dotfiles.NestedRepository
	err   error
}

type diffLoadedMsg struct {
	id          int
	title, text string
	err         error
}

func (m *Model) openRestore() tea.Cmd {
	selected, ok := m.packages.Selected()
	if !ok {
		return nil
	}
	m.restoreEntries = nil
	m.restorePlan = doctor.BuildRestorePlan(m.paths, selected.Name)
	m.mainRestore.SetPlan(m.restorePlan)
	m.restoreOpen = true
	m.mainFocused = true
	m.relayout()
	return nil
}

func (m *Model) openEntryRestore() tea.Cmd {
	selected, ok := m.packages.Selected()
	if !ok {
		return nil
	}
	entries := m.mainPkg.Marked()
	if len(entries) == 0 {
		issue, ok := m.mainPkg.SelectedIssue()
		if !ok {
			return nil
		}
		entries = []string{issue.Entry.PkgRel}
	}
	m.restoreEntries = entries
	m.restorePlan = doctor.BuildEntryRestorePlan(m.paths, selected.Name, entries)
	m.mainRestore.SetPlan(m.restorePlan)
	m.restoreOpen = true
	m.mainFocused = true
	m.relayout()
	return nil
}

func (m *Model) confirmRestore() tea.Cmd {
	m.restorePlan = doctor.BuildEntryRestorePlan(m.paths, m.restorePlan.Package, m.restoreEntries)
	m.mainRestore.SetPlan(m.restorePlan)
	if m.restorePlan.Fatal != nil {
		m.showError("Cannot restore", m.restorePlan.Fatal)
		return nil
	}
	if !m.restorePlan.Runnable() {
		return m.setFlash("restore blocked or empty; fix conflicting entries in Issues first")
	}
	m.confirmPopup.Open(actionRestore, m.restoreTitle(), m.restoreBody(), "stage the files again")
	m.popup = popupConfirm
	m.relayout()
	return nil
}

func (m Model) restoreTitle() string {
	if m.restoreEntries == nil {
		return "Restore " + m.restorePlan.Package
	}
	return "Restore " + plural(len(m.restorePlan.Selected), "entry", "entries") + " from " + m.restorePlan.Package
}

func (m Model) restoreBody() []string {
	plan := m.restorePlan
	body := []string{"Move " + plural(len(plan.Moves), "entry", "entries") + " back into the target?"}
	if m.restoreEntries != nil {
		for _, rel := range plan.Selected {
			body = append(body, "  "+plan.Package+"/"+rel)
		}
	}
	if plan.Partial() {
		body = append(body, "Keep "+plural(len(plan.Entries)-len(plan.Selected), "entry", "entries")+" linked and keep the package directory.")
		return body
	}
	return append(body, "Remove the empty package directory.")
}

func (m *Model) selectedIssue() (doctor.Issue, bool) {
	if m.focus == Issues {
		return m.issues.Selected()
	}
	return m.mainPkg.SelectedIssue()
}

func (m *Model) openFix() tea.Cmd {
	issue, ok := m.selectedIssue()
	if !ok {
		return nil
	}
	if !issue.Fixable {
		return m.setFlash(string(issue.State) + ": no automatic fix")
	}
	m.fixIssue = issue
	if issue.State == doctor.Invisible {
		paths := m.paths
		return func() tea.Msg {
			rows, err := doctor.InspectGitlinks(context.Background(), paths, issue.Package)
			return gitlinksLoadedMsg{issue: issue, rows: rows, err: err}
		}
	}
	if issue.State == doctor.Orphaned {
		body := []string{"Remove .gitmodules entries without a Git link?"}
		for _, path := range issue.Modules {
			body = append(body, "  "+path)
		}
		m.confirmPopup.Open(actionFix, "Fix orphaned "+issue.Package, body, "skip the commit; git reset restores the index")
		m.popup = popupConfirm
	} else if issue.State == doctor.Replaced {
		m.fixPopup.Open(issue)
		m.popup = popupFix
	} else {
		m.fixAction = doctor.Restow
		undo := "remove the package links with restore"
		switch issue.State {
		case doctor.Unnormalized:
			m.fixAction = doctor.Normalize
			undo = "rename the entry back manually after restoring"
		case doctor.Unowned:
			m.fixAction = doctor.Relink
			undo = "restore the entry to move the file back"
		}
		m.confirmPopup.Open(actionFix, "Fix "+string(issue.State), []string{issue.Package + "/" + issue.Entry.PkgRel, string(m.fixAction)}, undo)
		m.popup = popupConfirm
	}
	m.relayout()
	return nil
}

func (m *Model) gitlinksLoaded(msg gitlinksLoadedMsg) tea.Cmd {
	if msg.err != nil {
		m.showError("Cannot repair Git links", msg.err)
		return nil
	}
	if m.popup != popupNone || m.fixIssue.Package != msg.issue.Package {
		return nil
	}
	m.gitlinkFix = true
	m.repositoriesPopup.OpenTitled("Git links in "+msg.issue.Package, msg.rows)
	m.popup = popupRepositories
	m.relayout()
	return nil
}

func (m *Model) confirmGitlinks(choices map[string]dotfiles.RepositoryChoice) tea.Cmd {
	m.gitlinkFix = false
	m.gitlinkChoices = choices
	body := []string{"Repair Git links without a .gitmodules entry?"}
	sources := make([]string, 0, len(choices))
	for source := range choices {
		sources = append(sources, source)
	}
	sort.Strings(sources)
	changes, removals := 0, 0
	for _, source := range sources {
		choice := choices[source]
		rel, err := filepath.Rel(m.paths.Dotfiles, source)
		if err != nil {
			rel = source
		}
		line := "  " + filepath.ToSlash(rel) + " → " + choice.Action.String()
		if choice.Action == dotfiles.ConvertRepository {
			line += " (" + choice.URL + ")"
		}
		body = append(body, line)
		if choice.Action != dotfiles.KeepRepository {
			changes++
		}
		if choice.Action == dotfiles.RemoveRepositoryGit {
			removals++
		}
	}
	if changes == 0 {
		return m.setFlash("every Git link is set to Keep; nothing to do")
	}
	if removals > 0 {
		body = append(body, "Deleted .git directories and their history cannot be recovered.")
	}
	m.confirmPopup.Open(actionFix, "Fix invisible "+m.fixIssue.Package, body, "skip the commit; git reset restores the index")
	m.popup = popupConfirm
	m.relayout()
	return nil
}

func (m *Model) confirmRestow() tea.Cmd {
	m.restowPackages = nil
	for _, info := range m.pkgs {
		m.restowPackages = append(m.restowPackages, info.name)
	}
	if len(m.restowPackages) == 0 {
		return m.setFlash("no packages to restow")
	}
	m.confirmPopup.Open(actionRestow, "Restow all packages", []string{fmt.Sprintf("Restow %d packages?", len(m.restowPackages)), strings.Join(m.restowPackages, ", ")}, "restore packages with r")
	m.popup = popupConfirm
	m.relayout()
	return nil
}

func (m *Model) startConfirmed(action string) tea.Cmd {
	paths, runner := m.paths, m.runner
	switch action {
	case actionApply:
		return m.startExecution()
	case actionRestore:
		plan := doctor.BuildEntryRestorePlan(paths, m.restorePlan.Package, m.restoreEntries)
		if !plan.Runnable() {
			m.showError("Cannot restore", fmt.Errorf("restore plan changed or is blocked; inspect Issues"))
			return nil
		}
		return m.beginOperation("restore", dotfiles.AdoptPlan{}, func(ctx context.Context, events chan<- dotfiles.Event) dotfiles.Summary {
			return dotfiles.ExecuteRestore(ctx, plan, runner, events)
		})
	case actionFix:
		issue, fix := m.fixIssue, m.fixAction
		if issue.State == doctor.Invisible {
			choices := m.gitlinkChoices
			return m.beginOperation("fix", dotfiles.AdoptPlan{}, func(ctx context.Context, events chan<- dotfiles.Event) dotfiles.Summary {
				return doctor.RepairGitlinks(ctx, paths, issue.Package, choices, events)
			})
		}
		if issue.State == doctor.Orphaned {
			return m.beginOperation("fix", dotfiles.AdoptPlan{}, func(ctx context.Context, events chan<- dotfiles.Event) dotfiles.Summary {
				return doctor.RemoveOrphanedSubmodules(ctx, paths, issue.Package, events)
			})
		}
		return m.beginOperation("fix", dotfiles.AdoptPlan{}, func(ctx context.Context, events chan<- dotfiles.Event) dotfiles.Summary {
			return doctor.Fix(ctx, paths, issue, fix, runner, events)
		})
	case actionRestow:
		packages := append([]string(nil), m.restowPackages...)
		return m.beginOperation("restow", dotfiles.AdoptPlan{}, func(ctx context.Context, events chan<- dotfiles.Event) dotfiles.Summary {
			return doctor.RestowPackages(ctx, packages, runner, events)
		})
	}
	return nil
}

func (m *Model) syncIssueContext() {
	issue, ok := m.issues.Selected()
	if !ok {
		m.mainDetail.SetText("Issues", "no issues")
		return
	}
	lines := []string{issue.Glyph() + " " + string(issue.State), "ENTRY: " + issue.Entry.PackagePath(m.paths, issue.Package), "TARGET: " + issue.Entry.TargetPath(m.paths), "", issue.Detail, "", "f fix · D diff"}
	if !issue.Fixable {
		lines[len(lines)-1] = "report only · D diff"
	}
	m.mainDetail.SetText("Issue: "+issue.Package+"/"+issue.Entry.PkgRel, strings.Join(lines, "\n"))
}

func (m *Model) openDiff() tea.Cmd {
	issue, ok := m.selectedIssue()
	if !ok {
		return nil
	}
	m.diffID++
	id, paths := m.diffID, m.paths
	title := "Diff: " + issue.Package + "/" + issue.Entry.PkgRel
	m.diffOpen = true
	m.mainFocused = true
	m.mainDiff.SetText(title, "loading diff…")
	m.relayout()
	return func() tea.Msg {
		text, err := doctor.Diff(paths, issue)
		return diffLoadedMsg{id: id, title: title, text: text, err: err}
	}
}
