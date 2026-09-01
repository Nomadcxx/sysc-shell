package shell

import (
	"testing"

	"github.com/Nomadcxx/sysc-notify/protocol"
	"github.com/Nomadcxx/sysc-shell/internal/platform/niri"
)

func lineage(pid uint32, start uint64) protocol.Process {
	return protocol.Process{PID: pid, StartTime: start}
}

func TestFocusMatchesOneLiveProcess(t *testing.T) {
	live := map[uint32]uint64{42: 100} // pid -> start_time
	window := niri.Window{ID: 5, AppID: "mail"}
	lookup := func(pid uint32) (uint64, bool) { s, ok := live[pid]; return s, ok }

	got, ok := focusTarget([]protocol.Process{lineage(42, 100)}, []niri.Window{window}, lookup, func(w niri.Window, pid uint32) bool { return pid == 42 })
	if !ok || got != 5 {
		t.Fatalf("target = %v ok=%v, want window 5", got, ok)
	}
}

func TestFocusRefusesStaleStartTime(t *testing.T) {
	// The pid was recycled: /proc start time no longer matches the lineage.
	live := map[uint32]uint64{42: 999}
	lookup := func(pid uint32) (uint64, bool) { s, ok := live[pid]; return s, ok }
	window := niri.Window{ID: 5, AppID: "mail"}

	if _, ok := focusTarget([]protocol.Process{lineage(42, 100)}, []niri.Window{window}, lookup, func(w niri.Window, pid uint32) bool { return true }); ok {
		t.Fatal("focused a recycled pid")
	}
}

func TestFocusRefusesAmbiguity(t *testing.T) {
	live := map[uint32]uint64{42: 100}
	lookup := func(pid uint32) (uint64, bool) { s, ok := live[pid]; return s, ok }
	two := []niri.Window{{ID: 5, AppID: "mail"}, {ID: 6, AppID: "mail"}}

	if _, ok := focusTarget([]protocol.Process{lineage(42, 100)}, two, lookup, func(w niri.Window, pid uint32) bool { return true }); ok {
		t.Fatal("focused one of two matches")
	}
}

func TestFocusRefusesDeadProcesses(t *testing.T) {
	lookup := func(uint32) (uint64, bool) { return 0, false } // pid is gone
	window := niri.Window{ID: 5}
	if _, ok := focusTarget([]protocol.Process{lineage(42, 100)}, []niri.Window{window}, lookup, func(niri.Window, uint32) bool { return true }); ok {
		t.Fatal("focused a dead process")
	}
}

func TestFocusRefusesEmptyLineage(t *testing.T) {
	lookup := func(uint32) (uint64, bool) { return 0, false }
	if _, ok := focusTarget(nil, []niri.Window{{ID: 5}}, lookup, func(niri.Window, uint32) bool { return true }); ok {
		t.Fatal("focused with no lineage")
	}
}
