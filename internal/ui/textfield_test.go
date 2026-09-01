package ui

import "testing"

func TestTextFieldCommitAppendsAndPreeditRenders(t *testing.T) {
	t.Parallel()
	f := NewField("")
	f.Preedit("hel")
	if f.Text != "" || f.PreeditText != "hel" {
		t.Fatalf("preedit mutated committed text: %+v", f)
	}
	f.Commit("lo")
	if f.Text != "lo" || f.PreeditText != "" {
		t.Fatalf("commit = %+v, want text lo and empty preedit", f)
	}
}

func TestTextFieldBackspaceAndCursor(t *testing.T) {
	t.Parallel()
	f := NewField("ab")
	if f.Cursor != 2 {
		t.Fatalf("cursor = %d, want 2", f.Cursor)
	}
	f.Backspace()
	if f.Text != "a" || f.Cursor != 1 {
		t.Fatalf("backspace = %+v", f)
	}
	f.Backspace()
	f.Backspace()
	if f.Text != "" || f.Cursor != 0 {
		t.Fatalf("backspace must clamp, got %+v", f)
	}
}

func TestMultilineInsertsNewlineAndBackspacesAcrossIt(t *testing.T) {
	t.Parallel()
	f := NewField("ab")
	f.Multiline = true
	f.Cursor = 1
	f.Insert("\n")
	if f.Text != "a\nb" {
		t.Fatalf("insert newline = %q", f.Text)
	}
	f.Backspace()
	if f.Text != "ab" || f.Cursor != 1 {
		t.Fatalf("backspace across newline = %+v", f)
	}
}

func TestMultilineMovesByUTF8Rune(t *testing.T) {
	t.Parallel()
	f := NewField("a界b")
	f.Move(-1)
	if f.Cursor != len("a界") {
		t.Fatalf("cursor = %d", f.Cursor)
	}
	f.Move(-1)
	if f.Cursor != 1 {
		t.Fatalf("cursor after 界 = %d", f.Cursor)
	}
}

func TestMultilineSubmitOnEnterDoesNotInsert(t *testing.T) {
	t.Parallel()
	f := NewField("hi")
	f.Multiline = true
	f.SubmitOnEnter = true
	if f.Insert("\n") {
		t.Fatal("submit-on-enter inserted a newline")
	}
	if f.Text != "hi" {
		t.Fatalf("text = %q", f.Text)
	}
}

func TestMultilineFieldGrowsWithLineCount(t *testing.T) {
	t.Parallel()
	measure := func(s string, _ bool) (int, int) { return len(s) * 8, 16 }
	n := &Node{Kind: KindTextField, Text: "one\ntwo\nthree", Multiline: true, Padding: 0}
	h, err := columnChildHeight(n, 200, measure)
	if err != nil {
		t.Fatal(err)
	}
	if h != 48 {
		t.Fatalf("height = %d, want 48", h)
	}
}
