# Wallpaper Implementation Handover

Date: 2026-09-04.

This handover is for a **receiving agent**. Design and plan are already
approved. Execute the plan. Do not reopen chrome, engine split, or lifecycle.

## Paths

| Document | Absolute path |
|---|---|
| Design | `/home/nomadx/sysc-shell/docs/plans/2026-09-03-wallpaper-design.md` |
| Plan | `/home/nomadx/sysc-shell/docs/plans/2026-09-03-wallpaper.md` |
| Audit (second opinion, partially applied) | `/home/nomadx/sysc-shell/docs/plans/2026-09-03-wallpaper-audit.md` |
| This handover | `/home/nomadx/sysc-shell/docs/plans/2026-09-04-wallpaper-execution-handover.md` |

Tracker: `sysc-149` (in_progress). Design bead `sysc-146` is closed. Plan-write
bead `sysc-147` is closed. Wildcard coalescing `sysc-148` is later, P3 — do
not pull it into v1.

## Assignment

1. Worktree `milestone/wallpaper` from current `main` under `.worktrees/`
   (gitignored). Run `bd` only from `/home/nomadx/sysc-shell` with
   `BEADS_DB=/home/nomadx/sysc-shell/.beads/beads.db` if you must run it from
   the worktree.
2. Execute `docs/plans/2026-09-03-wallpaper.md` via
   `superpowers:executing-plans`, task by task, TDD, one commit per task
   Step 5.
3. Stop when Task 13's live Niri checks pass. Do not fold in control centre,
   notification leftovers, favorites, flatten, or gSlapper `*`.

Do not rewrite the design. If a plan step is wrong against landed APIs, amend
the plan in the Task 0 commit and keep going.

## Read first, in this order

1. This handover (session context the design does not repeat).
2. Design: `docs/plans/2026-09-03-wallpaper-design.md` (D1–D20).
3. Plan: `docs/plans/2026-09-03-wallpaper.md` (Tasks 0–13). Use the **patched**
   plan: All token, `SplitN(..., " ", 4)`, `-r 30`, `cfgHook`, spawn stub.
   Task 0 Step 1 says claim `sysc-149`, not the closed `sysc-147`.
4. Audit: `docs/plans/2026-09-03-wallpaper-audit.md`. Owner treated it as a
   second opinion. Accepted pins are already in the plan. Do not also "fix"
   AGENTS.md as a Task 0 gate.
5. `AGENTS.md` — Wayland owner, draw-after-invalidation, bd, live Niri env,
   never `pkill -f` a name also typed in the command.
6. Panel copy target: `internal/shell/popout_launcher.go` and
   `internal/shell/popout_launcher_test.go`. Wallpaper is the same surface
   class (Overlay, Exclusive, `CenterY`, shield, IPC toggle).
7. Theme write-back: `internal/shell/registry.go` `generateTheme`,
   `internal/theme/generate.go`, `config.ThemeGen`.
8. IPC names: `internal/ipc/server.go` `knownPanels`.
9. Roadmap M7 wallpaper paragraph in `docs/roadmap.md` — **overruled** by
   design D13. The shell launches gSlapper and owns the sockets.

## Session history (why the decisions look like this)

The owner asked for a wallpaper selector/viewer using **gSlapper** as the
engine with **awww** then **swaybg** as static fallback, and to copy DMS or
Noctalia. They pointed at their Noctalia v5 plugin
`/home/nomadx/noctalia-gslapper` as a small combined picker.

Three pickers exist. They are not interchangeable:

| Surface | Size | Grid | Video | Static renderer |
|---|---|---|---|---|
| Noctalia core panel | 980×700 | virtualized, folders as tiles, favorites + theme row | no | Noctalia Background layer |
| noctalia-gslapper plugin | 980×1100 | paginated 4×24 (plugin API cannot virtualize) | gSlapper | Noctalia `setWallpaper` |
| DMS v1.5.3 DankDash | ~700×410 | 4×4 pages, no search | no (older git tree had `gs:`) | DMS `WallpaperBackground` |

Owner chrome: **plugin layout + native virtualized grid**. Pagination was a
Noctalia plugin-API ceiling, not a product preference. No favorites, flatten,
color wallpaper, or theme row in v1.

Owner engine: **gSlapper-first** — stills *and* video go through gSlapper when
it is installed. That is **not** the plugin split (Noctalia surface for
images, gSlapper only for video). awww then swaybg only if gSlapper is
missing, and only for static files. Video tiles disable without gSlapper.

Owner lifecycle: the shell **launches** gSlapper. Copy Waytrogen's owned
sockets, not the roadmap "socket client only" line. Canonical engine code:

`/home/nomadx/waytrogen/src/changers/gslapper.rs`

as merged to `nikolaizombie1/waytrogen` **PR #61** ("Keep gSlapper running
between wallpaper changes"). Later PRs (#62 UTF-8, #63 SQLite WAL) do not
touch that file. An earlier local checkout was behind `origin/main`; after
fetch, `gslapper.rs` was identical to PR #61. Do not read the pre-1.5
`/tmp/gslapper.sock` + `pkill` path from PR #50.

All-outputs: Waytrogen maps All to gSlapper `*`. The plugin fans out one
process per connector. The owner asked which was best, agreed with **fan-out
+ owned sockets**, and warned it is more complex than it looks. Complexity
that must show up in the code: per-connector generation so DP-3 cannot
clobber DP-1; All is two applies with partial success; disconnect keeps the
assignment and reconnect restores it; stop a fallback **we started** before
launching gSlapper; never wait on the Wayland owner for the 3s socket wait.

Live outputs on 2026-09-03: **DP-3** 2560×1440 and **DP-1** 3440×1440. The
AGENTS.md "one output / two-output checks unrunnable" sentence is stale.
Task 0 re-reads `niri msg -j outputs`. Tests keep the `DP-1`/`DP-3` fixture
names even if connectors change.

Restore: the plugin hands the output back to Noctalia's Background surface.
sysc-shell will not own a wallpaper layer. Owner chose: IPC-stop our
gSlapper, then put the last still on awww/swaybg. No still → empty desktop.
That is the one intentional exception to gSlapper-first.

Theme: apply updates `theme.source=wallpaper` and `theme.seed` from the image
or a video still. No still → leave the seed. Hook lives on `Service.cfgHook`;
the registry points it at `generateTheme`.

GIF is video (plugin rule). Native Noctalia treated GIF as a still. gSlapper
plays it.

`awww` on this box is LGFae's swww successor (`pacman -Q awww`, provides
`swww`, binaries `/usr/bin/awww` and `/usr/bin/awww-daemon`). The owner said
awww, not swww.

## Do not reopen

- First-party panel vs M6 plugin vs `sysc-wallpaper` daemon — first-party.
- Shell-owned Background wallpaper surface.
- gSlapper `*` coalescing (`sysc-148`).
- Favorites / flatten / color / theme row in the picker.
- Importing QML, Luau, or Noctalia/DMS config.
- `pkill -f gslapper` / `pkill swaybg`. Last-resort kill matches only a
  command line that contains **our** socket path.
- Supervising a gSlapper whose socket we do not own.
- Adding wallpaper to `config.Default()` bar layout. `knownItems` may accept
  `"wallpaper"`; the default bar stays unchanged.
- Mixing notification-centre or control-centre work into this branch.

## Worktree and machine

```bash
export NIRI_SOCKET=$(ls /run/user/1000/niri.wayland-*.sock | head -1)
export WAYLAND_DISPLAY=wayland-1
export XDG_RUNTIME_DIR=/run/user/1000
```

Kill scratchpad shells by pid from `pgrep -f 'scratchpad/'`. Never
`pkill -f sysc-shell`.

`commit-msg` hook (`core.hooksPath`) rejects ordinary English matching
`agent`, `cursor`, `codex`, `llm`, `both`, `Hallmark`. "AGENTS.md" in a
commit message has already failed that hook.

Panel `configure`/`render`/`handle` take `Registry.mu`. Relays run off the
Wayland owner. `rebuildPanel` already holds the lock.

Copy launcher IPC toggle. A niri bind is optional; `docs/niri-hotkeys.md`
does not yet list wallpaper.

## Prior-art trees (read when a plan step is ambiguous)

- Plugin UI/service: `/home/nomadx/noctalia-gslapper/gslapper/panel.luau`,
  `service.luau`
- Native panel: `/home/nomadx/noctalia/src/shell/wallpaper/panel/wallpaper_panel.cpp`
- Engine IPC: `/home/nomadx/gSlapper/docs-site/content/docs/user-guide/ipc-control.mdx`
- Waytrogen changer: `/home/nomadx/waytrogen/src/changers/gslapper.rs`
- DMS picker (stills only): `/usr/share/quickshell/dms/Modules/DankDash/WallpaperTab.qml`

gSlapper help on this box: `-I` / `--ipc-socket`, `--no-save-state`, `-r` /
`--fps-cap`, `--transition-type`, `--cache-size`. Tests pin **`-r 30`**, not
the long form.

## Audit leftovers (ops, not design)

The 2026-09-03 audit also said `main` was dirty with notification-centre
files. That slice has since merged (`6acfbbb`). Re-check `git status` before
branching. Untracked `.cursor/` and stray test files are not this slice.

AGENTS.md still says one output. Do not block Task 0 on editing it. If you
touch it, that is a separate docs-only commit and the message must not
contain the substring `agent`.

## Done looks like

Task 13 recorded in `tests/integration/README.md`: panel maps; image on one
output and video on the other; All applies both; Pause/Resume; Restore shows
the still via awww or swaybg; theme seed updated; `pgrep -af gslapper`
command lines contain `$XDG_RUNTIME_DIR/sysc-shell/gslapper-`. Then close
`sysc-149` with that reason. Commit `.beads/issues.jsonl` with the closing
commit.
