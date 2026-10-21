package shell

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
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
	Name           string
	Icon           string
	StartupWMClass string
	Actions        []runningAppAction
}

type runningAppSlot struct {
	Key          string
	Name         string
	Icon         string
	Focused      bool
	Members      []niri.Window
	SteamGameIDs map[uint64]struct{}
	MRU          niri.Window
	Actions      []runningAppAction
	Image        *ui.Image
}

type runningAppMenuRowKind uint8

const (
	runningAppMenuHeader runningAppMenuRowKind = iota
	runningAppMenuSection
	runningAppMenuWindow
	runningAppMenuFloating
	runningAppMenuAction
	runningAppMenuWorkspace
	runningAppMenuForceQuit
	runningAppMenuForceQuitConfirm
	runningAppMenuForceQuitCancel
	runningAppMenuForceQuitDismiss
)

type runningAppMenuRow struct {
	Kind            runningAppMenuRowKind
	Label           string
	Title           string
	WindowID        uint64
	Focused         bool
	ActionID        string
	MoveWindowID    uint64
	MoveWorkspaceID uint64
}

type runningAppForceQuitState uint8

const (
	runningAppForceQuitIdle runningAppForceQuitState = iota
	runningAppForceQuitConfirming
	runningAppForceQuitPending
	runningAppForceQuitFailed
)

func groupRunningApps(windows []niri.Window, entries []runningAppEntry) []runningAppSlot {
	if len(windows) == 0 {
		return nil
	}
	order := make([]string, 0)
	byKey := map[string]*runningAppSlot{}
	for _, w := range windows {
		foldedSteamGame := false
		entry, ok := lookupRunningApp(w.AppID, entries)
		if !ok && steamGameAppID(w.AppID) {
			entry, ok = lookupRunningApp("steam", entries)
			foldedSteamGame = ok
		}
		key := strings.ToLower(w.AppID)
		if ok {
			key = strings.ToLower(entry.ID)
		}
		slot, exists := byKey[key]
		if !exists {
			slot = &runningAppSlot{Key: key, Name: w.AppID}
			if ok {
				slot.Icon = entry.Icon
				slot.Actions = entry.Actions
				if entry.Name != "" {
					slot.Name = entry.Name
				}
			}
			byKey[key] = slot
			order = append(order, key)
		}
		slot.Members = append(slot.Members, w)
		if foldedSteamGame {
			if slot.SteamGameIDs == nil {
				slot.SteamGameIDs = make(map[uint64]struct{})
			}
			slot.SteamGameIDs[w.ID] = struct{}{}
		}
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
			e := runningAppEntry{ID: id, Name: de.Name, Icon: de.Icon, StartupWMClass: de.StartupWMClass}
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
		if compareWindowMRU(w, best) < 0 {
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

func runningAppMenu(slot runningAppSlot, workspaces []niri.Workspace) []runningAppMenuRow {
	if len(slot.Members) == 0 && slot.MRU.ID == 0 && len(slot.Actions) == 0 {
		return nil
	}
	name := slot.Name
	if name == "" {
		name = slot.Key
	}
	rows := []runningAppMenuRow{{
		Kind:  runningAppMenuHeader,
		Label: fmt.Sprintf("%s (%s)", name, windowCount(len(slot.Members))),
	}}
	if len(slot.Members) > 0 {
		rows = append(rows, runningAppMenuRow{
			Kind: runningAppMenuForceQuit, Label: "Close all and force quit",
		})
	}

	members := slices.Clone(slot.Members)
	slices.SortFunc(members, compareWindowMRU)
	var gameRows []runningAppMenuRow
	for _, window := range members {
		row := runningAppWindowRow(window, workspaces)
		floating := runningAppFloatingRow(window)
		if foldedSteamGameWindow(slot, window) {
			gameRows = append(gameRows, row)
			if floating.WindowID != 0 {
				gameRows = append(gameRows, floating)
			}
			continue
		}
		rows = append(rows, row)
		if floating.WindowID != 0 {
			rows = append(rows, floating)
		}
	}
	if len(gameRows) > 0 {
		rows = append(rows, runningAppMenuRow{Kind: runningAppMenuSection, Label: "Games"})
		rows = append(rows, gameRows...)
	}

	for _, a := range slot.Actions {
		rows = append(rows, runningAppMenuRow{Kind: runningAppMenuAction, Label: a.Name, ActionID: a.ID})
	}
	if slot.MRU.ID != 0 {
		for _, workspace := range workspaces {
			if slot.MRU.HasWorkspace && workspace.ID == slot.MRU.WorkspaceID {
				continue
			}
			name := workspace.Name
			if name == "" {
				name = "Workspace " + strconv.Itoa(workspace.Index)
			}
			rows = append(rows, runningAppMenuRow{
				Kind:  runningAppMenuWorkspace,
				Label: "Move " + runningAppWindowTitle(slot.MRU) + " to " + name, MoveWindowID: slot.MRU.ID,
				MoveWorkspaceID: workspace.ID,
			})
		}
	}
	return rows
}

func runningAppForceQuitMenuRows(slot runningAppSlot, state runningAppForceQuitState, message string, workspaces []niri.Workspace) []runningAppMenuRow {
	name := slot.Name
	if name == "" {
		name = slot.Key
	}
	switch state {
	case runningAppForceQuitConfirming:
		return []runningAppMenuRow{
			{Kind: runningAppMenuHeader, Label: "Force quit " + name + "?"},
			{Kind: runningAppMenuSection, Label: name + " and its associated processes will stop."},
			{Kind: runningAppMenuForceQuitCancel, Label: "Cancel"},
			{Kind: runningAppMenuForceQuitConfirm, Label: "Force quit"},
		}
	case runningAppForceQuitPending:
		return []runningAppMenuRow{
			{Kind: runningAppMenuHeader, Label: "Stopping " + name},
			{Kind: runningAppMenuSection, Label: "Stopping the app scope and its associated processes…"},
			{Kind: runningAppMenuForceQuitDismiss, Label: "Close menu"},
		}
	case runningAppForceQuitFailed:
		if message == "" {
			message = "Could not stop the app and its associated processes. Check sysc-shell logs."
		}
		return []runningAppMenuRow{
			{Kind: runningAppMenuHeader, Label: "Force quit failed"},
			{Kind: runningAppMenuSection, Label: message},
			{Kind: runningAppMenuForceQuitDismiss, Label: "Close menu"},
		}
	default:
		return runningAppMenu(slot, workspaces)
	}
}

func runningAppForceQuitErrorMessage(err error) string {
	message := "Could not stop the app and its associated processes."
	if err != nil {
		if detail := strings.TrimSpace(err.Error()); detail != "" {
			message += " " + detail
		}
	}
	runes := []rune(message)
	if len(runes) > 180 {
		return string(runes[:179]) + "…"
	}
	return message
}

func runningAppWindowRow(window niri.Window, workspaces []niri.Workspace) runningAppMenuRow {
	label := runningAppWindowTitle(window)
	if window.HasWorkspace {
		for _, workspace := range workspaces {
			if workspace.ID != window.WorkspaceID {
				continue
			}
			workspaceName := workspace.Name
			if workspaceName == "" {
				workspaceName = "Workspace " + strconv.Itoa(workspace.Index)
			}
			label += " — " + workspaceName
			break
		}
	}
	return runningAppMenuRow{
		Kind: runningAppMenuWindow, Label: label, Title: runningAppWindowTitle(window),
		WindowID: window.ID, Focused: window.Focused,
	}
}

func runningAppFloatingRow(window niri.Window) runningAppMenuRow {
	if window.ID == 0 {
		return runningAppMenuRow{}
	}
	placement := "floating"
	if window.Floating {
		placement = "tiling"
	}
	return runningAppMenuRow{
		Kind: runningAppMenuFloating, Label: "Move " + runningAppWindowTitle(window) + " to " + placement,
		WindowID: window.ID,
	}
}

func runningAppWindowTitle(window niri.Window) string {
	if strings.TrimSpace(window.Title) == "" {
		return "Untitled window"
	}
	return window.Title
}

func foldedSteamGameWindow(slot runningAppSlot, window niri.Window) bool {
	_, ok := slot.SteamGameIDs[window.ID]
	return ok
}

func steamGameAppID(appID string) bool {
	return strings.HasPrefix(strings.ToLower(appID), "steam_app_")
}

func runningAppScopeAppIDs(slot runningAppSlot) []string {
	seen := make(map[string]struct{}, len(slot.Members))
	ids := make([]string, 0, len(slot.Members))
	for _, window := range slot.Members {
		appID := window.AppID
		if foldedSteamGameWindow(slot, window) {
			appID = "steam"
		}
		if appID == "" {
			continue
		}
		if _, ok := seen[appID]; ok {
			continue
		}
		seen[appID] = struct{}{}
		ids = append(ids, appID)
	}
	slices.Sort(ids)
	return ids
}

func runningAppScopeWindowPIDs(slot runningAppSlot) []int {
	seen := make(map[int]struct{}, len(slot.Members))
	var pids []int
	for _, window := range slot.Members {
		if window.Pid <= 0 {
			continue
		}
		if _, exists := seen[window.Pid]; exists {
			continue
		}
		seen[window.Pid] = struct{}{}
		pids = append(pids, window.Pid)
	}
	slices.Sort(pids)
	return pids
}

type niriAppScopeProperties struct {
	ID           string
	LoadState    string
	ActiveState  string
	KillMode     string
	ControlGroup string
}

func appScopeUnitMatchesAppID(escapedAppID, unit string) bool {
	if escapedAppID == "" {
		return false
	}
	if !strings.HasSuffix(unit, ".scope") {
		return false
	}
	for _, prefix := range []string{"app-" + escapedAppID + "-", "app-niri-" + escapedAppID + "-"} {
		if !strings.HasPrefix(unit, prefix) {
			continue
		}
		pid := strings.TrimSuffix(strings.TrimPrefix(unit, prefix), ".scope")
		value, err := strconv.ParseUint(pid, 10, 64)
		if err == nil && value > 0 && strconv.FormatUint(value, 10) == pid {
			return true
		}
	}
	return false
}

func appScopeUnitMatchesWindowPID(pid int, unit string) bool {
	const prefix = "app-niri-"
	if pid <= 0 || !strings.HasPrefix(unit, prefix) {
		return false
	}
	value, ok := appScopeUnitPID(unit)
	return ok && value == uint64(pid)
}

func appScopeUnitPID(unit string) (uint64, bool) {
	const prefix = "app-"
	if !strings.HasPrefix(unit, prefix) || !strings.HasSuffix(unit, ".scope") {
		return 0, false
	}
	base := strings.TrimSuffix(unit, ".scope")
	lastDash := strings.LastIndexByte(base, '-')
	if lastDash <= len(prefix) {
		return 0, false
	}
	suffix := base[lastDash+1:]
	value, err := strconv.ParseUint(suffix, 10, 64)
	return value, err == nil && value > 0 && strconv.FormatUint(value, 10) == suffix
}

func appScopeUnitFromProcCgroup(uid int, contents string) (string, bool) {
	prefix := fmt.Sprintf("/user.slice/user-%d.slice/user@%d.service/app.slice/", uid, uid)
	for _, line := range strings.Split(contents, "\n") {
		fields := strings.SplitN(line, ":", 3)
		if len(fields) != 3 || fields[0] != "0" || fields[1] != "" {
			continue
		}
		path := fields[2]
		if !strings.HasPrefix(path, prefix) {
			return "", false
		}
		unit := strings.TrimPrefix(path, prefix)
		if strings.Contains(unit, "/") {
			return "", false
		}
		if _, ok := appScopeUnitPID(unit); !ok {
			return "", false
		}
		return unit, true
	}
	return "", false
}

func readProcessCgroupDefault(pid int) (string, error) {
	if pid <= 0 {
		return "", errors.New("invalid process ID")
	}
	contents, err := os.ReadFile(filepath.Join("/proc", strconv.Itoa(pid), "cgroup"))
	if err != nil {
		return "", fmt.Errorf("read process %d cgroup: %w", pid, err)
	}
	return string(contents), nil
}

func parseNiriAppScopeProperties(output string) niriAppScopeProperties {
	var properties niriAppScopeProperties
	for _, line := range strings.Split(output, "\n") {
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		switch key {
		case "Id":
			properties.ID = value
		case "LoadState":
			properties.LoadState = value
		case "ActiveState":
			properties.ActiveState = value
		case "KillMode":
			properties.KillMode = value
		case "ControlGroup":
			properties.ControlGroup = value
		}
	}
	return properties
}

func validateNiriAppScopeProperties(uid int, unit string, properties niriAppScopeProperties) error {
	if properties.ID != unit || properties.LoadState != "loaded" || properties.ActiveState != "active" {
		return fmt.Errorf("scope %q is not the same loaded active unit", unit)
	}
	if properties.KillMode != "control-group" {
		return fmt.Errorf("scope %q does not terminate its control group", unit)
	}
	expected := fmt.Sprintf("/user.slice/user-%d.slice/user@%d.service/app.slice/%s", uid, uid, unit)
	if properties.ControlGroup != expected {
		return fmt.Errorf("scope %q is outside the current user's app.slice", unit)
	}
	return nil
}

func parseActiveAppScopeUnits(output string) []string {
	var units []string
	for _, line := range strings.Split(output, "\n") {
		fields := strings.Fields(line)
		if len(fields) > 0 && strings.HasSuffix(fields[0], ".scope") {
			units = append(units, fields[0])
		}
	}
	return units
}

func (r *Registry) forceQuitRunningAppScopes(appIDs []string, windowPIDs []int) error {
	ids := slices.Clone(appIDs)
	slices.Sort(ids)
	ids = slices.Compact(ids)
	pids := make([]int, 0, len(windowPIDs))
	for _, pid := range windowPIDs {
		if pid > 0 {
			pids = append(pids, pid)
		}
	}
	slices.Sort(pids)
	pids = slices.Compact(pids)
	processScopes := make(map[string]struct{}, len(pids))
	var processCgroupErrors []error
	if r.readProcessCgroup != nil {
		for _, pid := range pids {
			contents, err := r.readProcessCgroup(pid)
			if err != nil {
				processCgroupErrors = append(processCgroupErrors, fmt.Errorf("read current Niri window PID %d cgroup: %w", pid, err))
				continue
			}
			if unit, ok := appScopeUnitFromProcCgroup(os.Getuid(), contents); ok {
				processScopes[unit] = struct{}{}
			}
		}
	}
	if len(ids) == 0 && len(pids) == 0 {
		return errors.New("no current Niri app IDs or window PIDs")
	}
	if r.runArgvOutput == nil {
		return errors.New("systemd command runner is unavailable")
	}

	escaped := make([]string, 0, len(ids))
	for _, appID := range ids {
		if appID == "" {
			return errors.New("empty Niri app ID")
		}
		output, err := r.runArgvOutput([]string{"systemd-escape", "--", appID})
		if err != nil {
			return fmt.Errorf("escape Niri app ID: %w", err)
		}
		value := strings.TrimSpace(output)
		if value == "" {
			return errors.New("systemd returned an empty escaped Niri app ID")
		}
		escaped = append(escaped, value)
	}

	unitsOutput, err := r.runArgvOutput([]string{
		"systemctl", "--user", "list-units", "--type=scope", "--state=active",
		"--plain", "--no-legend", "--full", "--no-pager",
	})
	if err != nil {
		return fmt.Errorf("list active user scopes: %w", err)
	}
	var units []string
	seenUnits := make(map[string]struct{})
	for _, unit := range parseActiveAppScopeUnits(unitsOutput) {
		_, matched := processScopes[unit]
		for _, appID := range escaped {
			if appScopeUnitMatchesAppID(appID, unit) {
				matched = true
				break
			}
		}
		if !matched {
			for _, pid := range pids {
				if appScopeUnitMatchesWindowPID(pid, unit) {
					matched = true
					break
				}
			}
		}
		if matched {
			if _, exists := seenUnits[unit]; !exists {
				seenUnits[unit] = struct{}{}
				units = append(units, unit)
			}
		}
	}
	if len(units) == 0 {
		if len(processCgroupErrors) > 0 {
			return fmt.Errorf("no active app scope found for current Niri windows: %w", errors.Join(processCgroupErrors...))
		}
		return errors.New("no active app scope found for current Niri windows")
	}
	slices.Sort(units)

	for _, unit := range units {
		output, err := r.runArgvOutput([]string{
			"systemctl", "--user", "show", "--no-pager",
			"--property=Id,LoadState,ActiveState,KillMode,ControlGroup", unit,
		})
		if err != nil {
			return fmt.Errorf("inspect Niri app scope %q: %w", unit, err)
		}
		properties := parseNiriAppScopeProperties(output)
		if err := validateNiriAppScopeProperties(os.Getuid(), unit, properties); err != nil {
			return err
		}
	}

	var stopErrors []error
	for _, unit := range units {
		if r.forceQuitRunningAppScope == nil {
			return errors.New("app scope force-quit command is unavailable")
		}
		if err := r.forceQuitRunningAppScope(unit); err != nil {
			stopErrors = append(stopErrors, fmt.Errorf("force quit app scope %q: %w", unit, err))
		}
	}
	return errors.Join(stopErrors...)
}

func forceQuitNiriAppScopeDefault(unit string) error {
	if testing.Testing() {
		return errors.New("refusing to force-quit a user app scope from a test binary")
	}
	path, err := exec.LookPath("systemctl")
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, path, "--user", "kill", "--signal=SIGKILL", "--kill-whom=all", unit)
	output, err := cmd.CombinedOutput()
	if err != nil {
		if message := strings.TrimSpace(string(output)); message != "" {
			return fmt.Errorf("%w: %s", err, message)
		}
		return err
	}
	return nil
}

func windowCount(n int) string {
	if n == 1 {
		return "1 window"
	}
	return fmt.Sprintf("%d windows", n)
}

func workspaceMoveTargetExists(snapshot niri.Snapshot, windowID, workspaceID uint64) bool {
	if windowID == 0 || workspaceID == 0 {
		return false
	}
	windowFound, workspaceFound := false, false
	for _, window := range snapshot.Windows {
		if window.ID == windowID {
			windowFound = true
			if window.HasWorkspace && window.WorkspaceID == workspaceID {
				return false
			}
			break
		}
	}
	for _, workspace := range snapshot.Workspaces {
		if workspace.ID == workspaceID {
			workspaceFound = true
			break
		}
	}
	return windowFound && workspaceFound
}

const (
	runningAppTile     = 24
	runningAppIconSize = 18
	runningAppIconPad  = (runningAppTile - runningAppIconSize) / 2
	runningAppGap      = 4
	runningAppPad      = 8
	runningAppPrefix   = "running-app:"
)

func refreshRunningApps(cap, row *ui.Node, v barView, pad int) bool {
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
	cap.Padding = pad
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

func (r *Registry) attachRunningIconsAtLocked(scale120 int) {
	if r.trayIcons == nil {
		return
	}
	size := runningAppIconPixelSize(scale120)
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

func (r *Registry) applyRunningIconsLocked() []uint32 {
	changed := make([]uint32, 0, len(r.bars))
	for global, bar := range r.bars {
		r.attachRunningIconsAtLocked(bar.scale120())
		if bar.apply(r.viewLocked(bar.connector())) {
			changed = append(changed, global)
		}
	}
	return changed
}

func (r *Registry) reprojectRunningApps() {
	r.mu.Lock()
	changed := r.applyRunningIconsLocked()
	r.mu.Unlock()
	r.publish(changed)
}

func runningAppKey(action string) (string, bool) {
	return strings.CutPrefix(action, runningAppPrefix)
}

func runningSlotByKey(slots []runningAppSlot, key string) (runningAppSlot, bool) {
	for _, slot := range slots {
		if slot.Key == key {
			return slot, true
		}
	}
	return runningAppSlot{}, false
}

func currentRunningWindowValue(slots []runningAppSlot, key string, id uint64) (niri.Window, bool) {
	if id == 0 {
		return niri.Window{}, false
	}
	for _, slot := range slots {
		if slot.Key != key {
			continue
		}
		for _, window := range slot.Members {
			if window.ID == id {
				return window, true
			}
		}
	}
	return niri.Window{}, false
}

func currentRunningWindow(slots []runningAppSlot, key string, id uint64) bool {
	_, ok := currentRunningWindowValue(slots, key, id)
	return ok
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
		if err := r.runningMenu.openLocked(output, slot, anchor); err != nil {
			log.Print("running-app menu: ", err)
		}
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

func (r *Registry) sendNiriResultLocked(body any, complete func(error)) {
	if r.niriSend != nil {
		err := r.niriSend(body)
		go complete(err)
		return
	}
	socket := os.Getenv("NIRI_SOCKET")
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		complete(niri.Action(ctx, socket, body))
	}()
}

func (r *Registry) queryNiriWindows() ([]niri.Window, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	return niri.GetWindows(ctx, os.Getenv("NIRI_SOCKET"))
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
