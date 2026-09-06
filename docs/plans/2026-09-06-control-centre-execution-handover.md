# Control Centre Tranche Handover

Date: 2026-09-06. Branch point: `main` at `1faf28f`.

## What this document is

`docs/plans/2026-09-03-control-center-execution-handover.md` commissioned the
control-centre design and implementation plan. It was never executed: neither
`2026-09-03-control-center-design.md` nor `2026-09-03-control-center.md` exists
on any branch, and `sysc-158` has sat `in_progress` since 2026-09-03 with no
output.

**That handover is still the assignment. Read it first.** Its owner direction,
visual references, twelve decisions, project constraints, and plan shape all
still hold and are not repeated here.

This document exists because 107 commits landed under it while it sat, and a
session that follows it literally today will reconcile against a repository
that no longer matches its "read before designing" list. It says three things:

1. why the control centre is the next tranche and rendering qualification is not;
2. what the tracker is currently lying about, and what to fix before trusting it;
3. what changed under the 2026-09-03 commission, named precisely.

## Why this tranche

The owner asked whether M8 is next. It is not. `docs/roadmap.md` Milestone 8 is
**rendering qualification** — adding an EGL/OpenGL ES renderer beside `wl_shm`
for a *named failing case* with measured evidence. No such case has been
measured, and the milestone explicitly "does not expand shell features". It is
gated on evidence nobody has gathered, not on a queue position.

The largest outstanding cluster is the control centre, Milestone 7 item 4. It is
five tracked issues, one of which gates the other four:

```
sysc-158  Design and plan the control centre        in_progress  P1  (paperwork; blocks 154)
sysc-154  Control centre: shell access spine        open         P1  (blocks 155/156/157)
sysc-157  Control centre Network service and page   open         P2  (NetworkManager)
sysc-155  Control centre Bluetooth service and page open         P2  (BlueZ)
sysc-156  Control centre Media service and page     open         P2  (MPRIS)
```

Nothing else open has that shape. The rest of the board is follow-ups
(`sysc-79`…`sysc-88`, `sysc-192`, `sysc-193`, `sysc-194`), bugs, live gates, and
plugin work already in flight.

## Step 0: the tracker is not currently trustworthy

`bd ready` and `sysc-154`'s dependency list are both wrong today, in a way that
will send the next session to reconcile against work that already shipped.

### `sysc-154`'s gates are stale

It records five blockers. Their real state:

| Blocker | Recorded | Actual |
|---|---|---|
| `sysc-152` Implement DMS notification centre | dependency | **closed** 2026-09-03 |
| `sysc-142` Theme system parity | in_progress | branch `feature/theme-system-parity` is **fully merged into `main`** (`git log feature/theme-system-parity ^main` is empty) |
| `sysc-176` Theme system parity | in_progress | duplicate of `sysc-142` |
| `sysc-184` Theme system parity | in_progress | duplicate of `sysc-142` |
| `sysc-158` Design and plan | in_progress | **genuinely outstanding — the only real gate** |

So the control centre is gated on paperwork alone. Verify the theme-parity claim
before closing anything: `feature/theme-system-parity` merged at `41a06bc`, and
the axes it promised are live (`internal/theme`, `Metrics.CompactControl`,
`Metrics.StandardControl`, `theme.RoleCaption/RoleLabel/RoleTitle`,
`theme.PaletteNames()`, the motion tokens driving `internal/shell/animation.go`).

### Eleven duplicate clusters, ~13 redundant open issues

`bd list` currently holds the same work under two or three ids:

```
Theme system parity ................ sysc-142(ip)  sysc-176(ip)  sysc-184(ip)
System monitor panel ............... sysc-102      sysc-109      sysc-121(ip)
Card fill indistinguishable ........ sysc-104(ip)  sysc-110
Power panel ........................ sysc-134      sysc-137(ip)
M6E Screen Recorder ................ sysc-73(ip)   sysc-135
M6A Tasks 6-9 ...................... sysc-76(ip)   sysc-122
M7 live Niri gate (launcher) ....... sysc-93       sysc-118(ip)
M5 live matrix ..................... sysc-97       sysc-119
Group the sysmon widgets ........... sysc-54       sysc-113
Launcher theme icons ............... sysc-86       sysc-117
Metrics.Leased total ............... sysc-35(ip)   sysc-112(ip)
```

`sysc-161` already records one pair (`sysc-76`/`sysc-125`) as "the same issue
with one content hash", so the cause is known and unfixed. Of 80 open issues,
roughly 13 are copies — `bd ready` overstates the board by a sixth.

**Commissioned first action**, before any design work:

1. For each cluster, keep the id other issues already depend on, close the rest
   with `bd close <id> --reason "duplicate of <kept-id>"`, and re-point any
   dependency edge onto the kept id.
2. Re-point `sysc-154` at the kept theme-parity id, then close that id if the
   merge check above holds. Drop the `sysc-152` edge.
3. Confirm `sysc-158` is then the sole blocker: `bd dep tree sysc-154`.
4. Commit `.beads/issues.jsonl` with that work in one chore commit.

Do not fold this into a code commit, and do not skip it — the whole point of
`bd ready` is that it is honest about what is workable.

## What changed under the 2026-09-03 commission

The 2026-09-03 document's reconciliation instructions are stale in named ways.

**Its worktree pointer is dead.** It says the chrome implementation is active in
`/home/nomadx/sysc-shell/.worktrees/feature/chrome-catalogue-implementation`.
That path does not exist; `.worktrees/feature/` holds only `running-apps-pill`.
`sysc-141` is **closed** and the chrome catalogue is on `main`. Reconcile against
`main`, not against a worktree.

**Theme parity is no longer "another agent will execute it later".** It landed.
Design against the shipped API.

**Two panels exist that the 2026-09-03 list does not mention.** `PanelID` is now
eight values (`internal/shell/panel.go:9-17`), including `PanelWallpaper`. The
new implementation owners to read alongside its list:

- `internal/shell/popout_wallpaper.go` — the most recently designed panel, and
  the closest prior art for a control centre: labelled compact chrome rows, an
  inline dropdown, a virtualised grid, and derived (not stored) selection state.
- `internal/shell/menu.go` — the in-panel dropdown, already reused by
  `popout_plugins.go` and the wallpaper picker.
- `internal/render/mask.go` — antialiased ring and glyph masks added
  2026-09-06 (`5da46b3`). Use `RingMask` for any control-centre border; do not
  build a border as the difference of two fills.

**Three shell behaviours changed that a rail-plus-body layout will hit
immediately:**

1. **Scroll is now pointer-routed.** `scrollAt` in `internal/shell/panelhost.go`
   picks the deepest scrollable under the pointer and falls back to the first
   when the pointer is over none. Before `2885264` the wheel always went to the
   first scrollable in tree order. A control centre with a scrollable rail *and*
   a scrollable body was going to hit exactly that bug; it is fixed, and the
   design can assume per-region scrolling works.
2. **A panic in a widget's state seam presents as a blank shell, not a crash.**
   `sysc-wayland` v0.2.1 recovers panics raised in event handlers into a
   `dispatch: panic handling opcode=N senderID=M` error. `wl_output.done` is
   opcode 2 and runs `NewHost` → `buildBar` → `bar.apply`, so a widget missing
   its `refresh`/`format` seam makes the service read `active (running)` with
   nothing painted and nothing in the journal. `sysc-188` tracks the swallowed
   startup error; `adoptBar` (`internal/shell/registry.go`) now defers its unlock
   so the same panic no longer deadlocks `Registry.Close` on shutdown.
3. **A bar widget must supply `refresh` **or** (`inner` and `format`).** This is
   the contract `Bar.applyLocked` enforces. Any control-centre bar trigger has
   to satisfy it; a widget with neither is the failure in point 2.

**`sysc-194` is new** (2026-09-06): the wallpaper picker's engine strip reports
which engine paints an output but cannot choose one, because no engine override
exists anywhere. It is listed here only so the control-centre design does not
assume an engine can be selected from a quick control.

## Operating rules on this machine

These have each cost a session already. They are not optional.

- **`sysc-shell` runs from systemd on both machines.** Redeploy is
  `go build -o <tmp> ./cmd/sysc-shell`, `mv` over `~/.local/bin/sysc-shell`,
  `systemctl --user restart sysc-shell.service`. Never spawn it by hand
  alongside the running one.
- **Never `pkill -f` anything matching your own binary name.** The pattern
  matches the shell running the command and kills the live session.
- **`go test ./internal/shell` really runs `loginctl terminate-session self`.**
  Shadow `loginctl` (and `systemctl`) with a stub earlier on `PATH` or it logs
  the owner out.
- **Never `go build`/`go test ./...` with `-race`.** This box is zram-only swap
  with 16-way linking; a repo-wide race build hard-locks it. Cap `-p` and
  `GOMAXPROCS` (4 works).
- **The commit-msg hook rejects AI/agent attribution by naive substring**, so
  ordinary English trips it — "both" and "Hallmark" are confirmed hits. Check the
  message against the real pattern before committing; no `Co-Authored-By`
  trailer.
- **Run `bd` from `/home/nomadx/sysc-shell`**, never from a worktree, or it
  creates a second near-empty database and answers from the wrong graph.
- A live Niri runs here but `NIRI_SOCKET` is unset; derive it.

## The tranche, in order

1. **Tracker reconciliation** (Step 0 above). Chore commit. No code.
2. **`sysc-158`: design and plan.** Execute the 2026-09-03 handover with the
   corrections above. `superpowers:brainstorming` for the design, owner approval,
   then `superpowers:writing-plans`. Both documents committed to `main` as
   docs-only commits and registered in `docs/plans/README.md` in the same commit.
   Close `sysc-158`.
3. **`sysc-154`: the spine.** Only once `bd ready` shows it. Home, Audio,
   Monitor, Power, Weather, Calendar and Notifications functional; Media,
   Network and Bluetooth present as disabled destinations with a clear
   unavailable treatment and no action.
4. **`sysc-157` / `sysc-155` / `sysc-156`**, each behind its own approved design
   for NetworkManager ownership and secret agent, BlueZ pairing and trust
   boundary, and MPRIS discovery and position handling respectively. Not in this
   tranche.

## Acceptance

This handover is retired when `sysc-158` is closed with both documents on `main`
and registered, and `bd dep tree sysc-154` shows it ready. At that point delete
this file and its register row; the 2026-09-03 handover retires with it.

## Do not

- Do not start rendering qualification (M8). It needs a measured failing case
  against `wl_shm` first, and none has been recorded.
- Do not write control-centre product code before `sysc-158` closes.
- Do not implement NetworkManager, BlueZ or MPRIS in this tranche.
- Do not delete or absorb Settings, Launcher, Monitor, Session, Calendar,
  Notifications or Wallpaper. The control centre is another entry to them.
- Do not pre-empt audit `sysc-189`. The running-apps pill landed at `88ac06c`
  and is under post-implementation audit per
  `2026-09-06-running-apps-pill-audit-handover.md`; its findings may change
  `internal/shell/runningapps*.go` and the bar's icon scaling.
- **Check `git status` before you start.** This checkout is shared and other
  sessions leave uncommitted work in it — at the time of writing, `sysc-188`
  (swallowed startup errors, `registry.Close` deadlock) is in flight here across
  `cmd/sysc-shell/main.go`, `internal/shell/registry.go` and a new
  `registry_close_test.go`. Never `git checkout --`, `git stash` or `git add -A`
  over work you did not put there; snapshot first if you must move it.
