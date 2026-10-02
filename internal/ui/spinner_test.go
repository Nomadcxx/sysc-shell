package ui

import "testing"

// A spinner is a square: its width is its diameter in a row and in a column,
// and with none given it takes the default size rather than the band.
func TestSpinnerMeasuresAsASquare(t *testing.T) {
	t.Parallel()
	n := &Node{Kind: KindSpinner, Key: "s", Width: 32}
	if w, h, err := measureNode(n, 80, fakeMeasure); err != nil || w != 32 || h != 32 {
		t.Fatalf("row measure = %dx%d, %v; want 32x32", w, h, err)
	}
	if h, err := columnChildHeight(n, 200, fakeMeasure); err != nil || h != 32 {
		t.Fatalf("column height = %d, %v; want 32", h, err)
	}
	d := &Node{Kind: KindSpinner, Key: "d"}
	if w, h, err := measureNode(d, 80, fakeMeasure); err != nil || w != SpinnerSize || h != SpinnerSize {
		t.Fatalf("default row measure = %dx%d, %v; want %dx%d", w, h, err, SpinnerSize, SpinnerSize)
	}
}

// The animator holds a spinner's phase under its key, so a spinner needs one.
func TestSpinnerNeedsAStableKey(t *testing.T) {
	t.Parallel()
	if !Animated(&Node{Kind: KindSpinner}) {
		t.Fatal("a spinner is animated")
	}
	root := &Node{Kind: KindColumn, Children: []*Node{{Kind: KindSpinner}}}
	if err := ValidateKeys(root); err == nil {
		t.Fatal("a spinner without a key must be refused")
	}
}
