package shell

import (
	"github.com/Nomadcxx/sysc-shell/internal/ui"
	"testing"
	"time"

	"github.com/Nomadcxx/sysc-shell/internal/config"
)

// reference is a fixed instant, so format assertions do not depend on when the
// test runs.
var reference = time.Date(2026, 8, 30, 15, 4, 5, 0, time.UTC)

func TestAClockWidgetFormatsTheSharedSnapshot(t *testing.T) {
	t.Parallel()
	widgets := buildWidgets([]config.Item{
		{ID: "clock", Format: "15:04", Boundary: time.Minute},
		{ID: "clock", Format: "Mon 2 Jan", Boundary: time.Minute},
	}, 8)

	view := barView{Now: reference}
	if got := widgets[0].format(view); got != "15:04" {
		t.Fatalf("time clock = %q, want 15:04", got)
	}
	if got := widgets[1].format(view); got != "Sun 30 Aug" {
		t.Fatalf("date clock = %q, want Sun 30 Aug", got)
	}
}

// Before the first tick there is no time to show, and a bar must still render.
func TestAClockWidgetIsEmptyBeforeTheFirstTick(t *testing.T) {
	t.Parallel()
	widgets := buildWidgets([]config.Item{{ID: "clock", Format: "15:04"}}, 8)

	if got := widgets[0].format(barView{}); got != "" {
		t.Fatalf("clock before the first tick = %q, want empty", got)
	}
}

func TestNiriWidgetsReadTheirOutputsProjection(t *testing.T) {
	t.Parallel()
	widgets := buildWidgets([]config.Item{
		{ID: "workspace"},
		{ID: "window-title", MaxWidth: 120},
	}, 8)
	view := barView{
		Workspace: "code", Title: "Fixture One",
		Pills: []workspacePill{{Index: 1, Focused: true}, {Index: 2}},
	}

	// The workspace widget renders a pill row, so it refreshes a subtree
	// instead of formatting a string.
	if widgets[0].format != nil {
		t.Fatal("the workspace widget should refresh a tree, not format text")
	}
	if !widgets[0].refresh(view) {
		t.Fatal("the first refresh reported no change")
	}
	if got := pillIndices(widgets[0].node); len(got) != 2 || got[0] != "1" || got[1] != "2" {
		t.Fatalf("workspace pills = %v, want 1 and 2", got)
	}
	if widgets[0].refresh(view) {
		t.Fatal("an unchanged view rebuilt the pill row")
	}
	if got := widgets[1].format(view); got != "Fixture One" {
		t.Fatalf("title = %q, want Fixture One", got)
	}
	if got := widgets[1].inner.MaxWidth; got != 120 {
		t.Fatalf("title node max width = %d, want 120", got)
	}
}

func TestApplyWritesOnlyChangedText(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	bar, err := NewWithTheme(ThemeFrom(cfg, cfg.Bar), cfg.Bar, "DP-9")
	if err != nil {
		t.Fatalf("NewWithTheme: %v", err)
	}

	if changed := bar.apply(barView{Now: reference, Workspace: "code", Title: "Fixture One"}); !changed {
		t.Fatal("the first view reported no change")
	}
	// Re-applying the same view must report nothing: no change, no redraw.
	if changed := bar.apply(barView{Now: reference, Workspace: "code", Title: "Fixture One"}); changed {
		t.Fatal("an identical view reported a change")
	}
	// A different instant inside the same minute renders identical text.
	sameMinute := reference.Add(20 * time.Second)
	if changed := bar.apply(barView{Now: sameMinute, Workspace: "code", Title: "Fixture One"}); changed {
		t.Fatal("a tick inside the same minute reported a change")
	}
	// Crossing the minute must change.
	nextMinute := reference.Add(time.Minute)
	if changed := bar.apply(barView{Now: nextMinute, Workspace: "code", Title: "Fixture One"}); !changed {
		t.Fatal("crossing a minute boundary reported no change")
	}
}

func TestABarRemembersItsConnector(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	bar, err := NewWithTheme(ThemeFrom(cfg, cfg.Bar), cfg.Bar, "HDMI-A-9")
	if err != nil {
		t.Fatalf("NewWithTheme: %v", err)
	}
	if got := bar.connector(); got != "HDMI-A-9" {
		t.Fatalf("connector = %q, want HDMI-A-9", got)
	}
}

// Only the clock asks for tabular figures; nothing else should.
func TestOnlyClockWidgetsRequestTabularFigures(t *testing.T) {
	t.Parallel()
	widgets := buildWidgets([]config.Item{
		{ID: "clock", Format: "15:04"},
		{ID: "workspace"},
		{ID: "window-title", MaxWidth: 120},
	}, 8)

	if !widgets[0].inner.Tabular {
		t.Fatal("the clock node does not request tabular figures")
	}
	if widgets[1].inner.Tabular || widgets[2].inner.Tabular {
		t.Fatal("a non-clock widget requested tabular figures")
	}
}

func TestEveryBarWidgetIsWrappedInACapsule(t *testing.T) {
	t.Parallel()
	widgets := buildWidgets([]config.Item{
		{ID: "clock", Format: "15:04"},
		{ID: "workspace"},
		{ID: "window-title", MaxWidth: 200},
		{ID: "cpu", Display: "meter"},
		{ID: "memory", Display: "graph"},
	}, 8)
	if len(widgets) != 5 {
		t.Fatalf("built %d widgets", len(widgets))
	}
	for i, w := range widgets {
		if w.node.Kind != ui.KindCapsule {
			t.Errorf("widget %d root kind = %d, want KindCapsule", i, w.node.Kind)
		}
		if w.node.Padding != 8 {
			t.Errorf("widget %d padding = %d, want 8", i, w.node.Padding)
		}
		if len(w.node.Children) != 1 || w.node.Children[0] != w.inner {
			t.Errorf("widget %d capsule does not hold its inner node", i)
		}
		if w.inner.Kind == ui.KindCapsule {
			t.Errorf("widget %d was wrapped twice", i)
		}
	}
}

// format writes to the inner node, so a clock that ticks must still mark the
// bar changed after the wrap.
func TestApplyWritesThroughToTheInnerNode(t *testing.T) {
	t.Parallel()
	b, err := New("DP-1")
	if err != nil {
		t.Skipf("no system fonts: %v", err)
	}
	view := barView{Now: time.Date(2026, 8, 31, 11, 37, 0, 0, time.UTC)}
	if !b.apply(view) {
		t.Fatal("first apply must report a change")
	}
	var found bool
	for _, section := range b.widgets() {
		for _, w := range section {
			if w.inner.Kind == ui.KindText && w.inner.Text != "" {
				found = true
				if w.node.Text != "" {
					t.Errorf("text landed on the capsule, not the inner node: %q", w.node.Text)
				}
			}
		}
	}
	if !found {
		t.Fatal("no widget carried text on its inner node")
	}
}

// nodeText returns the first text a node subtree carries. Bar items are wrapped
// in a capsule, so a test that wants the rendered string reads through it
// rather than reaching for the chrome node.
func nodeText(n *ui.Node) string {
	if n == nil {
		return ""
	}
	if n.Kind == ui.KindText {
		return n.Text
	}
	for _, c := range n.Children {
		if s := nodeText(c); s != "" {
			return s
		}
	}
	return ""
}

// pillIndices reports the numerals a workspace pill row renders, in order, so a
// test can assert the projection reached the bar without depending on which
// node carries the text.
func pillIndices(n *ui.Node) []string {
	var out []string
	if n == nil {
		return out
	}
	if n.Kind == ui.KindCapsule && len(n.Children) == 1 && n.Children[0] != nil &&
		n.Children[0].Kind == ui.KindText {
		return append(out, n.Children[0].Text)
	}
	for _, c := range n.Children {
		out = append(out, pillIndices(c)...)
	}
	return out
}

func TestAGroupRendersOneCapsuleHoldingFlatMembers(t *testing.T) {
	t.Parallel()
	widgets := buildWidgets([]config.Item{{ID: "group", Items: []config.Item{
		{ID: "cpu", Display: "text"},
		{ID: "memory", Display: "text"},
	}}}, 8)
	if len(widgets) != 1 {
		t.Fatalf("built %d widgets, want one group", len(widgets))
	}
	g := widgets[0].node
	if g.Kind != ui.KindCapsule || g.Padding != 8 {
		t.Fatalf("group root = kind %d padding %d, want a padded capsule", g.Kind, g.Padding)
	}
	row := g.Children[0]
	if row.Kind != ui.KindRow || len(row.Children) != 2 {
		t.Fatalf("group holds %+v, want a row of two members", row)
	}
	for i, m := range row.Children {
		if m.Kind == ui.KindCapsule {
			t.Errorf("member %d is capsuled; members must be flat inside the group", i)
		}
	}
	// The group drives its members rather than formatting itself.
	if widgets[0].format != nil {
		t.Error("a group should refresh its members, not format text")
	}
	if widgets[0].refresh == nil {
		t.Error("a group has no refresh, so its members would never update")
	}
}

// A selector nested in a group still needs its service lease, or the group
// renders placeholders forever.
func TestGroupedMetricsStillAcquireTheirLeases(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	reg := NewRegistry(cfg)
	t.Cleanup(reg.Close)
	newHosts(t, reg, map[uint32]string{1: "DP-9"})

	if !reg.Metrics().Running() {
		t.Fatal("the default bar groups cpu and memory, and neither leased the sampler")
	}
}

// A grouped metric must still name itself, or a bare percentage inside a shared
// pill says nothing about what it measures.
func TestAGroupedMetricKeepsItsOwnTooltip(t *testing.T) {
	t.Parallel()
	b, err := New("DP-1")
	if err != nil {
		t.Skipf("no system fonts: %v", err)
	}
	if err := b.Layout(1536, BarHeight); err != nil {
		t.Fatal(err)
	}
	var group textWidget
	for _, section := range b.widgets() {
		for _, w := range section {
			if len(w.members) > 0 {
				group = w
			}
		}
	}
	if len(group.members) != 2 {
		t.Fatalf("default bar has no two-member group; found %d members", len(group.members))
	}
	for _, m := range group.members {
		if m.tooltip == "" {
			t.Fatalf("group member %+v has no tooltip", m.node)
		}
		mid := m.node.Bounds
		if mid.W == 0 {
			continue // an empty reading measures zero and cannot be hovered
		}
		got, _, _, ok := b.tooltipAtLocked(mid.X+mid.W/2, mid.Y+mid.H/2)
		if !ok || got != m.tooltip {
			t.Fatalf("hover over %+v gave %q ok=%v, want %q", mid, got, ok, m.tooltip)
		}
	}
}
