package ui

import "testing"

// sampleNode returns a minimally valid node of the given kind, populated well
// enough that a measure path can size it.
func sampleNode(k Kind) *Node {
	n := &Node{Kind: k, Text: "x", Width: 24}
	switch k {
	case KindMeter, KindSlider:
		n.Value, n.Min, n.Max, n.Step = 0.5, 0, 1, 0.1
	case KindGraph:
		n.Values = []float64{0.1, 0.9}
	case KindScroll, KindVirtualList:
		n.ItemCount, n.ItemHeight = 3, 10
		n.Item = func(int) *Node { return &Node{Kind: KindText, Text: "i"} }
	case KindRow, KindColumn, KindMenu, KindCapsule:
		n.Children = []*Node{{Kind: KindText, Text: "c"}}
	}
	return n
}

// allKinds is every kind declared in tree.go. A new kind appended to the iota
// without a line here fails TestAllKindsAreAccountedFor.
var allKinds = []Kind{
	KindRow, KindText, KindMeter, KindButton, KindGraph, KindColumn,
	KindSeparator, KindTab, KindToggle, KindSlider, KindMenu, KindTextField,
	KindScroll, KindVirtualList, KindCapsule,
}

// rowUnsupported and columnUnsupported name the kinds each measure path
// deliberately rejects. Every other kind must measure without error.
//
// This table is the guard for sysc-41: a kind that is declared but missing
// from a measure path returns "unsupported kind N" from a live configure,
// which fails the whole surface rather than the one node.
var (
	rowUnsupported    = map[Kind]bool{KindRow: true, KindColumn: true}
	columnUnsupported = map[Kind]bool{KindColumn: true}
)

func TestAllKindsAreAccountedFor(t *testing.T) {
	t.Parallel()
	if len(allKinds) != int(kindCount) {
		t.Fatalf("allKinds has %d entries but %d kinds are declared; add the new kind to allKinds and decide its case in each measure path",
			len(allKinds), int(kindCount))
	}
	for i, k := range allKinds {
		if int(k) != i {
			t.Fatalf("allKinds[%d] is kind %d; keep it in iota order", i, k)
		}
	}
}

func TestEveryKindMeasuresInARow(t *testing.T) {
	t.Parallel()
	for _, k := range allKinds {
		if rowUnsupported[k] {
			continue
		}
		if _, _, err := measureNode(sampleNode(k), 32, fakeMeasure); err != nil {
			t.Errorf("measureNode(kind %d): %v", k, err)
		}
	}
}

func TestEveryKindMeasuresInAColumn(t *testing.T) {
	t.Parallel()
	for _, k := range allKinds {
		if columnUnsupported[k] {
			continue
		}
		if _, err := columnChildHeight(sampleNode(k), 200, fakeMeasure); err != nil {
			t.Errorf("columnChildHeight(kind %d): %v", k, err)
		}
	}
}
