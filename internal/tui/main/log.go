package mainpanel

import (
	"fmt"
	"strings"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"

	"github.com/horbo/stower/internal/config"
	"github.com/horbo/stower/internal/dotfiles"
	"github.com/horbo/stower/internal/tui/components"
	"github.com/horbo/stower/internal/tui/styles"
)

type logKind int

const (
	logPlain logKind = iota
	logHeader
	logOK
	logFail
	logWarn
	logRollback
	logOutput
)

type logLine struct {
	kind logKind
	text string
}

type Log struct {
	paths   config.Paths
	home    string
	title   string
	lines   []logLine
	pkg     string
	pending int
	running bool
	done    bool
	summary dotfiles.Summary
	width   int
	height  int
	st      styles.Styles
	vp      viewport.Model
}

func NewLog(paths config.Paths, home string, st styles.Styles) *Log {
	vp := viewport.New()
	vp.FillHeight = false
	return &Log{paths: paths, home: home, st: st, vp: vp, pending: -1}
}

func (l *Log) Start(title string) {
	l.title = title
	l.lines = nil
	l.pkg = ""
	l.pending = -1
	l.running = true
	l.done = false
	l.summary = dotfiles.Summary{}
	l.vp.SetYOffset(0)
	l.render()
}

func (l *Log) Running() bool {
	return l.running
}

func (l *Log) Append(event dotfiles.Event) {
	if event.Package != "" && event.Package != l.pkg {
		if len(l.lines) > 0 {
			l.push(logPlain, "")
		}
		l.push(logHeader, event.Package)
		l.pkg = event.Package
		l.pending = -1
	}
	switch event.Kind {
	case dotfiles.StepStarted:
		l.pending = len(l.lines)
		l.push(logPlain, "· "+l.short(event.Message))
	case dotfiles.StepDone:
		l.resolve(logOK, "✔ "+l.short(event.Message))
	case dotfiles.StepFailed:
		l.resolve(logFail, "✘ "+l.short(event.Message)+reason(event.Err))
	case dotfiles.OutputLine:
		l.push(logOutput, "    "+l.short(event.Message))
	case dotfiles.Rollback:
		l.push(logRollback, "↩ "+l.short(event.Message)+reason(event.Err))
	case dotfiles.PackageDone:
		l.push(logOK, "✔ "+event.Package+" done")
	case dotfiles.PackageFailed:
		l.push(logFail, "✘ "+event.Package+" failed"+reason(event.Err))
	}
	l.render()
}

func (l *Log) Note(text string) {
	l.push(logWarn, text)
	l.render()
}

func (l *Log) Finish(summary dotfiles.Summary, cancelled bool) {
	l.running = false
	l.done = true
	l.summary = summary
	l.pending = -1
	l.push(logPlain, "")
	if cancelled {
		l.push(logFail, "✘ cancelled")
	}
	if len(summary.Succeeded) > 0 {
		l.push(logOK, fmt.Sprintf("✔ %s "+l.completedVerb()+": %s",
			plural(len(summary.Succeeded), "package", "packages"), strings.Join(summary.Succeeded, ", ")))
	}
	if len(summary.Skipped) > 0 {
		l.push(logWarn, fmt.Sprintf("⚠ %s skipped, nothing to move: %s",
			plural(len(summary.Skipped), "package", "packages"), strings.Join(summary.Skipped, ", ")))
	}
	for _, failure := range summary.Failed {
		l.push(logFail, "✘ "+failure.Package+" failed"+reason(failure.Err))
	}
	if len(summary.Succeeded) == 0 && len(summary.Skipped) == 0 && len(summary.Failed) == 0 {
		l.push(logPlain, "nothing to do")
	}
	l.render()
	l.vp.GotoBottom()
}

func (l *Log) Lines() []string {
	out := make([]string, 0, len(l.lines))
	for _, line := range l.lines {
		out = append(out, line.text)
	}
	return out
}

func (l *Log) push(kind logKind, text string) {
	l.lines = append(l.lines, logLine{kind: kind, text: text})
}

func (l *Log) resolve(kind logKind, text string) {
	if l.pending >= 0 && l.pending < len(l.lines) {
		l.lines[l.pending] = logLine{kind: kind, text: text}
		l.pending = -1
		return
	}
	l.push(kind, text)
}

func (l *Log) SetSize(width, height int) {
	if width == l.width && height == l.height {
		return
	}
	l.width = width
	l.height = height
	l.vp.SetWidth(max(0, width))
	l.vp.SetHeight(max(0, height))
	l.render()
}

func (l *Log) Update(msg tea.Msg) tea.Cmd {
	if l.running {
		return nil
	}
	vp, cmd := l.vp.Update(msg)
	l.vp = vp
	return cmd
}

func (l *Log) View() string {
	if l.width <= 0 || l.height <= 0 {
		return ""
	}
	return l.vp.View()
}

func (l *Log) Title() string {
	if l.title == "" {
		return "Log"
	}
	return "Log: " + l.title
}

func (l *Log) Counter() string {
	if l.running {
		return "running…"
	}
	if !l.done {
		return ""
	}
	counter := fmt.Sprintf("%d ok", len(l.summary.Succeeded))
	if failed := len(l.summary.Failed); failed > 0 {
		counter += fmt.Sprintf(" · %d failed", failed)
	}
	return counter
}

func (l *Log) Keys() []key.Binding {
	if l.running {
		return []key.Binding{key.NewBinding(key.WithKeys("ctrl+c"), key.WithHelp("ctrl+c", "cancel"))}
	}
	return []key.Binding{
		key.NewBinding(key.WithKeys("j", "k"), key.WithHelp("j/k", "scroll")),
		key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "close")),
	}
}

func (l *Log) render() {
	if l.width <= 0 {
		l.vp.SetContent("")
		return
	}
	rendered := make([]string, 0, len(l.lines))
	for _, line := range l.lines {
		rendered = append(rendered, l.style(line.kind, components.Truncate(line.text, l.width)))
	}
	if len(rendered) == 0 {
		rendered = append(rendered, l.st.Dim.Render("no operation has run yet"))
	}
	l.vp.SetContent(strings.Join(rendered, "\n"))
	if l.running {
		l.vp.GotoBottom()
	}
}

func (l *Log) style(kind logKind, text string) string {
	switch kind {
	case logHeader:
		return l.st.Header.Render(components.Fit(text, l.width))
	case logOK:
		return l.st.OK.Render(text)
	case logFail:
		return l.st.Error.Render(text)
	case logWarn:
		return l.st.Warn.Render(text)
	case logRollback:
		return l.st.Warn.Render(text)
	case logOutput:
		return l.st.Dim.Render(text)
	default:
		return text
	}
}

func (l *Log) short(text string) string {
	text = strings.ReplaceAll(text, l.paths.Dotfiles+"/", "")
	if l.home != "" {
		text = strings.ReplaceAll(text, l.home+"/", "~/")
	}
	return text
}

func reason(err error) string {
	if err == nil {
		return ""
	}
	return ": " + err.Error()
}

func (l *Log) completedVerb() string {
	switch l.title {
	case "restore":
		return "restored"
	case "fix":
		return "fixed"
	case "restow":
		return "restowed"
	default:
		return "applied"
	}
}
