# Design and Plan Register

Last updated: 2026-08-31.

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
| `-execution-handover` | Commissions the next tranche: scope, fixed decisions, required evidence, stop conditions. Written *for* the receiving agent. |
| `-design-handover` | Older name for the same thing. Not used for new work. |
| `-progress-handover` | State of in-flight implementation: what is done, what is not, what is blocked. |
| `-completion-handover` | Written after implementation: commit hashes, gate output, live observations, measurements, known defects. |
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
| `2026-08-30-panel-foundation-design.md` | design | Tranche 4A. Owner-approved; amended for the review. D1–D13. |
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

Live DMS grim on the owner's laptop vs default sysc-shell bar. Capsules, `SurfaceContainer` fill, and workspace dots. Not a widget-roster port. Epic `sysc-43`. Do not execute until the design is approved.

| Document | Kind | State |
|---|---|---|
| `2026-08-31-bar-visual-parity-research.md` | research | Live grim of DMS on eDP-1; Noctalia from installed settings.json (package not running). Assets under `docs/plans/assets/2026-08-31-bar-visual-parity/`. |
| `2026-08-31-bar-visual-parity-design.md` | design | D1–D9. Awaiting owner approval. |
| `2026-08-31-bar-visual-parity.md` | plan | Six TDD tasks. Do not execute until the design is approved. |

## Milestone 5: notifications and system tray

Milestone 5 now has researched designs and executable plans for both tranches. Product work still needs
the M4 surface vocabulary and tagged service releases.

| Document | Kind | State |
|---|---|---|
| `2026-08-30-notifications-and-tray-prior-art.md` | assessment | Noctalia, DMS, `sysc-notify`, and `sysc-tray` inventory. |
| `2026-08-30-notifications-and-tray-research.md` | research | Decisions D1–D14. |
| `2026-08-30-notifications-foundation-design.md` | design | Tranche 5A notification presentation. |
| `2026-08-30-notifications-foundation.md` | plan | Tranche 5A, 13 tasks. |
| `2026-08-30-sysc-notify-persistence-design.md` | service design | Persistence addendum owned by `sysc-notify`. |
| `2026-08-30-tray-foundation-design.md` | design | Tranche 5B tray presentation. |
| `2026-08-30-tray-foundation.md` | plan | Tranche 5B, 8 tasks. |
| `2026-08-30-notifications-and-tray-audit-handover.md` | audit-handover | Commissions the cross-repository design and plan audit for Tranches 5A and 5B. |
| `2026-08-30-notifications-and-tray-audit-report.md` | audit-report | Redesign required for both tranches; records service-contract, lifetime, release, persistence, and menu blockers. |
| `2026-08-31-notifications-and-tray-integration-design.md` | design | Corrected cross-repository ownership, parity, shared M3/M4 primitives, limits, release order, and gates. |

Original source worktree: `/home/nomadx/.config/superpowers/worktrees/sysc-shell/milestone/notifications-tray`.
Redesign worktree: `/home/nomadx/.config/superpowers/worktrees/sysc-shell/redesign/milestone-5`.

## Milestones 6 to 8: not yet designed

| Milestone | Scope | Note |
|---|---|---|
| 6 | External widget and plugin host | Versions the vocabulary the built-in widgets prove. |
| 7 | Shell breadth | Launcher consumes Milestone 4's text field, virtual list, keyboard focus, and popout placement. |
| 8 | Rendering qualification | Requires measured evidence against `wl_shm`. |

## Sibling repositories

| Repository | Head | State |
|---|---|---|
| `/home/nomadx/sysc-metrics` | `v0.2.0` (`c46fab8`) | Tagged and pushed. Core counters plus sysfs battery aggregate. Thermal omitted. |
| `sysc-wayland` | `v0.1.1` | Pinned dependency. Qualified. |
| `/home/nomadx/sysc-notify` | `32da2b5` | Design repository. No release tag; M5 needs the persistence and socket contracts implemented and tagged. |
| `/home/nomadx/sysc-tray` | `04ca018` | Design repository. No release tag; M5 needs the socket, item snapshot, and menu contracts implemented and tagged. |

## Work selection

Use `bd ready` and `bd blocked`. This register does not duplicate execution order or issue status.
