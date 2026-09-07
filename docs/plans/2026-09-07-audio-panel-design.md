# Standalone audio panel — Design

Date: 2026-09-07. Status: approved 2026-09-07 (mock approved; 560 px width kept; Devices rows are selected wells with trailing `check`; wing-tips ride main's `Style.Fillet` machinery; `render.AttachedMask` superseded — see D9)
(`docs/plans/assets/2026-09-07-audio-panel/`). Not yet registered in bd.

A first-party `PanelAudio` — Volumes and Devices as two tabs on one surface,
fused to the bar by concave wing-tips — plus the thin bar widget that owns it.

Composition target: the Noctalia audio popup
(`noctalia-volume.png`, `noctalia-audio_devices.png`, analysed 2026-09-07).
Behaviour and visual reference only. No QML, C++ or configuration is imported.

Sources (read, not imported):

- `internal/services/audio.go` — the shipped lease/poll/`wpctl` service
- `internal/shell/panel.go`, `panelhost.go`, `registry.go`, `widget.go`
- `internal/ui/controls.go` — `KindSlider`, `SliderTrack 4`, `SliderKnob 14`
- `internal/ui/tree.go` — `KindSegmented`, `KindScroll`, `KindImage`, `KindIcon`
- `internal/render/mask.go` — every shipped mask is convex
- `internal/render/icons/material/build.py` — the hand-curated glyph subset
- `internal/theme/profile.go`, `theme.go` — metrics, type roles, tokens
- `docs/plans/2026-09-03-control-center-design.md` — D1 wing-tips, D11 states
- `docs/plans/2026-09-06-connectivity-and-media-prior-art.md` — the slicing note

## Goal and scope

In:

- `PanelAudio`, public IPC name `audio`, opened by a bar widget or by IPC.
- Two tabs: **Volumes** (levels) and **Devices** (routing). Neither leaks into
  the other.
- `services.Audio` extended to enumerate sinks, sources and playback streams
  with per-node volume, mute, description and icon name, from `pw-dump`.
- Writes — set volume, set mute, set default device — through `wpctl` by node id.
- One `volume` bar widget: glyph, scroll to step, right-click to mute,
  left-click to toggle the panel.
- Wing-tip joints through main's `Style.Fillet`/`FilletFill` machinery
  (superseded `render.AttachedMask` — see D9).
- Four Material glyphs added to the embedded subset.

Out:

- MPRIS transport controls (`sysc-156`). This panel is a mixer, not a player.
- Per-device volume on the Devices tab. Routing is routing; levels are levels.
- Channel balance, per-channel volume, loopback, or node-graph editing.
- Any change to the control centre's designed Audio page. That page stays the
  thin slider `sysc-158` specifies; its rail entry opens this panel instead.
- Replacing the bar's existing cheap default-sink poll.

## Decisions

| # | Decision | Rejected alternative |
|---|---|---|
| D1 | **`PanelAudio`, 560 × 496 content, 584 drawn.** Drops from its bar widget, `Gap: 0` so `anchor = BarZone + 0`, and fused to the bar by **wing-tips**: top corners concave at radius 12, bottom two convex at 12, `logicalInset` widening the drawn surface by the 12 px bulge on each side. `alignX` centres the panel on its trigger; `clampAxis` keeps it on the output. The panel and the bar paint the **same** token, `SurfaceContainerLow` — a fused joint between two different surfaces reads as a seam | A detached popup with a gap and a shadow (the first prior-art pass read the Noctalia joint as square and butted; the owner corrected it). Centring on the output like the control centre — this panel belongs to its widget, not to the bar |
| D2 | **560 wide, sized to the Devices tab.** The longest real device name here — `AD106M High Definition Audio Controller Digital Stereo (HDMI)`, 61 chars — needs 448 px of text box; 560 − 32 panel padding − 24 card padding − 24 row padding − 20 check − 12 gap leaves exactly that. The Volumes rows front-elide instead, which still identifies a device, and the full name is one tab away | 480, which elided device names on both tabs. Sizing to the Volumes tab and letting Devices — the tab whose whole job is telling two similar names apart — truncate |
| D3 | **496 tall, sized to the Volumes tab** carrying output, input and two application rows: 16 + 116 header + 12 + 68 + 12 + 68 + 16 + 28 label + 68 + 8 + 68 + 16. The body is a `KindScroll`; Devices with four sinks and four sources overflows to 648 and scrolls, with a fade at the clip | A taller panel sized to the worst-case device count — device count is unbounded, so there is no height that removes the scroll, only one that adds a dead band to every other case |
| D4 | **One volume-row archetype, three uses.** Every row is `[identity 32] [role + name over slider] [value 44] [mute 32]`: identity is a role glyph for Output and Input and the resolved application icon for a stream; the trailing button is always the mute toggle. Output, Input and each application share one builder and one hit-test shape | Noctalia's two shapes — label-above for the device rows, icon-left for the application rows. Two card layouts for three rows of the same job is more code and a broken vertical rhythm |
| D5 | **The device name on a volume row is the link to the Devices tab.** It is the one thing on that row you would want to change and cannot, so it carries the affordance rather than a second control | A separate "change device" button per row. A dropdown per row, which is the Devices tab drawn twice |
| D6 | **Devices rows are selected wells, not radios.** The current device is a `SecondaryContainer` well with a trailing `check`; the rest are plain rows. `check` already ships in the glyph subset, and Material's list-selection idiom reads as "current route" rather than as a form to submit | Radio buttons, per the prior art. They cost two new glyphs, and a form control implies a pending choice that has to be confirmed — selection here is immediate |
| D7 | **`pw-dump` reads, `wpctl` writes.** A panel-scoped lease polls `pw-dump` at 1 Hz while the panel is open and parses one typed `Snapshot` — sinks, sources, streams, defaults, volume, mute, description, `application.icon-name`. Volumes are `cbrt(channelVolumes[0])`: PipeWire stores linear, `wpctl` displays cubic, and reading the raw value would show 14 % where the rest of the shell shows 51 %. Writes stay on `wpctl set-volume` / `set-mute` / `set-default` by node id. The bar widget keeps today's cheap default-sink poll untouched | `wpctl status` text parsing — one exec and no JSON, but a human-readable format with no stability contract and no icon name. A long-lived `pw-mon` child, which is the orphan-process shape `sysc-140` already records |
| D8 | **Application icons resolve through the shipped `icons.Resolver`,** keyed on `application.icon-name` and falling back to the lower-cased `application.name`. A stream whose icon does not resolve gets the role glyph, never a blank square | A second icon path for audio streams. Blocking the row on an icon that may never arrive |
| D9 | **`render.AttachedMask` lands here, in its own slice, and `sysc-154` depends on it.** `mask.go` is convex-only; this panel is the primitive's first consumer, and the control centre — designed but not started — is its second. The signature is exactly the one `sysc-158` specifies, so the control centre consumes it unchanged. The bulge ramps 0 → 12 with the reveal, so the wings appear as the panel clears the bar. **Superseded at rebase:** main shipped `Style.Fillet` + `FilletFill` with the notification-centre merge — analytic concave wedges painted with the bar's fill, wired into every panel surface by the theme, alpha-seam safe. This panel consumes that machinery instead: constant fillet 12, standard slide reveal, no per-frame ramp, no `AttachedMask`; `sysc-154` consumes it too | Blocking the audio panel on the control-centre spine, which is precisely the false dependency the 2026-09-06 slicing note argues against. A private wing mask inside the panel, extracted later |
| D10 | **Four glyphs join the embedded subset:** `mic`, `mic_off`, `graphic_eq`, `headphones`. `volume_up` and `volume_off` already ship. `build.py`'s `ICONS` and `materialfont.go`'s `materialIcons` are kept in step by hand and asserted by test — a name the subset does not hold shapes to nothing and paints an invisible control | Reusing `volume_up` for the input row, which would label the microphone a speaker. Drawing the mic as a custom mask |
| D11 | **Writes route through one `scheduleControl(h, run)` helper,** capturing under `r.mu`, running with the lock released, re-taking and verifying the host is still current before it sets or clears `errLabel` and rebuilds. `wpctl` must never run under `Registry.mu` | Calling `r.stepAudio` from the handler — lock-free only because IPC reaches it off the owner |
| D12 | **Five states, one vocabulary, shared with the control centre's D11.** *Unavailable*: no `pw-dump` or no `wpctl` — controls disabled at 38 % with a short reason. *Empty*: no application streams — a centred empty state under the Applications label, never an absent section. *Stale*: leased, no sample yet — dashes and an empty track, never `0%`. *In flight*: the requested value shows immediately and reconciles from the next sample. *Failure*: inline error line at the top of the affected tab, last known value restored | Painting `0%` while a sample is pending. A modal error. Hiding the Applications section when nothing is playing, which makes an empty mixer look broken |
| D13 | **One `volume` bar widget** — Material glyph reflecting level and mute, left-click toggles `PanelAudio`, right-click mutes, scroll steps by 5. It supplies both `inner` and `format`, satisfying `Bar.applyLocked`; a widget with neither seam is the blank-shell panic of the 2026-09-06 handover. The config type name is `volume`, not `audio`, matching the existing metric-widget naming | A widget that only routes, with no direct action — the reference shells all bind scroll and right-click. Reusing `network`, which `widget.go:184` already binds to throughput |

## Pixel contract (standard density, fontScale 1)

Values are `theme.Metrics` / `theme.typeRoles` tokens, not literals to copy.

| Region | Size | Token or derivation |
|---|---|---|
| Panel content | 560 × 496 | sized to Devices (width), Volumes (height) |
| Panel drawn | 584 × 496 | content + 2 × bulge |
| Bar gap | 0 | fused: `anchor = BarZone + Gap` |
| Top corners | concave, 12 | wing-tips, `AttachedMask` |
| Bottom corners | convex, 12 | `presets[PresetStandard].Radius` |
| Wing bulge | 12 each side | `logicalInset` left/right |
| Panel inset | 16 | `PanelPadding` |
| Card inset | 12 | `CardPadding` |
| Card gap | 12 | `SpacingScale[3]` |
| Header card | 116 tall | 12 + 40 + 12 + 40 + 12 |
| Icon well | 40 square, r12 | `StandardControl` |
| Tab strip | 40 tall, segments 32 | `StandardControl`, `CompactControl` |
| Volume row | 68 tall | 12 + 18 + 6 + 20 + 12 |
| Identity / mute | 32 circle | `CompactControl` |
| Value column | 44, tabular | fixed so 100 % does not shove the button |
| Slider | track 4, knob 14 | `ui.SliderTrack`, `ui.SliderKnob` |
| Device row | 44 tall, r8 | — |
| Title / role / body | 20/600, 14/500, 14/400 | `RoleHeadline`, `RoleLabel`, `RoleBody` |

Colour is the live generated dark palette. The mock uses this machine's
`~/.cache/sysc-shell/colors.json` verbatim; no value in it was invented.

## Information architecture

```
bar ── [ CPU/MEM ] [♪] [🔔] [⚙] [ clock ] ── bar
              │ fused, wing-tips, Gap 0
PanelAudio (560 × 496 content / 584 drawn)
├── header card ── icon well · "Audio" · close
│                  segmented: Volumes | Devices
└── body (KindScroll)
    ├── Volumes ── Output row · Input row
    │              "Applications" n · one row per stream
    └── Devices ── "Output device" · selectable list
                   "Input device"  · selectable list
```

## Testing

Automated: `go test ./internal/shell ./internal/ui ./internal/services
./internal/render` with `-p 4` and `GOMAXPROCS=4`, never `-race` repo-wide
(this box is zram-only swap and hard-locks), and `loginctl`/`systemctl`
shadowed by stubs earlier on `PATH` — `./internal/shell` really runs
`loginctl terminate-session self`. Service tests parse fixture `pw-dump` JSON
captured from a real session, including the cubic-volume conversion and a
hot-unplug between two dumps.

Live Niri: widget click, scroll and right-click; both tabs; a real device
switch heard through the speakers; an application row appearing and vanishing
as a stream starts and stops; the empty, unavailable and failure states; the
wing joint against a light and a dark wallpaper; Escape; focus order;
reduced motion.

## Implementation slices

Sized so each lands on its own and nothing waits on the spine.

1. **Wing-tip joint** — superseded: main's `Style.Fillet`/`FilletFill`
   machinery (shipped, wired into every panel surface) covers the contract;
   `AttachedMask` dropped at rebase. `sysc-154` consumes `Style.Fillet`.
2. **Glyph subset** — four names into `build.py` and `materialfont.go`,
   regenerate, assert the two lists agree.
3. **`services.Audio` enumeration** — `pw-dump` parse, `Snapshot` type,
   panel-scoped lease, cubic conversion, `SetDefault`. Peer of the existing
   poll; the bar path is untouched.
4. **`PanelAudio` chrome** — panel identity, wing-tips, header, tabs, IPC
   `audio`, focus and Escape. Empty body.
5. **Volumes tab** — the row archetype, three uses, `scheduleControl` writes.
6. **Devices tab** — selectable lists, `set-default`.
7. **`volume` bar widget** — glyph, scroll, right-click, panel toggle.

1 and 2 are independent of everything; 3 is independent of 1, 2 and 4.
