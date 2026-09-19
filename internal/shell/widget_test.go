package shell

import (
	"testing"
	"time"

	"github.com/Nomadcxx/sysc-shell/internal/config"
	"github.com/Nomadcxx/sysc-shell/internal/render"
	"github.com/Nomadcxx/sysc-shell/internal/ui"
)

// reference is a fixed instant, so format assertions do not depend on when the
// test runs.
var reference = time.Date(2026, 8, 30, 15, 4, 5, 0, time.UTC)

func TestAClockWidgetFormatsTheSharedSnapshot(t *testing.T) {
	t.Parallel()
	widgets := buildWidgets([]config.Item{
		{ID: "clock", Format: "15:04", Boundary: time.Minute},
		{ID: "clock", Format: "Mon 2 Jan", Boundary: time.Minute},
	}, 8, standardMetrics())

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
	widgets := buildWidgets([]config.Item{{ID: "clock", Format: "15:04"}}, 8, standardMetrics())

	if got := widgets[0].format(barView{}); got != "" {
		t.Fatalf("clock before the first tick = %q, want empty", got)
	}
}

func TestWordmarkWidgetIsBareAndBalancesItsClocks(t *testing.T) {
	t.Parallel()
	widgets := buildWidgets([]config.Item{
		{ID: "clock", Format: "15:04"},
		{ID: "wordmark"},
		{ID: "clock", Format: "Mon 2 Jan"},
	}, 8, standardMetrics())
	if len(widgets) != 3 {
		t.Fatalf("built %d widgets, want 3", len(widgets))
	}
	mark := widgets[1]
	if mark.node.Kind != ui.KindWordmark || mark.inner != nil {
		t.Fatalf("wordmark was wrapped: node kind=%d inner=%v", mark.node.Kind, mark.inner)
	}
	if mark.node.ImageH != launcherMarkHeight || mark.node.ImageW != render.WordmarkWidth(launcherMarkHeight) {
		t.Fatalf("wordmark size = %dx%d, want %dx%d", mark.node.ImageW, mark.node.ImageH,
			render.WordmarkWidth(launcherMarkHeight), launcherMarkHeight)
	}
	if mark.node.Gradient.Count != 4 || mark.node.Gradient.Motion != ui.GradientLoop ||
		mark.node.Gradient.From != 0 || mark.node.Gradient.To != 1 {
		t.Fatalf("wordmark gradient = %+v", mark.node.Gradient)
	}
	if got := mark.node.Gradient.Stops; got[0].Role != ui.PaintPrimary ||
		got[1].Role != ui.PaintSecondary || got[2].Role != ui.PaintTertiary ||
		got[3].Role != ui.PaintPrimary {
		t.Fatalf("wordmark roles = %+v", got)
	}
	for _, i := range []int{0, 2} {
		if got := widgets[i].inner.MinWidthText; got != "Wed 30 Sep" {
			t.Errorf("clock %d width floor = %q, want Wed 30 Sep", i, got)
		}
	}
}

func TestDefaultCentreBuildsTimeDateGroupAndMedia(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	metrics := standardMetrics()
	widgets := buildWidgets(cfg.Bar.Center, metrics.CapsulePadding, metrics)
	if len(widgets) != 3 {
		t.Fatalf("default centre widgets = %d, want group, wordmark, media", len(widgets))
	}

	group := widgets[0]
	if group.node.Kind != ui.KindCapsule || group.inner == nil || group.inner.Kind != ui.KindRow {
		t.Fatalf("time/date widget = %+v, want one capsule around one row", group)
	}
	if len(group.inner.Children) != 2 {
		t.Fatalf("time/date members = %d, want time and date", len(group.inner.Children))
	}
	for i, member := range group.inner.Children {
		if member.Kind != ui.KindText || !member.Tabular || member.MinWidthText != clockWidthFloor {
			t.Fatalf("time/date member %d = %+v, want tabular clock with floor %q", i, member, clockWidthFloor)
		}
	}

	mark := widgets[1]
	if mark.node.Kind != ui.KindWordmark || mark.inner != nil {
		t.Fatalf("wordmark = %+v, want bare wordmark", mark)
	}
	media := widgets[2]
	if media.node.Kind != ui.KindCapsule || media.inner == nil || !media.node.Absent {
		t.Fatalf("media = %+v, want an initially absent capsule", media)
	}
}

func TestLauncherWidgetUsesThemedMarkAndOpensLauncher(t *testing.T) {
	t.Parallel()
	widgets := buildWidgets([]config.Item{{ID: "launcher"}}, 8, standardMetrics())
	if len(widgets) != 1 || widgets[0].node == nil {
		t.Fatalf("launcher widgets = %+v", widgets)
	}
	if widgets[0].node == nil {
		t.Fatalf("launcher widgets = %+v", widgets)
	}
	// The launcher is a button like its neighbours, so it wears the same
	// surface capsule and gains the shared hover wash.
	outer := widgets[0].node
	if outer.Kind != ui.KindCapsule || outer.Action != panelLauncherAction {
		t.Fatalf("launcher capsule = %+v", outer)
	}
	if widgets[0].inner == nil || widgets[0].inner != outer.Children[0] {
		t.Fatalf("launcher capsule child = %+v", outer.Children)
	}
	n := widgets[0].inner
	if n.Kind != ui.KindWordmark || n.Mark != "launcher" {
		t.Fatalf("launcher node = %+v", n)
	}
	if n.ImageW != launcherMarkHeight || n.ImageH != launcherMarkHeight {
		t.Fatalf("launcher box = %dx%d, want %dx%d", n.ImageW, n.ImageH, launcherMarkHeight, launcherMarkHeight)
	}
	if n.Gradient.Motion != ui.GradientLoop {
		t.Fatalf("launcher gradient motion = %v, want the wordmark's looping ramp", n.Gradient.Motion)
	}
	if n.Action != panelLauncherAction || n.Name != "Open launcher" || n.Role != "button" {
		t.Fatalf("launcher identity = %+v", n)
	}
}

func TestNiriWidgetsReadTheirOutputsProjection(t *testing.T) {
	t.Parallel()
	widgets := buildWidgets([]config.Item{
		{ID: "workspace"},
		{ID: "window-title", MaxWidth: 120},
	}, 8, standardMetrics())
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
	if got := pillCount(widgets[0].node); got != 2 {
		t.Fatalf("workspace pills = %d, want 2", got)
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
	}, 8, standardMetrics())

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
	}, 8, standardMetrics())
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
			if w.inner == nil {
				continue
			}
			if nodeText(w.inner) != "" {
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

// pillCount reports the shape pills a workspace row renders, so a
// test can assert the projection reached the bar without depending on which
// node carries the text.
func pillCount(n *ui.Node) int {
	if n == nil {
		return 0
	}
	if n.Kind == ui.KindCapsule && len(n.Children) == 0 {
		return 1
	}
	total := 0
	for _, c := range n.Children {
		total += pillCount(c)
	}
	return total
}

func TestAGroupRendersOneCapsuleHoldingFlatMembers(t *testing.T) {
	t.Parallel()
	widgets := buildWidgets([]config.Item{{ID: "group", Items: []config.Item{
		{ID: "cpu", Display: "text"},
		{ID: "memory", Display: "text"},
	}}}, 8, standardMetrics())
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
	if len(group.members) != 4 {
		t.Fatalf("default bar has no four-member group; found %d members", len(group.members))
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

func TestGroupedRadialMetricsReportValueChanges(t *testing.T) {
	t.Parallel()
	widgets := buildWidgets([]config.Item{{ID: "group", Items: []config.Item{
		{ID: "cpu", Display: "radial"},
		{ID: "memory", Display: "radial"},
		{ID: "temperature", Display: "radial"},
		{ID: "gpu", Display: "radial"},
	}}}, noCapsule, standardMetrics())
	if len(widgets) != 1 || widgets[0].refresh == nil {
		t.Fatal("radial metric group did not build a refresh function")
	}

	first := fixtureSnapshot()
	if !widgets[0].refresh(barView{Metrics: first}) {
		t.Fatal("first radial metric values reported no change")
	}

	second := fixtureSnapshot()
	second.CPU.Usage.Fraction = 0.51
	second.Memory.Memory.UsedBytes = 580
	second.Thermal.Celsius = 72
	second.GPU.GPUs[0].Usage.Fraction = 0.03
	if !widgets[0].refresh(barView{Metrics: second}) {
		t.Fatal("changed radial metric values reported no change")
	}

	for i, want := range []float64{0.51, 0.58, 0.72, 0.03} {
		if got := widgets[0].members[i].node.Value; got != want {
			t.Errorf("radial member %d value = %v, want %v", i, got, want)
		}
	}
}

func TestWorkspacePillsPaintAsShapes(t *testing.T) {
	t.Parallel()
	m := standardMetrics()
	widgets := buildWidgets([]config.Item{{ID: "workspace"}}, 8, m)
	if len(widgets) != 1 || widgets[0].node == nil {
		t.Fatalf("workspace widgets = %+v", widgets)
	}
	if widgets[0].inner == nil {
		t.Fatalf("workspace widgets = %+v", widgets)
	}
	if outer := widgets[0].node; outer.Kind != ui.KindCapsule {
		t.Fatalf("workspace wrapper = %+v, want the shared capsule", outer)
	}
	row := widgets[0].inner
	if row.Kind != ui.KindRow || row.Gap != workspacePillGap {
		t.Fatalf("workspace row = %+v, want a row with the pill gap", row)
	}
	pills := []workspacePill{
		{ID: 11, Index: 1, Occupied: true},
		{ID: 12, Index: 2, Focused: true},
		{ID: 13, Index: 3, Urgent: true},
		{ID: 14, Index: 4, Name: "mail"},
	}
	if !widgets[0].refresh(barView{Pills: pills}) {
		t.Fatal("first refresh reported no change")
	}
	if len(row.Children) != len(pills) {
		t.Fatalf("pill count = %d, want %d", len(row.Children), len(pills))
	}
	wantFill := []ui.Fill{ui.FillContainer, ui.FillAccent, ui.FillError, ui.FillNone}
	wantName := []string{"Workspace 1", "Workspace 2", "Workspace 3", "Workspace mail"}
	wantAction := []string{"workspace:11", "workspace:12", "workspace:13", "workspace:14"}
	for i, c := range row.Children {
		if c == nil || c.Kind != ui.KindCapsule {
			t.Fatalf("pill %d = %+v, want a capsule", i, c)
		}
		if len(c.Children) != 0 {
			t.Fatalf("pill %d carries children %+v, want shapes only", i, c.Children)
		}
		if c.Fill != wantFill[i] {
			t.Errorf("pill %d fill = %v, want %v", i, c.Fill, wantFill[i])
		}
		// One shape family: an explicit stadium overrides the bar's inherited
		// capsule radius, so a square pill is a circle and the focused one is
		// a true pill rather than a rounded rectangle.
		if c.Shape != ui.ShapeStadium {
			t.Errorf("pill %d shape = %v, want ShapeStadium", i, c.Shape)
		}
		if c.Height != m.IconLarge {
			t.Errorf("pill %d height = %d, want %d", i, c.Height, m.IconLarge)
		}
		wantW := m.IconLarge
		if i == 1 {
			wantW = 2 * m.IconLarge
		}
		if c.Width != wantW {
			t.Errorf("pill %d width = %d, want %d", i, c.Width, wantW)
		}
		if c.Action != wantAction[i] || c.Name != wantName[i] || c.Role != "button" {
			t.Errorf("pill %d identity = (%q, %q, %q), want (%q, %q, button)",
				i, c.Action, c.Name, c.Role, wantAction[i], wantName[i])
		}
	}
	if widgets[0].refresh(barView{Pills: pills}) {
		t.Fatal("second refresh rebuilt an unchanged row")
	}
}

func TestWorkspaceFallbackKeepsItsLabel(t *testing.T) {
	t.Parallel()
	widgets := buildWidgets([]config.Item{{ID: "workspace"}}, 8, standardMetrics())
	row := widgets[0].inner
	if row == nil {
		t.Fatalf("workspace widgets = %+v", widgets)
	}
	if !widgets[0].refresh(barView{}) {
		t.Fatal("fallback refresh reported no change")
	}
	if len(row.Children) != 1 || row.Children[0] == nil ||
		len(row.Children[0].Children) != 1 || row.Children[0].Children[0] == nil ||
		row.Children[0].Children[0].Text != noWorkspace {
		t.Fatalf("fallback row = %+v, want one labelled capsule", row)
	}
}
