# Milestone 3 Implementation Audit Report

Date: 2026-08-31
Subject: merged Milestone 3 at `c734ed3`
Issue: `sysc-27`

## Verdict

Milestone 3 implements the approved 3A through 3D architecture, including the amendments from the
earlier tranche audits. The service ownership, selector-grained histories, per-output projection,
unavailable-state rendering, weather reconfiguration, and battery policy match their designs.

The audit found two major defects and three smaller defects. The major defects sit in terminal Niri
error propagation and tooltip buffer retirement. Neither changes an approved product decision. Both
need correction before the affected behavior can be claimed from a live Niri run.

The current automated gate does not exercise these failures. The live Niri matrices and recorded
performance baselines remain owner-deferred and are not counted as failed checks here.

## Findings

| # | Severity | Evidence | Finding | Required correction | Tracker |
|---|---|---|---|---|---|
| 1 | Major | `cmd/sysc-shell/main.go:46-63`; `internal/platform/niri/client.go:84-123`; 3A design `:358-360` | A terminal Niri error is buffered before `read` closes both channels. The pump can select the closed `snapshots` channel first and return without reading `niriErrs`. It then neither reports `streamFailed` nor cancels the process. The shell can remain mapped with stale workspace and title state, contrary to the design's fail-fast policy. | Make one terminal result authoritative, or drain the error path before treating snapshot closure as clean. Add a focused check where the stream publishes a snapshot and then fails. | `sysc-31` |
| 2 | Major | `internal/platform/wayland/tooltip.go:187-223,281-311`; `internal/platform/wayland/shm.go:15-18,94-96`; root `AGENTS.md` buffer-release rule | The tooltip attaches slot 0, installs no `wl_buffer.release` handler, and destroys the generation on reconfigure or hide without checking `retirement.outstanding`. This bypasses the release-aware generation path used by bar surfaces and violates the repository invariant that compositor-held storage is retired on release. | Put tooltip buffers through release-aware retirement. Reuse the M4 auxiliary-surface owner now present on `milestone/panels-controls` if M4 lands before the fix. Add configure, hide, and release ordering evidence. | `sysc-32` |
| 3 | Moderate | `internal/shell/tooltip.go:52-64,68-81`; 3D design `:264-265` | `time.Timer.Stop` cannot cancel an `AfterFunc` callback that has begun. `leave` can observe `shown == false`, then the old callback sets it true and sends a show request after pointer leave. No later hide follows, so the tooltip can remain on screen. The mutex prevents a data race but not this ordering error. | Give each dwell a generation or current-timer token. The callback must confirm it still owns the dwell before setting `shown` or sending. | `sysc-34` |
| 4 | Minor | `internal/platform/wayland/tooltip.go:226-278`; 3D completion handover, Defects | Tooltip paint hardcodes font size, radius, background, foreground, and the default system font. A valid configured theme changes the bar while the tooltip keeps the built-in palette and typeface. The 3D handover already recorded the font-map half of this defect. | Resolve tooltip paint data from the accepted theme and font owner. Do not create another theme model in the Wayland package. | `sysc-33` |
| 5 | Minor, dead path | `internal/services/metrics.go:246-251` | `Metrics.Leased` has no callers and panics for an unleased selector because `m.leases[sel]` is nil before `.len()`. Its documented yes/no query is not total. | Delete it, as Grok proposed. If a concrete consumer appears first, add a nil guard. | `sysc-35` |

## Specification audit

### Tranche 3A

D1 through D8 hold. Widgets remain concrete values keyed by `wl_registry` global. The projection joins
each connector to its active workspace and that workspace's active window. Clock leases are shared and
boundary-derived, reload acquires before release, title width is capped, and clock shaping requests
tabular figures. The four earlier 3A implementation findings remain fixed.

Finding 1 is a direct miss of 3A's terminal failure policy. The decoder reports malformed known events
as errors, but `main` can lose the error while consuming the paired channels.

### Tranche 3B

D1 through D8 hold after their approved amendments. One `Metrics` instance owns the stateful samplers.
Leases and rings use `Selector{Source, Subject, Direction}`; the last lease discards its ring. Text,
meter, and graph modes distinguish absence from zero and compare their complete node state before
invalidating. Rates remain library-owned, and `go.mod` pins `sysc-metrics@v0.2.0` without a `replace`.

Finding 5 is outside the live path. It supports deleting the unused helper rather than changing the
metrics design.

### Tranche 3C

D1 through D6 hold. Battery uses the shared metrics sampler, renders empty when absent, selects its icon
and label from each snapshot, and warns at or below the configured threshold while not charging.
The tagged library supplies one sysfs aggregate rather than the design's preferred UPower-first source;
Task 0 and the completion handover record that accepted library difference.

The battery icon plan contains one internal wording conflict: it reserves the final level for a full
charge, while its prescribed formula and the implementation select the final level from `6/7`
upward. The implementation follows the plan's code and its monotonic-range tests. Treat this as a glyph
threshold clarification, not a new implementation defect.

### Tranche 3D

D1 through D9 hold at the decision level. Weather uses bounded `net/http`, retains a last good reading
across failure, reconfigures coordinates and unit without replacing the service, and uses the recorded
icon-font and `Node.Tone` deviations. One owner goroutine creates Wayland objects.

The tooltip has the specified layer, exclusive-zone, keyboard, placement, and reload shape. Findings 2
through 4 show that its lifecycle and visual integration do not yet meet the repository invariants around
that shape.

## Grok quality-classification review

Most of `2026-08-31-m3-quality-findings-review.md` holds. Buckets B and D are accurate, as are the
recorded charter deviations. The proposed large deletion count still includes code with named later
consumers. The reviewer checked the M4 consumer claims against `milestone/panels-controls` at
`9264292`.

The current M4 implementation changes or sharpens five rows:

| Grok row | Audit result |
|---|---|
| `KindButton`, `Node.Action`, `ui.Hit`, and Bar press/release | Split. M4 consumes `KindButton` and `Node.Action`. Its `PanelHost` implements its own focusable hit traversal and press matching, so it does not consume `ui.Hit` or the inert Bar press path. The M5 tray plan names a bar `Handle` for icon activation, menus, and scroll, so keep that path as M5-reserved unless M5 chooses the `PanelHost` traversal instead. |
| `ProofStyle.Toggled` / `AccentOn` / `accent()` | Split. The M4 branch uses `AccentOn` for toggle paint and `accent()` for button and meter paint. No production code assigns `ProofStyle.Toggled`; that field remains a dead candidate. |
| `copyNode` recursion | Re-bucket to optional. M4 panel trees use `PanelHost`; the Bar still copies leaf widgets. The recursive branch has no current M4 caller. |
| `supportedEdges` false entries | Re-bucket to specified M3 validation. They distinguish an unknown value from a known edge that this bar milestone rejects. M4 panel placement mentioning bottom does not itself consume this map. |
| `historyLocked` and `Metrics.History` | Split. M4's system monitor now gives `historyLocked` a second production caller, so it is no longer a one-call wrapper. `Metrics.History` still has no product caller, but an M4 cross-package test now uses it; unexporting it requires rewriting that test and is no longer a zero-touch cleanup. |

Grok Bucket C items 1 through 4 remain valid deletions. Item 1 also fixes finding 5. Item 7 remains an
optional inline. `Bar.New` remains an optional convenience cut. The optional-shrink list should stay
separate from correctness work; the hardcoded tooltip style is the exception because it is already a
recorded 3D defect.

## Evidence and limits

The reviewer ran the automated gate against `main` at `6e2e142`:

- `go mod tidy -diff`: exit 0, no diff.
- `go test ./...`: exit 0 across the eight packages with tests.
- `go test -race -count=1 ./...`: exit 0 across the same packages.
- `go vet ./...`: exit 0.
- `go build -o /tmp/sysc-shell-m3-audit ./cmd/sysc-shell`: exit 0.
- `gofmt -l .`: exit 0 with no paths.
- `git diff --check`: exit 0.

No live Niri or hardware claim follows from that gate. The deferred matrices still cover multi-output
projection, socket loss, hotplug, hover surface behavior, suspend/resume, screenshots, and the recorded
CPU, RSS, redraw, allocation, and binary-size baselines.
