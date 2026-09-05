package tui

import (
	"context"
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/horbo/stower/internal/dotfiles"
)

func (m *Model) showError(title string, err error) {
	m.popup = popupError
	m.errorPopup.Open(title, err.Error())
	m.relayout()
}

func (m Model) apply() (tea.Model, tea.Cmd) {
	m.rebuildPlan()
	m.syncMain()
	for _, pkg := range m.plan.Packages {
		for _, blocked := range pkg.Blocked {
			m.failures[pkg.Package] = blocked.Reason
		}
	}
	m.staged.SetFailures(m.failures)
	if m.plan.Fatal != nil {
		m.showError("Cannot apply", m.plan.Fatal)
		return m, nil
	}
	if !m.plan.Runnable() {
		return m, m.setFlash("nothing to apply; blocked entries remain staged")
	}
	packages := 0
	for _, pkg := range m.plan.Packages {
		if len(pkg.Moves) > 0 {
			packages++
		}
	}
	body := []string{fmt.Sprintf("Apply %s in %s?", plural(m.plan.MoveCount(), "item", "items"), plural(packages, "package", "packages")), fmt.Sprintf("%d blocked entries excluded", m.plan.BlockedCount())}
	m.confirmPopup.Open(actionApply, "Apply staged plan", body, "restore the package with r")
	m.popup = popupConfirm
	m.relayout()
	return m, nil
}

func (m *Model) startExecution() tea.Cmd {
	if !m.plan.Runnable() {
		return nil
	}
	plan, runner := m.plan, m.runner
	m.failures = map[string]string{}
	m.staged.SetFailures(m.failures)
	return m.beginOperation("apply", plan, func(ctx context.Context, events chan<- dotfiles.Event) dotfiles.Summary {
		return dotfiles.Execute(ctx, plan, runner, events)
	})
}

func (m *Model) beginOperation(title string, plan dotfiles.AdoptPlan, work func(context.Context, chan<- dotfiles.Event) dotfiles.Summary) tea.Cmd {
	if m.exec != nil {
		return nil
	}
	m.runID++
	ctx, cancel := context.WithCancel(context.Background())
	run := &execution{id: m.runID, cancel: cancel, events: make(chan tea.Msg, eventBuffer), plan: plan}
	m.exec = run
	m.flash = ""
	m.logOpen = true
	m.restoreOpen = false
	m.diffOpen = false
	m.mainFocused = true
	m.mainLog.Start(title)
	m.relayout()
	start := func() tea.Msg {
		events := make(chan dotfiles.Event)
		done := make(chan dotfiles.Summary, 1)
		go func() { summary := work(ctx, events); close(events); done <- summary }()
		go func() {
			for event := range events {
				run.events <- execEventMsg{runID: run.id, event: event}
			}
			run.events <- execDoneMsg{runID: run.id, summary: <-done}
			close(run.events)
		}()
		return waitForEvent(run.events)()
	}
	return tea.Batch(start, m.status.SetRunning(true))
}

func waitForEvent(events <-chan tea.Msg) tea.Cmd {
	return func() tea.Msg { return <-events }
}

func (m *Model) cancelExecution() {
	if m.exec.cancelled {
		return
	}
	m.exec.cancelled = true
	m.exec.cancel()
	m.mainLog.Note("cancelling… waiting for rollback")
}

func (m Model) finishExecution(summary dotfiles.Summary) (tea.Model, tea.Cmd) {
	run := m.exec
	run.cancel()
	succeeded := map[string]bool{}
	for _, pkg := range summary.Succeeded {
		succeeded[pkg] = true
	}
	for _, pkg := range run.plan.Packages {
		if succeeded[pkg.Package] {
			for _, move := range pkg.Moves {
				delete(m.staging, move.From)
			}
			delete(m.removeGit, pkg.Package)
		}
	}
	if len(run.plan.Packages) > 0 {
		for _, failure := range summary.Failed {
			m.failures[failure.Package] = failure.Err.Error()
		}
	}
	for _, pkg := range run.plan.Packages {
		var reasons []string
		for _, blocked := range pkg.Blocked {
			reasons = append(reasons, blocked.Reason)
		}
		if len(reasons) > 0 {
			m.failures[pkg.Package] = strings.Join(reasons, "; ")
		}
	}
	m.mainLog.Finish(summary, run.cancelled)
	m.exec = nil
	m.status.SetRunning(false)
	m.rebuildPlan()
	m.syncMain()
	return m, refreshCmd(m.paths)
}
