# M4 Spec Review Sweep

Date: 2026-08-31
Issue: `sysc-36`
Commit: `01cae98` (`main`, merge of `milestone/panels-controls`)
Documents: `2026-08-30-panel-foundation-design.md` D1–D13, `2026-08-30-settings-osd-theme-catalog-design.md` D1–D10, the corrected 4A/4B plans, `2026-08-31-milestone-4-post-m3-audit-report.md`, `AGENTS.md`, and the Milestone 4 roadmap gate.

Live Niri matrices are owner-deferred and are not failures. M3 audit defects `sysc-31`–`sysc-35` are out of scope.

## Verdict

4A matches the approved design. The 13 plan tasks are on `main`. The post-M3 audit corrections hold: evdev keys are unmodified, `Invalidation.Global` is kept with `SurfaceID` added, system-monitor reuses `services.Metrics` without a second sampler or a `sysc-metrics` import in the popout, reload leaves aux surfaces mapped, opacity is resolved in `shell.Theme`, and there is no `replace` directive.

4B's 14 plan tasks have commits, but three contracts are not met in the landed code. Those are spec defects, not missing task commits. Follow-up issues: `sysc-38` (OSD), `sysc-39` (template apply paths), `sysc-40` (settings schema/virtual list).

`sysc-3` stays open. Live checklists were recorded unrun. `sysc-12` (launcher prior-art) is research for Milestone 7, not a 4A/4B ship item.

## Charter (`AGENTS.md`)

| Constraint | Result |
|---|---|
| Go only; no C++/Rust/Lua/Qt/QML | Holds. |
| Wayland types in `internal/platform/wayland`, Niri types in `internal/platform/niri` | Holds. |
| Pin `sysc-wayland`; no `dankgo` | Holds at `v0.2.0` (4B Task 1 release). No `replace`. |
| One Wayland-owner goroutine | Holds. Panel reveal and OSD hide timers send through channels. |
| Draw after invalidation | Holds. |
| UI primitives only for an approved component | 4A kinds: tab/button/separator. 4B kinds: toggle, slider, menu, text field, scroll, virtual list. |
| Niri first; no lock screen or compositor | Holds. Lock delegates to `session.locker`. |
| Runtime binaries: `LookPath`, argv, no shell | Holds for `loginctl`, `wpctl`, `brightnessctl`, `matugen`. |

## Roadmap exit gate

| Gate item | Evidence | Result |
|---|---|---|
| clock/calendar or system-monitor on each output | Both ship. Placement tests cover multi-output IDs. Live open-on-every-output unrun. | Automated hold; live deferred. |
| only the open panel requests keyboard focus | Panel `Exclusive`, shield/OSD `None`, single instance. | Holds in code. Live fall-through unrun (D3). |
| panel never changes the bar exclusive zone | `TestGateExclusiveZoneUnchangedByPanels`; aux `ExclusiveZone: -1`. | Holds. |
| placement within transformed/scaled bounds | `TestGatePlacementWithinBounds`; clamp on tiny outputs. | Holds. Live transform unrun. |
| keyboard-only covers every shipped control | 4A `TestGateKeyboardOnlyCoversControls`; 4B `TestAcceptKeyboardOnlyAllControls`. Virtual list is not a settings child (see 4B). | Holds for controls that are actually in the tree. |
| every interactive node has name and role | `TestGateAccessibleNamesAndRoles`, `TestAcceptAccessibleNamesRoles4B`. | Holds. |
| reduced-motion and high-contrast change behaviour | Reduced-motion skips reveal ticks. High-contrast is a generation flag through matugen `--contrast 1`. | Holds. |

## Tranche 4A — D1–D13

| # | Decision | Result |
|---|---|---|
| D1 | Shield + panel, Overlay, exclusive_zone −1 | Holds. `shieldSpec` / `panelSpec`. |
| D2 | Keyboard Exclusive on interactive panels | Holds. |
| D3 | Focus restoration by niri fall-through | Code unmaps Exclusive; live confirmation unrun. Contingency (explicit restore) not triggered. |
| D4 | One instance per panel ID; other-output retrigger moves it | Holds. `PanelSet`. |
| D5 | Bar-edge floating placement, IPC/hotkeys, no pointer-only launcher | Holds. Settings overrides align to `center` (4B D1). |
| D6 | SDF rounding and pre-blurred shadows via `blendMask` | Holds. |
| D7 | 4A controls only: button, label/separator, tabs; graph reused | Holds. Monitor uses `KindGraph`. |
| D8 | Roving focus Tab/arrows/Space/Enter/Escape | Holds. |
| D9 | matugen wallpaper/hex, fallback palette, 4B adds stock | Holds. Colour fields removed from schema; stale keys fail load. |
| D10 | System-monitor reuses `services.Metrics`; CPU+memory always; one FS/block/net from focused bar | Holds. `popout_monitor.go` does not import `sysc-metrics`. |
| D11 | Session via `loginctl` argv; locker optional | Holds. Missing `loginctl` hides those actions. |
| D12 | `ipc.v1.sock`, newline JSON, `panel.*` / `status` | Holds. `settings` accepted. `osd.step` reserved and implemented in 4B. |
| D13 | Reveal fade+slide ~150 ms, instant under reduced-motion | Reduced-motion instant holds. Reveal is 200 ms (`revealDuration`), not ~150 ms. Minor drift. |

Reload contract: `TestReloadKeepsOpenPanel` — `PrepareConfig` does not send aux close/open. Holds.

Post-M3 blockers: `keyboard.go` documents evdev unchanged; `TestKeyboardDeliversEvdevEscape`. `Invalidation{Global, SurfaceID}`. `HostCallbacks.OpaqueBackground` from `Theme.BackgroundOpaque()`.

## Tranche 4B — D1–D10

| # | Decision | Result |
|---|---|---|
| D1 | Settings is a 4A panel, centered, shield, Exclusive, single instance | Holds. |
| D2 | Schema-driven registry, bool/int/enum/string | Holds as a table. Gaps in coverage: see `sysc-40`. |
| D3 | Search hides sidebar, lists matches | Holds. Two-stage Escape clears query then closes. |
| D4 | Single-line text-input-v3 + cursor-shape I-beam | Holds. Bound optionally; `WantIME` / `IBeamAt` on the panel host. |
| D5 | Audio poll while leased; external delta → OSD | **Miss.** `OSDStep` takes a transient lease and releases. `relayAudioOSD` only sees `Changes()` while leased. Nothing holds a standing lease, so `wpctl` from a terminal does not show OSD. Design also says “leased by the OSD while visible”, which cannot start the OSD on an external change. Acceptance item 4 needs a process-lifetime lease (or equivalent). Filed `sysc-38`. |
| D6 | Brightness sysfs read; `brightnessctl` step; zero devices unavailable | Holds for reads. Same lease/OSD wiring as audio. |
| D7 | OSD per bar output, Overlay, keyboard none, −1, no shield, 1.5 s reset, glyph+label+bar, fade+slide | Surface policy and timer hold. **Content is a fill bar only** — no glyph, no label. **No fade/slide**; reduced-motion test only asserts one invalidation. Filed `sysc-38`. |
| D8 | ~10 stock family seeds through matugen | Holds. Ten names. Unknown stock seed fails config. |
| D9 | 16 templates, ours-only writes, niri include reversible, gtk name only if unset/ours | Catalog and niri/gtk hooks hold. **HookWrite targets `~/.config/sysc-shell/themes/<name>.conf`**, not the app XDG paths. **No kitty SIGUSR1.** Apply errors are discarded (`_ = ApplyNiri`). No single-flight supersede queue. Filed `sysc-39`. |
| D10 | Documented XF86 binds spawn `osd.step` | Holds in `docs/niri-hotkeys.md`. Design prose once said `ipc osd <kind> <action>`; the IPC section and plan Task 11 match the code. |

### Settings registry vs design table

| Section | Design | Landed |
|---|---|---|
| Bar | edge, height, gap, padding, spacing, font, **items per section** | Geometry and font. **No item add/remove/reorder.** |
| Widgets | options from the **configured** widget list | Built from `config.Default()` at `settings.Default()`, not the live bar. |
| Appearance | source wallpaper/hex/stock, seed, scheme, mode, high-contrast, template toggles | Present. `scheme` is a free string, not an enum. High-contrast lives under Accessibility as well (4A: accessibility owns the flag). |
| Panels | gap, padding, OSD position | Holds. |
| Session | locker | Holds. |
| Accessibility | reduced-motion, high-contrast | Holds. |

Virtual list: `KindVirtualList` and `Node.Item` exist and have layout tests. Settings content is `KindScroll` of concrete rows. Task 5 named settings as the consumer; the 4B keyboard gate mutates the scroll node's kind to pretend. Filed `sysc-40`.

`status` reports `audio` / `brightness` booleans. It does not report open panels, matugen presence (4A), or enabled templates (4B).

## Plan task inventory

4A Tasks 1–13 and 4B Tasks 1–14 each have a commit on the merged branch. `go.mod` pins `sysc-wayland v0.2.0` and `sysc-metrics v0.2.0`. `gofmt -l .` was empty at merge. `go test ./...` exited 0 on `01cae98`.

Task 14 live checklist is recorded unrun in `tests/integration/README.md`. That is owner-deferred evidence, not a silent pass.

## Out of scope, named so they are not re-filed

- Live Niri 4A/4B checklists and `sysc-5`.
- AT-SPI export, multi-line input, bar pointer launcher, attached placement.
- `sysc-12` launcher research (Milestone 7 consumer).
- M3 tooltip/stream/`Metrics.Leased` defects (`sysc-31`–`sysc-35`).
- Design D10 CLI wording vs `osd.step` (implementation follows the IPC section).
