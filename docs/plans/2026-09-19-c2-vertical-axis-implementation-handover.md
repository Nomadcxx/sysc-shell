# C2 vertical axis: implementation handover

- Date: 2026-09-19
- Issue: `sysc-322`
- Plan: [`2026-09-19-c2-vertical-axis.md`](2026-09-19-c2-vertical-axis.md),
  nine test-first tasks on `milestone/m9-c2-vertical-axis`
- Design: [`2026-09-19-c2-vertical-axis-design.md`](2026-09-19-c2-vertical-axis-design.md),
  decisions D7–D12, owner-approved in brainstorming on 2026-09-19
- Status: issued for execution by a fresh agent. The flow is **audit →
  implement → dev-machine gate → laptop gate → merge**, with stop conditions
  at every stage.

## What the brainstorm tranche produced

- The fork's resolution is decided and owned: D7 unifies `ArrangeBar`'s two
  composition paths, then generalises once. Every open question the commission
  raised has a recorded decision (D7–D12) in the addendum, committed as
  `9260e43` together with the commissioning execution handover and its register
  row.
- The implementation plan exists, is registered, and `sysc-322` references it
  in its bd design notes (committed as `f44706e`).
- Discovered work is in bd: the commission had already filed `sysc-419` (the
  top-edge-hardcoded body gap); the brainstorm found nothing further needing an
  issue — the cross-clamped-title behaviour is recorded as an addendum note and
  a plan test, and its readability is a gate question, not work.

## Stage 1 — audit before any code

Audit the two documents against the tree on `main`. This is a verification
pass, not a re-derivation:

1. Every row of the plan's anchors table points at the thing it names — line
   numbers drift, names do not. Confirm each function still exists with the
   shape the table claims.
2. Each decision D7–D12 as written in the addendum is executed by exactly one
   plan task, and no task executes a decision the addendum does not record.
3. The scope exclusions hold: no task paints the overflow indicator chrome or
   wires a non-zero reserve (D11 keeps that with `sysc-313`), and no task
   rotates text.
4. `bd show sysc-322`: its dependencies (`sysc-314`, `sysc-319`) are as the
   plan assumes — `sysc-314` merged (`c6a289a`), the `sysc-319` epic stays open
   through Milestone 9 and does not gate this slice (precedent: C1 and the
   overflow slice both ran while it was open).

**Stop condition:** anything disagrees, stop, write down the disagreement, and
report back. Do not patch the documents to match the code (or the reverse) and
continue on your own authority.

## Stage 2 — implement the plan

- Create the worktree for `milestone/m9-c2-vertical-axis` from `main`; work
  test-first per AGENTS.md: the smallest failing check before each change, one
  commit per task, every commit message screened through the real hook.
- Claim the work from the primary checkout: `bd update sysc-322 --status
  in_progress`, and commit `.beads/issues.jsonl` with the work.
- **T1's bar is absolute:** the twelve pinning tests in `internal/ui` pass
  unchanged through the unification refactor. If any of them needs rewriting in
  T1, the refactor is wrong — stop and report.
- T8's gate block is the unit gate: per-package race runs only, `gofmt` and
  `go vet` clean, `go.mod`/`go.sum` untouched.

## Stage 3 — dev-machine live gate

Plan task T9, run exactly as written: top and lower regression first (observing
`sysc-419`'s fix, not just deriving it), restart — never a configuration write
alone — between edge changes, then left and right under DP-1 rotation, `grim`
screenshots answering capsule rounding and clamped-title readability, the zone
measured differentially from `tile_size`. Record every observation and every
unclaimed item in the plan file itself. Rotate the output back to `normal`
afterwards.

## Stage 4 — the laptop gate

`ssh -p 7777 nomadx@192.168.0.64`

The laptop is the check the dev machine cannot give: real side edges on
hardware, at 1920x1080 scale 1.25 (logical 1536x864) — the machine that caught
sub-project B's clipping defects every test and the first live check missed.

- **The laptop is shared.** Check whether another session is using it before
  deploying. Back up the shell binary *and* the bar configuration before
  changing either, and restore both when the gate is done — two prior sessions
  took the owner's bar down by skipping this.
- Establish how the laptop runs its shell before deploying (a previous session
  built there and kept the old binary beside the running one). If the route for
  getting the branch onto the laptop is not obvious from the laptop's own
  state, ask the owner rather than improvising a push.
- Verify on a side edge: the bar anchors against the screen with its gap on the
  screen edge; the tray stacks (`sysc-tray.service` runs on the laptop); the
  capsule rounding reads correctly at scale 1.25; no item is cut mid-glyph and
  nothing vanishes without a count rising; a panel opened from the bar appears
  beside it with its attached column square.
- `grim` over ssh needs the session environment:
  `XDG_RUNTIME_DIR=/run/user/1000 WAYLAND_DISPLAY=wayland-1` — confirm against
  `ls /run/user/1000/` on the laptop rather than assuming the values carry
  over. Prefer screenshots pulled back over ssh; ask the owner to eyeball the
  screen if anything is ambiguous.
- Remember the scale trap: layout measures at the physical size and converts
  back, so judge clipping on the screenshot, not on logical numbers.
- Record the observations in the plan file. A failure found here is filed with
  `bd create ... --deps discovered-from:sysc-322`, then either fixed on the
  branch and re-gated, or recorded as unclaimed — **do not merge with an open
  defect.**

## Stage 5 — merge

All of the following, or no merge: the Stage 1 audit clean; T1–T8 green with
the horizontal suite unchanged; T9's observations recorded; the laptop gate
verified.

- Merge `milestone/m9-c2-vertical-axis` into `main` with a merge commit, the
  house pattern (`5eb1068` is the precedent). Re-run the T8 gate block on
  `main` after the merge.
- Close the slice from the primary checkout: `bd close sysc-322 --reason "..."`
  and commit `.beads/issues.jsonl`.
- Update the plan's register row in `docs/plans/README.md` to record the merge
  and its date, the way `2026-09-18-bar-edge-and-reserve.md`'s row does — in
  the same commit.
- Do not push without the owner's say-so.

## Stop conditions

The audit disagrees; T1 needs a pinning test rewritten; a unit gate or live
gate fails and the fix is not mechanical; the laptop is unavailable or mid-use;
anything turns up that the addendum does not have a decision for. In every
case: record what you found (bd for defects, `discovered-from:sysc-322`), leave
the tree clean, and report back. Unclaimed behaviour is recorded as unclaimed,
never faked.

## Machine traps

The plan's Carried traps section is the base list. On top of it:

- The `commit-msg` hook rejects AI attribution outright, not just the word
  list — a co-author trailer fails it (verified 2026-09-19). Plain messages
  only.
- Write "the lower edge": the b-word trips the substring rule.
- The shell has no argument parsing; it is driven over its IPC socket.
- Never `pkill -f` a binary name also typed in the command; kill by pid from
  `pgrep -f 'scratchpad/<name>'`.
- `bd export` truncates the JSONL on read commands; rebuild and commit in one
  command, and check `wc -l` and `git diff` before committing.
