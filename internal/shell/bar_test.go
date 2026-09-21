package shell

import (
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Nomadcxx/sysc-shell/internal/config"
	"github.com/Nomadcxx/sysc-shell/internal/platform/wayland"
	"github.com/Nomadcxx/sysc-shell/internal/services"
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

func TestDefaultCentreKeepsWordmarkAnchoredAcrossMediaAndClockChanges(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	const mediaMaxWidth = 180
	cfg.Bar.Center[2].MaxWidth = mediaMaxWidth
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
	wordmarkCentre := func(root *ui.Node) int {
		t.Helper()
		mark := findAction(root, panelControlCenterAction)
		if mark == nil {
			t.Fatal("rendered default bar has no wordmark")
		}
		return mark.Bounds.X + mark.Bounds.W/2
	}

	root := render()
	content := bar.contentLocked(width, BarHeight)
	wantCentre := content.X + content.W/2
	if got := wordmarkCentre(root); got != wantCentre {
		t.Fatalf("wordmark centre without media = %d, want %d", got, wantCentre)
	}
	if len(bar.sections()[1]) != 2 {
		t.Fatalf("centre without media = %d nodes, want group and wordmark", len(bar.sections()[1]))
	}
	group := bar.center[0]
	if group.node.Kind != ui.KindCapsule || group.inner == nil || group.inner.Kind != ui.KindRow || len(group.inner.Children) != 2 {
		t.Fatalf("default time/date group = %+v, want one capsule with two clocks", group)
	}
	wordmark := bar.center[1].node
	if wordmark.Action != panelControlCenterAction || wordmark.Name != "Control centre" || wordmark.Role != "button" {
		t.Fatalf("wordmark accessibility = %+v", wordmark)
	}
	groupBounds := group.node.Bounds

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
	if got := wordmarkCentre(root); got != wantCentre {
		t.Fatalf("wordmark centre with media = %d, want %d", got, wantCentre)
	}
	media := findAction(root, panelMediaAction)
	if media == nil {
		t.Fatal("rendered default bar has no media action")
	}
	title := findNode(root, func(n *ui.Node) bool { return n.Key == "media-title" })
	if title == nil || title.Bounds.W > mediaMaxWidth || !title.Marquee {
		t.Fatalf("rendered media title = %+v, want max width %d and marquee", title, mediaMaxWidth)
	}
	if got := bar.center[2].inner.Children[0].Kind; got != ui.KindImage {
		t.Fatalf("media leading node kind = %d, want resolved art image", got)
	}
	if bar.center[2].inner.Action != panelMediaAction || bar.center[2].inner.Name != "Media" || bar.center[2].inner.Role != "button" {
		t.Fatalf("media accessibility = %+v", bar.center[2].inner)
	}
	if group.node.Bounds != groupBounds {
		t.Fatalf("clock group bounds changed with media: before=%+v after=%+v", groupBounds, group.node.Bounds)
	}

	if !bar.apply(barView{Now: reference.Add(time.Minute), Media: present.Media, MediaArt: art}) {
		t.Fatal("the minute-boundary snapshot reported no change")
	}
	root = render()
	if got := wordmarkCentre(root); got != wantCentre {
		t.Fatalf("wordmark centre after minute boundary = %d, want %d", got, wantCentre)
	}
	if bar.center[0].node.Bounds != groupBounds {
		t.Fatalf("clock group bounds changed at the minute boundary: before=%+v after=%+v", groupBounds, bar.center[0].node.Bounds)
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
