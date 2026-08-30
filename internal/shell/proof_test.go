package shell

import (
	"sync"
	"testing"

	"github.com/Nomadcxx/sysc-shell/internal/platform/niri"
	"github.com/Nomadcxx/sysc-shell/internal/platform/wayland"
	"github.com/Nomadcxx/sysc-shell/internal/ui"
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

// withSyntheticAction appends a node carrying an action to the right section.
// No Tranche 3A widget carries one, so the press/release rule and hit testing
// are exercised through this node rather than through a shipped widget.
func withSyntheticAction(t *testing.T, p *Proof, width int) ui.Rect {
	t.Helper()
	p.right = append(p.right, &ui.Node{
		Kind: ui.KindButton, Text: "Synthetic", Padding: 4, Action: "synthetic-action",
	})
	layoutForTest(t, p, width)
	bounds := p.right[len(p.right)-1].Bounds
	if bounds.W <= 0 || bounds.H <= 0 {
		t.Fatalf("synthetic node was not arranged: %+v", bounds)
	}
	return bounds
}

// pressedAction reports the action recorded by the last press.
func pressedAction(p *Proof) string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.pressed
}

func TestProofPressRecordsTheHitAction(t *testing.T) {
	t.Parallel()

	p := newTestProof(t)
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

func TestProofPressOutsideEveryActionRecordsNothing(t *testing.T) {
	t.Parallel()

	p := newTestProof(t)
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
func TestProofReleaseOutsideThePressedNodeIsNotAClick(t *testing.T) {
	t.Parallel()

	p := newTestProof(t)
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

func TestProofPointerLeaveCancelsThePress(t *testing.T) {
	t.Parallel()

	p := newTestProof(t)
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
		t.Fatalf("arranged %d items, want workspace, window-title and two clocks", arranged)
	}
}
