# Settings, OSD, and Theme Catalog Design — Milestone 4, Tranche 4B

Date: 2026-08-30
Status: Owner-approved (sections validated 2026-08-30 through brainstorming).
Branch: `milestone/panels-controls`
Worktree: `/home/nomadx/.config/superpowers/worktrees/sysc-shell/milestone/panels-controls`

Second tranche of Milestone 4; builds on
[the 4A panel foundation](2026-08-30-panel-foundation-design.md). 4A passes the milestone exit gate
on its own surfaces; 4B is breadth: the settings UI, the OSD with shell-owned audio/brightness
services, stock themes, and enforced app theming.

Research and prior-art evidence: [research doc](2026-08-30-panels-and-controls-research.md),
[prior-art doc](2026-08-30-panels-and-controls-prior-art.md).

## Scope

Tranche 4B ships:

- Controls with 4B consumers: toggle, slider, menu/dropdown, text field, scroll area, virtual
  list — plus the text-input-v3 and cursor-shape-v1 binding generation in sysc-wayland that the
  text field requires.
- Settings modal: layer-shell panel on the 4A machinery, schema-driven registry, sidebar + search,
  atomic config persistence with live reload.
- OSD surfaces with shell-owned AudioService and BrightnessService (external-change detection),
  `osd.step` IPC verb, documented media/brightness niri keybinds.
- Stock themes (~10 named color-family seeds) and the enforced app-theming template catalog
  (~16 niri-ecosystem templates, Go template engine, apply hooks).

It ships no launcher, clipboard, notifications, tray, dock, first-party lockscreen, attached
placement, multi-line text input, or AT-SPI export.

## Decisions

| # | Decision | Rejected alternative |
|---|---|---|
| D1 | Settings is one layer-shell modal panel on the 4A machinery (Overlay, shield, Exclusive, single instance), centered on the focused output with 4A clamping. | An xdg-shell toplevel (Noctalia) — new protocol surface and window-management behavior for no gate benefit; the layer-shell path is already built and tested. |
| D2 | Schema-driven settings registry: each entry is config path + label + type (bool / int / enum / string) + enum options; content renders entries with the 4B controls. | Hand-written per-tab UI (DMS ~40 tabs). One line per setting; search, keyboard nav, and restyling fall out of the registry. |
| D3 | Search filters registry entries by label; while searching the sidebar hides and matches list (Noctalia behavior). | A build-time generated search index (DMS) — the registry is already the index. |
| D4 | Text field is single-line with text-input-v3 preedit rendering and cursor-shape I-beam. | Multi-line input — no M4 consumer. |
| D5 | AudioService: while leased, poll `wpctl get-volume @DEFAULT_AUDIO_SINK@` ~500 ms; expose level/muted; Step/Set/SetMute via `wpctl set-volume`; any delta (including external clients) emits a change event → OSD. wpctl absent → service unavailable, UI hidden. | PipeWire wire protocol or cgo (heavy dependency for parity behavior); event-only design misses external changes, which both reference shells surface. |
| D6 | BrightnessService: read `/sys/class/backlight/*/brightness` + `max_brightness` directly (no exec for reads); step via `brightnessctl set ±N%`; delta → OSD. Zero backlight devices → unavailable, UI hidden (this desktop has none — must be testable headless). | Exec-per-read; ignoring the zero-device case. |
| D7 | OSD: one surface per output with a bar, shown on all of them; Overlay, keyboard none, exclusive_zone −1, no shield; position token (8-corner + center, default bottom-center) offset by the bar's reserved zone + padding; ~1.5 s visibility, timer resets on repeated changes; fade+slide gated by reduced-motion; content = icon glyph + label + level bar. | OSD only on the focused output — volume/brightness are global, and both reference shells show on every output. |
| D8 | Stock themes = ~10 curated seed hexes named as color families (Blue, Purple, Green, Orange, Red, Cyan, Pink, Amber, Coral, Monochrome), run through the same matugen pipeline as wallpaper sources (DMS model). | Porting Noctalia's hand-written full palettes — the matugen path keeps one pipeline and one token shape; names stay honest about being seeds, not ported palettes. |
| D9 | Template catalog: ~16 niri-ecosystem templates via `go:embed`; Go `text/template` over the nested color map; apply hooks ported from Noctalia's bash to Go (plain write / include-injection / gtk theme-dir write); never overwrite user content; per-template toggles in settings, niri on by default, others off. | Shipping Noctalia's bash apply scripts (external runtime dependency), or the full catalog including hyprland/sway/mango/labwc — owner: this is a niri-only shell. |
| D10 | Hotkeys for volume/brightness: documented niri keybinds (XF86AudioRaiseVolume/LowerVolume/Mute, XF86MonBrightnessUp/Down) spawning `sysc-shell ipc osd <audio|brightness> <up|down|mute>`; the shell steps the value via the service and shows the OSD. | Compositor-side stepping — the shell would miss the change and the OSD would not fire. |

## Settings modal

Surface (D1): one settings panel ID on the 4A machinery; opens centered on the focused output
(bar-anchored alignment doesn't suit a large modal); same dismiss shield, Exclusive keyboard,
single-instance semantics.

Structure: sidebar of sections + content pane + search field in the header. Roving focus per the 4A
keyboard model: arrows within sidebar and match list, Tab into content, Escape closes.

Sections at M4 scope, all rendered from the registry (D2):

| Section | Entries |
|---|---|
| Bar | edge, height, gap, padding, spacing, font family/size, items per section |
| Widgets | per-widget options discovered from the configured widget list |
| Appearance | theme source (wallpaper/hex/stock), seed, scheme, mode, high-contrast, per-template toggles |
| Panels | gap/padding tokens, OSD position |
| Session | locker command |
| Accessibility | reduced-motion, high-contrast |

Persistence: atomic write (temp + rename) to `$XDG_CONFIG_HOME/sysc-shell/config.json`, then the
existing reload path (acquire-before-release, rollback on bad write) applies changes live — no
restart. This depends on 4A's contract that a reload leaves aux surfaces mapped: the settings modal
is itself a panel, so a reload that tore panels down would dismiss the settings UI on every change.
The registry validates before writing; invalid input stays in the control with an error state and
never reaches the file.

## Controls

Each enters with its consumer (roadmap rule):

| Control | Consumer |
|---|---|
| toggle | settings booleans (reduced-motion, template toggles, bar enabled) |
| slider | settings numerics (height, gap, padding, spacing) |
| menu/dropdown | settings enums (edge, theme source, scheme, mode, OSD position) |
| text field | settings search + string entries (locker command, font family, seed) |
| scroll area + virtual list | settings content (sections and long entry lists) |

Text input (D4): single-line; text-input-v3 with preedit rendering; cursor-shape-v1 I-beam on
hover/focus. Both protocols are new generated bindings in sysc-wayland (its scanner + XML
additions; sysc-wayland currently ships wayland-core, layer-shell, fractional-scale, viewporter,
xdg-shell). This is the one cross-repo deliverable of the tranche: sysc-wayland gains the
bindings and a release; sysc-shell consumes them. Niri support is source-verified:
TextInputManagerState, InputMethodManagerState (server-side IME; the client needs only
text-input-v3), wp_cursor_shape_device_v1.

Keyboard and accessibility: the 4A roving model and accessible name/role data extend to every 4B
control; keyboard-only operation of the whole settings modal is a tranche acceptance item.

## OSD and services

Services follow the M3 lease exemplar (consumer-counted; first starts, last stops):

- **AudioService** (D5): ~500 ms `wpctl get-volume @DEFAULT_AUDIO_SINK@` poll while leased; parses
  level + muted; Step/Set/SetMute via `wpctl set-volume`; any delta emits a change event. Leased by
  the OSD while visible and by any future mixer UI. Ceiling: polling misses sub-interval
  transients — accepted.
- **BrightnessService** (D6): sysfs reads; `brightnessctl set ±N%` steps; delta → change event.
  Zero devices → unavailable; OSD and settings entries hide.

OSD surface (D7): created on demand per output-with-bar, destroyed after the timeout; icon glyph +
label + level bar; fade+slide gated by reduced-motion; timer resets on repeated changes so holding
a key keeps it visible.

Triggers: AudioService/BrightnessService change events (including externally-induced changes
detected by polling) and the `osd.step` IPC verb. Hotkeys per D10.

## Stock themes and template catalog

**Stock themes** (D8): ~10 curated seed hexes named as color families. Selecting one sets theme
source = stock + seed; the 4A matugen pipeline generates the palette — one code path for
wallpaper, hex, and stock sources.

**Template catalog** (D9), informed by Noctalia's (MIT; attribution NOTICE on port):

- Templates: terminals alacritty, foot, ghostty, kitty, wezterm; compositor niri; system gtk3,
  gtk4, qt, kcolorscheme; editors emacs, helix; misc btop, cava, starship, scroll. (~16.)
- Each template: placeholder file rendered by Go `text/template` over the nested color map (the
  Noctalia `{{colors.*.dark.hex}}` placeholder style maps onto Go map-field chaining), plus an
  apply hook in Go: plain file write, include-injection, or gtk theme-dir write.
- **niri template**: renders `~/.config/niri/sysc-shell.kdl` (focus-ring, border, window shadow,
  tab-indicator colors) and idempotently appends `include "sysc-shell.kdl"` to `config.kdl`
  (grep-before-append; only our own include line is ever touched). Niri hot-reloads config, so
  borders update live — the enforced-theming parity behavior.
- **kitty**: writes config and signals SIGUSR1 to reload. Other apps: file write only; reload is
  the app's business (documented ceiling).
- **gtk**: writes a theme directory under the user themes path; switches `gtk-theme-name` only
  when it is unset or already ours — never overwrites a user's chosen theme.
- Per-template toggles live in settings (Appearance); defaults: niri on, everything else off.
  Apply runs single-flight with generation supersede (4A theming pipeline pattern); re-applies on
  palette change while a template is enabled.
- **Disabling a template undoes it.** An edit to another application's configuration must be
  reversible by the same toggle that made it. Turning a template off removes the file it generated
  and, for niri, removes the `include "sysc-shell.kdl"` line using the same ours-only line management
  the apply path uses; for gtk it restores `gtk-theme-name` only if the current value is still ours.
  Without this, disabling the niri template — or uninstalling the shell — leaves an include pointing
  at a file that no longer exists, and niri fails to load its configuration.

## IPC additions

- `osd.step` — params `{"kind":"audio|brightness","action":"up|down|mute"}`: steps via the service,
  shows the OSD, returns the new level.
- `panel.toggle|open|close` gain the `settings` panel ID.
- `status` gains service availability (audio/brightness devices present, templates enabled).

## Configuration

New fields (pointer wire types; edited through the settings UI itself):

- `osd`: `position` (8-corner + center token, default bottom-center).
- `theme.templates`: per-template enable map (default niri on).
- Existing 4A fields (`theme`, `accessibility`, `session`, `panels`, `bar`, widget options) are
  the settings UI's primary content.

## Tranche acceptance

4B-specific acceptance items (the milestone gate already passes on 4A surfaces):

| Item | Evidence |
|---|---|
| settings panel configures bar and widgets (roadmap) | live: change edge/height/items in settings, bar updates without restart |
| keyboard-only interaction covers every shipped control | roving-focus tests across toggle, slider, menu, text field, scroll area, virtual list |
| every interactive node carries an accessible name and role | node-data assertion tests for 4B controls |
| reduced-motion / high-contrast still honored | OSD and settings motion instant; tokens regenerated |
| OSD on each output, external changes detected | live: wpctl from a terminal fires OSD on all outputs |
| enforced theming live | enable niri template → focus-ring/border colors change without niri restart |
| stock themes | each seed generates a valid palette; fallback intact |
| config persistence | atomic write + reload; bad input never reaches the file |

## Risks and ceilings

1. **sysc-wayland release dependency**: text-input-v3 + cursor-shape-v1 bindings are a new
   sysc-wayland version; the plan sequences that first and pins it in go.mod.
2. **Polling services** (500 ms audio) miss sub-interval transients — accepted parity behavior.
3. **Zero-backlight desktop**: brightness path must be testable without hardware (fixture sysfs
   tree in tests; service reports unavailable cleanly).
4. **Include-injection safety**: grep-before-append, ours-only line management; a corrupted or
   missing config.kdl is reported, never rewritten. A read-only or externally managed `config.kdl`
   (nix, home-manager, a symlink into a store path) is reported as unavailable and the template is
   left disabled — the shell never fights a declarative configuration manager.
5. **App reload ceilings**: kitty via SIGUSR1; other apps need manual reload — documented per
   template.
6. **gtk theme switching** only when unset/ours — users keep their theme unless they opt in.
