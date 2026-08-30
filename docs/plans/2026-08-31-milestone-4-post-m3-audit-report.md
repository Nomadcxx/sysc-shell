# Milestone 4 Post-M3 Plan Audit

Date: 2026-08-31
Issue: `sysc-16`
Scope: the M4 roadmap gate, 4A/4B designs and plans, landed M1-M3 APIs, and existing 4A prework.

## Verdict

M4 still fits the roadmap, but the executable plans needed correction before integration work.
The original system-monitor blocker has cleared: M3 pins `sysc-metrics@v0.2.0` and owns sampler
lifetime. The corrected 4A plan restores the popout by consuming `services.Metrics`; it adds no
wrapper service or dependency.

## Applied findings

| Severity | Finding | Correction |
|---|---|---|
| blocker | 4A subtracted 8 from `wl_keyboard.key`. Wayland already supplies the evdev code, so Escape (`1`) would underflow. | Preserve the key value. Add 8 only if a future XKB lookup requires it. |
| blocker | The panel invalidation snippet replaced M2/M3's `wl_registry` global identity with connector identity. | Extend `Invalidation{Global}` with `SurfaceID`; do not rekey bars. |
| major | The old D10 condition cleared when 3B landed, but the plan still rejected `system-monitor`. | Restore CPU/memory plus bounded configured-resource tabs over `services.Metrics`, selector leases, histories, and `KindGraph`. |
| major | Task 9 expected reload to close panels while Task 6b and 4B require them to stay mapped. | Keep aux surfaces mapped and rebuild their content/theme in place. |
| major | `config.Theme.BackgroundOpaque` could not read generated palette tokens after 4A removed config colours. | Move the decision to `shell.Theme` and carry the boolean through `HostCallbacks`. |
| major | 4A accepted `source: stock` before any stock-name table existed. | Keep 4A to wallpaper/hex; 4B Task 12 adds stock names and validation. |
| major | 4B named a nonexistent text-input-v3 `SetSurfaces` request and allowed a local module replacement. | Use the protocol's real requests, release `sysc-wayland`, and pin the tag without `replace`. |
| major | Scroll controls had no complete pointer-axis transport contract. | Preserve fractional deltas, coalesce one event per pointer frame, and avoid double-applying discrete and continuous forms. |
| major | Config persistence used a predictable `path + ".tmp"` file and omitted sync/cleanup behavior. | Use `CreateTemp` in the target directory, mode 0600, file and directory sync, rename, and error cleanup. |
| medium | 4A described clickable bar launchers without keyboard parity or implementation steps. | Open 4A panels through IPC and compositor hotkeys; defer bar launchers until the bar has a keyboard-focus contract. |
| medium | Runtime binary policy and the 14-task 4B review cadence were unresolved. | Require no-shell argv execution with bounded failure handling; merge 4B at Tasks 8 and 11 before continuing. |
| minor | Task references, fake-compositor gate number, KDL JSON quoting, and stock/theme wording had drifted. | Align names and examples with the corrected task order and Niri syntax. |

## Preserved constraints

- `sysc-5` remains owner-deferred hardware evidence. The audit does not mark it passed.
- One goroutine owns every Wayland proxy.
- M3 keeps the only `sysc-metrics` sampler owner and process pump.
- Configuration reload stays acquire-before-release and retains open aux surfaces.
- M4 adds UI kinds only for named 4A or 4B consumers.

## Execution note

Commit these plan corrections on `main`, then rebase `milestone/panels-controls`. Existing Tasks 1,
5, and 8 prework provides the baseline. Apply Task 5's corrected public panel name and ordinary
`(output, ok)` lookup during integration; preserve all landed M3 tests.
