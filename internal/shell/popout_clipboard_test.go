package shell

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"strings"
	"testing"
	"time"

	clipboardclient "github.com/Nomadcxx/sysc-clipboard/client"
	clipboardprotocol "github.com/Nomadcxx/sysc-clipboard/protocol"
	"github.com/Nomadcxx/sysc-shell/internal/config"
	"github.com/Nomadcxx/sysc-shell/internal/platform/wayland"
	"github.com/Nomadcxx/sysc-shell/internal/render"
	"github.com/Nomadcxx/sysc-shell/internal/theme"
	"github.com/Nomadcxx/sysc-shell/internal/ui"
)

type clipboardRecorder struct {
	restored string
	pinned   struct {
		id     string
		pinned bool
	}
	deleted string
	cleared []clipboardprotocol.ClearScope
	thumbs  []string
}

func (r *clipboardRecorder) Restore(id string) error {
	r.restored = id
	return nil
}

func (r *clipboardRecorder) Pin(id string, pinned bool) error {
	r.pinned.id, r.pinned.pinned = id, pinned
	return nil
}

func (r *clipboardRecorder) Delete(id string) error {
	r.deleted = id
	return nil
}

func (r *clipboardRecorder) Clear(scope clipboardprotocol.ClearScope) error {
	r.cleared = append(r.cleared, scope)
	return nil
}

func (r *clipboardRecorder) Thumbnail(id string, _ uint16) error {
	r.thumbs = append(r.thumbs, id)
	return nil
}

func (r *clipboardRecorder) Resync() error { return nil }

func testClipboardEntry(id string, kind clipboardprotocol.Kind, preview string, pinned bool) clipboardprotocol.Entry {
	return clipboardprotocol.Entry{
		ID: id, Kind: kind, MIME: "text/plain", OfferedMIME: []string{"text/plain"},
		Size: uint64(len(preview)), SHA256: strings.Repeat("a", 64),
		CapturedAt: time.Date(2026, 9, 16, 5, 0, 0, 0, time.UTC), Preview: preview, Pinned: pinned,
	}
}

func clipboardTestRegistry(entries []clipboardprotocol.Entry) *Registry {
	r := &Registry{
		cfg: config.Default(),
		clipboard: clipboardProjection{Connected: true, Snapshot: clipboardprotocol.Snapshot{
			Revision: 1, Entries: entries, Persistence: clipboardprotocol.PersistenceDurable,
			Wayland: clipboardprotocol.WaylandReady,
		}},
	}
	return r
}

func clipboardOnlyRegistry(entries []clipboardprotocol.Entry) *Registry {
	r := clipboardTestRegistry(entries)
	r.mu.Lock()
	r.bars = make(map[uint32]*Bar)
	r.panelHosts = make(map[PanelID]*PanelHost)
	r.mu.Unlock()
	r.invalidations = make(chan wayland.Invalidation, 8)
	r.aux = make(chan wayland.AuxRequest, 8)
	r.closed = make(chan struct{})
	return r
}

func laidOutClipboardTree(r *Registry, h *PanelHost) *ui.Node {
	tree := r.panelTree(h)
	measure := func(s string, _ ui.TextAttrs) (int, int) { return len([]rune(s)) * 8, 16 }
	if err := ui.LayoutColumn(tree, panelTargetSize(PanelClipboard), measure); err != nil {
		panic(err)
	}
	return tree
}

func activateClipboardAction(t *testing.T, r *Registry, h *PanelHost, action string) bool {
	t.Helper()
	for i, node := range h.focus {
		if node != nil && node.Action == action {
			h.roving.Set(i)
			return h.activate(r)
		}
	}
	return false
}

func TestClipboardPanelIdentityAndGeometry(t *testing.T) {
	if got := PanelClipboard.String(); got != "clipboard" {
		t.Fatalf("PanelClipboard.String() = %q, want clipboard", got)
	}
	if got, err := parsePanelName("clipboard"); err != nil || got != PanelClipboard {
		t.Fatalf("parsePanelName(clipboard) = %v, %v", got, err)
	}
	if got, ok := panelIDFromAux("panel:clipboard"); !ok || got != PanelClipboard {
		t.Fatalf("panelIDFromAux(panel:clipboard) = %v, %v", got, ok)
	}
	if got := panelTargetSize(PanelClipboard); got != (ui.Rect{W: 720, H: 560}) {
		t.Fatalf("clipboard target size = %+v, want 720x560", got)
	}
}

func TestClipboardTreeUsesFocusedSearchAndStableMetadataRows(t *testing.T) {
	entries := []clipboardprotocol.Entry{
		testClipboardEntry("two", clipboardprotocol.KindText, "second preview", false),
		testClipboardEntry("one", clipboardprotocol.KindText, "first preview", true),
	}
	r := clipboardTestRegistry(entries)
	h := &PanelHost{id: PanelClipboard, place: Placement{Panel: panelTargetSize(PanelClipboard)}, theme: DefaultTheme(), search: ui.NewField("")}
	tree := laidOutClipboardTree(r, h)
	field := findKind(tree, ui.KindTextField)
	if field == nil || field.Name != "Search" || !field.Focusable {
		t.Fatalf("search field = %+v, want focused accessible textbox", field)
	}
	h.focus = ui.Focusables(tree)
	h.roving = ui.Roving{Count: len(h.focus)}
	h.focusByName("Search")
	if got := h.focused(); got == nil || got.Name != "Search" {
		t.Fatalf("focused node = %+v, want Search", got)
	}

	list := findKind(tree, ui.KindVirtualList)
	if list == nil || list.ItemCount != 2 || list.Item == nil {
		t.Fatalf("clipboard list = %+v, want two ID-keyed rows", list)
	}
	first, second := list.Item(0), list.Item(1)
	if first == nil || second == nil || first.Key != "clipboard-row:two" || second.Key != "clipboard-row:one" {
		t.Fatalf("row keys = %q, %q", first.Key, second.Key)
	}
	if !first.Focusable || first.Role != "button" || !strings.Contains(first.Name, "second preview") {
		t.Fatalf("first row accessibility = %+v", first)
	}
	if !treeHasText(tree, "second preview") || !treeHasText(tree, "first preview") {
		t.Fatalf("tree text = %v, want both bounded previews", texts(tree))
	}
	if treeHasText(tree, "payload") {
		t.Fatalf("tree contains raw payload text: %v", texts(tree))
	}
	if got := clipboardTooltip(r.clipboard); got != "Clipboard: 2 items (durable)" {
		t.Fatalf("tooltip = %q", got)
	}
}

func TestClipboardTreeRendersImagePlaceholderAndStateMessages(t *testing.T) {
	imageEntry := testClipboardEntry("image", clipboardprotocol.KindImage, "", false)
	imageEntry.MIME = "image/png"
	r := clipboardTestRegistry([]clipboardprotocol.Entry{imageEntry})
	h := &PanelHost{id: PanelClipboard, place: Placement{Panel: panelTargetSize(PanelClipboard)}, theme: DefaultTheme(), search: ui.NewField("")}
	tree := laidOutClipboardTree(r, h)
	if findKind(tree, ui.KindImage) == nil || !treeHasText(tree, "Image preview unavailable") {
		t.Fatalf("image tree lacks bounded thumbnail placeholder: %v", texts(tree))
	}

	h.query = "not-present"
	tree = laidOutClipboardTree(r, h)
	if !treeHasText(tree, "No clipboard matches") {
		t.Fatalf("no-match tree = %v", texts(tree))
	}

	r.clipboard.Connected = false
	tree = laidOutClipboardTree(r, h)
	if !treeHasText(tree, "Clipboard unavailable") {
		t.Fatalf("unavailable tree = %v", texts(tree))
	}
	r.clipboard.Connected = true
	r.clipboard.Snapshot.Persistence = clipboardprotocol.PersistenceVolatile
	tree = laidOutClipboardTree(r, h)
	if !strings.Contains(strings.Join(texts(tree), "\n"), "volatile") {
		t.Fatalf("volatile tree = %v", texts(tree))
	}
}

func TestClipboardStatusNoticeCentersBothLinesWithoutClipping(t *testing.T) {
	r := clipboardTestRegistry(nil)
	r.clipboard.Snapshot.Persistence = clipboardprotocol.PersistenceUnavailable
	h := &PanelHost{id: PanelClipboard, place: Placement{Panel: panelTargetSize(PanelClipboard)}, theme: DefaultTheme(), search: ui.NewField("")}
	tree := r.panelTree(h)
	measure := func(s string, attrs ui.TextAttrs) (int, int) {
		height := 16
		if attrs.Role == theme.RoleLabel {
			height = 20
		}
		return len([]rune(s)) * 8, height
	}
	if err := ui.LayoutColumn(tree, panelTargetSize(PanelClipboard), measure); err != nil {
		t.Fatal(err)
	}

	var notice *ui.Node
	for _, capsule := range findAllKind(tree, ui.KindCapsule) {
		if findText(capsule, "Clipboard persistence unavailable") != nil {
			notice = capsule
			break
		}
	}
	if notice == nil {
		t.Fatal("clipboard persistence notice is missing")
	}
	title := findText(notice, "Clipboard persistence unavailable")
	detail := findText(notice, "History remains available in memory")
	if title == nil || detail == nil {
		t.Fatal("clipboard persistence notice is missing one of its text lines")
	}
	topInset := title.Bounds.Y - notice.Bounds.Y
	bottomInset := notice.Bounds.Y + notice.Bounds.H - (detail.Bounds.Y + detail.Bounds.H)
	if topInset != bottomInset {
		t.Fatalf("notice insets = top %d, bottom %d; title/detail are not centred in %+v", topInset, bottomInset, notice.Bounds)
	}
}

func TestClipboardSearchSelectsOnlyVisibleRows(t *testing.T) {
	entries := []clipboardprotocol.Entry{
		testClipboardEntry("alpha", clipboardprotocol.KindText, "alpha text", false),
		testClipboardEntry("beta", clipboardprotocol.KindText, "beta text", false),
	}
	r := clipboardTestRegistry(entries)
	h := &PanelHost{id: PanelClipboard, place: Placement{Panel: panelTargetSize(PanelClipboard)}, theme: DefaultTheme(), search: ui.NewField("")}
	_ = laidOutClipboardTree(r, h)

	h.query = "beta"
	tree := laidOutClipboardTree(r, h)
	row := findClipboardRowAction(tree, "clipboard:restore:beta")
	if row == nil || !row.State.Has(ui.StateSelected) {
		t.Fatalf("filtered selected row = %+v, want beta selected", row)
	}
	if treeHasText(tree, "alpha text") || !treeHasText(tree, "beta text") {
		t.Fatalf("filtered tree = %v, want only beta metadata", texts(tree))
	}

	h.query = "missing"
	tree = laidOutClipboardTree(r, h)
	if h.clipboardSelectedID != "" {
		t.Fatalf("selection after no-match query = %q, want empty", h.clipboardSelectedID)
	}
	if treeHasText(tree, "Selected item") {
		t.Fatalf("no-match tree retained selected detail: %v", texts(tree))
	}
}

func TestClipboardTextRowIconIsInMaterialSubset(t *testing.T) {
	if !render.ValidMaterialIcon("content_copy") {
		t.Fatal("content_copy is not in the material icon subset")
	}
}

func TestClipboardActionsRouteThroughDaemonAndConfirmClears(t *testing.T) {
	r := clipboardTestRegistry([]clipboardprotocol.Entry{
		testClipboardEntry("one", clipboardprotocol.KindText, "one", false),
		testClipboardEntry("two", clipboardprotocol.KindText, "two", true),
	})
	sender := &clipboardRecorder{}
	r.clipboardSender = sender
	h := &PanelHost{id: PanelClipboard, place: Placement{Panel: panelTargetSize(PanelClipboard)}, theme: DefaultTheme(), search: ui.NewField("")}
	h.root = laidOutClipboardTree(r, h)
	h.focus = ui.Focusables(h.root)
	h.roving = ui.Roving{Count: len(h.focus)}

	if !activateClipboardAction(t, r, h, "clipboard:restore:two") || sender.restored != "two" {
		t.Fatalf("restore = %q", sender.restored)
	}
	if !activateClipboardAction(t, r, h, "clipboard:pin:two") || sender.pinned.id != "two" || sender.pinned.pinned != false {
		t.Fatalf("pin = %+v", sender.pinned)
	}
	if !activateClipboardAction(t, r, h, "clipboard:delete:two") || !treeHasText(h.root, "Delete clipboard item?") {
		t.Fatalf("delete did not enter confirmation: %v", texts(h.root))
	}
	if !activateClipboardAction(t, r, h, "clipboard:cancel-delete") {
		t.Fatal("delete confirmation cancel was not actionable")
	}
	if !activateClipboardAction(t, r, h, "clipboard:clear:all") || !treeHasText(h.root, "Clear all clipboard history?") {
		t.Fatalf("clear-all did not enter confirmation: %v", texts(h.root))
	}
	if !activateClipboardAction(t, r, h, "clipboard:confirm-clear:all") {
		t.Fatal("clear-all confirmation was not actionable")
	}
	if len(sender.cleared) != 1 || sender.cleared[0] != clipboardprotocol.ClearAll {
		t.Fatalf("clear calls = %v", sender.cleared)
	}
}

func TestClipboardSnapshotReplacementRetainsSelectedID(t *testing.T) {
	r := clipboardOnlyRegistry([]clipboardprotocol.Entry{
		testClipboardEntry("keep", clipboardprotocol.KindText, "keep", false),
		testClipboardEntry("drop", clipboardprotocol.KindText, "drop", false),
	})
	t.Cleanup(func() { close(r.closed) })
	sender := &clipboardRecorder{}
	r.BindClipboard(sender)
	h := &PanelHost{id: PanelClipboard, output: 7, stopAnim: make(chan struct{}), place: Placement{Panel: panelTargetSize(PanelClipboard)}, theme: DefaultTheme(), search: ui.NewField("")}
	r.panelHosts[PanelClipboard] = h
	h.root = laidOutClipboardTree(r, h)
	h.focus = ui.Focusables(h.root)
	h.roving = ui.Roving{Count: len(h.focus)}
	if !activateClipboardAction(t, r, h, "clipboard:restore:keep") {
		t.Fatal("could not select the retained entry")
	}

	update := clipboardclient.Update{Connected: true, Snapshot: clipboardprotocol.Snapshot{
		Revision: 2, Entries: []clipboardprotocol.Entry{
			testClipboardEntry("new", clipboardprotocol.KindText, "new", false),
			testClipboardEntry("keep", clipboardprotocol.KindText, "keep updated", false),
		}, Persistence: clipboardprotocol.PersistenceDurable, Wayland: clipboardprotocol.WaylandReady,
	}}
	r.ApplyClipboard(update)
	row := findClipboardRowAction(h.root, "clipboard:restore:keep")
	if row == nil || !row.State.Has(ui.StateSelected) {
		t.Fatalf("selected row after replacement = %+v", row)
	}
	if got := h.focused(); got == nil || got.Action != "clipboard:restore:keep" {
		t.Fatalf("focused row after replacement = %+v, want retained ID", got)
	}
}

func TestClipboardThumbnailDecodeIsBoundedAndReleasedOnClose(t *testing.T) {
	entry := testClipboardEntry("image", clipboardprotocol.KindImage, "", false)
	entry.MIME = "image/png"
	r := clipboardOnlyRegistry([]clipboardprotocol.Entry{entry})
	t.Cleanup(func() { close(r.closed) })
	h := &PanelHost{id: PanelClipboard, output: 7, stopAnim: make(chan struct{}), place: Placement{Panel: panelTargetSize(PanelClipboard)}, theme: DefaultTheme(), search: ui.NewField("")}
	r.panelHosts[PanelClipboard] = h
	h.root = laidOutClipboardTree(r, h)
	h.focus = ui.Focusables(h.root)
	h.roving = ui.Roving{Count: len(h.focus)}

	var encoded bytes.Buffer
	source := image.NewRGBA(image.Rect(0, 0, 2, 1))
	source.Set(0, 0, color.RGBA{R: 0xff, A: 0xff})
	if err := png.Encode(&encoded, source); err != nil {
		t.Fatal(err)
	}
	r.ApplyClipboard(clipboardclient.Update{
		Connected: true, Snapshot: r.clipboard.Snapshot,
		Message: clipboardprotocol.Message{Version: clipboardprotocol.Version, Type: clipboardprotocol.TypeThumbnail,
			Thumbnail: &clipboardprotocol.Thumbnail{ID: "image", MIME: "image/png", Width: 2, Height: 1, Data: encoded.Bytes()}},
	})
	if imageNode := findKind(h.root, ui.KindImage); imageNode == nil || imageNode.Image == nil {
		t.Fatalf("thumbnail node = %+v, want decoded image", imageNode)
	}

	r.ClosePanel(PanelClipboard)
	if h.clipboardThumbnails != nil {
		t.Fatal("clipboard thumbnails survived panel close")
	}
}

func TestClipboardPanelUsesFloatingCenteredExclusiveSurface(t *testing.T) {
	r := newPanelRegistry(t)
	r.mu.Lock()
	r.clipboard = clipboardTestRegistry([]clipboardprotocol.Entry{
		testClipboardEntry("one", clipboardprotocol.KindText, "one", false),
	}).clipboard
	r.mu.Unlock()
	if err := r.OpenPanel(PanelClipboard, 7, Trigger{BarEdge: "top", BarZone: 44, OutW: 1920, OutH: 1080}); err != nil {
		t.Fatal(err)
	}
	requests := drainAux(t, r, 2)
	h := r.panelHosts[PanelClipboard]
	if !h.place.CenterY || h.place.Panel != (ui.Rect{W: 720, H: 560}) {
		t.Fatalf("clipboard placement = %+v, want floating 720x560", h.place)
	}
	panel := requests[1].Open
	if panel == nil || panel.Width != 720 || panel.Height != 560 || panel.Keyboard != keyboardExclusive {
		t.Fatalf("clipboard surface = %+v, want 720x560 exclusive", panel)
	}
	want := h.place.Margins()
	wantTop := (h.place.Output.H - h.place.Panel.H) / 2
	if panel.MarginLeft != int32(want.Left) || panel.MarginTop != int32(wantTop) {
		t.Fatalf("clipboard margins = %d,%d, want %d,%d", panel.MarginLeft, panel.MarginTop, want.Left, wantTop)
	}
}

func TestClipboardKeyboardRestoresDeletesAndNavigatesByID(t *testing.T) {
	r := newPanelRegistry(t)
	entries := []clipboardprotocol.Entry{
		testClipboardEntry("one", clipboardprotocol.KindText, "alpha", false),
		testClipboardEntry("two", clipboardprotocol.KindText, "beta", false),
		testClipboardEntry("three", clipboardprotocol.KindText, "gamma", false),
	}
	r.mu.Lock()
	r.clipboard = clipboardTestRegistry(entries).clipboard
	r.mu.Unlock()
	sender := &clipboardRecorder{}
	r.BindClipboard(sender)
	if err := r.OpenPanel(PanelClipboard, 7, Trigger{OutW: 1920, OutH: 1080}); err != nil {
		t.Fatal(err)
	}
	requests := drainAux(t, r, 2)
	h := r.panelHosts[PanelClipboard]
	if err := requests[1].Open.Callbacks.Configure(720, 560, 120); err != nil {
		t.Fatal(err)
	}
	if got := h.focused(); got == nil || got.Name != "Search" {
		t.Fatalf("opening focus = %+v, want Search", got)
	}
	if !h.keyPress(r, keyEnter) || sender.restored != "one" {
		t.Fatalf("Enter restore = %q", sender.restored)
	}

	for i, node := range h.focus {
		if node != nil && node.Action == "clipboard:restore:three" {
			h.roving.Set(i)
			break
		}
	}
	if !h.keyPress(r, keyDelete) || !treeHasText(h.root, "Delete clipboard item?") {
		t.Fatalf("Delete confirmation tree = %v", texts(h.root))
	}
	if !activateClipboardAction(t, r, h, "clipboard:confirm-delete:three") || sender.deleted != "three" {
		t.Fatalf("confirmed delete = %q", sender.deleted)
	}

	h.query = "beta"
	h.search = ui.NewField("beta")
	r.rebuildPanel(h)
	list := findClipboardList(h.root)
	if list == nil || list.ItemCount != 1 || list.Item(0).Action != "clipboard:restore:two" {
		t.Fatalf("substring list = %+v", list)
	}

	h.query = ""
	h.search = ui.NewField("")
	r.rebuildPanel(h)
	h.logicalH = 120
	if !h.keyPress(r, keyEnd) || h.focused() == nil || h.focused().Action != "clipboard:restore:three" {
		t.Fatalf("End focus = %+v, want final ID", h.focused())
	}
}

func findClipboardRowAction(root *ui.Node, action string) *ui.Node {
	if root == nil {
		return nil
	}
	if root.Kind == ui.KindButton && root.Key == "clipboard-row:"+strings.TrimPrefix(action, "clipboard:restore:") && root.Action == action {
		return root
	}
	if root.Kind == ui.KindVirtualList && root.Item != nil {
		for i := 0; i < root.ItemCount; i++ {
			if got := findClipboardRowAction(root.Item(i), action); got != nil {
				return got
			}
		}
	}
	for _, child := range root.Children {
		if got := findClipboardRowAction(child, action); got != nil {
			return got
		}
	}
	return nil
}
