package components

import "testing"

func TestRowAt(t *testing.T) {
	tests := []struct {
		name   string
		offset int
		y      int
		count  int
		want   int
		ok     bool
	}{
		{name: "first row without scrolling", offset: 0, y: 0, count: 3, want: 0, ok: true},
		{name: "last row without scrolling", offset: 0, y: 2, count: 3, want: 2, ok: true},
		{name: "below the last row", offset: 0, y: 3, count: 3},
		{name: "scrolled list", offset: 4, y: 1, count: 10, want: 5, ok: true},
		{name: "scrolled past the end", offset: 8, y: 3, count: 10},
		{name: "negative y", offset: 0, y: -1, count: 3},
		{name: "empty list", offset: 0, y: 0, count: 0},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			row, ok := RowAt(test.offset, test.y, test.count)
			if ok != test.ok || row != test.want {
				t.Fatalf("RowAt(%d, %d, %d) = %d, %v; want %d, %v", test.offset, test.y, test.count, row, ok, test.want, test.ok)
			}
		})
	}
}
