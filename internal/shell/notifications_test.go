package shell

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/Nomadcxx/sysc-notify/protocol"
	"github.com/Nomadcxx/sysc-shell/internal/config"
	"github.com/Nomadcxx/sysc-shell/internal/notifyclient"
	v1 "github.com/Nomadcxx/sysc-shell/plugin/v1"
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

// The daemon confirms history.mark-seen with a history-seen delta, and a
// second shell or client marking entries seen arrives the same way (GH #39).
func TestNotificationHistorySeenDeltaMarksOnlyThoseEntries(t *testing.T) {
	r := NewRegistry(config.Default())
	m := snap(1)
	m.Snapshot.History = []protocol.HistoryEntry{
		{ID: 1, AppName: "App", Summary: "one", Timestamp: time.Unix(1_756_000_000, 0)},
		{ID: 2, AppName: "App", Summary: "two", Timestamp: time.Unix(1_756_000_001, 0)},
	}
	r.applyNotify(m)
	if got := r.unreadCount(); got != 2 {
		t.Fatalf("unread before = %d, want 2", got)
	}

	r.applyNotify(delta(1, 2, protocol.Delta{Kind: protocol.DeltaHistorySeen, IDs: []uint32{1}}))

	r.notify.mu.Lock()
	seen := map[uint32]bool{}
	for _, e := range r.notify.history {
		seen[e.ID] = e.Seen
	}
	r.notify.mu.Unlock()
	if !seen[1] || seen[2] {
		t.Fatalf("seen = %v, want entry 1 only", seen)
	}
	if got := r.unreadCount(); got != 1 {
		t.Fatalf("unread after = %d, want 1", got)
	}
}

func ptr(n protocol.Notification) *protocol.Notification { return &n }

type pluginToastRecorder struct {
	mu   sync.Mutex
	seen []protocol.Command
	next uint64
	err  error
}

func (s *pluginToastRecorder) Send(protocol.Command) (uint64, error) { return 0, nil }

func (s *pluginToastRecorder) SendProducer(c protocol.Command) (uint64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.err != nil {
		return 0, s.err
	}
	s.next++
	s.seen = append(s.seen, c)
	return s.next, nil
}

func (s *pluginToastRecorder) commands() []protocol.Command {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]protocol.Command(nil), s.seen...)
}

func TestPluginNotifyPublishesReplacingFreeToasts(t *testing.T) {
	r := NewRegistry(config.Default())
	rec := &pluginToastRecorder{}
	r.BindNotifications(rec)

	if _, err := r.PluginNotify(context.Background(), v1.NotifyParams{
		Summary: "Pinged Pixel", Urgency: v1.UrgencyNormal, TimeoutMS: 2500,
	}); err != nil {
		t.Fatalf("PluginNotify: %v", err)
	}
	res, err := r.PluginNotify(context.Background(), v1.NotifyParams{
		Summary: "Ringing", Body: "find me", Urgency: v1.UrgencyCritical,
	})
	if err != nil {
		t.Fatalf("PluginNotify: %v", err)
	}

	got := rec.commands()
	if len(got) != 2 {
		t.Fatalf("commands = %d, want 2", len(got))
	}
	first, second := got[0].Producer, got[1].Producer
	if first == nil || second == nil {
		t.Fatal("producer request missing")
	}
	if got[0].Kind != protocol.CommandProducerPublish {
		t.Fatalf("kind = %v", got[0].Kind)
	}
	if first.Key == second.Key {
		t.Fatalf("keys collide: %s", first.Key)
	}
	if first.Urgency != protocol.UrgencyNormal || first.ExpireTimeoutMS != 2500 {
		t.Fatalf("first = %+v", first)
	}
	// TimeoutMS 0 must map to the server default (-1), never persistent (0).
	if second.Urgency != protocol.UrgencyCritical || second.ExpireTimeoutMS != -1 {
		t.Fatalf("second = %+v", second)
	}
	if second.Summary != "Ringing" || second.Body != "find me" {
		t.Fatalf("text lost: %+v", second)
	}
	if res.ID != 2 {
		t.Fatalf("reply ID = %d, want 2", res.ID)
	}
}

func TestPluginNotifyWithoutClientReportsUnavailable(t *testing.T) {
	r := NewRegistry(config.Default())
	if _, err := r.PluginNotify(context.Background(), v1.NotifyParams{Summary: "x"}); err == nil {
		t.Fatal("want error before BindNotifications")
	}
}

// A plugin call cancelled before its toast is sent posts nothing (GH #43).
func TestPluginNotifyPostsNothingOnceCancelled(t *testing.T) {
	r := NewRegistry(config.Default())
	rec := &pluginToastRecorder{}
	r.BindNotifications(rec)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := r.PluginNotify(ctx, v1.NotifyParams{Summary: "late"}); !errors.Is(err, context.Canceled) {
		t.Fatalf("PluginNotify = %v, want context.Canceled", err)
	}
	if got := rec.commands(); len(got) != 0 {
		t.Fatalf("a cancelled call posted %+v", got)
	}
}
