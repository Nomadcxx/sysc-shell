package shell

import (
	"os"
	"strconv"
	"strings"

	"github.com/Nomadcxx/sysc-notify/protocol"
	"github.com/Nomadcxx/sysc-shell/internal/platform/niri"
)

// maxLineage is the design bound: a record may carry at most 16
// {pid,start_time} lineage entries.
const maxLineage = 16

// focusTarget picks the one window an accepted action may focus. The match is
// deliberately conservative: every step that cannot prove identity answers
// no-focus. A stale start_time (recycled pid), a dead process, an ambiguous
// set of windows, or an empty lineage all refuse.
func focusTarget(lineage []protocol.Process, windows []niri.Window, procStart func(pid uint32) (uint64, bool), owns func(niri.Window, uint32) bool) (uint64, bool) {
	if len(lineage) == 0 || len(lineage) > maxLineage {
		return 0, false
	}
	var match uint64
	found := false
	for _, p := range lineage {
		start, ok := procStart(p.PID)
		if !ok || start != p.StartTime {
			continue // dead or recycled
		}
		for _, w := range windows {
			if !owns(w, p.PID) {
				continue
			}
			if found && match != w.ID {
				return 0, false // ambiguous
			}
			match, found = w.ID, true
		}
	}
	return match, found
}

// procStartTime reads a process's start time from /proc so a recycled pid
// never matches its stale lineage entry. start_time is the 22nd field of
// stat; splitting after the comm close parenthesis leaves fields 3..N, so the
// start time sits at index 19 of the remainder.
func procStartTime(pid uint32) (uint64, bool) {
	line, err := os.ReadFile("/proc/" + strconv.FormatUint(uint64(pid), 10) + "/stat")
	if err != nil {
		return 0, false
	}
	end := strings.LastIndex(string(line), ")")
	if end < 0 {
		return 0, false
	}
	fields := strings.Fields(string(line)[end+1:])
	if len(fields) < 20 {
		return 0, false
	}
	start, err := strconv.ParseUint(fields[19], 10, 64)
	if err != nil {
		return 0, false
	}
	return start, true
}
