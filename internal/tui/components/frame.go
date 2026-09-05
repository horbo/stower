package components

import (
	"strings"

	"github.com/charmbracelet/x/ansi"

	"github.com/horbo/stower/internal/tui/styles"
)

const (
	topLeft     = "╭"
	topRight    = "╮"
	bottomLeft  = "╰"
	bottomRight = "╯"
	horizontal  = "─"
	vertical    = "│"
	ellipsis    = "…"
)

type Frame struct {
	Title   string
	Counter string
	Focused bool
	Styles  styles.Styles
}

func InnerWidth(width int) int {
	if width < 6 {
		return max(0, width-2)
	}
	return width - 4
}

func InnerHeight(height int) int {
	return max(0, height-2)
}

func (f Frame) Render(width, height int, body string) string {
	if width <= 0 || height <= 0 {
		return ""
	}
	if height == 1 {
		return f.RenderCollapsed(width)
	}
	lines := make([]string, 0, height)
	lines = append(lines, f.topEdge(width))
	inner := InnerWidth(width)
	bodyLines := strings.Split(body, "\n")
	for i := 0; i < height-2; i++ {
		line := ""
		if i < len(bodyLines) {
			line = bodyLines[i]
		}
		lines = append(lines, f.bodyLine(width, inner, line))
	}
	lines = append(lines, f.bottomEdge(width))
	return strings.Join(lines, "\n")
}

func (f Frame) RenderCollapsed(width int) string {
	return f.topEdge(width)
}

func (f Frame) topEdge(width int) string {
	border := f.Styles.Border
	title := f.Styles.Title
	counterStyle := f.Styles.Counter
	if f.Focused {
		border = f.Styles.BorderActive
		title = f.Styles.TitleActive
		counterStyle = f.Styles.BorderActive
	}
	if width <= 0 {
		return ""
	}
	if width < 4 {
		return border.Render(strings.Repeat(horizontal, width))
	}

	inner := width - 2
	titleText := f.Title
	counterText := f.Counter

	rightWidth := 0
	if counterText != "" {
		rightWidth = ansi.StringWidth(counterText) + 2
	}
	budget := inner - 3 - rightWidth
	if budget < 1 && counterText != "" {
		counterText = ""
		rightWidth = 0
		budget = inner - 3
	}
	switch {
	case budget < 1:
		titleText = ""
	case ansi.StringWidth(titleText) > budget:
		titleText = ansi.Truncate(titleText, budget, ellipsis)
	}

	used := rightWidth
	if titleText != "" {
		used += ansi.StringWidth(titleText) + 2
	}
	fill := inner - used
	if fill < 0 {
		fill = 0
	}

	var b strings.Builder
	b.WriteString(border.Render(topLeft))
	if titleText != "" {
		b.WriteString(border.Render(horizontal))
		b.WriteString(title.Render(titleText))
		b.WriteString(border.Render(" "))
	}
	b.WriteString(border.Render(strings.Repeat(horizontal, fill)))
	if counterText != "" {
		b.WriteString(border.Render(" "))
		b.WriteString(counterStyle.Render(counterText))
		b.WriteString(border.Render(" "))
	}
	b.WriteString(border.Render(topRight))
	return b.String()
}

func (f Frame) bottomEdge(width int) string {
	border := f.Styles.Border
	if f.Focused {
		border = f.Styles.BorderActive
	}
	if width < 2 {
		return border.Render(strings.Repeat(horizontal, max(0, width)))
	}
	return border.Render(bottomLeft + strings.Repeat(horizontal, width-2) + bottomRight)
}

func (f Frame) bodyLine(width, inner int, line string) string {
	border := f.Styles.Border
	if f.Focused {
		border = f.Styles.BorderActive
	}
	if width < 2 {
		return strings.Repeat(" ", max(0, width))
	}
	content := Fit(line, inner)
	pad := ""
	if width >= 6 {
		pad = " "
	}
	return border.Render(vertical) + pad + content + pad + border.Render(vertical)
}

func Fit(s string, width int) string {
	if width <= 0 {
		return ""
	}
	w := ansi.StringWidth(s)
	if w > width {
		return ansi.Truncate(s, width, ellipsis)
	}
	if w < width {
		return s + strings.Repeat(" ", width-w)
	}
	return s
}

func FitLeft(s string, width int) string {
	if width <= 0 {
		return ""
	}
	w := ansi.StringWidth(s)
	if w > width {
		return ansi.TruncateLeft(s, w-width+1, ellipsis)
	}
	if w < width {
		return s + strings.Repeat(" ", width-w)
	}
	return s
}

func TruncateLeft(s string, width int) string {
	if width <= 0 {
		return ""
	}
	w := ansi.StringWidth(s)
	if w <= width {
		return s
	}
	return ansi.TruncateLeft(s, w-width+1, ellipsis)
}

func Width(s string) int {
	return ansi.StringWidth(s)
}

func Truncate(s string, width int) string {
	if width <= 0 {
		return ""
	}
	if ansi.StringWidth(s) <= width {
		return s
	}
	return ansi.Truncate(s, width, ellipsis)
}
