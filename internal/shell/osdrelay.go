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

// relayLockKeysOSD runs outside the Wayland owner so Show can take Registry.mu.
func (r *Registry) relayLockKeysOSD(l *services.LockKeys) {
	if l == nil {
		return
	}
	// The lease starts the poll loop: while the OSD wants the lock state the
	// LED class is read, and when the relay ends the poll loop ends with it.
	release := l.Acquire()
	defer release()
	prev := l.Baseline()
	for {
		select {
		case <-r.closed:
			return
		case st, ok := <-l.Changes():
			if !ok {
				return
			}
			for _, view := range lockOSD(prev, st) {
				r.OSD().Show(view)
			}
			prev = st
			// The change channel is single-consumer, so the OSD relay feeds
			// the lock_keys bar widget when it owns the service.
			r.mu.Lock()
			r.lockKeysState = st
			changed := r.repaintBarsLocked()
			r.mu.Unlock()
			r.publish(changed)
		}
	}
}

// newLockKeys builds the lock-key service. A test swaps the seam for one
// rooted in a temporary directory.
var newLockKeys = seam.New(func() *services.LockKeys { return services.NewLockKeys("", 0) })

// startLockKeys wires the lock-key service when the OSD wants it. The OSD
// that shows Caps and Num Lock is the only consumer, so with it off nothing
// reads the keyboard LED class and the shell keeps no poller running.
func (r *Registry) startLockKeys(cfg config.Config) {
	if !cfg.Panels.OSDLockKeys {
		return
	}
	r.lockKeys = newLockKeys.Get()()
	// Seed the bar feed before the relay starts: a keyboard that already has
	// Caps Lock on must show it on the first frame, not at the first change.
	// The baseline is only read when a lease starts the poll loop, so take one
	// for the read. The relay takes its own lease next and sees the same state.
	release := r.lockKeys.Acquire()
	r.lockKeysState = r.lockKeys.Baseline()
	release()
	go r.relayLockKeysOSD(r.lockKeys)
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
