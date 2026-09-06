# Launcher chrome remainder handover

Date: 2026-09-06
Kind: execution-handover
Commission: `sysc-193`

Three items are left from the launcher design pass the owner approved on
2026-09-05 (plate A, plus the `//////SYSC//////` rail). The rest of that pass
has shipped. This document commissions the remainder.

State lives in bd, not in a status header. Query `sysc-193` from
`/home/nomadx/sysc-shell` with `BEADS_DB=/home/nomadx/sysc-shell/.beads/beads.db`.

---

## Assignment

Land items 1–3 below. Item 3 is an owner decision before it is code: ask, do
not choose for them.

Do not start the panel shadow (`sysc-192`), the frecency curve (`sysc-172`),
or the SVG rasteriser (`sysc-173`). Each is its own slice and two of them are
waiting on the owner. Do not restructure `KindTextField`; see item 2.

---

## Repository and workspace

| Item | Value |
|---|---|
| Primary checkout | `/home/nomadx/sysc-shell` |
| Branch | `main` at `21cad70` |
| Chrome file | `internal/shell/popout_launcher.go` |
| Chrome tests | `internal/shell/popout_launcher_test.go` (28 tests) |
| Field painter | `internal/render/paint.go`, `paintTextField` |
| Live reference | Noctalia launcher, in the 2026-09-06 session; DMS spotlight for spacing |

A second agent works in this repository. Check `git status` before you start
and do not revert files you did not write. Use a worktree under
`/home/nomadx/.config/superpowers/worktrees/sysc-shell/<branch>` and build to
your own binary path rather than sharing `~/.local/bin/sysc-shell`.

Run `bd` only from the primary checkout. A worktree `bd` creates a second
SQLite file and lies.

---

## Hazards, all of which have already bitten

- **Never** combine `./...` with `-race`. Repo-wide builds hard-lock this
  machine: zram-only swap, 16-way linking. Cap it:
  `GOMAXPROCS=2 go test -count=1 -p 1 ./internal/<pkg>`.
- `go test ./internal/shell` runs `loginctl terminate-session self` for real.
  Shadow `loginctl` on `PATH` before running it.
- Commit messages are rejected on a case-insensitive **substring** match that
  includes `bot` and `agent`, so "both" and "bottom" fail. Precheck every
  message. The pattern is in
  `~/.claude/projects/-home-nomadx-sysc-shell/memory/commit-hook-rejects-ai-attribution.md`.
- **Do not use `wtype`** to drive the panel. Attaching a virtual keyboard drops
  the panel's exclusive grab, dismisses it, and the keystrokes land in whatever
  terminal is underneath.
- Redeploy is `mv`, not `cp`: a cross-filesystem copy over the running binary
  gets `ETXTBSY`. Build to a path on the same filesystem, then move.
- `internal/ui` is a retained proof tree, not a browser. There is no flexbox
  and no `Grow`. Sizes are computed, not negotiated, and **a child that does
  not fit is a layout error that fails the whole surface and closes the
  panel**. Both spacing changes below can trip this.
- Two measure paths exist and can disagree: `measureNode` in
  `internal/ui/layout.go` and `columnChildHeight` in `internal/ui/column.go`.
  A node kind must be handled in each. This caused the half-height row bug
  (`sysc-190`); check both when you change sizes.

Open the panel without a keyboard:

```
sysc-shell ipc panel.toggle '{"panel":"launcher"}'
grim /tmp/shot.png
```

---

## What already shipped, so you do not rebuild it

| Piece | Commit |
|---|---|
| `//////SYSC//////` rail over a wordmark node, 700px panel, 8.5 rows | `02d4571`, `9a6993a` |
| Footer: result count plus the sysc-greet help line | `9a6993a` |
| Full browse list, no 50-entry cap | `9a6993a` |
| Equal row heights when an entry has no Comment or icon | `d351a5a` |
| Antialiased borders, drawn search glyph, centred field text | `5da46b3` |
| **Row hover** — item 1 of `sysc-193`, already done | `21368a0` |

Row hover turned out not to be a launcher gap at all. `interaction.apply` and
`hoverKeyAt` each rebuilt virtual-list rows through `Item(i)` instead of
walking the `Children` that layout had materialised, so the nodes they touched
carried no bounds and were discarded. No list row on any surface could take
the pointer. Do not re-open it.

---

## Code map

`internal/shell/popout_launcher.go`:

| Symbol | Line | Note |
|---|---|---|
| constants | 20 | `launcherRowHeight` 60, `launcherRowGap` 8, `launcherSlotHeight` 68, `launcherFieldHeight` 56, `launcherIconSlot` 40 |
| `launcherTree` | 190 | builds the field, rail, list, footer |
| `launcherListHeight` | 178 | single source of truth for the list viewport |
| `launcherRow` | 229 | wrapper column + capsule, carries `Action: "launch:<id>"` |
| `launcherRowBody` | 252 | icon node and label column |
| `launcherPointerPress` | 419 | where a click on a row is resolved |
| `launcherRowAt` | 492 | hit-tests a row from the tree |

Current row geometry, outermost first:

```
slot            68   launcherSlotHeight = 60 + 8
 column pad       4   launcherRowGap / 2, top and bottom
  capsule        60   Fill FillSoft when selected, ShapeMedium
   capsule pad    4
    body row     52   Gap 12, Padding 4
     icon        40   square, image or letter capsule
     labels      38   RoleLabel name, Gap 2, RoleBody comment
```

---

## 1. Row spacing: DMS's 12 above, 16 below

The list is currently a flat 8px gap with 4px padding at every level. DMS
gives its rows 12 above the text block and 16 below, and that asymmetry is
what makes its list breathe. Ours reads tight by comparison; the owner
flagged this against the live panel.

Apply the 12/16 inside the row capsule, not to the gap between rows: the gap
separates rows from each other, the padding is what surrounds the text block.

This changes the row's height, so it changes `launcherSlotHeight`, and the
list viewport is derived from it. `launcherListHeight` is the only place that
arithmetic lives — it was three copies once and they disagreed. Keep it that
way.

**Watch:** `TestLauncherListReachesThePanelFloor` pins that the list ends
exactly at the panel floor with no dead strip. It will fail if the new slot
height does not divide the viewport the way the old one did. That test is
correct; do not relax it. The panel is 700 tall and shows 8.5 rows today,
which is deliberate — the half row is the affordance that says "scrollable"
without a scrollbar. Keep a visible half row.

**Verify:** measure a real row in a screenshot, not only in a test.

## 2. A clear button in the search field

With a long query the only way back to the browse list is holding Backspace.
The field needs a trailing affordance that empties it.

`KindTextField` is a **leaf**, painted whole by `paintTextField`
(`internal/render/paint.go`). It is shared chrome: the wallpaper picker and
settings use it too. That constrains the approach. Two options:

**A. Paint a trailing glyph in `paintTextField`, hit-test it in the launcher.**
Mirror the leading magnifier: reserve a trailing inset the way
`searchGlyphInset`/`searchGlyphSize`/`searchGlyphGap` reserve the leading one,
draw a close glyph only when `n.Name == "Search"` and the text is non-empty,
and resolve the click in `launcherPointerPress` against that rect.
Recommended. It keeps the field a leaf, and the glyph machinery is already
there — `SearchGlyphMask` in `internal/render/mask.go` shows the pattern, and
`internal/render/icons/svg/close.svg` is already in the embedded subset if you
would rather use `KindIcon`.

**B. Restructure the field into a row of [glyph, field, button].**
Do not do this without a strong reason. It changes a shared control's shape,
its focus order, and its measurement in both measure paths, and every other
panel that uses a field pays for it.

Whichever you choose: the clear must also be reachable without a pointer, and
it already is — Escape closes the panel and Backspace empties the query. Do
not add a keybinding for it.

**Watch:** the trailing inset shortens the text box. `centreLine` and the
caret both derive from that box; check a long query still renders and the
caret still lands correctly at the end of the text.

## 3. The rail's slashes read light against the mark — OWNER DECISION

Live, `//////SYSC//////` renders with the slashes visibly lighter than the
wordmark beside them. The rail takes `RoleTitle` (16/600) and the mark is a
raster at `launcherMarkHeight` 23.

Two ways to balance it:

- drop the mark to about 19px so the slashes carry equal weight, or
- move the slashes up to `RoleHeadline` (20/600) and leave the mark alone.

They give different results: the first makes the rail smaller overall, the
second makes it heavier. **Ask the owner and show them both**, rendered live
on the eldritch theme, which is what ships as default. Do not pick one.

The owner has seen the live rail but never a side-by-side.

---

## Definition of done

- Items 1 and 2 implemented, with tests in `internal/shell/popout_launcher_test.go`.
- `GOMAXPROCS=2 go test -count=1 -p 1 ./internal/shell ./internal/ui ./internal/render` green.
- Verified live with a screenshot, not only by test. The half-height row bug
  and the top-aligned field text both passed every test that existed.
- Item 3 put to the owner with two rendered options, and their answer recorded
  on `sysc-193`.
- `sysc-193` closed with what shipped, or updated with what is left.
