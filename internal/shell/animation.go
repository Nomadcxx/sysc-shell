package shell

import (
	"math"
	"strings"
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
	rippleGrow = 500 * time.Millisecond
	rippleFade = 300 * time.Millisecond

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

	// springStep is the fixed integration lattice. Every spring scalar advances
	// in whole steps of this size and keeps the leftover fraction, so a stored
	// value depends on how much time passed, never on how the caller chopped it
	// into samples.
	springStep = 2 * time.Millisecond
	// springResumeGap is the longest gap a scalar integrates through. Beyond it
	// the value snaps: replaying a motion nobody watched is less honest than
	// arriving where it was already going, and the ceiling bounds one advance at
	// fewer than a thousand steps.
	springResumeGap = 2 * time.Second
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
	// animRipple is one press ripple's linear phase.
	animRipple
	// animStackX/Y/W/H are one toast card's drawn rectangle: the four
	// components move as separate scalars so an interrupted stack keeps each
	// component's position and velocity. They are internal to the toast host.
	animStackX
	animStackY
	animStackW
	animStackH
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
	// A spring channel replaces the duration curve with physics. spring holds
	// the coefficients, position and velocity are the stored x and v, advanced
	// is the last timestamp integrated, and remainder is the leftover fraction
	// of one lattice step. Eased, looping, and sprite values leave these zero.
	spring    *ui.Spring
	position  float64
	velocity  float64
	advanced  time.Time
	remainder time.Duration
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
	origins map[string][2]int
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
		values:  map[animKey]animValue{},
		origins: map[string][2]int{},
		wake:    make(chan struct{}, 1),
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
	if channel == animRipple {
		return func(p float64) float64 { return p }
	}
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
	case animStackX, animStackY, animStackW, animStackH:
		// Toast geometry reads as one motion whatever its direction, and one
		// token keeps the four components of a card moving together.
		return a.motion.Medium
	case animRipple:
		return rippleGrow + rippleFade
	}
	return 0
}

// springCapable reports whether a channel animates as a spring. Press, theme,
// ripple, and every looping channel keep their duration curves.
func springCapable(channel animChannel) bool {
	switch channel {
	case animHover, animSelect, animVisible, animProgress,
		animStackX, animStackY, animStackW, animStackH:
		return true
	}
	return false
}

// normalizedSpring reports whether a spring channel paints a normalized
// amount: opacity, hover tint, selection, or a meter value. Geometry channels
// hold logical pixels and are not clamped at the paint boundary.
func normalizedSpring(channel animChannel) bool {
	switch channel {
	case animHover, animSelect, animVisible, animProgress:
		return true
	}
	return false
}

// badgeProgress is the one-shot +N badge pop: an eased phase on the progress
// channel, not a value glide, so it keeps its duration curve.
func badgeProgress(node string, channel animChannel) bool {
	return channel == animProgress && strings.HasPrefix(node, badgeKeyPrefix)
}

// springFor derives the physical coefficients a channel animates with from the
// resolved duration table: omega = 12/T, critically damped. A nil result means
// the channel keeps its duration curve — reduced motion collapses it, and a
// zero token has no physical response to derive. The duration tokens set the
// physical response, not a hard completion deadline: the value is within a
// thousandth of its target at about T.
func (a *animator) springFor(channel animChannel, rising bool) *ui.Spring {
	if a.reduced {
		return nil
	}
	t := a.duration(channel, rising)
	if t <= 0 {
		return nil
	}
	// The expressive style settles spatial recipes harder: a stiffer spring,
	// not a different curve.
	factor := 1.0
	if a.spatial == theme.CurveOutQuart {
		factor = 1.1
	}
	omega := 12 * factor / t.Seconds()
	positionEpsilon, velocityEpsilon := 0.001, 0.001/t.Seconds()
	switch channel {
	case animStackX, animStackY, animStackW, animStackH:
		// Toast geometry is logical pixels, so its rest thresholds are pixels:
		// a card is settled when it is within a tenth of a pixel and its speed
		// is a tenth of a pixel a second for each second of token.
		positionEpsilon, velocityEpsilon = 0.1, 0.1/t.Seconds()
	}
	return &ui.Spring{
		Stiffness:       omega * omega,
		Damping:         2 * omega,
		PositionEpsilon: positionEpsilon,
		VelocityEpsilon: velocityEpsilon,
	}
}

// advanceSpring integrates one spring value to now on the fixed lattice and
// writes the result back into the map. It is the only writer of spring state,
// which is what keeps Value and the settlement queries from integrating the
// same timestamp twice, and what keeps a copied range value from silently
// dropping its integration. A clock that repeats or moves backwards integrates
// nothing.
//
// ponytail: whole 2 ms steps with an integer-nanosecond remainder, and a
// 2-second resume snap, bound one advance at fewer than a thousand steps per
// scalar. Analytic integration is the upgrade path if measured smoothness or
// catch-up cost ever defeats this bounded form.
func (a *animator) advanceSpring(key animKey, now time.Time) (float64, bool) {
	v, ok := a.values[key]
	if !ok || v.spring == nil {
		return 0, false
	}
	if now.After(v.advanced) {
		if elapsed := now.Sub(v.advanced); elapsed >= springResumeGap {
			v.position, v.velocity, v.remainder = v.to, 0, 0
			v.advanced = now
		} else {
			v.remainder += elapsed
			steps := v.remainder / springStep
			v.remainder -= steps * springStep
			v.advanced = now
			seconds := springStep.Seconds()
			for ; steps > 0; steps-- {
				x, velocity, rest := v.spring.Step(v.position, v.velocity, v.to, seconds)
				v.position, v.velocity = x, velocity
				if rest {
					break
				}
			}
		}
		a.values[key] = v
	}
	return v.position, v.position == v.to && v.velocity == 0
}

// clampUnit keeps a normalized paint value inside its frame. The stored value
// is deliberately not clamped: erasing overshoot would erase momentum.
func clampUnit(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}

// finite reports a real number that is neither NaN nor an infinity.
func finite(v float64) bool { return !math.IsNaN(v) && !math.IsInf(v, 0) }

// spinnerCycle is one turn of an activity spinner, and spinnerPoses the
// steps it takes: about thirty a second, smooth at an inline size while
// letting the frame loop sleep between poses the way a sprite's does.
const (
	spinnerCycle = 1200 * time.Millisecond
	spinnerPoses = 36
	// spinnerPrint stands in for a pose list: every spinner turns through
	// the same poses, so one fingerprint keeps a rebuild from restarting it.
	spinnerPrint uint64 = 0x5350494e // "SPIN"
)

// resolveSpriteMotion steps every sprite icon through its frames, writing
// the pose the surface clock has reached into the render copy's Icon the way
// resolveProgressMotion writes a glided value, and turns every spinner,
// writing its pose into Value. Under reduced motion nothing is tracked: the
// icon keeps its resting pose and a spinner rests at the top. Keys whose
// nodes left the tree are retired, which is also what ends a sprite's frames.
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
		if n.Kind == ui.KindSpinner && !anim.reduced {
			if key := n.StableKey(); key != "" {
				seen[key] = true
				anim.TargetCycle(key, spinnerCycle, spinnerPoses, spinnerPrint)
				pose := int(anim.Value(key, animSprite) * spinnerPoses)
				n.Value = float64(min(max(pose, 0), spinnerPoses-1)) / spinnerPoses
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
		if strings.HasPrefix(n.Key, badgeKeyPrefix) {
			seen[n.Key] = true
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
// pointer position from invalidating the surface. A spring channel consumes
// elapsed time under the old target and then keeps its stored position and
// velocity, changing only the destination and the preset the direction selects.
func (a *animator) Target(node string, channel animChannel, to float64) {
	if !finite(to) {
		// A non-finite target is a caller bug; refusing it leaves the value
		// exactly where it was rather than storing a NaN.
		return
	}
	key := animKey{node: node, channel: channel}
	now := a.now()
	current, ok := a.values[key]
	if ok && current.to == to {
		return
	}
	if springCapable(channel) && !badgeProgress(node, channel) {
		x, velocity := 0.0, 0.0
		advanced, remainder := now, time.Duration(0)
		if ok {
			if current.spring != nil {
				x, _ = a.advanceSpring(key, now)
				current = a.values[key]
			} else {
				x = current.at(now)
			}
			velocity, advanced, remainder = current.velocity, current.advanced, current.remainder
		}
		if spring := a.springFor(channel, to > x); spring != nil {
			a.values[key] = animValue{
				from: x, to: to, start: now, spring: spring,
				position: x, velocity: velocity, advanced: advanced, remainder: remainder,
			}
			a.nudge()
			return
		}
		// Reduced motion collapses the channel onto the duration curve below,
		// which snaps when the duration is zero.
	}
	from := 0.0
	if ok {
		if current.spring != nil {
			// A spring value only reaches here when reduced motion took the
			// channel away from physics; it settles to its stored target.
			from = current.to
		} else {
			from = current.at(now)
		}
	}
	dur := a.duration(channel, to > from)
	a.values[key] = animValue{from: from, to: to, start: now, dur: dur, ease: a.easeFor(channel)}
	a.nudge()
}

// TargetStackRect aims one card's drawn rectangle at its new slot through four
// fixed components, so an interrupted stack keeps each component's position and
// velocity instead of restarting a shared progress. Missing components are
// seeded at the old rectangle and left resting when that component does not
// move; components already in flight only change destination. Reduced motion
// creates no geometry state at all: the card is drawn at its slot.
func (a *animator) TargetStackRect(node string, from, to ui.Rect) {
	if a == nil {
		return
	}
	now := a.now()
	components := [4]struct {
		channel  animChannel
		from, to int
	}{
		{animStackX, from.X, to.X},
		{animStackY, from.Y, to.Y},
		{animStackW, from.W, to.W},
		{animStackH, from.H, to.H},
	}
	for _, c := range components {
		key := animKey{node: node, channel: c.channel}
		if _, ok := a.values[key]; !ok {
			if spring := a.springFor(c.channel, float64(c.to) > float64(c.from)); spring != nil {
				a.values[key] = animValue{
					from: float64(c.from), to: float64(c.from), start: now, spring: spring,
					position: float64(c.from), advanced: now,
				}
			}
		}
		// A component that did not move is already resting at its value; the
		// ordinary target path is a no-op for it.
		a.Target(node, c.channel, float64(c.to))
	}
}

// shiftStackRectX moves one card's stored x -- both where it is drawn and
// where it is heading -- by dx logical pixels, so a card still in flight keeps
// its drawn offset and its destination when a surface resizes. The y, width
// and height components are unaffected by a horizontal coordinate change.
func (a *animator) shiftStackRectX(node string, dx float64) {
	if a == nil || dx == 0 {
		return
	}
	key := animKey{node: node, channel: animStackX}
	v, ok := a.values[key]
	if !ok {
		return
	}
	v.position += dx
	v.to += dx
	a.values[key] = v
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

// SpriteRest sleeps to the next sprite pose, capped at the effect cadence
// when decorative gradients or effects also move. Other motion keeps the
// ordinary tick; input wakes a resting loop immediately.
func (a *animator) SpriteRest() (time.Duration, bool) {
	now := a.now()
	rest, sprites := time.Duration(0), false
	decorative := false
	for key, v := range a.values {
		if key.channel != animSprite {
			if key.channel == animGradient || key.channel == animEffect {
				decorative = decorative || !v.settled(now)
				continue
			}
			// A moving spring is ordinary motion: it keeps the tick, and it
			// advances here so a sprite-only surface cannot stall it.
			if v.spring != nil {
				if _, atRest := a.advanceSpring(key, now); !atRest {
					return 0, false
				}
				continue
			}
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
	if sprites && decorative {
		rest = min(rest, effectFrameCap)
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

// TargetRipple restarts one node's ripple at the new press origin.
func (a *animator) TargetRipple(node string, x, y int) {
	if a == nil || node == "" {
		return
	}
	if a.origins == nil {
		a.origins = map[string][2]int{}
	}
	a.origins[node] = [2]int{x, y}
	a.Reset(node, animRipple)
	a.Target(node, animRipple, 1)
}

// Ripple returns a node's resolved ripple frame and origin.
func (a *animator) Ripple(node string) (phase float64, x, y int, ok bool) {
	if a == nil || !a.has(node, animRipple) {
		return 0, 0, 0, false
	}
	p := a.origins[node]
	return a.Value(node, animRipple), p[0], p[1], true
}

// has reports whether a channel has ever been aimed at anything. A value that
// was never targeted is not the same as one resting at zero: the first means
// there is nothing to resolve, the second that it resolved to zero.
func (a *animator) has(node string, channel animChannel) bool {
	_, ok := a.values[animKey{node: node, channel: channel}]
	return ok
}

// Value is the resolved scalar for one channel. A spring advances to the
// current clock and reports its stored position; a normalized channel clamps
// the painted value into [0,1] while the stored state keeps any overshoot, so a
// reversal does not lose its momentum to a paint bound.
func (a *animator) Value(node string, channel animChannel) float64 {
	key := animKey{node: node, channel: channel}
	v, ok := a.values[key]
	if !ok {
		return 0
	}
	if v.spring != nil {
		x, _ := a.advanceSpring(key, a.now())
		if normalizedSpring(channel) {
			return clampUnit(x)
		}
		return x
	}
	return v.at(a.now())
}

// Settled reports whether every value has reached its target. While this is
// false the surface asks for another frame; once true it stops, so an idle
// shell schedules nothing. A spring settles on its physics state, not on an
// eased duration: advancing it here is what lets a cap-skipped frame still
// reach its endpoint.
func (a *animator) Settled() bool {
	now := a.now()
	for key, v := range a.values {
		if v.spring != nil {
			if _, rest := a.advanceSpring(key, now); !rest {
				return false
			}
			continue
		}
		if !v.settled(now) {
			return false
		}
	}
	return true
}

// SettledExceptEffects ignores decorative effects and gradients so surfaces
// can use the effect cadence after interactions and content motion settle.
func (a *animator) SettledExceptEffects() bool {
	now := a.now()
	for key, v := range a.values {
		if key.channel == animEffect || key.channel == animGradient {
			continue
		}
		if v.spring != nil {
			if _, rest := a.advanceSpring(key, now); !rest {
				return false
			}
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
	delete(a.origins, node)
	for key := range a.values {
		if key.node == node {
			delete(a.values, key)
		}
	}
}

// Retarget moves every colour-bearing value to a new palette by restarting the
// transitions in flight from their current rendered values. A theme change is
// a target change like any other, so it reuses the same clock. A spring keeps
// its stored position and velocity: its coefficients come from the resolved
// duration table (a speed or style change replaces them through Configure), and
// recreating the value would erase the momentum the channel exists to keep.
func (a *animator) Retarget() {
	defer a.nudge()
	now := a.now()
	for key, v := range a.values {
		if key.channel == animRipple {
			continue
		}
		if v.loop != ui.GradientNone {
			continue
		}
		if v.spring != nil {
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
	// stateLayer is this surface's hover-tint policy. The bar keeps its
	// furniture ruling; interactive panels opt in.
	stateLayer bool
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
			hovered := s.stateLayer && key != "" && key == s.hover
			pressed := key != "" && key == s.press
			n.HoverProgress, n.PressProgress = 0, 0
			n.Ripple = ui.RipplePaint{}
			// Only chrome carries a transition. A clickable tray row resolves
			// state so the host can react, but a display group that merely
			// reports a number never animates.
			if anim != nil && key != "" && ui.Animated(n) {
				anim.Target(key, animHover, boolValue(hovered))
				anim.Target(key, animPress, boolValue(pressed))
				anim.Target(key, animSelect, boolValue(n.State.Has(ui.StateSelected)))
				n.HoverProgress = anim.Value(key, animHover)
				n.PressProgress = anim.Value(key, animPress)
				hovered = n.HoverProgress > 0
				pressed = n.PressProgress > 0
				if phase, x, y, ok := anim.Ripple(key); ok {
					n.Ripple = ui.RipplePaint{Phase: phase, X: x, Y: y}
				}
			}
			n.State &^= ui.StateHovered | ui.StatePressed
			if hovered {
				n.State |= ui.StateHovered
			}
			if pressed {
				n.State |= ui.StatePressed
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
