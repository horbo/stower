package panels

import (
	tea "charm.land/bubbletea/v2"

	"github.com/horbo/stower/internal/tui/components"
)

type ActivateMsg struct{}

func activate() tea.Cmd {
	return func() tea.Msg { return ActivateMsg{} }
}

func (p *Packages) Click(x, y int) tea.Cmd {
	row, ok := components.RowAt(p.offset, y, len(p.items))
	if !ok {
		return nil
	}
	if row == p.cursor {
		return activate()
	}
	p.cursor = row
	p.clampOffset()
	return nil
}

func (p *Packages) Scroll(delta int) {
	p.move(delta)
}

func (p *Home) Click(x, y int) tea.Cmd {
	p.tree.Click(y)
	p.applyBadges()
	return nil
}

func (p *Home) Scroll(delta int) {
	p.tree.Scroll(delta)
	p.applyBadges()
}

func (p *Staged) Click(x, y int) tea.Cmd {
	row, ok := components.RowAt(p.offset, y, len(p.rows))
	if !ok || p.rows[row].Reason != "" {
		return nil
	}
	if row == p.cursor {
		return activate()
	}
	p.cursor = row
	p.clampOffset()
	return nil
}

func (p *Staged) Scroll(delta int) {
	step := 1
	if delta < 0 {
		step = -1
	}
	for i := 0; i < delta*step; i++ {
		p.move(step)
	}
}

func (p *Issues) Click(x, y int) tea.Cmd {
	row, ok := components.RowAt(p.offset(), y, len(p.items))
	if !ok {
		return nil
	}
	if row == p.cursor {
		return activate()
	}
	p.cursor = row
	return nil
}

func (p *Issues) Scroll(delta int) {
	p.cursor = min(max(0, len(p.items)-1), max(0, p.cursor+delta))
}
