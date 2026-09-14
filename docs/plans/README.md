# Design and Plan Register

Last updated: 2026-09-15.

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

## Animated gradient paint

Owner-approved 2026-09-07. Host-owned CPU ramp on rects and alpha masks, with
a looping offset. First consumer is the centred bar wordmark. Not a Noctalia
plugin API. Epic `sysc-217`.

| Document | Kind | State |
|---|---|---|
| `2026-09-07-gradient-paint-design.md` | design | D1–D12. `GradientPaint` on `ui.Node`, token stops, ping-pong wordmark, named idle-frame exception. |
| `2026-09-07-gradient-paint.md` | plan | Six TDD tasks (`sysc-218`–`sysc-223`): sampler, rect/mask fill, node field, looping animator, bar widget and default layout, architecture amendment. |

## Theme system parity

Owner-approved 2026-09-02. Extends the existing native Go palette and renderer
with one resolved composition model. Presets provide defaults; colour, density,
typography, shape, opacity, elevation, motion, and accessibility remain
independent axes. Epic `sysc-142` depends on the chrome catalogue.

| Document | Kind | State |
|---|---|---|
**The design is superseded by `2026-09-11-noctalia-parity-design.md`.** Its
mechanism survives and is restated there; its numeric ladders were derived from
observation rather than measurement, and every one of them differs from Noctalia
v4.7.7. The plan was executed, but its live Niri gate was never run and is
re-inherited by the superseding design rather than discharged.

| Document | Kind | State |
|---|---|---|
| `2026-09-02-theme-system-parity-design.md` | design | **Superseded 2026-09-11.** Owner-approved semantic palette, composition, type, density, shape, opacity, elevation, motion, accessibility, settings, and live-reload contract. D1, D3, D4, D6, D11, D13 and D15 carried forward verbatim into the superseding design. |
| `2026-09-02-theme-system-parity.md` | plan | Owner-directed implementation-first plan with resolver/configuration and shell-wide composition review checkpoints. Executed and merged 2026-09-05 (`1e28e30..41a06bc`); Task 13's live Niri gate, ten configurations needing human confirmation, remains unrun. |

## Noctalia v4 parity

Owner-approved approach 2026-09-11: visual parity with Noctalia v4,
near-indistinguishable, as the standing target for first-party surfaces.
Supersedes the theme-system-parity design above. Second document of the parity
tranche, after the backdrop blur design it depends on for the opacity floor.

| Document | Kind | State |
|---|---|---|
| `2026-09-11-noctalia-parity-design.md` | design | D1–D15. Re-bases spacing, radius, type, density and motion onto measured v4.7.7 constants; adds a second radius ladder for inputs and a `Display` type role; maps v4's 16 colour roles onto all 49 without deletion. Records the resolution of open `sysc-104`: the nested-surface floor drops 1.45:1 → 1.30:1, text floors unchanged, `Outline` keeps 3:1 while `OutlineVariant` is exempt. Calibration is measured, not assumed — bar 62 px and capsule 50 px both resolve at 2×, title em confirms 16 pt at 96 DPI, so v4 constants are logical px and points convert ×4/3. |
| `2026-09-11-noctalia-parity.md` | plan | Thirteen tasks, executing **three** designs: the parity ladders, the token-conformance gate as Task 1, and component parity as Tasks 5A and 5B (one master control dimension with per-shape odd/even forcing; padding resolved to ladder rungs). Carries the token-conformance design as its Task 1. That task is committed **red on purpose**: 96 of the 105 literal sites are `Gap` or `Padding`, the exact ladder Task 2 re-bases underneath them, so the gate lands against an enumerated worklist and Tasks 2–9 drive it green. Two hazards are called out because each would ship a runtime fault: `textRoleCount` is *derived* (`int(theme.RoleMono)+1`), so adding `RoleDisplay` without re-deriving it indexes past a fixed-size array while the existing guard test still passes; and the 1.45 floor is **not** a constant — `derive()` carries only 4.5/3.0 and the separation emerges from ladder steps, so the change targets the two assertions at `theme_test.go:252,255`. Re-inherits the live Niri gate `sysc-142` closed without running, with clipping in the densest surfaces named as the expected failure since type and density both shrink. |
| `2026-09-14-density-config-migration-design.md` | design | Keeps the 31 px current default and standard preset, restores `48/6/4` bar geometry through a hidden `standard` compatibility row while retaining current control metrics, treats selector-free existing documents as legacy, and writes an explicit standard preset so new sparse files retain their generation across reload. |
| `2026-09-14-density-config-migration.md` | plan | Three TDD tasks: project the hidden compatibility row from current metrics, classify selector-free parsed documents as legacy while preserving missing-file defaults, record preset provenance in sparse writes, and align Wayland surface expectations before the combined integration gate. |

## Parity tranche execution handover

Commissions execution of the whole 2026-09-11 tranche. Seven designs, five plans, none started.

| Document | Kind | State |
|---|---|---|
| `2026-09-12-parity-tranche-execution-handover.md` | execution-handover | **Blur half discharged 2026-09-13**; the rest still commissions. Recommended starting at backdrop blur Tasks 1–3 and stopping at the Task 3 measurement gate: it resolved the tranche's largest unknown first, unblocked parity's opacity floor, and stayed clear of the concurrent surface-polish pass. Records the hazards, the two corrections not to re-introduce (padding is not 14; stacking's consumer is the media card), the five unverified claims, and that no bd issue yet named any tranche work. Superseded for state by the continuation handover below. |
| `2026-09-15-parity-task9-completion-handover.md` | completion-handover | **Live.** Snapshot after the four-way landing at `9cb2acb`, with `main` green on all nine packages and the conformance scan passing for the first time since Task 1 committed it red. Records the order the merges had to take and why — density first (`d69b4d4`, the P1 `sysc-276` regression), parity Task 9 second (`2f97256`), Bluetooth last (`9cb2acb`) against a tree already carrying both — and that landing Task 9 first *created* the `widget.go` conflict the Bluetooth merge then resolved. Carries the three structural findings: the scan cannot see the arithmetic that models its literals, the control centre's spacing and widths are one composition (a 228 column held two 110 tiles at a gap of 8 and filled it exactly), and `buildWidgets`' int carries a `noCapsule` sentinel a metrics row cannot express. Names the one deliberate inconsistency left in `popout_process.go` and why touching it fails a test. Records a duplicate Task 9 left uncommitted by a concurrent session in `integrate/bluetooth-panel-main`, now redundant and to be discarded. Tasks 10 and 11 remain; Step 1 is superseded by the density migration, Step 2 still owes the input radius row, Step 3's live gate is runnable again. |
| `2026-09-14-parity-slice-execution-handover.md` | execution-handover | Superseded by the completion handover above. Commissioned Tasks 6, 5B, 7, 8, 9, 10 and 11 from `30b5f06`; 6, 5B, 7 and 8 are on `main`, 9 is complete on the branch. Its claim that the live gates cannot run is out of date — the laptop is reachable again. |
| `2026-09-13-parity-tranche-continuation-handover.md` | execution-handover | Live. Records backdrop blur as implemented and measured on `feature/panel-backdrop-blur` (twelve commits, rebased onto `79e9899`, unmerged): both stop gates cleared at 4.94 ms blur and 5.67 ms readback, ~11 ms per open. Carries the five corrections the live work forced — xrgb8888-only offers, the exactly-one-buffer pool, the rounded-mask clip, the shell owning a panel's `Style`, and the opacity bound sitting in three places — plus the proof that `capture_output_region` is output-relative, which D3 assumed. Records the post-rebase defect where `AttachedPanelStyle` painted an opaque root over the blur, and why that condition must survive future changes. Commissions the remaining tranche in order (smoothness, parity, media, stacking, template), notes that **no bd issue names smoothness, stacking, conformance or the template**, leaves the menu/drawer blur scope open for the owner, and records two hazards this session paid for: a second agent deploying over `~/.local/bin/sysc-shell`, and screenshot-diff verification being invalid on a changing screen. |

## Component parity (evidence audit)

Written 2026-09-11 after auditing the parity design against source. That design
sourced the global ladders correctly and the component layer not at all: it had
read 2 of v4.7.7's 52 components and none of its 261 composition modules, and it
carried two constants imported from **v5**. This design covers how a control gets
its size and how a surface gets its padding, and records the corrections.

| Document | Kind | State |
|---|---|---|
| `2026-09-11-component-parity-design.md` | design | D1–D8, D3 and D8 extended 2026-09-12. Every control derives from `baseWidgetSize` 33 × a per-control ratio, with odd forced on icon buttons and checkboxes and even on toggles and sliders — a *system*, not a table. Padding is a margin-ladder rung per surface, sourced from nine cards, **seven panels**, a settings pane and the launcher delegate: every panel insets at `marginL` with a `margin2L` reserve, the inter-card gap is `marginM` (the control centre's `marginL` is the lone exception), card interiors are `marginM`, dense cards `marginS`; `NBox` defines none. Hero type is an inline multiplier (`fontSizeXXXL × 1.75` ≈ 42 pt), not a role. The inverted hero card is a plain `mPrimary` rectangle and needs no primitive. **Corrects two committed designs.** Two gaps left open deliberately: panel dimensions diverge (v4's standard panel is 440 wide) and v4 settings panes use **no cards**, which is a composition change owned by the chrome catalogue. Has no plan of its own — executed as Tasks 5A and 5B of `2026-09-11-noctalia-parity.md`. |

## Token conformance

Third and last design of the parity tranche. Answers why the chrome catalogue
and theme system parity, both approved 2026-09-02, did not stop surfaces
re-deriving their own chrome. Enforcement is partial rather than absent: the
existing source scan bans legacy aliases and synthetic bold but says nothing
about a bare number.

| Document | Kind | State |
|---|---|---|
| `2026-09-11-token-conformance-design.md` | design | D1–D10. Adds one regexp to the existing `TestSurfaceSourcesCarryNoLegacyVisuals` scan rather than a new gate, plus a marked `token-exempt:` idiom carrying its reason at the site, and widens three behavioural checks past `PanelMonitor`/`PanelSession`. Measured: 105 literal geometry assignments in `internal/shell`, of which 58 `Gap` and 38 `Padding`; `internal/ui` and `internal/render` are clean, so the existing scan's package scope is already right. **Revises the tranche order** — the gate lands as task one of the parity plan, red against an enumerated worklist, because 96 of the 105 are the spacing ladder that parity re-bases underneath them. |

## Rendering smoothness (Milestone 8 remainder)

Closes the rest of `sysc-202`. The epic names four cases — animation frame time,
large blurred panels, image-heavy grids, CPU/power — and the blur design
measured the second. Also keeps the architecture document's unkept L193 promise
that "the bar milestone adds rectangle damage", which Milestone 2 never
delivered and no issue tracked.

| Document | Kind | State |
|---|---|---|
| `2026-09-11-rendering-smoothness-design.md` | design | D1–D9. Rectangle damage via a `Damaged` sibling callback on `HostCallbacks` rather than changing `Render`'s signature; dirty geometry owned by the tree, with `Scheduler.dirty` untouched; a theme-resolved pacing cap replacing the fixed 16 ms `animTick`; and coalescing the wallpaper picker, which today rebuilds its whole tree and repaints 4.1 MiB per decoded thumbnail. Full-buffer damage stays the default and the fallback — damage is an optimisation, never a correctness boundary. |
| `2026-09-11-rendering-smoothness.md` | plan | **Tasks 1–4 landed 2026-09-13** on `feature/rendering-smoothness` (`71ae0df`, `a0d28c8`, `98858d8`, `6a65808`): a pure `DamageSet` accumulating old **and** new bounds; `damageRects` submitting per-rectangle damage with a clipped full-buffer fallback, leaving every existing surface unchanged; the pacing cap in `animateSurface`, which always publishes the settling frame so no settled value is left unpainted; and dropping the picker's per-raster tree rebuild. No surface opts into rectangle damage — the path is built, not adopted. **Task 5 (coalescing) is dropped:** it flushed its last pending publish through an `icons.Worker.Progress()` channel that does not exist, leaving the final tile of a burst unpainted, and its premise is moot because the render path already coalesces twice — `publishSurface` drops on a full 8-deep channel, and `Scheduler.Invalidate` folds repeated calls into one redraw. **Task 6 is outstanding** and cannot retire `sysc-202` as written: `sysc-256`'s live gates are unrun and its two-output case is unrunnable on either machine here. The plan now carries an Amendments section — treat every line number in it as stale and locate anchors by symbol. |

## Surface stacking

One container kind whose children share its box, so content can sit over a
background image. Small because the hard parts already hold: paint walks
children forward and `Hit` walks them in reverse, so last-child-is-topmost is
already true in both.

| Document | Kind | State |
|---|---|---|
| `2026-09-11-surface-stacking-design.md` | design | D1–D9. `KindStack` modelled on `layoutCapsuleChild`; measures as the **max** of its children where a column sums, honouring an explicit `Height` to avoid the disagreement the `KindCapsule` case documents; scrim as an ordinary `FillScrim` child rather than a property; bilinear background sampling shared with the blur design. One consumer — the control-centre weather card — or it does not ship. |
| `2026-09-11-surface-stacking.md` | plan | Six tasks. Task 1 is end-to-end by necessity: `kindcoverage_test.go` fails the package for a kind that is not both measurable and paintable, so the kind cannot land half-built. Paint needs **one line** — `KindStack` joins the existing `KindColumn, KindDropZone, KindSegmented` case, because paint already walks children forward and `Hit` already walks them in reverse. Task 2 pins topmost-wins hit testing so a later refactor cannot silently break it. Task 4 depends on the blur plan's `paintImageSmooth`. Task 5 is a real consumer gate with a revert branch: if the weather card does not need a full-bleed image, delete the kind. |

## Media service, widget and page

The one feature slice of the parity tranche, and the design `sysc-156` says it
requires. Follows `2026-09-06-connectivity-and-media-prior-art.md` rather than
repeating it.

| Document | Kind | State |
|---|---|---|
| `2026-09-11-media-service-design.md` | design | D1–D11. MPRIS service as a peer of `audio.go`; hand-rolled on `godbus/dbus/v5` because no binding covers discovery — `leberKleber/go-mpris` is a client but has none and is stale since 2022, `go-music-players/mpris` is a server library — which satisfies AGENTS.md's rung order rather than bypassing it. Discovery via `NameOwnerChanged`, explicit active-player rules, art off the paint path, position interpolation in the service on the prior art's eight-consumer argument. Bar widget is `media`, verified free against `knownItems`. Re-slices `sysc-156` into service, widget and page so the service stops depending on the control-centre spine. |
| `2026-09-11-media-service.md` | plan | Ten TDD tasks against a `bus` seam and a fake bus — never a real session bus. Service skeleton and dependency (`godbus/dbus/v5 v5.2.2`, verified present in the local module cache so `GOPROXY=off` resolves); discovery; deterministic active-player selection; metadata decode where every field is a checked assertion and `file://` art only; position interpolated in the service with an injected clock and no timer; commands that fail quietly and re-select; registry ownership with a consumer-counted lifetime and deliberately **no** OSD relay; the `media` widget with a test that fails if a player picker appears in it; the page, left light because it waits on `sysc-253`; then the tracker split. |

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
| `2026-09-05-running-apps-pill-prior-art.md` | assessment | DMS RunningApps vs Dock vs FocusedApp; Noctalia Taskbar vs Dock vs ActiveWindow. Bar pill maps to RunningApps chrome; Niri windows are the model. `.desktop` Actions= are the app menu (Steam is not special; Spotify on this machine ships none). |
| `2026-09-05-running-apps-pill-design.md` | design | Owner-approved 2026-09-05. D1–D15. D8/D11: shell-owned XDG identity and niri spawn; launcher widget is optional. Audit amendments: focus_timestamp `{secs,nanos}`, D6 drops sticky last-focused. |
| `2026-09-05-running-apps-pill.md` | plan | Ten TDD tasks after audit: niri focus fields (`{secs,nanos}` + `WindowFocusChanged`), Action IPC, grouping, shell desktop-entry lookup (not sysc-launch), focus-or-cycle, menu rows, config, capsule, clicks/menu host, live Niri gate. |
| `2026-09-05-running-apps-pill-audit-report.md` | audit-report | Plan audit 2026-09-05. Findings 1–8 applied to the design and plan. |
| `2026-09-06-running-apps-pill-audit-handover.md` | audit-handover | Post-implementation audit of the 2026-09-05 pill session (D1–D15, prior art, owner live feedback, uncommitted sysc-181). Report: `2026-09-06-running-apps-pill-audit-report.md`. Beads `sysc-189`. |
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
| `2026-09-03-control-center-design.md` | design | Control centre (`sysc-158`): `PanelControlCenter` 700x564, 56 px icon rail plus one scrollable body reusing the shipped `PanelHost.section` seam (D1-D12). Its own UI, sharing services with the dedicated panels and embedding none of their trees. Seven functional pages; Media, Network and Bluetooth disabled in place. Adds sixteen Material ligatures. |
| `2026-09-10-control-center-dashboard-correction-design.md` | design | Owner-approved Home and attached-chrome correction for `sysc-154`: account image with fallback, separated quick-access pills, shipped radial resource gauges, correct transparent fillet margins, antialiased Noctalia-shaped joints, wordmark anchoring, focus, reveal, and reduced motion. |
| `2026-09-11-control-center-dashboard-correction.md` | plan | Six focused correction tasks for `sysc-154`: rebase, circular account image through the shipped worker, independent quick controls, radial resources, truthful opaque regions, antialiased joints, existing-animator reveal/focus, capped checks, and the scale-1.25 laptop gate. |
| `2026-09-07-notification-centre-polish-design.md` | design | Notification centre visual pass (`sysc-151`, `sysc-153`): Noctalia clone target for centre layout only. Three-level surface ladder, headline header with five circular buttons, one segmented filter row replacing tabs plus chips, merged list with live pinned, per-card remove, concave bar fillets. D1-D14. Card internals and toast chrome explicitly unchanged; does not amend the 2026-09-03 design. |
| `2026-09-07-notification-centre-polish.md` | plan | 17 tasks across three slices (`sysc-153`, `sysc-198`, `sysc-199`): history.remove and the rc.3 pin; then the shared chrome primitives (icon subset, PinEnd, icon tone, concave fillet); then the centre tree. Records that AGENTS.md's `-race` full-tree gate is unrunnable here and gives the per-package substitute. |
| `2026-09-03-control-center.md` | plan | Fourteen slices for `sysc-154`: the `AttachedMask` concave primitive, seventeen ligatures, panel identity and flush bar-centre placement, bar trigger, IPC section addressing, rail and page dispatch, the off-owner `scheduleControl` seam, the caffeine idle inhibit released on `Registry.Close`, seven pages, the daily forecast the wire layer already decodes, focus and reveal, then the live Niri matrix. TDD waived: one focused check per slice. |
| `2026-09-07-audio-panel.md` | plan | Nine tasks: attached-mask primitive with concave top corners (`sysc-154` gains the dep), four Material ligatures (mic/mic_off/graphic_eq/headphones), `pw-dump` enumeration service with cubic volume and `SetDefault`, fused wing-tip panel chrome with trigger-centred `AnchorX`, Volumes/Devices tabs with off-owner `scheduleControl` writes, volume bar widget, density verification and live Niri gate. |
| `2026-09-07-audio-panel-completion-handover.md` | completion-handover | Snapshot at `18470ab`: gate output, audit verdict (no blocking defects; four low deviations recorded), live IPC open/close and cubic-volume reconciliation. Pointer-driven matrix items owner-verified. |
| `2026-09-08-live-shell-correction-design.md` | design | Owner-approved correction pass: seamless themed mark, functional responsive audio, notification containment/bell, four sysmon gauges plus processes, weather, and far-left launcher. |
| `2026-09-08-live-shell-correction.md` | plan | Eight lean slices implementing and live-testing the approved correction across the existing shell owners and `sysc-metrics`. |
| `2026-09-11-shell-surface-polish-execution-handover.md` | execution-handover | Commissions the accepted monitor table's final visual pass: hidden process scrollbar, antialiased rounded chrome, restrained theme-derived button gradients, truthful attached-panel transparency, and one bar/panel root colour. Uses `sysc-121` and `sysc-54`. |
| `2026-09-06-connectivity-and-media-prior-art.md` | assessment | Noctalia and DMS division of service, bar widget and page for network, bluetooth and media (`sysc-155`/`156`/`157`). The service is a peer of the shell, not part of the control centre; the standalone widget carries no device picker; `network` is already bound to the throughput metric. |
| `2026-09-11-network-panel-design.md` | design | `PanelNetwork` (`sysc-157`): Wi-Fi and Ethernet tabs on one 460x560 surface, Direction B status-first composition, event-driven NetworkManager service over pinned `Wifx/gonetworkmanager/v2 v2.2.0` with only the secret export hand-written on `godbus/v5`, single-slot credential prompts, `wifi` bar widget, masked password field, nine added Material glyphs. D1-D18. Three slices; the `nm-applet` collision is the open risk. |
| `2026-09-11-network-panel.md` | plan | Thirteen TDD tasks in three slices for `sysc-157`: verify the cached dependency and fix the tracker, state types and signal bands, pushed service with lease lifecycle, backend and pin, nine glyphs and the `wifi` widget, panel identity and bar trigger, status-first header, tabs and access-point list, off-owner writes, masked fields, single-slot credential export, password card, live gate. Records the `GOPROXY=off` resolution trap and that the commit hook rejects the substring `agent`. |
| `2026-09-13-network-panel-execution-handover.md` | execution-handover | Continue `sysc-157` from branch `feature/network-panel` at `cfe692e`: Tasks 1 to 9 committed, Task 10 dirty in three credential files, then rebase onto current main before the password card and live Niri gate. Records the font-inventory merge rule, credential lifetime invariants, `nm-applet` collision, `sysc-254`, and the unresolved Down/Up figures. |
| `2026-09-13-panel-list-row-shape-design.md` | design | Wi-Fi access-point rows opt into `ShapeSmall` for a slightly rounded rectangular field. The global button default, surrounding card, tabs, header well, content layout, and interaction behaviour stay unchanged. Future panel lists adopt the role at their construction site when their design calls for it. |
| `2026-09-14-panel-list-row-inset-design.md` | design | AP rows keep `ShapeSmall` and add density-aware `ButtonPadding` so leading and trailing glyphs sit inside the surface. A reusable `panelListRow` constructor waits for a second matching panel consumer. |
| `2026-09-13-bluetooth-panel-design.md` | design | `sysc-155`: one Registry-owned, signal-driven BlueZ service over `godbus/v5`; full `KeyboardDisplay` pairing and authorization; `bluetooth` bar widget; standalone panel and control-centre page sharing one body; explicit scan, connect, trust, and Forget controls. One-adapter ceiling and Blueman default-agent collision recorded. |
| `2026-09-13-bluetooth-panel.md` | plan | `sysc-155`: preflight plus ten implementation slices covering the BlueZ reducer, single-slot pairing, Registry relay, glyphs/widget, shared body, standalone panel, and the mandatory functional control-centre page. The issue cannot close on standalone work alone. |
| `2026-09-14-bluetooth-panel-execution-handover.md` | execution-handover | Commissions `sysc-155` after `sysc-157` lands. Records the live network-worktree collision, one-service/shared-body boundary, complete pairing-agent invariants, and the control-centre page as a required consumer and completion gate. |
| `2026-09-13-bluetooth-panel-completion-handover.md` | completion-handover | Snapshot of `sysc-155` implementation, automated gates, live Niri/BlueZ observations, and hardware-limited device-action coverage. |
| `2026-09-13-panel-list-row-shape.md` | plan | One TDD code change for `sysc-157`: assert and set `ShapeSmall` on Wi-Fi access-point buttons, run focused and repository gates, then deploy the exact build to the laptop for visual confirmation without toggling Wi-Fi. |
| `2026-09-14-panel-list-row-inset.md` | plan | TDD update to assert both AP-row edge insets, apply `ButtonPadding`, run repository gates, and deploy the exact build for laptop visual confirmation. |
| `2026-09-15-weather-panel-design.md` | design | Owner-directed 2026-09-15 after the `sysc-277` audit. D1–D12: enriched Open-Meteo wire model (apparent, is_day, wind, humidity, UV, precipitation, root elevation/timezone), night glyphs in the custom weather font, one condition-word table and one glyph mapping shared by bar, panel and control-centre page, a structured bar tooltip, `PanelWeather` 460×560 on `panel:weather`, the three-state stale/error rule everywhere, a config location label, and Hourly deferred. |
| `2026-09-15-weather-panel.md` | plan | Ten TDD tasks for `sysc-277` in `.worktrees/feature/weather-panel` off `41eaf68`: wire model, Reading passthrough, night glyphs and vocabulary, config location, bar widget, panel identity, hero and details, forecast list, cc page restyle, gates and handover. |
| `2026-09-15-weather-panel-execution-handover.md` | execution-handover | Live. Ten commits on `feature/weather-panel` (`165f955..bd760f2`) executing the plan: enriched wire model, one weather vocabulary with night and detail glyphs, the upgraded bar widget, `PanelWeather`, the day list, and the cc page restyle. Commissions the audit **before** the owner-approved laptop live gate, lists the five points the audit must confirm beyond the spec walk, and records the three amendments the execution forced. The live gate needs the owner's coordinates for the laptop's `weather` config block. |

## Panel backdrop blur (Milestone 8 evidence)

Owner-approved approach 2026-09-11: visual parity with Noctalia v4 as the
target, rendering before tokens. Carries the measured answer `sysc-202` asked
for — a blurred panel backdrop costs 0.54–4.6 ms on `wl_shm`, inside one 60 Hz
frame, so blurred panels are **not** the named failing case that would justify
EGL/OpenGL ES. Epic `sysc-202`. The screencopy readback itself is unmeasured
and is the first implementation task.

| Document | Kind | State |
|---|---|---|
| `2026-09-11-panel-backdrop-blur-design.md` | design | D1–D16. Static per-open `zwlr_screencopy_manager_v1` region capture, CPU box blur at quarter resolution with no upsample pass, composited under `rootFill`; lifts the `OpacityMin = 80` floor that existed only because the shell could not blur. Amends the architecture document's rendering section and open gate. Panels only — not the bar, toasts, or OSD. |
| `2026-09-11-panel-backdrop-blur.md` | plan | Ten TDD tasks: vendor and generate the screencopy binding; bind it as an **optional** global (added to `interfaceMaximum`, deliberately absent from `requiredSingletons`, which is what makes a compositor without it degrade rather than fail); the pure blur kernel; a bilinear path beside `paintImage` that leaves the icon contract alone; `Style.Backdrop` composited beneath `rootFill`; the region capture; capture at the top of `openAux` before any surface exists; the opacity floor; config and settings; the architecture amendment and live gate. Two explicit stop-and-re-review gates on measured cost — the blur kernel against the predicted 4.6 ms, and the unmeasured screencopy readback. |

## Milestones 7 remainder and 8: not yet designed

| Milestone | Scope | Note |
|---|---|---|
| 7 (after launcher) | Clipboard, network/BT/MPRIS, control center, desktop widgets | Wallpaper has `2026-09-03-wallpaper-design.md`. Do not fold the rest into the launcher slice. |
| 8 | Rendering qualification | Blurred panels are measured and resolved in favour of `wl_shm` by `2026-09-11-panel-backdrop-blur-design.md`. The other named cases — animation frame time, image-heavy grids, CPU/power — still require evidence. |
| — | **Template panel** | Owner-requested 2026-09-12, to follow the parity tranche. One shipped surface composing every primitive the shell owns, setting out what a default panel looks like. **Four committed documents already name it as a planned consumer** — the stacking design D5/D8/D9, the stacking plan's Global Constraints, Task 5 and Self-Review, and the parity plan's D4 coverage note — so those gates no longer revert a primitive for want of a production consumer. It also serves Milestone 6's undertaking to version the vocabulary "proven by built-in widgets", which nothing currently proves: `internal/ui` carries 22 kinds against `plugin/v1`'s 10. Not yet designed; this row exists so the forward references do not dangle. |

## Sibling repositories

Verified 2026-09-02 against git, GitHub, and `go list -m`. `sysc-shell` pins the **tags**, not
whatever is checked out on each repo's `main`. Checking out `/home/nomadx/sysc-notify` or
`/home/nomadx/sysc-tray` on `main` shows docs only; that is not what this module compiles.

| Repository | Pin in go.mod | Where the pin lives | State |
|---|---|---|---|
| `/home/nomadx/sysc-wayland` | `v0.2.1` | `main` (`6bef268`) | Tagged and pushed. Object-argument fix. |
| `/home/nomadx/sysc-metrics` | `v0.3.0` | `main` (`263a6f7`) | Core counters, sysfs battery, CPU package temperature, GPU usage/temperature (`nvidia-smi` for NVIDIA). |
| `/home/nomadx/sysc-notify` | `v0.1.0-rc.3` | `redesign/v0.1` (`02cb723`). `origin/main` is still `32da2b5` (docs only). | Full daemon (`cmd/sysc-notify`, protocol 1.1, presenter socket). Never merged to default branch. Stable `v0.1.0` waits on `sysc-97`. Not installed or running on the development machine. Tag `v0.1.0-rc.3` is local until published. |
| `/home/nomadx/sysc-tray` | `v0.1.0-rc.1` | `origin/redesign/v0.1` tag `30d266e` (branch tip one docs commit later). `origin/main` is still `04ca018` (docs only). | Full daemon (`cmd/sysc-tray`). Same merge and install gap as notify. |
| `/home/nomadx/sysc-launch` | `v0.1.0` | `main` (`fb6f73c`) | Library plus one-shot CLI (`query` / `launch`). Not a daemon. Shell hosts `launcher.NewService` in-process. |

## Work selection

Use `bd ready` and `bd blocked`. This register does not duplicate execution order or issue status.
