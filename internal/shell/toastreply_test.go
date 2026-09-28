package shell

import (
	"testing"

	"github.com/Nomadcxx/sysc-notify/protocol"
	"github.com/Nomadcxx/sysc-shell/internal/platform/wayland"
	"github.com/Nomadcxx/sysc-shell/internal/ui"
)

// replyToast shows one inline-reply notification, id 1, on eDP-1.
func replyToast(t *testing.T) (*Registry, *toastHost, *fakeNotifySender) {
	t.Helper()
	r, h, sender := wiredToast(t)
	keepInvalidationsDrained(t, r)
	n := note(1, "Maya")
	n.InlineReply = true
	r.applyNotify(snap(1, n))
	return r, h, sender
}

// clickReplyField presses and releases the centre of card 1's reply field.
func clickReplyField(t *testing.T, r *Registry, h *toastHost) {
	t.Helper()
	r.mu.Lock()
	var x, y float64
	found := false
	for _, card := range h.cards["eDP-1"] {
		if f := findAction(card.root, "notify:1:reply"); f != nil {
			x = float64(card.rect.X + f.Bounds.X + f.Bounds.W/2)
			y = float64(card.rect.Y + f.Bounds.Y + f.Bounds.H/2)
			found = true
		}
	}
	r.mu.Unlock()
	if !found {
		t.Fatal("card 1 has no reply field")
	}
	h.handle("eDP-1", wayland.Event{Kind: wayland.EventPointerEnter, X: x, Y: y})
	h.handle("eDP-1", wayland.Event{Kind: wayland.EventPointerPress, X: x, Y: y})
	h.handle("eDP-1", wayland.Event{Kind: wayland.EventPointerRelease, X: x, Y: y})
}

func typeText(h *toastHost, s string) {
	for _, r := range s {
		h.handle("eDP-1", wayland.Event{Kind: wayland.EventKeyPress, Sym: uint32(r), Text: string(r)})
	}
}

func pressSym(h *toastHost, sym uint32) {
	h.handle("eDP-1", wayland.Event{Kind: wayland.EventKeyPress, Sym: sym})
}

// lastKeyboard is the keyboard mode the host most recently asked for, or false
// when it never asked.
func lastKeyboard(r *Registry, h *toastHost) (uint32, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	updates := h.harness().updates
	for i := len(updates) - 1; i >= 0; i-- {
		if updates[i].Keyboard != nil {
			return *updates[i].Keyboard, true
		}
	}
	return 0, false
}

func replying(r *Registry, h *toastHost) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return h.resolver.replying()
}

// A toast surface takes no keyboard until the reply field is pressed, which
// takes it in place (GH #40).
func TestToastReplyFieldRaisesTheKeyboard(t *testing.T) {
	r, h, _ := replyToast(t)
	if _, asked := lastKeyboard(r, h); asked {
		t.Fatal("the toast asked for the keyboard before any reply began")
	}
	clickReplyField(t, r, h)
	if !replying(r, h) {
		t.Fatal("pressing the reply field did not begin a reply")
	}
	if mode, _ := lastKeyboard(r, h); mode != keyboardExclusive {
		t.Fatalf("keyboard = %d, want exclusive", mode)
	}
}

func TestToastReplyEnterSendsTheText(t *testing.T) {
	r, h, sender := replyToast(t)
	clickReplyField(t, r, h)
	typeText(h, "on my way")

	r.mu.Lock()
	var field *ui.Node
	if cards := h.cards["eDP-1"]; len(cards) > 0 {
		field = findAction(cards[0].root, "notify:1:reply")
	}
	r.mu.Unlock()
	if field == nil || field.Text != "on my way" || !field.Editing {
		t.Fatalf("reply field = %+v, want the typed text, editing", field)
	}

	pressSym(h, ui.SymReturn)
	sent := sender.ofKind(protocol.CommandReply)
	if len(sent) != 1 || sent[0].ID != 1 || sent[0].Text != "on my way" {
		t.Fatalf("replies = %+v, want one for id 1 with the text", sent)
	}
	if replying(r, h) {
		t.Fatal("the reply stayed open after Enter")
	}
	if mode, _ := lastKeyboard(r, h); mode != keyboardNone {
		t.Fatalf("keyboard = %d after the reply, want none", mode)
	}
}

func TestToastReplyEscapeCancelsWithoutSending(t *testing.T) {
	r, h, sender := replyToast(t)
	clickReplyField(t, r, h)
	typeText(h, "draft")
	pressSym(h, ui.SymEscape)
	if got := sender.ofKind(protocol.CommandReply); len(got) != 0 {
		t.Fatalf("Escape sent %+v", got)
	}
	if replying(r, h) {
		t.Fatal("Escape left the reply open")
	}
	if mode, _ := lastKeyboard(r, h); mode != keyboardNone {
		t.Fatalf("keyboard = %d after Escape, want none", mode)
	}
}

func TestToastReplyPointerLeavingAnEmptyReplyCancels(t *testing.T) {
	r, h, _ := replyToast(t)
	clickReplyField(t, r, h)
	h.handle("eDP-1", wayland.Event{Kind: wayland.EventPointerLeave})
	if replying(r, h) {
		t.Fatal("an empty reply stayed open after the pointer left")
	}
	if mode, _ := lastKeyboard(r, h); mode != keyboardNone {
		t.Fatalf("keyboard = %d, want none", mode)
	}
}

// A reply with text survives the pointer leaving, but gives the keyboard
// back: the surface has no shield, so a click on another window never reaches
// the shell, and an exclusive grab would take that window's keystrokes into
// the reply. Clicking the field again resumes it with the text intact.
func TestToastReplyParksWhenThePointerLeavesAndResumesOnClick(t *testing.T) {
	r, h, sender := replyToast(t)
	clickReplyField(t, r, h)
	typeText(h, "ok")
	h.handle("eDP-1", wayland.Event{Kind: wayland.EventPointerLeave})

	if mode, _ := lastKeyboard(r, h); mode != keyboardNone {
		t.Fatalf("keyboard = %d after the pointer left, want none", mode)
	}
	r.mu.Lock()
	view := h.viewFor(1)
	r.mu.Unlock()
	if len(view.hovered) != 0 {
		t.Fatalf("view = %+v, a parked reply must not hold the card", view)
	}
	typeText(h, "ssh") // a stray key while parked changes nothing

	clickReplyField(t, r, h)
	if mode, _ := lastKeyboard(r, h); mode != keyboardExclusive {
		t.Fatalf("keyboard = %d after clicking back in, want exclusive", mode)
	}
	r.mu.Lock()
	view = h.viewFor(1)
	r.mu.Unlock()
	if len(view.hovered) == 0 {
		t.Fatalf("view = %+v, want the resumed reply to hold its card", view)
	}
	typeText(h, "!")
	pressSym(h, ui.SymReturn)
	sent := sender.ofKind(protocol.CommandReply)
	if len(sent) != 1 || sent[0].Text != "ok!" {
		t.Fatalf("replies = %+v, want one with the parked text resumed", sent)
	}
}

// The centre opening suppresses the stack, and the reply goes with its card.
func TestToastReplyEndsWhenTheCentreOpens(t *testing.T) {
	r, h, _ := replyToast(t)
	clickReplyField(t, r, h)
	r.mu.Lock()
	r.setCenterOpen(true)
	r.mu.Unlock()
	if replying(r, h) {
		t.Fatal("the reply outlived its card when the centre opened")
	}
	if mode, _ := lastKeyboard(r, h); mode != keyboardNone {
		t.Fatalf("keyboard = %d, want none", mode)
	}
}

// When the compositor closes the toast surface, the surface and its keyboard
// are already gone: the reply ends without an update to a dead surface.
func TestToastReplyEndsQuietlyWhenTheSurfaceCloses(t *testing.T) {
	r, h, _ := replyToast(t)
	clickReplyField(t, r, h)
	r.mu.Lock()
	before := len(h.harness().updates)
	r.mu.Unlock()
	r.DropAux(5, toastSurfaceID("eDP-1"))
	if replying(r, h) {
		t.Fatal("the reply outlived its surface")
	}
	r.mu.Lock()
	after := len(h.harness().updates)
	r.mu.Unlock()
	if after != before {
		t.Fatalf("closing sent %d updates to a surface that is gone", after-before)
	}
}

func TestToastReplyEndsWhenTheRecordCloses(t *testing.T) {
	r, h, _ := replyToast(t)
	clickReplyField(t, r, h)
	typeText(h, "x")
	r.applyNotify(delta(1, 2, protocol.Delta{Kind: protocol.DeltaClosed, ID: 1}))
	if replying(r, h) {
		t.Fatal("the reply outlived its notification")
	}
	if mode, _ := lastKeyboard(r, h); mode != keyboardNone {
		t.Fatalf("keyboard = %d, want none", mode)
	}
}
