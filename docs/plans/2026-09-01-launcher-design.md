# Milestone 7 Launcher — Design

Date: 2026-09-01. Status section intentionally absent — status lives in bd (`sysc-78`).

Commission: `2026-09-01-milestone-7-launcher-design-handover.md`. Prior art:
`2026-09-01-launcher-prior-art.md`.

## Commission amendments (owner, 2026-09-01)

The owner overruled two lines of the handover during brainstorming:

- **Elephant is first-class prior art.** "Borrow from heavily." The handover's
  "behavior only, do not import" stance is void. Prior-art §6's Elephant entry
  and §9's scoring recommendation are outdated; this design supersedes them.
- **GPL-3 is not a constraint for this project.** The licensing objection to
  Elephant reuse is removed.

Every other handover constraint stands: Go only, Niri only, one process / one
Wayland dispatch goroutine, reuse M4 panel machinery, pin
`go-freedesktop/desktopentry`, no sibling daemon, no plugin providers, no
`danksearch`/libqalculate, no local `replace` directives.

## Goal and scope

One application-launcher panel: centered, host-drawn, Exclusive Overlay with a
dismiss shield, at visual and interaction parity with Noctalia 5's default list
launcher. Providers in v1: Applications plus a `/` provider overview. Everything
else named in the roadmap's breadth milestone (clipboard, files, control
center, wallpaper, desktop widgets) is out. Calculator, emoji, windows, bar
glyph, app grid / compact mode, and category chips are follow-up slices tracked
in bd, not work inside this design.

## Decisions

### Chrome

- **D1 — Chrome family: Noctalia default list.** 560×500 logical panel, search
  field on top, single-column result list below, selected row painted as a full
  Primary fill with OnPrimary text. The DMS standalone chrome (620×600, footer
  mode chips, `primaryPressed` selection) and the Spotlight growing island are
  rejected. Owner decision against the handover's challenge prompt: even though
  this machine's daily driver is DMS standalone, the parity target is the
  Noctalia list.
- **D2 — Size is two constants, clamped.** `ui.Rect{W: 560, H: 500}` returned
  from the existing `panelTargetSize` switch, no config knob. Placement reuses
  the M4 D5 floating clamp (centered on the focused output, clamped inside the
  output minus padding and the bar reserved zone); on small outputs the clamp
  shrinks it. Geometry constants: field height 38, row height 48, icon slot
  40×40, inner padding 12 — recorded here so the icon and grid slices never
  need a layout change.
- **D3 — Empty-open shows the full application list.** Both references agree.
  Order: usage weight first (the history model naturally ranks by
  `LastUsed`/`Amount` at empty query), then name. Result count capped at 50 on
  every query, empty or not.

### Scoring and usage

- **D4 — Scoring: fzf + Elephant field weighting.** Pin
  `github.com/junegunn/fzf@v0.74.3` (MIT) and call `algo.FuzzyMatchV2` through
  one thin wrapper in the launcher package, the same seam Elephant uses in
  `pkg/common/fzf.go`. Candidate fields in order: Name, GenericName, Keywords,
  Exec, Comment. Port Elephant's `calcScore` weighting: the best field's raw
  score minus `min(fieldIndex*5, 50)`; a miss on Name but hit on a later field
  can never outrank a Name hit of equal raw score. Deterministic tiebreak:
  score descending, then name ascending. Cap 50 is a tested bound.
- **D5 — Usage store: Elephant history formula, ported, not imported.**
  The owner approved importing `elephant/v2/pkg/common/history`; measured in
  Task-0 reconnaissance, that package is *not* clean: it compiles all of
  `pkg/common`, which imports `adrg/xdg`, `charlievieth/fastwalk`,
  `fsnotify/fsnotify`, `pelletier/go-toml/v2`, and `shellescape` into the
  shell. Recorded deviation: port the ~130-line history model into
  `internal/launcher/history.go` with an attribution comment and table tests
  that pin the formula's behavior. The model: gob file, per-provider map of
  query → identifier → `{LastUsed, Amount}`, `Amount` capped at 10, score
  `max((10 − daysSinceLastUsed) * amount, 1)` divided by the prefix-length
  delta; empty query aggregates across all queries. Boost is added to the D4
  score and capped at +25 (tested bound) so usage never buries a clearly better
  textual match. Path: `$XDG_STATE_HOME/sysc-shell/launcher/history.gob`, mode
  0600. Privacy note: typed query prefixes used to launch are persisted
  verbatim; that is the product of a usage store, recorded here rather than
  discovered later. Corrupt file → start empty, log, keep running.
- **D6 — Usage records on successful activation only.** A launch that fails to
  spawn records nothing.

### Desktop files and spawn

- **D7 — Desktop-file policy: pinned parser, full spec filtering.** Pin
  `github.com/go-freedesktop/desktopentry@v0.1.0` (BSD-3). Exclusions:
  `Hidden`, `NoDisplay`, `OnlyShowIn` not intersecting
  `XDG_CURRENT_DESKTOP` (split on `:`), `NotShowIn` intersecting it, and
  `TryExec` not found in PATH at scan time. All exclusion branches are table
  tests. Desktop actions are parsed, offered on right-click via the landed M4
  `KindMenu` machinery, and are **not** searchable in v1 (searchable actions
  are a follow-up slice).
- **D8 — Terminal=true wraps in the user's terminal, own code.** Port of
  Elephant's `terminal.go` policy into the launcher package: `$TERMINAL` if
  set, else first of `kitty foot alacritty wezterm ghostty` on PATH, argv
  form `<term> -e <expanded argv>`. With no terminal found the entry is
  excluded at scan time with a debug log (recorded ceiling; a settings-visible
  "unlaunchable" badge is a later nicety, not v1).
- **D9 — Spawn through Niri, argv, no shell.** Activation runs
  `exec.CommandContext(ctx, "niri", "msg", "action", "spawn", "--", argv...)`
  with a bounded context from the launcher service goroutine — never the
  Wayland dispatch goroutine. `niri` resolves through `exec.LookPath`; missing
  `niri` is an activation error, surfaced, not fatal. `xdg_activation_v1` is
  not bound on `main`: v1 spawns without a token and focus behavior is Niri's
  default — recorded ceiling, binding the protocol is a follow-up slice.
  Elephant's `sh -c prefix` and DMS's `execDetached` are policy references
  only; neither pattern is copied.

### Providers and prefixes

- **D10 — `/` prefix scheme with a provider table.** The launcher owns a small
  ordered provider registry: name, prefix, glyph, query function. Bare `/`
  shows the overview (every registered provider, name, prefix, one-line
  description). `/apps …` or any text without a prefix queries Applications,
  the default provider. Unknown prefixes after `/` fall back to an overview
  filtered by the prefix text. `calc`, `emo`, `win`, and friends slot into the
  registry later with zero layout or routing change — the hook is the registry
  entry, not new UI. Elephant's provider fan-out and wire protocol are prior
  art for the registry shape only; there is no socket, no protobuf, no second
  process.
- **D11 — No new node kinds in v1.** The panel projects onto the landed
  vocabulary: a `KindColumn` containing one `KindTextField` and one
  `KindVirtualList` (row height 48, `ItemCount` = result count). Task 0
  confirmed that an item can use the existing full-width `KindButton` paint
  path when selected and the text path otherwise. Task 10 adds the missing
  OnPrimary colour mapping to that painter; it does not add a toolkit kind.

### Integration

- **D12 — Collector + query worker, snapshots only.** One collector goroutine
  scans XDG application directories at service start, owns the entry set, and
  republishes an immutable `[]Entry` snapshot through a channel. Refresh is
  rescan-if-stale (>60s) triggered on panel open — no `fsnotify` dependency in
  v1 (recorded upgrade path: add a watcher if stale rescans ever measure slow
  on real directories). Queries run on a single query goroutine with a
  generation counter; a newer keystroke supersedes an in-flight older query.
  Results publish as immutable slices. No Wayland call, no shared mutable
  state crosses into the dispatch loop: bounded exec for spawn, immutable
  snapshots back.
- **D13 — Panel, IPC, and bind are M4 machinery plus one name.** New
  `PanelID` `launcher`; `parsePanelName`, IPC `knownPanels`, and
  `panelTargetSize` each gain one entry. Open uses the existing
  `focusedTrigger` path (focused output, floating clamp). IPC
  `panel.toggle {"panel":"launcher"}` works immediately; the documented Niri
  bind is `Super+Space` in `docs/niri-hotkeys.md` (owner adjustable). No bar
  button in v1 — the M4 D5 keyboard-parity reasoning stands; the bar glyph is
  a follow-up slice.
- **D14 — Use the landed interactive-root coordinator.** Task 0 found
  `sysc-60` on `main`. Opening the launcher therefore closes the current
  interactive root through the existing `openPanelRootLocked` path, with no
  launcher-specific coordinator code.
- **D15 — Entrance gate: implementation may start now.** Nothing in v1 blocks
  on M5. `KindImage` is absent on `main`, so v1 paints a fallback glyph in the
  40×40 icon slot (one process-wide placeholder face from the existing icon
  font), behind an `Icon` seam in the result type so the real-theme-icon slice
  (`go-freedesktop/icontheme` + `KindImage`) is a swap, not a rework. The
  plan's Task 0 reconciles against landed `main` exactly as M6A's does: panel
  host surface APIs, `Trigger`/`focusedTrigger`, `ui.Field` IME path, the
  virtual-list scroll route, `KindMenu` wiring, IPC tables, and the
  desktopentry library's field coverage (`Terminal`, `TryExec`,
  `OnlyShowIn`/`NotShowIn`, actions, `ExpandExec`). Any named primitive
  missing → stop or amend the plan; no silent stubs.
- **D16 — Live grim optional.** Capturing Noctalia's list and this machine's
  DMS standalone (empty, typed, selected) is nice-to-have evidence for the
  completion handoff; it does not gate design, plan, or any task.

## Components and data flow

```
desktop dirs ──scan──▶ collector goroutine ──snapshot []Entry──┐
                                                                ▼
keystroke (Wayland goroutine) ──Query{text, gen}──▶ query worker ──▶ []Result
                                                                │
history.gob ◀──load/save── launcher service ◀──Activate(id)─────┘
        │
        └── spawn: niri msg action spawn -- argv (bounded exec, service goroutine)

panel (Wayland goroutine): Field (IME) + KindVirtualList projection,
selection index, Enter → Activate, Esc / shield click → close
```

Files (v1):

- `internal/launcher/` — service: `service.go` (collector, query worker,
  snapshot publication), `entry.go` (entry/result types, Icon seam),
  `apps.go` (Applications provider: filter policy, actions, spawn argv),
  `score.go` (fzf wrapper + field weighting + usage boost), `history.go`
  (ported usage store), `prefix.go` (prefix routing + overview),
  `terminal.go` (Terminal=true policy). Tests alongside each.
- `internal/shell/` — one `popout_launcher.go` (projection, key routing,
  activation, right-click actions menu) plus edits to `panel.go`,
  `panelhost.go` (`PanelID`, `panelTargetSize`, `parsePanelName`,
  `panelTree` case).
- `internal/ipc/server.go` — one `knownPanels` entry.
- `docs/niri-hotkeys.md` — the documented bind.

## Error handling

- Scan errors per directory: log, skip that directory, keep the rest.
  Total scan failure → empty result set with the panel's existing
  error-label line; the panel still opens.
- History load failure: start empty (D5). Save failure: log, continue;
  launch is never blocked by the store.
- Spawn failure (niri missing, non-zero exit): error label in the panel,
  panel stays open, no usage recorded (D6).
- Query worker backpressure: generation supersede, never queue growth.

## Testing

Table tests first, per the handover: desktop-file exclusion matrix, Exec
expansion (field codes, quoting, `Terminal` wrapping, actions), scoring order
on a golden entry set including the field-index penalty and the +25 usage
cap, prefix routing including the unknown-prefix fallback, history formula and
round-trip. Then panel projection tests (field present, list projection,
selection movement at bounds, activation payloads), one focused race test for
collector snapshot republish vs in-flight queries, IPC `panel.toggle` for the
new name. Live Niri gate recorded in the plan, owner-deferrable as in M4/M5:
open via documented bind, type, arrow, Enter launches, Escape closes, shield
click closes, two outputs, restart, idle with no frame loop.

## Follow-up slices (bd, discovered from `sysc-78`)

Calculator provider, emoji provider, windows/Niri-focus provider, bar glyph
button, app grid / compact mode / category chips config, searchable desktop
actions, fsnotify rescan, real theme icons (`icontheme` + `KindImage`),
`xdg_activation_v1` token binding, root-coordinator adoption, live grim
capture.
