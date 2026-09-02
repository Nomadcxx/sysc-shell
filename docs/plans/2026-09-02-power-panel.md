# Power Panel Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Grow the session panel into battery status, `powerprofilesctl` profiles, and the existing session actions, toggled by right-clicking the existing 3C bar battery pill (glyph + percent in a capsule).

**Architecture:** Keep `PanelSession` as the one surface. Port gamer-mode's `powerprofilesctl list`/`set` parser into `internal/shell/powerprofiles.go`. Reuse sysmon `monitorCard` chrome and intrinsic height. `Bar.Handle` starts distinguishing left vs right so the battery action fires only on right-click.

**Tech Stack:** Go, existing `internal/ui` / `internal/shell` panel host, `os/exec.LookPath` + argv. No godbus, no new module, no CGO.

**Design:** [2026-09-02-power-panel-design.md](2026-09-02-power-panel-design.md)

Do not port gamer-mode's freeze/kill engine. Do not start Milestone 6. Do not bump `sysc-metrics`.

Work from `/home/nomadx/sysc-shell`. After each task: `gofmt -w` the touched files, `go test -race -count=1` on the packages named in that task, then commit. Screen the commit message for the hook (no `agent`, `cursor`, `codex`, `llm`, `both`, `Hallmark`).

---

### Task 1: Parse `powerprofilesctl list`

**Files:**
- Create: `internal/shell/powerprofiles.go`
- Create: `internal/shell/powerprofiles_test.go`

**Step 1: Write the failing test**

```go
package shell

import "testing"

func TestParsePowerProfilesMarksTheStarredNameActive(t *testing.T) {
	t.Parallel()
	text := "" +
		"  performance:\n" +
		"    Driver:     amd_pstate\n" +
		"\n" +
		"* balanced:\n" +
		"    Driver:     amd_pstate\n" +
		"\n" +
		"  power-saver:\n" +
		"    Driver:     amd_pstate\n"
	names, active := parsePowerProfiles(text)
	want := []string{"performance", "balanced", "power-saver"}
	if len(names) != 3 || names[0] != want[0] || names[1] != want[1] || names[2] != want[2] {
		t.Fatalf("names = %v, want %v", names, want)
	}
	if active != "balanced" {
		t.Fatalf("active = %q, want balanced", active)
	}
}

func TestParsePowerProfilesIgnoresDetailLines(t *testing.T) {
	t.Parallel()
	names, active := parsePowerProfiles("    Driver: foo\nnot-a-profile\n")
	if len(names) != 0 || active != "" {
		t.Fatalf("names = %v active = %q, want empty", names, active)
	}
}

func TestPowerProfileLabel(t *testing.T) {
	t.Parallel()
	cases := map[string]string{
		"power-saver":  "Power saver",
		"balanced":     "Balanced",
		"performance":  "Performance",
		"cool-custom":  "cool-custom",
	}
	for name, want := range cases {
		if got := powerProfileLabel(name); got != want {
			t.Fatalf("%s: %q, want %q", name, got, want)
		}
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test -count=1 ./internal/shell -run 'TestParsePowerProfiles|TestPowerProfileLabel'`

Expected: FAIL, `parsePowerProfiles` undefined.

**Step 3: Write minimal implementation**

Port gamer-mode `parsePowerProfiles` (`gamer-mode/service.luau` ~1015–1029). A profile line is optional spaces, optional `*`, a name `[A-Za-z0-9][A-Za-z0-9_-]*`, a colon, optional trailing space.

```go
var profileLine = regexp.MustCompile(`^\s*(\*?)\s*([A-Za-z0-9][A-Za-z0-9_-]*):\s*$`)

func parsePowerProfiles(text string) (names []string, active string) {
	for _, line := range strings.Split(text, "\n") {
		m := profileLine.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		names = append(names, m[2])
		if m[1] == "*" {
			active = m[2]
		}
	}
	return names, active
}

func powerProfileLabel(name string) string {
	switch name {
	case "power-saver":
		return "Power saver"
	case "balanced":
		return "Balanced"
	case "performance":
		return "Performance"
	}
	return name
}

func profileSupports(names []string, name string) bool {
	for _, n := range names {
		if n == name {
			return true
		}
	}
	return false
}

func powerProfileSetArgv(name string) []string {
	return []string{"powerprofilesctl", "set", name}
}
```

Add a one-line comment that the parser is ported from `Nomadcxx/noctalia-gamermode`.

**Step 4: Run test to verify it passes**

Run: `go test -race -count=1 ./internal/shell -run 'TestParsePowerProfiles|TestPowerProfileLabel'`

Expected: PASS.

**Step 5: Commit**

```bash
git add internal/shell/powerprofiles.go internal/shell/powerprofiles_test.go
git commit -m "$(cat <<'EOF'
feat(shell): parse powerprofilesctl list output

EOF
)"
```

---

### Task 2: Hide the profile card without the tool; set only listed names

**Files:**
- Modify: `internal/shell/powerprofiles.go`
- Modify: `internal/shell/powerprofiles_test.go`
- Modify: `internal/shell/popout_session.go` (`runArgvDefault`: wait on `powerprofilesctl` the same way it waits on `loginctl`)

`Registry.runArgv` is already a per-registry hook. Tests replace it. Add a `lookPath` hook only if tests cannot inject availability another way. Prefer: `powerProfilesAvailable(look func(string) (string, error)) bool` that calls `look("powerprofilesctl")`, with tests passing a fake, production passing `exec.LookPath`.

**Step 1: Write the failing test**

```go
func TestPowerProfilesUnavailableWithoutBinary(t *testing.T) {
	t.Parallel()
	look := func(string) (string, error) { return "", exec.ErrNotFound }
	if powerProfilesAvailable(look) {
		t.Fatal("missing binary still counted as available")
	}
}

func TestPowerProfileSetArgv(t *testing.T) {
	t.Parallel()
	got := powerProfileSetArgv("performance")
	want := []string{"powerprofilesctl", "set", "performance"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("argv = %v, want %v", got, want)
	}
}

func TestPowerProfileSetRefusesUnknownName(t *testing.T) {
	t.Parallel()
	if profileSupports([]string{"balanced"}, "performance") {
		t.Fatal("unlisted name was accepted")
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test -count=1 ./internal/shell -run 'TestPowerProfilesUnavailable|TestPowerProfileSet'`

Expected: FAIL on the new symbols.

**Step 3: Write minimal implementation**

```go
func powerProfilesAvailable(look func(string) (string, error)) bool {
	_, err := look("powerprofilesctl")
	return err == nil
}
```

In `runArgvDefault`, treat `powerprofilesctl` like `loginctl`: `CommandContext` 5s `Run`, not `Start`.

**Step 4: Run test to verify it passes**

Run: `go test -race -count=1 ./internal/shell -run 'TestPowerProfiles|TestPowerProfile|TestSessionExec'`

Expected: PASS. Session argv tests still pass.

**Step 5: Commit**

```bash
git add internal/shell/powerprofiles.go internal/shell/powerprofiles_test.go internal/shell/popout_session.go
git commit -m "$(cat <<'EOF'
feat(shell): gate power profiles on powerprofilesctl

EOF
)"
```

---

### Task 3: Right-click battery toggles the session panel

**Files:**
- Modify: `internal/shell/bar.go` (`Handle`, `onAction`, `setActionHandler`)
- Modify: `internal/shell/bar_test.go` (`click` must send `buttonLeft`)
- Modify: `internal/shell/widget.go` (battery node `Action`)
- Modify: `internal/shell/metricwidget.go` (keep `panelMonitorAction`; add `panelSessionAction` next to it, or put the session constant in `batterywidget.go` / `widget.go` — one constant, same package)
- Modify: `internal/shell/registry.go` (`bindBarPanelActionsLocked`)
- Modify: `internal/shell/tray.go` only if `buttonLeft = 0x110` belongs next to the existing `buttonRight`
- Test: `internal/shell/panelhost_test.go` (mirror `TestClickingABarMetricOpensTheSystemMonitor`)
- Test: `internal/shell/batterywidget_test.go` or `widget` tests for the action string

`click` currently omits `Button`, so it is 0. After this task, 0 must not be treated as right-click. Send `buttonLeft` from `click`.

**Step 1: Write the failing tests**

```go
func TestABatteryWidgetOpensTheSessionPanel(t *testing.T) {
	t.Parallel()
	// build the default battery widget; assert inner.Action == panelSessionAction
}

func TestRightClickingTheBarBatteryOpensSession(t *testing.T) {
	t.Parallel()
	reg := newPanelRegistry(t)
	cb, err := reg.NewHost(7, "eDP-1")
	if err != nil {
		t.Fatal(err)
	}
	if err := cb.Configure(1536, 44, 120); err != nil {
		t.Fatal(err)
	}
	bar := reg.bars[7]
	target, ok := batteryClickTarget(bar)
	if !ok {
		t.Fatal("default bar has no laid-out battery")
	}
	drainAuxQueue(reg)
	if click(bar, target.X+target.W/2, target.Y+target.H/2) {
		t.Fatal("left-click on battery must stay inert")
	}
	if !clickButton(bar, target.X+target.W/2, target.Y+target.H/2, buttonRight) {
		t.Fatal("right-click on battery did not activate")
	}
	reqs := drainAux(t, reg, 2)
	if !strings.HasPrefix(reqs[1].Open.ID, "panel:session") {
		t.Fatalf("opened %q, want session", reqs[1].Open.ID)
	}
}

func TestClickingABarMetricStillOpensTheSystemMonitor(t *testing.T) {
	t.Parallel()
	// existing TestClickingABarMetricOpensTheSystemMonitor must still pass
	// after click() sends buttonLeft
}
```

Helper `batteryClickTarget` walks bar widgets for `Action == panelSessionAction` with non-zero bounds, same shape as `metricClickTarget`.

Change `onAction` to `func(action string, button uint32) bool`. `Handle` records the press button and passes `event.Button` on release.

`bindBarPanelActionsLocked`:

```go
bar.setActionHandler(func(action string, button uint32) bool {
	out, trig := r.triggerFor(global)
	switch {
	case action == panelMonitorAction && (button == 0 || button == buttonLeft):
		return r.TogglePanel(PanelMonitor, out, trig) == nil
	case action == panelSessionAction && button == buttonRight:
		return r.TogglePanel(PanelSession, out, trig) == nil
	}
	return false
})
```

Do not treat button 0 as right. After `click` sends `buttonLeft`, the `button == 0` clause is only a safety net; drop it if every test path sets a button.

**Step 2: Run test to verify it fails**

Run: `go test -count=1 ./internal/shell -run 'TestRightClickingTheBarBattery|TestABatteryWidgetOpens|TestClickingABarMetric'`

Expected: FAIL — battery has no action, handler ignores button.

**Step 3: Write minimal implementation**

- `const panelSessionAction = "panel:session"`
- Battery `textWidget` node gets `Action: panelSessionAction`
- `Bar` stores `pressedButton uint32` on press; release calls `onAction(action, event.Button)`
- Wire the handler as above
- `click` / new `clickButton` send the button on press and release

**Step 4: Run test to verify it passes**

Run: `go test -race -count=1 ./internal/shell -run 'TestRightClickingTheBarBattery|TestABatteryWidgetOpens|TestClickingABarMetric|TestSession'`

Expected: PASS. Left-click metric still opens sysmon. Left-click battery does not.

**Step 5: Commit**

```bash
git add internal/shell/bar.go internal/shell/bar_test.go internal/shell/widget.go internal/shell/registry.go internal/shell/panelhost_test.go internal/shell/tray.go internal/shell/batterywidget.go internal/shell/metricwidget.go
git commit -m "$(cat <<'EOF'
feat(shell): open session from a battery right-click

EOF
)"
```

---

### Task 4: Three-card session tree, 420 wide, height from content, trailing-edge align

**Files:**
- Modify: `internal/shell/popout_session.go` (`sessionTree`)
- Modify: `internal/shell/popout_session_test.go`
- Modify: `internal/shell/panelhost.go` (`panelTree` for `PanelSession`, `panelTargetSize`, `acquirePanelLeases`, `activate` for `profile:` actions, `openPanelRootLocked` height + align)
- Reuse: `monitorCard`, `monitorCardTitle`, `monitorSurfaceHeight` in `popout_monitor.go` / `panelhost.go`

Hold profile state on `PanelHost`: `profiles []string`, `profileActive string`, `profilesOK bool`. Load on open (and after a successful set) by running `powerprofilesctl list` through `runArgv` only if available. Tests stub `runArgv` and a lookPath field on `Registry`.

If injecting `list` stdout through `runArgv` is awkward (`runArgv` returns `error`, not stdout), add a narrow `runArgvOutput func(argv []string) (string, error)` used only for `powerprofilesctl list`. Production: `CommandContext` + `Output`. Set still uses `runArgv`.

**Step 1: Write the failing tests**

```go
func TestSessionPanelTargetSizeIs420(t *testing.T) {
	t.Parallel()
	got := panelTargetSize(PanelSession)
	if got.W != 420 {
		t.Fatalf("width = %d, want 420", got.W)
	}
}

func TestSessionTreeOmitsBatteryWithoutAPresentPack(t *testing.T) {
	t.Parallel()
	_, h := newSessionHost(t, "swaylock")
	// with empty Battery snapshot: no KindMeter, no heading named Battery
}

func TestSessionTreeOmitsProfilesWhenUnavailable(t *testing.T) {
	t.Parallel()
	reg, h := newSessionHost(t, "swaylock")
	reg.lookPath = func(string) (string, error) { return "", exec.ErrNotFound }
	reg.rebuildPanel(h)
	for _, n := range ui.Focusables(h.root) {
		if n.Role == "tab" {
			t.Fatalf("profile tab %q shown without powerprofilesctl", n.Name)
		}
	}
}

func TestSessionActionsRemain(t *testing.T) {
	t.Parallel()
	_, h := newSessionHost(t, "swaylock")
	got := focusableNames(h.root)
	// must still contain Lock, Log out, Suspend, Reboot, Power off
}

func TestSessionAlignsToTheTrailingEdge(t *testing.T) {
	t.Parallel()
	reg := newPanelRegistry(t)
	cb, err := reg.NewHost(7, "eDP-1")
	if err != nil {
		t.Fatal(err)
	}
	if err := cb.Configure(1536, 44, 120); err != nil {
		t.Fatal(err)
	}
	if err := reg.TogglePanelByName("session"); err != nil {
		t.Fatal(err)
	}
	reqs := drainAux(t, reg, 2)
	got := reqs[1].Open
	if got.MarginLeft == (1536-got.Width)/2 {
		t.Fatal("session still centred")
	}
}

func TestSelectingAListedProfileRunsSet(t *testing.T) {
	t.Parallel()
	reg, h := newSessionHost(t, "swaylock")
	h.profilesOK = true
	h.profiles = []string{"power-saver", "balanced", "performance"}
	h.profileActive = "balanced"
	reg.rebuildPanel(h)
	var got [][]string
	reg.runArgv = func(argv []string) error {
		got = append(got, append([]string(nil), argv...))
		return nil
	}
	activateNamed(h, reg, "Performance")
	if len(got) != 1 || !reflect.DeepEqual(got[0], []string{"powerprofilesctl", "set", "performance"}) {
		t.Fatalf("argv = %v", got)
	}
}
```

Existing `TestSessionActionsList` / `TestLockHiddenWithoutLocker` / `TestSessionExecMapping` must keep passing. They look at focusable names; profile tabs will appear only when `profilesOK` is true. Default `newSessionHost` should leave profiles unavailable so those tests stay four/five session buttons.

Battery card when present: heading Name `"Battery"`, a `KindMeter`, percent text. Inject a snapshot on the registry the same way monitor tests do (`r.sample`).

**Step 2: Run test to verify it fails**

Run: `go test -count=1 ./internal/shell -run 'TestSession'`

Expected: FAIL — width still 280, tree is still a bare button column, align still centre.

**Step 3: Write minimal implementation**

- `panelTargetSize(PanelSession)` → `ui.Rect{W: 420, H: 200}`
- `openPanelRootLocked`: `if id == PanelSession { place.Align = "right" }` and apply `monitorSurfaceHeight` like sysmon
- `acquirePanelLeases`: `PanelSession` acquires `SourceBattery` at `time.Second`
- `sessionTree`: column of cards (battery if present, profiles if `h.profilesOK && len(h.profiles) > 0`, session buttons). Profile button `Action: "profile:" + name`, `Name: powerProfileLabel(name)`, `Role: "tab"`, `Bold: name == h.profileActive`
- `activate`: prefix `profile:` → if `profileSupports`, `runArgv(powerProfileSetArgv(name))`, refresh list, `rebuildPanel`. Do not close the panel (session actions still close)
- Load profiles once on open when available

Battery card body: glyph + percent row, `KindMeter{Value: charge, Max: 1}`, state + optional time + optional watts. Reuse `batteryDuration` and `BatteryIconRune`.

**Step 4: Run test to verify it passes**

Run: `go test -race -count=1 ./internal/shell ./internal/ipc`

Expected: PASS.

**Step 5: Commit**

```bash
git add internal/shell/popout_session.go internal/shell/popout_session_test.go internal/shell/panelhost.go internal/shell/panelhost_test.go
git commit -m "$(cat <<'EOF'
feat(shell): stack battery and profiles on session

EOF
)"
```

---

### Task 5: IPC `power` alias and hotkey note

**Files:**
- Modify: `internal/ipc/server.go` (`knownPanels`)
- Modify: `internal/ipc/server_test.go`
- Modify: `internal/shell/panelhost.go` (`parsePanelName("power")` → `PanelSession`)
- Modify: `docs/niri-hotkeys.md` (Super+X still `session`; one sentence that the surface includes battery and profiles)

**Step 1: Write the failing test**

```go
func TestPowerIsAnAliasForSession(t *testing.T) {
	t.Parallel()
	id, err := parsePanelName("power")
	if err != nil || id != PanelSession {
		t.Fatalf("parsePanelName(power) = %v, %v", id, err)
	}
}

func TestIpcPowerAliasTogglesSession(t *testing.T) {
	t.Parallel()
	// same shape as TestServerRoundTrip, params panel=power
	// handler must see toggle:power (IPC passes the requested name through)
	// TogglePanelByName maps it to PanelSession
}
```

Decide one behaviour and test it: `parsePanelName` maps `power` → `PanelSession`, so `TogglePanelByName("power")` opens `panel:session`. IPC `knownPanels` accepts `"power"`. The handler still receives the string `"power"`; `TogglePanelByName` must accept it.

**Step 2: Run test to verify it fails**

Run: `go test -count=1 ./internal/shell ./internal/ipc -run 'TestPowerIsAnAlias|TestIpcPower'`

Expected: FAIL — unknown panel.

**Step 3: Write minimal implementation**

```go
case "session", "power":
    return PanelSession, nil
```

```go
"session": "",
"power":   "",
```

**Step 4: Run test to verify it passes**

Run: `go test -race -count=1 ./internal/shell ./internal/ipc`

Expected: PASS.

**Step 5: Commit**

```bash
git add internal/ipc/server.go internal/ipc/server_test.go internal/shell/panelhost.go docs/niri-hotkeys.md
git commit -m "$(cat <<'EOF'
feat(ipc): accept power as a session alias

EOF
)"
```

---

### Task 6: Live Niri gate (owner-deferred)

This machine's Cursor session is not the live compositor. The live host is the laptop. Do not claim the slice done from unit tests alone.

**Checklist (owner):**

1. Build and install over `~/.local/bin/sysc-shell`, restart by pid.
2. Right-click the bar battery: session surface maps under the bar, trailing edge, ~420 wide, height hugs the cards.
3. Left-click the battery: nothing.
4. Left-click CPU or Memory: sysmon still toggles.
5. Super+X toggles the same surface.
6. With `powerprofilesctl` present: three (or however many `list` returns) profile buttons; clicking one changes `powerprofilesctl get`.
7. Hide `powerprofilesctl` on `PATH` (or a machine without it): profile card gone, session buttons remain.
8. Session actions still call `loginctl` / the locker.
9. `niri msg -j layers` shows `panel:session` while open.

Record observations in bd, not by patching this plan. Close the tracking issue only after the owner runs the live list or explicitly defers it the same way M4 live matrices were deferred.

**No commit** unless a completion handover is requested.

---

## Execution notes

- `monitorCard` lives in `popout_monitor.go` in package `shell`. Session code may call it. Do not duplicate a third card helper.
- `KindButton` always fills accent. Active profile = `Bold`. Do not retouch `paint.go` in this slice.
- `gofmt -w . && test -z "$(gofmt -l .)"`, `go vet ./...`, `go test -race -count=1 ./...` before the last code commit.
- Kill a live binary by pid from `pgrep -a -f '/home/nomadx/.local/bin/sysc-shell'`. Never `pkill -f` a name typed in the command.
