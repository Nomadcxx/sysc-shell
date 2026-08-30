# Power Design — Milestone 3, Tranche 3C

Date: 2026-08-31
Status: Owner-approved in session. Not yet audited. **Signatures provisional — see Assumptions.**
Branch: `milestone/power`
Worktree: `/home/nomadx/.config/superpowers/worktrees/sysc-shell/milestone/power`

Implements the tranche described in
[the Milestone 3 charter](2026-08-30-built-in-widget-foundation-execution-handover.md). Extends the
sampling service [Tranche 3B](2026-08-30-core-metrics-design.md) builds and the icon face
[Tranche 3D](2026-08-30-weather-and-visual-vocabulary-design.md) builds.

## The limitation this design is written against

**`sysc-metrics` has no battery code.** At `v0.1.0` no file in that module references battery, thermal,
UPower or `power_supply`, and its only executed plan is core counters. This design is therefore written
against the *contract* its design states — "battery and UPS percentage, state, energy, rate, and
estimated time", with UPower's `DisplayDevice` preferred because it aggregates multiple batteries and a
direct sysfs fallback when UPower is absent — and not against a compiled API.

Consequences, stated plainly:

- **Every signature in the "Battery source" section below is provisional.** They are what the contract
  implies, not what exists.
- **The tranche cannot execute** until `sysc-metrics` ships power collection and tags a release
  containing it. That is `sysc-19` here and the `v0.2.0` gate there.
- **A reconciliation pass is required, not optional.** When that release lands, this design and its plan
  must be re-read against the real API before any code is written. The plan's Task 0 is that gate and
  refuses to proceed on a mismatch.
- **Only the service boundary is exposed.** Icons, tone, hiding, formatting and configuration sit above
  it and are unaffected by whatever shape the library takes.

This is recorded here rather than discovered later because the alternative — waiting — would leave
Milestone 3 undesigned indefinitely, and because everything above the boundary can be settled now.

## Scope

Tranche 3C ships one `battery` widget on every configured output: a level icon, an optional label, and a
warning tone when the charge is low and falling.

It ships no thermal, no UPS-specific display, no power profiles, no popout, no notification, and no
suspend or shutdown action.

## Decisions

| # | Decision | Rejected alternative |
|---|---|---|
| D1 | Battery is a new `Source` on Tranche 3B's `services.Metrics`. | A fourth `services.Power`. It is the same library, the same polling owner and the same lease mechanism; a separate service would duplicate the sampling loop, and the charter requires one process-level polling owner. |
| D2 | Fifteen glyphs are added to Tranche 3D's icon font. | A raster icon set, or reusing `KindMeter` with a drawn fill. |
| D3 | The widget renders empty when no battery is present, re-evaluated every snapshot. | Deciding at load, or rendering the `"-"` placeholder forever on a desktop. |
| D4 | The warning tone is low **and** not charging. | A bare percentage threshold. |
| D5 | Label content per instance: `percent`, `time`, `rate`, or none. | Percentage only. |
| D6 | Signatures are provisional against the library's design contract, with a mandatory reconciliation gate. | Deferring the whole design until the library ships power. |

## Prior art review

Reviewed on 2026-08-31 against local sources:

- Noctalia v5, `src/shell/bar/widgets/battery_widget.h`.
- DankMaterialShell at `892b8ae`, `Services/BatteryService.qml` and
  `Modules/DankBar/Widgets/Battery.qml`.

### What confirmed this design

**The warning state is a conjunction.** DMS computes `isLowBattery: batteryAvailable && batteryLevel <= 20`
and then colours on `isLowBattery && !isCharging`. Fifteen per cent while charging is not a warning, and
D4 follows. Noctalia reaches the same place differently, with a `warningThreshold` whose
`warningColor` defaults to the error role.

**Label content is a per-instance choice.** Noctalia's `BatteryLabelContent { Percent, Time, Rate }` is
exactly D5. Its `showLabel` covers the fourth case, no label at all.

**An icon set for level and charge state.** DMS ships `battery_1_bar` through `battery_6_bar`,
`battery_full`, `battery_alert`, and `battery_charging_20/30/50/60/80/90/full`. D2 takes that shape,
dropping `battery_saver` and `battery_std`, which are power-profile states this tranche does not ship.

### What changed this design

**Fifteen icons cost no new code, because of a decision made in Tranche 3D.** That tranche ships icons
as an embedded font face resolved through per-rune fallback rather than as baked raster assets at each
physical size. Adding fifteen glyphs to a font is font content; adding fifteen raster icons would have
multiplied the charter's "choose a minimum checked-in size set" problem fifteenfold. The 3D deviation
pays for itself here, and this is why 3C is ordered after 3D rather than beside it.

**Noctalia's `deviceSelector` is not adopted.** It exposes `deviceSelector = "auto"` to choose among
power devices. The library's contract prefers UPower's `DisplayDevice`, which already aggregates every
battery into one figure, so a selector would expose a choice the source does not offer. A machine with
two batteries reports one aggregate, which is the behaviour a bar wants.

### Anti-patterns observed

- **Noctalia animates the charging state.** `BatteryWidget` implements `onFrameTick` and
  `needsFrameTick`, driving a repaint per frame while charging. On a bar whose invariant is that no
  source change means no submitted frame, a permanent animation would defeat the idle-wakeup budget
  Milestone 2 established. The charging state is a static glyph here.

### Deferred, with prior art noted

| Feature | Prior art |
|---|---|
| Battery detail popout | DMS `BatteryPopout.qml`; Milestone 4 |
| Power profiles | Noctalia `power_profile_widget`; not in this charter |
| Low-battery notification | Neither shell does this in the bar; Milestone 5 |
| Thermal | `sysc-metrics` design lists thermal zones; no bar consumer yet |

## Battery source

**Provisional.** These are the types the library's design contract implies, shaped to match the value
types it already ships — a collection time, validity flags beside derived values, and `Issues` for
partial failure.

```go
// In sysc-metrics, expected at v0.2.0.
type BatteryState uint8

const (
	BatteryUnknown BatteryState = iota
	BatteryCharging
	BatteryDischarging
	BatteryFull
)

type BatterySnapshot struct {
	CollectedAt time.Time
	// Present is false on a machine with no battery. Every other field is
	// meaningless then.
	Present       bool
	Charge        float64 // 0..1
	ChargeValid   bool
	State         BatteryState
	EnergyJoules  float64
	RateWatts     float64
	RateValid     bool
	TimeRemaining time.Duration
	TimeValid     bool
	Issues        []Issue
}

func ReadBattery() (BatterySnapshot, error)
```

In the shell, `services.Metrics` gains `SourceBattery` and `Snapshot.Battery *metrics.BatterySnapshot`.
Everything else about the service is unchanged: the same lease set, the same single sampling goroutine,
the same rule that only leased sources are collected. A configuration with no battery widget never reads
a power supply.

The default sampling interval is 30 seconds. A battery percentage changes slowly, and the library's
contract puts the expensive part — UPower's bus connection or a sysfs walk — behind every read.

## The widget

```json
{"bar": {"items": {"right": [
  {"id": "battery", "label": "percent", "warn-below": 20}
]}}}
```

Options, each validated at load and rejected on other ids: `label` is `percent`, `time`, `rate` or
`none`, defaulting to `percent`; `warn-below` is a percentage from 1 to 99, defaulting to 20.

### Rendering

The widget emits one glyph and, unless `label` is `none`, one text field:

| State | Text |
|---|---|
| `percent` | `84%` |
| `time` | `2h14m` while discharging, `1h02m` while charging, empty when the estimate is invalid |
| `rate` | `12.4 W` |
| `none` | glyph only |

The glyph is chosen by charge and state: seven discharging levels including full, seven charging levels
including full, and one critical glyph used when the charge is at or below the warning threshold and the
battery is not charging.

An invalid estimate renders empty rather than a placeholder. The library's contract reports
`TimeValid` false while a rate is still settling, and a battery that has just been plugged in genuinely
has no estimate for a few seconds; showing `"-"` there would flicker.

### Absence is dynamic

When the snapshot reports `Present` false — a desktop, a removed battery, UPower gone — the widget
renders empty text and no glyph. Empty text measures zero-wide, so its section shrinks and no space is
reserved, which is the mechanism Tranche 3A already uses for an absent window title.

This is evaluated on **every snapshot**, not at load. One configuration therefore works unchanged on a
laptop and a desktop, and a battery appearing or disappearing at runtime needs no reload.

### The warning tone

`ToneError`, from Tranche 3D, when the charge is at or below `warn-below` **and** the state is not
charging. The token measures 5.34:1 against the background and is safe for text at size 14.

## Configuration

`battery` joins the item vocabulary. The `Item` type gains `Label string` and `WarnBelow int`, each
rejected on any other id, in the same manner Tranche 3B rejects `path` on a CPU widget.

No top-level block is needed: unlike weather, a battery has no coordinates and no unit to configure.

## Invalidation and redraw

Unchanged. The battery arrives inside the existing metrics `Snapshot`, so `Registry.UpdateMetrics`
already applies it and reports the globals whose text changed. A percentage that has not moved marks
nothing dirty, so a battery at 84 per cent costs one comparison per interval and no frame.

The glyph is part of the widget's text, so glyph changes are caught by the same comparison. Nothing
about the battery needs the plotted-widget exception Tranche 3B adds for meters and graphs.

## Files

New:

- `internal/shell/batterywidget.go`, `internal/shell/batterywidget_test.go`
- fifteen glyphs added to `internal/render/icons/` and its font

Changed:

- `internal/services/metrics.go` — `SourceBattery`, `Snapshot.Battery`, its collection branch
- `internal/render/iconfont.go` — the battery glyph range and `BatteryIconRune`
- `internal/config/config.go`, `load.go` — the `battery` item and its options
- `internal/shell/widget.go` — the battery widget in `buildWidgets`
- `go.mod`, `go.sum` — `sysc-metrics` raised to the release containing power
- `tests/integration/README.md` — the Tranche 3C live matrix

## Automated evidence

| Required behaviour | Check |
|---|---|
| A battery widget leases only the battery source | A battery-only configuration never reads CPU or block |
| Two bars share one service and one sample | Two leases yield one goroutine; one update changes both bars |
| An absent battery renders nothing | `Present` false yields empty text and zero width |
| Absence is re-evaluated per snapshot | Present, then absent, then present again, with no reload |
| The warning tone is a conjunction | Low and discharging is error; low and charging is normal |
| Each glyph band maps to the right icon | Table across the charge range in both states |
| The critical glyph appears only below the threshold and not charging | Boundary cases at the threshold exactly |
| An invalid time estimate renders empty | Not a placeholder, and not a zero duration |
| Label modes render their own field | Four modes, four assertions |
| An option on the wrong item is rejected | `label` and `warn-below` on a clock, naming the field path |
| An out-of-range threshold is rejected | 0 and 100 both fail |
| No charge change produces no redraw | Two identical snapshots mark nothing dirty |

## Live gate

- a laptop on battery: the level glyph tracks discharge and the label matches the system reading;
- plugging in switches to a charging glyph within one interval;
- crossing the threshold while discharging shows the warning tone; charging clears it;
- a desktop with no battery renders nothing and reserves no space, with the same configuration file;
- removing a battery at runtime, if the hardware allows, hides the widget with no reload;
- idle wakeups over 60 minutes against the Tranche 3B baseline.

## Dependencies and assumptions

1. **`sysc-metrics` ships power collection and tags a release containing it.** This is `sysc-19` and
   that repository's `v0.2.0` gate. Until then the tranche cannot execute. **The signatures in
   "Battery source" are provisional and must be reconciled against the real API before implementation;
   the plan's Task 0 is that gate.**
2. **Tranche 3B has landed**, providing `services.Metrics`, its lease set and its sampling loop.
3. **Tranche 3D has landed**, providing the embedded icon face and `ToneError`. Without it the fifteen
   glyphs have nowhere to live and the warning has no colour.
4. **UPower's `DisplayDevice` aggregates multiple batteries**, so the shell needs no device selector.
   If the library instead exposes per-device readings, this design gains a selector and its widget gains
   a required option — a reconciliation-pass outcome, not a redesign.

## Stop conditions

Return to the owner rather than improvising if implementation requires:

- a fourth service, or a second polling owner;
- a per-frame repaint for a charging animation;
- thermal, power profiles, or a suspend or shutdown action;
- a device selector, unless the reconciliation pass proves the source has no aggregate;
- a new dependency beyond the `sysc-metrics` release containing power;
- a raster icon pipeline.
