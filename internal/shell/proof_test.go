package shell

import (
	"sync"
	"testing"

	"github.com/Nomadcxx/sysc-shell/internal/platform/niri"
	"github.com/Nomadcxx/sysc-shell/internal/platform/wayland"
)

func TestProofConcurrentUpdateAndRender(t *testing.T) {
	p := newTestProof(t)
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
			p.UpdateNiri(niri.Snapshot{Workspaces: []niri.Workspace{{
				ID: 1, Index: i, Focused: true,
			}}})
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

func newTestProof(t *testing.T) *Proof {
	t.Helper()
	p, err := New()
	if err != nil {
		t.Fatal(err)
	}
	return p
}

// drain reports how many invalidations are waiting.
func drain(p *Proof) int {
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

func TestProofFirstSnapshotSetsWorkspace(t *testing.T) {
	t.Parallel()

	p := newTestProof(t)
	p.UpdateNiri(niri.Snapshot{
		FocusedOutput: "DP-1",
		Workspaces: []niri.Workspace{
			{ID: 1, Index: 1, Output: "DP-1", Active: true, Focused: true},
		},
	})

	if got := p.WorkspaceLabel(); got != "Workspace: 1" {
		t.Fatalf("label = %q, want %q", got, "Workspace: 1")
	}
	if drain(p) != 1 {
		t.Fatal("the first snapshot did not request exactly one redraw")
	}
}

func TestProofPrefersWorkspaceName(t *testing.T) {
	t.Parallel()

	p := newTestProof(t)
	p.UpdateNiri(niri.Snapshot{
		FocusedOutput: "DP-1",
		Workspaces: []niri.Workspace{
			{ID: 5, Index: 1, Name: "code", Output: "DP-1", Active: true, Focused: true},
		},
	})

	if got := p.WorkspaceLabel(); got != "Workspace: code" {
		t.Fatalf("label = %q, want the workspace name", got)
	}
}

func TestProofLaterSnapshotChangesTextAndInvalidatesOnce(t *testing.T) {
	t.Parallel()

	p := newTestProof(t)
	first := niri.Snapshot{
		FocusedOutput: "DP-1",
		Workspaces: []niri.Workspace{
			{ID: 1, Index: 1, Output: "DP-1", Active: true, Focused: true},
		},
	}
	p.UpdateNiri(first)
	drain(p)

	second := niri.Snapshot{
		FocusedOutput: "DP-1",
		Workspaces: []niri.Workspace{
			{ID: 3, Index: 2, Output: "DP-1", Active: true, Focused: true},
		},
	}
	p.UpdateNiri(second)

	if got := p.WorkspaceLabel(); got != "Workspace: 2" {
		t.Fatalf("label = %q, want %q", got, "Workspace: 2")
	}
	if got := drain(p); got != 1 {
		t.Fatalf("a changed snapshot requested %d redraws, want exactly 1", got)
	}
}

func TestProofRepeatedSnapshotRequestsNoRedraw(t *testing.T) {
	t.Parallel()

	p := newTestProof(t)
	snap := niri.Snapshot{
		FocusedOutput: "DP-1",
		Workspaces: []niri.Workspace{
			{ID: 1, Index: 1, Output: "DP-1", Active: true, Focused: true},
		},
	}
	p.UpdateNiri(snap)
	drain(p)

	p.UpdateNiri(snap)
	if got := drain(p); got != 0 {
		t.Fatalf("an unchanged snapshot requested %d redraws, want none", got)
	}
}

// press and release drive a full click at one point.
func click(p *Proof, x, y int) bool {
	p.Handle(wayland.Event{Kind: wayland.EventPointerMotion, X: float64(x), Y: float64(y)})
	pressed := p.Handle(wayland.Event{Kind: wayland.EventPointerPress})
	released := p.Handle(wayland.Event{Kind: wayland.EventPointerRelease})
	return pressed || released
}

// layoutForTest arranges the tree at the proof's fixed height.
func layoutForTest(t *testing.T, p *Proof, width int) {
	t.Helper()
	if err := p.Layout(width, BarHeight); err != nil {
		t.Fatal(err)
	}
}

func TestProofClickTogglesMeter(t *testing.T) {
	t.Parallel()

	p := newTestProof(t)
	layoutForTest(t, p, 600)
	drain(p)

	if got := p.MeterValue(); got != 0.25 {
		t.Fatalf("initial meter = %v, want 0.25", got)
	}

	button := p.ButtonBounds()
	if !click(p, button.X+button.W/2, button.Y+button.H/2) {
		t.Fatal("a click on the button reported no change")
	}
	if got := p.MeterValue(); got != 0.75 {
		t.Fatalf("meter after one click = %v, want 0.75", got)
	}
	if got := drain(p); got != 0 {
		t.Fatalf("the synchronous click queued %d duplicate invalidations", got)
	}

	if !click(p, button.X+button.W/2, button.Y+button.H/2) {
		t.Fatal("a second click reported no change")
	}
	if got := p.MeterValue(); got != 0.25 {
		t.Fatalf("meter after two clicks = %v, want 0.25", got)
	}
}

func TestProofClickImmediatelyAfterPointerEnter(t *testing.T) {
	p := newTestProof(t)
	layoutForTest(t, p, 600)
	button := p.ButtonBounds()
	x, y := button.X+button.W/2, button.Y+button.H/2

	p.Handle(wayland.Event{Kind: wayland.EventPointerEnter, X: float64(x), Y: float64(y)})
	p.Handle(wayland.Event{Kind: wayland.EventPointerPress})
	if !p.Handle(wayland.Event{Kind: wayland.EventPointerRelease}) {
		t.Fatal("a click immediately after enter reported no change")
	}
	if got := p.MeterValue(); got != meterHigh {
		t.Fatalf("meter after click = %v, want %v", got, meterHigh)
	}
}

func TestProofClickOutsideAnActionChangesNothing(t *testing.T) {
	t.Parallel()

	p := newTestProof(t)
	layoutForTest(t, p, 600)
	drain(p)

	if click(p, 1, 1) {
		t.Fatal("a click outside every action reported a change")
	}
	if got := p.MeterValue(); got != 0.25 {
		t.Fatalf("meter = %v, want it unchanged at 0.25", got)
	}
	if drain(p) != 0 {
		t.Fatal("a click outside every action requested a redraw")
	}
}

// TestProofReleaseOutsideThePressedNodeIsNotAClick covers the press/release
// rule: a click counts only when both land on the same node.
func TestProofReleaseOutsideThePressedNodeIsNotAClick(t *testing.T) {
	t.Parallel()

	p := newTestProof(t)
	layoutForTest(t, p, 600)
	drain(p)

	button := p.ButtonBounds()
	p.Handle(wayland.Event{Kind: wayland.EventPointerMotion, X: float64(button.X + button.W/2), Y: float64(button.Y + button.H/2)})
	p.Handle(wayland.Event{Kind: wayland.EventPointerPress})
	// Slide off the button before releasing.
	p.Handle(wayland.Event{Kind: wayland.EventPointerMotion, X: 1, Y: 1})
	if p.Handle(wayland.Event{Kind: wayland.EventPointerRelease}) {
		t.Fatal("a release outside the pressed node counted as a click")
	}
	if got := p.MeterValue(); got != 0.25 {
		t.Fatalf("meter = %v, want it unchanged", got)
	}
}

func TestProofPointerLeaveCancelsThePress(t *testing.T) {
	t.Parallel()

	p := newTestProof(t)
	layoutForTest(t, p, 600)

	button := p.ButtonBounds()
	p.Handle(wayland.Event{Kind: wayland.EventPointerMotion, X: float64(button.X + button.W/2), Y: float64(button.Y + button.H/2)})
	p.Handle(wayland.Event{Kind: wayland.EventPointerPress})
	p.Handle(wayland.Event{Kind: wayland.EventPointerLeave})
	p.Handle(wayland.Event{Kind: wayland.EventPointerMotion, X: float64(button.X + button.W/2), Y: float64(button.Y + button.H/2)})
	if p.Handle(wayland.Event{Kind: wayland.EventPointerRelease}) {
		t.Fatal("a release after the pointer left counted as a click")
	}
}

// The single root is gone: three sections are arranged into absolute bounds
// inside a content band derived from the theme tokens.
func TestProofArrangesSectionsInsideTheContentBand(t *testing.T) {
	t.Parallel()

	p := newTestProof(t)
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
	if arranged != 4 {
		t.Fatalf("arranged %d items, want the fixture's four", arranged)
	}
	if p.ButtonBounds().W <= 0 {
		t.Fatal("the button was not arranged")
	}
}

func TestProofUsesDMSButtonPadding(t *testing.T) {
	t.Parallel()
	p := newTestProof(t)
	if p.button.Padding != 4 {
		t.Fatalf("button padding = %d, want the DMS reference value 4", p.button.Padding)
	}
}
