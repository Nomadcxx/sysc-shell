# Bar surface styles and attached panels design

Date: 2026-09-25. Owner-directed on 2026-09-25 after the bar survey of
DankMaterialShell, Noctalia v5 and Caelestia (interactive mockups:
<https://claude.ai/artifact/JwBJ1YDCrUt62Skpm1Jdsa>). Status lives in bd.

The owner asked for three things:

1. A frosted bar as the default, with a solid bar and floating islands as
   options, chosen in Settings.
2. An attached bar: flush to the screen edge, with concave corners where it
   meets the screen.
3. Every panel attached to the bar with concave joints, except Settings,
   the launcher and the clipboard (amended by the owner on 2026-09-25: those
   three stay floating, because attaching them would confuse where they come
   from).

References were read for behaviour only. No QML, C++ or configuration is
imported.

## What exists, verified on `claude/charming-dijkstra-ako8z4` at `7bcc4f5`

- **Opacity without blur.** `appearance.bar-opacity` exists and is floored at
  `theme.OpacityMin` (80) by `opacityAlpha` in `internal/shell/theme.go`,
  because nothing blurs behind the bar. Presets default it to 100.
- **Panel blur by capture.** `appearance.blur-behind` takes one
  `zwlr_screencopy_manager_v1` capture per panel open and blurs it on the CPU
  (`docs/plans/2026-09-11-panel-backdrop-blur-design.md`). The bar was left out
  on purpose.
- **Attached panels already exist, but you can't see them.** Every panel that
  is not `CenterY` paints with `AttachEdge` and draws concave wedges joining
  the bar (`fillAttachFillets`, `filletCoverage` in `internal/render/canvas.go`;
  `filletMargin` in `internal/shell/panelhost.go`). `filletMargin` caps the
  wedge at `Panels.Padding − BarGap`, which is 8 − 4 = 4 px for every panel
  except the Control Centre. The theme's fillet is 12 px. The launcher,
  wallpaper and clipboard panels are `CenterY`, so they never attach. And the
  bar floats 4 px from every edge with a 12 px radius, so the joint reads as a
  panel hanging off a pill. This is why the attached look has never landed.
- **The existing joint was the wrong shape.** `filletCoverage` centred its
  circle on the junction of the bar edge and the panel side, which fills a
  convex quarter disc: a rounded block beside the panel that meets both edges
  at right angles. A concave joint centres the circle a radius away from both
  edges, so the arc runs tangent into each. Corrected in Phase A, where the
  coverage maths moved to `internal/ui` so the painter and blur regions share
  it.
- **Segmented bar groups do not exist.** `KindSegmented` is a row of equal
  buttons, each its own stadium, used for tab rows inside panels. A bar `group`
  is one capsule with its members flat inside it. The DMS "segments" style
  (joined pills with small inner radii) is out of scope here. D5's per-side
  fillets are a step toward it.

## Compositor blur is the enabling fact

Niri 26.04 implements `ext-background-effect-v1` (wayland-protocols 1.45,
staging). A client gives a `wl_surface` a blur region as a `wl_region`, and the
compositor blurs whatever is behind that region, live, on the GPU. Noctalia
(`src/shell/bar/bar.cpp`, `applyBarCompositorBlur`) and DMS
(`Widgets/WindowBlur.qml`) both do this for their bars and panels. The blur
parameters are the compositor's: Niri's `blur {}` block. The protocol carries
no radius.

For a bar this is the right tool: it costs the shell nothing per frame and
follows the scene. The CPU capture remains the right fallback for panels where
the compositor lacks the protocol.

Protocol facts the implementation must respect:

- The manager sends `capabilities` on bind and whenever support changes. The
  1.45 XML declares `blur` as value `0` in a bitfield, which is a defect: the
  corrected wire mask is `1`, and Noctalia tests `flags & 1`. Test the bit, not
  the generated constant.
- `get_background_effect` on a surface that already has one is a protocol
  error. There is one effect object per `wl_surface`, created once and
  destroyed with the surface.
- The blur region is a `wl_region`, a union of rectangles, double-buffered on
  `wl_surface.commit`. Rounded and concave edges are therefore approximated by
  one rectangle per pixel row.

## Decisions

### D1. Three bar styles

`bar.style` is `frosted` (default), `solid` or `islands`.

| Style | Bar ground | Pills | Blur region |
|---|---|---|---|
| `frosted` | translucent at `bar.frost-opacity` | translucent at `bar.pill-opacity`, over the ground | the bar body and its end fillets |
| `solid` | today's: `appearance.bar-opacity`, floor 80 | opaque | none |
| `islands` | not painted | translucent at `bar.pill-opacity` | the union of the visible capsules |

`islands` works without new widget chrome, because every bar widget is already
wrapped in a capsule by `capsuled`.

High contrast forces `solid` at full opacity, as it already forces every root
opaque.

### D2. Blur comes from the compositor; the bar never captures

The platform binds `ext_background_effect_manager_v1` as an optional global,
like `zwlr_screencopy_manager_v1`, and reports `blur` availability to the shell
through a new `Callbacks.Capabilities`. It is delivered before the first
`NewHost` and again whenever the capability changes.

The shell describes each surface's blur shape as logical rectangles. The
platform owns the effect object and the `wl_region`. It re-sends the region
only when the rectangles change, and clears it when they are empty.

Without the capability:

- `frosted` paints as `solid`, and the existing 80% floor applies.
- `islands` still skips the ground, but pills paint at the solid floor.
- One log line names the missing protocol.

The Settings description says frost needs Niri 26.04 or later.

`appearance.blur-radius` keeps governing only the CPU capture fallback.

### D3. Opacity model

Two new bar settings, both percentages:

- `bar.frost-opacity`: the frosted ground. Default 65, range 40–100.
- `bar.pill-opacity`: pills in `frosted` and `islands`. Default 70, range
  40–100.

The 40 floor is taste, not legibility. The comment on
`theme.OpacityMinBlurred` records that a blurred ground removes the wallpaper
detail the 80 floor exists for.

`appearance.bar-opacity` keeps meaning the `solid` bar, so choosing `solid`
restores today's look exactly.

Translucent pills are lifted toward `OnSurface` by `(1 − pill alpha) × 0.3`,
so a pill stays distinct from a translucent ground behind it. This is
Caelestia's `Colours.layer()` without its wallpaper-luminance term. Scaling
the lift by wallpaper luminance is a follow-up that needs the wallpaper service
to publish one number per output.

### D4. Blur shapes are the painted shapes

A pure function in `internal/ui` turns a shape into row strips:

```go
type SurfaceShape struct {
	Body                  Rect
	Radius                int    // convex corners
	AttachEdge            string // "top" | "bottom" | "": that edge's corners are square
	JointLeft, JointRight int    // concave wedges beside the attached edge, joining the bar
	EdgeFillet            int    // wedges off the far edge, curving into the screen's side
	EdgeLeft, EdgeRight   bool   // an attached bar sets both; a flush panel sets its flush side
}
func BlurStrips(s SurfaceShape) []Rect
```

It uses the same coverage rule as `filletCoverage` and `roundedInset`: a row is
included where coverage is at least half. So the blur never shows outside the
painted edge. Row count is bounded by `2 × (radius + fillet)` plus one body
rect.

In `islands`, the bar's strips are the union of each visible capsule's rounded
rect, rebuilt after layout. Absent media collapses its capsule, and its strips
go with it.

### D5. Bar shape: attached or floating

`bar.shape` is `attached` (default) or `floating`. `islands` ignores it and
lays out as floating, since there is no ground to attach.

**Attached:**

- The body is flush with the anchored edge and both output ends. The gap is
  zero at the edge and the ends, and the body height is unchanged
  (`Height − 2 × Gap`).
- The corners on the anchored side are square. The two inward corners are
  concave end fillets of radius `Fillet` (12). Each is a wedge below the body
  at the output edge, curving into the screen's side edge. It is the panel
  joint's wedge, mirrored.
- The layer surface grows by the fillet radius past the body, which the
  platform already anticipates in `regions.go` ("a later milestone that adds a
  shadow grows the surface past the exclusive zone and excludes that band
  here"). The exclusive zone stays the body height, and the fillet band is left
  out of the input region, so it is click-through.
- Only top and bottom edges are supported. Left and right bars stay gated on
  `sysc-314`, as the geometry design says.

**Floating** is today's geometry.

`config.Bar` gains `Shape` and a derived `Overhang` (the fillet radius when
attached, else 0), next to the fields `deriveBar` already derives from the
theme. `Extent()` becomes the anchored-edge extent without the overhang.
`SurfaceExtent()` adds it, and the platform sizes the surface from
`SurfaceExtent()`.

### D6. Every panel attaches, except Settings, the launcher and the clipboard

| Panel | Today | After |
|---|---|---|
| Clock, Monitor, Session, Notifications, Plugin, Audio, Control Centre, Network, Bluetooth, Weather | attached, 4 px joints | attached, full joints (D7) |
| Wallpaper | centred below the bar, floating | attached at the bar's centre |
| Launcher | centred below the bar, floating | unchanged: floating |
| Clipboard | centred in the whole output, modal | unchanged: floating |
| Settings | attached, centred | **floating**: centred below the bar, full radius, rim |

The launcher and clipboard are keyboard-first pickers summoned by hotkey as
often as from the bar. Attaching them would tie them visually to a bar
button they were not opened from.

In `islands` style, or with the bar disabled on that output, every panel is
detached. It sits `theme.MarginS` below the bar zone with full radius and its
rim, and no joints.

Not panels, and out of scope: toasts, the OSD, tray menus and the tray drawer,
the running-apps menu, tooltips, and plugin sticky surfaces.

### D7. Joint width per side, and panels that meet the screen edge

`filletMargin` becomes a per-side computation:

- Room on a side is the distance from the panel's edge to where the bar's
  straight edge ends. Attached, that is the output edge. Floating, it is the
  body end minus the bar radius.
- Each side's joint is `min(Fillet, room)`. The 4 px cap goes away, and a
  panel with room on one side and not the other keeps its one good joint.

In attached shape, a panel whose side would land within `Fillet` of the output
edge snaps flush to that edge. Session and Notifications do this; they are
right-aligned. That side has no joint. Instead, the panel's outer bottom corner
becomes a concave fillet into the screen edge. It continues the bar's end
fillet, which the panel now covers. The panel surface grows downward by the
fillet radius for that wedge, and the band is click-through.

`Placement` gains `JointLeft`, `JointRight`, `FlushLeft` and `FlushRight`.
`render.Style` gains the same four. They replace the single `Fillet` for
panels, and `Fillet` remains the theme token.

### D8. One joined ground

An attached panel already resolves its root at the bar's alpha
(`AttachedPanelStyle`). With a frosted bar that is the frost opacity, and the
panel publishes its body, joints and edge fillets as its blur region, so the
two surfaces read as one frosted sheet.

- Attached panels draw no rim. A rim across the joint is a seam, which is why
  the audio panel already omits it. Floating Settings and detached panels
  keep their rim.
- The panel overlaps the bar by 1 logical pixel on the attached edge (Noctalia
  `panel_overlap`). Without it, fractional scales leave a hairline between the
  two blurred regions.
- The reveal animation keeps scaling joints with opacity, as `panelReveal`
  does now.

### D9. Panel blur prefers the compositor

With `appearance.blur-behind` on and the capability present, a panel publishes
a blur region and takes no screencopy capture. The capture path is kept for
compositors without the protocol. This makes panel blur live, and removes the
roughly 11 ms per open recorded in
`2026-09-13-parity-tranche-continuation-handover.md`.

### D10. Settings

In the Settings panel:

- **Bar › Surface:** `Style` (Frosted, Solid, Islands) and `Shape` (Attached,
  Floating), beside Enabled and Edge.
- **Bar › Frost:** `Frost opacity` and `Pill opacity`.
- **Appearance › Opacity:** `Bar opacity` is relabelled "Solid bar opacity".
- **Appearance › Depth:** "Blur behind panels" says it uses the compositor
  when available.

All four new settings apply live. Style and opacity restyle the bar. Shape is
a layer-surface geometry change, which the reload path already applies without
remapping.

### D11. Niri configuration

A new `docs/niri-blur.md` documents:

- the Niri 26.04 requirement;
- the namespaces `sysc-shell:bar` and `sysc-shell-panel`;
- that the default xray behaviour suits the bar, which only ever sits over the
  wallpaper;
- a `layer-rule` setting `xray false` for `sysc-shell-panel`, so windows
  behind a panel show through its blur.

The shell publishes regions whether or not a rule exists, as Noctalia does.

## Defaults

`frosted` and `attached`, frost 65%, pills 70%. Choosing `solid` and
`floating` in Settings gives today's bar, apart from D7's fuller joints.

## Proof

- `BlurStrips` table tests: square body; rounded body; attached body with end
  fillets at top and bottom edges; panel with asymmetric joints; flush panel;
  a radius larger than half the body.
- The capability bit is tested against the corrected mask, with an event
  carrying `1` and one carrying `0`.
- Placement tests for every `PanelID`, in attached, floating, islands and
  bar-disabled modes.
- Pixel tests for the bar end fillets at top and bottom edges, and for a flush
  panel's screen-edge fillet.
- Config round trip for the four new settings. An old config without them
  loads as frosted and attached.
- Settings registry entries validate their ranges and options.

## Live gate

Owner-run on Niri 26.04+, `DP-1`:

- Each style × shape combination, over a bright and a dark wallpaper.
- Open every panel: joints visible and continuous, no hairline at the seam;
  Settings, the launcher and the clipboard floating.
- Session and Notifications flush to the right edge with a screen fillet.
- Switch style and shape live from Settings.
- Start on a compositor without the protocol, and confirm `frosted` falls back
  to solid with one log line.

## Rejected alternatives

- **Blurring the wallpaper image the shell owns, for the bar.** Correct only
  for a docked bar with a reserve, wrong under `bar.reserve = 0`, and it
  duplicates the compositor's work. Kept only as a possible fallback idea.
- **Screencopy for the bar.** The bar is always mapped. A live capture is a
  per-frame cost the blur design ruled out.
- **Attaching Settings.** Settings is a large, long-lived editor. Floating
  keeps it clearly separate from the transient panels, as the owner asked.
