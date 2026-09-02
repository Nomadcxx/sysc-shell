package shell

import (
	"strconv"
	"sync"
	"testing"

	"github.com/Nomadcxx/sysc-shell/internal/platform/wayland"
	"github.com/Nomadcxx/sysc-shell/internal/ui"
)

func TestBarConcurrentUpdateAndRender(t *testing.T) {
	p := newTestBar(t)
	if err := p.Configure(600, BarHeight, 120); err != nil {
		t.Fatal(err)
	}

	start := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		<-start
		for i := 0; i < 100_000; i++ {
			p.apply(barView{Workspace: strconv.Itoa(i), Title: "Fixture One"})
		}
	}()
	go func() {
		defer wg.Done()
		<-start
		pixels := make([]byte, 600*BarHeight*4)
		for i := 0; i < 20; i++ {
			if err := p.Render(pixels, 600, BarHeight, 600*4); err != nil {
				t.Error(err)
				return
			}
		}
	}()
	close(start)
	wg.Wait()
}

func newTestBar(t *testing.T) *Bar {
	t.Helper()
	p, err := New("DP-9")
	if err != nil {
		t.Fatal(err)
	}
	return p
}

// drain reports how many invalidations are waiting.
func drain(p *Bar) int {
	count := 0
	for {
		select {
		case <-p.Invalidations():
			count++
		default:
			return count
		}
	}
}

// A bar renders the view it is given. Which view each output gets is the
// Registry's decision and is covered in registry_test.go; the projection
// itself is covered in projection_test.go.
func TestABarRendersTheWorkspaceAndTitleItIsGiven(t *testing.T) {
	t.Parallel()

	p := newTestBar(t)
	view := barView{
		Workspace: "code", Title: "Fixture One",
		Pills: []workspacePill{{Index: 1, Focused: true, Occupied: true}, {Index: 2}},
	}
	if !p.apply(view) {
		t.Fatal("the first view reported no change")
	}

	sections := p.sections()
	if got := pillIndices(sections[0][0]); len(got) != 2 || got[0] != "1" || got[1] != "2" {
		t.Fatalf("workspace pills = %v, want 1 and 2", got)
	}
	if got := nodeText(sections[0][1]); got != "Fixture One" {
		t.Fatalf("title node = %q, want Fixture One", got)
	}
}

// An output Niri has not reported renders a stable fallback rather than an
// empty bar.
func TestABarRendersTheFallbackWorkspace(t *testing.T) {
	t.Parallel()

	p := newTestBar(t)
	p.apply(barView{Workspace: noWorkspace})

	if got := nodeText(p.sections()[0][0]); got != "-" {
		t.Fatalf("workspace node = %q, want the %q fallback", got, noWorkspace)
	}
	if got := nodeText(p.sections()[0][1]); got != "" {
		t.Fatalf("title node = %q, want empty with no window", got)
	}
}

// press and release drive a full click at one point.
func click(p *Bar, x, y int) bool {
	return clickButton(p, x, y, buttonLeft)
}

func clickButton(p *Bar, x, y int, button uint32) bool {
	p.Handle(wayland.Event{Kind: wayland.EventPointerMotion, X: float64(x), Y: float64(y)})
	pressed := p.Handle(wayland.Event{Kind: wayland.EventPointerPress, Button: button})
	released := p.Handle(wayland.Event{Kind: wayland.EventPointerRelease, Button: button})
	return pressed || released
}

// layoutForTest arranges the tree at the proof's fixed height.
func layoutForTest(t *testing.T, p *Bar, width int) {
	t.Helper()
	if err := p.Layout(width, BarHeight); err != nil {
		t.Fatal(err)
	}
}

// withSyntheticAction appends a node carrying an action to the right section.
// Metric widgets now carry one too; this node still covers a click that must
// not open the monitor.
func withSyntheticAction(t *testing.T, p *Bar, width int) ui.Rect {
	t.Helper()
	p.right = append(p.right, textWidget{
		node: &ui.Node{
			Kind: ui.KindButton, Text: "Synthetic", Padding: 4, Action: "synthetic-action",
		},
		format: func(barView) string { return "Synthetic" },
	})
	layoutForTest(t, p, width)
	bounds := p.right[len(p.right)-1].node.Bounds
	if bounds.W <= 0 || bounds.H <= 0 {
		t.Fatalf("synthetic node was not arranged: %+v", bounds)
	}
	return bounds
}

// pressedAction reports the action recorded by the last press.
func pressedAction(p *Bar) string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.pressed
}

func TestBarPressRecordsTheHitAction(t *testing.T) {
	t.Parallel()

	p := newTestBar(t)
	bounds := withSyntheticAction(t, p, 600)

	p.Handle(wayland.Event{
		Kind: wayland.EventPointerMotion,
		X:    float64(bounds.X + bounds.W/2), Y: float64(bounds.Y + bounds.H/2),
	})
	p.Handle(wayland.Event{Kind: wayland.EventPointerPress})

	if got := pressedAction(p); got != "synthetic-action" {
		t.Fatalf("pressed action = %q, want synthetic-action", got)
	}
}

func TestBarPressOutsideEveryActionRecordsNothing(t *testing.T) {
	t.Parallel()

	p := newTestBar(t)
	withSyntheticAction(t, p, 600)
	drain(p)

	if click(p, 1, 1) {
		t.Fatal("a click outside every action reported a change")
	}
	if got := pressedAction(p); got != "" {
		t.Fatalf("pressed action = %q, want none", got)
	}
	if drain(p) != 0 {
		t.Fatal("a click outside every action requested a redraw")
	}
}

// A click counts only when the press and the release land on the same node.
func TestBarReleaseOutsideThePressedNodeIsNotAClick(t *testing.T) {
	t.Parallel()

	p := newTestBar(t)
	bounds := withSyntheticAction(t, p, 600)

	p.Handle(wayland.Event{
		Kind: wayland.EventPointerMotion,
		X:    float64(bounds.X + bounds.W/2), Y: float64(bounds.Y + bounds.H/2),
	})
	p.Handle(wayland.Event{Kind: wayland.EventPointerPress})
	// Slide off the node before releasing.
	p.Handle(wayland.Event{Kind: wayland.EventPointerMotion, X: 1, Y: 1})
	if p.Handle(wayland.Event{Kind: wayland.EventPointerRelease}) {
		t.Fatal("a release outside the pressed node counted as a click")
	}
	if got := pressedAction(p); got != "" {
		t.Fatalf("the release left %q pressed", got)
	}
}

func TestBarPointerLeaveCancelsThePress(t *testing.T) {
	t.Parallel()

	p := newTestBar(t)
	bounds := withSyntheticAction(t, p, 600)
	x, y := float64(bounds.X+bounds.W/2), float64(bounds.Y+bounds.H/2)

	p.Handle(wayland.Event{Kind: wayland.EventPointerMotion, X: x, Y: y})
	p.Handle(wayland.Event{Kind: wayland.EventPointerPress})
	p.Handle(wayland.Event{Kind: wayland.EventPointerLeave})
	if got := pressedAction(p); got != "" {
		t.Fatalf("a pointer leave left %q pressed", got)
	}
	p.Handle(wayland.Event{Kind: wayland.EventPointerMotion, X: x, Y: y})
	if p.Handle(wayland.Event{Kind: wayland.EventPointerRelease}) {
		t.Fatal("a release after the pointer left counted as a click")
	}
}

// The single root is gone: three sections are arranged into absolute bounds
// inside a content band derived from the theme tokens.
func TestBarArrangesSectionsInsideTheContentBand(t *testing.T) {
	t.Parallel()

	p := newTestBar(t)
	surfaceHeight, _, _ := DefaultTheme().Geometry()
	if err := p.Layout(3396, surfaceHeight); err != nil {
		t.Fatal(err)
	}

	content := p.contentLocked(3396, surfaceHeight)
	if content.W <= 0 || content.H <= 0 {
		t.Fatalf("content band = %+v, want a positive band", content)
	}

	var arranged int
	for _, section := range p.sections() {
		for _, n := range section {
			arranged++
			if n.Bounds.W < 0 || n.Bounds.H < 0 {
				t.Fatalf("node has negative bounds %+v", n.Bounds)
			}
			if n.Bounds.Y < content.Y || n.Bounds.Y+n.Bounds.H > content.Y+content.H {
				t.Fatalf("node bounds %+v escape the content band %+v", n.Bounds, content)
			}
		}
	}
	// workspace, window-title, two clocks, and three status widgets.
	if arranged != 6 {
		t.Fatalf("arranged %d items, want the full default bar (cpu and memory now share a group)", arranged)
	}
}

// --- Resolved interaction state ---------------------------------------------

func TestBarHoverInvalidatesOnlyOnTargetChange(t *testing.T) {
	t.Parallel()
	p := newTestBar(t)
	bounds := withSyntheticAction(t, p, 1920)
	cx, cy := bounds.X+bounds.W/2, bounds.Y+bounds.H/2

	if !p.Handle(wayland.Event{Kind: wayland.EventPointerMotion, X: float64(cx), Y: float64(cy)}) {
		t.Fatal("entering a clickable capsule did not invalidate the bar")
	}
	if got := p.pointer.hover; got != "synthetic-action" {
		t.Fatalf("hover = %q, want synthetic-action", got)
	}
	// Sliding within the same pill resolves to the same action, so it costs no
	// frame.
	if p.Handle(wayland.Event{Kind: wayland.EventPointerMotion, X: float64(cx + 1), Y: float64(cy)}) {
		t.Error("motion inside the hovered capsule invalidated the bar")
	}
	if !p.Handle(wayland.Event{Kind: wayland.EventPointerLeave}) {
		t.Error("leaving the bar did not invalidate it")
	}
	if p.pointer.hover != "" || p.pointer.press != "" {
		t.Errorf("leave left hover %q press %q", p.pointer.hover, p.pointer.press)
	}
}

func TestBarPressAndReleaseTrackState(t *testing.T) {
	t.Parallel()
	p := newTestBar(t)
	bounds := withSyntheticAction(t, p, 1920)
	cx, cy := bounds.X+bounds.W/2, bounds.Y+bounds.H/2

	p.Handle(wayland.Event{Kind: wayland.EventPointerMotion, X: float64(cx), Y: float64(cy)})
	p.Handle(wayland.Event{Kind: wayland.EventPointerPress, Button: buttonLeft})
	if got := p.pointer.press; got != "synthetic-action" {
		t.Fatalf("press = %q, want synthetic-action", got)
	}
	p.Handle(wayland.Event{Kind: wayland.EventPointerRelease, Button: buttonLeft})
	if got := p.pointer.press; got != "" {
		t.Errorf("release left press at %q", got)
	}
}

func TestOnlyClickableCapsulesAnimate(t *testing.T) {
	t.Parallel()
	// The bar's CPU and memory display groups carry no action, so they are not
	// chrome the pointer can light up; only actionable pills animate.
	p := newTestBar(t)
	withSyntheticAction(t, p, 1920)
	root, _ := p.renderViewLocked()

	clickable, display := 0, 0
	var walk func(*ui.Node)
	walk = func(n *ui.Node) {
		if n == nil {
			return
		}
		if n.Kind == ui.KindCapsule {
			if ui.Animated(n) {
				clickable++
			} else {
				display++
			}
		}
		for _, c := range n.Children {
			walk(c)
		}
	}
	walk(root)
	if display == 0 {
		t.Fatal("the bar has no display-only capsules to distinguish")
	}
	for _, n := range root.Children {
		if n.Kind == ui.KindCapsule && n.Action == "" && ui.Animated(n) {
			t.Errorf("display capsule %q animates", nodeText(n))
		}
	}
}
