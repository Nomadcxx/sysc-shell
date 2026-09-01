package shell

import (
	"sync"
	"time"

	"github.com/Nomadcxx/sysc-notify/protocol"
	"github.com/Nomadcxx/sysc-shell/internal/notifyclient"
)

// notifyState is the shell's projection of service-owned notifications. It
// lives under Registry.mu; only the Wayland owner applies messages. The shell
// never invents expiry: lifetimes arrive with snapshots and deltas and are
// refreshed by renew replies.
type notifyState struct {
	mu         sync.Mutex
	generation uint64
	active     map[uint32]protocol.Notification
	lifetimes  map[uint32]protocol.Lifetime
	history    []protocol.HistoryEntry

	// outputs is the set of configured connector names the projection
	// projects to. Zero outputs means everything is suppressed.
	outputs    []string
	dnd        bool
	dndUntil   time.Time
	centerOpen bool
}

func newNotifyState() *notifyState {
	return &notifyState{
		active:    make(map[uint32]protocol.Notification),
		lifetimes: make(map[uint32]protocol.Lifetime),
	}
}

// applyNotify applies one immutable client message. Messages from a stale
// generation are discarded; a disconnect drops everything so a reconnecting
// snapshot starts clean.
func (s *notifyState) applyNotify(m notifyclient.Message) {
	s.mu.Lock()
	defer s.mu.Unlock()

	switch m.Kind {
	case notifyclient.KindSnapshot:
		s.generation = m.Generation
		s.active = make(map[uint32]protocol.Notification, len(m.Snapshot.Active))
		s.lifetimes = make(map[uint32]protocol.Lifetime, len(m.Snapshot.Lifetimes))
		for _, n := range m.Snapshot.Active {
			s.active[n.ID] = n
		}
		for _, lt := range m.Snapshot.Lifetimes {
			s.lifetimes[lt.ID] = lt
		}
		s.history = append(s.history[:0], m.Snapshot.History...)

	case notifyclient.KindDelta:
		if m.Generation != s.generation {
			return
		}
		d := m.Delta
		switch d.Kind {
		case protocol.DeltaAdded, protocol.DeltaReplaced:
			if d.Notification != nil {
				s.active[d.Notification.ID] = *d.Notification
			}
			if d.Lifetime != nil {
				s.lifetimes[d.Lifetime.ID] = *d.Lifetime
			}
		case protocol.DeltaClosed:
			delete(s.active, d.ID)
			delete(s.lifetimes, d.ID)
		case protocol.DeltaHistoryAdded:
			if d.History != nil {
				s.history = append(s.history, *d.History)
			}
		case protocol.DeltaHistoryRemoved:
			for _, id := range d.IDs {
				for i, e := range s.history {
					if e.ID == id {
						s.history = append(s.history[:i], s.history[i+1:]...)
						break
					}
				}
			}
		case protocol.DeltaHistoryCleared:
			s.history = s.history[:0]
		}

	case notifyclient.KindReply:
		// Renew replies refresh the authoritative lifetimes in place.
		if m.Generation != s.generation {
			return
		}
		for _, lt := range m.Reply.Lifetimes {
			s.lifetimes[lt.ID] = lt
		}

	case notifyclient.KindDisconnected:
		s.generation = 0
		s.active = make(map[uint32]protocol.Notification)
		s.lifetimes = make(map[uint32]protocol.Lifetime)
		s.history = s.history[:0]
	}
}

func (s *notifyState) activeIDs() []uint32 {
	s.mu.Lock()
	defer s.mu.Unlock()
	ids := make([]uint32, 0, len(s.active))
	for id := range s.active {
		ids = append(ids, id)
	}
	return ids
}

func (s *notifyState) lifetime(id uint32) *protocol.Lifetime {
	s.mu.Lock()
	defer s.mu.Unlock()
	lt, ok := s.lifetimes[id]
	if !ok {
		return nil
	}
	return &lt
}

func (s *notifyState) summary(id uint32) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.active[id].Summary
}

func (s *notifyState) historyCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.history)
}

// Registry-facing wrappers. The registry owns one notifyState; tests reach
// through these rather than the lock directly.
func (r *Registry) applyNotify(m notifyclient.Message) { r.notify.applyNotify(m) }
func (r *Registry) notifyActiveIDs() []uint32          { return r.notify.activeIDs() }
func (r *Registry) notifyLifetime(id uint32) *protocol.Lifetime {
	return r.notify.lifetime(id)
}
func (r *Registry) notifySummary(id uint32) string { return r.notify.summary(id) }
func (r *Registry) notifyHistoryCount() int        { return r.notify.historyCount() }
