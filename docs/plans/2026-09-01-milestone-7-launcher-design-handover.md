# Milestone 7 Launcher — Design Commission Handover

Date: 2026-09-01.

This handover is for a **receiving agent**. Produce an owner-approved **design**
and then an **executable implementation plan**. Do not write product code. Do not
widen scope into the rest of Milestone 7.

## Assignment

Design sysc-shell's application launcher as the first Milestone 7 vertical slice:
one Overlay panel, host-drawn, Niri-only, visual and interaction parity with
**Noctalia 5's default list launcher**.

Deliver, in a docs worktree branched from current `main`:

1. `docs/plans/YYYY-MM-DD-launcher-design.md` via `superpowers:brainstorming`.
   Stop after each section for owner approval. Do not implement.
2. `docs/plans/YYYY-MM-DD-launcher.md` via `superpowers:writing-plans`, only after
   the design is approved. Required `superpowers:executing-plans` header, exact
   files, TDD, small commit boundaries.
3. A register row in `docs/plans/README.md` in the same commit as each document.
4. Beads updates from the **primary** checkout `/home/nomadx/sysc-shell` (not the
   worktree). Claim the design issue when you start. Commit `.beads/issues.jsonl`
   with the documents.

Prior art is already done. Do not redo the inventory. Read it, then decide.

## Read these first, in order

1. This handover.
2. Prior art (authoritative for launcher UI and providers):
   `docs/plans/2026-09-01-launcher-prior-art.md`
3. Official Noctalia launcher docs (behavior, prefixes, dmenu):
   <https://docs.noctalia.dev/noctalia/launcher/>
4. Architecture: `docs/plans/2026-08-26-sysc-shell-design.md` (UI runtime, services,
   plugins, Niri). Constraint reminder: one Wayland dispatch goroutine; immutable
   snapshots; no Qt/QML/Lua; no lock screen.
5. Roadmap Milestone 7: `docs/roadmap.md` — launcher is candidate item 1 of a
   breadth milestone. You are not designing M7 as a whole.
6. M4 panel contracts: `docs/plans/2026-08-30-panel-foundation-design.md` D1–D5
   (Overlay, Exclusive, shield, IPC not pointer-only launcher, floating clamp).
7. M4 control vocabulary: `docs/plans/2026-08-30-settings-osd-theme-catalog-design.md`
   (text field, virtual list, settings is a centered modal — launcher placement
   should follow that, not bar-edge attach).
8. M6 exclusions: `docs/plans/2026-09-01-milestone-6-plugin-host-design.md` D5 —
   launcher providers, desktop widgets, and control-center plugin slots stay out
   of M6. Do not require the plugin host for v1.
9. M5 integration only as a **dependency list**, not a prerequisite to *design*:
   `docs/plans/2026-08-31-notifications-and-tray-integration-design.md` (KindImage,
   root chain, `xdg_activation_v1`). The design must say which of these v1 needs
   and what ships if they are late.
10. Live panel IPC and hotkeys: `internal/ipc/server.go`, `docs/niri-hotkeys.md`.
11. Current UI kinds: `internal/ui/tree.go`. There is **no `KindImage`** on `main`.
12. `AGENTS.md` and ponytail: first working rung, no extra toolkit, one focused
    check per non-trivial logic, bd only.

Noctalia sources if a claim in the prior-art doc is ambiguous:

- `/home/nomadx/noctalia/src/shell/launcher/launcher_panel.h`
- `/home/nomadx/noctalia/src/shell/launcher/launcher_panel.cpp`
- `/home/nomadx/noctalia/src/launcher/launcher_provider.h`
- `/home/nomadx/noctalia/src/launcher/app_provider.cpp`
- `/home/nomadx/noctalia/src/ui/style.h`
- `/home/nomadx/noctalia/src/config/config_types.h` `LauncherConfig` / `PanelConfig`

DMS is first-class prior art (this machine's live launcher is standalone
`full`/`compact`, not Spotlight). Read §5 of the prior-art doc, then only reopen
QML if a number is ambiguous:

- `/usr/share/quickshell/dms/Modals/DankLauncherV2/DankLauncherV2Modal.qml`
- `DankLauncherV2ModalStandalone.qml`, `DankLauncherV2ModalSpotlight.qml`
- `LauncherContent.qml`, `SpotlightLauncherContent.qml`
- `ResultItem.qml`, `SpotlightResultRow.qml`
- `/usr/share/quickshell/dms/Services/AppSearchService.qml`
- `/usr/share/quickshell/dms/Services/SessionService.qml` (`launchDesktopEntry`)
- `/usr/share/quickshell/dms/Common/SettingsData.qml`

Go reuse is decided at the library layer, not by copying another compositor
client. First rung is `github.com/go-freedesktop/desktopentry` (BSD-3, Scan +
ExpandExec). Elephant is GPL-3 — behavior only. Gofer has no LICENSE on `main`
and owns its own Wayland loop — do not copy. Details: prior-art §6.

## Fixed constraints (do not reopen)

- Go. No C++, QML, Luau, Qt.
- Niri only. Spawn through Niri (`niri msg action spawn` or the JSON socket), not
  a generic compositor helper.
- One process, one Wayland owner. Desktop-file IO, icon decode, and fuzzy scoring
  run off that goroutine and publish immutable snapshots.
- Reuse M4 panel machinery. No second surface host. No xdg-shell toplevel.
- Do not import Noctalia or DMS source. Behavior and visual tokens are fair;
  translation is not.
- Do not import Elephant (GPL-3), Walker, or Gofer's compositor client. Pin
  `go-freedesktop/desktopentry` rather than writing a `.desktop` parser.
- Do not add `danksearch`, libqalculate, or a new sibling daemon for v1. Notify/tray
  are daemons because D-Bus must outlive the bar. `.desktop` search does not.
- Do not design control center, clipboard history, network, MPRIS, wallpaper picker,
  or desktop widgets in this slice. Prefix hooks for later providers are allowed.
- Local `replace` directives are forbidden in any later implementation commit.
- Status lives in bd. Do not put "TODO" lists in the design header.

## Owner-intent defaults (challenge them in brainstorming, don't ignore them)

These are the commission's recommended answers. Brainstorming may change them
**with the owner**. If the owner is silent, keep them.

| Topic | Default | Why |
|---|---|---|
| Visual target | Noctalia **default list** 560×500, Primary-selected rows | Matches M4 Exclusive cards. **Challenge:** this laptop's daily launcher is DMS standalone 620×600 (`launcherStyle=full`). Spotlight and Connected stay out. |
| v1 providers | Applications + `/` provider overview | daily driver; other providers are later named slices |
| Calculator / emoji / windows | out of v1 | calc needs a Go substitute for libqalculate; emoji needs a dataset; windows is a Niri focus helper |
| App grid / compact / category chips | config later, not v1-blocking | defaults in Noctalia are list, icons on, compact off, categories on — chips can wait one tranche |
| Bar button | IPC + niri bind in v1; bar glyph only if keyboard parity exists | M4 D5 |
| Placement | centered floating, like settings | Noctalia launcher default; attached placement still rejected |
| Icons | real theme icons via the M5 `KindImage` path if it has landed; otherwise glyph fallback with a recorded ceiling | do not block the design on M5 |
| Activation | Niri spawn + `xdg_activation_v1` when the protocol is bound; without it, spawn anyway | honest capability |
| Plugin providers | not v1 | M6 freeze; add a capability in a later M7 tranche |

## Design must decide (known unknowns)

Write each as a numbered decision in the design. Do not hide them in prose.

1. **Chrome family.** Noctalia 560×500 list vs this-machine DMS 620×600 standalone
   (footer chips, primaryPressed selection). Spotlight growing island is out.
2. **Panel size.** Lock the chosen default, or a token? Clamp on small outputs?
3. **Empty-open behavior.** Both Noctalia and DMS-full show a full app list with
   empty query. Spotlight does not. Follow the chosen chrome.
4. **Scoring.** Noctalia weights + small usage cap vs DMS exact/prefix + frecency
   buckets. `sahilm/fuzzy` is MIT but will not match either. Cap 50 is a known bound.
5. **Desktop files.** Pin `go-freedesktop/desktopentry` unless a table test fails.
   Directories, Hidden/NoDisplay/`OnlyShowIn`, `Terminal=true` (terminal without
   `$TERMINAL`), TryExec, actions (right-click vs searchable). `ExpandExec` then
   Niri spawn — do not `os/exec` from the library README.
6. **Spawn.** `niri msg action spawn` argv vs shell. No `/bin/sh -lc` for app Exec
   unless a recorded ceiling says otherwise. Activation token ownership. DMS uses
   Quickshell `execDetached` plus optional NVIDIA/launchPrefix — policy reference
   only.
7. **Icons.** Prefer `go-freedesktop/icontheme` when painting. SVG raster (M5
   promised a bounded pure-Go path), pixmap 40 (Noctalia) vs 36 (DMS standalone),
   missing-icon glyph. One process-wide decoder.
8. **Usage store.** Path under `$XDG_STATE_HOME`, privacy, cap, what happens when
   `sortByUsage` is off. DMS frecency vs Noctalia count boost.
9. **Prefix scheme.** Keep `/` + overview so later providers (`calc`, `emo`, `win`)
   slot in without a layout change. DMS uses footer modes instead of prefixes;
   do not take Files/Plugins in v1.
10. **Kind gaps.** If `KindImage` is missing, is v1 glyph-only? If there is no
    segmented control, are category chips out? Do not invent a toolkit.
11. **Root chain.** Launcher is an interactive root. Opening it must close settings
    / session / (later) notification center. That needs M5 `sysc-60` or an explicit
    v1 limitation.
12. **Entrance gate.** May implementation start before M5 tags, or must `KindImage`
    and the root coordinator land first? The plan's Task 0 must be honest.
13. **Live grim.** Optional. Capture Noctalia list and DMS standalone (empty, typed,
    selected). Do not block the design on it.

## Plan requirements (once the design is approved)

- Task 0 reconciles against landed M4/M5 APIs the way M6A Task 0 does. If a named
  primitive is missing, stop or amend the plan — do not silently stub.
- TDD: failing table tests for desktop parse, scoring, prefix routing, usage boost
  cap, bounds; then panel projection tests; then IPC.
- One focused race test if a collector goroutine exists.
- No plugin protocol changes in this plan.
- Commit boundaries per provider/parser/panel, not one giant "add launcher".
- Live Niri gate: open via documented bind, type, arrow, Enter launches, Escape
  closes, shield click closes, two outputs, restart, idle (no frame loop). Record
  in the plan; owner may defer the hardware run as with earlier milestones.

## Stop conditions

Stop and ask the owner if any of these appear:

- The design needs EGL/blur/scrim to "look like Spotlight".
- The design needs a new D-Bus daemon, CGO, or libqalculate.
- The design needs plugin-host capabilities or M6 protocol changes.
- Scope creeps to clipboard-in-launcher, file search, or control center.
- Implementation is requested before the design is approved.
- `bd` from a worktree would create a second database — use
  `BEADS_DB=/home/nomadx/sysc-shell/.beads/beads.db` or run bd in the primary
  checkout.

## Repository / worktree

```bash
cd /home/nomadx/sysc-shell
git worktree add \
  /home/nomadx/.config/superpowers/worktrees/sysc-shell/milestone/launcher \
  -b milestone/launcher main
```

Do not start from `milestone/notifications-tray` or `redesign/milestone-5`; those
trees are stale relative to merged M4. Do not edit plugin-host or notify/tray
worktrees. Do not reset a dirty worktree.

Documents land on `main` as docs-only commits per `AGENTS.md`. Implementation, when
it happens, is a later worktree after the plan exists.

## Beads

From `/home/nomadx/sysc-shell`:

- `sysc-12` (launcher prior-art as M4 input) is closed; this research superseded it.
- Claim `sysc-78` (`Design and plan M7 launcher`) with
  `bd update sysc-78 --status in_progress`.
- Record follow-up slices (`calc` provider, bar glyph, app grid, emoji) as
  `bd create ... --deps discovered-from:<id>`, not as work inside the v1 plan.

## Success

The owner can read the design and see a centered Exclusive card, Applications
search, and an explicit chrome choice (Noctalia list vs this-machine DMS
standalone). The plan pins `go-freedesktop/desktopentry`, owns scoring, spawns
via Niri, and is executable without opening QML or C++. Elephant/Gofer are not
in the tree.
