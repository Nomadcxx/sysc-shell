package shell

import (
	"strings"
	"testing"
	"time"

	"github.com/Nomadcxx/sysc-notify/protocol"
	"github.com/Nomadcxx/sysc-shell/internal/config"
	"github.com/Nomadcxx/sysc-shell/internal/render"
	"github.com/Nomadcxx/sysc-shell/internal/ui"
)

func historyEntry(id uint32, desktop, app, summary string, ts time.Time, seen bool) protocol.HistoryEntry {
	return protocol.HistoryEntry{
		ID: id, DesktopEntry: desktop, AppName: app, Summary: summary,
		Timestamp: ts, Seen: seen,
	}
}

func TestCenterGroupsHistoryNewestFirst(t *testing.T) {
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

	groups := r.notifyGroups()
	if len(groups) != 2 {
		t.Fatalf("groups = %+v", groups)
	}
	// chat's entry is newer, so its group leads.
	if groups[0].key != "chat" {
		t.Fatalf("first group = %q, want chat (newest entry)", groups[0].key)
	}
	if len(groups[1].entries) != 2 || groups[1].entries[0].Summary != "new" {
		t.Fatalf("mail group order = %+v", groups[1].entries)
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

func TestCenterHeaderHasTitleDNDScheduleClearAndTabs(t *testing.T) {
	r := NewRegistry(config.Default())
	r.applyNotify(snap(1))
	tree := r.centerTree()

	if !containsText(tree, "Notifications") {
		t.Fatalf("title missing: %v", texts(tree))
	}
	dnd := buttonByName(tree, "DND")
	if dnd == nil || dnd.Action != "notify:center:dnd" || dnd.Text != notifyGlyph(false) {
		t.Fatalf("DND = %+v", dnd)
	}
	schedRune, _ := render.IconByName("schedule")
	sched := buttonByName(tree, "Schedule")
	if sched == nil || sched.Text != string(schedRune) {
		t.Fatalf("schedule = %+v", sched)
	}
	clear := buttonByName(tree, "Clear")
	if clear == nil || clear.Action != "notify:center:dismiss-all" || clear.Text != "Clear" {
		t.Fatalf("clear = %+v", clear)
	}
	cur := buttonByAction(tree, "notify:center:tab:0")
	if cur == nil || cur.Role != "tab" || cur.Text != "Current (0)" {
		t.Fatalf("current tab = %+v", cur)
	}
	hist := buttonByAction(tree, "notify:center:tab:1")
	if hist == nil || hist.Role != "tab" || hist.Text != "History (0)" {
		t.Fatalf("history tab = %+v", hist)
	}
	for _, b := range buttons(tree) {
		switch b.Text {
		case "1h", "Dismiss all", "Clear history":
			t.Fatalf("stub header button still present: %+v", b)
		}
		if b.Name == "Settings" || strings.Contains(strings.ToLower(b.Name), "keyboard") {
			t.Fatalf("unwanted header button: %+v", b)
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

func TestCenterCurrentTabShowsLiveNotHistory(t *testing.T) {
	r := NewRegistry(config.Default())
	r.applyNotify(snap(1, note(1, "live")))
	r.applyNotify(delta(1, 2, protocol.Delta{Kind: protocol.DeltaHistoryAdded,
		History: ptrH(historyEntry(2, "mail", "Mail", "old", time.Unix(1_756_000_000, 0), true))}))
	tree := r.centerTree()
	got := texts(tree)
	if !containsText(tree, "live") {
		t.Fatalf("current tab lacks live: %v", got)
	}
	if containsText(tree, "old") {
		t.Fatalf("current tab listed history: %v", got)
	}
	cur := buttonByAction(tree, "notify:center:tab:0")
	hist := buttonByAction(tree, "notify:center:tab:1")
	if cur == nil || cur.Text != "Current (1)" {
		t.Fatalf("current count = %+v", cur)
	}
	if hist == nil || hist.Text != "History (1)" {
		t.Fatalf("history count = %+v", hist)
	}
}

func TestCenterClearActionFollowsTab(t *testing.T) {
	r := NewRegistry(config.Default())
	r.applyNotify(snap(1))
	h := &PanelHost{id: PanelNotifications}
	tree := r.centerTreeFor(h)
	clear := buttonByName(tree, "Clear")
	if clear == nil || clear.Action != "notify:center:dismiss-all" {
		t.Fatalf("tab 0 clear = %+v", clear)
	}
	h.notifyTab = 1
	tree = r.centerTreeFor(h)
	clear = buttonByName(tree, "Clear")
	if clear == nil || clear.Action != "notify:center:clear-history" {
		t.Fatalf("tab 1 clear = %+v", clear)
	}
}

func TestCenterTabActivateSelectsHistory(t *testing.T) {
	r := NewRegistry(config.Default())
	r.applyNotify(snap(1))
	r.applyNotify(delta(1, 2, protocol.Delta{Kind: protocol.DeltaHistoryAdded,
		History: ptrH(historyEntry(2, "mail", "Mail", "old", time.Unix(1_756_000_000, 0), true))}))
	h := &PanelHost{id: PanelNotifications}
	r.rebuildPanel(h)
	found := false
	for i, n := range h.focus {
		if n.Action == "notify:center:tab:1" {
			h.roving.Set(i)
			h.activate(r)
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("history tab not focusable: %v", focusableNames(h.root))
	}
	if h.notifyTab != 1 {
		t.Fatalf("notifyTab = %d, want 1", h.notifyTab)
	}
	if !containsText(h.root, "old") {
		t.Fatalf("history tab lacks history: %v", texts(h.root))
	}
	if containsText(h.root, "Nothing to see here") {
		t.Fatalf("history tab showed current empty copy: %v", texts(h.root))
	}
	clear := buttonByName(h.root, "Clear")
	if clear == nil || clear.Action != "notify:center:clear-history" {
		t.Fatalf("rebuilt clear = %+v", clear)
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
	if h.lastAction != "notify:center:dismiss-all" {
		t.Fatalf("lastAction = %q", h.lastAction)
	}
}

func TestCenterDNDGlyphSwapsWhenOn(t *testing.T) {
	r := NewRegistry(config.Default())
	r.applyNotify(snap(1))
	r.setDND(true)
	dnd := buttonByName(r.centerTree(), "DND")
	if dnd == nil || dnd.Text != notifyGlyph(true) {
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

func buttonByAction(tree *ui.Node, action string) *ui.Node {
	for _, b := range buttons(tree) {
		if b.Action == action {
			return b
		}
	}
	return nil
}

var _ = ui.Rect{}
