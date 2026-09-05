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
	Quit      key.Binding
	ForceQuit key.Binding
	Help      key.Binding
	NextPanel key.Binding
	ModeNext  key.Binding
	ModePrev  key.Binding
	Refresh   key.Binding
	RestowAll key.Binding
	Back      key.Binding
	Enter     key.Binding
	Panels    key.Binding
	Toggle    key.Binding
	byPanel   [SidePanelCount]key.Binding
}

func defaultKeyMap() keyMap {
	m := keyMap{
		Quit:      key.NewBinding(key.WithKeys("q"), key.WithHelp("q", "quit")),
		ForceQuit: key.NewBinding(key.WithKeys("ctrl+c"), key.WithHelp("ctrl+c", "quit")),
		Help:      key.NewBinding(key.WithKeys("?"), key.WithHelp("?", "keys")),
		NextPanel: key.NewBinding(key.WithKeys("tab"), key.WithHelp("tab", "next panel")),
		ModeNext:  key.NewBinding(key.WithKeys("+"), key.WithHelp("+", "wider")),
		ModePrev:  key.NewBinding(key.WithKeys("_"), key.WithHelp("_", "narrower")),
		Refresh:   key.NewBinding(key.WithKeys("ctrl+r"), key.WithHelp("ctrl+r", "rescan")),
		RestowAll: key.NewBinding(key.WithKeys("R"), key.WithHelp("R", "restow all")),
		Back:      key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "back")),
		Enter:     key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "focus main")),
		Panels:    key.NewBinding(key.WithKeys("0", "1", "2", "3", "4"), key.WithHelp("0-4", "panels")),
		Toggle:    key.NewBinding(key.WithKeys("x"), key.WithHelp("x", "toggle .git removal")),
	}
	for _, p := range sidePanels {
		m.byPanel[p] = key.NewBinding(key.WithKeys(string(rune('0' + int(p)))))
	}
	return m
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

	keysPopup     *popups.Keys
	assignPopup   *popups.Assign
	confirmPopup  *popups.Confirm
	errorPopup    *popups.Error
	fixPopup      *popups.Fix
	commitPopup   *popups.Commit
	firstRunPopup *popups.FirstRun
	popup         popupKind

	git         gitState
	gitDeclined bool

	staging   dotfiles.Staging
	removeGit map[string]bool
	failures  map[string]string
	plan      dotfiles.AdoptPlan

	runner  dotfiles.Runner
	exec    *execution
	runID   int
	logOpen bool

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
		paths:         paths,
		home:          home,
		stowVersion:   stowVersion,
		st:            st,
		keys:          defaultKeyMap(),
		focus:         Packages,
		mode:          ModeNormal,
		mouse:         true,
		status:        panels.NewStatus(paths, home, stowVersion, st),
		packages:      panels.NewPackages(st),
		homePanel:     panels.NewHome(paths, home, st),
		staged:        panels.NewStaged(paths, home, st),
		issues:        panels.NewIssues(st),
		mainPkg:       mainpanel.NewPackage(paths, home, st),
		mainHome:      mainpanel.NewHomeEntry(paths, home, st),
		mainStaged:    mainpanel.NewStagedPlan(paths, home, st),
		mainLog:       mainpanel.NewLog(paths, home, st),
		mainRestore:   mainpanel.NewRestorePlan(st),
		mainDetail:    mainpanel.NewText(),
		mainDiff:      mainpanel.NewText(),
		fixPopup:      popups.NewFix(st),
		keysPopup:     popups.NewKeys(st),
		assignPopup:   popups.NewAssign(st),
		confirmPopup:  popups.NewConfirm(st),
		errorPopup:    popups.NewError(st),
		commitPopup:   popups.NewCommit(st),
		firstRunPopup: popups.NewFirstRun(st),
		staging:       dotfiles.Staging{},
		removeGit:     map[string]bool{},
		failures:      map[string]string{},
		runner:        stow.Runner{Dotfiles: paths.Dotfiles, Target: paths.Target},
	}
	m.side = [SidePanelCount]Panel{m.status, m.packages, m.homePanel, m.staged, m.issues}
	m.git = inspectGit(paths)
	m.status.SetGit(m.git.repo, len(m.git.dirty))
	m.openFirstRun()
	m.rebuildPlan()
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
		m.pkgs = msg.pkgs
		m.loadErr = msg.err
		m.git = msg.git
		m.packages.SetPackages(packageItems(msg.pkgs, msg.git))
		m.issues.SetIssues(msg.issues)
		m.status.SetIssues(len(msg.issues))
		m.status.SetGit(m.git.repo, len(m.git.dirty))
		m.homePanel.Reload()
		m.homePanel.SetStaging(m.staging)
		m.rebuildPlan()
		m.syncMain()
		return m, nil
	case flashExpiredMsg:
		if msg.id == m.flashID {
			m.flash = ""
		}
		return m, nil
	case panels.StageRequestMsg:
		return m, m.openAssign(msg.Path)
	case panels.UnstageRequestMsg:
		m.unstage(msg.Path)
		return m, nil
	case panels.UnstageGroupMsg:
		m.unstageGroup(msg.Package)
		return m, nil
	case panels.RenameGroupMsg:
		return m, m.openRename(msg.Package)
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
		return m, m.status.Update(msg)
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
	case popupFirstRun:
		return m, m.firstRunPopup.Update(msg)
	}

	if capturer, ok := m.focusedPanel().(inputCapturer); ok && capturer.CapturesInput() {
		cmd := m.focusedPanel().Update(msg)
		m.syncMain()
		return m, cmd
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
		m.syncMain()
		return m, nil
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
			m.syncMain()
			return m, nil
		}
		if m.logOpen {
			m.closeLog()
			m.mainFocused = false
			m.relayout()
			m.syncMain()
			return m, nil
		}
		if m.mainFocused {
			m.mainFocused = false
			m.relayout()
			return m, nil
		}
	case key.Matches(msg, m.keys.Enter):
		if m.restoreOpen && !m.logOpen {
			return m, m.confirmRestore()
		}
		if m.focus == Staged && !m.logOpen {
			return m.apply()
		}
		if !m.mainFocused && m.canFocusMain() {
			m.mainFocused = true
			m.relayout()
			return m, nil
		}
	case key.Matches(msg, m.keys.Toggle):
		if m.mainContext() == contextStagedPlan {
			m.toggleRemoveGit(m.staged.SelectedPackage())
			return m, nil
		}
	}

	for _, p := range sidePanels {
		if key.Matches(msg, m.keys.byPanel[p]) {
			m.closeLog()
			m.focus = p
			m.mainFocused = false
			m.relayout()
			m.syncMain()
			return m, nil
		}
	}

	cmd := m.focusedPanel().Update(msg)
	m.syncMain()
	return m, cmd
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

func (m *Model) relayout() {
	m.layout = Compute(m.width, m.height, m.layoutFocus(), m.mode)
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
	case popupFirstRun:
		m.firstRunPopup.SetSize(rect.Width, rect.Height)
	}
}

func (m *Model) syncMain() {
	m.syncPackageContext()
	m.syncIssueContext()
	m.syncHomeContext()
	m.mainStaged.SetPlan(m.plan, m.staged.SelectedPackage())
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

func (m *Model) syncHomeContext() {
	node, ok := m.homePanel.Selected()
	if !ok {
		m.mainHome.SetEntry("", "", false)
		return
	}
	pkg, staged := m.staging[node.Path]
	m.mainHome.SetEntry(node.Path, pkg, staged)
}

func (m *Model) rebuildPlan() {
	m.plan = dotfiles.BuildAdoptPlan(m.paths, m.staging)
	for i := range m.plan.Packages {
		m.plan.Packages[i].RemoveNestedGit = m.removeGit[m.plan.Packages[i].Package]
	}
	m.status.SetStaged(len(m.staging))
	m.staged.SetStaging(m.staging)
	m.staged.SetFailures(m.failures)
	m.homePanel.SetStaging(m.staging)
}

func (m *Model) stagingChanged() {
	m.rebuildPlan()
	m.syncMain()
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
		if m.removeGit[from] {
			delete(m.removeGit, from)
			m.removeGit[msg.Package] = true
		}
		m.stagingChanged()
		return m.setFlash("renamed " + from + " to " + msg.Package)
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
	m.stagingChanged()
	if len(dropped) > 0 {
		return m.setFlash(fmt.Sprintf("staged %s and dropped %s already staged below it",
			m.display(path), plural(len(dropped), "entry", "entries")))
	}
	return m.setFlash("staged " + m.display(path) + " into " + pkg)
}

func (m *Model) unstage(path string) {
	if _, ok := m.staging[path]; !ok {
		return
	}
	delete(m.staging, path)
	m.stagingChanged()
}

func (m *Model) unstageGroup(pkg string) {
	for path, staged := range m.staging {
		if staged == pkg {
			delete(m.staging, path)
		}
	}
	delete(m.removeGit, pkg)
	m.stagingChanged()
}

func (m *Model) toggleRemoveGit(pkg string) {
	if pkg == "" || !m.mainStaged.HasWarning(pkg) {
		return
	}
	m.removeGit[pkg] = !m.removeGit[pkg]
	m.stagingChanged()
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
		{Title: "Global", Bindings: append(m.globalKeys(), m.keys.Back, m.keys.ModePrev, m.keys.ForceQuit)},
	}
}
