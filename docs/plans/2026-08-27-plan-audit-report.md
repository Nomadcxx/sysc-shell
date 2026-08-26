# sysc-shell Plan-Audit Report

Date: 2026-08-27
Baseline audited: `97b9249` (`docs: define sysc-shell architecture`), plus `e8adce6`.

## Verdict

**Ready after listed fixes.**

The architecture is sound. Niri-first, one Wayland-owning goroutine, `wl_shm` first, out-of-process
plugins, and one repository all survive scrutiny. Every pinned dependency resolves, is permissively
licensed, and exposes the APIs the plan assumes. A live probe built against the exact pins created a
layer surface on this machine's Niri, drove fractional scale, ran an idle-correct poll loop, and tore
down without a protocol error.

Two findings would force rework if implementation started today: an integer `scale` parameter that
cannot represent Niri's fractional scale, and a documented Niri capability that does not exist. Both
are cheap to correct now and expensive to correct after Task 7.

## Verification environment

| Item | Value | How obtained |
|---|---|---|
| Go toolchain | `go1.27.0` | `go version` |
| `go` directive planned | 1.26 | plan Task 1 |
| Niri | `26.04 (8ed0da4)` | `niri msg -j version` |
| wayland / wayland-protocols | 1.26.0-1 / 1.49-1 | `pacman -Q` |
| Outputs | `DP-1` 3440x1440, `DP-3` 2560x1440, both scale 1.0 | `niri msg -j outputs` |
| Other layer clients present | `dms:bar`, `dms:notification-popup`, `quickshell` | `niri msg -j layers` |

All experiments ran under `/tmp/sysc-audit`. No module, dependency file, generated binding, or scratch
artifact was created in this repository.

Claims below are labelled **[V]** verified by execution on this machine, **[I]** inferred from source
or specification text, **[U]** unverified.

## Findings

Severity key: `B` blocker, `M1` fix before Milestone 1, `MS` fix before affected milestone, `D` safe to defer.

| # | Sev | Finding | Evidence | Affected section | Resolution |
|---|---|---|---|---|---|
| 1 | B | `App.Configure(width, height, scale int)` cannot represent fractional scale. The compositor sends a numerator over 120; integer scale collapses 1.25, 1.5 and 1.6 onto 1 or 2. | **[V]** live probe received `preferred_scale=180` at DP-3 scale 1.5. **[I]** `fractional-scale-v1.xml`, `preferred_scale`: "numerator of a fraction with a denominator of 120". | proof Task 7 Step 3 | Signature changed to `Configure(logicalWidth, logicalHeight, scale120 int)`. Applied. |
| 2 | B | The design claims the Niri adapter projects "outputs and focused output" from the event stream. Niri has **no output event**. | **[V]** `OutputsChanged` occurs 0 times in `/usr/bin/niri`; all 15 real event names occur 2+ times. **[V]** a full event-stream connect yields `WorkspacesChanged`, `WindowsChanged`, `KeyboardLayoutsChanged`, `OverviewOpenedOrClosed`, `ConfigLoaded`, `CastsChanged` — no output event. | design "Niri integration"; roadmap M2 | Design corrected: Wayland owns output identity and geometry; Niri owns workspace-to-output association only. Applied. |
| 3 | M1 | Task 8 never reads the reply envelope. Niri answers `"EventStream"` with `{"Ok":"Handled"}` before any event. On failure it answers `{"Err":"..."}`, which the plan's unknown-event rule would silently discard, hanging the client forever. | **[V]** raw socket probe: first line `{"Ok":"Handled"}`; a bogus request returns `{"Err":"error parsing request"}`. | proof Task 8 Steps 1, 3 | Task 8 now requires reading and validating the reply line first. Applied. |
| 4 | M1 | Task 7 sets a viewport **source** rectangle. It is neither required nor safe: a source rect in the wrong units is a `wp_viewport` protocol error. | **[I]** `fractional-scale-v1.xml` description specifies only destination + buffer scale 1. **[V]** probe set destination only and mapped correctly at 1.0 and 1.5. | proof Task 7 Step 3 | `set_source` removed; destination-only documented. Applied. |
| 5 | M1 | No buffer-size rounding rule. | **[I]** protocol: "the size is rounded halfway away from zero". **[V]** logical 1707 x scale120 180 -> 2560.5 -> buffer 2561. | proof Task 7 | Rule stated as `(logical*scale120 + 60) / 120`. Applied. |
| 6 | M1 | Plan says "damage" without naming the request. Under viewporter, surface units and buffer pixels differ. | **[I]** `wl_surface.damage` doc: "New clients should not use this request. Instead damage can be posted with `wl_surface.damage_buffer`". **[V]** probe used `damage_buffer` successfully. | proof Task 7; design "Rendering" | `damage_buffer` in buffer pixels specified. Applied. |
| 7 | M1 | "Cap each bound version to the generated client's supported version" is not implementable — `dankgo` exports no per-interface version constant. | **[V]** grep of `wayland/client/client.go`: the only `Version` symbol is the `RegistryGlobalEvent.Version` field. | proof Task 7 Step 3 | An explicit client-side version table is now required and its values are named. Applied. |
| 8 | M1 | A `wl_display.error` with no registered handler is **silently discarded**, so a fatal protocol error looks like an idle connection. | **[V]** `dankgo/wayland/client/client.go:211-215` — `case 0: if i.errorHandler == nil { return }`. | proof Task 7 Step 3 | `SetErrorHandler` is now mandatory, with a sticky fatal field. Applied. |
| 9 | M1 | Poll-loop mechanics unspecified. `dankgo` has no `prepare_read`/`read_events` split; `Dispatch()` reads exactly one message and blocks when none is pending. | **[V]** `context.go:91-97` (`GetDispatch` calls `ReadMsg` before returning the closure); `event.go:15-81` reads header then body directly from the socket with no user-space buffer. **[V]** probe: 12 dispatches for 12 messages. | proof Task 7 Step 3 | Drain rule specified: one dispatch per readiness, re-poll with timeout 0. Applied. |
| 10 | M1 | A two-slot scheduler must not infer buffer availability from frame completion. | **[V]** live: at `frame done #1` slot 0 was still busy; `wl_buffer.release` for slot 0 arrived only *after* slot 1 was attached and committed. Sequence repeated for 4 frames. | proof Task 5; design "Rendering" | Task 5 now requires an explicit release-before-frame ordering case. Applied. |
| 11 | M1 | Layer-surface configure width is **not** the output width. | **[V]** DP-1 logical width 3440, configure width **3396** — 44 logical px held by `dms:bar`'s exclusive zone. **[V]** at scale 1.5, Niri IPC reports DP-3 logical width 1706 while configure gives **1707**. | proof Tasks 7, 10; design "Output and surface model" | Explicit prohibition on deriving surface size from `wl_output` mode or Niri IPC. Applied. |
| 12 | M1 | `--output DP-1` selection contract was unstated. | **[V]** Niri advertises `wl_output` **version 4**; the probe read `DP-1`/`DP-3` from `wl_output.name`. `dankgo` generates `OutputNameEvent` (`client.go:6850`). | proof Task 7; design "Wayland platform" | `wl_output.name` at v4 named as the contract; `zxdg_output_manager_v1` explicitly not required. Applied. |
| 13 | M1 | Task 3's shaping assertions pass even if shaping degenerates to a plain `cmap` lookup with no joining. | **[V]** for `مرحبا` at the pin, nominal GIDs `[421 401 397 392 391]` vs shaped `[2027 2137 2225 3111 3097]` — 5/5 differ. | proof Task 3 Step 1 | Assertion replaced with "no shaped GID equals its nominal GID". Applied. |
| 14 | M1 | Task 3 leaves the joined-script fixture unresolved; `goregular` has no Arabic coverage. | **[V]** `Amiri-Regular.ttf` (553 KB, SIL OFL 1.1) and `OFL.txt` ship inside the pinned `go-text/typesetting` module at `font/testdata/`, per its `readme.md`. | proof Task 3 Step 1 | Named as the fixture, with the OFL notice requirement. Applied. |
| 15 | M1 | `wl_shm` format must be selected from the advertised list, not assumed. | **[V]** Niri advertises `[0x34324241, 1, 0x34324258, 0]`; ARGB8888 (`0`) is present but **last**, and the events need a roundtrip before pool creation. | proof Task 7 | Format scan plus named error specified. Applied. |
| 16 | M1 | Task 6's generator invocation writes six extra modules into `go.sum` during `go generate`. | **[V]** `go.sum` grew from 4 to 10 modules (`x/crypto`, `x/mod`, `x/sync`, `x/tools`, `gofumpt`, `strcase`) after one un-versioned `go run`. | proof Task 6 Step 2 | Version-suffixed invocation specified. Applied. |
| 17 | M1 | Generator output depends on the main module's `go` directive. | **[V]** `cmd/go-wayland-scanner/scanner.go:241` runs `go list -m -f {{.GoVersion}}` and feeds it to `gofumpt` as `LangVersion`. | proof Task 6 | Recorded as a reproducibility condition. Applied. |
| 18 | M1 | `preferred_scale` can change with no following configure, and the host state machine has no edge for it. | **[I]** `wp_fractional_scale_v1` is independent of layer-shell configure; a scale change at unchanged logical size emits only `preferred_scale`. | design "Output and surface model" | Scale-only reconfigure edge added. Applied. |
| 19 | M1 | Task 10 Step 3 gives no commands and no restore step for the non-1 scale gate. | **[V]** `niri msg output <name> scale <s>` is documented "changed temporarily and not saved into the config file"; capture/change/restore executed and verified on DP-3 during this audit. | proof Task 10 Step 3 | Full procedure with capture and restore. Applied. |
| 20 | MS | `Event{X, Y int}` discards sub-logical-pixel pointer precision and carries no input serial. | **[V]** `dankgo` delivers `SurfaceX/SurfaceY float64` and `PointerButtonEvent.Serial`. | proof Task 7 | Left as-is for the proof; serial required from Milestone 4. Recorded, not applied. |
| 21 | M1 | `docs/prior-art.md` presents `sysc-lock` as evidence for the Go layer-shell path. It is a CGO client. | **[V]** `~/Documents/sysc-lock/internal/wayland/wlr_layer_shell.go` opens with `#cgo pkg-config: wayland-client` and `#include "wlr-layer-shell-unstable-v1-protocol.c"`. | prior-art "sysc-lock" | Corrected. Applied. |
| 22 | MS | Duplicate-host risk on registry churn is unaddressed. | **[I]** connector names are reused across hotplug; `wl_registry` global names are not. | roadmap M2 | Invariant recorded in the roadmap. Applied. |
| 23 | MS | No image decoder covers SVG, and icon themes are largely SVG. | **[V]** `golang.org/x/image@v0.44.0` ships `bmp`, `tiff`, `webp`, `vector`, `draw` — no SVG; stdlib covers PNG/JPEG/GIF only. | roadmap M3, M6 | Recorded as an owner decision (D3). |
| 24 | MS | The plugin capability model's threat model is unstated, so "capabilities" could be read as OS-level isolation. | **[I]** design "Plugin model" lists only host-call restrictions; no namespace, seccomp, or cgroup mechanism appears anywhere. | design "Plugin model"; roadmap M5 | Scope of the guarantee stated explicitly. Applied. |
| 25 | MS | Nothing bounds a well-formed plugin's update rate or tree size after size validation passes. | **[I]** design "Plugin model" bounds message size and restart count only. | roadmap M5 | Design gate added. Applied. |
| 26 | D | `dankgo`'s `ReadMsg` treats a short read as fatal rather than resuming. | **[I]** `event.go:25-27, 65-67` return an error when `n != 8` or `n != msgSize`; `SOCK_STREAM` permits short reads. Never observed in this audit's runs. | dependency risk | Recorded as an upstream watch item. |
| 27 | D | `Context.Fd()` returns a descriptor used outside `RawConn.Control`. | **[I]** `context.go:61-71`; `syscall.RawConn` documents the descriptor as valid only during the callback. | proof Task 7 | Capture-once rule noted in the plan. Applied. |
| 28 | D | `GetDispatch`'s doc comment claims multi-goroutine safety. Two concurrent callers would interleave header and body reads. | **[I]** `context.go:88-90` vs `event.go:15-81` (two sequential socket reads per message, no lock). | dependency risk | Recorded; the single-owner rule already prevents it. |

## Answers

Ranked; several questions share one piece of evidence and are answered together.

**1. Connector-to-`wl_output` mapping — `fix before Milestone 1`.**
Bind `wl_output` at version 4 and match the `wl_output.name` event against `--output`. Niri 26.04
advertises `wl_output` v4 **[V]**, `dankgo` generates `OutputNameEvent` **[V]**, and the probe read
`DP-1` and `DP-3` this way **[V]**. `zxdg_output_manager_v1` is present (v3) but adds a second object
per output and a second event race for information v4 already carries; do not bind it. Correlating
through Niri IPC is also wrong — see finding 11.

**2, 3, 14. Fractional scale, rounding, and the render transform — `blocker` (2), `fix before Milestone 1` (3, 14).**
Integer scale cannot carry 120ths; the parameter becomes `scale120 int`. The exact contract is:
`wl_surface.set_buffer_scale` stays at its default 1; `wp_viewport.set_destination(logicalW, logicalH)`;
buffer size `= (logical * scale120 + 60) / 120` (round half away from zero); no `set_source`.
Layout and hit testing stay in logical units; the painter takes the physical buffer plus `scale120` and
scales at paint time. No explicit renderer transform object is needed for the proof's row of
axis-aligned rectangles and glyph masks — the text rasteriser already takes a pixel size, so shaping at
`16 * scale120 / 120` px yields correctly hinted glyphs rather than an upscaled 16 px mask.

**4. `dankgo` poll-loop primitives — `fix before Milestone 1`.**
Sufficient, but not shaped like libwayland. There is **no** `prepare_read`/`read_events` split and
**no flush**, because `WriteMsg` writes straight to the socket **[V]** and `ReadMsg` reads straight from
it with no user-space buffer **[V]**. That absence is what makes a plain `poll()` correct: no bytes can
hide in a client buffer. The loop is: `unix.Poll` on `Context.Fd()` plus a wake `eventfd`; on `POLLIN`
call `Dispatch()` exactly once; re-poll with timeout 0 to drain. Cancellation and wakeup come from the
eventfd, written by other goroutines that never touch a proxy. Fatal socket errors surface as a
`Dispatch()` error; fatal *protocol* errors do not — they arrive as `wl_display.error` and are dropped
unless a handler is set (finding 8). One blocking hazard remains: a large burst of requests could block
in `WriteMsgUnix` while the client is not reading. A bar's request volume makes this remote; recorded,
not designed around.

**5. Required globals and degradation — `fix before Milestone 1`.**
Required: `wl_compositor`, `wl_shm`, `wl_seat`, `wl_output`, `zwlr_layer_shell_v1`. Client-side caps and
the versions Niri 26.04 actually offers **[V]**: compositor 6/6, shm 1/2, seat 7/9, output 4/4,
layer-shell 4/5, fractional-scale 1/1, viewporter 1/1. `wp_fractional_scale_manager_v1` and
`wp_viewporter` should **fail the proof with a named error** when absent. They exist on the target
compositor, the proof's entire purpose is to qualify the fractional-scale path, and an integer
`wl_output.scale` fallback would silently substitute a different code path for the one under test. The
*shell* may add that fallback later; the *proof* must not.

**6. Pool and mapping lifetime — `fix before Milestone 1`.**
A `wl_shm_pool` may be destroyed while its buffers live; buffers stay valid **[I]**, and the probe
destroyed a pool with a busy buffer with no protocol error **[V]**. What must not happen is *writing*
into or `munmap`-ing storage the compositor still reads. Ownership rule: a buffer generation owns its
memfd, mapping, pool, and buffers together. On reconfigure, allocate a **new** generation, attach from
it, and retire the old one only after every one of its buffers has emitted `release` — or after the
surface is destroyed, whichever comes first. On shutdown, destroy buffers, destroy the pool, then
`munmap`, then roundtrip before closing the display, so the compositor has finished with the storage.

**7. Scheduler correctness — `fix before Milestone 1`.**
The two-slot design is right and its stated test list is not. Live ordering **[V]**: `frame done` fires
while the previous buffer is still busy, and `release` arrives only after the next attach and commit.
So frame-done must never imply a free slot, and Task 5's fifth step ("receive frame done and buffer
release") must be split into two cases with both orderings, plus: two invalidations during one pending
frame coalescing to one redraw; a configure arriving while a frame is pending (drop the pending render,
re-render at the new size); and close during a pending frame (stop, do not render).

**8. `wl_shm` format — `fix before Milestone 1`.**
`ARGB8888` is **not** guaranteed by the protocol; only `ARGB8888` and `XRGB8888` are mandatory in the
`wl_shm` spec, but a correct client still selects from the advertised list. Niri advertises
`[0x34324241, 1, 0x34324258, 0]` **[V]** — ARGB8888 (`0`) is present and last. The wire format is
host-endian, and `dankgo` decodes with a host-endian `unsafe` cast **[V]**, so on this little-endian
target the in-memory byte order is B, G, R, A, matching the plan's Task 4 canvas. Collect formats
before creating the pool and fail with a named error if ARGB8888 is missing.

**9. Pointer path — `fix before Milestone 1` (proof), `fix before affected milestone` (serial).**
Create the pointer on the `wl_seat.capabilities` event when the pointer bit is set, and call
`Pointer.Release()` when it clears; capabilities can change at runtime. `dankgo` already converts
`wl_fixed` to `float64` **[V]**, so no fixed-point handling is needed — but the coordinates are in the
viewport **destination** space, i.e. logical units, which is what hit testing wants. Track focus with
enter/leave and ignore motion while not entered. Act on button **press** and record the pressed node,
then treat **release** inside the same node as the click, so a press-drag-out does not fire. `Event`
should gain the input serial before Milestone 4; the proof does not need it.

**10, 11. Niri initial state and fixture accuracy — `safe to defer` (10), `fix before Milestone 1` (11).**
The event stream **does** deliver complete initial state, so no initial query and no reconciliation
sequence is needed **[V]**: the reply line is followed immediately by a full `WorkspacesChanged`
carrying every workspace on every output. That removes the race the question anticipates. What the plan
does need (finding 3) is to read the `{"Ok":"Handled"}` reply before entering the event loop.
On field shapes **[V]**: `{"id":5,"idx":1,"name":null,"output":"DP-3","is_urgent":false,"is_active":true,"is_focused":false,"active_window_id":80}`.
`name` and `output` are nullable and decode to `""`. `id` is a `u64` narrowed to `int64` — safe in
practice, worth a comment. The plan's `Snapshot.FocusedOutput` has no direct source; derive it from the
workspace whose `is_focused` is true. Fields may be **added** without breaking `encoding/json`;
`is_urgent` and `active_window_id` are recent examples the plan's struct correctly ignores. A **removed**
or **retyped** field breaks decoding, so keep every field optional and never fail a snapshot on one bad
workspace.

**12, 13. Text stack qualification — `fix before Milestone 1`.**
`go-text/typesetting` at `ddb7ff96ad4d` exposes exactly what Task 3 assumes, verified by execution
**[V]**: `font.ParseTTF`, `shaping.HarfbuzzShaper.Shape(Input) Output` with explicit `Direction`,
`Script`, `Language`, `Face` and 26.6 `Size`, `Face.GlyphData` returning `font.GlyphOutline`, and
segments that map onto `vector.Rasterizer`'s MoveTo/LineTo/QuadTo/CubeTo. A run produced 10 Latin glyphs
with 404 non-zero alpha pixels and 13 Arabic glyphs with 300, zero `notdef`, identical across two calls.
The CI fixture is `Amiri-Regular.ttf`, which ships inside the pinned module under OFL 1.1 with its
licence file **[V]**; vendor both. The proof does **not** need bidi paragraph segmentation. One
explicitly directed RTL run qualifies shaping, joining, and rasterisation without claiming general bidi
support — and the honest framing matters, because `typesetting/bidi` and `segmenter` exist and will be
needed once real user text (window titles) reaches the bar. Record that as a Milestone 3 gate, not a
proof deliverable.

**15. Is `App` a real boundary? — `fix before Milestone 1`, owner decision D1.**
Not as written. It exists for one production implementation and one test fake, and `Invalidations()
<-chan struct{}` on an interface makes lifetime ownership ambiguous — nothing says who closes it. The
lifecycle assertions Task 7 wants (bind order, configure-before-buffer, teardown order) are properties
of pure state machines, not of the app callback, so they need no fake at all. Recommendation: a concrete
struct with function fields, owned by the caller of `Run`. Recorded as D1 because it is a taste call
with real maintenance cost either way.

**16. Pointer-only interaction — `safe to defer`; accessibility gate — `fix before affected milestone`.**
Pointer-only is acceptable for a non-production probe: `keyboard_interactivity=none` is also the correct
production setting for a bar, so the proof is not deferring work it will need at Milestone 2.
Accessibility becomes a gate at **Milestone 4**, where panels first request keyboard focus and ship
interactive controls. At that gate: keyboard navigation and visible focus for every shipped control,
escape-to-close with focus restoration, accessible name and role on every interactive node, and
honouring `prefers-reduced-motion` and high-contrast equivalents. AT-SPI (D-Bus) screen-reader export
is a separate, larger decision — see Q26.

**17. Task 7's throwaway app — `fix before Milestone 1`.**
It does not justify code that Task 9 deletes, but the *risk* it covers is real: Task 7 is by far the
largest step, and going straight from unit fakes to the full integrated proof means a live failure has
five candidate causes. Resolution: keep the flat-colour path permanently behind a `--smoke` flag rather
than deleting it. It is a few lines, it stays useful for triaging every future live gate, and it removes
the delete-later work without merging two large tasks.

**18. Live scale test safety — `fix before Milestone 1`.**
Safe. `niri msg output` is documented as changing configuration "temporarily and not saved into the
config file" **[V]**, and the audit executed the full cycle on DP-3 and confirmed restoration **[V]**.
Both of this machine's outputs are at scale 1.0, so **no non-mutating alternative exists here** — the
gate must change a scale. Procedure:

```bash
BEFORE=$(niri msg -j outputs | python3 -c "import json,sys;print(json.load(sys.stdin)['DP-3']['logical']['scale'])")
niri msg output DP-3 scale 1.5
# run the gate
niri msg output DP-3 scale "$BEFORE"      # or: niri msg action load-config-file
niri msg -j outputs | python3 -c "import json,sys;print(json.load(sys.stdin)['DP-3']['logical']['scale'])"
```

Use the **non-focused** output. When another scaled output already exists, select it with `--output` and
skip the mutation entirely.

**19. Which source owns what — `fix before affected milestone`.**
Wayland owns everything about outputs; Niri owns everything about workspaces. There is no overlap to
reconcile, because Niri emits no output events at all (finding 2). Concretely:

| Property | Source | Note |
|---|---|---|
| Output existence, add/remove | `wl_registry.global` / `global_remove` for `wl_output` | the only hotplug signal |
| Connector identity (`DP-1`) | `wl_output.name` (v4) | **[V]** |
| Scale for rendering | `wp_fractional_scale_v1.preferred_scale` | per **surface**, not per output |
| Transform, mode, physical size | `wl_output.geometry` / `mode`, committed on `done` | |
| Usable surface size | `zwlr_layer_surface_v1.configure` | **not** the output size — **[V]** 3440 vs 3396 |
| Workspaces, active workspace per output | Niri `WorkspacesChanged` / `WorkspaceActivated` | keyed by output **name** |
| Focused output | derived from the workspace with `is_focused` | no dedicated event |

The one real ordering hazard: a Niri workspace event may name an output whose `wl_output` has not yet
been announced, or has already been removed. Hold Niri workspace state keyed by output name and join it
to hosts lazily; never create or destroy a host from a Niri event.

**20. Duplicate-host invariant — `fix before affected milestone`.**
Key `OutputHost` by the `wl_registry` global **name** (the `uint32`), never by connector string. Global
names are unique for the connection's lifetime and are never reused, whereas `DP-1` can disappear and
return as a different physical monitor. Creation happens only on `wl_registry.global`; destruction only
on `global_remove` or `zwlr_layer_surface_v1.closed`. Connector name is a lookup attribute for
configuration matching, not an identity.

**21. Font discovery and fallback — `fix before affected milestone`, and smaller than feared.**
`fontscan` is already inside the pinned dependency **[V]** and supplies the whole minimum:
`NewFontMap`, `UseSystemFonts(cacheDir)` (scans system directories and maintains a disk cache),
`SetQuery` for family/aspect matching, and `ResolveFace(rune)` for per-rune fallback. Milestone 2 needs
only: one configured family with a generic fallback, `ResolveFace` for runes the primary face lacks,
run splitting at face boundaries, and a bounded face cache. No font management project.

**22.** Answered with Q16.

**23. Image formats — `fix before affected milestone`, owner decision D3.**
Standard library covers PNG, JPEG and GIF. `golang.org/x/image@v0.44.0` adds WebP, TIFF and BMP **[V]**.
Album art and weather icons are fully covered. **SVG is not covered by either**, and freedesktop icon
themes are predominantly SVG — which reaches tray items, notification app icons, and the launcher. That
is a real gap needing an owner decision, not an implementation detail.

**24, 25. Plugin isolation and resource exhaustion — `fix before affected milestone`.**
As written, the capability model limits **only calls into the host**. Nothing in the design provides
filesystem, network, process or D-Bus isolation, and a plugin is an ordinary child process with the
shell's own privileges. That must be stated before protocol design, because "capabilities" invites the
stronger reading. The honest threat model for version one: plugins are **trusted code the user chose to
install**; capabilities prevent *accidental* over-reach and make intent auditable; they are not a
security boundary against hostile code. Strengthening it later means systemd user scopes, `seccomp`, or
bubblewrap — a separate design with its own milestone.
Separately, message-size validation bounds one message, not a sender. Milestone 5 needs, as gates: a
per-plugin update rate limit with coalescing; a maximum node count and tree depth per view; a bounded
inbound queue that drops to the newest snapshot rather than growing; and a layout/paint time budget that
marks a plugin degraded instead of stalling the shell. Without these, a well-formed plugin sending valid
updates in a tight loop starves every other widget.

**26. Missing protocols and services — `fix before affected milestone`.**
Every Wayland protocol the later roadmap implies is present on Niri 26.04 **[V]**:
`zwlr_data_control_manager_v1` v2 and `ext_data_control_manager_v1` v1 (clipboard history),
`zwp_primary_selection_device_manager_v1`, `zwlr_screencopy_manager_v1` v3, `xdg_activation_v1`,
`ext_foreign_toplevel_list_v1` and `zwlr_foreign_toplevel_manager_v1` v3, `zwp_idle_inhibit_manager_v1`
and `ext_idle_notifier_v1` v2, `ext_workspace_manager_v1`, `zwlr_output_manager_v1` v4,
`wp_cursor_shape_manager_v1` v2. Two of these change earlier decisions and belong in the roadmap now:
`wp_cursor_shape_manager_v1` is how a pointer cursor gets set without shipping cursor bitmaps, and
`zwlr_output_manager_v1` is the protocol path for output configuration if that ever becomes a feature.
What is **absent from the roadmap and not a Wayland protocol at all** is the D-Bus surface: the
notification server (`org.freedesktop.Notifications`), StatusNotifierItem plus `com.canonical.dbusmenu`
for the tray, MPRIS for media, and AT-SPI for screen readers. Each is a specification-sized subsystem —
the tray in particular, because `dbusmenu` is under-specified and application behaviour varies.
Milestone 6 lists them as one-line slices; they need design gates.

**27. gSlapper contract — `fix before affected milestone`.**
Yes, earlier than the wallpaper milestone implies — but not much earlier, and not as a protocol.
gSlapper is an independent process with its own Unix-socket control; the shell needs a supervision and
IPC contract only when it first *starts or queries* gSlapper. Nothing before Milestone 6 does. The real
early cost is smaller: decide **now** that the shell does not own gSlapper's lifecycle by default
(the user's service manager does), so the wallpaper slice is a client of an existing socket rather than
a supervisor. That decision costs nothing today and prevents a process-supervision subsystem from
appearing inside a wallpaper feature.

**28. Copyable behaviour versus adapted source — `safe to defer`.**
Noctalia, DankMaterialShell and dgop are all **MIT** **[V]**; `dankgo` is MIT; `go-text/typesetting` is
Unlicense **or** BSD-3-Clause; the generated bindings derive from a BSD-licensed scanner forked from
`go-wayland`; `Amiri-Regular.ttf` is OFL 1.1. Behaviour, layout, interaction and feature inventory are
not copyrightable and may be copied freely as requirements — that covers the great majority of what the
prior-art assessment proposes to reuse. MIT permits *source* adaptation too, provided the copyright
notice and licence text travel with it, so clean-room reimplementation is not legally required. The
constraint that actually binds is the project's own: the design forbids translating their source. So the
rule is a project rule, not a licence rule, with two carve-outs that do need notices — the vendored
protocol XML (each already carries its own copyright header, which must be preserved) and the vendored
Amiri font (OFL notice and licence file).

## Documentation changes made

| File | Change |
|---|---|
| `docs/plans/2026-08-26-architectural-proof.md` | `Configure` now takes `scale120`; buffer-rounding rule; `damage_buffer`; `set_source` removed; explicit bind-version table; mandatory `wl_display.error` handler; poll/drain rule; buffer-generation ownership; `wl_output.name` selection; shm format scan; Task 5 ordering cases; Task 3 joining assertion and Amiri fixture; Task 8 reply envelope; Task 6 version-suffixed generator; Task 7 `--smoke` flag retained instead of deleted; Task 10 scale procedure with restore. |
| `docs/plans/2026-08-26-sysc-shell-design.md` | Niri no longer credited with output events; output-property source table; scale-only reconfigure edge; fractional-scale contract; plugin isolation scope stated. |
| `docs/roadmap.md` | M2 output-source ownership and host-identity invariant; M4 accessibility gate; M5 resource-exhaustion gates; M6 D-Bus subsystem design gates. |
| `docs/prior-art.md` | `sysc-lock` corrected to CGO; scanner provenance and licence; Amiri fixture and `fontscan` recorded; local repository paths corrected. |
| `README.md` | Audit report linked. |

## Owner decisions

**D1 — the `App` boundary.** *Recommendation: replace the interface with a concrete struct of function
fields.* The interface has one production implementation and one fake, and the fake is unnecessary
because the lifecycle properties under test are pure state machines. Alternatives: (a) keep the
interface as written — lowest churn, keeps an unowned channel in a published contract; (b) keep an
interface but shrink it to `Render` and `Handle`, moving configure and invalidation to concrete types.
Cost of deferring: low. The signature already changes for finding 1, so this is the cheap moment.

**D2 — fractional-scale strictness.** *Recommendation: the proof fails with a named error when
`wp_fractional_scale_manager_v1` or `wp_viewporter` is absent.* The proof exists to qualify that path;
a silent integer fallback would let it pass without testing what it claims. Alternative: degrade to
`wl_output.scale` and report which path ran. Cost of deferring: low now, high once Milestone 2 depends
on the fallback being tested.

**D3 — SVG icons.** *Recommendation: decide before Milestone 3, prefer a pinned pure-Go SVG rasteriser
behind the icon component, cached to alpha masks at the resolved scale.* Neither the standard library
nor `x/image` decodes SVG **[V]**, and freedesktop icon themes are predominantly SVG. Alternatives:
(a) accept PNG-only icon lookup and degrade when a theme ships SVG only — cheapest, visibly worse on
common themes; (b) add a C rasteriser behind the CGO boundary — contradicts the Go-first constraint for
a non-measured reason. This is a real dependency and user-visible quality decision, not a detail.

**D4 — plugin isolation guarantee.** *Recommendation: state in the design that version one's capability
model is an intent and auditing mechanism over trusted, user-installed code, not a security boundary.*
Alternative: commit to OS-level sandboxing, which adds a design and a milestone. The decision must
precede protocol design because it changes what the handshake means.

**D5 — accessibility depth.** *Recommendation: make keyboard, focus, reduced-motion and contrast
acceptance gates at Milestone 4, and treat AT-SPI screen-reader export as a separate later milestone.*
Alternative: commit to AT-SPI at Milestone 4, which pulls a large D-Bus subsystem into the panel work.

## Research not completed

- **Multi-output and hotplug behaviour** — not exercised. This machine has two outputs but the audit did
  not disconnect one, which would have disturbed the operator's session beyond the reversible scale
  change. Findings 22 and Q19/Q20 are therefore **[I]**, from protocol semantics, not observation.
- **`zwlr_layer_shell_v1` version 5 `set_exclusive_edge`** — not exercised; the probe bound v4. Relevant
  only if Milestone 2 anchors a bar to a single edge on a corner-adjacent output.
- **Output transform** — not exercised. No rotated output was available and rotating one is more
  disruptive than a scale change.
- **`dankgo` short-read behaviour (finding 26)** — not reproduced. Triggering it requires filling the
  compositor's send buffer, which needs a deliberately stalled client. The risk is inferred from source.
- **`wl_output.scale` value under fractional scale** — not captured during the 1.5 run; the probe's
  output filter excluded it. Only relevant to the fallback path that D2 recommends against.
- **Niri source at `8ed0da4`** — the event and response inventory came from the compiled binary and live
  probes, not from reading upstream source. The behavioural conclusions are **[V]** against the installed
  build; their stability across Niri releases is **[U]**.

## Revised pre-implementation gate

Milestone 1 may start when all of the following hold.

1. The corrected plan documents carry the `scale120` signature, the rounding rule, the
   destination-only viewport contract, and `damage_buffer`.
2. The client-side bind-version table is written down with its values, and required versus optional
   globals are named with the error text for each missing required global.
3. Task 5's scheduler test list includes both frame/release orderings, configure-during-pending-frame,
   and close-during-pending-frame.
4. Task 3 vendors `Amiri-Regular.ttf` and `OFL.txt` and asserts contextual joining by GID divergence.
5. Task 8 reads and validates the `{"Ok":...}` reply before entering the event loop, and treats
   `{"Err":...}` as a startup failure.
6. Task 6 uses the version-suffixed generator invocation, and a clean `go mod tidy` leaves `go.sum`
   holding only the four approved dependencies and their transitive requirements.
7. Owner decisions **D1** and **D2** are resolved. **D3**, **D4** and **D5** may remain open.
8. The operator confirms the live-scale procedure, including the restore step, on the target machine.

Items 1 through 6 are complete in this commit. Items 7 and 8 remain with the owner.
