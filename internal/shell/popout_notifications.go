package shell

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/Nomadcxx/sysc-notify/protocol"
	"github.com/Nomadcxx/sysc-shell/internal/icons"
	"github.com/Nomadcxx/sysc-shell/internal/render"
	"github.com/Nomadcxx/sysc-shell/internal/ui"
)

// activeGroup is one Current-tab group: live notifications sharing a
// case-folded desktop entry (or app name when none is set), newest first.
type activeGroup struct {
	key     string
	members []protocol.Notification
}

func activeGroups(active []protocol.Notification) []activeGroup {
	byKey := map[string][]protocol.Notification{}
	order := []string{}
	for _, n := range active {
		key := n.DesktopEntry
		if key == "" {
			key = n.AppName
		}
		key = strings.ToLower(key)
		if _, ok := byKey[key]; !ok {
			order = append(order, key)
		}
		byKey[key] = append(byKey[key], n)
	}
	for _, members := range byKey {
		sort.Slice(members, func(i, j int) bool { return members[i].Timestamp.After(members[j].Timestamp) })
	}
	sort.Slice(order, func(i, j int) bool {
		a, b := byKey[order[i]], byKey[order[j]]
		aCrit, bCrit := groupCritical(a), groupCritical(b)
		if aCrit != bCrit {
			return aCrit
		}
		return a[0].Timestamp.After(b[0].Timestamp)
	})
	out := make([]activeGroup, 0, len(order))
	for _, key := range order {
		out = append(out, activeGroup{key: key, members: byKey[key]})
	}
	return out
}

func groupCritical(members []protocol.Notification) bool {
	for _, n := range members {
		if n.Urgency == protocol.UrgencyCritical {
			return true
		}
	}
	return false
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
func (r *Registry) markCenterSeen() []uint32 { return r.notify.markSeen() }
func (r *Registry) unreadCount() int         { return r.notify.unread() }
func (r *Registry) setDND(on bool)           { r.notify.setDND(on) }
func (r *Registry) setDNDPresetAt(now time.Time, d time.Duration) {
	r.notify.setDNDPreset(now, d)
}
func (r *Registry) dndStateAt(now time.Time) (time.Time, bool) { return r.notify.dndState(now) }

// centerTree builds the notification center: header, tabs, then the selected
// list. Nil host is Current. History rows have no actions.
func (r *Registry) centerTree() *ui.Node { return r.centerTreeFor(nil) }

func (r *Registry) centerTreeFor(h *PanelHost) *ui.Node {
	tab := 0
	if h != nil {
		tab = h.notifyTab
	}

	s := r.notify
	s.mu.Lock()
	active := make([]protocol.Notification, 0, len(s.active))
	for _, n := range s.active {
		active = append(active, n)
	}
	history := append([]protocol.HistoryEntry(nil), s.history...)
	s.mu.Unlock()

	now := r.clockNow()
	_, dnd := r.dndStateAt(now)
	sched, _ := render.IconByName("schedule")
	clearAction := "notify:center:dismiss-all"
	if tab == 1 {
		clearAction = "notify:center:clear-history"
	}
	filter := "all"
	expand := ""
	showMenu := false
	if h != nil {
		if h.notifyFilter != "" {
			filter = h.notifyFilter
		}
		expand = h.notifyExpand
		showMenu = h.notifyMenu
	}

	headerBtns := []*ui.Node{
		{Kind: ui.KindText, Text: "Notifications"},
		{Kind: ui.KindButton, Text: notifyGlyph(dnd), Action: "notify:center:dnd",
			Name: "DND", Role: "button", Focusable: true},
		{Kind: ui.KindButton, Text: string(sched), Action: "notify:center:schedule",
			Name: "Schedule", Role: "button", Focusable: true},
		{Kind: ui.KindButton, Text: "Clear", Action: clearAction,
			Name: "Clear", Role: "button", Focusable: true},
	}
	children := []*ui.Node{
		{Kind: ui.KindRow, Gap: cardGap, Children: headerBtns},
	}
	if showMenu {
		children = append(children, dndPresetColumn())
	}
	children = append(children, &ui.Node{Kind: ui.KindRow, Gap: cardGap, Children: []*ui.Node{
		{Kind: ui.KindButton, Text: fmt.Sprintf("Current (%d)", len(active)),
			Action: "notify:center:tab:0", Name: "Current", Role: "tab",
			Focusable: true, Bold: tab == 0},
		{Kind: ui.KindButton, Text: fmt.Sprintf("History (%d)", len(history)),
			Action: "notify:center:tab:1", Name: "History", Role: "tab",
			Focusable: true, Bold: tab == 1},
	}})

	body := []*ui.Node{}
	if tab == 1 {
		sort.Slice(history, func(i, j int) bool { return history[i].Timestamp.After(history[j].Timestamp) })
		children = append(children, historyChipRow(history, filter, now))
		shown := 0
		for _, e := range history {
			if !historyFilter(filter, e.Timestamp, now) {
				continue
			}
			shown++
			body = append(body, HistoryCard(e, now, r.lookupNotifyIcon(e.AppIcon), r.linksAllowed()))
		}
		if shown == 0 {
			body = append(body, &ui.Node{Kind: ui.KindText, Text: "Nothing to see here"})
		}
	} else if len(active) == 0 {
		body = append(body, &ui.Node{Kind: ui.KindText, Text: "Nothing to see here"})
	} else {
		for _, g := range activeGroups(active) {
			raster := r.lookupNotifyIcon(g.members[0].AppIcon)
			body = append(body, ActiveGroupCard(g, now, expand == g.key, raster, r.linksAllowed()))
		}
	}

	surfaceH := 300
	if h != nil {
		if h.logicalH > 0 {
			surfaceH = h.logicalH
		} else if h.place.Panel.H > 0 {
			surfaceH = h.place.Panel.H
		}
	}
	// ponytail: header+tabs+padding ≈ 80; remainder is the list viewport until chrome is measured.
	listH := surfaceH - 80
	if listH < 1 {
		listH = 1
	}
	children = append(children, &ui.Node{Kind: ui.KindScroll, Height: listH, Gap: cardGap, Children: body})

	return &ui.Node{Kind: ui.KindColumn, Gap: cardGap, Padding: cardPadding, Children: children}
}

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

func (r *Registry) clockNow() time.Time {
	if r != nil && !r.now.IsZero() {
		return r.now
	}
	return time.Now()
}

func (r *Registry) lookupNotifyIcon(name string) *ui.Image {
	if r == nil || r.trayIcons == nil || name == "" {
		return nil
	}
	key := icons.Key{Name: name, Size: cardIconSize}
	if img, ok := r.trayIcons.Lookup(key); ok {
		return img
	}
	_, _, _ = r.trayIcons.Request(key)
	return nil
}

var historyChips = []struct{ id, label string }{
	{"all", "All"},
	{"1h", "Last hour"},
	{"today", "Today"},
	{"yesterday", "Yesterday"},
	{"7d", "Last 7 days"},
	{"older", "Older"},
}

func historyChipRow(history []protocol.HistoryEntry, filter string, now time.Time) *ui.Node {
	showOlder := false
	for _, e := range history {
		if historyFilter("older", e.Timestamp, now) {
			showOlder = true
			break
		}
	}
	row := &ui.Node{Kind: ui.KindRow, Gap: cardGap}
	for _, c := range historyChips {
		if c.id == "older" && !showOlder {
			continue
		}
		row.Children = append(row.Children, &ui.Node{
			Kind: ui.KindButton, Text: c.label, Padding: 4,
			Action: "notify:center:filter:" + c.id, Name: c.label, Role: "button",
			Focusable: true, Bold: filter == c.id,
		})
	}
	return row
}

var dndPresets = []struct {
	label string
	id    string
}{
	{"15 minutes", "15m"},
	{"30 minutes", "30m"},
	{"1 hour", "1h"},
	{"3 hours", "3h"},
	{"8 hours", "8h"},
	{"Tomorrow 08:00", "tomorrow"},
	{"Until turned off", "until"},
}

func dndPresetColumn() *ui.Node {
	col := &ui.Node{Kind: ui.KindColumn, Gap: cardGap}
	for _, p := range dndPresets {
		col.Children = append(col.Children, &ui.Node{
			Kind: ui.KindButton, Text: p.label, Padding: 4,
			Action: "notify:center:preset:" + p.id, Name: p.label, Role: "button",
			Focusable: true,
		})
	}
	return col
}

func dndPresetDuration(id string, now time.Time) (d time.Duration, untilOff bool, ok bool) {
	switch id {
	case "15m":
		return 15 * time.Minute, false, true
	case "30m":
		return 30 * time.Minute, false, true
	case "1h":
		return time.Hour, false, true
	case "3h":
		return 3 * time.Hour, false, true
	case "8h":
		return 8 * time.Hour, false, true
	case "tomorrow":
		t := time.Date(now.Year(), now.Month(), now.Day(), 8, 0, 0, 0, now.Location()).AddDate(0, 0, 1)
		return t.Sub(now), false, true
	case "until":
		return 0, true, true
	}
	return 0, false, false
}
