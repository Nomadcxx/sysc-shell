package ui

import (
	"strings"
	"testing"
)

// TestCheckFitAgreesWithLayout pins the checker to the layout it mirrors: a
// violation must exist exactly when Layout would refuse the tree. The checker
// duplicates the placement rules rather than running them (Layout stops at the
// first rejection, a checker wants the whole list), so this is what keeps the
// two from drifting apart.
func TestCheckFitAgreesWithLayout(t *testing.T) {
	t.Parallel()

	padded := func(height int, kids ...*Node) *Node {
		return &Node{Kind: KindRow, Padding: 8, Height: height, Children: kids}
	}
	cases := []struct {
		name   string
		root   *Node
		bounds Rect
	}{
		{"padded row too short", padded(28,
			&Node{Kind: KindText, Text: "Avg 70%"}), Rect{W: 290, H: 28}},
		{"padded row with room", padded(42,
			&Node{Kind: KindText, Text: "Avg 70%"}), Rect{W: 290, H: 42}},
		{"button past the right edge", &Node{Kind: KindRow, Gap: 6, Children: []*Node{
			{Kind: KindText, Text: strings.Repeat("w", 40)},
			{Kind: KindButton, Text: "Export CSV", Width: 80},
		}}, Rect{W: 300, H: 28}},
		{"capsule fills the band", &Node{Kind: KindRow, Padding: 8, Children: []*Node{
			{Kind: KindCapsule, Children: []*Node{{Kind: KindText, Text: "A"}}},
		}}, Rect{W: 290, H: 42}},
		{"pin-end row reserves its button", &Node{Kind: KindRow, PinEnd: true, Gap: 8, Children: []*Node{
			{Kind: KindText, Text: strings.Repeat("caption ", 20)},
			{Kind: KindButton, Text: "Export CSV", Width: 80},
		}}, Rect{W: 412, H: 28}},
		{"scroll keeps its declared width", &Node{Kind: KindRow, Children: []*Node{
			{Kind: KindScroll, Width: 200, Height: 80},
		}}, Rect{W: 120, H: 80}},
		{"two violations in one tree", &Node{Kind: KindColumn, Children: []*Node{
			padded(28, &Node{Kind: KindText, Text: "one"}),
			padded(28, &Node{Kind: KindText, Text: "two"}),
		}}, Rect{W: 290, H: 200}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			problems := CheckFit(c.root, c.bounds, fakeMeasure)
			err := Layout(c.root, c.bounds, fakeMeasure)
			if (err != nil) != (len(problems) > 0) {
				t.Fatalf("CheckFit found %d problems (%v), Layout returned %v", len(problems), problems, err)
			}
		})
	}
}

// TestCheckFitReportsEveryViolation is the reason the checker exists: the two
// padded rows below both reject, and the layout would name only the first.
func TestCheckFitReportsEveryViolation(t *testing.T) {
	t.Parallel()

	root := &Node{Kind: KindColumn, Children: []*Node{
		{Kind: KindRow, Padding: 8, Height: 28, Path: "root.children[0]",
			Children: []*Node{{Kind: KindText, Text: "one", Path: "root.children[0].children[0]"}}},
		{Kind: KindRow, Padding: 8, Height: 28, Path: "root.children[1]",
			Children: []*Node{{Kind: KindText, Text: "two", Path: "root.children[1].children[0]"}}},
	}}
	problems := CheckFit(root, Rect{W: 290, H: 200}, fakeMeasure)
	if len(problems) != 2 {
		t.Fatalf("problems = %d (%v), want 2 — the layout stops at the first, the checker must not", len(problems), problems)
	}
	if problems[0].Path != "root.children[0].children[0]" || problems[1].Path != "root.children[1].children[0]" {
		t.Fatalf("problem paths = %q, %q", problems[0].Path, problems[1].Path)
	}
	for _, p := range problems {
		if !strings.Contains(p.Message, "does not fit in 274x12") {
			t.Errorf("problem %q missing the content box", p.Message)
		}
	}
}
