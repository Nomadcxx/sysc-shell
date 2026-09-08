package shell

import (
	"fmt"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/Nomadcxx/sysc-notify/protocol"
	"github.com/Nomadcxx/sysc-shell/internal/icons"
	"github.com/Nomadcxx/sysc-shell/internal/theme"
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

// centerTree builds the notification centre: a header block, then one
// scrolled list with live groups pinned above closed history.
func (r *Registry) centerTree() *ui.Node { return r.centerTreeFor(nil) }

func (r *Registry) centerTreeFor(h *PanelHost) *ui.Node {
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

	size := panelTargetSize(PanelNotifications)
	surfaceW, surfaceH := size.W, size.H
	if h != nil {
		if h.place.Panel.W > 0 {
			surfaceW = h.place.Panel.W
		}
		if h.logicalH > 0 {
			surfaceH = h.logicalH
		} else if h.place.Panel.H > 0 {
			surfaceH = h.place.Panel.H
		}
	}
	// Filter sits inside the header capsule, which itself sits in the root
	// column: both carry cardPadding, so the row's box is four pads shy of the
	// surface. Setting Width past that squeezes "Yesterday (N)" out of its slot
	// and the compositor tears the panel down.
	innerW := surfaceW - 4*cardPadding
	if innerW < 1 {
		innerW = 1
	}

	header := &ui.Node{
		Kind: ui.KindCapsule, Fill: ui.FillContainerHigh, Shape: ui.ShapeLarge,
		Padding: cardPadding, Children: []*ui.Node{
			{Kind: ui.KindColumn, Gap: cardGap, Children: []*ui.Node{
				centreHeaderRow(dnd),
				centreFilterRow(active, history, filter, now, innerW),
			}},
		},
	}
	children := []*ui.Node{header}
	if showMenu {
		children = append(children, dndPresetColumn())
	}

	sort.Slice(history, func(i, j int) bool { return history[i].Timestamp.After(history[j].Timestamp) })

	var live, closed []*ui.Node
	active = slices.DeleteFunc(active, func(n protocol.Notification) bool {
		return !historyFilter(filter, n.Timestamp, now)
	})
	for _, g := range activeGroups(active) {
		raster := r.lookupNotifyIcon(g.members[0].AppIcon)
		live = append(live, ActiveGroupCard(g, now, expand == g.key, raster, r.linksAllowed()))
	}
	for _, e := range history {
		if !historyFilter(filter, e.Timestamp, now) {
			continue
		}
		closed = append(closed, HistoryCard(e, now, r.lookupNotifyIcon(e.AppIcon), r.linksAllowed()))
	}

	body := []*ui.Node{}
	if len(live) > 0 {
		body = append(body, centreSectionLabel("LIVE"))
		body = append(body, live...)
	}
	if len(closed) > 0 {
		body = append(body, centreSectionLabel("EARLIER"))
		body = append(body, closed...)
	}
	if len(body) == 0 {
		body = append(body, &ui.Node{Kind: ui.KindText, Text: "Nothing to see here"})
	}

	scroll := &ui.Node{Kind: ui.KindScroll, Height: 1, Gap: cardGap, Children: body}
	children = append(children, scroll)
	root := &ui.Node{Kind: ui.KindColumn, Gap: cardGap, Padding: cardPadding, Children: children}
	fitNotificationBody(root, scroll, surfaceW, surfaceH, h)
	return root
}

func fitNotificationBody(root, scroll *ui.Node, width, height int, h *PanelHost) {
	if root == nil || scroll == nil {
		return
	}
	measure := func(s string, _ ui.TextAttrs) (int, int) { return len(s) * 8, 16 }
	if h != nil {
		measure = h.measureText()
	}
	fixed := 2*root.Padding + max(len(root.Children)-1, 0)*root.Gap
	innerW := max(width-2*root.Padding, 0)
	for _, child := range root.Children {
		if child == nil || child == scroll {
			continue
		}
		childH, err := ui.ContentHeight(child, innerW, measure)
		if err != nil {
			continue
		}
		fixed += childH
	}
	scroll.Height = max(height-fixed, 1)
}

func centreSectionLabel(text string) *ui.Node {
	return &ui.Node{Kind: ui.KindText, Text: text,
		TextRole: theme.RoleCaption, Tone: ui.ToneAccent}
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
	key := icons.Square(name, cardIconSize)
	if img, ok := r.trayIcons.Lookup(key); ok {
		return img
	}
	_, _, _ = r.trayIcons.Request(key)
	return nil
}

// centreIconButton is one circular control in the centre's header. The glyph
// carries no fill of its own; the button around it resolves one.
func centreIconButton(icon, action, name string) *ui.Node {
	return &ui.Node{
		Kind: ui.KindButton, Action: action, Name: name, Role: "button",
		Focusable: true, Shape: ui.ShapeCircle, Fill: ui.FillContainerHighest,
		Padding:  centreIconPad,
		Children: []*ui.Node{{Kind: ui.KindIcon, Icon: icon, IconSize: centreIconSize}},
	}
}

func centreHeaderRow(dnd bool) *ui.Node {
	title := &ui.Node{Kind: ui.KindRow, Gap: cardGap, Children: []*ui.Node{
		{Kind: ui.KindIcon, Icon: "notifications", IconSize: centreIconSize, Tone: ui.ToneAccent},
		{Kind: ui.KindText, Text: "Notifications", TextRole: theme.RoleHeadline},
	}}
	dndIcon := "notifications"
	if dnd {
		dndIcon = "do_not_disturb_on"
	}
	controls := &ui.Node{Kind: ui.KindRow, Gap: cardGap, Children: []*ui.Node{
		centreIconButton(dndIcon, "notify:center:dnd", "Do not disturb"),
		centreIconButton("schedule", "notify:center:schedule", "Schedule"),
		centreIconButton("delete", "notify:center:clear", "Clear"),
		centreIconButton("settings", "notify:center:settings", "Settings"),
		centreIconButton("close", "notify:center:close", "Close"),
	}}
	return &ui.Node{Kind: ui.KindRow, Gap: cardGap, PinEnd: true,
		Children: []*ui.Node{title, controls}}
}

var historyChips = []struct{ id, label string }{
	{"all", "All"},
	{"today", "Today"},
	{"yesterday", "Yesterday"},
	{"earlier", "Earlier"},
}

// bucketCount is how many entries one filter segment would show. Active
// notifications are counted with the closed ones because the merged list shows
// them together: a segment whose number disagrees with its list is a defect.
func bucketCount(bucket string, active []protocol.Notification, history []protocol.HistoryEntry, now time.Time) int {
	n := 0
	for _, a := range active {
		if historyFilter(bucket, a.Timestamp, now) {
			n++
		}
	}
	for _, e := range history {
		if historyFilter(bucket, e.Timestamp, now) {
			n++
		}
	}
	return n
}

// centreFilterRow is the one control selecting what the list shows. It replaced
// a tab row plus a six-chip row: two controls for one question.
func centreFilterRow(active []protocol.Notification, history []protocol.HistoryEntry, filter string, now time.Time, width int) *ui.Node {
	segments := make([]*ui.Node, 0, len(historyChips))
	for _, c := range historyChips {
		seg := &ui.Node{
			Kind: ui.KindButton, Action: "notify:center:filter:" + c.id,
			Name: c.label, Role: "tab", Focusable: true, Padding: 2,
			Children: []*ui.Node{{Kind: ui.KindText, TextRole: theme.RoleCaption,
				Text: fmt.Sprintf("%s %d", c.label, bucketCount(c.id, active, history, now))}},
		}
		if filter == c.id {
			seg.State |= ui.StateSelected
		}
		segments = append(segments, seg)
	}
	return &ui.Node{Kind: ui.KindSegmented, Key: "notify-filter", Gap: 2,
		Width: width, Children: segments}
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
