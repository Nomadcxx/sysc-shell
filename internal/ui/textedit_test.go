package ui

import "testing"

func field(text string, cursor, anchor int) *Field {
	return &Field{Text: text, Cursor: cursor, Anchor: anchor}
}

func TestGraphemeMotionNeverSplitsAClusterOrAnEmoji(t *testing.T) {
	s := "éx👩‍💻y" // e + combining acute, x, woman technologist (ZWJ), y
	f := field(s, 0, 0)
	var stops []int
	for i := 0; i < 5; i++ {
		f.MoveGrapheme(+1, false)
		stops = append(stops, f.Cursor)
	}
	want := []int{3, 4, 4 + len("👩‍💻"), len(s), len(s)}
	for i := range want {
		if stops[i] != want[i] {
			t.Fatalf("stops %v, want %v", stops, want)
		}
	}
}

func TestWordMotionAndDeletion(t *testing.T) {
	f := field("alpha beta_2  gamma", 19, 19)
	f.MoveWord(-1, false)
	if f.Cursor != 14 {
		t.Fatalf("word left from end = %d, want 14", f.Cursor)
	}
	f.MoveWord(-1, false)
	if f.Cursor != 6 {
		t.Fatalf("second word left = %d, want 6 (beta_2 is one word)", f.Cursor)
	}
	f.MoveWord(+1, false)
	if f.Cursor != 12 {
		t.Fatalf("word right = %d, want 12", f.Cursor)
	}
	g := field("alpha beta", 10, 10)
	if !g.DeleteWord(-1) || g.Text != "alpha " {
		t.Fatalf("delete word back = %q", g.Text)
	}
}

func TestShiftMotionExtendsAndPlainMotionCollapses(t *testing.T) {
	f := field("hello world", 0, 0)
	f.MoveWord(+1, true)
	if s, e := f.Selection(); s != 0 || e != 5 || f.SelectedText() != "hello" {
		t.Fatalf("selection %d..%d %q", s, e, f.SelectedText())
	}
	f.MoveGrapheme(-1, false)
	if f.HasSelection() || f.Cursor != 0 {
		t.Fatalf("left with a selection should collapse to its start, got cursor %d anchor %d", f.Cursor, f.Anchor)
	}
}

func TestInsertAndBackspaceReplaceTheSelection(t *testing.T) {
	f := field("hello world", 11, 6)
	f.Insert("there")
	if f.Text != "hello there" || f.HasSelection() {
		t.Fatalf("insert over selection = %q", f.Text)
	}
	f.SelectAll()
	f.Backspace()
	if f.Text != "" {
		t.Fatalf("backspace over select-all = %q", f.Text)
	}
}

func TestLineEdgesAndVerticalMotionInMultiline(t *testing.T) {
	f := &Field{Text: "abc\nde\nfghij", Cursor: 2, Anchor: 2, Multiline: true, goalCol: -1}
	if !f.MoveVertical(+1, false) || f.Cursor != 6 {
		t.Fatalf("down from col 2 = %d, want 6 (end of short line)", f.Cursor)
	}
	if !f.MoveVertical(+1, false) || f.Cursor != 9 {
		t.Fatalf("down again keeps goal col 2 = %d, want 9", f.Cursor)
	}
	f.MoveLineEdge(false, false)
	if f.Cursor != 7 {
		t.Fatalf("line start = %d, want 7", f.Cursor)
	}
	if !f.DeleteToLineEdge(true) || f.Text != "abc\nde\n" {
		t.Fatalf("delete to line end = %q", f.Text)
	}
	single := field("abc", 1, 1)
	if single.MoveVertical(+1, false) {
		t.Fatal("single-line field handled Down")
	}
}

func TestSelectWordAndLineAt(t *testing.T) {
	f := &Field{Text: "one two\nthree", Multiline: true}
	f.SelectWordAt(5)
	if f.SelectedText() != "two" {
		t.Fatalf("word at 5 = %q", f.SelectedText())
	}
	f.SelectLineAt(10)
	if f.SelectedText() != "three" {
		t.Fatalf("line at 10 = %q", f.SelectedText())
	}
}
