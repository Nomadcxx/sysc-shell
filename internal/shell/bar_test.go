package shell

import (
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Nomadcxx/sysc-shell/internal/config"
	"github.com/Nomadcxx/sysc-shell/internal/platform/wayland"
	"github.com/Nomadcxx/sysc-shell/internal/render"
	"github.com/Nomadcxx/sysc-shell/internal/services"
	"github.com/Nomadcxx/sysc-shell/internal/ui"
	tray "github.com/Nomadcxx/sysc-tray/protocol"
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

func TestBarSuppressesTrayWhenProjectionIsDisabled(t *testing.T) {
	bar := &Bar{
		trayNodes: []*ui.Node{{Kind: ui.KindText, Text: "tray"}},
		trayPrefs: config.TrayPreferences{Enabled: false},
	}
	sections := bar.sections()
	if len(sections) != 3 {
		t.Fatalf("sections = %d, want three bar sections", len(sections))
	}
	if len(sections[2]) != 0 {
		t.Fatalf("disabled tray projected %d nodes", len(sections[2]))
	}
	bar.trayPrefs.Enabled = true
	if got := len(bar.sections()[2]); got != 1 {
		t.Fatalf("enabled tray projected %d nodes, want one", got)
	}
}

func newTestBar(t *testing.T) *Bar {
	t.Helper()
	p, err := New("DP-9")
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func TestSideBarLayoutUsesVerticalAxis(t *testing.T) {
	cfg := config.Default()
	cfg.Bar.Edge = "left"
	bar, err := NewWithTheme(ThemeFrom(cfg, cfg.Bar), cfg.Bar, "DP-1")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(bar.stopAnimation)
	left := &ui.Node{Kind: ui.KindText, Text: "L"}
	center := &ui.Node{Kind: ui.KindText, Text: "C"}
	right := &ui.Node{Kind: ui.KindText, Text: "R"}
	bar.left = []textWidget{{node: left}}
	bar.center = []textWidget{{node: center}}
	bar.right = []textWidget{{node: right}}
	if err := bar.Configure(cfg.Bar.SurfaceExtent(), 800, 120); err != nil {
		t.Fatal(err)
	}
	content := bar.contentLocked(cfg.Bar.SurfaceExtent(), 800)
	if left.Bounds.Y != content.Y || right.Bounds.Y+right.Bounds.H != content.Y+content.H {
		t.Fatalf("side lanes = %+v/%+v, content %+v", left.Bounds, right.Bounds, content)
	}
	if got, want := center.Bounds.Y+center.Bounds.H/2, content.Y+content.H/2; got < want-1 || got > want+1 {
		t.Fatalf("centre Y = %d, want %d", got, want)
	}
	for _, node := range []*ui.Node{left, center, right} {
		if node.Bounds.W <= 0 || node.Bounds.H <= 0 || node.Bounds.X < content.X || node.Bounds.X+node.Bounds.W > content.X+content.W {
			t.Fatalf("node %+v escaped side content %+v", node.Bounds, content)
		}
	}
}

func TestSideBarTrayBudgetUsesVerticalExtent(t *testing.T) {
	bar := &Bar{}
	bar.theme.BarEdge = "right"
	bar.theme.Metrics.BarSpacing = 6
	content := ui.Rect{X: 4, Y: 10, W: 40, H: 800}
	center := []*ui.Node{{Bounds: ui.Rect{Y: 350, H: 40}}}
	right := []*ui.Node{{Bounds: ui.Rect{Y: 740, H: 20}}}
	if got := bar.trayAvailableLocked(content, center, right); got != 338 {
		t.Fatalf("tray available = %d, want 338 along Y", got)
	}
}

func TestSideBarComposition(t *testing.T) {
	for _, tc := range []struct {
		name   string
		edge   string
		height int
		wide   bool
	}{
		{"left compact", "left", 48, false},
		{"right compact", "right", 48, false},
		{"left textual", "left", 96, true},
		{"right textual", "right", 96, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := config.Default()
			cfg.Bar.Edge, cfg.Bar.Height = tc.edge, tc.height
			cfg.Bar.Left = []config.Item{{ID: "launcher"}, {ID: "workspace"}}
			cfg.Bar.Center = []config.Item{{ID: "group", Items: []config.Item{{ID: "wordmark"}}}}
			if tc.wide {
				cfg.Bar.Left = append(cfg.Bar.Left,
					config.Item{ID: "group", Items: []config.Item{{ID: "cpu", Display: "radial"}, {ID: "memory", Display: "radial"}}},
					config.Item{ID: "window-title", MaxWidth: 80})
				cfg.Bar.Center = config.Default().Bar.Center[:1]
			}
			cfg.Bar.Right = []config.Item{{ID: "running-apps"}}
			policy := cfg.ForConnector("DP-1")
			bar, err := NewWithTheme(ThemeFrom(cfg, policy), policy, "DP-1")
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(bar.stopAnimation)
			trayItems := []tray.Item{trayItem(1, "a"), trayItem(2, "b")}
			bar.setTray(trayItems, config.TrayPreferences{Enabled: true}, nil)
			view := barView{
				Now: time.Date(2026, time.October, 3, 9, 7, 0, 0, time.UTC), Title: "A long focused window title",
				Pills:   []workspacePill{{ID: 11, Index: 1, Focused: true}, {ID: 12, Index: 2, Occupied: true}},
				Running: []runningAppSlot{{Key: "firefox"}, {Key: "terminal"}},
			}
			bar.apply(view)
			if err := bar.Configure(policy.SurfaceExtent(), 864, 120); err != nil {
				t.Fatal(err)
			}
			content := bar.contentLocked(policy.SurfaceExtent(), 864)
			workspace := bar.left[1]
			if workspace.inner == nil || workspace.inner.Kind != ui.KindColumn || len(workspace.inner.Children) != 2 {
				t.Fatalf("workspace stack = %+v", workspace.inner)
			}
			focused := workspace.inner.Children[0]
			if focused.Width != bar.theme.Metrics.IconLarge || focused.Height != 2*bar.theme.Metrics.IconLarge || focused.Action != "workspace:11" {
				t.Fatalf("focused side workspace = %+v", focused)
			}
			if tc.wide && (bar.left[2].inner == nil || bar.left[2].inner.Kind != ui.KindColumn || len(bar.left[2].inner.Children) != 2 || bar.left[2].inner.Children[0].Kind != ui.KindRadialGauge) {
				t.Fatalf("metric group = %+v, want column", bar.left[2].inner)
			}
			pill := bar.center[0].node
			if pill.Kind != ui.KindCapsule || pill.Key != "centre" || pill.Action != panelControlCenterAction || pill.Name != "Control centre" || pill.Role != "button" || bar.center[0].inner.Kind != ui.KindColumn {
				t.Fatalf("centre pill = %+v, inner %+v", pill, bar.center[0].inner)
			}
			mark := findKind(pill, ui.KindWordmark)
			innerWidth := content.W - 2*pill.PaddingX
			if mark == nil || mark.ImageW <= 0 || mark.ImageW > innerWidth || mark.ImageW != centreMarkHeight || mark.Mark != "sysc-side" || mark.ImageH != render.WordmarkWidth(mark.ImageW) {
				t.Fatalf("side wordmark = %+v, available width %d", mark, innerWidth)
			}
			if tc.wide {
				children := bar.center[0].inner.Children
				if len(children) != 4 || children[1].Kind != ui.KindSeparator || children[2].Text != "09:07" || children[3].Text == "" {
					t.Fatalf("textual centre stack = %+v", children)
				}
			}
			apps := bar.right[0]
			if apps.inner == nil || apps.inner.Kind != ui.KindColumn || len(apps.inner.Children) != 2 {
				t.Fatalf("running-app stack = %+v", apps.inner)
			}
			if apps.inner.Children[0].Action != "running-app:firefox" || apps.inner.Children[1].Action != "running-app:terminal" {
				t.Fatalf("running-app order = %+v", apps.inner.Children)
			}
			if len(bar.trayNodes) != 2 || bar.trayNodes[0].Bounds.Y >= bar.trayNodes[1].Bounds.Y || apps.node.Bounds.Y+apps.node.Bounds.H > bar.trayNodes[0].Bounds.Y {
				t.Fatalf("tray/app stack = %+v / %+v", apps.node.Bounds, bar.trayNodes)
			}
			if bar.trayActions["tray-item:0"] != trayItems[0].Key || bar.trayActions["tray-item:1"] != trayItems[1].Key {
				t.Fatalf("tray identity/order = %+v", bar.trayActions)
			}
			for _, node := range []*ui.Node{workspace.node, pill, apps.node, bar.trayNodes[0], bar.trayNodes[1]} {
				if node.Bounds.W <= 0 || node.Bounds.H <= 0 || node.Bounds.X < content.X || node.Bounds.X+node.Bounds.W > content.X+content.W {
					t.Fatalf("side node %+v escapes content %+v", node.Bounds, content)
				}
			}
			for _, tc := range []struct {
				node *ui.Node
				want string
			}{{focused, "workspace:11"}, {apps.inner.Children[0], "running-app:firefox"}, {bar.trayNodes[0], "tray-item:0"}} {
				bar.mu.Lock()
				got, ok := bar.hitLocked(tc.node.Bounds.X+tc.node.Bounds.W/2, tc.node.Bounds.Y+tc.node.Bounds.H/2)
				bar.mu.Unlock()
				if !ok || got != tc.want {
					t.Errorf("hit on %+v = %q/%v, want %q", tc.node.Bounds, got, ok, tc.want)
				}
			}
			view.Now = view.Now.Add(time.Minute)
			view.Pills[0].Focused, view.Pills[1].Focused = false, true
			view.Running[0], view.Running[1] = view.Running[1], view.Running[0]
			bar.apply(view)
			if err := bar.Configure(policy.SurfaceExtent(), 864, 150); err != nil {
				t.Fatal(err)
			}
			if workspace.inner.Kind != ui.KindColumn || workspace.inner.Children[1].Height != 2*bar.theme.Metrics.IconLarge || bar.center[0].inner.Kind != ui.KindColumn || apps.inner.Kind != ui.KindColumn || apps.inner.Children[0].Action != "running-app:terminal" {
				t.Fatal("a refresh or scale change lost side composition")
			}
		})
	}
}

func TestZeroBarStopsAnimationSafely(t *testing.T) {
	t.Parallel()
	bar := &Bar{}
	bar.stopAnimation()
	bar.stopAnimation()
}

func TestBarGradientFramesFollowMotionPreference(t *testing.T) {
	t.Parallel()
	for _, reduced := range []bool{false, true} {
		t.Run(strconv.FormatBool(reduced), func(t *testing.T) {
			cfg := config.Default()
			cfg.Accessibility.ReducedMotion = reduced
			bar, err := NewWithTheme(ThemeFrom(cfg, cfg.Bar), cfg.Bar, "DP-9")
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(bar.stopAnimation)
			if bar.anim == nil {
				t.Fatal("NewWithTheme left the bar animator nil")
			}
			bar.mu.Lock()
			root, _ := bar.renderViewLocked()
			settled, running := bar.anim.Settled(), bar.anim.running.Load()
			bar.mu.Unlock()
			mark := findKind(root, ui.KindWordmark)
			if mark == nil {
				t.Fatal("default bar rendered no wordmark")
			}
			if reduced {
				if !settled || running || mark.GradientOffset != 0.5 {
					t.Fatalf("reduced motion: settled=%v running=%v offset=%v", settled, running, mark.GradientOffset)
				}
			} else if settled || !running {
				t.Fatalf("full motion: settled=%v running=%v", settled, running)
			}
		})
	}
}

func TestBarGradientSettlesWhenWordmarkLeavesTree(t *testing.T) {
	t.Parallel()
	bar := newTestBar(t)
	t.Cleanup(bar.stopAnimation)
	bar.mu.Lock()
	bar.renderViewLocked()
	if bar.anim.Settled() {
		bar.mu.Unlock()
		t.Fatal("wordmark did not start its gradient loop")
	}
	bar.center = nil
	bar.renderViewLocked()
	_, looping := bar.anim.values[animKey{node: "wordmark", channel: animGradient}]
	bar.mu.Unlock()
	if looping {
		t.Fatal("removed wordmark kept its gradient target")
	}
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
	if got := pillCount(sections[0][1]); got != 2 {
		t.Fatalf("workspace pills = %d, want 2", got)
	}
	if got := nodeText(sections[0][2]); got != "Fixture One" {
		t.Fatalf("title node = %q, want Fixture One", got)
	}
}

// An output Niri has not reported renders a stable fallback rather than an
// empty bar.
func TestABarRendersTheFallbackWorkspace(t *testing.T) {
	t.Parallel()

	p := newTestBar(t)
	p.apply(barView{Workspace: noWorkspace})

	if got := nodeText(p.sections()[0][1]); got != "-" {
		t.Fatalf("workspace node = %q, want the %q fallback", got, noWorkspace)
	}
	if got := nodeText(p.sections()[0][2]); got != "" {
		t.Fatalf("title node = %q, want empty with no window", got)
	}
}

func TestBarApplyUpdatesAnUncapsuledFormattedWidget(t *testing.T) {
	node := &ui.Node{Kind: ui.KindText}
	bar := &Bar{right: []textWidget{{
		node:   node,
		format: func(barView) string { return "42%" },
	}}}

	if !bar.apply(barView{}) {
		t.Fatal("the formatted widget did not report a change")
	}
	if node.Text != "42%" {
		t.Fatalf("widget text = %q, want 42%%", node.Text)
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
	bounds := withSyntheticAction(t, p, 1920)

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
	withSyntheticAction(t, p, 1920)
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
	bounds := withSyntheticAction(t, p, 1920)

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
	bounds := withSyntheticAction(t, p, 1920)
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
	// Every configured item reaches a bound, counted off the same default
	// config the bar was built from. A literal here goes stale the moment the
	// default bar gains or loses a widget, which is how it came to expect one
	// fewer than the bar produces.
	bar := config.Default().Bar
	want := len(bar.Left) + len(bar.Center) + len(bar.Right)
	// Media is configured but initially absent, so it intentionally has no
	// layout surface until a player snapshot arrives.
	if hasMediaItem(bar.Center) {
		want--
	}
	if arranged != want {
		t.Fatalf("arranged %d visible items, want %d", arranged, want)
	}
}

func TestBarCollapsesAbsentMediaAcrossRetainedConsumers(t *testing.T) {
	t.Parallel()

	bar := newTestBar(t)
	t.Cleanup(bar.stopAnimation)
	metrics := bar.themeSnapshot().Metrics
	widgets := buildWidgets([]config.Item{
		{ID: "wordmark"},
		{ID: "media", MaxWidth: 120},
	}, metrics.CapsulePadding, metrics)
	bar.left, bar.center, bar.right = nil, widgets, nil

	present := barView{Media: services.MediaState{
		Available: true, Status: services.PlaybackPlaying, Title: "Track",
	}}
	if !bar.apply(present) {
		t.Fatal("the present media view reported no change")
	}
	layoutForTest(t, bar, 800)
	media := widgets[1].node
	if media.Absent || media.Bounds.W == 0 {
		t.Fatalf("present media = %+v, want a visible arranged node", media)
	}
	mediaPoint := struct{ x, y int }{media.Bounds.X + media.Bounds.W/2, media.Bounds.Y + media.Bounds.H/2}
	if got := bar.actionBounds(panelMediaAction); got != media.Bounds {
		t.Fatalf("present media action bounds = %+v, want %+v", got, media.Bounds)
	}
	bar.mu.Lock()
	if got, ok := bar.hitLocked(mediaPoint.x, mediaPoint.y); !ok || got != panelMediaAction {
		bar.mu.Unlock()
		t.Fatalf("present media hit = %q/%v, want %q/true", got, ok, panelMediaAction)
	}
	tip, _, _, ok := bar.tooltipAtLocked(mediaPoint.x, mediaPoint.y)
	bar.mu.Unlock()
	if !ok || tip != "Media" {
		t.Fatalf("present media tooltip = %q/%v, want Media/true", tip, ok)
	}
	bar.mu.Lock()
	root, _ := bar.renderViewLocked()
	bar.mu.Unlock()
	if findAction(root, panelMediaAction) == nil {
		t.Fatal("present media was missing from the rendered tree")
	}

	if !bar.apply(barView{}) {
		t.Fatal("the absent media view reported no change")
	}
	layoutForTest(t, bar, 800)
	sections := bar.sections()
	if len(sections[1]) != 1 || sections[1][0] != widgets[0].node {
		t.Fatalf("absent centre sections = %+v, want only the wordmark", sections[1])
	}
	if media.Bounds != (ui.Rect{}) {
		t.Fatalf("absent media bounds = %+v, want zero surface", media.Bounds)
	}
	if got := bar.actionBounds(panelMediaAction); got != (ui.Rect{}) {
		t.Fatalf("absent media action bounds = %+v, want zero", got)
	}
	bar.mu.Lock()
	got, hit := bar.hitLocked(mediaPoint.x, mediaPoint.y)
	tip, _, _, tipOK := bar.tooltipAtLocked(mediaPoint.x, mediaPoint.y)
	root, _ = bar.renderViewLocked()
	bar.mu.Unlock()
	if hit && got == panelMediaAction {
		t.Fatalf("absent media retained hit action %q", got)
	}
	if tipOK && tip == "Media" {
		fatalf := "absent media retained tooltip %q"
		t.Fatalf(fatalf, tip)
	}
	if findAction(root, panelMediaAction) != nil {
		t.Fatal("absent media remained in the rendered tree")
	}

	if !bar.apply(present) {
		t.Fatal("the returning media view reported no change")
	}
	layoutForTest(t, bar, 800)
	if widgets[1].node.Absent || widgets[1].node.Bounds.W == 0 {
		t.Fatalf("returning media = %+v, want a visible arranged node", widgets[1].node)
	}
	if got := bar.actionBounds(panelMediaAction); got != widgets[1].node.Bounds {
		t.Fatalf("returning media action bounds = %+v, want %+v", got, widgets[1].node.Bounds)
	}
}

func TestDefaultCentrePillStaysAnchoredAcrossMediaAndClockChanges(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	const mediaMaxWidth = 180
	cfg.Bar.Center[1].MaxWidth = mediaMaxWidth
	bar, err := NewWithTheme(ThemeFrom(cfg, cfg.Bar), cfg.Bar, "DP-1")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(bar.stopAnimation)
	if !bar.apply(barView{Now: reference}) {
		t.Fatal("the initial clock snapshot reported no change")
	}
	const width = 1200
	if err := bar.Configure(width, BarHeight, 120); err != nil {
		t.Fatal(err)
	}

	render := func() *ui.Node {
		t.Helper()
		pixels := make([]byte, width*BarHeight*4)
		if err := bar.Render(pixels, width, BarHeight, width*4); err != nil {
			t.Fatal(err)
		}
		bar.mu.Lock()
		defer bar.mu.Unlock()
		root, _ := bar.renderViewLocked()
		return root
	}
	// The pill's left edge is where the band's centre puts it; an odd width
	// rounds the same way every time, so the edge is compared exactly.
	pillX := func(root *ui.Node) int {
		t.Helper()
		pill := findAction(root, panelControlCenterAction)
		if pill == nil || pill.Kind != ui.KindCapsule {
			t.Fatalf("rendered default bar has no centre pill: %+v", pill)
		}
		return pill.Bounds.X
	}

	root := render()
	content := bar.contentLocked(width, BarHeight)
	wantX := content.X + (content.W-bar.center[0].node.Bounds.W)/2
	if got := pillX(root); got != wantX {
		t.Fatalf("pill x without media = %d, want %d", got, wantX)
	}
	pill := bar.center[0].node
	pillBounds := pill.Bounds
	// The bar rebuilds a group's row from its members at layout, so the
	// hairline has to be one or it vanishes from the painted pill.
	if kids := bar.center[0].inner.Children; len(kids) != 4 || kids[1].Kind != ui.KindSeparator || kids[1].Bounds.W != 1 {
		t.Fatalf("laid-out pill row = %d nodes, want mark, a placed hairline, time and date", len(kids))
	}

	// The time, not only the mark, opens the control centre.
	clock := bar.center[0].inner.Children[2]
	if action, ok := bar.hitLocked(clock.Bounds.X+clock.Bounds.W/2, clock.Bounds.Y+clock.Bounds.H/2); !ok || action != panelControlCenterAction {
		t.Fatalf("hit on the time = %q, want %q", action, panelControlCenterAction)
	}

	art := &ui.Image{Width: mediaBarArtSize, Height: mediaBarArtSize, Stride: mediaBarArtSize * 4,
		Pix: make([]byte, mediaBarArtSize*mediaBarArtSize*4)}
	longTitle := strings.Repeat("A very long track title ", 12)
	present := barView{
		Now: reference, Media: services.MediaState{
			Available: true, Player: "org.mpris.MediaPlayer2.player",
			Status: services.PlaybackPlaying, Title: longTitle,
		}, MediaArt: art,
	}
	if !bar.apply(present) {
		t.Fatal("the present media snapshot reported no change")
	}
	root = render()
	if got := pillX(root); got != wantX {
		t.Fatalf("pill x with media = %d, want %d", got, wantX)
	}
	media := findAction(root, panelMediaAction)
	if media == nil {
		t.Fatal("rendered default bar has no media action")
	}
	if media.Bounds.X < pillBounds.X+pillBounds.W {
		t.Fatalf("media at x=%d overlaps the pill ending at %d", media.Bounds.X, pillBounds.X+pillBounds.W)
	}
	title := findNode(root, func(n *ui.Node) bool { return n.Key == "media-title" })
	if title == nil || title.Bounds.W > mediaMaxWidth || !title.Marquee {
		t.Fatalf("rendered media title = %+v, want max width %d and marquee", title, mediaMaxWidth)
	}
	if bar.center[1].inner.Action != panelMediaAction || bar.center[1].inner.Name != "Media" || bar.center[1].inner.Role != "button" {
		t.Fatalf("media accessibility = %+v", bar.center[1].inner)
	}
	if pill.Bounds != pillBounds {
		t.Fatalf("pill bounds changed with media: before=%+v after=%+v", pillBounds, pill.Bounds)
	}

	if !bar.apply(barView{Now: reference.Add(time.Minute), Media: present.Media, MediaArt: art}) {
		t.Fatal("the minute-boundary snapshot reported no change")
	}
	root = render()
	if got := pillX(root); got != wantX {
		t.Fatalf("pill x after minute boundary = %d, want %d", got, wantX)
	}
	if pill.Bounds != pillBounds {
		t.Fatalf("pill bounds changed at the minute boundary: before=%+v after=%+v", pillBounds, pill.Bounds)
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
	p.mu.Lock()
	root, _ := p.renderViewLocked()
	p.mu.Unlock()
	var hovered bool
	var walk func(*ui.Node)
	walk = func(n *ui.Node) {
		if n == nil {
			return
		}
		hovered = hovered || n.State.Has(ui.StateHovered)
		for _, child := range n.Children {
			walk(child)
		}
	}
	walk(root)
	if hovered {
		t.Error("the bar resolved hover onto its render copy; the furniture ruling holds no more")
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
