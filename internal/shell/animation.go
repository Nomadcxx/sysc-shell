package shell

import (
	"sync/atomic"
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

	// animTick is how often an unsettled value is resampled. It is half a
	// 60Hz frame rather than a whole one: a ticker at the frame period beats
	// against the compositor's vblank, drifting a whole frame every few
	// seconds, so some frames present no new value and others skip one.
	// Sampling twice a frame keeps every presented frame within half a tick of
	// the clock whatever the phase.
	//
	// This paces sampling, not painting. A frame is still only drawn when the
	// compositor returns the frame callback, so a faster tick costs a channel
	// send and a dirty flag, never an extra blit.
	animTick = 8 * time.Millisecond
	// effectTrip bounds one effect phase cycle. The surface frame cap controls
	// how often it is painted; this duration only controls the phase itself.
	// Weather is meant to drift, not to hurry: a short trip made clouds and
	// haze travel at a speed no sky moves at.
	effectTrip = 9 * time.Second
	// effectFrameCap paces a surface whose only work in flight is an effect.
	// A drifting scene on a 9-second cycle is indistinguishable at this rate
	// from one painted every vsync, and the difference is most of a core: an
	// effect repaints the whole surface, and its loop never settles, so an
	// open weather panel would otherwise animate at the interaction cadence
	// for as long as it is open.
	effectFrameCap = 66 * time.Millisecond
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
	// animGradient shifts a looping paint recipe independently of interaction.
	animGradient
	// animSweep is a linear wrapping paint offset, such as a marquee title.
	animSweep
	// animEffect is the phase of a host-owned effect layer.
	animEffect
	// animProgress is a declarative value glide: a plugin-flagged meter or
	// gauge glides to each revision's target instead of jumping.
	animProgress
	// animSprite is a plugin sprite cycle's phase: a linear 0-to-1 loop over
	// one pass through the icon's frames.
	animSprite
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
	loop     ui.GradientMotion
	// steps and poses belong to a sprite cycle: how many poses one pass
	// shows, and a fingerprint of which ones, so a new pose list restarts
	// the cycle while a new speed for the same list keeps its phase.
	steps int
	poses uint64
}

func (v animValue) at(now time.Time) float64 {
	if v.dur <= 0 {
		return v.to
	}
	elapsed := now.Sub(v.start)
	if elapsed <= 0 {
		return v.from
	}
	if v.loop != ui.GradientNone {
		trip := elapsed % v.dur
		if v.loop == ui.GradientPingPong {
			cycle := 2 * v.dur
			trip = elapsed % cycle
			if trip > v.dur {
				trip = cycle - trip
			}
		}
		p := float64(trip) / float64(v.dur)
		return v.from + (v.to-v.from)*p
	}
	if elapsed >= v.dur {
		return v.to
	}
	p := v.ease(float64(elapsed) / float64(v.dur))
	return v.from + (v.to-v.from)*p
}

func (v animValue) settled(now time.Time) bool {
	if v.dur <= 0 {
		return true
	}
	if v.loop != ui.GradientNone {
		return false
	}
	return now.Sub(v.start) >= v.dur
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
	// wake is nudged whenever a value is aimed somewhere new, so a frame
	// loop resting between sprite poses picks up a hover or a glide at once
	// instead of at the next pose.
	wake chan struct{}
	// running reports whether a frame loop is already ticking this surface, so
	// a second target change does not start a second ticker. It is atomic
	// because panel frame loops start from paths that hold no registry lock;
	// bar loops start under the bar lock and simply load it.
	running atomic.Bool
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
		wake:   make(chan struct{}, 1),
	}
}

// nudge wakes a resting frame loop. It never blocks: one pending nudge is
// as good as many.
func (a *animator) nudge() {
	select {
	case a.wake <- struct{}{}:
	default:
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
	case animProgress:
		// A value glide reads as one motion whatever its direction: a battery
		// filling and draining both travel the same road.
		return a.motion.Medium
	}
	return 0
}

// resolveSpriteMotion steps every sprite icon through its frames, writing
// the pose the surface clock has reached into the render copy's Icon the way
// resolveProgressMotion writes a glided value. Under reduced motion nothing
// is tracked and the icon keeps its resting pose. Keys whose nodes left the
// tree are retired, which is also what ends a sprite's frames.
func resolveSpriteMotion(anim *animator, root *ui.Node) {
	if anim == nil {
		return
	}
	seen := make(map[string]bool)
	var walk func(*ui.Node)
	walk = func(n *ui.Node) {
		if n == nil {
			return
		}
		if n.Kind == ui.KindIcon && len(n.Frames) > 1 && n.Cycle > 0 && !anim.reduced {
			if key := n.StableKey(); key != "" {
				seen[key] = true
				anim.TargetCycle(key, n.Cycle, len(n.Frames), posePrint(n.Frames))
				pose := int(anim.Value(key, animSprite) * float64(len(n.Frames)))
				n.Icon = n.Frames[min(max(pose, 0), len(n.Frames)-1)]
			}
		}
		for _, child := range n.Children {
			walk(child)
		}
	}
	walk(root)
	for key := range anim.values {
		if key.channel == animSprite && !seen[key.node] {
			delete(anim.values, key)
		}
	}
}

// posePrint fingerprints a pose list without allocating: FNV-1a over the
// names with a separator, so ["ab","c"] and ["a","bc"] differ.
func posePrint(frames []string) uint64 {
	h := uint64(14695981039346656037)
	for _, f := range frames {
		for i := 0; i < len(f); i++ {
			h = (h ^ uint64(f[i])) * 1099511628211
		}
		h = (h ^ 0xff) * 1099511628211
	}
	return h
}

// resolveProgressMotion glides every animated meter and gauge toward the value
// its tree carries, writing the interpolated value back into the node the way
// the marquee walk writes its sweep offset. Target is idempotent for an
// unchanged target, so a resolve per frame keeps one transition in flight and
// lets it settle; a changed target retargets from wherever the value is now.
// Reduced motion collapses the glide: duration returns zero, so the first
// resolve writes the target directly and nothing stays in flight. Keys whose
// nodes left the tree are retired so the animator never grows on a rebuild
// churn.
func resolveProgressMotion(anim *animator, root *ui.Node) {
	if anim == nil {
		return
	}
	seen := make(map[string]bool)
	var walk func(*ui.Node)
	walk = func(n *ui.Node) {
		if n == nil {
			return
		}
		if n.Animate && (n.Kind == ui.KindMeter || n.Kind == ui.KindRadialGauge) {
			if key := n.StableKey(); key != "" {
				seen[key] = true
				anim.Target(key, animProgress, n.Value)
				n.Value = anim.Value(key, animProgress)
			}
		}
		for _, child := range n.Children {
			walk(child)
		}
	}
	walk(root)
	for key := range anim.values {
		if key.channel == animProgress && !seen[key.node] {
			delete(anim.values, key)
		}
	}
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
	a.nudge()
}

// TargetLoop runs a linear paint offset until the node leaves the tree. An
// unchanged recipe keeps its start time, so rebuilding the tree preserves the
// visible phase.
func (a *animator) TargetLoop(node string, channel animChannel, from, to float64, dur time.Duration, motion ui.GradientMotion) {
	key := animKey{node: node, channel: channel}
	if current, ok := a.values[key]; ok && current.from == from && current.to == to &&
		current.dur == dur && current.loop == motion {
		return
	}
	now := a.now()
	if a.reduced {
		mid := (from + to) / 2
		a.values[key] = animValue{from: mid, to: mid, start: now}
		return
	}
	a.values[key] = animValue{from: from, to: to, start: now, dur: dur, loop: motion}
	a.nudge()
}

// TargetSweep runs a linear 0-to-1 phase until the node leaves the tree. It
// deliberately uses the linear loop path rather than the gradient's ping-pong
// mode: a marquee must never scroll backward at the seam.
func (a *animator) TargetSweep(node string, channel animChannel, dur time.Duration) {
	key := animKey{node: node, channel: channel}
	if current, ok := a.values[key]; ok && current.from == 0 && current.to == 1 &&
		current.dur == dur && current.loop == ui.GradientLoop {
		return
	}
	now := a.now()
	if a.reduced || dur <= 0 {
		a.values[key] = animValue{from: 0, to: 0, start: now}
		return
	}
	a.values[key] = animValue{from: 0, to: 1, start: now, dur: dur, loop: ui.GradientLoop}
	a.nudge()
}

// TargetCycle runs a sprite's phase: a linear 0-to-1 loop, one pass every
// cycle, over steps poses fingerprinted by poses. The same cycle is a no-op,
// so a rebuild keeps the phase. A new cycle for the same poses keeps the
// phase too -- the start is rebased so the pose on screen stays put and only
// the speed changes -- because a plugin that retargets its speed on every
// sample would otherwise hitch the motion each time. New poses start from
// the first.
func (a *animator) TargetCycle(node string, cycle time.Duration, steps int, poses uint64) {
	key := animKey{node: node, channel: animSprite}
	current, ok := a.values[key]
	if ok && current.dur == cycle && current.steps == steps && current.poses == poses {
		return
	}
	now := a.now()
	start := now
	if ok && current.poses == poses && current.steps == steps && current.dur > 0 {
		start = now.Add(-time.Duration(current.at(now) * float64(cycle)))
	}
	a.values[key] = animValue{from: 0, to: 1, start: start, dur: cycle, loop: ui.GradientLoop,
		steps: steps, poses: poses}
	a.nudge()
}

// SpriteRest reports how long a frame loop may sleep when every value still
// in flight is a sprite cycle: until the soonest next pose, when the picture
// can next change. ok is false while anything else is moving, which keeps
// the loop on its ordinary tick.
func (a *animator) SpriteRest() (time.Duration, bool) {
	now := a.now()
	rest, sprites := time.Duration(0), false
	for key, v := range a.values {
		if key.channel != animSprite {
			if !v.settled(now) {
				return 0, false
			}
			continue
		}
		if v.dur <= 0 || v.steps <= 0 {
			continue
		}
		pose := v.dur / time.Duration(v.steps)
		if pose <= 0 {
			continue
		}
		wait := pose - now.Sub(v.start)%pose
		if !sprites || wait < rest {
			rest = wait
		}
		sprites = true
	}
	return rest, sprites
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

// Value is the resolved scalar for one channel.
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

// SettledExceptEffects reports whether everything but the effect layers has
// come to rest. Effects loop forever, so they can never settle; asking this
// separately is what lets a surface drop to the effect cadence once its
// transitions are done, instead of pacing a drifting sky like a hover.
func (a *animator) SettledExceptEffects() bool {
	now := a.now()
	for key, v := range a.values {
		if key.channel == animEffect {
			continue
		}
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
	defer a.nudge()
	now := a.now()
	for key, v := range a.values {
		if v.loop != ui.GradientNone {
			continue
		}
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

// frameCap is the resolved shortest interval between repaints of this surface.
// A nil animator has no resolved table and returns zero, which means "do not
// pace" and leaves the pre-cap behaviour of one publish per tick — the safe
// direction, since pacing is an optimisation and never a correctness boundary.
func (a *animator) frameCap() time.Duration {
	if a == nil {
		return 0
	}
	return a.motion.FrameCap
}

// animateSurface is the shell's only animation scheduling path. It publishes a
// frame per tick while the surface has a value in flight and returns as soon as
// everything settles, so an idle shell schedules nothing. Panels and the OSD
// both drive their frames through it rather than keeping timers of their own.
//
// minInterval bounds how often it publishes, and is asked per tick: a surface
// paces interaction and a drifting effect differently, and which one is in
// flight changes while the loop runs. The ticker cadence is unchanged: this
// reduces blits, never frame requests, so it cannot add a frame source.
//
// settled and publish do their own locking: the surfaces they touch differ, and
// holding a lock across a publish would put the frame request under it.
func animateSurface(stop <-chan struct{}, settled func() bool, publish func(), minInterval func() time.Duration) {
	animateSurfaceResting(stop, nil, settled, publish, minInterval, nil)
}

// animateSurfaceResting is animateSurface for a surface that can carry
// sprite cycles. A sprite never settles, and its picture changes only at a
// pose boundary, so while sprites are all that is moving the loop sleeps
// until the next boundary rather than ticking every animTick: a bar cat costs
// one wake and one publish per pose, not ~125 idle checks a second. wake
// cuts a rest short when anything else starts moving; rest reports the
// sleep, and a nil rest keeps the plain tick.
func animateSurfaceResting(stop, wake <-chan struct{}, settled func() bool, publish func(), minInterval func() time.Duration, rest func() (time.Duration, bool)) {
	timer := time.NewTimer(animTick)
	defer timer.Stop()
	// deadline advances by whole ticks, as the ticker this replaced did, so
	// the ordinary cadence keeps its phase against the compositor.
	deadline := time.Now().Add(animTick)
	var last time.Time
	for {
		var now time.Time
		select {
		case <-stop:
			return
		case <-wake:
			now = time.Now()
		case now = <-timer.C:
		}
		done := settled()
		// A close nudges wake as it stops the loop -- it retargets the
		// surface's visibility -- so a woken loop can reach settled while the
		// close holds the lock settled takes. Once settled returns the close
		// has finished, and stop is checked again before anything publishes.
		select {
		case <-stop:
			return
		default:
		}
		// A skipped frame still advances the animation: values are computed
		// from the clock, not from how many times the surface was published.
		// Pacing changes how often we blit, never where the animation gets to.
		//
		// The settling frame always publishes, even inside the cap window, or
		// a settled value is left unpainted and the surface keeps a stale
		// pixel.
		if done || last.IsZero() || now.Sub(last) >= minInterval() {
			publish()
			last = now
		}
		if done {
			return
		}
		deadline = deadline.Add(animTick)
		if !deadline.After(now) {
			deadline = now.Add(animTick)
		}
		if rest != nil {
			// A millisecond past the boundary, so the pose has turned over by
			// the time the publish resolves it.
			if d, ok := rest(); ok && d+time.Millisecond > animTick {
				deadline = now.Add(d + time.Millisecond)
			}
		}
		timer.Reset(time.Until(deadline))
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
		// A virtual list is not rebuilt here. Layout materialises the visible
		// rows into Children with their bounds; calling Item again produces
		// fresh nodes that carry no bounds and that nothing paints, so the
		// state written onto them was discarded. That is why a list row could
		// never show hover.
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
		// Same reason as interaction.apply: the rows the pointer can be over
		// are the ones layout placed into Children. A rebuilt item carries no
		// bounds, so Contains never matched and no row was addressable.
		for _, c := range n.Children {
			walk(c)
		}
	}
	walk(root)
	return found
}
