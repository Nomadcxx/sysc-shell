package shell

import (
	"slices"

	"github.com/Nomadcxx/sysc-shell/internal/platform/niri"
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
