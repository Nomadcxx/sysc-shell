# Launcher Prior Art

Date: 2026-09-01.

Sources (2026-09-01):

- Noctalia 5.0.0 tree `/home/nomadx/noctalia` and
  <https://docs.noctalia.dev/noctalia/launcher/>
- DMS v1.5.3 `/usr/share/quickshell/dms/Modals/DankLauncherV2` plus
  `Services/AppSearchService.qml`, `Services/SessionService.qml`
- This machine's live DMS settings `~/.config/DankMaterialShell/settings.json`
- Go launchers/libraries: go-freedesktop, rkoesters/xdg, Elephant, Gofer, Walker,
  sahilm/fuzzy (see §6)

Method: read-only source inspection. Layout numbers are from code, not screenshots.

**There is no live grim of an open launcher in this repository.** Bar-parity assets
show the bar button only. Reconstruct pixels from §2 (Noctalia) and §5 (DMS).
Optional confirmation: grim of both shells (empty, typed, selected row, grid) under
`docs/plans/assets/2026-09-01-launcher/`.

---

## 1. What you are looking at

Noctalia's launcher is one Overlay layer-shell **panel**, not a fullscreen dimmed
desktop and not a Spotlight-style search island that grows with the query.

| Property | Default | Source |
|---|---|---|
| Preferred size | **560 × 500** logical px, then `scaled()` | `launcher_panel.h:57-58` |
| Keyboard | **Exclusive** for the whole open time | `launcher_panel.h:59` |
| Initial focus | the search `Input` | `launcher_panel.h:60`, `create()` at `:770` |
| Placement | **Floating**, screen position **center** | `config_types.h:912,920`; `panelPlacement()` `:689-691` |
| Open near click | **false** | `config_types.h:928` |
| Layer / exclusive zone | Overlay; centered panels use `exclusiveZone = -1` | M4 prior art, `panel_manager.cpp:834` |
| Namespace | `noctalia-panel` (same as other floating panels) | M4 prior art |
| Card padding | `Style::panelPadding` = **14** | `style.h:29`; applied by panel manager |
| Inner column gap | `spaceSm` = **8** | `launcher_panel.cpp:766` |
| Click shield | yes, when dismiss-on-outside and keyboard ≠ None | M4 research |

sysc-shell already matches this surface class: Overlay, Exclusive, click shield,
single instance, IPC toggle. Do not invent a second modal host.

Bar trigger: a **search glyph** (Tabler name default `"search"`, 16 px
`baseGlyphSize`) or a custom image. No label. Click opens the panel.
`launcher_widget.cpp:15-31`, `config_types.h:158` default start widgets include
`"launcher"`.

IPC: `noctalia msg panel-toggle launcher [query]` pre-fills the input (docs).
sysc-shell already has `panel.toggle` / `panel.open` / `panel.close` on
`$XDG_RUNTIME_DIR/sysc-shell/ipc.v1.sock`; known panels are clock, system-monitor,
session, settings (`internal/ipc/server.go:21-26`). Launcher is a new panel id.

---

## 2. Visual reconstruction (Noctalia default list)

This is the UI. Scale every length by the panel content scale. Tokens from
`src/ui/style.h:13-43`.

```text
Overlay layer-shell card, 560×500, radius from panel chrome, 14 px padding

┌──────────────────────────────────────────────────────────┐
│  Search field  height 38  font 14                        │
│  placeholder · clear button on the right                 │
│──────────────────────────────────────────────────────────│  gap 8
│  Compact segmented chips (hidden until categories exist) │
│  [grid] [history] [world] [code] [gamepad] …             │
│  icon-only, equal widths, tooltips are the labels        │
│──────────────────────────────────────────────────────────│  gap 8
│  Virtual list  (or 5-col grid when app_grid + apps-only) │
│                                                          │
│  ┌─ row, radiusMd 6, padH 8, padV 4, gap 12 ──────────┐  │
│  │ [40×40 icon or badge]  Title  14 SemiBold 1 line    │  │
│  │                        Subtitle 13 Normal 1 line    │  │
│  └─────────────────────────────────────────────────────┘  │
│     row gap 4                                            │
│  selected → fill Primary, text OnPrimary                 │
│  hover    → fill Hover,    text OnHover                  │
│  idle     → transparent (unless listItemBackground)      │
└──────────────────────────────────────────────────────────┘
```

### Search field

`create()` `launcher_panel.cpp:769-791`:

- `Input`, body font 14, **controlHeight 38**, horizontal padding 12 (`spaceMd`)
- placeholder i18n `launcher.search-placeholder`
- **clear button enabled**
- `onSubmit` activates the selected result
- `onKeyEvent` is launcher navigation (does not steal text editing)
- newlines in paste are flattened to a single-line preview (`:784-787`)

Readline chords while the input is focused (official docs, not re-derived here):
Ctrl+A/E/B/F, Alt+B/F, Ctrl+W/U/K, Alt+D.

### Category chips

Compact `Segmented`, equal segment widths, initially **not in layout**
(`:794-804`). Shown when the active provider exposes categories or there is a
recently-used set (`rebuildCategoryFilter` `:1372-1415`).

Chip order:

1. All — glyph `layout-grid`
2. Recently used — glyph `history` (only if usage tracking has hits)
3. Provider categories — each a Tabler glyph; tooltip is the human label

Applications map freedesktop `Categories` onto nine chips
(`app_provider.cpp:67-77`): internet/`world`, multimedia/`player-play`,
development/`code`, games/`device-gamepad-2`, graphics/`photo`,
office/`briefcase`, education/`school`, system/`settings`,
utilities/`tool`. Empty categories are omitted.

Emoji has its own people/animals/food/… chips (docs + `emoji_provider.h:20-21`).

**F6** reveals hidden chips without changing the filter; once visible, F6 / Shift+F6
cycle (`handleKeyEvent` `:1835-1837`, `:1812-1832`). This is the only dedicated
category keyboard.

### Result row (default list)

`LauncherResultRow` `:193-413`. Default (not compact, icons on):

| Part | Size |
|---|---|
| Icon / badge / fallback glyph | **40 px** (`kIconSizeDefault`) |
| Compact icon | **28 px** (`kIconSizeCompact`) |
| Title | font 14 SemiBold, 1 line, ellipsize |
| Subtitle | font 13, `OnSurfaceVariant`, 1 line; **hidden in compact** |
| Horizontal pad | 8 (`spaceSm`) |
| Vertical pad | 4 default / 2 compact |
| Icon-to-text gap | 12 default / 8 compact |
| Row corner radius | `radiusMd` 6, scaled |
| List row gap | 4 (`spaceXs`) |
| Overscan | 3 rows |

Leading visual priority (`bind` `:300-323`):

1. `result.badge` — emoji or single symbol, **replaces** the icon
2. else if `showIcons`: async file icon, glyph `app-window` (or `glyphName`) until loaded
3. else: no leading visual, text uses the full width

Selected / hover fills the **whole rounded row**, not a left accent bar
(`applyVisualState` `:377-394`):

- selected: `Primary` fill, `OnPrimary` title/icon, subtitle `OnPrimary` at 0.7
- hover, not selected: `Hover` fill, `OnHover`
- idle: clear, or `SurfaceVariant` if `shell.panel.listItemBackground` (default **false**)

### App grid (optional, not the default)

`shell.launcher.appGrid` default **false** (`config_types.h:943`). When it is on
**and every visible result is provider `Applications`**, the list becomes a
**5-column** icon grid (`kAppGridColumns`, `shouldUseAppGrid` `:918-928`). Mixed
calculator/emoji/session hits stay on the list.

Grid tile (`LauncherAppGridTile` `:415-`): centered column, pad 8, gap 4, icon 40,
caption 13 **two lines centered** under the icon. Same Primary/Hover fill. Column
and row gap 8. Left/right keys move by one cell; up/down by a row (`:1851-1868`).

### Empty and detail

Empty caption 13, `OnSurfaceVariant` (`:885-892`):

- no query: `launcher.empty.type-to-search`
- query with no hits: `launcher.empty.no-results`

Calculator can switch the body to a **detail** scroll (`presentation == "detail"`):
caption subtitle + wrapping body, list hidden (`:847-883`, `:1536-1542`). This is
the large-result view, not the default app list.

### Motion and chrome

Panel reveal/dismiss is the shared panel animation (100/200/400 ms, EaseOutQuad,
reduced-motion cancels). The launcher does not add its own open animation. Borders
and shadows follow `shell.panel.borders/shadow` (both default true). No fullscreen
dimmer; the M4 click shield is a transparent input surface, not a darkened scrim.

---

## 3. Interaction

| Input | Behavior | Source |
|---|---|---|
| Type | query all active providers; prefix routing | docs + `onInputChanged` |
| Enter / validate keybind | activate selected | `:1871-1873`, input `onSubmit` |
| Up / Down | move selection (by row in grid) | `:1851-1858` |
| Left / Right | grid only, move one cell | `:1861-1868` |
| PageUp / PageDown | page by `pageItemStride` | `:1839-1848` |
| F6 / Shift+F6 | show / cycle category chips | `:1835-1837` |
| Escape | panel cancel (shared panel keybind) | M4 + docs |
| Left click row | activate | adapter `onActivate` `:819` |
| Right click app | desktop-actions context menu | `onSecondaryActivate` `:820` |
| Activate prefix overview row | insert `prefix + " "` into the input, do not close | `:1761-1774` |
| Copy-style activate (calc, emoji, dmenu-without-exec) | close, then optional auto-paste via virtual keyboard | `supportsAutoPaste`, `setCopiedActivationCallback` `:53-55` |

Empty query for Applications returns **all desktop entries in stored (alpha) order**,
capped later by the virtual view (`app_provider.cpp:183-190`). Typed queries fuzzy-
match, cap **50** results (`kMaxSearchResults`).

Usage: providers that `trackUsage()` (Applications, Wallpaper, dmenu) get
`+0.1 * count` on the fuzzy score, capped at **+0.5** on typed queries so a weak
match cannot outrank a strong name hit (`launcher_panel.cpp:48-64`,
`usage_tracker.h`). Recently-used index drives the history chip. Persistence is
two files under state (counts + recent deque). `sortByUsage` default true;
turning it off drops persisted usage (`syncUsageTrackingState`).

---

## 4. Provider model (behavior inventory)

Interface: `src/launcher/launcher_provider.h`. Results are
`{id, providerId, title, subtitle, glyphName, iconName, iconPath, badge,
desktopActionId, category, presentation, query, score, recentlyUsedIndex}`.

Shared prefix character default `/` (`providerPrefix`). Type `/` alone → overview
of every provider that has a non-empty prefix; activating one inserts the prefix
(`providerOverviewResults` `:1450-1486`).

| Provider | Id | Default trigger | Global? | Activate | Notes |
|---|---|---|---|---|---|
| Applications | `Applications` | none (always global) | yes | spawn desktop Exec | usage, categories, 50-hit cap, Terminal=true |
| Calculator | `Calculator` | `calc` → `/calc ` | **yes** | copy result | libqalculate; global only if query contains a digit; `presentation=detail` |
| Emoji | `Emoji` | `emo` | no | copy character | badge is the emoji; categories |
| Wallpaper | `Wallpaper` | `wall` | no | apply wallpaper + maybe theme | usage; M7 wallpaper slice, not launcher v1 |
| Session | `Session` | `session` | no | same path as session panel | sysc-shell already has a session popout |
| Windows | `Windows` | `win` | no | compositor focus | Niri window list already in the shell projection |
| Dmenu entry | config id | e.g. `/cmd` | optional | `exec` or copy | `/bin/sh -lc`; `{selection}` `{query}` |
| `noctalia dmenu` | stdin session | scoped, no overview | — | print line to stdout | one-shot picker |
| Plugin | manifest | prefix from manifest | optional | async | M6 deferred launcher providers |

Application scoring (`app_provider.cpp:24-56`): name ×5 plus +500 for prefix,
generic name ×2, keywords ×0.8, categories ×0.3, desktop id ×1.5, exec unweighted.
Then usage boost. Desktop actions are a right-click menu by default;
`show_app_actions` (docs) can search them as extra rows — **not in the inspected
`LauncherConfig` struct**; treat as a later flag.

Official docs also mention pin markers, package-origin badges (Flatpak/Snap/Nix/
AppImage), and `show_app_origin_indicator`. Confirm against current headers before
putting them in a v1 visual spec; the inspected `LauncherConfig` has
categories, showIcons, compact, appGrid, sortByUsage, fetchExchangeRates,
providerPrefix, autoPaste, dmenu, providers — not pin/origin flags. Docs and
headers have drifted. **Headers win for v1 chrome; docs win for provider
behavior** unless the designing agent re-reads both.

Calculator depends on **libqalculate** (C++). sysc-shell cannot take that
dependency as-is. A Go calculator is a separate decision (stdlib `expr`, a subprocess,
or omit from v1).

---

## 5. DMS launcher (first-class)

DMS is the launcher this machine actually runs. Settings
(`~/.config/DankMaterialShell/settings.json`):

| Key | Value | Meaning |
|---|---|---|
| `launcherStyle` | `"full"` | standalone card, **not** Spotlight |
| `dankLauncherV2Size` | `"compact"` | **620 × 600** (`DankLauncherV2ModalStandalone.qml:42-69`) |
| `launcherUseOverlayLayer` | `false` | Top layer unless something else forces Overlay |
| `dankLauncherV2ShowFooter` | `true` | All / Apps / Files / Plugins chips |
| `dankLauncherV2ShowSourceBadges` | `true` | Flatpak/Snap/… badge on tiles |
| `dankLauncherV2IncludeFilesInAll` | `false` | files stay in Files mode |

`DankLauncherV2Modal.qml:66` picks backend: Spotlight only when
`launcherStyle === "spotlight"` and connected-frame is off; otherwise standalone
or connected.

### Three chromes

| Variant | Geometry | Empty state | File |
|---|---|---|---|
| **Standalone (this machine)** | centered fixed card; micro 500×480 / compact **620×600** / medium 720×720 / large 860×860 | results area filled (apps list) | `DankLauncherV2ModalStandalone.qml` |
| **Spotlight** | width min(680, screen−80); Y ≈ 33% of usable height; height **grows with query** | search bar only until `_hasQuery` (`SpotlightLauncherContent.qml:17-25`) | `DankLauncherV2ModalSpotlight.qml:59-80` |
| **Connected** | emerges from a frame edge | same content as standalone | `DankLauncherV2ModalConnected.qml` |

sysc-shell M4 panels are Exclusive overlay cards with a shield. **Standalone
maps onto that vocabulary. Spotlight and Connected do not.**

### Standalone visual (the one on this laptop)

```text
Centered card 620×600, elevation shadow, Theme.cornerRadius

┌──────────────────────────────────────────────────────────┐
│  [plugin badge if filtered]  DankTextField               │
│  1 px border; focused border = primary                   │
│──────────────────────────────────────────────────────────│
│  Results list (or 4-col grid)                            │
│  row 52: 36 px icon, name, subtitle                      │
│  selected: primaryPressed fill, 90 ms color anim, ripple │
│──────────────────────────────────────────────────────────│
│  Footer 36 px: [All] [Apps] [Files] [Plugins]            │
│  plus key-hint cluster when actions exist                │
└──────────────────────────────────────────────────────────┘
```

Search field: `LauncherContent.qml:472-478` — `DankTextField`, themed
`surfaceContainerHigh` fill, 1 px outline, primary when focused. Optional
primary-colored plugin-name pill to the left (`:435-470`).

Footer (`:298-361`): 36 px (76 in connected-arc). Mode chips 28 px tall,
selected `Theme.buttonBg`. Ctrl/Alt+1..4 switch modes (`:257-284`). Hidden when
size is `micro` or `dankLauncherV2ShowFooter` is false.

Grid: `appLauncherGridColumns` default **4** (`SettingsData.qml:516`).
`GridItem.qml`: icon size `clamp(32, width*0.45, 48)`, source badge top-right,
selected uses **primary text** on the label (not OnPrimary on a Primary fill).

### Spotlight visual (not this machine's default)

Search bar **56** px, circular 36 px leading well with a 20 px search/folder/
extension icon, pill **mode chips** (26 px, selected = primary fill) that appear
on query (`SpotlightLauncherContent.qml:274-350`). Results: row **64**, icon
**well 40×40** (border 1, inner icon 30, letter fallback), type badge, optional
clipboard/image preview (`SpotlightResultRow.qml`). Max results height
min(430, 55% screen). No results until the query is non-empty.

### Interaction and data

| Input | Behavior | Source |
|---|---|---|
| Enter | execute selected | `LauncherContent.qml:237-247` |
| Shift+Enter | paste selected (clipboard items) | `:239-241` |
| Up/Down, Ctrl+J/K | move | `:156-194` |
| PageUp/Down | ±8 | `:162-167` |
| Left/Right, Ctrl+H/L | grid cell | `:168-210` |
| Tab | action panel | `:228-231` |
| F10 / Menu | context menu | `:249-255` |
| Ctrl/Alt+1–4 | All / Apps / Files / Plugins | `:257-284` |
| Escape | close (or clear plugin filter) | `:135-144` |
| Right click | context: desktop actions, dGPU, edit app | `LauncherContextMenu.qml`, `ActionPanel.qml` |

Index comes from Quickshell `DesktopEntries.applications` (`AppSearchService.qml:51-52`),
not a Go parser. Hidden apps and per-app overrides live in `SessionData`.
`maxResults` on the service is **10** (`:25`) for some cached paths; `Scorer.js`
`groupBySection` caps **50** per section (`:182`). Do not treat 10 as the product
cap without re-reading the call site the design cares about.

Scoring (`Scorer.js` + `searchApplications` `:513-607`):

| Signal | Weight |
|---|---|
| exact name | 10000 |
| name prefix | 5000 |
| word-boundary | 3000 |
| substring | 500 |
| genericName prefix / includes | 800 / 400 |
| id includes | 350 |
| keyword prefix / includes | 300 / 150 |
| comment includes | 50 |
| fuzzy (Levenshtein, query ≥ 3) | ×100 |
| type bonus (app) | +1000 (`Scorer.js:10-17`) |
| frecency | time-bucket × usage, cap 2000 |
| used today / week / month | +1500 / +1000 / +500 |

Frecency buckets: 4 / 14 / 31 / 90 days at weights 100 / 70 / 50 / 30 / 10
(`AppSearchService.qml:28-48`, `calculateFrecency` `:470-510`). This is richer
than Noctalia's `+0.1 * count` capped at 0.5 on typed queries.

Launch (`SessionService.qml:220-275`): argv from Quickshell's already-expanded
`desktopEntry.command`; optional NVIDIA prefix; optional `launchPrefix`;
`Terminal=true` → `terminal -e sh -c …`; otherwise `Quickshell.execDetached`.
**Not** Niri `spawn`. sysc-shell must use Niri; copy the *policy* (argv, terminal
wrapper, no shell for ordinary Exec) not the helper.

Bar button: `LauncherButton.qml` BasePill, Material `apps` / distro / dank /
niri.svg. That is DMS bar chrome, not a requirement for a first sysc-shell bind.

Plugins, files, clipboard-in-launcher, app-edit view, dGPU launch: later M7, not
v1.

---

## 6. Go launchers and libraries (what we can actually borrow)

sysc-shell is BSD-3-shaped (sibling repos are BSD-3; do not pull GPL). UI stays
in `internal/ui`. The useful borrow is **index + Exec expansion + icon lookup**,
not a second Wayland client.

### Use (first rung)

| Module | License | What it is | Borrow? |
|---|---|---|---|
| [`github.com/go-freedesktop/desktopentry`](https://github.com/go-freedesktop/desktopentry) | **BSD-3**, CGO=0, Go 1.26.4 | `Scan` / `ScanDirs`, Hidden/NoDisplay tombstones, `ExpandExec` field codes, actions, `ShouldShowIn` | **Yes.** This is the desktop-file layer a launcher needs. Pin a version. Qualify with table tests on real `.desktop` files. Created 2026-08-06 (young); coverage claim is 100% — still pin and test. Our `go.mod` is `go 1.26`; the module asks `1.26.4` — bump toolchain if `go get` insists. |
| [`github.com/go-freedesktop/icontheme`](https://go-freedesktop.github.io/) | BSD-3 | Icon Theme spec: name → path at size/scale, inheritance, hicolor | **Yes, when KindImage exists.** M5 already needs SVG/theme icons. Do not invent a second lookup. |
| [`github.com/rkoesters/xdg`](https://github.com/rkoesters/xdg) | BSD-3 (2013) | Low-level keyfile / desktop parse | Only if `desktopentry` is rejected; `desktopentry` already stands on it. |
| [`github.com/adrg/xdg`](https://github.com/adrg/xdg) | MIT | XDG base dirs | Already the usual Go choice; `desktopentry` uses it. |

`ExpandExec` is the piece not worth rewriting: `%f %F %u %U %i %c %k %%`, quoting,
deprecated codes dropped. Launch **argv through Niri**, not `os/exec` from the
README snippet — the library stops at argv.

[`github.com/go-freedesktop/menu`](https://go-freedesktop.github.io/) (applications.menu
XML → category tree) is optional. Noctalia and DMS both use a **fixed chip list**,
not the full menu spec. Skip until category chips exist.

`go-freedesktop/notifications` is a competing notify stack. Ignore; we have
`sysc-notify`.

### Scoring: do not import a matcher blindly

| Library | License | Note |
|---|---|---|
| [`sahilm/fuzzy`](https://github.com/sahilm/fuzzy) | MIT, stdlib-only | Sublime-style filename fuzzy. Fine for a first matcher. Weights will not match Noctalia or DMS. |
| Port Noctalia/DMS weights as **our** table tests | — | Better visual/ranking parity. Small enough to write. |

Recommendation: write the scorer (Noctalia weights for v1, or DMS frecency if the
owner wants this-laptop ranking). Use `sahilm/fuzzy` only if a table test shows
it is smaller than 40 lines of subsequence matching.

### Do not import

| Project | Language / license | Why not |
|---|---|---|
| **[Elephant](https://github.com/abenz1267/elephant)** | **Go**, **GPL-3.0** | Closest Go *launcher backend* (desktop apps, files, clipboard, unix+protobuf, `.so` providers). GPL cannot enter this tree. Behavior (uwsm/app2unit, history) is reference only. Also uses Go binary plugins, which `AGENTS.md` forbids. |
| **[Walker](https://github.com/abenz1267/walker)** | **Rust + GTK4**, frontend for Elephant | Wrong language, GTK, GPL ecosystem. Prefix table (`=` calc, `/` files, `:` clipboard) is useful as a *later* provider map, not v1. |
| **[Gofer](https://codeberg.org/JakeAtLinux/Gofer)** | Pure Go, own wl_shm + X11 | Own compositor client — we already have `sysc-wayland`. No LICENSE file on `main` (404). Until a license exists, **zero source copy**. Behavior: XDG scan, detached GUI vs terminal CLI, dmenu stdin, web-search prefixes. Screenshot in `extra/screenshot.png` on the repo if a visual of a minimal Go launcher is needed. |
| **rainy** | Go dmenu, fork of Gofer | Picker only, not an app index. |
| **go-fzf-launcher** | Go wrapping fzf | External TUI. |
| **nwg-drawer / nwg-menu** | GTK | Wrong toolkit. |
| **lotos-linux/desktop** | Go | Thin parse + `Run()` via os/exec. Weaker than `desktopentry`. |
| **danksearch** | already rejected | `docs/prior-art.md` |

### First-rung decision for the designer

1. Pin `github.com/go-freedesktop/desktopentry` for Scan + ExpandExec.
2. Pin `github.com/go-freedesktop/icontheme` when painting icons (likely with M5).
3. Own the scorer and usage store.
4. Own spawn via Niri + optional `xdg_activation_v1`.
5. Do not vendor Gofer's renderer or Elephant's daemon.

---

## 7. What sysc-shell already owns

| Need | Status on `main` |
|---|---|
| Overlay Exclusive panel + shield + single instance | M4 `PanelHost` |
| Centered floating placement (settings-style) | 4B D1; launcher should use this, not bar-edge attach |
| `KindTextField` + text-input-v3 + I-beam | M4 |
| `KindVirtualList` | M4 (`internal/ui/tree.go:22`) |
| `KindButton`, row/column, scroll | M4 |
| `KindImage` | **not on main**; M5 5A adds it. App icons need it or a glyph-only v1 |
| Segmented category chips | **no**; M4 has tabs, not compact icon segmented |
| Context menu popup | M4 `internal/shell/menu.go` (tray/settings menus — reuse, don't fork) |
| IPC `panel.toggle` | yes; add panel id `launcher` |
| Niri hotkeys doc | `docs/niri-hotkeys.md` — add Super+Space or owner-chosen bind |
| `.desktop` parser / spawn | **none in-tree**; first rung is `go-freedesktop/desktopentry` (§6), spawn via Niri |
| Fuzzy match | **none**; write it, or MIT `sahilm/fuzzy` if a table test prefers it |
| Usage store | **none** |
| `xdg_activation_v1` | planned for M5 notify focus; launcher spawn wants a token |
| Bar keyboard focus | **none**; M4 forbade a pointer-only bar launcher. v1 can ship IPC/hotkey only |
| Plugin launcher providers | M6 explicitly out (`2026-09-01-milestone-6-plugin-host-design.md` D5) |

Calculator, emoji JSON, wallpaper apply, dmenu stdin, and overview type-to-launch
(`OverviewLauncherCapture`, tiny Exclusive surfaces while Niri overview is open,
`niriOverviewTypeToLaunchEnabled` default **false**) are later slices.

---

## 8. Do not copy

- Noctalia C++, Tabler atlas, libqalculate, Luau plugin providers
- DMS QML, Quickshell `DesktopEntries` / `execDetached`, file-index, plugin browse
- Elephant (GPL-3), Walker (GTK/Rust), Gofer UI/Wayland (unlicensed + wrong client)
- `danksearch` (`docs/prior-art.md`)
- Fullscreen dimmed scrim as the default (DMS optional `modalDarkenBackground`)
- Spotlight growing island or connected-frame emerge
- Attached bar-seam launcher (Noctalia *can*; default is Floating/center). M4
  rejected attached placement.
- Pointer-only bar button before the bar has a keyboard-focus contract

---

## 9. Recommended v1 cut

Two visual references now, and they disagree. The designer must pick with the
owner — do not silently blend them.

| | Noctalia default list | DMS on this machine (`full` / `compact`) |
|---|---|---|
| Size | 560 × 500 | 620 × 600 |
| Selected row | **Primary fill**, OnPrimary text | **primaryPressed** fill, ripple, icon unchanged |
| Row | 40 px icon + title + subtitle | 52 px row, 36 px bare icon |
| Empty open | full alpha app list | full app list (Spotlight would hide it) |
| Footer / modes | none (prefix `/` instead) | All / Apps / Files / Plugins |
| Ranking | fuzzy × weights + small usage cap | exact/prefix ladder + frecency buckets |

Commission default remains **Noctalia list chrome** (matches M4 Exclusive cards
and a single Applications provider). This laptop's DMS standalone card is the
fallback if the owner wants "what I click every day". Do not take Spotlight.

Implementation first rung (ponytail):

1. `go-freedesktop/desktopentry` for Scan + ExpandExec.
2. Own scorer (start with Noctalia weights unless the owner chooses DMS frecency).
3. M4 panel + virtual list + text field.
4. Niri spawn. No Elephant, no Gofer window, no Quickshell.

Out of v1: calculator, emoji, windows, wallpaper, session-in-launcher, dmenu,
files, clipboard, plugins, overview capture, app grid, category chips.
