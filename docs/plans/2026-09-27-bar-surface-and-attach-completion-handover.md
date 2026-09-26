# Bar Surface and Attached Panels Completion Handover

Date: 2026-09-27
Branch: `bar-surface`
Worktree: `/home/nomadx/sysc-shell/.worktrees/bar-surface`
PR: #16 (replaces #15)
Design: `2026-09-25-bar-surface-and-attach-design.md`
Plan: `2026-09-25-bar-surface-and-attach.md`
Previous handover: `2026-09-25-bar-surface-execution-handover.md`
Issues: epic `sysc-546`; phases `sysc-547` to `sysc-551`

Phases A–E are implemented. What remains is the owner's live gate on
Niri 26.04 or later (below). Its results belong in this file.

## Branch and PR

The work was first on a tool-prefixed branch (#15). The owner does not accept
tool attribution in public repositories, so the branch was rewritten with
`git filter-branch --tree-filter` over `4e1f888..`. The rewrite removed the
externally hosted mockup links and the tool-prefixed branch names from the
plan docs. The mockups those links pointed to are already committed under
`docs/plans/assets/`. Code is byte-identical to the pre-rewrite tip, and
authors and dates are unchanged. The result was pushed as `bar-surface` and
opened as #16; #15 is closed.

The execution handover still calls this PR "#15" in its branch table. Read
that as #16.

Four tool-prefixed branches remain on GitHub (`git ls-remote --heads origin`
lists them). Deleting them is the owner's call. `main` was deliberately not
rewritten, so it keeps PR #13's merge commit message.

## Commits

| Commit | Contents |
|---|---|
| `311b5a2` | docs(plans): bar surface styles and attached panels |
| `a99c8ab` | docs(plans): keep the launcher and clipboard floating |
| `31f45e8` | feat(wayland): generate ext-background-effect-v1 binding |
| `2fd3b22` | feat(wayland): report compositor blur capability |
| `fcca6b0` | fix(render): draw attached-panel joints as concave fillets |
| `d01f6d6` | feat(ui): describe painted silhouettes as blur strips |
| `6fb9d22` | feat(wayland): publish compositor blur regions |
| `e5e6f18` | docs(plans): bar surface execution handover |
| `c54a61d` | docs(plans): expand the bar surface execution handover |
| `1bd681c` | docs(plans): commit the centre pill and bar survey mockups |
| `a8c5382` | feat(config): bar style, shape and frost opacities (Task 5) |
| `9b17ff5` | feat(settings): bar style, shape and frost controls (Task 6) |
| `c92fe5c` | feat(shell): resolve bar style against compositor blur (Task 7) |
| `9557574` | feat(shell): publish the bar's blur shape per style (Task 8) |
| `177abea` | feat(bar): attached shape geometry (Task 9) |
| `9e69c43` | feat(render): concave end fillets for an attached bar (Task 10) |
| `9d62b9b` | feat(panels): attach panels with full joints and flush edges (Tasks 11–12) |
| `7f7512e` | feat(panels): join attached panels to the bar as one ground (Tasks 13–14) |
| `00074af` | feat(shell): log the compositor's blur answer (live-gate prerequisite) |
| `893dd9e` | docs: niri blur configuration (Task 15) |

## Where things live

- **Theme × capability.** `Theme.WithCompositor(blur)` is the one post-step
  after pure resolution (`internal/shell/theme.go`). The registry applies it
  wherever it builds a bar or panel theme. `Registry.SetCapabilities` restyles
  live bars and open panels, and logs the first answer and every change.
- **Bar geometry.** `config.Bar.Extent`, `SurfaceExtent`, `Overhang`,
  `Attached` and `BodyIn` (`internal/config/config.go`). `BodyIn` is the one
  body placement, used by the platform regions and by `Bar.bodyLocked`.
  Attached default: extent and zone 40, surface 52, input region the body
  only.
- **Bar blur.** `Bar.blurShape` (`internal/shell/bar.go`). Islands uses
  `render.CapsuleRadius`, the painter's own capsule rule.
- **Panel placement.** `Placement.Attached`, `Joints`, `layout` and `anchor`
  (`internal/shell/panel.go`). The surface is derived by
  `PanelHost.surfaceSize` and `surfaceBody`, and the panel blur by
  `PanelHost.blurShape` (`internal/shell/panelhost.go`).
- **Painter.** `render.CornerMask` with `Corners`; per-side `JointLeft` and
  `JointRight`; `EdgeFillet` with `EdgeLeft` and `EdgeRight`;
  `Style.squareCorners`; `fillEdgeFillets`
  (`internal/render/{mask,paint,canvas}.go`). `squareAttachedEdge` is gone.
- **Input regions at open.** `AuxSpec.InputRects`
  (`internal/platform/wayland/aux_surface.go`).

## Deviations from the plan, with reasons

1. **Style field names.** The fields are `EdgeFillet`, `EdgeLeft` and
   `EdgeRight`, not `EndFillets` and `EndEdge`. They mirror `ui.SurfaceShape`,
   and flush panels reuse them.
2. **The attached bar keeps its theme radius.** Capsules inherit
   `style.Radius`, so zeroing it would turn every pill into a stadium. The body
   is squared through `CornerMask` instead.
3. **One-pass body fill.** `squareAttachedEdge` blended a second band over the
   rounded fill. On any translucent attached surface that painted a denser
   band along the attached edge, so it was replaced.
4. **The 1 px overlap only happens under an opaque bar.** Over a translucent
   bar the doubled row would paint a darker line. The frosted default
   therefore has no overlap. Check it live for a hairline seam at fractional
   scale.
5. **End wedges paint after the final clear,** so `clearOutsideRoundedRect`
   needed no exception for them. The clear now takes per-corner squaring and
   per-side joints.
6. **Attached panels ignore `panels.gap`.** The joint wedges have to touch the
   bar. Floating and detached panels keep the gap. Detached panels use
   `theme.MarginS`.

## Fixes found along the way

- **The bar's `OpaqueBackground` hint is fixed at build.** It is now resolved
  as if blur were present, so a frosted bar that gains blur later is never
  marked opaque.
- **The panel trigger zone.** It used the bar's configured height, which now
  includes the overhang, and its fallback assumed a floating bar. It now
  subtracts the overhang and falls back to `Extent()`.
- **Plugin panel resize** sent the bare panel size and dropped its joints. It
  now keeps them and sends the body input region.
- **A compositor without the protocol** sent no capability event, so the shell
  could not say why frosted painted solid. The platform now reports the
  absence once, and the shell logs it.

## Automated gate

Fresh runs on 2026-09-27 from the worktree at `893dd9e`, on the owner's
machine:

```text
gofmt -l .                                            no output
GOMAXPROCS=4 go vet -p 2 ./...                        exit 0
git diff --exit-code origin/main -- go.mod go.sum     exit 0
GOMAXPROCS=4 go test -p 2 -count=1 ./...              25 packages ok; 2 FAIL (below)
go test -race, one package at a time: render, ui, config, settings,
  platform/wayland                                    ok
go test -race ./internal/shell -run
  'Panel|Joint|Blur|Placement|Attached|Wallpaper|Capabilit'
  (at 7f7512e)                                        no data race; only the two
                                                      pre-existing failures it selects
```

Failures, all pre-existing (the same sets fail at the branch base `4e1f888`):

- `internal/shell`: `TestPanelSectionValidationPrecedesMutation`,
  `TestRightClickingTheBarBatteryOpensSession`,
  `TestRightClickingBatteryCapsulePaddingOpensSession`,
  `TestABatteryWidgetOpensTheSessionPanel`.
- `tests/integration`: 16 tray tests (`TestTray*`).

The execution handover lists these as container-only. They also fail on this
machine, identically at the base.

## Live gate (owner, Niri 26.04 or later, `DP-1`)

Not yet run. Record the results here.

- [ ] Each style × shape (frosted, solid, islands × attached, floating), over a
      bright and a dark wallpaper.
- [ ] Open every panel. Joints are visible and continuous, with no hairline at
      the seam; check the frosted default especially (deviation 4). Settings,
      the launcher and the clipboard float.
- [ ] Session and Notifications sit flush to the right edge, with a screen
      fillet below.
- [ ] Switch style and shape live from Settings. The bar restyles through the
      reload path. **An open panel keeps the placement it opened with until
      it is reopened.**
- [ ] Start on a compositor without the protocol and confirm frosted falls back
      to solid, with the one line
      `shell: the compositor offers no blur (...)`.
- [ ] The centre pill's hairline (`FillOutlineVariant`, opaque) on a
      translucent pill: if it reads heavy, mix it at the pill alpha.
- [ ] With xray on, look for the wallpaper-coloured fringe on rounded edges
      (`docs/niri-blur.md`, "Known artefact").

## Remaining work

Everything below is tracked in bd, which is the source of truth for status.
This section gives the order and the starting points. A fresh session should
read, in order: this section, "Deviations from the plan", the design, and §6
of the execution handover (the reference-shell research).

### 1. Finish this branch (blocks the merge)

1. **The owner runs the live gate** above, on Niri 26.04 or later. The live
   gate is the owner's; do not mark it passed from tests.
2. **Fix what it finds.** Likely candidates:
   - A hairline between a panel and a **frosted** bar. There is no overlap
     over a translucent bar (deviation 4). Options: overlap by 1 px and paint
     the panel's first row at alpha 0 over the overlap; or accept the seam if
     the blur hides it. The overlap decision is in `spawnPanelLocked`
     (`place.Overlap`).
   - A heavy centre-pill hairline on a translucent pill. Mix its
     `FillOutlineVariant` stroke at `Theme.PillAlpha` in `barStyle`
     (`internal/shell/bar.go`).
   - A wallpaper fringe on rounded edges under xray. This is documented, and
     not fixable client-side beyond shrinking the region further.
3. **Record the gate results** in the live-gate section of this file, then
   close `sysc-551` and the epic `sysc-546`, and merge #16. The owner reviews
   and merges; do not merge without being asked.

### 2. Cleanup (owner's call; ask first)

- Delete the four tool-prefixed remote branches, then the local pre-rewrite
  branch (tool-prefixed, tip `0e24c83`), `backup/bar-surface-pre-rebase`, and
  the `.worktrees/bar-surface` worktree, once #16 merges.

### 3. Known bugs and limits

- **`sysc-586`: pre-existing test failures.** Four `internal/shell` tests and
  sixteen `tests/integration` tray tests fail on the owner's machine, and they
  fail identically at `4e1f888`. They hide new failures in two central
  packages, so fix them before the next large bar change. The three battery
  tests report "default bar has no laid-out battery": the tests probably
  assume a battery. Check that before touching the widget.
- **`sysc-588`: this branch's limits.**
  - The panel blur ignores the reveal's slide offset.
  - A settings change does not re-place open panels (joints, flush and detach
    are decided at open; see `retheThemeOpenSurfacesLocked` in
    `internal/shell/registry.go`).
  - `pluginHost.resizePanel` (`internal/shell/pluginhost.go`) keeps the
    margins the panel opened with.

### 4. Parity with DMS, Noctalia and Caelestia

The 2026-09-25 survey chose six upgrades (execution handover §6.2). The first
row below is the foundation they depend on.

| Item | Source | State | Issue | Where to start |
|---|---|---|---|---|
| Compositor blur, exact shape including concave corners | Noctalia `applyBarCompositorBlur`; DMS `WindowBlur.qml` | **Done** | – | `ui.BlurStrips`, `HostCallbacks.BlurShape` |
| 1. Frosted bar | all three | **Done**, minus the mock's rim and shadow | `sysc-587` | `barStyle`; a bar `Rim` and a shadow need the surface to grow like the overhang |
| 2. Floating islands | DMS island mode | **Done** | – | – |
| 4. Attached bar with concave joints, flush panels | Noctalia `concaveEdgeCorners`, `panelOverlap` | **Done** | – | – |
| Luminance-scaled pill lift | Caelestia `Colours.layer()` | Fixed 0.3 lift | `sysc-552` | `theme.Color.Luminance` (`internal/theme/contrast.go`); the wallpaper service for the wallpaper under each output |
| 3. Segmented groups, press squish | DMS segments, `pressedRadius` | Not started. **Now unblocked:** `render.CornerMask` gives per-corner radius | `sysc-553` | `measureSegmented`/`layoutSegmented` (`internal/ui/layout.go`); the bar `group` capsule |
| 5. Workspace track | Caelestia `OccupiedBg`, `ActiveIndicator` | Not designed; sketch in §6.2 | `sysc-555` | `internal/shell/workspacepills.go`; the Niri projection already has occupied and urgent |
| 6. Live centre island | DMS `DankIsland` | Not designed; sketch in §6.2 | `sysc-556` | The centre pill (`2026-09-25-centre-pill-design.md`); replaces the volume OSD (`internal/shell/osd.go`) |
| Centre pill open state | – | Not started | `sysc-554` | The centre pill |
| Hanging-tab centre (option E) | – | Not started. **Now unblocked:** per-corner radius and a surface taller than its zone both exist | `sysc-557` | `render.CornerMask`; `config.Bar.SurfaceExtent` shows the overhang pattern |
| Rim, soft and contact shadows, capsule border and opacity, hover tint, bar length or frame mode, corner styles, auto-hide, scroll and dead-zone actions | Noctalia, DMS, Caelestia | Not designed | `sysc-587` | Design first, and pick with the owner |

**Assessment.** The *surface* layer is at parity: compositor blur with exact
shapes, the three styles, the attached bar, and panels joined to it with
concave joints. That is the part the others build everything on, and the part
sysc-shell lacked. Three of the six survey upgrades are done. The other three
are the ones that make those shells feel alive (the workspace track, the live
island, segmented groups), and they are not designed yet. Before this branch
they were blocked on per-corner radius and taller-than-zone surfaces, which
now exist.

**Suggested order:**

1. `sysc-586`, so the gates are trustworthy again.
2. The workspace track (`sysc-555`): smallest, and the most visible daily.
3. Segmented groups (`sysc-553`).
4. The live centre island (`sysc-556`) with the pill open state (`sysc-554`).
5. The hanging tab (`sysc-557`) if the owner still wants it.
6. `sysc-587` items as the owner picks them.

Each new feature needs its own design → plan → implementation, as this one
had. The owner stops for review between phases.

### 5. Working rules for the next session

- **No tool attribution in this public repo:** no co-author trailers, no
  tool-prefixed branch names, no links to externally hosted artifacts in
  committed files, no generated-by footers in PR bodies. Before pushing, check
  that `git log -p <base>..HEAD | grep -ciE 'cl[a]ude|anthr[o]pic'` prints
  `0`. The brackets keep this file from matching its own check.
- **The commit-msg hook** rejects the substrings `bot` (so "bottom" and
  "both"), `agent`, `cursor`, `codex`, `llm` and similar. Write around them;
  never `--no-verify`.
- **Never run an uncapped `./...`, and never run `-race` over `./...`.** Both
  have hard-locked this machine. Use `GOMAXPROCS=4 go test -p 2 ./...`, and
  race one package at a time.
- **Tests that open panels** need a bar on the output
  (`withTestBar(t, reg, 7, cfg)` in `panelhost_test.go`). Without one, panels
  detach by design. Configure a panel at its spec's `Width`/`Height`, not at
  `panelTargetSize`, because the joints widen the surface.
- **The status of this work lives in bd** (`bd list`), under epic `sysc-546`.
