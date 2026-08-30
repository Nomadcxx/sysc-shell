package ui

import "testing"

func TestFocusOrderIsTreeOrder(t *testing.T) {
	t.Parallel()
	root := samplePanelTree()
	f := Focusables(root)
	if len(f) != 4 || f[0].Text != "Lock" {
		names := make([]string, len(f))
		for i, n := range f {
			names[i] = n.Text
		}
		t.Fatalf("focus order wrong: %v", names)
	}
}

func TestRovingIndexWrapsAndClamps(t *testing.T) {
	t.Parallel()
	r := &Roving{Count: 3}
	r.Next()
	r.Next()
	r.Next()
	if r.Index() != 0 {
		t.Fatal("must wrap")
	}
	r.Prev()
	if r.Index() != 2 {
		t.Fatal("must wrap back")
	}
	r.Set(99)
	if r.Index() != 2 {
		t.Fatalf("Set must clamp, got %d", r.Index())
	}
	r.Set(-4)
	if r.Index() != 0 {
		t.Fatalf("Set must clamp low, got %d", r.Index())
	}
}
