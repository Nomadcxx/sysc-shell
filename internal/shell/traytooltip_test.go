package shell

import (
	"strings"

	"github.com/Nomadcxx/sysc-shell/internal/config"
	"testing"

	"github.com/Nomadcxx/sysc-shell/internal/trayclient"
	tray "github.com/Nomadcxx/sysc-tray/protocol"
)

// A service-owned tooltip joins title and description, and the shell side
// clamps to the protocol bounds even though the service already validated.
func TestTrayTooltipFlattensTitleAndDescription(t *testing.T) {
	r := NewRegistry(config.Default())
	key := tray.ItemKey{Owner: "org.x", ObjectPath: "/org/x/1"}
	r.applyTray(trayclient.Message{Generation: 1, Kind: trayclient.KindSnapshot,
		Snapshot: tray.Snapshot{Items: []tray.Item{{
			Key:     key,
			Title:   "Chat",
			Tooltip: tray.Tooltip{Title: "Chat", Description: "Two unread"},
		}}}})
	if got := r.trayTooltipText(key); got != "Chat\nTwo unread" {
		t.Fatalf("tooltip = %q", got)
	}
}

func TestTrayTooltipDescriptionOnlyStillShows(t *testing.T) {
	r := NewRegistry(config.Default())
	key := tray.ItemKey{Owner: "org.x", ObjectPath: "/org/x/1"}
	r.applyTray(trayclient.Message{Generation: 1, Kind: trayclient.KindSnapshot,
		Snapshot: tray.Snapshot{Items: []tray.Item{{
			Key:     key,
			Tooltip: tray.Tooltip{Description: "Only a description"},
		}}}})
	if got := r.trayTooltipText(key); got != "Only a description" {
		t.Fatalf("tooltip = %q", got)
	}
}

// An item change carrying a new tooltip must update the flattened text.
func TestTrayTooltipFollowsItemChanges(t *testing.T) {
	r := NewRegistry(config.Default())
	key := tray.ItemKey{Owner: "org.x", ObjectPath: "/org/x/1"}
	r.applyTray(trayclient.Message{Generation: 1, Kind: trayclient.KindSnapshot,
		Snapshot: tray.Snapshot{Items: []tray.Item{{Key: key,
			Tooltip: tray.Tooltip{Title: "before"}}}}})
	r.applyTray(trayclient.Message{Generation: 1, Kind: trayclient.KindItemChanged,
		Item: tray.Item{Key: key, Tooltip: tray.Tooltip{Title: "after"}}})
	if got := r.trayTooltipText(key); got != "after" {
		t.Fatalf("tooltip = %q", got)
	}
}

// Losing the item hides its tooltip: an empty string cancels the dwell.
func TestTrayItemLossClearsTheTooltip(t *testing.T) {
	r := NewRegistry(config.Default())
	key := tray.ItemKey{Owner: "org.x", ObjectPath: "/org/x/1"}
	r.applyTray(trayclient.Message{Generation: 1, Kind: trayclient.KindSnapshot,
		Snapshot: tray.Snapshot{Items: []tray.Item{{Key: key,
			Tooltip: tray.Tooltip{Title: "present"}}}}})
	r.applyTray(trayclient.Message{Generation: 1, Kind: trayclient.KindItemRemoved,
		Removed: tray.ItemRemoved{Key: key}})
	if got := r.trayTooltipText(key); got != "" {
		t.Fatalf("tooltip survived removal: %q", got)
	}
}

func TestTrayTooltipClampsToTheProtocolBound(t *testing.T) {
	r := NewRegistry(config.Default())
	key := tray.ItemKey{Owner: "org.x", ObjectPath: "/org/x/1"}
	huge := strings.Repeat("x", tray.MaxTooltipBytes*3)
	r.applyTray(trayclient.Message{Generation: 1, Kind: trayclient.KindSnapshot,
		Snapshot: tray.Snapshot{Items: []tray.Item{{Key: key,
			Tooltip: tray.Tooltip{Description: huge}}}}})
	if got := len(r.trayTooltipText(key)); got > tray.MaxTooltipBytes+1 {
		t.Fatalf("tooltip = %d bytes, unbounded", got)
	}
}
