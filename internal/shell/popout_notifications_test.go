package shell

import (
	"testing"
	"time"

	"github.com/Nomadcxx/sysc-notify/protocol"
	"github.com/Nomadcxx/sysc-shell/internal/config"
	"github.com/Nomadcxx/sysc-shell/internal/ui"
)

func historyEntry(id uint32, desktop, app, summary string, ts time.Time, seen bool) protocol.HistoryEntry {
	return protocol.HistoryEntry{
		ID: id, DesktopEntry: desktop, AppName: app, Summary: summary,
		Timestamp: ts, Seen: seen,
	}
}

func TestCenterHistoryIsFlatNewestFirst(t *testing.T) {
	r := NewRegistry(config.Default())
	r.applyNotify(snap(1))
	older := time.Unix(1_756_000_000, 0)
	newer := older.Add(time.Hour)
	r.applyNotify(delta(1, 2, protocol.Delta{Kind: protocol.DeltaHistoryAdded,
		History: ptrH(historyEntry(1, "mail", "Mail", "old", older, true))}))
	r.applyNotify(delta(1, 3, protocol.Delta{Kind: protocol.DeltaHistoryAdded,
		History: ptrH(historyEntry(2, "mail", "Mail", "new", newer, false))}))
	r.applyNotify(delta(1, 4, protocol.Delta{Kind: protocol.DeltaHistoryAdded,
		History: ptrH(historyEntry(3, "chat", "Chat", "ping", newer.Add(time.Minute), false))}))

	h := &PanelHost{id: PanelNotifications}
	tree := r.centerTreeFor(h)
	got := texts(tree)
	ping, neu, old := -1, -1, -1
	for i, s := range got {
		switch s {
		case "ping":
			if ping < 0 {
				ping = i
			}
		case "new":
			if neu < 0 {
				neu = i
			}
		case "old":
			if old < 0 {
				old = i
			}
		}
	}
	if ping < 0 || neu < 0 || old < 0 {
		t.Fatalf("history texts = %v", got)
	}
	if !(ping < neu && neu < old) {
		t.Fatalf("order ping=%d new=%d old=%d in %v", ping, neu, old, got)
	}
	if buttonByName(tree, "All") == nil || buttonByName(tree, "Today") == nil {
		t.Fatalf("chips missing: %v", texts(tree))
	}
	if !historyRemoveSupported() {
		for _, b := range buttons(tree) {
			if b.Name == "Close" {
				t.Fatal("history painted close before history.remove exists")
			}
		}
	}
}

func TestActiveGroupsMailCountAndCriticalFirst(t *testing.T) {
	older := time.Unix(1_756_000_000, 0)
	newer := older.Add(time.Hour)
	chat := protocol.Notification{
		ID: 1, AppName: "Chat", Summary: "ping",
		Urgency: protocol.UrgencyNormal, Timestamp: newer.Add(time.Minute),
	}
	mailOld := protocol.Notification{
		ID: 2, AppName: "Mail", DesktopEntry: "MAIL", Summary: "old",
		Urgency: protocol.UrgencyNormal, Timestamp: older,
	}
	mailCrit := protocol.Notification{
		ID: 3, AppName: "Mail", DesktopEntry: "mail", Summary: "crit",
		Urgency: protocol.UrgencyCritical, Timestamp: newer,
	}

	got := activeGroups([]protocol.Notification{chat, mailOld, mailCrit})
	if len(got) != 2 {
		t.Fatalf("groups = %+v", got)
	}
	if got[0].key != "mail" {
		t.Fatalf("first group = %q, want mail (critical)", got[0].key)
	}
	if len(got[0].members) != 2 {
		t.Fatalf("mail count = %d, want 2", len(got[0].members))
	}
	if got[0].members[0].Summary != "crit" {
		t.Fatalf("mail order = %+v", got[0].members)
	}
	if got[1].key != "chat" {
		t.Fatalf("second group = %q, want chat (app name)", got[1].key)
	}
}

func TestCentreHeaderCarriesFiveCircularButtons(t *testing.T) {
	r := NewRegistry(config.Default())
	r.applyNotify(snap(1))
	tree := r.centerTreeFor(nil)

	for _, action := range []string{
		"notify:center:dnd", "notify:center:schedule", "notify:center:clear",
		"notify:center:settings", "notify:center:close",
	} {
		n := findAction(tree, action)
		if n == nil {
			t.Fatalf("missing action %s", action)
		}
		if n.Shape != ui.ShapeCircle {
			t.Fatalf("%s shape = %v, want circle", action, n.Shape)
		}
		if n.Fill != ui.FillContainerHighest {
			t.Fatalf("%s fill = %v, want ContainerHighest", action, n.Fill)
		}
		if n.Name == "" || n.Role != "button" {
			t.Fatalf("%s is not addressable: name=%q role=%q", action, n.Name, n.Role)
		}
	}
	for _, gone := range []string{"notify:center:tab:0", "notify:center:tab:1", "notify:center:clear-history"} {
		if findAction(tree, gone) != nil {
			t.Fatalf("retired action %s is still in the tree", gone)
		}
	}
}

func TestCenterEmptyStateNamesNothingToSeeHere(t *testing.T) {
	r := NewRegistry(config.Default())
	r.applyNotify(snap(1))
	tree := r.centerTree()
	if !containsText(tree, "Nothing to see here") {
		t.Fatalf("empty center tree lacks the empty state: %v", texts(tree))
	}
}

func TestMergedListPinsLiveAboveClosed(t *testing.T) {
	now := time.Date(2026, 9, 7, 15, 0, 0, 0, time.Local)
	r := NewRegistry(config.Default())
	r.now = now
	live := note(1, "live")
	live.Timestamp = now.Add(-time.Minute)
	r.applyNotify(snap(1, live))
	r.applyNotify(delta(1, 2, protocol.Delta{Kind: protocol.DeltaHistoryAdded,
		History: ptrH(historyEntry(2, "mail", "Mail", "old", now.Add(-2*time.Hour), true))}))

	if got := sectionLabels(r.centerTreeFor(nil)); len(got) != 2 || got[0] != "LIVE" || got[1] != "EARLIER" {
		t.Fatalf("labels = %v, want [LIVE EARLIER]", got)
	}
}

func TestEmptySectionEmitsNoLabel(t *testing.T) {
	now := time.Date(2026, 9, 7, 15, 0, 0, 0, time.Local)
	r := NewRegistry(config.Default())
	r.now = now
	r.applyNotify(snap(1))
	r.applyNotify(delta(1, 2, protocol.Delta{Kind: protocol.DeltaHistoryAdded,
		History: ptrH(historyEntry(2, "mail", "Mail", "old", now.Add(-2*time.Hour), true))}))

	if got := sectionLabels(r.centerTreeFor(nil)); len(got) != 1 || got[0] != "EARLIER" {
		t.Fatalf("labels = %v, want [EARLIER] only", got)
	}
}

func TestCardsSitOnTheHighContainer(t *testing.T) {
	r := NewRegistry(config.Default())
	r.applyNotify(snap(1))
	r.applyNotify(delta(1, 2, protocol.Delta{Kind: protocol.DeltaHistoryAdded,
		History: ptrH(historyEntry(2, "mail", "Mail", "old", r.clockNow(), true))}))

	card := firstCardCapsule(r.centerTreeFor(nil))
	if card == nil {
		t.Fatal("no card capsule")
	}
	if card.Fill != ui.FillContainerHigh {
		t.Fatalf("card fill = %v, want ContainerHigh", card.Fill)
	}
}

func TestCenterClearActionIsAlwaysClear(t *testing.T) {
	r := NewRegistry(config.Default())
	r.applyNotify(snap(1))
	h := &PanelHost{id: PanelNotifications}
	tree := r.centerTreeFor(h)
	clear := buttonByName(tree, "Clear")
	if clear == nil || clear.Action != "notify:center:clear" {
		t.Fatalf("clear = %+v", clear)
	}
}

func TestCenterActivateClearSetsLastAction(t *testing.T) {
	r := NewRegistry(config.Default())
	r.applyNotify(snap(1))
	h := &PanelHost{id: PanelNotifications}
	r.rebuildPanel(h)
	for i, n := range h.focus {
		if n.Name == "Clear" {
			h.roving.Set(i)
			h.activate(r)
			break
		}
	}
	if h.lastAction != "notify:center:clear" {
		t.Fatalf("lastAction = %q", h.lastAction)
	}
}

func TestClearVisibleSpansOnlyTheOpenFilter(t *testing.T) {
	now := time.Date(2026, 9, 7, 15, 0, 0, 0, time.Local)
	r := NewRegistry(config.Default())
	sender := &fakeNotifySender{}
	r.notifySender = sender
	r.now = now
	r.applyNotify(snap(1))
	r.applyNotify(delta(1, 2, protocol.Delta{Kind: protocol.DeltaHistoryAdded,
		History: ptrH(historyEntry(1, "mail", "Mail", "today", now.Add(-time.Hour), true))}))
	r.applyNotify(delta(1, 3, protocol.Delta{Kind: protocol.DeltaHistoryAdded,
		History: ptrH(historyEntry(2, "mail", "Mail", "old", now.AddDate(0, 0, -5), true))}))

	r.clearVisible(&PanelHost{notifyFilter: "today"})

	got := sender.ofKind(protocol.CommandHistoryRemove)
	if len(got) != 1 {
		t.Fatalf("history.remove = %+v", sender.cmds)
	}
	for _, id := range got[0].IDs {
		if id == 2 {
			t.Fatal("cleared an entry outside the open filter")
		}
	}
	if len(got[0].IDs) != 1 || got[0].IDs[0] != 1 {
		t.Fatalf("removed = %v, want [1]", got[0].IDs)
	}
}

func TestCenterScheduleShowsDurationPresets(t *testing.T) {
	r := NewRegistry(config.Default())
	r.applyNotify(snap(1))
	h := &PanelHost{id: PanelNotifications}
	r.rebuildPanel(h)
	for i, n := range h.focus {
		if n.Name == "Schedule" {
			h.roving.Set(i)
			h.activate(r)
			break
		}
	}
	if buttonByName(h.root, "1 hour") == nil || buttonByName(h.root, "Until turned off") == nil {
		t.Fatalf("presets missing: %v", texts(h.root))
	}
}

func TestCenterPresetOneHourMatchesSetDNDPresetAt(t *testing.T) {
	r := NewRegistry(config.Default())
	now := time.Unix(1_756_000_000, 0)
	r.now = now
	r.applyNotify(snap(1))
	h := &PanelHost{id: PanelNotifications, notifyMenu: true}
	r.rebuildPanel(h)
	for i, n := range h.focus {
		if n.Action == "notify:center:preset:1h" {
			h.roving.Set(i)
			h.activate(r)
			break
		}
	}
	end, on := r.dndStateAt(now)
	if !on || !end.Equal(now.Add(time.Hour)) {
		t.Fatalf("end = %v on=%v, want now+1h", end, on)
	}
}

func TestCenterDNDGlyphSwapsWhenOn(t *testing.T) {
	r := NewRegistry(config.Default())
	r.applyNotify(snap(1))
	r.setDND(true)
	dnd := buttonByName(r.centerTree(), "Do not disturb")
	if dnd == nil || len(dnd.Children) == 0 || dnd.Children[0].Icon != "do_not_disturb_on" {
		t.Fatalf("DND on = %+v", dnd)
	}
}

func TestCenterOpeningMarksShownEntriesSeen(t *testing.T) {
	r := NewRegistry(config.Default())
	r.applyNotify(snap(1))
	r.applyNotify(delta(1, 2, protocol.Delta{Kind: protocol.DeltaHistoryAdded,
		History: ptrH(historyEntry(2, "mail", "Mail", "old", time.Unix(1_756_000_000, 0), false))}))

	ids := r.markCenterSeen()
	if len(ids) != 1 || ids[0] != 2 {
		t.Fatalf("mark-seen ids = %v", ids)
	}
	if r.unreadCount() != 0 {
		t.Fatalf("unread after seen = %d", r.unreadCount())
	}
}

func TestUnreadBadgeCountsUnseenHistory(t *testing.T) {
	r := NewRegistry(config.Default())
	r.applyNotify(snap(1))
	if r.unreadCount() != 0 {
		t.Fatal("unread before any history")
	}
	r.applyNotify(delta(1, 2, protocol.Delta{Kind: protocol.DeltaHistoryAdded,
		History: ptrH(historyEntry(1, "mail", "Mail", "a", time.Unix(1_756_000_000, 0), false))}))
	r.applyNotify(delta(1, 3, protocol.Delta{Kind: protocol.DeltaHistoryAdded,
		History: ptrH(historyEntry(2, "mail", "Mail", "b", time.Unix(1_756_000_000, 0), true))}))
	if got := r.unreadCount(); got != 1 {
		t.Fatalf("unread = %d, want 1", got)
	}
}

func TestDNDBoundsPresetsToOneEndTime(t *testing.T) {
	r := NewRegistry(config.Default())
	now := time.Unix(1_756_000_000, 0)
	r.setDNDPresetAt(now, time.Hour)

	end, on := r.dndStateAt(now)
	if !on {
		t.Fatal("preset did not enable DND")
	}
	if !end.Equal(now.Add(time.Hour)) {
		t.Fatalf("end = %v, want now+1h", end)
	}
	// The preset clears through its end time.
	if _, on = r.dndStateAt(now.Add(2 * time.Hour)); on {
		t.Fatal("preset did not clear after its end")
	}
	// A second preset replaces the first.
	r.setDNDPresetAt(now, 2*time.Hour)
	end, _ = r.dndStateAt(now)
	if !end.Equal(now.Add(2 * time.Hour)) {
		t.Fatalf("second preset end = %v", end)
	}
}

func TestDNDPermanentHasNoEnd(t *testing.T) {
	r := NewRegistry(config.Default())
	r.setDND(true)
	end, on := r.dndStateAt(time.Now())
	if !on || !end.IsZero() {
		t.Fatalf("permanent DND end = %v on=%v, want zero", end, on)
	}
	r.setDND(false)
	if _, on = r.dndStateAt(time.Now()); on {
		t.Fatal("DND stayed on after clear")
	}
}

func ptrH(e protocol.HistoryEntry) *protocol.HistoryEntry { return &e }

func containsText(n *ui.Node, want string) bool {
	for _, s := range texts(n) {
		if s == want {
			return true
		}
	}
	return false
}

func buttonByName(tree *ui.Node, name string) *ui.Node {
	for _, b := range buttons(tree) {
		if b.Name == name {
			return b
		}
	}
	return nil
}

func TestBucketCountSpansActiveAndHistory(t *testing.T) {
	now := time.Date(2026, 9, 7, 15, 0, 0, 0, time.Local)
	active := []protocol.Notification{{ID: 1, Timestamp: now.Add(-time.Minute)}}
	history := []protocol.HistoryEntry{
		{ID: 2, Timestamp: now.Add(-3 * time.Hour)},
		{ID: 3, Timestamp: now.AddDate(0, 0, -1)},
		{ID: 4, Timestamp: now.AddDate(0, 0, -5)},
	}
	for bucket, want := range map[string]int{"all": 4, "today": 2, "yesterday": 1, "earlier": 1} {
		if got := bucketCount(bucket, active, history, now); got != want {
			t.Fatalf("bucketCount(%q) = %d, want %d", bucket, got, want)
		}
	}
}

func TestFilterRowIsOneSegmentedControl(t *testing.T) {
	now := time.Date(2026, 9, 7, 15, 0, 0, 0, time.Local)
	history := []protocol.HistoryEntry{{ID: 1, Timestamp: now.Add(-time.Hour)}}

	row := centreFilterRow(nil, history, "today", now, 392)
	if row.Kind != ui.KindSegmented {
		t.Fatalf("kind = %v, want segmented", row.Kind)
	}
	if len(row.Children) != 4 {
		t.Fatalf("segments = %d, want 4", len(row.Children))
	}
	if !row.Children[1].State.Has(ui.StateSelected) {
		t.Fatal("Today is not marked selected")
	}
	if row.Children[0].State.Has(ui.StateSelected) {
		t.Fatal("All is selected while the filter is today")
	}
	if got, want := row.Children[1].Children[0].Text, "Today (1)"; got != want {
		t.Fatalf("label = %q, want %q", got, want)
	}
}

func buttonByAction(tree *ui.Node, action string) *ui.Node {
	for _, b := range buttons(tree) {
		if b.Action == action {
			return b
		}
	}
	return nil
}

func sectionLabels(n *ui.Node) []string {
	var out []string
	var walk func(*ui.Node)
	walk = func(n *ui.Node) {
		if n == nil {
			return
		}
		if n.Kind == ui.KindText && (n.Text == "LIVE" || n.Text == "EARLIER") {
			out = append(out, n.Text)
		}
		for _, c := range n.Children {
			walk(c)
		}
	}
	walk(n)
	return out
}

func firstCardCapsule(n *ui.Node) *ui.Node {
	var caps []*ui.Node
	collectByKind(n, ui.KindCapsule, &caps)
	for _, c := range caps {
		if c.Shape == ui.ShapeCard {
			return c
		}
	}
	return nil
}

var _ = ui.Rect{}
