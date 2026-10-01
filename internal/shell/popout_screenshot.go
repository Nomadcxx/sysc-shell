package shell

import (
	"os"
	"os/exec"
	"time"
	"unicode/utf8"

	"github.com/Nomadcxx/sysc-shell/internal/ui"
)

const screenshotOpenFolderAction = "screenshot-open-folder"

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
	rows = append(rows, &ui.Node{
		Kind: ui.KindButton, Action: screenshotOpenFolderAction, Name: "Open folder", Role: "button", Focusable: true,
		Gap: m.ButtonPadding / 2, Padding: m.ButtonPadding, Height: m.StandardControl,
		Children: []*ui.Node{
			{Kind: ui.KindIcon, Icon: "folder_open", IconSize: m.IconNormal},
			{Kind: ui.KindText, Text: "Open folder"},
		},
	})
	dir := r.screenshotDirectory()
	_ = h.ensureText()
	avail := h.place.Panel.W - 2*m.PanelPadding - 2*m.CardPadding
	rows = append(rows, &ui.Node{Kind: ui.KindText, Text: shortenToWidth(dir, avail, h.measureText()), Name: dir})
	children = append(children, monitorCard(m, rows))
	return &ui.Node{Kind: ui.KindColumn, Gap: monitorCardGap, Padding: m.PanelPadding, Children: children}
}

// shortenToWidth is the longest shortenPath of p that measures within avail.
// Width, not character count, bounds it: a CJK path is twice as wide per rune.
func shortenToWidth(p string, avail int, measure ui.MeasureText) string {
	for n := utf8.RuneCountInString(p); ; n-- {
		s := shortenPath(p, n)
		if w, _ := measure(s, ui.TextAttrs{}); w <= avail || n <= 1 {
			return s
		}
	}
}

// shortenPath keeps the head and the tail of p within max runes, joined by an
// ellipsis, so the folder's own name stays visible.
func shortenPath(p string, max int) string {
	r := []rune(p)
	if len(r) <= max {
		return p
	}
	if max <= 0 {
		return ""
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
		h.errLabel = errSelectorOpen.Error()
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

// openScreenshotFolder runs under Registry.mu and opens the save directory off
// it, because starting a file manager is not the Wayland owner's business. The
// panel closes on success; a failure stays in the panel as its error line.
func (r *Registry) openScreenshotFolder(h *PanelHost) {
	dir := r.screenshotDirectory()
	open := r.openFolder
	if open == nil {
		open = openFolderDefault
	}
	go func() {
		err := open(dir)
		r.mu.Lock()
		defer r.mu.Unlock()
		if r.panelHosts[h.id] != h {
			return
		}
		if err != nil {
			h.errLabel = "Could not open the folder"
			r.rebuildPanel(h)
			r.publishSurface(h.output, panelSurfaceID(h.id))
			return
		}
		r.closePanelLocked(h.id)
	}()
}

// openFolderDefault creates the directory first: a fresh install has not saved
// a capture yet, and opening a missing folder would fail for no reason.
func openFolderDefault(dir string) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	cmd := exec.Command("xdg-open", dir)
	if err := cmd.Start(); err != nil {
		return err
	}
	go func() { _ = cmd.Wait() }()
	return nil
}
