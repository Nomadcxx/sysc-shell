# Screen Recorder panel — Design

Date: 2026-09-02. Status lives in bd (`sysc-136`).

M6E shipped the recorder process, bar toggle, and Settings schema. It left the
panel out (control-center stays M7). This design adds the missing working
surface: a composite bar pill and a sysmon-placed panel that edits the same
schema Settings already stores.

Owner choices, 2026-09-02:

- The panel is a second editor for the full recorder schema, one store (option C).
- Bar is a composite pill: camera identity, visible Record and Stop, no hidden
  mouse-button actions (approach 1).
- Camera left-click toggles the panel in the system-monitor slot.

Visual discipline (component-scope, utilitarian, inherit shell tokens):

- Pre-emit: P5 H4 E4 S5 R5 V4
- No new palette, type, or motion. Matugen tokens and existing capsule chrome.
- No nested capsules, no 2×3 icon grid, no emoji, no Tabler/Lucide mix.
- Recording uses `ToneError` as the only non-normal role (Noctalia paints the
  glyph error-red while capturing). Failed uses `camera-off` plus error copy,
  so the two states do not collapse into one.

## Goal and scope

Enable Screen Recorder in Settings. The bar shows a camera pill. Record and
Stop fire the saved settings. Left-click the camera opens a centered-off-bar
panel with live transport and the full schema.

In:

- Manifest `panels` entry and `panels` capability for `org.sysc.screen-recorder`.
- Bar: `[camera] Record Stop` in one capsule.
- Panel: plugin header (status, elapsed, transport, last path) stacked on
  host-generated settings for this plugin.
- Five project SVGs in `sysc-icons.ttf`: `camera`, `camera-off`, `record`,
  `stop`, `replay`.
- `hide_inactive` hides Record and Stop while idle; the camera stays.

Out:

- Control-center shortcut (M7).
- Screenshot, region, OCR, GIF, markup (Screen Toolkit).
- New `settings.set` host call; new v1 toggle/select/slider kinds.
- Glyph-override settings, `copy_to_clipboard`, Flatpak launch, portal restore.
- Changing an in-flight recording's codec or directory.
- Thumbnail gallery, `xdg-open` of the last file.
- Launcher-style vertical centering.

## Prior art

| | Bar | Panel | Settings |
|---|---|---|---|
| Noctalia `noctalia/screen_recorder` | One video glyph. Left toggle record, right replay, middle save. Colour by state. | None | Full schema under Settings → Plugins |
| Noctalia Screen Toolkit | Opens a tools panel | 380×260 dense grid, Record subpanel, last file | Separate from the official recorder |
| DMS community plugins | Bar widget + IPC | Mixed; some expose codec/FPS in a popout | Plugin settings page |
| sysc-shell today | One text button, Noctalia mouse mapping, no glyphs | None | Manifest schema in Settings → Plugins (bools are toggles; other types are a text stub) |
| sysc-shell sysmon | Left-click a metric | Floating, X-centered, hugs the bar, 640×480 | n/a |

Take from Noctalia official: one process, settings as the schema, error-role
while recording, `hide_inactive`.

Take from sysmon: placement and camera-as-identity (CPU click opens the box).

Do not take Screen Toolkit's grid, subpanels, or screenshot tools. Do not keep
the hidden mouse mapping once Record and Stop are visible.

## Decisions

| # | Decision | Rejected alternative |
|---|---|---|
| D1 | Optional plugin, enable/disable in Settings → Plugins. Same as today. | First-party built-in widget. |
| D2 | Bar pill is camera + Record + Stop in one capsule. Camera toggles the panel. Record/Stop do not. | Single Noctalia glyph. Camera-only pill. |
| D3 | Record and Stop stay on the pill at a stable width. Idle: Record live, Stop inert. Recording/Adopted/Stopping: Record inert, Stop live. Unavailable/Failed: both inert. | Morphing one label (reflow). Hide Stop until recording. |
| D4 | No pointer-button accelerators. Replay start/save live in the panel when `replay_enabled`. | Keep Noctalia right/middle on the camera. |
| D5 | Recording paints the camera with `ToneError`. Replay running swaps the camera to `replay`. Failed/unavailable use `camera-off` and error copy. | A third tone. Pulse/blink (no plugin motion; reduced-motion would kill it anyway). |
| D6 | Elapsed time is panel-only, tabular, while Recording, Adopted, or ReplayActive. | Elapsed on the bar. |
| D7 | `hide_inactive` hides Record and Stop when Idle. Camera remains so the panel is reachable. | Empty row (today). Hide the whole pill. |
| D8 | Panel uses existing `PanelPlugin` placement: Align default (horizontal center), hugs the bar, not `CenterY`. Size 480×560 from the manifest. | Widget-attached. Launcher mid-screen. Sysmon 640×480. |
| D9 | Manifest: `panels: [{id: "panel", width: 480, height: 560, placement: "attached", include_settings: true}]` and capability `panels`. `attached` already means this floating-off-bar slot. | New placement enum. Magic plugin-id in the host with no manifest flag. |
| D10 | Host composition: convert the plugin panel tree, then append host settings rows when `include_settings` is set. Plugin does not rebuild select/toggle/slider on the wire. | Duplicate the schema as plugin buttons. Add v1 control kinds. |
| D11 | Settings writes call `pluginHost.applySetting`. Same document Settings → Plugins writes. Plugin hears `settings.changed` and rebuilds the **next** argv. | `settings.set` host call. Restart GSR on every field change. |
| D12 | Upgrade `pluginSettingRow` to the same control kinds `settingsControl` uses (toggle, slider, menu, text field). Honor `visible_when`. `handlePluginManager` must apply the node value, not the literal `true`. Manager and recorder panel share this. | Recorder-only widgets. Leave the manager stub. |
| D13 | Folder is the existing path text field. This shell has no desktop folder picker. | Promise a native chooser. |
| D14 | Camera `panel.open` toggles **this** plugin's panel on that output. A second open of the same entry closes. Opening while another plugin owns `PanelPlugin` replaces it. No new call kind. | `OpenPanel` no-op when already open. New `panel.toggle` verb. |
| D15 | `KindButton` may set `Icon`. Convert resolves the catalogue glyph as the button label when `Text` is empty. Camera is an icon button; Record/Stop stay words. | PUA codepoints in plugin text. Separate inert `KindIcon` that cannot receive clicks. |
| D16 | Last path is the last successful artifact this process verified, truncated text. No directory listing, no ffmpeg thumbnail. | Recents list. |
| D17 | Control-center, region capture, clipboard copy, and glyph settings stay out. | Port the rest of Noctalia's setting keys. |

## Information architecture

Bar, one capsule:

```
[ camera ]  Record  Stop
```

Panel, one column, sysmon slot, host scroll, ~480×560:

```
[camera]  Screen Recorder
Recording                         00:12
[ Record ] [ Stop ] [ Start replay ] [ Save replay ]
~/Videos/Recordings/recording_20260902_120103.mp4

Capture
  Video source          [ focused | portal ]
  Show cursor           [ toggle ]
  Resolution            [ original        ]
  Frame rate            [ slider 1–240    ]

File
  Output directory      [ path field      ]
  Filename pattern      [ text field      ]

Video
  Video codec           [ menu            ]
  Quality (QP)          [ slider 0–51     ]
  Color range           [ menu            ]

Audio
  Audio source          [ menu            ]
  Audio codec           [ menu            ]
  Audio bitrate         [ slider          ]

Replay
  Replay buffer         [ toggle          ]
  (duration / pattern / storage when on)

Bar
  Hide when idle        [ toggle          ]
```

Headings are `KindText` with `Bold`, not nested capsules. Replay start/save
omit themselves when `replay_enabled` is false. `visible_when` hides bitrate
and replay extras.

## Data flow

1. Enable plugin → process, handshake, `settings.changed` with stored values.
2. Camera activate → `panel.open` → host toggles this entry (D14).
3. Record / Stop / replay actions → existing recorder state machine, focused
   output from `output.context`.
4. Schema control → `applySetting` → config file → `settings.changed` → next
   command line only.
5. While Recording/Adopted/ReplayActive the plugin ticks a keyed elapsed text
   once per second.
6. Shield, Escape, or disable closes the panel and still stops owned GSR on
   disable.

Failures stay on the header: error tone, bounded log tail (same string as the
tooltip). Schema stays usable. Missing `gpu-screen-recorder`: plugin visible,
stopped, `camera-off`, Record/Stop inert, panel still opens. Save notification
unchanged. No extra toast for Stop.

## Glyphs

Authoring-time SVGs in `internal/render/icons/svg/`, same 24×24 filled-path
language as `cpu.svg`. MIT, project-owned, not derived from Tabler or Material.
`build.py` maps them to PUA after `network` (`U+E01B`–`U+E01F`). `iconNames`
gains the five names so `KindIcon` / icon buttons resolve.

| Name | Use |
|---|---|
| `camera` | Idle identity, panel header |
| `camera-off` | Unavailable, failed |
| `record` | Catalogue only (bar uses the word Record) |
| `stop` | Catalogue only (bar uses the word Stop) |
| `replay` | Camera while ReplayActive; panel replay actions may use it |

`record` and `stop` ship so a later compact pill can drop the words without
another font rebuild. v1 of this UI does not put them on the bar.

## Code seams

- Catalogue: `internal/render/icons/build.py`, `iconfont.go` `iconNames`,
  `iconfont_test.go`.
- Convert: `internal/plugin/view.go` KindButton + Icon.
- Manifest: `internal/plugin/manifest.go` panel `include_settings`.
- Host panel: `pluginHost.panelTree` / `openPanel` in
  `internal/shell/pluginhost.go`.
- Settings rows: `pluginSettingRow` / `handlePluginManager` in
  `internal/shell/popout_plugins.go`; reuse `PanelHost.menus` / `fields`.
- Plugin trees: `plugins/reference/recorder/view.go` plus command
  `cmd/sysc-plugin-screen-recorder/main.go`.
- Elapsed: `recorder.Snapshot` needs a start time the view can format.

## Follow-ups (not this slice)

- `sysc-130` adopt dest path (process, not UI).
- Control-center shortcut (M7).
- Native folder picker if Settings grows one.
- Compact icon-only Record/Stop using the shipped glyphs.
- `include_settings` on other reference plugins.
