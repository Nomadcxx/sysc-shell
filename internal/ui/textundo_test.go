package ui

import "testing"

func typeString(f *Field, s string) {
	for _, r := range s {
		f.record(editInsert, string(r))
		f.Insert(string(r))
	}
}

func TestTypingCoalescesUntilWhitespaceFollowsAWord(t *testing.T) {
	f := NewField("")
	typeString(f, "hello world")
	if !f.Undo() || f.Text != "hello " {
		t.Fatalf("first undo = %q, want %q", f.Text, "hello ")
	}
	if !f.Undo() || f.Text != "" {
		t.Fatalf("second undo = %q, want empty", f.Text)
	}
	if f.Undo() {
		t.Fatal("undo past the start reported a change")
	}
	if !f.Redo() || f.Text != "hello " {
		t.Fatalf("redo = %q", f.Text)
	}
}

func TestCaretMoveBreaksCoalescing(t *testing.T) {
	f := NewField("")
	typeString(f, "ab")
	f.MoveGrapheme(-1, false)
	f.BreakUndo()
	typeString(f, "X")
	if !f.Undo() || f.Text != "ab" {
		t.Fatalf("undo after a caret move = %q, want ab", f.Text)
	}
}

func TestNewEditClearsRedo(t *testing.T) {
	f := NewField("")
	typeString(f, "ab")
	f.Undo()
	typeString(f, "c")
	if f.Redo() {
		t.Fatal("redo survived a new edit")
	}
}

func TestHistoryIsCapped(t *testing.T) {
	f := NewField("")
	for i := 0; i < 250; i++ {
		f.record(editOther, "")
		f.Insert("x ")
	}
	steps := 0
	for f.Undo() {
		steps++
	}
	if steps != undoLimit {
		t.Fatalf("undo steps = %d, want %d", steps, undoLimit)
	}
}

// Review focus 1: a reseed with new text collapses selection and history.
func TestSyncFromNewTextClearsSelectionAndHistory(t *testing.T) {
	f := NewField("")
	typeString(f, "abc")
	f.SelectAll()
	f.SyncFrom(&Node{Kind: KindTextField, Text: "server value", Cursor: 3})
	if f.HasSelection() || f.Cursor != 3 || f.Undo() {
		t.Fatalf("after reseed: cursor %d anchor %d", f.Cursor, f.Anchor)
	}
}

// Review focus 2: a builder that writes the caret at the end must not move a
// caret the user placed.
func TestSyncFromSameTextKeepsFieldCaret(t *testing.T) {
	f := NewField("abcdef")
	f.SetCaret(2, false)
	f.SyncFrom(&Node{Kind: KindTextField, Text: "abcdef", Cursor: 6})
	if f.Cursor != 2 {
		t.Fatalf("cursor = %d, want 2", f.Cursor)
	}
}

func TestSyncToWritesSelection(t *testing.T) {
	f := field("hello", 4, 1)
	n := &Node{Kind: KindTextField}
	f.SyncTo(n)
	if n.SelStart != 1 || n.SelEnd != 4 || n.Cursor != 4 {
		t.Fatalf("node %+v", n)
	}
}
