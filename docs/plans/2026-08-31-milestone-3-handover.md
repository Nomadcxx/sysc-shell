# Milestone 3 Handover

Date: 2026-08-31
Scope: every outstanding Milestone 3 matter — Tranches 3B, 3D and 3C, plus the upstream work 3C needs.
Supersedes: `2026-08-30-live-gate-to-tranche-3a-execution-handover.md`, which covered 3A only.

Do not edit the historical Milestone 2 progress handover, or 3A's completion handover.

## How to use this document

Read this file, then the design and plan for whichever tranche you are executing. Those two documents
are self-contained and assume no prior context; this one exists to tell you which of them to open, what
has already been decided, and which traps will cost you an hour if you meet them cold.

If you are picking up **Tranche 3B execution**, the short version is: the worktree exists, three tasks
are committed, resume at Task 3 of `2026-08-30-core-metrics.md`. Everything else here is background.

## Status at a glance

| Tranche | Design | Plan | Implementation | Issue |
|---|---|---|---|---|
| 3A — clock, date, workspace, window title | done | done | **merged** at `39a7760` | `sysc-6` closed |
| 3B — core metrics | done | done | **3 of 13 tasks** on `milestone/core-metrics` | `sysc-8` ready |
| 3D — weather and visual vocabulary | done | done | not started | `sysc-10` blocked by 3B |
| 3C — power | done | done | not started | `sysc-9` blocked by 3D and `sysc-19` |

Milestone 3's paperwork is complete. All four tranches are designed and planned; one is shipped.

`main` carries **30 unpushed commits**. Nothing has been pushed for `sysc-shell` this session by
deliberate choice — see "Open items for the owner".

## Repository map

| Path | Branch | State |
|---|---|---|
| `/home/nomadx/sysc-shell` | `main` | Primary checkout. Run `bd` only here. |
| `~/.config/superpowers/worktrees/sysc-shell/milestone/core-metrics` | `milestone/core-metrics` | **Tranche 3B in progress**, 3 commits, clean tree |
| `~/.config/superpowers/worktrees/sysc-shell/milestone/stable-bar` | `milestone/stable-bar` | Milestone 2, merged, historical |
| `~/.config/superpowers/worktrees/sysc-shell/milestone/panels-controls` | `milestone/panels-controls` | Milestone 4, another session |
| `~/.config/superpowers/worktrees/sysc-shell/milestone/notifications-tray` | `milestone/notifications-tray` | Milestone 5, another session |
| `~/.config/superpowers/worktrees/sysc-shell/audit/milestone-5` | `audit/milestone-5` | Milestone 5, another session |
| `~/.config/superpowers/worktrees/sysc-shell/redesign/milestone-5` | `redesign/milestone-5` | Milestone 5, another session |
| `/home/nomadx/sysc-metrics` | `main` at `v0.1.0` | External dependency, **published** |

**A second session is active in this repository**, working Milestone 4 and 5. It has moved `main` under
this session before and has created issues (`sysc-18`, `sysc-20`, `sysc-21`). Re-read `main` before
assuming it is where you left it, and expect the tracker to gain rows you did not add.

Worktrees for 3D and 3C do not exist yet. Their designs name
`~/.config/superpowers/worktrees/sysc-shell/milestone/weather-vocabulary` and `.../milestone/power`.

## What is done

### Tranche 3A — merged

Four read-only widgets on every output: two clocks, a Niri workspace label, a focused-window title.
Merged to `main` at `39a7760`, 22 implementation commits. 111 tests, race-clean, no dependency added.

Its completion handover is `2026-08-30-built-in-widget-foundation-completion-handover.md`. Read it if you
need the detail; the parts that matter downstream are in "Key decisions" below.

**The live matrix was never run** and no live behaviour has been claimed for 3A. The owner has directed
that live testing happens later and should not gate work. Do not re-raise it; do not claim it passed.

### `sysc-metrics v0.1.0` — audited, tagged, published

The single dependency that gated 3B. Audited on 2026-08-31 against its design, qualified, tagged and
pushed. `go get github.com/Nomadcxx/sysc-metrics@v0.1.0` resolves.

Audit report lives in that repository at `docs/plans/2026-08-31-core-counters-audit-report.md`. Verdict:
fit to tag, no blocking defect. Notable positives worth not re-litigating: no interface is declared
anywhere, returned snapshot slices genuinely do not alias sampler state, counter regressions invalidate
rates rather than spiking, and elapsed time is monotonic so a resume reports a correct rate.

**Its release gate was amended, deliberately.** `v0.1.0` had been tied to "suspend/resume and
UPower-to-sysfs fallback on real hardware" — power collectors that do not exist, so the gate could not be
met by the release it named. Gates are now per milestone: `v0.1.0` covers core counters, and that
evidence moves to `v0.2.0`. The amendment carries its date and reasoning inline in that repository's
design.

### Tranche 3B — 3 of 13 tasks

On `milestone/core-metrics`:

| Commit | Task |
|---|---|
| `1e61236` | Task 1 — `leaseSet` extracted from `clock.go`; 24 lines deleted, every clock test passes untouched |
| `ad4f07c` | Task 2 — metrics service lifetime; 19 tests race-clean; dependency at `v0.1.0`, no `replace` |

Task 0 (the dependency gate) passed. **Resume at Task 3.**

## What remains, in order

The order is enforced in the tracker, not just recommended. `sysc-10` depends on `sysc-8`; `sysc-9`
depends on `sysc-10` and `sysc-19`.

### 1. Finish Tranche 3B — `sysc-8`, ready now

Plan: `2026-08-30-core-metrics.md`. Resume at **Task 3** (the sampling loop). Tasks 3–12 remain.

Task 2 left two deliberate stubs at the bottom of `internal/services/metrics.go`, marked
`// TEMPORARY: replaced in Tasks 3 and 4` — `ring`, `newRing`, `historySize` and a no-op `run`. Task 3
replaces `run`; Task 4 replaces the ring. Do not leave them.

### 2. Tranche 3D — `sysc-10`

Design: `2026-08-30-weather-and-visual-vocabulary-design.md`. Plan:
`2026-08-30-weather-and-visual-vocabulary.md`. Eleven tasks.

Tasks 1–8 ship weather, the icon font and the error tone and stand alone. **Tasks 9–11 are the hover
tooltip and are cuttable as a unit** — Task 8 ends with an explicit note saying so. If the surface work
proves larger than expected, cut it and the tranche is still coherent.

Task 4 needs an authored icon font before the widget task. That is a one-time authoring step whose
output is committed; nothing converts at build or run time.

### 3. Tranche 3C — `sysc-9`

Design: `2026-08-31-power-design.md`. Plan: `2026-08-31-power.md`. Seven tasks.

**Blocked on `sysc-19`, and its signatures are provisional.** `sysc-metrics` has no battery code at
`v0.1.0`, so 3C's design argues from that library's stated contract rather than a compiled API. Plan
Task 0 is a mandatory reconciliation gate with a table mapping each kind of API difference to an action,
including two that stop the tranche outright.

### 4. Upstream — `sysc-19`, ready now, different repository

Power and thermal collectors in `sysc-metrics`. Filed in the repository that owns it as
**https://github.com/Nomadcxx/sysc-metrics/issues/1**, which records the provisional
`BatterySnapshot` shape 3C assumes so that a divergence is a tracked reconciliation rather than a
surprise. Needs its own design and plan there, then a `v0.2.0` release.

This is independent of 3B and 3D and can proceed in parallel.

## Key decisions

### Cross-cutting, established in 3A and inherited

These are load-bearing. Breaking one is a stop condition, not a refactor.

- **One goroutine owns the Wayland connection and every proxy.** Everything else signals it through a
  channel the wake pipe bridges. 3D's tooltip dwell timer is designed around this rather than against
  it: the timer fires on its own goroutine and sends a request; only the owner touches a surface.
- **Widget instances are keyed by `wl_registry` global, never by connector.** Two globals briefly share
  one connector during reconnect overlap and must stay distinct instances with distinct leases.
- **Services are concrete.** `Clock`, `Metrics` and `Weather` share a `leaseSet` struct, not an
  interface. No service registry, no dependency-injection container, no single-implementation interface.
- **A service starts on its first lease and stops on its last.** An accepted reload acquires the
  replacement leases before releasing the outgoing ones, so a service in continuous use is never
  restarted; `Starts()` staying at 1 is the assertion.
- **No source change means no submitted frame.** Change detection compares rendered text.
- **Invalidations publish with a blocking send.** Milestone 2 dropped them on a full channel, so a ninth
  changed bar never repainted. 3A fixed it and proves it at the transport with twelve bars against an
  eight-deep channel.

### Per tranche

**3B** — five separate item ids (`cpu`, `memory`, `filesystem`, `block`, `network`) rather than one
parameterised `metric`, so each validates only the options it accepts. Per-source leases, so a CPU-only
bar never opens `/proc/diskstats`. Fraction sources accept a meter; **rate sources do not**, because
bytes per second has no full scale — a meter on `block` or `network` is rejected at load. The shell
never computes a rate: `formatMetric` takes no interval, so the reference shell's defect of dividing by
a nominal interval is unreachable rather than merely avoided.

**3D** — weather from Open-Meteo with **configured coordinates only**; no geocoding, no automatic
location, no second remote host. Network bounds copied from the reference shell because it learned them
the hard way: 15-minute interval, 3s connect and 6s total timeouts, a 30-second minimum fetch floor,
three retries then capped exponential backoff. A failed fetch **preserves the last good reading**, which
is what makes staleness expressible — a stale reading renders normally with its age, and only a reading
that never arrived renders an error.

**3C** — battery is a **new source on 3B's `services.Metrics`**, not a fourth service; the entire
registry diff is one line in `metricSource`. Absence is **dynamic**: the widget renders empty whenever
the current snapshot reports no battery, re-evaluated every pass, so one configuration file works on a
laptop and a desktop and a removed battery needs no reload. The warning tone is a **conjunction** — low
*and* not charging — because 15% while charging is not alarming.

## Deviations from the charter, recorded

Each was taken deliberately with reasoning in its design. **Do not "fix" these back.**

| Deviation | Where | Why |
|---|---|---|
| The graph node ships in 3B, not 3D | 3B D5 | Metrics are its only plausible consumer; defining it in 3D and retrofitting would define it twice |
| Icons ship as a font face, not baked raster assets | 3D D4 | A font is resolution-independent, so the charter's "choose a minimum checked-in size set after measuring the fractional scales" problem stops existing. Every constraint the policy protects still holds: no runtime SVG, no CGO, no external conversion, deterministic committed artifact, licence recorded |
| No stale-data or error node kinds | 3D D5 | Weather needs a colour and a sentence, not two kinds. `Node.Tone` carries it |
| `Invalidation` rekeyed from connector to global inside 3A | 3A | Owner-approved at the Task 0 gate; the Milestone 2 correction was 3 of 4 clauses complete |

The icon-font deviation pays for itself in 3C: fifteen battery glyphs cost font content and **zero new
code**. Fifteen raster icons would have multiplied the size-set problem fifteenfold. That is why 3C is
ordered after 3D rather than beside it.

## Operational traps

These will cost you time if you meet them cold. Every one was hit this session.

**Commits from a worktree need `BEADS_DB`.** bd's pre-commit hook flushes the database to JSONL; run
from a worktree it finds no database and aborts, blocking *every* commit on the branch. Deleting the
stray `.beads/vc.db*` does not help — bd then reports "no beads database found". Use:

```bash
BEADS_DB=/home/nomadx/sysc-shell/.beads/beads.db git commit -F msg.txt
```

**`bd export --force` is not a full export.** An `export_hashes` table makes bd skip issues it thinks it
already exported, and `--force` only overrides the empty-database guard. The flush hook then rewrites
`.beads/issues.jsonl` with *only recently changed issues*, so a naive merge-from-HEAD silently
reinstates stale rows and drops new ones. The working sequence:

```bash
cp .beads/beads.db /tmp/beads.db.bak
sqlite3 .beads/beads.db "delete from export_hashes;"
bd export -o /tmp/full.jsonl        # verify the row count before installing
cp /tmp/full.jsonl .beads/issues.jsonl
```

Always diff the result against `HEAD` for lost issue ids before committing.

**The commit hook rejects innocent words.** It substring-matches AI-tool names case-insensitively, so
"**bot**h", "ro**bot**", "**agent**" and similar are rejected. This cost four rejected commits this
session; "both" is the usual culprit. Pre-check every message:

```bash
grep -oiE 'claude|anthropic|chatgpt|openai|copilot|cursor|cody|tabnine|codex|gemini|bard|llm|bot|agent' msg.txt
```

**Go prunes an unimported module.** 3B's Task 0 says to commit `go.mod` after `go get`, but `go mod
tidy` removes a dependency nothing imports yet, so there is nothing to commit. The dependency correctly
lands with Task 2, its first consumer. Fold this into the plan text if you touch it.

## Document map

Everything lives in `docs/plans/`.

**Milestone 3 charter and 3A**

- `2026-08-30-built-in-widget-foundation-execution-handover.md` — **the charter.** Defines all four
  tranches and the icon-asset policy. Still authoritative for scope
- `2026-08-30-built-in-widget-foundation-design.md` — 3A design, D1–D8
- `2026-08-30-built-in-widget-foundation.md` — 3A plan, 16 tasks
- `2026-08-30-built-in-widget-foundation-audit-report.md` — 3A audit
- `2026-08-30-built-in-widget-foundation-completion-handover.md` — 3A outcome, deviations, gaps

**3B, 3D, 3C**

- `2026-08-30-core-metrics-design.md` / `2026-08-30-core-metrics.md` — 13 tasks
- `2026-08-30-weather-and-visual-vocabulary-design.md` / `2026-08-30-weather-and-visual-vocabulary.md` — 11 tasks
- `2026-08-31-power-design.md` / `2026-08-31-power.md` — 7 tasks

**Supporting**

- `docs/roadmap.md` — milestone sequence and exit gates
- `docs/prior-art.md` — architectural assessment of Noctalia, DMS, dgop, dankgo
- `tests/integration/README.md` — live matrices per tranche
- `docs/plans/README.md` — the document register. **Stale**: it still says "Only 3A is designed" and
  describes 3A as work to start. Worth correcting

**Prior-art sources**, read at these paths during design:

- Noctalia v5 — `/home/nomadx/noctalia`, C++23
- DankMaterialShell at `892b8ae` — `/home/nomadx/Documents/GitHub/DankMaterialShell`

Neither has a widget-level prior-art document of its own for M3; the findings are embedded in each
tranche's design under "Prior art review". M4 and M5 have dedicated prior-art documents; M3 does not.

## Open items for the owner

1. **30 unpushed commits on `main`.** Not pushed because a second session is active in the repository
   and pushing was never requested. `sysc-metrics` was pushed, because its tag had to be resolvable.
2. **The live matrices have never been run** — 3A's, and the ones 3B, 3D and 3C will record. The owner
   has deferred live testing. `sysc-5` remains open for Milestone 2's hardware gate. Do not claim any
   live behaviour from the automated gates.
3. **No tranche after 3A has been audited.** 3A had an independent audit that caught seven findings, two
   of which would have left the tree red at a task boundary. 3B, 3D and 3C have had a self-review only.
   An audit before executing each is the pattern that worked.
4. **`docs/plans/README.md` is stale** and now contradicts the tracker.

## Invariants to preserve

If implementation seems to require any of these, stop and return to the owner rather than improvising.

- a second goroutine touching a Wayland proxy, including from a timer;
- one sampler, timer or polling goroutine per output;
- an interface over `Clock`, `Metrics` and `Weather`, or a service registry;
- a `replace` directive or an untagged module import;
- recomputing a rate in the shell from a nominal interval;
- a runtime SVG decoder, CGO library, or external conversion process;
- geocoding, automatic location, or any remote host besides Open-Meteo;
- a dismiss shield or keyboard focus for the tooltip — those belong to Tranche 4A's panels;
- a per-frame repaint for a charging animation;
- widget identity keyed by connector rather than Wayland global.
