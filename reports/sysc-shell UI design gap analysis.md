# sysc-shell's UI gaps against Noctalia, DMS, caelestia

**Executive verdict.** sysc-shell is at functional parity with Noctalia v5, DankMaterialShell, and caelestia on its core surfaces — bar composition, notification toasts, tray, wallpaper, and the token-driven theming spine — and ahead of at least one reference on blur, video wallpapers, swipe-to-dismiss notifications, and motion discipline. It trails on **breadth and finish**, not architecture: two shipped features are broken (the icon resolver drops **37% of application icons** for lack of SVG support, sysc-173; theming template apply is a stub that **overwrites real app configs**, sysc-405), four daily-driver subsystems do not exist at all (idle, screenshot, night light, dock — all parked as undesigned M9 sub-project E), and the micro-interaction layer that the references get structurally from shared primitives (press-point ripples, state layers, shape morphing) is absent. Most of the gap is already tracked in bd, and the two largest single items are correctness bugs in shipped features rather than new features. True parity is also smaller than it looks: the policy constraints remove the lock screen, greeter, multi-compositor support, and QML/plugin compatibility from the comparison entirely.

## Reading the matrix: what built, minimal, and missing mean here

Statuses carry the sysc notes' own three-way distinction: **built** (implemented and rendered), **minimal** (built but visibly thinner than the reference), **designed** (design doc landed on main, code not), **planned** (tracked in bd, not yet designed), **missing** (no evidence in notes or bd), **broken** (built but defective), **policy** (excluded by AGENTS.md, not a gap), **unverified** (notes could not confirm). Reference-shell cells reflect their main branches as of September 2026. sysc-shell evidence is repo commit `620cc52`.

### Bar and panel

| Capability | sysc-shell | Noctalia | DMS | caelestia |
|---|---|---|---|---|
| Widget roster breadth | built (~19 types) | built (33) | built (~31) | built (9 modules) |
| Bar edges beyond top | missing (top only, others rejected at load) | built (4 edges) | built (4 + vertical axis) | built (vertical-only by design) |
| Auto-hide (hover/smart/drag) | missing (sysc-587, undesigned) | built (3 modes) | built (hover controller) | built (hover + drag thresholds) |
| Capsule groups + accordion | minimal (group capsule; no accordion) | built (groups + accordion unfold) | built (segments, pill surfaces) | N/A (stacked rail) |
| Per-widget gesture bindings + dead zone | minimal (fixed click/wheel per widget) | built (9 gestures, 4-layer resolution) | minimal (wheel on some widgets) | built (screen-half scroll zones) |
| Bar layer above fullscreen | missing | built (`layer = overlay`) | unverified | unverified |
| Concave corners / edge flare | missing | built | built (SDF Frame ring) | built (blob border) |
| Bar surface styles (rim, shadow, frame, hover) | designed (sysc-587, sysc-585/546; blur sysc-551 in progress) | built | built | built |
| Settings-side bar editor | built (drag-drop, drop resolution, keyboard move) | built | built (drag-add + per-widget option pages) | minimal (config pages in Nexus) |
| Per-monitor bar overrides | built (per-output surfaces) | built | built (`screenPreferences`) | built (+ `excludedScreens`) |

### Control centre and quick settings

| Capability | sysc-shell | Noctalia | DMS | caelestia |
|---|---|---|---|---|
| Control centre panel | built (11 sections; spine unfinished, sysc-154/253) | built (12 tabs) | built | built (dashboard + utilities split) |
| Editable tile/shortcut grid | missing | built (6-slot grid, 16 tile types) | built (edit chrome, tile library) | minimal (quick-toggles card) |
| Per-app audio streams | unverified | built | built | built |
| Network depth (802.1X, VPN, cellular) | minimal | built (802.1X forms, cellular) | built (VPN tile, wifi modals) | built (wifi list, VPN add page) |
| Screen-time usage charts | missing | built | missing | missing |
| User/avatar card | missing | built | unverified | built (`~/.face`) |

### Notifications

| Capability | sysc-shell | Noctalia | DMS | caelestia |
|---|---|---|---|---|
| Toast cards (actions, inline reply, images, urgency) | built | built | built | built |
| Swipe-to-dismiss | built (35% commit, intent-then-delta) | **missing** (explicit absence) | built (spring stack reflow) | built (+ drag-to-expand) |
| Hover-pause countdown | missing | built (pauses timer + bar animates back) | minimal (hover tracking) | built |
| Grouping + unread badge | built (centre groups, bar badge) | minimal (history list) | unverified | built (+N badge pop) |
| Per-app filter rules UI | missing | built (per-sender rules, regex, bypass DND) | built (rules tab) | missing |
| DND timed presets | built | built | built (15 min–8 h) | minimal (toggle + toast) |
| Centre visual stratification | designed (sysc-233 blocked by sysc-231/232; sysc-240 in progress) | built | built | built |
| Keyboard-navigable centre + hint bars | unverified | minimal | built (controller + hints) | minimal |

### Launcher

| Capability | sysc-shell | Noctalia | DMS | caelestia |
|---|---|---|---|---|
| Fuzzy ranking + usage history | built (pinned `sysc-launch`) | built | built | built |
| Calculator provider | planned (sysc-79) | built (libqalculate) | built | built |
| Emoji provider | planned (sysc-80) | built | built | missing |
| Windows provider | planned (sysc-81) | built (`/win`) | built | missing |
| File search | missing | missing (explicit absence) | built (danksearch) | missing |
| App grid + category chips | planned (sysc-83) | built | built (app drawer, sections) | missing |
| Searchable desktop actions | planned (sysc-84) | built | built | missing |
| Prefix providers / dmenu mode | missing | built (`/calc`, `/emo`, `/pan`, dmenu) | built (pluggable sources) | built (`>` action grammar) |
| Real theme icons in results | planned (sysc-117; blocked by sysc-173) | built | built | built |

### Power, session, OSDs

| Capability | sysc-shell | Noctalia | DMS | caelestia |
|---|---|---|---|---|
| Session panel + power actions | built (battery card, profiles, actions) | built | built | built |
| Hold-to-confirm / countdown on destructive actions | missing | built (countdown ring + scrim) | built (hold-to-confirm, key-release wake) | minimal (config-gated dangerous actions) |
| Polkit agent dialog | missing | built | built | missing |
| Lock screen / greeter | policy | built | built | built |
| Audio/brightness OSD | built (220×64, 1.5 s, shared animator) | built | built | built |
| OSD kind breadth | minimal (2 kinds) | built (14 kinds) | built (11 variants) | minimal (3 sliders + toasts for the rest) |
| OSD serialization / panel coordination | built (one shared reveal animator) | unverified | built (`OSDManager` serialization) | built (shift + clamp against other panels) |
| Interactive OSD (wheel/drag on the OSD itself) | missing | minimal | minimal | built (wheel + drag, icon-in-handle) |
| Over-limit / muted visual states | minimal | built | built | built |

### Wallpaper and theming

| Capability | sysc-shell | Noctalia | DMS | caelestia |
|---|---|---|---|---|
| Engine + per-output assignment | built (gSlapper, stills **and video**) | built | built | built |
| Picker (search, thumbnails, folders) | built | built (favorites, monitor selector) | built (dash tab) | built (Nexus + launcher carousel) |
| Live scheme preview before commit | missing | minimal | minimal | **built** (signature: preview regenerates M3 palette) |
| Video wallpapers | built | missing | built | missing |
| Slideshow / cycling | missing | built | built (5 s–12 h, time-of-day) | missing |
| Wallpaper transition effects | minimal (cross-fade) | built (6-effect GPU pool, feathering) | minimal | minimal (crossfade) |
| Depth/desktop clock overlay | designed (sysc-499 blocked by sysc-505 calibration) | built (desktop widgets) | built | built (9 anchors, blurred plate) |
| M3 palette from wallpaper (matugen) | built | built | built | built |
| Stock palettes selectable in settings | broken (sysc-107: palettes exist, unselectable) | built (10) | built (20) | built (flavours/variants) |
| Dark/light/auto + independent shell mode | minimal (auto-follow unverified) | built (sunrise/sunset auto, `shell_mode`) | built | built |
| App-theming templates | broken (16 templates; apply is a stub overwriting configs, sysc-405) | built (~20 + community + hooks) | built (matugen → GTK/Qt/terminals) | built (CLI-mediated) |
| SVG icon support | missing (sysc-173) | built (bundled Tabler font + alias layer) | built (`DankSVGIcon` + system themes) | built (Material Symbols variable) |
| Single accent-color control | missing | built | built | minimal (flavour/variant only) |
| Radius system | built (two ladders, measured off Noctalia v4.7.7) | built (`corner_radius_scale`) | built (`radiusStrength` 0–100) | built (token rounding scale) |
| Blur behind surfaces | built (screencopy + CPU box blur, ~11 ms/open) | **missing** (alpha only) | built | built (Hyprland rule sync) |
| Luminance-aware transparency | missing | missing | missing | **built** (signature) |
| App icon colorization | missing | built | unverified | built (tray recolour) |

### Motion, typography, icons

| Capability | sysc-shell | Noctalia | DMS | caelestia |
|---|---|---|---|---|
| Duration/easing token system | built (33 ms cap, EaseOutCubic/Quart) | built (100/200/400 ms, 7 easings) | built (M3 curves + springs) | built (M3 Expressive, 14 semantic types) |
| Spring physics | missing | missing (explicit absence) | **built** (`SpringMotion`, substepped, reduced-motion aware) | missing (overshoot béziers instead) |
| Reduced-motion handling | built (first-class setting, 150 ms absolute cap) | built (global enable + speed) | built (respects system setting) | missing |
| Panel open/close motion | minimal (tokens exist; live behavior unverified) | built (directional clip reveal) | built | built (blob deformation) |
| Frame-callback-driven animation | broken (sysc-454: ticker-driven) | built | built | built |
| Declarative value animation | designed (2026-09-21 design; no consumers yet) | minimal | built (`NumericText`, list transitions) | minimal |
| Type scale | built (pt→logical ×4/3) | built (5 steps) | built (5 steps) | built (roles + variable axes) |
| Variable-font axes (width/roundness/fill) | missing | missing | missing | **built** (Google Sans Flex ROND/wdth, icon fill/grade) |
| Marquee for overflow text | built | built | built | minimal (crossfade instead) |
| Animated numerals | missing | missing | built | missing |
| Icon pipeline breadth | minimal (raster-only resolver + Material chrome subset) | built | built (4 sources + picker) | built |
| i18n / RTL mirroring | missing | built (27 catalogs, full RTL) | built | built |

### Peripheral widgets and settings app

| Capability | sysc-shell | Noctalia | DMS | caelestia |
|---|---|---|---|---|
| Dock | missing (M9-E, undesigned) | built (magnification, drag-reorder) | built (3 chrome systems incl. island) | N/A (the rail is the dock) |
| Desktop widgets + editor | planned (sysc-206, undesigned) | built (11 types + full editor) | built | minimal (clock + visualiser) |
| Clipboard history | built (sysc-535 upstream release open) | built (encrypted at rest, previews) | built (image previews, pins) | missing |
| Media richness (art, lyrics, chromes) | minimal | built (circular art) | built (3 chromes, lyrics, accent-from-art) | built (wavy ring, lyrics) |
| Audio visualizer | planned (deferred by parity design) | built (16-band PipeWire) | built (cava) | built (cava, auto-hide on tiled windows) |
| Screen recorder | built (plugin panel; attached panel designed, sysc-138) | plugin | missing | built (card + recording list) |
| Screenshot tool | missing (M9-E) | built (region select + annotation editor + cursor capture) | missing | built (area picker, delegates to swappy) |
| System monitor | built (drill-downs blocked upstream, sysc-536/537) | built | built (process manager) | built (performance tab) |
| Night light | missing (M9-E) | built (gamma + sunrise fade) | built (tile + schedule) | missing |
| Idle management | missing (no idle code; M7 candidate, M9-E undesigned) | built (ordered behaviors + pre-action fade) | built | built (ordered timeouts, inhibitors) |
| Privacy indicator (mic/camera/capture) | missing | built | built | minimal (mic status icon) |
| Keyboard-layout / lock-key widgets | missing | built | built | built (status icons + toasts) |
| KDE Connect / phone | planned (sysc-420, blocked by 17 deps) | missing | missing | missing |
| Niri overview type-to-launch | missing (untracked) | built | built (`NiriOverviewOverlay`) | N/A (Hyprland) |
| Settings surface + search | built (registry-driven, 8+ sections, search) | built (24 sections, toplevel window) | built (~60 tabs, alias search + deep-link) | built (Nexus, 11 pages, detachable) |
| Plugin store UI | designed/in progress (sysc-508, sysc-580) | built | built (registry + commit-pinned lockfile) | built |
| Config export (GUI state → declarative) | missing | built (merged TOML export) | missing | minimal (token files) |
| First-run wizard | missing | built | built | missing |
| Keybind recorder GUI | policy (compositor owns bindings) | built | built | N/A |

### Polish micro-interactions

| Capability | sysc-shell | Noctalia | DMS | caelestia |
|---|---|---|---|---|
| Press-point ripple | missing | missing (explicit absence) | built (`DankRipple`) | built (`StateLayer`, 600 ms) |
| Hover state layers / tints | minimal (deliberately dropped on bar: "the bar is furniture") | built (`hover_highlight`) | built (`StateLayer` everywhere) | built |
| Shape morphing as state | missing | missing | built (M3 shape catalog) | **built** (signature: pressed/checked/focused shapes) |
| Wavy progress (alive-while-playing) | missing | missing | built (`M3WaveProgress`) | built (rings + sliders pause when paused) |
| Elevation/shadow tokens | minimal (setting exists; shadows in sysc-587) | built (9 directions, contact shadow) | built (`ElevationShadow`) | built (dp ladder, animated) |
| Panel coordination (clamp/shift/exclude) | missing | minimal | minimal | **built** (signature) |
| Text/image crossfade on change | missing | minimal | minimal | built (`StyledText.animate`, `FadeImage`) |
| Empty states with personality | unverified | unverified | built (`CcEmptyState`) | built (dino illustration) |
| Keyboard hint bars | missing | minimal | built (clipboard + notification hints) | minimal |
| Tooltips | built (500 ms dwell, own surface) | built (side-aware, suppressed under panels) | built (delay + positioning) | **missing** (explicit absence) |
| Key repeat in text fields | broken (sysc-171) | built | built | built |
| UI feedback sounds | missing | minimal (volume earcon) | built (sounds settings category) | missing |
| Overlay scrollbar hover-expand | unverified | built (6→12 px, no reflow) | built | built |
| Sliding list highlight / row transitions | missing | minimal | built | built (highlight slides between launcher rows) |

## Seven critical gaps block daily-driver parity

Ranked by user impact. Each names what the references do, why it matters, and whether sysc-shell has it planned.

**1. SVG icon resolution (sysc-173, open).** The resolver searches png/xpm only; a theme offering only SVG yields no file, dropping **37% of application icons**. Noctalia sidesteps the problem with a bundled Tabler glyph font plus alias layer; DMS runs a four-source pipeline (Material Symbols, system themes, Nerd Fonts, SVG); caelestia uses variable Material Symbols. Icons are the first thing a user sees in the launcher, running-apps capsule, and tray on every single interaction — glyph fallbacks read as unfinished. sysc-117 (real theme icons in launcher results) and sysc-186 (running-apps/tray icon resolution) are both blocked behind it. Planned: yes, open in bd; needs one pinned Go SVG rasterizer.

**2. App-theming template apply overwrites real configs (sysc-405, open P1).** All 16 embedded templates (gtk3/gtk4, kitty, foot, wezterm, niri, …) exist with apply/unapply, but apply is a stub that clobbers user config files. Noctalia ships ~20 templates plus community ones with a hook runner; DMS pushes matugen output into GTK, Qt, terminals, and editors. This is the one gap that destroys user data, and it breaks the shell's headline promise — one wallpaper-derived palette theming the whole desktop. Planned: yes, P1 in bd.

**3. Idle management does not exist.** No idle code anywhere in the tree; `ext_idle_notifier_v1`/`zwp_idle_inhibit_manager_v1` are listed as M7 protocol candidates and idle behaviour is M9 sub-project E, "Not designed." All three references ship it: Noctalia has ordered behaviors with a **pre-action fade** (a click-through tint fades in before lock/suspend so activity can cancel), caelestia has ordered timeouts with inhibitors and return actions, DMS has an idle service with AC/battery-split sleep. A desktop that never blanks or suspends is not a daily driver. The existing caffeine toggle inhibits nothing. Planned: roadmap M7/M9-E, undesigned.

**4. Screenshot tool does not exist (M9-E, undesigned).** Noctalia has the deepest version: dimmed region select with a size badge, edge/corner resize, multi-monitor display picker, `ext-image-copy-capture` cursor compositing, and a built-in annotation editor. caelestia has a per-screen area picker with freeze and clipboard-only modes, delegating annotation to swappy. DMS is also missing it — so this is a two-of-three gap, but the two that have it demonstrate it is table stakes. Planned: M9-E, undesigned.

**5. Bar auto-hide and bar surface finish (sysc-587 open and undesigned; sysc-585/546 epic; sysc-551 blur Phase E in progress).** Noctalia has three auto-hide modes (pointer, smart-when-workspace-empty, IPC), concave edge corners, contact shadow at the panel seam, and an overlay layer for peeking above fullscreen; DMS has a hover-reveal controller and its Frame/island chrome; caelestia has edge-hover plus drag with per-panel thresholds. The bar is the shell's most visible surface and sysc's is always-on, top-only, and missing rim/shadow/hover treatment. Planned: yes — sysc-587 exists precisely to design these; sysc-546 is ready.

**6. Notification centre visual stratification (sysc-233, blocked by sysc-231/232; sysc-240 in progress).** The 2026-09-07 polish design — three-level surface ladder, circular header buttons, per-entry dismiss — explicitly says the shipped centre is "not readable… nothing separates." Everything functional is at parity (groups, tabs, DND presets, swipe, inline reply); the hub just looks unstratified. Blocked on an upstream `history.remove` (sysc-231) for per-entry dismiss. Planned: designed, awaiting unblock.

**7. Launcher breadth (sysc-79/80/81/83/84/117, all open).** Calculator, emoji, windows provider, app grid with category chips, and searchable desktop actions are all planned-not-built. Noctalia has every one plus prefix providers and dmenu mode; DMS has pluggable sources with source badges; caelestia multiplexes a `>` action grammar in one field. The launcher is the shell's front door, and every missing provider is a reflex users reach for daily. Planned: yes, six open issues.

Two further items sit just below the cut: **OSD breadth** (2 kinds vs Noctalia's 14 and DMS's 11 — media, lock-keys, keyboard layout, DND, privacy are all missing, untracked) and **night light** (M9-E; Noctalia and DMS both ship it). **Per-app notification rules** (Noctalia's filter system, DMS's rules tab) are missing entirely and untracked.

## Polish gaps: the micro-interaction layer

The references' polish is **structural, not per-surface**: one ripple, one state layer, one tooltip, one elevation token, applied everywhere by shared components. sysc-shell's node vocabulary (`internal/ui/tree.go`, 26 node kinds) is the natural place to add these as nodes so every widget inherits them.

**Press and hover feedback.** Press-point ripples expanding to the farthest corner — DMS's `DankRipple` and caelestia's `StateLayer` (600 ms, hover overlay at 0.08 alpha) demonstrate it; Noctalia has none, so this is a two-of-three differentiator. Hover tints that light only the hovered group member (Noctalia `hover_highlight`, animated EaseOutCubic). Shape-morph-on-press (radius large→small while pressed, caelestia `ButtonBase`). sysc has press scale 0.98 and deliberately no bar hover wash — the philosophy is defensible for the bar, but panels and popouts have no state language at all. Key repeat is broken everywhere text is typed (sysc-171).

**Shape and geometry.** Concave screen-edge corners that make the bar flare into the screen (Noctalia, per-position table). M3 shape morphing as the state language — the focused workspace takes a random cookie/burst shape, usage levels morph an M3 shape (caelestia `Workspace.qml`, DMS `MaterialShapes.js`). Wavy progress that ripples only while media plays (caelestia; DMS `M3WaveProgress`). Blob panel deformation — panels stretch toward the bar as they open (caelestia, `deformAmount` 0.03–0.25), the hardest visual to replicate and the most distinctive.

**Motion refinement.** Spring physics for layout-affecting motion — stacks reflow with stiffness/damping, notifications spring-follow the dismissed card (DMS `SpringMotion`; Noctalia and caelestia lack it too). Directional clip reveal for attached panels, direction taken from the bar side (Noctalia `panel_manager.cpp`). Text crossfade on every value change and image crossfade on load (caelestia `StyledText.animate`/`FadeImage`). Animated numerals (DMS `NumericText`). Two of sysc's own motion defects are polish gaps: ticker-driven animation instead of frame callbacks (sysc-454) and workspace pills rebuilding the row instead of retargeting (sysc-414). The declarative value-animation design (2026-09-21) has no consumers yet.

**Notification micro-interactions.** Hover-pause on the countdown with the urgency-colored top bar animating back (Noctalia; caelestia stops the expire timer on hover). Drag-to-expand revealing full markdown body and actions (caelestia). Collapse-on-dismiss stacking so remaining toasts slide to close gaps (Noctalia). +N group badge with scale/opacity pop (caelestia `ExtraIndicator`). Keyboard hint bars under navigable lists (DMS).

**Surfaces and lists.** Elevation as an animated dp ladder (caelestia) and nine-direction shadows with a contact shadow at the bar/panel seam (Noctalia). Panel coordination — the toast stack clamps against the OSD/session panels, the OSD shifts left when the sidebar opens (caelestia; sysc surfaces currently ignore each other). Overlay scrollbars that widen on hover without reflowing content (Noctalia). Vertical fade masks on scrollable lists (caelestia) — sysc's `KindEdgeFade` already does the bar variant, so this is extension, not new ground.

**OSD micro-interactions.** Value-reactive icon inside the slider handle (volume glyphs per level, `brightness_1..7` crossing sixths) and the handle icon crossfading into the percentage while dragging (caelestia `FilledSlider`) — the single most-copied micro-interaction in the set. Distinct over-limit (>100%) and muted states (Noctalia, DMS). A volume earcon with cooldown (Noctalia).

**Smaller items.** Side-aware tooltip placement that never covers other widgets, suppressed while a panel is open (Noctalia — sysc's 500 ms dwell tooltips are at par on mechanism, not on placement intelligence). Empty states with personality (caelestia's dino, DMS's `CcEmptyState`). Sliding highlight between launcher rows (caelestia) and readline editing keys (Noctalia). Source badges on launcher results (DMS). Clock digit anti-jitter via variable-font width scaling (caelestia — sysc's tabular figures already solve this). Live wallpaper→scheme preview in the picker (caelestia's signature; Noctalia's Theme/Colors row is a weaker version). Niri overview type-to-launch — a tiny keyboard-focus layer so typing in the overview opens the launcher pre-filled (Noctalia `overview_launcher_capture.cpp`, DMS `NiriOverviewOverlay`); Niri-specific, therefore in scope, and untracked in bd.

## Where sysc-shell is at par or ahead

Honest accounting, since the gap list is long. **Blur**: sysc renders backdrop blur in-shell (screencopy capture, quarter-resolution CPU box blur, measured ~11 ms per open) — Noctalia has no blur-behind at all, and sysc's is user-tunable per surface class. **Motion discipline**: a 33 ms frame cap, no continuous paint except a visible looping gradient, reduced-motion and high-contrast as first-class behavior-changing settings — stricter than any reference; Noctalia's global kill switch is the only comparable control. **Measured token parity**: the spacing ladder and dual radius ladders were re-based against measured Noctalia v4.7.7 values, not eyeballed. **Wallpaper**: video wallpapers (Noctalia lacks them), depth masks feeding a depth-clock overlay (unique among the four), pause-when-occluded, thumbnail worker, and the wallpaper doubling as the matugen seed — a tighter theme integration than DMS's. **Notifications**: functionally at or above parity — swipe-to-dismiss with intent-then-delta (Noctalia has no swipe), inline reply, images, styled body runs, urgency theming, DND timed presets, grouping, per-output presentation aggregation. **Tray**: full DBusMenu surfaces, overflow drawer with pin/unpin, wheel scroll, service-owned termination — complete against all three. **Bar editor and settings breadth**: drag-and-drop lane editing with keyboard move, and density/opacity/blur/elevation/motion as first-class settings axes — matches or exceeds the references' editors. **Weather**: sysc renders actual animated scene effects (rain, snow, drift) behind the hero card; Noctalia's "effects" is only a forecast-visuals toggle. **Accessibility**: keyboard-only operation and accessible name/role are enforced gates today, where the references are mixed. **Overflow**: the edge-fade signal and tray drawer are a cleaner story than any reference's.

## Out of scope by policy, not by omission

These reference features are excluded by AGENTS.md and the approved design, and must not be scored as gaps. **Lock screen and greeter** — all three references ship one (Noctalia's with widgets and fingerprint, DMS's with PAM and greetd, caelestia's with weather and fetch); sysc ships only an owner-supplied `session.locker` command setting, which is the policy-compliant ceiling. **Multi-compositor support** — DMS targets six compositors, caelestia is Hyprland-coupled (focus grab, blur-rule sync); sysc is Niri-first by constraint. **QML/Noctalia/DMS plugin and config compatibility** — DMS's plugin registry format and Noctalia's plugin-id scheme are irrelevant; sysc runs its own `plugin/v1` capability-declaring protocol and its own store design. **Keybind recorder GUI** — Niri owns bindings; sysc documents IPC invocations instead. **A general widget toolkit for third parties** — an explicit design non-goal. **Window switcher overlay** (Noctalia's carousel, DMS's switcher) — Niri's own Alt-Tab and overview own this; only the overview type-to-launch slice is portable, and it is listed above as a polish gap.

## The first ten moves, ordered by impact over effort

Given a Go/cairo stack, cheapest-first with the impact justified above:

1. **Stock theme selection in settings (sysc-107)** — the palettes already exist in `internal/theme/stock.go`; this is wiring a picker to existing code. Smallest effort, immediate theming win.
2. **Key repeat (sysc-171)** — small fix, improves every text field in the shell.
3. **SVG icon rasterizer (sysc-173)** — pin one Go SVG rasterization dependency; unblocks sysc-117 and sysc-186 and fixes the single most visible defect.
4. **Template apply correctness (sysc-405, P1)** — write-to-temp-then-swap plus backup, mirroring Noctalia's hook-runner pattern; stops data loss.
5. **Notification centre polish (sysc-240 in progress; sysc-233 unblocks when sysc-231 `history.remove` and sysc-232 land)** — design is done; this is execution.
6. **OSD kind expansion** — media, lock-keys/caps, keyboard layout, DND reuse the existing `OSDManager` and icon font; needs a bd issue (currently untracked).
7. **Launcher quick providers** — calculator (sysc-79) and windows (sysc-81) first, desktop actions (sysc-84) next, app grid + chips (sysc-83) after; each is a bounded provider behind the existing ranked-list panel.
8. **Bar auto-hide + surface styles** — run sysc-587's design first (bd requires it), then build from sysc-546 (ready); sysc-551 blur Phase E continues in parallel.
9. **Idle management** — design M9-E around `ext_idle_notifier_v1` (already an M7 candidate); minimal version is lock/dpms/suspend actions plus the inhibitor that caffeine toggles.
10. **Screenshot region tool** — caelestia's pattern is the lazy one: per-screen region select, then pipe to an external editor; defer Noctalia-style annotation.

One cheap extra worth a bd issue: **Niri overview type-to-launch** — a single keyboard-focus layer surface, high delight per line of code.

## Conclusion

The comparison reframes the work. sysc-shell's deficit is not architectural — its token contract, motion caps, and theme pipeline match or beat the references — and it is not primarily a feature deficit either: **two of the seven critical gaps are correctness bugs in shipped features**, and the notification-centre gap is a finished design waiting on an upstream socket message. The genuinely new work concentrates in M9 sub-project E (idle, screenshot, night light, dock) and in the micro-interaction layer. That layer is the strategic item: Noctalia, DMS, and caelestia all demonstrate that polish is what falls out of one shared primitive applied everywhere, so the highest-leverage move in `internal/ui` is adding state-layer, ripple, and shape-morph as node kinds — every future widget then inherits the polish for free, and the gap stops re-opening with each new surface. Finally, the policy filter is doing real work: with lock screen, greeter, multi-compositor, and plugin compatibility removed, the true parity target is perhaps three-quarters the size of the reference feature surface, and the remaining gap is closable in roughly the order above.

---

*Sources: research notes at `research_notes/sysc-shell UI design gap analysis/` — `sysc-shell-current-ui.md` (repo `620cc52`, bd queried read-only), `noctalia-ui.md` (Noctalia v5.1.0 main), `dms-ui.md` (DankMaterialShell `09e7ea0`), `caelestia-ui.md` (caelestia-dots/shell `20e625d6`). Reference file paths cited inline where they add precision; sysc statuses come only from the sysc notes' code reads, docs register, and bd queries.*
