package services

import (
	"os"
	"testing"
)

// TestDDCLiveProbe runs the real probe against /dev and /sys when
// SYSC_DDC_LIVE=1 is set. It is the runnable proof that bus-to-connector
// joining works on a given machine; skipped in normal runs because the
// hardware is not guaranteed present.
func TestDDCLiveProbe(t *testing.T) {
	if os.Getenv("SYSC_DDC_LIVE") == "" {
		t.Skip("set SYSC_DDC_LIVE=1 to probe real i2c buses")
	}
	for bus := 0; bus < 8; bus++ {
		connector, max, err := probeDDCBus(bus, "/dev", "/sys/class/drm", "/sys/bus/i2c/devices")
		t.Logf("bus %d: connector=%q max=%d err=%v", bus, connector, max, err)
	}
	b := NewBrightnessDDC("", "", 0)
	defer b.Close()
	if !b.Available() {
		t.Fatal("no brightness devices found")
	}
	for _, d := range b.Displays() {
		t.Logf("display id=%s label=%s kind=%s level=%d ok=%v", d.ID, d.Label, d.Kind, d.Level, d.OK)
	}
}
