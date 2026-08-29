# Built-in Widget Foundation Audit Handover

Date: 2026-08-30

## Purpose

This handover commissions two independent audits of the Milestone 3 Tranche 3A design and implementation
plan, before any product code is written against them.

1. A **standard technical audit** of correctness, completeness, and conformance to recorded project
   decisions.
2. A **Hallmark design audit** of the visual and interaction decisions the two documents fix.

The receiving agent audits. It does not implement, redesign, or edit the documents. Findings go back to
the owner, who decides what changes.

Both audits must distinguish what was **verified** from what was **asserted**. The project's recorded
practice is that a document may not claim live behavior from an automated gate, and may not carry a hash,
version, or API forward without re-checking it. Apply that standard to the documents under audit and to
your own report.

## Repository state

- Repository: `/home/nomadx/sysc-shell`
- Remote: `https://github.com/Nomadcxx/sysc-shell.git`
- Accepted local `main`: `296b0eb`
- Audit branch: `milestone/widget-foundation`
- Audit worktree:
  `/home/nomadx/.config/superpowers/worktrees/sysc-shell/milestone/widget-foundation`
- Branch head: `0a9587d`
- Branch contents: documentation only. No product code has been written for Tranche 3A.
- Milestone 2 worktree:
  `/home/nomadx/.config/superpowers/worktrees/sysc-shell/milestone/stable-bar`
- Milestone 2 branch: `milestone/stable-bar`, head `57b49f0`, ahead of `main` by 20 commits
- Milestone 2 status: review corrections still in progress. Read it for current contracts. **Do not edit
  it and do not branch product code from it.**

Commits under audit:

| Commit | Contents |
|---|---|
| `391787b` | The Tranche 3A design, plus the execution handover vendored so the branch is self-contained. |
| `0a9587d` | The Tranche 3A implementation plan. |

## Documents under audit

| Path | Lines | Role |
|---|---|---|
| `docs/plans/2026-08-30-built-in-widget-foundation-design.md` | 581 | The design. Fixes contracts and decisions. |
| `docs/plans/2026-08-30-built-in-widget-foundation.md` | 3631 | The executable plan. Fifteen TDD tasks plus a gate. |
| `docs/plans/2026-08-30-built-in-widget-foundation-execution-handover.md` | 371 | The charter both documents answer to. Audit the two against it; do not audit it. |

## Read these first

In this order:

1. The execution handover above. It is the charter: scope, fixed decisions, required evidence, and stop
   conditions.
2. `docs/roadmap.md`, Milestones 2 and 3.
3. `docs/plans/2026-08-26-sysc-shell-design.md` — UI runtime, services, Niri integration, rendering.
4. `docs/plans/2026-08-27-plan-audit-report.md` — the prior audit's method and its open owner decisions.
5. `docs/prior-art.md` — the recorded Noctalia, DMS and dgop assessment.
6. The design, then the plan.
7. Current code in the Milestone 2 worktree under `internal/shell`, `internal/ui`, `internal/render`,
   `internal/config`, and `internal/platform/niri`.

## What the design decides

The audit must engage with these decisions, not restate them. Each is recorded with its rejected
alternative in the design's Decisions table.

| # | Decision |
|---|---|
| D1 | Concrete widget values pairing a retained `ui.Node` with a pure `func(barView) string`. No widget interface. |
| D2 | Widget instances owned per output, keyed by `wl_registry` global. Connector is an attribute. |
| D3 | Focused-window title is the active window of *that output's* active workspace, via `Workspace.active_window_id`. |
| D4 | One `clock` widget parameterised by a Go layout string. No separate `date` widget. |
| D5 | Widget options attached per instance, through a string-or-object union. |
| D6 | Clock tick boundary derived from the layout string; 1 s or 1 min only. |
| D7 | Clock is consumer-counted. The Niri stream is an unconditional shell responsibility. |
| D8 | `ui.Node` gains `MaxWidth` for text, consumed by `window-title`. |

Four decisions were put to the owner and answered during design:

- Milestone 2 owns the global-keyed identity correction; Tranche 3A assumes it and names the contract.
- Per-output focused-window title, over a global one mirrored on every bar.
- Go layout strings validated at load, with the tick boundary derived from the layout.
- The Milestone 2 fixture vocabulary is replaced outright.

A fifth was reopened after a prior-art review and re-answered: widget options move from the bar to the
item, and `clock`/`date` collapse into one parameterised `clock`.

Two corrections were made after the design was first approved, both recorded in the documents:

- `WindowClosed` naming an unknown window is a **no-op**, not a stream error: the event's desired
  post-state already holds. `WorkspaceActiveWindowChanged` naming an unknown workspace remains an error,
  because it carries state with nowhere to go.
- `UpdateClock` and `UpdateNiri` return changed globals as `[]uint32`, so the tests do not depend on which
  invalidation transport the Milestone 2 correction lands.

## Audit 1: standard technical audit

### Scope

Correctness, completeness, internal consistency, and conformance. Both documents.

### Required checks

**Verification of external facts.** Every factual claim about something outside these documents must be
re-derived, not trusted. In particular:

- The Niri 26.04 wire schema the design tabulates: `Workspace` fields including `active_window_id`,
  `Window`'s ten fields, and the event set including `WorkspaceActiveWindowChanged`. Niri 26.04 is
  installed at `/usr/bin/niri`. Verify independently and report any field, type, nullability, or event
  name the design gets wrong.
- The prior-art claims. The design cites Noctalia at `/home/nomadx/noctalia` and DankMaterialShell at
  `/home/nomadx/Documents/GitHub/DankMaterialShell` (`892b8ae`), plus installed QML at
  `/usr/share/quickshell/dms` and live configuration under `~/.config/`. Every prior-art assertion should
  be traceable to a file, and ideally a symbol or line. Report any that is not.
- The Go standard-library behavior the design relies on: `time.Format` never erroring, `time.Truncate`
  operating on absolute time rather than local midnight, and Go timers running on a suspend-excluding
  monotonic clock. Report any that is wrong or overstated.
- The current signatures in the Milestone 2 worktree that the plan modifies.

**Plan executability.** The plan claims that a skilled Go engineer with no project context can execute it.
Test that claim:

- Does every task name exact files, exact commands, and expected failure text?
- Does the test code in each task compile against the types defined in earlier tasks? Check
  `NewWithTheme`'s arity, `buildWidgets`, `clockBoundaries`, `projectOutputs`, `outputState`,
  `noWorkspace`, `barView`, `textWidget`, `Registry.UpdateClock`, and `wayland.PreparedConfig.Rollback`
  for consistent names, parameters and return types across all fifteen tasks.
- Is any step a placeholder, a "similar to Task N", or an instruction without the code it needs?
- Does Task 7 leave the tree in a state where Task 8 can run? The plan deliberately parks
  `Registry.PrepareConfig` behind an error between Tasks 11 and 12. Confirm nothing else is left broken,
  and that the parked state is safe.
- Does the lock discipline hold? The plan asserts a total order: registry lock, then bar lock, with
  `Bar.Render` taking only the bar's. Verify no path in the plan violates it.

**Coverage against the charter.** The execution handover lists twelve required automated behaviors and a
live matrix. Map each to a task and a named test. Report gaps. Two the auditor should check especially:
"focus, title, workspace, close, and move events invalidate only affected output bars" — the plan may
cover *move* only at the decoder level; and "an accepted reload does not restart a service still in use".

**Conformance to fixed decisions.** The handover's stop-condition list is the checklist. The design claims
none applies. Verify each independently rather than accepting the claim.

**Failure-policy coherence.** The design treats unknown ids differently for two events, with a stated
rationale. Judge whether the distinction is principled or a rationalisation, and whether either choice can
stop the shell on a benign compositor event.

**Concurrency.** The clock service, its lease accounting, its rearm path, and the registry's use of it.
Look for a deadlock between `Registry.mu` and `Bar.mu`, a lease leak on any error path, a goroutine that
outlives cancellation, and whether `Clock.Close` and `Lease.Release` are genuinely idempotent.

### Output format

A findings table: severity, location by file and line range, the defect, the concrete consequence, and a
one-line fix. Group by severity. Separate **verified** findings from **suspected** ones. End with a
verdict: proceed, proceed with named corrections, or return for redesign.

## Audit 2: Hallmark design audit

### How to run it

Invoke the `hallmark` skill with its `audit` verb against the two documents. Follow
`references/verbs/audit.md`: for each finding return **Tell**, **Where**, **Severity**, **Fix**; group by
severity; do not edit; end with a `N critical · M major · K minor` count.

### What the design surface actually is

This is a Wayland bar rendered into `wl_shm` buffers, not a web page. Audit the visual and interaction
decisions these documents fix, which are:

- **Palette.** The theme tokens the bar ships with, in
  `internal/shell/theme.go` (`DefaultTheme`): background `#101418`, foreground `#e8ecf0`, accent
  `#0080ff`, muted `#303438`, error `#ff4040`. The design does not change them; it inherits them, which is
  itself a decision worth grading.
- **Geometry and spatial rhythm.** Nominal height 48, gap 4, painted body 40, exclusive zone 44, radius
  12, bar padding 8, item spacing 6. These are fixed by the charter, so grade whether the *new* widget
  layout respects the rhythm rather than proposing to change the numbers.
- **Typography.** One family at size 14, system-resolved with per-rune fallback. Judge sizing, the absence
  of any weight or size hierarchy between widgets, and figure rendering.
- **Default composition.** Which widgets land in which section: `workspace` and `window-title` left,
  `clock` centre, a second `clock` formatted as a date right. Grade the arrangement, the balance, and
  whether two clock instances in different sections reads as intentional or as a workaround.
- **Copy and formatting.** The default layouts `15:04` and `Mon 2 Jan`, and the `-` fallback for an
  unknown workspace.
- **States.** The empty-title state renders a zero-width node. The tranche is read-only, so there is no
  hover, press, or focus state to grade — say so rather than flagging their absence.
- **Bounded text.** The 260-logical-pixel cap on the window title, and how truncation reads.

### Categories that do not apply

Do not grade, and do not invent findings for: hero sections, calls to action, feature grids, card-in-card,
navs, footers, bento or other page macrostructures, aurora or blob backgrounds, floating orbs, scroll
animation, autoplay, LCP, Lottie, or Three.js. A 44-pixel bar has none of these. There is no `design.md`
in this repository and no Hallmark stamp in these files, so the stamp-versus-page and design-system-drift
checks do not apply either. Report the non-applicable set once, briefly, and move on.

### Questions the design does not currently answer

These are open, not prejudged. The audit should reach its own conclusion on each.

- **Figure rendering in the clock.** Nothing in the code or the documents mentions tabular or lining
  figures. A proportional face renders `15:04` with digits of differing advance, so the clock's width can
  change as the time changes. Grade whether that matters at this size, and whether a bar clock should
  request tabular figures.
- **Palette provenance.** Whether the inherited accent and error colours read as a considered palette or
  as the default attractor, and whether contrast against the background is adequate at size 14.
- **Token discipline.** Whether the new `MaxWidth` default of 260 belongs in the theme token set beside
  the other geometry, or is acceptable as a configuration default. The project's stated rule is that no
  component carries an independent pixel constant.
- **Hierarchy.** Every widget in this tranche renders in one colour at one size. Whether that is correct
  restraint for a bar, or an absence of design.
- **Truncation affordance.** What a truncated title should look like, given the tranche ships no tooltip
  and no marquee.

### Output format

Hallmark's own: Tell, Where, Severity, Fix, grouped, with the closing count. Add one short paragraph
naming which findings are blocked by the charter's fixed geometry and therefore cannot be acted on inside
Tranche 3A.

## Assumptions the documents already declare

Do not report these as discoveries. Do assess whether each is handled responsibly, and whether its named
fallback is adequate.

1. Milestone 2 delivers global-keyed host identity. Tranche 3A assumes it; Task 0 gates on it.
2. Milestone 2 wires `render.FontMap` into the bar. It exists and is unwired today; `internal/shell` still
   parses embedded `goregular`.
3. Milestone 2's invalidation correction delivers lossless delivery. Verified by test rather than assumed.
4. Niri emits `WorkspaceActiveWindowChanged` for a focus move *within* one workspace. **Not verified** —
   the design session had no windows open. DMS relies on the same event for the same purpose, which is
   indirect evidence. A local fallback is named. This is the single most consequential unverified claim in
   the design; if the auditor can test it on a live session with windows open, that is the highest-value
   check available.
5. No `sysc-metrics` import, tag, or module replacement. That is Tranche 3B.

## Stop conditions

Return to the owner without completing the audit if:

- either document requires a second goroutine to call Wayland, keys a widget instance by connector alone,
  creates one Niri connection or clock timer per output, introduces a widget schema, renderer interface,
  plugin protocol or injection container, imports untagged `sysc-metrics`, adds a runtime SVG decoder, or
  adds a dependency the standard library or pinned modules already cover;
- the plan cannot preserve the last accepted widget and service set after a failed reload;
- a claimed verification cannot be reproduced, and the claim is load-bearing;
- the two documents contradict each other on a contract.

## Completion handoff

Report:

- the branch, worktree, and the exact commits audited;
- the standard audit's findings table and verdict;
- the Hallmark audit's punch list and count;
- for every external fact you re-derived: what you checked, how, and whether it held;
- claims you could not verify, and what evidence would settle each;
- decisions you judge wrong, with the alternative and its cost;
- anything requiring live Niri or hardware.

Do not edit the design or the plan. Do not modify the Milestone 2 progress handover. Do not begin
implementation.
