package shell

import (
	"fmt"
	"time"

	metrics "github.com/Nomadcxx/sysc-metrics"

	"github.com/Nomadcxx/sysc-shell/internal/config"
	"github.com/Nomadcxx/sysc-shell/internal/render"
	"github.com/Nomadcxx/sysc-shell/internal/services"
	"github.com/Nomadcxx/sysc-shell/internal/ui"
)

// formatBattery renders one battery and the tone it paints in.
//
// Absence renders nothing rather than a placeholder: empty text measures
// zero-wide, so the section shrinks and one configuration works unchanged on a
// laptop and a desktop. This is decided from the current snapshot on every
// pass, so a battery appearing or disappearing needs no reload.
func formatBattery(item config.Item, snap services.Snapshot) (string, ui.Tone) {
	battery := snap.Battery
	if battery == nil || !battery.Present || !battery.ChargeValid {
		return "", ui.ToneNormal
	}

	charging := battery.State == metrics.BatteryCharging || battery.State == metrics.BatteryFull
	critical := !charging && battery.Charge*100 <= float64(item.WarnBelow)

	text := string(render.BatteryIconRune(battery.Charge, charging, critical))
	if label := batteryLabel(item, battery); label != "" {
		text += " " + label
	}

	if critical {
		return text, ui.ToneError
	}
	return text, ui.ToneNormal
}

// batteryLabel renders the configured field, or empty when the mode is none or
// the underlying value has not settled.
func batteryLabel(item config.Item, battery *metrics.BatterySnapshot) string {
	switch item.Label {
	case "time":
		// A battery just plugged in genuinely has no estimate for a few
		// seconds. Rendering nothing beats flickering a placeholder.
		if !battery.TimeValid {
			return ""
		}
		return batteryDuration(battery.TimeRemaining)
	case "rate":
		if !battery.RateValid {
			return ""
		}
		return fmt.Sprintf("%.1f W", battery.RateWatts)
	case "none":
		return ""
	}
	return fmt.Sprintf("%.0f%%", battery.Charge*100)
}

// batteryDuration renders hours and minutes. A bar has no room for seconds and
// no battery estimate is accurate to one.
func batteryDuration(d time.Duration) string {
	if d < 0 {
		return ""
	}
	hours := int(d.Hours())
	minutes := int(d.Minutes()) % 60
	if hours > 0 {
		return fmt.Sprintf("%dh%02dm", hours, minutes)
	}
	return fmt.Sprintf("%dm", minutes)
}
