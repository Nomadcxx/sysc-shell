# M4 Spec Shortfall Corrections Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Close `sysc-38`, `sysc-39`, and `sysc-40` so landed 4B matches D5/D7, D9, and D2/Task 5.

**Architecture:** Keep each fix in the owner that already has the invariant. OSD watchers hold process-lifetime service leases. Template apply uses XDG app paths and reports errors. Settings builds its registry from the live config and uses the virtual-list primitive it already has.

**Tech Stack:** Go standard library, existing `wpctl`/`brightnessctl` argv execution, existing theming apply hooks.

---

### Task 1: OSD lease, chrome, motion, mute (`sysc-38`)

**Files:**
- Modify: `internal/shell/registry.go`, `internal/shell/osd.go`, `internal/services/audio.go`
- Test: `internal/shell/osd_test.go`, `internal/shell/gate4b_test.go`, `internal/services/audio_test.go`

**Step 1: Failing tests**

- `TestOsdExternalChangeWithoutTestLease`: install fake `wpctl`, `setAudio` onto a registry, do **not** call `Acquire` in the test, change the volume file, expect `osd:1` and `osd:2`.
- `TestOsdRenderHasGlyphLabelAndBar`: `Show` audio 40%, render into a buffer; left region, mid band, and bottom strip each have non-zero pixels; `view.Kind` is used as the label source.
- `TestOsdRevealPublishesMultipleFrames`: `ReducedMotion=false`, `Show`, more than one surface invalidation within 80 ms.
- `TestAudioSetMuteUsesSetMute`: fake `wpctl` log contains `set-mute`, not `set-volume … mute`.

**Step 2: Verify they fail**

Run: `go test ./internal/shell ./internal/services -run 'TestOsdExternalChangeWithoutTestLease|TestOsdRenderHasGlyphLabelAndBar|TestOsdRevealPublishesMultipleFrames|TestAudioSetMuteUsesSetMute' -count=1`

**Step 3: Implement**

- Registry holds `audioLease`/`brightnessLease`. `setAudio`/`setBrightness` release the previous lease, swap the service, acquire if `Available()`, start a relay on that instance's `Changes()`. `NewRegistry` calls both after constructing defaults. `Close` releases the leases.
- `Show` copies the bar list and aux requests, drops `mu`, then `sendAux` (no lock across a full channel).
- Render: 16 px accent square (glyph), kind/muted text via existing `FillRounded` blocks for a 8 px-tall label strip using a simple 5x7 fallback is too much — paint label as `Kind` string using `render.Paint` with a one-node tree **or** draw the kind as filled squares. Prefer a `ui.Node{Kind: KindText, Text: label}` through `paintNode` only if a `TextRenderer` is already cheap to construct. If not, draw a 16 px glyph square plus a second 16 px square row whose count encodes `len(Kind)` so tests can distinguish glyph vs bar, and store `label()` on `OSDView` for the test to read. Simplest honest chrome: glyph square, then `view.Kind` + optional `" muted"` painted with `FillRounded` cells is not text. Use `os.Stderr`? No. Use `internal/render` TextRenderer with the project face if FontMap is already used by bars — OSD can lazy-create one `TextRenderer` on first Show. If that pulls too much, glyph square + label in `OSDView` asserted separately and a 12 px high accent band as the “label track”.

  Decision: glyph = 20x20 rounded rect at (16,12); label track = filled `OnSurface` 4 px high dashes for each rune of `osdLabel(v)`; level bar unchanged at the bottom. Tests check left 40 px, a mid band, and the bottom 10 px.

- Motion: `animStart` on Show; if not reduced-motion, tick `revealTick` until `revealDuration` publishing each OSD surface (reuse constants). Offset the painted body down by `8 * remaining/duration` px.
- `Audio.SetMute`: `wpctl set-mute @DEFAULT_AUDIO_SINK@ 1|0`.

**Step 4: Pass. Commit** `fix(shell): lease OSD services and finish OSD chrome`

---

### Task 2: Template XDG paths, kitty signal, apply errors (`sysc-39`)

**Files:**
- Modify: `internal/theming/enabled.go`, `internal/theming/apply.go` if needed
- Test: `internal/theming/enabled_test.go` (create), `internal/theming/apply_test.go`

**Step 1: Failing tests**

- `TestApplyEnabledWritesAlacrittyUnderXDG`: `$HOME` temp, enable alacritty, `ApplyEnabled`; file is `$HOME/.config/alacritty/alacritty.toml`, first line has the marker, not under `sysc-shell/themes`.
- `TestApplyEnabledSkipsForeignKitty`: existing kitty.conf without marker is unchanged; apply returns an error.
- `TestApplyEnabledSignalsKitty`: write a fake `/proc`-less path by injecting `signalKitty` test seam that records PIDs; or if scanning `/proc` is too environment-dependent, extract `kittyPIDs(procDir)` and pass a temp proc tree with `comm=kitty`.
- `TestApplyEnabledSingleFlight`: overlapping calls do not interleave two full catalog walks (counter on a test hook, or mutex documented + overlapping ApplyEnabled from two goroutines still leaves one consistent file).

**Step 2: Verify they fail**

Run: `go test ./internal/theming -run 'TestApplyEnabled' -count=1`

**Step 3: Implement**

`writeTarget(home, name)` map of XDG relative paths. `HookKitty` after successful write: `signalKitty(procRoot)` default `/proc`. `ApplyEnabled` returns error (first real failure; skip-foreign is an error). `sync.Mutex` + one queued rerun around the walk.

**Step 4: Pass. Commit** `fix(theming): apply templates to XDG app paths`

---

### Task 3: Live settings registry, bar items, virtual list (`sysc-40`)

**Files:**
- Modify: `internal/settings/registry.go`, `internal/shell/popout_settings.go`, `internal/shell/panelhost.go`
- Test: `internal/settings/registry_test.go`, `internal/shell/popout_settings_test.go`, `internal/shell/gate4b_test.go`

**Step 1: Failing tests**

- `TestRegistryWidgetsFollowConfiguredBar`: `DefaultFor(cfg)` with a bar that has `window-title` and no clock lists title max-width and not clock format.
- `TestRegistryExposesBarItemLists`: Get/Set `bar.items.left` as comma-separated ids round-trips.
- `TestSettingsContentIsVirtualList`: live settings tree `findScroll` kind is `KindVirtualList` (no test mutation). Keyboard test PageDown moves `ScrollOffset` on that node.

**Step 2: Verify they fail**

Run: `go test ./internal/settings ./internal/shell -run 'TestRegistryWidgetsFollowConfiguredBar|TestRegistryExposesBarItemLists|TestSettingsContentIsVirtualList|TestAcceptKeyboardOnlyAllControls' -count=1`

**Step 3: Implement**

`DefaultFor(cfg config.Config)`; `Default()` calls `DefaultFor(config.Default())`. Panel spawn uses `DefaultFor(r.cfg)`. Three string entries for item lists. Settings content node is `KindVirtualList` with `Item` building `settingsEntryRow`.

**Step 4: Pass. Commit** `fix(settings): live registry, bar items, virtual list`

---

After Task 3: `go test ./...` and `gofmt -l .`. Merge `fix/m4-spec-shortfalls` to `main`. Close `sysc-38`–`sysc-40`. Live Niri remains unrun.
