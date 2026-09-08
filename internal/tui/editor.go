package tui

import (
	"errors"
	"os"
	"os/exec"
	"strconv"

	tea "charm.land/bubbletea/v2"

	"github.com/horbo/stower/internal/config"
)

type editorFinishedMsg struct {
	err error
}

type editorTargets struct {
	repo   string
	target string
}

func execEditorProcess(argv []string, path string) tea.Cmd {
	args := make([]string, 0, len(argv))
	args = append(args, argv[1:]...)
	args = append(args, path)
	command := exec.Command(argv[0], args...)
	return tea.ExecProcess(command, func(err error) tea.Msg {
		return editorFinishedMsg{err: err}
	})
}

func (m Model) editorTargets() (editorTargets, bool) {
	if m.logOpen || m.diffOpen || m.restoreOpen {
		return editorTargets{}, false
	}
	switch {
	case m.focus == Home:
		node, ok := m.homePanel.Selected()
		if !ok || node.Path == "" {
			return editorTargets{}, false
		}
		return editorTargets{target: node.Path}, true
	case m.focus == Issues || (m.focus == Packages && m.mainFocused):
		issue, ok := m.selectedIssue()
		if !ok {
			return editorTargets{}, false
		}
		return editorTargets{
			repo:   issue.Entry.PackagePath(m.paths, issue.Package),
			target: issue.Entry.TargetPath(m.paths),
		}, true
	}
	return editorTargets{}, false
}

func (m *Model) openInEditor(wantTarget bool) tea.Cmd {
	targets, ok := m.editorTargets()
	if !ok {
		return nil
	}
	path := targets.repo
	if wantTarget || path == "" {
		path = targets.target
	}
	argv, err := config.Editor(os.Getenv)
	if err != nil {
		return m.setFlash(err.Error())
	}
	return m.execEditor(argv, path)
}

func (m Model) handleEditorFinished(msg editorFinishedMsg) (tea.Model, tea.Cmd) {
	if msg.err == nil {
		return m, refreshCmd(m.paths)
	}
	text := "editor failed: " + msg.err.Error()
	var exitErr *exec.ExitError
	if errors.As(msg.err, &exitErr) {
		text = "editor exited with code " + strconv.Itoa(exitErr.ExitCode())
	}
	return m, tea.Batch(m.setFlash(text), refreshCmd(m.paths))
}
