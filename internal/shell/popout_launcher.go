package shell

import (
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"unicode"

	launcher "github.com/Nomadcxx/sysc-launch"
	"github.com/Nomadcxx/sysc-shell/internal/icons"
	"github.com/Nomadcxx/sysc-shell/internal/platform/wayland"
	"github.com/Nomadcxx/sysc-shell/internal/render"
	"github.com/Nomadcxx/sysc-shell/internal/theme"
	"github.com/Nomadcxx/sysc-shell/internal/ui"
	v1 "github.com/Nomadcxx/sysc-shell/plugin/v1"
)

// Launcher chrome: SYSC rail, pill search, a list of padded row pills with an
// 8px gap between them. The 40px icon slot is a letter until a theme raster
// lands.
//
// The icon matches DMS spotlight exactly, which arrived at 40 independently.
// The row is DMS's too, but the figure that matters is not its height: it is
// the 12-above/16-below padding the height is derived from.
const (
	launcherIconSlot = 40
	// launcherGlyphSize is a provider row's Material glyph inside that slot.
	launcherGlyphSize = 24
	// launcherRowPadTop and launcherRowPadBottom are DMS's row padding: 12
	// above the text block and 16 below. The asymmetry is the point -- the
	// four extra pixels at the foot are what make its list breathe where our
	// flat 4-at-every-level read tight.
	//
	// A ui node has one padding scalar, so the capsule takes the top figure
	// and the difference falls out of the column inside it. See launcherRow.
	launcherRowPadTop    = 12
	launcherRowPadBottom = 16
	launcherRowHeight    = launcherRowPadTop + launcherIconSlot + launcherRowPadBottom
	launcherRowGap       = 8
	launcherSlotHeight   = launcherRowHeight + launcherRowGap
	launcherFieldHeight  = 56
	// launcherMarkHeight balances the raster against the slashes beside it.
	// At 23 the mark stood taller than the RoleTitle run and the slashes read
	// light next to it; the owner picked shrinking the mark over promoting the
	// slashes to RoleHeadline, so the rail is lighter overall rather than
	// heavier. The header is measured, not assumed, so the list takes back the
	// pixels the shorter mark frees.
	launcherMarkHeight = 19
	// launcherHints is sysc-greet's own help line, verbatim. The greeter puts
	// the same string under every menu, so the launcher reads as the same
	// family rather than inventing its own key legend.
	launcherHints = "\u2191\u2193 Navigate \u2022 Enter Select \u2022 Esc Close"
	// launcherSlashRun is one side of the rail. sysc-greet pads its own rails
	// to a fixed width and lets the count fall where it may; this one is a
	// fixed six each side because the mark between them is a raster, not text,
	// so there is no width to pad to. The rail takes RoleTitle: it is the
	// panel's title, and 16/600 carries its weight beside the mark.
	launcherSlashRun = "//////"
)

// launcherServiceLocked returns the process-wide launcher service, creating
// it on first use. Lazy creation keeps registries that never open the
// launcher from paying for an XDG scan; the first open scans, and later opens
// hit the service's rescan-if-stale path (D12). Caller holds r.mu.
func (r *Registry) launcherServiceLocked() *launcher.Service {
	if r.launcherSvc == nil {
		cfg := r.launcherServiceConfig()
		cfg.History = launcher.OpenHistory(launcherHistoryPath(os.Getenv), nil)
		r.launcherSvc = launcher.NewService(cfg)
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

// launcherSnap is one ranked publish stamped with the query generation that
// produced it. The service's own Results channel carries no generation, so a
// value already sitting there (or already dequeued) can belong to an older
// query (gh #90).
type launcherSnap struct {
	gen  uint64
	rows []launcher.Result
}

type launcherKind int

const (
	launcherKindApps launcherKind = iota
	launcherKindInline
	launcherKindProvider
	launcherKindOverview
)

// launcherPlan is the shape of one shell query. passed is the text the
// service hands to Rank or to a provider, which is how a callback proves it
// belongs to the current generation.
type launcherPlan struct {
	kind           launcherKind
	passed         string
	provider       string
	inlineProvider string
	need           int
}

// launcherBatch accumulates the callbacks of one service run. Inline calc and
// application rank publish as a single snapshot, matching the service.
// ponytail: this shell has one Inline provider; if another is added, collect
// rows by prefix and merge them in registry order.
type launcherBatch struct {
	gen        uint64
	haveInline bool
	haveMain   bool
	emitted    bool
	inline     []launcher.Result
	rows       []launcher.Result
}

// relayLauncher applies result snapshots to the open launcher panel. Service
// Results values are drained and ignored: they are unstamped, and applying
// one that was queued for the previous query clears launcherAwaiting (gh #90).
func (r *Registry) relayLauncher(svc *launcher.Service) {
	r.launcherMu.Lock()
	snaps := r.launcherSnaps
	r.launcherMu.Unlock()
	if snaps == nil {
		return
	}
	ch := svc.Results()
	for {
		select {
		case <-r.closed:
			return
		case snap := <-snaps:
			r.launcherMu.Lock()
			dequeued := r.onLauncherDequeued
			r.launcherMu.Unlock()
			if dequeued != nil {
				dequeued()
			}
			r.mu.Lock()
			h := r.panelHosts[PanelLauncher]
			var output uint32
			applied := false
			if h != nil && h.launcherQueryGen == snap.gen {
				h.launcherResults = launcherWithHints(h.query, snap.rows)
				h.launcherAwaiting = false
				r.rebuildPanel(h)
				output = h.output
				applied = true
			}
			after := r.afterLauncherSnap
			r.mu.Unlock()
			if after != nil {
				after()
			}
			if applied {
				r.publishSurface(output, panelSurfaceID(PanelLauncher))
			}
		case <-ch:
		}
	}
}

func (r *Registry) ensureLauncherSnaps() {
	r.launcherMu.Lock()
	defer r.launcherMu.Unlock()
	if r.launcherSnaps == nil {
		r.launcherSnaps = make(chan launcherSnap, 1)
	}
}

// rankLauncher is the service rank hook. It stamps the rows with the query
// generation after an optional test wait, so a publish can be held until a
// newer query has been issued.
func (r *Registry) rankLauncher(entries []launcher.Entry, query string, boost func(query, identifier string) int) []launcher.Result {
	rows := launcherRank(entries, query, boost)
	r.launcherMu.Lock()
	wait := r.launcherRankWait
	r.launcherMu.Unlock()
	if wait != nil {
		wait(query)
	}
	r.noteLauncherRows(launcherAppsPrefix, query, rows)
	return rows
}

// launcherSendQuery records the field's generation and submits text. Caller
// holds r.mu. A snapshot from an earlier generation no longer matches, so it
// cannot clear launcherAwaiting.
func (r *Registry) launcherSendQuery(h *PanelHost, text string) {
	plan := classifyLauncherQuery(text, r.launcherProviders())
	r.launcherMu.Lock()
	r.launcherGen++
	gen := r.launcherGen
	r.launcherPlan = plan
	r.launcherMu.Unlock()

	h.launcherQueryGen = gen
	h.launcherAwaiting = true
	if plan.kind == launcherKindOverview {
		h.launcherResults = r.launcherOverviewRows(plan.passed)
		h.launcherSel = 0
		h.launcherAwaiting = false
	}
	r.launcherServiceLocked().Query(text)
}

func classifyLauncherQuery(text string, providers []launcher.Provider) launcherPlan {
	text = strings.TrimSpace(text)
	if text == "" {
		return launcherPlan{kind: launcherKindApps, provider: launcherAppsPrefix, need: 1}
	}
	if !strings.HasPrefix(text, "/") {
		for _, provider := range providers {
			if provider.Inline {
				return launcherPlan{kind: launcherKindInline, passed: text,
					provider: launcherAppsPrefix, inlineProvider: provider.Prefix, need: 2}
			}
		}
		return launcherPlan{kind: launcherKindApps, passed: text, provider: launcherAppsPrefix, need: 1}
	}
	seg, rest, _ := strings.Cut(text[1:], " ")
	prefix := "/" + seg
	passed := strings.TrimSpace(rest)
	if prefix == launcherAppsPrefix {
		return launcherPlan{kind: launcherKindApps, passed: passed, provider: launcherAppsPrefix, need: 1}
	}
	for _, provider := range providers {
		if prefix == provider.Prefix {
			return launcherPlan{kind: launcherKindProvider, passed: passed, provider: provider.Prefix, need: 1}
		}
	}
	return launcherPlan{kind: launcherKindOverview, passed: seg}
}

// noteLauncherRows records one service callback. The callback's query has to
// match the plan for the current generation; a rank that started for the
// previous text is ignored, and so is a snapshot emitted before the new
// generation was issued.
func (r *Registry) noteLauncherRows(provider, query string, rows []launcher.Result) {
	r.launcherMu.Lock()
	defer r.launcherMu.Unlock()
	plan := r.launcherPlan
	if r.launcherGen == 0 || query != plan.passed {
		return
	}
	switch plan.kind {
	case launcherKindApps:
		if provider != launcherAppsPrefix {
			return
		}
	case launcherKindInline:
		if provider != launcherAppsPrefix && provider != plan.inlineProvider {
			return
		}
	case launcherKindProvider:
		if provider != plan.provider {
			return
		}
	default:
		return
	}
	b := &r.launcherBatch
	if b.gen != r.launcherGen || (b.emitted && (provider == plan.inlineProvider || plan.need == 1)) {
		*b = launcherBatch{gen: r.launcherGen}
	}
	cloned := append([]launcher.Result(nil), rows...)
	if provider == plan.inlineProvider && plan.kind == launcherKindInline {
		b.inline = cloned
		b.haveInline = true
	} else {
		b.rows = cloned
		b.haveMain = true
	}
	if plan.need == 2 && (!b.haveInline || !b.haveMain) {
		return
	}
	if !b.haveMain {
		return
	}
	out := b.rows
	if plan.need == 2 {
		out = append(append([]launcher.Result(nil), b.inline...), b.rows...)
	}
	b.emitted = true
	r.emitLauncherSnapLocked(launcherSnap{gen: r.launcherGen, rows: out})
}

func (r *Registry) emitLauncherSnapLocked(snap launcherSnap) {
	if r.launcherSnaps == nil {
		return
	}
	for {
		select {
		case r.launcherSnaps <- snap:
			return
		default:
			select {
			case <-r.launcherSnaps:
			default:
			}
		}
	}
}

// launcherOverviewRows is the service's unknown-prefix list. Overview never
// calls Rank or a provider, so the shell applies it with the query that asked
// for it instead of waiting on an unstamped publish.
func (r *Registry) launcherOverviewRows(filter string) []launcher.Result {
	filter = strings.ToLower(filter)
	type item struct{ id, name, comment, icon string }
	items := []item{{"/apps", "Applications", "Installed desktop applications", "glyph:apps"}}
	for _, p := range r.launcherProviders() {
		items = append(items, item{p.Prefix, p.Name, p.Description, p.Glyph})
	}
	out := make([]launcher.Result, 0, len(items))
	for _, it := range items {
		if filter != "" &&
			!strings.Contains(strings.ToLower(it.name), filter) &&
			!strings.Contains(strings.ToLower(it.id), filter) {
			continue
		}
		out = append(out, launcher.Result{Entry: launcher.Entry{
			ID: it.id, Name: it.name, Comment: it.comment, IconName: it.icon,
		}})
	}
	return out
}

// launcherHeader is the SYSC rail: six slashes, the brand mark, six slashes.
//
// The mark is KindWordmark rather than text because the surface renders one
// face at one size -- a larger or different-faced header would mean threading
// a size through MeasureText, which every measure path in the layout engine
// would have to change. A tinted raster sidesteps all of it and re-colours
// with the theme, so the mark follows the palette like the rest of the chrome.
func launcherHeader() *ui.Node {
	slashes := func() *ui.Node {
		return &ui.Node{Kind: ui.KindText, Text: launcherSlashRun,
			TextRole: theme.RoleTitle, Tone: ui.ToneAccent}
	}
	return &ui.Node{
		Kind: ui.KindRow, Gap: theme.MarginM, CenterX: true,
		Children: []*ui.Node{
			slashes(),
			{
				Kind:   ui.KindWordmark,
				ImageW: render.WordmarkWidth(launcherMarkHeight),
				ImageH: launcherMarkHeight,
			},
			slashes(),
		},
	}
}

// launcherHeaderHeight is the rail's laid-out height: a row reports its
// tallest child, which is the mark unless the title face is taller.
//
// Measured rather than assumed. A constant here was five pixels out from what
// the row actually laid out, and the list inherited the error as a dead strip
// along the bottom of the panel.
func (h *PanelHost) launcherHeaderHeight() int {
	tall := launcherMarkHeight
	if measure := h.measureText(); measure != nil {
		if _, th := measure(launcherSlashRun, ui.TextAttrs{Role: theme.RoleTitle}); th > tall {
			tall = th
		}
	}
	return tall
}

// launcherFooter is the strip DMS calls its launcher footer -- "mode tabs and
// keyboard hints at the bottom". There are no mode tabs to show until a grid
// view exists, so it carries the result count and the key legend.
//
// The count earns its place: the browse list is 199 entries on this machine
// and used to arrive capped at 50, which read as a list that simply stopped.
// Saying how many there are makes that visible.
//
// RoleCaption rather than a muted colour. The palette deliberately has no
// muted text tone -- it measures 1.47:1 and cannot carry text -- so the
// footer steps back by size, not by contrast.
func launcherFooter(h *PanelHost, count int) *ui.Node {
	// Hint rows (empty ID) explain; they are not results.
	for _, res := range h.launcherResults {
		if res.Entry.ID == "" {
			count--
		}
	}
	noun := "result"
	if strings.TrimSpace(h.query) == "" {
		noun = "app"
	}
	if count != 1 {
		noun += "s"
	}
	text := fmt.Sprintf("%d %s \u2022 %s", count, noun, launcherHints)
	if count == 0 {
		text = "No results \u2022 " + launcherHints
	}
	return &ui.Node{Kind: ui.KindText, CenterX: true, TextRole: theme.RoleCaption, Text: text}
}

// launcherFooterHeight is the footer's laid-out height, measured for the same
// reason the rail's is: the list sizes from what is left, so a constant that
// disagrees with the laid-out chrome shows up as a dead strip.
func (h *PanelHost) launcherFooterHeight() int {
	measure := h.measureText()
	if measure == nil {
		return 0
	}
	_, th := measure(launcherHints, ui.TextAttrs{Role: theme.RoleCaption})
	return th
}

// launcherListHeight is the viewport the virtual list gets: the panel minus
// its padding, the rail, the field, the footer, and the gaps between them.
//
// Selection scrolling, page steps, and the list node all size from this one
// function. They were three copies of the same arithmetic, and an error label
// already made them disagree.
func (h *PanelHost) launcherListHeight() int {
	used := 2*theme.MarginL + h.launcherHeaderHeight() + theme.MarginM +
		launcherFieldHeight + theme.MarginM + h.launcherFooterHeight() + theme.MarginM
	if h.errLabel != "" {
		used += 24 + theme.MarginM
	}
	return max(h.place.Panel.H-used, launcherSlotHeight)
}

// launcherTree projects the current snapshot: the rail and one text field
// above a virtual list of result capsules. The selected row is a muted wash;
// every row carries the 40px glyph slot, bold name, and comment.
func launcherTree(r *Registry, h *PanelHost) *ui.Node {
	if h.search == nil {
		h.search = ui.NewField("")
	}
	field := h.search.Node("Search")
	field.Width = max(h.place.Panel.W-2*theme.MarginL, 0)
	field.Height = launcherFieldHeight
	field.Padding = theme.MarginM

	head := []*ui.Node{launcherHeader()}
	if h.errLabel != "" {
		head = append(head, &ui.Node{Kind: ui.KindText, Text: h.errLabel, Tone: ui.ToneError})
	}
	head = append(head, field)

	results := h.launcherResults
	if len(results) == 0 {
		head = append(head,
			&ui.Node{Kind: ui.KindText, Text: "No results"},
			launcherFooter(h, 0))
		return &ui.Node{Kind: ui.KindColumn, Gap: theme.MarginM, Padding: theme.MarginL, Children: head}
	}
	h.launcherSel = min(max(h.launcherSel, 0), len(results)-1)

	listH := h.launcherListHeight()
	list := &ui.Node{
		Kind:         ui.KindVirtualList,
		Height:       listH,
		ItemCount:    len(results),
		ItemHeight:   launcherSlotHeight,
		ScrollOffset: h.launcherVisibleOffset(len(results)),
		Item: func(i int) *ui.Node {
			return launcherRow(r, h, results, i)
		},
	}
	return &ui.Node{Kind: ui.KindColumn, Gap: theme.MarginM, Padding: theme.MarginL,
		Children: append(head, list, launcherFooter(h, len(results)))}
}

func launcherRow(r *Registry, h *PanelHost, results []launcher.Result, i int) *ui.Node {
	if i < 0 || i >= len(results) {
		return nil
	}
	res := results[i]
	if h.launcherMenuID == res.Entry.ID && res.Action == "" && h.menu != nil && h.menu.Opened() {
		return h.menu.Node()
	}
	fill := ui.FillNone
	if i == h.launcherSel && res.Entry.ID != "" {
		fill = ui.FillSoft
	}
	// The wrapper column carries half the gap at each end, so the space
	// between two capsules is launcherRowGap and the space inside one is the
	// row's own padding. The two are separate on purpose: the gap separates
	// rows from each other, the padding surrounds the text block.
	//
	// The capsule is taller than its padding plus its body by exactly
	// launcherRowPadBottom-launcherRowPadTop, and the column inside it is what
	// spends that slack. A column stacks from the top of the box it is given
	// and leaves what it does not use at the foot; a row in the same box would
	// centre its children and split the slack evenly, which is the symmetric
	// padding this is trying to get away from.
	return &ui.Node{
		Kind: ui.KindColumn, Padding: launcherRowGap / 2,
		Children: []*ui.Node{{
			Kind: ui.KindCapsule, Fill: fill, Shape: ui.ShapeMedium,
			Padding: launcherRowPadTop, Height: launcherRowHeight,
			// By index: a desktop-action row shares its application's ID.
			Action: "launch:" + strconv.Itoa(i),
			Children: []*ui.Node{{
				Kind:     ui.KindColumn,
				Children: []*ui.Node{launcherRowBody(r, h, res.Entry)},
			}},
		}},
	}
}

func launcherRowBody(r *Registry, h *PanelHost, e launcher.Entry) *ui.Node {
	tone := ui.ToneNormal
	if e.ID == "" {
		tone = ui.ToneSubtle // a hint, not a result
	}
	labels := []*ui.Node{{Kind: ui.KindText, Text: e.Name, TextRole: theme.RoleLabel, Tone: tone}}
	if e.Comment != "" {
		labels = append(labels, &ui.Node{Kind: ui.KindText, Text: e.Comment, Tone: tone})
	}
	// Panel pad 12×2, capsule pad launcherRowPadTop×2, glyph, gap. The row
	// itself is unpadded: its vertical inset is the capsule's, and a second
	// one here would put the text block back off the 12/16 figures.
	labelW := h.place.Panel.W - 2*theme.MarginL - 2*launcherRowPadTop - launcherIconSlot - theme.MarginL
	if labelW < 80 {
		labelW = 80
	}
	return &ui.Node{
		Kind: ui.KindRow, Gap: theme.MarginL,
		Children: []*ui.Node{
			launcherIconNode(r, h, e),
			{Kind: ui.KindColumn, Gap: theme.MarginXXS, Width: labelW, Children: labels},
		},
	}
}

func launcherIconNode(r *Registry, h *PanelHost, e launcher.Entry) *ui.Node {
	// Provider rows have no desktop icon. "glyph:<name>" draws a Material
	// subset glyph in the slot; "text:<s>" draws s itself (an emoji).
	if name, ok := strings.CutPrefix(e.IconName, "glyph:"); ok {
		return &ui.Node{
			Kind: ui.KindCapsule, Width: launcherIconSlot, Height: launcherIconSlot,
			Fill: ui.FillContainer, Shape: ui.ShapeMedium,
			Children: []*ui.Node{{Kind: ui.KindIcon, Icon: name, IconSize: launcherGlyphSize}},
		}
	}
	if text, ok := strings.CutPrefix(e.IconName, "text:"); ok {
		return &ui.Node{
			Kind: ui.KindCapsule, Width: launcherIconSlot, Height: launcherIconSlot,
			Fill: ui.FillNone, Shape: ui.ShapeMedium,
			Children: []*ui.Node{{Kind: ui.KindText, Text: text, TextRole: theme.RoleDisplay}},
		}
	}
	if img := launcherLookupIcon(r, h, e.IconName); img != nil {
		return &ui.Node{Kind: ui.KindImage, ImageSize: launcherIconSlot, Image: img}
	}
	// Height as well as width: the slot is a square whichever branch runs.
	// Without it the capsule sized to its one line of text, and an entry whose
	// icon missed the cache *and* carried no Comment had nothing left holding
	// the row open -- both children were short, so the row drew at about half
	// the height of its neighbours. .desktop files with no Comment are common
	// enough that this was visible as soon as a few were installed.
	return &ui.Node{
		Kind: ui.KindCapsule, Width: launcherIconSlot, Height: launcherIconSlot,
		Fill: ui.FillContainer, Shape: ui.ShapeMedium,
		Children: []*ui.Node{{Kind: ui.KindText, Text: launcherGlyph(e.Name), TextRole: theme.RoleTitle}},
	}
}

func launcherLookupIcon(r *Registry, h *PanelHost, name string) *ui.Image {
	if r == nil || r.trayIcons == nil || name == "" {
		return nil
	}
	size := launcherIconSlot
	if scale := ui.Scale120(h.scale120); scale.Valid() {
		size = max(scale.Physical(launcherIconSlot), 1)
	}
	key := icons.Square(name, size)
	if img, ok := r.trayIcons.Lookup(key); ok {
		return img
	}
	_, _, _ = r.trayIcons.Request(key)
	return nil
}

func launcherGlyph(name string) string {
	for _, r := range name {
		return string(unicode.ToUpper(r))
	}
	return "?"
}

// launcherVisibleOffset keeps the selected row inside the viewport across the
// rebuild every keystroke triggers; the wheel offset survives via
// h.launcherScroll until selection forces a correction.
func (h *PanelHost) launcherVisibleOffset(count int) int {
	viewH := h.launcherListHeight()
	maxOff := max(count*launcherSlotHeight-viewH, 0)
	off := min(max(h.launcherScroll, 0), maxOff)
	if top := h.launcherSel * launcherSlotHeight; top < off {
		off = top
	}
	if bottom := (h.launcherSel + 1) * launcherSlotHeight; bottom > off+viewH {
		off = bottom - viewH
	}
	h.launcherScroll = off
	return off
}

func (h *PanelHost) launcherKeyPress(r *Registry, key uint32) bool {
	n := len(h.launcherResults)
	page := max(h.launcherPageRows(), 1)
	switch key {
	case keyUp:
		h.launcherMoveSel(r, -1)
		return true
	case keyDown:
		h.launcherMoveSel(r, 1)
		return true
	case keyPageUp:
		h.launcherMoveSel(r, -page)
		return true
	case keyPageDown:
		h.launcherMoveSel(r, page)
		return true
	case keyHome:
		if n > 0 {
			h.launcherMoveSel(r, -h.launcherSel)
		}
		return true
	case keyEnd:
		if n > 0 {
			h.launcherMoveSel(r, n-1-h.launcherSel)
		}
		return true
	case keyEnter:
		h.launcherActivateSelected(r)
		return true
	}
	return false
}

func (h *PanelHost) launcherMoveSel(r *Registry, delta int) {
	n := len(h.launcherResults)
	if n == 0 {
		return
	}
	h.launcherAttempt++
	h.launcherSel = min(max(h.launcherSel+delta, 0), n-1)
	r.rebuildPanel(h)
}

func (h *PanelHost) launcherPageRows() int {
	return max(h.launcherListHeight()/launcherSlotHeight, 1)
}

// launcherActivateSelected activates the highlighted row. An overview row
// (no argv, prefix ID) navigates into that provider instead of spawning.
func (h *PanelHost) launcherActivateSelected(r *Registry) {
	if len(h.launcherResults) == 0 || h.launcherAwaiting {
		return
	}
	h.launcherSel = min(h.launcherSel, len(h.launcherResults)-1)
	res := h.launcherResults[h.launcherSel]
	if res.Entry.ID == "" {
		return // a hint row explains; it does not act
	}
	if len(res.Entry.Argv) == 0 && strings.HasPrefix(res.Entry.ID, "/") {
		h.errLabel = ""
		h.launcherAttempt++
		h.query = res.Entry.ID
		h.search = ui.NewField(h.query)
		h.launcherSel = 0
		r.launcherSendQuery(h, h.query)
		r.rebuildPanel(h)
		return
	}
	// A Notes row closes the panel (or shows its error) from its own action.
	notes := res.Entry.ID == notesLauncherActionID || res.Entry.ID == notesLauncherTooLongID
	h.launcherSpawn(r, res.Entry.ID, res.Action, !notes)
}

const (
	notesLauncherActionID  = "sysc-notes-launcher-action"
	notesLauncherTooLongID = "sysc-notes-launcher-too-long"
)

func notesLauncherResults(query string) ([]launcher.Result, bool) {
	query = strings.TrimSpace(query)
	if query != "/nt" && !strings.HasPrefix(query, "/nt ") {
		return nil, false
	}
	body := strings.TrimSpace(strings.TrimPrefix(query, "/nt"))
	id, name, comment := notesLauncherActionID, "Open Notes", "Search your Markdown library or capture a thought"
	if body != "" {
		name = "Capture note: " + launcherPreview(body, 72)
		comment = "Create a Markdown note in your configured Notes folder"
		if len(body) > v1.MaxInputBytes {
			id, name, comment = notesLauncherTooLongID, "Capture is too long", "Notes captures are limited to 1 MiB"
		}
	}
	return []launcher.Result{{Entry: launcher.Entry{ID: id, Name: name, Comment: comment}}}, true
}

func launcherPreview(value string, maxRunes int) string {
	runes := []rune(value)
	if len(runes) <= maxRunes {
		return string(runes)
	}
	return string(runes[:maxRunes]) + "…"
}

func (h *PanelHost) launcherNotesAction(r *Registry, action string) {
	if h.launcherAttempt != h.launcherPendingAttempt {
		return // the user moved on before this capture reached its provider
	}
	attempt := h.launcherAttempt
	if action == notesLauncherTooLongID {
		h.errLabel = "Capture is too long (maximum 1 MiB)"
		r.rebuildPanel(h)
		return
	}
	body := ""
	if strings.HasPrefix(strings.TrimSpace(h.query), "/nt ") {
		body = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(h.query), "/nt"))
	}
	var output string
	if bar := r.bars[h.output]; bar != nil {
		output = bar.connector()
	}
	generation := h.output
	plugins := r.plugins
	go func() {
		var err error
		if plugins == nil {
			err = errors.New("Notes plugin is not running")
		} else {
			err = plugins.launcherNotes(output, generation, body)
		}
		r.mu.Lock()
		defer r.mu.Unlock()
		host := r.panelHosts[PanelLauncher]
		if host == nil || host.launcherAttempt != attempt {
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

// launcherSpawn activates through the service off the Wayland goroutine: the
// panel shows the error in place on failure (D6) and, when closeOnSuccess,
// closes on success. A provider that manages the panel itself passes false.
func (h *PanelHost) launcherSpawn(r *Registry, id, action string, closeOnSuccess bool) {
	// A retry that succeeds must not leave the last attempt's error up.
	h.errLabel = ""
	h.launcherAttempt++
	attempt := h.launcherAttempt
	h.launcherPendingAttempt = attempt
	svc := r.launcherServiceLocked()
	go func() {
		err := svc.Activate(id, action)
		r.mu.Lock()
		defer r.mu.Unlock()
		host := r.panelHosts[PanelLauncher]
		if host == nil || host.launcherAttempt != attempt {
			return
		}
		if err != nil {
			host.errLabel = err.Error()
			r.rebuildPanel(host)
			r.publishSurface(host.output, panelSurfaceID(PanelLauncher))
			return
		}
		if !closeOnSuccess {
			// The provider may have changed the panel (a Notes error label).
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
	i, ok := launcherRowAt(h.root, x, y)
	if !ok || i >= len(h.launcherResults) {
		return false
	}
	res := h.launcherResults[i]
	h.launcherAttempt++
	if e.Button == btnRight {
		if res.Action != "" {
			return true // an action row has no actions menu of its own
		}
		return h.openLauncherActions(r, res.Entry.ID)
	}
	h.launcherSel = i
	h.launcherActivateSelected(r)
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
	h.launcherSpawn(r, id, action, true)
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

// launcherRowAt finds the index of the laid-out row under a point by its
// launch action.
func launcherRowAt(n *ui.Node, x, y int) (int, bool) {
	if n == nil {
		return 0, false
	}
	if v, ok := strings.CutPrefix(n.Action, "launch:"); ok && n.Bounds.Contains(x, y) {
		i, err := strconv.Atoi(v)
		return i, err == nil && i >= 0
	}
	for _, c := range n.Children {
		if i, ok := launcherRowAt(c, x, y); ok {
			return i, true
		}
	}
	return 0, false
}
