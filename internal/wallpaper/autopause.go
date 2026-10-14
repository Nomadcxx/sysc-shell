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

// AutoOutput ties a connector to its working area and the workspace that
// workspace-based rules treat as active.
type AutoOutput struct {
	Connector   string
	Area        AutoArea
	WorkspaceID uint64
}

// AutoWindow is one Niri window projected for the evaluator. Floating
// windows never cover, and neither do windows without a known layout.
type AutoWindow struct {
	WorkspaceID uint64
	X, Y, W, H  float64
	Floating    bool
	HasLayout   bool
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

// outputCovered reports whether one window on the output's active workspace
// covers at least coverFraction of the working area in each dimension.
func outputCovered(in AutoInputs, output AutoOutput) bool {
	area := output.Area
	if area.W <= 0 || area.H <= 0 {
		return false
	}
	needW, needH := area.W*coverFraction, area.H*coverFraction
	for _, w := range in.Windows {
		if w.WorkspaceID != output.WorkspaceID || w.Floating || !w.HasLayout {
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
