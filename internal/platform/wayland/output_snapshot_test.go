package wayland

import "testing"

func TestOutputSnapshotIncludesIdleHostsAndRemoval(t *testing.T) {
	o := &owner{hosts: newHostSet()}
	h, _ := o.hosts.add(7, nil)
	h.applyName("DP-1")
	h.applyMode(1920, 1080)
	h.applyDone()
	h.state = hostIdle
	var seen []OutputInfo
	o.cb.OutputsChanged = func(outputs []OutputInfo) { seen = outputs }
	o.notifyOutputs()
	if len(seen) != 1 || seen[0].Connector != "DP-1" || seen[0].Width != 0 {
		t.Fatalf("idle output absent, or sized before its scale was known: %+v", seen)
	}
	// The bar is gone, but a surface on the output reported its scale: the
	// published size is logical, not the mode's pixels.
	h.scale = 150
	o.notifyOutputs()
	if len(seen) != 1 || seen[0].Width != 1536 || seen[0].Height != 864 {
		t.Fatalf("logical size ignored the remembered scale: %+v", seen)
	}
	o.hosts.remove(7)
	o.notifyOutputs()
	if len(seen) != 0 {
		t.Fatalf("removed output retained: %+v", seen)
	}
}
