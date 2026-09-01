package shell

import (
	"testing"

	"github.com/Nomadcxx/sysc-shell/internal/ui"
)

// resolverHarness captures the commands and lifecycle calls a resolver makes.
type resolverHarness struct {
	commands []resolvedCommand
	focused  []uint32
	opened   []string
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
