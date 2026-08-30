package shell

import (
	"testing"
	"time"

	metrics "github.com/Nomadcxx/sysc-metrics"

	"github.com/Nomadcxx/sysc-shell/internal/services"
)

// Absence must be re-evaluated on every snapshot, not decided at startup: a
// removed battery or a lost source must hide the widget with no reload.
func TestBatteryAbsenceIsReEvaluatedEverySnapshot(t *testing.T) {
	t.Parallel()
	reg := NewRegistry(batteryConfig())
	t.Cleanup(reg.Close)
	newHosts(t, reg, map[uint32]string{1: "DP-9"})

	present := services.Snapshot{Battery: &metrics.BatterySnapshot{
		Present: true, Charge: 0.84, ChargeValid: true, State: metrics.BatteryDischarging,
	}}
	absent := services.Snapshot{Battery: &metrics.BatterySnapshot{Present: false}}

	reg.UpdateMetrics(present)
	if got := reg.bars[1].left[0].node.Text; got == "" {
		t.Fatal("a present battery rendered nothing")
	}
	reg.UpdateMetrics(absent)
	if got := reg.bars[1].left[0].node.Text; got != "" {
		t.Fatalf("an absent battery rendered %q, want nothing", got)
	}
	reg.UpdateMetrics(present)
	if got := reg.bars[1].left[0].node.Text; got == "" {
		t.Fatal("a battery that came back rendered nothing")
	}
}

// An unchanged battery must not repaint. A percentage that moves once an hour
// would otherwise cost a frame every interval.
func TestAnUnchangedBatteryChangesNothing(t *testing.T) {
	t.Parallel()
	reg := NewRegistry(batteryConfig())
	t.Cleanup(reg.Close)
	newHosts(t, reg, map[uint32]string{1: "DP-9"})

	snap := services.Snapshot{Battery: &metrics.BatterySnapshot{
		Present: true, Charge: 0.84, ChargeValid: true, State: metrics.BatteryDischarging,
	}}
	if changed := reg.UpdateMetrics(snap); len(changed) != 1 {
		t.Fatalf("first snapshot changed %v, want global 1", changed)
	}
	if changed := reg.UpdateMetrics(snap); len(changed) != 0 {
		t.Fatalf("an identical snapshot changed %v", changed)
	}
}

// Crossing the threshold must repaint, because the tone changes even when the
// glyph band does not.
func TestCrossingTheWarningThresholdRepaints(t *testing.T) {
	t.Parallel()
	reg := NewRegistry(batteryConfig())
	t.Cleanup(reg.Close)
	newHosts(t, reg, map[uint32]string{1: "DP-9"})

	above := services.Snapshot{Battery: &metrics.BatterySnapshot{
		Present: true, Charge: 0.21, ChargeValid: true, State: metrics.BatteryDischarging,
	}}
	below := services.Snapshot{Battery: &metrics.BatterySnapshot{
		Present: true, Charge: 0.19, ChargeValid: true, State: metrics.BatteryDischarging,
	}}

	reg.UpdateMetrics(above)
	if changed := reg.UpdateMetrics(below); len(changed) != 1 {
		t.Fatalf("crossing the threshold changed %v, want global 1", changed)
	}
	if got := reg.bars[1].left[0].node.Tone; got != 1 {
		t.Fatalf("tone = %v below the threshold, want the error tone", got)
	}
}

// Every goroutine must stop with the registry.
func TestClosingTheRegistryStopsBatterySampling(t *testing.T) {
	t.Parallel()
	reg := NewRegistry(batteryConfig())
	newHosts(t, reg, map[uint32]string{1: "DP-9"})
	reg.Close()

	if reg.Metrics().Running() {
		t.Fatal("Close left the sampling service running")
	}
	_ = time.Now()
}
