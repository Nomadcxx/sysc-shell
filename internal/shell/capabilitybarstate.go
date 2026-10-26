package shell

import (
	"time"

	"github.com/Nomadcxx/sysc-shell/internal/services"
)

// The capability widgets read their state through barView, so the registry
// keeps one copy of each report beside the service that produced it. The
// relays below are the only writers of those copies: the Wayland owner reads
// them under Registry.mu in viewLocked.

// nightLightStateLocked returns the newest night light report. Before the
// relay seeds it this is the zero state, whose Supported is false, so the
// widget hides until the service reports.
func (r *Registry) nightLightStateLocked() services.NightLightState {
	return r.nightLightState
}

// caffeineStateLocked returns the idle inhibitor's request and deadline.
func (r *Registry) caffeineStateLocked() (bool, time.Time) {
	return r.inhibitWanted, r.inhibitUntil
}

// repaintBarsLocked re-applies the bar view to every bar and returns the
// globals whose content changed. Caller holds r.mu.
func (r *Registry) repaintBarsLocked() []uint32 {
	var changed []uint32
	for global, bar := range r.bars {
		if bar.apply(r.viewLocked(bar.connector())) {
			changed = append(changed, global)
		}
	}
	return changed
}

// repaintCapabilityBars invalidates the bars that took new state. It runs
// outside Registry.mu, matching every other repaint site: the owner drains
// the invalidations channel.
func (r *Registry) repaintCapabilityBars() {
	r.mu.Lock()
	changed := r.repaintBarsLocked()
	r.mu.Unlock()
	r.publish(changed)
}

// relayNightLightUpdates keeps the registry's night light copy current. The
// service had no consumer for its update channel before the bar widget, so
// this relay owns it outright.
func (r *Registry) relayNightLightUpdates() {
	nl := r.nightLight
	if nl == nil {
		return
	}
	seed := nl.State()
	r.mu.Lock()
	r.nightLightState = seed
	r.mu.Unlock()
	r.repaintCapabilityBars()
	ch := nl.Updates()
	for {
		select {
		case <-r.closed:
			return
		case st, ok := <-ch:
			if !ok {
				return
			}
			r.mu.Lock()
			current := r.nightLightState
			if st != current {
				r.nightLightState = st
			}
			r.mu.Unlock()
			if st != current {
				r.repaintCapabilityBars()
			}
		}
	}
}

// ensureLockKeysLocked claims the lock state for a bar that places the
// lock_keys widget. The OSD announces the same state and the change channel
// has a single consumer, so the relay is shared: the OSD announcement is a
// flag on the relay that runs. When the bar is the first consumer, that
// relay holds one lease for the service's lifetime, so the LED class is
// read only while a consumer wants the state.
func (r *Registry) ensureLockKeysLocked() {
	r.lockKeysBars++
	if r.lockKeys != nil {
		return
	}
	r.beginLockKeysLocked()
}

// ensureProfilesLocked starts the one power-profiles fetch the bar needs to
// show anything. Caller holds r.mu. A unit test that installs its own answer
// over the seam calls loadProfilesAsync itself, so adoption never spawns the
// tool from a test binary.
func (r *Registry) ensureProfilesLocked() {
	if r.profilesLoaded {
		return
	}
	r.profilesLoaded = true
	if runningAsTest() {
		return
	}
	go r.loadProfilesAsync()
}

// loadProfilesAsync reads powerprofilesctl off the owner, the same shape the
// session page uses, and republishes the bars with the answer.
func (r *Registry) loadProfilesAsync() {
	// The action handler may have scheduled this work while holding r.mu.
	r.mu.Lock()
	r.mu.Unlock()
	names, active, ok := r.fetchProfiles()
	r.mu.Lock()
	r.profiles, r.profileActive, r.profilesOK = names, active, ok
	r.mu.Unlock()
	r.repaintCapabilityBars()
}

// fetchProfiles runs the list command through the registry seam so tests can
// replace it. Caller holds no lock.
func (r *Registry) fetchProfiles() (names []string, active string, ok bool) {
	r.mu.Lock()
	run := r.runArgvOutput
	r.mu.Unlock()
	out, err := run([]string{"powerprofilesctl", "list"})
	if err != nil {
		return nil, "", false
	}
	names, active = parsePowerProfiles(out)
	return names, active, len(names) > 0
}

// setPowerProfileAsync applies one profile and re-reads the list so the bar
// shows what the service actually reports. Caller holds no lock.
func (r *Registry) setPowerProfileAsync(name string) {
	r.mu.Lock()
	if r.profilesPending || !profileSupports(r.profiles, name) {
		r.mu.Unlock()
		return
	}
	r.profilesPending = true
	run := r.runArgv
	r.mu.Unlock()
	go func() {
		// The click handler may still hold r.mu.
		r.mu.Lock()
		r.mu.Unlock()
		_ = run(powerProfileSetArgv(name))
		names, active, ok := r.fetchProfiles()
		r.mu.Lock()
		r.profilesPending = false
		if ok {
			r.profiles, r.profileActive, r.profilesOK = names, active, true
		} else {
			// The re-read failed: keep the visible selection truthful rather
			// than snapping back to a stale report.
			r.profileActive = name
		}
		r.mu.Unlock()
		r.repaintCapabilityBars()
	}()
}

// cyclePowerProfileBar switches to the next profile in service order.
func (r *Registry) cyclePowerProfileBar() {
	r.mu.Lock()
	next := nextPowerProfile(r.profiles, r.profileActive)
	r.mu.Unlock()
	if next == "" {
		return
	}
	r.setPowerProfileAsync(next)
}

// toggleCaffeineBar flips the idle inhibitor from a bar click. It mirrors
// the Control Centre's off/indefinite presets without a PanelHost: the hold
// is applied by a goroutine that waits for the click handler to leave
// Registry.mu, and the bar repaint stands in for the pane rebuild.
func (r *Registry) toggleCaffeineBar() {
	r.mu.Lock()
	if r.inhibitWanted {
		r.clearCaffeineLocked()
		r.pushIdleInputsLocked()
		if output, ok := r.rebuildScreensaverSettingsPanelLocked(); ok {
			r.publishSurfaceAsync(output, panelSurfaceID(PanelSettings))
		}
		if hold := r.inhibit; hold != nil {
			r.inhibit = nil
			r.shutdown.inhibit.store(nil)
			r.mu.Unlock()
			go func() {
				r.mu.Lock()
				r.mu.Unlock()
				_ = hold.Close()
			}()
		} else {
			r.mu.Unlock()
		}
		r.repaintCapabilityBars()
		return
	}
	r.inhibitWanted = true
	r.pushIdleInputsLocked()
	if output, ok := r.rebuildScreensaverSettingsPanelLocked(); ok {
		r.publishSurfaceAsync(output, panelSurfaceID(PanelSettings))
	}
	if r.inhibit != nil || r.inhibitStarting {
		r.mu.Unlock()
		r.repaintCapabilityBars()
		return
	}
	r.inhibitStarting = true
	start := r.startInhibit
	r.mu.Unlock()
	go func() {
		r.mu.Lock()
		r.mu.Unlock()
		hold, err := start()
		r.mu.Lock()
		r.inhibitStarting = false
		if err != nil {
			r.clearCaffeineLocked()
			r.pushIdleInputsLocked()
			r.mu.Unlock()
			r.repaintCapabilityBars()
			return
		}
		stopped := false
		select {
		case <-r.closed:
			stopped = true
		default:
		}
		keep := r.inhibitWanted && !stopped
		if keep {
			r.inhibit = hold
			r.shutdown.inhibit.store(hold)
		}
		r.mu.Unlock()
		r.repaintCapabilityBars()
		if !keep {
			_ = hold.Close()
		}
	}()
	r.repaintCapabilityBars()
}

// toggleThemeModeBar flips appearance.mode the way the hostcall path does and
// invalidates the bars at once: the text flips before the regenerated theme
// tokens arrive.
func (r *Registry) toggleThemeModeBar() {
	if _, err := r.themeModeToggle(); err != nil {
		return
	}
	r.repaintCapabilityBars()
}
