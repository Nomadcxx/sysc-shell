package wallpaper

import (
	"math"
	"time"
)

// Battery policy tokens for AutoSettings.OnBattery. Config validates the
// same closed vocabulary when loading.
const (
	OnBatteryKeep  = "keep"
	OnBatteryLimit = "limit-10"
	OnBatteryPause = "pause"
)

// UncoverDelay is how long an output must stay uncovered before its
// occluded reason clears. A window that merely moves must not restart a
// video a frame later.
const UncoverDelay = 250 * time.Millisecond

const coverFraction = 0.95

// AutoSettings is the configuration the evaluator reads.
type AutoSettings struct {
	PauseCovered    bool
	PauseLocked     bool
	OnBattery       string
	LowBatteryPause int // charge percent that pauses playback; 0 disables
}

// AutoArea is an output's working area in logical pixels: the output size
// minus this shell's own bar on its edge.
type AutoArea struct {
	X, Y, W, H float64
}

// AutoOutput ties a connector to its working area, its full output size and
// the workspace that workspace-based rules treat as active. OutputW and
// OutputH drive the fullscreen signature used when a window has no tile
// position; see outputCovered.
type AutoOutput struct {
	Connector   string
	Area        AutoArea
	OutputW     float64
	OutputH     float64
	WorkspaceID uint64
}

// AutoWindow is one Niri window projected for the evaluator. X and Y are the
// tile position in workspace-view coordinates and TileInView reports whether
// Niri sent one: Niri 26.04 sends none for tiled windows, including
// fullscreen ones. A window without a layout never covers.
type AutoWindow struct {
	WorkspaceID uint64
	X, Y, W, H  float64
	Floating    bool
	HasLayout   bool
	TileInView  bool
}

// AutoBattery is a battery reading. ChargeValid reports whether Charge is a
// real percentage; an invalid reading or a missing battery reads as AC.
type AutoBattery struct {
	Present     bool
	OnBattery   bool
	Charge      int
	ChargeValid bool
}

// AutoInputs is the complete pure input for one evaluation.
type AutoInputs struct {
	Settings       AutoSettings
	Outputs        []AutoOutput
	Windows        []AutoWindow
	OverviewOpen   bool
	Locked         bool
	LockBackground bool
	ScreensOff     bool
	Battery        AutoBattery
	Now            time.Time
}

// Evaluate computes each output's active automatic reasons. Manual pause is
// never produced here: it lives in the assignment store and survives every
// automatic transition. When deb is not nil, occlusion is debounced: cover
// applies immediately, uncover only after UncoverDelay.
func Evaluate(in AutoInputs, deb *Debouncer) map[string]Reasons {
	out := make(map[string]Reasons)
	if deb != nil {
		deb.retain(in.Outputs)
	}
	for _, output := range in.Outputs {
		var reasons Reasons
		if in.Settings.PauseLocked {
			if in.Locked {
				reasons = reasons.With(ReasonLock)
			}
			if in.LockBackground {
				reasons = reasons.With(ReasonLockBackground)
			}
			// The kiosk switch gates screens-off too: a demo machine must
			// keep playing through a blank.
			if in.ScreensOff {
				reasons = reasons.With(ReasonScreensOff)
			}
		}
		if in.Battery.Present && in.Battery.OnBattery {
			if in.Settings.LowBatteryPause > 0 && in.Battery.ChargeValid && in.Battery.Charge < in.Settings.LowBatteryPause {
				reasons = reasons.With(ReasonLowBattery)
			}
			if in.Settings.OnBattery == OnBatteryPause {
				reasons = reasons.With(ReasonBattery)
			}
		}
		if in.Settings.PauseCovered {
			covered := !in.OverviewOpen && outputCovered(in, output)
			if deb != nil {
				covered = deb.Confirm(output.Connector, covered, in.Now)
			}
			if covered {
				reasons = reasons.With(ReasonOccluded)
			}
		}
		if !reasons.Empty() {
			out[output.Connector] = reasons
		}
	}
	return out
}

// outputCovered reports whether a window on the output's active workspace
// covers it. A tile with a known position (a floating window on every Niri
// release, and any tiled window on releases that report one) has to
// intersect the working area by coverFraction in each dimension.
//
// A tile without a position is judged by size alone: Niri fullscreens a
// tiled window to the whole output, including the strip this shell's bar
// reserved, so a tile that large is a fullscreen window. Niri 26.04 sends
// no position for tiled windows, which also means a tiled window scrolled
// partway out of the workspace view cannot be told apart from one fully in
// view; such a tile that fills the output counts as covering.
func outputCovered(in AutoInputs, output AutoOutput) bool {
	area := output.Area
	if area.W <= 0 || area.H <= 0 {
		return false
	}
	needW, needH := area.W*coverFraction, area.H*coverFraction
	fullW, fullH := output.OutputW*coverFraction, output.OutputH*coverFraction
	for _, w := range in.Windows {
		if w.WorkspaceID != output.WorkspaceID || !w.HasLayout {
			continue
		}
		if !w.TileInView {
			if output.OutputW > 0 && output.OutputH > 0 && w.W >= fullW && w.H >= fullH {
				return true
			}
			continue
		}
		if overlap(w.X, w.W, area.X, area.W) >= needW && overlap(w.Y, w.H, area.Y, area.H) >= needH {
			return true
		}
	}
	return false
}

func overlap(a, length, b, bLength float64) float64 {
	start := math.Max(a, b)
	end := math.Min(a+length, b+bLength)
	if end <= start {
		return 0
	}
	return end - start
}

// BatteryFPS returns the Terminal Art frame cap for the current power
// source under the on-battery policy: 10 fps on battery, 20 on AC, 0 when
// no cap applies.
func BatteryFPS(settings AutoSettings, onBattery bool) int {
	if settings.OnBattery != OnBatteryLimit {
		return 0
	}
	if onBattery {
		return 10
	}
	return 20
}

// Debouncer holds cover state so an output that stops being covered keeps
// its pause for UncoverDelay. Cover applies immediately; only uncover
// waits, absorbing the instant where a window moves between two full-size
// positions. Confirm returns the debounced cover state.
type Debouncer struct {
	covered        map[string]bool
	uncoveredSince map[string]time.Time
}

// Confirm feeds one observation and returns the debounced state.
func (d *Debouncer) Confirm(connector string, covered bool, now time.Time) bool {
	if d.covered == nil {
		d.covered = make(map[string]bool)
		d.uncoveredSince = make(map[string]time.Time)
	}
	if covered {
		d.covered[connector] = true
		delete(d.uncoveredSince, connector)
		return true
	}
	if !d.covered[connector] {
		return false
	}
	since, ok := d.uncoveredSince[connector]
	if !ok {
		d.uncoveredSince[connector] = now
		return true
	}
	if now.Sub(since) >= UncoverDelay {
		d.covered[connector] = false
		delete(d.uncoveredSince, connector)
		return false
	}
	return true
}

// Pending reports whether any output is inside the uncover grace window.
func (d *Debouncer) Pending() bool {
	return len(d.uncoveredSince) > 0
}

// retain drops connectors that are no longer in the input. Without it a
// connector that leaves while its uncover grace is pending keeps Pending
// true forever, and the driver re-arms its wake-up every UncoverDelay.
func (d *Debouncer) retain(outputs []AutoOutput) {
	if len(d.covered) == 0 {
		return
	}
	keep := make(map[string]bool, len(outputs))
	for _, output := range outputs {
		keep[output.Connector] = true
	}
	for connector := range d.covered {
		if !keep[connector] {
			delete(d.covered, connector)
			delete(d.uncoveredSince, connector)
		}
	}
}
