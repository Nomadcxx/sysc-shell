package shell

import (
	"testing"
	"time"

	"github.com/Nomadcxx/sysc-notify/protocol"
	"github.com/Nomadcxx/sysc-shell/internal/config"
	"github.com/Nomadcxx/sysc-shell/internal/notifyclient"
)

func note(id uint32, summary string) protocol.Notification {
	return protocol.Notification{
		ID: id, AppName: "App", Summary: summary,
		Timestamp: time.Unix(1_756_000_000, 0), ExpireTimeoutMS: 5000,
	}
}

func snap(seq uint64, notes ...protocol.Notification) notifyclient.Message {
	s := protocol.Snapshot{Sequence: seq, Active: notes}
	for _, n := range notes {
		s.Lifetimes = append(s.Lifetimes, protocol.Lifetime{ID: n.ID, DurationMS: 5000, RemainingMS: 5000, Running: true})
	}
	return notifyclient.Message{Generation: 1, Kind: notifyclient.KindSnapshot, Sequence: seq, Snapshot: s}
}

func delta(generation, seq uint64, d protocol.Delta) notifyclient.Message {
	return notifyclient.Message{Generation: generation, Kind: notifyclient.KindDelta, Sequence: seq, Delta: d}
}

func TestNotificationSnapshotEstablishesTheProjection(t *testing.T) {
	r := NewRegistry(config.Default())
	r.applyNotify(snap(1, note(1, "a"), note(2, "b")))

	if got := r.notifyActiveIDs(); len(got) != 2 {
		t.Fatalf("active = %v, want 2 records", got)
	}
	if r.notifyLifetime(1) == nil || r.notifyLifetime(1).RemainingMS != 5000 {
		t.Fatalf("lifetime for 1 = %+v", r.notifyLifetime(1))
	}
}

func TestNotificationIgnoresAStaleGeneration(t *testing.T) {
	r := NewRegistry(config.Default())
	r.applyNotify(snap(1, note(1, "a")))

	// A delta from an older generation must not touch the projection.
	stale := delta(99, 2, protocol.Delta{Kind: protocol.DeltaAdded, Notification: ptr(note(9, "ghost"))})
	r.applyNotify(stale)
	if got := r.notifyActiveIDs(); len(got) != 1 {
		t.Fatalf("stale generation mutated the projection: %v", got)
	}
}

func TestNotificationAppliesAddReplaceCloseInOrder(t *testing.T) {
	r := NewRegistry(config.Default())
	r.applyNotify(snap(1, note(1, "a")))

	replacement := note(1, "a2")
	r.applyNotify(delta(1, 2, protocol.Delta{
		Kind: protocol.DeltaReplaced, Notification: &replacement,
		Lifetime: &protocol.Lifetime{ID: 1, DurationMS: 9000, RemainingMS: 9000, Running: true},
	}))
	if got := r.notifySummary(1); got != "a2" {
		t.Fatalf("replace left summary %q", got)
	}
	if r.notifyLifetime(1).RemainingMS != 9000 {
		t.Fatal("replace did not adopt the replacement lifetime")
	}

	r.applyNotify(delta(1, 3, protocol.Delta{Kind: protocol.DeltaAdded, Notification: ptr(note(2, "b")),
		Lifetime: &protocol.Lifetime{ID: 2, DurationMS: 5000, RemainingMS: 5000, Running: true}}))
	r.applyNotify(delta(1, 4, protocol.Delta{Kind: protocol.DeltaClosed, ID: 1}))
	if got := r.notifyActiveIDs(); len(got) != 1 || got[0] != 2 {
		t.Fatalf("after close, active = %v", got)
	}
}

func TestNotificationDisconnectDropsTheProjection(t *testing.T) {
	r := NewRegistry(config.Default())
	r.applyNotify(snap(1, note(1, "a")))
	r.applyNotify(notifyclient.Message{Generation: 1, Kind: notifyclient.KindDisconnected})
	if got := r.notifyActiveIDs(); len(got) != 0 {
		t.Fatalf("disconnect left %d records", len(got))
	}
	if r.notifyLifetime(1) != nil {
		t.Fatal("disconnect left a lifetime")
	}
}

func TestNotificationHistoryTracksDeltas(t *testing.T) {
	r := NewRegistry(config.Default())
	r.applyNotify(snap(1))
	entry := protocol.HistoryEntry{ID: 5, AppName: "App", Summary: "closed", Timestamp: time.Unix(1_756_000_000, 0)}
	r.applyNotify(delta(1, 2, protocol.Delta{Kind: protocol.DeltaHistoryAdded, History: &entry}))
	if got := r.notifyHistoryCount(); got != 1 {
		t.Fatalf("history = %d", got)
	}
	r.applyNotify(delta(1, 3, protocol.Delta{Kind: protocol.DeltaHistoryRemoved, IDs: []uint32{5}}))
	if got := r.notifyHistoryCount(); got != 0 {
		t.Fatalf("history after removal = %d", got)
	}
}

func ptr(n protocol.Notification) *protocol.Notification { return &n }
