package tui

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/horbo/stower/internal/config"
	"github.com/horbo/stower/internal/dotfiles"
)

func planCommand(t *testing.T, cmd tea.Cmd) tea.Cmd {
	t.Helper()
	msg := cmd()
	batch, ok := msg.(tea.BatchMsg)
	if !ok || len(batch) == 0 {
		t.Fatalf("scan command = %T, want tea.BatchMsg", msg)
	}
	return func() tea.Msg {
		for _, leaf := range batch {
			result := leaf()
			if _, ok := result.(planBuiltMsg); ok {
				return result
			}
		}
		t.Fatalf("scan command batch has no planBuiltMsg")
		return nil
	}
}

func TestStagingKeepsOnlyLatestPendingScan(t *testing.T) {
	model, paths := newStagingModel(t)
	m := model.(Model)
	first := filepath.Join(paths.Target, ".bar")
	second := filepath.Join(paths.Target, ".config", "foo")
	started := make(chan struct{}, 1)
	cancelled := make(chan struct{}, 1)
	calls := 0
	m.buildAdoptPlan = func(ctx context.Context, scanPaths config.Paths, staging dotfiles.Staging) dotfiles.AdoptPlan {
		calls++
		if staging[first] != "" {
			started <- struct{}{}
			<-ctx.Done()
			cancelled <- struct{}{}
			return dotfiles.AdoptPlan{Paths: scanPaths, Fatal: ctx.Err()}
		}
		return dotfiles.AdoptPlan{Paths: scanPaths}
	}

	m.staging[first] = "first"
	firstCmd := planCommand(t, m.stagingChanged())
	firstResult := make(chan tea.Msg, 1)
	go func() { firstResult <- firstCmd() }()
	<-started
	if !m.planActive || !strings.Contains(m.staged.View(), "Scanning…") {
		t.Fatal("staging did not enter the scanning state")
	}
	if !strings.Contains(m.staged.View(), ".bar") {
		t.Fatal("staged entry disappeared while its scan was running")
	}
	m.focus = Home
	before, ok := m.homePanel.Selected()
	if !ok {
		t.Fatal("home tree has no selected entry")
	}
	updated, _ := m.Update(stagingKeyMsg("j"))
	m = updated.(Model)
	after, ok := m.homePanel.Selected()
	if !ok || after.Path == before.Path {
		t.Fatal("home navigation stopped while the plan scan was running")
	}

	delete(m.staging, first)
	m.stagingChanged()
	m.staging[second] = "second"
	m.stagingChanged()
	<-cancelled
	stale := <-firstResult
	updated, next := m.Update(stale)
	m = updated.(Model)
	if next == nil {
		t.Fatal("canceled scan did not start the newest pending scan")
	}
	result := next()
	updated, stop := m.Update(result)
	m = updated.(Model)
	if stop != nil {
		stop()
	}

	if calls != 2 {
		t.Fatalf("scan calls = %d, want exactly the canceled scan and newest scan", calls)
	}
	if len(m.staging) != 1 || m.staging[second] != "second" {
		t.Fatalf("staging = %v, want only the newest entry", m.staging)
	}
	if m.planActive || !m.planReady || strings.Contains(m.staged.View(), "Scanning…") {
		t.Fatalf("plan active=%v ready=%v staged view=%q, want completed scan", m.planActive, m.planReady, m.staged.View())
	}
}

func TestApplyDoesNotRunUntilTheRevalidationCompletes(t *testing.T) {
	model, paths := newStagingModel(t)
	m := model.(Model)
	path := filepath.Join(paths.Target, ".bar")
	m.staging[path] = "misc"
	m.planVersion = 1
	m.planReady = true
	m.plan = runnableTestPlan(paths, path, "misc")
	started := make(chan struct{}, 1)
	release := make(chan struct{})
	m.buildAdoptPlan = func(ctx context.Context, scanPaths config.Paths, staging dotfiles.Staging) dotfiles.AdoptPlan {
		started <- struct{}{}
		select {
		case <-release:
			return runnableTestPlan(scanPaths, path, "misc")
		case <-ctx.Done():
			return dotfiles.AdoptPlan{Paths: scanPaths, Fatal: ctx.Err()}
		}
	}

	updated, cmd := m.apply()
	m = updated.(Model)
	result := make(chan tea.Msg, 1)
	go func() { result <- planCommand(t, cmd)() }()
	<-started
	if m.popup == popupConfirm || !m.planActive {
		t.Fatal("Apply opened confirmation before revalidation completed")
	}
	close(release)
	updated, stop := m.Update(<-result)
	m = updated.(Model)
	if stop != nil {
		stop()
	}
	if m.popup != popupConfirm {
		t.Fatal("Apply did not open confirmation after current revalidation completed")
	}
}

func TestStagingChangeInvalidatesPendingApply(t *testing.T) {
	model, paths := newStagingModel(t)
	m := model.(Model)
	path := filepath.Join(paths.Target, ".bar")
	extra := filepath.Join(paths.Target, ".config", "foo")
	m.staging[path] = "misc"
	m.planVersion = 1
	m.planReady = true
	m.plan = runnableTestPlan(paths, path, "misc")
	started := make(chan struct{}, 1)
	m.buildAdoptPlan = func(ctx context.Context, scanPaths config.Paths, staging dotfiles.Staging) dotfiles.AdoptPlan {
		started <- struct{}{}
		<-ctx.Done()
		return dotfiles.AdoptPlan{Paths: scanPaths, Fatal: ctx.Err()}
	}

	updated, cmd := m.apply()
	m = updated.(Model)
	result := make(chan tea.Msg, 1)
	go func() { result <- planCommand(t, cmd)() }()
	<-started
	m.staging[extra] = "config"
	m.stagingChanged()
	updated, next := m.Update(<-result)
	m = updated.(Model)
	if next == nil {
		t.Fatal("staging change did not schedule a replacement scan")
	}
	if m.popup == popupConfirm || m.applyPending {
		t.Fatal("staging change left a stale Apply intent")
	}
}

func runnableTestPlan(paths config.Paths, path, pkg string) dotfiles.AdoptPlan {
	return dotfiles.AdoptPlan{
		Paths: paths,
		Packages: []dotfiles.PackageAdopt{{
			Package: pkg,
			Moves:   []dotfiles.Move{{From: path, To: filepath.Join(paths.Dotfiles, pkg, "dot-bar")}},
		}},
	}
}
