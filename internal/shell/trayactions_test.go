package shell

import (
	"testing"

	"github.com/Nomadcxx/sysc-shell/internal/config"
	"github.com/Nomadcxx/sysc-shell/internal/trayclient"
	tray "github.com/Nomadcxx/sysc-tray/protocol"
)

// recordSender captures the commands a shell would emit for pointer input.
type recordSender struct {
	sent    []tray.Command
	replies []trayclient.Message // replayed back into the tracker, in order
}

func (s *recordSender) Send(c tray.Command) (uint64, error) {
	s.sent = append(s.sent, c)
	return uint64(len(s.sent)), nil
}

func commandHarness() (*Registry, *recordSender, tray.ItemKey) {
	r := NewRegistry(config.Default())
	key := tray.ItemKey{Owner: "org.x", ObjectPath: "/org/x/1"}
	r.applyTray(trayclient.Message{Generation: 1, Kind: trayclient.KindSnapshot,
		Snapshot: tray.Snapshot{Items: []tray.Item{{Key: key, Title: "Chat"}}}})
	return r, &recordSender{}, key
}

func TestTrayActivateSendsLogicalCoordinates(t *testing.T) {
	r, snd, key := commandHarness()
	r.trayActivate(snd, key, 7, 120, 40)
	if len(snd.sent) != 1 {
		t.Fatalf("sent %d commands", len(snd.sent))
	}
	c := snd.sent[0]
	if c.Kind != tray.CommandActivate || c.Item != key ||
		c.Output != 7 || c.X != 120 || c.Y != 40 {
		t.Fatalf("command = %+v", c)
	}
}

func TestTraySecondaryActivate(t *testing.T) {
	r, snd, key := commandHarness()
	r.trayActivateSecondary(snd, key, 3, 10, 10)
	if snd.sent[0].Kind != tray.CommandSecondaryActivate {
		t.Fatalf("kind = %q", snd.sent[0].Kind)
	}
}

func TestTrayScrollCarriesOrientationAndDelta(t *testing.T) {
	r, snd, key := commandHarness()
	r.trayScroll(snd, key, 2, -120, tray.ScrollVertical)
	c := snd.sent[0]
	if c.Kind != tray.CommandScroll || c.Delta != -120 ||
		c.Orientation != tray.ScrollVertical || c.Output != 2 {
		t.Fatalf("command = %+v", c)
	}
}

// A command on an item that no longer exists must not reach the service:
// the key embeds the generation, so a stale click is inert.
func TestTrayCommandOnADeadItemIsDropped(t *testing.T) {
	r, snd, key := commandHarness()
	r.applyTray(trayclient.Message{Kind: trayclient.KindDisconnected})
	r.trayActivate(snd, key, 1, 1, 1)
	if len(snd.sent) != 0 {
		t.Fatal("a command left for a dead item")
	}
}

// The tracker correlates replies by request ID and forgets a finished
// request, so a second reply for the same ID must not be acted on twice.
func TestTrayReplyIsActedOnOnce(t *testing.T) {
	r, snd, key := commandHarness()
	var retries []tray.ItemKey
	tracker := newTrayReplyTracker(r, func(k tray.ItemKey) { retries = append(retries, k) }, nil)
	r.trayActivate(snd, key, 1, 10, 10)
	tracker.note(1, key)

	fail := trayclient.Message{Kind: trayclient.KindReply, RequestID: 1,
		Reply: tray.Reply{OK: false, Item: key,
			Error: &tray.ProtocolError{Code: tray.ErrorStaleItem, Message: "gone"}}}
	tracker.apply(fail)
	tracker.apply(fail) // replayed
	if len(retries) != 1 {
		t.Fatalf("retries = %d, want exactly one", len(retries))
	}
}

// A stale reply — request ID never sent, or for an item that has since
// moved on — is dropped silently.
func TestTrayUnknownReplyIsDropped(t *testing.T) {
	r, _, key := commandHarness()
	var retries int
	tracker := newTrayReplyTracker(r, func(tray.ItemKey) { retries++ }, nil)
	tracker.apply(trayclient.Message{Kind: trayclient.KindReply, RequestID: 99,
		Reply: tray.Reply{OK: false, Item: key,
			Error: &tray.ProtocolError{Code: tray.ErrorStaleItem}}})
	if retries != 0 {
		t.Fatal("an unknown reply triggered a retry")
	}
}

// Unknown results never retry: only a stale-item error re-reads the current
// key; every other failure is logged and finished.
func TestTrayNonStaleReplyDoesNotRetry(t *testing.T) {
	r, snd, key := commandHarness()
	var retries int
	tracker := newTrayReplyTracker(r, func(tray.ItemKey) { retries++ }, nil)
	r.trayActivate(snd, key, 1, 10, 10)
	tracker.note(1, key)
	tracker.apply(trayclient.Message{Kind: trayclient.KindReply, RequestID: 1,
		Reply: tray.Reply{OK: false, Item: key,
			Error: &tray.ProtocolError{Code: tray.ErrorUnavailable, Message: "away"}}})
	if retries != 0 {
		t.Fatal("an unavailable error retried")
	}
}

// Terminate sends the full item key and marks the item pending until the
// service confirms with a removal.
func TestTrayTerminateSendsCommandAndMarksPending(t *testing.T) {
	r, snd, key := commandHarness()
	r.BindTray(snd)
	r.trayTerminate(key, 7)
	if len(snd.sent) != 1 || snd.sent[0].Kind != tray.CommandTerminate ||
		snd.sent[0].Item != key || snd.sent[0].Output != 7 {
		t.Fatalf("sent = %+v, want one terminate for the full key", snd.sent)
	}
	if !r.tray.isClosing(key) {
		t.Fatal("the termination was not marked pending")
	}
}

// A failed terminate reply restores the item: the pending mark is dropped
// and the item takes commands again.
func TestTrayTerminateFailureRestoresItem(t *testing.T) {
	r, snd, key := commandHarness()
	r.BindTray(snd)
	r.trayTerminate(key, 7)
	r.trayReplies.apply(trayclient.Message{Kind: trayclient.KindReply, RequestID: 1,
		Reply: tray.Reply{OK: false, Item: key,
			Error: &tray.ProtocolError{Code: tray.ErrorBusy, Message: "slow"}}})
	if r.tray.isClosing(key) {
		t.Fatal("a failed terminate left the item pending")
	}
	snd.sent = nil
	r.trayActivate(snd, key, 1, 10, 10)
	if len(snd.sent) != 1 {
		t.Fatal("a restored item refused commands")
	}
}

// A successful terminate keeps the item pending until the removal delta
// arrives; the removal then clears it together with the item.
func TestTrayTerminateSuccessClearsOnRemoval(t *testing.T) {
	r, snd, key := commandHarness()
	r.BindTray(snd)
	r.trayTerminate(key, 7)
	r.trayReplies.apply(trayclient.Message{Kind: trayclient.KindReply, RequestID: 1,
		Reply: tray.Reply{OK: true, Item: key}})
	if !r.tray.isClosing(key) {
		t.Fatal("a successful terminate cleared the pending mark early")
	}
	r.applyTray(trayclient.Message{Generation: 1, Kind: trayclient.KindItemRemoved,
		Removed: tray.ItemRemoved{Key: key}})
	if r.tray.isClosing(key) || r.tray.has(key) {
		t.Fatal("the removal did not clear the pending mark and the item")
	}
}

// While a termination is pending the item is inert: no activation, scroll,
// or menu traffic leaves the shell for it.
func TestTrayClosingItemDropsCommands(t *testing.T) {
	r, snd, key := commandHarness()
	r.BindTray(snd)
	r.trayTerminate(key, 7)
	snd.sent = nil
	r.trayActivate(snd, key, 1, 10, 10)
	r.trayScroll(snd, key, 1, 120, tray.ScrollHorizontal)
	if len(snd.sent) != 0 {
		t.Fatalf("sent %d commands for a closing item", len(snd.sent))
	}
}

// A stale terminate reply cancels the pending close without a retry: the
// generation it named is gone, and re-terminating its replacement was never
// asked.
func TestTrayTerminateStaleReplyCancelsWithoutRetry(t *testing.T) {
	r, _, key := commandHarness()
	var retries, cancels []tray.ItemKey
	tracker := newTrayReplyTracker(r,
		func(k tray.ItemKey) { retries = append(retries, k) },
		func(k tray.ItemKey) { cancels = append(cancels, k) })
	tracker.noteTerminate(1, key)
	tracker.apply(trayclient.Message{Kind: trayclient.KindReply, RequestID: 1,
		Reply: tray.Reply{OK: false, Item: key,
			Error: &tray.ProtocolError{Code: tray.ErrorStaleItem, Message: "gone"}}})
	if len(cancels) != 1 || len(retries) != 0 {
		t.Fatalf("cancels = %d retries = %d, want one cancel and no retry", len(cancels), len(retries))
	}
}
