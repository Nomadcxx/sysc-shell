# Bar surface styles and attached panels: execution handover

Date: 2026-09-25. Commissions Phases B–E of
`2026-09-25-bar-surface-and-attach.md` (design:
`2026-09-25-bar-surface-and-attach-design.md`). Status lives in bd; create the
issues listed in the plan's Tracking section first, since none exist yet.

## Where the work is

| Branch | PR | Contents |
|---|---|---|
| PR #13's branch | #13 | Centre pill (`6eec0d4`), its design, the bar-surface design, owner fix `294c708`. **Its tip was left at `3f5c2fa`** (Phase A pushed there by mistake); reset it to `294c708` before merging, or close #13 in favour of #15. |
| `bar-surface` | #15 | Everything in #13, the launcher/clipboard amendment, and Phase A (Tasks 1–4, `0d93063`..`3f5c2fa`). |

Start Phase B from `bar-surface` (or from `main` once #15 has
merged).

## Decisions made in conversation, not only in the design

- Frosted is the default style; solid (today's bar) and islands are options in
  Settings › Bar.
- Attached is the default shape.
- Settings, the launcher and the clipboard stay floating. The owner judged that
  attaching the launcher and clipboard would confuse users.
- Segmented bar groups do not exist (`KindSegmented` is only the panel tab
  row). They were deferred as a follow-up.
- The Caelestia-style wallpaper-luminance lift is deferred. v1 lifts pills from
  the palette only.

## Facts the next session must not rediscover

- **Capability bit.** The 1.45 XML declares `blur` as `0`. Test `flags&1`
  (`wayland.blurCapable`). Niri sends `1`.
- **Startup order.** The manager is bound before the outputs, so
  `Callbacks.Capabilities` reaches the shell before the first `NewHost`.
  `Registry.SetCapabilities` only stores the flag so far. Task 7 must re-theme
  the bars and panels when it changes.
- **Where blur is applied.** `HostCallbacks.BlurShape` is read in `renderJob`
  after `Render`, for the bar and for aux panels alike. Nothing sets it yet.
  Task 8 sets it for the bar, and Tasks 13–14 for panels.
- **The fillet shape was wrong before Phase A.** It is now concave and shared
  from `internal/ui/coverage.go`. Use `ui.FilletSpan` for regions and
  `ui.FilletCoverage` for paint.
- **One `EdgeFillet`.** `ui.SurfaceShape` uses one `EdgeFillet` with
  `EdgeLeft`/`EdgeRight` for both attached bar ends and flush panels. The plan
  text for Tasks 10 and 12 predates this; follow the type.
- **Why the joints were invisible.** `filletMargin`
  (`internal/shell/panelhost.go`) caps joints at `Panels.Padding − BarGap`,
  which is 4 px. Task 12 replaces it with per-side room.
- **Namespaces for Niri layer rules:** `sysc-shell:bar`, `sysc-shell-panel`.
- **Bar geometry.** On the default row the bar body is 40 px with 28 px pills,
  and `Theme.Fillet` is 12.
- **Known test failures.** Four `internal/shell` tests fail in a container
  without a battery or with root permissions:
  - `TestABatteryWidgetOpensTheSessionPanel`
  - `TestRightClickingTheBarBatteryOpensSession`
  - `TestRightClickingBatteryCapsulePaddingOpensSession`
  - `TestPanelSectionValidationPrecedesMutation`

  They fail identically on `main`.

## References outside the repo

- Reference shells read on 2026-09-25:
  - Noctalia `2734393`: `src/shell/bar/bar.cpp` (`applyBarCompositorBlur`),
    `src/config/config_types.h` (`BarConfig`), `docs/user/compositor-settings/niri.mdx`.
  - DMS `10e8086`: `Modules/DankBar/BarMetrics.qml`, `Modules/DankIsland/`,
    `Modules/Settings/DankBarAppearanceTab.qml`.
  - Caelestia `20e625d`: `services/Colours.qml` (`layer`, `alterColour`),
    `modules/bar/components/workspaces/`.
- Follow-up ideas from the survey, not yet designed: a workspace track with a
  stretchy indicator (Caelestia), and the centre pill as a live island for
  notifications, media and volume (DMS).
