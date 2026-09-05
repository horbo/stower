package tui

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/horbo/stower/internal/dotfiles"
	"github.com/horbo/stower/internal/stow"
	"github.com/horbo/stower/internal/tui/popups"
)

func prepareApply(t *testing.T) Model {
	t.Helper()
	model, paths := newStagingModel(t)
	m := model.(Model)
	m.stage(filepath.Join(paths.Target, ".bar"), "misc")
	m.focus = Staged
	updated, _ := m.apply()
	m = updated.(Model)
	if m.popup != popupConfirm || !strings.Contains(m.View().Content, "Apply staged plan") {
		t.Fatal("confirmation not rendered")
	}
	return m
}

func runApply(t *testing.T, m Model) Model {
	t.Helper()
	updated, cmd := m.Update(popups.ConfirmedMsg{Action: actionApply})
	m = updated.(Model)
	batch := cmd().(tea.BatchMsg)
	messages := make(chan tea.Msg, 1)
	go func() { messages <- batch[0]() }()
	for {
		select {
		case msg := <-messages:
			updated, next := m.Update(msg)
			m = updated.(Model)
			if m.exec == nil {
				if next != nil {
					updated, _ = m.Update(next())
					m = updated.(Model)
				}
				return m
			}
			go func() { messages <- next() }()
		case <-time.After(10 * time.Second):
			t.Fatal("execution did not finish")
		}
	}
}

func TestApplyRealStowAndRefresh(t *testing.T) {
	if _, err := exec.LookPath("stow"); err != nil {
		t.Fatal(err)
	}
	m := prepareApply(t)
	m.stage(filepath.Join(m.paths.Target, ".config", "foo"), "foo")
	updated, _ := m.apply()
	m = updated.(Model)
	m = runApply(t, m)
	if len(m.staging) != 0 || m.status.Running() {
		t.Fatal("operation did not clear staging and running state")
	}
	for path, pkg := range map[string]string{".bar": "misc", ".config/foo": "foo"} {
		owner, managed := dotfiles.ManagedBy(m.paths, filepath.Join(m.paths.Target, path))
		if !managed || owner != pkg {
			t.Fatalf("%s owner=%s managed=%v", path, owner, managed)
		}
	}
	for _, pkg := range packageItems(m.pkgs, m.git) {
		if (pkg.Name == "misc" || pkg.Name == "foo") && (!pkg.Linked || pkg.Failed) {
			t.Fatalf("package not healthy: %+v", pkg)
		}
	}
	m.focus = Home
	m.mainFocused = false
	m.closeLog()
	m.homePanel.Update(stagingKeyMsg("g"))
	if !strings.Contains(m.homePanel.View(), "[misc]") {
		t.Fatal("managed badge not refreshed")
	}
}

func TestApplyDestinationRaceKeepsSource(t *testing.T) {
	m := prepareApply(t)
	mkdir(t, filepath.Join(m.paths.Dotfiles, "misc"))
	write(t, filepath.Join(m.paths.Dotfiles, "misc", "dot-bar"), "existing")
	m = runApply(t, m)
	data, err := os.ReadFile(filepath.Join(m.paths.Target, ".bar"))
	if err != nil || string(data) != "x\n" {
		t.Fatalf("source changed: %q %v", data, err)
	}
	if len(m.staging) != 1 || !strings.Contains(m.failures["misc"], "destination already exists") {
		t.Fatalf("failure not staged: %v", m.failures)
	}
}

func TestApplyKeepsBlockedEntryInSuccessfulPackage(t *testing.T) {
	m := prepareApply(t)
	path := filepath.Join(m.paths.Target, ".other")
	write(t, path, "keep")
	mkdir(t, filepath.Join(m.paths.Dotfiles, "misc"))
	write(t, filepath.Join(m.paths.Dotfiles, "misc", "dot-other"), "existing")
	m.stage(path, "misc")
	m.runner = &applyRunner{}
	updated, _ := m.apply()
	m = updated.(Model)
	m = runApply(t, m)
	if len(m.staging) != 1 || m.staging[path] != "misc" {
		t.Fatalf("blocked entry lost: %v", m.staging)
	}
}

type applyRunner struct{ cancel context.CancelFunc }

func (r *applyRunner) DryRunRestow(pkg string) stow.Result {
	if r.cancel != nil {
		r.cancel()
	}
	return stow.Result{}
}
func (r *applyRunner) Restow(pkgs ...string) stow.Result { return stow.Result{} }
func (r *applyRunner) Unstow(pkg string) stow.Result     { return stow.Result{} }

func TestApplyCancellationRollsBackBeforeDone(t *testing.T) {
	m := prepareApply(t)
	runner := &applyRunner{}
	m.runner = runner
	updated, cmd := m.Update(popups.ConfirmedMsg{Action: actionApply})
	m = updated.(Model)
	runner.cancel = m.exec.cancel
	for _, k := range []string{"q", "esc", "2", "enter"} {
		updated, ignored := m.Update(keyMsg(k))
		m = updated.(Model)
		if ignored != nil || m.focus != Staged || !m.logOpen {
			t.Fatal("key handled while running")
		}
	}
	batch := cmd().(tea.BatchMsg)
	msg := batch[0]()
	for {
		if _, done := msg.(execDoneMsg); done {
			if data, err := os.ReadFile(filepath.Join(m.paths.Target, ".bar")); err != nil || string(data) != "x\n" {
				t.Fatal("done arrived before rollback")
			}
		}
		updated, next := m.Update(msg)
		m = updated.(Model)
		if m.exec == nil {
			break
		}
		msg = next()
	}
	lines := strings.Join(m.mainLog.Lines(), "\n")
	if !strings.Contains(lines, "↩ mv") || !strings.Contains(lines, "context canceled") || len(m.staging) != 1 {
		t.Fatalf("missing cancellation rollback: %s", lines)
	}
}

func TestRefreshHundredsOfEntries(t *testing.T) {
	model, paths := newStagingModel(t)
	for i := 0; i < 500; i++ {
		write(t, filepath.Join(paths.Target, fmt.Sprintf(".entry-%03d", i)), "x")
	}
	started := time.Now()
	msg := refreshCmd(paths)()
	read := time.Since(started)
	started = time.Now()
	model, _ = model.Update(msg)
	t.Logf("500-entry refresh: package scan=%s, UI refresh=%s", read, time.Since(started))
	_ = model
}

func TestCtrlCCancelsWithoutQuitting(t *testing.T) {
	m := prepareApply(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	m.exec = &execution{cancel: cancel}
	m.mainLog.Start("apply")
	updated, cmd := m.Update(tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl})
	m = updated.(Model)
	if cmd != nil || !m.exec.cancelled || ctx.Err() == nil {
		t.Fatal("ctrl+c did not cancel operation")
	}
	if !m.mainLog.Running() {
		t.Fatal("log stopped before rollback completed")
	}
}

func TestApplyConflictRollsBackOnlyFailedPackage(t *testing.T) {
	m := prepareApply(t)
	m.stage(filepath.Join(m.paths.Target, ".config", "foo"), "foo")
	mkdir(t, filepath.Join(m.paths.Dotfiles, "misc"))
	write(t, filepath.Join(m.paths.Dotfiles, "misc", "dot-conflict"), "repository")
	write(t, filepath.Join(m.paths.Target, ".conflict"), "target")
	updated, _ := m.apply()
	m = updated.(Model)
	m = runApply(t, m)
	if len(m.staging) != 1 || m.staging[filepath.Join(m.paths.Target, ".bar")] != "misc" {
		t.Fatalf("staging = %v", m.staging)
	}
	if data, err := os.ReadFile(filepath.Join(m.paths.Target, ".bar")); err != nil || string(data) != "x\n" {
		t.Fatal("failed move not restored")
	}
	if _, managed := dotfiles.ManagedBy(m.paths, filepath.Join(m.paths.Target, ".config", "foo")); !managed {
		t.Fatal("successful package undone")
	}
	lines := strings.Join(m.mainLog.Lines(), "\n")
	if !strings.Contains(lines, "↩ mv") || !strings.Contains(lines, "misc failed") || !strings.Contains(lines, "foo done") {
		t.Fatal(lines)
	}
}

func TestConfirmCancelAndErrorPopup(t *testing.T) {
	m := prepareApply(t)
	updated, cmd := m.Update(keyMsg("esc"))
	updated, _ = updated.Update(cmd())
	m = updated.(Model)
	if m.popup != popupNone || m.exec != nil {
		t.Fatal("cancel executed plan")
	}
	m.showError("Cannot apply", fmt.Errorf("device mismatch"))
	if !strings.Contains(m.View().Content, "device mismatch") {
		t.Fatal("error popup missing")
	}
	updated, cmd = m.Update(keyMsg("enter"))
	updated, _ = updated.Update(cmd())
	if updated.(Model).popup != popupNone {
		t.Fatal("error popup did not close")
	}
}
