// Package shell holds the bar model: its retained tree, its widgets, and the
// projection from service and Niri state into that tree.
package shell

import (
	"fmt"
	"math"
	"os"
	"slices"
	"strings"
	"sync"

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
	onTray        func(tray.ItemKey, trayArrangement, ui.Rect, wayland.Event) bool
	onPlugin      func(string, wayland.Event) bool

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
	style render.ProofStyle

	invalidations chan struct{}
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
		text:          render.NewTextRendererWithFontMap(fonts),
		invalidations: make(chan struct{}, 1),
		style: render.ProofStyle{
			Size:        theme.TextSize,
			Scale120:    ui.ScaleUnit,
			Background:  theme.Background,
			Foreground:  theme.Foreground,
			Track:       theme.Muted,
			Accent:      theme.Accent,
			AccentOn:    theme.Error,
			Error:       theme.Error,
			OnPrimary:   theme.OnPrimary,
			Radius:      theme.Radius,
			Capsule:     theme.Capsule,
			Container:   theme.Container,
			OnAccent:    theme.OnAccent,
			OnContainer: theme.OnContainer,
		},
	}

	b.left = buildWidgets(policy.Left, b.theme.CapsulePadding)
	b.center = buildWidgets(policy.Center, b.theme.CapsulePadding)
	b.right = buildWidgets(policy.Right, b.theme.CapsulePadding)
	return b, nil
}

// connector reports the output this bar renders for.
func (b *Bar) connector() string { return b.conn }

// widgets returns the three sections in paint order.
func (b *Bar) widgets() [][]textWidget { return [][]textWidget{b.left, b.center, b.right} }

// sections returns the retained nodes in paint order, for layout and painting.
func (b *Bar) sections() [][]*ui.Node {
	out := make([][]*ui.Node, 0, 3)
	for _, section := range b.widgets() {
		nodes := make([]*ui.Node, 0, len(section))
		for _, w := range section {
			nodes = append(nodes, w.node)
		}
		out = append(out, nodes)
	}
	out[2] = append(out[2], b.trayNodes...)
	return out
}

func (b *Bar) setTray(items []tray.Item, prefs config.TrayPreferences, images map[tray.ItemKey]*ui.Image) {
	b.mu.Lock()
	b.trayItems = append([]tray.Item(nil), items...)
	b.trayPrefs = config.TrayPreferences{
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

// trayArrangement re-derives the split from the current items at the width the
// last layout granted. The drawer reads it, so a drawer opened or refreshed
// between frames shows the same overflow the bar will.
func (b *Bar) trayArrangement() trayArrangement {
	b.mu.Lock()
	defer b.mu.Unlock()
	return arrangeTray(b.trayItems, b.trayPrefs, b.trayAvailable, trayItemSize, b.theme.Spacing)
}

// scale120 reports the output scale the bar was last configured at, in 120ths.
func (b *Bar) scale120() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return int(b.style.Scale120)
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
			// State lives on the inner node; the capsule is chrome.
			before := *w.inner
			if text := w.format(view); text != w.inner.Text {
				w.inner.Text = text
				changed = true
			}
			if w.inner.Value != before.Value || w.inner.Absent != before.Absent ||
				w.inner.Tone != before.Tone ||
				!slices.Equal(w.inner.Values, before.Values) {
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
	pad := b.theme.BarPadding
	return ui.Rect{
		X: body.X + pad, Y: body.Y + pad,
		W: max(0, body.W-2*pad), H: max(0, body.H-2*pad),
	}
}

func (b *Bar) bodyLocked(width, height int) ui.Rect {
	gap := b.theme.BarGap
	return ui.Rect{X: gap, Y: gap, W: max(0, width-2*gap), H: max(0, height-gap)}
}

func (b *Bar) layoutLocked(width, height int) error {
	// Shaping for paint happens at the physical size, so measuring at the
	// logical size and scaling the result up assumes glyph advances are linear
	// in point size. They are not: at scale 1.25 the painter shaped text wider
	// than layout had reserved and ellipsized a clock that fits.
	//
	// Measure at the size the painter will actually use, then convert back up.
	size := b.style.Scale120.Physical(b.style.Size)
	if size <= 0 {
		size = b.style.Size
	}
	measure := func(s string, tabular bool) (int, int) {
		w, h, err := b.text.Measure(s, size, tabular)
		if err != nil {
			return 0, 0
		}
		return b.style.Scale120.Logical(w), b.style.Scale120.Logical(h)
	}
	b.trayNodes = nil
	sections := b.sections()
	content := b.contentLocked(width, height)
	if err := ui.ArrangeBar(content,
		sections[0], sections[1], sections[2], b.theme.Spacing, measure); err != nil {
		return err
	}
	available := b.trayAvailableLocked(content, sections[1], sections[2])
	arranged := arrangeTray(b.trayItems, b.trayPrefs, available, trayItemSize, b.theme.Spacing)
	if len(arranged.Overflow) > 0 || len(arranged.Hidden) > 0 {
		reserve := trayItemSize
		if len(sections[2]) > 0 || available > trayItemSize {
			reserve += b.theme.Spacing
		}
		available = max(0, available-reserve)
		arranged = arrangeTray(b.trayItems, b.trayPrefs, available, trayItemSize, b.theme.Spacing)
	}
	b.trayArranged, b.trayAvailable = arranged, available
	b.rebuildTrayNodesLocked()
	sections = b.sections()
	return ui.ArrangeBar(content, sections[0], sections[1], sections[2], b.theme.Spacing, measure)
}

func (b *Bar) trayAvailableLocked(content ui.Rect, center, right []*ui.Node) int {
	start := content.X + content.W/2
	if len(center) > 0 {
		last := center[len(center)-1].Bounds
		start = last.X + last.W
	}
	if len(center) > 0 || len(right) > 0 {
		start += b.theme.Spacing
	}
	used := 0
	if len(right) > 0 {
		used = content.X + content.W - right[0].Bounds.X
		used += b.theme.Spacing
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
			Kind: ui.KindButton, Text: "…", Padding: 3, Action: trayDrawerAction,
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
func (b *Bar) renderViewLocked() (*ui.Node, render.ProofStyle) {
	root := &ui.Node{Kind: ui.KindRow}
	for _, section := range b.sections() {
		for _, n := range section {
			root.Children = append(root.Children, copyNode(n))
		}
	}
	return root, b.style
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
	if len(n.Children) > 0 {
		c.Children = make([]*ui.Node, len(n.Children))
		for i, child := range n.Children {
			c.Children[i] = copyNode(child)
		}
	}
	return &c
}

// hitLocked searches every section in reverse paint order.
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
func (b *Bar) tooltipAt(x, y int) (string, ui.Rect, bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.tooltipAtLocked(x, y)
}

// tooltipAtLocked reports the tooltip text and bounds under a point.
func (b *Bar) tooltipAtLocked(x, y int) (string, ui.Rect, bool) {
	for _, section := range b.widgets() {
		for _, w := range section {
			// A group's own node covers every member, so members are tried
			// first or the group would answer for all of them.
			for _, m := range w.members {
				if m.tooltip != "" && m.node.Bounds.Contains(x, y) {
					return m.tooltip, m.node.Bounds, true
				}
			}
			if w.tooltip != "" && w.node.Bounds.Contains(x, y) {
				return w.tooltip, w.node.Bounds, true
			}
		}
	}
	for _, node := range b.trayNodes {
		if node.Tooltip != "" && node.Bounds.Contains(x, y) {
			return node.Tooltip, node.Bounds, true
		}
	}
	return "", ui.Rect{}, false
}

// hoverTooltip reports the tooltip under the pointer after Handle has recorded
// the latest coordinates.
func (b *Bar) hoverTooltip() (string, ui.Rect, bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if !b.inside {
		return "", ui.Rect{}, false
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
		b.mu.Unlock()
		return false

	case wayland.EventPointerLeave:
		b.inside = false
		b.pressed = ""
		b.mu.Unlock()
		return false

	case wayland.EventPointerPress:
		if !b.inside {
			b.mu.Unlock()
			return false
		}
		action, ok := b.hitLocked(b.hoverAt.x, b.hoverAt.y)
		if ok {
			b.pressed = action
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
			changed := b.activateLocked(action)
			b.mu.Unlock()
			return changed
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
		gesture, isTray := b.trayGestureLocked(action)
		b.mu.Unlock()
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

// activateLocked applies a non-tray action and reports whether state changed.
// No built-in widget carries an action, so this is inert at runtime; the
// pointer path stays covered by tests and ready for future bar controls.
func (b *Bar) activateLocked(string) bool { return false }
