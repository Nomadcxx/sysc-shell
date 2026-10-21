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
	tracker := newTrayReplyTracker()
	r.trayActivate(snd, key, 1, 10, 10)
	tracker.note(1, key)

	fail := trayclient.Message{Kind: trayclient.KindReply, RequestID: 1,
		Reply: tray.Reply{OK: false, Item: key,
			Error: &tray.ProtocolError{Code: tray.ErrorStaleItem, Message: "gone"}}}
	got, stale := tracker.apply(fail)
	if !stale {
		t.Fatal("a stale reply did not ask for a reprojection")
	}
	if got != key {
		t.Fatalf("key = %+v, want %+v", got, key)
	}
	_, stale = tracker.apply(fail) // replayed
	if stale {
		t.Fatal("a replayed reply asked for a second reprojection")
	}
}

// A stale reply — request ID never sent, or for an item that has since
// moved on — is dropped silently.
func TestTrayUnknownReplyIsDropped(t *testing.T) {
	_, _, key := commandHarness()
	tracker := newTrayReplyTracker()
	got, stale := tracker.apply(trayclient.Message{Kind: trayclient.KindReply, RequestID: 99,
		Reply: tray.Reply{OK: false, Item: key,
			Error: &tray.ProtocolError{Code: tray.ErrorStaleItem}}})
	if stale {
		t.Fatal("an unknown reply asked for a reprojection")
	}
	if got != (tray.ItemKey{}) {
		t.Fatalf("key = %+v, want the zero key", got)
	}
}

// Unknown results never ask for a reprojection: only a stale-item error
// re-reads the current key; every other failure is logged and finished.
func TestTrayNonStaleReplyDoesNotRetry(t *testing.T) {
	r, snd, key := commandHarness()
	tracker := newTrayReplyTracker()
	r.trayActivate(snd, key, 1, 10, 10)
	tracker.note(1, key)
	_, stale := tracker.apply(trayclient.Message{Kind: trayclient.KindReply, RequestID: 1,
		Reply: tray.Reply{OK: false, Item: key,
			Error: &tray.ProtocolError{Code: tray.ErrorUnavailable, Message: "away"}}})
	if stale {
		t.Fatal("an unavailable error asked for a reprojection")
	}
}
