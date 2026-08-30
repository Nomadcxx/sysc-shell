# Power (Milestone 3, Tranche 3C) Completion Handover

Date: 2026-08-31
Branch: `milestone/power`, unmerged, based on `milestone/weather-vocabulary`
Worktree: `/home/nomadx/.config/superpowers/worktrees/sysc-shell/milestone/power`
Design: `2026-08-31-power-design.md`
Plan: `2026-08-31-power.md`
Issue: `sysc-9`
Library: `github.com/Nomadcxx/sysc-metrics@v0.2.0`

All seven tasks are implemented. The live matrix has not been run (owner-deferred).

## Commits

| Commit | Task |
|---|---|
| `0694e0d` | 0 — raise the metrics library to the power release |
| `2171386` | 0 — restore beads JSONL after worktree hook truncation |
| `11f4562` | 1 — add battery symbols to the project face |
| `ad03da7` | 2 — sample battery state as a leased source |
| `2acbec0` | 3 — add the battery widget vocabulary |
| `a64ac3b` | 4 — render battery level, label and warning |
| `db6b430` | 5 — lease the battery source per bar |
| `fe32cd3` | 6 — cover battery absence, tone and redraw |

## Task 0 reconciliation

`sysc-metrics` `v0.2.0` (`c46fab8`) matches the plan's assumed surface: `ReadBattery() (BatterySnapshot, error)` with `Present`, `Charge` in 0..1, `ChargeValid`, `State` (`Unknown`/`Charging`/`Discharging`/`Full`), `RateWatts`/`RateValid`, and `TimeRemaining`/`TimeValid`. No field was renamed. No per-device selector was required: one aggregate still covers Battery and UPS supplies.

Differences that did not stop execution:

- Collection is sysfs `/sys/class/power_supply` (Battery + UPS aggregate), not UPower DisplayDevice. The widget still sees one `Present` flag and one charge. Live matrix item 5 therefore says "stop the source", not "stop UPower".
- Thermal collectors were omitted from `v0.2.0`. Tranche 3C does not consume thermal.
- The `time` label mode shipped: the estimate is present.

`go.mod` pins `v0.2.0` with no `replace` directive.

## Automated gate

Run on 2026-08-31 at `fe32cd3`:

```
go mod tidy -diff      no difference
go test -race -count=1 ./...   ok, every package, no race report
go vet ./...           silent
go build -o /tmp/sysc-shell-tranche3c ./cmd/sysc-shell   succeeded
gofmt -l .             empty
git diff --check       clean
grep replace go.mod    no replace directive, correct
```

## Live matrix

Not run. Owner-deferred. The nine items are recorded in `tests/integration/README.md`.

## Recorded charter deviations (unchanged)

- Icons ship as an embedded font face rather than baked raster assets (3D).
- No stale/error node kinds: failure uses `Node.Tone` / `ToneError` (3D).
- Battery is a sixth source on the 3B sampling service, not a fourth service.

## Defects

- Live matrix items 1–9 have no hardware observations.
- `Acquire` still allocates a history ring for the battery selector; `record` skips it because `Snapshot.Value` has no battery arm. Left as designed so a graph would not plot a flat line.

## Next unblocked issue

Closing `sysc-9` unblocks `sysc-25` (M3 code quality sweep) and `sysc-26` (M3 spec review sweep). Those sweeps must not start until the owner confirms. Do not merge this branch unless asked.
