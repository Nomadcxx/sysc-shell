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

func TestCenterEmptyStateNamesNoNotifications(t *testing.T) {
	r := NewRegistry(config.Default())
	r.applyNotify(snap(1))
	tree := r.centerTree()
	found := false
	for _, s := range texts(tree) {
		if s == "No notifications" {
			found = true
		}
	}
	if !found {
		t.Fatalf("empty center tree lacks the empty state: %v", texts(tree))
	}
}

func TestCenterShowsActiveRecordsAboveHistory(t *testing.T) {
	r := NewRegistry(config.Default())
	r.applyNotify(snap(1, note(1, "live")))
	r.applyNotify(delta(1, 2, protocol.Delta{Kind: protocol.DeltaHistoryAdded,
		History: ptrH(historyEntry(2, "mail", "Mail", "old", time.Unix(1_756_000_000, 0), true))}))
	tree := r.centerTree()
	got := texts(tree)
	live, old := -1, -1
	for i, s := range got {
		if s == "live" {
			live = i
		}
		if s == "old" {
			old = i
		}
	}
	if live < 0 || old < 0 || live > old {
		t.Fatalf("order = %v", got)
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

var _ = ui.Rect{}
