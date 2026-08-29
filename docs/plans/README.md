# Design and Plan Register

Last updated: 2026-08-30.

Every design, plan, and handover this project has produced, with where it lives and whether it is still
live. Add a row here in the same commit that adds a document. A document that is not in this register is
one the project will lose.

## Why this file exists

Documents are written on milestone branches and only reach `main` when that milestone merges. No branch
currently holds the whole set:

| Branch | Head | Holds |
|---|---|---|
| `main` | `296b0eb` | Milestone 0 and 1 documents, plus the Milestone 2 commission |
| `milestone/stable-bar` | `57b49f0` | everything on `main`, plus the three Milestone 2 documents |
| `milestone/widget-foundation` | `9a114eb` | everything on `main`, plus the five Tranche 3A documents |

`milestone/stable-bar` and `milestone/widget-foundation` do **not** see each other's documents. Read the
branch column before concluding a document does not exist.

**This register is itself subject to that problem.** It lives on `milestone/widget-foundation`. Carry it
forward on every merge and re-check it against `git ls-tree -r --name-only <branch> -- docs/` rather than
trusting it blindly.

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

Status: **implemented, audited, corrections in flight, live gate not run.** Blocks Milestone 3
implementation.

**Current blocker (2026-08-30): scheduling, not technical.** The agent that owns the Milestone 2
corrections is quota-limited and resumes at 05:00 today. The corrections themselves are understood; they
are simply not written yet. Tranche 3A's Task 0 gates on three of them — global-keyed host identity, the
system font map wired into the bar, and lossless invalidation.

| Document | Kind | Branch | State |
|---|---|---|---|
| `2026-08-29-stable-multi-output-bar-design-handover.md` | commission | `main` | Closed. Geometry and ownership contracts. Its implementation status is historical. |
| `2026-08-29-stable-multi-output-bar-design.md` | design | **`milestone/stable-bar` only** | Owner-approved. |
| `2026-08-29-stable-multi-output-bar.md` | plan | **`milestone/stable-bar` only** | The five-task *correction* plan, not the original. |
| `2026-08-29-stable-multi-output-bar-progress-handover.md` | progress-handover | **`milestone/stable-bar` only** | Live. Records 16 tasks implemented, automated gate green, live gate not run. Do not edit from another tranche. |

### Known gap: the original Milestone 2 plan is missing

The progress handover records "all 16 plan tasks implemented", but **no 16-task plan exists on any branch
or on disk**. `2026-08-29-stable-multi-output-bar.md` was created already as the five-task correction plan
(`f483cc8`), after those 16 tasks had been committed. The original plan was never committed.

Consequence: Milestone 2's implementation cannot be reviewed against its own plan. The work is
recoverable only from the 19 `feat`/`test`/`fix` commits in `main..milestone/stable-bar`, running from
`74706ff` to `f3bdae1` for the original implementation plus four later `fix` commits. Those 19 commits do
not map one-to-one onto the handover's "16 tasks", and no document reconciles them.

This is the concrete reason this register exists. Do not repeat it: commit the plan before executing it.

### Uncommitted work on that branch

`internal/platform/wayland/policy_test.go` is modified and uncommitted in the `milestone/stable-bar`
worktree as of 2026-08-30. It is not backed up by any commit.

## Milestone 3 — built-in widget foundation

Split into four reviewed tranches by the charter. Only 3A is designed.

| Document | Kind | Branch | State |
|---|---|---|---|
| `2026-08-30-built-in-widget-foundation-execution-handover.md` | commission | `milestone/widget-foundation` | Live. The charter for Tranche 3A. Also vendored here from an untracked file that existed only in the `stable-bar` worktree. |
| `2026-08-30-built-in-widget-foundation-design.md` | design | **`milestone/widget-foundation` only** | Owner-approved, audited, amended. Records D1–D8 and the design-audit outcome. |
| `2026-08-30-built-in-widget-foundation.md` | plan | **`milestone/widget-foundation` only** | Complete. 16 tasks (0–15). Audit findings applied. **Execution blocked on Milestone 2** by Task 0's gates. |
| `2026-08-30-built-in-widget-foundation-audit-handover.md` | audit-handover | **`milestone/widget-foundation` only** | Closed. Commissioned the technical and design audits. |
| `2026-08-30-built-in-widget-foundation-audit-report.md` | audit-report | **`milestone/widget-foundation` only** | Closed. Verdict: proceed with named corrections; all seven applied at `9a114eb`. Verified assumption 4 live. |

### Tranche state

| Tranche | Scope | State |
|---|---|---|
| 3A | Clock, date, Niri workspace, focused-window title; service lifetime; per-output widget instances | Designed, planned, audited. Blocked on the Milestone 2 corrections merging. |
| 3B | CPU, memory, filesystem, block and network rates | **Blocked**: needs a reviewed, tagged `sysc-metrics` release. None exists. |
| 3C | Battery and remaining time | **Blocked**: needs `sysc-metrics` M2 power and thermal gates. |
| 3D | Weather, icons, meter/graph/tooltip nodes | Not started. Needs the icon-asset policy applied and an Open-Meteo decision. |

## Milestones 4 to 8 — not yet designed

No design or plan exists for any of these. The roadmap is the only record.

| Milestone | Scope | Note |
|---|---|---|
| 4 | Panels and standard controls | **The next milestone after 3.** Its rule is that candidate components enter only with a consumer. Accessibility becomes an acceptance gate here. |
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

## What can proceed while Milestone 2 finishes

Milestone 2 blocks *implementation* of Tranche 3A, not design work elsewhere, and its blocker is an
agent quota that clears at 05:00 on 2026-08-30. Available now, in rough order of value:

1. **Milestone 4 design.** The next milestone, wholly undesigned, and the one Tranche 3A's read-only
   restraint was chosen to avoid pre-empting. Its component set should be driven by named consumers.
2. **`sysc-metrics` qualification.** Audit the public API, qualify it from one proposed shell consumer,
   tag and push. This is the single dependency unblocking Tranche 3B.
3. **Launcher prior-art research**, scoped as input to Milestone 4's component vocabulary rather than as
   Milestone 7 implementation.
4. **Icon-asset policy application** for Tranche 3D: author the SVG sources and decide the checked-in
   raster size set after measuring the supported fractional scales.

Do not start Tranche 3A product code, and do not edit the Milestone 2 progress handover.
