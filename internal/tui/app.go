package tui

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/spinner"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/horbo/stower/internal/config"
	"github.com/horbo/stower/internal/doctor"
	"github.com/horbo/stower/internal/dotfiles"
	"github.com/horbo/stower/internal/stow"
	"github.com/horbo/stower/internal/tui/components"
	mainpanel "github.com/horbo/stower/internal/tui/main"
	"github.com/horbo/stower/internal/tui/panels"
	"github.com/horbo/stower/internal/tui/popups"
	"github.com/horbo/stower/internal/tui/styles"
)

const (
	popupMaxWidth = 70
	popupMargin   = 4
	flashTimeout  = 4 * time.Second
	eventBuffer   = 64
	actionApply   = "apply"
)

type packageInfo struct {
	name    string
	entries []dotfiles.Entry
	err     error
	report  doctor.PackageReport
}

type refreshedMsg struct {
	pkgs   []packageInfo
	issues []doctor.Issue
	git    gitState
	err    error
}

type flashExpiredMsg struct {
	id int
}

type planRequest struct {
	version int
	staging dotfiles.Staging
	choices map[string]dotfiles.RepositoryChoice
}

type planBuiltMsg struct {
	version int
	plan    dotfiles.AdoptPlan
	apply   bool
}

type homeEntryLoadedMsg struct {
	path       string
	generation int
	facts      mainpanel.EntryFacts
}

type previewRequest struct {
	path       string
	generation int
}

type execEventMsg struct {
	runID int
	event dotfiles.Event
}

type execDoneMsg struct {
	runID   int
	summary dotfiles.Summary
}

type execution struct {
	id        int
	cancel    context.CancelFunc
	events    chan tea.Msg
	cancelled bool
	plan      dotfiles.AdoptPlan
	title     string
}

type popupKind int

const (
	popupNone popupKind = iota
	popupKeys
	popupAssign
	popupConfirm
	popupError
	popupFix
	popupCommit
	popupFirstRun
	popupRepositories
)

type mainContext int

const (
	contextPackage mainContext = iota
	contextHomeEntry
	contextStagedPlan
	contextLog
	contextRestore
	contextIssue
	contextDiff
)

type keyMap struct {
	Quit         key.Binding
	ForceQuit    key.Binding
	Help         key.Binding
	NextPanel    key.Binding
	ModeNext     key.Binding
	ModePrev     key.Binding
	Refresh      key.Binding
	RestowAll    key.Binding
	Back         key.Binding
	Enter        key.Binding
	Panels       key.Binding
	Repositories key.Binding
	Open         key.Binding
	OpenTarget   key.Binding
	byPanel      [SidePanelCount]key.Binding
}

func defaultKeyMap() keyMap {
	m := keyMap{
		Quit:         key.NewBinding(key.WithKeys("q"), key.WithHelp("q", "quit")),
		ForceQuit:    key.NewBinding(key.WithKeys("ctrl+c"), key.WithHelp("ctrl+c", "quit")),
		Help:         key.NewBinding(key.WithKeys("?"), key.WithHelp("?", "keys")),
		NextPanel:    key.NewBinding(key.WithKeys("tab"), key.WithHelp("tab", "next panel")),
		ModeNext:     key.NewBinding(key.WithKeys("+"), key.WithHelp("+/-", "wider/narrower")),
		ModePrev:     key.NewBinding(key.WithKeys("_", "-"), key.WithHelp("_", "narrower")),
		Refresh:      key.NewBinding(key.WithKeys("ctrl+r"), key.WithHelp("ctrl+r", "rescan")),
		RestowAll:    key.NewBinding(key.WithKeys("R"), key.WithHelp("R", "restow all")),
		Back:         key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "back")),
		Enter:        key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "focus main")),
		Panels:       key.NewBinding(key.WithKeys("0", "1", "2", "3", "4"), key.WithHelp("0-4", "panels")),
		Repositories: key.NewBinding(key.WithKeys("x"), key.WithHelp("x", "Git repositories")),
		Open:         key.NewBinding(key.WithKeys("o"), key.WithHelp("o", "open in editor")),
		OpenTarget:   key.NewBinding(key.WithKeys("O"), key.WithHelp("O", "open target")),
	}
	for _, p := range sidePanels {
		m.byPanel[p] = key.NewBinding(key.WithKeys(string(rune('0' + int(p)))))
	}
	return m
}

type returnFocus struct {
	focus PanelID
	main  bool
	valid bool
}

type Model struct {
	paths       config.Paths
	home        string
	stowVersion string
	st          styles.Styles
	keys        keyMap

	width  int
	height int
	layout Layout

	focus       PanelID
	mainFocused bool
	mode        ScreenMode
	mouse       bool

	side           [SidePanelCount]Panel
	status         *panels.Status
	packages       *panels.Packages
	homePanel      *panels.Home
	staged         *panels.Staged
	issues         *panels.Issues
	mainPkg        *mainpanel.Package
	mainHome       *mainpanel.HomeEntry
	mainStaged     *mainpanel.StagedPlan
	mainLog        *mainpanel.Log
	mainRestore    *mainpanel.RestorePlan
	mainDetail     *mainpanel.Text
	mainDiff       *mainpanel.Text
	restoreOpen    bool
	diffOpen       bool
	diffID         int
	restorePlan    dotfiles.RestorePlan
	restoreEntries []string
	fixIssue       doctor.Issue
	fixAction      doctor.Action
	restowPackages []string

	keysPopup         *popups.Keys
	assignPopup       *popups.Assign
	confirmPopup      *popups.Confirm
	errorPopup        *popups.Error
	fixPopup          *popups.Fix
	commitPopup       *popups.Commit
	firstRunPopup     *popups.FirstRun
	repositoriesPopup *popups.Repositories
	popup             popupKind

	git         gitState
	gitDeclined bool

	staging                 dotfiles.Staging
	repositoryChoices       map[string]dotfiles.RepositoryChoice
	failures                map[string]string
	blocked                 map[string]string
	plan                    dotfiles.AdoptPlan
	planVersion             int
	planReady               bool
	planActive              bool
	planActiveVersion       int
	planCancel              context.CancelFunc
	planPending             *planRequest
	applyPending            bool
	buildAdoptPlan          func(context.Context, config.Paths, dotfiles.Staging) dotfiles.AdoptPlan
	inspectHomeEntry        func(context.Context, config.Paths, string) mainpanel.EntryFacts
	execEditor              func(argv []string, path string) tea.Cmd
	previewPath             string
	previewGeneration       int
	previewCancel           context.CancelFunc
	previewActive           bool
	previewActiveGeneration int
	previewPending          *previewRequest

	runner    dotfiles.Runner
	exec      *execution
	runID     int
	logOpen   bool
	logReturn returnFocus

	flash    string
	flashID  int
	renaming string

	pkgs    []packageInfo
	loadErr error
}

func New(paths config.Paths, stowVersion string) Model {
	st := styles.Default()
	home := os.Getenv("HOME")
	m := Model{
		paths:             paths,
		home:              home,
		stowVersion:       stowVersion,
		st:                st,
		keys:              defaultKeyMap(),
		focus:             Packages,
		mode:              ModeNormal,
		mouse:             true,
		status:            panels.NewStatus(paths, home, stowVersion, st),
		packages:          panels.NewPackages(st),
		homePanel:         panels.NewHome(paths, home, st),
		staged:            panels.NewStaged(paths, home, st),
		issues:            panels.NewIssues(st),
		mainPkg:           mainpanel.NewPackage(paths, home, st),
		mainHome:          mainpanel.NewHomeEntry(paths, home, st),
		mainStaged:        mainpanel.NewStagedPlan(paths, home, st),
		mainLog:           mainpanel.NewLog(paths, home, st),
		mainRestore:       mainpanel.NewRestorePlan(st),
		mainDetail:        mainpanel.NewText(),
		mainDiff:          mainpanel.NewText(),
		fixPopup:          popups.NewFix(st),
		keysPopup:         popups.NewKeys(st),
		assignPopup:       popups.NewAssign(st),
		confirmPopup:      popups.NewConfirm(st),
		errorPopup:        popups.NewError(st),
		commitPopup:       popups.NewCommit(st),
		firstRunPopup:     popups.NewFirstRun(st),
		staging:           dotfiles.Staging{},
		repositoryChoices: map[string]dotfiles.RepositoryChoice{},
		repositoriesPopup: popups.NewRepositories(st),
		failures:          map[string]string{},
		blocked:           map[string]string{},
		runner:            stow.Runner{Dotfiles: paths.Dotfiles, Target: paths.Target},
		buildAdoptPlan:    dotfiles.BuildAdoptPlanContext,
		inspectHomeEntry:  mainpanel.InspectContext,
		execEditor:        execEditorProcess,
		planReady:         true,
	}
	m.side = [SidePanelCount]Panel{m.status, m.packages, m.homePanel, m.staged, m.issues}
	m.git = inspectGit(paths)
	m.status.SetGit(m.git.repo, len(m.git.dirty))
	m.openFirstRun()
	m.refreshStagingViews()
	return m
}

func (m Model) WithMouse(enabled bool) Model {
	m.mouse = enabled
	return m
}

func (m Model) Init() tea.Cmd {
	return refreshCmd(m.paths)
}

func refreshCmd(paths config.Paths) tea.Cmd {
	return func() tea.Msg {
		issues, reports, err := doctor.Inspect(paths)
		infos := make([]packageInfo, 0, len(reports))
		for _, report := range reports {
			entries := make([]dotfiles.Entry, 0, len(report.Entries))
			for _, item := range report.Entries {
				entries = append(entries, item.Entry)
			}
			infos = append(infos, packageInfo{name: report.Package, entries: entries, err: report.Err, report: report})
		}
		return refreshedMsg{pkgs: infos, issues: issues, git: inspectGit(paths), err: err}
	}
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.relayout()
		return m, nil
	case refreshedMsg:
		if m.previewCancel != nil {
			m.previewCancel()
		}
		m.previewPath = ""
		m.previewGeneration++
		m.previewPending = nil
		m.pkgs = msg.pkgs
		m.loadErr = msg.err
		m.git = msg.git
		m.packages.SetPackages(packageItems(msg.pkgs, msg.git))
		m.issues.SetIssues(msg.issues)
		m.relayout()
		m.status.SetIssues(len(msg.issues))
		m.status.SetGit(m.git.repo, len(m.git.dirty))
		homeCmd := m.homePanel.Reload()
		return m, tea.Batch(homeCmd, m.stagingChanged())
	case planBuiltMsg:
		return m.handlePlanBuilt(msg)
	case homeEntryLoadedMsg:
		if !m.previewActive || msg.generation != m.previewActiveGeneration {
			return m, nil
		}
		m.previewActive = false
		m.previewCancel = nil
		if m.previewPending != nil {
			next := *m.previewPending
			m.previewPending = nil
			return m, m.startPreview(next)
		}
		if msg.generation == m.previewGeneration && msg.path == m.previewPath {
			m.mainHome.SetFacts(msg.path, msg.facts)
		}
		return m, nil
	case components.TreeLoadedMsg:
		cmd := m.homePanel.Update(msg)
		return m, tea.Batch(cmd, m.syncMain())
	case flashExpiredMsg:
		if msg.id == m.flashID {
			m.flash = ""
		}
		return m, nil
	case editorFinishedMsg:
		return m.handleEditorFinished(msg)
	case panels.StageRequestMsg:
		return m, m.openAssign(msg.Path)
	case panels.UnstageRequestMsg:
		return m, m.unstage(msg.Path)
	case panels.UnstageGroupMsg:
		return m, m.unstageGroup(msg.Package)
	case panels.RenameGroupMsg:
		return m, m.openRename(msg.Package)
	case popups.RepositoriesCancelledMsg:
		m.popup = popupNone
		return m, nil
	case popups.RepositoriesChosenMsg:
		m.popup = popupNone
		for path, choice := range msg.Choices {
			m.repositoryChoices[path] = choice
		}
		m.applyRepositoryChoices()
		return m, m.stagingChanged()
	case popups.AssignedMsg:
		return m, m.applyAssignment(msg)
	case popups.AssignCancelledMsg:
		m.popup = popupNone
		m.renaming = ""
		return m, nil
	case popups.ConfirmedMsg:
		m.popup = popupNone
		return m, m.startConfirmed(msg.Action)
	case popups.FixCancelledMsg:
		m.popup = popupNone
		return m, nil
	case popups.FixChosenMsg:
		m.popup = popupNone
		m.fixAction = msg.Action
		return m, m.startConfirmed(actionFix)
	case diffLoadedMsg:
		if msg.id == m.diffID && m.diffOpen {
			text := msg.text
			if msg.err != nil {
				text += "\n" + msg.err.Error()
			}
			m.mainDiff.SetText(msg.title, text)
		}
		return m, nil
	case popups.ConfirmCancelledMsg:
		m.popup = popupNone
		return m, nil
	case popups.CommitMsg:
		m.popup = popupNone
		packages := m.commitPopup.Packages()
		m.relayout()
		return m, commitCmd(m.paths.Dotfiles, packages, msg.Subject)
	case popups.CommitSkippedMsg:
		m.popup = popupNone
		m.relayout()
		return m, m.setFlash("commit skipped")
	case popups.CommitCancelledMsg:
		m.popup = popupNone
		m.relayout()
		return m, nil
	case popups.FirstRunAppliedMsg:
		return m, m.applyFirstRun(msg)
	case popups.FirstRunCancelledMsg:
		return m, m.cancelFirstRun(msg)
	case firstRunDoneMsg:
		if msg.err != nil {
			m.showError("Cannot prepare the dotfiles directory", msg.err)
			return m, nil
		}
		if msg.warning != "" {
			return m, tea.Batch(refreshCmd(m.paths), m.setFlash("git: "+msg.warning))
		}
		return m, refreshCmd(m.paths)
	case commitPreparedMsg:
		return m, m.commitPrepared(msg)
	case commitDoneMsg:
		return m, m.commitFinished(msg)
	case popups.ErrorClosedMsg:
		m.popup = popupNone
		return m, nil
	case execEventMsg:
		if m.exec == nil || msg.runID != m.exec.id {
			return m, nil
		}
		m.mainLog.Append(msg.event)
		return m, waitForEvent(m.exec.events)
	case execDoneMsg:
		if m.exec == nil || msg.runID != m.exec.id {
			return m, nil
		}
		return m.finishExecution(msg.summary)
	case spinner.TickMsg:
		return m, tea.Batch(m.status.Update(msg), m.staged.Update(msg), m.homePanel.Update(msg), m.mainHome.Update(msg))
	case panels.ActivateMsg:
		return m.activate()
	case tea.KeyPressMsg:
		return m.handleKey(msg)
	case tea.MouseMsg:
		return m.handleMouse(msg)
	}
	return m, m.focusedPanel().Update(msg)
}

func packageItems(pkgs []packageInfo, git gitState) []panels.Package {
	items := make([]panels.Package, 0, len(pkgs))
	for _, info := range pkgs {
		linked := true
		for _, item := range info.report.Entries {
			if item.State != doctor.OK {
				linked = false
			}
		}
		for _, entry := range info.entries {
			if entry.State != dotfiles.Linked {
				linked = false
				break
			}
		}
		items = append(items, panels.Package{Name: info.name, Linked: linked, Failed: info.err != nil, Dirty: git.dirty[info.name] > 0})
	}
	return items
}

func (m Model) handleKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	if m.exec != nil {
		if key.Matches(msg, m.keys.ForceQuit) {
			m.cancelExecution()
		}
		return m, nil
	}
	if key.Matches(msg, m.keys.ForceQuit) {
		return m, tea.Quit
	}
	switch m.popup {
	case popupKeys:
		if key.Matches(msg, m.keys.Back, m.keys.Help, m.keys.Quit) {
			m.popup = popupNone
			return m, nil
		}
		return m, m.keysPopup.Update(msg)
	case popupAssign:
		return m, m.assignPopup.Update(msg)
	case popupConfirm:
		return m, m.confirmPopup.Update(msg)
	case popupError:
		return m, m.errorPopup.Update(msg)
	case popupFix:
		return m, m.fixPopup.Update(msg)
	case popupCommit:
		return m, m.commitPopup.Update(msg)
	case popupRepositories:
		return m, m.repositoriesPopup.Update(msg)
	case popupFirstRun:
		return m, m.firstRunPopup.Update(msg)
	}

	if capturer, ok := m.focusedPanel().(inputCapturer); ok && capturer.CapturesInput() {
		cmd := m.focusedPanel().Update(msg)
		return m, tea.Batch(cmd, m.syncMain())
	}

	if !m.logOpen && !m.diffOpen && !m.restoreOpen {
		switch msg.String() {
		case "r":
			if m.focus == Packages && m.mainFocused {
				return m, m.openEntryRestore()
			}
			if m.focus == Packages {
				return m, m.openRestore()
			}
		case "space":
			if m.focus == Packages && m.mainFocused {
				m.mainPkg.ToggleMark()
				return m, nil
			}
		case "f":
			if m.focus == Issues || (m.focus == Packages && m.mainFocused) {
				return m, m.openFix()
			}
		case "D":
			if m.focus == Issues || (m.focus == Packages && m.mainFocused) {
				return m, m.openDiff()
			}
		case "c":
			if (m.focus == Status || m.focus == Packages) && !m.mainFocused {
				return m, m.openCommit()
			}
		case "o", "O":
			if _, ok := m.editorTargets(); ok {
				return m, m.openInEditor(msg.String() == "O")
			}
		}
	}
	switch {
	case key.Matches(msg, m.keys.Quit):
		return m, tea.Quit
	case key.Matches(msg, m.keys.Help):
		m.popup = popupKeys
		m.keysPopup.SetSections(m.keySections())
		m.relayout()
		return m, nil
	case key.Matches(msg, m.keys.ModeNext):
		m.mode = m.mode.Next()
		m.relayout()
		return m, nil
	case key.Matches(msg, m.keys.ModePrev):
		m.mode = m.mode.Prev()
		m.relayout()
		return m, nil
	case key.Matches(msg, m.keys.RestowAll):
		if m.logOpen || m.diffOpen {
			return m, nil
		}
		return m, m.confirmRestow()
	case key.Matches(msg, m.keys.Refresh):
		return m, refreshCmd(m.paths)
	case key.Matches(msg, m.keys.NextPanel):
		m.closeLog()
		m.focus = nextSide(m.focus)
		m.mainFocused = false
		m.relayout()
		return m, m.syncMain()
	case key.Matches(msg, m.keys.Back):
		if m.diffOpen {
			m.diffOpen = false
			m.mainFocused = m.focus == Packages
			m.relayout()
			return m, nil
		}
		if m.restoreOpen {
			m.restoreOpen = false
			m.mainFocused = false
			m.relayout()
			return m, m.syncMain()
		}
		if m.logOpen {
			return m, m.closeLogAndReturn()
		}
		if m.mainFocused {
			m.mainFocused = false
			m.relayout()
			return m, nil
		}
	case key.Matches(msg, m.keys.Enter):
		if m.logOpen {
			return m, m.closeLogAndReturn()
		}
		if m.restoreOpen {
			return m, m.confirmRestore()
		}
		if m.focus == Staged {
			return m.apply()
		}
		if !m.mainFocused && m.canFocusMain() {
			m.mainFocused = true
			m.relayout()
			return m, nil
		}
	case key.Matches(msg, m.keys.Repositories):
		if m.mainContext() == contextStagedPlan {
			return m, m.openRepositories()
		}
	}

	for _, p := range sidePanels {
		if key.Matches(msg, m.keys.byPanel[p]) {
			m.closeLog()
			m.focus = p
			m.mainFocused = false
			m.relayout()
			return m, m.syncMain()
		}
	}

	cmd := m.focusedPanel().Update(msg)
	return m, tea.Batch(cmd, m.syncMain())
}

func (m Model) activate() (tea.Model, tea.Cmd) {
	return m.handleKey(tea.KeyPressMsg{Code: tea.KeyEnter})
}

func nextSide(focus PanelID) PanelID {
	if !focus.IsSide() || focus == Issues {
		return Status
	}
	return focus + 1
}

type inputCapturer interface {
	CapturesInput() bool
}

func (m Model) canFocusMain() bool {
	switch m.focus {
	case Packages:
		_, ok := m.packages.Selected()
		return ok
	case Issues:
		_, ok := m.issues.Selected()
		return ok
	case Staged:
		return len(m.staging) > 0
	default:
		return false
	}
}

func (m Model) mainContext() mainContext {
	if m.logOpen {
		return contextLog
	}
	if m.diffOpen {
		return contextDiff
	}
	if m.restoreOpen {
		return contextRestore
	}
	switch m.focus {
	case Issues:
		return contextIssue
	case Home:
		return contextHomeEntry
	case Staged:
		return contextStagedPlan
	default:
		return contextPackage
	}
}

func (m Model) mainPanel() Panel {
	switch m.mainContext() {
	case contextHomeEntry:
		return m.mainHome
	case contextStagedPlan:
		return m.mainStaged
	case contextLog:
		return m.mainLog
	case contextRestore:
		return m.mainRestore
	case contextIssue:
		return m.mainDetail
	case contextDiff:
		return m.mainDiff
	default:
		return m.mainPkg
	}
}

func (m *Model) closeLog() {
	if m.exec != nil {
		return
	}
	m.logOpen = false
	m.restoreOpen = false
	m.diffOpen = false
	m.logReturn = returnFocus{}
}

func (m *Model) closeLogAndReturn() tea.Cmd {
	ret := m.logReturn
	m.closeLog()
	if ret.valid {
		m.focus = ret.focus
		m.mainFocused = ret.main && m.canFocusMain()
	} else {
		m.mainFocused = false
	}
	m.relayout()
	return m.syncMain()
}

func (m Model) focusedPanel() Panel {
	if m.mainFocused {
		return m.mainPanel()
	}
	if m.focus.IsSide() {
		return m.side[m.focus]
	}
	return m.mainPanel()
}

func (m *Model) layoutFocus() PanelID {
	if m.mainFocused {
		return Main
	}
	return m.focus
}

func (m *Model) collapsedSidePanels() [SidePanelCount]bool {
	var empty [SidePanelCount]bool
	empty[Staged] = m.staged.Len() == 0
	empty[Issues] = m.issues.Len() == 0
	return empty
}

func (m *Model) relayout() {
	m.layout = ComputeWith(m.width, m.height, m.layoutFocus(), m.mode, m.collapsedSidePanels())
	if m.layout.TooSmall {
		return
	}
	for _, p := range sidePanels {
		rect := m.layout.Side[p]
		if rect.Empty() || m.layout.Collapsed[p] {
			m.side[p].SetSize(components.InnerWidth(rect.Width), 0)
			continue
		}
		m.side[p].SetSize(components.InnerWidth(rect.Width), components.InnerHeight(rect.Height))
	}
	width, height := 0, 0
	if !m.layout.Main.Empty() {
		width = components.InnerWidth(m.layout.Main.Width)
		height = components.InnerHeight(m.layout.Main.Height)
	}
	m.mainPkg.SetSize(width, height)
	m.mainHome.SetSize(width, height)
	m.mainStaged.SetSize(width, height)
	m.mainLog.SetSize(width, height)
	m.mainRestore.SetSize(width, height)
	m.mainDetail.SetSize(width, height)
	m.mainDiff.SetSize(width, height)

	rect := m.popupRect()
	switch m.popup {
	case popupKeys:
		m.keysPopup.SetSize(rect.Width, rect.Height)
	case popupAssign:
		m.assignPopup.SetSize(rect.Width, rect.Height)
	case popupConfirm:
		m.confirmPopup.SetSize(rect.Width, rect.Height)
	case popupError:
		m.errorPopup.SetSize(rect.Width, rect.Height)
	case popupFix:
		m.fixPopup.SetSize(rect.Width, rect.Height)
	case popupCommit:
		m.commitPopup.SetSize(rect.Width, rect.Height)
	case popupRepositories:
		m.repositoriesPopup.SetSize(rect.Width, rect.Height)
	case popupFirstRun:
		m.firstRunPopup.SetSize(rect.Width, rect.Height)
	}
}

func (m *Model) syncMain() tea.Cmd {
	m.syncPackageContext()
	m.syncIssueContext()
	cmd := m.syncHomeContext()
	m.mainStaged.SetPlan(m.plan, m.staged.SelectedPackage())
	return cmd
}

func (m *Model) syncPackageContext() {
	selected, ok := m.packages.Selected()
	if !ok {
		m.mainPkg.SetPackage("", nil, nil)
		return
	}
	for _, info := range m.pkgs {
		if info.name == selected.Name {
			m.mainPkg.SetReport(info.report)
			return
		}
	}
	m.mainPkg.SetPackage(selected.Name, nil, nil)
}

func (m *Model) syncHomeContext() tea.Cmd {
	node, ok := m.homePanel.Selected()
	if !ok {
		if m.previewCancel != nil {
			m.previewCancel()
			m.previewCancel = nil
		}
		m.previewPath = ""
		m.previewGeneration++
		m.previewPending = nil
		m.mainHome.SetEntry("", "", false)
		return nil
	}
	pkg, staged := m.staging[node.Path]
	m.mainHome.SetEntry(node.Path, pkg, staged)
	if node.Path == m.previewPath {
		return nil
	}
	if m.previewCancel != nil {
		m.previewCancel()
	}
	m.previewPath = node.Path
	m.previewGeneration++
	generation, path := m.previewGeneration, node.Path
	req := previewRequest{path: path, generation: generation}
	if m.previewActive {
		m.previewPending = &req
		return m.mainHome.StartLoading()
	}
	return m.startPreview(req)
}

func (m *Model) startPreview(req previewRequest) tea.Cmd {
	ctx, cancel := context.WithCancel(context.Background())
	m.previewCancel = cancel
	m.previewActive = true
	m.previewActiveGeneration = req.generation
	inspect := m.inspectHomeEntry
	paths := m.paths
	load := func() tea.Msg {
		return homeEntryLoadedMsg{path: req.path, generation: req.generation, facts: inspect(ctx, paths, req.path)}
	}
	return tea.Batch(load, m.mainHome.StartLoading())
}

func cloneStaging(staging dotfiles.Staging) dotfiles.Staging {
	copy := make(dotfiles.Staging, len(staging))
	for path, pkg := range staging {
		copy[path] = pkg
	}
	return copy
}

func cloneChoices(choices map[string]dotfiles.RepositoryChoice) map[string]dotfiles.RepositoryChoice {
	copy := make(map[string]dotfiles.RepositoryChoice, len(choices))
	for path, choice := range choices {
		copy[path] = choice
	}
	return copy
}

func (m *Model) applyRepositoryChoices() {
	dotfiles.ApplyRepositoryChoices(&m.plan, m.repositoryChoices)
}

func (m *Model) refreshStagingViews() {
	m.status.SetStaged(len(m.staging))
	m.staged.SetStaging(m.staging)
	m.staged.SetFailures(m.failures)
	m.staged.SetBlocked(m.blocked)
	m.homePanel.SetStaging(m.staging)
}

func (m *Model) stagingChanged() tea.Cmd {
	for source := range m.repositoryChoices {
		covered := false
		for path := range m.staging {
			if source == path || pathInside(path, source) {
				covered = true
				break
			}
		}
		if !covered {
			delete(m.repositoryChoices, source)
		}
	}

	m.planVersion++
	m.planReady = len(m.staging) == 0
	m.applyPending = false
	if m.planCancel != nil {
		m.planCancel()
	}
	m.plan = dotfiles.AdoptPlan{Paths: m.paths}
	m.blocked = map[string]string{}
	m.mainStaged.SetScanning(len(m.staging) > 0)
	m.refreshStagingViews()
	m.relayout()
	sync := m.syncMain()
	if len(m.staging) == 0 {
		m.planPending = nil
		if !m.planActive {
			return tea.Batch(sync, m.staged.SetScanning(false))
		}
		return sync
	}
	req := &planRequest{version: m.planVersion, staging: cloneStaging(m.staging), choices: cloneChoices(m.repositoryChoices)}
	if m.planActive {
		m.planPending = req
		return tea.Batch(sync, m.staged.SetScanning(true))
	}
	return tea.Batch(sync, m.staged.SetScanning(true), m.startPlan(*req, false))
}

func (m *Model) startPlan(req planRequest, apply bool) tea.Cmd {
	ctx, cancel := context.WithCancel(context.Background())
	m.planActive = true
	m.planActiveVersion = req.version
	m.planCancel = cancel
	build, paths := m.buildAdoptPlan, m.paths
	return func() tea.Msg {
		plan := build(ctx, paths, req.staging)
		dotfiles.ApplyRepositoryChoices(&plan, req.choices)
		dotfiles.ValidateRepositoryChoices(ctx, &plan)
		return planBuiltMsg{version: req.version, plan: plan, apply: apply}
	}
}

func (m Model) handlePlanBuilt(msg planBuiltMsg) (tea.Model, tea.Cmd) {
	if !m.planActive || msg.version != m.planActiveVersion {
		return m, nil
	}
	m.planActive = false
	m.planCancel = nil
	if m.planPending != nil {
		next := *m.planPending
		m.planPending = nil
		return m, m.startPlan(next, false)
	}
	if msg.version != m.planVersion {
		if len(m.staging) == 0 {
			return m, m.staged.SetScanning(false)
		}
		return m, nil
	}
	m.plan = msg.plan
	m.mainStaged.SetScanning(false)
	m.applyRepositoryChoices()
	m.planReady = true
	m.blocked = map[string]string{}
	for _, pkg := range m.plan.Packages {
		for _, blocked := range pkg.Blocked {
			m.blocked[blocked.Path] = blocked.Reason
		}
	}
	m.staged.SetBlocked(m.blocked)
	sync := m.syncMain()
	stop := m.staged.SetScanning(false)
	if msg.apply && m.applyPending {
		m.applyPending = false
		updated, cmd := m.openApplyConfirmation()
		return updated, tea.Batch(sync, stop, cmd)
	}
	return m, tea.Batch(sync, stop)
}

func (m *Model) setFlash(text string) tea.Cmd {
	m.flash = text
	m.flashID++
	id := m.flashID
	return tea.Tick(flashTimeout, func(time.Time) tea.Msg { return flashExpiredMsg{id: id} })
}

func (m *Model) openAssign(path string) tea.Cmd {
	if err := dotfiles.ValidateStagingPath(m.paths, path); err != nil {
		return m.setFlash("cannot stage " + m.display(path) + ": " + err.Error())
	}
	m.popup = popupAssign
	m.renaming = ""
	cmd := m.assignPopup.Open("Assign to a package", m.display(path), path, m.knownPackages(), m.staging[path])
	m.relayout()
	return cmd
}

func (m *Model) openRename(pkg string) tea.Cmd {
	if pkg == "" {
		return nil
	}
	m.popup = popupAssign
	cmd := m.assignPopup.OpenRename("Rename package "+pkg, "renames the staged group "+pkg, m.otherPackages(pkg), pkg)
	m.renaming = pkg
	m.relayout()
	return cmd
}

func (m *Model) applyAssignment(msg popups.AssignedMsg) tea.Cmd {
	m.popup = popupNone
	if m.renaming != "" {
		from := m.renaming
		m.renaming = ""
		if msg.Package == from {
			return nil
		}
		for path, pkg := range m.staging {
			if pkg == from {
				m.staging[path] = msg.Package
			}
		}

		return tea.Batch(m.stagingChanged(), m.setFlash("renamed "+from+" to "+msg.Package))
	}
	return m.stage(msg.Path, msg.Package)
}

func (m *Model) stage(path, pkg string) tea.Cmd {
	if err := dotfiles.ValidateStagingPath(m.paths, path); err != nil {
		return m.setFlash("cannot stage " + m.display(path) + ": " + err.Error())
	}
	if ancestor, ok := stagedAncestor(m.staging, path); ok {
		return m.setFlash(m.display(path) + " is already covered by " + m.display(ancestor))
	}
	dropped := make([]string, 0, len(m.staging))
	for staged := range m.staging {
		if pathInside(path, staged) {
			dropped = append(dropped, staged)
		}
	}
	for _, staged := range dropped {
		delete(m.staging, staged)
	}
	m.staging[path] = pkg
	scan := m.stagingChanged()
	if len(dropped) > 0 {
		return tea.Batch(scan, m.setFlash(fmt.Sprintf("staged %s and dropped %s already staged below it",
			m.display(path), plural(len(dropped), "entry", "entries"))))
	}
	return tea.Batch(scan, m.setFlash("staged "+m.display(path)+" into "+pkg))
}

func (m *Model) unstage(path string) tea.Cmd {
	if _, ok := m.staging[path]; !ok {
		return nil
	}
	delete(m.staging, path)
	return m.stagingChanged()
}

func (m *Model) unstageGroup(pkg string) tea.Cmd {
	for path, staged := range m.staging {
		if staged == pkg {
			delete(m.staging, path)
		}
	}
	return m.stagingChanged()
}

func (m *Model) openRepositories() tea.Cmd {
	if !m.planReady || m.planActive {
		return m.setFlash("Scan in progress")
	}
	row, ok := m.staged.Selected()
	if !ok {
		return nil
	}
	var repositories []dotfiles.NestedRepository
	for _, pkg := range m.plan.Packages {
		if pkg.Package != row.Package {
			continue
		}
		for _, r := range pkg.Repositories {
			if row.Group || r.Source == row.Path || pathInside(row.Path, r.Source) {
				repositories = append(repositories, r)
			}
		}
	}
	if len(repositories) == 0 {
		return m.setFlash("no Git repositories in this selection")
	}
	m.repositoriesPopup.Open(repositories)
	m.popup = popupRepositories
	m.relayout()
	return nil
}

func (m Model) knownPackages() []string {
	seen := map[string]bool{}
	names := make([]string, 0, len(m.pkgs)+len(m.staging))
	for _, info := range m.pkgs {
		if !seen[info.name] {
			seen[info.name] = true
			names = append(names, info.name)
		}
	}
	for _, pkg := range m.staging {
		if !seen[pkg] {
			seen[pkg] = true
			names = append(names, pkg)
		}
	}
	sort.Strings(names)
	return names
}

func (m Model) otherPackages(pkg string) []string {
	names := make([]string, 0, len(m.pkgs))
	for _, name := range m.knownPackages() {
		if name != pkg {
			names = append(names, name)
		}
	}
	return names
}

func (m Model) display(path string) string {
	return components.DisplayPath(path, m.home)
}

func stagedAncestor(staging dotfiles.Staging, path string) (string, bool) {
	for staged := range staging {
		if pathInside(staged, path) {
			return staged, true
		}
	}
	return "", false
}

func pathInside(root, path string) bool {
	rel, err := filepath.Rel(filepath.Clean(root), filepath.Clean(path))
	if err != nil || rel == "." || rel == ".." {
		return false
	}
	return !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

func plural(n int, one, many string) string {
	if n == 1 {
		return fmt.Sprintf("%d %s", n, one)
	}
	return fmt.Sprintf("%d %s", n, many)
}

func (m Model) popupRect() Rect {
	w := min(m.width-popupMargin, popupMaxWidth)
	h := m.height - popupMargin
	if w < 1 || h < 1 {
		return Rect{}
	}
	return Rect{X: (m.width - w) / 2, Y: (m.height - h) / 2, Width: w, Height: h}
}

func (m Model) View() tea.View {
	view := tea.NewView(m.render())
	view.AltScreen = true
	if m.mouse {
		view.MouseMode = tea.MouseModeCellMotion
	}
	return view
}

func (m Model) render() string {
	if m.width <= 0 || m.height <= 0 {
		return ""
	}
	if m.layout.TooSmall {
		return m.st.Dim.Render("terminal too small")
	}

	layers := []*lipgloss.Layer{lipgloss.NewLayer(blank(m.width, m.height))}
	for _, p := range sidePanels {
		rect := m.layout.Side[p]
		if rect.Empty() {
			continue
		}
		layers = append(layers, layerAt(rect, m.renderSide(p, rect)))
	}
	if !m.layout.Main.Empty() {
		layers = append(layers, layerAt(m.layout.Main, m.renderMain(m.layout.Main)))
	}
	if !m.layout.KeyBar.Empty() {
		layers = append(layers, layerAt(m.layout.KeyBar, m.renderKeyBar(m.layout.KeyBar.Width)))
	}
	if m.popup != popupNone {
		if rect := m.popupRect(); !rect.Empty() {
			content := m.keysPopup.View()
			switch m.popup {
			case popupAssign:
				content = m.assignPopup.View()
			case popupConfirm:
				content = m.confirmPopup.View()
			case popupError:
				content = m.errorPopup.View()
			case popupFix:
				content = m.fixPopup.View()
			case popupCommit:
				content = m.commitPopup.View()
			case popupRepositories:
				content = m.repositoriesPopup.View()
			case popupFirstRun:
				content = m.firstRunPopup.View()
			}
			layers = append(layers, layerAt(rect, content).Z(1))
		}
	}
	return lipgloss.NewCompositor(layers...).Render()
}

func layerAt(rect Rect, content string) *lipgloss.Layer {
	return lipgloss.NewLayer(content).X(rect.X).Y(rect.Y)
}

func blank(w, h int) string {
	line := strings.Repeat(" ", w)
	lines := make([]string, h)
	for i := range lines {
		lines[i] = line
	}
	return strings.Join(lines, "\n")
}

func (m Model) renderSide(p PanelID, rect Rect) string {
	panel := m.side[p]
	frame := components.Frame{
		Title:   panel.Title(),
		Counter: panel.Counter(),
		Focused: !m.mainFocused && m.focus == p,
		Styles:  m.st,
	}
	if m.layout.Collapsed[p] {
		return frame.RenderCollapsed(rect.Width)
	}
	return frame.Render(rect.Width, rect.Height, panel.View())
}

func (m Model) renderMain(rect Rect) string {
	panel := m.mainPanel()
	frame := components.Frame{
		Title:   panel.Title(),
		Counter: panel.Counter(),
		Focused: m.mainFocused,
		Styles:  m.st,
	}
	return frame.Render(rect.Width, rect.Height, panel.View())
}

func (m Model) renderKeyBar(width int) string {
	if m.exec != nil {
		return components.Fit(" ctrl+c cancel", width)
	}
	if m.flash != "" {
		return components.Fit(" "+m.st.Warn.Render(m.flash), width)
	}
	if m.loadErr != nil {
		return components.Fit(" "+m.st.Error.Render("cannot read the dotfiles directory: "+m.loadErr.Error()), width)
	}
	var b strings.Builder
	b.WriteString(" ")
	for _, item := range m.keyBarItems() {
		b.WriteString(strings.Repeat(" ", item.gap))
		b.WriteString(m.renderBarItem(item))
	}
	return components.Fit(b.String(), width)
}

func (m Model) renderBarItem(item barItem) string {
	if !item.tab {
		return m.st.KeyName.Render(item.key) + " " + item.desc
	}
	if !m.mainFocused && m.focus == item.panel {
		return m.st.TabActive.Render(item.label())
	}
	return m.st.TabInactive.Render(item.label())
}

func (m Model) contextKeys() []key.Binding {
	keys := append([]key.Binding{}, m.focusedPanel().Keys()...)
	if targets, ok := m.editorTargets(); ok {
		keys = append(keys, m.keys.Open)
		if targets.repo != "" {
			keys = append(keys, m.keys.OpenTarget)
		}
	}
	if m.focus == Staged && !m.logOpen && len(m.staging) > 0 {
		keys = append(keys, key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "apply")))
	} else if !m.mainFocused && m.canFocusMain() {
		keys = append(keys, m.keys.Enter)
	}
	return keys
}

func (m Model) globalKeys() []key.Binding {
	return []key.Binding{
		m.keys.Panels,
		m.keys.NextPanel,
		m.keys.ModeNext,
		m.keys.RestowAll,
		m.keys.Refresh,
		m.keys.Help,
		m.keys.Quit,
	}
}

func (m Model) keySections() []popups.Section {
	title := m.mainPanel().Title()
	if !m.mainFocused {
		title = m.side[m.focus].Title()
	}
	return []popups.Section{
		{Title: title, Bindings: m.contextKeys()},
		{Title: "Global", Bindings: append(m.globalKeys(), m.keys.Back, m.keys.ForceQuit)},
	}
}
