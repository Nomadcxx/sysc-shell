package shell

import (
	"fmt"
	"slices"
	"testing"

	"github.com/Nomadcxx/sysc-shell/internal/config"
	"github.com/Nomadcxx/sysc-shell/internal/trayclient"
	tray "github.com/Nomadcxx/sysc-tray/protocol"
)

func trayItem(gen uint64, id string) tray.Item {
	return tray.Item{
		Key:      tray.ItemKey{Owner: ":1.42", ObjectPath: "/StatusNotifierItem", Generation: gen},
		ID:       id,
		Title:    id,
		Category: tray.CategoryApplicationStatus,
		Status:   tray.StatusActive,
		Icon:     tray.Icon{Name: "icon-" + id},
	}
}

func traySnapshot(seq uint64, items ...tray.Item) trayclient.Message {
	return trayclient.Message{Generation: 1, Kind: trayclient.KindSnapshot,
		Sequence: seq, Snapshot: tray.Snapshot{Sequence: seq, Items: items}}
}

func TestTraySnapshotProjectsItems(t *testing.T) {
	r := NewRegistry(config.Default())
	r.applyTray(traySnapshot(1, trayItem(1, "a"), trayItem(2, "b")))
	if got := r.trayItemCount(); got != 2 {
		t.Fatalf("items = %d", got)
	}
}

func TestTrayItemChangeKeepsIdentityAndUpdatesFields(t *testing.T) {
	r := NewRegistry(config.Default())
	r.applyTray(traySnapshot(1, trayItem(1, "a")))
	changed := trayItem(1, "a")
	changed.Title = "renamed"
	r.applyTray(trayclient.Message{Generation: 1, Kind: trayclient.KindItemChanged, Sequence: 2, Item: changed})
	if got := r.trayTitle(keyOf(changed)); got != "renamed" {
		t.Fatalf("title = %q", got)
	}
	if r.trayItemCount() != 1 {
		t.Fatal("a change added a second item")
	}
}

func TestTrayOwnerReplacementTakesANewGeneration(t *testing.T) {
	r := NewRegistry(config.Default())
	r.applyTray(traySnapshot(1, trayItem(1, "a")))
	// The same owner and path re-registered: generation 2 replaces generation 1.
	r.applyTray(trayclient.Message{Generation: 1, Kind: trayclient.KindItemAdded, Sequence: 2, Item: trayItem(2, "a")})
	if got := r.trayItemCount(); got != 2 {
		t.Fatalf("items = %d, want both generations tracked", got)
	}
	if r.trayTitle(keyOf(trayItem(2, "a"))) == "" {
		t.Fatal("the replacement generation is not projected")
	}
}

func TestTrayIgnoresAStaleGeneration(t *testing.T) {
	r := NewRegistry(config.Default())
	r.applyTray(traySnapshot(1, trayItem(1, "a")))
	r.applyTray(trayclient.Message{Generation: 99, Kind: trayclient.KindItemAdded, Sequence: 2, Item: trayItem(1, "ghost")})
	if got := r.trayItemCount(); got != 1 {
		t.Fatalf("stale generation mutated the projection: %d", got)
	}
}

func TestTrayItemRemovedDropsOnlyThatGeneration(t *testing.T) {
	r := NewRegistry(config.Default())
	r.applyTray(traySnapshot(1, trayItem(1, "a"), trayItem(2, "a")))
	r.applyTray(trayclient.Message{Generation: 1, Kind: trayclient.KindItemRemoved, Sequence: 2,
		Removed: tray.ItemRemoved{Key: trayItem(1, "a").Key}})
	if got := r.trayItemCount(); got != 1 {
		t.Fatalf("items = %d, want 1", got)
	}
	if r.trayTitle(keyOf(trayItem(2, "a"))) == "" {
		t.Fatal("the surviving generation was removed")
	}
}

func TestTrayNeedsAttentionReplacesTheNormalIcon(t *testing.T) {
	r := NewRegistry(config.Default())
	it := trayItem(1, "a")
	it.Status = tray.StatusNeedsAttention
	it.AttentionIcon = tray.Icon{Name: "urgent"}
	r.applyTray(traySnapshot(1, it))
	icon, _ := r.trayIconName(keyOf(it))
	if icon != "urgent" {
		t.Fatalf("attention icon = %q, want the attention replacement", icon)
	}
}

func TestTrayDisconnectDropsTheProjection(t *testing.T) {
	r := NewRegistry(config.Default())
	r.applyTray(traySnapshot(1, trayItem(1, "a")))
	r.applyTray(trayclient.Message{Generation: 1, Kind: trayclient.KindDisconnected})
	if got := r.trayItemCount(); got != 0 {
		t.Fatalf("disconnect left %d items", got)
	}
}

func TestTrayProjectsTheSameItemIndependentlyPerOutput(t *testing.T) {
	r := NewRegistry(config.Default())
	r.applyTray(traySnapshot(1, trayItem(1, "a")))
	// Projection is keyed by item, not output: both outputs read the same set.
	for _, connector := range []string{"eDP-1", "HDMI-A-1"} {
		if got := r.trayItemsFor(connector); len(got) != 1 {
			t.Fatalf("%s sees %d items", connector, len(got))
		}
	}
}

func TestTrayProjectionPreservesServiceOrder(t *testing.T) {
	r := NewRegistry(config.Default())
	items := make([]tray.Item, 24)
	want := make([]string, len(items))
	for i := range items {
		items[i] = trayItem(uint64(i+1), fmt.Sprintf("item-%02d", i))
		want[i] = items[i].ID
	}
	r.applyTray(traySnapshot(1, items...))
	for pass := 0; pass < 10; pass++ {
		if got := itemIDs(r.trayItemsFor("eDP-1")); !slices.Equal(got, want) {
			t.Fatalf("service order changed: got %v, want %v", got, want)
		}
	}
}

func keyOf(it tray.Item) tray.ItemKey { return it.Key }
