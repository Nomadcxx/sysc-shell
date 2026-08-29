# Panels and Standard Controls — Research Notes (Milestone 4)

Per-element research: what Niri allows (verified in niri source/docs/live), what prior art
(Noctalia 5.0.0, DMS 1.5.3) does, and the decision taken. Every design element gets an entry
before it enters the design doc.

## Keyboard policy for panels

### What Niri allows (niri main branch source, verified 2026-08-30)

Keyboard focus precedence, `src/niri.rs` `update_keyboard_focus` (~line 1130):
exit-confirm > lock > screenshot-ui > MRU > popup grabs (Overlay, Top, Bottom, Background) >
**exclusive-or-on-demand on Overlay** > Top > on-demand Bottom/Background > **layout windows** >
exclusive Bottom/Background (only when no layout windows).

Facts:

1. **Exclusive on Overlay beats every window.** `excl_focus_on_layer(Layer::Overlay)` is checked
   before `layout_focus()`. While an exclusive overlay surface is mapped, clicking windows does not
   move keyboard focus.
2. **Fullscreen windows hide the Top layer but not Overlay** (wiki, Layer-Shell Components:
   "Only the overlay layer will show up on top of full-screen windows"). Panels must use Overlay.
3. **OnDemand surfaces get keyboard only when clicked** (`focus_layer_surface_if_on_demand`,
   `niri.rs:6135`); clicking anything else clears `layer_shell_on_demand_focus` and keyboard falls
   back to layout focus.
4. **Clicks still activate windows in the layout while keyboard is elsewhere**:
   `on_pointer_button` calls `layout.activate_window(&window)` for the window under cursor
   (`src/input/mod.rs` ~3026) regardless of keyboard focus. Niri's layout focus therefore always
   tracks the last-clicked window.
5. **Focus restoration is automatic.** When the exclusive surface is destroyed,
   `update_keyboard_focus` falls through to `layout_focus()` — the last window niri activated.
   No client-side restore is required for the common path.
6. **Compositor keybinds still work** while a layer surface holds exclusive keyboard (binds are
   processed before delivery; only the shortcut-inhibitor protocol can suppress them). Panel open
   does not dead the user's hotkeys.
7. Multiple exclusive surfaces on one layer: first in layer order wins (`find_map`); avoid by
   single-instance policy.
8. Bottom/Background layers get exclusive focus only when the workspace has no windows — panels
   must not use those layers.

### What prior art does

Noctalia (`src/shell/panel/panel_manager.cpp:43-77`, `resolvePanelKeyboardPlan`):

- The 100 ms Exclusive→OnDemand relax (`kKeyboardRelaxDelay`) applies ONLY on the Hyprland
  focus-grab path ("a focus grab maps Exclusive so the panel wins the keyboard from the grab and
  hands it back once settled"). It is not a general pattern.
- On compositors without focus-grab (Niri): each panel keeps its declared mode for the whole open
  time. Control-center = OnDemand (pointer-first); launcher/clipboard/session/wallpaper/polkit =
  Exclusive (keyboard-first). Attached panels: initial None → relaxed Exclusive after the
  bar-anchored map to avoid racing the reveal.
- Outside dismissal: click shield (transparent input-region surface) or Hyprland focus grab —
  `wantsOutsideDismiss = dismissOnOutsideClick && keyboardMode != None`.

DMS (`Common/KeyboardFocus.qml:11-26`):

- None during screenshot; customFocus override; inactive → None; Hyprland focus-grab → OnDemand
  (+ grab); **else Exclusive** — i.e. on Niri every popout/modal is Exclusive for its whole open
  time.
- Escape handled by a focused Keys.onPressed item in the popout; modals default
  `closeOnEscapeKey: true`.
- Explicit focus restoration (`captureActiveToplevel` / `toplevel.activate()`) — redundant on
  Niri given fact 5, harmless.

### Decision

1. Layer: **Overlay** for all interactive panels and OSD (facts 2, 8).
2. Every interactive panel gets a **click shield**: fullscreen transparent layer surface, keyboard
   none, input region = whole output, z-below the panel. Click on shield closes the panel; the
   first outside click is consumed (Noctalia behavior). Deterministic regardless of keyboard mode.
3. Keyboard interactivity: **Exclusive for all interactive panels while open** (DMS model).
   Escape-to-close always works; "only the open panel requests keyboard focus" (gate) is trivially
   true. Per-panel OnDemand demotion is a future config knob if the keyboard lock feels hostile in
   daily use — not built until needed.
4. OSD surfaces: keyboard **none**.
5. Focus restoration: **rely on Niri's automatic layout-focus fallthrough** (fact 5). No
   `focus-window` message on close. ponytail ceiling: if live testing finds a case where focus
   lands wrong (e.g. window closed while panel open), add explicit restore using the tracked
   `active_window_id` from the Niri stream + `niri msg action focus-window`.
6. Single instance per panel ID process-wide avoids exclusive-vs-exclusive contention (fact 7).

## Placement and geometry

### What Niri allows

- Standard wlr-layer-shell: anchor edges + margins from anchored edges; exclusive_zone -1 leaves
  the surface floating over windows without reserving space.
- **Niri does not animate layer surfaces.** Layer rules (Configuration: Layer Rules wiki, since
  25.01) offer opacity, shadow, geometry-corner-radius, background-effect (blur/xray/noise),
  block-out-from, place-within-backdrop — no open/close animation properties; the Animations
  config page has no layer entries. Reveal/dismiss motion is the shell's job.
- Layer rules match by `namespace` regex; `niri msg layers` lists open namespaces. Users can give
  our panels compositor-side shadow/radius/blur by matching our namespace — we should document a
  recommended layer-rule snippet instead of rendering blurred shadows ourselves.
- Focused output for hotkey-triggered panels: the Niri event stream's Window struct carries the
  output name (verified live in M3 audit), so the existing projection suffices.

### What prior art does

- DMS: popout anchored to triggering bar edge with gaps (`getPopupTriggerPosition`,
  SettingsData.qml:2200-2241); clamp `alignedX/alignedY = Math.max(gap, Math.min(screen - size -
  gap, desired))` (DankPopoutStandalone.qml:504-545); section-relative anchor
  `triggerSection left|center|right`; single instance per screen with close-on-other-screen.
- Noctalia: open-near-click anchorX/anchorY (bar.cpp:327-358), clampMargin keeps margins in
  [padding, output - panel - padding] with screenPadding=8 (panel_manager.cpp:436-450); output
  fallback chain focusedInteractiveOutput(1200ms) → probeFocusedOutput(250ms) →
  preferredInteractiveOutput(1200ms).

### Decision

1. Trigger sources: bar-widget click (we own the bar, so the triggering widget's rectangle in
   output coordinates is known at click time — no probing, unlike DMS) or IPC/hotkey (no pointer
   anchor → focused output from the Niri projection, bar-relative default).
2. Panel opens on the triggering bar's output; one instance per panel ID process-wide; re-trigger
   from another output closes and reopens there (DMS close-on-other-screen).
3. Anchor floating off the triggering bar edge, offset by a gap token; horizontal alignment
   follows the triggering widget's section (left/center/right) — DMS triggerSection pattern.
4. Clamp fully inside output minus padding token and the bar's reserved zone (DMS clamp pattern).
   Bar geometry comes from our own config — no runtime probing.
5. Open-near-click (Noctalia anchorX/anchorY) skipped — widget-rectangle anchoring is
   deterministic and simpler. ponytail ceiling: add pointer-anchored placement if a panel ever
   feels detached from its trigger.
6. Reveal/dismiss motion rendered by the shell (Niri won't), gated by the reduced-motion
   preference. Owner decision (2026-08-30): **the shell renders its own corner rounding and
   shadows** — do not rely on user-configured niri layer-rules. Implementation approach
   (pre-blurred shadow texture vs blur pass; SDF rounded-rect shader) is decided in the
   look/motion design section. Prior art: Noctalia blur radius 12 / alpha 0.55 directional
   shadows via wl_region; DMS 5-level elevation shader.

## Controls and text input

### What Niri allows

- **text-input-v3** (`TextInputManagerState`, niri.rs:295/2357) and **input-method-v2**
  (`InputMethodManagerState`, :296/2358, with IME popup surfaces via `InputMethodHandler`,
  handlers/mod.rs:240) — text fields get full IME support. Client side only needs text-input-v3;
  input-method-v2 is for IME engines, not the shell.
- **wp_cursor_shape_device_v1** (`CursorShapeManagerState`, niri.rs:344/2431) — I-beam over text
  fields.
- Scrolling: standard wl_pointer axis + discrete axis events. Virtualization is purely
  shell-side layout work.
- sysc-wayland today has generated bindings only for wayland-core, layer-shell,
  fractional-scale, viewporter, xdg-shell. **M4 must generate text-input-v3 and cursor-shape-v1
  bindings with sysc-wayland-scanner** (new protocol XML in sysc-wayland/protocols).

### What prior art does

- Noctalia: custom C++ controls incl. Input (TextInputClient), Select+dropdown popup, Segmented,
  Stepper, ScrollView + VirtualGridView/VirtualListView, RovingListNav (roving tabindex for
  sidebar/list keyboard nav), SplitPaneFocus (sidebar+content). Arrow-key nav in sidebars;
  Enter/Space activation; Escape at panel level.
- DMS: DankToggle/Slider/TextField/Dropdown/TabBar/ButtonGroup/FilterChips/NumberStepper/
  Flickable/ListView/GridView/Scrollbar, all QML-drawn; keyboard via focusHelper Items and
  Keys.onPressed; settings search with Up/Down highlight navigation.

### Decision

1. Control set = roadmap list, each enters with a named consumer: scroll area + virtual list
   (settings content), toggle (settings options), slider (settings numeric options), menu /
   dropdown (settings enum options), text field (settings search + string options), tabs
   (system-monitor per-resource views), graphs (system-monitor history from sysc-metrics
   samples), plus button/label/separator basics.
2. Text fields: single-line only (search + command strings); text-input-v3 with preedit render;
   cursor-shape-v1 I-beam. Multi-line editing deferred — no M4 consumer.
3. Keyboard model: roving focus per panel (Noctalia RovingListNav pattern) — Tab/Shift+Tab
   between controls, arrow keys inside composite controls (list, tabs), Space/Enter activate,
   Escape closes panel. Every focusable node carries accessible name + role data (AT-SPI export
   stays deferred, but the data exists at the gate).
4. Pointer: press/release matching + hit testing reuse M3's retained ui.Handle; wheel/axis events
   drive scroll areas.

## Services: metrics, volume, brightness, IPC

### What exists on this system

- `/usr/bin/wpctl` (WirePlumber CLI), `/usr/bin/brightnessctl`, `/usr/bin/pactl` all installed.
- No `/sys/class/backlight` device on this desktop — brightness path must handle zero devices
  gracefully (service reports unavailable; OSD/controls hidden).
- sysc-metrics API (verified): per-resource samplers, each `Sample() (XSnapshot, error)` —
  CPUSampler, MemorySampler, FilesystemSampler, BlockSampler, NetworkSampler, UptimeSampler.
  Sequential polling by ONE owner, no internal goroutines; first/discontinuous samples carry
  Valid==false; cadence/caching/presentation are the consumer's job.

### What prior art does

- Noctalia: embedded libwireplumber (`WirePlumberMixer`, application_services.cpp:1270) —
  event-driven, C-only. Brightness via sysfs/backlight helpers.
- DMS: Quickshell built-in `Audio` (event-driven, QML-native); OSD shown on ANY volume change
  regardless of who changed it; brightness via backlight sysfs.
- Both show OSD on external changes, not only on their own key handling — the shell must detect
  changes made by other clients.

### Decision

1. **AudioService** (lease-counted, Clock pattern): while leased, poll
   `wpctl get-volume @DEFAULT_AUDIO_SINK@` (~500 ms); expose level/muted; Step/Set/SetMute via
   `wpctl set-volume`; any detected delta emits a change event → OSD. wpctl absent → service
   unavailable, audio UI hidden. PipeWire wire protocol / cgo libwireplumber rejected as
   disproportionate. ponytail ceiling: polling misses sub-interval transient changes; upgrade to
   an event source only if that matters.
2. **BrightnessService** (lease-counted): read `/sys/class/backlight/*/brightness` +
   `max_brightness` directly (no exec for reads); step via `brightnessctl set ±N%`; delta → OSD.
   Zero backlight devices → unavailable.
3. **MetricsService** (lease-counted): owns the sysc-metrics samplers; polls ~1 s while leased
   (system-monitor acquires while open); keeps a process-lifetime ring buffer (~2 min) so history
   survives panel close/open; Valid==false first samples render as "collecting". Sampling pauses
   with zero leases. ponytail ceiling: no history before first open in a session.
4. **IPC**: versioned Unix socket from day one (sysc-notify/sysc-tray plug into the same seam).
   Socket under $XDG_RUNTIME_DIR with a version marker; newline-delimited JSON
   {id, method, params} → {id, ok | error}; 0700 perms, same-user. M4 verbs: panel
   toggle/open/close, osd show, status. Prior art: niri msg IPC (JSON over Unix socket); DMS
   `dms ipc call <target> open|close|toggle`. Hotkeys = documented niri keybinds spawning
   `sysc-shell ipc ...` (DMS pattern — compositor owns keys, shell owns panels).

### Open items

- Live-verify fact 5 (auto-restore) on the installed Niri 26.04 during the M4 gate; the source
  read is from main.
- Click shield must not reserve space (exclusive_zone -1) and must not take keyboard (none), or
  it violates facts 1/6 itself.

## Theming (Section 6)

### Niri / system capability
- Theming is client-side; no compositor involvement. Preferences (dark/light, high contrast, reduced motion) are shell config written by the settings modal.
- sysc-shell renderer is a software canvas: fillRect + blendMask(*image.Alpha, x, y, color) (internal/render/canvas.go:58,73). Rounded corners and shadows are alpha-mask compositing problems; no GLSL/shader path exists or is needed.

### matugen (verified live, 4.2.0)
- Pipeline proven end-to-end: `matugen image -c config.toml <wallpaper> --prefer saturation` with template `{{colors.<token>.<dark|light>.hex}}` → rendered JSON file. Config fields are `input_path`/`output_path` (NOT input/output).
- `--prefer` required non-interactively: darkness|lightness|saturation|less-saturation|value|closest-to-fallback.
- `--contrast -1..1` verified: contrast=1 pushes on_surface to #ffffff, raises outline lightness — real contrast increase.
- `matugen color hex <HEX>` exists for solid-color sources (no wallpaper).
- Token namespace = standard Material3: 46 tokens incl. surface_container{,_low,_high,_lowest}, surface_bright/dim, outline_variant, inverse_*, scrim, shadow (token list cross-checked against DMS dank.json template, 116 placeholders).

### Prior art
- DMS: `dms matugen queue` worker binary wraps matugen (Theme.qml:1673 setDesiredTheme); colors JSON at $XDG_CACHE_HOME/DankMaterialShell/dms-colors.json with shape {colors:{dark:{token:hex},light:{...}}}; scheme default scheme-tonal-spot; matugenContrast setting → --contrast; stockColors fallback palette when matugen absent or wallpaper is hex; worker serialized (one run at a time, queue pending).
- Noctalia: 16 ColorRoles, dark/light via ThemeService, palette lerp crossfade on theme change.

### Decisions (Section 6)
- Shell owns one minimal embedded matugen config+template (~20 tokens × dark/light) → $XDG_CACHE_HOME/sysc-shell/colors.json; regenerate on startup and on theme-source config change; single-flight (ignore re-trigger while running, rerun after).
- Scheme type setting (default scheme-tonal-spot), dark/light setting (default dark; auto/portal deferred), high-contrast setting → --contrast 1 regeneration, reduced-motion setting → skip reveal/dismiss animation.
- Fallback stock palette compiled in (seeded from current ProofStyle values) when matugen missing/fails.
- Shadows: Go-generated pre-blurred rounded-rect alpha textures cached per size; drawn via blendMask. One panel elevation + one menu elevation (DMS 5-level scale = YAGNI).
- Corner rounding: SDF rounded-rect alpha mask cached per radius; tokens panel=12, control=6.
- Motion: fade + 8px slide from triggering bar edge, ~150ms ease-out; instant under reduced-motion.

## Theming addendum — enforced app theming + stock themes (owner request)

### Noctalia mechanism (verified in source)
- Template catalog `assets/templates/builtin.toml`: ~20 apps — terminals (alacritty, foot, ghostty, kitty, wezterm), compositors (niri, hyprland, labwc, sway, mango), system (gtk3, gtk4, qt, kcolorscheme), editors (emacs, helix), misc (btop, cava, starship, scroll).
- Each template dir = template file with `{{colors.<token>.<default|dark|light>.hex}}` placeholders + `apply.sh` with `output` (prints destination path) and `apply` (post-hook) commands.
- **niri**: renders `~/.config/niri/noctalia.kdl` containing focus-ring/border/shadow/tab-indicator/insert-hint colors; apply.sh idempotently appends `include "noctalia.kdl"` to user's config.kdl. niri hot-reloads config → window borders update live. This is the "eldritch theme applied to window borders" path.
- **kitty**: renders kitty.conf fragment + apply.sh include injection.
- TemplateApplyService (src/theme/template_apply_service.h): worker thread, single-flight, generation-counter supersede, reapplyLast on restart, IPC-registered.
- Stock themes: 10 built-in palettes (Ayu, Catppuccin, Dracula, Eldritch, Gruvbox, Kanagawa, Noctalia, Nord, Rosé Pine, Tokyo-Night), each dark+light, hand-written 16-role Palette PLUS TerminalPalette (ANSI16 + fg/bg/selection/cursor). Community palettes = downloaded JSON catalogs (skip).

### DMS mechanism (verified)
- Same idea via matugen per-app configs (matugen/configs/{niri,gtk3-dark,gtk3-light,kitty,foot,qt5ct,qt6ct,firefox,...}.toml) run by `dms matugen queue` worker; per-template enable toggles in settings (runDmsMatugenTemplates + matugenTemplate*).
- Stock themes: StockThemes.js = 10 color families (Blue, Purple, Green, Orange, Red, Cyan, Pink, Amber, Coral, Monochrome) × dark/light; each is just a primary seed → buildMatugenColorsFromTheme (Theme.qml:1882) builds full matugen-shape colors JSON → same token pipeline as wallpaper-generated.

### Decisions (Section 6 addendum)
- Theme source setting: wallpaper image | hex seed | named stock theme. All three flow through matugen (`image` / `color hex`) → same colors.json shape → one downstream pipeline.
- Stock themes = curated seed hexes through matugen (DMS approach), NOT hand-written full palettes (Noctalia approach) — matugen generates dark/light + all tokens from the seed. Ship ~8 named seeds. Compiled-in fallback palette only when matugen absent.
- OWNER DECISION: **full catalog parity minus unsupported compositors** (owner: niri-only shell — hyprland/sway/mango/labwc REMOVED) — ship ~16 templates: terminals alacritty/foot/ghostty/kitty/wezterm; compositor niri; system gtk3/gtk4/qt/kcolorscheme; editors emacs/helix; misc btop/cava/starship/scroll. Noctalia assets are MIT → port template files with attribution NOTICE.
- Template engine: Go text/template over nested color map — Noctalia's `{{colors.<token>.<mode>.hex}}` syntax renders verbatim via Go template map field chaining. Catalog manifest (id/name/category/output-path/apply-hook) embedded via go:embed.
- Apply hooks ported from bash to Go (no runtime shell): behaviors = plain write, include-injection (niri/kitty et al: idempotent append of include line, ours-named file sysc-shell.kdl/sysc-shell.conf), theme-dir write (gtk). Never overwrite user content.
- Reload: niri hot-reloads config automatically; kitty SIGUSR1; others documented as manual-reload ceiling.
- Per-template enable toggles in settings (Templates section). Defaults: niri on (we are a niri shell), all others off — writing into other apps' configs is opt-in.
- Stock themes named as color families (DMS-style honest naming: Blue/Purple/Green/Orange/Red/Cyan/Pink/Amber/Coral/Monochrome), since they are seeds through matugen, not exact named palettes like Dracula.
- Deferred: community/downloaded palettes, terminal ANSI16 derivation beyond M3-token mapping.

## OSD surfaces (Section 7)

### Niri capability
- Verified: niri source has NO OSD, volume, or brightness handling anywhere (grep of niri.rs + input/mod.rs = zero hits). Keybinds spawn commands; the shell owns OSD entirely.
- Layer-shell facts already established: Overlay layer visible over fullscreen; keyboard none surfaces never take focus; exclusive_zone -1.

### Prior art
- Noctalia: per-output OSD overlays, Overlay layer, keyboard None, exclusive 0, namespace noctalia-osd; position = 8-corner vocabulary + center (effectiveOsdPosition); triggers: volume/mic/brightness/caps/media/etc.
- DMS: DankOSD (dms:osd, Overlay, -1, None); osdPosition enum default BottomCenter incl. vertical variants; offsets honor all bars + dock; OSDManager cleans stale OSDs 3s after screensChanged.

### Decisions (Section 7)
- OSD surface: Overlay, keyboard none, exclusive_zone -1, no shield. Created on demand, destroyed after timeout (not resident).
- Shown on ALL outputs with a bar (both prior arts are per-output; volume/brightness is global state).
- Position: config token, 8 corners + center, default bottom-center; offset by bar reserved zone + padding token; bar geometry from own config.
- Behavior: ~1.5s visibility, timer resets on repeated changes; reveal/dismiss fade+slide gated by reduced-motion; content = icon glyph + label + level bar.
- Triggers: AudioService/BrightnessService change events (incl. externally-detected changes via polling) + IPC `osd` verb.
- Hotkeys: documented niri keybinds — XF86AudioRaiseVolume/LowerVolume/Mute, XF86MonBrightnessUp/Down → `sysc-shell ipc osd <audio|brightness> <up|down|mute>`; shell steps the value through the service and shows the OSD.
