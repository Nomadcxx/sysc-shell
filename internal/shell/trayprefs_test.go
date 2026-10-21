package shell

import (
	"slices"
	"testing"

	"github.com/Nomadcxx/sysc-shell/internal/config"
	tray "github.com/Nomadcxx/sysc-tray/protocol"
)

func preferenceItem(id, title string, generation uint64) tray.Item {
	return tray.Item{
		Key: tray.ItemKey{Owner: ":1." + id, ObjectPath: "/StatusNotifierItem", Generation: generation},
		ID:  id, Title: title, Status: tray.StatusActive,
	}
}

func itemIDs(items []tray.Item) []string {
	out := make([]string, len(items))
	for i := range items {
		out[i] = items[i].ID
	}
	return out
}

func TestStableTrayTokenPrefersUsefulIDThenTitle(t *testing.T) {
	tests := []struct {
		name  string
		item  tray.Item
		want  string
		valid bool
	}{
		{"id", preferenceItem("chat", "Chat", 1), "id:chat", true},
		{"generic id falls back", preferenceItem("StatusNotifierItem", "Chat", 1), "title:Chat", true},
		{"generic spelling falls back", preferenceItem("Status Notifier Item", "Mail", 1), "title:Mail", true},
		{"no useful identity", preferenceItem("StatusNotifierItem", "Status Notifier Item", 1), "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := stableTrayToken(tt.item)
			if got != tt.want || ok != tt.valid {
				t.Fatalf("stableTrayToken() = %q, %v; want %q, %v", got, ok, tt.want, tt.valid)
			}
		})
	}
}

func TestTrayArrangementUsesGeometryAndSavedOrder(t *testing.T) {
	items := []tray.Item{
		preferenceItem("a", "A", 1),
		preferenceItem("b", "B", 1),
		preferenceItem("c", "C", 1),
	}
	prefs := config.TrayPreferences{Pinned: []string{"id:b"}, Order: []string{"id:c", "id:b", "id:a"}}
	got := arrangeTray(items, prefs, 44, 20, 4)
	if ids := itemIDs(got.Bar); !slices.Equal(ids, []string{"b", "c"}) {
		t.Fatalf("bar order = %v, want pinned b then c", ids)
	}
	if ids := itemIDs(got.Overflow); !slices.Equal(ids, []string{"a"}) {
		t.Fatalf("overflow = %v, want a", ids)
	}
}

func TestTrayArrangementKeepsHiddenItemsRecoverable(t *testing.T) {
	items := []tray.Item{
		preferenceItem("a", "A", 1),
		preferenceItem("b", "B", 1),
		preferenceItem("c", "C", 1),
	}
	got := arrangeTray(items, config.TrayPreferences{Hidden: []string{"id:b"}}, 100, 20, 4)
	if ids := itemIDs(got.Bar); !slices.Equal(ids, []string{"a", "c"}) {
		t.Fatalf("bar = %v, want a c", ids)
	}
	if ids := itemIDs(got.Hidden); !slices.Equal(ids, []string{"b"}) {
		t.Fatalf("hidden section = %v, want b", ids)
	}
}

func TestTrayPassiveVisibilityDefaultsOffAndHonorsPreferences(t *testing.T) {
	items := []tray.Item{
		preferenceItem("a", "A", 1),
		preferenceItem("p", "Passive", 1),
		preferenceItem("h", "Hidden passive", 1),
		preferenceItem("b", "B", 1),
		preferenceItem("q", "Ordered passive", 1),
	}
	items[1].Status = tray.StatusPassive
	items[2].Status = tray.StatusPassive
	items[4].Status = tray.StatusPassive

	tests := []struct {
		name       string
		config     string
		wantBar    []string
		wantHidden []string
		wantPinned bool
	}{
		{
			name:       "hidden by default",
			config:     `{"tray":{"pinned":["id:p"],"hidden":["id:h"],"order":["id:b","id:a","id:q","id:p"]}}`,
			wantBar:    []string{"b", "a"},
			wantHidden: nil,
		},
		{
			name:       "shown items retain pinned, hidden and saved order",
			config:     `{"tray":{"show-passive":true,"pinned":["id:p"],"hidden":["id:h"],"order":["id:q","id:b","id:a","id:p"]}}`,
			wantBar:    []string{"p", "q", "b", "a"},
			wantHidden: []string{"h"},
			wantPinned: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg, err := config.Parse([]byte(tt.config))
			if err != nil {
				t.Fatal(err)
			}
			got := arrangeTray(items, cfg.Tray, 100, 20, 4)
			if ids := itemIDs(got.Bar); !slices.Equal(ids, tt.wantBar) {
				t.Fatalf("bar = %v, want %v", ids, tt.wantBar)
			}
			if ids := itemIDs(got.Hidden); !slices.Equal(ids, tt.wantHidden) {
				t.Fatalf("hidden = %v, want %v", ids, tt.wantHidden)
			}
			if tt.wantPinned && !got.Pinned[items[1].Key] {
				t.Fatal("passive item is not pinned")
			}
		})
	}
}

func TestTrayTokenCollisionIgnoresEveryPreferenceForThatToken(t *testing.T) {
	first := preferenceItem("shared", "One", 1)
	second := preferenceItem("shared", "Two", 2)
	got := arrangeTray([]tray.Item{first, second}, config.TrayPreferences{
		Hidden: []string{"id:shared"}, Pinned: []string{"id:shared"}, Order: []string{"id:shared"},
	}, 100, 20, 4)
	if ids := itemIDs(got.Bar); !slices.Equal(ids, []string{"shared", "shared"}) {
		t.Fatalf("colliding items = %v, want each visible in service order", ids)
	}
	if !slices.Equal(got.Collisions, []string{"id:shared"}) {
		t.Fatalf("collisions = %v", got.Collisions)
	}
}

func TestTrayPreferenceEditsAreReversibleAndOrdered(t *testing.T) {
	p := config.TrayPreferences{Order: []string{"id:a", "id:b", "id:c"}}
	live := []string{"id:a", "id:b", "id:c"}
	p = editTrayPreferences(p, trayPreferenceHide, "id:b", live)
	p = editTrayPreferences(p, trayPreferencePin, "id:b", live)
	p = editTrayPreferences(p, trayPreferenceEarlier, "id:c", live)
	if !slices.Equal(p.Hidden, []string{"id:b"}) || !slices.Equal(p.Pinned, []string{"id:b"}) ||
		!slices.Equal(p.Order, []string{"id:a", "id:c", "id:b"}) {
		t.Fatalf("edited preferences = %+v", p)
	}
	p = editTrayPreferences(p, trayPreferenceShow, "id:b", live)
	p = editTrayPreferences(p, trayPreferenceUnpin, "id:b", live)
	p = editTrayPreferences(p, trayPreferenceLater, "id:a", []string{"id:a", "id:c", "id:b"})
	if len(p.Hidden) != 0 || len(p.Pinned) != 0 ||
		!slices.Equal(p.Order, []string{"id:c", "id:a", "id:b"}) {
		t.Fatalf("reversed preferences = %+v", p)
	}
}

func TestTrayPreferenceMoveMaterializesEmptyAndPartialOrder(t *testing.T) {
	live := []string{"id:a", "id:b", "id:c"}
	empty := editTrayPreferences(config.TrayPreferences{}, trayPreferenceEarlier, "id:c", live)
	if !slices.Equal(empty.Order, []string{"id:a", "id:c", "id:b"}) {
		t.Fatalf("empty order move = %v", empty.Order)
	}
	partial := editTrayPreferences(config.TrayPreferences{Order: []string{"id:c"}}, trayPreferenceLater, "id:a", []string{"id:c", "id:a", "id:b"})
	if !slices.Equal(partial.Order, []string{"id:c", "id:b", "id:a"}) {
		t.Fatalf("partial order move = %v", partial.Order)
	}
}
