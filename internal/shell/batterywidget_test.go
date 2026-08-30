package shell

import (
	"strings"
	"testing"
	"time"

	metrics "github.com/Nomadcxx/sysc-metrics"

	"github.com/Nomadcxx/sysc-shell/internal/config"
	"github.com/Nomadcxx/sysc-shell/internal/render"
	"github.com/Nomadcxx/sysc-shell/internal/services"
	"github.com/Nomadcxx/sysc-shell/internal/ui"
)

// batteryAt builds a snapshot with one battery in a given state.
func batteryAt(charge float64, state metrics.BatteryState) services.Snapshot {
	return services.Snapshot{Battery: &metrics.BatterySnapshot{
		Present: true, Charge: charge, ChargeValid: true, State: state,
		RateWatts: 12.4, RateValid: true,
		TimeRemaining: 2*time.Hour + 14*time.Minute, TimeValid: true,
	}}
}

func TestABatteryRendersItsGlyphAndPercent(t *testing.T) {
	t.Parallel()
	item := config.Item{ID: "battery", Label: "percent", WarnBelow: 20}

	text, tone := formatBattery(item, batteryAt(0.84, metrics.BatteryDischarging))
	if tone != ui.ToneNormal {
		t.Fatalf("tone = %v, want normal at 84%%", tone)
	}
	if !strings.Contains(text, "84%") {
		t.Fatalf("text %q does not carry the percentage", text)
	}
	if !strings.ContainsRune(text, render.BatteryIconRune(0.84, false, false)) {
		t.Fatalf("text %q does not carry the level glyph", text)
	}
}

// Absence renders nothing at all, so the section shrinks on a desktop.
func TestAnAbsentBatteryRendersNothing(t *testing.T) {
	t.Parallel()
	item := config.Item{ID: "battery", Label: "percent", WarnBelow: 20}

	snap := services.Snapshot{Battery: &metrics.BatterySnapshot{Present: false}}
	if text, _ := formatBattery(item, snap); text != "" {
		t.Fatalf("an absent battery rendered %q, want nothing", text)
	}
	// An unleased or failed source is equally absent.
	if text, _ := formatBattery(item, services.Snapshot{}); text != "" {
		t.Fatalf("a missing battery source rendered %q, want nothing", text)
	}
}

// The warning is a conjunction: low while charging is not a warning.
func TestTheWarningToneRequiresLowAndNotCharging(t *testing.T) {
	t.Parallel()
	item := config.Item{ID: "battery", Label: "percent", WarnBelow: 20}

	if _, tone := formatBattery(item, batteryAt(0.15, metrics.BatteryDischarging)); tone != ui.ToneError {
		t.Fatalf("tone = %v at 15%% discharging, want error", tone)
	}
	if _, tone := formatBattery(item, batteryAt(0.15, metrics.BatteryCharging)); tone != ui.ToneNormal {
		t.Fatalf("tone = %v at 15%% charging, want normal", tone)
	}
	if _, tone := formatBattery(item, batteryAt(0.85, metrics.BatteryDischarging)); tone != ui.ToneNormal {
		t.Fatalf("tone = %v at 85%% discharging, want normal", tone)
	}
}

// The threshold is inclusive at its boundary and exclusive above it.
func TestTheWarningThresholdBoundary(t *testing.T) {
	t.Parallel()
	item := config.Item{ID: "battery", Label: "percent", WarnBelow: 20}

	if _, tone := formatBattery(item, batteryAt(0.20, metrics.BatteryDischarging)); tone != ui.ToneError {
		t.Fatal("exactly at the threshold did not warn")
	}
	if _, tone := formatBattery(item, batteryAt(0.21, metrics.BatteryDischarging)); tone != ui.ToneNormal {
		t.Fatal("one point above the threshold warned")
	}
}

func TestEachLabelModeRendersItsField(t *testing.T) {
	t.Parallel()
	snap := batteryAt(0.84, metrics.BatteryDischarging)

	percent, _ := formatBattery(config.Item{ID: "battery", Label: "percent", WarnBelow: 20}, snap)
	if !strings.Contains(percent, "84%") {
		t.Fatalf("percent mode rendered %q", percent)
	}
	timeMode, _ := formatBattery(config.Item{ID: "battery", Label: "time", WarnBelow: 20}, snap)
	if !strings.Contains(timeMode, "2h14m") {
		t.Fatalf("time mode rendered %q, want 2h14m", timeMode)
	}
	rate, _ := formatBattery(config.Item{ID: "battery", Label: "rate", WarnBelow: 20}, snap)
	if !strings.Contains(rate, "12.4 W") {
		t.Fatalf("rate mode rendered %q, want 12.4 W", rate)
	}
	none, _ := formatBattery(config.Item{ID: "battery", Label: "none", WarnBelow: 20}, snap)
	if strings.ContainsAny(none, "0123456789") {
		t.Fatalf("none mode rendered %q, want the glyph alone", none)
	}
}

// An unsettled estimate renders no label rather than a placeholder, because a
// battery just plugged in genuinely has no estimate for a few seconds.
func TestAnInvalidTimeEstimateRendersNoLabel(t *testing.T) {
	t.Parallel()
	snap := services.Snapshot{Battery: &metrics.BatterySnapshot{
		Present: true, Charge: 0.5, ChargeValid: true,
		State: metrics.BatteryCharging, TimeValid: false,
	}}

	text, _ := formatBattery(config.Item{ID: "battery", Label: "time", WarnBelow: 20}, snap)
	if strings.ContainsAny(text, "0123456789") {
		t.Fatalf("an invalid estimate rendered %q, want the glyph alone", text)
	}
	if text == "" {
		t.Fatal("an invalid estimate hid the whole widget; the battery is still present")
	}
}

// An invalid charge is absent data, not zero per cent.
func TestAnInvalidChargeRendersNothing(t *testing.T) {
	t.Parallel()
	snap := services.Snapshot{Battery: &metrics.BatterySnapshot{
		Present: true, ChargeValid: false, State: metrics.BatteryDischarging,
	}}
	if text, _ := formatBattery(config.Item{ID: "battery", Label: "percent"}, snap); text != "" {
		t.Fatalf("an invalid charge rendered %q, want nothing", text)
	}
}

func TestTheChargingGlyphFollowsTheState(t *testing.T) {
	t.Parallel()
	item := config.Item{ID: "battery", Label: "none", WarnBelow: 20}

	discharging, _ := formatBattery(item, batteryAt(0.5, metrics.BatteryDischarging))
	charging, _ := formatBattery(item, batteryAt(0.5, metrics.BatteryCharging))
	if discharging == charging {
		t.Fatal("charging and discharging rendered the same glyph")
	}
}
