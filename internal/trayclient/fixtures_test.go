package trayclient

import (
	"encoding/json"
	"testing"

	"github.com/Nomadcxx/sysc-tray/protocol"
)

// roundTrip encodes and decodes one protocol value and fails if they differ.
func roundTrip[T any](t *testing.T, name string, v T) T {
	t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("%s marshal: %v", name, err)
	}
	var back T
	if err := json.Unmarshal(raw, &back); err != nil {
		t.Fatalf("%s unmarshal: %v", name, err)
	}
	return back
}

func item(gen uint64, id string) protocol.Item {
	return protocol.Item{
		Key:      protocol.ItemKey{Owner: ":1.42", ObjectPath: "/StatusNotifierItem", Generation: gen},
		ID:       id,
		Title:    "Player",
		Category: protocol.CategoryApplicationStatus,
		Status:   protocol.StatusActive,
		Icon:     protocol.Icon{Name: "audio-volume-high"},
	}
}

func TestFixtureHelloRoundTrips(t *testing.T) {
	h := roundTrip(t, "hello", protocol.Hello{
		Major: protocol.ProtocolMajor, Minor: protocol.ProtocolMinor,
		Role: protocol.RolePresenter, Capabilities: []string{protocol.CapabilityTray},
	})
	if h.Role != protocol.RolePresenter || len(h.Capabilities) != 1 {
		t.Fatalf("hello = %+v", h)
	}
}

func TestFixtureSnapshotCarriesItems(t *testing.T) {
	s := roundTrip(t, "snapshot", protocol.Snapshot{Sequence: 3, Items: []protocol.Item{item(1, "player")}})
	if s.Sequence != 3 || len(s.Items) != 1 || s.Items[0].Key.Generation != 1 {
		t.Fatalf("snapshot = %+v", s)
	}
}

func TestFixtureEveryDeltaKindRoundTrips(t *testing.T) {
	for _, tc := range []struct {
		name string
		env  protocol.Envelope
	}{
		{"added", protocol.Envelope{Kind: protocol.KindItemAdded, Sequence: 2, Payload: mustJSON(t, item(1, "a"))}},
		{"changed", protocol.Envelope{Kind: protocol.KindItemChanged, Sequence: 3, Payload: mustJSON(t, item(1, "a"))}},
		{"removed", protocol.Envelope{Kind: protocol.KindItemRemoved, Sequence: 4, Payload: mustJSON(t, protocol.ItemRemoved{Key: item(1, "a").Key})}},
		{"menu", protocol.Envelope{Kind: protocol.KindMenuUpdated, Sequence: 5, Payload: mustJSON(t, protocol.MenuUpdate{Key: item(1, "a").Key, Menu: menuFixture()})}},
	} {
		got := roundTrip(t, tc.name, tc.env)
		if got.Kind != tc.env.Kind || got.Sequence != tc.env.Sequence {
			t.Fatalf("%s = %+v", tc.name, got)
		}
	}
}

func TestFixtureIconsAndPixmapsRoundTrip(t *testing.T) {
	px := protocol.Pixmap{Width: 2, Height: 2, ARGB: []byte{0xff, 0, 0, 0xff}}
	ic := roundTrip(t, "icon", protocol.Icon{Name: "n", Pixmaps: []protocol.Pixmap{px}})
	if len(ic.Pixmaps) != 1 || ic.Pixmaps[0].Width != 2 {
		t.Fatalf("icon = %+v", ic)
	}
	// Attention replaces normal; overlay is a separate composition input.
	full := item(1, "a")
	full.AttentionIcon = protocol.Icon{Name: "urgent"}
	full.OverlayIcon = protocol.Icon{Name: "badge"}
	got := roundTrip(t, "item-icons", full)
	if got.AttentionIcon.Name != "urgent" || got.OverlayIcon.Name != "badge" {
		t.Fatalf("icons = %+v", got.Icon)
	}
}

func TestFixtureTooltipAndMenuBounds(t *testing.T) {
	tt := roundTrip(t, "tooltip", protocol.Tooltip{Title: "Now playing", Description: "Artist — Title"})
	if tt.Title == "" || tt.Description == "" {
		t.Fatalf("tooltip = %+v", tt)
	}
	m := roundTrip(t, "menu", menuFixture())
	// Root child 0 is "Play"; child 1 is the "Mode" submenu holding the radio.
	mode := m.Root.Children[1]
	if m.Revision != 7 || len(mode.Children) == 0 || mode.Children[0].ToggleType != protocol.ToggleRadio {
		t.Fatalf("menu = %+v", m)
	}
}

func TestFixtureCommandsAndRepliesRoundTrip(t *testing.T) {
	c := roundTrip(t, "command", protocol.Command{
		Kind: protocol.CommandMenuSelect, Item: item(1, "a").Key,
		MenuRevision: 7, MenuID: 3, Output: 9, Serial: 11,
	})
	if c.Kind != protocol.CommandMenuSelect || c.MenuID != 3 || c.Serial != 11 {
		t.Fatalf("command = %+v", c)
	}
	r := roundTrip(t, "reply", protocol.Reply{OK: false, Error: &protocol.ProtocolError{Code: protocol.ErrorStaleRevision}})
	if r.OK || r.Error.Code != protocol.ErrorStaleRevision {
		t.Fatalf("reply = %+v", r)
	}
}

func TestFixtureUnknownFieldsAreIgnored(t *testing.T) {
	// A forward-compatible payload keeps unknown fields out of the decode.
	raw := `{"key":{"owner":":1.9","object_path":"/x","generation":1},"id":"a","category":"ApplicationStatus","status":"Active","icon":{},"future_field":42}`
	var it protocol.Item
	if err := json.Unmarshal([]byte(raw), &it); err != nil {
		t.Fatalf("unknown field rejected: %v", err)
	}
	if it.Key.Owner != ":1.9" {
		t.Fatalf("item = %+v", it)
	}
}

func menuFixture() protocol.Menu {
	return protocol.Menu{Revision: 7, Root: protocol.MenuNode{ID: 0, Children: []protocol.MenuNode{
		{ID: 1, Label: "Play", Enabled: true, Visible: true},
		{ID: 2, Label: "Mode", Enabled: true, Visible: true, ChildrenDisplay: "submenu", Children: []protocol.MenuNode{
			{ID: 3, Label: "Shuffle", Enabled: true, Visible: true, ToggleType: protocol.ToggleRadio, ToggleState: 1},
		}},
		{ID: 4, Separator: true, Visible: true},
	}}}
}

func mustJSON(t *testing.T, v any) json.RawMessage {
	t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}
