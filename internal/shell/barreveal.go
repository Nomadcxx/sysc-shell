package shell

import (
	"math"
	"time"

	"github.com/Nomadcxx/sysc-shell/internal/ui"
)

// An end section that overflows can be revealed: resting the pointer at its
// end of the bar for revealDwell leans the section out, then releases it on
// an elastic spring until its tail shows. It goes back by itself, slowly,
// once the pointer has been off the bar for revealGrace and nothing opened
// from the bar is still open (sysc-2270, owner picks 2026-11-09).
const (
	// revealEdge is how close to the bar's end, in logical pixels, the
	// pointer must rest. The pointer stops against the screen edge there, and
	// no item reaches it, so waiting shows no hover or tooltip.
	revealEdge = 2
	// revealQuiet passes before anything shows, so a pointer crossing the
	// edge on its way elsewhere changes nothing.
	revealQuiet = 250 * time.Millisecond
	revealDwell = 2 * time.Second
	// revealLean is how far the section leans out while the pointer waits,
	// and revealTint how strongly the edge warms.
	revealLean = 12.0
	revealTint = 0.4
	// revealGrace is the wait before going back, counted from the later of
	// the pointer leaving the bar and the last bar surface closing, which is
	// polled every revealHoldPoll.
	revealGrace    = 8 * time.Second
	revealHoldPoll = time.Second
	// revealGlide is the return, at motion speed 1.
	revealGlide = 1200 * time.Millisecond
	// revealSwipe is how far, in logical pixels, a press must travel along
	// the bar before it is a swipe rather than a click. Touch arrives as
	// pointer press, motion and release, so a finger and a mouse drag share it.
	revealSwipe = 24
)

// barSwipe is a press on an overflowing section that may become a swipe.
type barSwipe struct {
	armed, active bool
	side          ui.Side
	x, y          int
}

type revealPhase uint8

const (
	revealRest revealPhase = iota
	// revealArming: the pointer is at the edge, the lean is running and the
	// release is due at armed+revealDwell.
	revealArming
	// revealOpening: springing toward the tail.
	revealOpening
	// revealOpen: at the tail; away is set while the pointer is off the bar.
	revealOpen
	// revealClosing: gliding home.
	revealClosing
)

// sectionReveal is one end section's reveal. The shift itself is the
// animator's value under revealKey; this holds what the clock cannot.
type sectionReveal struct {
	phase revealPhase
	armed time.Time
	away  time.Time
	held  time.Time
	strip ui.Strip
	// window is what the section showed at the last placement; zero at rest.
	window ui.Rect
}

func revealKey(side ui.Side) string {
	if side == ui.SideStart {
		return "bar-reveal:start"
	}
	return "bar-reveal:end"
}

func revealTintKey(side ui.Side) string { return revealKey(side) + ":tint" }

// revealSection is the sections() index an end section lives at.
func revealSection(side ui.Side) int {
	if side == ui.SideStart {
		return 0
	}
	return 2
}

func revealSides() [2]ui.Side { return [2]ui.Side{ui.SideStart, ui.SideEnd} }

// rubberLean eases the lean against growing resistance, like an elastic band
// being pulled.
func rubberLean(x float64) float64 { return (1 - math.Exp(-2.5*x)) / (1 - math.Exp(-2.5)) }

func smoothStep(x float64) float64 { return x * x * (3 - 2*x) }

func sineInOut(x float64) float64 { return (1 - math.Cos(math.Pi*x)) / 2 }

// setHoldProbe wires the question "is anything opened from this bar still
// open?". The bar asks it only from its timer, holding no lock.
func (b *Bar) setHoldProbe(fn func() bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.holdProbe = fn
}

// scheduleRevealLocked asks for a check after d. One check is outstanding at
// a time: every check reschedules what it still waits for, so a second timer
// would poll beside the first for the rest of the reveal.
func (b *Bar) scheduleRevealLocked(d time.Duration) {
	due := b.anim.now().Add(d)
	if !b.revealDue.IsZero() && !due.Before(b.revealDue) {
		return
	}
	b.revealDue = due
	if b.revealAfter != nil {
		b.revealAfter(d, b.revealCheck)
	}
}

// atRevealEdgeLocked reports whether a surface point is at the given end of
// the bar's main axis.
func (b *Bar) atRevealEdgeLocked(side ui.Side, x, y int) bool {
	at, extent := x, b.configured.width
	if b.barAxis() == ui.Vertical {
		at, extent = y, b.configured.height
	}
	if side == ui.SideStart {
		return at < revealEdge
	}
	return at >= extent-revealEdge
}

// revealPointerLocked follows the pointer on the bar: it arms a section at
// its edge, cancels one that it left before the release, and holds an open
// one. A section gliding home springs out again only when the pointer comes
// back onto the bar, so one closed by a swipe stays closed under a mouse that
// never left. It reports whether anything visible changed.
func (b *Bar) revealPointerLocked(x, y int, entered bool) bool {
	if b.anim == nil {
		return false
	}
	now := b.anim.now()
	changed := false
	for _, side := range revealSides() {
		r := &b.reveal[side]
		if r.strip == (ui.Strip{}) {
			continue
		}
		r.away = time.Time{}
		edge := b.atRevealEdgeLocked(side, x, y)
		switch r.phase {
		case revealRest:
			if edge {
				b.armRevealLocked(side, now)
				changed = true
			}
		case revealArming:
			if !edge {
				b.cancelRevealLocked(side)
				changed = true
			}
		case revealClosing:
			if !entered {
				continue
			}
			// Back on the bar mid-glide: spring out again.
			b.anim.Target(revealKey(side), animReveal, float64(r.strip.MaxShift(side)))
			r.phase = revealOpening
			changed = true
		}
	}
	return changed
}

// revealLeaveLocked follows the pointer off the bar.
func (b *Bar) revealLeaveLocked() bool {
	if b.anim == nil {
		return false
	}
	now := b.anim.now()
	changed := false
	for _, side := range revealSides() {
		r := &b.reveal[side]
		switch r.phase {
		case revealArming:
			b.cancelRevealLocked(side)
			changed = true
		case revealOpening, revealOpen:
			r.away = now
			b.scheduleRevealLocked(revealHoldPoll)
		}
	}
	return changed
}

func (b *Bar) armRevealLocked(side ui.Side, now time.Time) {
	r := &b.reveal[side]
	r.phase, r.armed = revealArming, now
	ramp := revealDwell - revealQuiet
	if !b.anim.reduced {
		b.anim.TargetCurve(revealKey(side), animReveal, revealLean, revealQuiet, ramp, rubberLean)
	}
	b.anim.TargetCurve(revealTintKey(side), animRevealTint, revealTint, revealQuiet, ramp, smoothStep)
	b.scheduleRevealLocked(revealDwell)
}

func (b *Bar) cancelRevealLocked(side ui.Side) {
	r := &b.reveal[side]
	r.phase, r.armed = revealRest, time.Time{}
	b.anim.Target(revealKey(side), animReveal, 0)
	b.anim.TargetCurve(revealTintKey(side), animRevealTint, 0, 0, b.anim.motion.Short, smoothStep)
}

// advanceRevealLocked moves each section's phase on by the clock: the release
// at the end of the dwell, and arrival at either end of a move.
func (b *Bar) advanceRevealLocked(now time.Time) {
	if b.anim == nil {
		return
	}
	for _, side := range revealSides() {
		r := &b.reveal[side]
		key := revealKey(side)
		switch r.phase {
		case revealArming:
			if now.Before(r.armed.Add(revealDwell)) {
				continue
			}
			b.anim.Target(key, animReveal, float64(r.strip.MaxShift(side)))
			b.anim.TargetCurve(revealTintKey(side), animRevealTint, 0, 0, b.anim.motion.Short, smoothStep)
			r.phase = revealOpening
		case revealOpening:
			if b.anim.valueSettled(key, animReveal) {
				r.phase = revealOpen
				b.rehoverLocked()
			}
		case revealClosing:
			if b.anim.valueSettled(key, animReveal) {
				r.phase, r.away, r.held = revealRest, time.Time{}, time.Time{}
				b.rehoverLocked()
			}
		}
	}
}

// revealCheck is the reveal's timer body. It asks the hold probe with no lock
// held, because the registry locks before the bar, then advances the phases
// and starts any return that is due.
func (b *Bar) revealCheck() {
	b.mu.Lock()
	if b.anim == nil {
		b.mu.Unlock()
		return
	}
	probe, waiting := b.holdProbe, false
	for _, side := range revealSides() {
		r := b.reveal[side]
		waiting = waiting || ((r.phase == revealOpening || r.phase == revealOpen) && !r.away.IsZero())
	}
	b.mu.Unlock()
	held := waiting && probe != nil && probe()

	b.mu.Lock()
	now := b.anim.now()
	if !now.Before(b.revealDue) {
		// The outstanding check is this one (or one it superseded).
		b.revealDue = time.Time{}
	}
	b.advanceRevealLocked(now)
	for _, side := range revealSides() {
		r := &b.reveal[side]
		if (r.phase != revealOpening && r.phase != revealOpen) || r.away.IsZero() {
			continue
		}
		if held {
			r.held = now
			b.scheduleRevealLocked(revealHoldPoll)
			continue
		}
		since := r.away
		if r.held.After(since) {
			since = r.held
		}
		if due := since.Add(revealGrace); now.Before(due) {
			b.scheduleRevealLocked(min(revealHoldPoll, due.Sub(now)))
			continue
		}
		b.anim.TargetCurve(revealKey(side), animReveal, 0, 0, b.anim.motionScaled(revealGlide), sineInOut)
		r.phase = revealClosing
	}
	b.mu.Unlock()
	b.invalidate()
}

// setRevealStripsLocked takes the strips a fresh layout reported. A section
// that now fits is put back at rest at once; an open one follows its new
// tail without animating, and an opening one springs on toward it.
func (b *Bar) setRevealStripsLocked(over ui.BarOverflow) {
	if b.anim == nil {
		return
	}
	for _, side := range revealSides() {
		strip := over.LeftStrip
		if side == ui.SideEnd {
			strip = over.RightStrip
		}
		r := &b.reveal[side]
		r.strip = strip
		if strip == (ui.Strip{}) {
			if r.phase != revealRest || b.anim.Value(revealKey(side), animReveal) != 0 {
				b.anim.Reset(revealKey(side), animReveal)
				b.anim.Reset(revealTintKey(side), animRevealTint)
				*r = sectionReveal{}
			}
			r.window = ui.Rect{}
			continue
		}
		switch r.phase {
		case revealOpen:
			b.anim.TargetCurve(revealKey(side), animReveal, float64(strip.MaxShift(side)), 0, 0, nil)
		case revealOpening:
			b.anim.Target(revealKey(side), animReveal, float64(strip.MaxShift(side)))
		}
	}
}

// revealShifted reports whether a section is anywhere but its resting layout.
func (b *Bar) revealShiftedLocked(side ui.Side) bool {
	r := b.reveal[side]
	return r.strip != (ui.Strip{}) && (r.phase != revealRest || b.anim.Value(revealKey(side), animReveal) != 0)
}

// applyRevealLocked re-places each shifted section at the animator's shift.
// Layout leaves sections at rest; this runs after it and on every frame.
func (b *Bar) applyRevealLocked() error {
	if b.anim == nil {
		return nil
	}
	sections := b.sections()
	content := b.contentLocked(b.configured.width, b.configured.height)
	for _, side := range revealSides() {
		r := &b.reveal[side]
		shifted := b.revealShiftedLocked(side)
		if !shifted && r.window == (ui.Rect{}) {
			continue
		}
		// A section coming to rest is placed once more at shift 0, which is
		// its resting layout, so no item keeps a bound from the last frame.
		window, err := ui.PlaceStrip(sections[revealSection(side)], b.barAxis(), content, side, r.strip,
			b.anim.Value(revealKey(side), animReveal), b.theme.Metrics.BarSpacing, b.measureLocked())
		if err != nil {
			return err
		}
		r.window = window
		if !shifted {
			r.window = ui.Rect{}
		}
	}
	return nil
}

// revealMovingLocked reports whether a section is mid-move, when it takes no
// pointer input: what is under the pointer is about to be somewhere else.
// The reveal needs the bar's clock; a bar built without one never reveals.
func (b *Bar) revealMovingLocked(section int) bool {
	if b.anim == nil {
		return false
	}
	for _, side := range revealSides() {
		if revealSection(side) == section && b.reveal[side].strip != (ui.Strip{}) &&
			!b.anim.valueSettled(revealKey(side), animReveal) {
			return true
		}
	}
	return false
}

// rehoverLocked resolves hover again after a section stops moving under a
// pointer that did not.
func (b *Bar) rehoverLocked() {
	if !b.inside {
		return
	}
	action, _, _, _ := b.axisTargetLocked(b.hoverAt.x, b.hoverAt.y)
	b.pointer.setHover(action)
}

// revealChromeLocked is a shifted section's fades and the edge tint: a
// surface fade at each end of the window where items run on hidden, and the
// accent ramp at the bar's end while the pointer waits there.
func (b *Bar) revealChromeLocked(side ui.Side) []*ui.Node {
	if b.anim == nil {
		return nil
	}
	r := b.reveal[side]
	axis := b.barAxis()
	extent := b.theme.Metrics.StandardControl
	var out []*ui.Node
	main := func(rect ui.Rect) (int, int) {
		if axis == ui.Vertical {
			return rect.Y, rect.H
		}
		return rect.X, rect.W
	}
	along := func(band ui.Rect, start, length int) ui.Rect {
		if axis == ui.Vertical {
			return ui.Rect{X: band.X, Y: start, W: band.W, H: length}
		}
		return ui.Rect{X: start, Y: band.Y, W: length, H: band.H}
	}
	if r.window.W > 0 && r.window.H > 0 {
		lo, size := main(r.window)
		hi := lo + size
		first, last := hi, lo
		hiddenBefore, hiddenAfter := false, false
		seen := false
		for _, n := range b.sections()[revealSection(side)] {
			p, e := main(n.Bounds)
			if e == 0 {
				if seen {
					hiddenAfter = true
				} else {
					hiddenBefore = true
				}
				continue
			}
			seen = true
			first, last = min(first, p), max(last, p+e)
		}
		hiddenBefore = hiddenBefore || first < lo
		hiddenAfter = hiddenAfter || last > hi
		fade := min(extent, size)
		if hiddenBefore {
			out = append(out, &ui.Node{Kind: ui.KindEdgeFade, FadeAxis: axis, FadeLeading: true, Bounds: along(r.window, lo, fade)})
		}
		if hiddenAfter {
			out = append(out, &ui.Node{Kind: ui.KindEdgeFade, FadeAxis: axis, Bounds: along(r.window, hi-fade, fade)})
		}
	}
	if tint := b.anim.Value(revealTintKey(side), animRevealTint); tint > 0 {
		body := b.bodyLocked(b.configured.width, b.configured.height)
		lo, size := main(body)
		glow := min(2*extent, size)
		at, leading := lo+size-glow, false
		if side == ui.SideStart {
			at, leading = lo, true
		}
		out = append(out, &ui.Node{Kind: ui.KindEdgeFade, FadeAxis: axis, FadeLeading: leading, FadeTint: tint,
			Bounds: along(body, at, glow)})
	}
	return out
}

// revealWindowLocked is the window a shifted section shows, widened to the
// whole body across the bar so it clips along the main axis only; zero for a
// section at rest.
func (b *Bar) revealWindowLocked(section int) ui.Rect {
	for _, side := range revealSides() {
		w := b.reveal[side].window
		if revealSection(side) != section || w.W <= 0 || w.H <= 0 {
			continue
		}
		body := b.bodyLocked(b.configured.width, b.configured.height)
		if b.barAxis() == ui.Vertical {
			w.X, w.W = body.X, body.W
		} else {
			w.Y, w.H = body.Y, body.H
		}
		return w
	}
	return ui.Rect{}
}

// beginSwipeLocked starts following a press inside an overflowing section.
func (b *Bar) beginSwipeLocked(x, y int) {
	b.swipe = barSwipe{}
	if b.anim == nil {
		return
	}
	content := b.contentLocked(b.configured.width, b.configured.height)
	at, start, extent := x, content.X, content.W
	if b.barAxis() == ui.Vertical {
		at, start, extent = y, content.Y, content.H
	}
	end := start + extent
	for _, side := range revealSides() {
		strip := b.reveal[side].strip
		if strip == (ui.Strip{}) {
			continue
		}
		lo, hi := start, start+strip.Budget
		if side == ui.SideEnd {
			lo, hi = end-strip.Budget, end
		}
		if at >= lo && at < hi {
			b.swipe = barSwipe{armed: true, side: side, x: x, y: y}
			return
		}
	}
}

// swipeMotionLocked turns a press that has travelled revealSwipe along the
// bar into a swipe: the press is dropped, and the section opens toward the
// main-axis start or closes away from it, on the reveal spring.
func (b *Bar) swipeMotionLocked(x, y int) bool {
	if !b.swipe.armed || b.swipe.active {
		return false
	}
	delta := x - b.swipe.x
	if b.barAxis() == ui.Vertical {
		delta = y - b.swipe.y
	}
	if delta > -revealSwipe && delta < revealSwipe {
		return false
	}
	b.swipe.active = true
	b.pressed = ""
	b.pointer.setPress("")
	side := b.swipe.side
	r := &b.reveal[side]
	to, phase := float64(r.strip.MaxShift(side)), revealOpening
	if delta > 0 {
		to, phase = 0, revealClosing
	}
	b.anim.Target(revealKey(side), animReveal, to)
	b.anim.TargetCurve(revealTintKey(side), animRevealTint, 0, 0, b.anim.motion.Short, smoothStep)
	r.phase, r.armed, r.away = phase, time.Time{}, time.Time{}
	return true
}

// barHeld reports whether anything opened from the bar on an output is
// still open: a panel (built-in or plugin), the tray menu or drawer, the
// running-app menu or the media strip. A tooltip does not hold the bar.
func (r *Registry) barHeld(global uint32) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.openPanelHostsLocked(global, nil)) > 0 {
		return true
	}
	if m := r.trayMenu; m != nil && m.open_ && m.output == global {
		return true
	}
	if d := r.trayDrawer; d != nil && d.open_ && d.output == global {
		return true
	}
	if m := r.runningMenu; m != nil && m.open_ && m.output == global {
		return true
	}
	if s := r.mediaStrip; s != nil && s.open_ && s.output == global {
		return true
	}
	return false
}
