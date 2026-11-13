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
	if len(seen) != 1 || seen[0].Connector != "DP-1" || seen[0].Width != 1920 {
		t.Fatalf("idle output absent: %+v", seen)
	}
	o.hosts.remove(7)
	o.notifyOutputs()
	if len(seen) != 0 {
		t.Fatalf("removed output retained: %+v", seen)
	}
}
