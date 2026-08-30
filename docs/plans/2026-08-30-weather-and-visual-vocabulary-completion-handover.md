# Weather and Visual Vocabulary (Milestone 3, Tranche 3D) Completion Handover

Date: 2026-08-31
Branch: `milestone/weather-vocabulary`, unmerged
Worktree: `/home/nomadx/.config/superpowers/worktrees/sysc-shell/milestone/weather-vocabulary`
Design: `2026-08-30-weather-and-visual-vocabulary-design.md`
Plan: `2026-08-30-weather-and-visual-vocabulary.md`
Issue: `sysc-10`

All eleven tasks are implemented. The live matrix has not been run (owner-deferred).

## Commits

| Commit | Task |
|---|---|
| `9108d4e` | 1 — leased weather service lifetime |
| `af439e0` | 2 — fetch current weather within a bounded budget |
| `aded688` | 3 — prove the weather fetch floor holds |
| `822f279` | 4 — project symbols from an embedded face |
| `7bfb9b9` | 5 — paint failure text in the error token |
| `2844496` | 6 — weather source and widget configuration |
| `3475bbe` | 7 — render the weather widget from a reading |
| `dae88c9` | 8 — pump weather readings into the bar registry |
| `a80b834` | 9 — place and map a hover surface |
| `73c84bc` | 10 — raise a hover request after a dwell |
| `0aeef18` | 11 — wire hover surfaces and record the live matrix |

## Automated gate

Run on 2026-08-31 at `0aeef18`:

```
go mod tidy -diff      no difference
go test -race -count=1 ./...   ok, every package, no race report
go vet ./...           silent
go build -o /tmp/sysc-shell-tranche3d ./cmd/sysc-shell   succeeded
```

## Live matrix

Not run. Owner-deferred. The eleven items are recorded in `tests/integration/README.md`.

## Recorded charter deviations (unchanged)

- Icons ship as an embedded font face rather than baked raster assets.
- No stale/error node kinds: failure uses `Node.Tone` / `ToneError`.

## Defects

- `Weather.RequestURL` is exported so reload tests in package `shell` can inspect the request. It exists for tests.
- Tooltip painting uses a lazily created system font map on the Wayland owner; it does not share the bar's `FontMap`.
- Live matrix items 1–11 have no hardware observations.

## Next unblocked issue

`sysc-9` (Tranche 3C: power) remains blocked on `sysc-19` (sysc-metrics power collectors). Closing `sysc-10` does not make 3C ready.
