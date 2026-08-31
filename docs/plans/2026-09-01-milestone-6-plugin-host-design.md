# Milestone 6 External Widget and Plugin Host Design

## Goal

Run trusted external plugins as supervised processes and render their bar widgets, tooltips, settings,
and standard panels with shell-owned native UI. Ship five useful reference plugins: Timer, World Clock,
Notes, Weather, and Screen Recorder.

Milestone 6 ends when the protocol has a pinned version-one contract and the five reference plugins pass
their automated and live gates. The shell does not claim compatibility with Noctalia, DMS, QML, Lua, or
Luau.

## Inputs

This design follows:

- `docs/roadmap.md`, Milestone 6;
- `docs/plans/2026-08-26-sysc-shell-design.md`;
- the retained UI, bar, panel, settings, service, invalidation, and Wayland code on `main`;
- Noctalia v5's official Timer, World Clock, Notes, Screen Recorder, and Example plugins;
- the installed Noctalia Weather Indicator;
- DMS's plugin manager, variants example, manifests, and settings components.

The references supply behavior and information architecture. They do not supply runtime or wire
compatibility.

## Entrance gates

Implementation starts after the Milestone 5 exit gate. The shell must also have these concrete
primitives:

- panels paint their retained node tree, including the kinds used by built-in panels;
- `ui.KindImage` and the bounded image cache exist;
- one process-wide interactive-root chain owns panels and their attached children;
- the plugin host can submit invalidations without extending the current unbounded append path.

The corresponding beads carry status. M6 does not duplicate those fixes. The former dependency-level
surface crash is not an M6 gate unless a fresh live reproduction establishes one.

## Decisions

### D1. One process per enabled plugin

The shell starts one executable for each enabled plugin. The process owns its service logic and supplies
all declared views. Timer, World Clock, and Screen Recorder therefore share state without a shell-owned
cross-process broker.

The shell owns discovery, lifecycle, settings, persistent state, host calls, view instances, layout,
input, surfaces, and pixels. Disabling a plugin closes its views and stops its process. Settings and
persistent state remain on disk.

### D2. Trusted process model

Plugins run as the shell user with the shell's OS privileges. They can access the filesystem, network,
session bus, and subprocesses available to that user.

Capability grants control plugin requests to the shell. They provide negotiation, least-host-API
exposure, and an audit trail. They are not a security boundary. M6 adds no namespaces, seccomp,
Bubblewrap, systemd sandbox, or permission claim that the OS cannot enforce.

### D3. JSON manifests and JSON Lines protocol

The project already uses strict JSON configuration and the Go standard library supplies the required
parser and encoder. Each plugin directory contains `manifest.json`, one executable, and optional assets.
The shell starts the executable without a command shell. Standard input and output carry one JSON value
per line. Standard error carries logs.

The host rejects unknown manifest fields for schema version 1. Protocol messages may add fields within a
negotiated minor version; a v1 reader rejects unknown message types and ignores negotiated optional
fields it does not use.

### D4. The wire tree is not `ui.Node`

`ui.Node` contains arranged bounds, function-valued virtual-list items, focus state, and mutable control
data. None belongs on a wire.

`plugin/v1.Node` contains declarative data only. The host validates and copies each tree, converts it to
shell-owned nodes, performs layout away from the Wayland dispatch goroutine, and publishes an immutable
render result. A plugin cannot supply bounds, callbacks, focus indexes, IME state, or renderer objects.

### D5. Version one grows only from the five reference plugins

M6 supports the smallest vocabulary that preserves the selected plugins' useful behavior:

| Vocabulary | Reference consumer |
|---|---|
| rows, columns, text, icon/image, progress, themed containers | all five |
| buttons, select, toggle, numeric and path settings | Timer, Weather, Recorder |
| scroll and bounded lists | Notes, Weather, World Clock |
| keyed text input and multiline editing | Timer, Notes, World Clock |
| drag source and drop zone | World Clock |
| structured read-only tooltip | Weather and Timer |
| per-output context and pointer-button events | Recorder and bar widgets |
| notifications | Timer and Recorder |

Launcher providers, desktop widgets, control-center entries, wallpaper integration, arbitrary drawing,
custom shaders, and plugin-owned surfaces remain outside M6.

### D6. Reference plugins ship as external Go executables

The repository contains five standalone commands and their manifests. Packaging installs them as local
plugins, disabled by default. They speak the JSON protocol and never import shell internals. A small
public `plugin/v1` Go package holds wire types and framing shared by the host and reference commands.
The JSON fixtures, not the Go helper, define compatibility.

### D7. The local manager manages installed plugins

Settings gains a Plugins section modelled on the useful part of the Noctalia and DMS screens. It shows:

- the user plugin directory and a rescan action;
- one card per valid or rejected local manifest;
- name, version, author, description, capabilities, dependencies, and runtime state;
- enable or disable, retry after failure, recent bounded stderr, and declared settings.

M6 does not browse catalogs, download, update, remove, or execute installer hooks. Users install and
remove directories themselves.

## Files and discovery

The host scans immediate child directories under:

- `$XDG_CONFIG_HOME/sysc-shell/plugins` for user plugins;
- the package's system data directory for the five reference plugins.

It does not recurse. A duplicate plugin ID rejects both candidates and names their paths; user content
does not shadow packaged code. A rescan validates a complete candidate set before replacing discovery
state. Rescan does not restart an unchanged running plugin.

Schema version 1 requires:

```json
{
  "schema": 1,
  "id": "org.sysc.timer",
  "name": "Timer",
  "version": "1.0.0",
  "protocol": {"major": 1, "minor": 0},
  "exec": "bin/sysc-plugin-timer",
  "capabilities": ["notifications", "panels", "settings", "state"],
  "requires": {"commands": []},
  "services": [{"id": "timer"}],
  "widgets": [{"id": "bar", "settings": []}],
  "panels": [{"id": "panel", "width": 320, "height": 280,
               "placement": "attached"}],
  "settings": []
}
```

The validator constrains IDs, lengths, entry counts, relative executable and asset paths, panel sizes,
setting schemas, dependency names, and capability names. It resolves no shell text. `exec` names a
regular executable inside the plugin directory. The `requires.commands` check uses `exec.LookPath` and
keeps a missing-dependency plugin visible but stopped.

The main shell configuration stores enabled plugin IDs and bar placements. A placement has a plugin ID,
widget entry ID, and stable instance ID. The instance ID namespaces placement settings and lets two
copies of one widget retain different values.

## Handshake and lifecycle

The shell sends `host.hello` after `cmd.Start`. It contains supported protocol versions, the manifest
identity, granted capabilities, and fixed limits. The plugin returns `plugin.hello` with the selected
major and minor version, matching identity, and accepted capabilities. The process must complete the
handshake within two seconds and may not send view updates first.

The manifest declares requested capabilities. The host grants the intersection of requested and
supported capabilities. Version 1 host calls cover:

- read settings and receive setting changes;
- read and write namespaced persistent state;
- open or close a declared panel;
- send a notification through the M5 notification client;
- read the output context supplied with a view or input event.

The supervisor uses `os/exec`, context cancellation, pipes, and timers. It sends `host.shutdown`, closes
standard input, waits one second, then kills a process that did not exit. It keeps the last 64 KiB of
standard error and a structured exit reason.

An enabled plugin receives at most three automatic starts in a rolling 60-second failure window. A
process that runs for five minutes clears the failure window. Exhaustion marks the plugin failed until
the user retries, disables it, or a changed manifest causes a new validated generation.

The plugin owns subprocess behavior. Screen Recorder must stop a capture it owns during an orderly
disable. After a plugin or shell crash it may identify and adopt an existing recorder process by exact
executable and arguments. It must not use broad process-name kills.

## Protocol messages

Every message has `type`. Request and response messages also have an opaque `id`. View messages carry a
host-issued `view_id`, plugin entry ID, stable instance ID, output connector, and revision where
applicable.

Host to plugin:

- `host.hello`, `host.shutdown`;
- `view.open`, `view.close`, `view.resync`;
- `input.event` for activation, pointer button, value change, text change or submit, scroll, and drop;
- `settings.changed`;
- `host.reply`.

Plugin to host:

- `plugin.hello`;
- `view.snapshot` with one complete root;
- `view.patch` with independent keyed subtree replacements;
- `host.call`;
- `plugin.status` for a user-facing service state.

Snapshots replace one view at a new revision. A patch names its base revision and supplies keyed subtree
replacements. The host applies the patch only when the base matches. A dropped or stale patch triggers
one `view.resync`; the plugin responds with a snapshot. This rule lets the host drop update traffic
without retaining an unbounded patch chain.

The protocol carries no arbitrary callback names. Interactive nodes have stable node IDs and declare
which event kinds they emit. The host returns those IDs in `input.event`. Node IDs remain unique within a
view revision.

## View and control rules

Version 1 supports:

- layout: row, column, scroll, fixed-height virtual list;
- content: text, icon, image, separator, progress;
- controls: button, toggle, slider, select, text input, multiline input;
- drag and drop: drag source and drop zone.

Layout properties cover bounded width and height, grow, gap, padding, alignment, justification, radius,
fill, and border. Text properties cover size tiers, weight, tabular figures, wrapping, truncation, and
semantic theme roles. Colors use named theme roles with optional alpha or a validated literal color for
declared color settings.

The host requires accessible names and roles on interactive nodes. It derives focus order, hit testing,
IME state, hover, pressed state, and focus rings. Tooltips accept read-only content. Bar views accept
pointer controls but reject keyboard fields, scrolling, and drag targets.

Text inputs use stable keyed identity. The host owns the live buffer and preedit state. A snapshot with
the same key does not overwrite that buffer. The plugin changes the key or sends an explicit reseed
revision when an external file change must replace it. Text-change events use newest-wins coalescing;
submit and focus events remain ordered.

Each output receives a separate bar view for each configured plugin placement. The plugin process and
persistent state remain shared. A panel opens on the output that generated its trigger and joins the
process-wide interactive-root chain. Hot-unplug closes affected views and sends `view.close` without
stopping the plugin.

## Settings and state

The shell generates settings controls from manifest declarations. Version 1 supports boolean, integer,
float, string, select, color, file, and folder values, plus numeric bounds and `visible_when`. Settings
may have plugin scope or widget-instance scope.

The host validates a complete settings candidate before an atomic write. It stores plugin configuration
under the shell's existing JSON configuration and sends the committed values to the process. A plugin
cannot declare an arbitrary settings UI.

Persistent state is a namespaced JSON key/value store under
`$XDG_STATE_HOME/sysc-shell/plugins/<plugin-id>/`. The host writes with a temporary file, sync, and
rename. State calls have request IDs and deadlines. The shell caps key count, value size, and total file
size. Plugins keep transient high-rate state in their own process.

## Bounds and scheduling

The host applies these version-one ceilings before a plugin update reaches shell presentation:

| Resource | Limit |
|---|---:|
| discovered plugin directories | 128 |
| manifest | 256 KiB |
| JSON Lines message | 1 MiB |
| live views per plugin | 64 |
| nodes per view | 1,024 |
| tree depth | 16 |
| children per node | 256 |
| text in one node | 64 KiB |
| pending control messages per plugin | 32 |
| pending view update | one overwrite slot per view |
| stderr retained per process | 64 KiB |
| persistent state | 256 KiB per value, 4 MiB per plugin |

The stdout reader checks framing and a token bucket before JSON decoding. A plugin may burst to 120 view
updates in one second and sustain 60 per second. The host commits at most 30 frames per second for one
view and coalesces intermediate updates. Repeated rate violations mark the plugin degraded and stop it
after the host captures the reason.

A fixed worker pool validates, converts, and lays out plugin views. Each view keeps one pending job, and
new work replaces old work. A layout that exceeds 8 ms records an overrun; three overruns within ten
seconds mark that plugin degraded and suppress further view work until a clean snapshot or restart.
Node and depth limits keep a single layout finite. The Wayland dispatch goroutine receives only prepared
render state and bounded invalidations.

## Failure presentation

The manager distinguishes disabled, starting, running, degraded, failed, incompatible, and missing
dependency. It shows the last failure and bounded stderr.

A failed bar view becomes a fixed-width error placeholder with an accessible plugin name and status. An
open panel becomes shell-owned error content with Close, Retry, and Disable actions. Healthy plugins and
built-in widgets keep their views. A malformed view removes only that view; protocol corruption,
framing failure, flooding, or process exit fails the plugin generation.

## Reference plugin requirements

### Timer

Timer supplies one shared countdown service, a bar widget, tooltip, and attached panel. The panel accepts
a typed duration and offers start, pause, and reset. The bar and panel remain synchronized, update once a
second while active, and send a completion notification.

### World Clock

World Clock stores an ordered IANA timezone list. Its panel adds, validates, confirms removal, scrolls,
and reorders zones by drag and drop. Rows update once a second and show zone name, local time, and UTC
offset. The bar opens the attached panel.

### Notes

Notes supplies a bar widget and full-height panel. It lists files from a configured directory, creates,
renames, pins, confirms deletion, edits multiline text, autosaves, reports dirty or saved state, and
reconciles external file changes without overwriting an active unsaved buffer.

### Weather

Weather fetches Open-Meteo data with `net/http`. It supplies current conditions in the bar, a structured
tooltip, settings for location, units and presentation, and a seven-day forecast panel. It distinguishes
loading, fresh, stale, disabled, and failed data. It reuses the shell's icon/image path and never blocks a
view on network I/O.

### Screen Recorder

Screen Recorder supervises `gpu-screen-recorder`, checks the dependency before start, and exposes record,
stop, replay-buffer, and save-replay actions from the bar. It passes the focused output context, exposes
the Noctalia reference settings that apply to the local backend, reports status and logs, and sends save
or failure notifications. Control-center integration remains M7.

The trusted laptop at `nomadx@192.168.0.64:7777` has Niri and `/usr/bin/gpu-screen-recorder` for the live
gate.

## Tranches

### M6A: host kernel, manager, and Timer

Build discovery, manifest validation, supervision, handshake, settings/state storage, snapshot views,
input events, failure placeholders, and the local manager. Timer proves one shared service feeding a bar,
tooltip, and panel.

### M6B: incremental views and World Clock

Add keyed subtree patches, scroll/list behavior, drag and drop, per-output contexts, update coalescing,
and layout budgets. World Clock proves the path.

### M6C: Notes

Add multiline retained editing and the external-reseed contract. Port the file-backed Notes behavior and
prove autosave and external-change reconciliation.

### M6D: Weather

Add structured tooltips and any icon/image vocabulary not already supplied by M5. Port current
conditions, settings, stale/error behavior, and the seven-day panel.

### M6E: Screen Recorder

Add dependency reporting, focused-output host context, recorder lifecycle recovery, logs, and notification
calls. Port recording and replay-buffer behavior.

### M6F: version-one freeze and qualification

Freeze JSON fixtures, publish the protocol reference, run malformed/oversized/slow/crash/flood/deep-tree
fixtures, and qualify all five plugins on live Niri. No later milestone work enters this tranche.

## Verification

Each tranche leaves one focused runnable gate and runs race-enabled package tests. Pure protocol, tree,
settings, and layout behavior uses table tests. Fake plugin executables cover handshake timeout, wrong
identity, malformed JSON, oversized lines, stale revisions, crash loops, stderr truncation, flooding,
deep trees, blocked writes, and orderly disable.

Compatibility tests load committed JSON fixtures through the same decoder and compare host responses.
They do not rely on Go struct round trips. Two-output integration tests prove separate view IDs and shared
plugin state. Disable tests prove that the process exits and all nodes disappear.

The live gate on the trusted laptop covers manager enable/disable, each reference bar view and panel,
tooltip placement, keyboard and pointer input, Niri output context, plugin crash recovery, and a short
record/save/replay cycle. The handoff records frame rate, layout overruns, restart behavior, and recorder
artifacts.

## Exclusions

M6 does not add a plugin catalog, installer, updater, removal UI, sandbox, arbitrary UI toolkit, launcher
provider, desktop widget, control-center entry, wallpaper hook, QML loader, script runtime, lock screen,
or compositor feature.
