package shell

import (
	"testing"

	"github.com/Nomadcxx/sysc-shell/internal/ui"
)

// TestScrollBodyPrefersKeyedBodyOverChromeScroll pins the invariant the
// settings rail fix introduced (sysc-1016 follow-up): when a panel root holds
// chrome scrollables beside its content, keyboard paging resolves by key, not
// by tree order. The chrome scroll is built first, so the pre-order fallback
// alone would page the rail instead of the body.
func TestScrollBodyPrefersKeyedBodyOverChromeScroll(t *testing.T) {
	t.Parallel()
	chrome := &ui.Node{Kind: ui.KindScroll}
	body := &ui.Node{Kind: ui.KindScroll, Key: settingsBodyKey}
	root := &ui.Node{Kind: ui.KindRow, Children: []*ui.Node{
		chrome,
		{Kind: ui.KindColumn, Children: []*ui.Node{body}},
	}}
	if got := scrollBody(root); got != body {
		t.Fatalf("scrollBody paged the wrong node: got %p, want keyed body %p", got, body)
	}
	if got := findScroll(root); got != chrome {
		t.Fatalf("pre-order fallback should still land on chrome first: got %p", got)
	}
}

// TestScrollBodyFallsBackToSingleScrollable covers every panel the tall-font
// audit found with exactly one scrollable: launcher results, clipboard list,
// plugin wrapper, audio/bluetooth/network/system popouts. The keyed lookup
// misses and the walk returns the only body.
func TestScrollBodyFallsBackToSingleScrollable(t *testing.T) {
	t.Parallel()
	only := &ui.Node{Kind: ui.KindScroll}
	root := &ui.Node{Kind: ui.KindColumn, Children: []*ui.Node{only}}
	if got := scrollBody(root); got != only {
		t.Fatalf("single scrollable not returned: got %p", got)
	}
	grid := &ui.Node{Kind: ui.KindVirtualList, Key: settingsBodyKey}
	root2 := &ui.Node{Kind: ui.KindColumn, Children: []*ui.Node{grid}}
	if got := scrollBody(root2); got != grid {
		t.Fatalf("keyed virtual list not returned: got %p", got)
	}
}
