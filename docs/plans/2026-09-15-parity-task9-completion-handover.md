# Parity Task 9 completion handover

Date: 2026-09-15.

Task 9 of the Noctalia v4 parity slice is **finished**: the conformance scan is green for the first
time since Task 1 committed it red. Tasks 10 and 11 are **not started**. This handover records what
landed, what moved underneath it on `main`, and one collision that must be resolved before anyone
writes another line of parity code.

Read `2026-09-14-parity-slice-execution-handover.md` for the slice's origin. Do not re-derive it.

## Receiving state

| Ref | Commit | Meaning |
|---|---|---|
| `origin/feature/noctalia-parity` | `a60db03` | Task 9 complete, pushed |
| `origin/main` | `6e619d6` | 16 ahead of the branch |
| local `main` | `93f1302` | 2 unpushed docs commits |
| `integrate/bluetooth-panel-main` | `5dce420` | 3 commits, **unmerged**, plus 15 dirty files |

`feature/noctalia-parity` is 1 ahead of `origin/main` and 16 behind. `git merge-tree` reports the
merge **clean**, and the file sets do not intersect at all.

**Branch package state:** every package green. `TestSurfaceSourcesCarryNoLegacyVisuals` passes.
**`main` package state:** that same scan **fails** on `main`, because Task 9 is not merged yet.
`internal/wallpaper` is green there.

## READ THIS FIRST: two agents did Task 9 in parallel

A concurrent Codex session (`01a09bd7-7476-79e3-aff9-77a223cbc06f`, transcript at
`~/.codex/sessions/2026/09/14/rollout-2026-09-14T03-36-23-01a09bd7-7476-79e3-aff9-77a223cbc06f.jsonl`)
converted the same sites this session did. Its closing message reads:

> "The control-centre worker kept the measured outer contracts and adjusted the inner widths; the
> process worker reconciled its viewport arithmetic; the widget worker threaded the active icon
> metric through construction. They're running their focused gates before I review and commit any of
> it."

That work was **never committed**. It sits uncommitted in
`/home/nomadx/sysc-shell/.worktrees/integrate/bluetooth-panel-main` as 15 modified files:
`controlcenter_pages.go`, `popout_process.go`, `widget.go`, `notifywidget.go`, `wifiwidget.go`,
`bar.go` and their tests — near-identical to the 14 files in `a60db03`.

**Decide which copy survives before merging anything.** They are two independent solutions to one
problem and will conflict. This session's copy is committed, pushed and verified green; the other is
unverified from here. Diff them rather than assuming either is better:

```bash
git -C /home/nomadx/sysc-shell/.worktrees/integrate/bluetooth-panel-main diff > /tmp/theirs.patch
git diff origin/main...feature/noctalia-parity -- internal/shell > /tmp/mine.patch
```

That worktree's **committed** three commits are a different matter and are wanted — see below.

## What this session completed

Task 9 drove the scan from 189 sites to zero across these commits, all merged to `main` except the
last:

| Commit | Contents |
|---|---|
| `5d93130` | Task 6 motion re-base and its two timing failures |
| `00ae032` | Task 5B `CardGap` |
| `c91f736` | Task 7 contrast floor 1.45 → 1.30 |
| `3f6a333` | Task 8 role mapping |
| `e573908` | Named spacing rungs `MarginXXXS`…`MarginXL` |
| `3918ef9`, `89cf590`, `a77ea8f`, `e3e0520`, `1292287`, `3295b32` | the conversion, file by file |
| `a60db03` | **the finish — control centre, process, widgets. Not yet merged.** |

Tasks 6, 5B, 7 and 8 are complete and on `main`. The network panel slice (`sysc-157`) was merged and
`sysc-268` filed for its deferred live credential gate.

### The three structural findings behind it

1. **The scan cannot see arithmetic.** It matches literals on node fields only, never the expressions
   that model them. Every panel had at least one. Read the arithmetic *before* editing: that caught
   the audio viewport, the network `networkHeaderHeight` sum, and the control-centre `quickWidth`.
   Not doing it first is how `launcherListHeight` broke.
2. **The control centre's spacing and widths are one composition.** A 228 column held two 110 tiles
   at a gap of 8; a weather row held four 143 slots at the same gap. Each filled its container
   *exactly*, so moving the gap alone overflowed them and `configure()` rejected the child. Widths
   are derived now (`ccTileW`, the forecast slot) and passed in.
3. **`buildWidgets`' int is not just padding.** `noCapsule` (−1) means "build a group's members with
   no capsule chrome". A `theme.Metrics` cannot carry that sentinel, so the row rides *alongside* the
   int rather than replacing it.

### One thing deliberately left wrong, with a comment saying so

`popout_process.go`'s `used` estimate still spends `12` on its first gap where the column it models
uses `MarginM`. Reconciling it grows the table by one pixel and pushes measured content past its
surface; `TestToggleMonitorOpensTallerThanTheOldGuess` catches that. Something in that stack measures
one taller than the terms say. **Find it before touching that line.**

## What moved on `main`, and what it costs us

Six commits since `2c19c8d`, all wallpaper lifecycle and picker work
(`4fda0ed`, `0e0c6e3`, `fe3ec66`, `5dbf75f`, `ccd410f`, `6e619d6`) touching
`internal/wallpaper/*`, `popout_wallpaper.go`, `registry.go` and the systemd unit. **Zero overlap**
with the parity branch. They cost us nothing; merge `main` in and continue.

### But the density re-base caused a live P1 regression

**`sysc-276` — "Legacy standard density shrinks the live bar after main upgrade"** (P1, in_progress).
Deploying `main` to archThink changed an existing bar from the legacy 48 px `standard` row to the new
31 px `default` row, because `MetricsFor` folds `DensityStandard` onto `DensityDefault` after our
re-base. This is a direct consequence of parity Task 5.

A design and plan already exist and are registered:

- `docs/plans/2026-09-14-density-config-migration-design.md`
- `docs/plans/2026-09-14-density-config-migration.md` — three TDD tasks

Its approach: keep the 31 px default, restore `48/6/4` through a **hidden `standard` compatibility
row**, classify selector-free documents as legacy, and record preset provenance in sparse writes.

**Those three tasks appear to be implemented already**, as the committed commits in
`integrate/bluetooth-panel-main` — `4e53acb fix(theme): restore legacy bar geometry`,
`3f5f286 fix(config): preserve density generation`, `5dce420 test(wayland): follow current bar
geometry`. None of them is on `main` or `origin/main`. **They should land.**

## What Task 11 now means

The migration plan above **supersedes most of Task 11 Step 1**. Do not write a second migration.

- **Step 1 (migration):** covered by the density migration plan. Land those three commits, confirm
  `sysc-276`, and record the density rename (`standard` → `default`, `mini`/`spacious` new) in the
  release note.
- **Step 2 (settings):** still owed and untouched. `internal/settings/registry.go:62-70` already
  lists all five densities; the **input radius row is missing** — there is only
  `appearance.radius` at `:70`. Add the second axis beside it.
- **Step 3 (live gate):** **now runnable.** The handover that said it was blocked is out of date.
  The laptop answers on `ssh -p 7777 nomadx@192.168.0.64`, runs Niri
  (`/run/user/1000/niri.wayland-1.1584.sock`) with `sysc-shell.service` active.
  Note `go.mod` pins `sysc-notify v0.1.0-rc.3`, which that host **cannot resolve** — build on archPC
  and copy the binary. Deploy is `install -m755` into `~/.local/bin/sysc-shell` then
  `systemctl --user restart`; always back the binary up first and verify by reading the journal, not
  by `is-active` alone, since a handler panic reads active while painting nothing.

## Task 10 is untouched

Widen `TestSurfaceCardPaddingFollowsDensity`, `TestSurfaceCardTitlesAreTitleRole` and
`TestSurfaceHeadingsCarryARole` (`surfacerole_test.go:90,135,193`) past `PanelMonitor`/`PanelSession`
to every `PanelID` that opens without external state. `cardsOf` is at `:54`; where it returns empty,
record the panel as card-less rather than `t.Fatalf` — the current `t.Fatalf("no cards found")` will
turn a widened loop red for panels that legitimately build none. The eleven ids are in `panel.go:8-20`.

## Hazards this session paid for

- **The tracker's hooks fight you.** `pre-commit` runs `bd sync --flush-only` and **stages the result
  on every commit**, so a file validated before staging is replaced at commit time — that is how
  three fork records reached `main`. `post-merge` imports with `--resolve-collisions`, which mints a
  fresh ID on every collision (270 → 272 → 273 → 276 → 277 were all one issue). **Verify the
  committed blob after the commit, never the working file before it.** Fixes belong in the database,
  not the file: the flush overwrites the file regardless.
- **An exemption marker must sit on the same line as its literal.** The scan reads line by line. A
  comment above it exempts nothing. This cost four sites across two commits.
- **Never cap a completeness check.** `grep … | head -8` hid 16 of 24 `buildWidgets` callers and a
  sentinel, and sent a whole increment down the wrong path.
- **A gate count of zero can mean the package did not build.** Always read build status alongside it.
- **`--no-verify` is forbidden**, and the `commit-msg` hook rejects substrings case-insensitively
  (`both` contains `bot`). Screen every message.
- **gopls lies in these worktrees** — `undefined: Registry`, `use of internal package not allowed`.
  Trust `go build`.
- **Never `go test ./...` or `-race`.** Cap with `GOMAXPROCS=4 -p 2`.

## Start here

1. Resolve the duplicate Task 9 in `integrate/bluetooth-panel-main` — diff, pick one, discard the other.
2. Land that worktree's three density commits; confirm `sysc-276`.
3. Merge `origin/main` into `feature/noctalia-parity` (clean), then merge the branch to `main` so the
   scan goes green there.
4. Task 10, then Task 11 Steps 2 and 3.
