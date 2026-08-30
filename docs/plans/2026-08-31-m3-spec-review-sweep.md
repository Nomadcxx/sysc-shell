# M3 Spec Review Sweep

Date: 2026-08-31
Issue: `sysc-26`
Branch: `milestone/power` at `67dcd5b` (includes 3A–3D)
Documents: the four tranche designs, their plans, the 2026-08-31 tranche audits, `AGENTS.md`, and
the Milestone 3 charter (`2026-08-30-built-in-widget-foundation-execution-handover.md`).

Live Niri matrices are owner-deferred and are not failures. Recorded charter deviations are not
new defects.

## Verdict

The landed M3 widget code matches the approved designs. No new spec defects. Prior audit findings
remain applied. Two library/handover notes stay recorded, not reopened.

## Charter (`AGENTS.md`)

| Constraint | Result |
|---|---|
| Go only; no C++/Rust/Lua/Qt/QML | Holds. |
| Wayland types in `internal/platform/wayland`, Niri types in `internal/platform/niri` | Holds. Tooltip lives in `wayland`; dwell lives in `shell`. |
| `sysc-wayland@v0.1.1`; no `dankgo` | Holds. `sysc-metrics@v0.2.0`, no `replace`. |
| One Wayland-owner goroutine | Holds. Dwell fires on a timer goroutine and only sends `TooltipRequest`. |
| Draw after invalidation | Holds. |
| First-party widgets in Go | Holds. No binary plugins. |
| UI primitives only for an approved component | `KindMeter` / `KindGraph` are 3B D4/D5. `KindButton` is Milestone 1, reserved for 4A. |
| Niri first; no lock screen or compositor | Holds. |

## Tranche 3A

D1–D8 hold: no widget interface, instances keyed by `wl_registry` global, per-output title via
`projectOutputs`, one `clock` id, per-instance options, layout-derived 1s/1min boundary,
consumer-counted clock with an unconditional Niri stream, `MaxWidth` on the title. Clocks request
tabular figures.

The 2026-08-31 implementation audit's four findings stay applied (`needsLayout` in `apply`,
`DropHost` on a failed `hostBecameReady`, boundary-change reload test, focus/title/close
invalidation test).

## Tranche 3B

D1–D8 hold as amended: split ids, a second concrete `Metrics` service, per-selector leases,
text/meter/graph, graph node in 3B (recorded deviation), rings keyed by `Selector`, absent meter
and graph paint nothing, `MinWidthText` floors percentages at `"100%"`.

The 2026-08-31 audit's six findings stay applied.

## Tranche 3D

D1–D9 hold: Open-Meteo over `net/http`, a third concrete `Weather` on `leaseSet`, 6-second client
and request budget with no separate dial deadline (D3 amendment), icon font (recorded deviation),
`Node.Tone` rather than extra kinds (recorded deviation), tooltip as Overlay / `exclusive_zone −1`
/ keyboard none, placement clamped inside the output, dwell never touching a proxy. Tooltip was
cuttable and was implemented. `Weather.Reconfigure` is called across reload so coordinates and
unit change without `Starts()` incrementing.

Handover notes, not spec misses: `RequestURL` exported for tests; tooltip paint uses a lazy
system font map rather than the bar's `FontMap`.

## Tranche 3C

D1–D6 hold: `SourceBattery` on the 3B sampler, fifteen glyphs on the 3D face, empty render when
`Present` is false or charge is invalid, warning tone only when charge is at or below `warn-below`
and not charging, four label modes, Task 0 against `ReadBattery` / `BatterySnapshot` at
`v0.2.0`. No thermal, no popout, no per-frame charging animation, no fourth service.

Task 0 differences that did not stop execution, already in the 3C handover:

- Collection is sysfs `/sys/class/power_supply` (Battery + UPS aggregate), not UPower
  DisplayDevice. The design preferred UPower with a sysfs fallback; the tagged library shipped
  sysfs only. One aggregate and `Present` still exist, so D3 holds.
- Thermal was omitted from `v0.2.0`. 3C does not consume it.

## Out of scope, named so they are not re-filed

- Live matrices for 3A–3D: owner-deferred.
- Icon font vs raster, `Tone` vs extra node kinds, graph-in-3B: recorded deviations.
- `sysc-5` (Milestone 2 two-output hardware qualification): still open, not an M3 widget gap.
