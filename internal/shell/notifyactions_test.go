package shell

import (
	"testing"

	"github.com/Nomadcxx/sysc-notify/protocol"
	"github.com/Nomadcxx/sysc-shell/internal/ui"
)

// resolverHarness captures the commands and lifecycle calls a resolver makes.
type resolverHarness struct {
	commands []resolvedCommand
	focused  []uint32
	opened   []string
	expanded []uint32
}

type resolvedCommand struct {
	kind string
	id   uint32
	key  string
	text string
}

func (h *resolverHarness) invoke(id uint32, key string) {
	h.commands = append(h.commands, resolvedCommand{kind: "action", id: id, key: key})
}
func (h *resolverHarness) dismiss(id uint32) {
	h.commands = append(h.commands, resolvedCommand{kind: "dismiss", id: id})
}
func (h *resolverHarness) reply(id uint32, text string) {
	h.commands = append(h.commands, resolvedCommand{kind: "reply", id: id, text: text})
}
func (h *resolverHarness) hover(id uint32, on bool) {}
func (h *resolverHarness) openLink(href string)     { h.opened = append(h.opened, href) }
func (h *resolverHarness) toggleExpand(id uint32)   { h.expanded = append(h.expanded, id) }

func TestClassifyToastDrag(t *testing.T) {
	const w = 360
	cases := []struct {
		name   string
		dx, dy int
		want   toastGesture
	}{
		{"click", 2, 3, gestureNone},
		{"short left swipe", 60, 0, gestureNone},
		{"committed left swipe", 130, 10, gestureDismiss},
		{"right swipe never dismisses", -200, 0, gestureNone},
		{"down drag expands", 3, 20, gestureExpand},
		{"up drag toggles too", 0, -20, gestureExpand},
		{"under the expand threshold", 0, 11, gestureNone},
		{"mostly vertical beats a 35% horizontal", 130, 140, gestureExpand},
		{"mostly horizontal is today's dismiss", 140, 130, gestureDismiss},
		{"equal diagonal is not dominant", 130, 130, gestureNone},
	}
	for _, tc := range cases {
		if got := classifyToastDrag(tc.dx, tc.dy, w); got != tc.want {
			t.Errorf("%s: %v, want %v", tc.name, got, tc.want)
		}
	}
}

func TestVerticalDragOnACardTogglesExpand(t *testing.T) {
	h := &resolverHarness{}
	r := newNotifyResolver(h)
	root := NotificationCard(protocol.Notification{ID: 7, Summary: "Hello", Body: "World"}, nil, nil, false)
	if err := ui.LayoutColumn(root, ui.Rect{W: 360, H: 120}, func(s string, _ ui.TextAttrs) (int, int) { return len(s) * 8, 16 }); err != nil {
		t.Fatal(err)
	}
	r.press(root, 100, 20)
	r.release(root, 102, 50)
	if len(h.expanded) != 1 || h.expanded[0] != 7 || len(h.commands) != 0 {
		t.Fatalf("expanded %v commands %v", h.expanded, h.commands)
	}
}

func cardFixture() *ui.Node {
	body := &ui.Node{Kind: ui.KindColumn, Action: "notify:7:default", Children: []*ui.Node{
		{Kind: ui.KindText, Text: "summary"},
	}}
	root := &ui.Node{Kind: ui.KindColumn, Children: []*ui.Node{
		body,
		{Kind: ui.KindButton, Text: "One", Action: "notify:7:action:a1", Role: "button", Focusable: true},
	}}
	root.Bounds = ui.Rect{X: 0, Y: 0, W: 360, H: 96}
	body.Bounds = ui.Rect{X: 0, Y: 0, W: 360, H: 60}
	body.Children[0].Bounds = ui.Rect{X: 12, Y: 12, W: 100, H: 16}
	root.Children[1].Bounds = ui.Rect{X: 0, Y: 64, W: 80, H: 24}
	return root
}

func TestResolverInvokesAButtonActionOnMatchedPressRelease(t *testing.T) {
	hh := &resolverHarness{}
	rv := newNotifyResolver(hh)
	root := cardFixture()

	rv.press(root, 20, 70)   // inside the button
	rv.release(root, 20, 70) // release on the same button

	if len(hh.commands) != 1 || hh.commands[0].kind != "action" || hh.commands[0].key != "a1" {
		t.Fatalf("commands = %+v", hh.commands)
	}
	if hh.commands[0].id != 7 {
		t.Fatalf("id = %d", hh.commands[0].id)
	}
}

func TestResolverDismissesACardWithNoDefaultOnClick(t *testing.T) {
	hh := &resolverHarness{}
	rv := newNotifyResolver(hh)
	root := &ui.Node{Kind: ui.KindColumn, Action: "notify:7:dismiss", Children: []*ui.Node{
		{Kind: ui.KindText, Text: "headphones connected"},
	}}
	root.Bounds = ui.Rect{X: 0, Y: 0, W: 360, H: 96}
	root.Children[0].Bounds = ui.Rect{X: 12, Y: 12, W: 200, H: 16}

	rv.press(root, 50, 20)
	rv.release(root, 50, 20)
	if len(hh.commands) != 1 || hh.commands[0].kind != "dismiss" || hh.commands[0].id != 7 {
		t.Fatalf("commands = %+v, want dismiss of 7", hh.commands)
	}
}

func TestResolverInvokesTheDefaultActionOnABodyClick(t *testing.T) {
	hh := &resolverHarness{}
	rv := newNotifyResolver(hh)
	root := cardFixture()

	rv.press(root, 50, 20)
	rv.release(root, 50, 20)
	if len(hh.commands) != 1 || hh.commands[0].kind != "action" || hh.commands[0].key != "default" {
		t.Fatalf("body click commands = %+v", hh.commands)
	}
}

func TestResolverDoesNothingOnMismatchedPressRelease(t *testing.T) {
	hh := &resolverHarness{}
	rv := newNotifyResolver(hh)
	root := cardFixture()

	rv.press(root, 20, 70)  // button
	rv.release(root, 5, 20) // released on the body
	if len(hh.commands) != 0 {
		t.Fatalf("mismatched press/release invoked %+v", hh.commands)
	}
}

func TestResolverSwipeCommitsAtThirtyFivePercent(t *testing.T) {
	hh := &resolverHarness{}
	rv := newNotifyResolver(hh)
	root := cardFixture()

	rv.press(root, 340, 20)
	// 35% of 360 is 126; 130 exceeds it.
	rv.release(root, 340-130, 20)
	if len(hh.commands) != 1 || hh.commands[0].kind != "dismiss" || hh.commands[0].id != 7 {
		t.Fatalf("swipe commands = %+v", hh.commands)
	}
}

func TestResolverSwipeBelowThresholdReturns(t *testing.T) {
	hh := &resolverHarness{}
	rv := newNotifyResolver(hh)
	root := cardFixture()

	rv.press(root, 340, 20)
	rv.release(root, 340-100, 20) // 100 < 126
	if len(hh.commands) != 0 {
		t.Fatalf("short swipe committed %+v", hh.commands)
	}
}

func TestResolverInlineReplySubmitsTextAndCloses(t *testing.T) {
	hh := &resolverHarness{}
	rv := newNotifyResolver(hh)

	if !rv.beginReply(7) {
		t.Fatal("beginReply refused")
	}
	rv.submitReply(7, "on my way")
	if len(hh.commands) != 1 || hh.commands[0].kind != "reply" || hh.commands[0].text != "on my way" {
		t.Fatalf("reply commands = %+v", hh.commands)
	}
	if rv.replying() {
		t.Fatal("submit left the resolver in reply mode")
	}
}

func TestResolverCancelReplyReleasesWithoutCommand(t *testing.T) {
	hh := &resolverHarness{}
	rv := newNotifyResolver(hh)
	rv.beginReply(7)
	rv.cancelReply()
	if len(hh.commands) != 0 {
		t.Fatalf("cancel issued %+v", hh.commands)
	}
	if rv.replying() {
		t.Fatal("cancel left reply mode active")
	}
}

func TestResolverRecordCloseEndsTheReply(t *testing.T) {
	hh := &resolverHarness{}
	rv := newNotifyResolver(hh)
	rv.beginReply(7)
	rv.recordClosed(7)
	if rv.replying() {
		t.Fatal("a closed record left its reply active")
	}
}

func TestResolverOpensALinkThroughTheQualifiedOpener(t *testing.T) {
	hh := &resolverHarness{}
	rv := newNotifyResolver(hh)
	root := &ui.Node{Kind: ui.KindColumn, Children: []*ui.Node{
		{Kind: ui.KindText, Text: "the page", Action: "notify:3:link:https://example.test"},
	}}
	root.Bounds = ui.Rect{W: 360, H: 96}
	root.Children[0].Bounds = ui.Rect{X: 10, Y: 10, W: 100, H: 16}

	rv.press(root, 20, 14)
	rv.release(root, 20, 14)
	if len(hh.opened) != 1 || hh.opened[0] != "https://example.test" {
		t.Fatalf("opened = %v", hh.opened)
	}
	if len(hh.commands) != 0 {
		t.Fatalf("link click issued a protocol command: %+v", hh.commands)
	}
}

func TestResolverDwellSetsHoverAndRenews(t *testing.T) {
	hh := &resolverHarness{}
	rv := newNotifyResolver(hh)
	root := cardFixture()

	rv.hoverAt(root, 50, 20, true)
	rv.hoverAt(root, 50, 20, false)
	// Hover only feeds presentation aggregation; it must not issue commands.
	if len(hh.commands) != 0 {
		t.Fatalf("hover issued %+v", hh.commands)
	}
}

// --- Resolved interaction state ---------------------------------------------

func TestResolverHoverInvalidatesOnlyOnActionChange(t *testing.T) {
	rv := newNotifyResolver(&resolverHarness{})
	root := cardFixture()

	// The action button occupies x 0..80, y 64..88 in the fixture.
	if !rv.hoverAt(root, 20, 70, true) {
		t.Error("entering the action did not report a change")
	}
	if got := rv.pointer.hover; got != "notify:7:action:a1" {
		t.Fatalf("hover = %q, want the action key", got)
	}
	if !root.Children[1].State.Has(ui.StateHovered) {
		t.Error("the resolved tree does not mark the action hovered")
	}
	if rv.hoverAt(root, 30, 75, true) {
		t.Error("motion inside the hovered action reported a change")
	}

	// Off the action, onto the card body, which carries no action of its own.
	if !rv.hoverAt(root, 20, 20, true) {
		t.Error("leaving the action did not report a change")
	}
	if root.Children[1].State.Has(ui.StateHovered) {
		t.Error("the action stayed hovered after the pointer left it")
	}
	if !rv.hoverAt(root, 20, 20, false) {
		t.Error("leaving the card did not report a change")
	}
	if rv.pointer.hover != "" {
		t.Errorf("leave left hover at %q", rv.pointer.hover)
	}
}

func TestResolverPressSetsStateAndReleaseClearsIt(t *testing.T) {
	rv := newNotifyResolver(&resolverHarness{})
	root := cardFixture()

	rv.press(root, 20, 70)
	if got := rv.pointer.press; got != "notify:7:action:a1" {
		t.Fatalf("press = %q, want the action key", got)
	}
	if !root.Children[1].State.Has(ui.StatePressed) {
		t.Error("the resolved tree does not mark the action pressed")
	}
	rv.release(root, 20, 70)
	if rv.pointer.press != "" {
		t.Errorf("release left press at %q", rv.pointer.press)
	}
	if root.Children[1].State.Has(ui.StatePressed) {
		t.Error("the action stayed pressed after release")
	}
}
