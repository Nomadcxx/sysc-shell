package shell

import (
	"sort"
	"time"

	metrics "github.com/Nomadcxx/sysc-metrics"

	"github.com/Nomadcxx/sysc-shell/internal/wallpaper"
)

// autoPauseBatteryInterval is how often the battery snapshot the autopause
// policy reads is refreshed.
const autoPauseBatteryInterval = 30 * time.Second

// SetScreensOff records whether idle blanking has turned the displays off.
// Only idle blanking counts: a locked session and the Session menu keep the
// screens on, so they are not the screens-off reason.
func (r *Registry) SetScreensOff(off bool) {
	r.mu.Lock()
	changed := r.screensOff != off
	r.screensOff = off
	r.mu.Unlock()
	if changed {
		r.syncAutoPause()
	}
}

// SetLockActive records whether the session lock is up. The locker reports
// through here instead of enqueueing wallpaper commands itself so the
// kiosk switch and the remaining reasons are judged in one place.
func (r *Registry) SetLockActive(active bool) {
	r.mu.Lock()
	changed := r.locked != active
	r.locked = active
	r.mu.Unlock()
	if changed {
		r.syncAutoPause()
	}
}

// syncAutoPause evaluates the wallpaper autopause policy against the current
// registry state and enqueues only the differences. Automatic reasons never
// touch the persisted playback request. It must run without r.mu held.
func (r *Registry) syncAutoPause() {
	r.autoPauseSync.Lock()
	defer r.autoPauseSync.Unlock()

	in, svc, ok := r.autoPauseInputs()
	if !ok {
		return
	}

	desired := wallpaper.Evaluate(in, &r.autoPauseDebounce)
	if in.Settings.OnBattery == wallpaper.OnBatteryLimit && in.Battery.Present && in.Battery.OnBattery {
		unsupported := svc.Snapshot().FPSUnsupported
		for _, out := range in.Outputs {
			if unsupported[out.Connector] {
				// The engine cannot hold a rate bound, so the battery
				// policy falls back to the pause it can deliver.
				desired[out.Connector] = desired[out.Connector].With(wallpaper.ReasonBattery)
			}
		}
	}

	if r.autoPauseDebounce.Pending() {
		// The grace window only ends with time passing, and an idle
		// desktop sends no events; wake up when it elapses.
		time.AfterFunc(wallpaper.UncoverDelay, r.syncAutoPause)
	}

	var cmds []wallpaper.Command
	r.mu.Lock()
	prev := r.autoPauseApplied
	next := make(map[string]wallpaper.Reasons, len(desired))
	seen := make(map[string]bool, len(prev)+len(desired))
	for connector, reasons := range desired {
		next[connector] = reasons
		seen[connector] = true
	}
	for connector := range prev {
		seen[connector] = true
	}
	connectors := make([]string, 0, len(seen))
	for connector := range seen {
		connectors = append(connectors, connector)
	}
	sort.Strings(connectors)
	for _, connector := range connectors {
		added := next[connector] &^ prev[connector]
		removed := prev[connector] &^ next[connector]
		wallpaper.EachReason(func(reason wallpaper.Reason) {
			if added.Has(reason) {
				cmds = append(cmds, wallpaper.Command{Op: wallpaper.OpAutoPause, Token: connector, Reason: reason})
			}
			if removed.Has(reason) {
				cmds = append(cmds, wallpaper.Command{Op: wallpaper.OpAutoResume, Token: connector, Reason: reason})
			}
		})
	}
	r.autoPauseApplied = next

	fps := wallpaper.BatteryFPS(in.Settings, in.Battery.Present && in.Battery.OnBattery)
	present := make(map[string]bool, len(in.Outputs))
	for _, out := range in.Outputs {
		present[out.Connector] = true
	}
	for connector := range r.autoPauseFPS {
		if !present[connector] {
			// A connector that left carries no rate state into its next
			// session, matching the service clearing it on disconnect.
			delete(r.autoPauseFPS, connector)
		}
	}
	for _, out := range in.Outputs {
		connector := out.Connector
		last := r.autoPauseFPS[connector]
		want := fps
		if want == 0 {
			if last == 0 {
				continue
			}
			// Leaving the limit policy restores the engine default.
			want = 20
		}
		if last == 0 && want == 20 {
			// 20 is the engine's own default; sending it to an engine
			// that cannot take a rate would latch the unsupported hint
			// on an idle AC desktop.
			continue
		}
		if last == want {
			continue
		}
		r.autoPauseFPS[connector] = want
		cmds = append(cmds, wallpaper.Command{Op: wallpaper.OpSetFPS, Token: connector, FPS: want})
	}
	r.mu.Unlock()

	for _, cmd := range cmds {
		svc.Enqueue(cmd)
	}
}

// autoPauseInputs snapshots the state the autopause policy reads, together
// with the wallpaper service that will receive the result.
func (r *Registry) autoPauseInputs() (wallpaper.AutoInputs, *wallpaper.Service, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()

	svc := r.wallpaperSvc
	if svc == nil {
		return wallpaper.AutoInputs{}, nil, false
	}

	now := time.Now()
	if r.autoPauseNow != nil {
		now = r.autoPauseNow()
	}
	in := wallpaper.AutoInputs{
		Settings: wallpaper.AutoSettings{
			PauseCovered:    r.cfg.Wallpaper.PauseCovered,
			PauseLocked:     r.cfg.Wallpaper.PauseLocked,
			OnBattery:       r.cfg.Wallpaper.OnBattery,
			LowBatteryPause: r.cfg.Wallpaper.LowBatteryPause,
		},
		OverviewOpen:   r.niriSnapshot.OverviewOpen,
		Locked:         r.locked,
		LockBackground: r.backgroundHeld,
		ScreensOff:     r.screensOff,
		Battery:        r.autoPauseBattery,
		Now:            now,
	}

	active := make(map[string]uint64)
	for _, ws := range r.niriSnapshot.Workspaces {
		if ws.Output == "" || !(ws.Active || ws.Focused) {
			continue
		}
		active[ws.Output] = ws.ID
	}
	for _, bar := range r.bars {
		bar.mu.Lock()
		connector := bar.conn
		width, height := bar.output.width, bar.output.height
		bar.mu.Unlock()
		wsID, ok := active[connector]
		if !ok || width <= 0 || height <= 0 {
			continue
		}
		policy := r.cfg.ForConnector(connector)
		in.Outputs = append(in.Outputs, wallpaper.AutoOutput{
			Connector:   connector,
			Area:        autoPauseArea(policy.Edge, policy.Height, width, height),
			WorkspaceID: wsID,
		})
	}
	for _, w := range r.niriSnapshot.Windows {
		// A window scrolled out of view keeps its tile size but has no
		// view position, so its rectangle would sit wrongly at the origin.
		if !w.HasWorkspace || !w.TileInView {
			continue
		}
		in.Windows = append(in.Windows, wallpaper.AutoWindow{
			WorkspaceID: w.WorkspaceID,
			X:           w.TileX,
			Y:           w.TileY,
			W:           w.TileW,
			H:           w.TileH,
			Floating:    w.Floating,
			HasLayout:   w.HasLayout,
		})
	}
	if len(in.Outputs) == 0 {
		return wallpaper.AutoInputs{}, nil, false
	}
	return in, svc, true
}

// autoPauseArea is the working area of an output with its own bar excluded.
func autoPauseArea(edge string, thickness, width, height int) wallpaper.AutoArea {
	if thickness < 0 {
		thickness = 0
	}
	shrink := func(v int) int {
		if v < 0 {
			return 0
		}
		return v
	}
	switch edge {
	case "bottom":
		return wallpaper.AutoArea{W: float64(width), H: float64(shrink(height - thickness))}
	case "left":
		return wallpaper.AutoArea{X: float64(thickness), W: float64(shrink(width - thickness)), H: float64(height)}
	case "right":
		return wallpaper.AutoArea{W: float64(shrink(width - thickness)), H: float64(height)}
	default: // top
		return wallpaper.AutoArea{Y: float64(thickness), W: float64(width), H: float64(shrink(height - thickness))}
	}
}

// autoPauseBatteryPoll refreshes the battery snapshot the autopause policy
// reads and reevaluates whenever the power source changes.
func (r *Registry) autoPauseBatteryPoll() {
	refresh := func() {
		read := r.readBattery
		if read == nil {
			read = metrics.ReadBattery
		}
		snap, err := read()
		var battery wallpaper.AutoBattery
		if err == nil && snap.Present {
			battery = wallpaper.AutoBattery{
				Present:     true,
				OnBattery:   snap.State == metrics.BatteryDischarging,
				Charge:      int(snap.Charge*100 + 0.5),
				ChargeValid: snap.ChargeValid,
			}
		}
		r.mu.Lock()
		changed := r.autoPauseBattery != battery
		r.autoPauseBattery = battery
		r.mu.Unlock()
		if changed {
			r.syncAutoPause()
		}
	}
	refresh()
	ticker := time.NewTicker(autoPauseBatteryInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			refresh()
		case <-r.closed:
			return
		}
	}
}
