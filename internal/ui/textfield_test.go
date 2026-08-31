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
