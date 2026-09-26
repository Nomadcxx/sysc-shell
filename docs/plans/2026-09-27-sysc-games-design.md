# sysc-games — Game Library Plugin Design

> Exceeds hthienloc/dms-plugins lutrisLauncher (QML) as a native Go plugin for
> sysc-shell's v1 plugin protocol. Approved section-by-section 2026-09-27.

## Reference gaps (lutrisLauncher)

- Shells out `python3 -c` to read `~/.local/share/lutris/pga.db` (Python dep; reads runner/platform/playtime then doesn't use them).
- Fixed 20s fake "launching" timer; no real launch feedback, no now-playing, no crash signal.
- Self-maintained `playCounts` duplicating Lutris's own `lastplayed`/`playtime`.
- Covers: local `coverart/<slug>.jpg` or blank placeholder; no fallback chain.
- Right-click = cramped in-card overlay (last played, plays, hide only).
- No runner badges, process actions, categories — all parked in a README roadmap.

## Architecture

Single Go binary, stdio v1, mirroring `sysc-launch` layout (own go.mod, `cmd/` + importable root). Repo: standalone `sysc-games`, published into the plugin store catalog (like world-clock).

```
sysc-plugin-games
├── cmd/            plugin main: handshake, view loop, timers
├── source/
│   ├── source.go   Source interface + normalized Game struct
│   └── lutris/     first (only) implementation
├── panel/          UI tree builders — pure funcs: state → *v1.Node
├── covers/         art resolution + on-disk cache
└── running/        process detection (shared API, per-source hints)
```

**Game (normalized)**: `ID, Name, Slug, Runner, Platform, PlaytimeSec, LastPlayed, Directory, CoverPath, Source`.

**Source interface** (multi-launcher seam — Steam/Heroic later are new files, zero UI change; shipped with ONE implementation, no registry/factory):

```go
type Source interface {
    List(ctx) ([]Game, error)
    Launch(ctx, Game) error
    Stop(ctx, Game) error
    Running(ctx) (map[string]time.Time, error) // by Game.ID
    Sections(ctx) ([]string, error)            // Lutris categories / virtual games
}
```

**Lutris source**: reads `pga.db` with `modernc.org/sqlite` (pure Go, read-only URI + busy timeout → WAL-safe while Lutris runs). Launch via `xdg-open lutris:rungameid/<id>`. `Directory` column → Open Folder free. Native `hidden` column replaces the reference's homemade blacklist (confirm at schema check during build).

**Persistence**: favorites/tags/sort/session log via host `state.get/set`; API keys via manifest settings.

## Panel UX

Panel ~720×560 `attached`; `panel.resize`-aware (narrow → single column, detail collapses under grid). No grid node in v1: `KindList` of card rows, 2 covers per row, native scroll.

```
┌──────────────────────────────────────────────┐
│ [Library|Favorites|Playing|Hidden]  [🔍 search]│
├────────────────────────┬─────────────────────┤
│ ┌────────┐ ┌────────┐  │   HERO COVER        │
│ │ cover  │ │ cover  │  │   Celeste (2018)    │
│ │ ★ [wine]│ │[lutris]│  │   runner: wine · 12h│
│ │ Celeste│ │  Hades │  │   ▂▄▆ session graph │
│ └────────┘ └────────┘  │   [▶ Launch][⋯ More]│
│   (scroll)             │   last played 2d ago│
└────────────────────────┴─────────────────────┘
```

- **Cards**: `KindImage` cover, favorite star, runner chip (tone-colored text badge). Click = select into detail pane; double-click/Enter = launch (reference's one-click-instant-launch replaced with deck behavior).
- **`KindSegmented`** view control: Library / Favorites / Playing / Hidden.
- **Detail pane**: big art, runner/platform badges, Lutris-owned playtime + lastplayed, session-history `KindGraph` sparkline from sessions we record (Lutris has totals only — new data).
- **"⋯ More"**: card flips in place to action column (Stop, Configure, Open folder, Favorite, Hide, Remove from view); secondary click triggers same. One patch, no new widget types.
- **Keyboard**: `/` search, arrows grid nav, Enter launch, Esc back — manifest `EventShortcut` + list focus.
- Skipped: reference's date-format setting (QML quirk; shell renders timestamps).

## Bar element

Stateful pill, media-widget mental model (library state, not just "open me"). One keyed node, patched like weather's `barCurrentKey`:

```
idle:      🎮
1 running: [cover18px] Celeste ⏱ 42m    (marquee-ellided title + elapsed)
2+ running:[cover]     Celeste +2
```

- Left click (any state) = open panel, focused on running game.
- Right click while playing = mini switcher: running games × [⏹] stop, no panel detour.
- Empty/no library = subtle-tone glyph, never error-red.
- Polling: 5s bar-only, 2s panel-open, **zero** while panel closed and nothing running (battery).
- Elapsed granularity 60s in bar — glance target, not stopwatch.

## Lifecycle, errors, hard parts

Launch state machine per game: `idle → launching → running → idle`. `xdg-open` then `/proc` poll @1s; process appears ≤15s → running w/ elapsed timer; never appears → tone=error on card. No 20s theater.

Running detection is a heuristic, owned as such:
- Match: any `/proc/*/cmdline` containing the game `directory` or Lutris prefix path (covers wine/proton/native/prima — they all exec inside it).
- Stop: `Kill(-pgid, SIGTERM)`, SIGKILL after 5s grace; notify distinguishes graceful vs force-killed tone.
- `// ponytail: dir-based cmdline match; per-runner probes if false negatives appear`. False-negative degrades to reference behavior, never wrong data.

Session recording: `(game_id, start, end)` appended to host state on detected transitions; powers sparkline; survives restarts.

| Condition | Behavior |
|---|---|
| `pga.db` absent | "Lutris library not found" + install hint; bar subtle |
| DB locked mid-write | busy timeout, retry once, keep last snapshot + `(stale)` |
| Runner missing (wine uninstalled) | card tone=error + `Source.Diagnose()` tooltip |
| Cover missing | chain: local jpg/png → SteamGridDB (if key set) → generated tile (Go `image/draw` initials PNG into cache). Blank cards impossible |
| Unresponsive Stop | force-kill notify |

Settings: `steamgriddb_key` (string; empty = local art only), `source_lutris` (bool), `poll_running` (off/auto; off = never scan /proc), `hide_unavailable` (bool).

Manifest: `id: org.sysc.games`, capabilities `["panels","settings"]`, panel 720×560 attached, bar widget + running mini-list, `requires: {"commands": []}` (Lutris absence graceful, not hard dep).

## Testing

- Fake `/proc` via root-injected reader; fixture `pga.db` built by test helper.
- Pure-func tests on every tree builder (node IDs/events assertions), per `weather/view_test.go` pattern.
- Gate: `go test ./...`.

## Milestones (each independently shippable)

1. **M1 data**: `source` + `lutris` + fixture tests — no UI.
2. **M2 grid panel**: bar pill, list/detail, search/sort/favorite/hide — parity minus Python.
3. **M3 truth**: running detection, session graph, stop/configure/open-folder, switcher pill.
4. **M4 art**: fallback chain + SteamGridDB + generated tiles.
