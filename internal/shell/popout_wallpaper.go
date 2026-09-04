package shell

import (
	"context"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/Nomadcxx/sysc-shell/internal/config"
	"github.com/Nomadcxx/sysc-shell/internal/icons"
	"github.com/Nomadcxx/sysc-shell/internal/platform/niri"
	"github.com/Nomadcxx/sysc-shell/internal/platform/wayland"

	"github.com/Nomadcxx/sysc-shell/internal/ui"
	"github.com/Nomadcxx/sysc-shell/internal/wallpaper"
)

// Wallpaper picker chrome. The plugin's layout with a virtualized grid in place
// of its page buttons: pagination was a plugin-API ceiling, not a preference
// (D4/D8). Four 210px columns plus three 10px gaps and the padding come to 902,
// inside the 980 panel.
const (
	wallpaperColumns    = 4
	wallpaperTileWidth  = wallpaper.ThumbWidth
	wallpaperThumbH     = wallpaper.ThumbHeight
	wallpaperGridGap    = 10
	wallpaperPadding    = 16
	wallpaperFieldH     = 44
	wallpaperCaptionH   = 22
	wallpaperTileHeight = wallpaperThumbH + wallpaperCaptionH
	wallpaperRowHeight  = wallpaperTileHeight + wallpaperGridGap

	// Chrome controls. The output select is D4's 170px minimum.
	wallpaperOutputWidth     = 170
	wallpaperFilterWidth     = 220
	wallpaperControlPad      = 8
	wallpaperIconSize        = 18
	wallpaperPlaceholderIcon = 32
	wallpaperSelectedStroke  = 2

	// The folder band: compact rows, capped so folders never crowd out the
	// wallpapers they sit above.
	wallpaperDirColumns   = 4
	wallpaperDirMaxRows   = 2
	wallpaperDirRowHeight = 46
	wallpaperDirChipWidth = 210

	// wallpaperEnginePillH is the engine strip: shorter than a control, since
	// the pills are read, not pressed.
	wallpaperEnginePillH = 26

	// wallpaperCoverageTimeout bounds the compositor probe.
	wallpaperCoverageTimeout = 2 * time.Second
)

// wallpaperServiceLocked returns the running service, or nil before the
// registry has started one. Registry.mu is held.
func (r *Registry) wallpaperServiceLocked() *wallpaper.Service {
	if r.wallpaperSvc == nil && !runningAsTest() {
		return r.wallpaperStartLocked()
	}
	return r.wallpaperSvc
}

// relayWallpaper mirrors relayLauncher: snapshots arrive off the Wayland owner
// and the panel is rebuilt under Registry.mu.
func (r *Registry) relayWallpaper(svc *wallpaper.Service) {
	ch := svc.Updates()
	for {
		select {
		case <-r.closed:
			return
		case snap := <-ch:
			r.mu.Lock()
			h := r.panelHosts[PanelWallpaper]
			if h != nil {
				h.wallpaperSnap = snap
				if h.wallpaperDir == "" {
					h.wallpaperDir = firstRoot(snap)
				}
				r.rebuildPanel(h)
			}
			r.mu.Unlock()
			if h != nil {
				r.publishSurface(h.output, panelSurfaceID(PanelWallpaper))
			}
		}
	}
}

// firstRoot is the directory the picker opens on.
func firstRoot(snap wallpaper.Snapshot) string {
	if snap.Library == nil {
		return ""
	}
	if roots := snap.Library.Roots(); len(roots) > 0 {
		return roots[0]
	}
	return ""
}

// wallpaperSearch is the current search box text.
func wallpaperSearch(h *PanelHost) string {
	if h.search == nil {
		return ""
	}
	return h.search.Text
}

// wallpaperView is the current directory through the filter and the search box.
func wallpaperView(h *PanelHost) []wallpaper.Entry {
	if h.wallpaperSnap.Library == nil {
		return nil
	}
	return h.wallpaperSnap.Library.View(h.wallpaperDir, h.wallpaperFilter, wallpaperSearch(h))
}

// wallpaperMedia is what the tile grid shows: playable files only.
func wallpaperMedia(h *PanelHost) []wallpaper.Entry {
	view := wallpaperView(h)
	out := make([]wallpaper.Entry, 0, len(view))
	for _, e := range view {
		if !e.IsDir {
			out = append(out, e)
		}
	}
	return out
}

// wallpaperDirs is the current directory's children.
//
// They get their own short list rather than sharing the tile grid: a real
// library has dozens of folders, and at tile height they push every wallpaper
// off the first screen. They cannot be chips in a single row either -- that
// overflows and fails layout outright -- so they are a compact virtualized
// band of their own, capped to a couple of rows.
func wallpaperDirs(h *PanelHost) []wallpaper.Entry {
	view := wallpaperView(h)
	out := make([]wallpaper.Entry, 0, len(view))
	for _, e := range view {
		if e.IsDir {
			out = append(out, e)
		}
	}
	return out
}

// wallpaperDirBand is the folder navigator above the grid.
func wallpaperDirBand(h *PanelHost) *ui.Node {
	dirs := wallpaperDirs(h)
	if len(dirs) == 0 {
		return nil
	}
	rows := (len(dirs) + wallpaperDirColumns - 1) / wallpaperDirColumns
	visible := min(rows, wallpaperDirMaxRows)
	return &ui.Node{
		Kind:       ui.KindVirtualList,
		ItemCount:  rows,
		ItemHeight: wallpaperDirRowHeight,
		Height:     visible * wallpaperDirRowHeight,
		Item: func(row int) *ui.Node {
			start := row * wallpaperDirColumns
			chips := make([]*ui.Node, 0, wallpaperDirColumns)
			for i := start; i < start+wallpaperDirColumns && i < len(dirs); i++ {
				chip := wallpaperButton(h, "wallpaper-dir:"+dirs[i].Path, dirs[i].Name, false)
				chip.Width = wallpaperDirChipWidth
				chips = append(chips, chip)
			}
			return &ui.Node{Kind: ui.KindRow, Gap: 6, Children: chips}
		},
	}
}

// wallpaperTree projects the last snapshot as the D4 chrome: title and close,
// search with an output select, the kind filter with Up, the folder strip,
// banners, the active strip, the virtualized grid, and the media count.
func wallpaperTree(r *Registry, h *PanelHost) *ui.Node {
	if h.search == nil {
		h.search = ui.NewField("")
	}
	inner := max(h.place.Panel.W-2*wallpaperPadding, 0)

	children := []*ui.Node{
		wallpaperTitleRow(h),
		wallpaperSearchRow(h, inner),
		wallpaperFilterRow(h),
	}
	if strip := wallpaperFolderStrip(h); strip != nil {
		children = append(children, strip)
	}
	children = append(children, wallpaperEngineRow(h))
	children = append(children, wallpaperBanners(h)...)
	children = append(children, wallpaperActiveStrip(h))
	if band := wallpaperDirBand(h); band != nil {
		children = append(children, band)
	}

	media := wallpaperMedia(h)
	rows := (len(media) + wallpaperColumns - 1) / wallpaperColumns
	used := 0
	for _, child := range children {
		used += childHeightFor(child)
	}
	list := &ui.Node{
		Kind:       ui.KindVirtualList,
		ItemCount:  rows,
		ItemHeight: wallpaperRowHeight,
		Height:     max(h.place.Panel.H-2*wallpaperPadding-used-wallpaperCaptionH-wallpaperGridGap, 0),
		Item: func(row int) *ui.Node {
			return wallpaperRow(r, h, media, row)
		},
	}
	if len(media) == 0 {
		children = append(children, wallpaperEmptyState(h))
	} else {
		children = append(children, list)
	}
	children = append(children, wallpaperCount(media))

	return &ui.Node{
		Kind:     ui.KindColumn,
		Padding:  wallpaperPadding,
		Gap:      wallpaperGridGap,
		Children: children,
	}
}

// childHeightFor is the vertical budget one chrome row takes before the grid.
func childHeightFor(n *ui.Node) int {
	if n == nil {
		return 0
	}
	if n.Height > 0 {
		return n.Height + wallpaperGridGap
	}
	return wallpaperCaptionH + wallpaperGridGap
}

// wallpaperEmptyState explains an empty grid, which otherwise reads as a
// broken picker. A search that matches nothing is a different situation from a
// directory that holds nothing.
func wallpaperEmptyState(h *PanelHost) *ui.Node {
	text := "No supported wallpapers in this directory"
	if wallpaperSearch(h) != "" {
		text = "No wallpapers match your search"
	}
	if h.wallpaperSnap.Library == nil {
		text = "Indexing wallpaper library\u2026"
	}
	return &ui.Node{Kind: ui.KindText, Text: text, Height: wallpaperCaptionH}
}

// wallpaperTitleRow is the panel's name and its close control.
func wallpaperTitleRow(h *PanelHost) *ui.Node {
	return &ui.Node{
		Kind:   ui.KindRow,
		Gap:    wallpaperGridGap,
		Height: h.theme.ControlHeight,
		Children: []*ui.Node{
			{Kind: ui.KindText, Text: "Wallpaper", Bold: true},
			{
				Kind: ui.KindButton, Action: "wallpaper-close", Name: "Close",
				Role: "button", Focusable: true, Padding: wallpaperControlPad,
				Height:   h.theme.ControlHeight,
				Children: []*ui.Node{{Kind: ui.KindIcon, Icon: "close", IconSize: wallpaperIconSize}},
			},
		},
	}
}

// wallpaperSearchRow is the search field beside the output select. The select
// is the fan-out control: All is two applies, not gSlapper's wildcard (D14).
func wallpaperSearchRow(h *PanelHost, inner int) *ui.Node {
	field := h.search.Node("Search")
	field.Height = wallpaperFieldH
	field.Padding = 8
	field.Width = max(inner-wallpaperOutputWidth-wallpaperGridGap, 0)

	tokens := append([]string{wallpaper.AllOutputs}, h.wallpaperSnap.Connectors...)
	segments := make([]*ui.Node, 0, len(tokens))
	for _, token := range tokens {
		segments = append(segments, wallpaperSegment(h, "wallpaper-output:"+token,
			wallpaperOutputLabel(token), token == h.wallpaperOutput))
	}
	return &ui.Node{
		Kind: ui.KindRow, Gap: wallpaperGridGap, Height: wallpaperFieldH,
		Children: []*ui.Node{field, {
			Kind: ui.KindSegmented, Key: "wallpaper-output", Gap: 2,
			Width: wallpaperOutputWidth, Height: h.theme.ControlHeight, Children: segments,
		}},
	}
}

func wallpaperOutputLabel(token string) string {
	if token == wallpaper.AllOutputs {
		return "All"
	}
	return token
}

// wallpaperFilterRow is the kind filter, plus Up once the picker has descended
// out of a library root.
func wallpaperFilterRow(h *PanelHost) *ui.Node {
	filters := []struct {
		label string
		value wallpaper.Filter
	}{
		{"All", wallpaper.FilterAll},
		{"Images", wallpaper.FilterImages},
		{"Videos", wallpaper.FilterVideos},
	}
	segments := make([]*ui.Node, 0, len(filters))
	for _, f := range filters {
		segments = append(segments, wallpaperSegment(h,
			fmt.Sprintf("wallpaper-filter:%d", f.value), f.label, f.value == h.wallpaperFilter))
	}
	row := []*ui.Node{{
		Kind: ui.KindSegmented, Key: "wallpaper-filter", Gap: 2,
		Width: wallpaperFilterWidth, Height: h.theme.ControlHeight, Children: segments,
	}}

	if h.wallpaperSnap.Library != nil {
		if _, ok := h.wallpaperSnap.Library.Parent(h.wallpaperDir); ok {
			row = append(row, wallpaperButton(h, "wallpaper-up", "Up", false))
		}
	}
	return &ui.Node{Kind: ui.KindRow, Gap: wallpaperGridGap, Height: h.theme.ControlHeight, Children: row}
}

// wallpaperFolderStrip is D4's selector: every configured root, then the
// current directory's children. Without it a second library root is
// wallpaperFolderStrip is D4's selector, carrying the configured library roots.
// Without it a second root is unreachable, which is how the video directory
// went missing. It holds only the roots: that set is bounded by configuration,
// so the row can never overflow the way a strip of subdirectories does.
func wallpaperFolderStrip(h *PanelHost) *ui.Node {
	if h.wallpaperSnap.Library == nil {
		return nil
	}
	roots := h.wallpaperSnap.Library.Roots()
	if len(roots) < 2 {
		// One root is not a choice worth a control.
		return nil
	}
	chips := make([]*ui.Node, 0, len(roots))
	for _, root := range roots {
		chips = append(chips, wallpaperButton(h, "wallpaper-dir:"+root,
			filepath.Base(root), root == h.wallpaperDir))
	}
	return &ui.Node{Kind: ui.KindRow, Gap: 6, Height: h.theme.ControlHeight, Children: chips}
}

// wallpaperSegment is one exclusive choice inside a segmented control.
func wallpaperSegment(h *PanelHost, action, label string, selected bool) *ui.Node {
	n := &ui.Node{
		Kind: ui.KindButton, Action: action, Name: label,
		Role: "tab", Focusable: true, Padding: wallpaperControlPad,
		Height:   h.theme.ControlHeight,
		Children: []*ui.Node{{Kind: ui.KindText, Text: label}},
	}
	if selected {
		n.State |= ui.StateSelected
	}
	return n
}

// wallpaperButton is a standalone chip in the chrome.
func wallpaperButton(h *PanelHost, action, label string, selected bool) *ui.Node {
	n := &ui.Node{
		Kind: ui.KindButton, Action: action, Name: label,
		Role: "button", Focusable: true, Padding: wallpaperControlPad,
		Height:   h.theme.ControlHeight,
		Children: []*ui.Node{{Kind: ui.KindText, Text: label}},
	}
	if selected {
		n.State |= ui.StateSelected
	}
	return n
}

// wallpaperRow builds one row of up to four tiles. It runs inside layout, on
// the Wayland owner, so it only ever reads already-decoded rasters.
func wallpaperRow(r *Registry, h *PanelHost, media []wallpaper.Entry, row int) *ui.Node {
	start := row * wallpaperColumns
	tiles := make([]*ui.Node, 0, wallpaperColumns)
	for i := start; i < start+wallpaperColumns && i < len(media); i++ {
		tiles = append(tiles, wallpaperTile(r, h, media[i], i))
	}
	return &ui.Node{Kind: ui.KindRow, Gap: wallpaperGridGap, Children: tiles}
}

// wallpaperTile is a capsule around a thumbnail and a caption. The capsule
// supplies the tile chrome and takes its radius from the theme's CardRadius,
// so the tile follows the user's configured radius.
func wallpaperTile(r *Registry, h *PanelHost, entry wallpaper.Entry, index int) *ui.Node {
	raster := wallpaperThumbFor(r, entry)
	thumb := &ui.Node{
		Kind:   ui.KindImage,
		ImageW: wallpaperTileWidth,
		ImageH: wallpaperThumbH,
		Image:  raster,
	}
	// A thumbnail that has not decoded yet, or cannot be decoded at all, keeps
	// its box and shows the kind glyph rather than a hole in the grid (D6).
	var body []*ui.Node
	if raster == nil {
		placeholder := &ui.Node{
			Kind: ui.KindRow, Width: wallpaperTileWidth, Height: wallpaperThumbH,
		}
		if glyph := wallpaperPlaceholderGlyph(entry); glyph != "" {
			placeholder.Children = []*ui.Node{{
				Kind: ui.KindIcon, Icon: glyph, IconSize: wallpaperPlaceholderIcon,
			}}
		}
		body = append(body, placeholder)
	} else {
		body = append(body, thumb)
	}
	body = append(body, &ui.Node{
		Kind:     ui.KindText,
		Text:     wallpaperCaption(h, entry),
		MaxWidth: wallpaperTileWidth,
	})

	tile := &ui.Node{
		Kind:      ui.KindCapsule,
		Fill:      ui.FillContainerHigh,
		Width:     wallpaperTileWidth,
		Padding:   4,
		Action:    "wallpaper-tile",
		Name:      entry.Path,
		Focusable: true,
		Children: []*ui.Node{{
			Kind: ui.KindColumn, Gap: 4, Children: body,
		}},
	}
	// The output's current wallpaper is outlined, so the picker says what is
	// already applied rather than only what could be (D6).
	if matched, _ := wallpaperMatchCount(h, entry.Path); matched > 0 {
		tile.Stroke = wallpaperSelectedStroke
		tile.StrokeFill = ui.FillAccent
	}
	// The keyboard selection is a muted wash. StateSelected on a capsule paints
	// a solid accent slab, which reads as "applied" rather than "focused" and
	// drowns the thumbnail it is supposed to be highlighting.
	if index == h.wallpaperSel {
		tile.Fill = ui.FillSoft
	}
	// Without gSlapper a video cannot play, so its tile says so instead of
	// accepting a click that would do nothing (D6).
	if !wallpaperCanApply(h, entry) {
		tile.State |= ui.StateDisabled
	}
	return tile
}

// wallpaperCanApply reports whether a tile is activatable.
func wallpaperCanApply(h *PanelHost, entry wallpaper.Entry) bool {
	return entry.Kind != wallpaper.KindVideo || h.wallpaperSnap.Caps.GSlapper
}

// wallpaperPlaceholderGlyph is the tile's stand-in before a preview exists.
//
// Only names in the embedded Material subset may be used: an unknown name
// fails the whole surface at render time rather than drawing nothing. The
// subset has no folder or media glyphs, so a directory borrows the navigation
// chevron and a pending preview shows an empty box, which the "Generating
// previews" banner already accounts for.
func wallpaperPlaceholderGlyph(entry wallpaper.Entry) string {
	if entry.IsDir {
		return "chevron_right"
	}
	return ""
}

// wallpaperCaption is the filename, prefixed for a video and marked when the
// tile is what the selected outputs already show.
func wallpaperCaption(h *PanelHost, entry wallpaper.Entry) string {
	if entry.IsDir {
		return entry.Name
	}
	name := entry.Name
	if entry.Kind == wallpaper.KindVideo {
		name = "VIDEO \u00b7 " + name
	}
	if matched, total := wallpaperMatchCount(h, entry.Path); total > 1 && matched > 0 {
		return fmt.Sprintf("%s  %d / %d", name, matched, total)
	}
	return name
}

// wallpaperTargets resolves the output select to the connectors it acts on.
func wallpaperTargets(h *PanelHost) []string {
	if h.wallpaperOutput == wallpaper.AllOutputs {
		return h.wallpaperSnap.Connectors
	}
	return []string{h.wallpaperOutput}
}

// wallpaperMatchCount reports how many of the selected outputs already show
// path. It is read back from the snapshot rather than composed here, so the
// badge cannot drift from what is actually assigned.
func wallpaperMatchCount(h *PanelHost, path string) (matched, total int) {
	for _, connector := range wallpaperTargets(h) {
		total++
		if h.wallpaperSnap.Assignments[connector].Path == path {
			matched++
		}
	}
	return matched, total
}

// wallpaperBanners surfaces the capability, scan, and apply failures. They are
// separate rows because they have separate causes: a missing engine is not a
// bad directory is not a refused apply (D4).
func wallpaperBanners(h *PanelHost) []*ui.Node {
	var out []*ui.Node
	add := func(text string, tone ui.Tone) {
		if text == "" {
			return
		}
		out = append(out, &ui.Node{Kind: ui.KindText, Text: text, Tone: tone, Height: wallpaperCaptionH})
	}
	if done, total := h.wallpaperSnap.ThumbsDone, h.wallpaperSnap.ThumbsTotal; total > 0 && done < total {
		// Previews are generated slowly on purpose. Saying so is the
		// difference between a library that is still filling in and one that
		// looks broken.
		add(fmt.Sprintf("Generating previews \u00b7 %d / %d", done, total), ui.ToneNormal)
	}
	for _, connector := range wallpaperTargets(h) {
		if h.wallpaperSnap.Runtime[connector].State == wallpaper.StateStarting {
			add("Applying wallpaper\u2026", ui.ToneNormal)
			break
		}
	}
	if h.wallpaperSnap.Library != nil {
		add(h.wallpaperSnap.Library.Err, ui.ToneError)
	}
	add(h.wallpaperSnap.Err, ui.ToneError)
	for _, connector := range wallpaperTargets(h) {
		if owner := h.wallpaperSnap.Covered[connector]; owner != "" {
			add(fmt.Sprintf("%s is already painted by %s - a wallpaper set here will not be visible until that surface goes away",
				connector, owner), ui.ToneError)
		}
	}
	for _, connector := range h.wallpaperSnap.Connectors {
		if rt := h.wallpaperSnap.Runtime[connector]; rt.Err != "" {
			add(connector+": "+rt.Err, ui.ToneError)
		}
	}
	return out
}

// wallpaperEngineRow names the wallpaper engines this machine has, one pill
// each, in the order they are reached: gSlapper drives video and image, the
// static fallbacks are where Restore hands off. A pill is absent when its
// binary is not installed, which reads faster than a sentence saying so.
func wallpaperEngineRow(h *PanelHost) *ui.Node {
	var pills []*ui.Node
	if h.wallpaperSnap.Caps.GSlapper {
		pills = append(pills, wallpaperEnginePill(h, "gSlapper"))
	}
	for _, name := range h.wallpaperSnap.Caps.Statics {
		pills = append(pills, wallpaperEnginePill(h, name))
	}
	if len(pills) == 0 {
		// The one case prose earns its space: nothing can paint anything.
		return &ui.Node{
			Kind: ui.KindText, Height: wallpaperCaptionH, Tone: ui.ToneError,
			Text: "no wallpaper engine installed",
		}
	}
	return &ui.Node{
		Kind: ui.KindRow, Gap: 6, Height: wallpaperEnginePillH, Children: pills,
	}
}

func wallpaperEnginePill(h *PanelHost, label string) *ui.Node {
	return &ui.Node{
		Kind: ui.KindCapsule, Fill: ui.FillSoft, Padding: wallpaperControlPad,
		Height:   wallpaperEnginePillH,
		Radius:   h.theme.Radius,
		Children: []*ui.Node{{Kind: ui.KindText, Text: label}},
	}
}

// wallpaperActiveStrip is what the selected output is showing, with the
// controls that act on it. Pause is video-only because an image has no
// pipeline to hold (D7).
func wallpaperActiveStrip(h *PanelHost) *ui.Node {
	children := []*ui.Node{
		{Kind: ui.KindText, Text: wallpaperSummary(h.wallpaperSnap, h.wallpaperOutput)},
	}
	if paused, ok := wallpaperPlaybackState(h); ok {
		action, label := "wallpaper-pause", "Pause"
		if paused {
			action, label = "wallpaper-resume", "Resume"
		}
		children = append(children, wallpaperButton(h, action, label, false))
	}
	children = append(children, wallpaperButton(h, "wallpaper-restore", wallpaperRestoreLabel(h), false))
	return &ui.Node{
		Kind: ui.KindRow, Gap: wallpaperGridGap,
		Height: h.theme.ControlHeight, Children: children,
	}
}

func wallpaperRestoreLabel(h *PanelHost) string {
	if h.wallpaperOutput == wallpaper.AllOutputs {
		return "Restore all"
	}
	return "Restore"
}

// wallpaperPlaybackState reports whether the selected outputs hold a video and
// whether it is paused. All outputs offers the control when any of them does.
func wallpaperPlaybackState(h *PanelHost) (paused, ok bool) {
	for _, connector := range wallpaperTargets(h) {
		if h.wallpaperSnap.Assignments[connector].Kind != wallpaper.KindVideo {
			continue
		}
		ok = true
		if h.wallpaperSnap.Runtime[connector].State == wallpaper.StatePaused {
			paused = true
		}
	}
	return paused, ok
}

// wallpaperSummary is the active strip's line for one output, or the mixed
// summary for All.
func wallpaperSummary(snap wallpaper.Snapshot, output string) string {
	if output != wallpaper.AllOutputs {
		a, ok := snap.Assignments[output]
		if !ok {
			return output + " \u00b7 nothing assigned"
		}
		return fmt.Sprintf("%s \u00b7 %s \u00b7 %s", output, filepath.Base(a.Path),
			wallpaperStateName(snap.Runtime[output].State))
	}
	outputs, videos, images := 0, 0, 0
	for _, connector := range snap.Connectors {
		a, ok := snap.Assignments[connector]
		if !ok {
			continue
		}
		outputs++
		if a.Kind == wallpaper.KindVideo {
			videos++
		} else {
			images++
		}
	}
	if outputs == 0 {
		return "nothing assigned"
	}
	return fmt.Sprintf("%d outputs \u00b7 %d video \u00b7 %d image", outputs, videos, images)
}

func wallpaperStateName(s wallpaper.State) string {
	switch s {
	case wallpaper.StateStarting:
		return "starting"
	case wallpaper.StatePlaying:
		return "playing"
	case wallpaper.StatePaused:
		return "paused"
	case wallpaper.StateError:
		return "error"
	}
	return "static"
}

// wallpaperCount is the media count under the grid.
func wallpaperCount(media []wallpaper.Entry) *ui.Node {
	files := 0
	for _, e := range media {
		if !e.IsDir {
			files++
		}
	}
	return &ui.Node{
		Kind:   ui.KindText,
		Text:   fmt.Sprintf("%d items", files),
		Height: wallpaperCaptionH,
	}
}

// wallpaperKeyPress moves the grid selection and applies. It returns false for
// keys it does not own, so typing still reaches the search field.
func (h *PanelHost) wallpaperKeyPress(r *Registry, key uint32) bool {
	entries := wallpaperMedia(h)
	switch key {
	case keyLeft:
		h.wallpaperMoveSel(r, -1, len(entries))
	case keyRight:
		h.wallpaperMoveSel(r, 1, len(entries))
	case keyUp:
		h.wallpaperMoveSel(r, -wallpaperColumns, len(entries))
	case keyDown:
		h.wallpaperMoveSel(r, wallpaperColumns, len(entries))
	case keyEnter:
		// A focused chrome control owns Enter; only the grid falls through to
		// applying the selected tile.
		if n := h.focused(); n != nil && n.Kind == ui.KindButton && n.Action != "" {
			return false
		}
		h.wallpaperActivate(r)
	default:
		return false
	}
	return true
}

// wallpaperMoveSel walks the four-column grid, clamped at each end rather than
// wrapping: wrapping from the last tile to the first reads as a jump.
func (h *PanelHost) wallpaperMoveSel(r *Registry, delta, count int) {
	if count == 0 {
		h.wallpaperSel = 0
		return
	}
	h.wallpaperSel = min(max(h.wallpaperSel+delta, 0), count-1)
	r.rebuildPanel(h)
}

// wallpaperActivate applies the selected tile, or descends into it when it is
// a directory.
func (h *PanelHost) wallpaperActivate(r *Registry) {
	entries := wallpaperMedia(h)
	if h.wallpaperSel < 0 || h.wallpaperSel >= len(entries) {
		return
	}
	h.wallpaperApply(r, entries[h.wallpaperSel])
}

// wallpaperApply enqueues one assignment, or navigates into a directory. A
// video tile with no gSlapper is inert rather than an error the user has to
// dismiss: the banner already says why (D6).
func (h *PanelHost) wallpaperApply(r *Registry, entry wallpaper.Entry) {
	if entry.IsDir {
		h.wallpaperDir = entry.Path
		h.wallpaperSel = 0
		r.rebuildPanel(h)
		return
	}
	if entry.Kind == wallpaper.KindVideo && !h.wallpaperSnap.Caps.GSlapper {
		return
	}
	svc := r.wallpaperServiceLocked()
	if svc == nil {
		return
	}
	svc.Enqueue(wallpaper.Command{
		Op:    wallpaper.OpApply,
		Token: h.wallpaperOutput,
		Path:  entry.Path,
		Kind:  entry.Kind,
	})
}

// wallpaperRestore hands the selected output back to the static fallback.
func (h *PanelHost) wallpaperRestore(r *Registry) {
	if svc := r.wallpaperServiceLocked(); svc != nil {
		svc.Enqueue(wallpaper.Command{Op: wallpaper.OpRestore, Token: h.wallpaperOutput})
	}
}

// wallpaperSetPaused holds or releases playback on the selected output.
func (h *PanelHost) wallpaperSetPaused(r *Registry, paused bool) {
	svc := r.wallpaperServiceLocked()
	if svc == nil {
		return
	}
	op := wallpaper.OpResume
	if paused {
		op = wallpaper.OpPause
	}
	svc.Enqueue(wallpaper.Command{Op: op, Token: h.wallpaperOutput})
}

// wallpaperUp leaves the current directory, stopping at a library root.
func (h *PanelHost) wallpaperUp(r *Registry) {
	if h.wallpaperSnap.Library == nil {
		return
	}
	if parent, ok := h.wallpaperSnap.Library.Parent(h.wallpaperDir); ok {
		h.wallpaperDir = parent
		h.wallpaperSel = 0
		r.rebuildPanel(h)
	}
}

// wallpaperPointerPress applies the tile under the pointer.
func (h *PanelHost) wallpaperPointerPress(r *Registry, e wayland.Event) bool {
	path := wallpaperTileAt(h.root, int(math.Floor(e.X)), int(math.Floor(e.Y)))
	if path == "" {
		return false
	}
	entries := wallpaperMedia(h)
	for i, entry := range entries {
		if entry.Path == path {
			h.wallpaperSel = i
			h.wallpaperApply(r, entry)
			return true
		}
	}
	return false
}

// wallpaperTileAt finds the tile under a point.
func wallpaperTileAt(n *ui.Node, x, y int) string {
	if n == nil {
		return ""
	}
	if n.Action == "wallpaper-tile" && n.Bounds.Contains(x, y) {
		return n.Name
	}
	for _, c := range n.Children {
		if path := wallpaperTileAt(c, x, y); path != "" {
			return path
		}
	}
	return ""
}

// wallpaperThumbsLocked returns the picker's own decode worker, starting it on
// first use. Registry.mu is held.
//
// It is deliberately not the shared tray worker. A wallpaper directory of a few
// hundred files would evict every tray and notification icon from that cache
// and fill its 32-deep queue, so the two get separate instances of the same
// machinery rather than one contended cache.
func (r *Registry) wallpaperThumbsLocked() *icons.Worker {
	if r.wallpaperThumbs == nil {
		ctx, cancel := context.WithCancel(context.Background())
		r.wallpaperThumbCancel = cancel
		r.wallpaperThumbs = icons.NewWorker(icons.NewResolver("", nil), r.applyWallpaperThumb)
		go func() { _ = r.wallpaperThumbs.Run(ctx) }()
	}
	return r.wallpaperThumbs
}

// applyWallpaperThumb repaints the picker when a thumbnail finishes decoding.
// It runs on the worker's goroutine, never inside layout.
func (r *Registry) applyWallpaperThumb(_ icons.Key, image *ui.Image) {
	if image == nil {
		return
	}
	r.mu.Lock()
	h := r.panelHosts[PanelWallpaper]
	if h == nil {
		r.mu.Unlock()
		return
	}
	r.rebuildPanel(h)
	out := h.output
	r.mu.Unlock()
	r.publishSurface(out, panelSurfaceID(PanelWallpaper))
}

// wallpaperThumbFor returns an already-decoded thumbnail, queueing a decode
// when there is not one yet.
//
// This runs inside the virtual list's Item builder, which layout calls on the
// Wayland owner, so it must never decode here: it looks the raster up and asks
// for it, and the next snapshot picks up the result.
func wallpaperThumbFor(r *Registry, entry wallpaper.Entry) *ui.Image {
	if r == nil || entry.IsDir {
		return nil
	}
	// Always the generated preview, never the original. A wallpaper library is
	// tens of gigabytes of pixels; decoding a 4K still on the tile path would
	// stall the picker and blow the decoder's own file bound. The generator
	// fills the cache slowly in the background, and until it reaches this file
	// the tile keeps its kind glyph.
	source := wallpaper.CachedStillPath(entry.Path)
	if source == "" {
		return nil
	}
	if _, err := os.Stat(source); err != nil {
		return nil
	}
	key := icons.Key{Name: source, W: wallpaperTileWidth, H: wallpaperThumbH}
	worker := r.wallpaperThumbsLocked()
	if image, ok := worker.Lookup(key); ok {
		return image
	}
	_, _, _ = worker.Request(key)
	return nil
}

// wallpaperStartLocked starts the wallpaper service if it is not running.
// Registry.mu is held.
//
// The service starts with the registry rather than with the panel: an output's
// wallpaper has to come back at login whether or not anyone opens the picker
// (D20).
func (r *Registry) wallpaperStartLocked() *wallpaper.Service {
	if r.wallpaperSvc != nil {
		return r.wallpaperSvc
	}
	cfg := r.cfg.Wallpaper
	r.wallpaperSvc = wallpaper.NewService(wallpaper.ServiceConfig{
		Engine:      wallpaper.NewEngine(wallpaperRuntimeDir(), nil),
		Settings:    wallpaperSettings(cfg),
		Connectors:  r.connectorsLocked(),
		Roots:       []string{cfg.ImageDirectory, cfg.VideoDirectory},
		PersistPath: wallpaper.AssignmentsPath(),
		Coverage:    wallpaperCoverageProbe,
		CacheDir:    wallpaper.CacheDir(),
	})
	r.wallpaperSvc.SetConfigHook(r.setWallpaperSeed)
	go r.relayWallpaper(r.wallpaperSvc)
	return r.wallpaperSvc
}

// wallpaperSettings projects the config block onto the engine's settings.
func wallpaperSettings(cfg config.Wallpaper) wallpaper.Settings {
	return wallpaper.Settings{
		Scale:        cfg.Scale,
		Loop:         cfg.Loop,
		FPS:          cfg.FPS,
		Fade:         cfg.Fade,
		FadeDuration: cfg.FadeDuration,
		Hidden:       cfg.Hidden,
	}
}

// wallpaperRuntimeDir is where the owned gSlapper sockets live.
func wallpaperRuntimeDir() string {
	dir := os.Getenv("XDG_RUNTIME_DIR")
	if dir == "" {
		dir = os.TempDir()
	}
	return filepath.Join(dir, "sysc-shell")
}

// connectorsLocked lists the connectors that currently have a bar.
func (r *Registry) connectorsLocked() []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(r.bars))
	for _, bar := range r.bars {
		name := bar.connector()
		if name == "" || seen[name] {
			continue
		}
		seen[name] = true
		out = append(out, name)
	}
	slices.Sort(out)
	return out
}

// setWallpaperSeed points the theme at the applied image and regenerates the
// palette.
//
// The config file is deliberately not rewritten: the seed follows the
// wallpaper, and startup reconcile replays the assignment and calls this again,
// so persisting it would only duplicate state the assignment file already owns.
func (r *Registry) setWallpaperSeed(source, seed string) {
	if seed == "" {
		return
	}
	r.mu.Lock()
	if r.cfg.ThemeGen.Source == source && r.cfg.ThemeGen.Seed == seed {
		r.mu.Unlock()
		return
	}
	r.cfg.ThemeGen.Source = source
	r.cfg.ThemeGen.Seed = seed
	cfg := r.cfg
	r.mu.Unlock()

	// generateTheme runs the generator and writes the enabled templates, so it
	// is called outside the lock; it returns the previous palette unchanged if
	// the new one is incomplete, which is what keeps a bad seed from blanking
	// the shell.
	tokens := r.generateTheme(cfg)

	r.mu.Lock()
	r.tokens = tokens
	for _, bar := range r.bars {
		bar.apply(r.viewLocked(bar.connector()))
	}
	r.retheThemeOpenSurfacesLocked()
	outputs := r.outputGlobalsLocked()
	r.mu.Unlock()

	for _, global := range outputs {
		r.publishSurface(global, "")
	}
}

// wallpaperOutputConnected replays an output's saved wallpaper when it appears.
func (r *Registry) wallpaperOutputConnected(connector string) {
	r.mu.Lock()
	svc := r.wallpaperSvc
	r.mu.Unlock()
	if svc != nil && connector != "" {
		svc.Enqueue(wallpaper.Command{Op: wallpaper.OpConnect, Token: connector})
	}
}

// wallpaperOutputGone drops an output's runtime while keeping its assignment,
// so a monitor that comes back gets its wallpaper back (D20).
func (r *Registry) wallpaperOutputGone(connector string) {
	r.mu.Lock()
	svc := r.wallpaperSvc
	r.mu.Unlock()
	if svc != nil && connector != "" {
		svc.Enqueue(wallpaper.Command{Op: wallpaper.OpDisconnect, Token: connector})
	}
}

// connectorsSnapshot lists the live connectors without holding Registry.mu.
func (r *Registry) connectorsSnapshot() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.connectorsLocked()
}

// wallpaperAction handles a click or Enter on one of the picker's controls.
// It returns false for anything that is not ours, so the generic dispatch
// keeps working for every other panel.
func (h *PanelHost) wallpaperAction(r *Registry, n *ui.Node) bool {
	if h.id != PanelWallpaper || n == nil {
		return false
	}
	switch {
	case n.Action == "wallpaper-close":
		r.closePanelLocked(PanelWallpaper)
		return true
	case n.Action == "wallpaper-up":
		h.wallpaperUp(r)
		return true
	case n.Action == "wallpaper-restore":
		h.wallpaperRestore(r)
		return true
	case n.Action == "wallpaper-pause":
		h.wallpaperSetPaused(r, true)
		return true
	case n.Action == "wallpaper-resume":
		h.wallpaperSetPaused(r, false)
		return true
	case n.Action == "wallpaper-tile":
		for _, entry := range wallpaperMedia(h) {
			if entry.Path == n.Name {
				h.wallpaperApply(r, entry)
				return true
			}
		}
		return true
	}
	if token, ok := strings.CutPrefix(n.Action, "wallpaper-output:"); ok {
		h.wallpaperOutput = token
		r.rebuildPanel(h)
		return true
	}
	if value, ok := strings.CutPrefix(n.Action, "wallpaper-filter:"); ok {
		if f, err := strconv.Atoi(value); err == nil {
			h.wallpaperFilter = wallpaper.Filter(f)
			h.wallpaperSel = 0
			r.rebuildPanel(h)
		}
		return true
	}
	if dir, ok := strings.CutPrefix(n.Action, "wallpaper-dir:"); ok {
		h.wallpaperDir = dir
		h.wallpaperSel = 0
		r.rebuildPanel(h)
		return true
	}
	return false
}

// wallpaperCoverageProbe asks the compositor which outputs already carry a
// foreign Background surface. It runs on the service goroutine, never on the
// Wayland owner, and a compositor that cannot answer simply means no warning.
func wallpaperCoverageProbe() (map[string]string, error) {
	socket := os.Getenv("NIRI_SOCKET")
	if socket == "" {
		return nil, errors.New("shell: NIRI_SOCKET is unset")
	}
	ctx, cancel := context.WithTimeout(context.Background(), wallpaperCoverageTimeout)
	defer cancel()
	layers, err := niri.Layers(ctx, socket)
	if err != nil {
		return nil, err
	}
	return niri.BackgroundOwners(layers, wallpaperOurNamespace), nil
}

// wallpaperOurNamespace reports whether a layer namespace is one we put up.
// gSlapper announces itself as "slapper"; everything else on Background
// belongs to somebody else and is left alone (D17/D18).
func wallpaperOurNamespace(namespace string) bool {
	switch namespace {
	case "slapper", "awww-daemon", "swaybg":
		return true
	}
	return strings.HasPrefix(namespace, "sysc-shell")
}
