package tui

const (
	MinWidth       = 60
	MinHeight      = 16
	LandscapeWidth = 100
	StatusHeight   = 3
	MinPanelHeight = 6
	CollapsedRows  = 1
	KeyBarHeight   = 1
	sideMin        = 32
	sideMax        = 48
	portraitTop    = 40
	halfTop        = 50
	minMainHeight  = 3
)

type ScreenMode int

const (
	ModeNormal ScreenMode = iota
	ModeHalf
	ModeFullscreen
)

func (m ScreenMode) String() string {
	switch m {
	case ModeNormal:
		return "normal"
	case ModeHalf:
		return "half"
	case ModeFullscreen:
		return "fullscreen"
	default:
		return "unknown"
	}
}

func (m ScreenMode) Next() ScreenMode {
	if m >= ModeFullscreen {
		return ModeNormal
	}
	return m + 1
}

func (m ScreenMode) Prev() ScreenMode {
	if m <= ModeNormal {
		return ModeFullscreen
	}
	return m - 1
}

type Rect struct {
	X      int
	Y      int
	Width  int
	Height int
}

func (r Rect) Empty() bool {
	return r.Width <= 0 || r.Height <= 0
}

type Layout struct {
	Width     int
	Height    int
	TooSmall  bool
	Portrait  bool
	Mode      ScreenMode
	Focus     PanelID
	Expanded  PanelID
	Side      [SidePanelCount]Rect
	Collapsed [SidePanelCount]bool
	Main      Rect
	KeyBar    Rect
	TabStrip  bool
}

func (l Layout) Rects() []Rect {
	rects := make([]Rect, 0, SidePanelCount+2)
	for _, p := range sidePanels {
		if !l.Side[p].Empty() {
			rects = append(rects, l.Side[p])
		}
	}
	if !l.Main.Empty() {
		rects = append(rects, l.Main)
	}
	if !l.KeyBar.Empty() {
		rects = append(rects, l.KeyBar)
	}
	return rects
}

func Compute(w, h int, focus PanelID, mode ScreenMode) Layout {
	l := Layout{Width: w, Height: h, Mode: mode, Focus: focus}
	if w < MinWidth || h < MinHeight {
		l.TooSmall = true
		return l
	}
	l.Portrait = w < LandscapeWidth
	l.TabStrip = l.Portrait
	l.Expanded = expandedPanel(focus)
	l.KeyBar = Rect{X: 0, Y: h - KeyBarHeight, Width: w, Height: KeyBarHeight}
	body := h - KeyBarHeight

	switch {
	case mode == ModeFullscreen:
		layoutFullscreen(&l, w, body, focus)
	case l.Portrait:
		layoutPortrait(&l, w, body, focus, mode)
	default:
		layoutLandscape(&l, w, body, mode)
	}
	return l
}

func expandedPanel(focus PanelID) PanelID {
	switch focus {
	case Packages, Home, Staged, Issues:
		return focus
	default:
		return Packages
	}
}

func layoutFullscreen(l *Layout, w, body int, focus PanelID) {
	full := Rect{X: 0, Y: 0, Width: w, Height: body}
	if focus == Main || !focus.IsSide() {
		l.Main = full
		return
	}
	l.Side[focus] = full
}

func layoutPortrait(l *Layout, w, body int, focus PanelID, mode ScreenMode) {
	if focus == Main {
		l.Main = Rect{X: 0, Y: 0, Width: w, Height: body}
		return
	}
	top := portraitTopHeight(body, focus, mode)
	l.Side[focus] = Rect{X: 0, Y: 0, Width: w, Height: top}
	l.Main = Rect{X: 0, Y: top, Width: w, Height: body - top}
}

func portraitTopHeight(body int, focus PanelID, mode ScreenMode) int {
	if focus == Status {
		return StatusHeight
	}
	percent := portraitTop
	if mode == ModeHalf {
		percent = halfTop
	}
	top := body * percent / 100
	if top < StatusHeight {
		top = StatusHeight
	}
	if top > body-minMainHeight {
		top = body - minMainHeight
	}
	return top
}

func layoutLandscape(l *Layout, w, body int, mode ScreenMode) {
	side := sideWidth(w, mode)
	l.Main = Rect{X: side, Y: 0, Width: w - side, Height: body}

	l.Side[Status] = Rect{X: 0, Y: 0, Width: side, Height: StatusHeight}
	rest := body - StatusHeight
	heights := splitHeights(rest, l.Expanded)
	y := StatusHeight
	for i, p := range [4]PanelID{Packages, Home, Staged, Issues} {
		l.Side[p] = Rect{X: 0, Y: y, Width: side, Height: heights[i]}
		l.Collapsed[p] = heights[i] <= CollapsedRows
		y += heights[i]
	}
}

func sideWidth(w int, mode ScreenMode) int {
	if mode == ModeHalf {
		return w / 2
	}
	side := w / 3
	if side < sideMin {
		side = sideMin
	}
	if side > sideMax {
		side = sideMax
	}
	return side
}

func splitHeights(rest int, expanded PanelID) [4]int {
	var heights [4]int
	order := [4]PanelID{Packages, Home, Staged, Issues}
	if rest/4 >= MinPanelHeight {
		base := rest / 4
		extra := rest % 4
		for i := range heights {
			heights[i] = base
			if i < extra {
				heights[i]++
			}
		}
		return heights
	}
	for i := range heights {
		heights[i] = CollapsedRows
	}
	for i, p := range order {
		if p == expanded {
			heights[i] = rest - CollapsedRows*3
		}
	}
	return heights
}
