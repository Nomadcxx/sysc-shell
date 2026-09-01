package shell

import (
	"math"
	"os"
	"path/filepath"
	"strings"

	launcher "github.com/Nomadcxx/sysc-launch"
	"github.com/Nomadcxx/sysc-shell/internal/platform/wayland"
	"github.com/Nomadcxx/sysc-shell/internal/ui"
)

// Launcher chrome constants (D2): one field above a 48px-row list. The 40px
// icon slot is recorded so the KindImage swap (sysc-86) never changes layout.
const (
	launcherRowHeight   = 48
	launcherFieldHeight = 38
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
// list of results. The selected row is a full-width Primary-filled button;
// every other row is plain text (D11).
func launcherTree(h *PanelHost) *ui.Node {
	if h.search == nil {
		h.search = ui.NewField("")
	}
	field := h.search.Node("Search")
	field.Width = max(h.place.Panel.W-24, 0)

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

	list := &ui.Node{
		Kind:         ui.KindVirtualList,
		ItemCount:    len(results),
		ItemHeight:   launcherRowHeight,
		ScrollOffset: h.launcherVisibleOffset(len(results)),
		Item: func(i int) *ui.Node {
			return launcherRow(h, results, i)
		},
	}
	return &ui.Node{Kind: ui.KindColumn, Gap: 8, Padding: 12, Children: append(head, list)}
}

func launcherRow(h *PanelHost, results []launcher.Result, i int) *ui.Node {
	if i < 0 || i >= len(results) {
		return nil
	}
	res := results[i]
	if h.launcherMenuID == res.Entry.ID && h.menu != nil && h.menu.Opened() {
		return h.menu.Node()
	}
	if i == h.launcherSel {
		return &ui.Node{
			Kind: ui.KindButton, Text: res.Entry.Name, Padding: 12,
			Action: "launch:" + res.Entry.ID,
		}
	}
	return &ui.Node{
		Kind: ui.KindText, Text: res.Entry.Name, Padding: 12,
		Action: "launch:" + res.Entry.ID,
	}
}

// launcherVisibleOffset keeps the selected row inside the viewport across the
// rebuild every keystroke triggers; the wheel offset survives via
// h.launcherScroll until selection forces a correction.
//
// ponytail: the view height is derived from the fixed chrome constants rather
// than the laid-out list bounds, so an error label eats into the estimate.
func (h *PanelHost) launcherVisibleOffset(count int) int {
	viewH := h.place.Panel.H - 24 - launcherFieldHeight - 8
	maxOff := max(count*launcherRowHeight-viewH, 0)
	off := min(max(h.launcherScroll, 0), maxOff)
	if top := h.launcherSel * launcherRowHeight; top < off {
		off = top
	}
	if bottom := (h.launcherSel + 1) * launcherRowHeight; bottom > off+viewH {
		off = bottom - viewH
	}
	h.launcherScroll = off
	return off
}

func (h *PanelHost) launcherKeyPress(r *Registry, key uint32) bool {
	switch key {
	case keyUp, keyDown:
		delta := 1
		if key == keyUp {
			delta = -1
		}
		if n := len(h.launcherResults); n > 0 {
			h.launcherSel = min(max(h.launcherSel+delta, 0), n-1)
			r.rebuildPanel(h)
		}
		return true
	case keyEnter:
		h.launcherActivateSelected(r)
		return true
	}
	return false
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
