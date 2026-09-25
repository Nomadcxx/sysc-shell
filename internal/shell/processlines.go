package shell

import (
	"cmp"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"github.com/Nomadcxx/sysc-shell/internal/services"
)

type processLineKind uint8

const (
	lineSection processLineKind = iota
	lineGroup
	lineProcess
)

const (
	sectionApps  = "section:apps"
	sectionProcs = "section:procs"
)

type processTotals struct {
	CPU           float64
	CPUValid      bool
	Resident      uint64
	ResidentValid bool
	Swap          uint64
	SwapValid     bool
	IO            float64
	IOValid       bool
}

type processLine struct {
	Kind       processLineKind
	Key        string
	Depth      int
	Name, Icon string
	Expandable bool
	Expanded   bool
	Identity   services.ProcessIdentity
	User       string
	PIDText    string
	Totals     processTotals
	PID        int // sort key only; zero on groups
}

type processLineInput struct {
	Processes     []services.Process
	Apps          []runningAppSlot
	CurrentUID    uint32
	Query, Owner  string
	Sort          string
	Desc          bool
	Expanded      map[string]bool
	Collapsed     map[string]bool
	ShowApps      bool
	ShowProcesses bool
	Username      func(uint32) string
	AppIcon       func(name string) string
}

// processExecutable, processSwap and processIO read fields the pinned
// sysc-metrics release does not carry yet. Task 13 of the system monitor
// panel plan makes them read the new fields; until then every process is
// grouped by name and SWAP and DISK are unavailable.
func processExecutable(p services.Process) string   { return "" }
func processSwap(p services.Process) (uint64, bool) { return 0, false }
func processIO(p services.Process) (float64, bool)  { return 0, false }

// projectProcessLines is the whole process table as flat lines: an
// Applications section keyed by open windows and a Processes section grouped
// by executable. Filters apply to processes before grouping, so a group's
// totals cover only the members that survived them.
func projectProcessLines(in processLineInput) []processLine {
	kept := make([]services.Process, 0, len(in.Processes))
	for _, p := range in.Processes {
		if keepProcess(p, in) {
			kept = append(kept, p)
		}
	}
	var out []processLine
	if in.ShowApps && len(in.Apps) > 0 {
		// An application found by the name its row shows keeps every member
		// the owner filter allows, not only those whose own text matched.
		owned := make([]services.Process, 0, len(in.Processes))
		unsearched := in
		unsearched.Query = ""
		for _, p := range in.Processes {
			if keepProcess(p, unsearched) {
				owned = append(owned, p)
			}
		}
		out = appendSection(out, in, sectionApps, "Applications", appGroups(in, kept, owned))
	}
	if in.ShowProcesses {
		out = appendSection(out, in, sectionProcs, "Processes", exeGroups(in, kept))
	}
	return out
}

type processGroup struct {
	line    processLine
	members []services.Process
}

func keepProcess(p services.Process, in processLineInput) bool {
	switch in.Owner {
	case "user":
		if !p.UIDValid || p.UID != in.CurrentUID {
			return false
		}
	case "system":
		if !p.UIDValid || p.UID == in.CurrentUID {
			return false
		}
	}
	q := strings.ToLower(strings.TrimSpace(in.Query))
	if q == "" {
		return true
	}
	user := ""
	if p.UIDValid && in.Username != nil {
		user = in.Username(p.UID)
	}
	hay := strings.ToLower(strings.Join(append([]string{p.Name, processExecutable(p), user}, p.Args...), "\x00"))
	return strings.Contains(hay, q)
}

// appGroups assigns each kept process to at most one application: the one
// whose window PID it descends from. Descent stops at another application's
// window PID, so a browser started from a terminal is not counted twice.
func appGroups(in processLineInput, kept, owned []services.Process) []processGroup {
	byPID := make(map[int]services.Process, len(in.Processes))
	children := map[int][]int{}
	for _, p := range in.Processes {
		byPID[p.Identity.PID] = p
		children[p.ParentPID] = append(children[p.ParentPID], p.Identity.PID)
	}
	keptPID := make(map[int]bool, len(kept))
	for _, p := range kept {
		keptPID[p.Identity.PID] = true
	}
	ownedPID := make(map[int]bool, len(owned))
	for _, p := range owned {
		ownedPID[p.Identity.PID] = true
	}
	query := strings.ToLower(strings.TrimSpace(in.Query))
	windowOwner := map[int]string{}
	for _, slot := range in.Apps {
		for _, w := range slot.Members {
			if w.Pid > 0 {
				windowOwner[w.Pid] = slot.Key
			}
		}
	}
	var groups []processGroup
	for _, slot := range in.Apps {
		include := keptPID
		if query != "" && (strings.Contains(strings.ToLower(slot.Name), query) || strings.Contains(strings.ToLower(slot.Key), query)) {
			include = ownedPID
		}
		seen := map[int]bool{}
		var members []services.Process
		var walk func(pid int)
		walk = func(pid int) {
			if seen[pid] {
				return
			}
			if owner, ok := windowOwner[pid]; ok && owner != slot.Key {
				return
			}
			seen[pid] = true
			if p, ok := byPID[pid]; ok && include[pid] {
				members = append(members, p)
			}
			for _, c := range children[pid] {
				walk(c)
			}
		}
		for _, w := range slot.Members {
			if w.Pid > 0 {
				walk(w.Pid)
			}
		}
		if len(members) == 0 {
			continue
		}
		key := "app:" + slot.Key
		groups = append(groups, processGroup{
			line: processLine{Kind: lineGroup, Key: key, Name: slot.Name, Icon: slot.Icon,
				Expandable: true, Expanded: in.Expanded[key]},
			members: members,
		})
	}
	return groups
}

func exeGroups(in processLineInput, kept []services.Process) []processGroup {
	order := []string{}
	byKey := map[string]*processGroup{}
	for _, p := range kept {
		exe := processExecutable(p)
		key, name := "exe:"+exe, filepath.Base(exe)
		if exe == "" {
			key, name = "exe:name:"+p.Name, p.Name
		}
		g, ok := byKey[key]
		if !ok {
			icon := ""
			if in.AppIcon != nil {
				icon = in.AppIcon(name)
			}
			g = &processGroup{line: processLine{Kind: lineGroup, Key: key, Name: name, Icon: icon,
				Expandable: true, Expanded: in.Expanded[key]}}
			byKey[key] = g
			order = append(order, key)
		}
		g.members = append(g.members, p)
	}
	groups := make([]processGroup, 0, len(order))
	for _, key := range order {
		g := *byKey[key]
		if len(g.members) == 1 {
			// A group of one is the process itself.
			leaf := leafLine(in, g.members[0], 0)
			leaf.Icon = g.line.Icon
			g.line = leaf
		}
		groups = append(groups, g)
	}
	return groups
}

func appendSection(out []processLine, in processLineInput, key, title string, groups []processGroup) []processLine {
	if len(groups) == 0 {
		return out
	}
	open := !in.Collapsed[key]
	out = append(out, processLine{Kind: lineSection, Key: key, Name: title, Expandable: true, Expanded: open})
	if !open {
		return out
	}
	for i := range groups {
		if groups[i].line.Kind == lineGroup {
			groups[i].line.Totals, groups[i].line.User = groupTotals(in, groups[i].members)
		}
	}
	slices.SortStableFunc(groups, func(a, b processGroup) int { return compareLines(in, a.line, b.line) })
	for _, g := range groups {
		out = append(out, g.line)
		if g.line.Kind != lineGroup || !g.line.Expanded {
			continue
		}
		leaves := make([]processLine, len(g.members))
		for i, p := range g.members {
			leaves[i] = leafLine(in, p, 1)
		}
		slices.SortStableFunc(leaves, func(a, b processLine) int { return compareLines(in, a, b) })
		out = append(out, leaves...)
	}
	return out
}

func leafLine(in processLineInput, p services.Process, depth int) processLine {
	totals, user := groupTotals(in, []services.Process{p})
	return processLine{
		Kind: lineProcess, Depth: depth, Name: p.Name, Identity: p.Identity, User: user,
		Key:     "pid:" + strconv.Itoa(p.Identity.PID) + ":" + strconv.FormatUint(p.Identity.StartTimeTicks, 10),
		PIDText: strconv.Itoa(p.Identity.PID), PID: p.Identity.PID, Totals: totals,
	}
}

func groupTotals(in processLineInput, members []services.Process) (processTotals, string) {
	var t processTotals
	user, mixed := "", false
	for i, p := range members {
		if p.CPU.Valid {
			t.CPU, t.CPUValid = t.CPU+p.CPU.Fraction, true
		}
		if p.ResidentValid {
			t.Resident, t.ResidentValid = t.Resident+p.ResidentBytes, true
		}
		if s, ok := processSwap(p); ok {
			t.Swap, t.SwapValid = t.Swap+s, true
		}
		if r, ok := processIO(p); ok {
			t.IO, t.IOValid = t.IO+r, true
		}
		name := ""
		if p.UIDValid && in.Username != nil {
			name = in.Username(p.UID)
		}
		if i == 0 {
			user = name
		} else if name != user {
			mixed = true
		}
	}
	if mixed {
		user = ""
	}
	return t, user
}

// compareLines orders two lines by the sort key. A line whose value for that
// key is unavailable sorts last in either direction.
func compareLines(in processLineInput, a, b processLine) int {
	validity := func(l processLine) bool {
		switch in.Sort {
		case "cpu":
			return l.Totals.CPUValid
		case "mem":
			return l.Totals.ResidentValid
		case "swap":
			return l.Totals.SwapValid
		case "io":
			return l.Totals.IOValid
		}
		return true
	}
	if va, vb := validity(a), validity(b); va != vb {
		if va {
			return -1
		}
		return 1
	}
	var order int
	switch in.Sort {
	case "name":
		order = cmp.Compare(strings.ToLower(a.Name), strings.ToLower(b.Name))
	case "cpu":
		order = cmp.Compare(a.Totals.CPU, b.Totals.CPU)
	case "mem":
		order = cmp.Compare(a.Totals.Resident, b.Totals.Resident)
	case "swap":
		order = cmp.Compare(a.Totals.Swap, b.Totals.Swap)
	case "io":
		order = cmp.Compare(a.Totals.IO, b.Totals.IO)
	case "user":
		order = cmp.Compare(a.User, b.User)
	default:
		order = cmp.Compare(a.PID, b.PID)
	}
	if in.Desc {
		order = -order
	}
	if order == 0 {
		order = cmp.Compare(a.Key, b.Key)
	}
	return order
}
