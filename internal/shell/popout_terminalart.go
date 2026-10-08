package shell

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/Nomadcxx/sysc-shell/internal/files"
	"github.com/Nomadcxx/sysc-shell/internal/theme"
	"github.com/Nomadcxx/sysc-shell/internal/ui"
	"github.com/Nomadcxx/sysc-shell/internal/wallpaper"
	v1 "github.com/Nomadcxx/sysc-shell/plugin/v1"
)

const (
	// Three cards and two gaps fill the 640 panel's 608 interior exactly.
	artColumns = 3
	artCardW   = 196
	artCardH   = 56
)

// terminalArtTree is the Terminal Art panel: sysc-Go effects on the wallpaper
// layer. It reads the same wallpaper service snapshot as the Wallpaper panel
// and keeps its state in the same per-host fields, so the two panels are
// separate chrome over one assignment table.
func terminalArtTree(r *Registry, h *PanelHost) *ui.Node {
	children := []*ui.Node{artHeader(h), artStatusRow(h)}
	if h.wallpaperSnap.Caps.Terminal {
		children = append(children, artPaletteRow(h))
		if h.wallpaperMenu == "palette" {
			children = append(children, wallpaperOptionList(h, artPaletteOptions(h)))
		}
	}
	children = append(children, artBanners(h)...)
	effects := artEffects(h)
	for start := 0; start < len(effects); start += artColumns {
		row := &ui.Node{Kind: ui.KindRow, Gap: wallpaperGridGap, Height: artCardH}
		for i := start; i < min(start+artColumns, len(effects)); i++ {
			row.Children = append(row.Children, artCard(h, effects[i], i))
		}
		children = append(children, row)
	}
	children = append(children, &ui.Node{
		Kind: ui.KindText, Text: plural(len(effects), "effect"),
		TextRole: theme.RoleCaption, Height: wallpaperCaptionH,
	})
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

// artStatusRow says what the selected outputs run now and offers only the
// controls their current effect states support.
func artStatusRow(h *PanelHost) *ui.Node {
	children := []*ui.Node{{Kind: ui.KindText, Text: artStatusText(h)}}
	active, playing, paused := false, false, false
	var restorable, skipped []string
	for _, connector := range wallpaperTargets(h) {
		a := h.wallpaperSnap.Assignments[connector]
		if !artLive(h, connector) {
			continue
		}
		active = true
		switch h.wallpaperSnap.Runtime[connector].State {
		case wallpaper.StatePlaying:
			playing = true
		case wallpaper.StatePaused:
			paused = true
		}
		if a.PreviewPath == "" {
			skipped = append(skipped, connector)
		} else {
			restorable = append(restorable, connector)
		}
	}
	if active {
		if playing || paused {
			action, label := "art-pause", "Pause"
			if !playing && paused {
				action, label = "art-resume", "Resume"
			}
			children = append(children, wallpaperButton(h, action, label, false))
		}
		restore := wallpaperButton(h, "art-restore", "Restore still", false)
		if len(restorable) == 0 {
			restore.State |= ui.StateDisabled
			restore.Tooltip = "No previous still recorded"
		} else if len(skipped) > 0 {
			restore.Tooltip = fmt.Sprintf("Restores %s; skips %s without a previous still",
				strings.Join(restorable, ", "), strings.Join(skipped, ", "))
		}
		children = append(children, restore)
	}
	return &ui.Node{
		Kind: ui.KindRow, Gap: wallpaperGridGap, Height: h.theme.Metrics.StandardControl,
		Children: children,
	}
}

func artStatusText(h *PanelHost) string {
	snap := h.wallpaperSnap
	var parts []string
	for _, connector := range wallpaperTargets(h) {
		a, assigned := snap.Assignments[connector]
		if !assigned {
			parts = append(parts, connector+" \u00b7 nothing assigned")
			continue
		}
		if a.Kind != wallpaper.KindEffect {
			parts = append(parts, connector+" \u00b7 showing a wallpaper")
			continue
		}
		if snap.Runtime[connector].State == wallpaper.StateStatic {
			if a.PreviewPath == "" {
				parts = append(parts, connector+" \u00b7 no wallpaper displayed")
			} else {
				parts = append(parts, connector+" \u00b7 showing a wallpaper")
			}
			continue
		}
		parts = append(parts, fmt.Sprintf("%s \u00b7 %s \u00b7 %s \u00b7 %s", connector, a.Effect, a.Theme,
			wallpaperStateName(snap.Runtime[connector].State)))
	}
	if len(parts) == 0 {
		return "No outputs"
	}
	return strings.Join(parts, "   ")
}

func artPaletteRow(h *PanelHost) *ui.Node {
	combo := wallpaperCombo(h, "palette", artPalette(h), wallpaperPaletteWidth)
	combo.Action = "art-menu:palette"
	return &ui.Node{
		Kind: ui.KindRow, Gap: wallpaperGridGap, Height: wallpaperChromeH(h),
		Children: []*ui.Node{
			{Kind: ui.KindText, Text: "Palette", Height: wallpaperChromeH(h)},
			combo,
		},
	}
}

// artPalette is the palette the combo shows: what a selected output is
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

func artPaletteOptions(h *PanelHost) []wallpaperOption {
	current := artPalette(h)
	out := make([]wallpaperOption, 0, len(h.wallpaperSnap.Caps.Catalog.Themes))
	for _, name := range h.wallpaperSnap.Caps.Catalog.Themes {
		out = append(out, wallpaperOption{action: "art-palette:" + name, label: name, selected: name == current})
	}
	return out
}

func artBanners(h *PanelHost) []*ui.Node {
	var out []*ui.Node
	add := func(text string) {
		if text != "" {
			out = append(out, &ui.Node{Kind: ui.KindText, Text: text, Tone: ui.ToneError, Height: wallpaperCaptionH})
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
func artEffects(h *PanelHost) []wallpaper.EffectInfo {
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

func artCard(h *PanelHost, effect wallpaper.EffectInfo, index int) *ui.Node {
	id := effect.ID
	lines := []*ui.Node{{Kind: ui.KindText, Text: id, MaxWidth: artCardW - 2*theme.MarginS}}
	card := &ui.Node{
		Kind: ui.KindCapsule, Fill: ui.FillContainerHigh,
		Width: artCardW, Height: artCardH, Padding: theme.MarginS,
		Action: "art-apply:" + id, Name: id, Role: "button", Focusable: true,
	}
	var details []string
	if on := artRunningOn(h, id); len(on) > 0 {
		details = append(details, "on "+strings.Join(on, ", "))
		card.Stroke = wallpaperSelectedStroke
		card.StrokeFill = ui.FillAccent
	}
	if effect.Text {
		details = append(details, "requires artwork")
	}
	if len(details) > 0 {
		lines = append(lines, &ui.Node{Kind: ui.KindText, Text: strings.Join(details, " · "), TextRole: theme.RoleCaption})
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
	switch n.Action {
	case "art-close":
		r.closePanelLocked(PanelTerminalArt)
		return true
	case "art-pause":
		h.artEnqueue(r, wallpaper.OpPause)
		return true
	case "art-resume":
		h.artEnqueue(r, wallpaper.OpResume)
		return true
	case "art-restore":
		h.artEnqueue(r, wallpaper.OpRestore)
		return true
	}
	if n.Action == "art-menu:palette" {
		if h.wallpaperMenu == "palette" {
			h.wallpaperMenu = ""
		} else {
			h.wallpaperMenu = "palette"
		}
		r.rebuildPanel(h)
		return true
	}
	if token, ok := strings.CutPrefix(n.Action, "art-output:"); ok {
		h.wallpaperSelectOutput(r, token)
		return true
	}
	if id, ok := strings.CutPrefix(n.Action, "art-apply:"); ok {
		h.artApply(r, id)
		return true
	}
	if name, ok := strings.CutPrefix(n.Action, "art-palette:"); ok {
		h.wallpaperMenu = ""
		h.wallpaperEffectTheme = name
		// No thumbnails preview a palette, so a running effect takes it at
		// once and the wallpaper behind the panel is the preview.
		if svc := r.wallpaperServiceLocked(); svc != nil {
			h.wallpaperSnap = svc.Snapshot()
			for _, connector := range wallpaperTargets(h) {
				if a := h.wallpaperSnap.Assignments[connector]; artLive(h, connector) {
					svc.Enqueue(wallpaper.Command{
						Op: wallpaper.OpApply, Token: connector,
						Kind: wallpaper.KindEffect, Effect: a.Effect, Theme: name, Artwork: a.Artwork,
					})
				}
			}
		}
		r.rebuildPanel(h)
		return true
	}
	return false
}

// artApply runs effect id on the selected outputs.
func (h *PanelHost) artApply(r *Registry, id string) {
	if !h.wallpaperSnap.Caps.Terminal {
		return
	}
	effect, ok := h.artSelectEffect(id)
	if !ok {
		return
	}
	h.wallpaperOutput = wallpaperOutputSelection(h.wallpaperSnap, h.wallpaperOutput)
	r.rebuildPanel(h)
	h.focusByName(id)
	if effect.Text {
		h.artPickArtwork(r, id)
		return
	}
	if svc := r.wallpaperServiceLocked(); svc != nil {
		svc.Enqueue(wallpaper.Command{
			Op: wallpaper.OpApply, Token: h.wallpaperOutput,
			Kind: wallpaper.KindEffect, Effect: id, Theme: artPalette(h),
		})
	}
}

// artSelectEffect finds the current catalog index for a card action. Actions
// can arrive from either a key or a pointer and may outlive a rebuilt tree.
func (h *PanelHost) artSelectEffect(id string) (wallpaper.EffectInfo, bool) {
	for i, effect := range artEffects(h) {
		if effect.ID == id {
			h.wallpaperSel = i
			return effect, true
		}
	}
	return wallpaper.EffectInfo{}, false
}

// artPickArtwork runs the existing jailed file browser off Registry.mu, then
// applies the still-listed effect with the selected path.
func (h *PanelHost) artPickArtwork(r *Registry, id string) {
	svc := r.wallpaperServiceLocked()
	if svc == nil {
		return
	}
	output := h.wallpaperOutput
	theme := artPalette(h)
	r.scheduleControl(h, func() error {
		home, err := os.UserHomeDir()
		if err != nil {
			return fmt.Errorf("find home directory: %w", err)
		}
		picked, err := r.openFilesBrowser(context.Background(), v1.FilesBrowseParams{
			Root: filepath.Join(home, ".config"), Title: "Select artwork", Mode: files.ModePickFile,
		})
		if errors.Is(err, errFilesCancelled) {
			return nil
		}
		if err != nil {
			return err
		}
		snap := svc.Snapshot()
		found := false
		for _, effect := range snap.Caps.Catalog.Effects {
			if effect.ID == id {
				found = true
				break
			}
		}
		if !snap.Caps.Terminal || !found {
			return fmt.Errorf("effect %q is no longer available", id)
		}
		if !slices.Contains(snap.Caps.Catalog.Themes, theme) && len(snap.Caps.Catalog.Themes) > 0 {
			theme = snap.Caps.Catalog.Themes[0]
		}
		svc.Enqueue(wallpaper.Command{
			Op: wallpaper.OpApply, Token: wallpaperOutputSelection(snap, output),
			Kind: wallpaper.KindEffect, Effect: id, Theme: theme, Artwork: picked.Path,
		})
		return nil
	})
}

func artFocusedEffect(h *PanelHost, effects []wallpaper.EffectInfo) int {
	n := h.focused()
	if n == nil {
		return -1
	}
	id, ok := strings.CutPrefix(n.Action, "art-apply:")
	if !ok {
		return -1
	}
	for i, effect := range effects {
		if effect.ID == id {
			return i
		}
	}
	return -1
}

// artKeyPress walks the three-column grid from the focused card and applies it.
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
		h.artApply(r, effects[focused].ID)
		return true
	default:
		return false
	}
	h.wallpaperSel = gridMoveSel(focused, delta, len(effects))
	r.rebuildPanel(h)
	h.focusByName(effects[h.wallpaperSel].ID)
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

// artEnqueue refreshes state and sends op only to eligible selected effects,
// so stale controls and All outputs cannot reach unrelated assignments.
func (h *PanelHost) artEnqueue(r *Registry, op wallpaper.Op) {
	svc := r.wallpaperServiceLocked()
	if svc == nil {
		return
	}
	h.wallpaperSnap = svc.Snapshot()
	for _, connector := range wallpaperTargets(h) {
		if !artLive(h, connector) {
			continue
		}
		switch op {
		case wallpaper.OpPause:
			if h.wallpaperSnap.Runtime[connector].State != wallpaper.StatePlaying {
				continue
			}
		case wallpaper.OpResume:
			if h.wallpaperSnap.Runtime[connector].State != wallpaper.StatePaused {
				continue
			}
		case wallpaper.OpRestore:
			if h.wallpaperSnap.Assignments[connector].PreviewPath == "" {
				continue
			}
		}
		svc.Enqueue(wallpaper.Command{Op: op, Token: connector})
	}
	r.rebuildPanel(h)
}
