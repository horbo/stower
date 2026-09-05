package mainpanel

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/horbo/stower/internal/config"
	"github.com/horbo/stower/internal/dotfiles"
	"github.com/horbo/stower/internal/tui/styles"
)

func TestLogRollbackOrder(t *testing.T) {
	l := NewLog(config.Paths{Dotfiles: "/fake/dotfiles"}, "/fake", styles.Default())
	l.SetSize(100, 20)
	l.Start("apply")
	failure := errors.New("conflict")
	for _, e := range []dotfiles.Event{
		{Kind: dotfiles.StepStarted, Message: "mv /fake/.bar /fake/dotfiles/misc/dot-bar"},
		{Kind: dotfiles.StepDone, Message: "mv /fake/.bar /fake/dotfiles/misc/dot-bar"},
		{Kind: dotfiles.StepStarted, Message: "stow dry run"},
		{Kind: dotfiles.OutputLine, Message: "WARNING conflict"},
		{Kind: dotfiles.StepFailed, Message: "stow dry run", Err: failure},
		{Kind: dotfiles.Rollback, Message: "mv /fake/dotfiles/misc/dot-bar /fake/.bar"},
		{Kind: dotfiles.PackageFailed, Err: failure},
	} {
		e.Package = "misc"
		l.Append(e)
	}
	l.Finish(dotfiles.Summary{Failed: []dotfiles.PackageFailure{{Package: "misc", Err: failure}}}, true)
	want := []string{"misc", "✔ mv ~/.bar misc/dot-bar", "✘ stow dry run: conflict", "    WARNING conflict", "↩ mv misc/dot-bar ~/.bar", "✘ misc failed: conflict", "", "✘ cancelled", "✘ misc failed: conflict"}
	if !reflect.DeepEqual(l.Lines(), want) {
		t.Fatalf("lines = %#v", l.Lines())
	}
	if !strings.Contains(l.View(), "↩") {
		t.Fatal("rollback absent from rendered log")
	}
}

func TestLogScrollingAndSummary(t *testing.T) {
	l := NewLog(config.Paths{}, "", styles.Default())
	l.SetSize(80, 3)
	l.Start("apply")
	for i := 0; i < 30; i++ {
		l.Append(dotfiles.Event{Kind: dotfiles.OutputLine, Message: "output"})
	}
	bottom := l.vp.YOffset()
	l.Update(tea.KeyPressMsg{Code: tea.KeyUp})
	if l.vp.YOffset() != bottom {
		t.Fatal("running log scrolled manually")
	}
	l.Finish(dotfiles.Summary{Succeeded: []string{"misc"}, Skipped: []string{"blocked"}}, false)
	bottom = l.vp.YOffset()
	l.Update(tea.KeyPressMsg{Code: tea.KeyUp})
	if l.vp.YOffset() >= bottom {
		t.Fatal("finished log did not scroll")
	}
	if l.Counter() != "1 ok" {
		t.Fatal(l.Counter())
	}
	l.Start("again")
	if len(l.Lines()) != 0 || !l.Running() {
		t.Fatal("new run retained previous log")
	}
}
