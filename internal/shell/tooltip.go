package shell

import (
	"sync"
	"time"

	"github.com/Nomadcxx/sysc-shell/internal/platform/wayland"
	"github.com/Nomadcxx/sysc-shell/internal/ui"
)

// defaultDwell is how long a pointer must rest before a tooltip appears. A
// tooltip on every crossing would flicker across the whole bar.
const defaultDwell = 500 * time.Millisecond

// dwell turns pointer enter and leave into tooltip requests after a delay.
//
// The timer fires on its own goroutine and must never touch a Wayland proxy.
// It sends on this channel instead, which the owner's wake pipe bridges; the
// owner goroutine alone creates and destroys the surface.
type dwell struct {
	mu         sync.Mutex
	delay      time.Duration
	timer      *time.Timer
	shown      bool
	out        chan wayland.TooltipRequest
	closed     bool
	generation uint64
	// armed holds the request the current dwell (or the tooltip on screen)
	// belongs to, so repeated motion over the same widget is a no-op.
	armed      wayland.TooltipRequest
	armedValid bool
}

func newDwell(delay time.Duration) *dwell {
	if delay <= 0 {
		delay = defaultDwell
	}
	return &dwell{delay: delay, out: make(chan wayland.TooltipRequest, 4)}
}

// requests is the channel the process wires into wayland.Callbacks.Tooltips.
func (d *dwell) requests() <-chan wayland.TooltipRequest { return d.out }

// tooltipStyleFor resolves the paint for one floating tooltip. A tooltip
// floats over the desktop like a panel, not over the bar like a pill, so it
// takes the overlay root colour and the panel rim rather than the bar's own
// fill. The owner still falls back to the compiled palette when a colour is
// absent.
func tooltipStyleFor(t Theme) wayland.TooltipStyle {
	bg := t.SurfaceContainerHigh
	bg.A = t.Surfaces.Overlay
	return wayland.TooltipStyle{
		Background: bg,
		Foreground: t.OnSurface,
		Border:     t.Outline,
	}
}

// enter starts or restarts the dwell for one widget. Entering a second widget
// replaces the pending request rather than queueing behind it.
func (d *dwell) enter(global uint32, anchor ui.Rect, text string, style wayland.TooltipStyle) {
	if text == "" {
		d.leave()
		return
	}
	d.queue(wayland.TooltipRequest{Global: global, Anchor: anchor, Text: text, Style: style})
}

func (d *dwell) enterRoot(global uint32, anchor ui.Rect, root *ui.Node, style wayland.TooltipStyle) {
	if root == nil {
		d.leave()
		return
	}
	d.queue(wayland.TooltipRequest{Global: global, Anchor: anchor, Root: root, Style: style})
}

func (d *dwell) queue(req wayland.TooltipRequest) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.closed {
		return
	}
	// Pointer motion re-enters the same widget on every event. Re-arming each
	// time demanded a dead-still pointer for the whole dwell, so an identical
	// pending-or-shown request leaves the timer alone.
	if d.armedValid && d.armed == req {
		return
	}
	if d.timer != nil {
		d.timer.Stop()
	}
	d.generation++
	generation := d.generation
	d.armed, d.armedValid = req, true
	d.timer = time.AfterFunc(d.delay, func() { d.fire(generation, req) })
}

func (d *dwell) fire(generation uint64, req wayland.TooltipRequest) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.closed || generation != d.generation {
		return
	}
	d.timer = nil
	d.shown = true
	d.send(req)
}

// leave cancels a pending dwell, and hides a tooltip that is already up.
func (d *dwell) leave() {
	d.mu.Lock()
	d.generation++
	if d.timer != nil {
		d.timer.Stop()
		d.timer = nil
	}
	d.armedValid = false
	shown := d.shown
	d.shown = false
	closed := d.closed
	d.mu.Unlock()

	if shown && !closed {
		d.send(wayland.TooltipRequest{})
	}
}

// stop cancels everything. A reload and shutdown both reach it, because a
// tooltip is transient and reappears on the next hover.
func (d *dwell) stop() {
	d.mu.Lock()
	d.generation++
	if d.timer != nil {
		d.timer.Stop()
		d.timer = nil
	}
	d.armedValid = false
	wasShown := d.shown
	d.shown, d.closed = false, true
	d.mu.Unlock()

	if wasShown {
		d.send(wayland.TooltipRequest{})
	}
}

// send never blocks: a dropped hide would leave a tooltip on screen, so the
// buffer is sized for the few requests a hover can produce and a full channel
// drops the oldest rather than stalling the pointer path.
func (d *dwell) send(req wayland.TooltipRequest) {
	select {
	case d.out <- req:
		return
	default:
	}
	select {
	case <-d.out:
	default:
	}
	select {
	case d.out <- req:
	default:
	}
}
