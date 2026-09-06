package tui

import (
	"fmt"
	"testing"
)

var testSizes = []struct {
	w, h int
}{
	{60, 16},
	{59, 16},
	{80, 24},
	{100, 30},
	{70, 18},
	{200, 50},
	{100, 24},
	{120, 20},
	{60, 15},
}

var testModes = []ScreenMode{ModeNormal, ModeHalf, ModeFullscreen}

var testFocus = []PanelID{Status, Packages, Home, Staged, Issues, Main}

var testEmpty = []struct {
	name  string
	empty [SidePanelCount]bool
}{
	{name: "full"},
	{name: "staged", empty: emptyPanels(Staged)},
	{name: "issues", empty: emptyPanels(Issues)},
	{name: "both", empty: emptyPanels(Staged, Issues)},
}

func emptyPanels(ids ...PanelID) [SidePanelCount]bool {
	var empty [SidePanelCount]bool
	for _, id := range ids {
		empty[id] = true
	}
	return empty
}

func TestComputeTilesTheScreen(t *testing.T) {
	for _, size := range testSizes {
		for _, mode := range testModes {
			for _, focus := range testFocus {
				for _, empty := range testEmpty {
					name := fmt.Sprintf("%dx%d/%s/%s/%s", size.w, size.h, mode, focus, empty.name)
					t.Run(name, func(t *testing.T) {
						l := ComputeWith(size.w, size.h, focus, mode, empty.empty)
						if l.TooSmall {
							if size.w >= MinWidth && size.h >= MinHeight {
								t.Fatalf("unexpected TooSmall at %dx%d", size.w, size.h)
							}
							if len(l.Rects()) != 0 {
								t.Fatalf("TooSmall layout has %d rectangles", len(l.Rects()))
							}
							return
						}
						assertTiles(t, l, size.w, size.h)
					})
				}
				name := fmt.Sprintf("%dx%d/%s/%s", size.w, size.h, mode, focus)
				t.Run(name, func(t *testing.T) {
					l := Compute(size.w, size.h, focus, mode)
					if l.TooSmall {
						if size.w >= MinWidth && size.h >= MinHeight {
							t.Fatalf("unexpected TooSmall at %dx%d", size.w, size.h)
						}
						if len(l.Rects()) != 0 {
							t.Fatalf("TooSmall layout has %d rectangles", len(l.Rects()))
						}
						return
					}
					assertTiles(t, l, size.w, size.h)
				})
			}
		}
	}
}

func assertTiles(t *testing.T, l Layout, w, h int) {
	t.Helper()
	grid := make([][]int, h)
	for y := range grid {
		grid[y] = make([]int, w)
	}
	for _, rect := range l.Rects() {
		if rect.X < 0 || rect.Y < 0 || rect.X+rect.Width > w || rect.Y+rect.Height > h {
			t.Fatalf("rectangle %+v is outside %dx%d", rect, w, h)
		}
		for y := rect.Y; y < rect.Y+rect.Height; y++ {
			for x := rect.X; x < rect.X+rect.Width; x++ {
				grid[y][x]++
			}
		}
	}
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			switch grid[y][x] {
			case 1:
			case 0:
				t.Fatalf("cell %d,%d is not covered", x, y)
			default:
				t.Fatalf("cell %d,%d is covered %d times", x, y, grid[y][x])
			}
		}
	}
}

func TestComputeTooSmall(t *testing.T) {
	for _, size := range []struct{ w, h int }{{59, 16}, {60, 15}, {50, 10}, {0, 0}} {
		l := Compute(size.w, size.h, Packages, ModeNormal)
		if !l.TooSmall {
			t.Fatalf("%dx%d: expected TooSmall", size.w, size.h)
		}
	}
	if Compute(60, 16, Packages, ModeNormal).TooSmall {
		t.Fatal("60x16 must not be TooSmall")
	}
}

func TestComputeSideWidth(t *testing.T) {
	tests := []struct {
		w, h int
		mode ScreenMode
		want int
	}{
		{100, 30, ModeNormal, 33},
		{200, 50, ModeNormal, 48},
		{100, 30, ModeHalf, 50},
		{200, 50, ModeHalf, 100},
	}
	for _, test := range tests {
		l := Compute(test.w, test.h, Packages, test.mode)
		if l.Portrait {
			t.Fatalf("%dx%d: expected landscape", test.w, test.h)
		}
		if got := l.Side[Packages].Width; got != test.want {
			t.Fatalf("%dx%d %s: side width = %d, want %d", test.w, test.h, test.mode, got, test.want)
		}
		if got := l.Main.Width; got != test.w-test.want {
			t.Fatalf("%dx%d %s: main width = %d, want %d", test.w, test.h, test.mode, got, test.w-test.want)
		}
		if l.TabStrip {
			t.Fatalf("%dx%d: landscape must not use the tab strip", test.w, test.h)
		}
	}
}

func TestComputeLandscapeHeights(t *testing.T) {
	l := Compute(100, 30, Packages, ModeNormal)
	if got := l.Side[Status].Height; got != StatusHeight {
		t.Fatalf("status height = %d, want %d", got, StatusHeight)
	}
	for _, p := range []PanelID{Packages, Home, Staged, Issues} {
		if l.Collapsed[p] {
			t.Fatalf("%s must not be collapsed at 100x30", p)
		}
		if l.Side[p].Height < MinPanelHeight {
			t.Fatalf("%s height = %d, want at least %d", p, l.Side[p].Height, MinPanelHeight)
		}
	}
	if got := l.Main.Height; got != 30-KeyBarHeight {
		t.Fatalf("main height = %d, want %d", got, 30-KeyBarHeight)
	}
}

func TestComputeAccordion(t *testing.T) {
	for _, focus := range []PanelID{Packages, Home, Staged, Issues} {
		l := Compute(100, 24, focus, ModeNormal)
		if l.Collapsed[focus] {
			t.Fatalf("focused %s must not be collapsed", focus)
		}
		body := 24 - KeyBarHeight - StatusHeight
		if got := l.Side[focus].Height; got != body-3*CollapsedRows {
			t.Fatalf("focused %s height = %d, want %d", focus, got, body-3*CollapsedRows)
		}
		for _, other := range []PanelID{Packages, Home, Staged, Issues} {
			if other == focus {
				continue
			}
			if !l.Collapsed[other] || l.Side[other].Height != CollapsedRows {
				t.Fatalf("%s: want collapsed, got height %d", other, l.Side[other].Height)
			}
		}
	}
	l := Compute(100, 24, Status, ModeNormal)
	if l.Expanded != Packages || l.Collapsed[Packages] {
		t.Fatal("with Status focused the accordion must expand Packages")
	}
}

func TestComputePortrait(t *testing.T) {
	l := Compute(70, 18, Home, ModeNormal)
	if !l.Portrait || !l.TabStrip {
		t.Fatal("70x18 must be portrait with a tab strip")
	}
	body := 18 - KeyBarHeight
	if got := l.Side[Home].Height; got != body*40/100 {
		t.Fatalf("top panel height = %d, want %d", got, body*40/100)
	}
	if got := l.Main.Height; got != body-l.Side[Home].Height {
		t.Fatalf("main height = %d, want %d", got, body-l.Side[Home].Height)
	}
	for _, p := range []PanelID{Status, Packages, Staged, Issues} {
		if !l.Side[p].Empty() {
			t.Fatalf("%s must be hidden in portrait", p)
		}
	}
	if half := Compute(70, 18, Home, ModeHalf); half.Side[Home].Height != body*50/100 {
		t.Fatalf("half mode top height = %d, want %d", half.Side[Home].Height, body*50/100)
	}
	if status := Compute(70, 18, Status, ModeNormal); status.Side[Status].Height != StatusHeight {
		t.Fatalf("portrait status height = %d, want %d", status.Side[Status].Height, StatusHeight)
	}
	if main := Compute(70, 18, Main, ModeNormal); main.Main.Height != body || !main.Side[Home].Empty() {
		t.Fatal("portrait with main focused must give the whole body to main")
	}
}

func TestComputeFullscreen(t *testing.T) {
	l := Compute(100, 30, Issues, ModeFullscreen)
	if got := len(l.Rects()); got != 2 {
		t.Fatalf("fullscreen has %d rectangles, want 2", got)
	}
	if l.Side[Issues].Width != 100 || l.Side[Issues].Height != 29 {
		t.Fatalf("fullscreen panel = %+v", l.Side[Issues])
	}
	if !l.Main.Empty() {
		t.Fatal("main must be hidden when a side panel is fullscreen")
	}
	main := Compute(100, 30, Main, ModeFullscreen)
	if main.Main.Width != 100 || main.Main.Height != 29 {
		t.Fatalf("fullscreen main = %+v", main.Main)
	}
}

func TestScreenModeCycle(t *testing.T) {
	if got := ModeNormal.Next(); got != ModeHalf {
		t.Fatalf("next of normal = %s", got)
	}
	if got := ModeFullscreen.Next(); got != ModeNormal {
		t.Fatalf("next of fullscreen = %s", got)
	}
	if got := ModeNormal.Prev(); got != ModeFullscreen {
		t.Fatalf("prev of normal = %s", got)
	}
}

func TestComputeCollapsesEmptyPanels(t *testing.T) {
	empty := emptyPanels(Staged, Issues)

	base := Compute(100, 30, Packages, ModeNormal)
	l := ComputeWith(100, 30, Packages, ModeNormal, empty)
	if !l.Collapsed[Staged] || !l.Collapsed[Issues] {
		t.Fatalf("empty panels are not collapsed: staged %d, issues %d", l.Side[Staged].Height, l.Side[Issues].Height)
	}
	for _, p := range []PanelID{Staged, Issues} {
		if l.Side[p].Height != CollapsedRows {
			t.Fatalf("%s height = %d, want %d", p, l.Side[p].Height, CollapsedRows)
		}
	}
	for _, p := range []PanelID{Packages, Home} {
		if l.Collapsed[p] {
			t.Fatalf("%s must not be collapsed", p)
		}
		if l.Side[p].Height <= base.Side[p].Height {
			t.Fatalf("%s height = %d, want more than %d", p, l.Side[p].Height, base.Side[p].Height)
		}
	}
	assertTiles(t, l, 100, 30)

	tight := ComputeWith(100, 24, Packages, ModeNormal, empty)
	rest := 24 - KeyBarHeight - StatusHeight
	want := (rest - 2*CollapsedRows) / 2
	for _, p := range []PanelID{Packages, Home} {
		if tight.Collapsed[p] || tight.Side[p].Height != want {
			t.Fatalf("%s height = %d, want an even split of %d", p, tight.Side[p].Height, want)
		}
	}
	if !tight.Collapsed[Staged] || !tight.Collapsed[Issues] {
		t.Fatal("100x24 with two empty panels must keep them collapsed")
	}
	assertTiles(t, tight, 100, 24)

	focused := ComputeWith(100, 30, Issues, ModeNormal, empty)
	if !focused.Collapsed[Issues] {
		t.Fatal("a focused empty panel must stay collapsed")
	}
	if focused.Expanded != Packages {
		t.Fatalf("expanded panel = %s, want Packages", focused.Expanded)
	}
	assertTiles(t, focused, 100, 30)

	accordion := ComputeWith(100, 24, Packages, ModeNormal, [SidePanelCount]bool{})
	if accordion != Compute(100, 24, Packages, ModeNormal) {
		t.Fatal("without empty panels the layout must not change")
	}

	narrow := ComputeWith(100, 17, Packages, ModeNormal, empty)
	body := 17 - KeyBarHeight - StatusHeight
	if narrow.Side[Packages].Height != body-3*CollapsedRows {
		t.Fatalf("100x17 must still use the accordion: packages height = %d", narrow.Side[Packages].Height)
	}
	for _, p := range []PanelID{Home, Staged, Issues} {
		if !narrow.Collapsed[p] {
			t.Fatalf("%s must be collapsed in the accordion", p)
		}
	}
	assertTiles(t, narrow, 100, 17)
}
