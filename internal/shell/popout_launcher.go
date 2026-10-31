package shell

import (
	"context"
	"errors"
	"fmt"
	"log"
	"math"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode"

	launcher "github.com/Nomadcxx/sysc-launch"
	"github.com/Nomadcxx/sysc-shell/internal/config"
	"github.com/Nomadcxx/sysc-shell/internal/icons"
	"github.com/Nomadcxx/sysc-shell/internal/platform/wayland"
	"github.com/Nomadcxx/sysc-shell/internal/render"
	"github.com/Nomadcxx/sysc-shell/internal/theme"
	"github.com/Nomadcxx/sysc-shell/internal/ui"
	v1 "github.com/Nomadcxx/sysc-shell/plugin/v1"
)

const launcherApplicationsRefreshAfter = time.Minute

type launcherApplicationSnapshot struct {
	entries     []launcher.Entry
	attemptedAt time.Time
	available   bool
}

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
	// launcherScrollbarClear is the space between the pills and the scrollbar
	// in the panel's padding. It began as a full row gap, which read as
	// detached from the list on the desktop.
	launcherScrollbarClear = 5
	launcherFieldHeight    = 56
	// ponytail: keep history session-local and capped at 32; disk storage needs an explicit privacy setting.
	launcherQueryHistoryLimit = 32
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

	// Compact drops the comment line, so the row needs only a name beside a
	// smaller icon: 8 + 28 + 8 with a 4 gap is a 48 slot against the list's 76.
	launcherCompactIcon = 28
	launcherCompactPad  = 8
	launcherCompactGap  = 4

	// The grid follows Noctalia's app grid: five columns, a 40 icon centred
	// over a caption of at most two lines, and an 8 gap between tiles.
	launcherGridColumns = 5
	launcherGridGap     = 8
	launcherGridPad     = 10
	// launcherGridHints is launcherHints with the arrows the grid adds.
	launcherGridHints = "↑↓←→ Navigate • Enter Select • Esc Close"
	// launcherActionsHints is the actions view's legend: Esc goes back to the
	// results instead of closing.
	launcherActionsHints = "↑↓ Navigate • Enter Select • Esc Back"
)

// launcherLayout is the geometry one rebuild uses for the result list. Rows,
// the scroll offset, paging and the wheel all size from it, so a display
// mode cannot leave one of them on the list's figures.
type launcherLayout struct {
	mode                   string
	iconSize               int
	padTop, padBottom, gap int
	slot                   int // height of one virtual-list item
	columns                int // results per item: 1 outside the grid
	cellWidth, tileHeight  int // grid only
}

// launcherLayoutFor applies the saved display only to a result set made
// entirely of applications. A calculator answer, a provider query or the
// overview keeps the list, as Noctalia keeps mixed results off its grid.
func launcherLayoutFor(r *Registry, h *PanelHost) launcherLayout {
	l := launcherLayout{
		mode: config.LauncherDisplayList, iconSize: launcherIconSlot,
		padTop: launcherRowPadTop, padBottom: launcherRowPadBottom, gap: launcherRowGap,
		slot: launcherSlotHeight, columns: 1,
	}
	if r == nil || h == nil || !h.launcherAppsOnly {
		return l
	}
	switch r.cfg.Launcher.Display {
	case config.LauncherDisplayCompact:
		l.mode, l.iconSize = config.LauncherDisplayCompact, launcherCompactIcon
		l.padTop, l.padBottom, l.gap = launcherCompactPad, launcherCompactPad, launcherCompactGap
		l.slot = l.padTop + l.iconSize + l.padBottom + l.gap
	case config.LauncherDisplayGrid:
		l.mode, l.columns, l.gap, l.padTop, l.padBottom = config.LauncherDisplayGrid, launcherGridColumns, launcherGridGap, launcherGridPad, launcherGridPad
		// Each visual row sits half a gap inside the list on every side, as
		// a list row does, and the tiles in it are a gap apart.
		rowWidth := h.place.Panel.W - 2*theme.MarginL - l.gap
		l.cellWidth = max((rowWidth-(l.columns-1)*l.gap)/l.columns, 1)
		l.tileHeight = 2*l.padTop + l.iconSize + theme.MarginS + 2*launcherCaptionLineHeight(h)
		l.slot = l.tileHeight + l.gap
	}
	return l
}

// launcherCaptionLineHeight is one line of a grid caption as the painter sets
// it, measured so a tall font grows the tile instead of clipping its name.
func launcherCaptionLineHeight(h *PanelHost) int {
	measure := h.measureText()
	if measure == nil {
		return 16
	}
	_, lh := measure("Ag", ui.TextAttrs{Role: theme.RoleCaption})
	return lh
}

// launcherServiceLocked returns the process-wide launcher service, creating
// it on first use. Lazy creation keeps registries that never open the
// launcher from paying for an XDG scan; the first open scans, and later opens
// hit the service's rescan-if-stale path (D12). Caller holds r.mu.
func (r *Registry) launcherServiceLocked() *launcher.Service {
	if r.launcherSvc != nil && (!sameLauncherCommandProviders(r.launcherServiceProviders, r.cfg.Launcher.Providers) ||
		!sameLauncherActions(r.launcherServiceActions, r.cfg.Launcher.Actions)) {
		stale := r.launcherSvc
		r.launcherSvc = nil
		r.shutdown.launcher.store(nil)
		r.launcherServiceProviders = nil
		r.launcherServiceActions = nil
		r.launcherMu.Lock()
		r.launcherProviderGeneration++
		r.launcherMu.Unlock()
		if r.launcherServiceCloseDone == nil {
			r.launcherServiceCloseDone = make(map[*launcher.Service]chan struct{})
		}
		closed := make(chan struct{})
		r.launcherServiceCloseDone[stale] = closed
		go func() {
			stale.Close()
			close(closed)
			r.mu.Lock()
			delete(r.launcherServiceCloseDone, stale)
			r.mu.Unlock()
		}()
	}
	if r.launcherSvc == nil {
		cfg := r.launcherServiceConfig()
		cfg.History = launcher.OpenHistory(launcherHistoryPath(os.Getenv), nil)
		r.launcherSvc = launcher.NewService(cfg)
		r.shutdown.launcher.store(r.launcherSvc)
		r.launcherServiceProviders = cloneLauncherCommandProviders(r.cfg.Launcher.Providers)
		r.launcherServiceActions = cloneLauncherActions(r.cfg.Launcher.Actions)
		if !r.launcherRelayStarted {
			r.launcherRelayStarted = true
			go r.relayLauncher(r.launcherSvc)
		}
	}
	return r.launcherSvc
}

// refreshLauncherApplicationsLocked requests a fresh scan away from the
// Wayland owner. Settings renders the last immutable copy immediately, or a
// loading state until the first reply arrives.
func (r *Registry) refreshLauncherApplicationsLocked(h *PanelHost) {
	current := r.launcherApplications.Load()
	if r.launcherAppsRefreshing || current != nil && time.Since(current.attemptedAt) < launcherApplicationsRefreshAfter {
		return
	}
	svc := r.launcherServiceLocked()
	r.launcherAppsRefreshing = true
	go func() {
		entries, err := svc.RefreshApplications()
		now := time.Now()

		r.mu.Lock()
		r.launcherAppsRefreshing = false
		previous := r.launcherApplications.Load()
		if err == nil {
			r.launcherApplications.Store(&launcherApplicationSnapshot{entries: entries, attemptedAt: now, available: true})
		} else if r.launcherSvc != svc {
			// A retired service's error is not a scan result; let Settings retry
			// against the replacement service when it rebuilds.
		} else if previous != nil {
			r.launcherApplications.Store(&launcherApplicationSnapshot{
				entries: previous.entries, attemptedAt: now, available: previous.available,
			})
		} else {
			r.launcherApplications.Store(&launcherApplicationSnapshot{attemptedAt: now})
		}
		currentHost := r.panelHosts[PanelSettings]
		visible := currentHost != nil && currentHost.section == "Applications"
		var output uint32
		if visible {
			r.rebuildPanel(currentHost)
			output = currentHost.output
		}
		r.mu.Unlock()

		if visible {
			r.publishSurface(output, panelSurfaceID(PanelSettings))
		}
	}()
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
	// appsOnly is true when every row came from the applications provider,
	// the one result set the compact and grid displays apply to.
	appsOnly bool
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
	kind            launcherKind
	passed          string
	provider        string
	inlineProviders []string
}

// launcherBatch accumulates inline provider and main result callbacks into the
// single snapshot the panel applies for a query.
type launcherBatch struct {
	gen      uint64
	haveMain bool
	emitted  bool
	inline   map[string][]launcher.Result
	rows     []launcher.Result
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
				// Rows for the current query belong to the results the
				// actions view hides, so Esc shows them rather than older ones.
				rows := launcherWithHints(h.query, snap.rows)
				if back := h.launcherActionsBack; back != nil {
					back.results, back.appsOnly = rows, snap.appsOnly
				} else {
					h.launcherResults, h.launcherAppsOnly = rows, snap.appsOnly
				}
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

// rankLauncher is the service rank hook. It stamps rows with the service
// generation and applies the current preference snapshot, so a replaced
// service or a stale preference snapshot cannot publish outdated rows.
func (r *Registry) rankLauncher(providerGeneration uint64, entries []launcher.Entry, query string, boost func(query, identifier string) int) []launcher.Result {
	prefs := r.launcherAppPrefs.Load()
	rows := launcherRankWithPreferences(entries, query, boost, prefs)
	r.launcherMu.Lock()
	wait := r.launcherRankWait
	r.launcherMu.Unlock()
	if wait != nil {
		wait(query)
	}
	r.launcherMu.Lock()
	defer r.launcherMu.Unlock()
	// A repeated query can finish after a preference refresh has queued the
	// same text again. Do not stamp rows ranked from the previous snapshot as
	// current while holding the same lock used to advance query generations.
	if r.launcherAppPrefs.Load() != prefs || providerGeneration != r.launcherProviderGeneration {
		return rows
	}
	r.noteLauncherRowsLocked(launcherAppsPrefix, query, rows)
	return rows
}

// launcherSendQuery records the field's generation and submits text. Caller
// holds r.mu. A snapshot from an earlier generation no longer matches, so it
// cannot clear launcherAwaiting.
func (r *Registry) launcherSendQuery(h *PanelHost, text string) {
	svc := r.launcherServiceLocked()
	plan := classifyLauncherQuery(text, r.launcherProviders())
	r.launcherMu.Lock()
	r.launcherGen++
	gen := r.launcherGen
	r.launcherPlan = plan
	r.launcherMu.Unlock()

	h.launcherQueryGen = gen
	h.launcherAwaiting = true
	// The actions view belongs to the result set that opened it. A new query
	// puts those rows back until its own snapshot replaces them, so a delayed
	// result cannot leave an old desktop action live. The caller sets the
	// selection.
	if back := h.launcherActionsBack; back != nil {
		h.launcherResults, h.launcherAppsOnly = back.results, back.appsOnly
		h.launcherActionsBack = nil
	}
	if plan.kind == launcherKindOverview {
		h.launcherResults = r.launcherOverviewRows(plan.passed)
		h.launcherAppsOnly = false
		h.launcherSel = 0
		h.launcherAwaiting = false
	}
	svc.Query(text)
}

// refreshOpenLauncherLocked re-ranks the current query after application
// preferences change. Caller holds Registry.mu; its bool reports whether a
// launcher surface needs invalidation after the caller unlocks.
func (r *Registry) refreshOpenLauncherLocked() (uint32, bool) {
	h := r.panelHosts[PanelLauncher]
	if h == nil {
		return 0, false
	}
	h.launcherSel = 0
	h.launcherScroll = 0
	r.launcherSendQuery(h, h.query)
	r.rebuildPanel(h)
	return h.output, true
}

func classifyLauncherQuery(text string, providers []launcher.Provider) launcherPlan {
	text = strings.TrimSpace(text)
	if text == "" {
		return launcherPlan{kind: launcherKindApps, provider: launcherAppsPrefix}
	}
	if !strings.HasPrefix(text, "/") {
		var inlineProviders []string
		for _, provider := range providers {
			if provider.Inline {
				inlineProviders = append(inlineProviders, provider.Prefix)
			}
		}
		if len(inlineProviders) > 0 {
			return launcherPlan{kind: launcherKindInline, passed: text,
				provider: launcherAppsPrefix, inlineProviders: inlineProviders}
		}
		return launcherPlan{kind: launcherKindApps, passed: text, provider: launcherAppsPrefix}
	}
	seg, rest, _ := strings.Cut(text[1:], " ")
	prefix := "/" + seg
	passed := strings.TrimSpace(rest)
	if prefix == launcherAppsPrefix {
		return launcherPlan{kind: launcherKindApps, passed: passed, provider: launcherAppsPrefix}
	}
	for _, provider := range providers {
		if prefix == provider.Prefix {
			return launcherPlan{kind: launcherKindProvider, passed: passed, provider: provider.Prefix}
		}
	}
	return launcherPlan{kind: launcherKindOverview, passed: seg}
}

// noteLauncherRows records one service callback. The callback's query has to
// match the plan for the current generation; a rank that started for the
// previous text is ignored, and so is a snapshot emitted before the new
// generation was issued.
func (r *Registry) noteLauncherRowsForGeneration(generation uint64, provider, query string, rows []launcher.Result) {
	r.launcherMu.Lock()
	defer r.launcherMu.Unlock()
	if generation != r.launcherProviderGeneration {
		return
	}
	r.noteLauncherRowsLocked(provider, query, rows)
}

// noteLauncherRowsLocked is noteLauncherRows for a caller that already holds
// launcherMu, as application ranking must check its preference snapshot and
// stamp rows atomically with respect to query generation changes.
func (r *Registry) noteLauncherRowsLocked(provider, query string, rows []launcher.Result) {
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
		if provider != launcherAppsPrefix && !slices.Contains(plan.inlineProviders, provider) {
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
	if b.gen != r.launcherGen || b.emitted {
		*b = launcherBatch{gen: r.launcherGen}
	}
	cloned := append([]launcher.Result(nil), rows...)
	isInline := plan.kind == launcherKindInline && slices.Contains(plan.inlineProviders, provider)
	if isInline {
		if b.inline == nil {
			b.inline = make(map[string][]launcher.Result, len(plan.inlineProviders))
		}
		b.inline[provider] = cloned
	} else {
		b.rows = cloned
		b.haveMain = true
	}
	if !b.haveMain {
		return
	}
	out := b.rows
	appsOnly := plan.kind == launcherKindApps
	if plan.kind == launcherKindInline {
		if len(b.inline) != len(plan.inlineProviders) {
			return
		}
		out = nil
		for _, prefix := range plan.inlineProviders {
			out = append(out, b.inline[prefix]...)
		}
		appsOnly = len(out) == 0
		out = append(out, b.rows...)
	}
	b.emitted = true
	r.emitLauncherSnapLocked(launcherSnap{gen: r.launcherGen, rows: out, appsOnly: appsOnly})
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
	items := []item{{launcherAppsPrefix, "Applications", "Installed desktop applications", "glyph:apps"}}
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

func (r *Registry) launcherOverviewPrefix(id string) bool {
	if id == launcherAppsPrefix {
		return true
	}
	for _, provider := range r.launcherProviders() {
		if provider.Prefix == id {
			return true
		}
	}
	return false
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
func launcherFooter(h *PanelHost, count int, l launcherLayout) *ui.Node {
	if h.launcherActionsBack != nil && len(h.launcherResults) > 0 {
		// The view's first row is the application whose actions it shows.
		text := fmt.Sprintf(h.tr("launcher.actions_for", "%s actions"), h.launcherResults[0].Entry.Name) +
			" • " + launcherActionsHints
		return &ui.Node{Kind: ui.KindText, CenterX: true, TextRole: theme.RoleCaption, Text: text}
	}
	// Hint rows (empty ID) explain; they are not results.
	for _, res := range h.launcherResults {
		if res.Entry.ID == "" {
			count--
		}
	}
	hints := launcherHints
	if l.mode == config.LauncherDisplayGrid && count > 0 {
		hints = launcherGridHints
	}
	nounKey, one, other := "launcher.noun.result", "result", "results"
	if strings.TrimSpace(h.query) == "" {
		nounKey, one, other = "launcher.noun.app", "app", "apps"
	}
	// ponytail: launcherHints stays English; sysc-greet prints it verbatim.
	text := fmt.Sprintf("%d %s \u2022 %s", count, h.trn(nounKey, count, one, other), hints)
	if count == 0 {
		text = h.tr("launcher.no_results", "No results") + " \u2022 " + hints
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
	field := h.search.Node(h.tr("common.search", "Search"))
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
			launcherFooter(h, 0, launcherLayoutFor(r, h)))
		return &ui.Node{Kind: ui.KindColumn, Gap: theme.MarginM, Padding: theme.MarginL, Children: head}
	}
	h.launcherSel = min(max(h.launcherSel, 0), len(results)-1)

	l := launcherLayoutFor(r, h)
	listH := h.launcherListHeight()
	list := &ui.Node{
		Kind:         ui.KindVirtualList,
		Height:       listH,
		ItemCount:    (len(results) + l.columns - 1) / l.columns,
		ItemHeight:   l.slot,
		ScrollOffset: h.launcherVisibleOffset(len(results), l),
		// Each pill sits half a gap inside the list on every side, so the bar
		// starts the rest of launcherScrollbarClear beyond the list's right
		// edge, and its ends stop level with the first and last pill instead
		// of past them.
		ScrollbarGap:  launcherScrollbarClear - l.gap/2,
		ScrollbarEnds: l.gap / 2,
		Item: func(i int) *ui.Node {
			if l.mode == config.LauncherDisplayGrid {
				return launcherGridRow(r, h, results, i, l)
			}
			return launcherRow(r, h, results, i, l)
		},
	}
	return &ui.Node{Kind: ui.KindColumn, Gap: theme.MarginM, Padding: theme.MarginL,
		Children: append(head, list, launcherFooter(h, len(results), l))}
}

func launcherRow(r *Registry, h *PanelHost, results []launcher.Result, i int, l launcherLayout) *ui.Node {
	if i < 0 || i >= len(results) {
		return nil
	}
	res := results[i]
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
		Kind: ui.KindColumn, Padding: l.gap / 2,
		Children: []*ui.Node{{
			Kind: ui.KindCapsule, Fill: fill, Shape: ui.ShapeMedium,
			Padding: l.padTop, Height: l.padTop + l.iconSize + l.padBottom,
			// By index: a desktop-action row shares its application's ID.
			Action: "launch:" + strconv.Itoa(i),
			Children: []*ui.Node{{
				Kind:     ui.KindColumn,
				Children: []*ui.Node{launcherRowBody(r, h, res.Entry, l)},
			}},
		}},
	}
}

// launcherGridRow is one virtual-list item of the grid: up to l.columns
// tiles.
func launcherGridRow(r *Registry, h *PanelHost, results []launcher.Result, row int, l launcherLayout) *ui.Node {
	start := row * l.columns
	if row < 0 || start >= len(results) {
		return nil
	}
	end := min(start+l.columns, len(results))
	tiles := make([]*ui.Node, 0, end-start)
	for i := start; i < end; i++ {
		tiles = append(tiles, launcherGridTile(r, h, results[i], i, l))
	}
	return &ui.Node{Kind: ui.KindColumn, Padding: l.gap / 2, Children: []*ui.Node{{
		Kind: ui.KindRow, Gap: l.gap, Height: l.tileHeight, CenterY: true, Children: tiles,
	}}}
}

// launcherGridTile is one application: the icon centred a pad below the
// tile's top, and the name centred under it on at most two lines.
func launcherGridTile(r *Registry, h *PanelHost, res launcher.Result, i int, l launcherLayout) *ui.Node {
	fill := ui.FillNone
	if i == h.launcherSel && res.Entry.ID != "" {
		fill = ui.FillSoft
	}
	inner := max(l.cellWidth-2*l.padTop, 1)
	icon := launcherIconNodeSized(r, h, res.Entry, l.iconSize, true)
	icon.CenterX = true
	content := &ui.Node{Kind: ui.KindColumn, Gap: theme.MarginS, Width: inner, Children: []*ui.Node{icon}}
	if measure := h.measureText(); measure != nil {
		attrs := ui.TextAttrs{Role: theme.RoleCaption}
		lines := wrapLines(res.Entry.Name, inner, func(s string) int { w, _ := measure(s, attrs); return w }, 2)
		for _, line := range lines {
			content.Children = append(content.Children, &ui.Node{
				Kind: ui.KindText, Text: line, TextRole: theme.RoleCaption, CenterX: true, MaxWidth: inner,
			})
		}
	}
	return &ui.Node{
		Kind: ui.KindCapsule, Fill: fill, Shape: ui.ShapeMedium,
		Width: l.cellWidth, Height: l.tileHeight, Padding: l.padTop,
		// By index, as a list row: launcherRowAt hit-tests the tile unchanged.
		Action:   "launch:" + strconv.Itoa(i),
		Children: []*ui.Node{content},
	}
}

func launcherRowBody(r *Registry, h *PanelHost, e launcher.Entry, l launcherLayout) *ui.Node {
	tone := ui.ToneNormal
	if e.ID == "" {
		tone = ui.ToneSubtle // a hint, not a result
	}
	// Panel pad 12×2, capsule pad ×2, glyph, gap. The row itself is unpadded:
	// its vertical inset is the capsule's, and a second one here would put the
	// text block back off the 12/16 figures.
	labelW := max(h.place.Panel.W-2*theme.MarginL-2*l.padTop-l.iconSize-theme.MarginL, 80)
	name := &ui.Node{Kind: ui.KindText, Text: e.Name, TextRole: theme.RoleLabel, Tone: tone}
	block := &ui.Node{Kind: ui.KindColumn, Gap: theme.MarginXXS, Width: labelW, Children: []*ui.Node{name}}
	if l.mode == config.LauncherDisplayCompact {
		// Compact is the name alone, on one line. Left unpinned, the row
		// centres it on the smaller icon.
		name.MaxWidth = labelW
		return &ui.Node{Kind: ui.KindRow, Gap: theme.MarginM, Children: []*ui.Node{launcherIconNodeSized(r, h, e, l.iconSize, false), block}}
	}
	if e.Comment != "" {
		block.Children = append(block.Children, &ui.Node{Kind: ui.KindText, Text: e.Comment, Tone: tone})
	}
	// Height pins the label block to the icon slot: two labels must not
	// outgrow the row and eat the capsule's 16 foot on a tall font.
	block.Height = l.iconSize
	return &ui.Node{
		Kind: ui.KindRow, Gap: theme.MarginL,
		Children: []*ui.Node{launcherIconNodeSized(r, h, e, l.iconSize, false), block},
	}
}

func launcherIconNode(r *Registry, h *PanelHost, e launcher.Entry) *ui.Node {
	return launcherIconNodeSized(r, h, e, launcherIconSlot, false)
}

// launcherIconNodeSized draws the icon slot at size: the list's 40, compact's
// 28. A provider glyph keeps its 24 until the slot is too small for it. Grid
// centers a fallback glyph in its column; list keeps the legacy capsule child.
func launcherIconNodeSized(r *Registry, h *PanelHost, e launcher.Entry, size int, centerFallbackGlyph bool) *ui.Node {
	// Provider rows have no desktop icon. "glyph:<name>" draws a Material
	// subset glyph in the slot; "text:<s>" draws s itself (an emoji).
	if name, ok := strings.CutPrefix(e.IconName, "glyph:"); ok {
		return &ui.Node{
			Kind: ui.KindCapsule, Width: size, Height: size,
			Fill: ui.FillContainer, Shape: ui.ShapeMedium,
			Children: []*ui.Node{{Kind: ui.KindIcon, Icon: name, IconSize: min(launcherGlyphSize, size-12)}},
		}
	}
	if text, ok := strings.CutPrefix(e.IconName, "text:"); ok {
		return &ui.Node{
			Kind: ui.KindCapsule, Width: size, Height: size,
			Fill: ui.FillNone, Shape: ui.ShapeMedium,
			Children: []*ui.Node{{Kind: ui.KindText, Text: text, TextRole: theme.RoleDisplay}},
		}
	}
	if img := launcherLookupIcon(r, h, e.IconName, size); img != nil {
		return &ui.Node{Kind: ui.KindImage, ImageSize: size, Image: img}
	}
	// Height as well as width: the slot is a square whichever branch runs.
	// Without it the capsule sized to its one line of text, and an entry whose
	// icon missed the cache *and* carried no Comment had nothing left holding
	// the row open -- both children were short, so the row drew at about half
	// the height of its neighbours. .desktop files with no Comment are common
	// enough that this was visible as soon as a few were installed.
	glyph := &ui.Node{Kind: ui.KindText, Text: launcherGlyph(e.Name), TextRole: theme.RoleTitle}
	if centerFallbackGlyph {
		glyph.CenterX, glyph.CenterY = true, true
		glyph = &ui.Node{Kind: ui.KindColumn, Children: []*ui.Node{glyph}}
	}
	return &ui.Node{
		Kind: ui.KindCapsule, Width: size, Height: size,
		Fill: ui.FillContainer, Shape: ui.ShapeMedium,
		Children: []*ui.Node{glyph},
	}
}

func launcherLookupIcon(r *Registry, h *PanelHost, name string, size int) *ui.Image {
	if r == nil || r.trayIcons == nil || name == "" {
		return nil
	}
	if scale := ui.Scale120(h.scale120); scale.Valid() {
		size = max(scale.Physical(size), 1)
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
// The offset is in visual rows, which hold l.columns results each.
func (h *PanelHost) launcherVisibleOffset(count int, l launcherLayout) int {
	viewH := h.launcherListHeight()
	rows, sel := (count+l.columns-1)/l.columns, h.launcherSel/l.columns
	maxOff := max(rows*l.slot-viewH, 0)
	off := min(max(h.launcherScroll, 0), maxOff)
	if top := sel * l.slot; top < off {
		off = top
	}
	if bottom := (sel + 1) * l.slot; bottom > off+viewH {
		off = bottom - viewH
	}
	h.launcherScroll = off
	return off
}

func (h *PanelHost) launcherKeyPress(r *Registry, key uint32) bool {
	n := len(h.launcherResults)
	l := launcherLayoutFor(r, h)
	grid := l.mode == config.LauncherDisplayGrid && n > 0
	page := max(h.launcherPageItems(l), 1)
	// The actions view is a list of one application's rows: the arrows stay
	// in it rather than stepping into query history.
	history := h.launcherActionsBack == nil
	switch key {
	case keyEsc:
		return h.launcherCloseActions(r)
	case keyUp:
		// In the grid, Up climbs rows first and reaches query history only
		// from the top row, where the list always reaches it.
		if history && (!grid || h.launcherSel < l.columns) && h.launcherHistoryMove(r, -1) {
			return true
		}
		h.launcherMoveSel(r, -l.columns)
		return true
	case keyDown:
		if history && h.launcherHistoryMove(r, 1) {
			return true
		}
		h.launcherMoveSel(r, l.columns)
		return true
	case keyLeft, keyRight:
		// Only the grid takes the horizontal arrows; elsewhere they move the
		// field's caret.
		if !grid {
			return false
		}
		if key == keyLeft {
			h.launcherMoveSel(r, -1)
		} else {
			h.launcherMoveSel(r, 1)
		}
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

func (h *PanelHost) launcherHistoryMove(r *Registry, direction int) bool {
	history := r.launcherQueryHistory
	if !h.launcherHistoryBrowsing {
		if direction >= 0 || strings.TrimSpace(h.query) != "" || len(history) == 0 {
			return false
		}
		h.launcherHistoryBrowsing = true
		h.launcherHistoryCursor = len(history)
		h.launcherHistoryDraft = h.query
	}

	if direction < 0 {
		if h.launcherHistoryCursor > 0 {
			h.launcherHistoryCursor--
		}
	} else if h.launcherHistoryCursor >= len(history)-1 {
		query := h.launcherHistoryDraft
		h.launcherHistoryBrowsing = false
		h.launcherHistoryDraft = ""
		h.launcherSetHistoryQuery(r, query)
		return true
	} else {
		h.launcherHistoryCursor++
	}
	h.launcherSetHistoryQuery(r, history[h.launcherHistoryCursor])
	return true
}

func (h *PanelHost) launcherSetHistoryQuery(r *Registry, query string) {
	h.query = query
	h.search = ui.NewField(query)
	h.errLabel = ""
	h.launcherAttempt++
	h.launcherSel = 0
	h.launcherScroll = 0
	r.launcherSendQuery(h, query)
	r.rebuildPanel(h)
}

func (r *Registry) rememberLauncherQueryLocked(h *PanelHost) {
	if h == nil || h.launcherHistoryBrowsing || strings.TrimSpace(h.query) == "" {
		return
	}
	r.launcherQueryHistory = append(r.launcherQueryHistory, h.query)
	if len(r.launcherQueryHistory) > launcherQueryHistoryLimit {
		r.launcherQueryHistory = append([]string(nil), r.launcherQueryHistory[len(r.launcherQueryHistory)-launcherQueryHistoryLimit:]...)
	}
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

// launcherPageItems is how many results one page step moves: the visual rows
// that fit, times the results in each row.
func (h *PanelHost) launcherPageItems(l launcherLayout) int {
	return max(h.launcherListHeight()/l.slot, 1) * l.columns
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
	if len(res.Entry.Argv) == 0 && r.launcherOverviewPrefix(res.Entry.ID) {
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
	// Session actions keep the launcher open until runSessionAction publishes
	// its asynchronous success or error result.
	session := sessionArgv(res.Entry.ID, "") != nil
	h.launcherSpawn(r, res.Entry.ID, res.Action, !notes && !session)
}

const (
	notesLauncherActionID  = "sysc-notes-launcher-action"
	notesLauncherTooLongID = "sysc-notes-launcher-too-long"
)

func notesLauncherResults(query string, tr translateFunc) ([]launcher.Result, bool) {
	query = strings.TrimSpace(query)
	if query != "/nt" && !strings.HasPrefix(query, "/nt ") {
		return nil, false
	}
	body := strings.TrimSpace(strings.TrimPrefix(query, "/nt"))
	id, name, comment := notesLauncherActionID, cardT(tr, "launcher.notes.name", "Open Notes"), cardT(tr, "launcher.notes.comment", "Search your Markdown library or capture a thought")
	if body != "" {
		name = cardT(tr, "launcher.notes.capture", "Capture note") + ": " + launcherPreview(body, 72)
		comment = cardT(tr, "launcher.notes.capture_comment", "Create a Markdown note in your configured Notes folder")
		if len(body) > v1.MaxInputBytes {
			id, name, comment = notesLauncherTooLongID, cardT(tr, "launcher.notes.toolong", "Capture is too long"), cardT(tr, "launcher.notes.toolong_comment", "Notes captures are limited to 1 MiB")
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
		h.errLabel = h.tr("launcher.notes.toolong_max", "Capture is too long (maximum 1 MiB)")
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
	serial := h.inputSerial
	svc := r.launcherServiceLocked()
	go func() {
		err := r.activateLauncherService(h, svc, attempt, id, action, serial)
		r.mu.Lock()
		host := r.panelHosts[PanelLauncher]
		if host == nil || host.launcherAttempt != attempt {
			r.mu.Unlock()
			return
		}
		if err != nil {
			host.errLabel = err.Error()
			r.rebuildPanel(host)
			r.publishSurface(host.output, panelSurfaceID(PanelLauncher))
			r.mu.Unlock()
			return
		}
		if !closeOnSuccess {
			// The provider may have changed the panel (a Notes error label).
			r.publishSurface(host.output, panelSurfaceID(PanelLauncher))
			r.mu.Unlock()
			return
		}
		pasteClipboard := isLauncherClipboardResult(id)
		output := host.output
		if pasteClipboard {
			owner, generation, ok := r.roots.current()
			if ok && owner == panelGroupRoot() {
				r.roots.closeRoot(generation)
			} else {
				r.closePanelLocked(PanelLauncher)
			}
		} else {
			r.closePanelLocked(PanelLauncher)
		}
		r.mu.Unlock()
		if pasteClipboard {
			ctx, cancel := context.WithTimeout(context.Background(), launcherClipboardBarrierTimeout)
			err := r.sendAuxWait(ctx, wayland.AuxRequest{Output: output, Barrier: true})
			cancel()
			if err != nil {
				log.Printf("launcher: wait for panel close before paste: %v", err)
				return
			}
			r.runClipboardPaste()
		}
	}()
}

const launcherClipboardBarrierTimeout = 2 * time.Second

func isLauncherClipboardResult(id string) bool {
	return strings.HasPrefix(id, launcherClipboardResultPrefix)
}

func (r *Registry) runClipboardPaste() {
	select {
	case <-r.closed:
		return
	default:
	}
	r.mu.Lock()
	_, _, rootOpen := r.roots.current()
	run := r.runArgv
	r.mu.Unlock()
	if rootOpen || run == nil {
		return
	}
	if err := run([]string{"wtype", "-M", "ctrl", "-k", "v", "-m", "ctrl"}); err != nil {
		log.Printf("launcher: paste clipboard history: %v", err)
	}
}

func (r *Registry) activateLauncherService(h *PanelHost, svc *launcher.Service, attempt uint64, id, action string, serial uint32) error {
	r.launcherActivationMu.Lock()
	defer r.launcherActivationMu.Unlock()
	r.mu.Lock()
	h.launcherPendingAttempt = attempt
	r.launcherActivationSerial = serial
	r.mu.Unlock()

	err := svc.Activate(id, action)
	r.mu.Lock()
	r.launcherActivationSerial = 0
	r.mu.Unlock()
	if errors.Is(err, launcher.ErrServiceClosed) {
		r.mu.Lock()
		closed := r.launcherServiceCloseDone[svc]
		r.mu.Unlock()
		if closed != nil {
			<-closed
		}
	}
	return err
}

// launcherPointerPress handles row clicks. A left press launches immediately
// (Noctalia's single-click); a right press opens the row's desktop actions
// view. Rows are deliberately not focusable, so the field keeps keyboard
// focus and typing always filters.
func (h *PanelHost) launcherPointerPress(r *Registry, e wayland.Event) bool {
	if h.launcherAwaiting {
		return true
	}
	x, y := int(math.Floor(e.X)), int(math.Floor(e.Y))
	i, ok := launcherRowAt(h.root, x, y)
	if !ok || i >= len(h.launcherResults) {
		return false
	}
	h.launcherAttempt++
	if e.Button == btnRight {
		return h.openLauncherActions(r, i)
	}
	h.launcherSel = i
	h.launcherActivateSelected(r)
	return true
}

// launcherSavedResults is the result list the actions view replaced.
type launcherSavedResults struct {
	results     []launcher.Result
	appsOnly    bool
	sel, scroll int
}

// openLauncherActions replaces the results with application i's desktop
// actions, drawn as ordinary list rows: the application's own row, then one
// row per action, which launcherActivateSelected runs through res.Action.
// An application without actions, an action row, or a row inside the view
// opens nothing.
func (h *PanelHost) openLauncherActions(r *Registry, i int) bool {
	if h.launcherAwaiting || h.launcherActionsBack != nil || i < 0 || i >= len(h.launcherResults) {
		return true
	}
	res := h.launcherResults[i]
	app := res.Entry
	if res.Action != "" || len(app.Actions) == 0 {
		return true
	}
	rows := make([]launcher.Result, 0, 1+len(app.Actions))
	rows = append(rows, launcher.Result{Entry: app})
	for _, a := range app.Actions {
		row := app
		row.Name = a.Name
		row.Comment = app.Name + " · " + h.tr("launcher.desktop_action", "desktop action")
		if a.IconName != "" {
			row.IconName = a.IconName
		}
		rows = append(rows, launcher.Result{Entry: row, Action: a.ID})
	}
	h.launcherActionsBack = &launcherSavedResults{
		results: h.launcherResults, appsOnly: h.launcherAppsOnly,
		sel: h.launcherSel, scroll: h.launcherScroll,
	}
	// Not applications only, so every display mode lays the view out as a list.
	h.launcherResults, h.launcherAppsOnly = rows, false
	h.launcherSel, h.launcherScroll = 1, 0
	r.rebuildPanel(h)
	return true
}

// launcherCloseActions puts back the results, selection and scroll the
// actions view replaced. It reports false when no view is showing, so Esc
// closes the launcher as usual.
func (h *PanelHost) launcherCloseActions(r *Registry) bool {
	back := h.launcherActionsBack
	if back == nil {
		return false
	}
	h.launcherActionsBack = nil
	h.launcherResults, h.launcherAppsOnly = back.results, back.appsOnly
	h.launcherSel, h.launcherScroll = back.sel, back.scroll
	h.launcherAttempt++
	r.rebuildPanel(h)
	return true
}

// launcherRowAt finds the index of the laid-out row under a point by its
// launch action. A row scrolled partly out of the list keeps its full bounds
// but is clipped to the list, so a point outside the list is never a row: the
// field above it would otherwise launch whatever is scrolled under it.
func launcherRowAt(n *ui.Node, x, y int) (int, bool) {
	if n == nil || n.Kind == ui.KindVirtualList && !n.Bounds.Contains(x, y) {
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
