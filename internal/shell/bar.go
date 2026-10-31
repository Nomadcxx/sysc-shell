// Package shell holds the bar model: its retained tree, its widgets, and the
// projection from service and Niri state into that tree.
package shell

import (
	"fmt"
	"math"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/Nomadcxx/sysc-shell/internal/config"
	"github.com/Nomadcxx/sysc-shell/internal/platform/wayland"
	"github.com/Nomadcxx/sysc-shell/internal/render"
	"github.com/Nomadcxx/sysc-shell/internal/ui"
	tray "github.com/Nomadcxx/sysc-tray/protocol"
)

// BarHeight is the nominal bar height token. It is not a Wayland dimension:
// the painted body is BarHeight-2*BarGap and the surface is BarGap + body, so
// the exclusive zone is 44 for the default 48/4 pair.
const BarHeight = 48

// BarGap is the outer gap between the screen edge and the painted body.
const BarGap = 4

const barScrollActionPrefix = "bar-scroll:"

// Bar owns the model, the retained tree, the text renderer and style, and one
// buffered invalidation channel, for exactly one output.
//
// UpdateNiri, Handle, and rendering arrive from different goroutines. The mutex
// is held only while copying or changing state; shaping and painting happen
// after the state has been copied out.
type Bar struct {
	mu                    sync.Mutex
	pressed               string
	hover                 ui.Rect
	hoverAt               struct{ x, y int }
	inside                bool
	axisValue120Action    string
	axisValue120Remainder int64

	// Sections are arranged by ui.ArrangeBar into absolute bounds, so painting
	// and hit testing walk them as one flat list.
	left, center, right []textWidget
	trayNodes           []*ui.Node
	trayItems           []tray.Item
	trayPrefs           config.TrayPreferences
	trayImages          map[tray.ItemKey]*ui.Image
	trayActions         map[string]tray.ItemKey
	trayArranged        trayArrangement
	// trayAvailable is the logical main-axis extent the last layout granted tray icons,
	// after any reserve for the overflow control. The drawer re-derives the
	// same arrangement from it without waiting for the next frame.
	trayAvailable int
	// overflow is what the last layout could not place, per section.
	overflow ui.BarOverflow
	onTray   func(tray.ItemKey, trayArrangement, ui.Rect, wayland.Event) bool
	onPlugin func(string, wayland.Event) bool
	onAction func(action string, button uint32) bool
	onAxis   func(action string, delta int) bool

	// conn is the connector this bar renders for. It selects configuration and
	// joins Niri state; it is never this bar's identity, which is its Wayland
	// global.
	conn string

	theme Theme

	// configured is the last size the Wayland owner gave us, and whether one
	// has arrived. apply re-lays out at this size: the owner configures once,
	// before any widget has text, and every later change arrives through apply
	// alone.
	configured struct {
		width, height int
		set           bool
	}
	// output is the logical size of the screen this bar is on, which the bar's
	// own configure cannot report: that one describes the bar strip. A panel is
	// placed inside the output, so its height comes from here.
	output struct {
		width, height int
	}
	// needsLayout marks the arrangement stale after a text change. It is set
	// by apply, which runs on the clock, metrics and Niri pump goroutines, and
	// consumed by Render, which the Wayland owner calls.
	//
	// The re-layout cannot happen in apply. Arranging shapes text through the
	// font map, which is not safe for concurrent use and is owned by the
	// Wayland goroutine; measuring from a pump would race that goroutine's
	// painting.
	needsLayout bool
	// layoutFailing makes the re-layout log edge-triggered, so a bar whose
	// content stops fitting reports once rather than on every update.
	layoutFailing bool

	text  *render.TextRenderer
	style render.Style

	// Render owns the font map and cached frame. Serializing renders keeps the
	// cache coherent across the compositor's alternating shared-memory buffers.
	renderMu   sync.Mutex
	paintCache barPaintCache

	// pointer is the bar's resolved hover/press state as stable keys, and anim
	// is its one clock. Only clickable capsules carry either: a CPU or memory
	// display group has no action, so it never animates.
	pointer  interaction
	anim     *animator
	stopAnim chan struct{}
	stopOnce sync.Once

	invalidations chan struct{}
	// onPublish is the registry seam wired at adopt time: it routes an
	// invalidation to the Wayland owner for this bar's global. The private
	// channel above is the fallback for a stand-alone bar (proofs, tests);
	// in production nothing reads it, so routing through the registry is
	// what lets animation frames actually reach the owner.
	onPublish          func()
	mediaWidget        bool
	lockKeysWidget     bool
	powerProfileWidget bool
}

// New builds a bar from the built-in defaults for one connector.
func New(connector string) (*Bar, error) {
	cfg := config.Default()
	return NewWithTheme(ThemeFrom(cfg, cfg.Bar), cfg.Bar, connector)
}

// NewWithTheme builds a bar from resolved theme tokens, a bar policy, and the
// connector whose Niri state it reads.
func NewWithTheme(theme Theme, policy config.Bar, connector string) (*Bar, error) {
	if err := theme.Valid(); err != nil {
		return nil, err
	}
	fonts, err := render.NewSystemFontMap(policy.FontFamily, render.DefaultFontCacheDir())
	if err != nil {
		return nil, err
	}
	return newBarWithText(theme, policy, connector, render.NewTextRendererWithFontMap(fonts)), nil
}

func newWithTextRenderer(theme Theme, policy config.Bar, connector string, text *render.TextRenderer) (*Bar, error) {
	if err := theme.Valid(); err != nil {
		return nil, err
	}
	return newBarWithText(theme, policy, connector, text), nil
}

func newBarWithText(theme Theme, policy config.Bar, connector string, text *render.TextRenderer) *Bar {
	b := &Bar{
		conn:               connector,
		theme:              theme,
		mediaWidget:        hasMediaItem(policy.Left) || hasMediaItem(policy.Center) || hasMediaItem(policy.Right),
		lockKeysWidget:     hasBarItem(policy, "lock_keys"),
		powerProfileWidget: hasBarItem(policy, "power_profile"),
		text:               text,
		invalidations:      make(chan struct{}, 1),
		style:              barStyle(theme),
		pointer:            interaction{stateLayer: true},
		anim:               newAnimator(nil, false, theme.Motion),
		stopAnim:           make(chan struct{}),
	}

	side := b.barAxis() == ui.Vertical
	contentWidth := max(0, b.theme.barGeometry().Body()-2*b.theme.Metrics.BarPadding)
	b.left = buildWidgetsAxis(policy.Left, b.theme.Metrics.CapsulePadding, b.theme.Metrics, side, contentWidth)
	b.center = buildWidgetsAxis(policy.Center, b.theme.Metrics.CapsulePadding, b.theme.Metrics, side, contentWidth)
	b.right = buildWidgetsAxis(policy.Right, b.theme.Metrics.CapsulePadding, b.theme.Metrics, side, contentWidth)
	return b
}

// connector reports the output this bar renders for.
func (b *Bar) connector() string { return b.conn }

// configuredSize is the last logical configure, which is the exclusive-zone
// band the compositor granted. IPC panels centre and flush against it; the
// fallback 1920x1080 is only for a bar that has not mapped yet.
func (b *Bar) configuredSize() (w, h int) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if !b.configured.set {
		return 0, 0
	}
	return b.configured.width, b.configured.height
}

// widgets returns the three sections in paint order.
func (b *Bar) widgets() [][]textWidget { return [][]textWidget{b.left, b.center, b.right} }

func visibleWidgetNode(w textWidget) *ui.Node {
	if w.node == nil {
		return nil
	}
	if w.hideWhenAbsent && w.node.Absent {
		clearNodeBounds(w.node)
		return nil
	}
	if len(w.members) > 0 {
		container := w.inner
		if container == nil {
			container = w.node
		}
		children := make([]*ui.Node, 0, len(w.members))
		for _, member := range w.members {
			if node := visibleWidgetNode(member); node != nil {
				children = append(children, node)
			}
		}
		container.Children = children
	}
	return w.node
}

func clearNodeBounds(n *ui.Node) {
	if n == nil {
		return
	}
	n.Bounds = ui.Rect{}
	for _, child := range n.Children {
		clearNodeBounds(child)
	}
}

// sections returns the retained nodes in paint order, for layout and painting.
func (b *Bar) sections() [][]*ui.Node {
	out := make([][]*ui.Node, 0, 3)
	for _, section := range b.widgets() {
		nodes := make([]*ui.Node, 0, len(section))
		for _, w := range section {
			if node := visibleWidgetNode(w); node != nil {
				nodes = append(nodes, node)
			}
		}
		out = append(out, nodes)
	}
	if b.trayPrefs.Enabled {
		out[2] = append(out[2], b.trayNodes...)
	}
	return out
}

func (b *Bar) setTray(items []tray.Item, prefs config.TrayPreferences, images map[tray.ItemKey]*ui.Image) {
	b.mu.Lock()
	b.trayItems = append([]tray.Item(nil), items...)
	b.trayPrefs = config.TrayPreferences{
		Enabled: prefs.Enabled, ShowPassive: prefs.ShowPassive,
		Hidden: append([]string(nil), prefs.Hidden...), Pinned: append([]string(nil), prefs.Pinned...),
		Order: append([]string(nil), prefs.Order...),
	}
	b.trayImages = make(map[tray.ItemKey]*ui.Image, len(images))
	for key, image := range images {
		b.trayImages[key] = image
	}
	b.needsLayout = true
	b.mu.Unlock()
}

func (b *Bar) setTrayHandler(fn func(tray.ItemKey, trayArrangement, ui.Rect, wayland.Event) bool) {
	b.mu.Lock()
	b.onTray = fn
	b.mu.Unlock()
}

func (b *Bar) setPluginHandler(fn func(string, wayland.Event) bool) {
	b.mu.Lock()
	b.onPlugin = fn
	b.mu.Unlock()
}

func (b *Bar) setActionHandler(fn func(action string, button uint32) bool) {
	b.mu.Lock()
	b.onAction = fn
	b.mu.Unlock()
}

func (b *Bar) setAxisHandler(fn func(action string, delta int) bool) {
	b.mu.Lock()
	b.onAxis = fn
	b.mu.Unlock()
}

// trayArrangement re-derives the split from the current items at the width the
// last layout granted. The drawer reads it, so a drawer opened or refreshed
// between frames shows the same overflow the bar will.
func (b *Bar) trayArrangement() trayArrangement {
	b.mu.Lock()
	defer b.mu.Unlock()
	return arrangeTray(b.trayItems, b.trayPrefs, b.trayAvailable, max(trayItemSize, b.theme.Metrics.BarItemMin), b.theme.Metrics.BarSpacing)
}

// setOutputSize records the screen's logical size. The Wayland owner calls it
// just before Configure.
func (b *Bar) setOutputSize(width, height int) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.output.width, b.output.height = width, height
}

// outputSize reports the screen's logical size, zero before the first mode.
func (b *Bar) outputSize() (w, h int) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.output.width, b.output.height
}

// scale120 reports the output scale the bar was last configured at, in 120ths.
func (b *Bar) scale120() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return int(b.style.Scale120)
}

func (b *Bar) themeSnapshot() Theme {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.theme
}

// retheme replaces the resolved paint roles without discarding the output
// geometry already supplied by the Wayland configure path.
func (b *Bar) retheme(theme Theme) {
	b.mu.Lock()
	if b.anim != nil {
		// A speed or reduced-motion change reaches a running bar animation
		// instead of waiting for the next surface.
		b.anim.Configure(theme.Motion, theme.Motion.Reduced)
	}
	scale, body := b.style.Scale120, b.style.Body
	b.theme = theme
	b.style = barStyle(theme)
	b.style.Scale120, b.style.Body = scale, body
	b.mu.Unlock()
	b.invalidate()
}

// apply writes each widget's state from the view and reports whether anything
// changed. A false return means no layout and no redraw: no state change, no
// submitted frame.
func (b *Bar) apply(view barView) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.applyLocked(view)
}

func (b *Bar) applyLocked(view barView) bool {
	changed := false
	for _, section := range b.widgets() {
		for _, w := range section {
			// A meter and a graph carry their state on the node rather than in
			// text, and format writes it as a side effect. The previous state
			// is captured first so every display mode is compared, not just
			// the one whose state happens to be a string.
			if w.refresh != nil {
				if w.refresh(view) {
					changed = true
				}
				continue
			}
			// State lives on the inner node; the capsule is chrome. An
			// uncapsuled widget keeps its state on node itself.
			state := w.inner
			if state == nil {
				state = w.node
			}
			if state == nil || w.format == nil {
				continue
			}
			before := *state
			if text := w.format(view); text != state.Text {
				state.Text = text
			}
			if nodeVisualStateChanged(before, *state) {
				changed = true
			}
		}
	}
	// Measured widths follow the new text, so the arrangement is now stale.
	// Render performs it, on the goroutine that owns the font map.
	if changed {
		b.needsLayout = true
	}
	return changed
}

// Invalidations is the stand-alone seam for a bar no registry has adopted;
// proofs and tests own it and never close it. An adopted bar routes its
// invalidations through the registry instead (see onPublish).
func (b *Bar) Invalidations() <-chan struct{} { return b.invalidations }

// invalidate requests one coalesced redraw.
func (b *Bar) invalidate() {
	select {
	case b.invalidations <- struct{}{}:
	default:
	}
	if b.onPublish != nil {
		b.onPublish()
	}
}

// Layout arranges the three sections at the logical configure size.
func (b *Bar) Layout(width, height int) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.layoutLocked(width, height)
}

// rememberSizeLocked records the size later applies re-lay out at.
func (b *Bar) rememberSizeLocked(width, height int) {
	b.configured.width, b.configured.height, b.configured.set = width, height, true
}

// relayoutLocked re-arranges at the last configured size when a change has
// made the arrangement stale. The caller must be the Wayland owner: this
// shapes text.
//
// Without it the bar is arranged exactly once, at configure time, when every
// widget is still empty, so the first clock tick and the first window title
// would measure into a zero-width box and never appear.
//
// A failure keeps the previous bounds rather than clearing them, because a
// stale arrangement still paints something coherent while an empty one paints
// nothing. It is reported once per transition; the next success clears it.
func (b *Bar) relayoutLocked() {
	if !b.needsLayout || !b.configured.set {
		return
	}
	if region := shellTraceRegion("shell.layout"); region != nil {
		defer region.End()
	}
	b.needsLayout = false
	err := b.layoutLocked(b.configured.width, b.configured.height)
	switch {
	case err != nil && !b.layoutFailing:
		b.layoutFailing = true
		fmt.Fprintf(os.Stderr, "sysc-shell: bar %s cannot arrange its content: %v\n", b.conn, err)
	case err == nil && b.layoutFailing:
		b.layoutFailing = false
		fmt.Fprintf(os.Stderr, "sysc-shell: bar %s arranged its content again\n", b.conn)
	}
}

// contentLocked derives the content band from the theme tokens. The surface is
// the configure size; the body is that surface inset by the gap on the anchored
// edge and both ends; the content is the body inset by the padding.
func (b *Bar) contentLocked(width, height int) ui.Rect {
	body := b.bodyLocked(width, height)
	pad := b.theme.Metrics.BarPadding
	return ui.Rect{
		X: body.X + pad, Y: body.Y + pad,
		W: max(0, body.W-2*pad), H: max(0, body.H-2*pad),
	}
}

func (b *Bar) bodyLocked(width, height int) ui.Rect {
	var body ui.Rect
	body.X, body.Y, body.W, body.H = b.theme.barGeometry().BodyIn(width, height)
	return body
}

// blurShape is the region the compositor blurs behind the bar, in surface
// coordinates: the body when frosted, each visible capsule when islands, and
// nothing when the bar paints solid or the compositor cannot blur. It reads the
// arrangement Render just brought up to date.
func (b *Bar) blurShape() []ui.Rect {
	b.mu.Lock()
	defer b.mu.Unlock()
	if !b.theme.Blur || !b.configured.set {
		return nil
	}
	if b.theme.BarStyle == "islands" {
		var out []ui.Rect
		for _, section := range b.sections() {
			for _, n := range section {
				if n.Kind != ui.KindCapsule || n.Bounds.W <= 0 || n.Bounds.H <= 0 {
					continue
				}
				// Zero is half the shorter side, which BlurStrips clamps to.
				radius := render.CapsuleRadius(b.style, n)
				if radius <= 0 {
					radius = min(n.Bounds.W, n.Bounds.H)
				}
				out = append(out, ui.BlurStrips(ui.SurfaceShape{Body: render.ChromeBounds(n), Radius: radius, Scale: b.style.Scale120})...)
			}
		}
		return out
	}
	shape := ui.SurfaceShape{
		Body:   b.bodyLocked(b.configured.width, b.configured.height),
		Radius: b.theme.Radius,
		Scale:  b.style.Scale120,
	}
	if b.theme.barGeometry().Attached() {
		// An attached bar meets the screen along its edge and curves into the
		// screen's sides at both ends.
		shape.Radius, shape.AttachEdge = 0, b.theme.BarEdge
		shape.EdgeFillet, shape.EdgeLeft, shape.EdgeRight = b.theme.Fillet, true, true
	}
	return ui.BlurStrips(shape)
}

func (b *Bar) layoutLocked(width, height int) error {
	// Shaping for paint happens at the physical size, so measuring at the
	// logical size and scaling the result up assumes glyph advances are linear
	// in point size. They are not: at scale 1.25 the painter shaped text wider
	// than layout had reserved and ellipsized a clock that fits.
	//
	// Measure at the size the painter will actually use, then convert back up.
	measure := func(s string, attrs ui.TextAttrs) (int, int) {
		w, h, err := b.text.Measure(s, render.SpecFor(b.style, attrs), attrs.Tabular)
		if err != nil {
			return 0, 0
		}
		return b.style.Scale120.Logical(w), b.style.Scale120.Logical(h)
	}
	b.trayNodes = nil
	sections := b.sections()
	content := b.contentLocked(width, height)
	axis := b.barAxis()
	for _, section := range sections {
		for _, n := range section {
			if _, err := barPointerSlots(n, axis, b.theme.Metrics.BarItemMin); err != nil {
				return err
			}
		}
	}
	// The first pass only sizes the tray's available width; the authoritative
	// overflow is the second, after the tray nodes are rebuilt.
	if _, err := ui.ArrangeBar(content, axis,
		sections[0], sections[1], sections[2], b.theme.Metrics.BarSpacing, measure); err != nil {
		return err
	}
	available := b.trayAvailableLocked(content, sections[1], sections[2])
	slot := max(trayItemSize, b.theme.Metrics.BarItemMin)
	arranged := arrangeTray(b.trayItems, b.trayPrefs, available, slot, b.theme.Metrics.BarSpacing)
	if len(arranged.Overflow) > 0 || len(arranged.Hidden) > 0 {
		drawer := b.trayDrawerNode()
		if _, err := barPointerSlots(drawer, axis, b.theme.Metrics.BarItemMin); err != nil {
			return err
		}
		reserve, _, err := ui.Measure(drawer, measure)
		if axis == ui.Vertical {
			reserve, err = ui.ContentHeight(drawer, content.W, measure)
		}
		if err != nil {
			return err
		}
		if len(sections[2]) > 0 || available > reserve {
			reserve += b.theme.Metrics.BarSpacing
		}
		available = max(0, available-reserve)
		arranged = arrangeTray(b.trayItems, b.trayPrefs, available, slot, b.theme.Metrics.BarSpacing)
	}
	b.trayArranged, b.trayAvailable = arranged, available
	b.rebuildTrayNodesLocked()
	for _, n := range b.trayNodes {
		if _, err := barPointerSlots(n, axis, b.theme.Metrics.BarItemMin); err != nil {
			return err
		}
	}
	sections = b.sections()
	over, err := ui.ArrangeBar(content, axis, sections[0], sections[1], sections[2], b.theme.Metrics.BarSpacing, measure)
	if err != nil {
		return err
	}
	// Recorded rather than drawn: sysc-313 stops the silent clip, and the
	// chrome that shows the count belongs to the bar composition surface.
	b.overflow = over
	return nil
}

// Overflow reports what the last layout could not place, per section.
func (b *Bar) Overflow() ui.BarOverflow {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.overflow
}

func (b *Bar) barAxis() ui.Axis {
	if b.theme.BarEdge == "left" || b.theme.BarEdge == "right" {
		return ui.Vertical
	}
	return ui.Horizontal
}

func (b *Bar) trayAvailableLocked(content ui.Rect, center, right []*ui.Node) int {
	mainStart, mainEnd := content.X, content.X+content.W
	position := func(r ui.Rect) (int, int) { return r.X, r.W }
	if b.barAxis() == ui.Vertical {
		mainStart, mainEnd = content.Y, content.Y+content.H
		position = func(r ui.Rect) (int, int) { return r.Y, r.H }
	}
	start := mainStart + (mainEnd-mainStart)/2
	if len(center) > 0 {
		last := center[len(center)-1].Bounds
		p, extent := position(last)
		start = p + extent
	}
	// ArrangeBar reserves this separation even for an empty centre lane.
	start += b.theme.Metrics.BarSpacing
	used := 0
	if len(right) > 0 {
		p, _ := position(right[0].Bounds)
		used = mainEnd - p
		used += b.theme.Metrics.BarSpacing
	}
	return max(0, mainEnd-start-used)
}

func (b *Bar) rebuildTrayNodesLocked() {
	b.trayActions = make(map[string]tray.ItemKey, len(b.trayArranged.Bar))
	for i, item := range b.trayArranged.Bar {
		action := fmt.Sprintf("tray-item:%d", i)
		b.trayActions[action] = item.Key
		b.trayNodes = append(b.trayNodes, &ui.Node{
			Kind: ui.KindImage, ImageSize: trayItemSize, Image: b.trayImages[item.Key],
			Action: action, Tooltip: b.trayTooltip(item),
		})
	}
	if len(b.trayArranged.Overflow) > 0 || len(b.trayArranged.Hidden) > 0 {
		b.trayNodes = append(b.trayNodes, b.trayDrawerNode())
	}
}

func (b *Bar) trayDrawerNode() *ui.Node {
	return &ui.Node{Kind: ui.KindButton, Text: "…", Padding: b.theme.Metrics.CapsulePadding, Action: trayDrawerAction,
		Tooltip: "Tray items", Focusable: true, Name: "Tray items", Role: "button"}
}

func (b *Bar) trayTooltip(item tray.Item) string {
	text := strings.TrimSpace(item.Tooltip.Title)
	if description := strings.TrimSpace(item.Tooltip.Description); description != "" {
		if text != "" {
			text += "\n"
		}
		text += description
	}
	if text == "" {
		text = strings.TrimSpace(item.Title)
	}
	if len(text) > tray.MaxTooltipBytes {
		text = text[:tray.MaxTooltipBytes]
	}
	return text
}

// Configure records a new logical size and scale from the Wayland owner.
func (b *Bar) Configure(logicalWidth, logicalHeight, scale120 int) error {
	b.mu.Lock()
	defer b.mu.Unlock()

	scale := ui.Scale120(scale120)
	if !scale.Valid() {
		return fmt.Errorf("shell: scale120 %d is not usable", scale120)
	}
	b.style.Scale120 = scale
	b.style.Body = b.bodyLocked(logicalWidth, logicalHeight)
	b.style.Radius = b.theme.Radius
	b.rememberSizeLocked(logicalWidth, logicalHeight)
	b.needsLayout = false
	return b.layoutLocked(logicalWidth, logicalHeight)
}

// Render paints the arranged tree into the physical buffer.
func (b *Bar) Render(pixels []byte, width, height, stride int) error {
	canvas, err := render.NewCanvas(pixels, width, height, stride)
	if err != nil {
		return err
	}

	b.renderMu.Lock()
	defer b.renderMu.Unlock()

	b.mu.Lock()
	// The arrangement is brought up to date here rather than in apply, because
	// this is the goroutine that owns the font map.
	b.relayoutLocked()
	root, style := b.renderViewLocked()
	b.mu.Unlock()

	return b.paintCache.paint(canvas, root, b.text, style)
}

// renderViewLocked copies the mutable values painting reads, so the Niri
// goroutine can update the model while shaping and rasterization run.
//
// The three sections are already arranged into absolute bounds, so they flatten
// into one child list: the painter walks bounds, not structure.
func (b *Bar) renderViewLocked() (*ui.Node, render.Style) {
	root := &ui.Node{Kind: ui.KindRow}
	sections := b.sections()
	for _, section := range sections {
		for _, n := range section {
			root.Children = append(root.Children, copyNode(n))
		}
	}
	// Last, so the fade sits over the widgets it softens. The extent comes
	// from the control ladder rather than a literal, like every other measured
	// piece of this bar.
	root.Children = append(root.Children,
		overflowFades(sections, b.overflow, b.theme.Metrics.StandardControl, b.barAxis())...)
	// The painter consumes an immutable mask, so state is resolved onto the
	// copy that is about to be drawn rather than onto live model state.
	b.pointer.apply(root, b.anim)
	// Hover and press use the same state layers as the other surfaces.
	b.resolveGradientMotionLocked(root)
	b.resolveMediaMotionLocked(root)
	resolveProgressMotion(b.anim, root)
	resolveSpriteMotion(b.anim, root)
	b.startBarFramesLocked()
	return root, b.style
}

func (b *Bar) resolveGradientMotionLocked(root *ui.Node) {
	seen := make(map[string]bool)
	var walk func(*ui.Node)
	walk = func(n *ui.Node) {
		if n == nil {
			return
		}
		if n.Gradient.Motion != ui.GradientNone {
			if key := n.StableKey(); key != "" {
				seen[key] = true
				b.anim.TargetLoop(key, animGradient, n.Gradient.From, n.Gradient.To, gradientTrip, n.Gradient.Motion)
				n.GradientOffset = b.anim.Value(key, animGradient)
			}
		}
		for _, child := range n.Children {
			walk(child)
		}
	}
	walk(root)
	for key := range b.anim.values {
		if key.channel == animGradient && !seen[key.node] {
			delete(b.anim.values, key)
		}
	}
	b.startBarFramesLocked()
}

func (b *Bar) resolveMediaMotionLocked(root *ui.Node) {
	seen := make(map[string]bool)
	var walk func(*ui.Node)
	walk = func(n *ui.Node) {
		if n == nil {
			return
		}
		if n.Marquee {
			key := n.StableKey()
			if key != "" && b.anim != nil {
				spec := render.SpecFor(b.style, ui.TextAttrsOf(n))
				advance, _, err := b.text.Measure(n.Text, spec, n.Tabular)
				cell := b.style.Scale120.Physical(n.Bounds.W)
				gap, _, gapErr := b.text.Measure(strings.Repeat(" ", 8), spec, n.Tabular)
				cycle := advance + gap
				if err == nil && gapErr == nil && advance > cell && cycle > 0 && !b.anim.reduced {
					seen[key] = true
					trip := time.Duration(math.Ceil(float64(cycle) / marqueePixelsPerSecond * float64(time.Second)))
					b.anim.TargetSweep(key, animSweep, trip)
					n.TextOffset = int(math.Round(b.anim.Value(key, animSweep) * float64(cycle)))
					return
				}
			}
			n.Marquee = false
			n.TextOffset = 0
		}
		for _, child := range n.Children {
			walk(child)
		}
	}
	walk(root)
	for key := range b.anim.values {
		if key.channel == animSweep && !seen[key.node] {
			delete(b.anim.values, key)
		}
	}
	b.startBarFramesLocked()
}

func (b *Bar) startBarFramesLocked() {
	if b.anim == nil || b.anim.running.Load() || b.anim.Settled() {
		return
	}
	b.anim.running.Store(true)
	go b.barFrameLoop()
}

func (b *Bar) barFrameLoop() {
	defer func() {
		b.mu.Lock()
		b.anim.running.Store(false)
		b.mu.Unlock()
	}()
	// Resolve the cap per tick so interaction resumes the normal cadence.
	frameCap := func() time.Duration {
		b.mu.Lock()
		defer b.mu.Unlock()
		if b.anim.SettledExceptEffects() {
			return effectFrameCap
		}
		return b.anim.frameCap()
	}
	b.mu.Lock()
	wake := b.anim.wake
	b.mu.Unlock()
	animateSurfaceResting(b.stopAnim, wake, func() bool {
		b.mu.Lock()
		defer b.mu.Unlock()
		return b.anim.Settled()
	}, b.invalidate, frameCap, func() (time.Duration, bool) {
		b.mu.Lock()
		defer b.mu.Unlock()
		return b.anim.SpriteRest()
	})
}

func (b *Bar) stopAnimation() {
	if b.stopAnim == nil {
		return
	}
	b.stopOnce.Do(func() { close(b.stopAnim) })
}

// copyNode deep-copies a node so no pointer into live model state reaches the
// painter.
func copyNode(n *ui.Node) *ui.Node {
	if n == nil {
		return nil
	}
	c := *n
	// Values is cloned, not shared: the promise above is that no pointer into
	// live model state reaches the painter, and a slice header carries one.
	if len(n.Values) > 0 {
		c.Values = append([]float64(nil), n.Values...)
	}
	// Frames too: the sprite walk writes the resolved pose into the copy's
	// Icon, and must never be handed the live list to alias.
	if len(n.Frames) > 0 {
		c.Frames = append([]string(nil), n.Frames...)
	}
	if len(n.Children) > 0 {
		c.Children = make([]*ui.Node, len(n.Children))
		for i, child := range n.Children {
			c.Children[i] = copyNode(child)
		}
	}
	return &c
}

// copyNodeVisible deep-copies a node like copyNode and drops children the view
// cannot see. The view is a scroll viewport in tree coordinates; a KindScroll
// or KindVirtualList narrows it to its own bounds. A child is dropped only
// when its box misses the view and its subtree cannot paint outside that box:
// a menu's option list floats over later rows and a gap scrollbar paints in
// the parent's padding, so those subtrees stay whole. Children are dropped
// only under flow containers, where a child's position carries no meaning --
// a menu's options, a stack's layers and a segmented control's segments are
// addressed by index. An unbounded view keeps everything (GH #454).
func copyNodeVisible(n *ui.Node, view ui.Rect, bounded bool) *ui.Node {
	if n == nil {
		return nil
	}
	if n.Kind == ui.KindScroll || n.Kind == ui.KindVirtualList {
		// A zero-sized view restricts nothing: clip ignores a restrict
		// without area, so such a scroll's children paint wherever its
		// parent allows. Narrowing to its empty bounds would drop them.
		if n.Bounds.W > 0 && n.Bounds.H > 0 {
			if bounded {
				view = intersectRects(view, n.Bounds)
			} else {
				view = n.Bounds
			}
			bounded = true
		}
	}
	c := *n
	if len(n.Values) > 0 {
		c.Values = append([]float64(nil), n.Values...)
	}
	if len(n.Frames) > 0 {
		c.Frames = append([]string(nil), n.Frames...)
	}
	if len(n.Children) > 0 {
		prune := bounded && flowChildren(n.Kind)
		c.Children = make([]*ui.Node, 0, len(n.Children))
		for _, child := range n.Children {
			if prune && child != nil && !rectOverlap(child.Bounds, view) && !ui.MayDrawOutsideBox(child) {
				// A dropped child is replaced by a bare node in its slot.
				// Positions carry meaning elsewhere -- applyEditorView maps
				// live nodes to copies by index and a menu's options are
				// addressed by row index -- so the copy keeps every slot; an
				// empty KindText in a dropped slot paints nothing and copies
				// for nothing.
				c.Children = append(c.Children, &ui.Node{Kind: ui.KindText, Bounds: child.Bounds})
				continue
			}
			c.Children = append(c.Children, copyNodeVisible(child, view, bounded))
		}
	}
	return &c
}

// flowChildren reports whether a kind lays its children out as free-flowing
// content, where dropping one cannot renumber anything that addresses
// children by position.
func flowChildren(k ui.Kind) bool {
	switch k {
	case ui.KindRow, ui.KindColumn, ui.KindCapsule, ui.KindScroll, ui.KindDropZone:
		return true
	}
	return false
}

func rectOverlap(a, b ui.Rect) bool {
	return a.X < b.X+b.W && b.X < a.X+a.W && a.Y < b.Y+b.H && b.Y < a.Y+a.H
}

func intersectRects(a, b ui.Rect) ui.Rect {
	x0, y0 := max(a.X, b.X), max(a.Y, b.Y)
	x1, y1 := min(a.X+a.W, b.X+b.W), min(a.Y+a.H, b.Y+b.H)
	return ui.Rect{X: x0, Y: y0, W: max(x1-x0, 0), H: max(y1-y0, 0)}
}

// hitLocked searches every section in reverse paint order.
func (b *Bar) actionBounds(action string) ui.Rect {
	b.mu.Lock()
	defer b.mu.Unlock()
	for _, section := range b.sections() {
		for _, n := range section {
			if r, ok := nodeActionBounds(n, action); ok {
				return r
			}
		}
	}
	return ui.Rect{}
}

// hoveredAction is the action under the pointer, or empty when the pointer is
// outside the bar. It is what the media strip's hover intent watches.
func (b *Bar) hoveredAction() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	if !b.inside {
		return ""
	}
	return b.pointer.hover
}

func nodeActionBounds(n *ui.Node, action string) (ui.Rect, bool) {
	if n == nil {
		return ui.Rect{}, false
	}
	if n.Action == action {
		return n.Bounds, true
	}
	for _, c := range n.Children {
		if r, ok := nodeActionBounds(c, action); ok {
			return r, true
		}
	}
	return ui.Rect{}, false
}

func (b *Bar) hitLocked(x, y int) (string, bool) {
	body := b.bodyLocked(b.configured.width, b.configured.height)
	sections := b.sections()
	for i := len(sections) - 1; i >= 0; i-- {
		section := sections[i]
		for j := len(section) - 1; j >= 0; j-- {
			if action, ok := hitBarNode(section[j], x, y, body, b.barAxis()); ok {
				return action, true
			}
		}
	}
	return "", false
}

// barPointerSlots reserves nonoverlapping main-axis space for each control.
// Glyphs and labels keep their intrinsic cross-axis size inside the capsule.
func barPointerSlots(n *ui.Node, axis ui.Axis, size int) (int, error) {
	if n == nil {
		return 0, nil
	}
	n.MinWidth, n.MinHeight = 0, 0
	floor, count := 0, 0
	inline := axis == ui.Horizontal && (n.Kind == ui.KindRow || n.Kind == ui.KindButton) || axis == ui.Vertical && n.Kind == ui.KindColumn
	pad := n.Padding
	if axis == ui.Horizontal && n.Kind == ui.KindCapsule {
		pad = ui.CapsulePadX(n)
	}
	childSize := size
	// A pill around one control is that control's target, so its padding
	// already counts toward the slot.
	if soleCapsuleAction(n) != nil {
		childSize = max(0, size-2*pad)
	}
	for _, child := range n.Children {
		childMin, err := barPointerSlots(child, axis, childSize)
		if err != nil {
			return 0, err
		}
		if inline {
			if count > 0 {
				floor += n.Gap
			}
			floor += childMin
		} else {
			floor = max(floor, childMin)
		}
		count++
	}
	if len(n.Children) == 0 {
		w, h, err := ui.Measure(n, func(string, ui.TextAttrs) (int, int) { return 0, 0 })
		if err != nil {
			return 0, err
		}
		floor = w
		if axis == ui.Vertical {
			floor = h
		}
	} else if floor > 0 {
		floor += 2 * pad
	}
	if n.Action != "" {
		floor = max(floor, size)
	}
	if axis == ui.Vertical {
		n.MinHeight = max(floor, n.Height)
		return n.MinHeight, nil
	}
	n.MinWidth = max(floor, n.Width)
	return n.MinWidth, nil
}

// hitBarNode extends a control's target across the visible band, excluding
// the outside gap and any subtree layout dropped. Paint bounds stay intact.
func hitBarNode(n *ui.Node, x, y int, body ui.Rect, axis ui.Axis) (string, bool) {
	if n == nil || n.Absent || n.Kind == ui.KindEffect || n.State.Has(ui.StateDisabled) || n.Bounds.W <= 0 || n.Bounds.H <= 0 {
		return "", false
	}
	if !barTargetBounds(n.Bounds, body, axis).Contains(x, y) {
		return "", false
	}
	for i := len(n.Children) - 1; i >= 0; i-- {
		if n.Children[i] == nil {
			continue
		}
		childBody := body
		// Cross-axis siblings keep separate targets. A valid multi-row plugin
		// must not have its first row covered by the last row's expansion.
		if len(n.Children) > 1 && (axis == ui.Horizontal && n.Kind == ui.KindColumn || axis == ui.Vertical && n.Kind == ui.KindRow) {
			childBody = intersectRects(body, n.Children[i].Bounds)
		}
		if action, ok := hitBarNode(n.Children[i], x, y, childBody, axis); ok {
			return action, true
		}
	}
	if n.Action != "" {
		return n.Action, true
	}
	if c := soleCapsuleAction(n); c != nil && !c.State.Has(ui.StateDisabled) && c.Bounds.W > 0 && c.Bounds.H > 0 {
		return c.Action, true
	}
	return "", false
}

// soleCapsuleAction returns the outermost node carrying a pill's only action,
// or nil when n is not a capsule or its subtree holds no or several distinct
// actions. Built-in widgets repeat one action on the pill and its glyph.
func soleCapsuleAction(n *ui.Node) *ui.Node {
	if n == nil || n.Kind != ui.KindCapsule {
		return nil
	}
	var found *ui.Node
	several := false
	var walk func(*ui.Node)
	walk = func(c *ui.Node) {
		if c == nil || c.Absent || several {
			return
		}
		if c.Action != "" {
			if found == nil {
				found = c
			} else if c.Action != found.Action {
				several = true
				return
			}
		}
		for _, child := range c.Children {
			walk(child)
		}
	}
	walk(n)
	if several {
		return nil
	}
	return found
}

func barTargetBounds(bounds, body ui.Rect, axis ui.Axis) ui.Rect {
	if bounds.W <= 0 || bounds.H <= 0 || body.W <= 0 || body.H <= 0 {
		return bounds
	}
	if axis == ui.Vertical {
		bounds.X, bounds.W = body.X, body.W
	} else {
		bounds.Y, bounds.H = body.Y, body.H
	}
	return bounds
}

func (b *Bar) scrollActionAtLocked(x, y int) (config.BarScrollAction, string, bool) {
	body := b.bodyLocked(b.configured.width, b.configured.height)
	sections := b.widgets()
	for sectionIndex := len(sections) - 1; sectionIndex >= 0; sectionIndex-- {
		section := sections[sectionIndex]
		for widgetIndex := len(section) - 1; widgetIndex >= 0; widgetIndex-- {
			key := fmt.Sprintf("%d:%d", sectionIndex, widgetIndex)
			if action, key, ok := scrollActionInWidget(section[widgetIndex], x, y, key, body, b.barAxis()); ok {
				return action, key, true
			}
		}
	}
	return "", "", false
}

func scrollActionInWidget(w textWidget, x, y int, key string, body ui.Rect, axis ui.Axis) (config.BarScrollAction, string, bool) {
	node := visibleWidgetNode(w)
	if node == nil || !barTargetBounds(node.Bounds, body, axis).Contains(x, y) {
		return "", "", false
	}
	for memberIndex := len(w.members) - 1; memberIndex >= 0; memberIndex-- {
		memberKey := fmt.Sprintf("%s:%d", key, memberIndex)
		if action, memberKey, ok := scrollActionInWidget(w.members[memberIndex], x, y, memberKey, body, axis); ok {
			return action, memberKey, true
		}
	}
	if w.scrollAction == "" || w.scrollAction == config.ScrollActionNone {
		return "", "", false
	}
	return w.scrollAction, key, true
}

func (b *Bar) axisTargetLocked(x, y int) (clickAction, axisAction, axisKey string, ok bool) {
	clickAction, ok = b.hitLocked(x, y)
	axisAction, axisKey = clickAction, clickAction
	if scrollAction, scrollKey, found := b.scrollActionAtLocked(x, y); found {
		axisAction = barScrollActionPrefix + string(scrollAction)
		axisKey = fmt.Sprintf("%s:%s:%s", barScrollActionPrefix, scrollKey, scrollAction)
		ok = true
	}
	return
}

// tooltipAt reports the tooltip text and bounds under a point.
func (b *Bar) tooltipAt(x, y int) (string, *ui.Node, ui.Rect, bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.tooltipAtLocked(x, y)
}

func (b *Bar) tooltipAtLocked(x, y int) (string, *ui.Node, ui.Rect, bool) {
	body, axis := b.bodyLocked(b.configured.width, b.configured.height), b.barAxis()
	for _, section := range b.widgets() {
		for _, w := range section {
			if w.node != nil && visibleWidgetNode(w) == nil {
				continue
			}
			for _, m := range w.members {
				if visibleWidgetNode(m) == nil {
					continue
				}
				if tip := widgetTooltip(m); (tip != "" || m.tooltipTree() != nil) && barTargetBounds(m.node.Bounds, body, axis).Contains(x, y) {
					return tip, m.tooltipTree(), m.node.Bounds, true
				}
			}
			if w.node == nil {
				continue
			}
			if tip := widgetTooltip(w); (tip != "" || w.tooltipTree() != nil) && barTargetBounds(w.node.Bounds, body, axis).Contains(x, y) {
				return tip, w.tooltipTree(), w.node.Bounds, true
			}
		}
	}
	for _, node := range b.trayNodes {
		if node.Tooltip != "" && barTargetBounds(node.Bounds, body, axis).Contains(x, y) {
			return node.Tooltip, nil, node.Bounds, true
		}
	}
	return "", nil, ui.Rect{}, false
}

func widgetTooltip(w textWidget) string {
	if w.node != nil && w.node.Tooltip != "" {
		return w.node.Tooltip
	}
	if w.inner != nil && w.inner.Tooltip != "" {
		return w.inner.Tooltip
	}
	return w.tooltip
}

func (b *Bar) hoverTooltip() (string, *ui.Node, ui.Rect, bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if !b.inside {
		return "", nil, ui.Rect{}, false
	}
	return b.tooltipAtLocked(b.hoverAt.x, b.hoverAt.y)
}

// Handle applies a pointer event and reports whether the model changed. A click
// counts only when the press and the release land on the same node, so the
// press target is recorded and compared on release.
func (b *Bar) Handle(event wayland.Event) bool {
	b.mu.Lock()

	switch event.Kind {
	case wayland.EventPointerEnter, wayland.EventPointerMotion:
		// Pointer coordinates carry sub-pixel precision; hit testing works in
		// whole logical pixels, so they are floored here and nowhere else.
		b.hoverAt.x = int(math.Floor(event.X))
		b.hoverAt.y = int(math.Floor(event.Y))
		b.inside = true
		// Keep partial wheel deltas with the same resolved item/action target.
		action, _, axisKey, _ := b.axisTargetLocked(b.hoverAt.x, b.hoverAt.y)
		if b.axisValue120Action != axisKey {
			b.axisValue120Action = ""
			b.axisValue120Remainder = 0
		}
		changed := b.pointer.setHover(action)
		b.mu.Unlock()
		return changed

	case wayland.EventPointerLeave:
		b.inside = false
		b.pressed = ""
		b.axisValue120Action = ""
		b.axisValue120Remainder = 0
		changed := b.pointer.clear()
		b.mu.Unlock()
		return changed

	case wayland.EventPointerPress:
		if !b.inside {
			b.mu.Unlock()
			return false
		}
		action, ok := b.hitLocked(b.hoverAt.x, b.hoverAt.y)
		if ok {
			b.pressed = action
			b.pointer.setPress(action)
		}
		pluginFn := b.onPlugin
		b.mu.Unlock()
		if ok && pluginFn != nil {
			if _, isPlugin := parsePluginAction(action); isPlugin {
				return pluginFn(action, event)
			}
		}
		return false

	case wayland.EventPointerRelease:
		pressed := b.pressed
		pluginFn := b.onPlugin
		b.pressed = ""
		b.pointer.setPress("")
		if pressed == "" || !b.inside {
			b.mu.Unlock()
			return false
		}
		action, ok := b.hitLocked(b.hoverAt.x, b.hoverAt.y)
		if !ok || action != pressed {
			b.mu.Unlock()
			return false
		}
		if _, isPlugin := parsePluginAction(action); isPlugin {
			b.mu.Unlock()
			if pluginFn == nil {
				return false
			}
			return pluginFn(action, event)
		}
		gesture, isTray := b.trayGestureLocked(action)
		if !isTray {
			fn := b.onAction
			b.mu.Unlock()
			if fn == nil {
				return false
			}
			return fn(action, event.Button)
		}
		b.mu.Unlock()
		return gesture.deliver(event)

	case wayland.EventPointerAxis:
		// A wheel has no press to pair with, so it acts where it lands.
		action, axisAction, axisKey, ok := b.axisTargetLocked(b.hoverAt.x, b.hoverAt.y)
		if !ok || !b.inside {
			b.mu.Unlock()
			return false
		}
		delta := int(event.AxisDiscrete)
		if delta != 0 {
			// Discrete steps remain authoritative when a compositor supplies them.
			b.axisValue120Action = ""
			b.axisValue120Remainder = 0
		} else if event.AxisValue120 != 0 {
			if b.axisValue120Action != axisKey {
				b.axisValue120Action = axisKey
				b.axisValue120Remainder = 0
			}
			b.axisValue120Remainder += int64(event.AxisValue120)
			delta = int(b.axisValue120Remainder / 120)
			b.axisValue120Remainder %= 120
		}
		axisFn := b.onAxis
		gesture, isTray := b.trayGestureLocked(action)
		b.mu.Unlock()
		if axisFn != nil && axisFn(axisAction, delta) {
			return true
		}
		// The overflow control does not scroll: only an item forwards a wheel.
		if !isTray || gesture.key.IsZero() {
			return false
		}
		return gesture.deliver(event)
	}
	b.mu.Unlock()
	return false
}

// trayGesture is one resolved tray target, copied out from under the bar lock.
//
// The handler takes the registry lock and the registry takes the bar lock, so
// delivering under b.mu would invert the order and deadlock. Everything the
// handler needs is copied out first and the lock is released before the call.
type trayGesture struct {
	key      tray.ItemKey
	arranged trayArrangement
	anchor   ui.Rect
	fn       func(tray.ItemKey, trayArrangement, ui.Rect, wayland.Event) bool
}

func (g trayGesture) deliver(event wayland.Event) bool {
	if g.fn == nil {
		return false
	}
	return g.fn(g.key, g.arranged, g.anchor, event)
}

// trayGestureLocked resolves one action to its tray target. A zero key names
// the overflow control; the second return distinguishes that from an action
// this bar does not own at all.
func (b *Bar) trayGestureLocked(action string) (trayGesture, bool) {
	key, isItem := b.trayActions[action]
	if !isItem && action != trayDrawerAction {
		return trayGesture{}, false
	}
	gesture := trayGesture{key: key, arranged: b.trayArranged, fn: b.onTray}
	for _, node := range b.trayNodes {
		if node.Action == action {
			gesture.anchor = node.Bounds
			break
		}
	}
	return gesture, true
}

// barStyle is the bar's painter style: the theme's roles plus the bar's own
// unit scale.
func barStyle(theme Theme) render.Style {
	style := theme.Style()
	style.Scale120 = ui.ScaleUnit
	if theme.BarTextSize > 0 && theme.BarTextSize != style.Size {
		applyBarLabelSize(&style.Type, theme.BarTextSize)
		style.Size = theme.BarTextSize
	}
	// An attached bar meets the screen along its edge and curves into the
	// screen's sides at both ends, the shape blurShape publishes.
	if g := theme.barGeometry(); g.Attached() {
		style.AttachEdge = g.Edge
		style.EdgeFillet, style.EdgeLeft, style.EdgeRight = theme.Fillet, true, true
	}
	// A translucent pill is lifted toward the foreground by (1 - alpha) x 0.3,
	// scaled by the inverse wallpaper luminance, so it stays distinct from the
	// translucent ground behind it (design D3).
	if a := theme.PillAlpha; a > 0 && a < 0xff {
		lift := 0.3 * (1 - float64(a)/255) * (1 - theme.WallpaperLuminance)
		style.Capsule = render.LerpColor(style.Capsule, style.Foreground, lift)
		style.Capsule.A = a
	}
	return style
}
