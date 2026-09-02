# Screen Recorder Panel Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Give Screen Recorder a camera pill (Record / Stop) and a sysmon-placed panel that edits the same schema Settings already stores.

**Architecture:** The plugin owns GSR and the header tree. The host toggles `PanelPlugin`, converts the header, and appends settings rows from the manifest when `include_settings` is set. Writes go through `pluginHost.applySetting`. No new v1 control kinds and no `settings.set`.

**Tech Stack:** Go, `plugin/v1`, existing panel host, `internal/render/icons/build.py` (authoring-time). No new module.

**Design:** [2026-09-02-recorder-panel-design.md](2026-09-02-recorder-panel-design.md). bd: `sysc-136`.

Work from `/home/nomadx/.config/superpowers/worktrees/sysc-shell/milestone/plugin-host` on `milestone/plugin-host`. Run `bd` from `/home/nomadx/sysc-shell`. Worktree commits need `BEADS_DB=/home/nomadx/sysc-shell/.beads/beads.db`. Do not `bd export` (it clobbers JSONL); edit the JSONL surgically if status must move.

After each task: `gofmt -w` the touched files, `go test -race -count=1` on the packages named in that task, then commit. Screen the commit message for the hook (no `agent`, `cursor`, `codex`, `llm`, `both`, `Hallmark`).

Do not close `sysc-73`. Do not build a control-center tile, screenshot toolkit, or native folder picker.

---

### Task 1: Author recorder glyphs and extend the catalogue

**Files:**
- Create: `internal/render/icons/svg/camera.svg`
- Create: `internal/render/icons/svg/camera-off.svg`
- Create: `internal/render/icons/svg/record.svg`
- Create: `internal/render/icons/svg/stop.svg`
- Create: `internal/render/icons/svg/replay.svg`
- Modify: `internal/render/icons/build.py` (`GLYPHS`, after `network`)
- Modify: `internal/render/iconfont.go` (`iconNames` plus consecutive runes after `iconNetwork`)
- Modify: `internal/render/iconfont_test.go`

**Step 1: Write the failing test**

In `iconfont_test.go`:

```go
func TestRecorderCatalogueNamesResolve(t *testing.T) {
	t.Parallel()
	for _, name := range []string{"camera", "camera-off", "record", "stop", "replay"} {
		if _, ok := IconByName(name); !ok {
			t.Fatalf("%q is missing from the catalogue", name)
		}
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test -race -count=1 ./internal/render -run TestRecorderCatalogueNamesResolve -v`

Expected: FAIL, `camera is missing from the catalogue`

**Step 3: Write the SVGs (24×24 filled paths, same language as `cpu.svg`)**

`camera.svg`:

```svg
<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24">
  <path d="M9 4h2l1 2h6a3 3 0 0 1 3 3v9a3 3 0 0 1-3 3H6a3 3 0 0 1-3-3V9a3 3 0 0 1 3-3h2l1-2z"/>
  <path d="M12 10.5a3.5 3.5 0 1 0 0 7a3.5 3.5 0 1 0 0-7z"/>
</svg>
```

`camera-off.svg`: the camera paths plus a filled slash `M3.2 4.6l1.4-1.4 16.6 16.6-1.4 1.4z`.

`record.svg`: `M12 6a6 6 0 1 0 0 12a6 6 0 1 0 0-12z`

`stop.svg`: `M7 7h10v10H7z`

`replay.svg`:

```svg
<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24">
  <path d="M12 4v3l4-4-4-4v3a8 8 0 1 0 8 8h-2a6 6 0 1 1-6-6z"/>
</svg>
```

These are original geometric drawings. Do not copy Tabler or Material outlines.

Append to `GLYPHS` in `build.py`:

```python
    ("uniE01B", 0xE01B, "camera"),
    ("uniE01C", 0xE01C, "camera-off"),
    ("uniE01D", 0xE01D, "record"),
    ("uniE01E", 0xE01E, "stop"),
    ("uniE01F", 0xE01F, "replay"),
```

In `iconfont.go`, add runes after `iconNetwork` (`iconCamera` … `iconReplay`) and put the five names in `iconNames`. Rebuild: `python3 internal/render/icons/build.py` from the worktree (fontTools already used for this font). Commit the TTF.

**Step 4: Run test to verify it passes**

Run: `go test -race -count=1 ./internal/render -run 'TestRecorderCatalogue|TestIconRunesResolve' -v`

Expected: PASS. `IconByName("camera")` is a PUA rune that shapes with the icon face.

**Step 5: Commit**

```bash
git add internal/render/icons/svg/camera.svg internal/render/icons/svg/camera-off.svg \
  internal/render/icons/svg/record.svg internal/render/icons/svg/stop.svg \
  internal/render/icons/svg/replay.svg internal/render/icons/build.py \
  internal/render/icons/sysc-icons.ttf internal/render/iconfont.go \
  internal/render/iconfont_test.go
git commit -m "$(cat <<'EOF'
feat(render): add recorder glyphs to the icon catalogue

EOF
)"
```

---

### Task 2: Let a button carry a catalogue icon

**Files:**
- Modify: `plugin/v1/node.go` (`vocabulary`: if `KindButton` has `Icon`, run `icon()`)
- Modify: `plugin/v1/node_test.go`
- Modify: `internal/plugin/view.go` (`KindButton` convert)
- Test: `internal/plugin/view_test.go`

**Step 1: Write the failing test**

```go
func TestConvertResolvesAButtonIcon(t *testing.T) {
	t.Parallel()
	root := &v1.Node{Kind: v1.KindButton, ID: "camera", Icon: "camera",
		Name: "Open screen recorder", Role: "button",
		Events: []v1.EventKind{v1.EventActivate}}
	if err := v1.Validate(root, v1.ViewBar); err != nil {
		t.Fatal(err)
	}
	got, err := Convert(root, v1.ViewBar)
	if err != nil {
		t.Fatal(err)
	}
	want, ok := render.IconByName("camera")
	if !ok {
		t.Fatal("catalogue missing camera")
	}
	if got.Text != string(want) || got.Action != "camera" {
		t.Fatalf("button = %+v", got)
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test -race -count=1 ./internal/plugin -run TestConvertResolvesAButtonIcon -v`

Expected: FAIL, button text empty (Icon ignored).

**Step 3: Minimal convert**

In the `KindButton` case, if `n.Icon != ""`, resolve with `render.IconByName` (same error as `KindIcon` on miss). If `n.Text == ""`, set `out.Text` to the glyph. If `Text` is set, keep `Text` and ignore `Icon` for v1 of this UI (camera is icon-only). Validate `Icon` on buttons the same way as `KindIcon`.

**Step 4: Run test to verify it passes**

Run: `go test -race -count=1 ./plugin/v1 ./internal/plugin -run 'ButtonIcon|ConvertResolvesAButtonIcon|ConvertRejectsAnIcon' -v`

Expected: PASS.

**Step 5: Commit**

```bash
git commit -m "$(cat <<'EOF'
feat(plugin): resolve catalogue icons on buttons

EOF
)"
```

---

### Task 3: Declare the recorder panel and `include_settings`

**Files:**
- Modify: `internal/plugin/manifest.go` (`wirePanel.IncludeSettings`, `Panel.IncludeSettings`)
- Modify: `internal/plugin/manifest_test.go`
- Modify: `plugins/reference/recorder/manifest.json`

**Step 1: Write the failing test**

Extend a valid-panel case: a panel with `"include_settings": true` loads, and `Panel.IncludeSettings` is true. A panel that omits the field stays false (Weather unchanged). `include_settings` is a known field so `DisallowUnknownFields` does not reject the recorder manifest.

**Step 2: Run test to verify it fails**

Run: `go test -race -count=1 ./internal/plugin -run 'IncludeSettings|ValidPanel' -v`

Expected: FAIL (field unknown or not plumbed).

**Step 3: Minimal implementation**

Add `IncludeSettings bool \`json:"include_settings,omitempty"\`` to `wirePanel` and copy it onto `Panel`. Update recorder `manifest.json`:

```json
"capabilities": ["notifications", "panels", "settings", "state"],
"panels": [{"id": "panel", "width": 480, "height": 560, "placement": "attached", "include_settings": true}]
```

**Step 4: Run test to verify it passes**

Run: `go test -race -count=1 ./internal/plugin -run 'Manifest|IncludeSettings|Panel' -v`

Expected: PASS. Recorder manifest loads.

**Step 5: Commit**

```bash
git commit -m "$(cat <<'EOF'
feat(plugin): declare the recorder panel and include_settings

EOF
)"
```

---

### Task 4: Composite bar pill and input map

**Files:**
- Modify: `plugins/reference/recorder/view.go`
- Modify: `plugins/reference/recorder/view_test.go`

**Step 1: Rewrite the failing checks** (replace `TestBarTreeStates`, `TestBarTreeHidesWhenIdleAndConfigured`, `TestHandleInputButtons`)

Bar tree:

- Root `KindRow` gap 8.
- Button `id=camera`, `Icon` from D5 (`camera` / `camera-off` / `replay`), `ToneError` when Recording, Adopted, Failed, or Unavailable. `Name` "Open screen recorder". Events: activate only.
- Button `id=record`, Text `Record`, inert copy is still present: keep the node, set `Name` "Record" always. When not Idle (or when Unavailable/Failed), the command must ignore activate (tested in HandleInput). Design D3: Record live only when Idle. Encode inert as: still a button (stable width) but `HandleInput` returns false for record unless Idle.
- Button `id=stop`, Text `Stop`. HandleInput returns stop only when Recording, Adopted, or Stopping.

`hide_inactive` + Idle: camera remains; Record and Stop omitted.

HandleInput returns `(open, record, stop, replay, save)` or a small action enum. No secondary/middle mapping.

PanelTree (stub for this task is fine if Task 5 owns the full header): at least Validate as `ViewPanel`. Prefer implementing the full header here if the tests fit in this package.

**Step 2: Run tests to verify they fail**

Run: `go test -race -count=1 ./plugins/reference/recorder -run 'BarTree|HandleInput' -v`

Expected: FAIL against the current single `record` button.

**Step 3: Minimal implementation** matching D2–D7.

**Step 4: Run tests to verify they pass**

Run: `go test -race -count=1 ./plugins/reference/recorder -run 'BarTree|HandleInput|Tooltip' -v`

Expected: PASS. `v1.Validate(..., ViewBar)` succeeds. Idle+hide_inactive still contains the camera icon name in the tree (flatten may show no text; assert a child with `ID=="camera"`).

**Step 5: Commit**

```bash
git commit -m "$(cat <<'EOF'
feat(plugin): put camera, record, and stop on the recorder pill

EOF
)"
```

---

### Task 5: Panel header tree and elapsed

**Files:**
- Modify: `plugins/reference/recorder/recorder.go` (start time on Snapshot)
- Modify: `plugins/reference/recorder/view.go` (`PanelTree`, `FormatElapsed`)
- Modify: `plugins/reference/recorder/view_test.go`
- Modify: `plugins/reference/recorder/recorder_test.go` if Snapshot shape changes

**Step 1: Write the failing test**

```go
func TestPanelTreeIdle(t *testing.T) {
	t.Parallel()
	root := PanelTree(Snapshot{Mode: Idle}, Config{}, time.Time{})
	if err := v1.Validate(root, v1.ViewPanel); err != nil {
		t.Fatal(err)
	}
	body := flatten(root)
	if !strings.Contains(body, "Screen Recorder") || !strings.Contains(body, "Record") {
		t.Fatalf("panel = %q", body)
	}
	if strings.Contains(body, "Start replay") {
		t.Fatal("replay controls shown while disabled")
	}
}

func TestPanelTreeReplayControls(t *testing.T) {
	t.Parallel()
	root := PanelTree(Snapshot{Mode: Idle}, Config{ReplayEnabled: true}, time.Time{})
	if !strings.Contains(flatten(root), "Start replay") {
		t.Fatal("missing replay start")
	}
}

func TestPanelTreeElapsed(t *testing.T) {
	t.Parallel()
	root := PanelTree(Snapshot{Mode: Recording, Elapsed: 72 * time.Second}, Config{}, time.Time{})
	if !strings.Contains(flatten(root), "01:12") {
		t.Fatalf("elapsed missing: %q", flatten(root))
	}
}
```

Use whatever Config field name already exists for `replay_enabled`. Last artifact: a panel with `Artifact` set includes that path.

**Step 2: Run test to verify it fails**

Run: `go test -race -count=1 ./plugins/reference/recorder -run TestPanelTree -v`

Expected: FAIL, `PanelTree` undefined.

**Step 3: Minimal implementation**

`Snapshot.Elapsed` is computed in `Snapshot()` from an unexported `started time.Time` set when entering Recording/ReplayActive/Adopted and cleared on Idle. Format `mm:ss` with tabular text, keyed `elapsed`. Header column gap 8: icon+title row, status word, elapsed (only when live), transport row, last path text.

**Step 4: Run test to verify it passes**

Run: `go test -race -count=1 ./plugins/reference/recorder -run 'PanelTree|Recorder|Snapshot' -v`

Expected: PASS.

**Step 5: Commit**

```bash
git commit -m "$(cat <<'EOF'
feat(plugin): add the recorder panel header tree

EOF
)"
```

---

### Task 6: Toggle this plugin's panel and compose settings under it

**Files:**
- Modify: `internal/shell/pluginhost.go` (`openPanel`, `panelTree`)
- Modify: `internal/shell/pluginhost_test.go`
- Modify: `internal/shell/popout_plugins.go` only if grouping lives there

**Step 1: Write the failing tests**

1. `openPanel` when this plugin's panel is already the open `PanelPlugin` on that output closes it (toggle). Opening recorder while another plugin's panel is up replaces it.
2. `panelTree` for a view whose manifest panel has `include_settings` appends labelled setting rows (at least "Output directory") under the converted plugin root. Weather (`include_settings` false) stays plugin-only.

Reuse `bindTestPlugin` with a recorder-shaped manifest fixture if the helper can take a manifest string; otherwise a small `WriteHelperPlugin` in the test.

**Step 2: Run tests to verify they fail**

Run: `go test -race -count=1 ./internal/shell -run 'Plugin.*Panel|IncludeSettings|ToggleRecorder' -v`

Expected: FAIL.

**Step 3: Minimal implementation**

- `openPanel`: if `h.panel` is this plugin+entry and `PanelPlugin` is open on `global`, `ClosePanel` and return success. Else existing open path. Camera therefore toggles via existing `panel.open`.
- `panelTree`: convert plugin root as today; if `include_settings`, wrap `KindColumn` of plugin root plus `pluginPanelSettings(r, pluginID)`.
- `pluginPanelSettings`: walk the recorder key groups from the design (Capture, File, Video, Audio, Replay, Bar). Skip keys hidden by `visible_when` (add `plugin.SettingVisible(s, values) bool` next to `CheckValues` if needed). Reuse `pluginSettingRow` (still bool/text until Task 7).
- Size already comes from the open view's Width/Height (480×560).

**Step 4: Run tests to verify they pass**

Run: `go test -race -count=1 ./internal/shell -run 'Plugin|Panel' -v`

Expected: PASS. Existing Weather/Timer panel tests still pass.

**Step 5: Commit**

```bash
git commit -m "$(cat <<'EOF'
feat(plugin): toggle the recorder panel and compose its settings

EOF
)"
```

---

### Task 7: Real setting controls and real apply values

**Files:**
- Modify: `internal/shell/popout_plugins.go` (`pluginSettingRow`, `handlePluginManager`)
- Modify: `internal/shell/popout_plugins_test.go`
- Modify: `internal/shell/panelhost.go` if activate must pass the node into the manager handler

**Step 1: Write the failing tests**

1. A select setting renders `KindMenu` (or the same node `settingsControl` uses for enums).
2. An int setting with min/max renders `KindSlider`.
3. A folder/string setting renders `KindTextField`.
4. `replay_duration` is absent when `replay_enabled` is false, present when true.
5. Activating a select or typing a directory calls `applySetting` with that value, not the literal `true`. Rejected values leave config unchanged (existing test stays).

**Step 2: Run tests to verify they fail**

Run: `go test -race -count=1 ./internal/shell -run 'PluginSetting|plugin-set|VisibleWhen' -v`

Expected: FAIL, bools only / apply always `true`.

**Step 3: Minimal implementation**

Mirror `settingsControl`: bool → toggle, int → slider, select → `NewMenu`, else `ui.NewField`. Store menus/fields on `PanelHost` keyed by `pluginID+"."+key` so Settings and the recorder panel can share the helper.

Change `handlePluginManager` to take the activated `*ui.Node` and decode:

- toggle → bool from `n.Value`
- slider → int from `n.Value`
- menu → selected option string
- text field → `n.Text`

Then `applySetting`. Rebuild the panel.

Honor `visible_when` via `plugin.SettingVisible`.

**Step 4: Run tests to verify they pass**

Run: `go test -race -count=1 ./internal/shell -run 'Plugin|Settings' -v`

Expected: PASS.

**Step 5: Commit**

```bash
git commit -m "$(cat <<'EOF'
feat(plugin): render plugin settings with the same controls Settings uses

EOF
)"
```

---

### Task 8: Wire the recorder command

**Files:**
- Modify: `cmd/sysc-plugin-screen-recorder/main.go`
- Test: `cmd/sysc-plugin-screen-recorder` if a test exists; otherwise `plugins/reference/recorder` client tests / `tests/integration/plugin_recorder_gate_test.go` only as needed for panel snapshots

**Step 1: Write the failing check**

An existing e2e that builds a bar snapshot and sends activate on `record` still starts recording. Add: activate on `camera` issues `panel.open` with `entry=panel`. Panel views get `PanelTree`. While Recording, a tick updates elapsed. Settings change still rebuilds the next command, not the current process.

If the command has no test file, add `main_test.go` that drives `run` with the helper fake recorder (same Options as integration) and asserts a `host.call` of `panel.open` after a camera event.

**Step 2: Run test to verify it fails**

Run: `go test -race -count=1 ./cmd/sysc-plugin-screen-recorder ./plugins/reference/recorder -v`

Expected: FAIL, panel views still get `BarTree` / camera is ignored.

**Step 3: Minimal implementation**

In `publish`, switch on `v.kind` (`ViewBar` / `ViewTooltip` / `ViewPanel`). Handle `camera` → `CallPanelOpen`. Record/Stop as today's toggle paths. Replay start/save from panel node ids. Tick once a second while not Idle so elapsed moves. `SettingsChanged` already reparses config.

**Step 4: Run tests to verify they pass**

Run: `go test -race -count=1 ./cmd/sysc-plugin-screen-recorder ./plugins/reference/recorder ./tests/integration -run PluginRecorderGate -v`

Expected: PASS. Existing fake-backend gate still green.

**Step 5: Commit**

```bash
git commit -m "$(cat <<'EOF'
feat(plugin): open the recorder panel and drive it from the command

EOF
)"
```

---

### Task 9: Live Niri check (do not claim done without it)

**Files:**
- Create: `docs/plans/2026-09-02-recorder-panel-handover.md` only after the live run
- Modify: `docs/plans/README.md` (register the handover in that same commit)

On the trusted laptop (`ssh -p 7777 nomadx@192.168.0.64`):

1. Build plugin + shell from the worktree. Do not `pkill -f` a name also in the agent shell; kill by pid from `pgrep -f 'scratchpad/'` if needed.
2. Enable `org.sysc.screen-recorder`. Confirm the pill shows the camera.
3. Left-click Record: a file appears under the configured directory using current settings.
4. Stop. Left-click camera: panel opens in the sysmon slot (centered off the bar, not mid-screen like the launcher).
5. Change directory or codec in the panel; confirm Settings → Plugins shows the same value; next Record uses it.
6. Enable replay in the panel; Start replay / Save replay still work.
7. `hide_inactive`: idle hides Record/Stop, camera remains; recording shows Stop.
8. `niri msg -j layers` while the panel is open. Escape / shield closes it. Disable reaps GSR.

If notifyd is still absent, record that as before. Close `sysc-136` only when this run is honest. Keep `sysc-130` open.

**Commit** the handover with the JSONL close line, surgically.

---

## Done when

- Camera / Record / Stop match D2–D7.
- Panel is the sysmon slot, header plus full schema, one store.
- Glyphs are in `sysc-icons.ttf` and addressable by name.
- `PluginRecorderGate` still passes.
- Live checklist above has been run, or the handover says which step did not.
