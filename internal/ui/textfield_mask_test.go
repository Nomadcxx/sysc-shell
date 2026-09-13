package ui

import "testing"

// Masking is a render-time substitution. The field keeps the real value, so
// editing, cursor movement and submit are unchanged and exactly one code path
// knows about the disguise.
func TestMaskedFieldKeepsItsRealValue(t *testing.T) {
	f := NewField("")
	f.Masked = true
	f.Insert("hunter2")
	if f.Text != "hunter2" {
		t.Fatalf("Text = %q; masking must not touch the stored value", f.Text)
	}
	if f.Cursor != len("hunter2") {
		t.Errorf("Cursor = %d, want %d", f.Cursor, len("hunter2"))
	}
	n := f.Node("Password")
	if !n.Masked {
		t.Error("Node must carry Masked so the renderer can disguise it")
	}
	if n.Text != "hunter2" {
		t.Errorf("Node.Text = %q; the renderer substitutes, the tree does not", n.Text)
	}
}

// Backspace and cursor motion operate on the real runes, not on bullets.
func TestMaskedFieldEditsTheRealRunes(t *testing.T) {
	f := NewField("")
	f.Masked = true
	f.Insert("abc")
	f.Backspace()
	if f.Text != "ab" {
		t.Fatalf("Text = %q, want %q", f.Text, "ab")
	}
	f.Move(-1)
	f.Insert("X")
	if f.Text != "aXb" {
		t.Errorf("Text = %q, want %q", f.Text, "aXb")
	}
}

// An unmasked field must be unaffected: the flag is opt-in per field.
func TestUnmaskedFieldReportsNoMask(t *testing.T) {
	f := NewField("visible")
	if n := f.Node("Search"); n.Masked {
		t.Error("a field without Masked must not mark its node")
	}
}

// Four sites derive geometry from a field's text: two measurement paths, the
// drawn string, and the committed prefix that places the caret. They all go
// through these helpers, so they cannot disagree.
func TestDisplayHelpersMaskEveryGeometrySite(t *testing.T) {
	f := NewField("hunter2")
	f.Masked = true
	n := f.Node("Password")

	if got, want := DisplayText(n), "•••••••"; got != want {
		t.Errorf("DisplayText = %q, want %q", got, want)
	}
	if got, want := DisplayPrefix(n, 3), "•••"; got != want {
		t.Errorf("DisplayPrefix(3) = %q, want %q", got, want)
	}
	n.Preedit = "ab"
	if got, want := DisplayPreedit(n), "••"; got != want {
		t.Errorf("DisplayPreedit = %q, want %q", got, want)
	}
}

func TestDisplayHelpersPassThroughWhenUnmasked(t *testing.T) {
	n := NewField("visible").Node("Search")
	if got := DisplayText(n); got != "visible" {
		t.Errorf("DisplayText = %q, want the real value", got)
	}
	if got := DisplayPrefix(n, 3); got != "vis" {
		t.Errorf("DisplayPrefix(3) = %q, want %q", got, "vis")
	}
}

// A cursor outside the value must not panic the painter: it clamps.
func TestDisplayPrefixClampsAnOutOfRangeCursor(t *testing.T) {
	f := NewField("abc")
	f.Masked = true
	n := f.Node("Password")
	if got, want := DisplayPrefix(n, 99), "•••"; got != want {
		t.Errorf("DisplayPrefix(99) = %q, want %q", got, want)
	}
	if got, want := DisplayPrefix(n, -1), "•••"; got != want {
		t.Errorf("DisplayPrefix(-1) = %q, want %q", got, want)
	}
}

// Newlines survive the disguise, so a masked multiline field still breaks
// where it should instead of collapsing into one row of bullets.
func TestMaskPreservesNewlines(t *testing.T) {
	f := NewField("a\nb")
	f.Masked = true
	if got, want := DisplayText(f.Node("x")), "•\n•"; got != want {
		t.Errorf("DisplayText = %q, want %q", got, want)
	}
}
