# Milestone 7 Wallpaper Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Ship the v1 wallpaper picker: a 980×1100 Exclusive overlay that lists images and videos, applies them through gSlapper (awww then swaybg if gSlapper is missing), and restores a still through the static fallback.

**Architecture:** `internal/wallpaper` owns library, per-connector assignments, and engine argv/IPC. A service goroutine publishes immutable snapshots. The shell gains `PanelWallpaper` that projects those snapshots onto the landed M4 panel vocabulary (`KindTextField`, `KindVirtualList` of 4-tile rows, `KindImage`) with no new node kind. Apply, pause, and restore never run on the Wayland owner.

**Tech Stack:** Go, existing panel host and IPC, Unix-socket gSlapper IPC, `exec` of `gslapper` / `awww` / `swaybg` / optional `gst-launch-1.0`. No new module pin.

**Design:** `docs/plans/2026-09-03-wallpaper-design.md` (decisions D1–D20)

**Audit:** `docs/plans/2026-09-03-wallpaper-audit.md`. Pins below are the
accepted subset (Task 0 bead, Task 2 All token, Task 3 `SplitN`, Task 4 `-r`,
Task 8 `cfgHook`, Task 9 spawn stub). Dirty-tree and one-output notes stay
operational, not plan text.

**Amended 2026-09-04** (Task 0 Step 5, reconciliation against landed `main`).
Seven corrections; none reopens the design's chrome, engine split, or
lifecycle:

1. **Tasks 9a and 9b are new**, inserted before Task 10. `KindImage` measures
   a square (`internal/ui/layout.go` `case KindImage: return size, size`) and
   every landed user is a square icon, so the design's 210x96 landscape thumb
   has no expression in the current node vocabulary. Task 9a adds one; Task 9b
   gives the decoder a non-square target. Task numbering past 9 is unchanged
   so the handover's "Task 13 gate" still names the gate.
2. **Tile radius comes from the theme, not the literal 8** in the design's
   geometry table. `Theme.CardRadius` is user-configurable through
   `cfg.Theme.Radius`, and `KindCapsule` with `FillContainerHigh` already
   resolves to it (`internal/render/paint.go` `case ui.KindCapsule`). The tile
   is a capsule around the raster; the raster itself is not rounded, because
   `paintImage` has no radius clip and adding one is a render change this
   slice does not need.
3. **Cover-crop happens in the thumbnailer, not the painter.** `paintImage`
   scales the raster to fill its box with no aspect preservation, so the
   cached JPEG must already be cropped to the tile aspect. The design's cache
   (224x126, 1.78:1) and its tile (210x96, 2.19:1) disagree; the cache is
   restated as **210x96** so one aspect governs.
4. **Task 12 gains output hotplug.** D20's disconnect/reconnect rule was
   tested at store level in Task 2 but nothing routed real output events to
   the service, which left D20 implemented and dead.
5. **Repo-wide `-race` is removed from Steps 0.2 and 12.4.** Combining
   `./...` with `-race` hard-locked this machine twice on 2026-09-03 (zram-only
   swap, 16-way link parallelism). Capped per-package runs replace it.
6. **`hidden = none` keeps its default, and the cost is now explicit.**
   gSlapper's `--auto-stop` is documented as "required for video IPC changes",
   so the design's default makes every video-to-video swap take D15's
   stop-and-relaunch path rather than the `change` path. That is a real
   CPU-versus-continuity tradeoff and stays the owner's call; Task 6 records
   it and Task 4 tests each branch.
7. **The Pause affordance follows gSlapper's reported kind, not ours.**
   gSlapper lists GIF under *Image* formats while D10 classifies it as video.
   Library classification still drives the tile; the active strip's
   Pause/Resume follows the kind `query` reports, so a GIF never shows a
   control its pipeline cannot honour.

---

### Task 0: Reconcile the plan with landed main

**Files:**
- Read: `docs/plans/2026-09-03-wallpaper-design.md`
- Inspect: `internal/shell/panel.go`, `internal/shell/panelhost.go`, `internal/shell/popout_launcher.go`, `internal/ui/tree.go`, `internal/ipc/server.go`, `internal/config/config.go`, `internal/config/load.go`, `internal/shell/registry.go`, `internal/theme/generate.go`, `internal/icons/worker.go`

**Step 1:** Confirm `sysc-149` is in progress (`bd show sysc-149` from
`/home/nomadx/sysc-shell`). `sysc-146` is the closed design bead. Branch
`milestone/wallpaper` from a clean `main`; do not carry the notification-centre
or toast working tree into this slice.

**Step 2:** Run `GOMAXPROCS=4 go test -count=1 -p 2 ./...`. Record any
pre-existing failure in bd instead of weakening a wallpaper check.

Never combine `./...` with `-race`: it hard-locked this machine twice on
2026-09-03 and there is no disk swap to absorb the spike. Race coverage comes
from capped per-package runs (`GOMAXPROCS=4 go test -race -count=1 ./internal/wallpaper`),
which is what every `-race` step below means.

**Step 3:** Verify the APIs this plan names still exist: `PanelID` / `parsePanelName` / `panelTargetSize` / `panelTree`, `Placement.CenterY`, `ui.Field`, `KindVirtualList` `ItemCount`/`ItemHeight`, `KindImage`, IPC `knownPanels`, `Registry.generateTheme`.

**Step 4:** Confirm live outputs with `niri msg -j outputs` still include DP-1 and DP-3. If not, record the new connectors in bd; tests keep the `DP-1`/`DP-3` fixture names.

**Step 5:** Commit only a plan correction if reconciliation changed this document. Otherwise make no commit.

### Task 1: Classify media and own the socket path

**Files:**
- Create: `internal/wallpaper/media.go`
- Test: `internal/wallpaper/media_test.go`

**Step 1:** Write the failing table test:

```go
func TestClassifyName(t *testing.T) {
	cases := []struct {
		name string
		kind Kind // Image, Video, or 0
	}{
		{"a.jpg", KindImage},
		{"a.JPEG", KindImage},
		{"a.png", KindImage},
		{"a.webp", KindImage},
		{"a.jxl", KindImage},
		{"a.bmp", KindImage},
		{"a.gif", KindVideo},
		{"a.mp4", KindVideo},
		{"a.mkv", KindVideo},
		{"a.webm", KindVideo},
		{"a.mov", KindVideo},
		{"a.avi", KindVideo},
		{"a.m4v", KindVideo},
		{"a.txt", 0},
		{"noext", 0},
	}
	// ...
}

func TestSocketPath(t *testing.T) {
	got := socketPath("/run/user/1000/sysc-shell", "DP-1")
	if got != "/run/user/1000/sysc-shell/gslapper-DP-1.sock" {
		t.Fatalf("got %q", got)
	}
	if socketPath("/run/user/1000/sysc-shell", "HDMI-A-1") == got {
		t.Fatal("connectors must not share a socket")
	}
	if socketPath("/tmp", "DP-1; rm -rf /") != "/tmp/gslapper-DP-1-rm--rf-.sock" {
		t.Fatal("sanitize connector to [A-Za-z0-9._-]")
	}
}
```

**Step 2:** Run `go test -count=1 ./internal/wallpaper -run 'TestClassifyName|TestSocketPath' -v`. Expected: compile failure, package does not exist.

**Step 3:** Implement `Kind`, `ClassifyName`, `SanitizeConnector`, `socketPath`. No I/O.

**Step 4:** Run the same test. Expected: PASS.

**Step 5:** Commit `feat(wallpaper): classify media and name owned sockets`.

### Task 2: Assignment store and generations

**Files:**
- Create: `internal/wallpaper/assign.go`
- Test: `internal/wallpaper/assign_test.go`

**Step 1:** Write the failing table test that is the plugin self-check, ported:

- Independent `DP-1` image and `DP-3` video
- `Apply` takes the output-select token (`"all"` or a connector). `"all"`
  expands over a fixed `[]string{"DP-1","DP-3"}` fixture inside the store /
  service, not in the panel
- Stale generation ignored (`Apply` with gen 1 after gen 2 exists is a no-op)
- Disconnect keeps the assignment, clears runtime
- Reconnect restores the saved assignment as a desired apply
- Partial All: DP-3 fails, DP-1 succeeds, DP-3 keeps prior assignment
- Theme seed: image path; video with still; video without still leaves previous seed

Keep the store a plain struct with methods. No engine, no goroutine.

**Step 2:** Run `go test -count=1 ./internal/wallpaper -run TestAssign -v`. Expected: FAIL.

**Step 3:** Implement `Store`, `Assignment`, `Runtime`, `Apply`, `Disconnect`, `Reconnect`, `SeedPath`. `Apply` accepts `"all"` or one connector; expansion uses the current connector list. Generation is per connector, uint64, incremented at the start of each requested apply.

**Step 4:** Run `go test -race -count=1 ./internal/wallpaper -run TestAssign -v`. Expected: PASS.

**Step 5:** Commit `feat(wallpaper): per-connector assignment store`.

### Task 3: gSlapper IPC client

**Files:**
- Create: `internal/wallpaper/ipc.go`
- Test: `internal/wallpaper/ipc_test.go`

**Step 1:** Write failing tests against a `net.ListenUnix` fixture (copy the Waytrogen shape, in Go):

- `query` writes `query\n` and parses `STATUS: playing image /wallpapers/space name.png`
- `ParseStatus` splits with `strings.SplitN(line, " ", 4)` so a path with
  spaces is the fourth field
- `STATUS: paused video /tmp/a.mp4` sets paused + video
- `ERROR: no pipeline` returns that error
- Empty reply is an error
- Commands containing `\n` are rejected before dial
- `classifyChangeError("cannot update path (use --auto-stop for video changes)")` is restart; other errors are keep
- A `change` reply is accepted on the `OK` prefix: the IPC docs return bare
  `OK` with transitions off and `OK: transition started` with them on, so an
  exact-match accept would fail the moment the fade setting is enabled

Read one line. Do not wait for EOF (gSlapper keeps the socket open).

**Step 2:** Run `go test -count=1 ./internal/wallpaper -run 'TestIPC|TestClassifyChange' -v`. Expected: FAIL.

**Step 3:** Implement `Request(socket, command, timeout)`, `ParseStatus`, `classifyChangeError`. Timeouts: 2s default, 6s for pause/resume.

**Step 4:** Run the tests. Expected: PASS.

**Step 5:** Commit `feat(wallpaper): gSlapper line IPC`.

### Task 4: Launch argv and capability probe

**Files:**
- Create: `internal/wallpaper/gslapper.go`
- Test: `internal/wallpaper/gslapper_test.go`

**Step 1:** Write failing tests:

- `helpSupports([]byte("--ipc-socket PATH --transition-type TYPE --cache-size MB"))` true; old help without `--cache-size` false
- `launchArgs` for DP-1, fill, loop, fps 30, fade off, socket `/run/user/1000/sysc-shell/gslapper-DP-1.sock`, path `/tmp/a.mp4` contains `-I`, that socket, `--no-save-state`, `-o` with `fill no-audio loop`, `-r`, `30` (short form, not `--fps-cap`), connector `DP-1`, path. No `*`. Hidden auto-pause adds `--auto-pause`; auto-stop adds `--auto-stop`;
never each together.
- Fade on emits `--transition-type fade` and `--transition-duration <secs>`;
  fade off emits neither (gSlapper defaults to `none`). There is no `--fade`
  flag on the binary.
- `videoChangeNeedsRestart(hidden)` is true for `none` and `auto-pause`, false
  for `auto-stop`. gSlapper documents `--auto-stop` as "required for video IPC
  changes", so this predicate is what decides whether a video-to-video apply
  takes the `change` path or D15's stop-and-relaunch path. Test each branch.

**Step 2:** Run `go test -count=1 ./internal/wallpaper -run 'TestHelpSupports|TestLaunchArgs' -v`. Expected: FAIL.

**Step 3:** Implement settings struct + argv builder + help probe. Do not `exec` in this task.

**Step 4:** Run the tests. Expected: PASS.

**Step 5:** Commit `feat(wallpaper): gSlapper launch argv`.

### Task 5: Static fallback argv

**Files:**
- Create: `internal/wallpaper/fallback.go`
- Test: `internal/wallpaper/fallback_test.go`

**Step 1:** Write failing tests:

- `awwwImgArgs(path, "DP-1")` is `awww img --outputs DP-1 <path>`
- All-outputs is not a thing at this layer; the service calls once per connector
- `swaybgArgs(path, "DP-1")` is `swaybg -o DP-1 -i <path> -m fill`
- `pickFallback("awww", "swaybg")` prefers awww when the name is on PATH (inject a lookup func); neither → error

**Step 2:** Run `go test -count=1 ./internal/wallpaper -run 'TestAwww|TestSwaybg|TestPickFallback' -v`. Expected: FAIL.

**Step 3:** Implement argv + lookup. No spawn.

**Step 4:** Run the tests. Expected: PASS.

**Step 5:** Commit `feat(wallpaper): awww and swaybg argv`.

### Task 6: Config section and assignment file

**Files:**
- Modify: `internal/config/config.go`, `internal/config/load.go`, `internal/config/write.go`, `internal/config/config_test.go`
- Create: `internal/wallpaper/persist.go`
- Test: `internal/wallpaper/persist_test.go`

**Step 1:** Failing config test: missing `wallpaper` block inherits

```
image_directory = ~/Pictures/Wallpapers
video_directory = ~/Videos/Wallpapers
scale = fill
loop = true
fps = 30
fade = false
fade_duration = 0.5
hidden = none
```

Unknown scale/fps/hidden fail load. Seed path is still `theme-gen`, not this
block.

`hidden = none` is deliberate and keeps D19's vocabulary: playback continues
when the wallpaper is occluded. The cost is that video-to-video swaps restart
the process instead of using `change` (Task 4's `videoChangeNeedsRestart`).
Changing the default to `auto-stop` buys smooth swaps and costs playback
continuity on reveal; that is the owner's tradeoff, not this plan's. Assert
the default is `none` so a later flip is a deliberate test edit.

Failing persist test: round-trip `assignments.json` mode 0600; reject a path with a newline.

**Step 2:** Run the new tests. Expected: FAIL.

**Step 3:** Add `Wallpaper` on `config.Config`, wire decode/encode, persist load/save under `$XDG_STATE_HOME/sysc-shell/wallpaper/assignments.json`.

**Step 4:** `gofmt -w . && go test -race -count=1 ./internal/config ./internal/wallpaper`. Expected: PASS. `git diff --exit-code -- go.mod go.sum`.

**Step 5:** Commit `feat(wallpaper): config defaults and assignment file`.

### Task 7: Directory index

**Files:**
- Create: `internal/wallpaper/library.go`
- Test: `internal/wallpaper/library_test.go`

**Step 1:** Write a failing test over `t.TempDir()`:

- Image root with `a.jpg`, `sub/b.png`, `c.gif`, `skip.txt` → current dir lists `a.jpg` (image), `c.gif` (video), directory `sub`; not `skip.txt`
- Navigate into `sub` lists `b.png`; Up returns
- Non-UTF-8 filename skipped (write a `[]byte` name if the OS allows; otherwise skip that row with a comment)
- Unreadable root → error string, empty list

Do not recurse into the grid. Child dirs are entries, not flattened files (D9).

**Step 2:** Run `go test -count=1 ./internal/wallpaper -run TestLibrary -v`. Expected: FAIL.

**Step 3:** Implement a stdlib walk that builds an in-memory index once; `View(root, dir, filter, search)` returns the current page of media + child dirs. Filter `all|images|videos`. Search is a case-insensitive substring on the filename.

**Step 4:** Run the test. Expected: PASS.

**Step 5:** Commit `feat(wallpaper): directory index`.

### Task 8: Service goroutine and snapshot

**Files:**
- Create: `internal/wallpaper/service.go`
- Test: `internal/wallpaper/service_test.go`

**Step 1:** Write a failing test with a fake engine (interface, one implementation in tests):

- `Apply` on DP-1 publishes a snapshot with that assignment
- A second `Apply` with a slower fake that finishes after a newer one does not commit (generation)
- `Disconnect("DP-3")` then `Reconnect("DP-3")` asks the fake to apply the saved path
- Commands arrive on a channel; `Snapshot()` is immutable
- `Service` has a `cfgHook func(source, seed string)` field. After a successful
  image apply (or a video apply that produced a still) the test recorder sees
  `("wallpaper", path)`. A video apply with no still does not call the hook.

The fake must not touch Wayland. `exec` stays behind the engine interface so Task 9 can fill it in. The registry later sets `cfgHook` to write `ThemeGen.Source`/`Seed` and call `generateTheme`.

The hook updates the registry's in-memory `config.Config` and regenerates the
palette; it does **not** rewrite the user's config file. Startup reconcile
(Task 12) replays the saved assignment, which re-runs the hook, so the seed
survives a restart without the shell editing a file the user owns. Task 13
reads the seed off the live theme, not off disk.

**Step 2:** Run `go test -race -count=1 ./internal/wallpaper -run TestService -v`. Expected: FAIL.

**Step 3:** Implement `Service` with `Updates() <-chan Snapshot`, `Enqueue(Command)`, `cfgHook`, one loop goroutine. `ponytail:` one mutex on the store is enough; per-connector locks if two-output apply latency shows contention.

**Step 4:** Run the race test. Expected: PASS.

**Step 5:** Commit `feat(wallpaper): service snapshots`.

### Task 9: Real engines (exec + IPC wait)

**Files:**
- Modify: `internal/wallpaper/gslapper.go`, `internal/wallpaper/fallback.go`
- Test: `internal/wallpaper/engine_test.go`

**Step 1:** Write a failing test that injects spawn. The engine holds:

```go
spawn func(argv []string) (Process, error)

type Process interface {
	Wait() error
	Stop() error
}
```

Cases:

- No socket → `spawn` is called with `launchArgs`, poll `query` until OK or 3s
- Child exits before ready (`Wait` returns, `query` never succeeds) → error, `Stop` called
- Existing socket + `query` OK → `change`; auto-stop error → `Stop`, then `spawn` once
- Restore → `stop` IPC, wait for socket file gone, then fallback argv
- Never builds a `pkill` argv

Do not talk to a real `gslapper` in unit tests.

**Step 2:** Run `go test -count=1 ./internal/wallpaper -run TestEngine -v`. Expected: FAIL.

**Step 3:** Implement spawn (own process group, stdin/stdout/stderr nil), `waitForQuery`, `stopOwned`, fallback start recording pid on `Runtime`. Probe PATH for gslapper/awww/swaybg at service start for `Capabilities`.

**Step 4:** Run the test. Expected: PASS.

**Step 5:** Commit `feat(wallpaper): launch, change, stop, restore`.

### Task 9a: Landscape raster boxes

A wallpaper thumbnail is the first non-square raster in this tree. Every landed
`KindImage` is a square icon (`bar.go`, `notifycard.go`, `traydrawer.go`,
`popout_launcher.go`), and both measure paths return `size, size`. Without this
task the design's 210x96 tile thumb can only render squashed into a square box.

**Files:**
- Modify: `internal/ui/tree.go`, `internal/ui/layout.go`, `internal/ui/column.go`
- Test: `internal/ui/layout_test.go` (or the file that already measures kinds)

**Step 1:** Write the failing test:

- A `KindImage` with `ImageW: 210, ImageH: 96` measures 210x96 and reports
  row height 96
- A `KindImage` with only `ImageSize: 40` still measures 40x40, so the four
  landed square users are untouched
- `ImageW` set without `ImageH` (or the reverse) falls back to the square
  `ImageSize` rather than measuring zero

**Step 2:** Run `go test -count=1 ./internal/ui -run TestImage -v`. Expected: FAIL.

**Step 3:** Add `ImageW`/`ImageH` beside `ImageSize` on `ui.Node`, documented as
the landscape form that overrides the square edge when both are positive. Teach
`measureNode` and the column height path to prefer them. `paintImage` already
scales the raster into whatever box it is given, so `internal/render` needs no
change.

**Step 4:** Run `go test -count=1 ./internal/ui ./internal/render ./internal/shell`. Expected: PASS.

**Step 5:** Commit `feat(ui): measure landscape image boxes`.

### Task 9b: Non-square decode and a thumbnail worker

`internal/icons.Worker` is the landed off-owner decode with a bounded cache,
in-flight collapsing, and a resolver that already returns absolute paths
verbatim. It is the right machinery and the wrong instance: its `Key.Size` is
one int, and its caps (`MaxCacheEntries` 256, `MaxQueue` 32) are tuned for
icons. Pointing a several-hundred-file wallpaper library at the shared
`r.trayIcons` would evict every tray and notification icon and starve their
decodes, so wallpaper gets its own instance of the same worker.

**Files:**
- Modify: `internal/icons/worker.go`, `internal/icons/theme.go`, `internal/icons/*_test.go`
- Modify call sites: `internal/shell/tray.go`, `internal/shell/popout_launcher.go`, `internal/shell/popout_notifications.go`

**Step 1:** Write the failing test:

- `Key{Name: abs, W: 210, H: 96}` decodes to a 210x96 raster
- A square request (`W == H`) is byte-identical to what the old `Size` form produced
- `Request` still rejects a key with a non-positive dimension

**Step 2:** Run `go test -count=1 ./internal/icons -v`. Expected: FAIL.

**Step 3:** Replace `Key.Size` with `Key.W`/`Key.H`; thread each through
`Resolve` (which uses the size only for theme-directory lookup, so pass the
larger edge) and `decodeRaster`. Update the three shell call sites to the square
form. This is mechanical and compiler-checked.

**Step 4:** Run `go test -count=1 ./internal/icons ./internal/shell`. Expected: PASS.

**Step 5:** Commit `feat(icons): decode non-square rasters`.

### Task 10: Panel id, size, IPC, tree

**Files:**
- Modify: `internal/shell/panel.go`, `internal/shell/panelhost.go`, `internal/ipc/server.go`, `internal/ipc/server_test.go`
- Create: `internal/shell/popout_wallpaper.go`
- Test: `internal/shell/popout_wallpaper_test.go`

**Step 1:** Write failing tests modeled on `popout_launcher_test.go`:

- `parsePanelName("wallpaper")` → `PanelWallpaper`
- `panelTargetSize` is 980×1100
- `OpenPanel` on 1920×1080 top bar: `CenterY` true, overlay Exclusive, shield present
- Tree has a search field and a `KindVirtualList`
- IPC `knownPanels` includes `wallpaper`

**Step 2:** Run `go test -count=1 ./internal/shell -run TestWallpaper -v` and `./internal/ipc -run TestPanel`. Expected: FAIL.

**Step 3:** Add the panel id, size, `CenterY`, `h.search`, `wallpaperTree` projecting the last snapshot (empty library is fine). Wire `parsePanelName` and IPC. Row height: 96 thumb + caption ≈ 128. ItemCount = ceil(n/4). Do not apply wallpapers yet.

**Step 4:** Run the tests. Expected: PASS.

**Step 5:** Commit `feat(shell): wallpaper panel chrome`.

### Task 11: Project tiles, keys, apply, restore

**Files:**
- Modify: `internal/shell/popout_wallpaper.go`, `internal/shell/panelhost.go`, `internal/shell/registry.go`
- Test: `internal/shell/popout_wallpaper_test.go`

**Step 1:** Extend the panel test with a stub service:

- Four image paths → one virtual-list row of four tiles; click/Enter enqueues `assign-image`
- Arrow right moves selection inside the row; down moves a row
- Output select All vs DP-1 sends `Apply("all", path)` vs `Apply("DP-1", path)`
- Restore enqueues `restore`
- The `n / m` partial caption and the active strip's mixed summary are read
  back from the store, not composed in the panel: a two-output snapshot with
  one video and one image renders `2 outputs - 1 video - 1 image`, and
  changing the snapshot changes the string
- Theme seed: registry installs `cfgHook`; after a successful image apply the
  next `generateTheme` sees `Source=wallpaper` and that path (recorder hook,
  not matugen)

**Step 2:** Run the test. Expected: FAIL.

**Step 3:** Project each tile as a `KindCapsule` (`Width: 210`,
`Fill: FillContainerHigh`) around a column of `KindImage` (`ImageW: 210`,
`ImageH: 96`) plus caption. The capsule supplies the tile chrome and takes its
radius from `Theme.CardRadius`, so the tile follows the user's configured
radius instead of the design table's literal 8. Handle keys next to the
launcher branch in `panelhost.go` (do not fork the Exclusive path).
`relayWallpaper` mirrors `relayLauncher`: receive snapshot, rebuild panel under
`Registry.mu`.

Thumbnails: the tile builder runs inside `Item(i)`, which layout calls **on the
Wayland owner**, so it may only `Lookup` an already-decoded raster and
otherwise `Request` and fall back to a glyph. Decoding, `gst-launch-1.0`, and
the disk cache all stay off the owner. Use a wallpaper-owned `icons.Worker`
(Task 9b), never the shared `r.trayIcons`. Cached stills are written already
cropped to 210x96 so `paintImage`'s stretch is a no-op; the cache key stays
path+mtime. A late decode publishes and the next snapshot picks it up.

**Step 4:** `gofmt -w . && go vet ./... && go test -race -count=1 ./internal/shell ./internal/wallpaper`. Expected: PASS.

**Step 5:** Commit `feat(shell): wallpaper apply and restore`.

### Task 12: Startup reconcile and bar item

**Files:**
- Modify: `internal/shell/registry.go` (or the construct path that already starts notify/tray relays), `internal/config/config.go` (`knownItems`)
- Test: `internal/shell` or `internal/config` as appropriate

**Step 1:** Failing tests:

- Construct with a saved DP-1 assignment and a fake engine that records `Apply` → one apply on start
- Output arrival for a connector with a saved assignment replays it; arrival
  for an unknown connector applies nothing (D20: a newly seen output stays
  untouched until the user assigns)
- Output removal calls `Disconnect`, which keeps the assignment and drops the
  runtime
- `knownItems` accepts `wallpaper`; default bar layout is unchanged

**Step 2:** Run the tests. Expected: FAIL.

**Step 3:** Start the wallpaper service when the registry starts (not lazy: D20 startup reconcile). Clicking a bar item `wallpaper` toggles the panel (same path as other panel items). Do not add it to `Default()`.

Wire real output events into the service: `Registry.NewHost(global, connector)`
is the arrival seam and the bar-removal path is the departure seam. Both hold
`Registry.mu`, so each one enqueues on the service rather than calling into it
inline. Without this, D20 is tested in Task 2 and dead in the product.

**Step 4:** Run `GOMAXPROCS=4 go test -count=1 -p 2 ./...`, then
`GOMAXPROCS=4 go test -race -count=1 ./internal/wallpaper ./internal/shell`.
Expected: PASS. Do not combine `./...` with `-race` (Step 0.2).

**Step 5:** Commit `feat(shell): restore wallpapers at start`.

### Task 13: Live Niri gate

**Files:**
- Modify: `tests/integration/README.md` (add a Milestone 7 wallpaper subsection; do not invent a second matrix file)

**Step 1:** Export the compositor environment:

```bash
export NIRI_SOCKET=$(ls /run/user/1000/niri.wayland-*.sock | head -1)
export WAYLAND_DISPLAY=wayland-1
export XDG_RUNTIME_DIR=/run/user/1000
```

**Step 2:** Run a scratchpad `sysc-shell`. Kill by pid from `pgrep -f 'scratchpad/'`, never `pkill -f`.

**Step 3:** Record:

- `niri msg -j layers` shows the wallpaper panel when toggled
- Image on DP-1, video on DP-3 (or the live connector names)
- All applies both
- Pause / Resume
- Restore stops the owned gSlapper socket and shows the still via awww or swaybg
- `theme.seed` in the written config (or the live theme) is the still or image
- `pgrep -af gslapper` command lines contain `$XDG_RUNTIME_DIR/sysc-shell/gslapper-`
- A GIF applies, and `query` is recorded verbatim. gSlapper documents GIF as an
  image format while D10 classifies it as video; whatever `query` reports is
  what the active strip offers, so record whether Pause appears and whether it
  answers `OK`
- A video-to-video swap on one output is recorded twice: once at the default
  `hidden = none` (expect D15's stop-and-relaunch) and once at
  `hidden = auto-stop` (expect a plain `change`). This is the only live proof
  of Task 4's `videoChangeNeedsRestart` branch

**Step 4:** If a check is unrunnable, record why in bd (`sysc-146` discovered-from), keep the overlay. Do not claim the slice done.

**Step 5:** Commit `docs(wallpaper): record the live Niri gate` only if the README subsection landed. No product commit that skips the gate.

---

Stop when Task 13's runnable checks pass. Do not pull favorites, flatten, `*` coalescing (`sysc-148`), or desktop widgets.
