package shell

import (
	"math"
	"testing"
	"time"

	metrics "github.com/Nomadcxx/sysc-metrics"
	"github.com/Nomadcxx/sysc-shell/internal/config"
	"github.com/Nomadcxx/sysc-shell/internal/platform/wayland"
	shellplugin "github.com/Nomadcxx/sysc-shell/internal/plugin"
	"github.com/Nomadcxx/sysc-shell/internal/services"
	"github.com/Nomadcxx/sysc-shell/internal/theme"
	"github.com/Nomadcxx/sysc-shell/internal/ui"
	v1 "github.com/Nomadcxx/sysc-shell/plugin/v1"
)

// revealPlugins stand in for the owner's laptop bar plugins.
func revealPlugins() map[string]*v1.Node {
	ev := []v1.EventKind{v1.EventActivate}
	icon := func(name string) *v1.Node { return &v1.Node{Kind: v1.KindIcon, Icon: name, Tone: v1.ToneAccent} }
	btn := func(id string, n *v1.Node) *v1.Node {
		n.Kind, n.ID, n.Role, n.Events = v1.KindButton, id, "button", ev
		if n.Name == "" {
			n.Name = id
		}
		return &v1.Node{Kind: v1.KindRow, Children: []*v1.Node{n}}
	}
	return map[string]*v1.Node{
		"airquality-live":            {Kind: v1.KindRow, Children: []*v1.Node{{Kind: v1.KindText, Text: "AIR 21  POL 34"}}},
		"thinkpad-controls-test":     btn("open", &v1.Node{Name: "ThinkPad", Text: "ThinkPad"}),
		"privacybit-testbench":       btn("open", &v1.Node{Name: "Privacy", Children: []*v1.Node{{Kind: v1.KindIcon, Icon: "remove_moderator"}}}),
		"protonvpn-1":                btn("bar", &v1.Node{Name: "ProtonVPN", Children: []*v1.Node{icon("proton")}}),
		"github-notifications-smoke": btn("open", &v1.Node{Name: "GitHub", Children: []*v1.Node{icon("github")}}),
		"moonbit-1":                  btn("bar", &v1.Node{Name: "Moonbit", Children: []*v1.Node{icon("moonbit")}}),
		"mini-docker-1":              btn("open", &v1.Node{Name: "Docker", Children: []*v1.Node{icon("docker")}}),
		"kdeconnect-1":               btn("open", &v1.Node{Icon: "smartphone", Name: "Phone"}),
	}
}

// revealPolicy is the owner's laptop bar: thirteen items on the right, the
// notifications bell last.
func revealPolicy(edge, style string) (config.Config, config.Bar) {
	cfg, policy := renderMatrixPolicy(edge, style, "attached", "compact")
	cfg, policy = withMatrixDensity(cfg, policy, theme.DensityComfortable)
	plug := func(id, inst string) config.Item {
		return config.Item{ID: "plugin", Plugin: "org.sysc." + id, Entry: "bar", Instance: inst}
	}
	policy.Left = []config.Item{{ID: "launcher"}, {ID: "workspace"}, {ID: "window-title", MaxWidth: 260}}
	policy.Center = []config.Item{
		{ID: "group", Items: []config.Item{{ID: "wordmark"}, {ID: "clock", Format: "15:04"}, {ID: "clock", Format: "Mon 2 Jan"}}},
		plug("airquality", "airquality-live"),
	}
	policy.Right = []config.Item{
		{ID: "battery", Label: "percent", WarnBelow: 20},
		plug("thinkpad", "thinkpad-controls-test"), plug("privacy", "privacybit-testbench"),
		{ID: "wifi"}, {ID: "clipboard"}, plug("protonvpn", "protonvpn-1"), {ID: "weather"}, {ID: "volume"},
		plug("github-notifications", "github-notifications-smoke"), plug("moonbit", "moonbit-1"),
		plug("mini-docker", "mini-docker-1"), plug("kdeconnect", "kdeconnect-1"), {ID: "notifications"},
	}
	return cfg, policy
}

type revealRigOptions struct {
	edge, style string
	outW, outH  int
	tray        bool
	reduced     bool
}

// revealRig drives one bar through a reveal on a fake clock. Timers are
// recorded rather than run; at() fires the bar's timer body itself.
type revealRig struct {
	t      *testing.T
	bar    *Bar
	policy config.Bar
	clock  *fakeClock
	start  time.Time
	w, h   int
	timers []time.Duration
	held   bool
	acted  []string
	frames map[string]pluginFrame
}

func newRevealRig(t *testing.T, o revealRigOptions) *revealRig {
	t.Helper()
	if o.edge == "" {
		o.edge = "top"
	}
	if o.style == "" {
		o.style = "frosted"
	}
	if o.outW == 0 {
		o.outW = 1600
	}
	if o.outH == 0 {
		o.outH = 1080
	}
	cfg, policy := revealPolicy(o.edge, o.style)
	frames := map[string]pluginFrame{}
	for inst, wire := range revealPlugins() {
		root, err := shellplugin.Convert(wire, v1.ViewBar)
		if err != nil {
			t.Fatal(inst, err)
		}
		stampPluginActions(root, inst)
		frames[inst] = pluginFrame{Root: root, Revision: 1, ViewID: inst}
	}
	bar := newRenderMatrixBar(t, cfg, policy, pluginFrame{}, false)
	if !o.tray {
		bar.setTray(nil, config.TrayPreferences{}, nil)
	}
	bar.apply(revealView(frames, 7))
	bar.stopAnimation()
	r := &revealRig{t: t, bar: bar, policy: policy, clock: &fakeClock{t: time.Unix(1000, 0)}, frames: frames}
	r.start = r.clock.t
	bar.mu.Lock()
	bar.anim = newAnimator(r.clock.now, o.reduced, bar.theme.Motion)
	bar.revealAfter = func(d time.Duration, _ func()) { r.timers = append(r.timers, d) }
	bar.holdProbe = func() bool { return r.held }
	bar.onAction = func(action string, _ uint32) bool { r.acted = append(r.acted, action); return true }
	bar.mu.Unlock()
	r.w, r.h = barSurfaceSize(policy, o.outW, o.outH)
	if err := bar.Configure(r.w, r.h, 120); err != nil {
		t.Fatal(err)
	}
	r.frame()
	return r
}

func revealView(frames map[string]pluginFrame, temperature float64) barView {
	return barView{
		Now:     time.Date(2026, time.November, 8, 16, 10, 0, 0, time.UTC),
		Title:   "M3 overflow execution handover",
		Pills:   []workspacePill{{ID: 11, Index: 1, Name: "one", Focused: true}, {ID: 12, Index: 2, Name: "two", Occupied: true}},
		Plugins: frames,
		Metrics: batteryAt(0.49, metrics.BatteryDischarging),
		Weather: services.Reading{Observed: true, Temperature: temperature, Code: 2, Unit: services.UnitCelsius},
		Audio:   services.AudioState{Level: 40, Muted: true},
		Network: services.NetworkState{Kind: services.ConnWireless, WirelessEnabled: true, Connected: true, Strength: 80},
		Unread:  3,
	}
}

func (r *revealRig) frame() {
	r.t.Helper()
	pix := make([]byte, r.w*r.h*4)
	if err := r.bar.Render(pix, r.w, r.h, r.w*4); err != nil {
		r.t.Fatal(err)
	}
}

// at moves the clock to d after the rig started, fires the timer body as a
// due timer would, and renders.
func (r *revealRig) at(d time.Duration) {
	r.t.Helper()
	r.clock.t = r.start.Add(d)
	r.bar.revealCheck()
	r.frame()
}

func (r *revealRig) pointer(kind wayland.EventKind, x, y int) {
	r.t.Helper()
	r.bar.Handle(wayland.Event{Kind: kind, X: float64(x) + 0.5, Y: float64(y) + 0.5, Button: 0x110})
	r.frame()
}

func (r *revealRig) move(x, y int) { r.pointer(wayland.EventPointerMotion, x, y) }
func (r *revealRig) leave()        { r.pointer(wayland.EventPointerLeave, 0, 0) }

// edge is the last pixel of the bar at its end-section end.
func (r *revealRig) edge() (int, int) {
	if r.policy.Edge == "left" || r.policy.Edge == "right" {
		return r.w / 2, r.h - 1
	}
	return r.w - 1, r.h / 2
}

func (r *revealRig) middle() (int, int) { return r.w / 2, r.h / 2 }

func (r *revealRig) shift() float64 {
	r.bar.mu.Lock()
	defer r.bar.mu.Unlock()
	return r.bar.anim.Value(revealKey(ui.SideEnd), animReveal)
}

func (r *revealRig) tint() float64 {
	r.bar.mu.Lock()
	defer r.bar.mu.Unlock()
	return r.bar.anim.Value(revealTintKey(ui.SideEnd), animRevealTint)
}

func (r *revealRig) phase() revealPhase {
	r.bar.mu.Lock()
	defer r.bar.mu.Unlock()
	return r.bar.reveal[ui.SideEnd].phase
}

func (r *revealRig) maxShift() float64 {
	r.bar.mu.Lock()
	defer r.bar.mu.Unlock()
	return float64(r.bar.reveal[ui.SideEnd].strip.MaxShift(ui.SideEnd))
}

func (r *revealRig) hit(b ui.Rect) (string, bool) {
	r.bar.mu.Lock()
	defer r.bar.mu.Unlock()
	return r.bar.hitLocked(b.X+b.W/2, b.Y+b.H/2)
}

func (r *revealRig) rightBounds() []ui.Rect {
	r.bar.mu.Lock()
	defer r.bar.mu.Unlock()
	var out []ui.Rect
	for _, n := range r.bar.sections()[2] {
		out = append(out, n.Bounds)
	}
	return out
}

// open rests the pointer at the edge until the section is revealed at 3.5 s.
func (r *revealRig) open() {
	r.t.Helper()
	if r.maxShift() <= 0 {
		r.t.Fatal("the rig's right section does not overflow")
	}
	r.move(r.edge())
	r.at(2 * time.Second)
	r.at(3500 * time.Millisecond)
	if r.phase() != revealOpen || r.shift() != r.maxShift() {
		r.t.Fatalf("not open at 3.5 s: phase %d shift %.2f of %.0f", r.phase(), r.shift(), r.maxShift())
	}
}

func TestRevealPassingPointerShowsNothing(t *testing.T) {
	r := newRevealRig(t, revealRigOptions{})
	r.move(r.edge())
	r.at(200 * time.Millisecond)
	if r.shift() != 0 || r.tint() != 0 {
		t.Fatalf("at 0.2 s shift %.2f tint %.2f, want nothing shown", r.shift(), r.tint())
	}
	r.move(r.middle())
	r.at(3 * time.Second)
	if r.phase() != revealRest || r.shift() != 0 || r.tint() != 0 {
		t.Fatalf("after passing: phase %d shift %.2f tint %.2f, want rest", r.phase(), r.shift(), r.tint())
	}
}

func TestRevealDwellOpens(t *testing.T) {
	r := newRevealRig(t, revealRigOptions{})
	if b := r.bar.actionBounds(panelNotificationsAction); b != (ui.Rect{}) {
		t.Fatalf("bell visible at rest: %+v", b)
	}
	r.move(r.edge())
	r.at(1900 * time.Millisecond)
	if r.phase() != revealArming || r.shift() <= 0 || r.shift() > revealLean || r.tint() <= 0 {
		t.Fatalf("at 1.9 s phase %d shift %.2f tint %.2f, want a lean of at most %v and a tint", r.phase(), r.shift(), r.tint(), revealLean)
	}
	r.at(2 * time.Second)
	if r.phase() != revealOpening {
		t.Fatalf("at 2 s phase %d, want opening", r.phase())
	}
	r.at(3500 * time.Millisecond)
	if r.phase() != revealOpen || r.shift() != r.maxShift() {
		t.Fatalf("at 3.5 s phase %d shift %.2f, want open at %.0f", r.phase(), r.shift(), r.maxShift())
	}
	bell := r.bar.actionBounds(panelNotificationsAction)
	body := r.bar.bodyLocked(r.w, r.h)
	if bell.W == 0 || bell.X < body.X || bell.X+bell.W > body.X+body.W {
		t.Fatalf("bell at %+v, want inside the body %+v", bell, body)
	}
	if action, ok := r.hit(bell); !ok || action != panelNotificationsAction {
		t.Fatalf("hit at the bell = %q %v, want %q", action, ok, panelNotificationsAction)
	}
	if _, _, _, ok := r.bar.tooltipAt(bell.X+bell.W/2, bell.Y+bell.H/2); !ok {
		t.Fatal("no tooltip at the revealed bell")
	}
}

func TestRevealLeaveEarlySpringsHome(t *testing.T) {
	r := newRevealRig(t, revealRigOptions{})
	rest := r.rightBounds()
	r.move(r.edge())
	r.at(1500 * time.Millisecond)
	if r.shift() <= 0 {
		t.Fatalf("no lean at 1.5 s: %.2f", r.shift())
	}
	r.move(r.middle())
	r.at(3500 * time.Millisecond)
	if r.phase() != revealRest || r.shift() != 0 || r.tint() != 0 {
		t.Fatalf("after leaving early: phase %d shift %.2f tint %.2f, want rest", r.phase(), r.shift(), r.tint())
	}
	for i, b := range r.rightBounds() {
		if b != rest[i] {
			t.Fatalf("item %d at %+v, want its resting %+v", i, b, rest[i])
		}
	}
}

func TestRevealNoHitWhileMoving(t *testing.T) {
	r := newRevealRig(t, revealRigOptions{})
	r.move(r.edge())
	r.at(2 * time.Second)
	r.at(2100 * time.Millisecond)
	var target ui.Rect
	r.bar.mu.Lock()
	for _, n := range r.bar.sections()[2] {
		if n.Bounds.W > 0 && n.Action != "" {
			target = n.Bounds
		}
	}
	r.bar.mu.Unlock()
	if target.W == 0 {
		t.Fatal("no clickable right item placed mid-move")
	}
	if action, ok := r.hit(target); ok {
		t.Fatalf("hit %q mid-move, want nothing", action)
	}
	r.pointer(wayland.EventPointerPress, target.X+target.W/2, target.Y+target.H/2)
	r.pointer(wayland.EventPointerRelease, target.X+target.W/2, target.Y+target.H/2)
	if len(r.acted) != 0 {
		t.Fatalf("delivered %v mid-move, want nothing", r.acted)
	}
}

func TestRevealParkedAtEdgeStaysOpen(t *testing.T) {
	r := newRevealRig(t, revealRigOptions{})
	r.open()
	r.move(r.edge())
	r.at(20 * time.Second)
	if r.phase() != revealOpen || r.shift() != r.maxShift() {
		t.Fatalf("pointer parked at the edge: phase %d shift %.2f, want still open", r.phase(), r.shift())
	}
}

func TestRevealGraceThenGlide(t *testing.T) {
	r := newRevealRig(t, revealRigOptions{})
	r.open()
	r.at(4 * time.Second)
	r.leave()
	if len(r.timers) == 0 || r.timers[len(r.timers)-1] != revealHoldPoll {
		t.Fatalf("timers after leaving = %v, want a %v poll", r.timers, revealHoldPoll)
	}
	r.at(11900 * time.Millisecond)
	if r.phase() != revealOpen || r.shift() != r.maxShift() {
		t.Fatalf("at 11.9 s phase %d shift %.2f, want still open", r.phase(), r.shift())
	}
	r.at(12 * time.Second)
	if r.phase() != revealClosing {
		t.Fatalf("at 12 s phase %d, want the glide home", r.phase())
	}
	r.at(12600 * time.Millisecond)
	if s := r.shift(); s <= 0 || s >= r.maxShift() {
		t.Fatalf("at 12.6 s shift %.2f, want part way home", s)
	}
	r.at(13300 * time.Millisecond)
	if r.phase() != revealRest || r.shift() != 0 {
		t.Fatalf("at 13.3 s phase %d shift %.2f, want rest", r.phase(), r.shift())
	}
}

func TestRevealPointerBackDuringWaitAndGlide(t *testing.T) {
	r := newRevealRig(t, revealRigOptions{})
	r.open()
	r.at(4 * time.Second)
	r.leave()
	r.at(8 * time.Second)
	r.pointer(wayland.EventPointerEnter, r.w/2, r.h/2)
	r.at(13 * time.Second)
	if r.phase() != revealOpen {
		t.Fatalf("pointer back at 8 s: phase %d at 13 s, want open", r.phase())
	}
	r.leave()
	r.at(21 * time.Second)
	if r.phase() != revealClosing {
		t.Fatalf("at 21 s phase %d, want the glide home", r.phase())
	}
	r.at(21500 * time.Millisecond)
	r.pointer(wayland.EventPointerEnter, r.w/2, r.h/2)
	if r.phase() != revealOpening {
		t.Fatalf("pointer back mid-glide: phase %d, want opening", r.phase())
	}
	r.at(23500 * time.Millisecond)
	if r.phase() != revealOpen || r.shift() != r.maxShift() {
		t.Fatalf("at 23.5 s phase %d shift %.2f, want open again", r.phase(), r.shift())
	}
}

func TestRevealTrayItemFollows(t *testing.T) {
	r := newRevealRig(t, revealRigOptions{tray: true})
	r.open()
	r.bar.mu.Lock()
	tray := r.bar.trayNodes
	var last *ui.Node
	if len(tray) > 0 {
		last = tray[len(tray)-1]
	}
	r.bar.mu.Unlock()
	if last == nil || last.Bounds.W == 0 {
		t.Fatalf("last tray item not revealed: %+v", last)
	}
	b := last.Bounds
	if action, ok := r.hit(b); !ok || action != last.Action {
		t.Fatalf("hit at the revealed tray item = %q %v, want %q", action, ok, last.Action)
	}
	if tip, _, _, ok := r.bar.tooltipAt(b.X+b.W/2, b.Y+b.H/2); !ok || tip != last.Tooltip {
		t.Fatalf("tooltip at the revealed tray item = %q %v, want %q", tip, ok, last.Tooltip)
	}
}

func TestRevealRelayoutWhileOpen(t *testing.T) {
	r := newRevealRig(t, revealRigOptions{})
	r.open()
	before := r.maxShift()
	// The phone pill sits in the hidden tail; a longer label there lengthens
	// the run without touching the resting prefix.
	wire := &v1.Node{Kind: v1.KindRow, Children: []*v1.Node{{Kind: v1.KindButton, ID: "open", Role: "button",
		Name: "Phone", Icon: "smartphone", Text: "Pixel 8 Pro", Events: []v1.EventKind{v1.EventActivate}}}}
	root, err := shellplugin.Convert(wire, v1.ViewBar)
	if err != nil {
		t.Fatal(err)
	}
	stampPluginActions(root, "kdeconnect-1")
	r.frames["kdeconnect-1"] = pluginFrame{Root: root, Revision: 2, ViewID: "kdeconnect-1"}
	r.bar.apply(revealView(r.frames, 7))
	r.frame()
	if after := r.maxShift(); after == before {
		t.Fatalf("max shift unchanged at %.0f after a wider reading", after)
	}
	if r.phase() != revealOpen || r.shift() != r.maxShift() {
		t.Fatalf("after relayout: phase %d shift %.2f, want open at the new %.0f", r.phase(), r.shift(), r.maxShift())
	}
	if err := r.bar.Configure(3440, r.h, 120); err != nil {
		t.Fatal(err)
	}
	r.w = 3440
	r.frame()
	if r.phase() != revealRest || r.shift() != 0 {
		t.Fatalf("section fits a wider bar: phase %d shift %.2f, want rest", r.phase(), r.shift())
	}
}

func TestRevealVerticalBottomEdge(t *testing.T) {
	r := newRevealRig(t, revealRigOptions{edge: "right", outH: 800})
	r.open()
	content := r.bar.contentLocked(r.w, r.h)
	bounds := r.rightBounds()
	last := bounds[len(bounds)-1]
	if last.H == 0 || last.Y+last.H != content.Y+content.H {
		t.Fatalf("last item at %+v, want flush to the content end %d", last, content.Y+content.H)
	}
}

func TestRevealReducedMotion(t *testing.T) {
	r := newRevealRig(t, revealRigOptions{reduced: true})
	r.move(r.edge())
	r.at(time.Second)
	if r.shift() != 0 || r.tint() != revealTint {
		t.Fatalf("reduced at 1 s: shift %.2f tint %.2f, want no lean and the tint on", r.shift(), r.tint())
	}
	r.at(2 * time.Second)
	if r.shift() != r.maxShift() {
		t.Fatalf("reduced at 2 s: shift %.2f, want %.0f at once", r.shift(), r.maxShift())
	}
	r.at(3 * time.Second)
	r.leave()
	r.at(11 * time.Second)
	if r.shift() != 0 {
		t.Fatalf("reduced at the end of the grace: shift %.2f, want home at once", r.shift())
	}
}

func TestRevealIslandsBlurClipped(t *testing.T) {
	r := newRevealRig(t, revealRigOptions{style: "islands"})
	r.move(r.edge())
	r.at(2 * time.Second)
	r.at(2150 * time.Millisecond)
	r.bar.mu.Lock()
	window := r.bar.reveal[ui.SideEnd].window
	r.bar.mu.Unlock()
	if window.W == 0 {
		t.Fatal("no window mid-move")
	}
	if math.IsNaN(r.shift()) {
		t.Fatal("shift is NaN")
	}
	for _, b := range r.bar.blurShape() {
		if b.X+b.W <= window.X {
			continue
		}
		if b.X < window.X || b.X+b.W > window.X+window.W {
			t.Fatalf("blur rect %+v escapes the section window %+v", b, window)
		}
	}
}

// firstClickable is the first right item with an action that shows now.
func (r *revealRig) firstClickable() (ui.Rect, string) {
	r.bar.mu.Lock()
	defer r.bar.mu.Unlock()
	for _, n := range r.bar.sections()[2] {
		var found *ui.Node
		walkNodes(n, func(c *ui.Node) {
			if found == nil && c.Action != "" && c.Bounds.W > 0 {
				found = c
			}
		})
		if found != nil {
			return found.Bounds, found.Action
		}
	}
	r.t.Fatal("no clickable right item shows")
	return ui.Rect{}, ""
}

func (r *revealRig) drag(from ui.Rect, dx int) {
	x, y := from.X+from.W/2, from.Y+from.H/2
	r.pointer(wayland.EventPointerMotion, x, y)
	r.pointer(wayland.EventPointerPress, x, y)
	r.pointer(wayland.EventPointerMotion, x+dx, y)
	r.pointer(wayland.EventPointerRelease, x+dx, y)
}

func TestRevealSwipeOpensAndCloses(t *testing.T) {
	r := newRevealRig(t, revealRigOptions{})
	item, _ := r.firstClickable()
	r.drag(item, -30)
	if len(r.acted) != 0 {
		t.Fatalf("a swipe delivered %v, want no click", r.acted)
	}
	if r.phase() != revealOpening {
		t.Fatalf("after swiping toward the centre: phase %d, want opening", r.phase())
	}
	r.at(2 * time.Second)
	if r.phase() != revealOpen {
		t.Fatalf("at 2 s phase %d, want open", r.phase())
	}
	item, _ = r.firstClickable()
	r.drag(item, 30)
	if len(r.acted) != 0 || r.phase() != revealClosing {
		t.Fatalf("after swiping back: acted %v phase %d, want no click and closing", r.acted, r.phase())
	}
	r.move(r.middle())
	r.at(4 * time.Second)
	if r.phase() != revealRest || r.shift() != 0 {
		t.Fatalf("at 4 s phase %d shift %.2f, want rest though the pointer stayed on the bar", r.phase(), r.shift())
	}
}

func TestRevealShortDragStillClicks(t *testing.T) {
	r := newRevealRig(t, revealRigOptions{})
	item, action := r.firstClickable()
	r.drag(item, -10)
	if len(r.acted) != 1 || r.acted[0] != action || r.phase() != revealRest {
		t.Fatalf("short drag: acted %v phase %d, want one click on %q and rest", r.acted, r.phase(), action)
	}
}

func TestRevealHeldByOpenMenu(t *testing.T) {
	r := newRevealRig(t, revealRigOptions{})
	r.open()
	r.at(4 * time.Second)
	r.leave()
	r.held = true
	for s := 5; s <= 20; s++ {
		r.at(time.Duration(s) * time.Second)
	}
	r.held = false
	r.at(27 * time.Second)
	if r.phase() != revealOpen {
		t.Fatalf("7 s after the menu closed: phase %d, want still open", r.phase())
	}
	r.at(28 * time.Second)
	if r.phase() != revealClosing {
		t.Fatalf("8 s after the menu closed: phase %d, want the glide home", r.phase())
	}
}
