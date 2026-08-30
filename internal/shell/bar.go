// Package shell holds the bar model: its retained tree, its widgets, and the
// projection from service and Niri state into that tree.
package shell

import (
	"fmt"
	"math"
	"strconv"
	"sync"

	"github.com/Nomadcxx/sysc-shell/internal/config"
	"github.com/Nomadcxx/sysc-shell/internal/platform/niri"
	"github.com/Nomadcxx/sysc-shell/internal/platform/wayland"
	"github.com/Nomadcxx/sysc-shell/internal/render"
	"github.com/Nomadcxx/sysc-shell/internal/ui"
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
	mu        sync.Mutex
	workspace string
	pressed   string
	hover     ui.Rect
	hoverAt   struct{ x, y int }
	inside    bool

	// Sections are arranged by ui.ArrangeBar into absolute bounds, so painting
	// and hit testing walk them as one flat list.
	left, center, right []*ui.Node

	theme Theme
	label *ui.Node

	text  *render.TextRenderer
	style render.ProofStyle

	invalidations chan struct{}
}

// New builds a bar from the default theme and the default item set.
func New() (*Bar, error) {
	return NewWithTheme(DefaultTheme(), config.Default().Bar)
}

// NewWithTheme builds a bar from resolved theme tokens and item ids.
func NewWithTheme(theme Theme, policy config.Bar) (*Bar, error) {
	if err := theme.Valid(); err != nil {
		return nil, err
	}
	fonts, err := render.NewSystemFontMap(policy.FontFamily, render.DefaultFontCacheDir())
	if err != nil {
		return nil, err
	}

	b := &Bar{
		workspace:     "-",
		theme:         theme,
		text:          render.NewTextRendererWithFontMap(fonts),
		invalidations: make(chan struct{}, 1),
		style: render.ProofStyle{
			Size:       theme.TextSize,
			Scale120:   ui.ScaleUnit,
			Background: theme.Background,
			Foreground: theme.Foreground,
			Track:      theme.Muted,
			Accent:     theme.Accent,
			AccentOn:   theme.Error,
		},
	}

	b.label = &ui.Node{Kind: ui.KindText, Text: b.workspaceLabelLocked()}

	b.left = b.build(policy.Left)
	b.center = b.build(policy.Center)
	b.right = b.build(policy.Right)
	return b, nil
}

// build turns configured items into nodes. Ids are validated at load, so an
// unknown id cannot reach here.
func (b *Bar) build(items []config.Item) []*ui.Node {
	out := make([]*ui.Node, 0, len(items))
	for _, item := range items {
		switch item.ID {
		case "workspace":
			out = append(out, b.label)
		case "clock":
			out = append(out, &ui.Node{Kind: ui.KindText})
		case "window-title":
			out = append(out, &ui.Node{Kind: ui.KindText, MaxWidth: item.MaxWidth})
		}
	}
	return out
}

// sections returns the three sections in paint order.
func (b *Bar) sections() [][]*ui.Node { return [][]*ui.Node{b.left, b.center, b.right} }

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

// SetWorkspace records this output's workspace label and reports whether it
// changed. The Registry owns invalidation, because it knows which connector the
// redraw belongs to.
func (b *Bar) SetWorkspace(label string) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	if label == b.workspace {
		return false
	}
	b.workspace = label
	return true
}

// UpdateNiri applies a workspace snapshot, invalidating only on a real change.
func (b *Bar) UpdateNiri(snapshot niri.Snapshot) {
	if b.SetWorkspace(activeWorkspace(snapshot)) {
		b.invalidate()
	}
}

// activeWorkspace names the focused workspace, preferring its name over its
// index.
func activeWorkspace(snapshot niri.Snapshot) string {
	for _, w := range snapshot.Workspaces {
		if !w.Focused {
			continue
		}
		if w.Name != "" {
			return w.Name
		}
		return strconv.Itoa(w.Index)
	}
	return "-"
}

func (b *Bar) workspaceLabelLocked() string { return "Workspace: " + b.workspace }

// WorkspaceLabel reports the rendered workspace text.
func (b *Bar) WorkspaceLabel() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.workspaceLabelLocked()
}

// Layout arranges the three sections at the logical configure size.
func (b *Bar) Layout(width, height int) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.layoutLocked(width, height)
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
	b.label.Text = b.workspaceLabelLocked()
	measure := func(s string) (int, int) {
		w, h, err := b.text.Measure(s, b.style.Size)
		if err != nil {
			return 0, 0
		}
		return w, h
	}
	return ui.ArrangeBar(b.contentLocked(width, height),
		b.left, b.center, b.right, b.theme.Spacing, measure)
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
	return b.layoutLocked(logicalWidth, logicalHeight)
}

// Render paints the arranged tree into the physical buffer.
func (b *Bar) Render(pixels []byte, width, height, stride int) error {
	canvas, err := render.NewCanvas(pixels, width, height, stride)
	if err != nil {
		return err
	}

	b.mu.Lock()
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
	b.label.Text = b.workspaceLabelLocked()
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

// Handle applies a pointer event and reports whether the model changed. A click
// counts only when the press and the release land on the same node, so the
// press target is recorded and compared on release.
func (b *Bar) Handle(event wayland.Event) bool {
	b.mu.Lock()
	defer b.mu.Unlock()

	switch event.Kind {
	case wayland.EventPointerEnter, wayland.EventPointerMotion:
		// Pointer coordinates carry sub-pixel precision; hit testing works in
		// whole logical pixels, so they are floored here and nowhere else.
		b.hoverAt.x = int(math.Floor(event.X))
		b.hoverAt.y = int(math.Floor(event.Y))
		b.inside = true
		return false

	case wayland.EventPointerLeave:
		b.inside = false
		b.pressed = ""
		return false

	case wayland.EventPointerPress:
		if !b.inside {
			return false
		}
		action, ok := b.hitLocked(b.hoverAt.x, b.hoverAt.y)
		if ok {
			b.pressed = action
		}
		return false

	case wayland.EventPointerRelease:
		pressed := b.pressed
		b.pressed = ""
		if pressed == "" || !b.inside {
			return false
		}
		action, ok := b.hitLocked(b.hoverAt.x, b.hoverAt.y)
		if !ok || action != pressed {
			return false
		}
		return b.activateLocked(action)
	}
	return false
}

// activateLocked applies an action and reports whether state changed. No
// Tranche 3A node carries an action, so this is inert at runtime; the pointer
// path stays covered by tests and ready for Milestone 4 controls.
func (b *Bar) activateLocked(action string) bool { return false }
