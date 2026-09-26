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

## Known limits and follow-ups

- The panel blur region does not follow the reveal's slide offset, so for the
  length of the reveal it can sit a few pixels off the paint.
- A settings change reaches open panels' colours but not their placement:
  joints, flush and detach are decided when a panel opens.
- Follow-ups, all filed in bd: scale the pill lift by wallpaper luminance;
  segmented bar groups; the centre-pill open state; the workspace track; the
  live centre island; the hanging-tab centre element.
