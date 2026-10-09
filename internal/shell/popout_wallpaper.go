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

	"github.com/Nomadcxx/sysc-shell/internal/theme"
	"github.com/Nomadcxx/sysc-shell/internal/ui"
	"github.com/Nomadcxx/sysc-shell/internal/wallpaper"
)

// Wallpaper picker chrome. The plugin's layout with a virtualized grid in place
// of its page buttons: pagination was a plugin-API ceiling, not a preference
// (D4/D8). The grid's tile size is derived from the panel width (wallpaperGrid).
const (
	wallpaperColumns  = 4
	wallpaperGridGap  = 10
	wallpaperPadding  = 16
	wallpaperFieldH   = 44
	wallpaperCaptionH = 22

	// The tile grid. The gaps are what keep rows apart: the virtual list
	// centres a row of tile height inside the row pitch, so the pitch minus
	// the tile is empty space. The scroll strip is the 4 px scrollbar the list
	// paints over its right edge, plus clearance from the last column. The
	// row pitch these give (144 at the 980 panel) is what still fits four rows
	// in the laptop's 580 px grid.
	wallpaperColGap       = 12
	wallpaperRowGap       = 12
	wallpaperTilePad      = 5
	wallpaperTileInnerGap = 5
	wallpaperTileCaptionH = 20
	wallpaperScrollStrip  = 12

	// Chrome controls. The output select is D4's 170px minimum.
	wallpaperOutputWidth     = 170
	wallpaperFilterWidth     = 220
	wallpaperControlPad      = 8
	wallpaperIconSize        = 18
	wallpaperPlaceholderIcon = 32
	wallpaperSelectedStroke  = 2

	// The folder dropdown. Options are one per row and the list is capped so
	// a library of dozens of folders scrolls inside its own box instead of
	// pushing the grid off the panel.
	wallpaperOptionRowH   = 30
	wallpaperOptionMaxRow = 6
	wallpaperFolderWidth  = 300
	wallpaperPaletteWidth = 240

	// wallpaperAutoPalette is the theme option that means "derive the palette
	// from whatever wallpaper is applied", which is the behaviour before any
	// scheme is pinned.
	wallpaperAutoPalette = "Auto (from wallpaper)"

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
			var hosts []*PanelHost
			for _, id := range []PanelID{PanelWallpaper, PanelTerminalArt} {
				h := r.panelHosts[id]
				if h == nil {
					continue
				}
				h.wallpaperSnap = snap
				h.wallpaperOutput = wallpaperOutputSelection(snap, h.wallpaperOutput)
				if id == PanelWallpaper && h.wallpaperDir == "" {
					h.wallpaperOpenDir(r, firstRoot(snap))
				}
				r.rebuildPanel(h)
				hosts = append(hosts, h)
			}
			bars := r.noteEffectOutputsLocked(snap)
			var surfacePubs []wayland.Invalidation
			if len(bars) > 0 {
				// Panels attached to a rethemed bar share its ground.
				cfg, tokens := r.effectiveThemeLocked()
				surfacePubs = r.retheThemeOpenSurfacesLocked(cfg, tokens)
			}
			r.mu.Unlock()
			for _, h := range hosts {
				r.publishSurface(h.output, panelSurfaceID(h.id))
			}
			for _, global := range bars {
				r.publishSurface(global, "")
			}
			for _, p := range surfacePubs {
				r.publishSurface(p.Global, p.SurfaceID)
			}
		}
	}
}

// noteEffectOutputsLocked records which outputs play a terminal effect and
// rethemes each bar whose answer changed, returning those bars' outputs.
// Paused counts: the frozen frame is still the wallpaper behind the bar.
func (r *Registry) noteEffectOutputsLocked(snap wallpaper.Snapshot) []uint32 {
	next := map[string]bool{}
	for _, connector := range snap.Connectors {
		state := snap.Runtime[connector].State
		if snap.Assignments[connector].Kind == wallpaper.KindEffect &&
			(state == wallpaper.StatePlaying || state == wallpaper.StatePaused) {
			next[connector] = true
		}
	}
	var changed []uint32
	for global, bar := range r.bars {
		connector := bar.connector()
		if next[connector] == r.effectOutputs[connector] {
			continue
		}
		bar.retheme(bar.themeSnapshot().WithEffectBehind(next[connector], r.caps.Blur))
		changed = append(changed, global)
	}
	r.effectOutputs = next
	return changed
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
// A real library has dozens of folders -- this one has 26, named after the
// themes their wallpapers belong to -- so they cannot share the tile grid and
// they overflow any single row. They are the options of a dropdown.
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

// wallpaperChromeH is the height of one chrome control. The Wallpaper panel
// sizes it with its chrome metrics; Terminal Art shares the helpers and keeps
// the compact size.
func wallpaperChromeH(h *PanelHost) int {
	if h.id == PanelWallpaper {
		return wallpaperChromeOf(h).control
	}
	return h.theme.Metrics.CompactControl
}

// wallpaperChrome is the picker's section metrics. A short panel (the laptop)
// uses the compact set, so the sectioned chrome still leaves three and a half
// rows of tiles (owner decision, 2026-10-09).
type wallpaperChrome struct {
	headerH, control, cardPad, footerH int
}

// wallpaperCompactBelow is the panel height under which the chrome goes
// compact. The laptop's panel is about 820; the desktop's 1100.
const wallpaperCompactBelow = 960

func wallpaperChromeOf(h *PanelHost) wallpaperChrome {
	m := h.theme.Metrics
	if h.place.Panel.H > 0 && h.place.Panel.H < wallpaperCompactBelow {
		return wallpaperChrome{headerH: 48, control: m.CompactControl, cardPad: 8, footerH: 40}
	}
	return wallpaperChrome{headerH: 56, control: m.StandardControl, cardPad: 12, footerH: 48}
}

// wallpaperCombo is a closed dropdown: the current value and a chevron. It is
// a button rather than a ui.KindMenu because KindMenu renders its options
// inline and unbounded, which a 26-folder list cannot be.
func wallpaperCombo(h *PanelHost, menu, value string, width int) *ui.Node {
	label := value
	if label == "" {
		label = "None"
	}
	n := wallpaperButton(h, "wallpaper-menu:"+menu, label+"  \u25be", false)
	n.Width = width
	n.Height = wallpaperChromeH(h)
	if h.wallpaperMenu == menu {
		n.State |= ui.StateSelected
	}
	return n
}

// wallpaperOptionList is an open dropdown's options: one per row, capped so
// the list scrolls inside its own box rather than growing without bound.
//
// It is a second scrollable region in this panel, which is only safe because
// the wheel is routed to the region under the pointer rather than to the first
// one in the tree (scrollAt).
func wallpaperOptionList(h *PanelHost, opts []wallpaperOption) *ui.Node {
	visible := min(len(opts), wallpaperOptionMaxRow)
	return &ui.Node{
		Kind:       ui.KindVirtualList,
		ItemCount:  len(opts),
		ItemHeight: wallpaperOptionRowH,
		Height:     visible * wallpaperOptionRowH,
		Item: func(row int) *ui.Node {
			if row < 0 || row >= len(opts) {
				return &ui.Node{Kind: ui.KindRow}
			}
			opt := opts[row]
			n := wallpaperButton(h, opt.action, opt.label, opt.selected)
			n.Height = wallpaperOptionRowH
			return &ui.Node{Kind: ui.KindRow, Children: []*ui.Node{n}}
		},
	}
}

// wallpaperOption is one row of an open dropdown.
type wallpaperOption struct {
	action   string
	label    string
	selected bool
}

// wallpaperFolderOptions are the roots and the current directory's children,
// in one list. Keeping the roots here is what makes a second library -- the
// video directory -- reachable without a strip of its own.
func wallpaperFolderOptions(h *PanelHost) []wallpaperOption {
	out := make([]wallpaperOption, 0, 8)
	if lib := h.wallpaperSnap.Library; lib != nil {
		for _, root := range lib.Roots() {
			out = append(out, wallpaperOption{
				action:   "wallpaper-dir:" + root,
				label:    wallpaperRootLabel(root),
				selected: root == h.wallpaperDir,
			})
		}
	}
	for _, dir := range wallpaperDirs(h) {
		out = append(out, wallpaperOption{
			action:   "wallpaper-dir:" + dir.Path,
			label:    dir.Name,
			selected: dir.Path == h.wallpaperDir,
		})
	}
	return out
}

// wallpaperPaletteOptions are the named schemes, with Auto first.
func wallpaperPaletteOptions(h *PanelHost) []wallpaperOption {
	current := wallpaperPaletteLabel(h)
	out := []wallpaperOption{{
		action:   "wallpaper-palette:auto",
		label:    wallpaperAutoPalette,
		selected: current == wallpaperAutoPalette,
	}}
	for _, name := range theme.PaletteNames() {
		out = append(out, wallpaperOption{
			action:   "wallpaper-palette:" + name,
			label:    name,
			selected: name == current,
		})
	}
	return out
}

// wallpaperTree projects the last snapshot as the sectioned SYSC chrome: a
// header band carrying the rail, a controls card (what is showing and where,
// then how to find something), the path rule, the virtualized grid, and a
// footer band (owner decisions, 2026-10-09 audit).
func wallpaperTree(r *Registry, h *PanelHost) *ui.Node {
	if h.search == nil {
		h.search = ui.NewField("")
	}
	// Mirrored under Registry.mu so the theme combobox can name what is
	// pinned without the tree builders reaching for the registry.
	h.wallpaperPaletteSource = r.cfg.ThemeGen.Source
	h.wallpaperPaletteSeed = r.cfg.ThemeGen.Seed
	h.wallpaperThemeErr = r.themeErr
	h.wallpaperTreeScale = h.scale120

	inner := max(h.place.Panel.W-2*wallpaperPadding, 0)
	media := wallpaperMedia(h)

	children := []*ui.Node{
		wallpaperHeader(h, inner),
		wallpaperControls(h, inner),
	}
	if h.wallpaperMenu == "folder" {
		children = append(children, wallpaperOptionList(h, wallpaperFolderOptions(h)))
	}
	children = append(children, wallpaperBanners(r, h)...)
	children = append(children, wallpaperRule(h, inner, len(media)))

	// The theme list opens beside its combo in the footer, so it sits under
	// the grid and the grid gives up the height.
	var after []*ui.Node
	if h.wallpaperMenu == "palette" {
		after = append(after, wallpaperOptionList(h, wallpaperPaletteOptions(h)))
	}
	after = append(after, wallpaperFooter(h, inner))

	grid := wallpaperGridOf(h)
	grid.dither, grid.noteTop = wallpaperDither(h, grid)
	rows := (len(media) + wallpaperColumns - 1) / wallpaperColumns
	used := 0
	for _, child := range append(slices.Clone(children), after...) {
		used += childHeightFor(child)
	}
	list := &ui.Node{
		Kind:       ui.KindVirtualList,
		ItemCount:  rows,
		ItemHeight: grid.pitch,
		Height:     max(h.place.Panel.H-2*wallpaperPadding-used, 0),
		Item: func(row int) *ui.Node {
			return wallpaperRow(r, h, grid, media, row)
		},
	}
	if len(media) == 0 {
		children = append(children, wallpaperEmptyState(r, h))
	} else {
		children = append(children, list)
	}
	children = append(children, after...)

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
// broken picker, and offers the one action that gets out of it.
func wallpaperEmptyState(r *Registry, h *PanelHost) *ui.Node {
	row := func(text string, lead, button *ui.Node) *ui.Node {
		n := &ui.Node{Kind: ui.KindRow, Gap: theme.MarginM, Height: wallpaperChromeH(h)}
		if lead != nil {
			n.Children = append(n.Children, lead)
		}
		n.Children = append(n.Children, &ui.Node{Kind: ui.KindText, Text: text})
		if button != nil {
			n.Children = append(n.Children, button)
		}
		return n
	}
	lib := h.wallpaperSnap.Library
	if lib == nil {
		if r != nil && r.backgroundHeld {
			// The banner above carries the reason. Spinning here would claim
			// indexing is under way when nothing is indexing.
			return row("Wallpaper service is held", nil, nil)
		}
		return row("Indexing wallpaper library\u2026", &ui.Node{Kind: ui.KindSpinner, Key: "wallpaper-indexing"}, nil)
	}
	if search := wallpaperSearch(h); search != "" {
		return row(fmt.Sprintf("No wallpapers match %q", search), nil,
			wallpaperButton(h, "wallpaper-clear-search", "Clear search", false))
	}
	if h.wallpaperFilter != wallpaper.FilterAll && len(lib.View(h.wallpaperDir, wallpaper.FilterAll, "")) > 0 {
		text := "No videos here"
		if h.wallpaperFilter == wallpaper.FilterImages {
			text = "No images here"
		}
		return row(text, nil, wallpaperButton(h, fmt.Sprintf("wallpaper-filter:%d", wallpaper.FilterAll), "Show all", false))
	}
	if _, ok := lib.Parent(h.wallpaperDir); ok {
		return row("No images or videos in "+filepath.Base(h.wallpaperDir), nil,
			wallpaperButton(h, "wallpaper-up", "Up", false))
	}
	return row("No supported wallpapers in this library", nil,
		wallpaperButton(h, "wallpaper-library-settings", "Library settings", false))
}

// wallpaperHeader is the header band: the SYSC rail centred on the band, with
// Refresh and Close pinned to its right edge. The rail is measured so a
// leading spacer can centre it on the whole band rather than on what the
// buttons leave.
func wallpaperHeader(h *PanelHost, inner int) *ui.Node {
	c := wallpaperChromeOf(h)
	pad := (c.headerH - c.control) / 2
	measure := h.measureText()
	title := ui.TextAttrs{Role: theme.RoleTitle}
	slashW, _ := measure(launcherSlashRun, title)
	wordW, _ := measure("WALLPAPER", title)
	railW := 2*slashW + wordW + 2*theme.MarginM
	lead := max((inner-2*pad-railW)/2, 0)

	slashes := func() *ui.Node {
		return &ui.Node{Kind: ui.KindText, Text: launcherSlashRun, TextRole: theme.RoleTitle, Tone: ui.ToneAccent}
	}
	rail := &ui.Node{
		Kind: ui.KindRow, Gap: theme.MarginM, Height: c.control,
		Children: []*ui.Node{
			// An empty column is the spacer: a nested row measures from its
			// children and ignores Width, a column honours it.
			{Kind: ui.KindColumn, Width: max(lead-theme.MarginM, 0), Height: c.control},
			slashes(),
			{Kind: ui.KindText, Text: "WALLPAPER", Name: "Wallpaper", Role: "heading",
				TextRole: theme.RoleTitle, Tone: ui.ToneAccent},
			slashes(),
		},
	}
	iconButton := func(action, name, icon string) *ui.Node {
		return &ui.Node{
			Kind: ui.KindButton, Action: action, Name: name,
			Role: "button", Focusable: true, Padding: wallpaperControlPad,
			Width: c.control, Height: c.control,
			Children: []*ui.Node{{Kind: ui.KindIcon, Icon: icon, IconSize: wallpaperIconSize}},
		}
	}
	buttons := &ui.Node{Kind: ui.KindRow, Gap: theme.MarginM, Height: c.control, Children: []*ui.Node{
		iconButton("wallpaper-refresh", "Refresh", "restart_alt"),
		iconButton("wallpaper-close", "Close", "close"),
	}}
	return &ui.Node{
		Kind: ui.KindCapsule, Fill: ui.FillContainerHigh, Height: c.headerH, Padding: pad,
		Children: []*ui.Node{{
			Kind: ui.KindRow, PinEnd: true, Height: c.control, Children: []*ui.Node{rail, buttons},
		}},
	}
}

// wallpaperLabel is a mono section label in the SYSC style: accent slashes,
// then the name in the muted foreground. A positive width fixes the label's
// column, so the controls after it line up across rows; the column carries the
// width because a nested row measures from its children.
func wallpaperLabel(name string, width, height int) *ui.Node {
	// The slashes sit in their own row: a two-child row led by text pins its
	// second child to the right edge, which split a short label across the
	// column.
	label := &ui.Node{
		Kind: ui.KindRow, Gap: theme.MarginXS, Height: height,
		Children: []*ui.Node{
			{Kind: ui.KindRow, Children: []*ui.Node{
				{Kind: ui.KindText, Text: "//", TextRole: theme.RoleMono, Tone: ui.ToneAccent},
			}},
			{Kind: ui.KindText, Text: name, TextRole: theme.RoleMono, Tone: ui.ToneSubtle},
		},
	}
	if width <= 0 {
		return label
	}
	return &ui.Node{Kind: ui.KindColumn, Width: width, Height: height, Children: []*ui.Node{label}}
}

// wallpaperSearchName names the search field. The picker opens with it
// focused, by this name, and the painter draws the search glyph and the clear
// affordance only for a field named exactly "Search".
const wallpaperSearchName = "Search"

// wallpaperHairline is the rule between the controls card's two rows.
const wallpaperHairline = 1

// wallpaperLabelW is the width of a controls-card label column.
const wallpaperLabelW = 76

// wallpaperPinned is a row whose right group is pinned to the right edge.
func wallpaperPinned(height int, left, right []*ui.Node) *ui.Node {
	return &ui.Node{
		Kind: ui.KindRow, PinEnd: true, Height: height,
		Children: []*ui.Node{
			{Kind: ui.KindRow, Gap: wallpaperGridGap, Height: height, Children: left},
			{Kind: ui.KindRow, Gap: wallpaperGridGap, Height: height, Children: right},
		},
	}
}

// wallpaperControls is the controls card. The first row is where an apply
// lands and what is there now, with the controls that act on it; the second
// is how to find something. The output select lives here rather than in the
// header: it picks the target of an apply, beside Pause and Restore.
func wallpaperControls(h *PanelHost, inner int) *ui.Node {
	c := wallpaperChromeOf(h)
	width := inner - 2*c.cardPad
	measure := h.measureText()

	// Row one: display, now showing, then the actions pinned right.
	var actions []*ui.Node
	if paused, ok := wallpaperPlaybackState(h); ok {
		action, label := "wallpaper-pause", "Pause"
		if paused {
			action, label = "wallpaper-resume", "Resume"
		}
		actions = append(actions, wallpaperButton(h, action, label, false))
	}
	for _, connector := range wallpaperTargets(h) {
		if h.wallpaperSnap.Assignments[connector].Kind == wallpaper.KindEffect {
			actions = append(actions, wallpaperButton(h, "wallpaper-open-art", "Open Terminal Art", false))
			break
		}
	}
	targets, hasEffects := wallpaperRestoreTargets(h)
	restore := wallpaperButton(h, "wallpaper-restore", wallpaperRestoreLabel(h), false)
	if len(targets) == 0 {
		restore.State |= ui.StateDisabled
		if hasEffects {
			restore.Tooltip = "Restore effects in Terminal Art"
		} else {
			restore.Tooltip = "No wallpaper assigned"
		}
	} else if hasEffects {
		restore.Tooltip = "Terminal Art outputs are skipped"
	}
	actions = append(actions, restore)
	actionsW := 0
	for _, a := range actions {
		w, _ := measure(a.Name, ui.TextAttrs{})
		actionsW += w + 2*wallpaperControlPad + wallpaperGridGap
	}
	output := wallpaperOutputSelect(h, "wallpaper-output:")
	outputW := output.Width
	if outputW == 0 {
		outputW, _ = measure(output.Text, ui.TextAttrs{Role: theme.RoleMono})
	}
	nowW := max(width-wallpaperLabelW-outputW-actionsW-3*wallpaperGridGap, 0)
	now := &ui.Node{
		Kind: ui.KindRow, Gap: theme.MarginXS, Height: c.control,
		Children: []*ui.Node{
			{Kind: ui.KindText, Text: "\u25b8", TextRole: theme.RoleMono, Tone: ui.ToneAccent},
			{Kind: ui.KindText, Text: "now: " + wallpaperSummary(h.wallpaperSnap, h.wallpaperOutput),
				Name: "Now showing", TextRole: theme.RoleMono, Tone: ui.ToneSubtle,
				MaxWidth: max(nowW-wallpaperIconSize, 0)},
		},
	}
	display := wallpaperPinned(c.control,
		[]*ui.Node{wallpaperLabel("display", wallpaperLabelW, c.control), output, now}, actions)

	// Row two: search, kind filter, folder, and Up once below a root.
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
	show := &ui.Node{
		Kind: ui.KindSegmented, Key: "wallpaper-filter", Gap: theme.MarginXXS,
		Width: wallpaperFilterWidth, Height: c.control, Children: segments,
	}
	tail := []*ui.Node{show, wallpaperCombo(h, "folder", wallpaperFolderLabel(h), wallpaperFolderWidth)}
	used := wallpaperLabelW + wallpaperFilterWidth + wallpaperFolderWidth + 3*wallpaperGridGap
	if h.wallpaperSnap.Library != nil {
		if _, ok := h.wallpaperSnap.Library.Parent(h.wallpaperDir); ok {
			up := wallpaperButton(h, "wallpaper-up", "Up", false)
			tail = append(tail, up)
			upW, _ := measure("Up", ui.TextAttrs{})
			used += wallpaperGridGap + upW + 2*wallpaperControlPad
		}
	}
	// The field draws its own search glyph, so the row carries no second one.
	field := h.search.Node(wallpaperSearchName)
	field.Height = c.control
	// One padded line has to fit the compact row: 8 overflowed it.
	field.Padding = 6
	field.Width = max(width-used, 0)
	find := &ui.Node{
		Kind: ui.KindRow, Gap: wallpaperGridGap, Height: c.control,
		Children: append([]*ui.Node{wallpaperLabel("find", wallpaperLabelW, c.control), field}, tail...),
	}

	sep := &ui.Node{Kind: ui.KindSeparator, Height: wallpaperHairline}
	gap := theme.MarginM
	cardH := 2*c.cardPad + 2*c.control + wallpaperHairline + 2*gap
	return &ui.Node{
		Kind: ui.KindCapsule, Fill: ui.FillContainerHigh, Padding: c.cardPad,
		Height: cardH,
		Children: []*ui.Node{{
			Kind: ui.KindColumn, Gap: gap, Children: []*ui.Node{display, sep, find},
		}},
	}
}

// wallpaperRule is the line over the grid, drawn in box characters: the
// folder on the left, the count on the right, rule between.
func wallpaperRule(h *PanelHost, inner, count int) *ui.Node {
	measure := h.measureText()
	mono := ui.TextAttrs{Role: theme.RoleMono}
	dir := wallpaperRulePath(h.wallpaperDir)
	tail := plural(count, "item")
	glyphW, lineH := measure("\u2500", mono)
	if glyphW <= 0 {
		glyphW = 8
	}
	leftW, _ := measure("\u2500\u2500 "+dir+" ", mono)
	rightW, _ := measure(" "+tail+" \u2500\u2500", mono)
	fill := max((inner-leftW-rightW)/glyphW, 2)
	text := func(s string, tone ui.Tone) *ui.Node {
		return &ui.Node{Kind: ui.KindText, Text: s, TextRole: theme.RoleMono, Tone: tone}
	}
	return &ui.Node{
		Kind: ui.KindRow, Height: max(lineH, 16), Name: "Folder " + dir + ", " + tail,
		Children: []*ui.Node{
			text("\u2500\u2500 ", ui.ToneSubtle),
			text(dir, ui.ToneNormal),
			text(" "+strings.Repeat("\u2500", fill)+" ", ui.ToneSubtle),
			text(tail, ui.ToneNormal),
			text(" \u2500\u2500", ui.ToneSubtle),
		},
	}
}

// wallpaperRulePath shortens the open folder for the rule: the home directory
// reads as ~.
func wallpaperRulePath(dir string) string {
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		if rest, ok := strings.CutPrefix(dir, home); ok && (rest == "" || strings.HasPrefix(rest, "/")) {
			return "~" + rest
		}
	}
	return dir
}

// wallpaperOutputSelect is All plus one segment per connector, each acting
// prefix+token. With one connector there is nothing to choose, so it is a
// caption naming the output instead of a two-segment control.
func wallpaperOutputSelect(h *PanelHost, prefix string) *ui.Node {
	ch := wallpaperChromeH(h)
	if len(h.wallpaperSnap.Connectors) == 1 {
		return &ui.Node{
			Kind: ui.KindText, Text: h.wallpaperSnap.Connectors[0],
			TextRole: theme.RoleCaption, Height: ch,
		}
	}
	tokens := append([]string{wallpaper.AllOutputs}, h.wallpaperSnap.Connectors...)
	segments := make([]*ui.Node, 0, len(tokens))
	for _, token := range tokens {
		segments = append(segments, wallpaperSegment(h, prefix+token,
			wallpaperOutputLabel(token), token == h.wallpaperOutput))
	}
	return &ui.Node{
		Kind: ui.KindSegmented, Key: strings.TrimSuffix(prefix, ":"), Gap: theme.MarginXXS,
		Width: wallpaperOutputWidth, Height: ch, Children: segments,
	}
}

func wallpaperOutputLabel(token string) string {
	if token == wallpaper.AllOutputs {
		return "All"
	}
	return token
}

// wallpaperRootLabel names a library root by its directory.
func wallpaperRootLabel(root string) string { return filepath.Base(root) }

// wallpaperFolderLabel is the folder combobox's closed value.
func wallpaperFolderLabel(h *PanelHost) string {
	if h.wallpaperDir == "" {
		return ""
	}
	return filepath.Base(h.wallpaperDir)
}

// wallpaperPaletteLabel is the theme combobox's closed value: the pinned
// scheme, or Auto when the palette follows the applied wallpaper.
func wallpaperPaletteLabel(h *PanelHost) string {
	if h.wallpaperPaletteSource == "palette" && h.wallpaperPaletteSeed != "" {
		return h.wallpaperPaletteSeed
	}
	return wallpaperAutoPalette
}

// wallpaperSegment is one exclusive choice inside a segmented control.
func wallpaperSegment(h *PanelHost, action, label string, selected bool) *ui.Node {
	n := &ui.Node{
		Kind: ui.KindButton, Action: action, Name: label,
		Role: "tab", Focusable: true, Padding: wallpaperControlPad,
		Height:   wallpaperChromeH(h),
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
		Height:   wallpaperChromeH(h),
		Children: []*ui.Node{{Kind: ui.KindText, Text: label}},
	}
	if selected {
		n.State |= ui.StateSelected
	}
	return n
}

// wallpaperGrid is the tile geometry for one grid width. Tiles share the width
// left beside the scroll strip, the thumbnail fills the tile inside its
// padding, and its height keeps the preview cache's aspect: the painter scales
// a raster to its box without preserving aspect, so a box off that ratio would
// stretch every preview.
type wallpaperGrid struct {
	tileW, tileH   int
	thumbW, thumbH int
	pitch          int
	// dither is the texture a decoding tile shows, sized to the thumbnail
	// box; empty when there is nothing to measure it with. noteTop places a
	// one-line note in the middle of the box.
	dither  []string
	noteTop int
	// keyW and keyH are the thumbnail box in physical pixels: the size a
	// preview is decoded to, so the painter never scales it up.
	keyW, keyH int
}

func wallpaperGridFor(width int) wallpaperGrid {
	tileW := max((width-wallpaperScrollStrip-(wallpaperColumns-1)*wallpaperColGap)/wallpaperColumns, 2*wallpaperTilePad+1)
	thumbW := tileW - 2*wallpaperTilePad
	thumbH := max(thumbW*wallpaper.ThumbHeight/wallpaper.ThumbWidth, 1)
	tileH := 2*wallpaperTilePad + thumbH + wallpaperTileInnerGap + wallpaperTileCaptionH
	return wallpaperGrid{tileW: tileW, tileH: tileH, thumbW: thumbW, thumbH: thumbH, pitch: tileH + wallpaperRowGap,
		keyW: thumbW, keyH: thumbH}
}

// wallpaperDither sizes the decoding texture to the thumbnail box: a sparse
// scatter of light shade, so it reads as a field rather than a pattern and
// stays behind the note. It also reports where a one-line note centres.
func wallpaperDither(h *PanelHost, g wallpaperGrid) ([]string, int) {
	glyphW, lineH := h.measureText()("\u2591", ui.TextAttrs{Role: theme.RoleMono})
	if glyphW <= 0 || lineH <= 0 {
		return nil, 0
	}
	cols, lines := g.thumbW/glyphW, g.thumbH/lineH
	out := make([]string, 0, lines)
	for i := range lines {
		row := []rune(strings.Repeat(" ", cols))
		for j := range row {
			// A fixed scatter, about one cell in five, different per line.
			if (j*7+i*13)%11 < 2 {
				row[j] = '\u2591'
			}
		}
		out = append(out, string(row))
	}
	return out, max((g.thumbH-lineH)/2, 0)
}

// wallpaperGridOf is the grid for this host's panel width.
func wallpaperGridOf(h *PanelHost) wallpaperGrid {
	g := wallpaperGridFor(max(h.place.Panel.W-2*wallpaperPadding, 0))
	if s := ui.Scale120(h.scale120); s.Valid() {
		g.keyW, g.keyH = s.Physical(g.thumbW), s.Physical(g.thumbH)
	}
	return g
}

// wallpaperRow builds one row of up to four tiles. It runs inside layout, on
// the Wayland owner, so it only ever reads already-decoded rasters.
//
// The row carries the tile height, so the virtual list centres it in the row
// pitch and the difference stays empty: that is the gap between rows.
func wallpaperRow(r *Registry, h *PanelHost, g wallpaperGrid, media []wallpaper.Entry, row int) *ui.Node {
	start := row * wallpaperColumns
	tiles := make([]*ui.Node, 0, wallpaperColumns)
	for i := start; i < start+wallpaperColumns && i < len(media); i++ {
		tiles = append(tiles, wallpaperTile(r, h, g, media[i], i))
	}
	return &ui.Node{Kind: ui.KindRow, Gap: wallpaperColGap, Height: g.tileH, Children: tiles}
}

// wallpaperTile is a capsule around a thumbnail and a caption. The capsule
// supplies the tile chrome and takes its radius from the theme's CardRadius,
// so the tile follows the user's configured radius.
//
// A thumbnail that is not ready keeps its box and says why, in the SYSC mono
// voice: a dither field with "decoding" while the preview is coming, or
// "no preview" once the generator has recorded that it cannot make one (D6).
func wallpaperTile(r *Registry, h *PanelHost, g wallpaperGrid, entry wallpaper.Entry, index int) *ui.Node {
	raster, state := wallpaperThumbFor(r, g, entry)
	var thumb *ui.Node
	switch {
	case raster != nil:
		thumb = &ui.Node{Kind: ui.KindImage, ImageW: g.thumbW, ImageH: g.thumbH, Image: raster}
	case state == wallpaperPreviewFailed:
		thumb = wallpaperThumbNote(g, nil, "\u2717", ui.ToneError, "no preview")
	case entry.IsDir:
		thumb = &ui.Node{Kind: ui.KindRow, Width: g.thumbW, Height: g.thumbH, Children: []*ui.Node{{
			Kind: ui.KindIcon, Icon: wallpaperPlaceholderGlyph(entry), IconSize: wallpaperPlaceholderIcon,
		}}}
	default:
		thumb = wallpaperThumbNote(g, g.dither, wallpaperSpinFrame(index), ui.ToneAccent, "decoding")
	}
	if entry.Kind == wallpaper.KindVideo && !entry.IsDir {
		thumb = wallpaperVideoTag(g, thumb)
	}

	caption := &ui.Node{Kind: ui.KindRow, Gap: theme.MarginXS, Height: wallpaperTileCaptionH}
	name := &ui.Node{Kind: ui.KindText, Text: wallpaperCaption(entry), MaxWidth: g.thumbW, Height: wallpaperTileCaptionH}
	caption.Children = []*ui.Node{name}
	if tag := wallpaperAppliedTag(h, entry); tag != "" {
		chip := &ui.Node{Kind: ui.KindText, Text: tag, Name: "Applied", TextRole: theme.RoleMono, Tone: ui.ToneAccent}
		tagW, _ := h.measureText()(tag, ui.TextAttrsOf(chip))
		name.MaxWidth = max(g.thumbW-tagW-theme.MarginXS, 0)
		caption.Children = append(caption.Children, chip)
	}

	content := &ui.Node{Kind: ui.KindColumn, Gap: wallpaperTileInnerGap, Children: []*ui.Node{thumb, caption}}
	child := content
	// The keyboard selection is a muted wash plus the accent corner brackets.
	// StateSelected on a capsule paints a solid accent slab, which reads as
	// "applied" rather than "focused" and drowns the thumbnail.
	if index == h.wallpaperSel {
		child = &ui.Node{Kind: ui.KindStack, Children: []*ui.Node{content, wallpaperCorners(g)}}
	}
	tile := &ui.Node{
		Kind:      ui.KindCapsule,
		Fill:      ui.FillContainerHigh,
		Width:     g.tileW,
		Height:    g.tileH,
		Padding:   wallpaperTilePad,
		Action:    "wallpaper-tile",
		Name:      entry.Path,
		Focusable: true,
		Children:  []*ui.Node{child},
	}
	// The output's current wallpaper is outlined, so the picker says what is
	// already applied rather than only what could be (D6).
	if matched, _ := wallpaperMatchCount(h, entry); matched > 0 {
		tile.Stroke = wallpaperSelectedStroke
		tile.StrokeFill = ui.FillAccent
	}
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

// wallpaperThumbNote fills a thumbnail box that has no raster: an optional
// dither field behind a centred mono line, a glyph then a word.
func wallpaperThumbNote(g wallpaperGrid, dither []string, glyph string, tone ui.Tone, word string) *ui.Node {
	field := &ui.Node{Kind: ui.KindColumn, Width: g.thumbW, Height: g.thumbH}
	for _, line := range dither {
		field.Children = append(field.Children, &ui.Node{
			Kind: ui.KindText, Text: line, TextRole: theme.RoleMono, Tone: ui.ToneSubtle, MaxWidth: g.thumbW,
		})
	}
	note := &ui.Node{Kind: ui.KindRow, Gap: theme.MarginXS, CenterX: true, Children: []*ui.Node{
		{Kind: ui.KindText, Text: glyph, TextRole: theme.RoleMono, Tone: tone},
		{Kind: ui.KindText, Text: word, TextRole: theme.RoleMono},
	}}
	return &ui.Node{Kind: ui.KindStack, Width: g.thumbW, Height: g.thumbH, Children: []*ui.Node{
		field,
		{Kind: ui.KindColumn, Children: []*ui.Node{{Kind: ui.KindColumn, Height: g.noteTop}, note}},
	}}
}

// wallpaperCorners is the keyboard selection's accent corner brackets, drawn
// in box characters at the tile's four inner corners.
func wallpaperCorners(g wallpaperGrid) *ui.Node {
	corner := func(s string) *ui.Node {
		return &ui.Node{Kind: ui.KindText, Text: s, TextRole: theme.RoleMono, Tone: ui.ToneAccent}
	}
	pair := func(left, right string) *ui.Node {
		return &ui.Node{Kind: ui.KindRow, PinEnd: true, Children: []*ui.Node{corner(left), corner(right)}}
	}
	inner := g.tileH - 2*wallpaperTilePad
	return &ui.Node{Kind: ui.KindColumn, Height: inner, Children: []*ui.Node{
		pair("\u250c", "\u2510"),
		{Kind: ui.KindColumn, Height: max(inner-2*wallpaperTileCaptionH, 0)},
		pair("\u2514", "\u2518"),
	}}
}

// wallpaperSpinFrames is the braille spinner sysc-greet draws.
var wallpaperSpinFrames = []string{"\u280b", "\u2819", "\u2839", "\u2838", "\u283c", "\u2834", "\u2826", "\u2827", "\u2807", "\u280f"}

// wallpaperSpinFrame is a spinner frame. It is picked rather than animated:
// the frame moves when the grid repaints for a landed preview, so it turns
// while work is actually landing and stops when it is not.
func wallpaperSpinFrame(n int) string {
	return wallpaperSpinFrames[((n%len(wallpaperSpinFrames))+len(wallpaperSpinFrames))%len(wallpaperSpinFrames)]
}

// wallpaperAppliedTag names where a tile is applied: the output, or how many
// of the selected outputs show it when the select is All.
func wallpaperAppliedTag(h *PanelHost, entry wallpaper.Entry) string {
	matched, total := wallpaperMatchCount(h, entry)
	switch {
	case matched == 0:
		return ""
	case total == 1:
		return "[\u2713] " + wallpaperTargets(h)[0]
	default:
		return fmt.Sprintf("[\u2713] %d/%d", matched, total)
	}
}

// wallpaperCanApply reports whether a tile is activatable.
func wallpaperCanApply(h *PanelHost, entry wallpaper.Entry) bool {
	switch entry.Kind {
	case wallpaper.KindVideo:
		return h.wallpaperSnap.Caps.GSlapper
	}
	return true
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

// wallpaperCaption is the filename. A video is marked on its thumbnail
// (wallpaperVideoTag), so the caption keeps its whole width for the name.
func wallpaperCaption(entry wallpaper.Entry) string {
	return entry.Name
}

// wallpaperVideoTag lays a small "\u25b6 VID" plate over a video's thumbnail
// corner. The plate is the scrim, so the tag stays legible over any frame.
func wallpaperVideoTag(g wallpaperGrid, thumb *ui.Node) *ui.Node {
	plate := &ui.Node{
		Kind: ui.KindCapsule, Fill: ui.FillScrim, Padding: theme.MarginXXS, PaddingX: theme.MarginXS,
		Children: []*ui.Node{{Kind: ui.KindText, Text: "\u25b6 VID", Name: "Video", TextRole: theme.RoleMono}},
	}
	// The thumbnail rides in a column: a stack only lays out column and row
	// layers, and a still-decoding thumbnail is a stack of its own.
	return &ui.Node{Kind: ui.KindStack, Width: g.thumbW, Height: g.thumbH, Children: []*ui.Node{
		{Kind: ui.KindColumn, Children: []*ui.Node{thumb}},
		{Kind: ui.KindColumn, Padding: theme.MarginXS, Children: []*ui.Node{{Kind: ui.KindRow, Children: []*ui.Node{plate}}}},
	}}
}

// wallpaperTargets resolves the output select to the connectors it acts on.
func wallpaperTargets(h *PanelHost) []string {
	output := wallpaperOutputSelection(h.wallpaperSnap, h.wallpaperOutput)
	if output == wallpaper.AllOutputs {
		return h.wallpaperSnap.Connectors
	}
	return []string{output}
}

func wallpaperOutputSelection(snap wallpaper.Snapshot, selected string) string {
	if selected == wallpaper.AllOutputs || slices.Contains(snap.Connectors, selected) {
		return selected
	}
	return wallpaper.AllOutputs
}

// wallpaperMatchCount reports how many of the selected outputs already show
// path. It is read back from the snapshot rather than composed here, so the
// badge cannot drift from what is actually assigned.
func wallpaperMatchCount(h *PanelHost, entry wallpaper.Entry) (matched, total int) {
	for _, connector := range wallpaperTargets(h) {
		total++
		if h.wallpaperSnap.Assignments[connector].Path == entry.Path {
			matched++
		}
	}
	return matched, total
}

// wallpaperBanners surfaces the capability, scan, and apply failures. They are
// separate rows because they have separate causes: a missing engine is not a
// bad directory is not a refused apply (D4).
func wallpaperBanners(r *Registry, h *PanelHost) []*ui.Node {
	var out []*ui.Node
	add := func(text string, tone ui.Tone) {
		if text == "" {
			return
		}
		out = append(out, &ui.Node{Kind: ui.KindText, Text: text, Tone: tone, Height: wallpaperCaptionH})
	}
	// A held background means no service, no library, and no assignment. The
	// only copy of the reason used to live on the Lock Screen page.
	if r != nil && r.backgroundHeld && r.backgroundError != "" {
		add("Wallpaper and screensaver are held: "+r.backgroundError+".", ui.ToneError)
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
	// The palette is the other half of applying a wallpaper. A generator that
	// cannot produce a usable one leaves the old colours up, which is correct
	// but looks exactly like nothing having happened unless it says so.
	if h.wallpaperThemeErr == theme.ErrAwaitingWallpaper.Error() {
		add(h.wallpaperThemeErr, ui.ToneSubtle)
	} else {
		add(h.wallpaperThemeErr, ui.ToneError)
	}
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

// wallpaperFooter is the footer band: the key legend (or, while previews are
// generating, their progress) on the left; the shell theme and the engine
// painting the selected outputs pinned right.
func wallpaperFooter(h *PanelHost, inner int) *ui.Node {
	c := wallpaperChromeOf(h)
	pad := (c.footerH - c.control) / 2

	left := []*ui.Node{{Kind: ui.KindText, Text: wallpaperHints, TextRole: theme.RoleMono, Tone: ui.ToneSubtle}}
	if progress := wallpaperProgress(h); progress != nil {
		left = []*ui.Node{progress}
	}

	engine := &ui.Node{Kind: ui.KindText, Role: "status", Name: "Engine", TextRole: theme.RoleMono}
	if name := wallpaperActiveEngine(h); name == wallpaper.EngineGSlapper {
		engine.Text = "gSlapper"
	} else if name != "" {
		engine.Text = name
	} else if wallpaperNoEngine(h) {
		engine.Text, engine.Tone = "no wallpaper engine installed", ui.ToneError
	}
	right := []*ui.Node{
		wallpaperLabel("theme", 0, c.control),
		wallpaperCombo(h, "palette", wallpaperPaletteLabel(h), wallpaperPaletteWidth),
		{Kind: ui.KindRow, Height: c.control, Children: []*ui.Node{
			{Kind: ui.KindText, Text: "[", TextRole: theme.RoleMono, Tone: ui.ToneSubtle},
			engine,
			{Kind: ui.KindText, Text: "]", TextRole: theme.RoleMono, Tone: ui.ToneSubtle},
		}},
	}
	return &ui.Node{
		Kind: ui.KindCapsule, Fill: ui.FillContainerHigh, Height: c.footerH, Padding: pad,
		PaddingX: wallpaperPadding,
		Children: []*ui.Node{wallpaperPinned(c.control, left, right)},
	}
}

// wallpaperBarCells is the progress bar's width in block characters.
const wallpaperBarCells = 20

// wallpaperProgress is the footer's preview progress while generation runs,
// or nil when it is done: a braille spinner, a block-character bar and the
// count. The open folder's progress leads when the walk is reporting it, since
// that is the grid being waited on; the library's follows.
func wallpaperProgress(h *PanelHost) *ui.Node {
	snap := h.wallpaperSnap
	done, total := snap.ThumbsDone, snap.ThumbsTotal
	if total <= 0 || done >= total {
		return nil
	}
	shown, of, library := done, total, ""
	if snap.ThumbsFolder != "" && snap.ThumbsFolder == h.wallpaperDir && snap.ThumbsFolderTotal > 0 {
		shown, of = snap.ThumbsFolderDone, snap.ThumbsFolderTotal
		library = fmt.Sprintf("\u00b7 library %d/%d", done, total)
	}
	filled, rest := wallpaperBar(shown, of, wallpaperBarCells)
	text := func(s string, tone ui.Tone) *ui.Node {
		return &ui.Node{Kind: ui.KindText, Text: s, TextRole: theme.RoleMono, Tone: tone}
	}
	bar := &ui.Node{Kind: ui.KindRow, Children: []*ui.Node{
		text("\u2595", ui.ToneSubtle), text(filled, ui.ToneAccent), text(rest, ui.ToneSubtle), text("\u258f", ui.ToneSubtle),
	}}
	row := &ui.Node{
		Kind: ui.KindRow, Gap: theme.MarginM, Role: "progressbar", Name: "Generating previews",
		Value: float64(shown), Max: float64(of),
		Children: []*ui.Node{
			text(wallpaperSpinFrame(done), ui.ToneAccent),
			text("previews", ui.ToneNormal),
			bar,
			text(fmt.Sprintf("%d/%d", shown, of), ui.ToneNormal),
		},
	}
	if library != "" {
		row.Children = append(row.Children, text(library, ui.ToneSubtle))
	}
	return row
}

// wallpaperBar draws done of total as cells of full block, a shaded edge where
// the work is, and light shade for the rest.
func wallpaperBar(done, total, cells int) (filled, rest string) {
	if total <= 0 || cells <= 0 {
		return "", strings.Repeat("\u2591", max(cells, 0))
	}
	full := min(done*cells/total, cells)
	filled = strings.Repeat("\u2588", full)
	left := cells - full
	edge := ""
	switch {
	case left >= 2:
		edge = "\u2593\u2592"
	case left == 1:
		edge = "\u2593"
	}
	return filled, edge + strings.Repeat("\u2591", left-len([]rune(edge)))
}

// wallpaperHints is the key legend, in the form sysc-greet and the launcher
// use. The left and right arrows are spaced: Fira Code joins an adjacent pair
// into one double-headed arrow.
const wallpaperHints = "\u2191\u2193 \u2190 \u2192 Navigate \u2022 Enter Apply \u2022 / Search \u2022 Esc Close"

// wallpaperNoEngine reports that nothing installed can paint a wallpaper.
func wallpaperNoEngine(h *PanelHost) bool {
	return !h.wallpaperSnap.Caps.GSlapper && len(h.wallpaperSnap.Caps.Statics) == 0
}

// wallpaperActiveEngine is the engine painting the selected outputs, or "" when
// they disagree.
//
// An output nothing has painted yet reports the engine that would take it, so
// the row always names a default: on a fresh session every output is in that
// state, and a row of three names with none marked was the thing that made
// "installed" and "in use" look the same. With All selected, an engine is only
// named when it drives every target.
func wallpaperActiveEngine(h *PanelHost) string {
	targets := wallpaperTargets(h)
	fallback := h.wallpaperSnap.Caps.EngineFor(wallpaper.KindImage)
	if len(targets) == 0 {
		return fallback
	}
	agreed := ""
	for _, connector := range targets {
		name := h.wallpaperSnap.Runtime[connector].Engine
		if name == "" {
			name = fallback
		}
		if name == "" || (agreed != "" && agreed != name) {
			return ""
		}
		agreed = name
	}
	return agreed
}

func wallpaperRestoreLabel(h *PanelHost) string {
	if h.wallpaperOutput == wallpaper.AllOutputs {
		return "Restore all"
	}
	return "Restore"
}

// wallpaperPlaybackState reports playback controls only for videos that are
// playing or paused. A restored or failed video has no playback to control.
// An effect's playback is the Terminal Art panel's.
func wallpaperPlaybackState(h *PanelHost) (paused, ok bool) {
	playing := false
	for _, connector := range wallpaperTargets(h) {
		if h.wallpaperSnap.Assignments[connector].Kind != wallpaper.KindVideo {
			continue
		}
		switch h.wallpaperSnap.Runtime[connector].State {
		case wallpaper.StatePlaying:
			playing = true
		case wallpaper.StatePaused:
			paused = true
		}
	}
	return paused && !playing, playing || paused
}

func wallpaperAssignmentLabel(a wallpaper.Assignment) string {
	if a.Kind == wallpaper.KindEffect && a.Effect != "" {
		return fmt.Sprintf("Terminal Art: %s (%s)", a.Effect, a.Theme)
	}
	return filepath.Base(a.Path)
}

// wallpaperSummary is the active strip's line for one output, or the mixed
// summary for All.
func wallpaperSummary(snap wallpaper.Snapshot, output string) string {
	if output != wallpaper.AllOutputs {
		a, ok := snap.Assignments[output]
		if !ok {
			return output + " \u00b7 nothing assigned"
		}
		return fmt.Sprintf("%s \u00b7 %s \u00b7 %s", output, wallpaperAssignmentLabel(a),
			wallpaperStateName(snap.Runtime[output].State))
	}
	outputs, videos, images, effects := 0, 0, 0, 0
	for _, connector := range snap.Connectors {
		a, ok := snap.Assignments[connector]
		if !ok {
			continue
		}
		outputs++
		switch a.Kind {
		case wallpaper.KindVideo:
			videos++
		case wallpaper.KindEffect:
			effects++
		default:
			images++
		}
	}
	if outputs == 0 {
		return "nothing assigned"
	}
	summary := fmt.Sprintf("%s \u00b7 %d video \u00b7 %d image",
		plural(outputs, "output"), videos, images)
	if effects > 0 {
		summary += fmt.Sprintf(" \u00b7 %d effect", effects)
	}
	return summary
}

// plural counts a noun. The All summary read "1 outputs" on a laptop, which is
// where a single-output machine first saw this line.
func plural(n int, noun string) string {
	if n == 1 {
		return "1 " + noun
	}
	return fmt.Sprintf("%d %ss", n, noun)
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

// wallpaperMoveSel walks the four-column grid.
func (h *PanelHost) wallpaperMoveSel(r *Registry, delta, count int) {
	h.wallpaperSel = gridMoveSel(h.wallpaperSel, delta, count)
	if count > 0 {
		r.rebuildPanel(h)
	}
}

// gridMoveSel moves a grid selection, clamped at each end rather than
// wrapping: wrapping from the last tile to the first reads as a jump.
func gridMoveSel(sel, delta, count int) int {
	if count == 0 {
		return 0
	}
	return min(max(sel+delta, 0), count-1)
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
		h.wallpaperOpenDir(r, entry.Path)
		h.wallpaperSel = 0
		r.rebuildPanel(h)
		return
	}
	if entry.Kind == wallpaper.KindVideo && !h.wallpaperSnap.Caps.GSlapper {
		return
	}
	h.wallpaperOutput = wallpaperOutputSelection(h.wallpaperSnap, h.wallpaperOutput)
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
	svc := r.wallpaperServiceLocked()
	if svc == nil {
		return
	}
	h.wallpaperSnap = svc.Snapshot()
	h.wallpaperOutput = wallpaperOutputSelection(h.wallpaperSnap, h.wallpaperOutput)
	targets, _ := wallpaperRestoreTargets(h)
	for _, connector := range targets {
		svc.Enqueue(wallpaper.Command{Op: wallpaper.OpRestore, Token: connector})
	}
	r.rebuildPanel(h)
}

// wallpaperRestoreTargets keeps Terminal Art assignments in their owning
// panel, even when a stale Restore action arrives from the Wallpaper panel.
func wallpaperRestoreTargets(h *PanelHost) ([]string, bool) {
	var targets []string
	hasEffects := false
	for _, connector := range wallpaperTargets(h) {
		a, assigned := h.wallpaperSnap.Assignments[connector]
		if !assigned {
			continue
		}
		if a.Kind == wallpaper.KindEffect {
			hasEffects = true
			continue
		}
		targets = append(targets, connector)
	}
	return targets, hasEffects
}

// wallpaperSetPaused holds or releases playback on the selected output.
func (h *PanelHost) wallpaperSetPaused(r *Registry, paused bool) {
	svc := r.wallpaperServiceLocked()
	if svc == nil {
		return
	}
	h.wallpaperSnap = svc.Snapshot()
	h.wallpaperOutput = wallpaperOutputSelection(h.wallpaperSnap, h.wallpaperOutput)
	op := wallpaper.OpResume
	if paused {
		op = wallpaper.OpPause
	}
	// An effect's playback is the Terminal Art panel's.
	for _, connector := range wallpaperTargets(h) {
		if h.wallpaperSnap.Assignments[connector].Kind != wallpaper.KindVideo {
			continue
		}
		state := h.wallpaperSnap.Runtime[connector].State
		if paused {
			if state != wallpaper.StatePlaying {
				continue
			}
		} else if state != wallpaper.StatePaused {
			continue
		}
		svc.Enqueue(wallpaper.Command{Op: op, Token: connector})
	}
	r.rebuildPanel(h)
}

// wallpaperOpenDir shows dir in the grid and tells the service, which
// generates that folder's previews before the rest of the library.
// Registry.mu is held.
func (h *PanelHost) wallpaperOpenDir(r *Registry, dir string) {
	h.wallpaperDir = dir
	if r != nil && r.wallpaperSvc != nil && dir != "" {
		r.wallpaperSvc.Enqueue(wallpaper.Command{Op: wallpaper.OpFocusFolder, Path: dir})
	}
}

// wallpaperUp leaves the current directory, stopping at a library root.
func (h *PanelHost) wallpaperUp(r *Registry) {
	if h.wallpaperSnap.Library == nil {
		return
	}
	if parent, ok := h.wallpaperSnap.Library.Parent(h.wallpaperDir); ok {
		h.wallpaperOpenDir(r, parent)
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
	picker := r.panelHosts[PanelWallpaper]
	media := r.panelHosts[PanelControlCenter]
	var pickerOut, mediaOut uint32
	mediaOpen := false
	if picker != nil {
		pickerOut = picker.output
		// Painting reuses the laid-out tree, and the grid's rows are built
		// during layout, so the raster only reaches its tile once the panel is
		// laid out again. That is a layout pass, not a rebuild: the tree, the
		// scroll offset and the focus all stay as they are.
		if picker.logicalW > 0 {
			_ = picker.configure(picker.logicalW, picker.logicalH, picker.scale120)
		}
	}
	if media != nil && media.section == "media" {
		mediaOut = media.output
		mediaOpen = true
		r.rebuildPanel(media)
	}
	r.mu.Unlock()
	// The picker, laid out again above, only needs a repaint. The Media page must
	// rebuild because its fallback art is retained in the tree itself.
	if picker != nil {
		r.publishSurface(pickerOut, panelSurfaceID(PanelWallpaper))
	}
	if mediaOpen {
		r.publishSurface(mediaOut, panelSurfaceID(PanelControlCenter))
	}
}

// wallpaperPreviewState is what a tile can say about its preview.
type wallpaperPreviewState int

const (
	wallpaperPreviewPending wallpaperPreviewState = iota
	wallpaperPreviewReady
	wallpaperPreviewFailed
)

// wallpaperThumbFor returns an already-decoded thumbnail, queueing a decode
// when there is not one yet, and says why there is none.
//
// This runs inside the virtual list's Item builder, which layout calls on the
// Wayland owner, so it must never decode here: it looks the raster up and asks
// for it, and the arrival lays the panel out again.
func wallpaperThumbFor(r *Registry, g wallpaperGrid, entry wallpaper.Entry) (*ui.Image, wallpaperPreviewState) {
	if r == nil || entry.IsDir {
		return nil, wallpaperPreviewPending
	}
	// Always the generated preview, never the original. A wallpaper library is
	// tens of gigabytes of pixels; decoding a 4K still on the tile path would
	// stall the picker and blow the decoder's own file bound.
	source := wallpaper.CachedStillPath(entry.Path)
	if source == "" {
		return nil, wallpaperPreviewPending
	}
	if _, err := os.Stat(source); err != nil {
		if wallpaper.PreviewFailed(entry.Path) {
			return nil, wallpaperPreviewFailed
		}
		return nil, wallpaperPreviewPending
	}
	key := wallpaperThumbKey(source, g)
	worker := r.wallpaperThumbsLocked()
	if image, ok := worker.Lookup(key); ok {
		return image, wallpaperPreviewReady
	}
	_, _, _ = worker.Request(key)
	return nil, wallpaperPreviewPending
}

// wallpaperThumbKey is the decode request for one cached preview: the
// preview file scaled to the grid's thumbnail box.
func wallpaperThumbKey(preview string, g wallpaperGrid) icons.Key {
	return icons.Key{Name: preview, W: g.keyW, H: g.keyH}
}

// wallpaperStartLocked starts the wallpaper service if it is not running.
// Registry.mu is held.
//
// The service starts with the registry rather than with the panel: an output's
// wallpaper has to come back at login whether or not anyone opens the picker
// (D20).
func (r *Registry) wallpaperStartLocked() *wallpaper.Service {
	if r.backgroundHeld {
		return r.wallpaperSvc
	}
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
		// Given at construction: startup reconcile publishes the restored
		// wallpaper's seed before a hook installed afterwards would be there
		// to hear it, and the palette would sit at its defaults all session.
		ConfigHook: r.setWallpaperSeed,
	})
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

// setPalette pins a named scheme, or returns the palette to the applied
// wallpaper when name is "auto".
//
// It writes the two fields the appearance.palette setting writes, so the
// picker and the settings panel are two ways to the same state rather than
// two competing ones.
func (r *Registry) setPalette(name string) {
	r.mu.Lock()
	source, seed := "palette", name
	if name == "auto" {
		// Auto has no seed of its own: the next apply supplies one, and until
		// then the palette already published stays.
		source, seed = "wallpaper", r.wallpaperSeedLocked()
	}
	if r.cfg.ThemeGen.Source == source && r.cfg.ThemeGen.Seed == seed {
		r.mu.Unlock()
		return
	}
	r.cfg.ThemeGen.Source = source
	r.cfg.ThemeGen.Seed = seed
	cfg := r.cfg
	r.mu.Unlock()

	r.republishTheme(cfg)
}

// wallpaperSeedLocked is the seed the running service last derived, so Auto
// returns to the current wallpaper rather than to nothing. Registry.mu held.
func (r *Registry) wallpaperSeedLocked() string {
	svc := r.wallpaperServiceLocked()
	if svc == nil {
		return r.cfg.ThemeGen.Seed
	}
	return svc.Snapshot().Seed
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
	// A pinned scheme or saved palette outranks the wallpaper. Without this the
	// first apply after choosing Catppuccin or a saved palette would silently
	// put the palette back on whatever matugen derives from the image, and the
	// choice would look like it had never been made.
	if src := r.cfg.ThemeGen.Source; src == "palette" || src == "custom" {
		r.mu.Unlock()
		return
	}
	if r.cfg.ThemeGen.Source == source && r.cfg.ThemeGen.Seed == seed {
		r.mu.Unlock()
		return
	}
	r.cfg.ThemeGen.Source = source
	r.cfg.ThemeGen.Seed = seed
	cfg := r.cfg
	r.mu.Unlock()

	r.republishTheme(cfg)
}

// republishTheme regenerates the palette for cfg and repaints every surface
// with it. It is the shared tail of both palette seams -- a wallpaper apply
// and an explicit scheme -- so the two cannot drift.
func (r *Registry) republishTheme(cfg config.Config) {
	// generateTheme runs the generator and writes the enabled templates, so it
	// is called outside the lock; it returns the previous palette unchanged if
	// the new one is incomplete, which is what keeps a bad seed from blanking
	// the shell.
	tokens, genErr := r.generateTheme(cfg)
	themeErr := ""
	if genErr != nil {
		themeErr = genErr.Error()
	}
	r.paintTheme(cfg, tokens, themeErr, true)
	if !runningAsTest() && generatedTheme(genErr) {
		go r.publishCommittedThemeSelection(cfg, tokens)
	}
}

// paintTheme repaints every surface with tokens. commit is true for the
// palette of record: it replaces the published tokens and records why the
// published palette is not the requested one. The sysc-780 preview passes
// commit=false: the committed palette stays authoritative in r.tokens and the
// published failure reason is untouched, so a preview can be reverted by
// repainting what the config already names.
//
// A commit that lands while a preview is up repaints the committed palette,
// keeps the previewing flag set until the next hide, and refreshes the reason
// that hide will restore: a preview must not erase or resurrect a generation
// failure recorded while it was up.
func (r *Registry) paintTheme(cfg config.Config, tokens theme.Tokens, themeErr string, commit bool) {
	r.mu.Lock()
	outputs, surfacePubs := r.paintThemeLocked(cfg, tokens, themeErr, commit)
	r.mu.Unlock()
	r.publishTheme(outputs, surfacePubs)
}

// paintThemeLocked resolves and applies a palette while holding Registry.mu.
// Keeping the resolution and state update in one critical section lets preview
// hide restore the latest committed palette without a stale snapshot window.
func (r *Registry) paintThemeLocked(cfg config.Config, tokens theme.Tokens, themeErr string, commit bool) (map[string]uint32, []wayland.Invalidation) {
	nextBars := make(map[*Bar]Theme, len(r.bars))
	for _, bar := range r.bars {
		next, err := resolveOutputTheme(cfg, bar.connector(), tokens, r.caps.Blur, r.effectOutputs[bar.connector()])
		if err != nil {
			if commit {
				r.themeErr = err.Error()
				if r.previewing {
					r.previewPrevErr = r.themeErr
				}
			}
			return nil, nil
		}
		nextBars[bar] = next
	}
	if commit {
		r.invalidateThemePreviewLocked()
		r.tokens = tokens
		r.themeErr = ""
		if themeErr != "" {
			r.themeErr = themeErr
		}
		if r.previewing {
			r.previewPrevErr = r.themeErr
		}
	} else {
		r.previewTheme = &themePreviewState{cfg: cfg, tokens: tokens}
		if !r.previewing {
			r.previewing = true
			r.previewPrevErr = r.themeErr
		}
	}
	for _, bar := range r.bars {
		bar.retheme(nextBars[bar])
		bar.apply(r.viewLocked(bar.connector()))
	}
	surfacePubs := r.retheThemeOpenSurfacesLocked(cfg, tokens)
	return r.outputGlobalsLocked(), surfacePubs
}

func (r *Registry) publishTheme(outputs map[string]uint32, surfacePubs []wayland.Invalidation) {
	for _, global := range outputs {
		r.publishSurface(global, "")
	}
	for _, p := range surfacePubs {
		r.publishSurface(p.Global, p.SurfaceID)
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
	case n.Action == "wallpaper-refresh":
		if svc := r.wallpaperServiceLocked(); svc != nil {
			svc.Enqueue(wallpaper.Command{Op: wallpaper.OpRefresh})
		}
		return true
	case strings.HasPrefix(n.Action, "wallpaper-menu:"):
		name := strings.TrimPrefix(n.Action, "wallpaper-menu:")
		if h.wallpaperMenu == name {
			h.wallpaperMenu = ""
		} else {
			h.wallpaperMenu = name
		}
		r.rebuildPanel(h)
		return true
	case strings.HasPrefix(n.Action, "wallpaper-palette:"):
		h.wallpaperMenu = ""
		r.setPalette(strings.TrimPrefix(n.Action, "wallpaper-palette:"))
		r.rebuildPanel(h)
		return true
	case n.Action == "wallpaper-up":
		h.wallpaperUp(r)
		return true
	case n.Action == "wallpaper-clear-search":
		h.search.Clear()
		h.wallpaperSel = 0
		r.rebuildPanel(h)
		return true
	case n.Action == "wallpaper-library-settings":
		r.closePanelLocked(PanelWallpaper)
		r.openSettingsAtLocked(h.output, "Wallpaper")
		return true
	case n.Action == "wallpaper-open-art":
		if art := r.switchPanelLocked(h, PanelTerminalArt); art != nil {
			art.wallpaperOutput = wallpaperOutputSelection(art.wallpaperSnap, h.wallpaperOutput)
			r.rebuildPanel(art)
		}
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
		h.wallpaperSelectOutput(r, token)
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
		h.wallpaperOpenDir(r, dir)
		h.wallpaperSel = 0
		h.wallpaperMenu = ""
		r.rebuildPanel(h)
		return true
	}
	return false
}

// wallpaperSelectOutput makes token the selected output.
func (h *PanelHost) wallpaperSelectOutput(r *Registry, token string) {
	// The node may have been built before a hot-unplug snapshot arrived.
	// Validate against the service's current connector list before letting a
	// stale action become the selected target.
	if svc := r.wallpaperServiceLocked(); svc != nil {
		h.wallpaperSnap = svc.Snapshot()
	}
	if token != wallpaper.AllOutputs && !slices.Contains(h.wallpaperSnap.Connectors, token) {
		token = wallpaper.AllOutputs
	}
	h.wallpaperOutput = token
	r.rebuildPanel(h)
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
	case "slapper", "awww-daemon", "swaybg", "sysc-terminal":
		return true
	}
	return strings.HasPrefix(namespace, "sysc-shell")
}
