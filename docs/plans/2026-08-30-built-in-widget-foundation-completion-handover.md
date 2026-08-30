# Tranche 3A Completion Handover

Date: 2026-08-30

Branch: `milestone/widget-foundation`
Worktree: `/home/nomadx/.config/superpowers/worktrees/sysc-shell/milestone/widget-foundation`
Base: `main` at `0657ab6`
Issue: `sysc-6`

## Status

All 16 tasks (0 through 15) of
[the executable plan](2026-08-30-built-in-widget-foundation.md) are implemented and committed. The
full automated gate passes.

**The live matrix is not run.** Task 15 Step 4 is outstanding, and no live behavior is claimed
anywhere in this document. Two independent reasons:

- this session has no Niri session at all — `NIRI_SOCKET` and `WAYLAND_DISPLAY` are both unset;
- the plan gates the Tranche 3A matrix on Milestone 2 passing its own, and `sysc-5` is still open
  with its two-output, hotplug, mixed-scale, pointer-routing and 60-minute idle rows outstanding.

Leave `sysc-6` open until the matrix in `tests/integration/README.md` is executed.

## Commits, oldest first

| Hash | Subject |
|---|---|
| `9acf2b5` | fix(wayland): address invalidations by output global |
| `8b0b455` | feat(niri): decode window state from the event stream |
| `a4d572a` | feat(niri): track each workspace's active window |
| `2170512` | perf(niri): publish only when projected state changes |
| `eac4115` | feat(services): add a consumer-counted clock service |
| `13cc40a` | test(services): cover clock alignment and newest-wins delivery |
| `edd9d35` | feat(ui): cap text node width for unbounded user text |
| `558d9e7` | feat(config): adopt the tranche 3A widget vocabulary |
| `63aef13` | feat(shell): project workspace and window title per connector |
| `fc65f52` | refactor(shell): rename the proof type to Bar |
| `e48d5fe` | feat(shell): build widget views and key bars by output global |
| `ea92cec` | feat(shell): stage reload so a live service is never restarted |
| `12fc8f5` | feat(shell): pump clock snapshots into the bar registry |
| `e494507` | test(shell): cover sharing, fallback, title bounds and teardown |
| `be80c4b` | feat(render): shape clock runs with tabular figures |
| `909fbfd` | docs: record the tranche 3A live matrix |

47 files changed, 3126 insertions, 2621 deletions.

## Task 0 gate outcome

Three Milestone 2 contracts were checked against merged `main` at `0657ab6`.

**Font map wiring — confirmed.** `internal/shell/proof.go:80` constructed `render.NewSystemFontMap`
with the policy's family; no `goregular` or `render.ParseFace` remained in `internal/shell`. Window
titles get per-rune fallback and cluster-safe truncation.

**Global-keyed host identity — three of four clauses.** `NewHost(global uint32, connector string)`,
`DropHost(global uint32)` and `PreparedConfig.Hosts map[uint32]HostCallbacks` were all correct. The
fourth was not: `wayland.Invalidation` still named a connector, and `owner.invalidate` matched on
`h.connector`. The owner directed this be absorbed into Tranche 3A rather than returned to
Milestone 2. It landed as `9acf2b5`, with a test for the case connector routing cannot express —
two bars sharing one connector during reconnect overlap.

**Lossless invalidation — absent, and now fixed here.** The lossy non-blocking send with a
`default:` drop was still present in `internal/shell/registry.go`. Recorded at Task 0 and carried
into Task 13 as the plan directs. `e48d5fe` replaced it with a blocking send released by
`Registry.Close`, and `e494507` proves it at the transport: twelve bars change against a channel
that buffers eight, with a concurrent drainer, and every changed global must arrive. This closes
the defect Milestone 2's correction did not cover.

## Automated gate

Run at `909fbfd`:

```
$ go mod tidy -diff          # no difference
$ go vet ./...               # silent
$ go build -o /tmp/sysc-shell-milestone3 ./cmd/sysc-shell   # ok
$ gofmt -l .                 # nothing
$ git diff --check           # nothing
$ git diff origin/main -- go.mod go.sum   # empty

$ go test -race -count=1 ./...
ok  github.com/Nomadcxx/sysc-shell/cmd/sysc-shell        1.016s
ok  github.com/Nomadcxx/sysc-shell/internal/config       1.016s
ok  github.com/Nomadcxx/sysc-shell/internal/platform/niri     1.032s
ok  github.com/Nomadcxx/sysc-shell/internal/platform/wayland  1.018s
ok  github.com/Nomadcxx/sysc-shell/internal/render       1.810s
ok  github.com/Nomadcxx/sysc-shell/internal/services     1.029s
ok  github.com/Nomadcxx/sysc-shell/internal/shell        1.693s
ok  github.com/Nomadcxx/sysc-shell/internal/ui           1.015s
```

111 tests pass. No race. No dependency added: the `go.mod`/`go.sum` diff against `origin/main` is
empty.

The only measured baseline available without a live session is **binary size: 10,566,190 bytes**.
Every other baseline in the plan needs a running shell and is outstanding with the matrix.

## Deviations from the plan

Five, none of which changes a design decision. Each is committed with its reasoning.

**1. `Invalidation` rekeyed inside this tranche** rather than returned to Milestone 2. Owner-approved
at the Task 0 gate. Design assumption 1 anticipated the mismatch and called it a one-boundary fix.

**2. `WindowsChanged` removed from the ignored-event fixture** (`8b0b455`). Task 1 made it a modelled
event, so an empty set there published a snapshot ahead of the workspaces
`TestStreamIgnoresUnknownEvents` asserts on. The plan did not anticipate this; the fixture was simply
wrong once the event became modelled state. Not one of the audit's seven findings.

**3. Pointer tests rebuilt on a synthetic action node** (`558d9e7`). The plan says to keep
"pointer-routing tests that use a synthetic node", but none existed — every one drove the removed
toggle button. With `activateLocked` returning false unconditionally, a matched click is
indistinguishable from an unmatched one through `Handle`'s return value, so the tests now assert on
press recording and the press/release match instead. This is what the design means by keeping that
logic under test through a synthetic action node.

**4. `Registry.PrepareConfig` keeps `[]wayland.HostIdentity`** (`e48d5fe`, `ea92cec`). The plan's
stub and its Task 11 replacement both take `map[uint32]string`, which does not satisfy
`wayland.Callbacks.PrepareConfig` at `cmd/sysc-shell/main.go:81` and would not build. `HostIdentity`
already carries exactly the same pair. The plan's tests were adapted with an `identities` helper.

**5. The socket-level window test takes its snapshots rather than draining to close** (`e494507`).
The plan's version ranges the snapshot channel until close, which necessarily observes the fake
compositor hanging up — correctly reported as a stream failure since `808e2a8`, well before this
branch. It now uses `nextSnapshot`, the pattern the rest of that file already uses, and still
asserts window state arrives through the real socket read path.

Task 14 (tabular figures) was **not** cut. The audit's one major finding is fixed: `ui.Node.Tabular`
reaches the shaper as the `tnum` feature, only the clock sets it, and measurement, truncation and
rasterising all take the same flag so the drawn run and its reserved space cannot disagree.

## Invariants held

Each was checked, not assumed:

- one goroutine owns the Wayland connection; no new code calls a Wayland proxy;
- widget instances are keyed by `wl_registry` global, connector is an attribute only —
  `TestTwoGlobalsSharingAConnectorKeepDistinctInstances` proves the reconnect-overlap case;
- one Niri connection and one clock timer for the process;
- accepted reloads acquire before releasing — `Starts()` stays 1 across a reload;
- rejected reloads preserve widgets, lease counts and visible text;
- `wl_shm` remains the only renderer; no icons, no SVG decoder, no plugin or widget framework;
- no source change means no submitted frame — a tick inside one boundary reports nothing dirty;
- every goroutine stops under cancellation, with no leak after `Registry.Close`.

No stop condition in the design or the execution handover was triggered.

## Known gaps

- **The whole live matrix.** Ten numbered items in `tests/integration/README.md`, plus every runtime
  baseline. Nothing here substitutes for them.
- **Live matrix item 4 remains the one consequential unknown.** The audit verified live on niri
  `26.04 (8ed0da4)` that an in-workspace focus move emits `WorkspaceActiveWindowChanged`, so the
  join is believed correct; this tranche proves it only against fixtures. If the title does not
  update live, consume `WindowFocusChanged` as a second trigger and record it as a design change.
- **`sysc-5` is still open** with its Milestone 2 hardware rows outstanding. Milestone 2 has not
  passed its full hardware gate and must not be described as having done so.

## Tracker state

`sysc-6` is `in_progress` and should stay open until the live matrix runs.

Two problems in the tracker itself, neither touched and neither caused by the code:

- **Duplicate issues.** `sysc-18` duplicates `sysc-5` and `sysc-19` duplicates `sysc-6`, both created
  2026-08-30 21:20. `bd validate` reports one duplicate group and three pollution items. A second
  session was operating on the same database at that moment, so these were left for the owner.
- **`bd` truncates the tracked JSONL.** Many `bd` invocations rewrite `.beads/issues.jsonl` with only
  recently-changed issues; it cycled 17 to 1 to 3 to 19 to 1 lines during this session. The database
  retains every issue, so nothing is lost, but do not trust that file mid-session.

**Committing from a worktree needs `BEADS_DB`.** bd's `pre-commit` hook aborts with
`Failed to flush bd changes to JSONL` because the worktree has no usable database, which blocks every
commit on the branch. Deleting the stray `.beads/vc.db*` does not help — bd then reports
`no beads database found`. Every commit in this tranche used:

```bash
BEADS_DB=/home/nomadx/sysc-shell/.beads/beads.db git commit -F <message>
```

That exits 0 and leaves the worktree's `.beads/issues.jsonl` untouched.

## Next

1. Close out `sysc-5` on two-output hardware, following `tests/integration/README.md`.
2. Run the Tranche 3A matrix and record the baselines, then close `sysc-6`.
3. `sysc-17` (Tranche 4A, panel foundation) is blocked on `sysc-6` and unblocks when it closes.
4. `sysc-16` (six Milestone 4 review findings) is `in_progress` and independent of this tranche.
5. Tranches 3B and 3C stay blocked on `sysc-7`, the `sysc-metrics` release tag.

Do not edit the historical Milestone 2 progress handover.
