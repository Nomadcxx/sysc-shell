package shell

import (
	"math"
	"os"
	"path/filepath"
	"strings"
	"unicode"

	launcher "github.com/Nomadcxx/sysc-launch"
	"github.com/Nomadcxx/sysc-shell/internal/icons"
	"github.com/Nomadcxx/sysc-shell/internal/platform/wayland"
	"github.com/Nomadcxx/sysc-shell/internal/theme"
	"github.com/Nomadcxx/sysc-shell/internal/ui"
)

// Launcher chrome: pill search, 60px-row list, 8px gap between pills.
// The 40px icon slot is a letter until a theme raster lands.
const (
	launcherRowHeight   = 60
	launcherRowGap      = 8
	launcherSlotHeight  = launcherRowHeight + launcherRowGap
	launcherFieldHeight = 44
	launcherIconSlot    = 40
)

// launcherServiceLocked returns the process-wide launcher service, creating
// it on first use. Lazy creation keeps registries that never open the
// launcher from paying for an XDG scan; the first open scans, and later opens
// hit the service's rescan-if-stale path (D12). Caller holds r.mu.
func (r *Registry) launcherServiceLocked() *launcher.Service {
	if r.launcherSvc == nil {
		r.launcherSvc = launcher.NewService(launcher.ServiceConfig{
			History: launcher.OpenHistory(launcherHistoryPath(os.Getenv), nil),
		})
		go r.relayLauncher(r.launcherSvc)
	}
	return r.launcherSvc
}

// launcherHistoryPath is the shell's existing ranking file. sysc-launch's
// DefaultHistory uses a separate sysc-launch directory; this keeps the two
// consumers from merging usage data.
func launcherHistoryPath(getenv func(string) string) string {
	if getenv == nil {
		getenv = os.Getenv
	}
	base := getenv("XDG_STATE_HOME")
	if base == "" {
		base = filepath.Join(getenv("HOME"), ".local", "state")
	}
	return filepath.Join(base, "sysc-shell", "launcher", "history.gob")
}

// relayLauncher applies result snapshots to the open launcher panel. The
// channel is owned by the service; the relay ends with the registry.
func (r *Registry) relayLauncher(svc *launcher.Service) {
	ch := svc.Results()
	for {
		select {
		case <-r.closed:
			return
		case results := <-ch:
			r.mu.Lock()
			h := r.panelHosts[PanelLauncher]
			if h != nil {
				h.launcherResults = results
				r.rebuildPanel(h)
			}
			r.mu.Unlock()
			if h != nil {
				r.publishSurface(h.output, panelSurfaceID(PanelLauncher))
			}
		}
	}
}

// launcherTree projects the current snapshot: one text field above a virtual
// list of result capsules. The selected row is a muted wash; every row
// carries the 40px glyph slot, bold name, and comment.
func launcherTree(r *Registry, h *PanelHost) *ui.Node {
	if h.search == nil {
		h.search = ui.NewField("")
	}
	field := h.search.Node("Search")
	field.Width = max(h.place.Panel.W-24, 0)
	field.Height = launcherFieldHeight
	field.Padding = 8

	head := []*ui.Node{}
	if h.errLabel != "" {
		head = append(head, &ui.Node{Kind: ui.KindText, Text: h.errLabel, Tone: ui.ToneError})
	}
	head = append(head, field)

	results := h.launcherResults
	if len(results) == 0 {
		head = append(head, &ui.Node{Kind: ui.KindText, Text: "No results"})
		return &ui.Node{Kind: ui.KindColumn, Gap: 8, Padding: 12, Children: head}
	}
	h.launcherSel = min(max(h.launcherSel, 0), len(results)-1)

	listH := h.place.Panel.H - 24 - launcherFieldHeight - 8
	if h.errLabel != "" {
		listH -= 24
	}
	if listH < launcherSlotHeight {
		listH = launcherSlotHeight
	}
	list := &ui.Node{
		Kind:         ui.KindVirtualList,
		Height:       listH,
		ItemCount:    len(results),
		ItemHeight:   launcherSlotHeight,
		ScrollOffset: h.launcherVisibleOffset(len(results)),
		Item: func(i int) *ui.Node {
			return launcherRow(r, h, results, i)
		},
	}
	return &ui.Node{Kind: ui.KindColumn, Gap: 8, Padding: 12, Children: append(head, list)}
}

func launcherRow(r *Registry, h *PanelHost, results []launcher.Result, i int) *ui.Node {
	if i < 0 || i >= len(results) {
		return nil
	}
	res := results[i]
	if h.launcherMenuID == res.Entry.ID && h.menu != nil && h.menu.Opened() {
		return h.menu.Node()
	}
	fill := ui.FillNone
	if i == h.launcherSel {
		fill = ui.FillSoft
	}
	pad := launcherRowGap / 2
	return &ui.Node{
		Kind: ui.KindColumn, Padding: pad,
		Children: []*ui.Node{{
			Kind: ui.KindCapsule, Fill: fill, Padding: 4, Shape: ui.ShapeMedium,
			Action:   "launch:" + res.Entry.ID,
			Children: []*ui.Node{launcherRowBody(r, h, res.Entry)},
		}},
	}
}

func launcherRowBody(r *Registry, h *PanelHost, e launcher.Entry) *ui.Node {
	labels := []*ui.Node{{Kind: ui.KindText, Text: e.Name, TextRole: theme.RoleLabel}}
	if e.Comment != "" {
		labels = append(labels, &ui.Node{Kind: ui.KindText, Text: e.Comment})
	}
	// Panel pad 12×2, capsule pad 4×2, row pad 4×2, glyph, gap.
	labelW := h.place.Panel.W - 24 - 8 - 8 - launcherIconSlot - 12
	if labelW < 80 {
		labelW = 80
	}
	return &ui.Node{
		Kind: ui.KindRow, Gap: 12, Padding: 4,
		Children: []*ui.Node{
			launcherIconNode(r, h, e),
			{Kind: ui.KindColumn, Gap: 2, Width: labelW, Children: labels},
		},
	}
}

func launcherIconNode(r *Registry, h *PanelHost, e launcher.Entry) *ui.Node {
	if img := launcherLookupIcon(r, h, e.IconName); img != nil {
		return &ui.Node{Kind: ui.KindImage, ImageSize: launcherIconSlot, Image: img}
	}
	return &ui.Node{
		Kind: ui.KindCapsule, Width: launcherIconSlot, Fill: ui.FillContainer, Shape: ui.ShapeMedium,
		Children: []*ui.Node{{Kind: ui.KindText, Text: launcherGlyph(e.Name), TextRole: theme.RoleTitle}},
	}
}

func launcherLookupIcon(r *Registry, h *PanelHost, name string) *ui.Image {
	if r == nil || r.trayIcons == nil || name == "" {
		return nil
	}
	size := launcherIconSlot
	if scale := ui.Scale120(h.scale120); scale.Valid() {
		size = max(scale.Physical(launcherIconSlot), 1)
	}
	key := icons.Key{Name: name, Size: size}
	if img, ok := r.trayIcons.Lookup(key); ok {
		return img
	}
	_, _, _ = r.trayIcons.Request(key)
	return nil
}

func launcherGlyph(name string) string {
	for _, r := range name {
		return string(unicode.ToUpper(r))
	}
	return "?"
}

// launcherVisibleOffset keeps the selected row inside the viewport across the
// rebuild every keystroke triggers; the wheel offset survives via
// h.launcherScroll until selection forces a correction.
//
// ponytail: the view height is derived from the fixed chrome constants rather
// than the laid-out list bounds, so an error label eats into the estimate.
func (h *PanelHost) launcherVisibleOffset(count int) int {
	viewH := h.place.Panel.H - 24 - launcherFieldHeight - 8
	maxOff := max(count*launcherSlotHeight-viewH, 0)
	off := min(max(h.launcherScroll, 0), maxOff)
	if top := h.launcherSel * launcherSlotHeight; top < off {
		off = top
	}
	if bottom := (h.launcherSel + 1) * launcherSlotHeight; bottom > off+viewH {
		off = bottom - viewH
	}
	h.launcherScroll = off
	return off
}

func (h *PanelHost) launcherKeyPress(r *Registry, key uint32) bool {
	n := len(h.launcherResults)
	page := max(h.launcherPageRows(), 1)
	switch key {
	case keyUp:
		h.launcherMoveSel(r, -1)
		return true
	case keyDown:
		h.launcherMoveSel(r, 1)
		return true
	case keyPageUp:
		h.launcherMoveSel(r, -page)
		return true
	case keyPageDown:
		h.launcherMoveSel(r, page)
		return true
	case keyHome:
		if n > 0 {
			h.launcherMoveSel(r, -h.launcherSel)
		}
		return true
	case keyEnd:
		if n > 0 {
			h.launcherMoveSel(r, n-1-h.launcherSel)
		}
		return true
	case keyEnter:
		h.launcherActivateSelected(r)
		return true
	}
	return false
}

func (h *PanelHost) launcherMoveSel(r *Registry, delta int) {
	n := len(h.launcherResults)
	if n == 0 {
		return
	}
	h.launcherSel = min(max(h.launcherSel+delta, 0), n-1)
	r.rebuildPanel(h)
}

func (h *PanelHost) launcherPageRows() int {
	viewH := h.place.Panel.H - 24 - launcherFieldHeight - 8
	return max(viewH/launcherSlotHeight, 1)
}

// launcherActivateSelected activates the highlighted row. An overview row
// (no argv, prefix ID) navigates into that provider instead of spawning.
func (h *PanelHost) launcherActivateSelected(r *Registry) {
	if len(h.launcherResults) == 0 {
		return
	}
	h.launcherSel = min(h.launcherSel, len(h.launcherResults)-1)
	res := h.launcherResults[h.launcherSel]
	if len(res.Entry.Argv) == 0 && strings.HasPrefix(res.Entry.ID, "/") {
		h.query = res.Entry.ID
		h.search = ui.NewField(h.query)
		h.launcherSel = 0
		r.launcherServiceLocked().Query(h.query)
		r.rebuildPanel(h)
		return
	}
	h.launcherSpawn(r, res.Entry.ID, "")
}

// launcherSpawn activates through the service off the Wayland goroutine: the
// panel closes on success and shows the error in place on failure (D6).
func (h *PanelHost) launcherSpawn(r *Registry, id, action string) {
	svc := r.launcherServiceLocked()
	go func() {
		err := svc.Activate(id, action)
		r.mu.Lock()
		defer r.mu.Unlock()
		host := r.panelHosts[PanelLauncher]
		if host == nil {
			return
		}
		if err != nil {
			host.errLabel = err.Error()
			r.rebuildPanel(host)
			r.publishSurface(host.output, panelSurfaceID(PanelLauncher))
			return
		}
		r.closePanelLocked(PanelLauncher)
	}()
}

// launcherPointerPress handles row clicks. A left press launches immediately
// (Noctalia's single-click); a right press opens the row's desktop actions as
// a KindMenu (D7). Rows are deliberately not focusable, so the field keeps
// keyboard focus and typing always filters.
func (h *PanelHost) launcherPointerPress(r *Registry, e wayland.Event) bool {
	x, y := int(math.Floor(e.X)), int(math.Floor(e.Y))
	id := launcherRowAt(h.root, x, y)
	if id == "" {
		return false
	}
	if e.Button == btnRight {
		return h.openLauncherActions(r, id)
	}
	for i, res := range h.launcherResults {
		if res.Entry.ID == id {
			h.launcherSel = i
			break
		}
	}
	h.launcherSpawn(r, id, "")
	return true
}

func (h *PanelHost) openLauncherActions(r *Registry, id string) bool {
	var actions []launcher.Action
	for _, res := range h.launcherResults {
		if res.Entry.ID == id {
			actions = res.Entry.Actions
			break
		}
	}
	if len(actions) == 0 {
		return true
	}
	names := make([]string, len(actions))
	for i, a := range actions {
		names[i] = a.Name
	}
	h.menu = NewMenu(names, 0)
	h.menu.Open()
	h.launcherMenuID = id
	h.launcherActions = actions
	r.rebuildPanel(h)
	return true
}

// applyLauncherMenu runs the selected desktop action after the menu closes
// with a committed selection.
func (h *PanelHost) applyLauncherMenu(r *Registry) {
	if h.menu == nil {
		return
	}
	idx := h.menu.Index()
	if idx < 0 || idx >= len(h.launcherActions) {
		return
	}
	action := h.launcherActions[idx].ID
	id := h.launcherMenuID
	h.launcherMenuID = ""
	h.launcherSpawn(r, id, action)
}

func (h *PanelHost) activateLauncher(r *Registry, n *ui.Node) bool {
	if n.Kind != ui.KindMenu || h.menu == nil {
		return false
	}
	if h.menu.Opened() {
		h.menu.Select()
		h.applyLauncherMenu(r)
	} else {
		h.menu.Open()
	}
	r.rebuildPanel(h)
	return true
}

// launcherRowAt finds the laid-out row under a point by its launch action.
func launcherRowAt(n *ui.Node, x, y int) string {
	if n == nil {
		return ""
	}
	if id, ok := strings.CutPrefix(n.Action, "launch:"); ok && n.Bounds.Contains(x, y) {
		return id
	}
	for _, c := range n.Children {
		if id := launcherRowAt(c, x, y); id != "" {
			return id
		}
	}
	return ""
}
