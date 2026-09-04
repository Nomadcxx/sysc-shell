package shell

import (
	"strings"

	"github.com/Nomadcxx/sysc-shell/internal/platform/niri"
)

type runningAppAction struct {
	ID   string
	Name string
	Exec string
}

type runningAppEntry struct {
	ID             string
	Icon           string
	StartupWMClass string
	Actions        []runningAppAction
}

type runningAppSlot struct {
	Key     string
	Icon    string
	Focused bool
	Members []niri.Window
	MRU     niri.Window
	Actions []runningAppAction
}

func groupRunningApps(windows []niri.Window, lookup map[string]runningAppEntry) []runningAppSlot {
	if len(windows) == 0 {
		return nil
	}
	order := make([]string, 0)
	byKey := map[string]*runningAppSlot{}
	for _, w := range windows {
		entry, ok := lookupRunningWindow(w.AppID, lookup)
		key := strings.ToLower(w.AppID)
		if ok {
			key = strings.ToLower(entry.ID)
		}
		slot, exists := byKey[key]
		if !exists {
			slot = &runningAppSlot{Key: key}
			if ok {
				slot.Icon = entry.Icon
				slot.Actions = entry.Actions
			}
			byKey[key] = slot
			order = append(order, key)
		}
		slot.Members = append(slot.Members, w)
		if w.Focused {
			slot.Focused = true
		}
	}
	out := make([]runningAppSlot, 0, len(order))
	for _, key := range order {
		slot := byKey[key]
		slot.MRU = mruMember(slot.Members)
		out = append(out, *slot)
	}
	return out
}

func lookupRunningWindow(appID string, lookup map[string]runningAppEntry) (runningAppEntry, bool) {
	lower := strings.ToLower(appID)
	if e, ok := lookup[lower]; ok {
		return e, true
	}
	if strings.HasPrefix(lower, "steam_app") {
		if e, ok := lookup["steam"]; ok {
			return e, true
		}
	}
	return runningAppEntry{}, false
}

func mruMember(members []niri.Window) niri.Window {
	best := members[0]
	for _, w := range members[1:] {
		if w.FocusTimestamp > best.FocusTimestamp {
			best = w
		}
	}
	return best
}
