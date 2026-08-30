# Panel Foundation Design — Milestone 4, Tranche 4A

Date: 2026-08-30
Status: Owner-approved (sections validated 2026-08-30 through brainstorming).
Branch: `milestone/panels-controls`
Worktree: `/home/nomadx/.config/superpowers/worktrees/sysc-shell/milestone/panels-controls`

Milestone 4 is split into two tranches. This design is the first:

- **Tranche 4A (this document)** — panel machinery, placement, the control vocabulary with 4A
  consumers, core theming, the gate popouts (clock/calendar, system-monitor, session/power), and
  the IPC socket with panel verbs and documented hotkeys.
- **Tranche 4B** — settings modal and schema registry, OSD surfaces with audio/brightness
  services, stock themes, and the enforced app-theming template catalog. See
  [the 4B design](2026-08-30-settings-osd-theme-catalog-design.md).

The split follows the roadmap rule that candidate components enter only with a consumer: every
control shipped here has a 4A consumer, and every control whose only M4 consumer lives in settings,
OSD, or the theme catalog ships in 4B. Tranche 4A is sized to pass the milestone's exit gate on its
own surfaces; 4B is breadth.

Research backing every decision lives in
[the research document](2026-08-30-panels-and-controls-research.md); prior-art inventories in
[the prior-art document](2026-08-30-panels-and-controls-prior-art.md).

## Ordering constraints

- Milestones 1, 2, and 3 have merged. `sysc-5` still tracks the owner-deferred Milestone 2
  multi-output hardware qualification; it does not block this tranche and must not be reported as passed.
- 4A consumes the landed M3 surface: the bar, the
  consumer-counted clock service, the Niri projection, the config/reload path, and the retained
  `ui.Handle` press/release matching and hit testing that 3A kept inert "because Milestone 4 needs
  it". The system monitor consumes 3B's metrics service and graph node. Changes to `Registry`,
  configuration, rendering, and process wiring must keep 3C and 3D paths intact.
- The design and plan are written docs-only, in parallel with M2 corrections, per the orchestration
  document.

## Scope

Tranche 4A ships:

- Panel machinery: per-panel dismiss shield + panel surface, both layer-shell Overlay with
  exclusive_zone −1; Exclusive keyboard while open; one instance per panel ID process-wide.
- Placement: floating, centered off the focused output's bar, clamped inside the output.
- Shell-rendered corner rounding and shadows (no reliance on user layer-rules).
- Controls with 4A consumers: button, label, separator, and tabs; the system monitor reuses M3's
  graph node. The roving keyboard model and accessible name/role data cover each interactive node.
- Theming core: matugen-generated Material 3 tokens, dark/light, high-contrast, reduced-motion,
  fallback stock palette; `Theme` replaces `ProofStyle`.
- Popouts: clock/calendar, system-monitor over the landed M3 metrics service, and session/power
  (logind via `loginctl`; lock delegates to a configured external locker).
- IPC: `$XDG_RUNTIME_DIR/sysc-shell/ipc.v1.sock`, newline-delimited JSON, panel verbs, `status`,
  and the `sysc-shell ipc` CLI with documented niri keybinds.

It ships no settings UI (config is hand-edited until 4B), no OSD, no audio/brightness services, no
launcher, clipboard, notifications, tray, dock, first-party lockscreen, attached placement, or
AT-SPI export.

## Decisions

| # | Decision | Rejected alternative |
|---|---|---|
| D1 | Two layer-shell surfaces per panel: fullscreen dismiss shield (keyboard none) + panel surface, both Overlay, exclusive_zone −1. | A single surface with input-region tricks, or compositor-side outside-click dismissal — niri has no outside-click dismissal for layer surfaces; Noctalia's click shield and DMS's clickcatcher both implement it shell-side. |
| D2 | Keyboard `Exclusive` for every interactive panel while open. | Per-panel OnDemand mixing (Noctalia control-center style). Exclusive makes the gate items "escape always closes" and "only the open panel requests keyboard focus" true by construction. Per-panel demotion is a future config knob. |
| D3 | Focus restoration relies on niri's automatic layout-focus fall-through when the exclusive surface unmaps. | DMS-style explicit restore of the captured toplevel — verified redundant on niri (`update_keyboard_focus` falls through to `layout_focus()`, the last activated window). Contingency below if live testing disagrees. |
| D4 | One instance per panel ID process-wide; a trigger on the same output toggles, while a trigger after focus moves closes and reopens there. | Per-output panel instances. Popouts follow user focus, not outputs; matches DMS `currentPopoutsByScreen` close-on-other-screen. |
| D5 | Floating placement centered off the focused output's bar edge and clamped fully inside the output minus padding and the bar's reserved zone. | Noctalia Attached/seamless placement and pointer-widget anchoring. A clickable bar launcher needs keyboard parity, so 4A uses IPC and compositor hotkeys. |
| D6 | The shell renders its own corner rounding and shadows: SDF rounded-rect alpha masks and pre-blurred shadow textures composited via `blendMask`. | Relying on user-configured niri layer-rules (owner decision): the shell must look right with zero user config, and layer-rules are per-user, not per-panel-instance. |
| D7 | Controls enter only with a 4A consumer: button (session actions), label/separator (all panels), and tabs (system-monitor resources). The system monitor reuses M3's `KindGraph`; 4A does not create a second graph type. | Shipping the full roadmap control list at once. Toggle, slider, menu, text field, scroll area, and virtual list have 4B consumers. |
| D8 | Roving focus per panel: Tab/Shift+Tab between controls, arrows inside composites, Space/Enter activate, Escape closes. | A full focus graph with universal tab stops. Noctalia's `RovingListNav` pattern; keeps keyboard model testable per panel. |
| D9 | Theme = matugen-generated Material 3 tokens from wallpaper or hex source; dark/light and high-contrast settings; compiled-in fallback palette seeded from current `ProofStyle` when matugen is absent or fails. 4B adds named stock sources with its catalog. | Hand-authored static palettes only, or the freedesktop color-scheme portal for auto dark/light (deferred; no consumer need yet). |
| D10 | The system monitor reuses M3's concrete `services.Metrics`, its `Snapshot`, selector leases, and history rings. It always leases CPU and memory at one second; it also leases the first configured filesystem, block, and network selector on the focused output, one per source. | Restoring the pre-M3 `MetricsService` wrapper or sampling `sysc-metrics` from the panel. M3 now owns sampler lifetime and sequential access, so a second owner would duplicate work and break that contract. |
| D11 | Session actions exec `loginctl` (poweroff/reboot/suspend/terminate-session). | A D-Bus client dependency (godbus). `loginctl` goes through logind with identical polkit handling, keeps the shell stdlib-only, and covers every 4A action. |
| D12 | IPC = versioned Unix socket `ipc.v1.sock`, newline-delimited JSON `{"id","method","params"}` → `{"id","ok"|"error"}`; version in the filename. | Sharing niri's socket, or an unversioned path. The same socket is the planned seam for sysc-notify/sysc-tray; filename versioning lets a v2 coexist. |
| D13 | Reveal/dismiss motion is shell-rendered: fade + 8 px slide off the triggering bar edge, ~150 ms ease-out, instant under reduced-motion. | Waiting for niri layer-surface animations — verified nonexistent (layer-rules expose no animation properties; Animations config has no layer entries). |

## Panel surface model

Every open panel is two surfaces on the triggering output:

1. **Dismiss shield** — fullscreen, transparent, Overlay, exclusive_zone −1, keyboard none, input
   region covering the whole output, stacked below the panel. A click on it closes the panel and is
   consumed. This is the outside-click dismissal both reference shells implement shell-side.
2. **Panel surface** — Overlay, exclusive_zone −1, keyboard `Exclusive`, sized by content, stacked
   above the shield.

Facts that shape this (all source-verified against niri main; see research doc):

- While an Overlay surface holds `Exclusive` keyboard it beats every window in focus precedence;
  compositor keybinds still work (binds are processed before delivery to the surface).
- Clicking a window while a panel is open still updates niri's layout focus, so when the panel
  unmaps, focus falls through to the last activated window automatically (D3).
- Fullscreen windows hide `Top` layer surfaces but not `Overlay` — panels must be Overlay.
- Two exclusive surfaces on one layer resolve to the first in layer order; the single-instance
  policy (D4) avoids ever stacking two.

Escape handling: the panel's keyboard handler closes on Escape (gate). The shield never has
keyboard focus, so Escape always reaches the panel.

**A configuration reload must not destroy open panels.** Reload rebuilds bars; panel surfaces stay
mapped and re-resolve their theme and content in place on the next frame. This is a contract, not an
optimisation: Tranche 4B's settings modal is itself a panel and writes the configuration on every
change, so tearing panels down on reload would eject the user from the settings UI on every toggle,
and would kill a visible OSD. Panel content is rebuilt per render already, so re-resolving is the
same work a theme change does.

OSD surfaces (4B) reuse none of the shield machinery: keyboard none, no shield, created on demand.

## Placement and geometry

- **Trigger source.** 4A opens panels through IPC and compositor hotkeys. The focused output comes
  from the Niri projection (Window events carry output names, verified in the M3 audit), and the
  panel uses centered bar-relative alignment. A clickable bar launcher waits until the bar has a
  keyboard-focus contract; adding a pointer-only launcher would fail the M4 accessibility gate.
- **Output.** The panel opens on the focused output's bar. One instance per panel ID (D4):
  re-trigger from another output closes and reopens there.
- **Anchor.** Floating off the focused output's bar edge, offset by a gap token and centered along
  that edge. Pointer-widget alignment can extend the same placement type after the bar gains a
  keyboard-focus contract.
- **Clamp.** Fully inside the output minus an 8 px padding token and the bar's reserved zone:
  `x = clamp(desired, pad, outputW − panelW − pad)`, same for y (DMS `alignedX/alignedY` pattern).
  Bar geometry comes from the shell's own config — no compositor probing.
- **Bounds gate.** Placement must remain correct under fractional scale and output transform; the
  gate test covers transformed and scaled outputs.
- **Rounding and shadows** (D6): panel radius 12, control radius 6, as SDF rounded-rect alpha
  masks cached per radius; shadows are Go-generated pre-blurred rounded-rect alpha textures cached
  per size, two elevations only (panel, in-panel menu). Both composite through the existing
  software canvas `blendMask` path — no new renderer primitive.

## Controls and interaction

Controls shipping here, each with its consumer:

| Control | Consumer |
|---|---|
| button | session/power actions; tab headers reuse tab control |
| label, separator | every panel |
| tabs | system-monitor resources |
| graph | system-monitor history, reusing M3 `KindGraph` |


Deferred to 4B with their consumers: toggle, slider, menu/dropdown, text field (settings); scroll
area and virtual list (settings content). Consequently the text-input-v3 and cursor-shape-v1
binding generation that Section-4 research flagged also moves to 4B — 4A needs neither protocol.

Keyboard model (D8): roving focus per panel. Tab/Shift+Tab moves between controls, arrows move
inside composites (tab strip), Space/Enter activate, Escape closes the panel. Initial focus goes to
the panel's primary control. Pointer input reuses the M3-retained `ui.Handle` press/release
matching and hit testing.

Accessibility data: every focusable node carries an accessible name and role on the node itself.
AT-SPI screen-reader export remains a separate decision (roadmap) — the gate item is the data, not
the export.

## Theming core

Pipeline (verified live end-to-end with matugen 4.2.0; see research doc):

1. The shell embeds a minimal matugen config + template (~20 Material 3 tokens × dark/light,
   `{{colors.<token>.<mode>.hex}}` placeholders) via `go:embed`.
2. Generation: `matugen image -c <config> <wallpaper> --prefer saturation` for wallpaper sources,
   `matugen color hex <HEX>` for hex seeds; high-contrast appends `--contrast 1`. Output is
   `$XDG_CACHE_HOME/sysc-shell/colors.json`. (Assumption: the `color` subcommand accepts the same
   config/template flags as `image` — the plan verifies in its first task.)
3. Regenerate at startup and on theme-source change; single-flight with one queued rerun.
4. Preferences: dark/light setting (default dark; portal-driven auto deferred), high-contrast
   (regenerate with `--contrast 1`), reduced-motion (animations instant).
5. Fallback: compiled-in stock palette seeded from the current `ProofStyle` colors when matugen is
   absent or fails — the shell always has a theme.

`Theme` replaces `ProofStyle` as the render token source, mapping fg → `on_surface`, bg →
`surface`, accent → `primary`, muted → `on_surface_variant`; error and radius carry over. The bar
and M3 widgets render from the same struct, so the whole shell follows the generated palette.

Because generation now owns colour, the hand-set colour fields in the existing configuration —
`theme.background`, `theme.foreground`, `theme.accent`, `theme.muted`, `theme.error` — are
**removed from the schema**. Leaving them in place would make a user's edit a silent no-op, and this
project's rule is that an unknown or unusable entry fails the whole candidate rather than being
ignored. There is no compatibility promise, so a stale file fails at load with its field path named.
`theme.radius` stays: it is geometry, not colour, and generation does not produce it.

One consumer needs care. The current Wayland client derives the opaque-region hint from
`config.Theme.BackgroundOpaque()`. Palette generation moves that decision into `shell.Theme`, which
reads the active `surface` token. `HostCallbacks` carries the resolved boolean into the Wayland layer;
the platform package does not import theme or shell types.

Motion (D13): fade + 8 px slide off the triggering bar edge, ~150 ms ease-out, instant under
reduced-motion. Niri does not animate layer surfaces, so reveal/dismiss is the shell's own
frame-driven animation on the canvas.

## Services

All services follow the M3 clock exemplar: concrete types, no interfaces; consumer-counted leases
(first consumer starts, last stops); acquire-before-release on reload.

- **Clock** — reused from M3 via lease; the clock/calendar popout is a consumer alongside the bar
  clocks. Finest boundary wins across consumers.
- **Metrics** — reused from M3 (D10). The system monitor holds selector leases only while open.
  M3's sampler goroutine stays the single sequential owner of `sysc-metrics`; snapshots continue
  through the existing process pump. A selector history survives panel close only when a bar still
  leases it, matching M3's rule that a removed consumer must not bridge an unobserved time gap.
- **No new service.** Milestone 4 therefore adds **no** module dependency: `go.mod` is untouched by
  this tranche; M3 already pins `sysc-metrics@v0.2.0`.

Audio/brightness services and their change-detection polling ship in 4B with the OSD.

## Popouts

**clock/calendar** — triggered through IPC or a compositor hotkey. Large clock face (leased clock service)
plus a month calendar grid computed from stdlib `time`. Fixed content; no scrolling.

**system-monitor** — triggered through IPC or a compositor hotkey. CPU and memory tabs always
ship. The focused bar may add one filesystem, block, and network tab from its configured metric
selectors. Each tab shows the current value and the existing normalized history graph. This bounded
selection avoids inventing a resource picker before 4B's list controls exist.

**session/power** — triggered through IPC or a compositor hotkey. Button grid: lock, logout, suspend,
reboot, power off (D11):

| Action | Command |
|---|---|
| lock | configured external locker (config `session.locker`; no default — hidden when unset) |
| logout | `loginctl terminate-session self` |
| suspend | `loginctl suspend` |
| reboot | `loginctl reboot` |
| power off | `loginctl poweroff` |

A first-party lockscreen (ext-session-lock) is a later milestone; lock always delegates.

Runtime commands use `exec.LookPath` and argv slices, never a shell. Missing `matugen` selects the
compiled palette; missing `loginctl` hides its actions; an unset or missing locker hides Lock.
Command errors stay in the open panel or IPC response. This accepts native or already-installed
tools without turning them into Go module dependencies.

## IPC and hotkeys

- **Socket**: `$XDG_RUNTIME_DIR/sysc-shell/ipc.v1.sock` inside a 0700 directory; socket mode 0600.
  Bind failure
  doubles as the single-instance check. Version in the filename so v2 can coexist.
- **Envelope**: newline-delimited JSON. Request `{"id":1,"method":"panel.toggle","params":{...}}`;
  response `{"id":1,"ok":{...}}` or `{"id":1,"error":"..."}`.
- **Methods in 4A**:
  - `panel.toggle` / `panel.open` / `panel.close` — params
    `{"panel":"clock|system-monitor|session|settings"}` (`settings` accepted once 4B lands;
    single-instance semantics per D4).
  - `status` — shell version, open panels, capability probes (matugen present).
  - `osd.step` is reserved for 4B.
- **CLI**: `sysc-shell ipc <method> [params-json]` — connect, send, print, exit.
- **Hotkeys**: documented niri keybinds spawning the CLI (DMS pattern: compositor owns keys, shell
  owns panels). 4A documents clock, system-monitor, and session toggles; media/brightness keys come
  with 4B's OSD.
- **Future seam**: sysc-notify and sysc-tray get their own method namespaces on this socket in
  their milestones; server-pushed `{"event":...}` lines are added when they need them, not now.

## Configuration

New config fields (pointer wire types, absent inherits default; hand-edited until the 4B settings
UI), written atomically (temp + rename) and applied through the existing reload path:

- `theme`: `source` (`wallpaper` | `hex`), `seed` (hex or image path),
  `scheme` (default `scheme-tonal-spot`), `mode` (`dark` | `light`, default dark),
  `high-contrast` (bool).
- `accessibility`: `reduced-motion` (bool), `high-contrast` mirror of theme's for gate clarity —
  single source: `accessibility` owns both flags; `theme` holds only generation inputs.
- `session`: `locker` (command string).
- `panels`: `gap`, `padding` (placement tokens).

## Tranche gate

The roadmap exit gate evaluated on 4A surfaces:

| Gate item | 4A evidence |
|---|---|
| clock/calendar **or** system-monitor popout works on each output | both ship; live tests open each on every output. |
| only the open panel requests keyboard focus | Exclusive + single instance; live test: windows keep focus until panel opens; no second instance |
| a panel never changes the bar's exclusive zone | assert bar exclusive zone unchanged across open/close cycles |
| placement remains within transformed and scaled output bounds | clamp tests under fractional scale + output transform |
| keyboard-only interaction covers every shipped control | roving-focus tests: every button reachable and operable without pointer |
| every interactive node carries an accessible name and role | node-data assertion tests |
| reduced-motion and high-contrast preferences change behavior | reduced-motion → instant reveal; high-contrast → regenerated tokens differ (measured) |

Accessibility is an acceptance gate at this milestone; AT-SPI export is explicitly out (roadmap).

## Prior art review

Full inventories with file:line evidence: [prior-art doc](2026-08-30-panels-and-controls-prior-art.md)
(Noctalia v5 C++ and DMS v1.5.3 QML). Niri capability claims source-verified against niri main and
recorded in [the research doc](2026-08-30-panels-and-controls-research.md). What shaped this design:

- **Keyboard**: niri focus precedence (`update_keyboard_focus`), automatic layout-focus
  fall-through, compositor keybinds surviving exclusive layer focus, Overlay-vs-fullscreen
  behavior; Noctalia's per-panel modes (its 100 ms Exclusive→OnDemand relax is Hyprland
  focus-grab-specific, not niri behavior); DMS going Exclusive for every popout on Niri.
- **Dismissal**: both shells implement outside-click shell-side (Noctalia click shield, DMS
  clickcatcher) — niri provides nothing.
- **Placement**: DMS `triggerSection` alignment + `alignedX/alignedY` clamping; Noctalia
  `clampMargin`. Both keep popouts exclusive_zone −1/0 and never touch the bar's reservation.
- **Motion**: niri layer-rules expose opacity/shadow/corner-radius but no animation — reveal is
  client-side in every reference shell.
- **Theming**: DMS matugen pipeline (exec + JSON tokens + stock-seed fallback) verified live;
  Noctalia MIT-licensed template catalog informs 4B, not this tranche.

## Risks and ceilings

1. **Focus fall-through (D3)** is source-verified but not yet live-tested with this shell's
   surfaces. The plan's first live task verifies; contingency is explicit restore via the tracked
   `active_window_id` + `niri msg action focus-window` (projection already holds the data).
2. **Shield pointer delivery**: keyboard-Exclusive panels must still let the shield receive its
   click — pointer events are independent of keyboard focus in niri; verified by live test in the
   plan, not assumed.
3. **Software shadows** are pre-blurred textures cached per size — no realtime blur. Ceiling
   acceptable for two elevations.
4. **`loginctl` exec (D11)** gives no PrepareForSleep signals or lock inhibitors. Add a D-Bus
   dependency when the first-party lockscreen milestone needs them.
6. **IPC** has no subscriptions or auth beyond file perms; add when sysc-notify lands.
7. **matugen `color` subcommand flags** assumed symmetric with `image`; first plan task verifies.
