package shell

import (
	"fmt"

	"github.com/Nomadcxx/sysc-shell/internal/theme"
	"github.com/Nomadcxx/sysc-shell/internal/ui"
)

const (
	// powerRingGutter keeps the outward keyboard focus ring inside the
	// scroll viewport on every side.
	powerRingGutter   = 5
	powerCompactWidth = 420
	powerCardMinWidth = 136
	// powerCardMaxWidth keeps full-screen cards card-sized on wide outputs.
	powerCardMaxWidth = 176
	// powerDialogWidth bounds the measure of confirmation and failure text.
	powerDialogWidth = 400
)

func powerColumns(width, cardWidth, gap int, full bool) int {
	counts := []int{2, 1}
	if full {
		counts = []int{6, 3, 2, 1}
	}
	for _, n := range counts {
		if n*cardWidth+(n-1)*gap <= width {
			return n
		}
	}
	return 1
}

func powerGap() int { return max(theme.MarginM, 8) }

// powerPad is the inset around the header and body: the compact surface's,
// and the full-screen sheet's.
func (h *PanelHost) powerPad() int { return h.metrics().PanelPadding + powerRingGutter }

// powerMargin keeps the full-screen sheet off the output edges.
func (h *PanelHost) powerMargin() int { return h.metrics().PanelPadding }

func (h *PanelHost) powerCardSize() (int, int) {
	m := h.metrics()
	w, lh := 0, 0
	for _, a := range sessionActionOptions() {
		tw, th := h.measureText()(h.powerActionLabel(a), ui.TextAttrs{})
		w, lh = max(w, tw), max(lh, th)
	}
	return max(powerCardMinWidth, w+2*m.CardPadding), max(m.StandardControl, h.powerIconSize()+lh+2*m.CardPadding+theme.MarginM)
}

func (h *PanelHost) powerHeaderHeight() int {
	_, th := h.measureText()(h.tr("power.title", "Power"), ui.TextAttrs{Role: theme.RoleHeadline})
	return max(h.metrics().StandardControl, th)
}

func powerTree(h *PanelHost) *ui.Node {
	m := h.metrics()
	gap, pad := powerGap(), h.powerPad()
	w, ht := h.place.Panel.W, h.place.Panel.H
	if h.logicalW > 0 {
		w, ht = h.logicalW, h.logicalH
	}
	outer := 0
	if h.powerFullscreen {
		outer = h.powerMargin()
	}
	avail := max(1, w-2*outer-2*pad-2*powerRingGutter)

	mode, modeKey, modeIcon := "Full screen", "power.fullscreen", "fullscreen"
	if h.powerFullscreen {
		mode, modeKey, modeIcon = "Compact view", "power.compact", "fullscreen_exit"
	}
	title := h.tr("power.title", "Power")
	header := &ui.Node{Kind: ui.KindRow, Gap: gap, PinEnd: true, Height: h.powerHeaderHeight(), Children: []*ui.Node{
		{Kind: ui.KindText, Text: title, TextRole: theme.RoleHeadline, Name: title, Role: "heading"},
		{Kind: ui.KindRow, Gap: gap, Children: []*ui.Node{
			powerIconButton(h, "power:mode", h.tr(modeKey, mode), modeIcon),
			powerIconButton(h, "power:close", h.tr("power.close", "Close"), "close"),
		}},
	}}

	var content *ui.Node
	switch {
	case h.sessionPending:
		content = &ui.Node{Kind: ui.KindColumn, Gap: gap, Width: min(avail, powerDialogWidth), Children: []*ui.Node{
			{Kind: ui.KindSpinner, Width: m.IconLarge, Height: m.IconLarge, CenterX: true},
			{Kind: ui.KindText, Text: h.tr("power.requesting", "Requesting action…"), CenterX: true},
		}}
	case h.powerFailed != "":
		label := h.sessionActionLabel(h.powerFailed)
		content = h.powerDialog(avail, fmt.Sprintf(h.tr("power.failed", "%s failed"), label), h.errLabel,
			h.powerDialogButton("power:back", h.tr("power.back", "Back"), ui.FillNone, ui.ToneNormal),
			h.powerDialogButton("power:retry", h.tr("power.retry", "Try again"), ui.FillAccent, ui.ToneNormal))
	case h.powerConfirm != "":
		content = h.powerDialog(avail, h.powerConfirmTitle(h.powerConfirm),
			h.tr("power.unsaved", "Open apps will close. Unsaved work may be lost."),
			h.powerDialogButton("power:cancel", h.tr("power.cancel", "Cancel"), ui.FillNone, ui.ToneNormal),
			h.powerDialogButton("power:confirm", h.sessionActionLabel(h.powerConfirm), ui.FillError, ui.ToneError))
	default:
		content = h.powerDeck(avail)
	}
	contentH, _ := ui.ContentHeight(content, avail, h.measureText())

	if !h.powerFullscreen {
		bodyH := max(1, ht-2*pad-header.Height-gap)
		return &ui.Node{Kind: ui.KindColumn, Padding: pad, Gap: gap, Children: []*ui.Node{header, powerBody(content, bodyH, contentH)}}
	}
	headerW, _ := h.measureText()(title, ui.TextAttrs{Role: theme.RoleHeadline})
	headerW += 2*m.StandardControl + 2*gap
	sheetW := min(w-2*outer, max(content.Width+2*powerRingGutter, headerW)+2*pad)
	bodyH := max(1, min(contentH+2*powerRingGutter, ht-2*outer-2*pad-header.Height-gap))
	sheet := &ui.Node{
		Kind: ui.KindCapsule, Key: "power-sheet", Fill: ui.FillRole, FillRole: ui.PaintSurface, InkRole: ui.PaintOnSurface, Shape: ui.ShapePanel,
		Padding: pad, Width: sheetW, CenterX: true, CenterY: true,
		Children: []*ui.Node{{Kind: ui.KindColumn, Gap: gap, Children: []*ui.Node{header, powerBody(content, bodyH, contentH)}}},
	}
	return &ui.Node{Kind: ui.KindColumn, Padding: outer, Height: ht, Children: []*ui.Node{sheet}}
}

// powerBody scrolls content that outgrows the room, with the ring gutter
// reserved on every side and the scrollbar out in the surrounding padding.
func powerBody(content *ui.Node, height, contentH int) *ui.Node {
	content.CenterX = true
	return &ui.Node{Kind: ui.KindScroll, Key: "power-body", Height: height, Padding: powerRingGutter, ScrollbarGap: 2, Children: []*ui.Node{
		{Kind: ui.KindColumn, Height: max(height-2*powerRingGutter, contentH), Children: []*ui.Node{content}},
	}}
}

// powerDeck lays the six action cards out in as many columns as fit. Every
// card is neutral: the confirmation, not the chooser, carries the warning.
func (h *PanelHost) powerDeck(avail int) *ui.Node {
	m := h.metrics()
	gap := powerGap()
	cw, _ := h.powerCardSize()
	cols := powerColumns(avail, cw, gap, h.powerFullscreen)
	if h.powerFullscreen {
		cw = max(cw, min(powerCardMaxWidth, (avail-(cols-1)*gap)/cols))
	} else {
		cw = max(1, (avail-(cols-1)*gap)/cols)
	}
	ch := h.powerCardHeight(cw)
	deck := &ui.Node{Kind: ui.KindColumn, Gap: gap, Width: cols*cw + (cols-1)*gap}
	var row *ui.Node
	for i, a := range sessionActionOptions() {
		if i%cols == 0 {
			row = &ui.Node{Kind: ui.KindRow, Gap: gap, Height: ch}
			deck.Children = append(deck.Children, row)
		}
		label := h.powerActionLabel(a)
		n := &ui.Node{Kind: ui.KindButton, Key: a.id, Shape: ui.ShapeCard, Action: a.id, Name: label, Role: "button", Focusable: true, Width: cw, Height: ch, Padding: m.CardPadding, Children: []*ui.Node{
			{Kind: ui.KindColumn, Gap: theme.MarginM, Children: []*ui.Node{
				{Kind: ui.KindIcon, Icon: a.icon, IconSize: h.powerIconSize(), CenterX: true},
				h.powerCardLabel(label, max(1, cw-2*m.CardPadding)),
			}},
		}}
		if a.id == "session-lock" && !h.powerLockAvailable {
			n.State |= ui.StateDisabled
		}
		row.Children = append(row.Children, n)
	}
	return deck
}

// powerDialog is the Material basic dialog: headline, supporting text and
// trailing actions, which stack when one row does not fit.
func (h *PanelHost) powerDialog(avail int, headline, body string, cancel, confirm *ui.Node) *ui.Node {
	gap := powerGap()
	w := min(avail, powerDialogWidth)
	var actions *ui.Node
	if cancel.Width+gap+confirm.Width <= w {
		actions = &ui.Node{Kind: ui.KindRow, PinEnd: true, Height: cancel.Height, Children: []*ui.Node{
			{Kind: ui.KindColumn},
			{Kind: ui.KindRow, Gap: gap, Children: []*ui.Node{cancel, confirm}},
		}}
	} else {
		cancel.Width, confirm.Width = w, w
		actions = &ui.Node{Kind: ui.KindColumn, Gap: gap, Children: []*ui.Node{confirm, cancel}}
	}
	return &ui.Node{Kind: ui.KindColumn, Gap: gap, Width: w, Children: []*ui.Node{
		h.wrappedText(headline, theme.RoleTitle, ui.ToneNormal, w, 0),
		h.wrappedText(body, theme.RoleBody, ui.ToneNormal, w, 0),
		actions,
	}}
}

func (h *PanelHost) powerDialogButton(action, label string, fill ui.Fill, tone ui.Tone) *ui.Node {
	m := h.metrics()
	tw, _ := h.measureText()(label, ui.TextAttrs{})
	return &ui.Node{Kind: ui.KindButton, Action: action, Key: action, Name: label, Text: label, Role: "button", Focusable: true,
		Fill: fill, Tone: tone, Height: m.StandardControl, Width: max(tw+4*m.ButtonPadding, 2*m.StandardControl)}
}

func (h *PanelHost) powerConfirmTitle(action string) string {
	switch action {
	case "session-logout":
		return h.tr("power.confirm.session-logout", "Log out?")
	case "session-reboot":
		return h.tr("power.confirm.session-reboot", "Restart?")
	}
	return h.tr("power.confirm.session-poweroff", "Power off?")
}

func powerIconButton(h *PanelHost, action, name, icon string) *ui.Node {
	m := h.metrics()
	return &ui.Node{Kind: ui.KindButton, Action: action, Key: action, Name: name, Role: "button", Focusable: true, Width: m.StandardControl, Height: m.StandardControl, Children: []*ui.Node{{Kind: ui.KindIcon, Icon: icon, IconSize: m.IconNormal}}}
}

func (h *PanelHost) focusPowerAction(action string) {
	for i, n := range h.focus {
		if n.Action == action && !n.State.Has(ui.StateDisabled) {
			h.roving.Set(i)
			return
		}
	}
}

func (h *PanelHost) activatePower(r *Registry, n *ui.Node) bool {
	if r.panelHosts[h.id] != h || n == nil || n.State.Has(ui.StateDisabled) {
		return false
	}
	switch n.Action {
	case "power:close":
		r.dismissPowerLocked(h)
		return true
	case "power:mode":
		r.setPowerPresentationLocked(h, !h.powerFullscreen)
		return true
	}
	if h.sessionPending {
		return true
	}
	switch n.Action {
	case "power:cancel":
		action := h.powerConfirm
		h.powerConfirm, h.errLabel = "", ""
		r.rebuildPanel(h)
		h.focusPowerAction(action)
		return true
	case "power:back":
		action := h.powerFailed
		h.powerFailed, h.errLabel = "", ""
		r.rebuildPanel(h)
		h.focusPowerAction(action)
		return true
	case "power:retry":
		if h.powerFailed == "" {
			return false
		}
		action := h.powerFailed
		h.powerFailed, h.errLabel = "", ""
		r.runSessionAction(h, action)
		r.rebuildPanel(h)
		return true
	case "power:confirm":
		if h.powerConfirm == "" {
			return false
		}
		r.runSessionAction(h, h.powerConfirm)
		r.rebuildPanel(h)
		return true
	}
	for _, a := range sessionActionOptions() {
		if n.Action != a.id {
			continue
		}
		if a.id == "session-lock" && r.cfg.Session.Locker == "" {
			return false
		}
		h.errLabel, h.powerFailed = "", ""
		if a.destructive {
			h.powerConfirm = a.id
			r.rebuildPanel(h)
			h.focusPowerAction("power:cancel")
		} else {
			r.runSessionAction(h, a.id)
			r.rebuildPanel(h)
		}
		return true
	}
	return false
}

func (h *PanelHost) powerKeyInput(r *Registry, k ui.KeyInput) bool {
	if k.Mods.Has(ui.ModCtrl | ui.ModAlt | ui.ModSuper) {
		return false
	}
	switch k.Sym {
	case ui.SymEscape:
		switch {
		case h.sessionPending:
		case h.powerFailed != "":
			return h.activatePower(r, &ui.Node{Action: "power:back"})
		case h.powerConfirm != "":
			return h.activatePower(r, &ui.Node{Action: "power:cancel"})
		}
		r.dismissPowerLocked(h)
		return true
	case ui.SymTab, ui.SymRight, ui.SymDown:
		if k.Mods.Has(ui.ModShift) {
			h.roving.Prev()
		} else {
			h.roving.Next()
		}
	case ui.SymISOLeftTab, ui.SymLeft, ui.SymUp:
		h.roving.Prev()
	case ui.SymReturn, ui.SymKPEnter, ' ':
		return h.activatePower(r, h.focused())
	default:
		return false
	}
	h.afterFocusChange(r)
	return true
}

func (r *Registry) dismissPowerLocked(h *PanelHost) {
	output, back := h.output, h.powerReturnSection
	r.closePanelLocked(h.id)
	if back != "" {
		if err := r.openPanelRootLocked(PanelControlCenter, output, h.powerTrigger); err == nil {
			cc := r.panelHosts[PanelControlCenter]
			cc.section = back
			r.rebuildPanel(cc)
			for i, n := range cc.focus {
				if n.Action == "cc:power" && (h.powerReturnName == "" || n.Name == h.powerReturnName) {
					cc.roving.Set(i)
					break
				}
			}
			cc.focusVisible = h.focusVisible
		}
	}
}

func (r *Registry) setPowerPresentationLocked(h *PanelHost, full bool) {
	if h.powerFullscreen == full {
		return
	}
	h.powerFullscreen = full
	h.place = r.panelPlacementLocked(PanelPower, h.output, h.powerTrigger, panelTargetSize(PanelPower))
	h.fitPowerPlacement()
	h.rect = h.place.Rect()
	h.logicalW, h.logicalH = h.place.Panel.W, h.place.Panel.H
	h.syncPowerGround(h.logicalW, h.logicalH)
	r.rebuildPanel(h)
	r.sendPanelPlacementLocked(h)
	r.publishSurface(h.output, panelSurfaceID(h.id))
}

// syncPowerGround keeps the captured backdrop only at the size it was captured
// at. The platform recaptures the region the surface opened with, so a resized
// view would stretch a stale frame; full screen dims the live desktop instead.
// ponytail: blur in full screen needs an AuxUpdate that moves the capture region.
func (h *PanelHost) syncPowerGround(w, ht int) {
	h.powerGroundFallback = h.powerFullscreen || h.powerOpenSize != (ui.Rect{W: w, H: ht})
}

func (h *PanelHost) fitPowerPlacement() {
	h.powerFitScale = h.scale120
	if h.powerFullscreen {
		h.place.BarZone, h.place.Gap, h.place.Padding = 0, 0, 0
		h.place.Panel = h.place.Output
		return
	}
	cw, _ := h.powerCardSize()
	gap, pad := powerGap(), h.powerPad()
	maxWidth := max(1, h.place.Output.W-2*h.place.Padding)
	h.place.Panel.W = min(maxWidth, max(powerCompactWidth, 2*cw+gap+2*pad+2*powerRingGutter))
	avail := max(1, h.place.Panel.W-2*pad-2*powerRingGutter)
	cols := powerColumns(avail, cw, gap, false)
	ch := h.powerCardHeight(max(1, (avail-(cols-1)*gap)/cols))
	rows := (len(sessionActionOptions()) + cols - 1) / cols
	h.place.Panel.H = h.powerHeaderHeight() + 2*pad + gap + rows*ch + (rows-1)*gap + 2*powerRingGutter
	h.place.Panel.W, h.place.Panel.H = h.place.FittedSize()
}

func (h *PanelHost) powerActionLabel(a sessionAction) string {
	if a.id == "session-lock" && h.powerLockLabel != "" {
		return h.powerLockLabel
	}
	return h.sessionActionLabel(a.id)
}

// sessionActionLabel names an action in the interface language.
func (h *PanelHost) sessionActionLabel(action string) string {
	for _, a := range sessionActionOptions() {
		if a.id == action {
			return h.tr("session.action."+a.key, a.name)
		}
	}
	return action
}

func (h *PanelHost) powerCardHeight(width int) int {
	m := h.metrics()
	height := m.StandardControl
	for _, a := range sessionActionOptions() {
		label := h.powerCardLabel(h.powerActionLabel(a), max(1, width-2*m.CardPadding))
		lh, _ := ui.ContentHeight(label, max(1, width-2*m.CardPadding), h.measureText())
		height = max(height, h.powerIconSize()+lh+2*m.CardPadding+theme.MarginM)
	}
	return height
}

func (h *PanelHost) powerIconSize() int {
	if h.powerFullscreen {
		return h.metrics().IconNormal * 2
	}
	return h.metrics().IconLarge
}

func (h *PanelHost) powerCardLabel(text string, width int) *ui.Node {
	n := h.wrappedText(text, theme.RoleBody, ui.ToneNormal, width, 0)
	for _, line := range n.Children {
		line.CenterX = true
	}
	return n
}
