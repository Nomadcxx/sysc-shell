package shell

import (
	"bufio"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/Nomadcxx/sysc-shell/internal/config"
	"github.com/Nomadcxx/sysc-shell/internal/platform/niri"
	"github.com/Nomadcxx/sysc-shell/internal/platform/wayland"
	"github.com/Nomadcxx/sysc-shell/internal/render"
	"github.com/Nomadcxx/sysc-shell/internal/ui"
)

func TestRunningAppsMenuWindowRowsExposeAccessibleNames(t *testing.T) {
	t.Parallel()
	reg, _, host := openRunningAppsWindowMenuSnapshot(t, niri.Snapshot{
		Windows: []niri.Window{
			{ID: 80, AppID: "steam", Title: "Store", FocusTimestamp: 10},
			{ID: 81, AppID: "steam", Title: "Library full accessible title", Focused: true, FocusTimestamp: 20, WorkspaceID: 7, HasWorkspace: true},
		},
		Workspaces: []niri.Workspace{{ID: 7, Index: 2, Name: "Build"}},
	})

	row := findAction(host.root, "running-app-window:81")
	if row == nil {
		t.Fatal("MRU window row missing")
	}
	if row.Name != "Library full accessible title — Build" || row.Role != "menuitem" || !row.Focusable {
		t.Fatalf("window row accessibility = %+v", row)
	}
	if len(row.Children) != 1 || !strings.HasPrefix(row.Children[0].Text, "● ") {
		t.Fatalf("focused window marker = %+v", row.Children)
	}
	close := findAction(host.root, "running-app-close:81")
	if close == nil {
		t.Fatal("window close control missing")
	}
	if close.Name != "Close Library full accessible title — Build" || close.Role != "button" || !close.Focusable {
		t.Fatalf("close accessibility = %+v", close)
	}
	if close.Bounds.W <= 0 || close.Bounds.H <= 0 || close.Action == row.Action {
		t.Fatalf("close target is not independently actionable: row=%+v close=%+v", row, close)
	}
	if !host.open_ || len(reg.running) != 1 {
		t.Fatal("opening the menu changed live state")
	}
}

func TestRunningAppsMenuTogglesSelectedWindowAndWaitsForSnapshot(t *testing.T) {
	reg, _, host := openRunningAppsWindowMenu(t, []niri.Window{
		{ID: 80, AppID: "steam", Title: "Older", FocusTimestamp: 10},
		{ID: 81, AppID: "steam", Title: "Newest", Focused: true, FocusTimestamp: 20},
	})
	var sent []any
	reg.niriSend = func(action any) error { sent = append(sent, action); return nil }

	toggle := findAction(host.root, "running-app-floating:80")
	if toggle == nil || toggle.Name != "Move Older to floating" {
		t.Fatalf("tiled window action = %+v", toggle)
	}
	host.choose(host.hitRows[toggle.Action])
	if len(sent) != 1 {
		t.Fatalf("Niri actions = %v, want one toggle", sent)
	}
	if got, ok := sent[0].(niri.ToggleWindowFloating); !ok || got.ID != 80 {
		t.Fatalf("floating action = %#v, want ToggleWindowFloating{ID: 80}", sent[0])
	}
	if !host.open_ || findAction(host.root, "running-app-floating:80") != nil {
		t.Fatal("pending toggle closed the menu or left the same toggle available")
	}
	reg.mu.Lock()
	host.toggleFloatingLocked(80)
	reg.mu.Unlock()
	if len(sent) != 1 {
		t.Fatalf("duplicate pending toggle sent %d Niri actions, want one", len(sent))
	}
	if reg.niriSnapshot.Windows[0].Floating || !reg.niriSnapshot.Windows[1].Focused {
		t.Fatalf("shell reported state before confirmation or changed focus: %+v", reg.niriSnapshot.Windows)
	}

	reg.UpdateNiri(niri.Snapshot{Windows: []niri.Window{
		{ID: 80, AppID: "steam", Title: "Older", FocusTimestamp: 10, Floating: true},
		{ID: 81, AppID: "steam", Title: "Newest", Focused: true, FocusTimestamp: 20},
	}})
	toggle = findAction(host.root, "running-app-floating:80")
	if toggle == nil || toggle.Name != "Move Older to tiling" {
		t.Fatalf("confirmed floating state action = %+v", toggle)
	}
	host.choose(host.hitRows[toggle.Action])
	if len(sent) != 2 {
		t.Fatalf("Niri actions = %v, want a second toggle after confirmation", sent)
	}
	if got, ok := sent[1].(niri.ToggleWindowFloating); !ok || got.ID != 80 {
		t.Fatalf("tiling action = %#v, want ToggleWindowFloating{ID: 80}", sent[1])
	}
	reg.UpdateNiri(niri.Snapshot{Windows: []niri.Window{
		{ID: 80, AppID: "steam", Title: "Older", FocusTimestamp: 10},
		{ID: 81, AppID: "steam", Title: "Newest", Focused: true, FocusTimestamp: 20},
	}})
	toggle = findAction(host.root, "running-app-floating:80")
	if toggle == nil || toggle.Name != "Move Older to floating" || !host.open_ {
		t.Fatalf("confirmed tiled state action = %+v, menu open = %t", toggle, host.open_)
	}

	focused := findAction(host.root, "running-app-floating:81")
	if focused == nil {
		t.Fatal("focused window placement action missing")
	}
	host.choose(host.hitRows[focused.Action])
	if len(sent) != 3 {
		t.Fatalf("Niri actions = %v, want focused window toggle", sent)
	}
	if got, ok := sent[2].(niri.ToggleWindowFloating); !ok || got.ID != 81 {
		t.Fatalf("focused action = %#v, want ToggleWindowFloating{ID: 81}", sent[2])
	}
}

func TestRunningAppsMenuKeepsToggledWindowSelectedWhilePending(t *testing.T) {
	reg, _, host := openRunningAppsWindowMenu(t, []niri.Window{
		{ID: 80, AppID: "steam", Title: "Older", FocusTimestamp: 10},
		{ID: 81, AppID: "steam", Title: "Newest", FocusTimestamp: 20},
	})
	reg.niriSend = func(any) error { return nil }
	for range 3 {
		if !host.handle(wayland.Event{Kind: wayland.EventKeyPress, Key: keyDown}) {
			t.Fatal("Down ignored")
		}
	}
	option, ok := host.optionRow(host.menu.cursor)
	if !ok {
		t.Fatal("selected option has no menu row")
	}
	if row := host.rows[option]; row.Kind != runningAppMenuFloating || row.WindowID != 80 {
		t.Fatalf("selected row = %+v, want the placement action for window 80", row)
	}
	if !host.handle(wayland.Event{Kind: wayland.EventKeyPress, Key: keyEnter}) {
		t.Fatal("Enter ignored")
	}
	if got := selectedRunningAppWindow(host); got != 80 {
		t.Fatalf("selected window while toggle is pending = %d, want target window 80", got)
	}
}

func TestRunningAppsFloatingToggleIgnoresClosedWindow(t *testing.T) {
	reg, _, host := openRunningAppsWindowMenu(t, []niri.Window{
		{ID: 80, AppID: "steam", Title: "Closed"},
		{ID: 81, AppID: "steam", Title: "Still open", Focused: true},
	})
	var sent []any
	reg.niriSend = func(action any) error { sent = append(sent, action); return nil }
	reg.UpdateNiri(niri.Snapshot{Windows: []niri.Window{
		{ID: 81, AppID: "steam", Title: "Still open", Focused: true},
	}})

	reg.mu.Lock()
	host.toggleFloatingLocked(80)
	_, pending := host.floatPending[80]
	reg.mu.Unlock()
	if len(sent) != 0 || pending {
		t.Fatalf("closed window toggle sent %v and left pending=%t", sent, pending)
	}
}

func TestRunningAppsFloatingToggleUnknownOutcomeWaitsForSnapshot(t *testing.T) {
	t.Setenv("NIRI_SOCKET", filepath.Join(t.TempDir(), "missing.sock"))
	reg, _, host := openRunningAppsWindowMenu(t, []niri.Window{
		{ID: 80, AppID: "steam", Title: "Older", FocusTimestamp: 10},
		{ID: 81, AppID: "steam", Title: "Newest", Focused: true, FocusTimestamp: 20},
	})
	const generation = 1
	reg.mu.Lock()
	host.floatPending = map[uint64]runningAppFloatingRequest{
		80: {target: true, generation: generation},
	}
	reg.mu.Unlock()
	host.finishFloatingToggle(80, generation, errors.New("connection reset after send"))

	reg.mu.Lock()
	_, pending := host.floatPending[80]
	visible := findAction(host.root, "running-app-floating:80") != nil
	reg.mu.Unlock()
	if !pending || visible {
		t.Fatalf("ambiguous outcome state: pending=%t control-visible=%t", pending, visible)
	}

	reg.UpdateNiri(niri.Snapshot{Windows: []niri.Window{
		{ID: 80, AppID: "steam", Title: "Older", FocusTimestamp: 10},
		{ID: 81, AppID: "steam", Title: "Newest", FocusTimestamp: 20},
	}})
	reg.mu.Lock()
	_, pending = host.floatPending[80]
	visible = findAction(host.root, "running-app-floating:80") != nil
	reg.mu.Unlock()
	if !pending || visible {
		t.Fatalf("unchanged snapshot resolved an ambiguous toggle: pending=%t control-visible=%t", pending, visible)
	}

	reg.UpdateNiri(niri.Snapshot{Windows: []niri.Window{
		{ID: 80, AppID: "steam", Title: "Older", FocusTimestamp: 10, Floating: true},
		{ID: 81, AppID: "steam", Title: "Newest", Focused: true, FocusTimestamp: 20},
	}})
	reg.mu.Lock()
	_, pending = host.floatPending[80]
	toggle := findAction(host.root, "running-app-floating:80")
	name := ""
	if toggle != nil {
		name = toggle.Name
	}
	reg.mu.Unlock()
	if pending || name != "Move Older to tiling" {
		t.Fatalf("confirmed placement did not resolve the toggle: pending=%t action=%q", pending, name)
	}
}

func TestRunningAppsFloatingToggleAmbiguousFailureRefreshesWindowState(t *testing.T) {
	socket := filepath.Join(t.TempDir(), "niri.sock")
	listener, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = listener.Close() })
	t.Setenv("NIRI_SOCKET", socket)
	request := make(chan string, 1)
	go func() {
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		line, err := bufio.NewReader(conn).ReadString('\n')
		if err != nil {
			request <- "read error: " + err.Error()
			return
		}
		request <- strings.TrimSpace(line)
		_, _ = fmt.Fprintln(conn, `{"Ok":{"Windows":[{"id":80,"title":"Older","app_id":"steam","is_floating":false}]}}`)
	}()

	reg, _, host := openRunningAppsWindowMenu(t, []niri.Window{
		{ID: 80, AppID: "steam", Title: "Older", FocusTimestamp: 10},
		{ID: 81, AppID: "steam", Title: "Newest", Focused: true, FocusTimestamp: 20},
	})
	reg.niriSend = func(any) error {
		return errors.New("connection reset after send")
	}
	toggle := findAction(host.root, "running-app-floating:80")
	if toggle == nil {
		t.Fatal("floating action is missing")
	}
	host.choose(host.hitRows[toggle.Action])

	select {
	case got := <-request:
		if got != `"Windows"` {
			t.Fatalf("recovery request = %q, want %q", got, `"Windows"`)
		}
	case <-time.After(time.Second):
		t.Fatal("ambiguous toggle did not request fresh Niri window state")
	}
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		reg.mu.Lock()
		_, pending := host.floatPending[80]
		reg.mu.Unlock()
		if !pending {
			break
		}
		time.Sleep(time.Millisecond)
	}
	reg.mu.Lock()
	_, pending := host.floatPending[80]
	toggle = findAction(host.root, "running-app-floating:80")
	reg.mu.Unlock()
	if pending || toggle == nil || toggle.Name != "Move Older to floating" {
		t.Fatalf("fresh unchanged state did not restore the action: pending=%t toggle=%+v", pending, toggle)
	}
}

func TestRunningAppsFloatingToggleNotSentFailureKeepsControlAvailable(t *testing.T) {
	reg, _, host := openRunningAppsWindowMenu(t, []niri.Window{
		{ID: 80, AppID: "steam", Title: "Older", FocusTimestamp: 10},
		{ID: 81, AppID: "steam", Title: "Newest", Focused: true, FocusTimestamp: 20},
	})
	reg.niriSend = func(any) error {
		return &niri.ActionNotSentError{Cause: errors.New("connection refused")}
	}
	toggle := findAction(host.root, "running-app-floating:80")
	if toggle == nil {
		t.Fatal("floating control missing before request")
	}
	host.choose(host.hitRows[toggle.Action])

	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		reg.mu.Lock()
		_, pending := host.floatPending[80]
		reg.mu.Unlock()
		if !pending {
			break
		}
		time.Sleep(time.Millisecond)
	}
	reg.mu.Lock()
	_, pending := host.floatPending[80]
	_, unavailable := host.floatUnavailable[80]
	visible := findAction(host.root, "running-app-floating:80") != nil
	reg.mu.Unlock()
	if pending || unavailable || !visible {
		t.Fatalf("unsent failure state: pending=%t unavailable=%t control-visible=%t", pending, unavailable, visible)
	}
}

func TestRunningAppsFloatingToggleRefusalDisablesOnlyThatControlUntilPlacementChanges(t *testing.T) {
	reg, _, host := openRunningAppsWindowMenu(t, []niri.Window{
		{ID: 80, AppID: "steam", Title: "Older", FocusTimestamp: 10},
		{ID: 81, AppID: "steam", Title: "Newest", Focused: true, FocusTimestamp: 20},
	})
	reg.niriSend = func(any) error { return &niri.ActionRefusalError{Reason: "unsupported action"} }
	toggle := findAction(host.root, "running-app-floating:80")
	if toggle == nil {
		t.Fatal("floating control missing before request")
	}
	host.choose(host.hitRows[toggle.Action])

	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		reg.mu.Lock()
		_, unavailable := host.floatUnavailable[80]
		reg.mu.Unlock()
		if unavailable {
			break
		}
		time.Sleep(time.Millisecond)
	}
	reg.mu.Lock()
	_, unavailable := host.floatUnavailable[80]
	open := host.open_
	controlVisible := findAction(host.root, "running-app-floating:80") != nil
	otherControlsVisible := findAction(host.root, "running-app-floating:81") != nil && findAction(host.root, "running-app-window:80") != nil
	reg.mu.Unlock()
	if !open || !unavailable {
		t.Fatal("refused toggle did not leave isolated unavailable feedback in the open menu")
	}
	if controlVisible {
		t.Fatal("refused floating control remained available")
	}
	if !otherControlsVisible {
		t.Fatal("one unavailable toggle disabled unrelated menu controls")
	}

	reg.UpdateNiri(niri.Snapshot{Windows: []niri.Window{
		{ID: 80, AppID: "steam", Title: "Older", FocusTimestamp: 10, Floating: true},
		{ID: 81, AppID: "steam", Title: "Newest", Focused: true, FocusTimestamp: 20},
	}})
	reg.mu.Lock()
	toggle = findAction(host.root, "running-app-floating:80")
	var name string
	if toggle != nil {
		name = toggle.Name
	}
	reg.mu.Unlock()
	if name != "Move Older to tiling" {
		t.Fatalf("changed placement did not restore the confirmed toggle: %q", name)
	}
}

func TestRunningAppsMenuForceQuitVisibleBeforeLongActionLists(t *testing.T) {
	t.Parallel()
	reg, bar := steamMenuReg(t)
	actions := make([]runningAppAction, 9)
	for i := range actions {
		actions[i] = runningAppAction{ID: fmt.Sprintf("Action%d", i), Name: fmt.Sprintf("Action %d", i)}
	}
	reg.runningIndex = []runningAppEntry{{ID: "steam", Actions: actions}}
	reg.UpdateNiri(niri.Snapshot{
		Windows: []niri.Window{{ID: 80, AppID: "steam", Title: "Steam", WorkspaceID: 1, HasWorkspace: true}},
		Workspaces: []niri.Workspace{
			{ID: 1, Index: 1}, {ID: 2, Index: 2}, {ID: 3, Index: 3}, {ID: 4, Index: 4}, {ID: 5, Index: 5},
		},
	})
	if !bar.onAction("running-app:steam", buttonRight) {
		t.Fatal("right click ignored")
	}
	if findAction(reg.runningMenu.root, "running-app-force-quit") == nil {
		t.Fatal("app-level force quit is hidden below the initial six-row viewport")
	}
}

func TestRunningAppsMenuStartsOnMRUWindow(t *testing.T) {
	t.Parallel()
	_, _, host := openRunningAppsWindowMenu(t, []niri.Window{
		{ID: 80, AppID: "steam", Title: "Older", FocusTimestamp: 10},
		{ID: 81, AppID: "steam", Title: "Newest", FocusTimestamp: 20},
	})
	if len(host.menu.options) < 2 || host.menu.options[host.menu.cursor] != "Newest" {
		t.Fatalf("initial menu options/cursor = %v/%d, want MRU row first", host.menu.options, host.menu.cursor)
	}
	if host.menu.cursor != 1 {
		t.Fatalf("initial cursor = %d, want the MRU window after the app action", host.menu.cursor)
	}
}

func TestRunningAppsMenuCloseControlClosesOnlyItsWindow(t *testing.T) {
	t.Parallel()
	reg, _, host := openRunningAppsWindowMenu(t, []niri.Window{
		{ID: 80, AppID: "steam", Title: "Store", FocusTimestamp: 10},
		{ID: 81, AppID: "steam", Title: "Library", Focused: true, FocusTimestamp: 20},
	})
	var sent []any
	reg.niriSend = func(action any) error { sent = append(sent, action); return nil }
	close := findAction(host.root, "running-app-close:81")
	if close == nil {
		t.Fatal("window close control missing")
	}
	x := float64(close.Bounds.X + close.Bounds.W/2)
	y := float64(close.Bounds.Y + close.Bounds.H/2)
	if !host.handle(wayland.Event{Kind: wayland.EventPointerPress, X: x, Y: y}) {
		t.Fatal("close press ignored")
	}
	if !host.handle(wayland.Event{Kind: wayland.EventPointerRelease, X: x, Y: y}) {
		t.Fatal("close release ignored")
	}
	if len(sent) != 1 {
		t.Fatalf("Niri actions = %v, want one CloseWindow", sent)
	}
	if got, ok := sent[0].(niri.CloseWindow); !ok || got.ID != 81 {
		t.Fatalf("close action = %#v, want CloseWindow 81", sent[0])
	}
}

func TestRunningAppsMenuForceQuitRequiresConfirmationAndCancelDoesNothing(t *testing.T) {
	t.Parallel()
	reg, _, host := openRunningAppsWindowMenu(t, []niri.Window{
		{ID: 80, AppID: "steam", Title: "Store", FocusTimestamp: 10},
	})
	var calls, stopped int
	installRunningAppScopeTestCommands(reg, func(string) error {
		stopped++
		return nil
	})
	reg.runArgvOutput = func(argv []string) (string, error) {
		calls++
		return "", errors.New("should not run before confirmation")
	}
	forceQuit := findAction(host.root, "running-app-force-quit")
	if forceQuit == nil {
		t.Fatal("app-level force-quit row missing")
	}
	index := host.hitRows[forceQuit.Action]
	host.choose(index)
	if !host.open_ || host.forceQuitState != runningAppForceQuitConfirming {
		t.Fatal("force-quit row did not open a confirmation")
	}
	if calls != 0 || stopped != 0 {
		t.Fatalf("force quit ran before confirmation: commands=%d stops=%d", calls, stopped)
	}
	if findAction(host.root, "running-app-force-quit-confirm") == nil {
		t.Fatal("confirmation action missing")
	}
	cancel := -1
	for i, row := range host.rows {
		if row.Kind == runningAppMenuForceQuitCancel {
			cancel = i
			break
		}
	}
	if cancel < 0 {
		t.Fatal("confirmation cancel action missing")
	}
	host.choose(cancel)
	if host.open_ || calls != 0 || stopped != 0 {
		t.Fatalf("cancel state: open=%t commands=%d stops=%d", host.open_, calls, stopped)
	}
}

func TestRunningAppsMenuForceQuitEscapeDoesNotRunProcessWork(t *testing.T) {
	t.Parallel()
	reg, _, host := openRunningAppsWindowMenu(t, []niri.Window{
		{ID: 80, AppID: "steam", Title: "Store", FocusTimestamp: 10},
	})
	var calls, stopped int
	installRunningAppScopeTestCommands(reg, func(string) error {
		stopped++
		return nil
	})
	reg.runArgvOutput = func([]string) (string, error) {
		calls++
		return "", errors.New("should not run before confirmation")
	}
	forceQuit := findAction(host.root, "running-app-force-quit")
	if forceQuit == nil {
		t.Fatal("app-level force-quit row missing")
	}
	host.choose(host.hitRows[forceQuit.Action])
	if !host.handle(wayland.Event{Kind: wayland.EventKeyPress, Key: keyEsc}) {
		t.Fatal("Escape ignored in force-quit confirmation")
	}
	if host.open_ || calls != 0 || stopped != 0 {
		t.Fatalf("Escape state: open=%t commands=%d stops=%d", host.open_, calls, stopped)
	}
}

func TestRunningAppsMenuForceQuitSuccessClosesMenu(t *testing.T) {
	t.Parallel()
	reg, _, host := openRunningAppsWindowMenu(t, []niri.Window{
		{ID: 80, AppID: "steam", Title: "Store", FocusTimestamp: 10},
	})
	stopped := make(chan string, 1)
	installRunningAppScopeTestCommands(reg, func(unit string) error {
		stopped <- unit
		return nil
	})
	forceQuit := findAction(host.root, "running-app-force-quit")
	if forceQuit == nil {
		t.Fatal("app-level force-quit row missing")
	}
	host.choose(host.hitRows[forceQuit.Action])
	confirm := findAction(host.root, "running-app-force-quit-confirm")
	if confirm == nil {
		t.Fatal("confirmation action missing")
	}
	host.choose(host.hitRows[confirm.Action])
	select {
	case unit := <-stopped:
		if unit != "app-niri-steam-545355.scope" {
			t.Fatalf("stopped unit = %q, want Steam Niri scope", unit)
		}
	case <-time.After(time.Second):
		t.Fatal("systemd stop was not started")
	}
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		reg.mu.Lock()
		open := host.open_
		reg.mu.Unlock()
		if !open {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("successful force quit did not close the menu")
}

func TestRunningAppForceQuitFailureMessageIsBounded(t *testing.T) {
	t.Parallel()
	message := runningAppForceQuitErrorMessage(errors.New(strings.Repeat("x", 1000)))
	if utf8.RuneCountInString(message) > 180 {
		t.Fatalf("force-quit error message has %d runes, want at most 180", utf8.RuneCountInString(message))
	}
}

func TestRunningAppsMenuForceQuitMapsSteamOnlyGameToSteamScope(t *testing.T) {
	t.Parallel()
	reg, _, host := openRunningAppsWindowMenu(t, []niri.Window{
		{ID: 82, AppID: "steam_app_123", Title: "Game", FocusTimestamp: 10},
	})
	if findAction(host.root, "running-app-force-quit") == nil {
		t.Fatal("app-level force-quit row missing when only a folded Steam game window exists")
	}
	if got := runningAppScopeAppIDs(reg.running[0]); len(got) != 1 || got[0] != "steam" {
		t.Fatalf("Steam game scope IDs = %v, want [steam]", got)
	}
}

func TestRunningAppsMenuForceQuitRunsOffLockAndReportsFailure(t *testing.T) {
	t.Parallel()
	reg, _, host := openRunningAppsWindowMenu(t, []niri.Window{
		{ID: 80, AppID: "steam", Title: "Store", FocusTimestamp: 10},
	})
	started := make(chan string, 1)
	release := make(chan struct{})
	lockAvailable := make(chan bool, 1)
	installRunningAppScopeTestCommands(reg, func(unit string) error {
		if reg.mu.TryLock() {
			reg.mu.Unlock()
			lockAvailable <- true
		} else {
			lockAvailable <- false
		}
		started <- unit
		<-release
		return errors.New("stop denied")
	})
	var sent []any
	reg.niriSend = func(action any) error {
		sent = append(sent, action)
		return nil
	}
	forceQuit := findAction(host.root, "running-app-force-quit")
	if forceQuit == nil {
		t.Fatal("app-level force-quit row missing")
	}
	host.choose(host.hitRows[forceQuit.Action])
	confirm := findAction(host.root, "running-app-force-quit-confirm")
	if confirm == nil {
		t.Fatal("confirmation action missing")
	}
	confirmIndex := host.hitRows[confirm.Action]
	returned := make(chan struct{})
	go func() {
		host.choose(confirmIndex)
		close(returned)
	}()
	select {
	case <-returned:
	case <-time.After(time.Second):
		close(release)
		t.Fatal("force-quit choice blocked while starting systemd work")
	}
	select {
	case unit := <-started:
		if unit != "app-niri-steam-545355.scope" {
			close(release)
			t.Fatalf("stopped unit = %q, want Steam Niri scope", unit)
		}
	case <-time.After(time.Second):
		close(release)
		t.Fatal("systemd stop was not started")
	}
	if !<-lockAvailable {
		close(release)
		t.Fatal("systemd stop ran while Registry.mu was held")
	}
	reg.mu.Lock()
	pending := host.forceQuitState == runningAppForceQuitPending
	reg.mu.Unlock()
	if !pending {
		close(release)
		t.Fatal("pending state was not shown while systemd stopped the scope")
	}
	close(release)
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		reg.mu.Lock()
		state := host.forceQuitState
		root := host.root
		reg.mu.Unlock()
		if state == runningAppForceQuitFailed {
			if findText(root, "Force quit failed") == nil {
				t.Fatal("failed force quit was not reported in the menu")
			}
			if len(sent) != 0 {
				t.Fatalf("failed force quit sent Niri actions: %v", sent)
			}
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("systemd force-quit failure did not reach the menu")
}

func TestRunningAppsMenuKeepsFailureSurfaceWhenNiriRemovesAppDuringForceQuit(t *testing.T) {
	t.Parallel()
	reg, bar := steamMenuReg(t)
	reg.runningIndex = []runningAppEntry{{ID: "discord"}}
	reg.UpdateNiri(niri.Snapshot{Windows: []niri.Window{{ID: 80, AppID: "discord", Title: "Discord", Pid: 545355}}})
	if !bar.onAction("running-app:discord", buttonRight) {
		t.Fatal("right click ignored")
	}
	host := reg.runningMenu
	reg.runArgvOutput = func(argv []string) (string, error) {
		switch {
		case argv[0] == "systemd-escape":
			return "discord\n", nil
		case argv[0] == "systemctl" && argv[2] == "list-units":
			return "app-discord-545355.scope loaded active running Discord\napp-niri-sh-545355.scope loaded active running Discord\n", nil
		case argv[0] == "systemctl" && argv[2] == "show":
			unit := argv[len(argv)-1]
			uid := os.Getuid()
			return fmt.Sprintf("Id=%s\nLoadState=loaded\nActiveState=active\nKillMode=control-group\nControlGroup=/user.slice/user-%d.slice/user@%d.service/app.slice/%s\n",
				unit, uid, uid, unit), nil
		default:
			return "", fmt.Errorf("unexpected command: %v", argv)
		}
	}
	stopped := make(chan string, 2)
	secondScopeStarted := make(chan struct{})
	releaseSecondScope := make(chan struct{})
	var releaseOnce sync.Once
	release := func() { releaseOnce.Do(func() { close(releaseSecondScope) }) }
	defer release()
	reg.forceQuitRunningAppScope = func(unit string) error {
		stopped <- unit
		if unit == "app-niri-sh-545355.scope" {
			close(secondScopeStarted)
			<-releaseSecondScope
			return errors.New("stop denied")
		}
		return nil
	}
	forceQuit := findAction(host.root, "running-app-force-quit")
	if forceQuit == nil {
		t.Fatal("app-level force-quit row missing")
	}
	host.choose(host.hitRows[forceQuit.Action])
	confirm := findAction(host.root, "running-app-force-quit-confirm")
	if confirm == nil {
		t.Fatal("confirmation action missing")
	}
	host.choose(host.hitRows[confirm.Action])
	for _, want := range []string{"app-discord-545355.scope", "app-niri-sh-545355.scope"} {
		select {
		case got := <-stopped:
			if got != want {
				t.Fatalf("stopped scope = %q, want %q", got, want)
			}
		case <-time.After(time.Second):
			t.Fatalf("scope %q was not stopped", want)
		}
	}
	select {
	case <-secondScopeStarted:
	case <-time.After(time.Second):
		t.Fatal("second scope did not block before failure")
	}

	reg.UpdateNiri(niri.Snapshot{})
	reg.mu.Lock()
	open := host.open_
	state := host.forceQuitState
	reg.mu.Unlock()
	if !open || state != runningAppForceQuitPending {
		t.Fatalf("menu closed while force quit was pending and its app slot disappeared: open=%t state=%d", open, state)
	}

	release()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		reg.mu.Lock()
		open, state, root := host.open_, host.forceQuitState, host.root
		reg.mu.Unlock()
		if state == runningAppForceQuitFailed {
			if !open || findText(root, "Force quit failed") == nil {
				t.Fatal("force-quit failure was not left visible in the open menu")
			}
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("second-scope force-quit failure did not reach the menu")
}

func TestRunningAppsMenuSecondaryClickDoesNotActivateRow(t *testing.T) {
	t.Parallel()
	reg, _, host := openRunningAppsWindowMenu(t, []niri.Window{{
		ID: 80, AppID: "steam", Title: "Window", FocusTimestamp: 10,
	}})
	var sent []any
	reg.niriSend = func(action any) error { sent = append(sent, action); return nil }
	row := findAction(host.root, "running-app-window:80")
	if row == nil {
		t.Fatal("window row missing")
	}
	x, y := float64(row.Bounds.X+row.Bounds.W/2), float64(row.Bounds.Y+row.Bounds.H/2)
	host.handle(wayland.Event{Kind: wayland.EventPointerPress, Button: buttonRight, X: x, Y: y})
	if host.handle(wayland.Event{Kind: wayland.EventPointerRelease, Button: buttonRight, X: x, Y: y}) {
		t.Fatal("secondary click activated a menu row")
	}
	if len(sent) != 0 || !host.open_ {
		t.Fatalf("secondary click sent %v or closed the menu", sent)
	}
}

func TestRunningAppsMenuDeleteAndXCloseTheSelectedWindow(t *testing.T) {
	for _, tc := range []struct {
		name string
		key  uint32
		sym  uint32
	}{
		{name: "Delete", key: keyDelete},
		{name: "x", key: 45, sym: uint32('x')},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			reg, _, host := openRunningAppsWindowMenu(t, []niri.Window{
				{ID: 80, AppID: "steam", Title: "Older", FocusTimestamp: 10},
				{ID: 81, AppID: "steam", Title: "Newest", Focused: true, FocusTimestamp: 20},
			})
			var sent []any
			reg.niriSend = func(action any) error { sent = append(sent, action); return nil }
			if !host.handle(wayland.Event{Kind: wayland.EventKeyPress, Key: tc.key, Sym: tc.sym}) {
				t.Fatalf("%s ignored", tc.name)
			}
			if len(sent) != 1 {
				t.Fatalf("Niri actions = %v, want one CloseWindow", sent)
			}
			if got, ok := sent[0].(niri.CloseWindow); !ok || got.ID != 81 {
				t.Fatalf("close action = %#v, want CloseWindow 81", sent[0])
			}
		})
	}
}

func TestRunningAppsMenuEnterAndSpaceActivateWindowRows(t *testing.T) {
	for _, tc := range []struct {
		name       string
		keys       []uint32
		wantWindow uint64
	}{
		{name: "Enter selects MRU", keys: []uint32{keyEnter}, wantWindow: 81},
		{name: "Space selects after arrows", keys: []uint32{keyDown, keyDown, keySpace}, wantWindow: 80},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			reg, _, host := openRunningAppsWindowMenu(t, []niri.Window{
				{ID: 80, AppID: "steam", Title: "Older", FocusTimestamp: 10},
				{ID: 81, AppID: "steam", Title: "Newest", FocusTimestamp: 20},
			})
			var sent []any
			reg.niriSend = func(action any) error { sent = append(sent, action); return nil }
			for _, key := range tc.keys {
				if !host.handle(wayland.Event{Kind: wayland.EventKeyPress, Key: key}) {
					t.Fatalf("key %d was ignored", key)
				}
			}
			if len(sent) != 1 {
				t.Fatalf("Niri actions = %v, want one FocusWindow", sent)
			}
			if got, ok := sent[0].(niri.FocusWindow); !ok || got.ID != tc.wantWindow {
				t.Fatalf("focus action = %#v, want window %d", sent[0], tc.wantWindow)
			}
		})
	}
}

func TestRunningAppsMenuSnapshotRefreshPreservesWindowSelection(t *testing.T) {
	t.Parallel()
	reg, _, host := openRunningAppsWindowMenu(t, []niri.Window{
		{ID: 80, AppID: "steam", Title: "Older", FocusTimestamp: 10},
		{ID: 81, AppID: "steam", Title: "Newest", FocusTimestamp: 20},
	})
	var requests []wayland.AuxRequest
	host.request = func(request wayland.AuxRequest) { requests = append(requests, request) }
	for range 2 {
		if !host.handle(wayland.Event{Kind: wayland.EventKeyPress, Key: keyDown}) {
			t.Fatal("Down ignored")
		}
	}
	reg.UpdateNiri(niri.Snapshot{Windows: []niri.Window{
		{ID: 80, AppID: "steam", Title: "Older", FocusTimestamp: 10},
		{ID: 81, AppID: "steam", Title: "Newer", FocusTimestamp: 20},
		{ID: 82, AppID: "steam", Title: "Newest", FocusTimestamp: 30},
	}})
	if !host.open_ {
		t.Fatal("snapshot refresh closed the menu")
	}
	for _, request := range requests {
		if request.Open != nil {
			t.Fatalf("snapshot refresh reopened an existing surface: %+v", request)
		}
	}
	if got := selectedRunningAppWindow(host); got != 80 {
		t.Fatalf("selected window after snapshot = %d, want preserved ID 80", got)
	}
	if findAction(host.root, "running-app-window:82") == nil {
		t.Fatal("new MRU window missing from refreshed host tree")
	}
}

func TestRunningAppsMenuConfigureResizesExistingSurface(t *testing.T) {
	t.Parallel()
	_, _, host := openRunningAppsWindowMenu(t, []niri.Window{{ID: 80, AppID: "steam", Title: "Window"}})
	var requests []wayland.AuxRequest
	host.request = func(request wayland.AuxRequest) { requests = append(requests, request) }
	width, height := host.logicalW, host.logicalH
	if err := host.configure(width, height+1, host.scale120); err != nil {
		t.Fatal(err)
	}
	if !host.open_ {
		t.Fatal("configure mismatch closed the menu")
	}
	if len(requests) != 1 || requests[0].Open != nil || requests[0].Update == nil {
		t.Fatalf("configure correction = %+v, want one in-place update", requests)
	}
	if requests[0].Update.Height == nil || int(*requests[0].Update.Height) != host.logicalH {
		t.Fatalf("configure correction height = %+v, layout = %d", requests[0].Update.Height, host.logicalH)
	}
}

func TestRunningAppsMenuRethemeUpdatesExistingSurfaceInPlace(t *testing.T) {
	t.Parallel()
	reg, _, host := openRunningAppsWindowMenu(t, []niri.Window{{ID: 80, AppID: "steam", Title: "Window"}})
	var requests []wayland.AuxRequest
	host.request = func(request wayland.AuxRequest) { requests = append(requests, request) }
	next := reg.surfaceTheme()
	next.Metrics.IconButton += 24
	if !host.rethemeLocked(next, host.fontFamily) {
		t.Fatal("retheme failed")
	}
	if !host.open_ {
		t.Fatal("retheme closed the menu")
	}
	if len(requests) != 1 || requests[0].Open != nil || requests[0].Update == nil {
		t.Fatalf("retheme correction = %+v, want one in-place update", requests)
	}
	if requests[0].Update.Height == nil || int(*requests[0].Update.Height) != host.logicalH {
		t.Fatalf("retheme correction height = %+v, layout = %d", requests[0].Update.Height, host.logicalH)
	}
}

func TestRunningAppsMenuSnapshotRefreshFallsBackToMRUAndClosesMissingSlot(t *testing.T) {
	t.Parallel()
	reg, _, host := openRunningAppsWindowMenu(t, []niri.Window{
		{ID: 80, AppID: "steam", Title: "Older", FocusTimestamp: 10},
		{ID: 81, AppID: "steam", Title: "Newest", FocusTimestamp: 20},
	})
	for range 2 {
		if !host.handle(wayland.Event{Kind: wayland.EventKeyPress, Key: keyDown}) {
			t.Fatal("Down ignored")
		}
	}
	reg.UpdateNiri(niri.Snapshot{Windows: []niri.Window{
		{ID: 81, AppID: "steam", Title: "Newest", FocusTimestamp: 20},
		{ID: 82, AppID: "steam", Title: "New MRU", FocusTimestamp: 30},
	}})
	if got := selectedRunningAppWindow(host); got != 82 {
		t.Fatalf("selected window after removing cursor target = %d, want new MRU ID 82", got)
	}
	reg.UpdateNiri(niri.Snapshot{})
	if host.open_ {
		t.Fatal("menu stayed open after its app slot disappeared")
	}
}

func TestRunningAppsMenuFitsConfiguredLargeFonts(t *testing.T) {
	t.Parallel()
	reg, _, host := openRunningAppsWindowMenu(t, manySteamWindows(10))
	cfg := reg.cfg.Clone()
	cfg.Bar.FontSize = 80
	reg.mu.Lock()
	reg.retheThemeOpenSurfacesLocked(cfg, reg.tokens)
	reg.mu.Unlock()

	for i := 0; i < len(host.menu.options)-1; i++ {
		if !handleRunningAppMenuEvent(reg, host, wayland.Event{Kind: wayland.EventKeyPress, Key: keyDown}) {
			t.Fatal("Down ignored")
		}
	}
	if findAction(host.root, "running-app-force-quit") == nil {
		t.Fatal("app-level force quit disappeared while paging through the menu")
	}
	if visible := assertRunningAppMenuVisibleOptions(t, host); visible >= menuVisibleRows {
		t.Fatalf("large font retained %d visible rows, want fewer than %d", visible, menuVisibleRows)
	}
	assertLastRunningAppMenuRowFits(t, host, host.logicalH)
}

func selectedRunningAppWindow(host *runningAppMenuHost) uint64 {
	if host.menu == nil {
		return 0
	}
	row, ok := host.optionRow(host.menu.cursor)
	if !ok || host.rows[row].Kind != runningAppMenuWindow {
		return 0
	}
	return host.rows[row].WindowID
}

func TestRunningAppsMenuViewportScrollKeepsOriginalWindowMapping(t *testing.T) {
	t.Parallel()
	reg, _, host := openRunningAppsWindowMenu(t, manySteamWindows(10))
	assertRunningAppMenuVisibleOptions(t, host)
	for host.menu.cursor < len(host.menu.options)-1 {
		if !handleRunningAppMenuEvent(reg, host, wayland.Event{Kind: wayland.EventPointerAxis, AxisDiscrete: 1}) {
			t.Fatal("wheel down ignored")
		}
	}
	if host.menu.cursor != len(host.menu.options)-1 {
		t.Fatalf("wheel cursor = %d, want final option %d", host.menu.cursor, len(host.menu.options)-1)
	}
	assertRunningAppMenuVisibleOptions(t, host)
	for i := 0; i < len(host.menu.options) && findAction(host.root, "running-app-window:103") == nil; i++ {
		if !handleRunningAppMenuEvent(reg, host, wayland.Event{Kind: wayland.EventPointerAxis, AxisDiscrete: -1}) {
			t.Fatal("wheel up ignored")
		}
	}
	row := findAction(host.root, "running-app-window:103")
	if row == nil || row.Bounds.W <= 0 || row.Bounds.H <= 0 {
		t.Fatalf("scrolled window 103 is not visible: %+v", row)
	}
	var sent []any
	reg.niriSend = func(action any) error { sent = append(sent, action); return nil }
	x, y := float64(row.Bounds.X+row.Bounds.W/2), float64(row.Bounds.Y+row.Bounds.H/2)
	host.handle(wayland.Event{Kind: wayland.EventPointerPress, X: x, Y: y})
	if !host.handle(wayland.Event{Kind: wayland.EventPointerRelease, X: x, Y: y}) {
		t.Fatal("visible scrolled row click ignored")
	}
	if len(sent) != 1 {
		t.Fatalf("Niri actions = %v, want one FocusWindow", sent)
	}
	if got, ok := sent[0].(niri.FocusWindow); !ok || got.ID != 103 {
		t.Fatalf("focused action = %#v, want original window ID 103", sent[0])
	}

	reg, _, host = openRunningAppsWindowMenu(t, manySteamWindows(10))
	for host.menu.cursor < len(host.menu.options)-1 {
		if !handleRunningAppMenuEvent(reg, host, wayland.Event{Kind: wayland.EventKeyPress, Key: keyDown}) {
			t.Fatal("Down ignored")
		}
	}
	if host.menu.cursor != len(host.menu.options)-1 {
		t.Fatalf("keyboard cursor = %d, want final option %d", host.menu.cursor, len(host.menu.options)-1)
	}
}

func TestRunningAppsMenuScrollKeepsDynamicChromeWithinSurface(t *testing.T) {
	t.Parallel()
	windows := manySteamWindows(6)
	windows = append(windows, niri.Window{
		ID: 200, AppID: "steam_app_123", Title: "Game", FocusTimestamp: 20,
	})
	reg, _, host := openRunningAppsWindowMenu(t, windows)
	initialHeight := host.logicalH
	var updates []wayland.AuxRequest
	host.request = func(request wayland.AuxRequest) { updates = append(updates, request) }
	for host.menu.cursor < len(host.menu.options)-1 {
		if !handleRunningAppMenuEvent(reg, host, wayland.Event{Kind: wayland.EventKeyPress, Key: keyDown}) {
			t.Fatal("Down ignored")
		}
	}
	if findText(host.root, "Games") == nil {
		t.Fatal("Games heading is not visible at the end of the list")
	}
	if host.logicalH <= initialHeight {
		t.Fatalf("surface height stayed at %d despite extra viewport rows", initialHeight)
	}
	if len(updates) == 0 || updates[len(updates)-1].Update == nil || updates[len(updates)-1].Update.Height == nil {
		t.Fatalf("scrolled surface was not resized: %+v", updates)
	}
	if got := int(*updates[len(updates)-1].Update.Height); got != host.logicalH {
		t.Fatalf("resize height = %d, layout height = %d", got, host.logicalH)
	}
	assertLastRunningAppMenuRowFits(t, host, host.logicalH)
}

func TestRunningAppsMenuCloseControlRemainsTargetableAfterScroll(t *testing.T) {
	t.Parallel()
	reg, _, host := openRunningAppsWindowMenu(t, manySteamWindows(10))
	for i := 0; i < len(host.menu.options) && findAction(host.root, "running-app-close:103") == nil; i++ {
		if !handleRunningAppMenuEvent(reg, host, wayland.Event{Kind: wayland.EventPointerAxis, AxisDiscrete: 1}) {
			t.Fatal("wheel down ignored")
		}
	}
	assertRunningAppMenuVisibleOptions(t, host)
	close := findAction(host.root, "running-app-close:103")
	if close == nil {
		t.Fatal("scrolled window close control missing")
	}
	var sent []any
	reg.niriSend = func(action any) error { sent = append(sent, action); return nil }
	x, y := float64(close.Bounds.X+close.Bounds.W/2), float64(close.Bounds.Y+close.Bounds.H/2)
	host.handle(wayland.Event{Kind: wayland.EventPointerPress, X: x, Y: y})
	if !host.handle(wayland.Event{Kind: wayland.EventPointerRelease, X: x, Y: y}) {
		t.Fatal("scrolled close control release ignored")
	}
	if len(sent) != 1 {
		t.Fatalf("Niri actions = %v, want one CloseWindow", sent)
	}
	if got, ok := sent[0].(niri.CloseWindow); !ok || got.ID != 103 {
		t.Fatalf("close action = %#v, want CloseWindow 103", sent[0])
	}
}

func handleRunningAppMenuEvent(reg *Registry, host *runningAppMenuHost, event wayland.Event) bool {
	handled := host.handle(event)
	for {
		select {
		case <-reg.invalidations:
		default:
			return handled
		}
	}
}

func manySteamWindows(count int) []niri.Window {
	windows := make([]niri.Window, count)
	for i := range windows {
		windows[i] = niri.Window{ID: uint64(100 + i), AppID: "steam", Title: "Window", FocusTimestamp: int64(i + 1)}
	}
	return windows
}

func assertRunningAppMenuVisibleOptions(t *testing.T, host *runningAppMenuHost) int {
	t.Helper()
	count := 0
	var walk func(*ui.Node)
	walk = func(node *ui.Node) {
		if node == nil {
			return
		}
		if node.Role == "menuitem" && node.Focusable {
			count++
		}
		for _, child := range node.Children {
			walk(child)
		}
	}
	walk(host.root)
	if count > menuVisibleRows {
		t.Fatalf("visible menu items = %d, want at most %d", count, menuVisibleRows)
	}
	return count
}

func TestRunningAppsMenuGeometryUsesMeasuredFirstRequestAndTruncates(t *testing.T) {
	t.Parallel()
	title := strings.Repeat("界面 safety title ", 80)
	for _, tc := range []struct {
		name   string
		edge   string
		width  int
		height int
		wantW  int
	}{
		{name: "narrow output", edge: "top", width: 100, height: 700, wantW: 100},
		{name: "forty percent clamp", edge: "bottom", width: 800, height: 700, wantW: 320},
		{name: "wide output clamp", edge: "left", width: 1600, height: 900, wantW: 420},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			reg, _, host, anchor, requests := runningAppsMenuGeometryHarness(t, tc.edge, tc.width, tc.height, title)
			if err := openRunningAppsGeometryMenu(t, reg, host); err != nil {
				t.Fatal(err)
			}
			spec := openedRunningAppsMenuSpec(t, *requests)
			if int(spec.Width) != tc.wantW {
				t.Fatalf("first requested width = %d, want %d", spec.Width, tc.wantW)
			}
			if host.text == nil {
				t.Fatal("first surface was requested before initializing its text renderer")
			}
			row := findAction(host.root, "running-app-window:80")
			if row == nil || row.Name != title {
				t.Fatalf("accessible window name = %+v, want full title", row)
			}
			if len(row.Children) != 1 || row.Children[0].Text == title || !utf8.ValidString(row.Children[0].Text) {
				t.Fatalf("visible row text = %+v, want valid shortened text", row.Children)
			}
			_, measuredHeight := host.size()
			if int(spec.Height) != measuredHeight || measuredHeight > trayMenuMaxHeight || measuredHeight > tc.height {
				t.Fatalf("first requested height = %d, measured = %d, output = %d", spec.Height, measuredHeight, tc.height)
			}
			assertLastRunningAppMenuRowFits(t, host, int(spec.Height))
			want := trayMenuOnOutput(tc.edge, anchor, int(spec.Width), int(spec.Height), tc.width, tc.height)
			if spec.Anchor != want.anchor || spec.MarginLeft != want.marginLeft || spec.MarginTop != want.marginTop || spec.MarginRight != want.marginRight {
				t.Fatalf("first placement = (%d,%d,%d,%d), want (%d,%d,%d,%d)", spec.Anchor, spec.MarginLeft, spec.MarginTop, spec.MarginRight, want.anchor, want.marginLeft, want.marginTop, want.marginRight)
			}
		})
	}
}

func TestRunningAppsMenuGeometryFitsAtEveryScaleAndBarEdge(t *testing.T) {
	for _, edge := range []string{"top", "bottom", "left", "right"} {
		for _, scale := range []int{120, 150, 180, 240} {
			t.Run(edge+"/"+strconv.Itoa(scale), func(t *testing.T) {
				t.Parallel()
				reg, _, host, _, requests := runningAppsMenuGeometryHarness(t, edge, 1200, 900, strings.Repeat("Wide title ", 30))
				if err := openRunningAppsGeometryMenu(t, reg, host); err != nil {
					t.Fatal(err)
				}
				spec := openedRunningAppsMenuSpec(t, *requests)
				if err := host.configure(int(spec.Width), int(spec.Height), scale); err != nil {
					t.Fatal(err)
				}
				if int(host.style.Scale120) != scale {
					t.Fatalf("style scale = %d, want %d", host.style.Scale120, scale)
				}
				width, height := host.size()
				if height > trayMenuMaxHeight || height > 900 {
					t.Fatalf("measured height = %d, exceeds output bounds", height)
				}
				assertLastRunningAppMenuRowFits(t, host, height)
				got := host.spec()
				want := trayMenuOnOutput(edge, host.anchor, width, height, 1200, 900)
				if int(got.Width) != width || int(got.Height) != height {
					t.Fatalf("configured spec size = %dx%d, measured = %dx%d", got.Width, got.Height, width, height)
				}
				if got.Anchor != want.anchor || got.MarginLeft != want.marginLeft || got.MarginTop != want.marginTop || got.MarginRight != want.marginRight {
					t.Fatalf("scale %d placement = (%d,%d,%d,%d), want (%d,%d,%d,%d)", scale, got.Anchor, got.MarginLeft, got.MarginTop, got.MarginRight, want.anchor, want.marginLeft, want.marginTop, want.marginRight)
				}
			})
		}
	}
}

func TestRunningAppsMenuRendererFailureLeavesMenuClosed(t *testing.T) {
	t.Parallel()
	reg, _, host, _, requests := runningAppsMenuGeometryHarness(t, "top", 1200, 800, "Window")
	reg.fontCatalog = registryFontCatalog{catalog: &render.SystemFontCatalog{}}
	if err := openRunningAppsGeometryMenu(t, reg, host); err == nil {
		t.Fatal("open succeeded without a usable text renderer")
	}
	if host.open_ {
		t.Fatal("renderer failure left the menu open")
	}
	if len(*requests) != 0 {
		t.Fatalf("renderer failure sent %d surface requests", len(*requests))
	}
}

func TestRunningAppsMenuRecomputesGeometryAfterFontStyleChange(t *testing.T) {
	t.Parallel()
	reg, _, host, _, requests := runningAppsMenuGeometryHarness(t, "top", 1200, 800, strings.Repeat("wide ", 6))
	if err := openRunningAppsGeometryMenu(t, reg, host); err != nil {
		t.Fatal(err)
	}
	before := openedRunningAppsMenuSpec(t, *requests)
	cfg := reg.cfg.Clone()
	cfg.Bar.FontSize = 80
	reg.mu.Lock()
	published := reg.retheThemeOpenSurfacesLocked(cfg, reg.tokens)
	reg.mu.Unlock()
	if host.style.Size <= 20 {
		t.Fatalf("rethemed text size = %d, want updated configured size", host.style.Size)
	}
	if len(*requests) < 3 {
		t.Fatalf("theme change sent %d requests, want updated menu geometry", len(*requests))
	}
	after := (*requests)[len(*requests)-1].Update
	if after == nil || after.Width == nil || int32(*after.Width) <= before.Width {
		t.Fatalf("rethemed surface update = %+v, before width = %d", after, before.Width)
	}
	found := false
	for _, invalidation := range published {
		if invalidation.Global == host.output && invalidation.SurfaceID == runningAppMenuSurfaceID {
			found = true
		}
	}
	if !found {
		t.Fatal("rethemed menu was not scheduled for repaint")
	}
}

func runningAppsMenuGeometryHarness(t *testing.T, edge string, outputWidth, outputHeight int, title string) (*Registry, *Bar, *runningAppMenuHost, ui.Rect, *[]wayland.AuxRequest) {
	t.Helper()
	cfg := config.Default()
	cfg.Bar.Edge = edge
	cfg.Bar.Left, cfg.Bar.Center = nil, nil
	cfg.Bar.Right = []config.Item{{ID: "running-apps"}}
	reg := NewRegistry(cfg)
	t.Cleanup(reg.Close)
	reg.runningIndex = []runningAppEntry{{ID: "steam", Actions: []runningAppAction{{ID: "Store", Name: "Store", Exec: "steam steam://store"}}}}
	newHosts(t, reg, map[uint32]string{1: "DP-9"})
	bar := reg.bars[1]
	bar.setOutputSize(outputWidth, outputHeight)
	reg.UpdateNiri(niri.Snapshot{Windows: []niri.Window{{ID: 80, AppID: "steam", Title: title, FocusTimestamp: 10}}})
	host := newRunningAppMenuHost(reg)
	var requests []wayland.AuxRequest
	host.request = func(request wayland.AuxRequest) { requests = append(requests, request) }
	reg.runningMenu = host
	return reg, bar, host, ui.Rect{X: 250, Y: 300, W: 50, H: 24}, &requests
}

func openRunningAppsGeometryMenu(t *testing.T, reg *Registry, host *runningAppMenuHost) error {
	t.Helper()
	slot, ok := runningSlotByKey(reg.running, "steam")
	if !ok {
		t.Fatal("Steam slot missing")
	}
	reg.mu.Lock()
	err := host.openLocked(1, slot, ui.Rect{X: 250, Y: 300, W: 50, H: 24})
	reg.mu.Unlock()
	return err
}

func openedRunningAppsMenuSpec(t *testing.T, requests []wayland.AuxRequest) *wayland.AuxSpec {
	t.Helper()
	for _, request := range requests {
		if request.Open != nil && request.Open.ID == runningAppMenuSurfaceID {
			return request.Open
		}
	}
	t.Fatal("no running-app surface request")
	return nil
}

func assertLastRunningAppMenuRowFits(t *testing.T, host *runningAppMenuHost, height int) {
	t.Helper()
	last := 0
	var walk func(*ui.Node)
	walk = func(node *ui.Node) {
		if node == nil {
			return
		}
		if node.Role == "menuitem" && node.Focusable {
			last = max(last, node.Bounds.Y+node.Bounds.H)
		}
		for _, child := range node.Children {
			walk(child)
		}
	}
	walk(host.root)
	if last == 0 || last > height {
		t.Fatalf("last visible menu item bottom = %d, surface height = %d", last, height)
	}
}

func openRunningAppsWindowMenu(t *testing.T, windows []niri.Window) (*Registry, *Bar, *runningAppMenuHost) {
	t.Helper()
	return openRunningAppsWindowMenuSnapshot(t, niri.Snapshot{Windows: windows})
}

func openRunningAppsWindowMenuSnapshot(t *testing.T, snapshot niri.Snapshot) (*Registry, *Bar, *runningAppMenuHost) {
	t.Helper()
	reg, bar := steamMenuReg(t)
	reg.UpdateNiri(snapshot)
	if !bar.onAction("running-app:steam", buttonRight) {
		t.Fatal("right click ignored")
	}
	host := reg.runningMenu
	w, height := host.size()
	if err := host.configure(w, height, 120); err != nil {
		t.Fatal(err)
	}
	return reg, bar, host
}
