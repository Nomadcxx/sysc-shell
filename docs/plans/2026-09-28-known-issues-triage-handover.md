# Known issues triage — handover (2026-09-28)

Every item from the session's "known issues" queue was assessed against `origin/main a941d35`.
What no longer obtains was closed with evidence; what still obtains lives in bd and is commissioned
below. bd is the status source; this document is the evidence snapshot, not a task list.

## Closed during this pass (claim → proof)

| Issue | Disposition |
|---|---|
| sysc-456, sysc-496 | Fixed today: framing `Encoder` mutex (`34d70d6`) with a concurrent-encode integrity test; `SessionStarted` → `reopenViews` re-announces held views after crash-restart (`ed5f924`, `a941d35`) with a crash-once helper test. |
| sysc-186, sysc-117 | Already shipped earlier: `attachRunningIconsAtLocked` requests at `scale.Physical(18)` per bar and re-requests on configure (`88ac06c`, pinned tests); launcher rows resolve `Entry.IconName` through the shared tray icon cache (`3ed8e33`, `2ef3b78`, `popout_launcher.go:355`). |
| sysc-35 | `Metrics.Leased` appears nowhere in the tree; the removal already happened. |
| sysc-139 | Plugin panel hosts now size from the manifest (`attach()` uses `max(scr.w, …)`), so the 320-default mechanism is gone. Live re-check rides on sysc-138. |
| sysc-154 | The control-centre spine is on main (`controlcenter_*.go`, pages, leases). |
| sysc-255 | Blur is integrated: screencopy binding on main, `BlurRadius` wired in `panelhost.go`, sysc-632 closed as superseded, work archived 2026-09-28. |
| sysc-276 | `88ac06c` scales bar surfaces per output density — that is the fix. |
| sysc-499 | Depth clock integration is on main (`depthclock.go`, `pluginwallpaper.go`, live `sysc-wallpaper-depth-clock` layer). DP-1 calibration remains open as sysc-505. |
| sysc-566/567/579/580/612/293 | Duplicate hygiene: closed as copies of sysc-310, sysc-317, sysc-309, sysc-508 (twice), sysc-277. Canonical ids stay open. |

Ten stale `in_progress` claims from dead sessions (sysc-97, 104, 121, 137, 240, 265, 277, 312, 322,
335) were re-queued to `open`. Claims that may still be live were left untouched: sysc-548, sysc-590
(yesterday's), sysc-628 (today's) — verify no session owns them before claiming.

## Still obtains — commissionable

1. **sysc-590** P1 — micro-interaction primitives (state layer, ripple, shape morph, panel
   coordination). Plan committed on main: `docs/plans/2026-09-27-microinteraction-primitives.md`,
   7 tasks. This is the next tranche; execute it from the plan.
2. **sysc-585** P1 — sysc-plugins CI is red. Different repository (`~/sysc-plugins`); needs its own
   session. The `2026-09-28-robustness-tranche-handover.md` covers the GitHub-side sweep that feeds it.
3. **sysc-548** — bar surface Phase B (frosted, solid, islands). No frosted/island styling exists in
   the tree (`grep` over `internal/shell`, `internal/theming`). Verify the claim is dead, then finish
   from the bar-surface plan.
4. **sysc-310, sysc-317, sysc-309, sysc-508** — the canonical duplicate originals, all from before
   the control-centre redesign. Re-verify each against main, close if stale, otherwise action.
5. **sysc-97, 104, 121, 137, 240, 265, 277, 312, 322, 335** — re-queued 2026-09-02…09-17 items.
   Each needs a ten-minute "does this still obtain" check against current main before any
   implementation; several predate the control-centre and weather surface work and may be obsolete.
6. **sysc-505** — DP-1 wallpaper depth calibration (fill, stretch, original, panscan). Needs live
   eyes on the machine; nothing to code first.

## Gate baseline at `a941d35` — do not chase

- `internal/shell -race`: `TestPanelSectionValidationPrecedesMutation`, three `*Battery*` tests, and
  `TestRevealAnimationInvalidatesUntilDone` (clock-sensitive; fails identically at baseline).
- `tests/integration`: the 16 `TestTray*` failures need two outputs; this machine has one
  (sysc-641, sysc-586).
- Desktop runs `a1b16d0`; main is ahead (follow-ups + plugin fixes) by operator choice — no
  redeploy was requested.

## Ground rules

- Worktree off `origin/main`; never commit in the primary checkout (`main` lags there and it holds
  other sessions' uncommitted files).
- Run `bd` only in the primary. After every bd mutation the tracked JSONL truncates — recover with
  `sqlite3 .beads/beads.db "DELETE FROM export_hashes;" && bd export -o .beads/issues.jsonl`, verify
  with `wc -l` and a status cross-check against `bd list --status <st>`, then copy with an absolute
  path. A disk-full hiccup today left a mid-write truncation; the recovery ritual restored it
  (545 docs: 163 open, 3 in_progress, 379 closed).
- `GOMAXPROCS=4`, `-race` per package only, commit-message hook screens ordinary English words —
  write the message to a file, validate it against `~/.git-hooks/commit-msg`, then `--no-verify`.
- Deploy only through `scripts/deploy`, from a clean worktree, when the operator asks for it.
