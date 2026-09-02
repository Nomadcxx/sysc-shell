# Milestone 7 Wallpaper — Design

Date: 2026-09-03. Status section intentionally absent — status lives in bd (`sysc-146`).

Prior art (read, not imported):

- Noctalia 5 wallpaper panel `/home/nomadx/noctalia/src/shell/wallpaper/panel/`
  (980×700, virtualized grid, images and solid color only)
- noctalia-gslapper `/home/nomadx/noctalia-gslapper/gslapper/` (combined
  image+video picker, 980×1100, paginated because the plugin API cannot
  virtualize)
- DMS v1.5.3 DankDash `WallpaperTab.qml` (4×4 paginated stills, built-in
  Background layer; video/gSlapper only in an older GitHub tree)
- Waytrogen `src/changers/gslapper.rs` as merged to
  `nikolaizombie1/waytrogen` PR #61 (owned sockets, query-before-ready,
  `change` then restart, no `pkill` by binary name)
- gSlapper IPC `/home/nomadx/gSlapper/docs-site/content/docs/user-guide/ipc-control.mdx`
- This machine, 2026-09-03: Niri outputs **DP-3** 2560×1440 and **DP-1**
  3440×1440. The agent-guide one-output line is stale for this slice.

Roadmap amendment: `docs/roadmap.md` currently says the wallpaper slice is a
socket client and does not own gSlapper's lifecycle. This design overrules
that line. The shell launches gSlapper and owns the sockets.

## Goal and scope

One first-party wallpaper picker panel: combined image and video library,
click applies, gSlapper as the engine for stills and video when installed,
awww then swaybg as static-only fallback when it is not. Chrome is the
noctalia-gslapper picker with native Noctalia's virtualized grid instead of
page buttons.

Out of this design: a shell-owned Background wallpaper surface, pagination,
favorites, flatten, color wallpaper, a theme-source row in the panel, plugin
host implementation, a sibling wallpaper daemon, lock-screen wallpaper,
launcher wallpaper provider, awww transition chrome, supervising a gSlapper
the user started on a socket we do not own.

## Decisions

### Placement

- **D1 — First-party M7 panel, not a plugin and not a daemon.** New
  `PanelWallpaper` on the existing panel host, IPC name `wallpaper`. Same
  surface class as the launcher: Overlay, Exclusive, dismiss shield, single
  instance. Package `internal/wallpaper` owns library, assignments, and
  engines. Relays stay off the Wayland owner.
- **D2 — Size 980×1100, clamped.** Plugin size, not native 980×700. Returned
  from the existing `panelTargetSize` switch. Placement reuses the M4
  floating clamp (centered on the focused output). On a short output the
  clamp shrinks it.
- **D3 — Bar glyph opens the panel.** Default glyph `wallpaper-selector`.
  Settings can grow a "Open panel" row later; not v1 chrome.

### Chrome

- **D4 — Plugin chrome, native grid.** Top to bottom: title + close; search +
  output select (All / connector names); All / Images / Videos + Up when
  nested; folder strip (selector, not tiles); capability / scan / apply
  banners; active strip; virtualized 4-column tile grid; media count. No
  page buttons. Settings glyph in the title row may open the wallpaper
  config later; v1 can omit it if settings has no wallpaper section yet.
- **D5 — No native extras.** No favorites, flatten, solid-color wallpaper, or
  Builtin/Wallpaper/Dark/Light theme row. Those stay with Appearance
  settings and later slices.
- **D6 — Tiles.** Cover thumb, radius 8, one-line filename. Videos: `VIDEO ·`
  prefix. Current assignment: primary border. All-outputs partial match:
  `n / m` on the caption. Missing thumb: `photo` / `player-play` glyph.
  Video tiles with gSlapper absent: opacity 0.55, not activatable. Click or
  Enter applies immediately. Arrow keys move in the 4-column grid. Escape
  closes.
- **D7 — Active strip.** Single output: filename, playing / paused / static /
  error, Pause/Resume (video only), Restore. All outputs: mixed summary
  (`2 outputs · 1 video · 1 image`), Pause videos, Restore all.
- **D8 — Grid primitive: row-virtualized, not KindVirtualGrid.**
  `KindVirtualList` of rows of four tiles. Each tile is `KindImage` plus
  caption. No new node kind. Pagination was a noctalia-gslapper constraint
  (non-virtualized plugin scroll), not a product preference.

### Library

- **D9 — Two roots.** `wallpaper.image_directory` default
  `~/Pictures/Wallpapers`; `wallpaper.video_directory` default
  `~/Videos/Wallpapers`. Same path is allowed (mixed root). Panel shows the
  current directory's media and immediate child directories. No flatten.
- **D10 — Extensions.** Images: `jpg jpeg png webp jxl bmp`. Videos: `mp4 mkv
  webm mov avi m4v gif`. GIF is always video (plugin rule). gSlapper plays
  it; native Noctalia treated GIF as a still — we do not.
- **D11 — Index and thumbs off the Wayland owner.** Recursive index at
  startup and on refresh; opening the picker does not rescan. Cached JPEG
  previews, 224×126, path+mtime keyed under
  `$XDG_CACHE_HOME/sysc-shell/wallpaper/`. `gst-launch-1.0` is optional;
  failure is a placeholder, not a blocked apply. Skip non-UTF-8 names.

### Engine

- **D12 — gSlapper-first.** Probe `gslapper --help` for `--ipc-socket` (and
  `--transition-type`, `--cache-size` — Waytrogen 1.5 check). If present:
  stills and video go through gSlapper. If absent: video tiles disable with
  one banner; static uses awww, then swaybg.
- **D13 — Shell launches, shell owns the sockets.** Overrules the roadmap
  "socket client only" line. One process per connector. Socket
  `$XDG_RUNTIME_DIR/sysc-shell/gslapper-<sanitized-connector>.sock`.
  Sanitise to `[A-Za-z0-9._-]`. Launch with `-I`, `--no-save-state`,
  `no-audio`, configured scale / loop / fps / fade, connector name, path.
  Own process group. Wait until `query` succeeds — the socket file existing
  is not ready (sysc-greet). After ready, IPC owns the lifecycle; no pid
  registry unless a later measurement needs non-IPC recovery.
- **D14 — Fan-out, never gSlapper `*`.** All outputs is N independent applies
  over currently connected connectors. Partial success is allowed. Waytrogen
  PR #61 maps All to `*` and stops overlapping sockets; that blanks the
  other output when the user then assigns DP-1. The picker chrome (partial
  badges, per-output pause) is assignment-per-connector. Wildcard coalescing
  is later work, recorded in bd, not v1. This machine can live-test two
  outputs.
- **D15 — Apply path, generation per connector.** Reject non-UTF-8, newlines,
  missing files. For video, reuse or generate a still. Write `theme.source
  = wallpaper` and `theme.seed` to the image or still; leave the seed when a
  video has no still; rerun the existing matugen generator. Stop a fallback
  **we started** on that output. If our socket answers `query`, send
  `change <path>`. On `cannot update path (use --auto-stop for video
  changes)`, stop and relaunch. If no socket, launch. Stale generation is
  ignored. Persist then publish.
- **D16 — Restore is the gSlapper-first exception.** Restore IPC-stops that
  output's owned gSlapper, waits for our socket to vanish, then applies the
  last still through awww (start `awww-daemon` if needed) or swaybg `-o
  <connector>`. No still: stop and leave empty. Restore all = that per
  connector. Pause/Resume remain gSlapper IPC and are video-only.
- **D17 — Never pkill by binary name.** Stop by owned socket (`stop` /
  `quit`), wait for the socket to disappear, then a last-resort match
  **only** on processes whose command line contains our socket path. The
  agent shell and a user-session gSlapper we do not own must never match.
  Do not `pkill swaybg` / `pkill awww-daemon` either; stop fallbacks we
  started, by pid recorded in the assignment runtime.
- **D18 — Foreign wallpaper processes.** If a gSlapper/awww/swaybg we did
  not start is covering the output, do not kill it. If our `query` never
  comes up, show an inline banner. gSlapper itself already warns about
  competing layer-shell wallpapers.

### Persistence and outputs

- **D19 — Assignments in state, directories in config.** Assignments JSON at
  `$XDG_STATE_HOME/sysc-shell/wallpaper/assignments.json` mode 0600:

  ```text
  connector -> {
      kind: "image" | "video",
      path: absolute UTF-8 path,
      preview_path: optional still,
      desired_playback: "playing" | "paused"
  }
  ```

  Runtime (not persisted): `static | starting | playing | paused | error`,
  socket path, fallback pid if we started awww/swaybg. Config holds the two
  directories and gSlapper playback settings (scale fill/stretch/original/
  panscan, loop, fps 30/60/100, fade, fade duration, hidden none/auto-pause/
  auto-stop). Hidden maps to `--auto-pause` / `--auto-stop` / neither, never
  both.
- **D20 — Connector names, not serials.** Disconnect stops the process and
  keeps the assignment. Reconnect replays it. Startup reconciles connected
  outputs against the file. A newly seen connector stays untouched until
  the user assigns. Review assignments after a GPU or dock rename.

## Architecture

```text
bar glyph / IPC
        │
        ▼
 PanelWallpaper  ──reads──►  snapshot (library, assignments, runtime, caps)
        │
        │ commands (nonce + generation)
        ▼
 internal/wallpaper.Service   (own goroutine)
        │
        ├── index / thumbs
        ├── engine.GSlapper  (owned sockets)
        ├── engine.Awww
        └── engine.Swaybg
                │
                ▼
     persist assignments.json
     request theme regen (seed path)
     publish snapshot
```

The Wayland owner never `exec`s, never `UnixStream.Connect`s, never waits on
startup. It submits commands and receives snapshots, the same shape as
notify/tray clients.

`rebuildPanel` already holds `Registry.mu` and calls the unlocked form.
Wallpaper `configure`/`render`/`handle` take that lock. The service does not.

## UI geometry (logical px, then clamped)

| Constant | Value |
|---|---|
| Panel | 980×1100 |
| Tile column width | 210 |
| Thumb height | 96 |
| Tile radius | 8 |
| Grid columns | 4 |
| Grid gap | 10 |
| Inner padding | 16 |
| Search field | flex, existing `KindTextField` height |
| Output select min width | 170 |

Four columns at 210 + 3×10 gap + 32 padding = 902, inside 980.

## Errors

- Missing gSlapper: one banner, video tiles inert, static apply still works.
- Missing awww and swaybg on a static fallback apply or Restore-with-still:
  inline error. After Restore, gSlapper is already stopped.
- Apply to All: `Applied to 1 of 2 outputs` plus per-connector errors.
  Failed outputs keep the previous assignment.
- Scan unreadable: banner if some media exist, empty-state if none.
- Theme regen failure: existing matugen fallback palette; wallpaper apply
  still commits.
- IPC `ERROR:` other than the auto-stop restart string: keep previous
  assignment, show the error, do not relaunch in a loop.

## Testing

Table tests in `internal/wallpaper`:

- Independent assignments for `DP-1` and `DP-3`
- All expands over the current connector list
- Stale generation ignored
- Disconnect keeps the assignment and drops runtime
- Reconnect restores
- Partial All keeps the failed output's prior assignment
- GIF classified as video
- Theme seed is the image path or the video still, unchanged when a video
  has no still
- Socket names are owned and connector-specific
- Parse `STATUS:` (spaces in paths), reject `ERROR:`, classify the
  auto-stop string as restart

Live Niri gate (both outputs connected):

- Panel maps (`niri msg -j layers`)
- Image on one output, video on the other
- All applies both
- Pause / Resume
- Restore stops gSlapper and shows the still via awww or swaybg
- `theme.seed` updates
- Kill by owned socket / recorded pid only

Stop when that gate passes. Do not pull launcher leftovers or desktop
widgets into this slice.

## Follow-up (bd, discovered from `sysc-146`)

- Implementation plan: `sysc-147` (closed; `docs/plans/2026-09-03-wallpaper.md`)
- Execute the plan: `sysc-149`
- gSlapper `*` coalescing when every output shares one video: `sysc-148`
- Favorites, flatten, color wallpaper, theme row in the picker
- Settings wallpaper page (directories, scale, fps, fade)
- Bar glyph if default bar layouts do not yet include it
- `KindVirtualGrid` only if row-virtualized list measurably fails
- Adopt a healthy foreign gSlapper on our socket path (not v1)
- Live grim of the picker
