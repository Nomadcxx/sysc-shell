package shell

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/Nomadcxx/sysc-notify/protocol"
	"github.com/Nomadcxx/sysc-shell/internal/ui"
)

func TestNotifyCentrePopulatedLayout(t *testing.T) {
	for _, critical := range []bool{false, true} {
		t.Run(fmt.Sprintf("critical=%v", critical), func(t *testing.T) {
			r := newPanelRegistry(t)
			now := time.Date(2026, 9, 7, 12, 0, 0, 0, time.UTC)
			r.now = now
			n := baseNotification()
			n.Timestamp = now
			n.Summary = strings.Repeat("A long notification summary ", 5)
			if critical {
				n.Urgency = protocol.UrgencyCritical
			}
			msg := snap(1, n)
			msg.Snapshot.History = []protocol.HistoryEntry{{ID: 9, AppName: "Chat", Summary: "An earlier message", Timestamp: now.Add(-time.Hour), Urgency: n.Urgency}}
			r.applyNotify(msg)
			if err := r.OpenPanel(PanelNotifications, 7, Trigger{BarEdge: "top", BarZone: 44, OutW: 1536, OutH: 1440}); err != nil {
				t.Fatal(err)
			}
			panel := drainAux(t, r, 2)[1].Open
			w, h := int(panel.Width), int(panel.Height)
			if err := panel.Callbacks.Configure(w, h, 120); err != nil {
				t.Fatal(err)
			}
			root := r.panelHosts[PanelNotifications].root
			for _, text := range findAllKind(root, ui.KindText) {
				if text.Bounds.W <= 0 || text.Bounds.H <= 0 {
					t.Errorf("text %q has no bounds: %+v", text.Text, text.Bounds)
				}
			}
			for _, name := range []string{"Do not disturb", "Schedule", "Clear all", "Settings", "Close", "Dismiss", "Remove"} {
				button := buttonByName(root, name)
				if button == nil {
					t.Fatalf("missing %s", name)
				}
				b := button.Bounds
				if b.W <= 0 || b.H <= 0 {
					t.Errorf("%s has no bounds: %+v", name, b)
					continue
				}
				if action, _ := ui.Hit(root, b.X+b.W/2, b.Y+b.H/2); action != button.Action {
					t.Errorf("%s hit = %q, want %q", name, action, button.Action)
				}
			}
			for _, card := range findAllKind(root, ui.KindCapsule) {
				if card.Shape != ui.ShapeCard {
					continue
				}
				for _, b := range buttons(card) {
					if b.Action == "notify:7:dismiss" || b.Action == "notify:9:remove" {
						if got, want := b.Bounds.X+b.Bounds.W, card.Bounds.X+card.Bounds.W-card.Padding; got != want {
							t.Errorf("remove right edge = %d, want card edge %d", got, want)
						}
					}
				}
			}
			if err := panel.Callbacks.Render(make([]byte, w*h*4), w, h, w*4); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestNotifyCentreFiltersGroupMembersAcrossMidnight(t *testing.T) {
	now := time.Date(2026, 9, 7, 0, 5, 0, 0, time.UTC)
	for _, filter := range []string{"today", "yesterday", "earlier"} {
		t.Run(filter, func(t *testing.T) {
			r := newPanelRegistry(t)
			r.now = now
			notes := []protocol.Notification{
				{ID: 1, AppName: "Mail", Summary: "today", Timestamp: now},
				{ID: 2, AppName: "Mail", Summary: "yesterday", Timestamp: now.Add(-10 * time.Minute)},
				{ID: 3, AppName: "Mail", Summary: "earlier", Timestamp: now.AddDate(0, 0, -2)},
			}
			r.applyNotify(snap(1, notes...))
			host := &PanelHost{id: PanelNotifications, notifyFilter: filter, notifyExpand: "mail"}
			root := r.centerTreeFor(host)
			for _, n := range notes {
				if got, want := containsText(root, n.Summary), n.Summary == filter; got != want {
					t.Errorf("%s visible = %v, want %v", n.Summary, got, want)
				}
			}
			for _, groupDismiss := range []bool{false, true} {
				sender := &fakeNotifySender{}
				r.notifySender = sender
				if groupDismiss {
					host.activateNotify(r, &ui.Node{Action: "notify:center:dismiss-group:mail"})
				} else {
					r.clearVisible(host)
				}
				cmds := sender.ofKind(protocol.CommandDismiss)
				if len(cmds) != 1 || notes[cmds[0].ID-1].Summary != filter {
					t.Errorf("group=%v dismissed outside %s: %+v", groupDismiss, filter, cmds)
				}
			}
		})
	}
}

func TestNotifyToastKeepsUnfilledCard(t *testing.T) {
	toast := NotificationCard(baseNotification(), nil, nil, false)
	if got := toast.Children[0].Fill; got != ui.FillNone {
		t.Fatalf("toast fill = %v, want FillNone", got)
	}
	now := time.Now()
	live := ActiveGroupCard(activeGroup{members: []protocol.Notification{baseNotification()}}, now, false, nil, false)
	history := HistoryCard(protocol.HistoryEntry{ID: 1, Summary: "history"}, now, nil, false)
	for _, card := range []*ui.Node{live, history} {
		if card.Children[0].Fill != ui.FillContainerHigh {
			t.Fatal("centre card lost its high container fill")
		}
	}
}

func TestNotifyCentreFitsFullHistory(t *testing.T) {
	r := newPanelRegistry(t)
	now := time.Date(2026, 9, 7, 12, 0, 0, 0, time.UTC)
	r.now = now
	msg := snap(1)
	for i := 0; i < protocol.MaxHistoryEntries; i++ {
		msg.Snapshot.History = append(msg.Snapshot.History, protocol.HistoryEntry{
			ID: uint32(i + 1), AppName: "Mail", Summary: "Yesterday's message", Timestamp: now.AddDate(0, 0, -1),
		})
	}
	r.applyNotify(msg)
	if err := r.OpenPanel(PanelNotifications, 7, Trigger{BarEdge: "top", BarZone: 44, OutW: 1536, OutH: 1440}); err != nil {
		t.Fatal(err)
	}
	panel := drainAux(t, r, 2)[1].Open
	for _, scale := range []int{120, 150, 240} {
		if err := panel.Callbacks.Configure(int(panel.Width), int(panel.Height), scale); err != nil {
			t.Errorf("scale %d: %v", scale, err)
		}
	}
}
