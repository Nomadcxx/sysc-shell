package shell

import (
	"time"

	"github.com/Nomadcxx/sysc-shell/internal/ui"
)

// Catalogue transition durations. Press and panel visibility are asymmetric:
// a control commits faster than it releases, and a panel leaves faster than it
// arrives.
const (
	animHoverDuration    = 120 * time.Millisecond
	animPressInDuration  = 80 * time.Millisecond
	animPressOutDuration = 120 * time.Millisecond
	animSelectDuration   = 180 * time.Millisecond
	animPanelInDuration  = 200 * time.Millisecond
	animPanelOutDuration = 150 * time.Millisecond

	// Reduced motion keeps panel visibility as a plain fade, and no longer than
	// the shortest transition the catalogue otherwise uses.
	animReducedPanelDuration = animPanelOutDuration

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
	values  map[animKey]animValue
	// running reports whether a frame loop is already ticking this surface, so
	// a second target change does not start a second ticker.
	running bool
}

func newAnimator(now func() time.Time, reduced bool) *animator {
	if now == nil {
		now = time.Now
	}
	return &animator{now: now, reduced: reduced, values: map[animKey]animValue{}}
}

func easeFor(channel animChannel) func(float64) float64 {
	// Selection settles harder so the moving indicator arrives decisively;
	// everything else uses the catalogue's default curve.
	if channel == animSelect {
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
			return animReducedPanelDuration
		}
		return 0
	}
	switch channel {
	case animHover:
		return animHoverDuration
	case animPress:
		if rising {
			return animPressInDuration
		}
		return animPressOutDuration
	case animSelect:
		return animSelectDuration
	case animVisible:
		if rising {
			return animPanelInDuration
		}
		return animPanelOutDuration
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
	a.values[key] = animValue{from: from, to: to, start: now, dur: dur, ease: easeFor(channel)}
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
			dur: a.duration(key.channel, v.to > v.at(now)), ease: easeFor(key.channel)}
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
