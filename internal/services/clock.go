// Package services holds the shell's process-scoped data sources.
//
// A service starts when its first consumer acquires a lease and stops when the
// last one is released, so a configuration that uses no clock costs no timer
// and no goroutine. Services here are concrete: there is no registry, no
// container, and no interface with one implementation.
package services

import (
	"fmt"
	"sync"
	"time"
)

// Clock publishes the current time to every consumer, aligned to the finest
// boundary any live lease requires.
//
// One Clock serves every bar. Consumers receive one shared snapshot per tick
// rather than sampling independently, so two outputs cost one wake-up.
type Clock struct {
	mu     sync.Mutex
	leases leaseSet
	// stop is closed to end the running goroutine; nil means not running.
	stop chan struct{}
	// done is closed by the goroutine as it exits, so a stop can wait for it.
	done chan struct{}
	// starts counts goroutine starts. A boundary change re-arms rather than
	// restarting, so this stays at one across a reload.
	starts int

	// rearm wakes the goroutine when a newly acquired lease needs a shorter
	// boundary than the one it is currently sleeping on.
	rearm   chan struct{}
	updates chan time.Time
}

// Lease is one consumer's claim on a service. Exactly one of clock, metrics,
// or weather is set; the zero value is an already-released lease.
type Lease struct {
	clock    *Clock
	metrics  *Metrics
	weather  *Weather
	selector Selector
	boundary time.Duration
}

func NewClock() *Clock {
	return &Clock{
		rearm:   make(chan struct{}, 1),
		updates: make(chan time.Time, 1),
	}
}

// Updates carries the newest time. The channel is created once and never
// closed, so it survives stop and start cycles. The shell is its only receiver
// and fans each snapshot out to every bar.
func (c *Clock) Updates() <-chan time.Time { return c.updates }

// Acquire registers a consumer needing updates at least every boundary.
func (c *Clock) Acquire(boundary time.Duration) (*Lease, error) {
	if boundary <= 0 {
		return nil, fmt.Errorf("services: clock boundary %v is not positive", boundary)
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	lease := &Lease{clock: c, boundary: boundary}
	previous, current := c.leases.add(lease)

	switch {
	case c.stop == nil:
		c.startLocked()
	case previous != 0 && current < previous:
		// The goroutine is asleep on a longer deadline; wake it to re-arm.
		select {
		case c.rearm <- struct{}{}:
		default:
		}
	}
	return lease, nil
}

// Release drops a consumer, stopping the clock when it was the last one. It is
// idempotent and safe on a nil lease.
func (l *Lease) Release() {
	switch {
	case l == nil:
		return
	case l.clock != nil:
		c := l.clock
		l.clock = nil
		c.releaseClock(l)
	case l.metrics != nil:
		m := l.metrics
		l.metrics = nil
		m.releaseMetric(l)
	case l.weather != nil:
		w := l.weather
		l.weather = nil
		w.releaseWeather(l)
	}
}

// releaseClock drops one clock lease, stopping the goroutine when it was the
// last one.
func (c *Clock) releaseClock(l *Lease) {
	c.mu.Lock()
	if !c.leases.remove(l) {
		c.mu.Unlock()
		return
	}
	done := c.stopIfUnusedLocked()
	c.mu.Unlock()

	// Waiting outside the lock: the goroutine takes the same mutex.
	if done != nil {
		<-done
	}
}

// Close releases every lease and stops the goroutine. It is safe to call twice.
func (c *Clock) Close() {
	c.mu.Lock()
	for _, l := range c.leases.clear() {
		l.clock = nil
	}
	done := c.stopIfUnusedLocked()
	c.mu.Unlock()

	if done != nil {
		<-done
	}
}

// Running reports whether the timer goroutine is live.
func (c *Clock) Running() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.stop != nil
}

// Starts counts how many times the goroutine has started.
func (c *Clock) Starts() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.starts
}

func (c *Clock) startLocked() {
	c.stop, c.done = make(chan struct{}), make(chan struct{})
	c.starts++
	go c.run(c.stop, c.done)
}

// stopIfUnusedLocked ends the goroutine when no lease remains, returning the
// channel the caller should wait on after unlocking.
func (c *Clock) stopIfUnusedLocked() chan struct{} {
	if c.leases.len() > 0 || c.stop == nil {
		return nil
	}
	close(c.stop)
	done := c.done
	c.stop, c.done = nil, nil
	return done
}

func (c *Clock) run(stop, done chan struct{}) {
	defer close(done)

	for {
		c.mu.Lock()
		boundary := c.leases.finest()
		c.mu.Unlock()
		if boundary <= 0 {
			return
		}

		timer := time.NewTimer(time.Until(nextBoundary(time.Now(), boundary)))
		select {
		case <-stop:
			timer.Stop()
			return
		case <-c.rearm:
			// A shorter boundary arrived; recompute the deadline.
			timer.Stop()
		case <-timer.C:
			send(c.updates, time.Now())
		}
	}
}

// nextBoundary reports the first instant strictly after now that is aligned to
// b. Each tick recomputes its own deadline from the wall clock, so error
// cannot accumulate the way a fixed-period ticker's does.
func nextBoundary(now time.Time, b time.Duration) time.Time {
	next := now.Truncate(b).Add(b)
	if !next.After(now) {
		next = next.Add(b)
	}
	return next
}

// send publishes the newest time, replacing one the consumer has not read.
// This goroutine is the only sender, so the retry always finds room.
func send(updates chan time.Time, now time.Time) {
	select {
	case updates <- now:
		return
	default:
	}
	select {
	case <-updates:
	default:
	}
	select {
	case updates <- now:
	default:
	}
}
