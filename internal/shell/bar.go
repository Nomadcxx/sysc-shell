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

// Bar owns the model, the retained tree, the text renderer and style, and one
// buffered invalidation channel, for exactly one output.
//
// UpdateNiri, Handle, and rendering arrive from different goroutines. The mutex
// is held only while copying or changing state; shaping and painting happen
// after the state has been copied out.
type Bar struct {
	mu      sync.Mutex
	pressed string
	hover   ui.Rect
	hoverAt struct{ x, y int }
	inside  bool

	// Sections are arranged by ui.ArrangeBar into absolute bounds, so painting
	// and hit testing walk them as one flat list.
	left, center, right []textWidget
	trayNodes           []*ui.Node
	trayItems           []tray.Item
	trayPrefs           config.TrayPreferences
	trayImages          map[tray.ItemKey]*ui.Image
	trayActions         map[string]tray.ItemKey
	trayArranged        trayArrangement
	// trayAvailable is the logical width the last layout granted tray icons,
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

	// pointer is the bar's resolved hover/press state as stable keys, and anim
	// is its one clock. Only clickable capsules carry either: a CPU or memory
	// display group has no action, so it never animates.
	pointer  interaction
	anim     *animator
	stopAnim chan struct{}
	stopOnce sync.Once

	invalidations chan struct{}
	mediaWidget   bool
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

	b := &Bar{
		conn:          connector,
		theme:         theme,
		mediaWidget:   hasMediaItem(policy.Left) || hasMediaItem(policy.Center) || hasMediaItem(policy.Right),
		text:          render.NewTextRendererWithFontMap(fonts),
		invalidations: make(chan struct{}, 1),
		style:         barStyle(theme),
		anim:          newAnimator(nil, false, theme.Motion),
		stopAnim:      make(chan struct{}),
	}

	b.left = buildWidgets(policy.Left, b.theme.Metrics.CapsulePadding, b.theme.Metrics)
	b.center = buildWidgets(policy.Center, b.theme.Metrics.CapsulePadding, b.theme.Metrics)
	b.right = buildWidgets(policy.Right, b.theme.Metrics.CapsulePadding, b.theme.Metrics)
	return b, nil
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
		Enabled: prefs.Enabled,
		Hidden:  append([]string(nil), prefs.Hidden...), Pinned: append([]string(nil), prefs.Pinned...),
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
	return arrangeTray(b.trayItems, b.trayPrefs, b.trayAvailable, trayItemSize, b.theme.Metrics.BarSpacing)
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

// Invalidations is the channel the Wayland owner receives from. The proof owns
// it and never closes it.
func (b *Bar) Invalidations() <-chan struct{} { return b.invalidations }

// invalidate requests one coalesced redraw.
func (b *Bar) invalidate() {
	select {
	case b.invalidations <- struct{}{}:
	default:
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
				out = append(out, ui.BlurStrips(ui.SurfaceShape{Body: n.Bounds, Radius: radius})...)
			}
		}
		return out
	}
	shape := ui.SurfaceShape{
		Body:   b.bodyLocked(b.configured.width, b.configured.height),
		Radius: b.theme.Radius,
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
	// The first pass only sizes the tray's available width; the authoritative
	// overflow is the second, after the tray nodes are rebuilt.
	if _, err := ui.ArrangeBar(content,
		sections[0], sections[1], sections[2], b.theme.Metrics.BarSpacing, measure); err != nil {
		return err
	}
	available := b.trayAvailableLocked(content, sections[1], sections[2])
	arranged := arrangeTray(b.trayItems, b.trayPrefs, available, trayItemSize, b.theme.Metrics.BarSpacing)
	if len(arranged.Overflow) > 0 || len(arranged.Hidden) > 0 {
		reserve := trayItemSize
		if len(sections[2]) > 0 || available > trayItemSize {
			reserve += b.theme.Metrics.BarSpacing
		}
		available = max(0, available-reserve)
		arranged = arrangeTray(b.trayItems, b.trayPrefs, available, trayItemSize, b.theme.Metrics.BarSpacing)
	}
	b.trayArranged, b.trayAvailable = arranged, available
	b.rebuildTrayNodesLocked()
	sections = b.sections()
	over, err := ui.ArrangeBar(content, sections[0], sections[1], sections[2], b.theme.Metrics.BarSpacing, measure)
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

func (b *Bar) trayAvailableLocked(content ui.Rect, center, right []*ui.Node) int {
	start := content.X + content.W/2
	if len(center) > 0 {
		last := center[len(center)-1].Bounds
		start = last.X + last.W
	}
	if len(center) > 0 || len(right) > 0 {
		start += b.theme.Metrics.BarSpacing
	}
	used := 0
	if len(right) > 0 {
		used = content.X + content.W - right[0].Bounds.X
		used += b.theme.Metrics.BarSpacing
	}
	return max(0, content.X+content.W-start-used)
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
		b.trayNodes = append(b.trayNodes, &ui.Node{
			Kind: ui.KindButton, Text: "…", Padding: b.theme.Metrics.CapsulePadding, Action: trayDrawerAction,
			Tooltip: "Tray items", Focusable: true, Name: "Tray items", Role: "button",
		})
	}
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

	b.mu.Lock()
	// The arrangement is brought up to date here rather than in apply, because
	// this is the goroutine that owns the font map.
	b.relayoutLocked()
	root, style := b.renderViewLocked()
	b.mu.Unlock()

	return render.Paint(canvas, root, b.text, style)
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
		overflowFades(sections, b.overflow, b.theme.Metrics.StandardControl)...)
	// The painter consumes an immutable mask, so state is resolved onto the
	// copy that is about to be drawn rather than onto live model state.
	b.pointer.apply(root, b.anim)
	// A hover wash marks a control a dialog raises; the bar is furniture, and
	// pointer-over lightening on every clickable pill reads as a popup. The
	// bar paints its clickables at rest. Press survives: it marks a real
	// activation, not a cursor that happens to pass.
	clearHoverLocked(root)
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
	// Resolve the cap once, under the lock: a theme reload can replace the
	// animator, and the call expression below runs unlocked.
	b.mu.Lock()
	frameCap := b.anim.frameCap()
	wake := b.anim.wake
	b.mu.Unlock()
	animateSurfaceResting(b.stopAnim, wake, func() bool {
		b.mu.Lock()
		defer b.mu.Unlock()
		return b.anim.Settled()
	}, b.invalidate, func() time.Duration { return frameCap }, func() (time.Duration, bool) {
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
	sections := b.sections()
	for i := len(sections) - 1; i >= 0; i-- {
		section := sections[i]
		for j := len(section) - 1; j >= 0; j-- {
			if action, ok := ui.Hit(section[j], x, y); ok {
				return action, true
			}
		}
	}
	return "", false
}

// tooltipAt reports the tooltip text and bounds under a point.
func (b *Bar) tooltipAt(x, y int) (string, *ui.Node, ui.Rect, bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.tooltipAtLocked(x, y)
}

func (b *Bar) tooltipAtLocked(x, y int) (string, *ui.Node, ui.Rect, bool) {
	for _, section := range b.widgets() {
		for _, w := range section {
			if w.node != nil && visibleWidgetNode(w) == nil {
				continue
			}
			for _, m := range w.members {
				if visibleWidgetNode(m) == nil {
					continue
				}
				if tip := widgetTooltip(m); (tip != "" || m.tooltipTree() != nil) && m.node.Bounds.Contains(x, y) {
					return tip, m.tooltipTree(), m.node.Bounds, true
				}
			}
			if w.node == nil {
				continue
			}
			if tip := widgetTooltip(w); (tip != "" || w.tooltipTree() != nil) && w.node.Bounds.Contains(x, y) {
				return tip, w.tooltipTree(), w.node.Bounds, true
			}
		}
	}
	for _, node := range b.trayNodes {
		if node.Tooltip != "" && node.Bounds.Contains(x, y) {
			return node.Tooltip, nil, node.Bounds, true
		}
	}
	return "", nil, ui.Rect{}, false
}

func widgetTooltip(w textWidget) string {
	if w.node != nil && w.node.Tooltip != "" {
		return w.node.Tooltip
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
		// A bar item's action is already its stable key. Only a change of
		// resolved target repaints; sliding along one pill costs nothing.
		action, _ := b.hitLocked(b.hoverAt.x, b.hoverAt.y)
		changed := b.pointer.setHover(action)
		b.mu.Unlock()
		return changed

	case wayland.EventPointerLeave:
		b.inside = false
		b.pressed = ""
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
		action, ok := b.hitLocked(b.hoverAt.x, b.hoverAt.y)
		if !ok || !b.inside {
			b.mu.Unlock()
			return false
		}
		axisFn := b.onAxis
		gesture, isTray := b.trayGestureLocked(action)
		b.mu.Unlock()
		delta := int(event.AxisDiscrete)
		if delta == 0 {
			switch {
			case event.AxisValue120 > 0:
				delta = 1
			case event.AxisValue120 < 0:
				delta = -1
			}
		}
		if axisFn != nil && axisFn(action, delta) {
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
	// An attached bar meets the screen along its edge and curves into the
	// screen's sides at both ends, the shape blurShape publishes.
	if g := theme.barGeometry(); g.Attached() {
		style.AttachEdge = g.Edge
		style.EdgeFillet, style.EdgeLeft, style.EdgeRight = theme.Fillet, true, true
	}
	// A translucent pill is lifted toward the foreground by (1 - alpha) x 0.3,
	// so it stays distinct from the translucent ground behind it (design D3).
	if a := theme.PillAlpha; a > 0 && a < 0xff {
		style.Capsule = render.LerpColor(style.Capsule, style.Foreground, 0.3*(1-float64(a)/255))
		style.Capsule.A = a
	}
	return style
}
