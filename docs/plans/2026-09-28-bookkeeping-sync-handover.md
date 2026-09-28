# Bookkeeping, sync and laptop deploy — handover (2026-09-28)

Picks up after a session that landed text input parity (sysc-623), did a four-repo bookkeeping
pass, archived and removed all local clutter, and deployed fresh `main` builds to the laptop.
Read "Gotchas" before touching beads or deploying.

## State now

| Repo | Local | Notes |
|---|---|---|
| sysc-shell | `main` = origin `e3281f9`, one checkout | Untracked in the primary checkout (left alone): `.tmp-go/`, four `docs/plans/2026-09-2{5,7}-*.md` drafts, `research_notes/`, a built `sysc-shell` binary |
| sysc-plugins | `main` = origin `b0cc10d` | Untracked handover docs and `docs/plans/artifacts/` left in place |
| sysc-notify | `main` = origin `9b919fd` | `docs/plans/…persistence-design.local-draft.md` is an older local draft kept aside |
| sysc-launch | `main` = origin `60f9f20` | clean |

A new session started after the cleanup and owns `.worktrees/feature/launcher-providers` and
`.worktrees/feature/ui-correctness-tranche` (sysc-591). Leave them alone.

### Laptop (`ssh -p 7777 nomadx@192.168.0.64`), everything from `main`, all `vcs.modified=false`

- sysc-shell `e3281f9` via `scripts/deploy --host laptop --force` (replaced the launcher-providers
  test build `65314cf`). Rollback: `~/.local/bin/sysc-shell.before-deploy-20260927T181812`.
- Plugins: 14 built from sysc-plugins `b0cc10d` into `~/sysc-plugins-deploy/b0cc10d/plugins/`; the
  existing 13 links in `~/.config/sysc-shell/plugins/` now point there. The old targets are listed in
  `~/.config/sysc-shell/plugin-links.before-20260928`. `games` is built but not linked (it was never
  installed). `~/sysc-main-test` and `~/sysc-plugins-main` are no longer referenced.
- Weather: `org.sysc.weather/bin/sysc-plugin-weather` rebuilt from sysc-shell `e3281f9`; the old binary
  is `.before-20260928`. It had failed `plugin.hello` (EOF) for two days; it now runs.
- sysc-notify `9b919fd` and sysc-launch `60f9f20` in `~/.local/bin` (`.before-20260928` copies kept);
  `sysc-notify.service` restarted.
- Verified after the restart: shell and notify active, 11 plugin processes up, no panic, error or
  view rejection. The calendar's "button does not fit in 240x32" rejections (stale plugin build) are gone.
- Not rebuilt: sysc-clipboard (origin has no `main`; its default branch is `feature/clipboard-v0.1`),
  sysc-tray (the laptop binary is a dirty `f413712` build; `sysc-tray.service` is disabled, and the
  shell logs `lstat /run/user/1000/sysc-tray: no such file`). The owner wants both audited later.
- The launcher calculator, emoji and desktop-action rows are gone from the laptop until
  `feature/launcher-providers` lands on main.

## Done this session

- Text input parity (sysc-623) merged to main and live-gated; closed.
- Audit fixes for GitHub #3–#11 landed from an uncommitted worktree as `8a6fb83..e03cb79`.
- GitHub issues closed with fix commits cited: sysc-shell #3–#11, #18, #19, #21, #23–#27, #29, #31;
  sysc-notify #1.
- sysc-plugins PR #3 (protonvpn handover doc) merged as `b0cc10d`.
- Every worktree, local branch and stash in all four repos was pushed to origin under
  `archive/2026-09-28/…` (sysc-shell 41, sysc-plugins 7, sysc-launch 1) and verified before removal.
  Leftover uncommitted work was committed as `archive: uncommitted work from <path>` commits on
  those refs. Recover any of it with `git fetch origin 'refs/heads/archive/*:refs/remotes/origin/archive/*'`.

## Still to action

1. **Close sysc-632 as superseded; that unblocks sysc-633, sysc-634, sysc-635 and sysc-641.** Its
   "uncommitted tooltip backdrop" work (formerly stash `0d73464`, now
   `archive/2026-09-28/stash-0-on-main-tooltip-blur-wip-backup`) is fully on main via PR #17
   (backdrop, border, bottom-edge placement, dwell de-duplication). Its panel-resize half is an earlier
   draft of PR #34; the only lines not in #34 are that draft's own `resizePanel` variant.
2. **Open GitHub issues, all still valid on `e3281f9`:**
   - #20 (sysc-633): `aux_surface.go` and `tooltip.go` do not re-check `h.alive` after `captureRegion`.
   - #22 (sysc-634): `NewInbound` and `Schedule.Due` have no non-test callers; `StateDegraded` is never set.
   - #28: toasts are still blur-exempt (D13, `toasthost.go`). Note sysc-635 is titled "Exempt toasts
     from the blur treatment", the opposite of #28's ask; reconcile that decision.
   - #30 (sysc-636): `notifycard.go` still has `cardIconSize = 56`, `RoleTitle` summary, `FillNone`.
3. **Open PRs, unfinished by the owner's account:** sysc-shell #33 (theming templates; it carries the same
   GH#7 gate now on main, so it merges cleanly) and #34 (panel resize feedback).
4. **Blocked:** sysc-plugins `feature/plugin-readme` needs `catalog.Entry.Readme` from sysc-shell
   `feature/plugin-store-ui` (`745a502`, archived), which is not on main.
5. **sysc-launch uncapped results** (Sept 3 edit removing the 50-result cap): archived as
   `archive/2026-09-28/uncapped-ranking-results`. It is not in use, since v0.1.0 and v0.2.0 both cap. The owner decides.
6. **Audits the owner asked for later:** sysc-clipboard and sysc-notify.
7. sysc-591 Tasks 5–6 (key-repeat retargeting) will conflict with text input parity's `keyboard.go`
   (enter/leave, `deliverKey`, `keyEvent`); rebase on main and resolve there.

## Gotchas

- **Beads export is broken (bd 0.17.7).** `bd export` skips every issue as "timestamp-only" and can
  leave `.beads/issues.jsonl` holding only the pending records (8 lines against 530). A pull re-imports
  the JSONL and can revert a `bd close`. Before committing the tracker, check the line count against
  `bd stats`. To write a change, start from `git show origin/main:.beads/issues.jsonl`, keep untouched
  lines byte for byte, and re-encode only the changed records with Go-style escaping (`&<>` as
  `&`…). `bd list --json` omits dependencies and comments, so never build the file from it.
- **Deploying:** only `scripts/deploy`; a startup guard restores the stamped build over hand copies.
  Build from a clean worktree at origin/main: untracked files in a checkout make `vcs.modified=true`,
  which the script refuses. The laptop has no Go or rsync; ship with `tar | ssh`.
- **Shell is zsh:** `$var:r…` is a modifier (use `${var}`), and unquoted `$list` does not word-split
  (run loops under `bash -c`).
- The commit-msg hook rejects substrings such as `cursor`, `bot` (so "both"), `agent`, `claude`.
