package shell

import (
	"time"

	"github.com/Nomadcxx/sysc-shell/internal/render"
	"github.com/Nomadcxx/sysc-shell/internal/theme"
	"github.com/Nomadcxx/sysc-shell/internal/ui"
)

// Catalogue transition durations. Press and panel visibility are asymmetric:
// a control commits faster than it releases, and a panel leaves faster than it
// arrives.
const (
	// reducedPanelCap bounds the one transition reduced motion keeps. The
	// token table can be slowed to four times its length, and a fade that long
	// is the thing reduced motion exists to prevent, so the cap is absolute
	// rather than another token.
	reducedPanelCap = 150 * time.Millisecond

	// panelSlidePx is how far a panel starts from its anchored edge.
	panelSlidePx = 8
	// pressScale is the visual shrink a pressed control takes. It changes the
	// painted rectangle only; layout bounds do not move.
	pressScale = 0.98

	// animTick is the frame cadence while a value is unsettled.
	animTick = 16 * time.Millisecond
)

// animChannel is one animated property of one keyed node.
type animChannel uint8

const (
	animHover animChannel = iota
	animPress
	animSelect
	// animVisible is the surface's own enter/exit, keyed by the surface rather
	// than a node.
	animVisible
	// animTheme crossfades a surface's palette, keyed by the surface.
	animTheme
)

// animKey addresses one value: a stable node key plus the channel. Keys are
// node identities that survive tree rebuilds, never node pointers.
type animKey struct {
	node    string
	channel animChannel
}

// animValue is one scalar in flight.
type animValue struct {
	from, to float64
	start    time.Time
	dur      time.Duration
	ease     func(float64) float64
}

func (v animValue) at(now time.Time) float64 {
	if v.dur <= 0 {
		return v.to
	}
	elapsed := now.Sub(v.start)
	if elapsed <= 0 {
		return v.from
	}
	if elapsed >= v.dur {
		return v.to
	}
	p := v.ease(float64(elapsed) / float64(v.dur))
	return v.from + (v.to-v.from)*p
}

func (v animValue) settled(now time.Time) bool {
	return v.dur <= 0 || now.Sub(v.start) >= v.dur
}

// animator holds one surface's in-flight visual state. It is deliberately
// concrete and package-private: it stores scalars keyed by node identity and
// never holds a renderer, a canvas, or a node pointer, so a tree rebuild cannot
// leave it pointing at freed state.
//
// One animator owns a surface. Every transition on that surface shares its
// clock, so a surface schedules frames from a single place.
type animator struct {
	now     func() time.Time
	reduced bool
	// motion is the resolved duration table, already divided by the speed
	// factor. The animator never scales it again: dividing once is what keeps
	// a recipe from being sped up twice.
	motion  theme.MotionTokens
	spatial theme.Curve
	values  map[animKey]animValue
	// running reports whether a frame loop is already ticking this surface, so
	// a second target change does not start a second ticker.
	running bool
}

func newAnimator(now func() time.Time, reduced bool, motion render.MotionSet) *animator {
	if now == nil {
		now = time.Now
	}
	m := motion.Durations
	if m == (theme.MotionTokens{}) {
		// A surface built before the tokens reached it still has to animate.
		m = theme.BaseMotion
	}
	return &animator{
		now: now, reduced: reduced || motion.Reduced,
		motion: m, spatial: motion.Spatial,
		values: map[animKey]animValue{},
	}
}

// easeFor is a method because the curve is a theme axis now: an expressive
// motion style settles every spatial recipe harder, not just selection.
func (a *animator) easeFor(channel animChannel) func(float64) float64 {
	// Selection settles harder so the moving indicator arrives decisively;
	// everything else uses the curve the theme chose.
	if channel == animSelect || a.spatial == theme.CurveOutQuart {
		return ui.EaseOutQuart
	}
	return ui.EaseOutCubic
}

// duration reports how long a channel takes in the given direction, honouring
// reduced motion. Hover, press, and selection snap; panel visibility keeps a
// short fade so a surface does not appear without warning.
func (a *animator) duration(channel animChannel, rising bool) time.Duration {
	if a.reduced {
		if channel == animVisible {
			return min(a.motion.Short, reducedPanelCap)
		}
		// A palette change snaps: it is a colour change, not motion, and
		// holding the old colours for any length of time is the thing reduced
		// motion has no reason to want.
		return 0
	}
	switch channel {
	case animHover:
		return a.motion.Short
	case animPress:
		// A control commits faster than it releases.
		if rising {
			return a.motion.Shorter
		}
		return a.motion.Short
	case animSelect:
		return a.motion.Medium
	case animVisible:
		// A panel leaves faster than it arrives.
		if rising {
			return a.motion.Medium
		}
		return a.motion.Short
	case animTheme:
		// A palette change is a whole-surface change, so it takes the time a
		// whole surface takes to arrive.
		return a.motion.Medium
	}
	return 0
}

// Target aims a value at to. A reversal starts from wherever the value is
// rendering right now, so a control that turns around mid-flight never jumps.
// Re-aiming at the current target is a no-op, which is what keeps an unchanged
// pointer position from invalidating the surface.
func (a *animator) Target(node string, channel animChannel, to float64) {
	key := animKey{node: node, channel: channel}
	now := a.now()
	current, ok := a.values[key]
	if ok && current.to == to {
		return
	}
	from := 0.0
	if ok {
		from = current.at(now)
	}
	dur := a.duration(channel, to > from)
	a.values[key] = animValue{from: from, to: to, start: now, dur: dur, ease: a.easeFor(channel)}
}

// Reset drops a value so the next Target starts it from zero rather than from
// wherever the previous transition left it. A palette crossfade needs this: its
// endpoints are the two themes, and the progress between them always begins at
// the start.
func (a *animator) Reset(node string, channel animChannel) {
	delete(a.values, animKey{node: node, channel: channel})
}

// has reports whether a channel has ever been aimed at anything. A value that
// was never targeted is not the same as one resting at zero: the first means
// there is nothing to resolve, the second that it resolved to zero.
func (a *animator) has(node string, channel animChannel) bool {
	_, ok := a.values[animKey{node: node, channel: channel}]
	return ok
}

// Value is the resolved scalar for one channel, in [0,1].
func (a *animator) Value(node string, channel animChannel) float64 {
	v, ok := a.values[animKey{node: node, channel: channel}]
	if !ok {
		return 0
	}
	return v.at(a.now())
}

// Settled reports whether every value has reached its target. While this is
// false the surface asks for another frame; once true it stops, so an idle
// shell schedules nothing.
func (a *animator) Settled() bool {
	now := a.now()
	for _, v := range a.values {
		if !v.settled(now) {
			return false
		}
	}
	return true
}

// Forget drops a node's values. A control that left the tree must not hold a
// transition open and keep the surface requesting frames.
func (a *animator) Forget(node string) {
	for key := range a.values {
		if key.node == node {
			delete(a.values, key)
		}
	}
}

// Retarget moves every colour-bearing value to a new palette by restarting the
// transitions in flight from their current rendered values. A theme change is
// a target change like any other, so it reuses the same clock.
func (a *animator) Retarget() {
	now := a.now()
	for key, v := range a.values {
		if v.settled(now) {
			continue
		}
		a.values[key] = animValue{from: v.at(now), to: v.to, start: now,
			dur: a.duration(key.channel, v.to > v.at(now)), ease: a.easeFor(key.channel)}
	}
}

// PressScale is the factor a pressed control paints at. Layout is untouched;
// only the visual rectangle and its contents shrink.
func (a *animator) PressScale(node string) float64 {
	return 1 - (1-pressScale)*a.Value(node, animPress)
}

// PanelSlide is how far, in logical pixels, a surface still sits from its
// anchored edge. Reduced motion moves nothing and fades instead.
func (a *animator) PanelSlide(node string) int {
	if a.reduced {
		return 0
	}
	return int(float64(panelSlidePx)*(1-a.Value(node, animVisible)) + 0.5)
}

// PanelOpacity is a surface's resolved opacity.
func (a *animator) PanelOpacity(node string) float64 {
	return a.Value(node, animVisible)
}

// animateSurface is the shell's only animation scheduling path. It publishes a
// frame per tick while the surface has a value in flight and returns as soon as
// everything settles, so an idle shell schedules nothing. Panels and the OSD
// both drive their frames through it rather than keeping timers of their own.
//
// settled and publish do their own locking: the surfaces they touch differ, and
// holding a lock across a publish would put the frame request under it.
func animateSurface(stop <-chan struct{}, settled func() bool, publish func()) {
	tick := time.NewTicker(animTick)
	defer tick.Stop()
	for {
		select {
		case <-stop:
			return
		case <-tick.C:
			done := settled()
			publish()
			if done {
				return
			}
		}
	}
}

// interaction is a surface's resolved pointer state, stored as stable node keys
// rather than node pointers. A tree rebuild replaces every node, so a pointer
// held across one is stale by definition; a key still names the same control.
type interaction struct {
	hover string
	press string
}

// setHover aims at a new hovered key and reports whether anything changed.
// Motion inside the same control returns false, which is what stops ordinary
// pointer movement from invalidating the surface every frame.
func (s *interaction) setHover(key string) bool {
	if s.hover == key {
		return false
	}
	s.hover = key
	return true
}

// setPress aims at a new pressed key and reports whether anything changed.
func (s *interaction) setPress(key string) bool {
	if s.press == key {
		return false
	}
	s.press = key
	return true
}

// clear drops all pointer state, as when the pointer leaves the surface.
func (s *interaction) clear() bool {
	changed := s.hover != "" || s.press != ""
	s.hover, s.press = "", ""
	return changed
}

// apply writes the resolved pointer state onto the current tree and aims the
// surface clock at it. Selection and disabled arrive from the composer and are
// preserved: they describe what a control is, not what the pointer is doing.
//
// Hosts share this so every surface resolves state the same way, and so a
// control cannot animate on one surface but not another.
func (s interaction) apply(root *ui.Node, anim *animator) {
	var walk func(*ui.Node)
	walk = func(n *ui.Node) {
		if n == nil {
			return
		}
		if interactive(n) {
			key := n.StableKey()
			hovered := key != "" && key == s.hover
			pressed := key != "" && key == s.press
			n.State &^= ui.StateHovered | ui.StatePressed
			if hovered {
				n.State |= ui.StateHovered
			}
			if pressed {
				n.State |= ui.StatePressed
			}
			// Only chrome carries a transition. A clickable tray row resolves
			// state so the host can react, but a display group that merely
			// reports a number never animates.
			if anim != nil && key != "" && ui.Animated(n) {
				anim.Target(key, animHover, boolValue(hovered))
				anim.Target(key, animPress, boolValue(pressed))
				anim.Target(key, animSelect, boolValue(n.State.Has(ui.StateSelected)))
			}
		}
		if n.Kind == ui.KindVirtualList && n.Item != nil {
			for i := 0; i < n.ItemCount; i++ {
				walk(n.Item(i))
			}
			return
		}
		for _, c := range n.Children {
			walk(c)
		}
	}
	walk(root)
}

func boolValue(b bool) float64 {
	if b {
		return 1
	}
	return 0
}

// interactive reports whether the pointer resolves state onto a node. Chrome
// always does; so does anything else the user can click, such as a tray row
// that is an image rather than a button. A node that only reports a value --
// the bar's CPU and memory groups -- carries no action and resolves nothing.
func interactive(n *ui.Node) bool {
	return ui.Animated(n) || (n != nil && n.Action != "")
}

// hoverKeyAt returns the stable key of the interactive node under a point, or ""
// when the point is over nothing that animates. A disabled control reports no
// key: it neither highlights nor activates.
func hoverKeyAt(root *ui.Node, x, y int) string {
	found := ""
	var walk func(*ui.Node)
	walk = func(n *ui.Node) {
		if n == nil || !n.Bounds.Contains(x, y) {
			return
		}
		if interactive(n) && !n.State.Has(ui.StateDisabled) {
			if key := n.StableKey(); key != "" {
				found = key
			}
		}
		if n.Kind == ui.KindVirtualList && n.Item != nil {
			for i := 0; i < n.ItemCount; i++ {
				walk(n.Item(i))
			}
			return
		}
		for _, c := range n.Children {
			walk(c)
		}
	}
	walk(root)
	return found
}
