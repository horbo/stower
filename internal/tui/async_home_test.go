package tui

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/horbo/stower/internal/config"
	"github.com/horbo/stower/internal/dotfiles"
	"github.com/horbo/stower/internal/tui/components"
	mainpanel "github.com/horbo/stower/internal/tui/main"
)

type previewCall struct {
	ctx     context.Context
	path    string
	release chan struct{}
}

type previewGate struct {
	calls chan *previewCall
}

func newPreviewGate() *previewGate {
	return &previewGate{calls: make(chan *previewCall, 8)}
}

func (g *previewGate) inspect() func(context.Context, string) mainpanel.EntryFacts {
	return func(ctx context.Context, path string) mainpanel.EntryFacts {
		call := &previewCall{ctx: ctx, path: path, release: make(chan struct{})}
		g.calls <- call
		select {
		case <-call.release:
		case <-ctx.Done():
		}
		return mainpanel.InspectContext(ctx, path)
	}
}

func (g *previewGate) next(t *testing.T) *previewCall {
	t.Helper()
	select {
	case call := <-g.calls:
		return call
	case <-time.After(2 * time.Second):
		t.Fatal("inspectHomeEntry was not called")
		return nil
	}
}

func (g *previewGate) allow(call *previewCall) {
	close(call.release)
}

func runTeaCmd(cmd tea.Cmd, out chan<- tea.Msg) {
	if cmd == nil {
		return
	}
	msg := cmd()
	if batch, ok := msg.(tea.BatchMsg); ok {
		for _, leaf := range batch {
			if leaf == nil {
				continue
			}
			go runTeaCmd(leaf, out)
		}
		return
	}
	out <- msg
}

func nextHomeEntryLoaded(t *testing.T, ch chan tea.Msg) homeEntryLoadedMsg {
	t.Helper()
	for {
		select {
		case msg := <-ch:
			if loaded, ok := msg.(homeEntryLoadedMsg); ok {
				return loaded
			}
		case <-time.After(2 * time.Second):
			t.Fatal("no homeEntryLoadedMsg arrived")
			return homeEntryLoadedMsg{}
		}
	}
}

func TestHomePreviewOneActiveOnePendingStaleIgnored(t *testing.T) {
	model, _ := newStagingModel(t)
	model = send(t, model, "2")
	m := model.(Model)
	g := newPreviewGate()
	m.inspectHomeEntry = g.inspect()

	out := make(chan tea.Msg, 32)

	beforePath := cursorPath(t, m)
	updated, cmd := m.Update(stagingKeyMsg("j"))
	m = updated.(Model)
	go runTeaCmd(cmd, out)
	firstPath := cursorPath(t, m)
	if firstPath == beforePath {
		t.Fatal("j did not move the cursor")
	}
	firstCall := g.next(t)
	if firstCall.path != firstPath {
		t.Fatalf("the preview started for %q, want %q", firstCall.path, firstPath)
	}
	if !m.previewActive {
		t.Fatal("moving the cursor did not start an active preview")
	}

	updated, cmd = m.Update(stagingKeyMsg("j"))
	m = updated.(Model)
	go runTeaCmd(cmd, out)
	secondPath := cursorPath(t, m)
	if secondPath == firstPath {
		t.Fatal("the second j did not move the cursor")
	}

	if !m.previewActive {
		t.Fatal("want the first preview to still be active")
	}
	if m.previewPending == nil || m.previewPending.path != secondPath {
		t.Fatalf("want a pending preview for %q, got %+v", secondPath, m.previewPending)
	}

	staleLoaded := nextHomeEntryLoaded(t, out)
	if staleLoaded.path != firstPath {
		t.Fatalf("the first completed preview path = %q, want %q", staleLoaded.path, firstPath)
	}
	updated, cmd = m.Update(staleLoaded)
	m = updated.(Model)
	go runTeaCmd(cmd, out)

	if !strings.Contains(ansi.Strip(m.mainHome.View()), "Loading") {
		t.Fatal("the stale preview result populated the current entry")
	}

	secondCall := g.next(t)
	if secondCall.path != secondPath {
		t.Fatalf("the second preview started for %q, want %q", secondCall.path, secondPath)
	}
	g.allow(secondCall)

	current := nextHomeEntryLoaded(t, out)
	if current.path != secondPath {
		t.Fatalf("the second completed preview path = %q, want %q", current.path, secondPath)
	}
	updated, _ = m.Update(current)
	m = updated.(Model)

	if strings.Contains(ansi.Strip(m.mainHome.View()), "Loading") {
		t.Fatal("the current preview result did not clear the loading state")
	}
}

func TestHomeCursorMoveDoesNotSynchronouslyScanOrInspect(t *testing.T) {
	model, paths := newStagingModel(t)
	model = send(t, model, "2")
	model = moveTo(t, model, filepath.Join(paths.Target, ".config"))
	model = send(t, model, "right")
	m := model.(Model)

	var planCalls, inspectCalls int32
	realPlan := m.buildAdoptPlan
	m.buildAdoptPlan = func(ctx context.Context, scanPaths config.Paths, staging dotfiles.Staging) dotfiles.AdoptPlan {
		atomic.AddInt32(&planCalls, 1)
		return realPlan(ctx, scanPaths, staging)
	}
	realInspect := m.inspectHomeEntry
	m.inspectHomeEntry = func(ctx context.Context, path string) mainpanel.EntryFacts {
		atomic.AddInt32(&inspectCalls, 1)
		return realInspect(ctx, path)
	}

	updated, cmd := m.Update(stagingKeyMsg("j"))
	m = updated.(Model)
	if atomic.LoadInt32(&planCalls) != 0 || atomic.LoadInt32(&inspectCalls) != 0 {
		t.Fatalf("cursor move called plan=%d inspect=%d synchronously, want 0", planCalls, inspectCalls)
	}

	out := make(chan tea.Msg, 32)
	go runTeaCmd(cmd, out)
	deadline := time.After(500 * time.Millisecond)
drain:
	for {
		select {
		case msg := <-out:
			if _, ok := msg.(components.TreeLoadedMsg); ok {
				t.Fatal("cursor move triggered a tree load")
			}
		case <-deadline:
			break drain
		}
	}
}

func TestManagedBadgeSurvivesCursorMovesAndUnrelatedStagingChange(t *testing.T) {
	model, paths := newStagingModel(t)
	model = send(t, model, "2")

	model = moveTo(t, model, filepath.Join(paths.Target, ".bar"))
	model = send(t, model, "space")
	model = typeName(t, model, "misc")

	for i := 0; i < 5; i++ {
		model = send(t, model, "j")
		model = send(t, model, "k")
	}

	model = moveTo(t, model, filepath.Join(paths.Target, ".zshrc"))
	if !strings.Contains(ansi.Strip(model.View().Content), "[zsh]") {
		t.Fatal("the managed badge disappeared after cursor moves and an unrelated staging change")
	}
}

func TestStagedPanelShowsBlockedNestedGitScanErrorAndRecovers(t *testing.T) {
	model, paths := newStagingModel(t)
	root := filepath.Join(paths.Target, ".private")
	blocked := filepath.Join(root, "blocked")
	mkdir(t, blocked)
	write(t, filepath.Join(blocked, "file"), "x")
	if err := os.Chmod(blocked, 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(blocked, 0o755) })
	if _, err := os.ReadDir(blocked); err == nil {
		t.Skip("the test process can read mode-000 directories")
	}

	updated, cmd := model.Update(tea.KeyPressMsg{Code: 'r', Mod: tea.ModCtrl})
	model = deliverAll(t, updated, cmd)

	model = send(t, model, "2")
	model = moveTo(t, model, root)
	model = send(t, model, "space")
	model = typeName(t, model, "private")

	m := model.(Model)
	if reason := m.blocked[root]; reason == "" {
		t.Fatalf("the plan did not block %s: blocked=%v", root, m.blocked)
	}
	if m.plan.Runnable() {
		t.Fatal("a plan whose only entry is blocked must not be runnable")
	}

	model = send(t, model, "3")
	plain := ansi.Strip(model.View().Content)
	if !strings.Contains(plain, "✘") {
		t.Fatalf("the staged panel does not show the blocked reason:\n%s", plain)
	}

	found := false
	for i := 0; i < 10; i++ {
		m = model.(Model)
		if row, ok := m.staged.Selected(); ok && row.Path == root {
			found = true
			break
		}
		model = send(t, model, "j")
	}
	if !found {
		t.Fatal("the cursor cannot reach the blocked path row")
	}

	model = send(t, model, "u")
	m = model.(Model)
	if len(m.staging) != 0 {
		t.Fatalf("u did not unstage the blocked entry: %v", m.staging)
	}

	model = stageAndWait(t, m, root, "private")
	m = model.(Model)
	if reason := m.blocked[root]; reason == "" {
		t.Fatal("re-staging the blocked entry did not reblock it")
	}

	if err := os.Chmod(blocked, 0o755); err != nil {
		t.Fatal(err)
	}
	refreshed, refreshedCmd := m.Update(tea.KeyPressMsg{Code: 'r', Mod: tea.ModCtrl})
	m = deliverAll(t, refreshed, refreshedCmd).(Model)
	if reason, ok := m.blocked[root]; ok {
		t.Fatalf("blocked did not clear after chmod and refresh: %q", reason)
	}
}

func TestRenamePackageInvalidatesInFlightScanAndReachesReady(t *testing.T) {
	model, paths := newStagingModel(t)
	m := model.(Model)
	path := filepath.Join(paths.Target, ".bar")
	started := make(chan struct{}, 1)
	cancelled := make(chan struct{}, 1)
	calls := 0
	m.buildAdoptPlan = func(ctx context.Context, scanPaths config.Paths, staging dotfiles.Staging) dotfiles.AdoptPlan {
		calls++
		if staging[path] == "misc" {
			started <- struct{}{}
			<-ctx.Done()
			cancelled <- struct{}{}
			return dotfiles.AdoptPlan{Paths: scanPaths, Fatal: ctx.Err()}
		}
		return dotfiles.AdoptPlan{Paths: scanPaths}
	}

	m.staging[path] = "misc"
	firstCmd := planCommand(t, m.stagingChanged())
	firstVersion := m.planVersion
	firstResult := make(chan tea.Msg, 1)
	go func() { firstResult <- firstCmd() }()
	<-started
	if m.planReady {
		t.Fatal("the plan became ready before the scan finished")
	}

	m.staging[path] = "tools"
	m.stagingChanged()
	if m.planVersion <= firstVersion {
		t.Fatalf("planVersion = %d, want greater than %d after the rename", m.planVersion, firstVersion)
	}
	if m.planReady {
		t.Fatal("the plan is ready while the rename scan is still pending")
	}
	<-cancelled
	stale := <-firstResult
	updated, next := m.Update(stale)
	m = updated.(Model)
	if m.planReady {
		t.Fatal("the superseded scan must not mark the plan ready")
	}
	if next == nil {
		t.Fatal("the superseded scan did not start the pending rename scan")
	}

	result := next()
	updated, stop := m.Update(result)
	m = updated.(Model)
	if stop != nil {
		stop()
	}
	if !m.planReady {
		t.Fatal("the plan did not become ready after the rename scan's planBuiltMsg")
	}
	if got := m.staging[path]; got != "tools" {
		t.Fatalf("staging = %v, want tools", m.staging)
	}
	if calls != 2 {
		t.Fatalf("scan calls = %d, want the superseded scan plus the rename scan", calls)
	}
}

func TestStagingADirectoryDropReplacementInvalidatesInFlightScanAndReachesReady(t *testing.T) {
	model, paths := newStagingModel(t)
	m := model.(Model)
	child := filepath.Join(paths.Target, ".config", "foo")
	parent := filepath.Join(paths.Target, ".config")
	started := make(chan struct{}, 1)
	cancelled := make(chan struct{}, 1)
	calls := 0
	m.buildAdoptPlan = func(ctx context.Context, scanPaths config.Paths, staging dotfiles.Staging) dotfiles.AdoptPlan {
		calls++
		if _, ok := staging[child]; ok {
			started <- struct{}{}
			<-ctx.Done()
			cancelled <- struct{}{}
			return dotfiles.AdoptPlan{Paths: scanPaths, Fatal: ctx.Err()}
		}
		return dotfiles.AdoptPlan{Paths: scanPaths}
	}

	m.staging[child] = "foo"
	firstCmd := planCommand(t, m.stagingChanged())
	firstVersion := m.planVersion
	firstResult := make(chan tea.Msg, 1)
	go func() { firstResult <- firstCmd() }()
	<-started
	if m.planReady {
		t.Fatal("the plan became ready before the scan finished")
	}

	delete(m.staging, child)
	m.staging[parent] = "cfg"
	m.stagingChanged()
	if m.planVersion <= firstVersion {
		t.Fatalf("planVersion = %d, want greater than %d after replacing the descendant", m.planVersion, firstVersion)
	}
	if m.planReady {
		t.Fatal("the plan is ready while the replacement scan is still pending")
	}
	<-cancelled
	stale := <-firstResult
	updated, next := m.Update(stale)
	m = updated.(Model)
	if m.planReady {
		t.Fatal("the superseded scan must not mark the plan ready")
	}
	if next == nil {
		t.Fatal("the superseded scan did not start the pending replacement scan")
	}

	result := next()
	updated, stop := m.Update(result)
	m = updated.(Model)
	if stop != nil {
		stop()
	}
	if !m.planReady {
		t.Fatal("the plan did not become ready after the replacement scan's planBuiltMsg")
	}
	if len(m.staging) != 1 || m.staging[parent] != "cfg" {
		t.Fatalf("staging = %v, want only the parent directory", m.staging)
	}
	if calls != 2 {
		t.Fatalf("scan calls = %d, want the superseded scan plus the replacement scan", calls)
	}
}
