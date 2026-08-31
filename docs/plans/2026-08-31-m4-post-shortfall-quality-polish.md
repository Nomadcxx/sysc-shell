# M4 Post-Shortfall Quality Polish

Date: 2026-08-31
Scope: leftovers after sysc-38–40, then the four polish tasks in `2026-08-31-m4-post-shortfall-polish.md`.

Previous quality findings 1–4 were the shortfalls. Finding 5 (Show vs aux) was half-fixed. Finding 6 (`set-mute`) was fixed. Finding 7 (`status`) was open.

## Applied in this polish

1. **`hideAll` no longer holds `Registry.mu` across `sendAux`.** Same deadlock class as `Show`. `Close` uses `prepareHide` and sends after unlock (`closed` makes those sends fail-open).
2. **Template apply supersede keeps the latest job.** A palette change while apply is in flight no longer reruns the stale home/tokens.
3. **`Registry.Status()`** reports `version`, `audio`, `brightness`, `panels`, `matugen`, `templates`. IPC `status` uses it.
4. **`Focusables` walks `KindVirtualList.Item`.** Settings dropped the pre-fill `Children` loop that defeated virtualization and left focus on stale nodes after layout.

## Inspected, not a defect

- Standing OSD leases still start a relay goroutine per `setAudio`/`setBrightness`. The previous relay exits on `Registry.closed`, not on swap. Tests swap once; production constructs once. Ceiling: close the old `Changes` channel or capture the instance in the relay (already does) and return when that audio is `Close`d — `Changes` is never closed. A leaked wait until process exit if `setAudio` is used more than once. Leave it.
- `generateTheme` still assigns apply errors to `_`. Settings persist-then-reload has no channel to surface a skipped foreign kitty.conf. The apply function returns the error; the palette path is fire-and-forget. Documented ceiling.
- OSD motion is slide offset, not alpha fade. Reduced-motion still skips the ticker.
- `ApplyEnabled` is process-global. Tests that call it are serial. Correct for one shell process.

## Tests that now prove what they claim

| Test | Now |
|---|---|
| `TestAcceptOsdOnEachOutputExternalChange` | Uses `setAudio` (standing lease). No test-held `Acquire`. |
| `TestAcceptKeyboardOnlyAllControls` | PageDown on a tree that is already `KindVirtualList`. |
| `TestOsdShowReleasesLockBeforeAux` / `TestOsdHideReleasesLockBeforeAux` | Lock vs full aux channel. |
| `TestApplyEnabledSupersedeUsesLatestHome` | Latest `$HOME` receives the write. |
| `TestFocusablesWalksVirtualListItem` | Empty `Children`, `Item` still focusable. |
| `TestRegistryStatusReportsOpenPanelsAndTemplates` | Open settings + default niri enable. |

## Not applied

Real icon-font OSD chrome. Live Niri checklists. M3 `sysc-31`–`sysc-35`.
