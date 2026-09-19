package shell

import (
	"testing"

	"github.com/Nomadcxx/sysc-shell/internal/config"
	"github.com/Nomadcxx/sysc-shell/internal/platform/niri"
)

// A shape-only pill keeps the switching the numbered pills never had: the
// action carries the compositor's workspace id, so a click focuses that
// workspace rather than whatever the bar last projected into that slot.
func TestWorkspacePillClickFocusesWorkspace(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.Bar.Left = []config.Item{{ID: "workspace"}}
	cfg.Bar.Center, cfg.Bar.Right = nil, nil
	reg := NewRegistry(cfg)
	t.Cleanup(reg.Close)
	var sent []any
	reg.niriSend = func(body any) error {
		sent = append(sent, body)
		return nil
	}
	newHosts(t, reg, map[uint32]string{1: "DP-9"})
	reg.UpdateNiri(niri.Snapshot{Workspaces: []niri.Workspace{
		{ID: 11, Index: 1, Output: "DP-9", Focused: true, Active: true},
		{ID: 12, Index: 2, Output: "DP-9"},
	}})

	bar := reg.bars[1]
	if !bar.onAction("workspace:12", buttonLeft) {
		t.Fatal("left click on a pill ignored")
	}
	if len(sent) != 1 {
		t.Fatalf("niri sends = %d, want 1 FocusWorkspace", len(sent))
	}
	if fw, ok := sent[0].(niri.FocusWorkspace); !ok || fw.ID != 12 {
		t.Fatalf("click sent %+v, want FocusWorkspace 12", sent[0])
	}

	// Scroll cycling and a context menu are follow-ups; the other buttons do
	// nothing rather than focusing on the way past.
	if bar.onAction("workspace:12", buttonRight) {
		t.Error("right click claimed a pill it has no behaviour for")
	}
	if len(sent) != 1 {
		t.Fatalf("niri sends after right click = %d, want 1", len(sent))
	}
}

// The pill is nested inside the row inside the bar's capsule, so a click only
// reaches it if the hit test descends to the deepest node carrying an action.
// The dispatch check above calls the handler directly and cannot see that, and
// the live gate is the only other place it would show up.
func TestWorkspacePillIsHitAtItsOwnBounds(t *testing.T) {
	t.Parallel()
	bar := newTestBar(t)
	t.Cleanup(bar.stopAnimation)
	metrics := bar.themeSnapshot().Metrics
	widgets := buildWidgets([]config.Item{{ID: "workspace"}}, metrics.CapsulePadding, metrics)
	bar.left, bar.center, bar.right = widgets, nil, nil
	if !bar.apply(barView{Pills: []workspacePill{
		{ID: 11, Index: 1, Focused: true},
		{ID: 12, Index: 2, Occupied: true},
	}}) {
		t.Fatal("the pill view reported no change")
	}
	layoutForTest(t, bar, 800)

	row := widgets[0].inner
	if row == nil || len(row.Children) != 2 {
		t.Fatalf("pill row = %+v, want two pills", row)
	}
	for i, want := range []string{"workspace:11", "workspace:12"} {
		pill := row.Children[i]
		if pill.Bounds.W == 0 || pill.Bounds.H == 0 {
			t.Fatalf("pill %d = %+v, want an arranged node", i, pill.Bounds)
		}
		x := pill.Bounds.X + pill.Bounds.W/2
		y := pill.Bounds.Y + pill.Bounds.H/2
		bar.mu.Lock()
		got, ok := bar.hitLocked(x, y)
		bar.mu.Unlock()
		if !ok || got != want {
			t.Errorf("hit at pill %d = %q/%v, want %q/true", i, got, ok, want)
		}
	}
}
