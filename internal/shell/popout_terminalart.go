package shell

import (
	"fmt"
	"slices"
	"strings"

	"github.com/Nomadcxx/sysc-shell/internal/theme"
	"github.com/Nomadcxx/sysc-shell/internal/ui"
	"github.com/Nomadcxx/sysc-shell/internal/wallpaper"
)

const (
	// Three cards and two gaps fill the 640 panel's 608 interior exactly.
	artColumns = 3
	artCardW   = 196
	artCardH   = 56
	artInnerW  = artColumns*artCardW + (artColumns-1)*wallpaperGridGap
	// artPaletteMenu keys the palette dropdown in h.menus and is its action.
	artPaletteMenu = "art-palette"
	// artGridKey keys the effects grid so its scroll survives the rebuild
	// every wallpaper snapshot causes.
	artGridKey = "art-grid"
)

// terminalArtTree is the Terminal Art panel: sysc-Go effects on the wallpaper
// layer. It reads the same wallpaper service snapshot as the Wallpaper panel
// and keeps its state in the same per-host fields, so the two panels are
// separate chrome over one assignment table.
//
// Top to bottom: what the selected outputs run now, with controls per output;
// any errors; then the effects with the palette they start in. The grid
// scrolls in whatever height the chrome above leaves it.
func terminalArtTree(r *Registry, h *PanelHost) *ui.Node {
	children := []*ui.Node{artHeader(h)}
	children = append(children, artNowPlaying(h)...)
	children = append(children, artBanners(h)...)
	effects := artEffects(h)
	if h.wallpaperSnap.Caps.Terminal && len(effects) > 0 {
		children = append(children, artEffectsHeader(h, len(effects)))
		used := 0
		for _, child := range children {
			used += childHeightFor(child)
		}
		grid := &ui.Node{
			Kind: ui.KindScroll, Key: artGridKey, Gap: wallpaperGridGap,
			Height: max(h.place.Panel.H-2*wallpaperPadding-used, artCardH),
		}
		if prev := keyedScroll(h.root, artGridKey); prev != nil {
			grid.ScrollOffset = prev.ScrollOffset
		}
		for start := 0; start < len(effects); start += artColumns {
			row := &ui.Node{Kind: ui.KindRow, Gap: wallpaperGridGap, Height: artCardH}
			for i := start; i < min(start+artColumns, len(effects)); i++ {
				row.Children = append(row.Children, artCard(h, effects[i], i))
			}
			grid.Children = append(grid.Children, row)
		}
		children = append(children, grid)
	}
	return &ui.Node{
		Kind: ui.KindColumn, Padding: wallpaperPadding, Gap: wallpaperGridGap,
		Children: children,
	}
}

func artHeader(h *PanelHost) *ui.Node {
	return &ui.Node{
		Kind: ui.KindRow, Gap: wallpaperGridGap, Height: h.theme.Metrics.StandardControl,
		Children: []*ui.Node{
			{Kind: ui.KindText, Text: "Terminal Art", TextRole: theme.RoleTitle},
			wallpaperOutputSelect(h, "art-output:"),
			{
				Kind: ui.KindButton, Action: "art-close", Name: "Close",
				Role: "button", Focusable: true, Padding: wallpaperControlPad,
				Height:   h.theme.Metrics.StandardControl,
				Children: []*ui.Node{{Kind: ui.KindIcon, Icon: "close", IconSize: wallpaperIconSize}},
			},
		},
	}
}

// artNowPlaying is a section heading and one row per selected output.
func artNowPlaying(h *PanelHost) []*ui.Node {
	out := []*ui.Node{artSectionLabel("Now playing")}
	targets := wallpaperTargets(h)
	if len(targets) == 0 {
		return append(out, &ui.Node{Kind: ui.KindText, Text: "No outputs", Tone: ui.ToneSubtle, Height: wallpaperCaptionH})
	}
	// Every row's connector takes the widest one's width, so the details
	// start in one column.
	widest := slices.MaxFunc(targets, func(a, b string) int { return len(a) - len(b) })
	for _, connector := range targets {
		out = append(out, artPlayingRow(h, connector, widest))
	}
	return out
}

func artSectionLabel(text string) *ui.Node {
	return &ui.Node{Kind: ui.KindText, Text: text, TextRole: theme.RoleLabel, Tone: ui.ToneSubtle, Height: wallpaperCaptionH}
}

// artPlayingRow says what one output shows and offers only the controls its
// effect's state supports. The controls are pinned to the right edge and the
// details clip before them, so no effect or palette name can push a control
// out of the panel: that overflow once made the panel unopenable.
func artPlayingRow(h *PanelHost, connector, widest string) *ui.Node {
	info := &ui.Node{Kind: ui.KindRow, Gap: theme.MarginM, Children: []*ui.Node{
		{Kind: ui.KindText, Text: connector, TextRole: theme.RoleLabel, MinWidthText: widest},
	}}
	row := &ui.Node{
		Kind: ui.KindRow, PinEnd: true, Gap: wallpaperGridGap,
		Height: h.theme.Metrics.StandardControl, Children: []*ui.Node{info},
	}
	if !artLive(h, connector) {
		info.Children = append(info.Children, &ui.Node{Kind: ui.KindText, Text: artIdleText(h, connector), Tone: ui.ToneSubtle})
		return row
	}
	a := h.wallpaperSnap.Assignments[connector]
	state := h.wallpaperSnap.Runtime[connector].State
	stateTone := ui.ToneSubtle
	if state == wallpaper.StateError {
		stateTone = ui.ToneError
	}
	info.Children = append(info.Children,
		&ui.Node{Kind: ui.KindText, Text: a.Effect + " \u00b7 " + a.Theme},
		&ui.Node{Kind: ui.KindText, Text: wallpaperStateName(state), TextRole: theme.RoleCaption, Tone: stateTone},
	)
	controls := &ui.Node{Kind: ui.KindRow, Gap: theme.MarginS}
	switch state {
	case wallpaper.StatePlaying:
		controls.Children = append(controls.Children, artIconButton(h, "art-pause:"+connector, "pause", "Pause "+connector))
	case wallpaper.StatePaused:
		controls.Children = append(controls.Children, artIconButton(h, "art-resume:"+connector, "play_arrow", "Resume "+connector))
	}
	restore := artIconButton(h, "art-restore:"+connector, "wallpaper", "Restore still on "+connector)
	if a.PreviewPath == "" {
		restore.State |= ui.StateDisabled
		restore.Tooltip = "No previous still recorded"
	}
	controls.Children = append(controls.Children, restore)
	row.Children = append(row.Children, controls)
	return row
}

// artIdleText describes an output that is not running an effect.
func artIdleText(h *PanelHost, connector string) string {
	a, assigned := h.wallpaperSnap.Assignments[connector]
	switch {
	case !assigned:
		return "Nothing assigned"
	case a.Kind == wallpaper.KindEffect && a.PreviewPath == "":
		return "No wallpaper displayed"
	}
	return "Showing a wallpaper"
}

func artIconButton(h *PanelHost, action, icon, name string) *ui.Node {
	return &ui.Node{
		Kind: ui.KindButton, Action: action, Name: name, Tooltip: name,
		Role: "button", Focusable: true, Padding: wallpaperControlPad,
		Height:   h.theme.Metrics.StandardControl,
		Children: []*ui.Node{{Kind: ui.KindIcon, Icon: icon, IconSize: wallpaperIconSize}},
	}
}

// artEffectsHeader names the grid and holds the palette its cards start in.
// The palette is an overlay menu, so opening it does not move the grid.
func artEffectsHeader(h *PanelHost, count int) *ui.Node {
	return &ui.Node{
		Kind: ui.KindRow, PinEnd: true, Gap: wallpaperGridGap, Height: h.theme.Metrics.StandardControl,
		Children: []*ui.Node{
			{Kind: ui.KindRow, Gap: theme.MarginS, Children: []*ui.Node{
				{Kind: ui.KindText, Text: "Effects", TextRole: theme.RoleLabel, Tone: ui.ToneSubtle},
				{Kind: ui.KindText, Text: fmt.Sprint(count), TextRole: theme.RoleCaption, Tone: ui.ToneSubtle},
			}},
			{Kind: ui.KindRow, Gap: theme.MarginM, Children: []*ui.Node{
				{Kind: ui.KindText, Text: "Palette", Tone: ui.ToneSubtle},
				artPaletteControl(h),
			}},
		},
	}
}

// artPaletteControl is the palette dropdown. An open menu survives rebuilds;
// a closed one follows what the selected outputs run.
func artPaletteControl(h *PanelHost) *ui.Node {
	themes := h.wallpaperSnap.Caps.Catalog.Themes
	if h.menus == nil {
		h.menus = map[string]*Menu{}
	}
	m := h.menus[artPaletteMenu]
	if m == nil || !m.Opened() {
		m = NewMenu(themes, max(slices.Index(themes, artPalette(h)), 0))
		h.menus[artPaletteMenu] = m
	}
	n := m.Node()
	n.Action = artPaletteMenu
	n.Name = "Palette"
	n.Padding = theme.MarginXS
	n.Width = wallpaperPaletteWidth
	n.Height = h.theme.Metrics.StandardControl
	return n
}

// artPalette is the palette the dropdown shows: what a selected output is
// running, otherwise the last pick here, otherwise the catalog's first.
func artPalette(h *PanelHost) string {
	for _, connector := range wallpaperTargets(h) {
		if a := h.wallpaperSnap.Assignments[connector]; a.Kind == wallpaper.KindEffect && a.Theme != "" {
			return a.Theme
		}
	}
	return wallpaperEffectTheme(h)
}

// wallpaperEffectTheme is the last palette picked here, else the catalog's first.
func wallpaperEffectTheme(h *PanelHost) string {
	if h.wallpaperEffectTheme != "" {
		return h.wallpaperEffectTheme
	}
	if ts := h.wallpaperSnap.Caps.Catalog.Themes; len(ts) > 0 {
		return ts[0]
	}
	return "nord"
}

// artSetPalette records the pick for the next card and, because no thumbnail
// previews a palette, re-runs each selected live effect in it at once: the
// wallpaper behind the panel is the preview.
func (h *PanelHost) artSetPalette(r *Registry, name string) {
	h.wallpaperEffectTheme = name
	if svc := r.wallpaperServiceLocked(); svc != nil {
		h.wallpaperSnap = svc.Snapshot()
		for _, connector := range wallpaperTargets(h) {
			if a := h.wallpaperSnap.Assignments[connector]; artLive(h, connector) {
				svc.Enqueue(wallpaper.Command{
					Op: wallpaper.OpApply, Token: connector,
					Kind: wallpaper.KindEffect, Effect: a.Effect, Theme: name,
				})
			}
		}
	}
	r.rebuildPanel(h)
}

func artBanners(h *PanelHost) []*ui.Node {
	var out []*ui.Node
	add := func(text string) {
		if text != "" {
			out = append(out, &ui.Node{Kind: ui.KindText, Text: text, Tone: ui.ToneError, Height: wallpaperCaptionH, MaxWidth: artInnerW})
		}
	}
	if !h.wallpaperSnap.Caps.Terminal {
		add(artNotInstalled)
	}
	add(h.errLabel)
	add(h.wallpaperSnap.Err)
	for _, connector := range wallpaperTargets(h) {
		if rt := h.wallpaperSnap.Runtime[connector]; rt.Err != "" {
			add(connector + ": " + rt.Err)
		}
	}
	return out
}

// artEffects is the catalog reported by sysc-terminal.
func artEffects(h *PanelHost) []string {
	return h.wallpaperSnap.Caps.Catalog.Effects
}

// artRunningOn lists the outputs running effect id.
func artRunningOn(h *PanelHost, id string) []string {
	var out []string
	for _, connector := range h.wallpaperSnap.Connectors {
		if artLive(h, connector) && h.wallpaperSnap.Assignments[connector].Effect == id {
			out = append(out, connector)
		}
	}
	return out
}

func artCard(h *PanelHost, id string, index int) *ui.Node {
	lines := []*ui.Node{{Kind: ui.KindText, Text: id, MaxWidth: artCardW - 2*theme.MarginS}}
	card := &ui.Node{
		Kind: ui.KindCapsule, Fill: ui.FillContainerHigh,
		Width: artCardW, Height: artCardH, Padding: theme.MarginS,
		Action: "art-apply:" + id, Name: id, Role: "button", Focusable: true,
	}
	if on := artRunningOn(h, id); len(on) > 0 {
		lines = append(lines, &ui.Node{
			Kind: ui.KindText, Text: "on " + strings.Join(on, ", "), TextRole: theme.RoleCaption,
			Tone: ui.ToneAccent, MaxWidth: artCardW - 2*theme.MarginS,
		})
		card.Stroke = wallpaperSelectedStroke
		card.StrokeFill = ui.FillAccent
	}
	card.Children = []*ui.Node{{Kind: ui.KindColumn, Gap: theme.MarginXXS, Children: lines}}
	if index == h.wallpaperSel {
		card.Fill = ui.FillSoft
	}
	if !h.wallpaperSnap.Caps.Terminal {
		card.State |= ui.StateDisabled
	}
	return card
}

// artAction handles the Terminal Art panel's controls.
func (h *PanelHost) artAction(r *Registry, n *ui.Node) bool {
	if n != nil && h.id == PanelSettings {
		return h.artSettingsAction(r, n)
	}
	if h.id != PanelTerminalArt || n == nil {
		return false
	}
	if n.Action == "art-close" {
		r.closePanelLocked(PanelTerminalArt)
		return true
	}
	for prefix, op := range map[string]wallpaper.Op{
		"art-pause:": wallpaper.OpPause, "art-resume:": wallpaper.OpResume, "art-restore:": wallpaper.OpRestore,
	} {
		if connector, ok := strings.CutPrefix(n.Action, prefix); ok {
			h.artEnqueue(r, op, connector)
			return true
		}
	}
	if token, ok := strings.CutPrefix(n.Action, "art-output:"); ok {
		h.wallpaperSelectOutput(r, token)
		return true
	}
	if id, ok := strings.CutPrefix(n.Action, "art-apply:"); ok {
		h.artApply(r, id)
		return true
	}
	return false
}

// artApply runs effect id on the selected outputs.
func (h *PanelHost) artApply(r *Registry, id string) {
	if !h.wallpaperSnap.Caps.Terminal || !h.artSelectEffect(id) {
		return
	}
	h.wallpaperOutput = wallpaperOutputSelection(h.wallpaperSnap, h.wallpaperOutput)
	r.rebuildPanel(h)
	h.focusByName(id)
	if svc := r.wallpaperServiceLocked(); svc != nil {
		svc.Enqueue(wallpaper.Command{
			Op: wallpaper.OpApply, Token: h.wallpaperOutput,
			Kind: wallpaper.KindEffect, Effect: id, Theme: artPalette(h),
		})
	}
}

// artSelectEffect finds the current catalog index for a card action. Actions
// can arrive from either a key or a pointer and may outlive a rebuilt tree.
func (h *PanelHost) artSelectEffect(id string) bool {
	if i := slices.Index(artEffects(h), id); i >= 0 {
		h.wallpaperSel = i
		return true
	}
	return false
}

func artFocusedEffect(h *PanelHost, effects []string) int {
	n := h.focused()
	if n == nil {
		return -1
	}
	id, ok := strings.CutPrefix(n.Action, "art-apply:")
	if !ok {
		return -1
	}
	return slices.Index(effects, id)
}

// artKeyPress walks the three-column grid from the focused card, keeping it
// in view, and applies it on Enter.
func (h *PanelHost) artKeyPress(r *Registry, key uint32) bool {
	effects := artEffects(h)
	focused := artFocusedEffect(h, effects)
	if focused < 0 {
		return false
	}
	delta := 0
	switch key {
	case keyLeft:
		delta = -1
	case keyRight:
		delta = 1
	case keyUp:
		delta = -artColumns
	case keyDown:
		delta = artColumns
	case keyEnter:
		h.artApply(r, effects[focused])
		return true
	default:
		return false
	}
	h.wallpaperSel = gridMoveSel(focused, delta, len(effects))
	r.rebuildPanel(h)
	h.focusByName(effects[h.wallpaperSel])
	h.revealFocusedRow()
	return true
}

const artNotInstalled = "sysc-terminal is not installed. Install it to /usr/local/bin"

// terminalArtSettingsTree is Settings → Terminal Art: whether the engine is
// there, the palette the panel starts on, and the way into the panel. Picking
// a palette here writes config only; a running effect is the panel's.
func terminalArtSettingsTree(r *Registry, h *PanelHost) *ui.Node {
	var caps wallpaper.Capabilities
	if r != nil {
		if svc := r.wallpaperServiceLocked(); svc != nil {
			caps = svc.Snapshot().Caps
		}
	}
	var rows []*ui.Node
	if !caps.Terminal {
		rows = append(rows, &ui.Node{Kind: ui.KindText, Text: artNotInstalled, Tone: ui.ToneError, Height: wallpaperCaptionH})
	} else {
		effects := len(caps.Catalog.Effects)
		rows = append(rows, &ui.Node{
			Kind: ui.KindText, Text: "sysc-terminal \u00b7 sysc-Go \u00b7 " + plural(effects, "effect"),
			Role: "status", Height: wallpaperCaptionH,
		})
		palette := h.draft.TerminalArt.Palette
		if !slices.Contains(caps.Catalog.Themes, palette) && len(caps.Catalog.Themes) > 0 {
			palette = caps.Catalog.Themes[0]
		}
		combo := wallpaperCombo(h, "default", palette, wallpaperPaletteWidth)
		combo.Action = "art-menu:default"
		rows = append(rows, &ui.Node{
			Kind: ui.KindRow, Gap: wallpaperGridGap, Height: wallpaperChromeH(h),
			Children: []*ui.Node{{Kind: ui.KindText, Text: "Default palette", Height: wallpaperChromeH(h)}, combo},
		})
		if h.wallpaperMenu == "default" {
			opts := make([]wallpaperOption, 0, len(caps.Catalog.Themes))
			for _, name := range caps.Catalog.Themes {
				opts = append(opts, wallpaperOption{action: "art-default:" + name, label: name, selected: name == palette})
			}
			rows = append(rows, wallpaperOptionList(h, opts))
		}
	}
	open := wallpaperButton(h, "art-open", "Open Terminal Art", false)
	rows = append(rows, &ui.Node{Kind: ui.KindRow, Height: wallpaperChromeH(h), Children: []*ui.Node{open}})
	return settingsBody(h, theme.MarginM, rows...)
}

func (h *PanelHost) artSettingsAction(r *Registry, n *ui.Node) bool {
	switch {
	case n.Action == "art-menu:default":
		if h.wallpaperMenu == "default" {
			h.wallpaperMenu = ""
		} else {
			h.wallpaperMenu = "default"
		}
		r.rebuildPanel(h)
		return true
	case n.Action == "art-open":
		r.switchPanelLocked(h, PanelTerminalArt)
		return true
	}
	name, ok := strings.CutPrefix(n.Action, "art-default:")
	if !ok || h.set == nil {
		return false
	}
	h.wallpaperMenu = ""
	if e := h.set.ByPath("terminal-art.palette"); e != nil {
		h.commitSetting(r, e, name)
	}
	r.rebuildPanel(h)
	return true
}

// artLive reports an effect assignment that has not returned to its still.
// Starting and failed effects remain live so Restore can recover them.
func artLive(h *PanelHost, connector string) bool {
	return h.wallpaperSnap.Assignments[connector].Kind == wallpaper.KindEffect &&
		h.wallpaperSnap.Runtime[connector].State != wallpaper.StateStatic
}

// artEnqueue refreshes state and sends op to connector only when its current
// state supports it, so a control drawn from a stale snapshot does nothing.
func (h *PanelHost) artEnqueue(r *Registry, op wallpaper.Op, connector string) {
	svc := r.wallpaperServiceLocked()
	if svc == nil {
		return
	}
	h.wallpaperSnap = svc.Snapshot()
	state := h.wallpaperSnap.Runtime[connector].State
	eligible := artLive(h, connector)
	switch op {
	case wallpaper.OpPause:
		eligible = eligible && state == wallpaper.StatePlaying
	case wallpaper.OpResume:
		eligible = eligible && state == wallpaper.StatePaused
	case wallpaper.OpRestore:
		eligible = eligible && h.wallpaperSnap.Assignments[connector].PreviewPath != ""
	}
	if eligible {
		svc.Enqueue(wallpaper.Command{Op: op, Token: connector})
	}
	r.rebuildPanel(h)
}
