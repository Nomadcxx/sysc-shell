package shell

import (
	"os"
	"path/filepath"
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

func TestEmptyNotificationCentreShowsIconAndMutedCopy(t *testing.T) {
	r := newPanelRegistry(t)
	r.mu.Lock()
	tree := r.centerTree()
	r.mu.Unlock()
	if containsText(tree, "Nothing to see here") || !containsText(tree, "No notifications") {
		t.Fatalf("empty centre copy: %v", texts(tree))
	}
	mutedIcon, mutedCopy := false, false
	var walk func(*ui.Node)
	walk = func(n *ui.Node) {
		if n == nil {
			return
		}
		if n.Kind == ui.KindIcon && n.Icon == "notifications" && n.Tone == ui.ToneSubtle {
			mutedIcon = true
		}
		if n.Kind == ui.KindText && n.Text == "No notifications" && n.Tone == ui.ToneSubtle {
			mutedCopy = true
		}
		for _, child := range n.Children {
			walk(child)
		}
	}
	walk(tree)
	if !mutedIcon || !mutedCopy {
		t.Fatalf("empty centre lacks muted icon or copy: icon=%v copy=%v", mutedIcon, mutedCopy)
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

func TestCenterClearAllActionIsAlwaysClear(t *testing.T) {
	r := NewRegistry(config.Default())
	r.applyNotify(snap(1))
	h := &PanelHost{id: PanelNotifications}
	tree := r.centerTreeFor(h)
	clear := buttonByName(tree, "Clear all")
	if clear == nil || clear.Action != "notify:center:clear" {
		t.Fatalf("clear = %+v", clear)
	}
}

func TestCenterClearSendsDismissAllThenHistoryClear(t *testing.T) {
	r := NewRegistry(config.Default())
	sender := &fakeNotifySender{}
	r.notifySender = sender
	r.applyNotify(snap(1, note(7, "active")))
	r.applyNotify(delta(1, 2, protocol.Delta{Kind: protocol.DeltaHistoryAdded,
		History: ptrH(historyEntry(8, "mail", "Mail", "closed", time.Unix(1_756_000_000, 0), true))}))

	h := &PanelHost{id: PanelNotifications}
	if !h.activateNotify(r, &ui.Node{Action: "notify:center:clear"}) {
		t.Fatal("clear action was not handled")
	}
	if got := sender.cmds; len(got) != 2 || got[0].Kind != protocol.CommandDismissAll || got[1].Kind != protocol.CommandHistoryClear {
		t.Fatalf("clear commands = %+v, want dismiss-all then history.clear", got)
	}
}

func TestCenterActivateClearAllSetsLastAction(t *testing.T) {
	r := NewRegistry(config.Default())
	r.applyNotify(snap(1))
	h := &PanelHost{id: PanelNotifications}
	r.rebuildPanel(h)
	for i, n := range h.focus {
		if n.Name == "Clear all" {
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

func TestCenterUnseenIDsLeavesTheProjectionAlone(t *testing.T) {
	r := NewRegistry(config.Default())
	r.applyNotify(snap(1))
	r.applyNotify(delta(1, 2, protocol.Delta{Kind: protocol.DeltaHistoryAdded,
		History: ptrH(historyEntry(2, "mail", "Mail", "old", time.Unix(1_756_000_000, 0), false))}))

	ids := r.notify.unseenIDs()
	if len(ids) != 1 || ids[0] != 2 {
		t.Fatalf("unseen ids = %v", ids)
	}
	if r.unreadCount() != 1 {
		t.Fatalf("reading the unseen ids changed unread to %d", r.unreadCount())
	}
}

// openCentreWithUnseen opens the notification centre over one unseen history
// entry, id 2, with the given command sender.
func openCentreWithUnseen(t *testing.T, sender *fakeNotifySender) *Registry {
	t.Helper()
	r := newPanelRegistry(t)
	r.notifySender = sender
	r.applyNotify(snap(1))
	r.applyNotify(delta(1, 2, protocol.Delta{Kind: protocol.DeltaHistoryAdded,
		History: ptrH(historyEntry(2, "mail", "Mail", "old", time.Unix(1_756_000_000, 0), false))}))
	if err := r.OpenPanel(PanelNotifications, 7, Trigger{
		BarEdge: "top", BarZone: 44, OutW: 1536, OutH: 1440,
	}); err != nil {
		t.Fatal(err)
	}
	return r
}

// Opening the centre while the daemon is unreachable must not clear the badge:
// the daemon still holds those entries unseen and would restore them on the
// next snapshot (GH #38).
func TestCentreOpenKeepsUnreadWhenMarkSeenFails(t *testing.T) {
	r := openCentreWithUnseen(t, &fakeNotifySender{fail: true})
	if got := r.unreadCount(); got != 1 {
		t.Fatalf("unread = %d after a failed mark-seen, want 1", got)
	}
}

// The daemon's history-seen delta is what clears the badge.
func TestCentreOpenClearsUnreadWhenTheDaemonConfirms(t *testing.T) {
	sender := &fakeNotifySender{}
	r := openCentreWithUnseen(t, sender)
	sent := sender.ofKind(protocol.CommandHistoryMarkSeen)
	if len(sent) != 1 || len(sent[0].IDs) != 1 || sent[0].IDs[0] != 2 {
		t.Fatalf("mark-seen commands = %+v, want one for id 2", sent)
	}
	if got := r.unreadCount(); got != 1 {
		t.Fatalf("unread = %d before the daemon confirmed, want 1", got)
	}
	r.applyNotify(delta(1, 3, protocol.Delta{Kind: protocol.DeltaHistorySeen, IDs: []uint32{2}}))
	if got := r.unreadCount(); got != 0 {
		t.Fatalf("unread = %d after the confirmation, want 0", got)
	}
}

// A history entry that arrives while the centre is open must be marked seen
// too: the user is looking at it, and the badge must not come back until the
// centre is reopened (GH #50).
func TestCentreOpenMarksNewHistorySeen(t *testing.T) {
	sender := &fakeNotifySender{}
	r := openCentreWithUnseen(t, sender)
	r.applyNotify(delta(1, 3, protocol.Delta{Kind: protocol.DeltaHistoryAdded,
		History: ptrH(historyEntry(3, "chat", "Chat", "new", time.Unix(1_756_000_100, 0), false))}))
	sent := sender.ofKind(protocol.CommandHistoryMarkSeen)
	if len(sent) != 2 {
		t.Fatalf("mark-seen commands = %+v, want a second after the new entry", sent)
	}
	if len(sent[1].IDs) != 2 || sent[1].IDs[0] != 2 || sent[1].IDs[1] != 3 {
		t.Fatalf("second mark-seen = %v, want ids [2 3]", sent[1].IDs)
	}
	if got := r.unreadCount(); got != 2 {
		t.Fatalf("unread = %d before the daemon confirmed, want 2", got)
	}
	r.applyNotify(delta(1, 4, protocol.Delta{Kind: protocol.DeltaHistorySeen, IDs: []uint32{2, 3}}))
	if got := r.unreadCount(); got != 0 {
		t.Fatalf("unread = %d after the confirmation, want 0", got)
	}
}

// The bar badge reads unread history, so a notify message that changes it
// must repaint the bar rather than wait for the next clock tick.
func TestNotifyMessageRepaintsTheBarBadge(t *testing.T) {
	r := newPanelRegistry(t)
	withTestBar(t, r, 7, config.Default())
	r.applyNotify(snap(1))
	for len(r.invalidations) > 0 {
		<-r.invalidations
	}

	r.applyNotify(delta(1, 2, protocol.Delta{Kind: protocol.DeltaHistoryAdded,
		History: ptrH(historyEntry(2, "mail", "Mail", "old", time.Unix(1_756_000_000, 0), false))}))

	select {
	case inv := <-r.invalidations:
		if inv.Global != 7 || inv.SurfaceID != "" {
			t.Fatalf("invalidation = %+v, want the bar on global 7", inv)
		}
	default:
		t.Fatal("an unread change did not repaint the bar")
	}
}

func TestNotificationRebuildKeepsEveryDescendantInsideThePanel(t *testing.T) {
	r := newPanelRegistry(t)
	r.applyNotify(snap(1))
	if err := r.OpenPanel(PanelNotifications, 7, Trigger{
		BarEdge: "top", BarZone: 44, OutW: 1536, OutH: 1440,
	}); err != nil {
		t.Fatal(err)
	}
	panel := drainAux(t, r, 2)[1].Open
	if err := panel.Callbacks.Configure(int(panel.Width), int(panel.Height), 120); err != nil {
		t.Fatal(err)
	}

	r.applyNotify(delta(1, 2, protocol.Delta{
		Kind: protocol.DeltaAdded, Notification: ptr(note(9, "incoming")),
		Lifetime: &protocol.Lifetime{ID: 9, DurationMS: 5000, RemainingMS: 5000, Running: true},
	}))

	h := r.panelHosts[PanelNotifications]
	contentBottom := h.root.Bounds.Y + h.root.Bounds.H - h.root.Padding
	var walk func(*ui.Node)
	walk = func(n *ui.Node) {
		if n == nil {
			return
		}
		if n != h.root && n.Bounds.Y+n.Bounds.H > contentBottom {
			t.Errorf("%v bottom %d exceeds panel content bottom %d", n.Kind, n.Bounds.Y+n.Bounds.H, contentBottom)
		}
		for _, child := range n.Children {
			walk(child)
		}
	}
	walk(h.root)
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
	if got, want := row.Children[1].Children[0].Text, "Today 1"; got != want {
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

func TestDesktopEntryIconResolvesIconKeysFromApplicationsDirs(t *testing.T) {
	root := t.TempDir()
	t.Setenv("XDG_DATA_HOME", root)
	t.Setenv("XDG_DATA_DIRS", "")
	dir := filepath.Join(root, "applications")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	contents := "[Desktop Entry]\nName=Firefox\nIcon=firefox\n"
	if err := os.WriteFile(filepath.Join(dir, "firefox.desktop"), []byte(contents), 0o644); err != nil {
		t.Fatal(err)
	}

	if got := desktopEntryIcon("firefox"); got != "firefox" {
		t.Fatalf("entry icon = %q, want firefox", got)
	}
	if got := desktopEntryIcon("absent"); got != "" {
		t.Fatalf("absent entry = %q, want empty", got)
	}
	if got := desktopEntryIcon("no/separator"); got != "" {
		t.Fatalf("separator entry = %q, want empty", got)
	}
}

// The centre's header glyph is frozen when the tree is built; an expiring
// timed preset must rebuild an open centre instead of leaving the DND icon
// lit. gh #56.
func TestCenterDNDGlyphRefreshesWhenAPresetExpiresWhileOpen(t *testing.T) {
	r := NewRegistry(config.Default())
	r.applyNotify(snap(1))
	now := time.Unix(1_756_000_000, 0)
	r.now = now
	r.setDNDPresetAt(now, time.Minute)
	h := &PanelHost{id: PanelNotifications, notifyMenu: true}
	r.mu.Lock()
	r.panelHosts[PanelNotifications] = h
	r.rebuildPanel(h)
	r.mu.Unlock()
	before := buttonByName(h.root, "Do not disturb")
	if before == nil || len(before.Children) == 0 || before.Children[0].Icon != "do_not_disturb_on" {
		t.Fatalf("preset DND glyph = %+v, want do_not_disturb_on", before)
	}

	r.UpdateClock(now.Add(2 * time.Minute))

	after := buttonByName(h.root, "Do not disturb")
	if after == nil || len(after.Children) == 0 || after.Children[0].Icon != "notifications" {
		t.Fatalf("stale DND glyph after expiry: %+v", after)
	}
}
