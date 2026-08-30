# Tranche 3B Audit Brief

Date: 2026-08-31
For: the agent auditing Tranche 3B (core metrics) and reviewing it against its spec.
Subject: `milestone/core-metrics` at `28aa607`, thirteen commits ahead of `main`, unmerged.

This is a starting brief, not the record. The record is
`2026-08-30-core-metrics-completion-handover.md`; read it second.

## Get there and prove the tree is green

```bash
cd ~/.config/superpowers/worktrees/sysc-shell/milestone/core-metrics
go mod tidy -diff && go test -race -count=1 ./... && go vet ./... && gofmt -l . && git diff --check
```

I ran that at `28aa607` and every part passed. Re-run it rather than trusting this line.

**Traps that will cost you an hour.** A commit from this worktree needs
`BEADS_DB=/home/nomadx/sysc-shell/.beads/beads.db git commit …`, or bd's pre-commit hook aborts and
blocks every commit on the branch. The same hook substring-matches AI-tool names case-insensitively and
rejects innocent words containing them — "both" and "robot" are the usual casualties. Run `bd` only in
`/home/nomadx/sysc-shell`. A second session is active in this repository on Milestones 4 and 5 and has
moved `main` before; re-read it rather than assuming.

## Read in this order

1. `2026-08-30-core-metrics-design.md` — decisions D1–D8, the evidence table, the stop conditions. 372 lines.
2. `2026-08-30-core-metrics-completion-handover.md` — what was built, the three declared deviations, and
   four characteristics I flagged for exactly this review.
3. `2026-08-30-core-metrics.md` — the 13-task plan, only where you need a task's intent. 3119 lines; do
   not read it front to back.
4. `2026-08-30-built-in-widget-foundation-execution-handover.md` — the Milestone 3 charter, still
   authoritative for what 3B is and is not allowed to contain.

## Scope

In: `internal/services/{leases,metrics}.go`, `internal/shell/{metricwidget,widget,registry,bar}.go`,
`internal/config/{config,load}.go`, `internal/ui/{tree,layout}.go`, `internal/render/paint.go`,
`cmd/sysc-shell/main.go`, and their tests. `git diff main..HEAD` is 26 files, +2146 / -327.

Out: anything thermal, battery, GPU or process (Tranche 3C and `sysc-metrics` M2); tooltips, icons and
weather (3D); the live matrix.

## The live matrix is deferred, deliberately

It is recorded in `tests/integration/README.md` and has **not** been run. The owner has deferred live
testing for the milestone. Do not raise it as a finding, and do not treat the automated gate as evidence
of live behaviour. If a claim in the code or docs asserts live behaviour, *that* is a finding.

## Already declared — confirm or refute, do not re-report as new

1. **Task 6's paint fixture was rewritten.** The plan's `newPaintFixture` set no `style.Body`, which
   `Paint` rejects, and its `filledPixels` counted the non-zero background as filled. The replacement
   reuses `newTestCanvas`/`testStyle` and counts accent pixels with exact expected counts. Judge whether
   the replacement asserts the right thing.
2. **`Registry.Close` now closes the metrics service.** Task 9's diff omitted it; Task 10's prose assumes
   it. Without it the sampling goroutine outlives the registry.
3. One error string reworded, "has no" to "has none". No behaviour change.

## Where to look hardest

Ranked by where I think a defect is most likely, with anchors.

| Area | Anchor | The question |
|---|---|---|
| Filesystem history subject | `internal/services/metrics.go:386` | `record` pushes `Filesystems[0]`, not the widget's configured mount. A filesystem graph therefore plots a different mount than its text sibling names. D6 puts one ring per source, so this follows from the design — but is the design wrong here, or should the ring key on the subject? |
| Lock discipline | `internal/shell/registry.go:265`, `:281` | `viewLocked` runs under `r.mu` and calls into `Metrics`, which takes `m.mu`. Confirm no path takes them in the other order. The sampling goroutine takes `m.mu` and never `r.mu`; verify that holds everywhere. |
| Sampler ownership | `internal/services/metrics.go:219`, `:258` | The three stateful samplers are constructed inside `run` and must never escape it. One goroutine, for the process lifetime, is a charter invariant. |
| Repaint breadth | `internal/shell/bar.go:103` | `hasPlottedWidget` forces a repaint every sample for any bar carrying a meter *or* a graph. A graph genuinely changes every tick; an unchanged meter does not. Is the wider net justified or a defect against "no source change, no submitted frame"? |
| Newest-wins publish | `internal/services/metrics.go:322` | `sendSnapshot` drains then re-sends. It assumes a single sender. Confirm that assumption holds and that no snapshot can be lost when it does not. |
| Early return in `run` | `internal/services/metrics.go:219` | The loop returns when `finestLocked()` is zero. Confirm this cannot leave `m.stop` non-nil with no goroutine, i.e. `Running()` cannot lie. |
| Graph painting | `internal/render/paint.go:112` | Integer column width truncates: `box.W / len(values)`. With 120 samples in 48 logical pixels the tail is dropped by the `len(values) > box.W` slice. Check the arithmetic at the boundaries and that nothing writes outside `box`. |
| Validation completeness | `internal/config/load.go:339`, `:414` | Every option must be required on exactly one id and rejected on all others, each failure naming its exact field path. Look for a combination that slips through — particularly an option on an *unknown* id, and `direction` interacting with `selector`. |
| Allocation per update | `internal/shell/registry.go:281`, `internal/shell/metricwidget.go:194` | `historyLocked` copies every leased ring once per view assembly, then `normalise` allocates again per graph. Bounded and small, but it is the tranche's only per-update allocation of consequence. |

## Invariants — a breach is a stop condition, not a refactor

- One goroutine owns the Wayland connection and every proxy; no new code touches a proxy.
- One goroutine owns all three `sysc-metrics` samplers. No sampler, timer or polling goroutine per output.
- Widget instances keyed by `wl_registry` global, never by connector.
- Services are concrete. No interface over `Clock` and `Metrics`, no registry, no injection container.
- No `replace` directive, no untagged `sysc-metrics` import. `go.mod` must read `v0.1.0`.
- The shell never recomputes a rate from a nominal interval. `formatMetric` takes no interval; that
  signature is the enforcement.
- A metrics failure degrades to `"-"` and never stops the shell.
- Invalidations publish with a blocking send. Milestone 2 dropped them on a full channel and a ninth
  changed bar never repainted.
- No new dependency beyond `sysc-metrics`.

## Deliberate charter deviations — do not "fix" these back

- **The graph node ships in 3B, not 3D** (D5). Metrics are its only plausible consumer.
- **Five separate item ids**, not one parameterised `metric` (D1).
- **A meter is rejected on a rate source at load** (D-rate rule). A graph stays legal everywhere because
  it normalises against its own window.
- **An unavailable value renders `"-"`**, not a stale or error node (D7). Those belong to 3D.

## Calibration

3A's audit found seven findings, two of which would have left the tree red at a task boundary. That is
the yield to expect from a tranche of this size. 3B has had self-review only.

Its report is `2026-08-30-built-in-widget-foundation-audit-report.md`; its shape — scope, findings,
claims that could not be verified, decisions judged wrong, recommended action — is worth matching.

## After the audit

`sysc-8` is still open and the branch is unmerged; closing and merging are the owner's calls. `sysc-10`
(Tranche 3D) unblocks when `sysc-8` closes. `sysc-19` (power and thermal collectors in `sysc-metrics`,
a different repository) is independent and ready now.
