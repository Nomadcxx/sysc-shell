# Chrome Catalogue — Design Commission Handover

Date: 2026-09-02.

This handover is for a **receiving agent**. Produce an owner-approved **design**
that nails a shared chrome language, then an **executable implementation plan**.
Do not write product code until those two documents exist and the owner has
approved the design. The first code consumer, once planned, is the session /
power panel that already ships and looks unfinished.

## Goal

sysc-shell already has widgets, panels, and a Material 3 token set. It does
**not** have a single look for interactive chrome. `KindButton` paints a sharp
rectangle always filled with `Primary`. Every profile chip and session action
on the live laptop therefore looks selected, labels clip (`Balanced` →
`Balanc…`, `76%` → `7…`), and hover does nothing even though the panel host
already tracks the pointer.

The owner wants a **catalogue of reusable default elements** — buttons, bar
pills, outlined pills, filled-when-on, hover, press — so the next panel is
composed from the same pieces as the last one. Commonality is the point.
Ad-hoc restyles of one tree are how the bricks happened.

This is **shell chrome**, not a general application toolkit. `AGENTS.md`
forbids inventing primitives with no approved consumer. Every element in the
catalogue must name the shipped surface that uses it. Do not grow `internal/ui`
into Qt.

Borrow **extensively** from live DMS and Noctalia pixels and from their
installed QML/C++ as behavior references. Do not import their source, do not
port Material Symbols, do not preserve QML APIs.

## Why this exists now

2026-09-02 live Niri on the laptop (`ssh -p 7777 nomadx@192.168.0.64`):
`panel.open {"panel":"session"}` mapped `sysc-shell-panel` + shield. Battery
card, three profile buttons, session actions were all present. The chrome was
a stack of accent-filled rectangles. The compositor session was then taken
over by the gpu-screen-recorder plugin gate. **Do not restart sysc-shell on
that host while that gate is running.** Use the grim already taken and the
in-tree prior-art screenshots.

Trigger issue: growing `PanelSession` (`sysc-134`) made the missing button
language visible. Do not close `sysc-134` from this commission; live Niri for
power is still owner-deferred.

## Assignment

In a **docs worktree** branched from current `origin/main` (`a7a50d1` or
whatever `main` is when you start; check):

1. `docs/plans/YYYY-MM-DD-chrome-catalogue-design.md` via
   `superpowers:brainstorming`. Stop after each section for owner approval.
   Do not implement.
2. `docs/plans/YYYY-MM-DD-chrome-catalogue.md` via
   `superpowers:writing-plans`, only after the design is approved. Required
   `superpowers:executing-plans` header, exact files, TDD, small commit
   boundaries.
3. A register row in `docs/plans/README.md` in the same commit as each
   document.
4. Beads from the **primary** checkout `/home/nomadx/sysc-shell` (not the
   worktree). Create the tracking issue if it does not exist yet (see Beads).
   Commit `.beads/issues.jsonl` with the documents.

The design must include a **named glyph inventory** with original 24×24 SVGs
specified (and, in the later implementation plan, written) for every chrome
icon the catalogue needs. Authoring-time SVG → `sysc-icons.ttf` is the
existing path. Runtime SVG parsing for chrome is forbidden.

## Read these first, in order

1. This handover.
2. `AGENTS.md` and ponytail. First working rung. UI primitives only for an
   approved shell component. bd only. No Qt/QML/Lua. Niri first.
3. Architecture: `docs/plans/2026-08-26-sysc-shell-design.md` (UI runtime,
   one Wayland owner, draw-after-invalidation).
4. Icon-asset policy and the recorded font-face deviation:
   - `docs/plans/2026-08-30-built-in-widget-foundation-execution-handover.md`
     § Icon asset policy (no runtime SVG, no CGO, authored SVG kept, licence
     beside assets).
   - `docs/plans/2026-08-31-milestone-3-handover.md` deviations table: icons
     ship as an embedded font, not baked rasters. **Do not "fix" this back.**
   - `internal/render/icons/LICENSE` — original geometric drawings, MIT, not
     derived from Material / Noctalia / DMS.
   - `internal/render/icons/build.py` and `svg/cpu.svg` — the drawing language
     (24×24 filled `path` `d`, no strokes, no `<use>`).
5. What already paints:
   - `internal/ui/tree.go` (`Kind`, `Fill`, `Tone`, `Node`)
   - `internal/render/paint.go` (`KindButton` = `fillRect` + accent;
     `KindCapsule` = stadium, `min(radius, half short side)`)
   - `internal/shell/theme.go` / `internal/theme/theme.go` (token mapping)
   - `internal/shell/widget.go` (bar capsules)
   - `internal/shell/popout_session.go` (the first consumer)
6. Bar chrome already decided: `docs/plans/2026-08-31-bar-visual-parity-design.md`
   D1–D9 plus the numbered-pill amendment. Live grim under
   `docs/plans/assets/2026-08-31-bar-visual-parity/`.
7. Panel chrome notes: `docs/plans/assets/2026-08-31-bar-visual-parity/refs/README.md`
   (segmented control is the strongest missing primitive; card-fill contrast
   is `sysc-104` / `sysc-110`).
8. Plugin chrome (do not execute that plan): 
   `docs/plans/2026-09-02-plugin-visual-polish-design.md` D11 — do not mix
   ASCII with weather icons; expand PUA when a shipped surface needs a named
   glyph.
9. Power panel contracts this catalogue must not break:
   `docs/plans/2026-09-02-power-panel-design.md`. Right-click battery, left
   inert, `power` IPC alias, no gamer-mode freeze engine, no drawn battery
   silhouette.
10. Controls already shipped (toggle, slider, menu, field, virtual list):
    `docs/plans/2026-08-30-settings-osd-theme-catalog-design.md`. Restyle
    them to the catalogue; do not replace them with a second set.

## Prior art — study, do not copy

The owner named DMS and Noctalia as the visual teachers. Study **pixels
first**, then QML/C++ for numbers that pixels cannot give (radius, padding,
hover opacity, duration).

### Screenshots (in-tree, this laptop, 1920×1080 physical, scale 1.25)

| File | What to extract |
|---|---|
| `docs/plans/assets/2026-08-31-bar-visual-parity/dms-bar.png` and `dms-bar-right.png` | Per-widget stadium capsules, `sch` fill vs slab, accent only on on-state |
| `refs/dms-dash-default.png` | Tab row, cards, profile chip |
| `refs/dms-process-element.png` | Segmented filter, value chips |
| `refs/noctalia-bar-idle.png` | Tight capsules, numbered workspace pills, grouped sysmon |
| `refs/noctalia-control-center.png` | ON = accent-filled tile, OFF = outline; header circular icon buttons; bar is true pills, inner tiles are rounded rects |
| `refs/noctalia-settings.png` | Segmented groups, row controls |
| `refs/noctalia-notifications.png` | Header pills, All/Today segmented control, per-card action button |
| `refs/noctalia-weather.png` | Segmented Daily/Hourly |
| Live grim 2026-09-02 (if still on the laptop at `/tmp/sysc-live-session-open.png`) | Current session panel: the defect being fixed |

Copy those PNGs into a new `docs/plans/assets/YYYY-MM-DD-chrome-catalogue/`
folder only if you crop a detail the design cites. Do not duplicate the whole
set.

### Installed DMS (behavior + metrics)

Read as untrusted reference, never copy into the tree:

- `/usr/share/quickshell/dms/Modules/Plugins/BasePill.qml` — capsule radius,
  padding, `Theme.widgetBaseBackgroundColor`
- `/usr/share/quickshell/dms/Widgets/DankButtonGroup.qml` — segmented
  `selectionMode: "single"`, selected vs idle, `buttonHeight` 32/40, scale-to-fit
- `/usr/share/quickshell/dms/Modules/DankBar/Popouts/BatteryPopout.qml` — header
  (icon + large % + status + rate/time), nested-surface Health/Capacity chips,
  `DankButtonGroup` for profiles. Health/HID stay out of sysc-shell until
  metrics supply them (`power-panel-design.md`).
- `/usr/share/quickshell/dms/Common/Theme.qml` `getPowerProfileLabel` /
  `getPowerProfileIcon` — labels only; draw our own glyphs
- Hover: search `containsMouse`, `Theme.*Hover`, `withAlpha` on those files

### Installed / backed-up Noctalia

- Live grim above is authoritative for v5 chrome.
- Session / power tab in the control centre (ON fill, OFF outline, profile
  control). Path under `/usr/share` or `~/noctalia` if present; if missing,
  the grim + `~/.config/noctalia/settings.json` are enough.
- Gamer-mode is **not** a chrome reference for this catalogue. Its
  `powerprofilesctl` parse already landed in `internal/shell/powerprofiles.go`.

### What to take vs refuse

| Take | Refuse |
|---|---|
| Stadium pills on the bar and on buttons | DMS BatteryPill silhouette / Noctalia Graphic battery (3C D2, power-panel D) |
| Accent fill = on / selected / focused only | Filling every `KindButton` with Primary (today) |
| Idle = container fill or outline | A second colour language that ignores matugen tokens |
| Hover = slight dim of the **current** fill (not a new accent) | Glow, scale-up, shadow-only hover, CSS-like transition libraries |
| Press = slightly darker than hover, same shape | Ripple, bounce, overshoot |
| Segmented profile row (DMS `DankButtonGroup`) | Three independent always-on bricks |
| Nested card vs pill contrast using a **high** container token | Mapping cards and bar pills to the same mid `SurfaceContainer` (`sysc-104`) |
| Header circular icon buttons (Noctalia CC close/power/settings) | Porting the whole control-centre IA |
| Original 24×24 filled paths in our icon font | Material Symbols, Tabler, Lucide, copied SVG `d` from DMS/Noctalia |

Noctalia CC inner tiles are rounded rectangles, not full stadiums. The owner
asked for **pill-shaped buttons**. Default to stadium (`radius = half short
side`, same as `KindCapsule`) unless brainstorming with the owner picks
rounded-rect for dense tiles. Record that as a numbered decision.

## Fixed constraints (do not reopen)

- Go. No C++, QML, Luau, Qt, Quickshell.
- One Wayland dispatch goroutine. Hover/press state is model data on the
  tree or host, published by the existing `Handle` paths. Paint stays dumb.
- Draw only after invalidation. Hover dim is an invalidation of that
  surface, not a 60 fps loop. Reduced-motion: still paint the settled hover
  colour; do not animate the dim if `Accessibility.ReducedMotion`.
- No runtime SVG parser for chrome. Author SVG → `python3
  internal/render/icons/build.py` → commit `sysc-icons.ttf`. `go build`
  must not invoke the builder (existing charter).
- Do not import DMS or Noctalia. Do not add a CSS engine, a scene graph, or
  a component package outside `internal/ui` + `internal/render` + the shell
  trees that consume them.
- Do not pin a new module for this work.
- Do not start Milestone 6, rewrite settings IA, or reopen bar-parity D1–D9
  except where this catalogue **uses** those capsules.
- Do not steal the laptop compositor from the recorder live gate.
- Local `replace` directives stay forbidden.
- Status lives in bd. Do not put a TODO list in the design header.

## Owner-intent defaults (challenge them in brainstorming, don't ignore them)

These are the commission's recommended answers. Brainstorming may change them
**with the owner**. If the owner is silent, keep them.

| Topic | Default | Why |
|---|---|---|
| Scope of paint | Restyle `KindButton` in the painter so every consumer inherits | Owner rejected "session panel only"; chose shared primitive, session as first layout consumer |
| Button shape | Stadium (capsule radius) | Owner: "all buttons should be pill shaped" |
| Fill | Idle = `FillContainer` (or outline — pick one in design). Selected/on/destructive confirm = `FillAccent`. Hover = dim current fill ~10–15%. Press = dimmer | DMS/Noctalia on-state; today's all-accent is the defect |
| Hover | Pointer already tracked (`PanelHost.hoverX/Y`, `Bar.hoverAt`, toast `hovered`). Paint a `Hovered` (and `Pressed`) bit on the node or a host overlay rect. No new kind | Least new API |
| Segmented control | Compose a `KindRow` of catalogue buttons with exclusive selection; add `KindSegmented` only if composition cannot share radius/gap/hit. `KindTab` is currently measured as text — do not silently reuse it | Four prior-art sightings; YAGNI a kind until a test fails |
| Cards | Keep `monitorCard` / `KindCapsule`. Map **panel cards** to a higher container token than bar pills (`sysc-104`). One decision, owned here if the catalogue introduces `FillContainerHigh` | Nested surfaces are indistinguishable today |
| First layout consumer | `popout_session.go` battery / profile / session trees | The live defect |
| Other trees this slice | Do **not** hand-rewrite settings/sysmon/launcher/notify as a drive-by. They pick up button paint automatically. Optional follow-up issues if a tree needs different padding or icons | Owner option 3 |
| Motion | Hover/press are colour, not layout. Duration 0 if reduced-motion, else ≤150 ms colour only if a frame callback already exists; otherwise snap. Do not add a tween helper | Draw-after-invalidation |
| Icons | Expand `sysc-icons.ttf` with original SVGs named in the design. Empty `Text` + icon name on a button is allowed (recorder-panel D15 pattern) | Existing font path |
| Destructive actions | Power off / reboot stay the same shape; colour is still accent or error — pick with the owner. Do not invent a confirm modal in v1 | Session already calls `loginctl` |

## Design must decide (known unknowns)

Write each as a numbered decision in the design. Do not hide them in prose.

1. **Idle chrome.** Container-filled pill vs outline (stroke) pill vs both as
   variants (`FillContainer` vs a new `FillOutline`). Noctalia CC uses outline
   for off tiles. DMS `DankButtonGroup` uses filled selected + quieter idle.
   Session profile row wants the DMS group. Session actions may want full-width
   stadiums. One system, two variants is allowed; three is not.
2. **Hover and press encoding.** Flag on `ui.Node` vs host-computed overlay vs
   `Tone`. Who sets it (panel `Handle`, bar `Handle`, tray menu). Invalidation
   granularity. Keyboard focus vs pointer hover (focus-visible ring already
   required by a11y gates — do not replace it with hover).
3. **Dim recipe.** Token (`Primary` darkened) vs alpha vs a new
   `PrimaryHover` from matugen. Must stay WCAG-sensible on both dark and light
   generated palettes. Record the formula. `sysc-104` contrast floor is 1.10
   today and is too low — do not lock another undetectable fill.
4. **Button padding and min size.** Today's session buttons set no padding, so
   a stretched column child is a full-width brick and the label clips. Lock
   default padding, min height (DMS 32/40 is a starting number), and how a
   `KindRow` of three profile labels shares width without `Balanc…`.
5. **Segmented vs separate buttons.** Profile row is exclusive. Session
   actions are independent. Same primitive, different `Bold`/`Fill` rules.
6. **Card vs pill fill tokens.** Bar capsule, panel card, nested chip
   (DMS Health/Capacity). One, two, or three `Fill` values. Touch matugen
   `tpl.json` only if a token is missing. Coordinate with `sysc-104`.
7. **Glyph inventory (required, not optional).** Name every new SVG, its PUA
   slot after `network` (`U+E01A`), the consumer, and the 24×24 language.
   Seed list below. The design may cut unused names; it may not say "and
   whatever else we need later" without a bd follow-up.
8. **Icon on buttons.** Text only, icon only, icon+text. Session actions are
   words today; Noctalia CC uses icons on header rounds. Do not require icons
   on Log out if the word fits.
9. **Where the catalogue lives in code.** Prefer restyling existing kinds
   (`KindButton`, `KindCapsule`) plus maybe `Hovered`/`Pressed`/`Fill` values.
   A new `internal/ui/chrome.go` of constructors (`PillButton(...)`) is
   allowed if it is thin and used. A parallel widget kit is not.
10. **Bar hover.** Owner said this may belong on the bar too. Bar items are
    already capsules; they do not hover-dim. Decide whether bar capsules dim
    on pointer, or only `KindButton`. Capsule dim on the whole CPU/memory
    group is a different hit target than dimming one button.
11. **Settings / launcher / notify inheritance.** Automatic via painter vs
    explicit restyle tasks. Default: painter only this slice.
12. **Live proof.** Session panel grim on the laptop after the recorder gate
    yields the compositor. Right-click battery, Super+X, hover, press,
    profile click, `niri msg -j layers`. Owner may defer hardware; the plan
    still writes the checklist.

## Catalogue the design must name

Minimum set. Add only with a shipped consumer.

| Element | Shipped consumer | Prior art |
|---|---|---|
| Bar item pill | `widget.go` `capsuled` | DMS `BasePill`, Noctalia bar capsules |
| Grouped bar pill | cpu+memory group (`sysc-54`) | Noctalia sysmon group |
| Workspace numbered pills | already shipped | Noctalia numbered, DMS dots (rejected) |
| Idle / selected / hover / press button | session profiles + session actions; then every `KindButton` | DMS `DankButtonGroup`; Noctalia CC tiles |
| Full-width action pill | Log out / Suspend / Reboot / Power off | session card; Noctalia power menu if present |
| Nested chip / small capsule | battery % row; later Health if metrics exist | DMS Health/Capacity `StyledRect` |
| Panel card | `monitorCard` | both refs |
| Segmented exclusive row | power profiles | DMS profile group; Noctalia settings/weather/notifications |
| Circular icon button | optional header close; calendar `<` `>` | Noctalia CC header |
| Focus ring | already required on focusable nodes | keep; not hover |
| Meter / graph | already shipped; restyle only if a token changes | sysmon |

Do not add radial gauges, tables, nav rails, or image-background cards here.
Those are recorded in the bar-parity refs README as future vocabulary.

## SVG generation (required)

Chrome icons are **project-owned filled paths**, same pipeline as weather and
CPU.

Rules (already policy — enforce them):

- Author in `internal/render/icons/svg/<name>.svg`, `viewBox="0 0 24 24"`,
  one or more `<path d="...">`, no stroke, no transforms, no `<g>` that the
  builder cannot flatten. Match `cpu.svg`.
- Original geometry. **Do not** trace Material / Tabler / Phosphor / DMS
  `DankIcon` / Noctalia SVG. If it looks like a Material battery, it is
  wrong; we already have battery runes.
- Append to `GLYPHS` in `build.py` and to `iconfont.go` / `iconNames`.
  Rebuild with `python3 internal/render/icons/build.py` from the worktree
  (fontTools is already used). Commit the TTF in the same commit as the SVGs.
- Licence stays `internal/render/icons/LICENSE`.
- Expand PUA only for names the design lists. Plugin polish D11 stands:
  no weather-rune stand-ins for unrelated actions.

### Seed inventory

Existing (do not redraw): weather 8, battery 15, `cpu`, `memory`, `disk`,
`network`. Recorder plan (if not yet on the branch you start from) wants
`camera`, `camera-off`, `record`, `stop`, `replay` — do not collide; append
after whatever `network` currently is.

New names to confirm or cut in brainstorming, each with a consumer:

| Name | Consumer |
|---|---|
| `lock` | session Lock (hidden without locker) |
| `logout` | session Log out |
| `suspend` | session Suspend |
| `reboot` | session Reboot |
| `poweroff` | session Power off |
| `performance` | profile tab (or skip if the word fits) |
| `balanced` | profile tab |
| `power-saver` | profile tab |
| `close` | panel header / notify dismiss if the catalogue covers it |
| `chevron-left` / `chevron-right` | clock month nav (`<` `>` today) |
| `search` | settings search / launcher field |
| `settings` | settings entry if a header round exists |
| `bell` | notification centre header |
| `dnd` | DND control |
| `check` | selected segment, if selected is not fill-only |
| `volume` / `brightness` / `mute` | OSD (today an accent square) |
| `launcher` | bar launcher glyph if M7 wants one this slice — default **cut**; launcher v1 is glyph-behind-a-seam already |

The design's inventory table is the source of truth. Implementation writes
exactly those SVGs, no extras "for later".

## First consumer and regression surface

After paint + session layout:

- Right-click battery still opens session; left-click stays inert.
- Super+X and `panel.toggle {"panel":"power"}` still the same surface.
- Profile buttons share a row without clipping; active is fill (and/or
  `Bold`), not "all blue".
- Session actions are stadiums, full width of the card, hover-dim visible
  on grim.
- `powerprofilesctl` hide-when-missing still holds.
- Unit tests that assume `KindButton` is an accent rectangle must change
  **with** the painter, not be deleted to hide the restyle.
- Bar overflow `…`, tray menu rows, launcher result rows, notify actions,
  settings sidebar, clock `<` `>` will look different the moment
  `KindButton` paints as a pill. That is intended. Screenshot or test the
  ones that can clip.

## Plan requirements (once the design is approved)

- Task 0: inventory every `KindButton` construction site (grep already
  lists `popout_session.go`, `panelhost.go`, `bar.go` tray overflow,
  `traymenu`/`traydrawer`, `popout_launcher.go`, `notifycard.go`,
  `popout_notifications.go`, `popout_settings.go`, `popout_clock.go`).
  One table in the plan: keep / inherits paint / needs padding tweak.
- TDD: painter tests for stadium + idle/selected/hover/press fills (pixel
  or colour-at-point, matching existing `paint_test.go` capsule proofs).
  Layout tests for profile row min width (`Balanced` unclipped at 420).
  Session tree tests for fill/action, not just kind counts.
- SVG tasks: failing `IconByName` tests, write SVGs, `build.py`, commit TTF.
- Session layout task after the painter, not before.
- Hover wiring task: set flags from existing `Handle`, invalidate, reduced-motion.
- No plugin protocol changes. No new Wayland protocol.
- Commit boundaries: tokens/fill → button paint → hover/press → SVGs →
  session tree → (optional) card fill token. Not one "make it pretty".
- Live Niri checklist in the plan; owner may defer.

## Stop conditions

Stop and ask the owner if any of these appear:

- A general toolkit / new package of dozens of unused components.
- Runtime SVG, CGO, or a Material font pin.
- EGL, blur, or drop shadows required "to look like DMS".
- Rewriting settings, launcher, or sysmon trees as this slice.
- Reopening gamer-mode freeze/kill or a lock screen.
- Implementation requested before the design is approved.
- `bd` from a worktree would create a second database — use
  `BEADS_DB=/home/nomadx/sysc-shell/.beads/beads.db`.
- The laptop is still running the recorder live gate and someone asks you
  to `pkill` sysc-shell. Kill by pid only, and only when the owner says
  the recorder gate is done.

## Repository / worktree

Primary checkout `/home/nomadx/sysc-shell` is often **dirty** with uncommitted
sysmon/theme WIP. Do not stash-pop or commit that pile. Do not implement on
the dirty tree.

```bash
cd /home/nomadx/sysc-shell
git fetch origin
git worktree add \
  /home/nomadx/.config/superpowers/worktrees/sysc-shell/feature/chrome-catalogue \
  -b feature/chrome-catalogue origin/main
```

Documents land on `main` as **docs-only commits** per `AGENTS.md`.
Implementation, when it happens, is a later worktree after the plan exists.

Do not edit `.worktrees/feature/power-panel` (removed after merge) or the
plugin-host / recorder worktrees for this commission.

## Beads

From `/home/nomadx/sysc-shell`:

- Claim `sysc-141` (`Chrome catalogue: shared pills, button states, project SVGs`)
  with `bd update sysc-141 --status in_progress` when design starts.
- Discovered from `sysc-134`. Do not close `sysc-134` from this work.
Record extra glyphs or a settings restyle pass as
`bd create ... --deps discovered-from:<id>`, not as silent plan growth.

Do not close `sysc-134` from this work. Do not close `sysc-104` unless the
design's fill-token decision explicitly owns that contrast bug.

## Success

The owner can read the design and see: one button, one pill, one hover, one
press, a finite SVG list, and a rule that new panels compose those instead of
inventing a fourth blue rectangle. The plan is executable by a later agent
with TDD and does not require opening QML except as cited prior art.

Skipped until a later issue: control-centre tile grid, radial gauges,
Material icon font, bar hover if the owner cuts it, restyling every panel
tree by hand, live Niri if the owner defers.
