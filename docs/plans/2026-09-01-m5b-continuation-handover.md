# Milestone 5 continuation handover — 5B in flight

Date: 2026-09-01
Kind: handover
Branch: `milestone/m5a-notifications`
Worktree: `/home/nomadx/.config/superpowers/worktrees/sysc-shell/milestone/m5a-notifications`

This handover picks up where `2026-09-01-m5a-execution-handover.md` ended. Tranche 5A is
complete. Tranche 5B (tray) is six tasks in of nine: Tasks 1–6 are implemented, tested, and
committed on the branch. Tasks 7–9 remain, plus two hardware-gated live checks and the merge
to `main`.

## Tracker

`bd` is the only tracker. Run with the repo database:

```bash
export BEADS_DB=/home/nomadx/sysc-shell/.beads/beads.db
bd list    # from the worktree
```

- `sysc-63` M5 Tranche 5A notifications — closed.
- `sysc-64` M5 Tranche 5B tray — **in_progress**. Close it when Task 8 lands and the
  two-output gate passes.
- `sysc-65` Qualify and release Milestone 5 — closed (qualification issue from the earlier
  audit round; reopen or create a new issue for the combined gate in Task 9 if preferred).
- When you start Task 7, create/claim issues per the plan with
  `bd create "..." --deps discovered-from:sysc-64` and commit `.beads/issues.jsonl` in the
  same commit as the code.

## What is done (branch `milestone/m5a-notifications`)

| Commit | Content |
|---|---|
| `df6e17c` | Task 1: `sysc-tray` pinned at `v0.1.0-rc.1`, protocol fixtures |
| `6f9ce42` | Task 2: generation-safe `internal/trayclient` (socket checks, handshake, ordered deltas, backpressure, reconnect) |
| `307d75c` | Task 3: item projection (`tray.go`), attention icon override, overlay-last half-size icon composition (`trayicon.go`) |
| `0e5716a` | Task 4: flattened bounded tooltips, activate/secondary/scroll commands, exactly-once reply tracker (`trayactions.go`) |
| `a105040` | Task 5: bounded menu model — depth 8, 512 rows, duplicate-ID rejection, back stack, accessible roles (`traymenu.go`) |
| `a763165` | Task 6: menu surface host (`traymenuhost.go`), `rootTrayMenu` root kind (`root.go`) |

Verification state: `go test -count=1 ./...`, `go vet`, and `gofmt` are all clean as of
`a763165`.

## Key decisions already made — do not re-litigate

1. **Overlay aux fallback for menus.** Task 6 Step 1 (live Niri probe of a 1x1 `xdg_popup`
   parented via `zwlr_layer_surface_v1.get_popup`) is **not yet run** — it needs the laptop
   with Niri. Per the design (`2026-08-30-tray-foundation-design.md`, "Surfaces" section),
   the fallback was implemented: one Overlay auxiliary surface with OnDemand keyboard behind
   `trayMenuHost.spec()`. When the live probe runs:
   - If Niri accepts the popup sequence, swap `spec()` to create the popup `surfaceUnit`
     and record the protocol trace in the Task 9 handover.
   - If Niri rejects it, keep the fallback and record the compositor evidence.
2. **Menu revision rules.** `trayMenuHost` tracks `revision` (visible tree) and `knownRev`
   (newest from service). Property-only updates (same visible ID sequence) apply in place
   and keep focus by entry ID. Structural updates defer while `interact` is set; only the
   newest deferred revision applies on `idle()`. A selection with `revision != knownRev`
   sends nothing, appends to `refreshAsked`, and returns stale.
3. **Reply correlation.** `trayReplyTracker` forgets a request before firing its retry, so
   replays are dropped. Only `ErrorStaleItem` retriggers; every other failure is terminal.
4. **Item identity.** Tray items are keyed by the full `ItemKey` (owner + path +
   generation); the projection lives in `trayState` under its own mutex and is
   output-independent. Generations never enter persisted preference tokens (Task 7 rule).
5. **Commit hook.** `~/.git-hooks/commit-msg` rejects any message containing the substrings
   `bot` or `agent` (among others). This bites innocuous words: "bottom" (say "lower"),
   "both" (say "each"). Check your message with
   `grep -iE '(bot|agent|claude|cursor|codex|gpt-[0-9]|llm)'` before committing.
6. **Pre-commit bd flush needs `BEADS_DB`** set explicitly, as shown above.

## Remaining work

### Task 7: Overflow and preferences (next)

Plan: `docs/plans/2026-08-30-tray-foundation.md` lines 160–177.
Create `internal/shell/traydrawer.go`, `internal/shell/trayprefs.go`; modify
`internal/config/config.go`. TDD per the plan's Step 1 list: geometry overflow, pinned-first
order, hidden exclusion with a recoverable hidden section, show/hide, pin/unpin, move
earlier/later, atomic reload, stable token selection (non-generic SNI ID, then non-generic
title), token collision (two live items sharing a token get neither preference), keyboard
accessibility. The drawer is a virtual-list root reusing tray item nodes — attach it to the
root chain like `trayMenuHost` does. Commit `feat(shell): add tray overflow preferences`.

### Task 8: Wiring and failure recovery

Plan: lines 181–210. Modify `cmd/sysc-shell/main.go` (mirror the `notifyclient` goroutine
wiring already there for `trayclient`), `internal/shell/registry.go`. Create
`tests/integration/tray_test.go` with the fake-service/fake-compositor matrix the plan lists
(two outputs, hotplug, generations, stale replies, popup failure, malformed siblings,
preference collision, root replacement, cleanup). The 5A integration test
(`internal/shell/integration_test.go`) is the pattern. Note: the `refreshAsked` slice in
`trayMenuHost` is a deliberate seam — Task 8 wires it to a real menu-refresh command send.
Run the plan's Step 3 gauntlet, then commit `test(shell): qualify tray presentation`.

### Task 9: Combined gate and completion handover

Plan: lines 214–231. Create `docs/plans/2026-08-31-milestone-5-completion-handover.md`
(plan's named date; use the real date if it drifts and note it), record the combined
notify+tray live matrix results, rerun the full automated suite from a clean checkout, and
commit `docs: record milestone 5 qualification`. Register the document in
`docs/plans/README.md` in the same commit.

### Hardware-gated live checks (laptop with Niri, two outputs)

- **5A live gate** (`t10-live` in the old session list): two-output Niri run for
  notifications — toasts per output, center, DND, inline reply.
- **Task 6 Step 1 popup probe**: 1x1 `xdg_popup` via `get_popup` with pointer serial grab.
  Outcome decides fallback vs popup path for `trayMenuHost.spec()` (see decision 1).
- **Task 9 combined matrix**: center plus drawer root replacement, tray menu from drawer,
  tooltip closure before roots, inline reply replacing a menu, both services restarting
  independently, shell restart, hotplug, mixed scale/transform, 60 minutes idle.

### Merge

After Task 9: merge `milestone/m5a-notifications` into `main` (it is 40+ commits ahead;
`main` is at `7a58765`). Keep the branch until the live gate passes, then close `sysc-64`
and the milestone issues with `bd close ... --reason "..."`.

## Housekeeping notes

- `main` at `/home/nomadx/sysc-shell` had a modified `.beads/issues.jsonl` at handover time —
  that is the tracker flush; commit or discard deliberately.
- The worktree list is crowded (`git worktree list`); the M5 work is only in
  `milestone/m5a-notifications`. `redesign/milestone-5` and `milestone/notifications-tray`
  are stale earlier attempts — do not build on them.
- Tests use `config.Default()` for `NewRegistry` and the `hostHarness` in
  `internal/shell/toasthost.go` to capture `wayland.AuxRequest`s. Reuse both.
