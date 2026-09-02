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

func TestFocusablesWalksVirtualListItem(t *testing.T) {
	t.Parallel()
	root := &Node{
		Kind:      KindVirtualList,
		ItemCount: 3,
		Item: func(i int) *Node {
			return &Node{Kind: KindButton, Text: string(rune('a' + i)), Focusable: true}
		},
	}
	f := Focusables(root)
	if len(f) != 3 || f[0].Text != "a" || f[2].Text != "c" {
		t.Fatalf("virtual list focus = %d %+v", len(f), f)
	}
}

func TestFocusablesSkipsInertVirtualListItems(t *testing.T) {
	t.Parallel()
	calls := 0
	root := &Node{
		Kind: KindColumn,
		Children: []*Node{
			{Kind: KindTextField, Focusable: true, Name: "Search"},
			{
				Kind: KindVirtualList, ItemCount: 200, ItemHeight: 48,
				Item: func(int) *Node {
					calls++
					return &Node{Kind: KindText, Text: "x"}
				},
			},
		},
	}
	f := Focusables(root)
	if len(f) != 1 || f[0].Name != "Search" {
		t.Fatalf("focus = %d %+v, want only Search", len(f), f)
	}
	if calls > 8 {
		t.Fatalf("instantiated %d inert rows, want a short probe", calls)
	}
}
