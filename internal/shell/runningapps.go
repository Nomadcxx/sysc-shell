package shell

import (
	"context"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/go-freedesktop/desktopentry"

	"github.com/Nomadcxx/sysc-shell/internal/icons"
	"github.com/Nomadcxx/sysc-shell/internal/platform/niri"
	"github.com/Nomadcxx/sysc-shell/internal/ui"
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
	Image   *ui.Image
}

type runningAppMenuRow struct {
	Label    string
	ActionID string
	CloseAll bool
}

func groupRunningApps(windows []niri.Window, entries []runningAppEntry) []runningAppSlot {
	if len(windows) == 0 {
		return nil
	}
	order := make([]string, 0)
	byKey := map[string]*runningAppSlot{}
	for _, w := range windows {
		entry, ok := lookupRunningApp(w.AppID, entries)
		if !ok && strings.HasPrefix(strings.ToLower(w.AppID), "steam_app") {
			entry, ok = lookupRunningApp("steam", entries)
		}
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

func runningAppMenu(slot runningAppSlot) []runningAppMenuRow {
	rows := make([]runningAppMenuRow, 0, len(slot.Actions)+1)
	for _, a := range slot.Actions {
		rows = append(rows, runningAppMenuRow{Label: a.Name, ActionID: a.ID})
	}
	return append(rows, runningAppMenuRow{Label: "Close all", CloseAll: true})
}

const (
	runningAppTile     = 24
	runningAppIconSize = 18
	runningAppIconPad  = (runningAppTile - runningAppIconSize) / 2
	runningAppGap      = 4
	runningAppPad      = 8
	runningAppPrefix   = "running-app:"
)

func refreshRunningApps(cap, row *ui.Node, v barView) bool {
	if len(v.Running) == 0 {
		if len(cap.Children) == 0 && cap.Padding == 0 {
			return false
		}
		cap.Children = nil
		cap.Padding = 0
		row.Children = nil
		return true
	}
	if runningAppsMatch(row, v.Running) && len(cap.Children) == 1 {
		return false
	}
	cap.Padding = runningAppPad
	cap.Children = []*ui.Node{row}
	row.Children = row.Children[:0]
	for _, s := range v.Running {
		row.Children = append(row.Children, runningAppTileNode(s))
	}
	return true
}

func runningAppsMatch(row *ui.Node, slots []runningAppSlot) bool {
	if len(row.Children) != len(slots) {
		return false
	}
	for i, s := range slots {
		c := row.Children[i]
		if c == nil || c.Action != runningAppPrefix+s.Key || c.Width != runningAppTile {
			return false
		}
		child := (*ui.Node)(nil)
		if len(c.Children) == 1 {
			child = c.Children[0]
		}
		if s.Image != nil {
			if child == nil || child.Kind != ui.KindImage || child.Image != s.Image {
				return false
			}
			continue
		}
		if child == nil || child.Kind != ui.KindText {
			return false
		}
	}
	return true
}

func runningAppTileNode(s runningAppSlot) *ui.Node {
	child := &ui.Node{Kind: ui.KindText, Text: runningAppLetter(s.Key)}
	if s.Image != nil {
		child = &ui.Node{Kind: ui.KindImage, Image: s.Image, ImageSize: runningAppIconSize}
	}
	return &ui.Node{
		Kind: ui.KindCapsule, Width: runningAppTile, Height: runningAppTile,
		Padding: runningAppIconPad, Fill: ui.FillNone,
		Action: runningAppPrefix + s.Key, Children: []*ui.Node{child},
	}
}

func runningAppLetter(key string) string {
	for _, r := range key {
		return strings.ToUpper(string(r))
	}
	return "?"
}

func runningAppIconPixelSize(scale120 int) int {
	scale := ui.Scale120(scale120)
	if !scale.Valid() {
		return runningAppIconSize
	}
	return max(scale.Physical(runningAppIconSize), 1)
}

func (r *Registry) attachRunningIconsLocked() {
	if r.trayIcons == nil {
		return
	}
	size := runningAppIconSize
	for _, bar := range r.bars {
		size = runningAppIconPixelSize(bar.scale120())
		break
	}
	for i := range r.running {
		name := r.running[i].Icon
		if name == "" {
			r.running[i].Image = nil
			continue
		}
		key := icons.Square(name, size)
		if img, ok := r.trayIcons.Lookup(key); ok {
			r.running[i].Image = img
			continue
		}
		_, _, _ = r.trayIcons.Request(key)
	}
}

func (r *Registry) reprojectRunningApps() {
	r.mu.Lock()
	r.attachRunningIconsLocked()
	changed := make([]uint32, 0, len(r.bars))
	for global, bar := range r.bars {
		if bar.apply(r.viewLocked(bar.connector())) {
			changed = append(changed, global)
		}
	}
	r.mu.Unlock()
	r.publish(changed)
}

func runningAppKey(action string) (string, bool) {
	return strings.CutPrefix(action, runningAppPrefix)
}

func runningSlotPresent(slots []runningAppSlot, key string) bool {
	for _, s := range slots {
		if s.Key == key {
			return true
		}
	}
	return false
}

func (r *Registry) handleRunningAppClick(output uint32, key string, button uint32) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	var slot runningAppSlot
	found := false
	for _, s := range r.running {
		if s.Key == key {
			slot, found = s, true
			break
		}
	}
	if !found {
		return false
	}
	switch button {
	case 0, buttonLeft:
		if id := nextFocusID(slot); id != 0 {
			r.sendNiriLocked(niri.FocusWindow{ID: id})
		}
		return true
	case buttonRight:
		anchor := ui.Rect{}
		if bar, ok := r.bars[output]; ok {
			anchor = bar.actionBounds(runningAppPrefix + key)
		}
		if r.runningMenu == nil {
			r.runningMenu = newRunningAppMenuHost(r)
		}
		r.runningMenu.openLocked(output, slot, anchor)
		return true
	}
	return false
}

func (r *Registry) sendNiriLocked(body any) {
	if r.niriSend != nil {
		if err := r.niriSend(body); err != nil {
			log.Print("niri action: ", err)
		}
		return
	}
	socket := os.Getenv("NIRI_SOCKET")
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		if err := niri.Action(ctx, socket, body); err != nil {
			log.Print("niri action: ", err)
		}
	}()
}

func (r *Registry) spawnDesktopExecLocked(execLine string) {
	argv, err := desktopentry.ExpandExec(&desktopentry.Entry{Exec: execLine}, nil, "")
	if err != nil {
		log.Print("running-apps exec: ", err)
		return
	}
	full := append([]string{"niri", "msg", "action", "spawn", "--"}, argv...)
	if err := r.runArgv(full); err != nil {
		log.Print("niri spawn: ", err)
	}
}

func xdgApplicationDirs() []string {
	dataHome := os.Getenv("XDG_DATA_HOME")
	if dataHome == "" {
		dataHome = filepath.Join(os.Getenv("HOME"), ".local", "share")
	}
	dataDirs := os.Getenv("XDG_DATA_DIRS")
	if dataDirs == "" {
		dataDirs = "/usr/local/share:/usr/share"
	}
	dirs := []string{filepath.Join(dataHome, "applications")}
	for _, d := range strings.Split(dataDirs, ":") {
		if d != "" {
			dirs = append(dirs, filepath.Join(d, "applications"))
		}
	}
	for i, j := 0, len(dirs)-1; i < j; i, j = i+1, j-1 {
		dirs[i], dirs[j] = dirs[j], dirs[i]
	}
	return dirs
}
