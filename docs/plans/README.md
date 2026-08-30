# Design and Plan Register

Last updated: 2026-08-30.

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
| `milestone/panels-controls` | Merged into `main` on 2026-08-30; stays live for further Milestone 4 work |

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

Split into four reviewed tranches by the charter. Only 3A is designed.

| Document | Kind | Branch | State |
|---|---|---|---|
| `2026-08-30-built-in-widget-foundation-execution-handover.md` | commission | `main` | Historical commission that produced the approved design and plan. |
| `2026-08-30-live-gate-to-tranche-3a-execution-handover.md` | execution-handover | `main` | Current receiving-agent handover. Start Tranche 3A now; `sysc-5` remains open as deferred hardware qualification. |
| `2026-08-30-built-in-widget-foundation-design.md` | design | `main` | Owner-approved, audited, amended. Records D1–D8 and the design-audit outcome. |
| `2026-08-30-built-in-widget-foundation.md` | plan | `main` | Complete. 16 tasks (0–15). Audit findings applied. Ready to execute. |
| `2026-08-30-built-in-widget-foundation-audit-handover.md` | audit-handover | `main` | Closed. Commissioned the technical and design audits. |
| `2026-08-30-built-in-widget-foundation-audit-report.md` | audit-report | `main` | Closed. Verdict: proceed with named corrections; all seven applied at `9a114eb`. Verified assumption 4 live. |

### Tranche state

| Tranche | Scope | State |
|---|---|---|
| 3A | Clock, date, Niri workspace, focused-window title; service lifetime; per-output widget instances | Designed, planned, audited, and ready to execute. |
| 3B | CPU, memory, filesystem, block and network rates | **Blocked**: needs a reviewed, tagged `sysc-metrics` release. None exists. |
| 3C | Battery and remaining time | **Blocked**: needs `sysc-metrics` M2 power and thermal gates. |
| 3D | Weather, icons, meter/graph/tooltip nodes | Not started. Needs the icon-asset policy applied and an Open-Meteo decision. |

## Milestone 4 — panels and standard controls

Designed, planned, reviewed and corrected. All documents are on `main`; `milestone/panels-controls`
stays live for further work.

| Document | Kind | State |
|---|---|---|
| `2026-08-30-panels-and-controls-prior-art.md` | assessment | Noctalia v5 and DMS v1.5.3 inventories with file:line evidence. |
| `2026-08-30-panels-and-controls-research.md` | research | Niri capability claims, source-verified against niri main. |
| `2026-08-30-panel-foundation-design.md` | design | Tranche 4A. Owner-approved; amended for the review. D1–D13. |
| `2026-08-30-panel-foundation.md` | plan | Tranche 4A, **13 tasks**. Ready to execute once its prerequisites clear. |
| `2026-08-30-settings-osd-theme-catalog-design.md` | design | Tranche 4B. Owner-approved; amended for the review. D1–D10. |
| `2026-08-30-settings-osd-theme-catalog.md` | plan | Tranche 4B, 14 tasks. See the open scope question below. |
| `2026-08-30-milestone-4-review.md` | review | Closed. Both blockers fixed in `3fc026c`; findings tracked as `sysc-16`. |

### Tranche state

| Tranche | Scope | State |
|---|---|---|
| 4A | Panel machinery, placement, rounding and shadows, button/label/separator, matugen theming, clock/calendar and session/power popouts, IPC | Ready. Blocked only by the M2 corrections and Tranche 3A. |
| 4B | Controls, settings modal and registry, OSD with audio/brightness services, stock themes, template catalog | Planned. Blocked by 4A. Scope question open — see below. |

### What the review changed

- The system-monitor popout, `MetricsService`, and the `tabs` and `graphs` controls are **deferred out
  of Milestone 4** until Tranche 3B qualifies and tags `sysc-metrics`. 4A had reached for a local
  `replace` on an untagged module — a recorded stop condition — and the exit gate is a disjunction, so
  clock/calendar carries it. **Milestone 4 now adds no module dependency.**
- A configuration reload leaves panel surfaces mapped. Tearing them down would have dismissed 4B's
  settings modal on every change made inside it.
- Colour fields that palette generation now owns are removed from the schema, and unknown keys are
  rejected at load, so a stale file fails with its field path named rather than silently doing nothing.
- Every template apply hook gained a reverse, so disabling one removes the include line and file it
  added to another application's configuration.

### Open owner decisions

- **External binary dependencies** (`sysc-16`). matugen and `loginctl` in 4A, `wpctl` and
  `brightnessctl` in 4B are the first runtime binary dependencies in a shell that is otherwise pure Go
  over a pinned client. Each has a clean fallback and beats its alternative, but the dependency ladder
  in the project design stops at "new code" and does not cover execing a binary the user may not have.
- **4B tranche size** (`sysc-16`). Six controls, the settings modal, a schema registry, persistence,
  two services, OSD, ten themes, sixteen templates, and a cross-repository `sysc-wayland` release, in
  fourteen tasks. Tranche 3A ships four read-only text widgets in sixteen. Splitting along the seams
  the design already names — controls and settings, OSD and services, themes and catalog — would give
  three reviewable pieces.

## Milestones 5 to 8 — not yet designed

No design or plan exists for any of these. The roadmap is the only record.

| Milestone | Scope | Note |
|---|---|---|
| 5 | Notifications and system tray | Depends on `sysc-notify` and `sysc-tray`, neither of which has a repository yet. |
| 6 | External widget and plugin host | Versions the vocabulary the built-in widgets prove. |
| 7 | Shell breadth | Launcher is item 1. It consumes Milestone 4's text field, virtual list, scroll area, keyboard focus, and popout placement, so its component demands are worth capturing while Milestone 4 is designed. |
| 8 | Rendering qualification | Only on measured evidence against `wl_shm`. |

## Sibling repositories

| Repository | Head | State |
|---|---|---|
| `/home/nomadx/sysc-metrics` | `d821afe` | Clean, pushed. **No release tag.** Its own register is `docs/plans/`: an approved design, a core-counters plan, and execution, merge and completion handovers. Blocks Tranche 3B until a tag exists. |
| `sysc-wayland` | `v0.1.1` | Pinned dependency. Qualified. |
| `sysc-notify`, `sysc-tray` | — | Approved boundaries and roadmaps only. No repository yet. Milestone 5. |

## Execution order

Everything below is written, reviewed, and waiting. `bd ready` is authoritative; this is the same
graph in prose.

1. **Milestone 2 corrections** (`sysc-4`): complete and merged.
2. **Tranche 3A** (`sysc-6`): ready now. 16 tasks, plan on `main`. Its Task 0 gates on the three
   corrections from step 1 and stops if any is absent.
3. **Milestone 2 hardware qualification** (`sysc-5`): remains open in parallel and needs a live session
   with two outputs. It blocks claims of full Milestone 2 hardware qualification, not development.
4. **Tranche 4A** (`sysc-17`): 13 tasks, plan on `main`. Needs steps 1 and 2; explicitly does **not**
   need Tranches 3B, 3C or 3D.
5. **Tranche 4B**: 14 tasks, after 4A, subject to the scope question above.

Off the critical path, and startable now: **`sysc-metrics` qualification** (`sysc-7`) — audit the
public API, qualify it from a proposed consumer, tag and push. It unblocks Tranche 3B (`sysc-8`) and
3C (`sysc-9`), and it is what would let the deferred system-monitor popout return.

## Parallel work

Start Tranche 3A now. The deferred live Niri gate can run in another session when suitable hardware is
available. Other independent work includes:

1. **`sysc-metrics` qualification** (`sysc-7`). Audit the public API, qualify it from one proposed shell
   consumer, tag and push. The single dependency unblocking Tranches 3B and 3C, and the thing that
   would let Milestone 4's system-monitor popout return.
2. **The two open Milestone 4 owner decisions** (`sysc-16`): the external-binary dependency policy, and
   whether Tranche 4B splits.
3. **Icon-asset policy application** for Tranche 3D: author the SVG sources and decide the checked-in
   raster size set after measuring the supported fractional scales.
4. **Launcher prior-art research** (`sysc-12`), scoped as input to the component vocabulary rather than
   as Milestone 7 implementation. Lower value now that Milestone 4 is designed.

Do not edit the Milestone 2 progress handover.
