# Built-in Widget Foundation Design — Milestone 3, Tranche 3A

Date: 2026-08-30
Status: Owner-approved. Audited 2026-08-30; all findings applied. Amended for the design audit's
major finding (tabular figures) and for assumption 4, which is now verified live.
Branch: `milestone/widget-foundation`
Worktree: `/home/nomadx/.config/superpowers/worktrees/sysc-shell/milestone/widget-foundation`

Supersedes nothing. Implements the tranche described in
[the execution handover](2026-08-30-built-in-widget-foundation-execution-handover.md).

## Scope

Tranche 3A ships four read-only text widget instances, of three types, on every configured output:

- a clock, parameterised by format, instantiated twice by default to show time and date;
- Niri workspace text per output;
- Niri focused-window title per output.

It ships the shared service lifetime, per-output widget instances, validated configuration, and the
projection and invalidation paths those widgets need. It ships no icons, no metrics, no weather, no
popouts, no keyboard focus, no animation, and no new interactive behavior.

## Decisions

| # | Decision | Rejected alternative |
|---|---|---|
| D1 | Concrete widget values that build `ui.Node` trees; no widget interface. | An internal `Widget` interface. Clock and Niri widgets share no lifecycle: one holds a service lease, the other reads shell-held state. |
| D2 | Widget instances owned per output, keyed by `wl_registry` global. | Keying by connector. Forbidden by the handover and broken across reconnect overlap. |
| D3 | Focused-window title is the active window of *that output's* active workspace. | Global focused window mirrored on every bar. See [Prior art](#prior-art-review). |
| D4 | One `clock` widget parameterised by a Go layout string. | Separate `clock` and `date` widget IDs. Neither reference shell has a date widget. |
| D5 | Widget options attached per instance, via a string-or-object union. | Options on the bar. Prevents two differently formatted clocks and has nowhere to put `max-width`. |
| D6 | Clock tick boundary derived from the layout string; 1 s or 1 min only. | A configured interval, or an unconditional 1 s tick. |
| D7 | Clock is consumer-counted. The Niri stream is an unconditional shell responsibility. | Consumer-counting the Niri stream. It is one process-wide connection with no reconnect path in this tranche. |
| D8 | `ui.Node` gains `MaxWidth` for text, consumed by `window-title`. | Relying on section collision alone to bound unbounded user text. |

## Prior art review

Reviewed on 2026-08-30 against local sources:

- Noctalia v5, `/home/nomadx/noctalia`, C++23, `src/shell/bar/` and `src/time/`.
- DankMaterialShell, `/home/nomadx/Documents/GitHub/DankMaterialShell` at `892b8ae`, plus the installed
  QML at `/usr/share/quickshell/dms`.
- The live configuration and plugin set for both, under `~/.config/`.

### What confirmed this design

**Per-output instances keyed by the Wayland global.** Noctalia's `BarInstance` holds
`std::uint32_t outputName` — the registry global name — beside `wl_output* output`, and owns
`startWidgets` / `centerWidgets` / `endWidgets` as per-instance vectors. Its `ClockWidget` is constructed
as `ClockWidget(wl_output*, Options)`. This matches D2 and the per-bar widget ownership in D1, and it
matches the existing `hostSet` keying in `internal/platform/wayland/hosts.go`.

**Concrete widget objects.** Neither bar uses a declarative widget schema. Both build typed widget
objects from a factory keyed by a configured ID string.

**Change detection before re-measure.** `ClockWidget::doUpdate` compares the formatted string against
`m_lastPrimaryText` and only then calls `setText` and `measure`. This design compares against
`node.Text`, which is the same behavior without a parallel cache, because the retained node already holds
the last rendered text.

**Empty state renders zero-width.** DMS sets `width: !hasWindowsOnCurrentWorkspace ? 0 : …` on its
focused-app widget; Noctalia exposes `hideMode`. An empty title measuring zero is the shared default.

**Declared tick precision.** Quickshell exposes `SystemClock.precision` as `Seconds` / `Minutes` /
`Hours`, and DMS's own `DisplayService` and `WallpaperCyclingService` declare `Minutes`. Consumer-declared
precision, rather than one global rate, is a framework-level concept in the closest analogue. This
supports D6.

### What changed this design

**A text node needs its own maximum width.** Noctalia's `ActiveWindow` carries `maxWidth` (default 260,
145 in the live configuration) and `useFixedWidth`. DMS clamps twice: `width: Math.min(implicitWidth,
250)` with `elide: Text.ElideRight` on the title label, and a widget-level `maxNormalWidth`. Three
independent caps.

Without one, a long title consumes its whole section's budget before anything truncates, and in the
default layout proposed here it would squeeze the adjacent `workspace` widget to zero width. This is a
real defect the review caught. D8 adds `MaxWidth`, which the handover permits because a shipped widget
consumes it.

**Suppress publication when decoded state is unchanged.** Noctalia's Niri backend returns
`if (next == m_workspaces) return false;` before notifying its consumers. `state.apply` in
`internal/platform/niri/events.go` already returns a `publish bool`, so extending it to compare avoids a
wake-pipe byte and a whole projection pass for every no-op event. `WindowOpenedOrChanged` fires on
floating, urgency and focus changes that never alter a title, so this is not a rare case. It serves the
Milestone 2 idle-wakeup gate directly.

**A dangling `active_window_id` must project to empty, not error.** Between `WindowClosed` and the
following `WorkspaceActiveWindowChanged`, a workspace's `active_window_id` names a window that is gone.
DMS handles the equivalent case through its null branch. This becomes an ordering fixture test.

### What confirmed a contested choice

Both projects show a **global** focused window on every bar, which appeared to contradict D3. It does not.

- Noctalia's `compositors::WorkspaceMetadataBackend` parameterises `workspaceWindows(outputName)`,
  `workspaceKeys(outputName)` and `appIdsByWorkspace(outputName)` by output, but `focusedWindowId()` takes
  no output at all. `ext_workspace_manager_v1`, the dwl IPC protocol and KDE virtual desktops have no
  per-output active window, so the shared interface cannot express one. Its Niri backend consumes
  `WindowFocusChanged` and ignores `active_window_id` entirely.
- DMS's `FocusedApp.qml` branches `CompositorService.isNiri` against `isHyprland` inside one widget, and
  Hyprland's active window is global.

Both bottom out at a global window because of a portability constraint `sysc-shell` does not have. Niri is
the only platform contract here, and Niri publishes `Workspace.active_window_id` per workspace.

Two further observations support D3. Noctalia exposes `followFocusedScreen` as a per-widget option on its
Workspace widget, so the per-output versus global axis is a real, user-visible choice rather than an
oversight. And DMS's `NiriService.qml` *does* consume `WorkspaceActiveWindowChanged` and never consumes
`WindowFocusChanged` at all — the join this design uses is the one already shipping on Niri.

### Anti-patterns observed

These are recorded because they are evidence for rules the handover already imposes.

- DMS's built-in `Clock.qml` declares `precision: SystemClock.Seconds` while rendering `"HH:mm"`, waking
  59 times a minute to produce identical text. The third-party `modernClock` plugin gets it right with
  `precision: root.showSeconds ? SystemClock.Seconds : SystemClock.Minutes`. Deriving the boundary from
  the format string itself, as D6 does, removes the possibility of the desync both variants carry.
- The `dankPomodoroTimer` plugin runs `Timer { interval: 1000 }` and `Timer { interval: 100 }` inside the
  widget. This is the per-widget timer the handover forbids, and the reason the clock is a shared,
  consumer-counted service.

### Deferred, with prior art noted

Not built in this tranche; recorded so a later tranche does not rediscover them.

| Feature | Prior art |
|---|---|
| Per-widget timezone | Noctalia `ClockWidget::Options::timezone`, `formatTimezoneTime` |
| Separate vertical-bar format | Noctalia `formatVertical`, `verticalFormat` |
| Tooltips | Both; Noctalia `tooltipFormat` |
| Title marquee or scroll | Noctalia `titleScrollMode`, `scrollingMode` |
| Application icon beside the title | Noctalia `displayMode`, `IconResolver`; DMS desktop-entry lookup |
| Locale-aware formatting | DMS uses `Qt.locale()` and `Locale.ShortFormat`; Noctalia uses format strings. Prior art is split; the handover directs format strings for this tranche. |
| Plugin manifest shape | DMS `plugin.json`: `id`, `version`, `capabilities`, `permissions`, `component`, `settings`, `requires_dms`. A Milestone 6 reference. |
| Namespaced plugin widget IDs | Noctalia `plugin:<id>` entries in the same widget list, with nested `defaultSettings`. |

## Widget ownership and view construction

Every Tranche 3A widget is a pure function from an immutable view to a string. There is no per-widget
mutable state, so there is no lifecycle to abstract and no interface to define.

```go
// internal/shell/widget.go

// barView is the immutable input every widget formats from. The Registry
// assembles it from one process-wide clock snapshot and this output's Niri
// projection.
type barView struct {
	Now       time.Time // zero until the first clock tick
	Workspace string
	Title     string
}

// textWidget is one configured widget instance. The node is retained; format is
// the widget's whole behavior.
type textWidget struct {
	node   *ui.Node
	format func(barView) string
}
```

The four format functions are: the clock's captured layout applied to `view.Now`; `view.Workspace`; and
`view.Title`. A clock whose `Now` is zero formats to the empty string, so a bar renders correctly before
the first tick.

`Bar.apply(view)` writes `node.Text` only where it differs and reports whether anything changed. A bar
re-layouts and invalidates only on a true return. Change detection lives in that one loop rather than in
each widget, because the retained node already holds the last rendered text.

### Ownership

- **`Registry`** is process-scoped. It owns `bars map[uint32]*Bar` keyed by Wayland global, the accepted
  `config.Config`, the per-connector Niri projection, the process-wide clock snapshot, the clock service,
  and the leases each bar holds.
- **`Bar`** is output-scoped. It owns its retained nodes, resolved theme, text renderer, its three
  sections of `textWidget` values, and its connector string.

The connector is an attribute on the `Bar`, never a key. During reconnect overlap two `Bar` values carry
the same connector and remain distinct instances with distinct leases; both read the same projection,
which is correct, because they are two transient views of one physical connector. `DropHost(global)`
removes exactly one.

## Service lifetime

`internal/services.Clock` is concrete. There is no service registry, no dependency-injection container,
and no single-implementation interface.

```go
func NewClock() *Clock
func (c *Clock) Updates() <-chan time.Time          // newest-wins, capacity 1
func (c *Clock) Acquire(boundary time.Duration) (*Lease, error)
func (l *Lease) Release()                            // idempotent, nil-safe
func (c *Clock) Close()
func (c *Clock) Running() bool                       // test observability
func (c *Clock) Starts() int                         // test observability
```

- **First consumer starts, last stops.** A lease count moving 0→1 starts the goroutine; 1→0 cancels it and
  waits for exit before `Running` reports false.
- **The service ticks at the finest boundary any live lease requires.** Coarser widgets re-format at that
  rate and report no change, which produces no redraw. `finest` is recomputed on acquire and release; a
  boundary that shrinks signals a capacity-1 `rearm` channel so the running goroutine re-arms without
  restarting. `Starts()` therefore does not increase when only the boundary changes.
- **Cancellation** stops the goroutine promptly through its `select` on `ctx.Done()`.

The update channel is created once with the `Clock`, is never closed, and survives stop and start cycles.
The registry is its only receiver and fans the snapshot out to every bar, so all bars observe one
snapshot from one update.

### Reload is acquire-before-release

The existing two-phase reload is the transaction. `wayland.PreparedConfig` gains a `Rollback func()`
beside its `Commit`.

| Phase | Action |
|---|---|
| Prepare | Build the replacement `Bar` for every ready, enabled host. Acquire each new bar's leases now. |
| Prepare fails | Release every lease acquired during this prepare. Live state is untouched. |
| Owner rejects after prepare | `Rollback()` releases the same set. |
| Commit | Swap `bars`, `leases` and `cfg` under one lock, apply the current view to each new bar, then release the **old** leases. |

Because prepare acquires before commit releases, the count never reaches zero for a service still in use,
so an accepted reload cannot stop and restart it. A rejected reload leaves lease counts, goroutines and
visible widgets identical.

`Rollback` is required rather than decorative: `owner.prepareConfig` can still fail after calling the
shell's `PrepareConfig`, when a prepared host is missing or its callbacks do not validate. Today the
prepared bars are simply dropped; with leases attached, that would leak a running goroutine.

`DropHost(global)` releases that bar's leases. `Registry.Close()` releases everything and closes the
clock, so shutdown stops every goroutine.

### Why the Niri stream is not consumer-counted

The shell already owns exactly one process-wide Niri connection, opened in `cmd/sysc-shell/main.go` before
Wayland. It is the source of the focused-output derivation, and Milestone 4 workspace commands and
Milestone 5 window/PID lineage will both consume it.

Consumer-counting it would mean tearing down and re-establishing the compositor connection whenever a
configuration edit removed the last Niri widget. That discards the complete-initial-snapshot guarantee the
event stream provides and adds a reconnect path with no consumer today. It stays unconditional.

Consumer counting is proven instead through the clock, which genuinely reaches zero: a configuration whose
items contain no `clock` leaves the service with no leases and no goroutine.

## Clock scheduling

Go's `time` package only. No ticker.

Each iteration computes its own deadline from the wall clock:

```
next := time.Now().Truncate(b).Add(b)
if time.Until(next) <= 0 { next = next.Add(b) }
```

then sleeps on a `time.Timer` until `next`, `ctx.Done()`, or `rearm`. Recomputing every iteration means
error cannot accumulate the way a fixed-period ticker's does.

If the wall clock jumps backwards the timer may fire before `next`. The published value is `time.Now()`
either way, so an early publish re-renders identical text and produces no redraw; the guard above only
prevents a busy loop.

**Timezone** is the process's local zone, through `time.Local`. Go resolves `time.Local` once at startup,
so a timezone change mid-session is not observed until restart. This is stated rather than fixed; a
per-widget timezone exists in Noctalia and is deferred.

**Suspend and resume.** Go timers run on `CLOCK_MONOTONIC`, which does not advance while the machine is
suspended. A timer armed before a long suspend therefore fires late in wall-clock terms, bounded by one
boundary — at most 60 seconds of stale text after resume. This is reasoning, not a measurement, and is
listed in the live matrix.

### Boundary derivation

`time.Format` never returns an error, so a Go layout cannot be validated by parsing. It can be validated
by observation, and the same probe yields the tick boundary.

Against a fixed reference instant in UTC, so the result is deterministic:

- if `+1s` renders differently, the boundary is **1 second**;
- otherwise the boundary is **1 minute**;
- if the layout renders identically at every probe in `{1s, 1min, 1h, 24h, 32d, 400d}`, it does not depend
  on the time at all and the candidate is **rejected** with its field path.

The rejection catches the real typo class: `"HH:MM"` is not a Go layout and renders literally as `HH:MM`
forever. The long probes prevent a false rejection of a legitimately coarse layout such as
`"January 2006"`, which varies only across months.

The boundary is capped at one minute deliberately. A daily layout could in principle sleep a day, but
`Truncate(24*time.Hour)` truncates to UTC midnight, not local midnight, so a date would flip at the wrong
moment and would break across a daylight-saving transition. Re-formatting once a minute and detecting no
change is correct, needs no calendar arithmetic, and costs one wake per minute for the whole process —
which any bar with a clock already pays.

Derivation lives in `internal/config` as an unexported helper, because it is validation of a configuration
value. `internal/services` receives a `time.Duration` and stays free of configuration types.

## Niri window state

The existing event stream is extended. No second connection, no `niri msg`.

Wire fields verified on 2026-08-30 against the installed `niri 26.04 (8ed0da4)` by reading its serialised
type names and by capturing the initial event burst; only JSON key paths were retained, no window titles
or machine state.

`Workspace` carries `{id, idx, name, output, is_urgent, is_active, is_focused, active_window_id}`.
`Window` carries `{id, title, app_id, pid, workspace_id, is_focused, is_floating, is_urgent, layout,
focus_timestamp}`.

| Event | Payload | Handling |
|---|---|---|
| `WorkspacesChanged` | `{workspaces}` | Replace the whole set; now also reads `active_window_id`. |
| `WorkspaceActivated` | `{id, focused}` | Existing behavior. |
| `WorkspaceActiveWindowChanged` | `{workspace_id, active_window_id}` | Set that workspace's active window. |
| `WindowsChanged` | `{windows}` | Build the whole set, then replace. |
| `WindowOpenedOrChanged` | `{window}` | Insert or replace by id. |
| `WindowClosed` | `{id}` | Remove by id. |
| `WindowFocusChanged` | `{id}` | Ignored. The projection is per output, not global. |
| `WindowLayoutsChanged`, `WindowFocusTimestampChanged`, `WindowUrgencyChanged`, `WorkspaceUrgencyChanged`, `KeyboardLayout*`, `Overview*`, `ConfigLoaded`, `Cast*` | | Ignored, under the existing unknown-event tolerance. |

Only `id` is required on a `Window`. `title`, `app_id`, `workspace_id` and `pid` are nullable in the
protocol and map to empty or absent. `active_window_id` is nullable on a `Workspace`.

**Failure policy** follows the existing package rules. A malformed known event is a stream error that
publishes nothing; `WindowsChanged` builds its whole set before replacing so a bad member cannot publish
partial state. Noctalia's backend is laxer here, skipping members it cannot parse; the stricter existing
policy is kept.

Unknown ids are treated by consequence, not uniformly:

- `WorkspaceActiveWindowChanged` naming an unknown workspace is a **stream error**, matching the existing
  `WorkspaceActivated` treatment. The event carries state that cannot be recorded anywhere, so the
  projection would silently keep showing a stale title, and this tranche has no resync path.
- `WindowClosed` naming an unknown window is a **no-op that publishes nothing**. The event's desired
  post-state — that window absent from the set — already holds, so there is nothing to diverge. Erroring
  here would stop the shell over an event that asks for no change.

**Publication is suppressed when the decoded state is unchanged**, so a no-op event produces no snapshot,
no wake and no projection pass.

### Projection

Per connector, in `internal/shell`:

```
workspace W := the workspace on this connector with is_active, preferring is_focused   (existing rule)
label       := W.Name, else itoa(W.Index), else "-"
title       := the window whose ID equals W.ActiveWindowID, else ""
```

A dangling `active_window_id` yields an empty title rather than an error. Both fallbacks are stable: `"-"`
for an unknown workspace, empty for no window. An empty text node measures zero-wide, so the section
simply shrinks.

There is no persistent "Niri unavailable" state in this tranche. A stream failure cancels the process
context and the shell exits, which is the behavior the live matrix already covers. The only fallback path
is the interval before the first snapshot arrives.

The projection updates every bar whose connector matches, which is one bar normally and two during
reconnect overlap. Wayland global remains the bar identity throughout.

## Configuration

An item is either a bare ID string or an object carrying that ID plus its options.

```json
{
  "bar": {
    "items": {
      "left":   ["workspace", {"id": "window-title", "max-width": 260}],
      "center": ["clock"],
      "right":  [{"id": "clock", "format": "Mon 2 Jan"}]
    }
  }
}
```

The vocabulary is exactly `clock`, `workspace`, `window-title`. `shell-name`, `meter` and `toggle` leave
`knownItems` and the defaults. There is no compatibility promise, so a stale configuration fails loudly
with a named field path.

The union is decoded by one `UnmarshalJSON` on the wire item type, which dispatches on a leading quote.
Options remain exact named fields with pointer types; there is no free-form map, no reflection, and no
configuration library.

```go
// Item is one validated widget instance.
type Item struct {
	ID       string
	Format   string        // clock only
	Boundary time.Duration // derived from Format; clock only
	MaxWidth int           // window-title only; 0 means unbounded
}
```

Validation, each failure naming its exact path:

- the ID must be known;
- `format` is accepted only on `clock`, and must vary with time as described above;
- `max-width` is accepted only on `window-title`, and must be positive;
- an option on the wrong ID is an error rather than a silently ignored field.

Defaults, which exercise all three IDs and both clock instances so the live matrix needs no special
configuration:

| Section | Items |
|---|---|
| left | `workspace`, `window-title` with `max-width` 260 |
| center | `clock` with format `15:04` |
| right | `clock` with format `Mon 2 Jan` |

The 260 default matches Noctalia's shipped `ActiveWindow` default; DMS uses 250.

## UI primitives

One addition, consumed by `window-title`:

```go
// Node gains, for KindText:
MaxWidth int // 0 means unbounded
```

`measureNode` clamps a text node's measured width to `MaxWidth` when set. Everything downstream is
unchanged: `placeSection` still grants `min(natural, remaining)`, and the painter still truncates a node
granted less than its natural width. The cap therefore composes with the existing collision rules — a
title never exceeds its cap, and can still be squeezed below it when its section runs out of room.

No other primitive is added. No icon, meter, graph, tooltip or stale-state node has a consumer in this
tranche.

## Invalidation and redraw

A clock tick:

1. the timer fires at the boundary; the goroutine publishes `time.Now()` to the newest-wins channel;
2. the clock pump goroutine in `main.go` receives and calls `Registry.UpdateClock`;
3. the registry records the snapshot and calls `Bar.apply(view)` on every bar;
4. each bar re-formats its widgets, writes only changed text, re-layouts if anything changed, and reports
   that;
5. `UpdateClock` returns the globals whose text actually changed, and the registry publishes exactly
   those on the invalidation transport;
6. the owner drains them and calls `sched.Invalidate()` on those hosts only;
7. `nextJob` renders a dirty host with a free slot and no frame pending; `Submitted` clears the flag.

A Niri title change follows the same path from step 3, with the projection deciding which connectors, and
therefore which globals, changed.

`UpdateClock` and `UpdateNiri` both return the changed globals as `[]uint32`, mirroring the existing
`UpdateNiri` which already returns changed connectors. That return value is both what feeds the
invalidation transport and what the tests assert against, so the evidence below does not depend on which
transport the Milestone 2 correction lands.

No state change means step 4 reports false, nothing is marked dirty, and no frame is submitted.

Multiple updates between frames coalesce: the dirty set is a set, and the scheduler's `dirty` flag is
already idempotent. The latest visible state cannot be dropped, because the node text is written before
the global is marked dirty and the mark is not consumed until the owner drains it.

**The registry keeps publishing; the returned globals are not a substitute for it.** `UpdateClock` and
`UpdateNiri` both return the changed globals *and* send one `wayland.Invalidation` per changed global on
the channel `Registry.Invalidations()` exposes. `cmd/sysc-shell/main.go` wires that channel into
`wayland.Callbacks`, so the channel — not the return value — is what actually drives a frame. The return
value exists for the tests and for any future transport.

Milestone 2's version of this send is non-blocking with a `default:` drop on a capacity-8 channel, so a
ninth changed bar loses its redraw. Tranche 3A does not inherit that. The send **blocks** instead: the
owner's bridge goroutine drains the channel continuously into an unbounded queue, so blocking is bounded,
and `Registry.Close` closes a `closed` channel that releases any pending send at shutdown. A dropped
invalidation is a bar that never repaints, which is precisely the defect this tranche must not ship.

The integration checkpoint tests this at the transport, not just at the return value: more bars change at
once than the channel can buffer, a drainer runs concurrently, and every changed global must arrive.

## Files

New:

- `internal/services/clock.go`, `internal/services/clock_test.go`
- `internal/shell/widget.go`, `internal/shell/widget_test.go`
- `internal/platform/niri/testdata/*.jsonl` — synthetic fixtures using connectors `DP-9` and `HDMI-A-9`
  and fixture titles. No real connector names, window titles or machine state enter Git.

Changed:

- `internal/platform/niri/events.go` — `Window`, `Workspace.ActiveWindowID`, the four new events,
  unchanged-state suppression
- `internal/platform/niri/client_test.go` — fixture-driven cases
- `internal/config/config.go` — `Item`, vocabulary, defaults
- `internal/config/load.go` — union decoding, per-item validation, boundary derivation
- `internal/shell/registry.go` — global keys, lease ownership, `UpdateClock`, dirty set, `Close`
- `internal/shell/proof.go` → `internal/shell/bar.go`, `proof_test.go` → `bar_test.go` — `Proof` becomes
  `Bar`; fixture widgets removed
- `internal/ui/tree.go`, `internal/ui/layout.go` — `MaxWidth`
- `internal/platform/wayland/client.go` — `PreparedConfig.Rollback`
- `cmd/sysc-shell/main.go` — clock pump goroutine, `Registry.Close`

### One judgment call

The `meter`, `toggle` and `shell-name` fixture widgets are removed, but `Handle`'s press/release matching
and hit testing are **kept**. That logic was corrected in `725a390`, is covered by tests, and Milestone 4
needs it. With no node carrying an `Action` it is inert at runtime while remaining under test through a
synthetic action node. Deleting and later restoring it would be more total churn than leaving it, and the
tranche stays read-only either way.

## Automated evidence

Every behavior the handover requires maps to a runnable check.

| Required behavior | Check |
|---|---|
| Two bars consume one clock service and one update | Two leases yield one goroutine; one `UpdateClock` changes both bars' text |
| Removing one bar retains the service | `DropHost(g1)` leaves `Running()` true |
| Removing the last consumer stops the goroutine | `DropHost(g2)` leaves `Running()` false; goroutine count returns to baseline |
| A rejected reload alters nothing | A failing `PrepareConfig` leaves lease count, bar set and visible text unchanged |
| An accepted reload does not restart a live service | `Starts()` is still 1 after reload, including a boundary change |
| Initial window state yields the right title per output | Fixture `WindowsChanged` plus `WorkspacesChanged` projected across two connectors |
| Focus, title, workspace, close and move invalidate only affected bars | `UpdateNiri` returns exactly the expected globals and no others |
| A dangling active window id is tolerated | `WindowClosed` before `WorkspaceActiveWindowChanged` projects to an empty title |
| Two globals sharing a connector keep distinct instances | Registry holds two bars, two lease sets; dropping one keeps the other |
| Unavailable Niri state renders a stable fallback | Empty snapshot projects to `"-"` and empty title, bar still renders |
| Long titles truncate without exceeding bounds | A long multi-script title with `MaxWidth` set stays within its cap and within the section |
| No source change produces no redraw | Two `UpdateClock` calls inside one boundary mark nothing dirty on the second |
| Cancellation stops every goroutine, no data race | `go test -race`, goroutine count before and after |

At each integration checkpoint:

```bash
go mod tidy -diff
go test -race -count=1 ./...
go vet ./...
go build -o /tmp/sysc-shell-milestone3 ./cmd/sysc-shell
gofmt -l .
git diff --check
```

These do not demonstrate live behavior and no live claim is made from them.

## Live gate

Run only after Milestone 2 passes its own matrix. Reusable commands go in `tests/integration/README.md`;
connector names, titles and measurements stay out of Git.

- one output, then at least two;
- one clock snapshot rendered on every configured bar;
- independent workspace and title text per output;
- focus moved between two windows in the same workspace updates the title — the underlying event was
  verified live during the audit, so this item confirms the rendered result rather than the event;
- focus and title changes without restarting the shell;
- output reconnect with no duplicate or missing widget instance;
- reload adding and removing clock and Niri widgets;
- Niri socket loss and shell shutdown;
- suspend and resume timing against the clock's long-lived timer.

Baselines to record before any budget is set: idle CPU and wakeups, CPU during clock and Niri updates,
RSS, submitted and skipped frame counts, layout and paint duration, allocations per update, binary size.

## Dependencies and assumptions

1. **Milestone 2 delivers global-keyed host identity.** Tranche 3A assumes `NewHost` and `PreparedConfig`
   are keyed by `wl_registry` global with the connector carried alongside, and that `Invalidation` names a
   global. The current branch keys all of these by connector. Per the owner's decision this correction
   belongs to Milestone 2; the contract is named here so a mismatch is a one-boundary fix.
2. **Milestone 2 wires `render.FontMap` into the bar.** `NewSystemFontMap` and `SplitRuns` exist and are
   correct but have no caller; `internal/shell` still parses embedded `goregular`. Window titles are the
   first unbounded user text and need per-rune fallback and cluster truncation. Tranche 3A adds only the
   bound test on top.
3. **Milestone 2's invalidation transport keeps the shape `chan wayland.Invalidation`.** Tranche 3A no
   longer depends on Milestone 2 fixing the lossy drop — it publishes with a blocking send of its own and
   tests delivery at the transport. Only the channel's element type is assumed; if the merged correction
   changed it, `Registry.publish` and one wiring line in `main.go` adapt.
4. **Niri emits `WorkspaceActiveWindowChanged` when focus moves between windows within one workspace.**
   **Verified live on 2026-08-30** by the audit, on niri `26.04 (8ed0da4)`: two windows were spawned on
   one workspace and focus alternated between them with `niri msg action focus-window`. Every move emitted
   exactly one `WorkspaceActiveWindowChanged{"workspace_id":3,"active_window_id":N}`, each followed by a
   `WindowFocusChanged`. The projection this design uses is therefore correct, and the contingency of
   consuming `WindowFocusChanged` as a second trigger is not needed. This was the design's most
   consequential unverified claim; it now holds.
5. **No `sysc-metrics` import, tag, or module replacement.** That is Tranche 3B.

## Design audit outcome

A design audit was run against this document and the plan on 2026-08-30. Result: 0 critical, 1 major,
3 minor. Its findings are recorded here with what was done about each.

**Major — figure rendering.** Nothing requested tabular figures, so a proportional face gives digits
different advances and the centre clock shifts by a pixel or two every minute, because `ArrangeBar` pins
the centre from the section's own width. **Accepted.** `ui.Node` gains a `Tabular` flag and
`ui.MeasureText` widens to `func(text string, tabular bool)`, so the flag reaches
`shaping.Input.FontFeatures` as `tnum`. Only the clock sets it. This is the one change that grew scope
after the audit and it is isolated in a single late task the owner may cut.

**Minor — token discipline (`defaultTitleMaxWidth = 260` lives in `internal/config`).** *Not adopted, with
reason.* The recommendation was to lift the default into the theme token set. But `config.Default()`
already owns every other geometry default — height 48, gap 4, padding 8, spacing 6, radius 12, font size
14 — and `Theme` is *derived* from them through `ThemeFrom`. Moving 260 alone would make it the only
geometry default that does not live beside the others, and `internal/config` cannot import
`internal/shell` to reach the token set. The rule the finding cites — no component carries an independent
pixel constant — is satisfied: 260 is a named configuration default a component reads, not a literal
buried in a component.

**Minor — palette provenance (`accent #0080ff`).** Noted, not actionable here. The accent is an
owner-supplied baseline fixed by the charter, and no Tranche 3A widget paints in accent or error, so
nothing in this tranche depends on it. It reopens when a widget first uses colour.

**Minor — muted token contrast (1.47:1).** Noted. `muted #303438` is sound as a decorative track fill and
would fail as text at size 14. Tranche 3A removes the meter, so the token has no consumer at all in this
tranche. Recording the constraint beside the token belongs with whoever next touches `internal/shell/theme.go`.

Measured contrast from the audit, for the record: foreground/background 15.6:1, accent/background 4.87:1,
error/background 5.34:1, muted/background 1.47:1.

## Stop conditions

The handover lists designs that must be returned for review. None applies:

- no second goroutine calls Wayland; the clock publishes to a channel the existing wake pipe bridges;
- widget instances are keyed by Wayland global, never by connector;
- there is one Niri connection and one clock timer for the process, not one per output;
- no widget schema, renderer interface, plugin protocol or injection container is introduced;
- no `sysc-metrics` code is imported and no local replacement is added;
- no runtime SVG decoder or asset pipeline appears; this tranche ships no icons;
- no panel, keyboard focus, workspace command, weather, metric, graph or animation is added;
- a failed reload preserves the last accepted widget and service set through `Rollback`;
- every new goroutine stops under cancellation;
- no new dependency is added; the standard library and pinned modules cover everything.
