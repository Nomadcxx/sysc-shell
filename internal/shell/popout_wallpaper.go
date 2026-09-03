package shell

import (
	"context"
	"fmt"
	"math"

	"github.com/Nomadcxx/sysc-shell/internal/icons"
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
)

// wallpaperServiceLocked returns the running service, or nil before the
// registry has started one. Registry.mu is held.
func (r *Registry) wallpaperServiceLocked() *wallpaper.Service { return r.wallpaperSvc }

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

// wallpaperEntries is the current directory's view through the filter and the
// search box.
func wallpaperEntries(h *PanelHost) []wallpaper.Entry {
	if h.wallpaperSnap.Library == nil {
		return nil
	}
	search := ""
	if h.search != nil {
		search = h.search.Text
	}
	return h.wallpaperSnap.Library.View(h.wallpaperDir, h.wallpaperFilter, search)
}

// wallpaperTree projects the last snapshot. Row count is the tile count over
// four columns, so the list virtualizes rows rather than tiles.
func wallpaperTree(r *Registry, h *PanelHost) *ui.Node {
	if h.search == nil {
		h.search = ui.NewField("")
	}
	inner := max(h.place.Panel.W-2*wallpaperPadding, 0)

	field := h.search.Node("Search")
	field.Width = inner
	field.Height = wallpaperFieldH
	field.Padding = 8

	children := []*ui.Node{field}
	if banner := wallpaperBanner(h); banner != nil {
		children = append(children, banner)
	}
	children = append(children, wallpaperActiveStrip(h))

	entries := wallpaperEntries(h)
	rows := (len(entries) + wallpaperColumns - 1) / wallpaperColumns
	used := 0
	for _, child := range children {
		used += childHeightFor(child)
	}
	list := &ui.Node{
		Kind:       ui.KindVirtualList,
		ItemCount:  rows,
		ItemHeight: wallpaperRowHeight,
		Padding:    0,
		Height:     max(h.place.Panel.H-2*wallpaperPadding-used, 0),
		Item: func(row int) *ui.Node {
			return wallpaperRow(r, h, entries, row)
		},
	}
	children = append(children, list, wallpaperCount(entries))

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
	return wallpaperFieldH + wallpaperGridGap
}

// wallpaperRow builds one row of up to four tiles. It runs inside layout, on
// the Wayland owner, so it only ever reads already-decoded rasters.
func wallpaperRow(r *Registry, h *PanelHost, entries []wallpaper.Entry, row int) *ui.Node {
	start := row * wallpaperColumns
	tiles := make([]*ui.Node, 0, wallpaperColumns)
	for i := start; i < start+wallpaperColumns && i < len(entries); i++ {
		tiles = append(tiles, wallpaperTile(r, h, entries[i], i))
	}
	return &ui.Node{Kind: ui.KindRow, Gap: wallpaperGridGap, Children: tiles}
}

// wallpaperTile is a capsule around a thumbnail and a caption. The capsule
// supplies the tile chrome and takes its radius from the theme's CardRadius,
// so the tile follows the user's configured radius.
func wallpaperTile(r *Registry, h *PanelHost, entry wallpaper.Entry, index int) *ui.Node {
	thumb := &ui.Node{
		Kind:   ui.KindImage,
		ImageW: wallpaperTileWidth,
		ImageH: wallpaperThumbH,
		Image:  wallpaperThumbFor(r, entry),
	}
	caption := &ui.Node{
		Kind:     ui.KindText,
		Text:     wallpaperCaption(h, entry),
		MaxWidth: wallpaperTileWidth,
	}
	body := &ui.Node{
		Kind:     ui.KindColumn,
		Gap:      4,
		Children: []*ui.Node{thumb, caption},
	}
	tile := &ui.Node{
		Kind:     ui.KindCapsule,
		Fill:     ui.FillContainerHigh,
		Width:    wallpaperTileWidth,
		Padding:  4,
		Action:   "wallpaper-tile",
		Name:     entry.Path,
		Children: []*ui.Node{body},
	}
	if index == h.wallpaperSel {
		tile.State = ui.StateHovered
	}
	return tile
}

// wallpaperCaption is the filename, prefixed for a video and marked when the
// tile is what the selected output already shows.
func wallpaperCaption(h *PanelHost, entry wallpaper.Entry) string {
	if entry.IsDir {
		return entry.Name
	}
	name := entry.Name
	if entry.Kind == wallpaper.KindVideo {
		name = "VIDEO · " + name
	}
	if matched, total := wallpaperMatchCount(h, entry.Path); total > 1 && matched > 0 {
		return fmt.Sprintf("%s  %d / %d", name, matched, total)
	}
	return name
}

// wallpaperMatchCount reports how many of the selected outputs already show
// path. It is read back from the snapshot rather than composed here, so the
// badge cannot drift from what is actually assigned.
func wallpaperMatchCount(h *PanelHost, path string) (matched, total int) {
	targets := []string{h.wallpaperOutput}
	if h.wallpaperOutput == wallpaper.AllOutputs {
		targets = h.wallpaperSnap.Connectors
	}
	for _, connector := range targets {
		total++
		if h.wallpaperSnap.Assignments[connector].Path == path {
			matched++
		}
	}
	return matched, total
}

// wallpaperBanner surfaces a missing engine or a scan failure, or nil when
// there is nothing to say.
func wallpaperBanner(h *PanelHost) *ui.Node {
	text := ""
	switch {
	case !h.wallpaperSnap.Caps.GSlapper:
		text = "gslapper is not installed — video wallpapers are unavailable"
	case h.wallpaperSnap.Err != "":
		text = h.wallpaperSnap.Err
	case h.wallpaperSnap.Library != nil && h.wallpaperSnap.Library.Err != "":
		text = h.wallpaperSnap.Library.Err
	}
	if text == "" {
		return nil
	}
	return &ui.Node{Kind: ui.KindText, Text: text, Tone: ui.ToneNormal, Height: wallpaperCaptionH}
}

// wallpaperActiveStrip summarises what the selected output is showing. The
// mixed summary is counted off the snapshot, not composed from the click that
// produced it.
func wallpaperActiveStrip(h *PanelHost) *ui.Node {
	return &ui.Node{
		Kind:   ui.KindText,
		Text:   wallpaperSummary(h.wallpaperSnap, h.wallpaperOutput),
		Height: wallpaperCaptionH,
	}
}

// wallpaperSummary is the active strip's line for one output, or the mixed
// summary for All.
func wallpaperSummary(snap wallpaper.Snapshot, output string) string {
	if output != wallpaper.AllOutputs {
		a, ok := snap.Assignments[output]
		if !ok {
			return output + " · nothing assigned"
		}
		return fmt.Sprintf("%s · %s · %s", output, a.Path, wallpaperStateName(snap.Runtime[output].State))
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
	return fmt.Sprintf("%d outputs · %d video · %d image", outputs, videos, images)
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
func wallpaperCount(entries []wallpaper.Entry) *ui.Node {
	media := 0
	for _, e := range entries {
		if !e.IsDir {
			media++
		}
	}
	return &ui.Node{
		Kind:   ui.KindText,
		Text:   fmt.Sprintf("%d items", media),
		Tone:   ui.ToneNormal,
		Height: wallpaperCaptionH,
	}
}

// wallpaperKeyPress moves the grid selection and applies. It returns false for
// keys it does not own, so typing still reaches the search field.
func (h *PanelHost) wallpaperKeyPress(r *Registry, key uint32) bool {
	entries := wallpaperEntries(h)
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
	entries := wallpaperEntries(h)
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
	entries := wallpaperEntries(h)
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
	source := entry.Path
	if entry.Kind == wallpaper.KindVideo {
		// A video cannot be decoded by the image path; its still is extracted
		// into the cache off the owner, and until then the tile shows nothing.
		source = wallpaper.CachedStillPath(entry.Path)
		if source == "" {
			return nil
		}
	}
	key := icons.Key{Name: source, W: wallpaperTileWidth, H: wallpaperThumbH}
	worker := r.wallpaperThumbsLocked()
	if image, ok := worker.Lookup(key); ok {
		return image
	}
	_, _, _ = worker.Request(key)
	return nil
}
