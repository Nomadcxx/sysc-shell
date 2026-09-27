package shell

import "github.com/Nomadcxx/sysc-shell/internal/services"

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
		}
	}
}
