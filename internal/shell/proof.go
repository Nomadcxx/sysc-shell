// Package shell holds the proof application: the model, its retained tree, and
// the projection from Niri state into that tree.
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

// toggleAction names the button's action.
const toggleAction = "toggle-meter"

// Meter values the button toggles between.
const (
	meterLow  = 0.25
	meterHigh = 0.75
)

// Proof owns the model, the retained tree, the text renderer and style, and one
// buffered invalidation channel.
//
// UpdateNiri, Handle, and rendering arrive from different goroutines. The mutex
// is held only while copying or changing state; shaping and painting happen
// after the state has been copied out.
type Proof struct {
	mu        sync.Mutex
	workspace string
	toggled   bool
	pressed   string
	hover     ui.Rect
	hoverAt   struct{ x, y int }
	inside    bool

	// Sections are arranged by ui.ArrangeBar into absolute bounds, so painting
	// and hit testing walk them as one flat list.
	left, center, right []*ui.Node

	theme  Theme
	label  *ui.Node
	meter  *ui.Node
	button *ui.Node

	text  *render.TextRenderer
	style render.ProofStyle

	invalidations chan struct{}
}

// New builds a bar with the Milestone 2 fixture: a static name on the left, the
// output's workspace in the centre, and a meter plus a toggle on the right.
//
// The fixture reuses the proof's node kinds and adds none. It exercises
// alignment, truncation, hit testing and redraw across all three sections
// without a widget framework.
func New() (*Proof, error) {
	return NewWithTheme(DefaultTheme(), config.Default().Bar)
}

// NewWithTheme builds a bar from resolved theme tokens and item ids.
func NewWithTheme(theme Theme, policy config.Bar) (*Proof, error) {
	if err := theme.Valid(); err != nil {
		return nil, err
	}
	fonts, err := render.NewSystemFontMap(policy.FontFamily, render.DefaultFontCacheDir())
	if err != nil {
		return nil, err
	}

	p := &Proof{
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

	p.label = &ui.Node{Kind: ui.KindText, Text: p.workspaceLabelLocked()}
	p.meter = &ui.Node{Kind: ui.KindMeter, Width: 120, Value: meterLow}
	p.button = &ui.Node{Kind: ui.KindButton, Text: "Toggle", Padding: 2, Action: toggleAction}

	p.left = p.build(policy.Left)
	p.center = p.build(policy.Center)
	p.right = p.build(policy.Right)
	return p, nil
}

// build turns configured item ids into nodes. Ids are validated at load, so an
// unknown id cannot reach here.
func (p *Proof) build(ids []string) []*ui.Node {
	out := make([]*ui.Node, 0, len(ids))
	for _, id := range ids {
		switch id {
		case "shell-name":
			out = append(out, &ui.Node{Kind: ui.KindText, Text: "sysc-shell"})
		case "workspace":
			out = append(out, p.label)
		case "meter":
			out = append(out, p.meter)
		case "toggle":
			out = append(out, p.button)
		}
	}
	return out
}

// sections returns the three sections in paint order.
func (p *Proof) sections() [][]*ui.Node { return [][]*ui.Node{p.left, p.center, p.right} }

// Invalidations is the channel the Wayland owner receives from. The proof owns
// it and never closes it.
func (p *Proof) Invalidations() <-chan struct{} { return p.invalidations }

// invalidate requests one coalesced redraw.
func (p *Proof) invalidate() {
	select {
	case p.invalidations <- struct{}{}:
	default:
	}
}

// SetWorkspace records this output's workspace label and reports whether it
// changed. The Registry owns invalidation, because it knows which connector the
// redraw belongs to.
func (p *Proof) SetWorkspace(label string) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	if label == p.workspace {
		return false
	}
	p.workspace = label
	return true
}

// UpdateNiri applies a workspace snapshot, invalidating only on a real change.
func (p *Proof) UpdateNiri(snapshot niri.Snapshot) {
	if p.SetWorkspace(activeWorkspace(snapshot)) {
		p.invalidate()
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

func (p *Proof) workspaceLabelLocked() string { return "Workspace: " + p.workspace }

// WorkspaceLabel reports the rendered workspace text.
func (p *Proof) WorkspaceLabel() string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.workspaceLabelLocked()
}

// MeterValue reports the current meter fill.
func (p *Proof) MeterValue() float64 {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.meter.Value
}

// ButtonBounds reports the button's arranged logical bounds.
func (p *Proof) ButtonBounds() ui.Rect {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.button.Bounds
}

// Layout arranges the three sections at the logical configure size.
func (p *Proof) Layout(width, height int) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.layoutLocked(width, height)
}

// contentLocked derives the content band from the theme tokens. The surface is
// the configure size; the body is that surface inset by the gap on the anchored
// edge and both ends; the content is the body inset by the padding.
func (p *Proof) contentLocked(width, height int) ui.Rect {
	body := p.bodyLocked(width, height)
	pad := p.theme.BarPadding
	return ui.Rect{
		X: body.X + pad, Y: body.Y + pad,
		W: max(0, body.W-2*pad), H: max(0, body.H-2*pad),
	}
}

func (p *Proof) bodyLocked(width, height int) ui.Rect {
	gap := p.theme.BarGap
	return ui.Rect{X: gap, Y: gap, W: max(0, width-2*gap), H: max(0, height-gap)}
}

func (p *Proof) layoutLocked(width, height int) error {
	p.label.Text = p.workspaceLabelLocked()
	measure := func(s string) (int, int) {
		w, h, err := p.text.Measure(s, p.style.Size)
		if err != nil {
			return 0, 0
		}
		return w, h
	}
	return ui.ArrangeBar(p.contentLocked(width, height),
		p.left, p.center, p.right, p.theme.Spacing, measure)
}

// Configure records a new logical size and scale from the Wayland owner.
func (p *Proof) Configure(logicalWidth, logicalHeight, scale120 int) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	scale := ui.Scale120(scale120)
	if !scale.Valid() {
		return fmt.Errorf("shell: scale120 %d is not usable", scale120)
	}
	p.style.Scale120 = scale
	p.style.Body = p.bodyLocked(logicalWidth, logicalHeight)
	p.style.Radius = p.theme.Radius
	return p.layoutLocked(logicalWidth, logicalHeight)
}

// Render paints the arranged tree into the physical buffer.
func (p *Proof) Render(pixels []byte, width, height, stride int) error {
	canvas, err := render.NewCanvas(pixels, width, height, stride)
	if err != nil {
		return err
	}

	p.mu.Lock()
	root, style := p.renderViewLocked()
	p.mu.Unlock()

	return render.Paint(canvas, root, p.text, style)
}

// renderViewLocked copies the mutable values painting reads, so the Niri
// goroutine can update the model while shaping and rasterization run.
//
// The three sections are already arranged into absolute bounds, so they flatten
// into one child list: the painter walks bounds, not structure.
func (p *Proof) renderViewLocked() (*ui.Node, render.ProofStyle) {
	p.label.Text = p.workspaceLabelLocked()
	root := &ui.Node{Kind: ui.KindRow}
	for _, section := range p.sections() {
		for _, n := range section {
			root.Children = append(root.Children, copyNode(n))
		}
	}
	return root, p.style
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
func (p *Proof) hitLocked(x, y int) (string, bool) {
	sections := p.sections()
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
func (p *Proof) Handle(event wayland.Event) bool {
	p.mu.Lock()
	defer p.mu.Unlock()

	switch event.Kind {
	case wayland.EventPointerEnter, wayland.EventPointerMotion:
		// Pointer coordinates carry sub-pixel precision; hit testing works in
		// whole logical pixels, so they are floored here and nowhere else.
		p.hoverAt.x = int(math.Floor(event.X))
		p.hoverAt.y = int(math.Floor(event.Y))
		p.inside = true
		return false

	case wayland.EventPointerLeave:
		p.inside = false
		p.pressed = ""
		return false

	case wayland.EventPointerPress:
		if !p.inside {
			return false
		}
		action, ok := p.hitLocked(p.hoverAt.x, p.hoverAt.y)
		if ok {
			p.pressed = action
		}
		return false

	case wayland.EventPointerRelease:
		pressed := p.pressed
		p.pressed = ""
		if pressed == "" || !p.inside {
			return false
		}
		action, ok := p.hitLocked(p.hoverAt.x, p.hoverAt.y)
		if !ok || action != pressed {
			return false
		}
		return p.activateLocked(action)
	}
	return false
}

// activateLocked applies an action and reports whether state changed.
func (p *Proof) activateLocked(action string) bool {
	if action != toggleAction {
		return false
	}
	p.toggled = !p.toggled
	p.style.Toggled = p.toggled
	if p.toggled {
		p.meter.Value = meterHigh
	} else {
		p.meter.Value = meterLow
	}
	return true
}
