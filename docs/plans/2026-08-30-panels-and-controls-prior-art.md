# Milestone 4 Prior Art: Panels and Standard Controls

Date: 2026-08-30
Sources: Noctalia source snapshot `/home/nomadx/noctalia` (meson version 5.0.0, no git history);
DMS installed copy `/usr/share/quickshell/dms` (v1.5.3 "The Wolverine").
Method: read-only agent investigation, every claim carries file:line evidence.

Purpose: Milestone 4 targets functional and visual parity with these two shells for panel
surfaces and standard controls. This inventory is the evidence base for the design.

## Noctalia

### 1. Panel / popout surfaces

All built-in panels are registered in `src/app/application_ui.cpp:556-693` and dispatched through
`PanelManager` (`src/shell/panel/panel_manager.cpp`). Each panel declares layer/anchor/keyboard at
`Panel::keyboardMode()` / `Panel::layer()` (`src/shell/panel/panel.h:36-43`).

| Panel id | Trigger | Contents | Anchoring / layer / keyboard |
|---|---|---|---|
| `control-center` | bar button or `control_center` IPC action (`src/app/application_services.cpp:1474`); 12 tabs (Home/Media/Audio/Monitor/System/Power/Network/Bluetooth/Weather/Calendar/Notifications/ScreenTime) (`src/shell/control_center/control_center_panel.h:99-135`) | sidebar of tab buttons + tab bodies (sliders, toggles, etc.) | Default `PanelPlacement::Attached` else Floating; `LayerShellKeyboard::OnDemand` default; size 700px x 520px scaled (`src/shell/control_center/control_center_panel.cpp:78` / `.h:90-91`) |
| `launcher` | bar button or `launcher` IPC action (`src/app/application_services.cpp:1472`); `panel-toggle launcher /emo` opens emoji scoped | search input, segmented category filter, virtual grid/list view (`src/shell/launcher/launcher_panel.cpp:760-880`) | Floating or Attached; `LayerShellKeyboard::Exclusive` (`src/shell/launcher/launcher_panel.h:59`); 560x500 scaled (`src/shell/launcher/launcher_panel.h:57-58`) |
| `clipboard` | bar clipboard widget | history list | Floating; `LayerShellKeyboard::Exclusive` (`src/shell/clipboard/clipboard_panel.h:38-40`); 720x560 |
| `session` | bar power menu | grid of session action buttons with countdown overlay | Floating; `LayerShellKeyboard::Exclusive` (`src/shell/session/session_panel.h:33-37`) |
| `wallpaper` | settings "set wallpaper" / picker | grid of wallpaper tiles | Floating; `LayerShellKeyboard::Exclusive` (`src/shell/wallpaper/panel/wallpaper_panel.h:57-60`); 980x700 |
| `tray-drawer` | tray icon overflow | status notifier items | Floating; `LayerShellKeyboard::OnDemand` (`src/shell/tray/tray_drawer_panel.h:21-24`) |
| `polkit` | agent request | password input | Floating; `LayerShellKeyboard::Exclusive`, `LayerShellLayer::Overlay` (`src/shell/polkit/polkit_panel.h:30-34`) |
| `setup-wizard` | first run only (`src/app/application_ui.cpp:696`) | wizard pages | Floating; `LayerShellKeyboard::OnDemand` (`src/shell/setup_wizard/setup_wizard_panel.h:24-27`); 620x580 |
| `test` | dev-only | n/a | Floating; `OnDemand` (`src/shell/test/test_panel.h`) |

Detached layer-shell config is built in `PanelManager::openPanel`
(`src/shell/panel/panel_manager.cpp:480-545`). Namespace `noctalia-panel`; layer =
`shell.panel.floatingLayer` (default `"overlay"`, `src/config/config_types.h:911`); anchor +
margins computed by `shell::screenPositionAnchor` (`src/shell/screen_position.h:24-46`) for the
eight screen-corner positions plus `auto` (bar-relative) and `center`. `exclusiveZone = -1` for
centered panels else `0` (`src/shell/panel/panel_manager.cpp:506`). Keyboard interactivity chosen
by `resolvePanelKeyboardPlan` (`src/shell/panel/panel_manager.cpp:33-58`): `None` skips focus
entirely; with a focus grab it starts `Exclusive` then relaxes to `OnDemand` after
`kKeyboardRelaxDelay=100ms` (`src/shell/panel/panel_manager.cpp:14`).

Attached panel path (`src/shell/panel/panel_manager.cpp:617-810`): namespace
`noctalia-attached-panel`, dual-anchored against the bar edge with `exclusiveZone = 0`,
concave-corner overhang, `panelOverlap` overlap to hide the seam
(`src/shell/panel/panel_manager.cpp:744-758`).

Settings window — separate xdg-shell toplevel (`wayland/toplevel_surface.h`, used in
`src/shell/settings/settings_window.cpp`), not a layer-shell surface. App id
`dev.noctalia.Noctalia` (`src/shell/settings/settings_window.cpp:69`). Fixed sizes
`kWindowWidth=1280` / `kWindowHeight=600`, `kWindowMinWidth=1020`
(`src/shell/settings/settings_window.cpp:26-30`).

Notification toast — `LayerShellLayer::Overlay` or `Top` (configurable),
`LayerShellKeyboard::None`, namespace `noctalia-notification`, `exclusiveZone = 0`
(`src/shell/notification/notification_toast.cpp:1971-1987`).

OSD overlay (volume/mic/brightness/wifi/bluetooth/power-profile/caffeine/night-light/dnd/lock-keys/
keyboard-layout/media/privacy/kbd-backlight — `src/shell/osd/osd_overlay.h:13-29`) —
`LayerShellLayer::Overlay`, `LayerShellKeyboard::None`, `exclusiveZone = 0`, namespace
`noctalia-osd` (`src/shell/osd/osd_overlay.cpp:497-521`). Per-output instances. Position via
`effectiveOsdPosition` (`src/shell/osd/osd_overlay.cpp:97`) supporting the same 8-corner
vocabulary as panels plus center.

Window switcher — `LayerShellLayer::Overlay`, `LayerShellKeyboard::Exclusive`, `exclusiveZone = -1`
(`src/shell/switcher/window_switcher.cpp:966-971`).

Overview launcher capture (Niri overview type-to-launch) — `LayerShellLayer::Overlay`,
`LayerShellKeyboard::Exclusive`, `exclusiveZone = -1`
(`src/shell/overview/overview_launcher_capture.cpp:145-150`).

Screen corners — four layer-shell surfaces per output at the four corners, `LayerShellLayer::Top`,
`LayerShellKeyboard::None`, `exclusiveZone = -1`, `size x size` squares, namespace
`noctalia-screen-corner` (`src/shell/screen_corners/screen_corners.cpp:12-23,134-140`).

Hot corners — `LayerShellLayer` from `Bar::highestLayerForOutput`
(`src/shell/bar/bar.cpp:1901-1907`), `exclusiveZone = -1`
(`src/shell/hot_corners/hot_corners.cpp:149-176`).

Bar — multi-output, `LayerShellLayer` from config (`src/shell/bar/bar.cpp:672`), reserves exclusive
zone via `reservedBarExclusiveZone` (`src/shell/bar/bar.cpp:603`).

Dock — `LayerShellLayer` configurable, reserves `exclusiveZone = panelH + min(marginEdge, shadow)`
when `reserveSpace` set (`src/shell/dock/dock_geometry.cpp:209-241`).

Desktop widget host — `LayerShellLayer::Bottom`, `exclusiveZone = -1`, keyboard = None
(`src/shell/desktop/desktop_widgets_host.cpp:209-216`).

Desktop widget editor — `LayerShellLayer::Bottom`, `exclusiveZone = -1`, keyboard = OnDemand
(`src/shell/desktop/editor/desktop_widgets_editor.cpp:387-392`).

Wallpaper — `LayerShellLayer::Background`, `exclusiveZone = -1`
(`src/shell/wallpaper/wallpaper.cpp:1291-1297`).

Backdrop — `LayerShellLayer::Background`, `exclusiveZone = -1`
(`src/shell/backdrop/backdrop.cpp:251-257`).

Lock screen — `ext_session_lock_surface_v1` (separate Wayland protocol,
`src/shell/lockscreen/lock_surface.cpp:42-43,585`).

Tooltip manager — separate wl-popup on hover, parent = `zwlr_layer_surface_v1` of the surface it
belongs to (`src/shell/tooltip/tooltip_manager.cpp:208-211`).

### 2. Controls (custom C++ widgets, no QML)

Rendering is custom OpenGL ES via `render/backend/gles_render_backend.*` and per-effect GLSL
programs under `src/render/programs/*` (rect/glyph/image/blur/audio-spectrum/wallpaper/etc).
Scene graph + hit-testing in `src/render/scene/{node,input_area,input_dispatcher}.{h,cpp}`.
No Qt or GTK.

Builder namespace `ui::*` in `src/ui/builders.h:600-625` and `src/ui/builders.cpp`. Widgets in
`src/ui/controls/`:

| Widget | Header | Used in (selected examples) |
|---|---|---|
| `Button` (`button.h:34`, variants: Standard/Icon/Tab/Bare/Primary; `ButtonContentAlign` `button.h:17`) | `button.h` | everywhere — bar widgets, session, settings factory (`settings_control_factory.cpp:180`), tabs (`home_tab.cpp:567`) |
| `Toggle` (`toggle.h:17`, `ToggleSize` `toggle.h:9`) | `toggle.h` | audio/network/power/notification-filter/settings factory (`settings_control_factory.cpp:103,322`) |
| `Slider` (`slider.h:11`) | `slider.h` | audio tab (`audio_tab.cpp:1054,1612`), settings factory (`settings_control_factory.cpp:505`) |
| `RangeSlider` (`range_slider.h:13`) | `range_slider.h` | settings factory (`settings_control_factory.cpp:612`) |
| `Input` (text) (`input.h:25`, `TextInputClient`) | `input.h` | launcher search (`launcher_panel.cpp:760`), settings search/forms (`settings_control_factory.cpp:494`) |
| `Checkbox` (`checkbox.h:13`) | `checkbox.h` | settings factory |
| `RadioButton` (`radio_button.h:10`) | `radio_button.h` | audio tab (`audio_tab.cpp:867`) |
| `Select` (`select.h`) + `SelectDropdownPopup` (`select_dropdown_popup.h:21`) | `select*.h` | audio tab (`audio_tab.cpp:1686`), settings factory (`settings_control_factory.cpp:399`) |
| `Segmented` (`segmented.h`) | `segmented.h` | launcher category filter (`launcher_panel.cpp:790`), settings factory (`settings_control_factory.cpp:363`) |
| `Stepper` (`stepper.h:16`) | `stepper.h` | settings factory (`settings_control_factory.cpp:975,1015`) |
| `KeybindRecorder` (`keybind_recorder.h`) | `keybind_recorder.h` | settings keybinds (`settings_content.cpp:649,698`), session actions (`settings_content_session_action.cpp:314`) |
| `SearchPicker` (`search_picker.h`) | `search_picker.h` | widget-add popup (`widget_add_popup.cpp:422`), search-picker popup (`search_picker_popup.cpp:144`) |
| `ColorPicker` + `ColorPickerSheet` (`color_picker.h:18,83`) | `color_picker.h` | theme editor (`color_picker_dialog_popup.cpp:35-37`) |
| `GlyphPicker` (`glyph_picker.h`) | `glyph_picker.h` | theme editor |
| `ColorPickerDialog` / `GlyphPickerDialog` / `FileDialog` (as layer popups) | `ui/dialogs/*` | settings dialog presenters (`application_ui.cpp:923-929`) |
| `Collapsible` (`collapsible.h:13`) | `collapsible.h` | settings |
| `ListEditor` (`list_editor.h:16`) | `list_editor.h` | list entries |
| `ScrollView` (`scroll_view.h:20`) | `scroll_view.h` | sidebar/content scrolling, control center |
| `VirtualGridView` / `VirtualListView` | `virtual_*_view.h` | launcher, wallpaper, sessions |
| `Flex` / `Box` | `box.h`, `flex.h` | layout primitives |
| `Label`, `Image`, `Separator`, `Spacer`, `Spinner`, `ProgressBar`, `DropZone`, `MarkdownView`, `Graph` | `label.h`, `image.h`, `separator.h`, `spacer.h`, `spinner.h`, `progress_bar.h`, `drop_zone.h`, `markdown_view.h`, `graph.h` | various |
| `ContextMenuControl` + `ContextMenuPopup` (xwayland-free wayland popup) | `context_menu_popup.h:55` | dock context menu (`dock_context_menu.cpp:148-244`) |
| `RovingListNav` (roving tabindex) | `roving_list_nav.h:13` | sidebar nav (control-center `control_center_panel.cpp:130`, settings `settings_sidebar.cpp:176`) |
| `SplitPaneFocus` (sidebar-content nav) | `ui/split_pane_focus.{h,cpp}` | settings (`settings_window.cpp:1106`), control-center (`control_center_panel.cpp:421`) |
| `Tooltip` | `ui/controls/tooltip_content.h` | all controls via `InputArea::setTooltip*` |

### 3. Settings window structure

Separate xdg-shell toplevel (`src/shell/settings/settings_window.cpp`), not a panel.

- Sidebar nav: `settings::kSettingsSections` — 21 sections defined at
  `src/shell/settings/settings_registry.cpp:67-89`: Appearance, Wallpaper, Templates, Desktop,
  Dock, Panels, Launcher, ControlCenter, Notifications, Osd, Shell, Keybinds, Security, System,
  Services, Location, Power, Hooks, Niri, Bar (with sub-bars + per-monitor overrides), Plugins
  (special — external content). `enum class SettingsSection` at `settings_registry.h:25-46`.
  Sidebar built in `src/shell/settings/settings_sidebar.cpp:176-374` using `RovingListNavHost` +
  nested ScrollView; secondary row for bars; tertiary row for monitor overrides.
- Content built by `settings::addSettingsContentSections` (`src/shell/settings/settings_content.cpp`),
  with per-section content helpers in `settings_content_*.cpp` and `settings_content_common.cpp`.
- Global search: `Input` field in window header (`settings_content.h:42` —
  `SettingsContentContext::searchQuery`), fed by `Ctrl+F` (`settings_window.cpp:1091`), filtered by
  `normalizedSettingQuery` (`settings_content.cpp:1226`); when non-empty the sidebar is hidden and
  entries are listed. Per-entry search limit `kMaxSearchResults` (`settings_content.cpp:1275`).
- Editor popups: `settings_sheet_popup.cpp`, `widget_add_popup.cpp`, `search_picker_popup.cpp`,
  `config_export_dialog_popup.cpp`, `color_picker_dialog_popup.cpp`,
  `glyph_picker_dialog_popup.cpp`, `file_dialog_popup.cpp` — all layer-shell popups stacked above
  the toplevel.
- Configurable areas: TOML-driven via `SettingsRegistry::kSettingsSections` +
  `src/shell/settings/settings_control_factory.cpp` constructs
  Toggle/Slider/Select/Input/Segmented/Stepper/KeybindRecorder/ColorPicker/SearchPicker/ListEditor/
  Collapsible controls from schema entries.

### 4. Keyboard focus / interaction

- `InputArea` (`src/render/scene/input_area.h`) is the focus/pointer unit; `InputDispatcher`
  (`src/render/scene/input_dispatcher.h:47,55`) tracks `m_focusedArea`.
- Panel keyboard policy declared per-panel: `Exclusive` for launcher/clipboard/session/wallpaper/
  polkit (see section 1); `OnDemand` for control-center (default), tray-drawer, setup-wizard;
  `None` for OSDs/notifications/screen-corners/desktop-widgets.
- `dismissTransientUi()` + Escape handling: `PanelManager::onKeyboardEvent`
  (`panel_manager.cpp:1828-1871`): Escape -> `handleGlobalKey` on active panel -> if no match,
  `dismissTransientUi()` -> else `closePanel()`. Settings window has its own Escape handling:
  closes widget-add / config-export / search-picker / editor-sheet / actions-menu popups, then
  clears inline-edit state (`settings_window.cpp:999-1090`). Launcher handles Escape via
  `handleGlobalKey` (`launcher_panel.cpp:1196-1217`) which forwards to `handleKeyEvent`
  (`launcher_panel.cpp:1793-`). Wallpaper panel handles Escape-equivalents via
  `KeybindMatcher::matches(KeybindAction::Cancel,...)` (`wallpaper_panel.cpp:1538+`).
- Focus restoration on close: panel close keeps an animated reveal; clipboard/launcher close
  without animation when auto-paste must hit the underlying toplevel
  (`application_ui.cpp:541-554`).
- Keyboard relax: `m_keyboardRelaxTimer.start(kKeyboardRelaxDelay=100ms, ...)` to drop from
  `Exclusive` to `OnDemand` after settle (`panel_manager.cpp:1108-1117,1194-1204`).
- Initial focus: `Panel::initialFocusArea()` returns the panel's input to focus on open (launcher
  input `launcher_panel.cpp:1195`, polkit input `polkit_panel.h:36`).
- Outside-click dismissal: `wantsOutsideDismiss = dismissOnOutsideClick && keyboardMode != None`
  (`panel_manager.cpp:495`); handled via `m_clickShield` (input-region click shield) or
  `m_focusGrab` (Hyprland `hyprland-focus-grab-v1`, `protocols/hyprland-focus-grab-v1.xml`).
- Hotkeys: configured per `KeybindAction` in TOML; `KeybindMatcher::setMatcher` binds each action
  to `ConfigService::matchesKeybind` (`application_services.cpp:655-669`). The launcher/control-center
  are opened by user-configured keybinds or the `panel-toggle <id>` IPC verb
  (`panel_manager.cpp:2635`). Bar dead-zone actions can also toggle panels via
  `panel-toggle <id> [context]` / `panel-open` verbs (`bar.cpp:327-377,2419-2421`).

### 5. Look (theme / radius / shadows / fonts / animation)

- Color tokens: 16 `ColorRole`s declared in `src/ui/palette.h:12-30` (`Primary, OnPrimary,
  Secondary, OnSecondary, Tertiary, OnTertiary, Error, OnError, Surface, OnSurface,
  SurfaceVariant, OnSurfaceVariant, Outline, Shadow, Hover, OnHover`). Tokens in `kColorRoleTokens`
  `palette.h:36-52`. Resolved via `colorForRole`/`colorSpecFromRole` (`palette.h:111-115`).
  Dark/light via `setResolvedThemeLight` from `ThemeService` (`theme/theme_service.cpp:528,541`).
- Corner radii (logical px, `src/ui/style.h:18-21`): `radiusSm=3`, `radiusMd=6`, `radiusLg=9`,
  `radiusXl=12`. `scaledRadius*` helpers scale by UI scale (`style.h:79-83`). User-scalable via
  `cornerRadiusScale()`.
- Shadows: blur radius fixed `kBlurRadius = 12` (`src/shell/surface/shadow.h:10`); bleed computed
  per direction from `ShadowConfig::direction` (Down/Up/Left/Right/Center/DownLeft/.../UpRight,
  `config_types.h:748-769`) with `alpha=0.55` default (`config_types.h:899-903`); per-component
  opt-out (`surface_shadow::style` `shadow.cpp:25`); blur regions submitted via
  `wl_compositor`/`wl_region`.
- Spacing constants (`src/ui/style.h:23-29`): `spaceXs=4`, `spaceSm=8`, `spaceMd=12`, `spaceLg=16`;
  `cardPadding=14`, `panelPadding=14`; `controlHeightSm=32`, `controlHeight=38`,
  `controlHeightLg=44`.
- Font sizes (`src/ui/style.h:36-39`): `fontSizeMini=11`, `fontSizeCaption=13`, `fontSizeBody=14`,
  `fontSizeTitle=16`, `fontSizeHeader=20`. Font-weight catalog at
  `src/render/text/font_weight_catalog.*`.
- Animation: `AnimationManager` (`src/render/animation/animation_manager.h:9-29`) with
  `animate(from, to, durationMs, easing, setter, onComplete, owner)`. Easings
  `Linear/EaseInQuad/EaseOutQuad/EaseInOutQuad/EaseOutCubic/EaseInOutCubic/EaseOutBack`
  (`render/animation/animation.h:7-15`, default `EaseOutQuad`). Standard durations
  (`src/ui/style.h:9-11`): `animFast=100ms`, `animNormal=200ms`, `animSlow=400ms`. Global
  `AnimationConfig{enabled,speed}` (`config_types.h:890-896`); `AnimationManager::animateTimer`
  ignores motion enable for fixed-time effects (`animation_manager.h:26`); `reduceMotion()`
  cancels all. Used: panel reveal, OSD reveal, bar hover, notification toast entry/exit, dock
  hover-zoom, launcher auto-hide.
- Theme application: `ThemeService` (`src/theme/theme_service.cpp:528,541`) drives light/dark +
  palette; `Palette::lerpPalette` interpolates for cross-fades; `paletteChanged` signal restyles
  every subscribed control.

### 6. Placement / geometry

- Panel placement policy declared per-panel via `PanelPlacement::Attached` or `Floating`
  (`src/shell/panel/panel.h:62-63`, `config_types.h:920-926` per-panel `<id>Placement`).
- Floating placement uses one of `kPanelPositions` tokens
  (`top_left|top_center|top_right|center_left|center|center_right|bottom_left|bottom_center|
  bottom_right|auto`) translated to layer-shell anchor + margins by `shell::screenPositionAnchor`
  (`src/shell/screen_position.h:24-46`); per-panel default = `auto` (bar-relative) or `center`
  (launcher/clipboard/polkit) (`config_types.h:920-925`).
- Attached placement (`panel_manager.cpp:617-810`): single-bar panels anchor against the bar's
  reserved edge; multi-bar edge picks the source bar (`config_types.h` `shell.panel_anchor_bar`,
  `panel_manager.cpp:115-149`). Geometry derived from
  `BarConfig::marginEdge/marginEnds/thickness/position`; concave-corner nesting
  `barR + cornerRadius` (`panel_manager.cpp:752-758`); cross-axis outset wraps shadow bleed +
  cornerRadius (`panel_manager.cpp:709-718`); bar overlap `barConfig.panelOverlap` to hide the seam
  (`panel_manager.cpp:759`).
- Edge clamping: `clampMargin(desired, panelSize, outputSize, padding)` in
  `panel_manager.cpp:447-450` keeps every margin inside `[padding, output - panel - padding]`.
  `screenPadding = Style::spaceSm (8)` logical px used as inset (`panel_manager.cpp:436`). Fill
  axes dual-anchor with size `0` so the compositor subtracts exclusive zones
  (`panel_manager.cpp:541-562`).
- Open-near-click: `request.anchorX/anchorY` passed by `openPanelAtBarPointer`
  (`src/shell/bar/bar.cpp:327-358`) -> `request.hasAnchorPosition`; honored when
  `openNearClickEnabledForPanel` returns true (`panel_manager.cpp:177-208`).
- Output fallback: `m_platform->focusedInteractiveOutput(1200ms)` then
  `probeFocusedOutput(..., 250ms)` then `preferredInteractiveOutput(1200ms)`
  (`panel_manager.cpp:308-333`).
- OSD placement: 8-corner vocabulary + center via
  `effectiveOsdPosition(orientation, horizontalPosition, verticalPosition)`
  (`src/shell/osd/osd_overlay.cpp:97-395`), anchors one or two edges accordingly.
- Tooltip placement: layer-popup anchored to the parent `zwlr_layer_surface_v1`; offset by
  `centeringOffset` (`src/ui/dialogs/layer_popup_host.h:23`).
- Bar placement: top/bottom/left/right via `LayerShellAnchor` (`bar.cpp:432-441,283-286`),
  exclusive zone from `reservedBarExclusiveZone` (`bar.cpp:603`).
- Dock placement: top/bottom/left/right via `dock_geometry.cpp:109-117`, `exclusiveZone` reserved
  when `cfg.reserveSpace` (`dock_geometry.cpp:209-241`).

## DMS v1.5.3 "The Wolverine"

### 1. Panel/popup surfaces

All use `PanelWindow` (Quickshell), namespace under `dms:*`.

| Surface | File:line | Trigger | Anchors | Layer | Excl. | kbdFocus |
|---|---|---|---|---|---|---|
| DankBar (per-output) | `Modules/DankBar/DankBarWindow.qml:365-366,764` | Variants of Quickshell.screens `Modules/DankBar/DankBar.qml:163-180` | top/bottom/left/right by config | `dBarLayer` from `LayerShell.fromEnv("DMS_DANKBAR_LAYER", WlrLayer.Overlay-or-Top)` `:365` | `effectiveBarThickness+spacing+bottomGap` `:764` | (default; bars carry no keyboard) |
| ControlCenter popout | `Modules/ControlCenter/ControlCenterPopout.qml:5`; ns `Widgets/DankPopoutStandalone.qml:571,673` | bar button -> `barWindow.triggerControlCenter` `Modules/DankBar/DankBarWindow.qml:38-67`; IPC `control-center` `DMSShellIPC.qml:256` | full-height (`fullHeightSurface`) | `DMS_POPOUT_LAYER` (Overlay/Top) `:90` | `-1` `:675` | `KeyboardFocus.keyboardFocus(...)` `:676` |
| DankDash popout | `Modules/DankDash/DankDashPopout.qml:6,72`; `triggerDashTab(tabId)` `Modules/DankBar/DankBarWindow.qml:69-119` | bar clock section | standalone `Widgets/DankPopoutStandalone.qml:678` (left/top/bottom true; `_fullHeight:bottom`) | as CC | `-1` | `KeyboardFocus.keyboardFocus(...)` |
| NotificationCenter popout | `Modules/Notifications/Center/NotificationCenterPopout.qml:6`; ns `Widgets/DankPopoutStandalone.qml:571` | bar notif button -> `toggleNotificationCenter` `Services/PopoutService.qml:165-170`; IPC `notifications` | standalone | as CC | `-1` | as CC |
| Notification popup card | `Modules/Notifications/Popup/NotificationPopup.qml:58` (`dms:notification-popup`); layer `Overlay` `:193`; excZone `-1` `:198`; kbdFocus `None` `:199`; anchors `top+left` only `:341-344` | per-notification via `NotificationPopupManager` | corner-anchored per `notifBarSide` `:21-39` | `DMS_OSD_LAYER`/Overlay | `-1` | None |
| OSD (volume/brightness/...) | `Widgets/DankOSD.qml:95-101` (`dms:osd`, `Overlay`, `-1`, `None`); per-OSD content `Modules/OSD/*.qml` | `OSDManager.showOSD` `Common/OSDManager.qml:40-58` | per `osdPosition` (`Common/SettingsData.qml:931` `Position.BottomCenter` default) | `DMS_OSD_LAYER` | `-1` | None |
| Toast | `Modules/Toast.qml:11,48-50` (`dms:toast`, Overlay, `-1`, None) | `ToastService` (error/info/warning) | centered horizontally `Modules/Toast.qml:62-63` | Overlay | `-1` | None |
| Battery/VPN/SysUpdate/DWL popouts | `Modules/DankBar/Popouts/{Battery,Vpn,SystemUpdate,DWLLayout}Popout.qml:8` each | bar buttons -> `PopoutService.toggle*` `Services/PopoutService.qml:332-388` | standalone | as CC | `-1` | as CC |
| AppDrawer popout | `Modules/AppDrawer/AppDrawerPopout.qml:6` | IPC `app-drawer` / bar button | standalone | as CC | `-1` | as CC |
| ProcessList popout | `Modules/ProcessList/ProcessListPopout.qml:8,46`; IPC `processlist` | bar button | standalone | as CC | `-1` | as CC |
| Notepad popout | `Modules/Notepad/NotepadPopoutWindow.qml` | bar notepad widget | standalone | as CC | `-1` | as CC |
| Dock | `Modules/Dock/Dock.qml:18-32` (`dms:dock`); reservation window `Dock.qml:496-508` (`dms:dock-exclusion`) | always-on-bottom (configurable `dockPosition:Position.Bottom` `Common/SettingsData.qml:831`) | bottom/left/right per config | Overlay-or-Top `:32` | `dockReserveZone` `:505` or `-1` | (default) |
| FrameWindow (connected-frame) | `Modules/Frame/FrameWindow.qml:11,23-25` (`dms:frame`, Top, `ExclusionMode.Ignore`) | screen edge | full edge | Top | ignore | (default) |
| Workspace overview (Hyprland) | `Modules/WorkspaceOverlays/HyprlandOverview.qml:23,33-35` (`dms:workspace-overview`) | hyprland overview | center | per-bar/Top | `-1` | (default) |
| Niri overview | `Modules/WorkspaceOverlays/NiriOverviewOverlay.qml:93,140-142` (`dms:niri-overview-spotlight`) | niri-inOverview | center | per | `-1` | (default) |
| Greeter surface | `Modules/Greetd/GreeterSurface.qml:12,25-26` (`Overlay`, `WlrKeyboardFocus.Exclusive`) | `DMS_RUN_GREETER=1` | fullscreen | Overlay | (n/a) | Exclusive |
| Lock fade-to-lock/dpms | `Modules/Lock/{FadeToLockWindow,FadeToDpmsWindow}.qml:9,21-24` (`dms:fade-to-{lock,dpms}`, Overlay, `-1`, `active?Exclusive:None`) | idle/lock | fullscreen | Overlay | `-1` | Exclusive |
| Color picker modal | `Modals/DankColorPickerModal.qml:11` | `barWindow.colorPickerRequested` `Modules/DankBar/DankBarWindow.qml:368-370` | center | per-modal | `-1` | kbdFocus helper |
| DankLauncherV2 / Spotlight | `Modals/DankLauncherV2/DankLauncherV2Modal{Standalone,Connected,Spotlight}.qml`; ns `dms:spotlight` `:397/:627/:345`; clickcatcher `dms:spotlight:clickcatcher` `:334,283` | IPC `spotlight`/`launcher` `DMSShellIPC.qml:1400,1441`; `PopoutService.toggleDankLauncherV2` | fullscreen modal layer | Top-or-Overlay via `DMS_MODAL_LAYER` | `-1` | `KeyboardFocus.keyboardFocus` |
| Settings modal | `Modals/Settings/SettingsModal.qml` (via `DankModal` `Modals/Common/DankModal.qml:11` ns `dms:modal`); `DankModalStandalone/Connected` `Modals/Common/DankModal{Standalone,Connected}.qml:268-276,401-409` | IPC `settings` `DMSShellIPC.qml:1057` | fullscreen modal | `DMS_MODAL_LAYER` | `-1` | `KeyboardFocus.keyboardFocus` |
| PowerMenu modal | `Modals/PowerMenuModal.qml:9` (DankModal) | IPC `powermenu` `DMSShellIPC.qml:176`; CC button `lockRequested` `Modals/PowerMenuModal.qml:36` | center | per-modal | `-1` | as modal |
| Other modals | `Modals/NotificationModal.qml:8`, `MuxModal.qml`, `NetworkInfoModal.qml`, `PowerProfileModal.qml`, `KeybindsModal{Window,Overlay}.qml:11,8`, `ProcessListModal.qml:14`, `WorkspaceRenameModal.qml:11`, `WindowRuleModal.qml:10`, `AppPickerModal.qml`, `BrowserPickerModal.qml`, `DisplayConfirmationModal.qml`, `Changelog/ChangelogModal.qml:10`, `Clipboard/ClipboardHistoryModal.qml`, `FileBrowser/FileBrowserModal.qml:9`, `Greeter/GreeterModal.qml:12`, `BluetoothPairingModal.qml`, `WifiPasswordModal.qml`, `WifiQRCodeModal.qml`, `SwitchUserModal.qml`, `PolkitAuthModal/SurfaceModal.qml`, `DankColorPickerModal.qml`, `NetworkWiredInfoModal.qml` | bar buttons, IPC `keybinds`/`clipboard`/... | all DankModal-based -> centered | `DMS_MODAL_LAYER` | `-1` | `KeyboardFocus.keyboardFocus(shouldHaveFocus, customKeyboardFocus)` |
| WallpaperBackground / BlurredWallpaperBackground | `Modules/WallpaperBackground.qml:23`, `Modules/BlurredWallpaperBackground.qml:17,25` (`dms:blurwallpaper`) | always | fullscreen | Background (implied) | n/a | n/a |
| Tray menu / DND duration menu | `Modules/Notifications/Center/DndDurationPopup.qml:8,20-23` (`dms:dnd-duration-menu`, Overlay, `-1`, None); tray via `TrayMenuManager` | right-click tray icon | at icon | Overlay | `-1` | None |

Notes: popouts use dual PanelWindow (`backgroundWindow`+`contentWindow`)
`Widgets/DankPopoutStandalone.qml:563,669`. Content surfaces bind
`WlrLayershell.keyboardFocus: KeyboardFocus.keyboardFocus(shouldBeVisible, customKeyboardFocus)`
`:676` and `WlrLayershell.exclusiveZone: -1` `:675`.

### 2. Controls

Custom Material3 widgets in `Widgets/` (no native Qt Controls except `QtQuick.Controls` for
`Popup`/`TextField` decoration in a few files).

| Control | Defined | Heavy callers |
|---|---|---|
| `DankToggle` | `Widgets/DankToggle.qml` | `Modules/Settings/WidgetsTabSection.qml:26`, `Widgets/KeybindItem.qml:11` |
| `DankSlider` | `Widgets/DankSlider.qml` | `Modals/WindowRuleModal.qml` (5x), `Modules/ControlCenter/Widgets/*.qml` (3x), `OSD/VolumeOSD|MicVolumeOSD.qml` |
| `DankTextField` | `Widgets/DankTextField.qml` | `Modals/WindowRuleModal.qml:24`, `Modals/WifiPasswordModal.qml:6`, `Settings/PrinterTab.qml` etc. |
| `DankDropdown` | `Widgets/DankDropdown.qml` | `Modules/Settings/DisplayConfigTab.qml`, `ThemeColorsTab.qml`, `NotificationsTab.qml` |
| `DankTabBar` | `Widgets/DankTabBar.qml` | `Modules/DankDash/DankDashPopout.qml`, `Settings/WallpaperTab.qml`, `Settings/ThemeColorsTab.qml`, `Settings/GammaControlTab.qml`, `Settings/WorkspaceAppearanceCard.qml` |
| `DankButton` | `Widgets/DankButton.qml` / `DankActionButton.qml` | pervasive |
| `DankButtonGroup`, `DankFilterChips`, `DankNumberStepper`, `DankIconPicker`, `DankLocationSearch`, `DankSpinner` | `Widgets/` | settings/plugin pages |
| `DankFlickable` / `DankListView` / `DankGridView` | `Widgets/DankFlickable|DankListView|DankGridView.qml` | `SettingsSidebar.qml:42`, all settings tabs (43 calls) |
| `DankScrollbar` | `Widgets/DankScrollbar.qml` | scroll containers |
| Menus are custom (`TrayMenuManager`, `LauncherContextMenu.qml`, `DockContextMenuBase.qml`, `NotificationContextMenu.qml`); no raw QtQuick.Controls Switch/Popup for primary UI. | | |

### 3. Settings panels

Entry: `Modals/Settings/SettingsModal.qml` (DankModal -> standalone/connected). Sidebar
`Modals/Settings/SettingsSidebar.qml:69` `categoryStructure` declares ~9 categories
(personalization, dankbar, workspaces_widgets, dock_launcher, network, displays_widgets,
time_weather, plugins, system). Each leaf has `tabIndex` mapped 1:1 to `Loader`s in
`Modals/Settings/SettingsContent.qml:33-714` (one Loader per tab: Wallpaper, TimeWeather,
Keybinds, DankBar, Workspaces, CompositorLayout, WindowRules, DankBarAppearance, DisplayConfig,
GammaControl, DisplayWidgets, NetworkStatus/Ethernet/Wifi/VPN, Printer, Launcher, ThemeColors,
LockScreen, Greeter, Plugins, About, TypographyMotion, Sounds, MediaPlayer, Notifications, OSD,
DefaultApps, RunningApps, SystemUpdater, PowerSleep, Widgets, Clipboard, DesktopWidgets, Audio,
Locale, Mux, Frame, Users, AutoStart, Battery, DankDash). Each `.qml` in `Modules/Settings/*.qml`
is one tab.

- Search: `DankTextField searchField` + `Services/SettingsSearchService.qml:53` loads
  `translations/settings_search_index.json`; per-card registration; sidebar highlights matches
  `SettingsSidebar.qml:22-66` (searchActive, keyboardHighlightIndex, Up/Down nav). Backend index
  generated by `translations/extract_settings_index.py:589`.
- Persistence: `Common/SettingsData.qml:3606` `FileView settingsFile path:
  $XDG_CONFIG_HOME/DankMaterialShell/settings.json` (atomicWrites, watchChanges); parser in
  `Common/settings/SettingsStore.js`. Plugin settings: `pluginSettingsPath`
  `Common/SettingsData.qml:81` (`plugin_settings.json`). Session/runtime:
  `Common/SessionData.qml:1394,1431` (state & greeter session JSON via FileView). Settings spec
  (typed enums) lives in `Common/settings/SettingsSpec.js` + `SessionSpec.js`.
- Enums `Common/SettingsData.qml:18-31`: `Position` (Top/Bottom/Left/Right/TopCenter/BottomCenter/
  LeftCenter/RightCenter), `AnimationSpeed` (None/Short/Medium/Long/Custom), `AnimationVariant`
  (Material/Fluent/Dynamic), `AnimationEffect` (Standard/Directional/Depth).

### 4. Keyboard focus + interaction

- Property: Quickshell uses `WlrLayershell.keyboardFocus` (not `keyboardInteractivity`).
  Resolution helper: `Common/KeyboardFocus.qml:11-26` — returns `WlrKeyboardFocus.None` when
  `PopoutManager.screenshotActive`/inactive; `OnDemand` if `CompositorService.useHyprlandFocusGrab`;
  else `Exclusive`. `useHyprlandFocusGrab` = `isHyprland && DMS_HYPRLAND_EXCLUSIVE_FOCUS !== "1"`
  `Services/CompositorService.qml:26`.
- Applied at content windows: popouts `Widgets/DankPopoutStandalone.qml:676`,
  `DankPopoutConnected.qml` (via impl); modals `Modals/Common/DankModalStandalone.qml:276`,
  `DankModalConnected.qml:409`; explicit: Greeter `Exclusive`
  `Modules/Greetd/GreeterSurface.qml:26`, FadeToLock/DPMS `Exclusive when active`
  `Modules/Lock/FadeToLockWindow.qml:24`/`FadeToDpmsWindow.qml:24`, OSD/Toast/NotificationPopup/
  BackgroundWindow `None` `Widgets/DankOSD.qml:101`, `Modules/Toast.qml:50`,
  `Modules/Notifications/Popup/NotificationPopup.qml:199`,
  `Widgets/DankPopoutStandalone.qml:574`.
- Escape-to-close: `Widgets/DankPopoutStandalone.qml:951-960` — `focusHelper` `Item focus:
  !root.contentHandlesKeys; Keys.onPressed: if (event.key === Qt.Key_Escape) { close();
  event.accepted = true; }`. Modal default `closeOnEscapeKey: true`
  `Modals/Common/DankModalStandalone.qml:32`.
- Focus restoration: `Common/KeyboardFocus.qml:28-33` captures `ToplevelManager.activeToplevel`
  and `Qt.callLater(() => toplevel.activate())` to restore.
- Keyboard helpers: modal InputMethod reset on hide `Widgets/DankPopoutStandalone.qml:646-651`;
  Settings `focusSearch()` `SettingsSidebar.qml:28-30`; tab nav via arrow keys +
  `selectHighlighted()` `:53-66`; clipboard/notification keyboard controllers
  `Modals/Clipboard/ClipboardKeyboardController.qml`,
  `Modules/Notifications/Center/NotificationKeyboardController.qml`.
- Global hotkeys: not registered in QML. Compositor-driven: actions described in
  `Common/KeybindActions.js:1+` (`spawn dms ipc call <target> <open|close|toggle|focusOrToggle>`);
  targets include `spotlight`, `launcher`, `spotlight-bar`, `clipboard`, `notifications`,
  `processlist`, `settings`, `powermenu`, `control-center`, `keybinds`
  `Common/KeybindActions.js:16-64`. IPC handlers live in `DMSShellIPC.qml` (`IpcHandler {
  function open/close/toggle/status }` per target `:107-1500+`). `Services/KeybindsService.qml:11-99`
  reads/writes compositor config (`niri`/`hyprland`/`mangowc`) via `dms keybinds ...` CLI.

### 5. Look — theming & motion

- `Common/Theme.qml` is the singleton. Matugen at `matugen/configs/*.toml` (`base.toml`,
  `gtk3-{dark,light}.toml`, `hyprland.toml`, `niri.toml`, `mangowc.toml`, `kitty.toml`,
  `alacritty.toml`, `firefox.toml`, `zenbrowser.toml`, `qt5ct.toml`, `qt6ct.toml`, ...);
  templates `matugen/templates/*`. Wallpaper -> `matugenColors` JSON in `stateDir`
  `Common/Theme.qml:14` (`$XDG_CACHE_HOME/DankMaterialShell`). Color tokens M3:
  `primary/on_primary/primary_container/secondary/tertiary/surface/surface_variant/
  surfaceContainer{Lowest..Highest}/outline/background/error/warning/info/success`
  `Common/Theme.qml:453-477`. Dark/light: `SessionData.isLightMode` `Theme.qml:32`; auto via
  gamma backend or IP/location `Theme.qml:300-377`.
- Default `cornerRadius = 12` `Common/Theme.qml:1224-1229`; `connectedCornerRadius` uses
  `SettingsData.frameRounding` when frame connected `:1082`; surface radii via `Theme.cornerRadius`
  everywhere (`Widgets/DankPopoutStandalone.qml:944`, `Widgets/DankOSD.qml:104`).
- Spacing scale `Common/Theme.qml:1259-1265`: `spacingXXS:2 / XS:4 / S:8 / M:12 / L:16 / XL:24`.
  Sizing: `barHeight:48`, `iconSize:24` `:1269-1271`. Panel/popup transparency
  `panelTransparency:0.85`, `popupTransparency` from settings (or `frameOpacity` in connected)
  `:1274-1280`.
- Fonts: defaults `Inter Variable`, mono `Fira Code` `Common/Theme.qml:19-20`; `fontScale`
  default 1.0; sizes `Small:12 / Medium:14 / Large:16 / XLarge:20` scaled `:1265-1268`.
- Elevation: 5 levels `Common/Theme.qml:838-866` (L1:4/1/0.2, L2:8/4/0.25, L3:12/6/0.3,
  L4:16/8/0.3, L5:20/10/0.3), directional light `:790-810`, rendered by shader
  `Shaders/qsb/elevation_rect.frag.qsb` via `Common/ElevationShadow.qml:42`; ambient layer `:978`.
- Animation presets `Common/Theme.qml:1004-1043` (`None:0/0/0/0/0`; `Short:50/75/150/250/500`;
  `Medium:100/150/300/500/1000`; `Long:150/225/450/750/1500`; `ExtraLong:200/300/600/1000/2000`).
  Properties `shorterDuration/shortDuration/mediumDuration/longDuration/extraLongDuration`
  `:1045-1050`. Easings `standardEasing: Easing.OutCubic`, `emphasizedEasing: Easing.OutQuart`
  `:1051-1052`. Per-element durations `popoutAnimationDuration` `:1200`, `modalAnimationDuration`
  `:1212`, `notification{Enter,Exit,Expand,Collapse,Inline{Expand,Collapse}}Duration`
  `:1150-1182`. Curve table `expressiveCurves` (`emphasized`/`standard`/...) `:1056-1066`.
  Variants (`isDirectionalEffect`/`isDepthEffect`/`isConnectedEffect`) forwarded from
  `Common/AnimVariants.qml` `:1079-1095`; collapse scale + offset
  `Theme.effectScaleCollapsed`/`effectAnimOffset`. Custom curves `enter/exit` x `popout/modal`
  from `AnimVariants` (consumed by popouts/modals `Widgets/DankPopoutStandalone.qml:25-28`,
  `Modals/Common/DankModalStandalone.qml:36-37`). `snapshotListModelChanges: shortDuration <= 0`
  `:1047`.

### 6. Placement / geometry

- Per-output: `Variants { model: Quickshell.screens }` for the bar
  `Modules/DankBar/DankBar.qml:163-180`; dock uses same pattern `Modules/Dock/Dock.qml:18`.
  Filter by `SettingsData.barConfigs[*].screenPreferences`; `showOnLastDisplay` fallback when
  single screen `:167-170`. `screenName` = `modelData.name`
  `Modules/DankBar/DankBarWindow.qml:373`.
- Popout trigger math: `Common/SettingsData.qml:2200-2241`
  `getPopupTriggerPosition(pos, screen, barThickness, widgetWidth, barSpacing, barPosition,
  barConfig)` -> `{x,y,width}` per `Position.{Left,Right,Bottom,Top}` honoring
  `popupGapsAuto`/`popupGapsManual`. Adjacent-bar offsets: `getAdjacentBarInfo` `:2243-2312`,
  `getBarBounds` `:2314+` (`hasAdjacent{Top,Bottom,Left,Right}Bar` properties
  `DankBarWindow.qml:687-810`). Edge clearance helper `_frameEdgeInset(side)`
  `Widgets/DankPopoutStandalone.qml:103`. Used by `barWindow.triggerControlCenter/Dash/Battery/etc.`
  `Modules/DankBar/DankBarWindow.qml:38-119`.
- Clamping (popout): `Widgets/DankPopoutStandalone.qml:504-545` — `alignedX/alignedY` clamp via
  `Math.max(leftGap, Math.min(screenWidth - popupWidth - rightGap, ...))`. Same pattern for
  vertical bars and centered positioning. `dpr` = `CompositorService.getScreenScale(screen)`
  `:431`. Full-height variant `fullHeightSurface` `_fullHeight` => top anchored to bottom edge
  `:684-690`.
- Bar edge clamp: `inputMask` masks + `exclusiveZone`
  `Modules/DankBar/DankBarWindow.qml:614-643,764`. Frame integration (`usesFrameBarChrome`)
  `FrameWindow` reserves instead of bar (`reserveExclusiveWhenAutoHidden`) `:557,764`.
- Section-relative anchors: `triggerSection` `left|center|right` (`Widgets/DankPopout.qml:21`,
  consumed in `DankPopoutStandalone.qml:728-740`). `autoBarShadowDirection` derived from bar
  position+section `:138-167`.
- Popout manager / single instance: `Common/PopoutManager.qml:42-58` `currentPopoutsByScreen` map;
  closing on other screen before opening `:154-178`; screenshot handshake `:17`; hover-dismiss
  `:39-50`.
- Modal manager: `Common/ModalManager.qml:18-30` `currentModalsByScreen`; `keepPopoutsOpen` and
  `allowStacking` opts `Modals/Common/DankModalStandalone.qml:42-44`.
- Connected-frame mode: `CompositorService.usesConnectedFrameChromeForScreen` /
  `effectiveConnectedFrameModeActive` (FrameTransitionState) drives borderless docking
  (`DankPopoutConnected.qml`), `connectedSurfaceRadius`, `connectedSurfaceColor`,
  `connectedCornerRadius` `Common/Theme.qml:1082-1093`; `ConnectedModalChrome.qml` lease bridge
  for IPC.
- Launcher/spotlight: `DankLauncherV2ModalSpotlight.qml:64-91` width-fixed 680px, frame-inset
  aware, layers via `DMS_MODAL_LAYER`.
- OSD position: `osdPosition` enum `SettingsData.Position.*` (TopCenter/TopLeft/.../BottomCenter
  default `931`); vertical layouts `SettingsData.Position.LeftCenter|RightCenter`
  (`Widgets/DankOSD.qml:127`); offsets honor all bars + dock `Widgets/DankOSD.qml:131-170`.
- Multi-output: popouts/menus routed by `screen.name`; `DankBar` `Variants` per-screen;
  `OSDManager.screensChangedDelayTimer` (3s) `Common/OSDManager.qml:12-23` cleans stale OSDs on
  screen removal. `KeyboardFocus.registerBarWindow/unregisterBarWindow` track bars by name for
  hover dismissal `Common/KeyboardFocus.qml:36-48`. Connected-frame lease per-screen via
  `ConnectedSurfaceLease`.

## Cross-shell comparison (distilled for design)

| Concern | Noctalia | DMS | Parity implication |
|---|---|---|---|
| Panel surface type | layer-shell panel, Attached (bar-edge) or Floating (8-corner/center/auto) | layer-shell popout anchored to bar section, dual-window (bg+content); centered fullscreen modals | Both anchor popouts to the triggering bar; both clamp to output; both use exclusiveZone -1/0 so the bar keeps its reservation |
| Keyboard focus | Per-panel Exclusive/OnDemand/None; Exclusive relaxes to OnDemand after 100ms; initial focus element per panel | Per-surface keyboardFocus helper (Exclusive default, OnDemand on Hyprland focus-grab, None for OSD/toast) | sysc-shell needs a per-panel keyboard policy enum + one-shot exclusive grab + escape handling |
| Escape | PanelManager Escape -> panel global key -> dismiss transient -> close | focusHelper Keys.onPressed Escape -> close; modals closeOnEscapeKey default true | Escape-to-close is universal |
| Focus restoration | implicit (close returns focus; auto-paste path skips animation) | explicit: capture ToplevelManager.activeToplevel, re-activate on close | Roadmap gate "focus restoration" matches DMS explicit pattern |
| Outside dismiss | click shield surface or hyprland-focus-grab-v1 | hover-dismiss + screenshot handshake in PopoutManager | Outside-click dismissal expected |
| Controls | Toggle, Slider, RangeSlider, Input, Checkbox, Radio, Select, Segmented, Stepper, ScrollView, VirtualList/Grid, Button variants, Graph, KeybindRecorder, ColorPicker | DankToggle, DankSlider, DankTextField, DankDropdown, DankTabBar, DankButton, DankFlickable/ListView/GridView, DankScrollbar, NumberStepper, IconPicker | Roadmap list (scroll area, virtual list, toggle, slider, menu, text field, tabs, graphs) is the intersection both shells need |
| Settings | separate xdg-toplevel, 21-section sidebar, global search, TOML schema -> control factory | centered modal, ~9 categories / ~40 tabs, JSON search index, settings.json persistence | Both: sidebar + searchable settings; sysc-shell roadmap says "settings panels for bar and widget configuration" |
| Hotkeys | internal keybind matcher + IPC `panel-toggle <id>` | compositor-side keybinds spawning `dms ipc call <target> toggle` | On Niri, DMS pattern = niri keybind -> IPC toggle; Noctalia also supports IPC toggle |
| Look tokens | 16 ColorRoles (M3-like), radius 3/6/9/12, spacing 4/8/12/16, control heights 32/38/44, fonts 11/13/14/16/20, anim 100/200/400ms, reduceMotion | Material3 tokens via matugen, radius 12, spacing 2/4/8/12/16/24, fonts 12/14/16/20 (Inter/Fira Code), elevation 5 levels, anim presets None..ExtraLong with variants | Both: token-based theming, dark/light, small radius scale, ~100-300ms motion, motion-disable path |
| Multi-output | focusedInteractiveOutput fallback chain; per-output OSD | per-screen Variants; PopoutManager per screen; stale cleanup on screen removal | Panels must be per-output instances keyed like M3 widgets |
