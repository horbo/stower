package components

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/horbo/stower/internal/tui/styles"
)

var frameWidths = []int{20, 40, 80}

func testFrame(focused bool) Frame {
	return Frame{Title: "[1] Packages", Counter: "1 of 6", Focused: focused, Styles: styles.Default()}
}

func TestFrameExactSize(t *testing.T) {
	body := "first line\nsecond line that is quite long and will be truncated somewhere"
	for _, width := range frameWidths {
		for _, height := range []int{2, 3, 6} {
			for _, focused := range []bool{false, true} {
				out := testFrame(focused).Render(width, height, body)
				lines := strings.Split(out, "\n")
				if len(lines) != height {
					t.Fatalf("width %d height %d focused %v: %d lines", width, height, focused, len(lines))
				}
				for i, line := range lines {
					if got := ansi.StringWidth(line); got != width {
						t.Fatalf("width %d height %d focused %v: line %d has width %d: %q",
							width, height, focused, i, got, ansi.Strip(line))
					}
				}
			}
		}
	}
}

func TestFrameCollapsedIsOneLine(t *testing.T) {
	for _, width := range frameWidths {
		out := testFrame(true).RenderCollapsed(width)
		if strings.Contains(out, "\n") {
			t.Fatalf("width %d: collapsed frame has more than one line", width)
		}
		if got := ansi.StringWidth(out); got != width {
			t.Fatalf("width %d: collapsed width = %d", width, got)
		}
		if !strings.Contains(ansi.Strip(out), "[1] Pa") {
			t.Fatalf("width %d: collapsed frame lost the title: %q", width, ansi.Strip(out))
		}
		if height1 := testFrame(true).Render(width, 1, "body"); height1 != out {
			t.Fatalf("width %d: height 1 must render the collapsed variant", width)
		}
	}
}

func TestFrameTitleAndCounterPlacement(t *testing.T) {
	top := ansi.Strip(strings.Split(testFrame(false).Render(40, 3, ""), "\n")[0])
	if !strings.HasPrefix(top, "╭─[1] Packages ") {
		t.Fatalf("title is not on the left of the top edge: %q", top)
	}
	if !strings.HasSuffix(top, " 1 of 6 ╮") {
		t.Fatalf("counter is not on the right of the top edge: %q", top)
	}
}

func TestFrameTruncatesTitle(t *testing.T) {
	frame := Frame{Title: "Package: a-very-long-package-name-that-never-fits", Counter: "12", Styles: styles.Default()}
	for _, width := range frameWidths {
		top := ansi.Strip(frame.RenderCollapsed(width))
		if got := ansi.StringWidth(top); got != width {
			t.Fatalf("width %d: top edge width = %d: %q", width, got, top)
		}
		if width < 60 && !strings.Contains(top, "…") {
			t.Fatalf("width %d: long title was not truncated: %q", width, top)
		}
	}
}

func TestFrameDropsCounterWhenTooNarrow(t *testing.T) {
	frame := Frame{Title: "Status", Counter: "10 of 10", Styles: styles.Default()}
	top := ansi.Strip(frame.RenderCollapsed(12))
	if ansi.StringWidth(top) != 12 {
		t.Fatalf("top edge width = %d: %q", ansi.StringWidth(top), top)
	}
	if strings.Contains(top, "10 of 10") {
		t.Fatalf("counter must be dropped when it does not fit: %q", top)
	}
}

func TestFrameDegradesAtTinyWidths(t *testing.T) {
	for width := 0; width < 8; width++ {
		out := testFrame(false).Render(width, 3, "body")
		if width == 0 {
			if out != "" {
				t.Fatalf("width 0 must render nothing, got %q", out)
			}
			continue
		}
		for _, line := range strings.Split(out, "\n") {
			if got := ansi.StringWidth(line); got != width {
				t.Fatalf("width %d: line width = %d: %q", width, got, ansi.Strip(line))
			}
		}
	}
}

func TestFitAndTruncate(t *testing.T) {
	if got := Fit("ab", 5); got != "ab   " {
		t.Fatalf("Fit padded to %q", got)
	}
	if got := Fit("abcdef", 4); ansi.StringWidth(got) != 4 || !strings.HasSuffix(got, "…") {
		t.Fatalf("Fit truncated to %q", got)
	}
	if got := Truncate("abc", 10); got != "abc" {
		t.Fatalf("Truncate changed a short string to %q", got)
	}
}
