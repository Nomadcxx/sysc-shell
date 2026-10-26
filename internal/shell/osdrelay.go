package shell

import (
	"slices"

	"github.com/Nomadcxx/sysc-shell/internal/config"
	"github.com/Nomadcxx/sysc-shell/internal/platform/niri"
	"github.com/Nomadcxx/sysc-shell/internal/seam"
	"github.com/Nomadcxx/sysc-shell/internal/services"
)

// lockOSD names the lock keys that changed. If both change in one poll, the
// second view replaces the first on screen.
func lockOSD(prev, next services.LockState) []OSDView {
	var out []OSDView
	if prev.Caps != next.Caps {
		out = append(out, OSDView{Kind: osdCapsLock, On: next.Caps})
	}
	if prev.Num != next.Num {
		out = append(out, OSDView{Kind: osdNumLock, On: next.Num})
	}
	return out
}

// relayLockKeys runs outside the Wayland owner so Show can take Registry.mu.
// One relay consumes the change channel for the service's lifetime: the
// channel has a single consumer, so the OSD announcement and the bar widget
// share it. The relay holds the poller's lease while it runs: the registry
// closing or the last consumer going away cancels it, and its return gives
// the lease back, which stops the poll loop with it.
func (r *Registry) relayLockKeys(l *services.LockKeys, cancel <-chan struct{}) {
	if l == nil {
		return
	}
	// The lease starts the poll loop: while a consumer wants the lock state
	// the LED class is read, and when the relay ends the poll loop ends with
	// it.
	release := l.Acquire()
	defer release()
	prev := l.Baseline()
	for {
		select {
		case <-r.closed:
			return
		case <-cancel:
			return
		case st, ok := <-l.Changes():
			if !ok {
				return
			}
			r.mu.Lock()
			announce := r.lockKeysOSD
			r.lockKeysState = st
			changed := r.repaintBarsLocked()
			r.mu.Unlock()
			if announce {
				for _, view := range lockOSD(prev, st) {
					r.OSD().Show(view)
				}
			}
			prev = st
			r.publish(changed)
		}
	}
}

// newLockKeys builds the lock-key service. A test swaps the seam for one
// rooted in a temporary directory.
var newLockKeys = seam.New(func() *services.LockKeys { return services.NewLockKeys("", 0) })

// startLockKeys turns the lock-key announcement on. The service is shared
// with the bars, so the knob only flips a flag when the service is already
// running: the relay that reads the change channel starts announcing. A
// service nobody else wants is built here. Once the registry is live the
// caller holds Registry.mu.
func (r *Registry) startLockKeys(cfg config.Config) {
	if !cfg.Panels.OSDLockKeys {
		return
	}
	r.lockKeysOSD = true
	if r.lockKeys != nil {
		return
	}
	r.beginLockKeysLocked()
}

// beginLockKeysLocked builds the lock-key service and starts the relay that
// reads it. Once the registry is live the caller holds Registry.mu.
func (r *Registry) beginLockKeysLocked() {
	cancel := make(chan struct{})
	l := newLockKeys.Get()()
	r.lockKeys = l
	r.lockKeysRelayCancel = cancel
	// Seed the bar feed before the relay starts: a keyboard that already has
	// Caps Lock on must show it on the first frame, not at the first change.
	// The baseline is only read when a lease starts the poll loop, so take one
	// for the read. The relay takes its own lease next and sees the same state.
	release := l.Acquire()
	r.lockKeysState = l.Baseline()
	release()
	go r.relayLockKeys(l, cancel)
}

// stopLockKeys turns the lock-key announcement off. The service stays while
// a bar places the lock_keys widget: the bars read the same state, so
// closing it would empty their widget. Once the registry is live the caller
// holds Registry.mu.
func (r *Registry) stopLockKeys() {
	r.lockKeysOSD = false
	if r.lockKeysBars > 0 {
		return
	}
	r.endLockKeysLocked()
}

// releaseLockKeysBarLocked gives back one bar's claim on the lock state.
// The service closes when the OSD does not want it either. Once the registry
// is live the caller holds Registry.mu.
func (r *Registry) releaseLockKeysBarLocked() {
	if r.lockKeysBars > 0 {
		r.lockKeysBars--
	}
	if r.lockKeysBars == 0 && !r.lockKeysOSD {
		r.endLockKeysLocked()
	}
}

// endLockKeysLocked cancels the relay and closes the service. The relay is
// cancelled first so its lease comes back, then the service closes: Close
// stops the poll loop even with that lease still outstanding and waits for
// the loop to end, so no poller outlives the consumer that asked for it.
// Once the registry is live the caller holds Registry.mu.
func (r *Registry) endLockKeysLocked() {
	if r.lockKeysRelayCancel != nil {
		close(r.lockKeysRelayCancel)
		r.lockKeysRelayCancel = nil
	}
	if r.lockKeys != nil {
		r.lockKeys.Close()
		r.lockKeys = nil
	}
}

// layoutOSD announces a layout switch. The first snapshot after connecting
// only reports the current layout, so it is never announced.
func layoutOSD(prev, next niri.KeyboardLayouts, seen bool) (OSDView, bool) {
	if !seen || next.Current == prev.Current && slices.Equal(next.Names, prev.Names) {
		return OSDView{}, false
	}
	if next.Current < 0 || next.Current >= len(next.Names) {
		return OSDView{}, false
	}
	return OSDView{Kind: osdLayout, Text: next.Names[next.Current]}, true
}

// mediaOSD announces transport changes: play, pause, stop, a new track, or a
// seek. Position ticks are not announcements.
func mediaOSD(prev, next services.MediaState) (OSDView, bool) {
	if !next.Available {
		return OSDView{}, false
	}
	if next.Status == prev.Status && next.Title == prev.Title && next.Seeks == prev.Seeks {
		return OSDView{}, false
	}
	level := 0
	if next.LengthUS > 0 {
		level = int(next.PositionUS * 100 / next.LengthUS)
	}
	return OSDView{
		Kind:  osdMedia,
		Text:  next.Title,
		Level: level,
		On:    next.Status == services.PlaybackPlaying,
		Muted: next.Status == services.PlaybackStopped,
	}, true
}
