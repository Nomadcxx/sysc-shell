package shell

import (
	"fmt"
	"sort"
	"time"

	"github.com/Nomadcxx/sysc-notify/protocol"
	"github.com/Nomadcxx/sysc-shell/internal/ui"
)

// notifyGroup is one center group: the entries of one desktop entry (or app
// name when none is set), newest first.
type notifyGroup struct {
	key     string
	entries []protocol.HistoryEntry
}

// notifyGroups groups closed history by desktop entry then app name, newest
// first. Active records never appear here; the center renders them above the
// groups from the live projection.
func (s *notifyState) groups() []notifyGroup {
	s.mu.Lock()
	history := append([]protocol.HistoryEntry(nil), s.history...)
	s.mu.Unlock()

	byKey := map[string][]protocol.HistoryEntry{}
	order := []string{}
	latest := map[string]time.Time{}
	for _, e := range history {
		key := e.DesktopEntry
		if key == "" {
			key = e.AppName
		}
		if _, ok := byKey[key]; !ok {
			order = append(order, key)
		}
		byKey[key] = append(byKey[key], e)
		if e.Timestamp.After(latest[key]) {
			latest[key] = e.Timestamp
		}
	}
	for _, entries := range byKey {
		sort.Slice(entries, func(i, j int) bool { return entries[i].Timestamp.After(entries[j].Timestamp) })
	}
	sort.Slice(order, func(i, j int) bool { return latest[order[i]].After(latest[order[j]]) })

	groups := make([]notifyGroup, 0, len(order))
	for _, key := range order {
		groups = append(groups, notifyGroup{key: key, entries: byKey[key]})
	}
	return groups
}

// markCenterSeen flags every unread history entry seen and returns the ids
// the caller must report with history.mark-seen. The projection updates
// locally; the service delta confirms.
func (s *notifyState) markSeen() []uint32 {
	s.mu.Lock()
	defer s.mu.Unlock()
	var ids []uint32
	for i := range s.history {
		if !s.history[i].Seen {
			s.history[i].Seen = true
			ids = append(ids, s.history[i].ID)
		}
	}
	return ids
}

// unread counts unseen history entries for the bar badge.
func (s *notifyState) unread() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	n := 0
	for _, e := range s.history {
		if !e.Seen {
			n++
		}
	}
	return n
}

// DND. A preset stores one end time and clears when it passes; permanent DND
// has no end. One timer clears whichever end is set.
func (s *notifyState) setDND(on bool) {
	s.mu.Lock()
	s.dnd = on
	s.dndUntil = time.Time{}
	s.mu.Unlock()
}

func (s *notifyState) setDNDPreset(now time.Time, d time.Duration) {
	s.mu.Lock()
	s.dnd = true
	s.dndUntil = now.Add(d)
	s.mu.Unlock()
}

func (s *notifyState) dndState(now time.Time) (time.Time, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.dnd {
		return time.Time{}, false
	}
	if !s.dndUntil.IsZero() && !now.Before(s.dndUntil) {
		return time.Time{}, false
	}
	return s.dndUntil, true
}

// Registry wrappers.
func (r *Registry) notifyGroups() []notifyGroup { return r.notify.groups() }
func (r *Registry) markCenterSeen() []uint32    { return r.notify.markSeen() }
func (r *Registry) unreadCount() int            { return r.notify.unread() }
func (r *Registry) setDND(on bool)              { r.notify.setDND(on) }
func (r *Registry) setDNDPresetAt(now time.Time, d time.Duration) {
	r.notify.setDNDPreset(now, d)
}
func (r *Registry) dndStateAt(now time.Time) (time.Time, bool) { return r.notify.dndState(now) }

// centerTree builds the notification center: the header controls, the active
// records, then grouped history. History rows have no actions.
func (r *Registry) centerTree() *ui.Node {
	s := r.notify
	s.mu.Lock()
	active := make([]protocol.Notification, 0, len(s.active))
	for _, n := range s.active {
		active = append(active, n)
	}
	lifetimes := make(map[uint32]protocol.Lifetime, len(s.lifetimes))
	for id, lt := range s.lifetimes {
		lifetimes[id] = lt
	}
	s.mu.Unlock()
	sort.Slice(active, func(i, j int) bool { return active[i].ID > active[j].ID })

	children := []*ui.Node{{
		Kind: ui.KindRow, Gap: cardGap, Children: []*ui.Node{
			{Kind: ui.KindButton, Text: "DND", Action: "notify:center:dnd",
				Name: "DND", Role: "button", Focusable: true},
			{Kind: ui.KindButton, Text: "1h", Action: "notify:center:dnd:1h",
				Name: "DND 1h", Role: "button", Focusable: true},
			{Kind: ui.KindButton, Text: "Dismiss all", Action: "notify:center:dismiss-all",
				Name: "Dismiss all", Role: "button", Focusable: true},
			{Kind: ui.KindButton, Text: "Clear history", Action: "notify:center:clear-history",
				Name: "Clear history", Role: "button", Focusable: true},
		},
	}}

	if len(active) == 0 && r.unreadCount() == 0 && len(groups(r.notify)) == 0 {
		children = append(children, &ui.Node{Kind: ui.KindText, Text: "No notifications"})
		return &ui.Node{Kind: ui.KindColumn, Gap: cardGap, Padding: cardPadding, Children: children}
	}

	for _, n := range active {
		children = append(children, NotificationCard(n, cloneLifetime(lifetimes, n.ID), r.linksAllowed()))
	}

	for _, g := range r.notifyGroups() {
		children = append(children, &ui.Node{Kind: ui.KindText, Text: g.key, Bold: true})
		for _, e := range g.entries {
			children = append(children, HistoryCard(e, r.linksAllowed()))
		}
	}

	return &ui.Node{Kind: ui.KindColumn, Gap: cardGap, Padding: cardPadding, Children: children}
}

func groups(s *notifyState) []notifyGroup { return s.groups() }

// cloneLifetime copies a lifetime so a card never aliases the projection's
// map value. A missing lifetime stays missing.
func cloneLifetime(lifetimes map[uint32]protocol.Lifetime, id uint32) *protocol.Lifetime {
	lt, ok := lifetimes[id]
	if !ok {
		return nil
	}
	return &lt
}

// linksAllowed reports the qualified opener capability. Task 10 wires the
// real capability; the center builds with links off until then.
func (r *Registry) linksAllowed() bool { return false }

var _ = fmt.Sprintf
