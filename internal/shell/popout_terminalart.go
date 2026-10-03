package shell

import (
	"fmt"
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
			row.Children = append(row.Children, artCard(h, effects[i].ID, i))
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

// artStatusRow says what the selected outputs run now, with Pause and
// Restore still when any of them runs an effect.
func artStatusRow(h *PanelHost) *ui.Node {
	children := []*ui.Node{{Kind: ui.KindText, Text: artStatusText(h)}}
	running, paused, still := false, false, false
	for _, connector := range wallpaperTargets(h) {
		a := h.wallpaperSnap.Assignments[connector]
		if a.Kind != wallpaper.KindEffect {
			continue
		}
		running = true
		paused = paused || h.wallpaperSnap.Runtime[connector].State == wallpaper.StatePaused
		still = still || a.PreviewPath != ""
	}
	if running {
		action, label := "art-pause", "Pause"
		if paused {
			action, label = "art-resume", "Resume"
		}
		restore := wallpaperButton(h, "art-restore", "Restore still", false)
		if !still {
			restore.State |= ui.StateDisabled
			restore.Tooltip = "No previous still recorded"
		}
		children = append(children, wallpaperButton(h, action, label, false), restore)
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
		a := snap.Assignments[connector]
		if a.Kind != wallpaper.KindEffect {
			parts = append(parts, connector+" \u00b7 showing a wallpaper")
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
		add("sysc-terminal is not installed. Install it to /usr/local/bin")
	}
	add(h.wallpaperSnap.Err)
	for _, connector := range h.wallpaperSnap.Connectors {
		if h.wallpaperSnap.Assignments[connector].Kind != wallpaper.KindEffect {
			continue
		}
		if rt := h.wallpaperSnap.Runtime[connector]; rt.Err != "" {
			add(connector + ": " + rt.Err)
		}
	}
	return out
}

// artEffects is the catalog without text effects, which need artwork the
// shell has no way to supply yet.
func artEffects(h *PanelHost) []wallpaper.EffectInfo {
	var out []wallpaper.EffectInfo
	for _, e := range h.wallpaperSnap.Caps.Catalog.Effects {
		if !e.Text {
			out = append(out, e)
		}
	}
	return out
}

// artRunningOn lists the outputs running effect id.
func artRunningOn(h *PanelHost, id string) []string {
	var out []string
	for _, connector := range h.wallpaperSnap.Connectors {
		if a := h.wallpaperSnap.Assignments[connector]; a.Kind == wallpaper.KindEffect && a.Effect == id {
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
		lines = append(lines, &ui.Node{Kind: ui.KindText, Text: "on " + strings.Join(on, ", "), TextRole: theme.RoleCaption})
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
	if h.id != PanelTerminalArt || n == nil {
		return false
	}
	switch n.Action {
	case "art-close":
		r.closePanelLocked(PanelTerminalArt)
		return true
	case "art-pause":
		h.wallpaperSetPaused(r, true)
		return true
	case "art-resume":
		h.wallpaperSetPaused(r, false)
		return true
	case "art-restore":
		h.wallpaperRestore(r)
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
			for _, connector := range wallpaperTargets(h) {
				if a := h.wallpaperSnap.Assignments[connector]; a.Kind == wallpaper.KindEffect {
					svc.Enqueue(wallpaper.Command{
						Op: wallpaper.OpApply, Token: connector,
						Kind: wallpaper.KindEffect, Effect: a.Effect, Theme: name,
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
	h.wallpaperOutput = wallpaperOutputSelection(h.wallpaperSnap, h.wallpaperOutput)
	if svc := r.wallpaperServiceLocked(); svc != nil {
		svc.Enqueue(wallpaper.Command{
			Op: wallpaper.OpApply, Token: h.wallpaperOutput,
			Kind: wallpaper.KindEffect, Effect: id, Theme: artPalette(h),
		})
	}
}
