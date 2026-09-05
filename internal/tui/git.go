package tui

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/horbo/stower/internal/config"
	"github.com/horbo/stower/internal/gitx"
	"github.com/horbo/stower/internal/tui/popups"
)

const gitIgnoreContent = ".DS_Store\n"

type gitState struct {
	available bool
	repo      bool
	dirty     map[string]int
}

func inspectGit(paths config.Paths) gitState {
	state := gitState{available: gitx.Available(), dirty: map[string]int{}}
	if !state.available || !gitx.IsRepo(paths.Dotfiles) {
		return state
	}
	state.repo = true
	lines, err := gitx.Porcelain(paths.Dotfiles)
	if err != nil {
		return state
	}
	for _, line := range lines {
		if name := topLevel(gitx.StatusPath(line)); name != "" {
			state.dirty[name]++
		}
	}
	return state
}

func topLevel(path string) string {
	path = strings.TrimSuffix(filepath.ToSlash(path), "/")
	if i := strings.Index(path, "/"); i >= 0 {
		return path[:i]
	}
	return path
}

func (g gitState) dirtyNames() []string {
	names := make([]string, 0, len(g.dirty))
	for name := range g.dirty {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

type commitPreparedMsg struct {
	packages []string
	subject  string
	lines    []string
	manual   bool
	err      error
}

type commitDoneMsg struct {
	subject string
	err     error
}

type firstRunDoneMsg struct {
	err error
}

func prepareCommitCmd(dir string, packages []string, change gitx.Change, manual bool) tea.Cmd {
	return func() tea.Msg {
		lines, err := gitx.Porcelain(dir, packages...)
		if err != nil {
			return commitPreparedMsg{manual: manual, err: err}
		}
		change.Files = len(lines)
		return commitPreparedMsg{packages: packages, subject: gitx.Subject(change), lines: lines, manual: manual}
	}
}

func commitCmd(dir string, packages []string, subject string) tea.Cmd {
	return func() tea.Msg {
		return commitDoneMsg{subject: subject, err: gitx.AddAndCommit(dir, packages, subject)}
	}
}

func firstRunCmd(dir string, msg popups.FirstRunAppliedMsg) tea.Cmd {
	return func() tea.Msg {
		if msg.Create {
			if err := os.MkdirAll(dir, 0o755); err != nil {
				return firstRunDoneMsg{err: err}
			}
		}
		if msg.GitInit {
			if err := gitx.Init(dir); err != nil {
				return firstRunDoneMsg{err: err}
			}
		}
		if msg.GitIgnore {
			path := filepath.Join(dir, ".gitignore")
			if _, err := os.Stat(path); errors.Is(err, fs.ErrNotExist) {
				if err := os.WriteFile(path, []byte(gitIgnoreContent), 0o644); err != nil {
					return firstRunDoneMsg{err: err}
				}
			}
		}
		return firstRunDoneMsg{}
	}
}

func (m *Model) openFirstRun() {
	_, err := os.Stat(m.paths.Dotfiles)
	if errors.Is(err, fs.ErrNotExist) {
		m.firstRunPopup.OpenMissing(m.paths.Dotfiles, m.git.available)
		m.popup = popupFirstRun
		return
	}
	if err != nil {
		return
	}
	if m.git.available && !m.git.repo && !m.gitDeclined {
		m.firstRunPopup.OpenOffer(m.paths.Dotfiles)
		m.popup = popupFirstRun
	}
}

func (m *Model) applyFirstRun(msg popups.FirstRunAppliedMsg) tea.Cmd {
	m.popup = popupNone
	if !msg.GitInit {
		m.gitDeclined = true
	}
	m.relayout()
	return firstRunCmd(m.paths.Dotfiles, msg)
}

func (m *Model) cancelFirstRun(msg popups.FirstRunCancelledMsg) tea.Cmd {
	m.popup = popupNone
	m.relayout()
	if !msg.Offer {
		return tea.Quit
	}
	m.gitDeclined = true
	return nil
}

func (m *Model) openCommit() tea.Cmd {
	if !m.git.available {
		return m.setFlash("git is not installed")
	}
	if !m.git.repo {
		return m.setFlash(m.display(m.paths.Dotfiles) + " is not a git repository")
	}
	names := m.git.dirtyNames()
	if len(names) == 0 {
		return m.setFlash("nothing to commit")
	}
	change := gitx.Change{Operation: gitx.Update, Packages: names}
	return prepareCommitCmd(m.paths.Dotfiles, names, change, true)
}

func (m *Model) commitPrepared(msg commitPreparedMsg) tea.Cmd {
	if msg.err != nil {
		if msg.manual {
			m.showError("Cannot read the git status", msg.err)
			return nil
		}
		return m.setFlash("git: " + msg.err.Error())
	}
	if len(msg.lines) == 0 {
		if msg.manual {
			return m.setFlash("nothing to commit")
		}
		return nil
	}
	m.commitPopup.Open(msg.packages, msg.lines, msg.subject)
	m.popup = popupCommit
	m.relayout()
	return nil
}

func (m *Model) commitFinished(msg commitDoneMsg) tea.Cmd {
	if msg.err != nil {
		if errors.Is(msg.err, gitx.ErrNothingToCommit) {
			return m.setFlash("nothing to commit")
		}
		m.showError("Cannot commit", msg.err)
		return nil
	}
	return tea.Batch(m.setFlash("committed: "+msg.subject), refreshCmd(m.paths))
}

func (m Model) commitChange(title string, succeeded []string) (gitx.Change, bool) {
	switch title {
	case actionApply:
		return gitx.Change{Operation: gitx.Add, Packages: succeeded}, true
	case actionRestore:
		return gitx.Change{Operation: gitx.Remove, Packages: succeeded}, true
	case actionFix:
		entry := m.fixIssue.Package
		if base := filepath.Base(m.fixIssue.Entry.PkgRel); base != "." && base != string(filepath.Separator) {
			entry += "/" + base
		}
		return gitx.Change{Operation: gitx.Fix, Entry: entry, Packages: succeeded}, true
	case actionRestow:
		return gitx.Change{Operation: gitx.Restow, Packages: succeeded}, true
	}
	return gitx.Change{}, false
}

func (m *Model) offerCommit(title string, succeeded []string) tea.Cmd {
	if !m.git.available || !m.git.repo || len(succeeded) == 0 {
		return nil
	}
	change, ok := m.commitChange(title, succeeded)
	if !ok {
		return nil
	}
	return prepareCommitCmd(m.paths.Dotfiles, succeeded, change, false)
}
