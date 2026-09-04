package shell

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/go-freedesktop/desktopentry"

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

func lookupRunningApp(appID string, entries []runningAppEntry) (runningAppEntry, bool) {
	for _, e := range entries {
		if strings.EqualFold(e.ID, appID) {
			return e, true
		}
	}
	for _, e := range entries {
		if e.StartupWMClass != "" && strings.EqualFold(e.StartupWMClass, appID) {
			return e, true
		}
	}
	if i := strings.LastIndexAny(appID, "./"); i >= 0 && i+1 < len(appID) {
		tail := appID[i+1:]
		for _, e := range entries {
			if strings.EqualFold(e.ID, tail) {
				return e, true
			}
		}
	}
	return runningAppEntry{}, false
}

// loadRunningAppEntries walks XDG application dirs. Hidden tombstones drop an
// id; NoDisplay entries stay so a running window still has an icon.
func loadRunningAppEntries(dirs []string) []runningAppEntry {
	byID := map[string]runningAppEntry{}
	for _, dir := range dirs {
		_ = filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() || !strings.HasSuffix(d.Name(), ".desktop") {
				return nil
			}
			rel, relErr := filepath.Rel(dir, path)
			if relErr != nil {
				return nil
			}
			id := strings.ReplaceAll(strings.TrimSuffix(rel, ".desktop"), string(filepath.Separator), "-")
			de, perr := desktopentry.ParseFile(path)
			if perr != nil {
				return nil
			}
			if de.Hidden {
				delete(byID, id)
				return nil
			}
			e := runningAppEntry{ID: id, Icon: de.Icon, StartupWMClass: de.StartupWMClass}
			// ponytail: go-freedesktop leaves Action.ID empty; zip Actions= order.
			ids := desktopActionIDs(path)
			for i, a := range de.Actions {
				aid := a.ID
				if aid == "" && i < len(ids) {
					aid = ids[i]
				}
				e.Actions = append(e.Actions, runningAppAction{ID: aid, Name: a.Name, Exec: a.Exec})
			}
			byID[id] = e
			return nil
		})
	}
	out := make([]runningAppEntry, 0, len(byID))
	for _, e := range byID {
		out = append(out, e)
	}
	return out
}

func desktopActionIDs(path string) []string {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	for _, line := range strings.Split(string(raw), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "[") && line != "[Desktop Entry]" {
			return nil
		}
		if rest, ok := strings.CutPrefix(line, "Actions="); ok {
			var ids []string
			for _, p := range strings.Split(rest, ";") {
				if p != "" {
					ids = append(ids, p)
				}
			}
			return ids
		}
	}
	return nil
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

func nextFocusID(slot runningAppSlot) uint64 {
	if len(slot.Members) == 0 {
		return 0
	}
	for i, w := range slot.Members {
		if w.Focused {
			return slot.Members[(i+1)%len(slot.Members)].ID
		}
	}
	return slot.MRU.ID
}
