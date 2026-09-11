# Panel backdrop blur — Design

Date: 2026-09-11. Status lives in bd.

Approach owner-approved 2026-09-11 in brainstorming — visual parity with
Noctalia as the target, rendering before tokens, and a static capture. The
decisions below elaborate that approach and are pending owner review.

A blurred backdrop behind floating panel surfaces:
captured once per open through `zwlr_screencopy_manager_v1`, blurred on the CPU
at reduced resolution, composited under the existing `rootFill`. No GPU work,
no per-frame cost, no continuous frame loop.

Visual target is Noctalia v4. Every panel in the v4 reference set
(`noctalia-control_center.png`, `-audio_devices.png`, `-wifi.png`,
`-notifications.png`, `-calendar.png`, analysed 2026-09-11) sits on a
translucent ground over a blurred backdrop, and the owner's own v4 configuration
ran `general.enableBlurBehind: true` with `ui.panelBackgroundOpacity: 0.93`.
Behaviour and visual reference only. No QML, C++, or configuration is imported.

This slice exists because the shell cannot currently express that ground.
`theme.OpacityMin = 80` exists *only* because we cannot blur, and says so in its
own comment: "Opacity stops at 80 because the shell has no portable compositor
blur behind text; below that, wallpaper detail reads through a label." Parity
is unreachable while that floor stands.

Sources (read, not imported):

- `internal/render/paint.go:55` — `rootFill`, the surface's own background at
  theme opacity; `:181` `FillRounded(box, radius, style.rootFill())` is its one
  call site
- `internal/render/canvas.go:36` — `premultiply`, canvas memory order; `:264`
  `blendPixel`
- `internal/render/image.go` — `paintImage`, nearest-neighbour blit of a
  premultiplied raster
- `internal/render/style.go` — `Style`, the renderer-ready token view
- `internal/platform/wayland/shm.go:70` — `formatARGB8888` pool buffers
- `internal/platform/wayland/client.go:808` — `DamageBuffer(0, 0, w, h)`, the
  only damage call in the tree
- `internal/shell/panelhost.go:401` — `spawnPanelLocked`; `:1658`
  `panelTargetSize`; `:1689` `audioPanelSize`
- `internal/shell/theme.go:255` — `opacityAlpha`; `:262` the `OpacityMin` clamp
- `internal/platform/wayland/layershell/generate.go` — the generated-binding
  pattern this slice copies
- `docs/plans/2026-08-26-sysc-shell-design.md` — rendering contract, L19,
  L172–196, L353, L372
- `docs/roadmap.md` — Milestone 8, rendering qualification
- bd `sysc-202` — "Keep wl_shm while it meets measured budgets. Add EGL/OpenGL
  ES only for a named failing case (… large blurred panels …). Needs
  measurements, not a design-first feature epic."

## Goal and scope

In:

- `zwlr_screencopy_manager_v1` binding, generated from upstream XML.
- One region capture per panel open, before that panel's surfaces map.
- CPU separable box blur at reduced resolution, stored at reduced resolution.
- Backdrop composited beneath `rootFill`, inside the existing rounded / fillet
  geometry.
- `OpacityMin` floor lifted while a surface carries a backdrop.
- Config: `appearance.blur-behind`, `appearance.blur-radius`.
- Amendment to the architecture document and the roadmap M8 entry.

Out:

- Per-frame or live blur. The backdrop is static for the life of one open.
- EGL/OpenGL ES. This slice is the evidence that wl_shm suffices, not a step
  toward a second renderer.
- Bar, toast, OSD and tooltip surfaces. The bar is docked and opaque; its text
  sits directly over the wallpaper, which is a different problem.
- Image-backed cards, arc gauges, and the audio spectrum. Those are parity
  work against the superseding parity design, not backdrop work.
- Rectangle damage. Full-buffer damage stays as it is.

## Measured evidence

`sysc-202` asks for measurements, not a design-first feature epic. These were
taken 2026-09-11 on this machine, single-threaded, `GOMAXPROCS=4`, go1.27.0,
on synthetic premultiplied ARGB8888 buffers at real panel dimensions. Three box
passes per axis, radius 24 at full resolution and `24/k` at reduced.

| Panel | full-res | 4× + upsample pass | 2× no upsample | 4× no upsample |
|---|---|---|---|---|
| notifications 416×300 | 4.75 ms | 1.85 ms | 1.50 ms | **0.54 ms** |
| control centre 700×564 | 14.80 ms | 5.81 ms | 4.67 ms | **1.69 ms** |
| settings 900×620 | 20.83 ms | 8.20 ms | 6.59 ms | **2.39 ms** |
| wallpaper 980×1100 | 40.15 ms | 15.87 ms | 12.70 ms | **4.61 ms** |
| audio 1120×960 | 40.38 ms | 15.82 ms | 12.64 ms | **4.60 ms** |
| full output 3440×1440 | 185.87 ms | 73.20 ms | 58.15 ms | 21.14 ms |

Findings:

1. Cost is linear in pixel count at ~9.7 ms/MiB and **independent of radius**,
   which is the sliding-window box filter behaving as intended.
2. A naive full-resolution blur is not affordable: 40 ms on opening the Audio
   or Wallpaper panel is a visible hitch even paid once.
3. Downsampling with an explicit upsample pass returns only ~2.6×, because the
   upsample is itself a full-resolution pass with four taps per output pixel
   and comes to dominate the cost it was meant to save.
4. Dropping that pass returns **8.7–8.8×**. The worst regular panel is 4.6 ms.

The honest total is bounded, not exact: 4.6 ms is blur alone, and 15.8 ms is
blur plus a *separate* full-resolution upsample. The real figure sits between,
nearer the low end, because the blit iterating every destination pixel is paid
by the frame regardless. **Both bounds fit inside one 60 Hz frame (16.67 ms).**

Conclusion, for the M8 gate: a blurred panel backdrop does **not** miss the
frame budget on `wl_shm`, and therefore does not constitute the named failing
case that would justify EGL/OpenGL ES.

This measurement does **not** cover the screencopy copy itself — see D16.

## Decisions

### D1 — The backdrop source is screencopy, not the wallpaper

`zwlr_screencopy_manager_v1` version 3 is advertised live by this Niri
(registry name 32, verified 2026-09-11 alongside `zwlr_layer_shell_v1` v5,
`wp_viewporter`, and `wp_fractional_scale_manager_v1`).

Rejected: decoding the cached wallpaper still and blurring that. It needs no
new protocol, but the shell does not own display pixels — gSlapper does, and
`internal/wallpaper` only caches a still, and only for `KindVideo`. A backdrop
derived from the wallpaper is simply wrong whenever a window is behind the
panel, which for a control centre over a maximised window is the common case.
Screencopy is the only source that is correct.

Rejected: a hybrid that falls back to the wallpaper still. Two sources, two
correctness stories, for a case the no-blur path already handles. The fallback
for "no screencopy" is today's opaque panel, not a second backdrop.

### D2 — Capture is static: once per open

The backdrop is captured and blurred when the panel opens and held until it
closes. No timer, no recurring cost, so the architecture document's
no-continuous-frame-loop rule is untouched and the 60-minute idle gate passes
unchanged.

Accepted consequence: a video or terminal behind the panel freezes in the
backdrop while the panel is open. This is acceptable for transient panels and
most visible on the settings panel, which stays open longest.

Rejected: periodic re-capture, even via `copy_with_damage` (since v2). It buys
liveness nobody asked for and costs a recurring timer that would need its own
named exception and its own measurement.

Rejected: re-capture on Niri events (workspace switch, focus change). No timer,
and it tracks the changes that matter — but it is more wiring for a case D2
already accepts, and it still would not catch a playing video. Available later
without redesign.

### D3 — Capture the panel region, not the output

`capture_output_region` takes its region in **output logical coordinates**,
which is the same space as `Placement`. For the Audio panel that is 1120×960
(~4.3 MiB) rather than the full 3440×1440 (~18.9 MiB): a 12× reduction in both
copy and blur.

### D4 — Blur at reduced resolution, and store it that way

Downsample by 4 with a box average, blur at `radius/4`, and keep the result at
quarter resolution. A blur is a low-pass filter, so the discarded detail is
detail the blur would have destroyed. Premultiplied ARGB averages linearly —
which is exactly why the canvas stores premultiplied pixels — so neither
resample needs an un-premultiply round trip.

No upsample pass exists. The backdrop is handed to the blit at reduced size and
scaled during the composite that the frame pays for anyway. Per the table, this
is the difference between 2.6× and 8.8×.

### D5 — The backdrop blit samples bilinearly

`paintImage` is nearest-neighbour today, which is correct for icons: the icon
worker produces the exact size the node asked for, and resampling there would
be a second, worse scaler. A quarter-resolution backdrop is the opposite case —
scaling up 4× with nearest would band visibly across a large flat ground.

The backdrop therefore takes a bilinear sampling path. It is a separate entry
point, not a change to `paintImage`'s contract, so icon painting is untouched.

### D6 — Capture happens before the panel's surfaces map

Screencopy captures the composited output, including already-mapped layer
surfaces. Capturing after our own panel maps would blur the panel into its own
backdrop.

`spawnPanelLocked` computes the full `Placement` — output rect, panel size,
gap, alignment — before the `PanelHost` is constructed and before any surface
is created. That is the hook point. The **shield** surface (`shieldSurfaceID`)
maps alongside the panel and must also be excluded, so the capture precedes
both.

`overlay_cursor` is 0. A frozen cursor in the backdrop is an artefact.

### D7 — The scale contract goes through `Scale120`

The capture region is logical; the returned buffer is in output buffer pixels,
and its dimensions arrive on the frame's `buffer` event. Under fractional
scale those differ. All crop and sample arithmetic routes through the existing
`ui.Scale120`, as the rest of the renderer does: layout and hit testing in
logical units, painting in buffer pixels.

The frame's `flags` event may report `y_invert`. The compositing path honours
it rather than assuming an orientation.

### D8 — The opacity floor lifts only while a backdrop is present

`theme.OpacityMin = 80` drops to a lower floor for a surface that carries a
backdrop, and keeps 80 for one that does not. The floor is not deleted: its
reasoning still holds exactly when there is no blur behind the text.

### D9 — The protocol binding follows the generated pattern

`protocols/wlr-screencopy-unstable-v1.xml` plus
`internal/platform/wayland/screencopy/` with a `generate.go` carrying the
upstream source, revision and SHA-256, exactly as `layershell` and
`fractionalscale` do. Upstream is
`gitlab.freedesktop.org/wlroots/wlr-protocols`, `unstable/`, SHA-256
`131b8f9b4aad0c8a9cf705e90d2a1511a5ca0c477637fd3400cf1cc4fa963fb8`, providing
`zwlr_screencopy_manager_v1` and `zwlr_screencopy_frame_v1` at version 3
(verified twice, 2026-09-11).

Wayland types stay inside `internal/platform/wayland`, per the engineering
rules. The shell asks for a backdrop; it never sees a protocol object.

### D10 — Architecture and roadmap amendment

`2026-08-26-sysc-shell-design.md` currently states at L193: "The project will
add EGL/OpenGL ES only when profiling shows that shared-memory rendering misses
an agreed frame, CPU, or power budget." The Measured evidence section is that
profiling for the blur case, and it shows wl_shm does **not** miss the budget.

Amendments:

- L172–196, Rendering: record that a panel backdrop is captured through
  screencopy and blurred on the CPU at reduced resolution, and that this is
  paid once per open rather than per frame.
- L372, open qualification gates: "Measure shared-memory rendering before
  deciding whether to add EGL/OpenGL ES" is satisfied for blurred panels and
  cites this design. It remains open for the other named cases.
- `docs/roadmap.md`, Milestone 8: note that "large blurred panels" is measured
  and resolved in favour of wl_shm.

L19 needs **no** amendment. A static backdrop introduces no frame loop, which
is the property D2 was chosen to preserve.

Rejected: quietly shipping renderer work while the architecture document still
says GPU work needs profiling evidence, and the roadmap still lists blurred
panels as an open M8 case. That is the failure the gradient design's D11 was
written to avoid.

### D11 — Configuration

`appearance.blur-behind` (bool) and `appearance.blur-radius` (int, logical
pixels at full resolution, divided by the downsample factor internally).
Registered in `internal/settings/registry.go` beside the existing opacity rows.

Ships **default off**. The live Niri gate flips the default on. Parity wants it
on; shipping it on before it has run on real hardware would be asserting a
result this design has not yet earned.

### D12 — Failure is silent and total

If the manager is absent, the frame fails, the buffer cannot be allocated, or
the copy does not arrive within a bounded wait, the panel paints exactly as it
does today: opaque, at the `OpacityMin` floor. A backdrop is decoration. It
never blocks a panel from opening and never leaves one unpainted — the failure
mode that `sysc-wayland` handler panics already taught this project.

### D13 — Testing

Per-package named tests only. **Do not run `go test ./...` or `-race`**: a
repo-wide build with the race detector exhausts memory on this machine.

- Box blur: a single bright pixel spreads symmetrically; a uniform field is
  unchanged; output is independent of radius for a uniform field; edges clamp
  rather than wrap.
- Downsample: a 4×4 constant block averages to its constant; premultiplied
  channels stay consistent.
- Bilinear sample: midpoint between two known pixels is their mean; corners
  return the corner pixels; no read outside the source.
- Scale: a logical region under a non-1 `Scale120` produces the expected buffer
  rectangle.
- `y_invert` flips the composited result.
- Opacity: floor is lower with a backdrop present and 80 without.
- Failure: a capture that fails leaves the panel painted opaque, and opening
  still succeeds.

### D14 — Tracker

Hangs off epic `sysc-202`. Tasks are created when the implementation plan is
written, not by this document. Status lives in bd.

### D15 — Scope boundary

One panel proves this slice. Audio is the first consumer: it is the worst-case
size at 1120×960, and it has a v4 reference shot to compare against. Extending
to the other panels is configuration, not design.

### D16 — Open risks

1. **The screencopy copy is unmeasured.** The benchmark covers blur only. The
   copy is a GPU→CPU readback performed by the compositor, and on some drivers
   that is the dominant cost — plausibly anywhere from under a millisecond to
   well beyond the blur itself. This is the single largest unknown, and the
   first implementation task is to measure it on this machine before any
   compositing work is built on top. If it proves expensive, D2's static
   capture is what contains the damage: it is paid once per open, not per
   frame.
2. Capture-before-map ordering is asserted from reading `spawnPanelLocked`, not
   yet observed. If the compositor composites our surface sooner than expected,
   the backdrop contains the panel.
3. Two-output behaviour is unrunnable here — this machine has one output
   (`DP-1`, 3440×1440, scale 1.0) and Niri has no runtime virtual output.
   Per-output capture is designed but cannot be gated locally.
4. A panel larger than its output, clamped by `FittedSize`, must capture the
   clamped rect, not the requested one.

## Files

| Path | Change |
|---|---|
| `protocols/wlr-screencopy-unstable-v1.xml` | upstream protocol (new) |
| `internal/platform/wayland/screencopy/generate.go` | scanner directive, revision, SHA-256 (new) |
| `internal/platform/wayland/screencopy/screencopy.go` | generated binding (new) |
| `internal/platform/wayland/capture.go` | region capture on the Wayland owner goroutine (new) |
| `internal/render/blur.go` | downsample, box blur, bilinear sample (new) |
| `internal/render/style.go` | `Backdrop` on `Style` |
| `internal/render/paint.go` | `rootFill` composites the backdrop beneath the fill |
| `internal/shell/panelhost.go` | request capture in `spawnPanelLocked`, before map; release on close |
| `internal/shell/theme.go` | opacity floor conditional on a backdrop |
| `internal/config/config.go`, `load.go` | `blur-behind`, `blur-radius` |
| `internal/settings/registry.go` | two settings rows |
| `docs/plans/2026-08-26-sysc-shell-design.md` | D10 amendments |
| `docs/roadmap.md` | Milestone 8 note |

## Stop

Opening the Audio panel on Niri shows its content over a blurred capture of
whatever was behind it, at an opacity below the old floor of 80. Closing and
reopening re-captures. Turning `blur-behind` off restores exactly today's
appearance. No frame loop is introduced: the idle gate still shows no
continuous redraw. `plugin/v1` is untouched.

Live Niri check, owner-deferred: scale 1 and one non-1 scale; a panel over a
maximised window; a panel over bare wallpaper; capture-and-blur wall time
recorded for the Audio panel.

## Verified during design

- `zwlr_screencopy_manager_v1` v3 live on this Niri, registry name 32.
- `capture_output_region` takes output **logical** coordinates; `copy_with_damage`
  exists since v2; `buffer_done` since v3.
- Upstream XML SHA-256 `131b8f9b…63fb8`, fetched twice with identical digest.
- Canvas is little-endian premultiplied ARGB8888 (`canvas.go:43`); shm buffers
  are `formatARGB8888` (`shm.go:70`). Same memory order — a capture needs no
  conversion.
- `rootFill` has exactly one call site (`paint.go:181`).
- `DamageBuffer(0, 0, w, h)` at `client.go:808` and `tooltip.go:243` are the
  only damage calls in the tree; no rectangle damage exists to disturb.
- Zero occurrences of "blur" in `internal/` before this slice.
- Panel dimensions from `panelTargetSize`; Audio resolves to 1120×960 on a
  3440×1440 output via `audioPanelSize`.
- Blur timings above, taken on this machine 2026-09-11.

Not verified: the screencopy readback cost, capture/map ordering against a live
compositor, and any two-output behaviour. See D16.
