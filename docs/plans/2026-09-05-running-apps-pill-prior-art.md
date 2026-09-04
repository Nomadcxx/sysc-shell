# Running-apps bar pill — prior art

Date: 2026-09-05.

Sources (2026-09-05):

- DMS tree `/home/nomadx/Documents/GitHub/DankMaterialShell`
  (`Modules/DankBar/Widgets/RunningApps.qml`, `FocusedApp.qml`,
  `WorkspaceSwitcher.qml`, `Modules/Dock/*`, `Services/CompositorService.qml`,
  `Services/NiriService.qml`, `Common/Paths.qml`)
- This machine's live DMS settings `~/.config/DankMaterialShell/settings.json`
- Noctalia tree `/home/nomadx/noctalia`
  (`src/shell/bar/widgets/taskbar_widget.{h,cpp}`, `src/shell/dock/*`,
  `src/wayland/wayland_toplevels.{h,cpp}`,
  `src/compositors/niri/niri_workspace_backend.cpp`,
  `src/system/app_identity.{h,cpp}`)
- This machine's live Noctalia settings `~/.config/noctalia/settings.json`
- sysc-shell: `internal/platform/niri`, `internal/shell/widget.go`,
  `internal/shell/projection.go`, `internal/icons`, launcher via
  `github.com/Nomadcxx/sysc-launch`

Method: read-only source inspection. Layout numbers are from code, not
screenshots. No live grim of a running-apps pill was taken for this note;
DMS on this machine does not currently place `runningApps` on either bar.

This is findings, not a design. Open forks are in §8.

---

## 1. What you are looking at

Three different products share "icons of open apps". They are not interchangeable.

| Product | Surface | Shows | Typical click | Typical right-click |
|---|---|---|---|---|
| **DMS RunningApps** | One **bar capsule** | Open windows (optionally filtered) | Activate that window | Close only |
| **DMS Dock** | Separate layer-shell dock | Pinned + running | Activate / cycle / launch | Pin, desktop actions, close, close-all |
| **DMS FocusedApp** | One **bar capsule** | The single focused window's icon + title | (title, not a switcher) | — |
| **Noctalia Taskbar** | One **bar widget** | Windows, optional workspace groups, optional pins | Activate or launch pinned | Pin, desktop actions, close, close-all |
| **Noctalia Dock** | Separate layer-shell dock | Pinned + running | Activate / cycle / launch | Pin, desktop actions, close, close-all |
| **Noctalia ActiveWindow** | One **bar widget** | The single focused window's icon + title | — | — |

The request ("dynamic **pill** on the **bar**", Steam / Firefox / Brave icons
when those apps are loaded, right-click) maps to **DMS RunningApps** for
placement and chrome, with **Noctalia Taskbar** as the richer interaction
reference. It does **not** map to a dock: a dock is a second surface, pinned
slots, magnification, and auto-hide. The owner asked for a bar pill.

Related but different: DMS `showWorkspaceApps` draws tiny app icons **inside
workspace dots**. That is occupancy decoration, not a running-apps pill. This
machine has it off (`showWorkspaceApps: false`).

### What this machine actually ran

DMS bars (DP-3 "Main Bar", DP-1 "Bar 2"):

```
left:  launcherButton, workspaceSwitcher, focusedWindow
```

`showDock: false`. `runningApps` is **not** on either bar. Compact / all-workspaces
defaults are still in settings (`runningAppsCompactMode: true`,
`runningAppsCurrentWorkspace: false`).

Noctalia:

```
bar left:  SystemMonitor, ActiveWindow (icon+text, maxWidth 145), MediaMini, …
dock:      enabled, bottom, floating, pinnedApps = [], onlySameOutput = true
```

No `Taskbar` widget on the bar. The running-app icons lived on the **dock**,
which with an empty pin list is a running-only strip on a separate surface.

So the daily-driver history is: focused-window title on the bar, plus (under
Noctalia) a bottom dock. The new request is to put the running-app icons **on
the bar**, in a capsule.

---

## 2. DMS RunningApps (the bar pill)

File: `Modules/DankBar/Widgets/RunningApps.qml`.

Settings copy: "Shows all running applications with focus indication."

### Chrome

One `Rectangle` capsule. Radius = `Theme.cornerRadius`. Fill =
`Theme.widgetBaseBackgroundColor` at widget transparency. Horizontal padding
`Theme.spacingS` unless `dankBarNoBackground`. **Hidden when `windowCount === 0`**
(`visible` and `calculatedSize` both collapse). Clip is off so a context menu
can hang off the pill.

The pill **grows with the set**. Compact:

```
size = n * 24 + (n-1) * spacingXS + 2 * horizontalPadding
```

Each tile is 24×24; the icon inside is 18×18. Expanded mode adds a 120 px
title column per tile (`24 + spacingXS + 120`). This machine uses compact
(`runningAppsCompactMode: true`).

Tiles sit in a `Row` (`spacingXS`). Vertical bars swap to a `Column` (sysc-shell
has no vertical bar yet).

### Data

Model: `CompositorService.sortedToplevels`, which is Quickshell
`ToplevelManager` (wlr-foreign-toplevel) **reordered** on Niri by matching each
toplevel to a Niri window (`NiriService.sortToplevels`). Match key: `appId`
plus title. Activate is **not** the Wayland toplevel activate; it is wrapped to
`NiriService.focusWindow(niriWindow.id)` because foreign-toplevel activate is
unreliable on Niri.

Optional filter: `runningAppsCurrentWorkspace` runs
`NiriService.filterCurrentWorkspace` (that output's active workspace). Default
off — the pill lists **every** open window on the session. Settings also has
`runningAppsCurrentMonitor` and `runningAppsGroupByApp`; **neither is read by
`RunningApps.qml`**. Grouping lives on the Dock only. The bar pill is one tile
per toplevel.

Settings `barMaxVisibleRunningApps: 0` means no overflow cap in the live file;
overflow UI exists as a flag (`barShowOverflowBadge`) but the widget itself
does not clip.

### Per-tile appearance

- Icon via `DesktopEntries.heuristicLookup(moddedAppId(appId)).icon` then
  `Quickshell.iconPath`.
- `app_id` containing `steam_app` (case-insensitive): skip the theme icon and
  paint a Material `sports_esports` glyph. Steam games rarely have desktop
  files matching `steam_app_NNNN`.
- Else if the image is not ready: first letter of the desktop-entry name, else
  first letter of `appId`, else `?`.
- Focused tile: primary fill at 0.2 opacity (0.3 on hover). Unfocused hover:
  `primaryHover` at 0.1. Idle unfocused: transparent on the shared capsule.
- Expanded mode: elided window title to the right of the icon.
- Tooltip: `"AppName • WindowTitle"`.

`Paths.moddedAppId` is a four-entry rewrite (Spotify → `spotify-launcher`,
`beepertexts` → `beeper`, Home Assistant, Transmission). The live settings
file also has a longer `appIdSubstitutions` table; RunningApps uses the
hardcoded function, not that table.

### Interaction

| Input | Behaviour |
|---|---|
| Left click | `toplevel.activate()` (Niri `focusWindow` after the join) |
| Right click | Overlay `PanelWindow` (layer Overlay, exclusiveZone -1, keyboard None) with a **single item: Close** (100×32). Click-outside dismisses. **Does not paint `.desktop` Actions.** |
| Wheel | Cycle activate among listed windows. Mouse wheel steps immediately; touchpad accumulates to 500. |
| Hover | Tooltip |

RunningApps is a **window** tile. The app-specific menu (Steam Store/Library/Friends,
Firefox New Window / Private) lives on the Dock, the launcher, and Spotlight —
not on this widget. See §3 and §4a.

---

## 2a. The app's own right-click menu is `.desktop` Actions

This is not a Steam special case and not a live menu from the running process.

Freedesktop desktop files carry extra `Exec` targets under `[Desktop Action …]`.
The shell looks up the window's `app_id` in the desktop-entry index and appends
`entry.actions` to the context menu. That is the entire "Steam has its own
menu" behaviour.

This machine, `/usr/share/applications/steam.desktop`:

```
Actions=Store;Community;Library;Servers;Screenshots;News;Settings;BigPicture;Friends;
```

Each action is `steam steam://…` (Library → `steam://open/games`, Settings →
`steam://open/settings`, Friends → `steam://open/friends`, …). Firefox ships
`new-window`, `new-private-window`, `open-profile-manager`. Brave ships
`new-window`, `new-private-window`. An app with no `Actions=` key contributes
zero extra rows; the shell-added Close / Close all still appear.

Spotify is the useful negative. `/usr/share/applications/spotify.desktop` on
this machine has **no** `Actions=` key (Name, Exec, Icon, StartupWMClass only).
Discord and mpv are the same. A Spotify right-click that shows Play / Next is
the **tray** StatusNotifierItem D-Bus menu, or the bar **MPRIS** pill — not
the running-apps / dock icon. DMS only special-cases Spotify for icon lookup
(`appId === "Spotify"` → `spotify-launcher` in `Paths.moddedAppId`).

Inventory of **visible** desktop files with `Actions=` on this machine
(2026-09-05, `/usr/share/applications` + user + Flatpak exports, Hidden /
NoDisplay skipped): **38 files**. Not an Electron club. Native examples:

| App | File | Actions |
|---|---|---|
| Steam | `steam.desktop` | Store, Community, Library, Servers, Screenshots, News, Settings, Big Picture, Friends |
| LibreOffice | `libreoffice-startcenter.desktop` | Writer, Calc, Impress, Draw, Base, Math |
| Spectacle | `org.kde.spectacle.desktop` | 9 capture / record targets |
| Firefox | `firefox.desktop` | New Window, New Private Window, Profile Manager |
| Brave / Chromium | `brave-browser.desktop` / `chromium.desktop` | New Window, New Private Window |
| Konsole | `org.kde.konsole.desktop` | New Window, New Tab |
| Ghostty / Alacritty | `com.mitchellh.ghostty.desktop` / `Alacritty.desktop` | New Window |
| Nautilus / Thunar / Nemo | `org.gnome.Nautilus.desktop` etc. | New Window / Home / Computer / Trash |
| Parole | `org.xfce.Parole.desktop` | Play, Previous, Next |
| Sublime Text | `sublime_text.desktop` | New Window, New File |
| Mousepad / Xfce Terminal | `org.xfce.mousepad.desktop` etc. | Preferences |

The shell does not keep an app-specific menu table. It paints `entry.actions`
for whatever desktop file the `app_id` resolved to. Steam looks custom because
Valve shipped nine actions; Spotify looks empty because Spotify shipped none.

Where the references actually inject those rows:

| Surface | Paints `entry.actions` | Evidence |
|---|---|---|
| DMS Dock | yes | `DockContextMenu.qml:302` → `SessionService.launchDesktopAction` |
| DMS RunningApps (bar pill) | **no** | Close only, `:612-630` |
| DMS launcher / Spotlight / AppDrawer | yes | `SpotlightContextMenu.qml`, `AppDrawerPopout.qml` |
| Noctalia Taskbar | yes | `taskbar_widget.cpp:2574-2583` then close / close-all |
| Noctalia Dock | yes | `dock_context_menu.cpp:199-208` — windows, then actions, then close |
| sysc-shell launcher | yes | `openLauncherActions` in `popout_launcher.go`; D7. Already ships. |

This machine's Noctalia **dock is enabled** (empty pins, running-only). That is
the Steam right-click the owner used. DMS `showDock` is currently false, so a
DMS Steam menu today is the launcher/Spotlight path, not the bar.

This is **not** the tray icon. Steam also publishes a StatusNotifierItem with a
live D-Bus menu (Noctalia tray pins `"steam"`). That menu is the app talking
over tray protocol. The running-apps / dock / taskbar menu is the static
desktop-file list. Do not join those two.

Launch of an action is Niri `spawn` of the expanded `Exec`. Same compositor
seam as the launcher; the bar does not call `launcher.Service`.

---

## 3. DMS Dock (not the requested surface)

`Modules/Dock/Dock.qml` + `DockApps.qml` + `DockAppButton.qml` +
`DockContextMenu.qml`. Disabled on this machine (`showDock: false`).

Same toplevel source as RunningApps. Adds:

- Pinned apps persist even with zero windows (launchers).
- Optional `groupByApp`: one slot per `appId`, `windows[]` behind it.
- Ungrouped running windows that are not pinned append after the pin list.
- Left click: activate if one window, else cycle, else launch the desktop
  entry.
- Right click: window list (when grouped), `.desktop` actions, pin/unpin,
  optional "Launch on dGPU", Close Window / Close All Windows.

This is the interaction ceiling. It is a second surface with position, autohide,
overview-open, spacing, and transparency settings. Out of scope for a bar pill
unless a later slice explicitly asks for a dock.

---

## 4. Noctalia Taskbar (the bar widget)

Files: `src/shell/bar/widgets/taskbar_widget.h` (~230 lines of options alone),
`taskbar_widget.cpp` (~3100 lines).

Not on this machine's Noctalia bar. It is the compositor-portable, configurable
relative of RunningApps.

### Model

`TaskModel` per tile:

```
handleKey, order, appId, title, iconPath, workspaceKey,
desktopEntryId, instanceCount, active, pinned, running, firstHandle
```

Windows come from `wlr-foreign-toplevel-management` (and
`ext-foreign-toplevel-list` on KDE). Niri's event stream is a **second** source
used to assign windows to workspaces (`NiriWorkspaceBackend` tracks `is_focused`,
workspace membership, layout). Focus of a Niri window is
`requestAction({"FocusWindow": {"id": id}})` on the Niri IPC socket — JSON on
the compositor socket, not `niri msg` and not foreign-toplevel activate.

### Options that matter for a first cut

From `TaskbarWidgetOptions` / settings registry (`taskbar.windows`,
`taskbar.grouping`):

| Option | Default | Meaning |
|---|---|---|
| `show_all_outputs` | false | Else this bar's output only |
| `only_active_workspace` | false | Filter to the active workspace |
| `group_by_workspace` | false | Nested workspace capsules around tiles |
| `group_single_icon_per_app` | false | Collapse instances to one icon |
| `show_window_title` | false | Icon + truncated title |
| `show_active_indicator` | true | Focus chrome |
| `pinned` | [] | Pinned-not-running tiles (launchers) |
| `minimal` | false | Tighter chrome |

Workspace grouping is a second product: labels, empty-workspace hide, icons vs
count vs dots inside the group. That is not a running-apps pill.

### Identity and icons

`app_identity::findDesktopEntry` matches compositor `app_id` against desktop
file id, `StartupWMClass`, and name, with a punctation-stripped identity key
and a last-segment tail fallback (`org.mozilla.firefox` → `firefox`). Tests in
`tests/app_identity_test.cpp` cover colliding Chat/Mail desktop files.

Icon path is resolved through `IconResolver` (XDG theme), same family as the
launcher and dock. Colorization of app bitmaps is a separate bake
(`ui/app_icon_colorization.h`); this machine's Noctalia dock has
`colorizeIcons: false`.

### Interaction

Left click: `activateTaskModel` / `activateOrLaunchPinned`. Scroll:
`cycleAdjacent` (workspace groups when grouping, else tasks). Right click:
`openTaskContextMenu`.

Menu, in order:

1. Pin / Unpin (only when not grouping by workspace and a desktop entry exists)
2. Desktop-file actions
3. Separator
4. Close (primary window)
5. Close all (when `windowsForApp` returns more than one)

Menu host is `ContextMenuPopup` parented to the bar layer surface. Close uses
`zwlr_foreign_toplevel_handle_v1.close`; on Niri, focus uses the Niri action
above.

Pins merge into the flat strip (`applyPinnedMerge`). Group-by-workspace ignores
pins.

---

## 5. Noctalia Dock (again, not the bar pill)

Enabled on this machine, empty `pinnedApps`, `onlySameOutput: true`,
`groupApps: false`, `groupClickAction: cycle`. That configuration is "icons of
whatever is open on this output", on a bottom floating dock.

Same identity/icon/menu family as the taskbar. Extra: magnification, badges /
dots for instance count, drag-reorder of pins, launcher glyph at start/end.

Useful as interaction prior art (cycle vs focus, instance badge). Not as
surface prior art for a bar capsule.

---

## 6. How "running UI apps" are actually selected

Neither shell scans `/proc`.

A window exists in the compositor's toplevel list. That **is** the UI filter:
headless processes never appear. Steam the client is one toplevel (`steam`); a
Proton game is often another (`steam_app_NNNN`). Firefox / Brave are one
toplevel per window, same `app_id`.

DMS on Niri still binds `ToplevelManager` and **joins** it to Niri windows by
`(app_id, title)` so it can call Niri focus/close. The join is a Quickshell
portability leftover. Noctalia keeps foreign-toplevel for activate/close on
compositors that implement it, and talks to Niri IPC for focus and workspace
assignment.

sysc-shell is Niri-only. The Niri event stream already publishes every window
(`internal/platform/niri`: `id`, `title`, `app_id`, `workspace_id`). That list
is the model. Foreign-toplevel is not required for a first cut.

Gaps in the current projection:

- `is_focused` is on the wire (Noctalia reads it) and is dropped.
- `pid` is on the wire in some events and is dropped (not needed if we do not
  scan processes).
- The niri package is **event-stream only**. It never sends an action. Launcher
  spawn shells out to `niri msg action spawn`. Focus/close for this widget need
  `FocusWindow` / `CloseWindow` (Noctalia sends JSON on the same socket family;
  argv `niri msg action focus-window --id` is the existing `runArgv` style).
- `projectOutputs` collapses windows to **one title per output**. The pill
  needs the window slice, not the title join.

---

## 7. What sysc-shell already has

| Piece | Where | Reuse |
|---|---|---|
| Niri window list | `internal/platform/niri` | Extend fields; do not add a process scanner |
| Per-output title + workspace pills | `internal/shell/projection.go` | Sibling projection, not a replacement |
| Bar capsule chrome | `KindCapsule` in `widget.go` | The pill shell |
| Bar item vocabulary | `config.knownItems` | New id, e.g. `running-apps` |
| Theme app icons (raster) | `internal/icons` Resolver + worker | Same path as launcher / tray. SVG-only themes still miss (M5 decision) |
| Desktop entries | `go-freedesktop/desktopentry` in sysc-shell | `app_id` → icon name / actions. Independent of the launcher widget. |
| `KindImage` on the bar | tray, launcher rows | 18 px tiles |
| Right-click overlay | tray `KindMenu` / panel host | Close menu. DMS RunningApps is Close-only; Noctalia/DMS Dock are richer |
| Click actions on bar widgets | `registry.bindBarPanelActionsLocked` | New action ids for focus / menu |
| Window-title widget | `window-title` | Stays. Running-apps does not replace it (DMS keeps both; this machine uses FocusedApp and not RunningApps) |

`sysc-81` (launcher windows/Niri-focus provider) is a search provider, not this
pill. `sysc-86` / `sysc-117` (real theme icons in the launcher) share the icon
resolver. `sysc-103` (bar roster thinner than the reference) is the roster gap
this widget would fill one slot of.

---

## 8. Open forks (for the design, not decided here)

These are the product choices the references disagree on. A design has to pick.

1. **Slot identity.** One icon per window (DMS RunningApps, Noctalia taskbar
   default) vs one icon per application (Noctalia `group_single_icon_per_app`,
   DMS Dock `groupByApp`). Steam-with-a-game is two windows either way unless
   grouped; two Firefox windows are two icons unless grouped.
2. **Which windows.** All session windows (DMS RunningApps default) vs this
   output (Noctalia taskbar / dock `onlySameOutput`) vs this output's active
   workspace (`runningAppsCurrentWorkspace` / `only_active_workspace`).
3. **Right-click depth.** The app-specific rows are not optional chrome: they
   are `Desktop Entry` `Actions=`, which is how Steam paints Store/Library/
   Friends and Firefox paints New Window / Private. DMS Dock, Noctalia
   Taskbar, Noctalia Dock, and the sysc-shell launcher already do this.
   DMS RunningApps does **not** (Close only) — that is why a Close-only bar
   pill would feel wrong for Steam. Remaining choice is only the
   **shell-added** footer: Close all (grouped icon) vs also listing each
   window title. Pin is a separate product.
4. **Titles in the pill.** Compact icons only (DMS default on this machine,
   Noctalia `show_window_title: false`) vs icon+title (DMS expanded, 120 px
   per tile). Titles fight the existing `window-title` widget.
5. **Pins.** Running-only (the request, DMS RunningApps) vs pinned-not-running
   tiles (Noctalia Taskbar, both docks). Pins are a second product.
6. **Empty state.** Hide the capsule (DMS RunningApps) vs reserve space vs show
   pinned-only.
7. **Steam games.** Generic gamepad (DMS) vs try `steam_app_NNNN` theme icons
   vs one Steam icon for all `steam`/`steam_app_*` when grouped.
8. **Action transport.** JSON `FocusWindow`/`CloseWindow` on a Niri request
   socket (Noctalia) vs `niri msg action …` argv (how sysc-shell already
   launches). Event stream stays read-only either way.

Recommended starting point, pending owner confirmation: DMS RunningApps chrome
(one capsule, compact 18 px icons, hide when empty, keep `window-title`) on
Niri windows directly, with slot identity and menu depth chosen in the design
rather than inherited from the unused Dock/Taskbar stacks.
