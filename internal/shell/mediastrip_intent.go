package shell

import (
	"sync"
	"time"

	"github.com/Nomadcxx/sysc-shell/internal/ui"
)

const (
	mediaStripOpenDelay = 250 * time.Millisecond
	mediaStripGrace     = 400 * time.Millisecond
)

// mediaStripRequest asks the registry to open the strip at a pill, or to
// close a peeking one.
type mediaStripRequest struct {
	open   bool
	global uint32
	anchor ui.Rect
}

// mediaStripIntent turns pointer presence over the pill and the strip into
// open and close requests after a delay. Like dwell, its timers fire on their
// own goroutine and only send; Registry.relayMediaStrip applies each request
// under Registry.mu. Callers may hold Registry.mu: the lock order is
// Registry.mu, then i.mu, and nothing here takes Registry.mu.
type mediaStripIntent struct {
	mu        sync.Mutex
	after     func(time.Duration, func()) func() bool
	stop      func() bool
	gen       uint64
	overPill  bool
	overStrip bool
	shown     bool
	closed    bool
	global    uint32
	anchor    ui.Rect
	out       chan mediaStripRequest
}

func newMediaStripIntent(after func(time.Duration, func()) func() bool) *mediaStripIntent {
	if after == nil {
		after = func(d time.Duration, f func()) func() bool { return time.AfterFunc(d, f).Stop }
	}
	return &mediaStripIntent{after: after, out: make(chan mediaStripRequest, 8)}
}

func (i *mediaStripIntent) requests() <-chan mediaStripRequest { return i.out }

// pill reports whether the pointer is over the pill's art or title.
func (i *mediaStripIntent) pill(global uint32, anchor ui.Rect, over bool) {
	i.mu.Lock()
	defer i.mu.Unlock()
	if over == i.overPill && (!over || global == i.global) {
		return
	}
	i.overPill = over
	if over {
		i.global, i.anchor = global, anchor
		if i.shown {
			i.cancelLocked()
		} else {
			i.armLocked(mediaStripOpenDelay, mediaStripRequest{open: true, global: global, anchor: anchor})
		}
		return
	}
	i.settleLocked()
}

// strip reports the pointer entering or leaving the strip surface.
func (i *mediaStripIntent) strip(over bool) {
	i.mu.Lock()
	defer i.mu.Unlock()
	i.overStrip = over
	if over {
		i.cancelLocked()
		return
	}
	i.settleLocked()
}

// setShown records whether a strip is on screen, whatever opened it.
func (i *mediaStripIntent) setShown(shown bool) {
	i.mu.Lock()
	defer i.mu.Unlock()
	i.shown = shown
	if !shown {
		i.overStrip = false
	}
}

func (i *mediaStripIntent) close() {
	i.mu.Lock()
	defer i.mu.Unlock()
	i.closed = true
	i.cancelLocked()
}

// settleLocked runs when the pointer has left something: a pending open is
// dropped, and a shown strip the pointer is no longer over starts its grace.
func (i *mediaStripIntent) settleLocked() {
	i.cancelLocked()
	if i.shown && !i.overPill && !i.overStrip {
		i.armLocked(mediaStripGrace, mediaStripRequest{})
	}
}

func (i *mediaStripIntent) cancelLocked() {
	i.gen++
	if i.stop != nil {
		i.stop()
		i.stop = nil
	}
}

func (i *mediaStripIntent) armLocked(d time.Duration, req mediaStripRequest) {
	i.cancelLocked()
	gen := i.gen
	i.stop = i.after(d, func() {
		i.mu.Lock()
		defer i.mu.Unlock()
		if i.closed || gen != i.gen {
			return // a stale timer that raced its stop
		}
		i.stop = nil
		select {
		case i.out <- req:
		default:
		}
	})
}
