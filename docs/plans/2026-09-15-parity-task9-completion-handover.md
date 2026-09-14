# Parity Task 9 completion handover

Date: 2026-09-15.

Task 9 of the Noctalia v4 parity slice is **finished and merged**. The conformance scan is green on
`main` for the first time since Task 1 committed it red. The density regression it caused is fixed,
and the Bluetooth panel landed on top. **Tasks 10 and 11 are not started.**

Read `2026-09-14-parity-slice-execution-handover.md` for the slice's origin. Do not re-derive it.

## Receiving state

`main` = `origin/main` = **`9cb2acb`**. Working tree clean. **All nine packages green**, including
`TestSurfaceSourcesCarryNoLegacyVisuals`.

Four streams converged this session, in this order and for this reason:

| Commit | What | Why this order |
|---|---|---|
| `1c1f35b` | this handover + two register rows | — |
| `d69b4d4` | density migration | P1 regression; clean; land the fix before anything builds on it |
| `2f97256` | **parity Task 9** | clean; turns the scan green on `main` |
| `9cb2acb` | Bluetooth panel | conflicted; resolved last, against a tree already carrying the other two |

`feature/noctalia-parity`, `feature/bluetooth-panel`, `feature/network-panel` and
`integrate/bluetooth-panel-main` are all now fully contained in `main` and can be retired.

## What Task 9 did

189 literal geometry sites now read the spacing ladder, the density row, or a named constant. The few
belonging to none of those are exempted **at the site, with a reason**. `theme` gained named rungs
`MarginXXXS`…`MarginXL`, which is what made the conversion possible at all — most surfaces have no
`Metrics` value in scope, and a package constant needs only an import.

### Three findings worth more than the diff

1. **The scan cannot see arithmetic.** It matches literals on node fields only, never the expressions
   that model them, and every panel had at least one. Read the arithmetic *before* editing. That
   caught the audio viewport, the network `networkHeaderHeight` sum (which spans a function
   boundary), and the control centre's `quickWidth`. Not doing it first is how `launcherListHeight`
   broke — its chrome estimate read `24 + 8 + 8 + 8` while the nodes it modelled had moved.
2. **The control centre's spacing and widths are one composition.** A 228 column held two 110 tiles
   at a gap of 8; a weather row held four 143 slots at the same gap. Each filled its container
   *exactly*, so moving the gap alone overflowed it and `configure()` refused the child. This was
   attempted piecemeal, reverted, and redone as one pass: widths are derived (`ccTileW`, the forecast
   slot) and passed in, and the measured grid is named rather than repeated.
3. **`buildWidgets`' int is not padding.** `noCapsule` (−1) means "build a group's members with no
   capsule chrome". A `theme.Metrics` cannot carry that sentinel, so the row rides *alongside* the
   int. An earlier attempt to replace it was reverted.

### One thing deliberately left inconsistent

`popout_process.go`'s `used` estimate still spends `12` on its first gap where the column it models
uses `MarginM`. Reconciling it grows the table by one pixel and pushes measured content past its
surface; `TestToggleMonitorOpensTallerThanTheOldGuess` catches that. Something there measures one
taller than the terms say. **Find it before touching that line** — the comment above it says so.

## The density regression, and how it was fixed

**`sysc-276`** (P1, still `in_progress` in bd — *the fix has landed; close it after a live check*).
Deploying `main` shrank an established bar from the legacy 48 px `standard` row to the new 31 px
`default`, because `MetricsFor` folded `DensityStandard` onto `DensityDefault` after the parity
re-base.

Fixed by `d69b4d4`, implementing `2026-09-14-density-config-migration.md`: the new table stays, and
`standard` becomes a **hidden compatibility row** — current metrics with only bar height, padding and
spacing restored to `48/6/4`. Selector-free documents are classified as legacy, and sparse writes
record their preset so a reload cannot silently regenerate them onto different geometry.

**This supersedes most of Task 11 Step 1. Do not write a second migration.**

## The Bluetooth merge, and the two conflicts

`9cb2acb`. Both resolved by keeping every side, never choosing:

- **`registry.go`** — `Close()` had learned two lifecycles independently. `main` stops the wallpaper
  service and cancels its thumbnail worker; the branch stops the Bluetooth service and closes its
  relay. Dropping either leaks a process or a goroutine, so the merged `Close` captures all of them
  before releasing the lock. All three variables were already declared, so keeping both compiles.
- **`widget.go`** — *this conflict was created by landing Task 9 first.* `buildWidgets` had gained the
  density row, and the Bluetooth widget arrives through the same switch. It now takes the row the way
  its neighbours do. Two stale `buildWidgets` callers in `bluetoothwidget_test.go` needed the third
  argument as well.

`sysc-155` is closed. **`sysc-275`** — the live device-action gate (pairing, trust, forget on an
owner-approved disposable device) — is open and was never run.

## Loose end: a duplicate Task 9 nobody should resurrect

A concurrent Codex session (`01a09bd7-7476-79e3-aff9-77a223cbc06f`, transcript under
`~/.codex/sessions/2026/09/14/`) converted the **same sites** in parallel. Its work was never
committed and still sits as **15 dirty files** in
`/home/nomadx/sysc-shell/.worktrees/integrate/bluetooth-panel-main` — `controlcenter_pages.go`,
`popout_process.go`, `widget.go`, `notifywidget.go`, `wifiwidget.go` and their tests.

That worktree's **commits** are merged (they were the density fix). Its **dirty files are now
redundant**: the equivalent landed in `2f97256`, and anyone who resumes there will fight a conflict
with `main` for no gain. Discard them. A patch is preserved at
`…/scratchpad/duplicate-task9-integrate-worktree.patch` if the decision is ever questioned.

## What remains

**Task 10 — untouched.** Widen `TestSurfaceCardPaddingFollowsDensity`,
`TestSurfaceCardTitlesAreTitleRole` and `TestSurfaceHeadingsCarryARole`
(`surfacerole_test.go:90,135,193`) past `PanelMonitor`/`PanelSession` to every `PanelID` that opens
without external state. `cardsOf` is at `:54`; where it returns empty, record the panel as card-less
rather than `t.Fatalf` — the current `t.Fatalf("no cards found")` turns a widened loop red for panels
that legitimately build none. The eleven ids are in `panel.go:8-20`.

**Task 11 Step 1 — superseded** by the density migration. Only the release note is owed: `standard`
→ `default`, with `mini` and `spacious` newly available.

**Task 11 Step 2 — owed.** `internal/settings/registry.go:62-70` already lists all five densities, but
carries only `appearance.radius`. The **input radius row is missing**; add the second axis beside it.

**Task 11 Step 3 — now runnable.** The older handover's claim that the live gates cannot run is out of
date. The laptop answers on `ssh -p 7777 nomadx@192.168.0.64`, runs Niri
(`/run/user/1000/niri.wayland-1.1584.sock`) with `sysc-shell.service` active. `go.mod` pins
`sysc-notify v0.1.0-rc.3`, which that host **cannot resolve** — build on archPC and copy the binary.
Deploy is `install -m755` into `~/.local/bin/sysc-shell` then `systemctl --user restart`. Back the
binary up first, and verify from the journal: a handler panic reads `active` while painting nothing.
A live deploy of the merged tree was verified this way earlier in the session.

## Hazards paid for

- **The tracker's hooks fight you.** `pre-commit` runs `bd sync --flush-only` and **stages the result
  on every commit**, so a file validated before staging is replaced at commit time — that is how
  three fork records reached `main`. `post-merge` imports with `--resolve-collisions`, minting a
  fresh ID per collision (270 → 272 → 273 → 276 → 277 were one issue). **Verify the committed blob
  after committing, never the working file before.** Fixes belong in the database; the flush
  overwrites the file regardless.
- **An exemption marker must sit on the same line as its literal.** The scan reads line by line. A
  comment above it exempts nothing. This cost four sites across two commits.
- **Never cap a completeness check.** `grep … | head -8` hid 16 of 24 `buildWidgets` callers *and* a
  sentinel, and sent a whole increment down the wrong path.
- **A gate count of zero can mean the package did not build.** Read build status alongside it.
- **`--no-verify` is forbidden**, and `commit-msg` rejects substrings case-insensitively: `both`
  contains `bot`. Screen every message before committing.
- **gopls lies in these worktrees** (`undefined: Registry`, `use of internal package not allowed`).
  Trust `go build`.
- **Never `go test ./...` or `-race`.** Cap with `GOMAXPROCS=4 -p 2`.

## Start here

1. Discard the duplicate in `integrate/bluetooth-panel-main`; retire the four merged branches.
2. Close `sysc-276` after confirming the bar geometry on a live deploy.
3. Task 10, then Task 11 Steps 2 and 3. `sysc-275` remains open for the Bluetooth live gate.
