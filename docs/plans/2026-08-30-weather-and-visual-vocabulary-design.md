# Weather and Visual Vocabulary Design — Milestone 3, Tranche 3D

Date: 2026-08-30
Status: Owner-approved in session. Not yet audited.
Branch: `milestone/weather-vocabulary`
Worktree: `/home/nomadx/.config/superpowers/worktrees/sysc-shell/milestone/weather-vocabulary`

Implements the tranche described in
[the Milestone 3 charter](2026-08-30-built-in-widget-foundation-execution-handover.md). Builds on
[Tranche 3A](2026-08-30-built-in-widget-foundation-design.md) and reuses the lease bookkeeping
[Tranche 3B](2026-08-30-core-metrics-design.md) extracts. Reads forward to
[the Tranche 4A panel design](2026-08-30-panel-foundation-design.md) for the surface model.

## Scope

Tranche 3D ships:

- a `weather` widget on every configured output, from Open-Meteo with configured coordinates;
- eight project-owned weather icons as a font face;
- an error tone for text that reports a failure rather than a value;
- a hover tooltip on its own surface.

It ships no geocoding, no automatic location, no forecast panel, no runtime SVG decoding, and no
keyboard interaction. The tooltip is the last task and may be cut without stranding the rest.

## Decisions

| # | Decision | Rejected alternative |
|---|---|---|
| D1 | Open-Meteo over `net/http` with configured coordinates. | Geocoding, or automatic location. The charter defers both pending a separate privacy and failure-policy decision, and DMS reaches a third-party reverse geocoder to do it. |
| D2 | `services.Weather`, a third concrete service on the shared `leaseSet`. | A generic polled-remote-source abstraction over `Clock`, `Metrics` and `Weather`. Three concrete services with shared bookkeeping is not three implementations of an interface. |
| D3 | Bounded network discipline: 15-minute interval, 3s connect and 6s total timeouts, a 30-second minimum fetch floor, three retries at 30s, then `min(60s × 2ⁿ, 300s)` backoff. | An unbounded retry loop, or a bare `http.Get` with no timeout. |
| D4 | The eight icons ship as a project-owned font face injected into `FontMap`. | Baked PNG or alpha-mask assets per the charter's literal policy. **Recorded deviation**; see below. |
| D5 | No new node kinds. `Node.Tone` carries `normal` or `error`; staleness is expressed in the text. | Separate stale-data and error node kinds, which the charter permits only if a widget needs them. Weather needs a colour and a sentence, not two kinds. |
| D6 | The tooltip is an OSD-shaped surface: one Overlay layer surface, `exclusive_zone −1`, keyboard none, no dismiss shield. | Tranche 4A's panel shape, which pairs a panel with a fullscreen dismiss shield and keyboard `Exclusive`. |
| D7 | Tooltip placement adopts Tranche 4A's D5 rule verbatim. | A second placement rule that 4A would later have to reconcile or replace. |
| D8 | The dwell timer signals the owner goroutine; it never touches a proxy. | A `time.AfterFunc` creating the surface directly, which would break the one-goroutine invariant. |
| D9 | The tooltip is the final task and is cuttable at that boundary. | Interleaving surface work through the tranche. |

## Prior art review

Reviewed on 2026-08-30 against local sources:

- Noctalia v5, `/home/nomadx/noctalia`, `src/shell/bar/widgets/weather_widget.h` and the glyph registry
  under `src/render/text/`.
- DankMaterialShell at `892b8ae`, `Services/WeatherService.qml` and
  `Modules/DankBar/Widgets/Weather.qml`.

### What confirmed this design

**Eight icons cover the whole WMO code set.** DMS maps every weather code to one of `clear_day`,
`partly_cloudy_day`, `cloud`, `cloudy_snowing`, `foggy`, `rainy`, `snowing_heavy`, `thunderstorm`. Eight
symbols is small enough to satisfy the charter's instruction not to build a general asset pipeline
before the first icon exists.

**Icons as glyphs are established.** Noctalia renders bar icons through a `glyph_registry` and a
`glyph_program`, not as raster assets, and its weather widget pairs a `Glyph` with a `Label`. D4 follows
that shape rather than the charter's raster route.

**The widget is an icon plus a bounded label.** Noctalia's `WeatherWidget::Options` is
`maxWidth = 160`, `showCondition`, `showTemperature`. Its `m_lastText` and `m_lastGlyph` compare before
setting, which is the change detection Tranche 3A already implements in `Bar.apply`.

### What changed this design

**Network failure needs a vocabulary, and DMS supplies it.** `WeatherService.qml` carries
`updateInterval` 900000, `maxRetryAttempts` 3, `retryDelay` 30000, `minFetchInterval` 30000, and a
persistent backoff of `Math.min(60000 * Math.pow(2, persistentRetryCount), 300000)`. Its fetch runs
`curl -sS --fail --connect-timeout 3 --max-time 6 --limit-rate 100k --compressed` under
`nice -n 19 ionice -c3`. Every one of those bounds exists because an unbounded weather fetch is a hang
in a shell that must keep painting. D3 adopts the numbers directly; Go expresses them as an
`http.Client` timeout plus a per-request context deadline, and the size cap replaces `--limit-rate`.

**A failed fetch must not discard a good reading.** Nothing in the reference shells throws away the last
observation on a network error, and it is what makes staleness expressible at all: a reading with an age
is still information, where an empty widget is not. The service therefore retains the last good reading
across failures, and only a reading that never arrived renders an error.

### Anti-patterns observed

- **DMS reaches a third-party reverse geocoder.** `getLocationName` calls
  `https://nominatim.openstreetmap.org/reverse?lat=…&lon=…`, sending the user's coordinates to a second
  host purely to render a place name. D1 ships configured coordinates and no geocoding at all; the
  charter already required a separate privacy decision before automatic location, and this is why.
- **DMS fetches by subprocess.** The charter forbids an external process; Go's `net/http` needs none.

### Deferred, with prior art noted

| Feature | Prior art |
|---|---|
| Forecast panel or dashboard | DMS `WeatherTab.qml`, `WeatherOverviewCard.qml`; Milestone 4 |
| Automatic location and geocoding | DMS geocoding and Nominatim; needs the charter's privacy decision |
| Humidity, pressure, wind, sunrise | Available in the same Open-Meteo response; no bar consumer |
| Per-widget unit override | DMS `SettingsData.useFahrenheit` is global; unit is process-wide here too |

## The weather service

`internal/services.Weather` is concrete, on the same `leaseSet` as `Clock` and `Metrics`.

```go
type Unit uint8 // UnitCelsius, UnitFahrenheit

func NewWeather(latitude, longitude float64, unit Unit) *Weather
func (w *Weather) Acquire(interval time.Duration) (*Lease, error)
func (w *Weather) Updates() <-chan Reading   // newest-wins, capacity 1
func (w *Weather) Close()
func (w *Weather) Running() bool
func (w *Weather) Starts() int
```

```go
// Reading is the newest observation and the state of the fetch that produced
// it. Observed is false until the first success; a failure after that leaves
// the observation intact and only advances FailedSince, which is what makes a
// stale reading distinguishable from no reading at all.
type Reading struct {
	Observed bool
	// Temperature is in Unit, not always Celsius: the request asks the API for
	// the configured unit rather than converting here, so the reading has to
	// carry which one it is.
	Temperature float64
	Unit        Unit
	Code        int // WMO weather code
	FetchedAt   time.Time
	FailedSince time.Time // zero while healthy
}
```

Lifetime is `Clock`'s: the first lease starts the goroutine, the last stops it and waits for exit, and a
shorter interval re-arms rather than restarting, so `Starts()` stays at 1 across a reload.

### Fetching

One `http.Client` is constructed with `Timeout: 6 * time.Second`, and each request additionally carries
a context deadline, so neither a stalled connect nor a slow body can outlive the budget. The request is:

```
https://api.open-meteo.com/v1/forecast
  ?latitude=<lat>&longitude=<lon>
  &current=temperature_2m,weather_code
  &timezone=auto
  [&temperature_unit=fahrenheit]
```

Only `current=temperature_2m,weather_code` is requested. DMS asks for humidity, apparent temperature,
pressure, wind, a seven-day daily block and sunrise times; none has a consumer on a 44-pixel bar, and
every unused field is bytes and parsing on a 15-minute timer.

The body is read through an `io.LimitedReader` with a small cap and decoded with `encoding/json`. A
response exceeding the cap is an error rather than an allocation.

Failure handling, in order: a failure increments an attempt count and retries after 30 seconds, up to
three times; beyond that the delay becomes `min(60s × 2ⁿ, 300s)`. A success resets both. Independently,
no fetch is issued within 30 seconds of the previous one regardless of what asks. Logging is
edge-triggered — one line when fetching begins failing, one when it recovers.

`net/http` and `encoding/json` are standard library. This tranche adds no dependency.

## Icons

The eight symbols ship as one project-owned font, embedded with `go:embed`, resolved by `FontMap` for a
private-use codepoint range before the system query runs:

```go
// Face resolves one rune, falling back per rune and caching the result.
//
// A project icon rune resolves to the embedded face first, so a system font
// that happens to cover the private-use range can never win.
func (m *FontMap) Face(r rune) *font.Face {
	if face, ok := m.cache[r]; ok {
		return face
	}
	face := m.iconFace(r)
	if face == nil {
		face = outlineFaceForRune(m.inner.ResolveFace(r), m.primary, r)
	}
	...
}
```

`SplitRuns` already divides text where the resolved face changes, so an icon rune becomes its own run
with no further work, and the existing shaping, mask and `blendMask` path renders it tinted with the
theme foreground exactly as text is. `render.ParseFace` already exists to load the embedded bytes.

**Why a font rather than baked masks.** The charter's policy asks for committed PNG or alpha-mask
outputs at the physical sizes the shell uses, and requires the plan to choose a minimum size set after
measuring the bar's fractional scales, with a nearest-larger-and-downscale fixture if it wants fewer.
A font removes that problem rather than solving it: outlines are resolution-independent, so there is no
size set, no downscale fixture, and no drift when a new scale appears.

Every constraint the policy protects is still met. No SVG is parsed at runtime; no runtime SVG
dependency, CGO library or external conversion process is added; the artifact is deterministic and
committed; and the authored SVG sources are committed beside the font with the licence of both. What
changes is the format of the baked artifact, which is why this is recorded as a deviation rather than
silently taken.

## Tone

`ui.Node` gains one field:

```go
// Tone selects which theme colour paints a text node. Error is for text that
// reports a failure instead of a value; a stale value is still a value and
// stays normal, carrying its age in the text.
type Tone uint8

const (
	ToneNormal Tone = iota
	ToneError
)
```

The painter maps `ToneError` to `style.Error` and everything else to `style.Foreground`. No new node
kind is added, because nothing in this tranche needs one.

The muted token is deliberately not used. The Tranche 3A design audit measured it at 1.47:1 against the
background, sound as a decorative track fill and unusable for text at size 14, so "grey out a stale
reading" was never available. Error measures 5.34:1 and is safe.

## The tooltip surface

One tooltip exists process-wide, matching Tranche 4A's single-instance policy for panels. It appears
after a hover dwell on a widget carrying tooltip text and disappears on pointer leave.

**Surface shape.** Tranche 4A defines two: a panel, which pairs a content surface with a fullscreen
dismiss shield and takes keyboard `Exclusive`; and an OSD, which its design describes as reusing "none
of the shield machinery: keyboard none, no shield, created on demand". A hover tooltip takes no keyboard
and needs no outside-click dismissal, so it is the OSD shape — a single Overlay layer surface with
`exclusive_zone −1` and keyboard none.

**Placement** adopts 4A's D5 rule unchanged: anchored off the triggering bar's edge, aligned to the
triggering widget's section, clamped fully inside the output minus padding and the bar's reserved space.
Writing 3D against that rule means 4A adopts it rather than reconciling two.

**Not pulled forward** from 4A: D6's SDF rounded-rect masks and pre-blurred shadows, and D13's fade and
slide motion. Both belong to the panel foundation, and a tooltip is legible without them.

**The dwell timer never touches Wayland.** A `time.AfterFunc` fires on its own goroutine; if it created
the surface it would break the invariant that one goroutine owns the connection and every proxy.
Instead it sends on a channel the owner's existing wake pipe already bridges — the same route
invalidations take since Tranche 3A. The owner goroutine, and only it, creates the layer surface,
attaches a buffer, and destroys it.

**Reload closes an open tooltip** rather than re-resolving it. Tranche 4A contracts that a reload must
not destroy open panels, because its settings modal writes configuration on every change and would
eject the user from its own UI; a tooltip is transient and reappears on the next hover, so that contract
does not extend to it.

## Configuration

Coordinates are process-wide because the service is, so they take a top-level block — the first beyond
`bar`, `theme` and `outputs`:

```json
{
  "weather": {"latitude": -33.87, "longitude": 151.21, "unit": "celsius"},
  "bar": {
    "items": {
      "right": [{"id": "weather", "max-width": 160, "show-condition": true}]
    }
  }
}
```

```go
// Weather is the process-wide weather source. Zero value means unconfigured,
// which is an error only when a weather widget exists.
type Weather struct {
	Latitude  float64
	Longitude float64
	Unit      string
	Interval  time.Duration
	Configured bool
}
```

Validation, each failure naming its field path:

- `latitude` in −90..90, `longitude` in −180..180;
- `unit` is `celsius` or `fahrenheit`, defaulting to `celsius`;
- `interval` is positive, defaulting to 15 minutes;
- **a `weather` item with no `weather` block is an error naming `weather.latitude`.**

That last rule is the configuration's first cross-section check. It is worth the machinery: the
alternative is a widget that renders an error forever because of a missing block the user was never told
about.

The item reuses Tranche 3A's `MaxWidth` and adds `show-condition`, defaulting false.

## Invalidation and redraw

Unchanged. A reading publishes to the newest-wins channel; a pump goroutine calls
`Registry.UpdateWeather`, which applies the view to every bar and returns the globals whose text
changed, publishing exactly those with a blocking send. Two identical readings mark nothing dirty, so a
15-minute timer costs at most one repaint per interval and none when the temperature is unchanged.

## Files

New:

- `internal/services/weather.go`, `internal/services/weather_test.go`
- `internal/shell/weatherwidget.go`, `internal/shell/weatherwidget_test.go`
- `internal/render/icons/` — authored SVG sources, the generated font, and their licences
- `internal/render/iconfont.go`, `internal/render/iconfont_test.go`
- `internal/platform/wayland/tooltip.go`, `internal/platform/wayland/tooltip_test.go`
- `internal/shell/tooltip.go`, `internal/shell/tooltip_test.go`

Changed:

- `internal/render/fontmap.go` — icon face resolution
- `internal/render/paint.go` — tone selection
- `internal/ui/tree.go` — `Node.Tone`
- `internal/config/config.go`, `load.go` — the `weather` block, the item, the cross-section check
- `internal/shell/widget.go`, `registry.go` — the weather widget, its lease, `UpdateWeather`
- `internal/platform/wayland/client.go` — tooltip surface lifecycle on the owner goroutine
- `cmd/sysc-shell/main.go` — weather pump
- `tests/integration/README.md` — the Tranche 3D live matrix

## Automated evidence

| Required behaviour | Check |
|---|---|
| Two bars share one service and one fetch | Two leases yield one goroutine; one update changes both bars |
| The last consumer stops the goroutine | `DropHost` leaves `Running()` false; goroutine count returns to baseline |
| An accepted reload does not restart a live service | `Starts()` is still 1 across an interval change |
| A request cannot hang | A server that never responds fails within the timeout budget |
| A failure preserves the last good reading | The observation survives; `FailedSince` advances |
| A never-fetched reading renders the error tone | Distinguished from a stale reading, which renders normally |
| Backoff is bounded | The delay reaches the cap and stays there |
| The minimum fetch floor holds | Two requests inside the floor issue one fetch |
| An oversized response is rejected | No unbounded buffering |
| An icon rune resolves to the project face | `SplitRuns` isolates it; the system query is not consulted |
| A weather item with no coordinates fails at load | The error names `weather.latitude` |
| Out-of-range coordinates fail at load | Both bounds on both fields |
| The dwell timer touches no proxy | The timer signals a channel; the owner creates the surface |
| A tooltip closes on pointer leave and on reload | Surface destroyed, nothing leaked |
| No reading change produces no redraw | Two identical readings mark nothing dirty |
| Cancellation stops every goroutine | `go test -race`, goroutine count before and after |

The fetch tests run against an `httptest` server, never the live API.

## Live gate

Recorded in `tests/integration/README.md` when the plan lands. Coordinates, place names and measurements
stay out of Git.

- one output, then at least two, each rendering the reading independently;
- the icon and the temperature render, and the icon changes with the code;
- disconnect the network: the reading goes stale with its age, and the shell keeps painting;
- reconnect: the reading recovers without a restart, within one backoff step;
- an unreachable host at startup renders the error tone rather than an empty widget;
- reload changing coordinates, unit and interval without restarting the service;
- hover a widget: the tooltip appears after the dwell, is placed inside the output, and closes on leave;
- hover near an output edge: the tooltip stays fully on-screen;
- reload with a tooltip open: it closes and no surface leaks;
- idle CPU and wakeups over 60 minutes against the Tranche 3B baseline.

## Dependencies and assumptions

1. **Tranche 3B has landed**, providing the `leaseSet` this service reuses. If 3D is executed first,
   Task 1 of the 3B plan must be executed here instead.
2. **Tranche 4A has not landed.** This design reads its surface model but depends on none of its code.
   If 4A lands first, the tooltip should adopt its aux-surface machinery rather than the minimal
   lifecycle here.
3. **Open-Meteo needs no API key** and is reachable over HTTPS.
4. **The icon font is authored and committed** before the widget task. Producing it is a build-time
   step, not a runtime one.

## Stop conditions

Return to the owner rather than improvising if implementation requires:

- a second goroutine calling a Wayland proxy, including from the dwell timer;
- a runtime SVG decoder, a CGO library, or an external conversion process;
- geocoding, automatic location, or any second remote host;
- an interface over `Clock`, `Metrics` and `Weather`;
- a dismiss shield or keyboard focus for the tooltip;
- a new dependency; `net/http` and `encoding/json` cover the fetch;
- a forecast panel or popout, which belong to Milestone 4.
