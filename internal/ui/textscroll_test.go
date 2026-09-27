package ui

import "testing"

func TestKeepCaretVisible(t *testing.T) {
	cases := []struct {
		name                                       string
		scroll, caretX, textW, viewW, margin, want int
	}{
		{"fits: never scrolls", 40, 10, 80, 100, 8, 0},
		{"caret past the right edge", 0, 150, 300, 100, 8, 58},
		{"caret before the left edge", 120, 60, 300, 100, 8, 52},
		{"caret inside the window stays put", 50, 100, 300, 100, 8, 50},
		{"never past the end", 250, 300, 300, 100, 8, 200},
		{"never negative", 5, 2, 300, 100, 8, 0},
		{"zero view", 30, 10, 300, 0, 8, 0},
	}
	for _, tc := range cases {
		if got := KeepCaretVisible(tc.scroll, tc.caretX, tc.textW, tc.viewW, tc.margin); got != tc.want {
			t.Errorf("%s: got %d, want %d", tc.name, got, tc.want)
		}
	}
}
