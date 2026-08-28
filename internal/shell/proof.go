// Package shell holds the proof application: the model, its retained tree, and
// the projection from Niri state into that tree.
package shell

import (
	"fmt"
	"strconv"
	"sync"

	"github.com/Nomadcxx/sysc-shell/internal/platform/niri"
	"github.com/Nomadcxx/sysc-shell/internal/platform/wayland"
	"github.com/Nomadcxx/sysc-shell/internal/render"
	"github.com/Nomadcxx/sysc-shell/internal/ui"
	"golang.org/x/image/font/gofont/goregular"
)

// BarHeight is the logical height and exclusive zone of the proof bar.
const BarHeight = 48

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

	root   *ui.Node
	label  *ui.Node
	meter  *ui.Node
	button *ui.Node

	text  *render.TextRenderer
	style render.ProofStyle

	invalidations chan struct{}
}

// New builds the proof with its fixed tree:
//
//	row padding=12 gap=12
//	├── text "sysc-shell"
//	├── text "Workspace: <value>"
//	├── meter width=120 value=0.25|0.75
//	└── button action="toggle-meter" text="Toggle"
func New() (*Proof, error) {
	face, err := render.ParseFace(goregular.TTF)
	if err != nil {
		return nil, err
	}

	p := &Proof{
		workspace:     "-",
		text:          render.NewTextRenderer(face),
		invalidations: make(chan struct{}, 1),
		style: render.ProofStyle{
			Size:       16,
			Scale120:   ui.ScaleUnit,
			Background: render.Color{R: 0x10, G: 0x14, B: 0x18, A: 0xff},
			Foreground: render.Color{R: 0xe8, G: 0xec, B: 0xf0, A: 0xff},
			Track:      render.Color{R: 0x30, G: 0x34, B: 0x38, A: 0xff},
			Accent:     render.Color{R: 0x00, G: 0x80, B: 0xff, A: 0xff},
			AccentOn:   render.Color{R: 0xff, G: 0x60, B: 0x00, A: 0xff},
		},
	}

	p.label = &ui.Node{Kind: ui.KindText, Text: p.workspaceLabelLocked()}
	p.meter = &ui.Node{Kind: ui.KindMeter, Width: 120, Value: meterLow}
	// The row's fixed padding of 12 leaves 48-2*12 = 24 logical pixels of
	// content height, and text at size 16 measures 20 tall, so the button's
	// own padding is capped at 2 and the button fills the content band.
	p.button = &ui.Node{Kind: ui.KindButton, Text: "Toggle", Padding: 2, Action: toggleAction}
	p.root = &ui.Node{
		Kind:    ui.KindRow,
		Padding: 12,
		Gap:     12,
		Children: []*ui.Node{
			{Kind: ui.KindText, Text: "sysc-shell"},
			p.label,
			p.meter,
			p.button,
		},
	}
	return p, nil
}

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

// UpdateNiri applies a workspace snapshot, invalidating only on a real change.
func (p *Proof) UpdateNiri(snapshot niri.Snapshot) {
	label := activeWorkspace(snapshot)

	p.mu.Lock()
	changed := label != p.workspace
	if changed {
		p.workspace = label
	}
	p.mu.Unlock()

	if changed {
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

// Root exposes the arranged tree.
func (p *Proof) Root() *ui.Node { return p.root }

// ButtonBounds reports the button's arranged logical bounds.
func (p *Proof) ButtonBounds() ui.Rect {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.button.Bounds
}

// Layout arranges the tree at the logical configure size.
func (p *Proof) Layout(width, height int) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.layoutLocked(width, height)
}

func (p *Proof) layoutLocked(width, height int) error {
	measure := func(s string) (int, int) {
		w, h, err := p.text.Measure(s, p.style.Size)
		if err != nil {
			return 0, 0
		}
		return w, h
	}
	return ui.Layout(p.root, ui.Rect{W: width, H: height}, measure)
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

// renderViewLocked copies the mutable values painting reads so the Niri
// goroutine can update the model while shaping and rasterization run.
func (p *Proof) renderViewLocked() (*ui.Node, render.ProofStyle) {
	root := *p.root
	root.Children = make([]*ui.Node, len(p.root.Children))
	// ponytail: The proof tree is one level; use a recursive snapshot when a
	// nested shell component becomes a real consumer.
	for i, child := range p.root.Children {
		copy := *child
		if child == p.label {
			copy.Text = p.workspaceLabelLocked()
		}
		root.Children[i] = &copy
	}
	return &root, p.style
}

// Handle applies a pointer event and reports whether the model changed. A click
// counts only when the press and the release land on the same node, so the
// press target is recorded and compared on release.
func (p *Proof) Handle(event wayland.Event) bool {
	p.mu.Lock()
	defer p.mu.Unlock()

	switch event.Kind {
	case wayland.EventPointerEnter, wayland.EventPointerMotion:
		p.hoverAt.x, p.hoverAt.y = event.X, event.Y
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
		action, ok := ui.Hit(p.root, p.hoverAt.x, p.hoverAt.y)
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
		action, ok := ui.Hit(p.root, p.hoverAt.x, p.hoverAt.y)
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
