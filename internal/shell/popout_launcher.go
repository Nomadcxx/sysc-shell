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
	launcherSlashRun         = "//////"
	launcherGridGap          = 8
	launcherGridMinCellWidth = 136
)

type launcherViewLayout struct {
	grid           bool
	compact        bool
	iconSize       int
	rowPadTop      int
	rowPadBottom   int
	rowGap         int
	tilePadding    int
	tileContentGap int
	tileHeight     int
	columns        int
	cellWidth      int
	itemHeight     int
}

func launcherGridColumns(width int) int {
	return max((max(width, 1)+launcherGridGap)/(launcherGridMinCellWidth+launcherGridGap), 1)
}

func launcherLayoutFor(r *Registry, h *PanelHost) launcherViewLayout {
	layout := launcherViewLayout{
		iconSize: launcherIconSlot, rowPadTop: launcherRowPadTop,
		rowPadBottom: launcherRowPadBottom, rowGap: launcherRowGap,
		columns: 1, itemHeight: launcherSlotHeight,
	}
	if r.launcherQueryKind(h) != launcherKindApps {
		return layout
	}
	layout.compact = h.launcherCompact
	if layout.compact {
		layout.iconSize = 32
		layout.rowPadTop, layout.rowPadBottom, layout.rowGap = 8, 10, 6
		layout.tilePadding, layout.tileContentGap = theme.MarginS, theme.MarginXS
	} else {
		layout.tilePadding, layout.tileContentGap = theme.MarginM, theme.MarginS
	}
	if !h.launcherGrid {
		layout.itemHeight = layout.rowPadTop + layout.iconSize + layout.rowPadBottom + layout.rowGap
		return layout
	}
	layout.grid = true
	layout.tileHeight = 2*layout.tilePadding + layout.iconSize + layout.tileContentGap
	if _, labelHeight := h.measureText()("Ag", ui.TextAttrs{Role: theme.RoleLabel}); labelHeight > 0 {
		layout.tileHeight += labelHeight
	}
	width := max(h.place.Panel.W-2*theme.MarginL-launcherGridGap, 1)
	layout.columns = launcherGridColumns(width)
	layout.cellWidth = max((width-(layout.columns-1)*launcherGridGap)/layout.columns, 1)
	layout.itemHeight = layout.tileHeight + launcherGridGap
	return layout
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
		surface := PanelSettings
		var output uint32
		if visible {
			r.rebuildPanel(currentHost)
			output = currentHost.output
		} else if currentHost = r.panelHosts[PanelLauncher]; currentHost != nil {
			r.rebuildPanel(currentHost)
			output = currentHost.output
			surface = PanelLauncher
			visible = true
		}
		r.mu.Unlock()

		if visible {
			r.publishSurface(output, panelSurfaceID(surface))
		}
	}()
}

func (r *Registry) launcherQueryKind(h *PanelHost) launcherKind {
	return classifyLauncherQuery(h.query, r.launcherProviders()).kind
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
				h.launcherBaseResults = launcherWithHints(h.query, snap.rows)
				h.launcherResults = h.launcherBaseResults
				if r.launcherQueryKind(h) == launcherKindApps {
					h.launcherResults = launcherCategoryResults(h.launcherBaseResults, h.launcherCategory)
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
	// A menu belongs to the result set that opened it. Drop it whenever a
	// query changes so a delayed result cannot leave an old desktop action live.
	h.menu = nil
	h.launcherMenuID = ""
	h.launcherActions = nil
	if plan.kind == launcherKindOverview {
		h.launcherBaseResults = r.launcherOverviewRows(plan.passed)
		h.launcherResults = h.launcherBaseResults
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
	if plan.kind == launcherKindInline {
		if len(b.inline) != len(plan.inlineProviders) {
			return
		}
		out = nil
		for _, prefix := range plan.inlineProviders {
			out = append(out, b.inline[prefix]...)
		}
		out = append(out, b.rows...)
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

func launcherCategoryResults(rows []launcher.Result, category string) []launcher.Result {
	if category == "" {
		return rows
	}
	filtered := make([]launcher.Result, 0, len(rows))
	for _, row := range rows {
		if slices.Contains(row.Entry.Categories, category) {
			filtered = append(filtered, row)
		}
	}
	return filtered
}

func launcherCategories(entries []launcher.Entry) []string {
	seen := make(map[string]struct{})
	for _, entry := range entries {
		for _, category := range entry.Categories {
			if category != "" {
				seen[category] = struct{}{}
			}
		}
	}
	categories := make([]string, 0, len(seen))
	for category := range seen {
		categories = append(categories, category)
	}
	slices.Sort(categories)
	return categories
}

const launcherCategoryAction = "launcher-category:"

func launcherCategoryChip(h *PanelHost, category, label string) *ui.Node {
	action := launcherCategoryAction + category
	content := []*ui.Node{{Kind: ui.KindText, Text: label, TextRole: theme.RoleLabel}}
	state := ui.Interaction(0)
	if category == h.launcherCategory {
		state |= ui.StateSelected
		content = append([]*ui.Node{{Kind: ui.KindIcon, Icon: "check", IconSize: h.theme.Metrics.IconSmall}}, content...)
	}
	return &ui.Node{
		Kind: ui.KindButton, Action: action, Key: action, Name: label, Role: "button",
		Focusable: true, Height: h.theme.Metrics.CompactControl, Padding: h.theme.Metrics.ButtonPadding,
		Fill: ui.FillContainerHigh, Shape: ui.ShapeMedium, State: state,
		Children: []*ui.Node{{Kind: ui.KindRow, Gap: theme.MarginXS, Children: content}},
	}
}

func launcherCategoryControls(r *Registry, h *PanelHost) *ui.Node {
	if r.launcherQueryKind(h) != launcherKindApps {
		return nil
	}
	snapshot := r.launcherApplications.Load()
	if snapshot == nil || !snapshot.available {
		return nil
	}
	categories := launcherCategories(snapshot.entries)
	if len(categories) == 0 && h.launcherCategory == "" {
		return nil
	}
	chips := []*ui.Node{launcherCategoryChip(h, "", h.tr("launcher.category.all", "All"))}
	for _, category := range categories {
		chips = append(chips, launcherCategoryChip(h, category, category))
	}
	width := max(h.place.Panel.W-2*theme.MarginL, 0)
	return h.flowRows(chips, width, theme.MarginXS)
}

func launcherBrowseControls(r *Registry, h *PanelHost) *ui.Node {
	if r.launcherQueryKind(h) != launcherKindApps {
		return nil
	}
	width := max(h.place.Panel.W-2*theme.MarginL, 0)
	view := &ui.Node{
		Kind: ui.KindSegmented, Key: "launcher-view", Name: h.tr("launcher.view.label", "Launcher view"), Role: "radiogroup",
		Width: min(width, 144), Height: h.theme.Metrics.CompactControl, Gap: theme.MarginXXS,
		Children: []*ui.Node{
			launcherViewButton("list", h.tr("launcher.view.list", "List"), !h.launcherGrid, h.theme.Metrics.CompactControl),
			launcherViewButton("grid", h.tr("launcher.view.grid", "Grid"), h.launcherGrid, h.theme.Metrics.CompactControl),
		},
	}
	compactLabel := h.tr("launcher.view.compact", "Compact")
	compact := &ui.Node{
		Kind: ui.KindButton, Action: "launcher-compact", Key: "launcher-compact",
		Name: compactLabel, Role: "button", Focusable: true,
		Height: h.theme.Metrics.CompactControl, Padding: h.theme.Metrics.ButtonPadding,
		Fill: ui.FillContainerHigh, Shape: ui.ShapeMedium,
		Children: []*ui.Node{{Kind: ui.KindText, Text: compactLabel, TextRole: theme.RoleLabel}},
	}
	if h.launcherCompact {
		compact.State |= ui.StateSelected
	}
	controls := []*ui.Node{h.flowRows([]*ui.Node{view, compact}, width, theme.MarginXS)}
	if categories := launcherCategoryControls(r, h); categories != nil {
		controls = append(controls, categories)
	}
	return &ui.Node{Kind: ui.KindColumn, Gap: theme.MarginM, Children: controls}
}

func launcherViewButton(mode, label string, selected bool, height int) *ui.Node {
	action := "launcher-view:" + mode
	button := &ui.Node{
		Kind: ui.KindButton, Action: action, Key: action, Name: label, Role: "radio", Focusable: true,
		Height: height, Padding: theme.MarginXS,
		Children: []*ui.Node{{Kind: ui.KindText, Text: label, TextRole: theme.RoleLabel}},
	}
	if selected {
		button.State |= ui.StateSelected
	}
	return button
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
func launcherFooter(h *PanelHost, count int) *ui.Node {
	// Hint rows (empty ID) explain; they are not results.
	for _, res := range h.launcherResults {
		if res.Entry.ID == "" {
			count--
		}
	}
	nounKey, one, other := "launcher.noun.result", "result", "results"
	if strings.TrimSpace(h.query) == "" {
		nounKey, one, other = "launcher.noun.app", "app", "apps"
	}
	// ponytail: launcherHints stays English; sysc-greet prints it verbatim.
	text := fmt.Sprintf("%d %s \u2022 %s", count, h.trn(nounKey, count, one, other), launcherHints)
	if count == 0 {
		text = h.tr("launcher.no_results", "No results") + " \u2022 " + launcherHints
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
func (h *PanelHost) launcherListHeight(layout launcherViewLayout) int {
	used := 2*theme.MarginL + h.launcherHeaderHeight() + theme.MarginM +
		launcherFieldHeight + theme.MarginM + h.launcherFooterHeight() + theme.MarginM
	if h.launcherBrowseControlsHeight > 0 {
		used += h.launcherBrowseControlsHeight + theme.MarginM
	}
	if h.errLabel != "" {
		used += 24 + theme.MarginM
	}
	return max(h.place.Panel.H-used, layout.itemHeight)
}

// launcherTree projects the current snapshot: the rail and one text field
// above a virtual list of result capsules. The selected row is a muted wash;
// every row carries the 40px glyph slot, bold name, and comment.
func launcherTree(r *Registry, h *PanelHost) *ui.Node {
	layout := launcherLayoutFor(r, h)
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
	controls := launcherBrowseControls(r, h)
	h.launcherBrowseControlsHeight = 0
	if controls != nil {
		if height, err := ui.ContentHeight(controls, max(h.place.Panel.W-2*theme.MarginL, 0), h.measureText()); err == nil {
			h.launcherBrowseControlsHeight = height
		}
		head = append(head, controls)
	}

	results := h.launcherResults
	if len(results) == 0 {
		head = append(head,
			&ui.Node{Kind: ui.KindText, Text: "No results"},
			launcherFooter(h, 0))
		return &ui.Node{Kind: ui.KindColumn, Gap: theme.MarginM, Padding: theme.MarginL, Children: head}
	}
	h.launcherSel = min(max(h.launcherSel, 0), len(results)-1)

	itemCount := len(results)
	if layout.grid {
		itemCount = (itemCount + layout.columns - 1) / layout.columns
	}
	listH := h.launcherListHeight(layout)
	list := &ui.Node{
		Kind:         ui.KindVirtualList,
		Height:       listH,
		ItemCount:    itemCount,
		ItemHeight:   layout.itemHeight,
		ScrollOffset: h.launcherVisibleOffset(len(results), layout),
		// Each pill sits launcherRowGap/2 inside the list on every side, so
		// the bar starts the rest of launcherScrollbarClear beyond the list's
		// right edge, and its ends stop level with the first and last pill
		// instead of past them.
		ScrollbarGap:  launcherScrollbarClear - launcherRowGap/2,
		ScrollbarEnds: launcherRowGap / 2,
		Item: func(i int) *ui.Node {
			if layout.grid {
				return launcherGridRow(r, h, results, i, layout)
			}
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
	layout := launcherLayoutFor(r, h)
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
		Kind: ui.KindColumn, Padding: layout.rowGap / 2,
		Children: []*ui.Node{{
			Kind: ui.KindCapsule, Fill: fill, Shape: ui.ShapeMedium,
			Padding: layout.rowPadTop, Height: layout.rowPadTop + layout.iconSize + layout.rowPadBottom,
			// By index: a desktop-action row shares its application's ID.
			Action: "launch:" + strconv.Itoa(i),
			Children: []*ui.Node{{
				Kind:     ui.KindColumn,
				Children: []*ui.Node{launcherRowBody(r, h, res.Entry, layout)},
			}},
		}},
	}
}

func launcherGridRow(r *Registry, h *PanelHost, results []launcher.Result, row int, layout launcherViewLayout) *ui.Node {
	start := row * layout.columns
	if start < 0 || start >= len(results) {
		return nil
	}
	end := min(start+layout.columns, len(results))
	tiles := make([]*ui.Node, 0, end-start)
	for i := start; i < end; i++ {
		tiles = append(tiles, launcherGridTile(r, h, results[i], i, layout))
	}
	return &ui.Node{Kind: ui.KindColumn, Padding: launcherGridGap / 2, Children: []*ui.Node{{
		Kind: ui.KindRow, Height: layout.tileHeight, Gap: launcherGridGap, Children: tiles,
	}}}
}

func launcherGridTile(r *Registry, h *PanelHost, result launcher.Result, index int, layout launcherViewLayout) *ui.Node {
	if h.launcherMenuID == result.Entry.ID && result.Action == "" && h.menu != nil && h.menu.Opened() {
		return h.menu.Node()
	}
	fill := ui.FillNone
	if index == h.launcherSel && result.Entry.ID != "" {
		fill = ui.FillSoft
	}
	icon := launcherIconNodeSized(r, h, result.Entry, layout.iconSize)
	icon.CenterX = true
	name := &ui.Node{
		Kind: ui.KindText, Text: result.Entry.Name, TextRole: theme.RoleLabel,
		Tone: ui.ToneNormal, CenterX: true,
		MaxWidth: max(layout.cellWidth-2*layout.tilePadding, 1),
	}
	if result.Entry.ID == "" {
		name.Tone = ui.ToneSubtle
	}
	return &ui.Node{
		Kind: ui.KindButton, Action: "launch:" + strconv.Itoa(index), Key: "launch:" + strconv.Itoa(index),
		Width: layout.cellWidth, Height: layout.tileHeight, Fill: fill, Shape: ui.ShapeMedium,
		Padding: layout.tilePadding, Name: result.Entry.Name, Role: "button",
		Children: []*ui.Node{{Kind: ui.KindColumn, Gap: layout.tileContentGap, Children: []*ui.Node{icon, name}}},
	}
}

func launcherRowBody(r *Registry, h *PanelHost, e launcher.Entry, layout launcherViewLayout) *ui.Node {
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
	labelW := h.place.Panel.W - 2*theme.MarginL - 2*layout.rowPadTop - layout.iconSize - theme.MarginL
	if labelW < 80 {
		labelW = 80
	}
	return &ui.Node{
		Kind: ui.KindRow, Gap: theme.MarginL,
		Children: []*ui.Node{
			launcherIconNodeSized(r, h, e, layout.iconSize),
			// Height pins the label block to the icon slot: two labels must not
			// outgrow the row and eat the capsule's 16 foot on a tall font.
			{Kind: ui.KindColumn, Gap: theme.MarginXXS, Width: labelW, Height: layout.iconSize, Children: labels},
		},
	}
}

func launcherIconNode(r *Registry, h *PanelHost, e launcher.Entry) *ui.Node {
	return launcherIconNodeSized(r, h, e, launcherIconSlot)
}

func launcherIconNodeSized(r *Registry, h *PanelHost, e launcher.Entry, size int) *ui.Node {
	glyphSize := max(min(launcherGlyphSize, size-12), 12)
	// Provider rows have no desktop icon. "glyph:<name>" draws a Material
	// subset glyph in the slot; "text:<s>" draws s itself (an emoji).
	if name, ok := strings.CutPrefix(e.IconName, "glyph:"); ok {
		return &ui.Node{
			Kind: ui.KindCapsule, Width: size, Height: size,
			Fill: ui.FillContainer, Shape: ui.ShapeMedium,
			Children: []*ui.Node{{Kind: ui.KindIcon, Icon: name, IconSize: glyphSize}},
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
	return &ui.Node{
		Kind: ui.KindCapsule, Width: size, Height: size,
		Fill: ui.FillContainer, Shape: ui.ShapeMedium,
		Children: []*ui.Node{{Kind: ui.KindText, Text: launcherGlyph(e.Name), TextRole: theme.RoleTitle}},
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
func (h *PanelHost) launcherVisibleOffset(count int, layout launcherViewLayout) int {
	itemCount, selected := count, h.launcherSel
	if layout.grid {
		itemCount = (count + layout.columns - 1) / layout.columns
		selected /= layout.columns
	}
	viewH := h.launcherListHeight(layout)
	maxOff := max(itemCount*layout.itemHeight-viewH, 0)
	off := min(max(h.launcherScroll, 0), maxOff)
	if top := selected * layout.itemHeight; top < off {
		off = top
	}
	if bottom := (selected + 1) * layout.itemHeight; bottom > off+viewH {
		off = bottom - viewH
	}
	h.launcherScroll = off
	return off
}

func (h *PanelHost) launcherKeyPress(r *Registry, key uint32) bool {
	if focused := h.focused(); focused != nil && focused.Kind != ui.KindTextField {
		return false
	}
	n := len(h.launcherResults)
	layout := launcherLayoutFor(r, h)
	page := max(h.launcherPageItems(layout), 1)
	switch key {
	case keyUp:
		delta := -1
		if layout.grid {
			delta = -layout.columns
		}
		h.launcherMoveSel(r, delta)
		return true
	case keyDown:
		delta := 1
		if layout.grid {
			delta = layout.columns
		}
		h.launcherMoveSel(r, delta)
		return true
	case keyLeft:
		if layout.grid {
			h.launcherMoveSel(r, -1)
			return true
		}
	case keyRight:
		if layout.grid {
			h.launcherMoveSel(r, 1)
			return true
		}
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

func (h *PanelHost) launcherPageItems(layout launcherViewLayout) int {
	rows := max(h.launcherListHeight(layout)/layout.itemHeight, 1)
	if layout.grid {
		return rows * layout.columns
	}
	return rows
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
	svc := r.launcherServiceLocked()
	go func() {
		err := r.activateLauncherService(h, svc, attempt, id, action)
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

func (r *Registry) activateLauncherService(h *PanelHost, svc *launcher.Service, attempt uint64, id, action string) error {
	r.launcherActivationMu.Lock()
	defer r.launcherActivationMu.Unlock()
	r.mu.Lock()
	h.launcherPendingAttempt = attempt
	r.mu.Unlock()

	err := svc.Activate(id, action)
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
// (Noctalia's single-click); a right press opens the row's desktop actions as
// a KindMenu (D7). Rows are deliberately not focusable, so the field keeps
// keyboard focus and typing always filters.
func (h *PanelHost) launcherPointerPress(r *Registry, e wayland.Event) bool {
	if h.launcherAwaiting {
		return true
	}
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
	if h.launcherAwaiting {
		return true
	}
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
	if h.menu == nil || h.launcherAwaiting {
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
	if mode, ok := strings.CutPrefix(n.Action, "launcher-view:"); ok {
		if r.launcherQueryKind(h) != launcherKindApps {
			return true
		}
		switch mode {
		case "list":
			if !h.launcherGrid {
				return true
			}
			h.launcherGrid = false
		case "grid":
			if h.launcherGrid {
				return true
			}
			h.launcherGrid = true
		default:
			return false
		}
		h.resetLauncherBrowsePosition()
		r.rebuildPanel(h)
		return true
	}
	if n.Action == "launcher-compact" {
		if r.launcherQueryKind(h) != launcherKindApps {
			return true
		}
		h.launcherCompact = !h.launcherCompact
		h.resetLauncherBrowsePosition()
		r.rebuildPanel(h)
		return true
	}
	if category, ok := strings.CutPrefix(n.Action, launcherCategoryAction); ok {
		if r.launcherQueryKind(h) == launcherKindApps {
			h.launcherCategory = category
			h.launcherResults = launcherCategoryResults(h.launcherBaseResults, category)
			h.resetLauncherBrowsePosition()
			r.rebuildPanel(h)
		}
		return true
	}
	if h.launcherAwaiting {
		return true
	}
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

func (h *PanelHost) resetLauncherBrowsePosition() {
	h.launcherSel = 0
	h.launcherScroll = 0
	h.launcherAttempt++
	h.menu = nil
	h.launcherMenuID = ""
	h.launcherActions = nil
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
