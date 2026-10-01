package shell

import (
	"time"

	"github.com/Nomadcxx/sysc-shell/internal/ui"
)

// screenshotPathRunes bounds the save-folder caption in the 360 px panel.
const screenshotPathRunes = 40

type screenshotRow struct{ name, action, icon string }

// The glyphs are the committed Material subset's; it has no camera or crop.
var screenshotRows = []screenshotRow{
	{"Region", "screenshot-region", "add"},
	{"Window", "screenshot-window", "web_asset"},
	{"Screen", "screenshot-screen", "desktop_windows"},
}

// screenshotTree is the launcher: a card of mode rows over the save folder.
// The session panel is its shape.
func screenshotTree(r *Registry, h *PanelHost) *ui.Node {
	m := h.theme.Metrics
	children := make([]*ui.Node, 0, 2)
	if h.errLabel != "" {
		children = append(children, &ui.Node{Kind: ui.KindText, Text: h.errLabel, Tone: ui.ToneError})
	}
	rows := []*ui.Node{monitorCardTitle("Screenshot", 0)}
	for _, row := range screenshotRows {
		rows = append(rows, &ui.Node{
			Kind: ui.KindButton, Action: row.action, Name: row.name, Role: "button", Focusable: true,
			Gap: m.ButtonPadding / 2, Padding: m.ButtonPadding, Height: m.StandardControl,
			Children: []*ui.Node{
				{Kind: ui.KindIcon, Icon: row.icon, IconSize: m.IconNormal},
				{Kind: ui.KindText, Text: row.name},
			},
		})
	}
	dir := r.screenshotDirectory()
	rows = append(rows, &ui.Node{Kind: ui.KindText, Text: shortenPath(dir, screenshotPathRunes), Name: dir})
	children = append(children, monitorCard(m, rows))
	return &ui.Node{Kind: ui.KindColumn, Gap: monitorCardGap, Padding: m.PanelPadding, Children: children}
}

// shortenPath keeps the head and the tail of p within max runes, joined by an
// ellipsis, so the folder's own name stays visible.
func shortenPath(p string, max int) string {
	r := []rune(p)
	if len(r) <= max || max < 3 {
		return p
	}
	head := (max - 1) / 2
	tail := max - 1 - head
	return string(r[:head]) + "…" + string(r[len(r)-tail:])
}

// screenshotPanelCloseDelay gives the compositor time to unmap the panel
// before a capture reads the screen. The live gate measures whether it is
// enough.
const screenshotPanelCloseDelay = 150 * time.Millisecond

// launchScreenshot runs under Registry.mu. An open selector is refused in the
// panel, which stays up so choosing again is the retry. Otherwise the panel
// closes first and the capture starts off the lock, because Screenshot takes
// it; a later failure arrives as the existing failure toast.
func (r *Registry) launchScreenshot(h *PanelHost, mode string) {
	if r.selector != nil {
		h.errLabel = errSelectorOpen.Error() + ": finish or cancel it"
		r.rebuildPanel(h)
		r.publishSurface(h.output, panelSurfaceID(h.id))
		return
	}
	start := r.startScreenshot
	if start == nil {
		start = r.Screenshot
	}
	r.closePanelLocked(h.id)
	go func() {
		time.Sleep(screenshotPanelCloseDelay)
		if err := start(mode); err != nil {
			r.screenshotToast("", err)
		}
	}()
}
