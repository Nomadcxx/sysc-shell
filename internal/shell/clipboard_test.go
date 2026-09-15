package shell

import (
	"testing"
	"time"

	clipboardclient "github.com/Nomadcxx/sysc-clipboard/client"
	clipboardprotocol "github.com/Nomadcxx/sysc-clipboard/protocol"
	"github.com/Nomadcxx/sysc-shell/internal/config"
	"github.com/Nomadcxx/sysc-shell/internal/ui"
)

func TestClipboardWidgetUsesContentPasteAndPanelAction(t *testing.T) {
	t.Parallel()

	widgets := buildWidgets([]config.Item{{ID: "clipboard"}}, 8, standardMetrics())
	if len(widgets) != 1 || widgets[0].inner == nil {
		t.Fatalf("clipboard widgets = %+v", widgets)
	}
	n := widgets[0].inner
	if n.Kind != ui.KindIcon || n.Icon != "content_paste" {
		t.Fatalf("clipboard node = %+v, want content_paste icon", n)
	}
	if n.Action != panelClipboardAction || n.Name != "Clipboard" || n.Role != "button" {
		t.Fatalf("clipboard accessibility/action = action %q name %q role %q", n.Action, n.Name, n.Role)
	}
	if widgets[0].node.Action != panelClipboardAction {
		t.Fatalf("clipboard capsule action = %q, want %q", widgets[0].node.Action, panelClipboardAction)
	}
}

func TestClipboardTooltipReportsConnectionAndPersistence(t *testing.T) {
	t.Parallel()

	entry := clipboardprotocol.Entry{ID: "one", Kind: clipboardprotocol.KindText}
	cases := []struct {
		name string
		view clipboardProjection
		want string
	}{
		{
			name: "unavailable",
			view: clipboardProjection{Snapshot: clipboardprotocol.Snapshot{Wayland: clipboardprotocol.WaylandUnavailable}},
			want: "Clipboard unavailable",
		},
		{
			name: "durable",
			view: clipboardProjection{Connected: true, Snapshot: clipboardprotocol.Snapshot{
				Entries: []clipboardprotocol.Entry{entry}, Persistence: clipboardprotocol.PersistenceDurable,
				Wayland: clipboardprotocol.WaylandReady,
			}},
			want: "Clipboard: 1 item (durable)",
		},
		{
			name: "volatile",
			view: clipboardProjection{Connected: true, Snapshot: clipboardprotocol.Snapshot{
				Entries: []clipboardprotocol.Entry{entry}, Persistence: clipboardprotocol.PersistenceVolatile,
				Wayland: clipboardprotocol.WaylandReady,
			}},
			want: "Clipboard: 1 item (volatile)",
		},
		{
			name: "persistence unavailable",
			view: clipboardProjection{Connected: true, Snapshot: clipboardprotocol.Snapshot{
				Persistence: clipboardprotocol.PersistenceUnavailable, Wayland: clipboardprotocol.WaylandReady,
			}},
			want: "Clipboard: 0 items (persistence unavailable)",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := clipboardTooltip(tc.view); got != tc.want {
				t.Fatalf("tooltip = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestClipboardProjectionCopiesMetadataAndCoalescesUnchangedUpdates(t *testing.T) {
	cfg := config.Default()
	r := NewRegistry(cfg)
	t.Cleanup(r.Close)
	newHosts(t, r, map[uint32]string{1: "DP-9"})

	entry := clipboardprotocol.Entry{
		ID: "one", Kind: clipboardprotocol.KindText, MIME: "text/plain", OfferedMIME: []string{"text/plain"},
		Size: 3, Preview: "one", CapturedAt: time.Date(2026, 9, 16, 5, 0, 0, 0, time.UTC),
	}
	update := clipboardclient.Update{Connected: true, Snapshot: clipboardprotocol.Snapshot{
		Revision: 1, Entries: []clipboardprotocol.Entry{entry},
		Persistence: clipboardprotocol.PersistenceDurable, Wayland: clipboardprotocol.WaylandReady,
	}}
	if got := r.applyClipboard(update); len(got) != 1 || got[0] != 1 {
		t.Fatalf("first clipboard update changed %v, want global 1", got)
	}
	update.Snapshot.Entries[0].OfferedMIME[0] = "application/x-mutated"
	if got := r.clipboard.Snapshot.Entries[0].OfferedMIME[0]; got != "text/plain" {
		t.Fatalf("projection retained caller mutation %q", got)
	}

	unchanged := clipboardclient.Update{Connected: true, Snapshot: r.clipboard.Snapshot}
	if got := r.applyClipboard(unchanged); len(got) != 0 {
		t.Fatalf("unchanged clipboard update changed %v", got)
	}
}

func TestClipboardLeftAndRightClicksToggleTheSamePanel(t *testing.T) {
	for _, tc := range []struct {
		name   string
		button uint32
	}{
		{name: "left", button: buttonLeft},
		{name: "right", button: buttonRight},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := NewRegistry(config.Default())
			t.Cleanup(r.Close)
			bar := &Bar{}
			r.bindBarPanelActionsLocked(1, bar)
			if !bar.onAction(panelClipboardAction, tc.button) {
				t.Fatalf("%s click was not handled", tc.name)
			}
			requests := drainAux(t, r, 2)
			if requests[1].Open == nil || requests[1].Open.ID != "panel:clipboard" {
				t.Fatalf("%s click opened %+v, want panel:clipboard", tc.name, requests[1].Open)
			}
			if _, ok := r.panelHosts[PanelClipboard]; !ok {
				t.Fatalf("%s click did not create PanelClipboard", tc.name)
			}
		})
	}
}

func TestClipboardProjectionShowsDaemonErrorsUntilNextState(t *testing.T) {
	r := clipboardOnlyRegistry([]clipboardprotocol.Entry{testClipboardEntry("one", clipboardprotocol.KindText, "one", false)})
	t.Cleanup(func() { close(r.closed) })
	h := &PanelHost{
		id: PanelClipboard, output: 7, stopAnim: make(chan struct{}),
		place: Placement{Panel: panelTargetSize(PanelClipboard)}, theme: DefaultTheme(), search: ui.NewField(""),
	}
	r.panelHosts[PanelClipboard] = h
	h.root = laidOutClipboardTree(r, h)
	h.focus = ui.Focusables(h.root)
	h.roving = ui.Roving{Count: len(h.focus)}

	r.ApplyClipboard(clipboardclient.Update{
		Connected: true, Snapshot: r.clipboard.Snapshot,
		Message: clipboardprotocol.Message{Version: clipboardprotocol.Version, Type: clipboardprotocol.TypeError,
			Error: &clipboardprotocol.ErrorBody{Code: clipboardprotocol.ErrorUnavailable, Message: "restore failed"}},
	})
	if !treeHasText(h.root, "restore failed") {
		t.Fatalf("error tree = %v, want daemon error", texts(h.root))
	}

	next := r.clipboard.Snapshot
	next.Revision++
	r.ApplyClipboard(clipboardclient.Update{
		Connected: true, Snapshot: next,
		Message: clipboardprotocol.Message{Version: clipboardprotocol.Version, Type: clipboardprotocol.TypeSnapshot, Snapshot: &next},
	})
	if treeHasText(h.root, "restore failed") {
		t.Fatalf("error survived the next state: %v", texts(h.root))
	}
}
