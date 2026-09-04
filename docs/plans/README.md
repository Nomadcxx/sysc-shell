# Design and Plan Register

Last updated: 2026-09-04.

Every design, plan, and handover this project has produced, with where it lives and whether it is still
live. Add a row here in the same commit that adds a document. A document that is not in this register is
one the project will lose.

## This file indexes documents. bd tracks state.

`bd ready` and `bd blocked` are authoritative for what is done, in flight, or gated. This register is
authoritative for what documents exist and what each one is for. Do not duplicate status between them: a
status written into a document header drifts, and one already did.

## Why this file exists

Documents were first written on milestone branches and reached `main` at different times. This was
resolved on 2026-08-30. Every registered document now lives on `main`, including the Milestone 2 set.

| Branch | Holds |
|---|---|
| `main` | Every registered document |
| `milestone/stable-bar` | Milestone 2 implementation branch, merged into `main` at `309f9d2` |
| `milestone/panels-controls` | Merged into `main` at `01cae98` (2026-08-31). Branch deleted after the local merge. |

Re-check with `git ls-tree -r --name-only <branch> -- docs/` rather than trusting this table blindly.

## Document kinds

Naming is `YYYY-MM-DD-<topic>[-<kind>].md`. A topic with no kind suffix is the implementation plan.

| Kind | Purpose |
|---|---|
| *(none)* | The executable implementation plan. Carries the `superpowers:executing-plans` header, exact files, TDD steps, and commit boundaries. |
| `-design` | The approved design. Fixes contracts and decisions before any plan is written. |
| `-execution-handover` | Commissions the next tranche. When that tranche has a completion snapshot or remaining items are in bd, retire it the same way as a progress handover. |
| `-design-handover` | Older name for the same thing. Not used for new work. |
| `-progress-handover` | In-flight only. When the work lands, or remaining items are in bd, **delete the file** and drop its register row. Do not patch it to keep it current. |
| `-completion-handover` | Snapshot after implementation: commit hashes, gate output, live observations, measurements, known defects. **Do not edit later. Do not delete.** A later correction goes in bd or the register, not back into the snapshot. |
| `-project-handover` | Same retirement rule as a progress handover. Remaining-work maps drift; harvest into bd, then remove. |
| `-audit-handover` | Commissions an audit. |
| `-audit-report` | The audit's findings and verdict. |

## Milestone 0 and 1 — foundation and architectural proof

Status: **complete and merged to `main`.**

| Document | Kind | Branch | State |
|---|---|---|---|
| `2026-08-26-sysc-shell-design.md` | design | `main` | Approved; amended 2026-08-27 by the plan audit. The project-wide architecture. Still the governing document. |
| `2026-08-26-development-orchestration.md` | process | `main` | Live. Defines the design, dependency, review, performance, hardware, and handoff gates. |
| `2026-08-26-architectural-proof.md` | plan | `main` | Executed. Milestone 1. |
| `2026-08-27-plan-audit-handover.md` | audit-handover | `main` | Closed. |
| `2026-08-27-plan-audit-report.md` | audit-report | `main` | Closed, but its owner decisions D3/D4/D5 remain referenced. D3 (SVG) was settled by the Tranche 3A charter's icon-asset policy. |
| `2026-08-28-architectural-proof-review-fixes.md` | plan | `main` | Executed. |
| `2026-08-28-implementation-handover.md` | handover | `main` | Closed. Milestone 1 to Milestone 2. |
| `../roadmap.md` | roadmap | all branches | Live. The milestone sequence and every exit gate. Amended 2026-08-27. |
| `../prior-art.md` | assessment | all branches | Live. Noctalia, DMS, dgop, `dankgo`, notification and tray references, with licensing. Architectural only — it records no widget-level findings; those live in the Tranche 3A design. |

## Milestone 2 — stable bar on every output

Milestone 2 implementation and review corrections are on `main`. The remaining exit gate is `sysc-5`,
which needs two connected outputs, physical hotplug and pointer checks, and the full idle duration.

| Document | Kind | Branch | State |
|---|---|---|---|
| `2026-08-29-stable-multi-output-bar-design-handover.md` | commission | `main` | Closed. Geometry and ownership contracts. Its implementation status is historical. |
| `2026-08-29-stable-multi-output-bar-design.md` | design | `main` | Owner-approved. |
| `2026-08-29-stable-multi-output-bar.md` | plan | `main` | The five-task correction plan, not the missing original. Executed. |
| `2026-08-29-stable-multi-output-bar-progress-handover.md` | progress-handover | `main` | Historical pre-correction state. Do not edit from another tranche. |

### Known gap: the original Milestone 2 plan is missing

The progress handover records "all 16 plan tasks implemented", but **no 16-task plan exists on any branch
or on disk**. `2026-08-29-stable-multi-output-bar.md` was created already as the five-task correction plan
(`f483cc8`), after those 16 tasks had been committed. The original plan was never committed.

Consequence: Milestone 2's implementation cannot be reviewed against its own plan. The work is
recoverable only from the 19 `feat`/`test`/`fix` commits in `main..milestone/stable-bar`, running from
`74706ff` to `f3bdae1` for the original implementation plus four later `fix` commits. Those 19 commits do
not map one-to-one onto the handover's "16 tasks", and no document reconciles them.

This is the concrete reason this register exists. Do not repeat it: commit the plan before executing it.

The formerly uncommitted `internal/platform/wayland/policy_test.go` landed in `72cb086`; `sysc-13` tracks
that resolved defect.

## Milestone 3 — built-in widget foundation

Split into four reviewed tranches by the charter. 3A and 3B are merged.

| Document | Kind | Branch | State |
|---|---|---|---|
| `2026-08-30-built-in-widget-foundation-execution-handover.md` | commission | `main` | Historical commission that produced the approved design and plan. |
| `2026-08-30-live-gate-to-tranche-3a-execution-handover.md` | execution-handover | `main` | Current receiving-agent handover. Start Tranche 3A now; `sysc-5` remains open as deferred hardware qualification. |
| `2026-08-30-built-in-widget-foundation-design.md` | design | `main` | Owner-approved, audited, amended. Records D1–D8 and the design-audit outcome. |
| `2026-08-30-built-in-widget-foundation.md` | plan | `main` | Complete. 16 tasks (0–15). Audit findings applied. Ready to execute. |
| `2026-08-30-built-in-widget-foundation-audit-handover.md` | audit-handover | `main` | Closed. Commissioned the technical and design audits. |
| `2026-08-30-built-in-widget-foundation-audit-report.md` | audit-report | `main` | Closed. Verdict: proceed with named corrections; all seven applied at `9a114eb`. Verified assumption 4 live. |
| `2026-08-31-tranche-3a-implementation-audit-report.md` | audit-report | `main` | Implementation vs spec + Q/A. Finding 1 (major, `apply` does not re-layout) fixed at `b669622`. Findings 2–4 (DropHost on failed `hostBecameReady`; reload boundary-change test; focus/title/close invalidation test) applied at `b43ace7`. Closed. |
| `2026-08-30-core-metrics-design.md` | design | `main` | Owner-approved, audited, amended. D1–D8; D3, D6, D7 and D8 amended 2026-08-31. Graph node ships in 3B (charter deviation D5). |
| `2026-08-30-core-metrics.md` | plan | `main` | Executed. 13 tasks. |
| `2026-08-30-core-metrics-completion-handover.md` | completion-handover | `main` | Closed. Implementation complete; superseded by the audit and its corrections. |
| `2026-08-31-tranche-3b-audit-brief.md` | audit-handover | `main` | Closed. Commissioned the 3B audit. |
| `2026-08-31-core-metrics-audit-report.md` | audit-report | `main` | Closed. Verdict: proceed with named corrections. Two majors (D6 grain, D7 meter/graph); all six applied. |
| `2026-08-31-core-metrics-audit-corrections.md` | corrections | `main` | Closed. All six findings applied and evidenced. |
| `2026-08-30-weather-and-visual-vocabulary-design.md` | design | `main` | Owner-approved, audited, amended. D1–D9; D4 is a recorded charter deviation (icons as a font). D3 amended and `Weather.Reconfigure` added 2026-08-31. |
| `2026-08-30-weather-and-visual-vocabulary.md` | plan | `main` | 11 tasks; 9–11 (tooltip) were cuttable and were implemented. Audit amendments applied 2026-08-31. |
| `2026-08-30-weather-and-visual-vocabulary-completion-handover.md` | completion-handover | `main` | Implementation complete; live matrix owner-deferred. |
| `2026-08-31-weather-and-visual-vocabulary-audit-report.md` | audit-report | `main` | Closed. Verdict: amend before executing; all six amendments applied. |
| `2026-08-31-power-design.md` | design | `main` | Owner-approved, audited. D1–D6. Signatures provisional on `sysc-19`. Scope sentence aligned with D4 2026-08-31. |
| `2026-08-31-power.md` | plan | `main` | Executed. 7 tasks. Task 0 reconciled against `sysc-metrics@v0.2.0`. |
| `2026-08-31-power-audit-report.md` | audit-report | `main` | Closed. Verdict: rebase onto post-D6 `Metrics`, then wait for Task 0. Findings 1, 3, 4, 5 applied; finding 6 is 3D's `Tone` comparison. |
| `2026-08-31-power-completion-handover.md` | completion-handover | `main` | Implementation complete; live matrix owner-deferred. |
| `2026-08-31-m3-code-quality-sweep.md` | audit-report | `main` | Ponytail pass over 3A–3D. Report only; cuts listed, not applied. |
| `2026-08-31-m3-spec-review-sweep.md` | audit-report | `main` | Implementation vs designs, plans, charter, and 2026-08-31 audits. |
| `2026-08-31-m3-quality-findings-review.md` | audit-handover | `main` | Classifies ponytail cuts as M4-reserved, specified M3 API, real-dead, or optional shrink. For a second agent to confirm. |
| `2026-08-31-milestone-3-implementation-audit-report.md` | audit-report | `main` | Independent merged-M3 spec and correctness audit, including Grok classification corrections. |
| `2026-08-31-m3-audit-defect-corrections.md` | plan | `main` | TDD plan for the five defects filed by the merged-M3 implementation audit. |

### Tranche state

| Tranche | Scope | State |
|---|---|---|
| 3A | Clock, date, Niri workspace, focused-window title; service lifetime; per-output widget instances | Merged. |
| 3B | CPU, memory, filesystem, block and network rates | Merged. Audited; all six findings applied. |
| 3C | Battery and remaining time | Merged. Live matrix owner-deferred. |
| 3D | Weather, icons, error tone, tooltip | Merged. Live matrix owner-deferred. |

## Milestone 4 — panels and standard controls

Designed, planned, reviewed, executed and merged to `main` at `01cae98`. Spec shortfalls
`sysc-38`–`sysc-40` closed at `b4c4bc6`. Live Niri checklists remain owner-deferred.

| Document | Kind | State |
|---|---|---|
| `2026-08-30-panels-and-controls-prior-art.md` | assessment | Noctalia v5 and DMS v1.5.3 inventories with file:line evidence. |
| `2026-08-30-panels-and-controls-research.md` | research | Niri capability claims, source-verified against niri main. |
| `2026-08-30-panel-foundation-design.md` | design | Tranche 4A. Owner-approved; amended for M3 auxiliary ownership and one process-wide root chain. |
| `2026-08-30-panel-foundation.md` | plan | Tranche 4A, **13 tasks**. Ready to execute once its prerequisites clear. |
| `2026-08-30-panel-foundation-parallel-execution-handover.md` | execution-handover | Starts Tasks 1, 5 and 8 while Tranche 3A continues, then joins the remaining plan after M3 integration. |
| `2026-08-30-settings-osd-theme-catalog-design.md` | design | Tranche 4B. Owner-approved; amended for the review. D1–D10. |
| `2026-08-30-settings-osd-theme-catalog.md` | plan | Tranche 4B, 14 tasks with three mandatory review boundaries. |
| `2026-08-30-milestone-4-review.md` | review | Closed. Both blockers fixed in `3fc026c`; findings tracked as `sysc-16`. |
| `2026-08-31-milestone-4-post-m3-audit-report.md` | audit-report | Reconciles 4A/4B with landed M1-M3 APIs and corrects the executable plans. |
| `2026-08-31-milestone-4-execution-handover.md` | execution-handover | Commissions corrected 4A and the three reviewed 4B slices in the existing M4 worktree. |
| `2026-08-31-m4-spec-review-sweep.md` | audit-report | Implementation vs 4A/4B designs, post-M3 audit, charter, roadmap gate. Live matrices owner-deferred. |
| `2026-08-31-m4-code-quality-sweep.md` | audit-report | Correctness and reliability pass over landed M4. Report only. |
| `2026-08-31-m4-spec-shortfall-corrections.md` | plan | TDD plan for sysc-38, sysc-39, sysc-40. |
| `2026-08-31-m4-post-shortfall-polish.md` | plan | Hide-lock, apply supersede, status IPC, virtual-list focus. |
| `2026-08-31-m4-post-shortfall-spec-check.md` | audit-report | Re-check of sysc-38–40 vs 4A/4B after the shortfall merge. |
| `2026-08-31-m4-post-shortfall-quality-polish.md` | audit-report | Leftovers applied; remaining ceilings. |

### Tranche state

| Tranche | Scope | State |
|---|---|---|
| 4A | Panel machinery, placement, rounding and shadows, button/label/separator/tabs, matugen theming, clock/calendar, system-monitor and session/power popouts, IPC | Merged to `main` at `01cae98`. Live checklist unrun. |
| 4B | Controls, settings modal and registry, OSD with audio/brightness services, stock themes, template catalog | Merged to `main` at `01cae98`. Live checklist unrun. `sysc-38`–`sysc-40` closed at `b4c4bc6`. |

### Post-M3 reconciliation

- The system-monitor returns because 3B qualified and pinned `sysc-metrics@v0.2.0`. It reuses
  `services.Metrics`, selector leases, histories, and M3's `KindGraph`; M4 adds no wrapper service or
  module dependency.
- A configuration reload leaves panel surfaces mapped. Tearing them down would have dismissed 4B's
  settings modal on every change made inside it.
- Colour fields that palette generation now owns are removed from the schema, and unknown keys are
  rejected at load, so a stale file fails with its field path named rather than silently doing nothing.
- Every template apply hook gained a reverse, so disabling one removes the include line and file it
  added to another application's configuration.

### Resolved plan decisions

- Runtime binaries use `exec.LookPath`, argv slices, bounded contexts, and no shell. Missing optional
  tools hide the capability or select the documented fallback.
- 4B remains one design and plan, executed as three reviewed slices: Tasks 1-8, 9-11, and 12-14.

## Bar visual parity (post-M4 chrome)

Live DMS grim on the owner's laptop vs default sysc-shell bar. Capsules, `SurfaceContainer` fill, and workspace pills. Not a widget-roster port. Epic `sysc-43`, executed and merged 2026-09-02. Remaining live judgment is `sysc-54`; card-fill contrast is `sysc-104`.

| Document | Kind | State |
|---|---|---|
| `2026-08-31-bar-visual-parity-research.md` | research | Live grim of DMS on eDP-1; Noctalia from installed settings.json (package not running). Assets under `docs/plans/assets/2026-08-31-bar-visual-parity/`. |
| `2026-08-31-bar-visual-parity-design.md` | design | D1–D9, including the numbered-pill amendment. Executed. |
| `2026-08-31-bar-visual-parity.md` | plan | Six TDD tasks. Executed and merged. |

## Power panel (battery, profiles, session)

Owner-approved 2026-09-02. Grows the M4 session panel. Does not start Milestone 6.
Gamer-mode's freeze/kill engine stays a plugin.

| Document | Kind | State |
|---|---|---|
| `2026-09-02-power-panel-design.md` | design | One `session` surface: battery card, `powerprofilesctl` row, existing session actions. Right-click bar battery. IPC `power` alias. |
| `2026-09-02-power-panel.md` | plan | Six TDD tasks: parser, LookPath gate, right-click, three-card tree, IPC alias, owner-deferred live Niri list. |

## Chrome catalogue (shared buttons, pills, hover)

Owner-commissioned 2026-09-02 after the live session panel mapped but painted as
accent-filled rectangles. Design-first: one reusable chrome language, original
SVGs, DMS/Noctalia as prior art. Not a general toolkit. Do not execute product
code from the handover.

| Document | Kind | State |
|---|---|---|
| `2026-09-02-chrome-catalogue-design.md` | design | Owner-approved native chrome, motion, Material Symbols subset, semantic theme composition, and session-panel consumer. |
| `2026-09-02-chrome-catalogue.md` | plan | Owner-directed implementation-first plan for semantic chrome, native transitions, Material subset, session composition, and focused regression checks. |

## Theme system parity

Owner-approved 2026-09-02. Extends the existing native Go palette and renderer
with one resolved composition model. Presets provide defaults; colour, density,
typography, shape, opacity, elevation, motion, and accessibility remain
independent axes. Epic `sysc-142` depends on the chrome catalogue.

| Document | Kind | State |
|---|---|---|
| `2026-09-02-theme-system-parity-design.md` | design | Owner-approved semantic palette, composition, type, density, shape, opacity, elevation, motion, accessibility, settings, and live-reload contract. |
| `2026-09-02-theme-system-parity.md` | plan | Owner-directed implementation-first plan with resolver/configuration and shell-wide composition review checkpoints. |

## Milestone 5: notifications and system tray

Shell presentation is on `main`. The services this milestone consumes are tagged candidates in their
own repositories (`sysc-notify v0.1.0-rc.2`, `sysc-tray v0.1.0-rc.1`); those tags are full daemons.
Their default branch `main` is still the original docs-only commit. The live matrix is `sysc-97`.

| Document | Kind | State |
|---|---|---|
| `2026-08-30-notifications-and-tray-prior-art.md` | assessment | Noctalia, DMS, `sysc-notify`, and `sysc-tray` inventory. |
| `2026-08-30-notifications-and-tray-research.md` | research | Decisions D1–D14. |
| `2026-08-30-notifications-foundation-design.md` | design | Tranche 5A notification presentation. |
| `2026-08-30-notifications-foundation.md` | plan | Tranche 5A, 10 tasks. |
| `2026-08-30-sysc-notify-persistence-design.md` | service design | Persistence addendum owned by `sysc-notify`. |
| `2026-08-30-tray-foundation-design.md` | design | Tranche 5B tray presentation. |
| `2026-08-30-tray-foundation.md` | plan | Tranche 5B, 9 tasks. |
| `2026-08-30-notifications-and-tray-audit-handover.md` | audit-handover | Commissions the cross-repository design and plan audit for Tranches 5A and 5B. |
| `2026-08-30-notifications-and-tray-audit-report.md` | audit-report | Redesign required for both tranches; records service-contract, lifetime, release, persistence, and menu blockers. |
| `2026-08-31-notifications-and-tray-integration-design.md` | design | Corrected cross-repository ownership, parity, shared M3/M4 primitives, limits, release order, and gates. |
| `2026-09-02-milestone-5-completion-handover.md` | completion-handover | Snapshot at `2604d77`. Leave as written. Live remainder is `sysc-97`. |
| `2026-09-01-milestone-5-shell-prerequisites.md` | plan | `AuxUpdate` and process-wide interactive-root ownership required before 5A. |
| `2026-09-03-notification-centre-design.md` | design | Post-5A first-party centre. DMS 1.5.3 popout + toast chrome (D1–D17). Amends D10. Clipping defect is D17. |
| `2026-09-03-notification-centre.md` | plan | TDD: unclip toasts, bar bell, `PanelNotifications`, Current/History, header commands (`sysc-150`), DMS toast chrome. |
| `/home/nomadx/.config/superpowers/worktrees/sysc-notify/redesign/v0.1/docs/plans/2026-08-31-sysc-notify-v0.1.md` | service plan | Executable notify service and candidate/stable release gates. |
| `/home/nomadx/.config/superpowers/worktrees/sysc-tray/redesign/v0.1/docs/plans/2026-08-31-sysc-tray-v0.1.md` | service plan | Executable tray service and candidate/stable release gates. |

Original source worktree: `/home/nomadx/.config/superpowers/worktrees/sysc-shell/milestone/notifications-tray`.
Redesign worktree: `/home/nomadx/.config/superpowers/worktrees/sysc-shell/redesign/milestone-5`.

## Milestone 6: external widget and plugin host

Milestone 6 hosts trusted external processes and ships five reference plugins. Progress lives in bd.
Implementation is on `milestone/plugin-host` (main already merged into it) and is expected on `main`
from the session that holds that branch. Do not start a second M6 branch.

| Document | Kind | Purpose |
|---|---|---|
| `2026-09-01-milestone-6-plugin-host-design.md` | design | Process model, protocol, UI vocabulary, manager, limits, reference suite, and tranche boundaries. |
| `2026-09-01-milestone-6a-plugin-host-timer.md` | plan | Host kernel, local manager, bounded draft protocol, and Timer vertical slice. |
| `2026-09-01-milestone-6b-world-clock.md` | plan | Revisioned patches, bounded scheduling, lists, drag/reorder, and World Clock. |
| `2026-09-01-milestone-6c-notes.md` | plan | Multiline retained editing, safe note storage, autosave, and external reconciliation. |
| `2026-09-01-milestone-6d-weather.md` | plan | Shared Open-Meteo client, structured tooltips, Weather settings, and forecast panel. |
| `2026-09-01-milestone-6e-screen-recorder.md` | plan | Recorder configuration, exact process ownership, replay buffer, context, and live gate. |
| `2026-09-01-milestone-6e-recorder-handover.md` | completion-handover | Automated PluginRecorderGate output and live GSR 6.0.1 observations on the laptop. |
| `2026-09-02-recorder-panel-design.md` | design | Composite camera pill, sysmon-placed panel, full schema on the same store as Settings. |
| `2026-09-02-recorder-panel.md` | plan | Glyphs, bar pill, panel header, host-composed settings, command wiring, live Niri check. |
| `2026-09-02-recorder-panel-handover.md` | completion-handover | Recorder panel live gate on the laptop: pill, record/stop/replay, hide_inactive, panel layout failure. |
| `2026-09-01-milestone-6f-protocol-qualification.md` | plan | Protocol v1 fixtures, abuse and recovery gates, packaging, live qualification, and handoff. |

## Plugin visual polish (post-M6 chrome)

Secondary to Milestone 6. Two tranches: prior-art IA, then Hallmark voices. Do not
execute while `sysc-66` is open. Not QML/Luau compatibility.

| Document | Kind | Purpose |
|---|---|---|
| `2026-09-02-plugin-visual-polish-research.md` | research | Noctalia official plugins + DMS PluginComponent/BasePill vs Timer, World Clock, manager. |
| `2026-09-02-plugin-visual-polish-audit-report.md` | audit-report | Hallmark audit of plugin trees and host paint. 2 critical · 6 major · 2 minor. |
| `2026-09-02-plugin-visual-polish-design.md` | design | D1–D13. Host chrome + trees. Approved 2026-09-02. |
| `2026-09-02-plugin-visual-polish.md` | plan | T1 six tasks (`sysc-127`), T2 three tasks (`sysc-128`). Blocked on `sysc-66`. |

## Milestone 7: shell breadth

Not designed as a whole. Launcher v1 and the wallpaper picker have designs.
Remaining M7 clusters (clipboard, session adapters, control center, desktop
widgets) are still unordered.

| Document | Kind | State |
|---|---|---|
| `2026-09-01-launcher-prior-art.md` | assessment | Noctalia 5 list + first-class DMS DankLauncherV2 (this machine: full/compact) + Go reuse. §6/§9 superseded 2026-09-01: Elephant is first-class prior art, GPL-3 not a constraint (owner decision). No live grim in-tree. |
| `2026-09-01-milestone-7-launcher-design-handover.md` | execution-handover | Commissions the launcher design and implementation plan. Do not write product code from it. |
| `2026-09-01-launcher-design.md` | design | M7 launcher v1: Noctalia list chrome 560×500, fzf + Elephant-weighted scoring, ported usage store, pinned desktopentry, Niri argv spawn, `/` prefix registry, glyph icons behind an Icon seam. 16 numbered decisions. |
| `2026-09-01-launcher.md` | plan | 13 tasks: Task 0 reconciliation, pinned deps, TDD slices for exclusions/exec/scoring/history/prefix/service/spawn, panel projection, IPC + Super+Space bind, owner-deferrable live Niri gate. |
| `2026-09-02-sysc-launch-extraction-design.md` | design | Owner-approved boundary for history-preserving extraction into `github.com/Nomadcxx/sysc-launch`, a presentation-neutral library plus diagnostic CLI. |
| `2026-09-02-sysc-launch-extraction.md` | plan | Nine-task history-preserving extraction, public API and CLI, local two-module gate, v0.1.0 release, and immutable shell pin. |
| `2026-09-03-wallpaper-design.md` | design | M7 wallpaper picker (`sysc-146`): noctalia-gslapper chrome + native virtualized grid, gSlapper-first with awww/swaybg static fallback, owned per-connector sockets, Restore exception. Overrules the roadmap socket-client-only line. |
| `2026-09-03-wallpaper.md` | plan | 14 tasks (`sysc-149`): assignment store, IPC, argv, config, library, service, panel chrome, apply/restore, startup reconcile, live Niri gate. Audit pins applied 2026-09-03. |
| `2026-09-03-wallpaper-audit.md` | audit-report | Pre-implementation audit. Accepted: bd claim, All token, `SplitN`, `-r`, `cfgHook`, spawn stub. Dirty `main` and AGENTS.md output line left as ops, not plan text. |
| `2026-09-04-wallpaper-execution-handover.md` | execution-handover | Commissions implementing `sysc-149`. Session context: chrome mix, gSlapper-first, Waytrogen sockets, fan-out All, Restore exception. Do not reopen those. |
| `2026-09-05-launcher-ui-handover.md` | execution-handover | Commissions the launcher UI audit. Three diagnosed defects first: the 50-result cap in sysc-launch that ends the browse list in the E's, missing client-side key repeat, and frecency judged only after the cap is lifted. Then the chrome, which has never had a design pass. |
| `2026-09-03-control-center-execution-handover.md` | execution-handover | Commissions the Noctalia-shaped control-centre design and implementation plan (`sysc-158`); DMS supplies quick-control density. |

## Milestones 7 remainder and 8: not yet designed

| Milestone | Scope | Note |
|---|---|---|
| 7 (after launcher) | Clipboard, network/BT/MPRIS, control center, desktop widgets | Wallpaper has `2026-09-03-wallpaper-design.md`. Do not fold the rest into the launcher slice. |
| 8 | Rendering qualification | Requires measured evidence against `wl_shm`. |

## Sibling repositories

Verified 2026-09-02 against git, GitHub, and `go list -m`. `sysc-shell` pins the **tags**, not
whatever is checked out on each repo's `main`. Checking out `/home/nomadx/sysc-notify` or
`/home/nomadx/sysc-tray` on `main` shows docs only; that is not what this module compiles.

| Repository | Pin in go.mod | Where the pin lives | State |
|---|---|---|---|
| `/home/nomadx/sysc-wayland` | `v0.2.1` | `main` (`6bef268`) | Tagged and pushed. Object-argument fix. |
| `/home/nomadx/sysc-metrics` | `v0.3.0` | `main` (`263a6f7`) | Core counters, sysfs battery, CPU package temperature, GPU usage/temperature (`nvidia-smi` for NVIDIA). |
| `/home/nomadx/sysc-notify` | `v0.1.0-rc.2` | `origin/redesign/v0.1` (`f0d76af`). `origin/main` is still `32da2b5` (docs only). | Full daemon (`cmd/sysc-notify`, protocol 1.1, presenter socket). Never merged to default branch. Stable `v0.1.0` waits on `sysc-97`. Not installed or running on the development machine. |
| `/home/nomadx/sysc-tray` | `v0.1.0-rc.1` | `origin/redesign/v0.1` tag `30d266e` (branch tip one docs commit later). `origin/main` is still `04ca018` (docs only). | Full daemon (`cmd/sysc-tray`). Same merge and install gap as notify. |
| `/home/nomadx/sysc-launch` | `v0.1.0` | `main` (`fb6f73c`) | Library plus one-shot CLI (`query` / `launch`). Not a daemon. Shell hosts `launcher.NewService` in-process. |

## Work selection

Use `bd ready` and `bd blocked`. This register does not duplicate execution order or issue status.
