# Parity tranche continuation handover

Date: 2026-09-13.

Backdrop blur is implemented and measured. This handover discharges the blur half of
`2026-09-12-parity-tranche-execution-handover.md` and commissions what the tranche still owes. Read that
earlier handover for the tranche's origin and its two standing corrections; do not re-derive them here.

## Receiving state

Branch `feature/panel-backdrop-blur` in
`/home/nomadx/.config/superpowers/worktrees/sysc-shell/feature/panel-backdrop-blur`, rebased onto `main`
at `79e9899`, **twelve commits, unmerged and unpushed**, tree clean, all affected packages green.

| Commit | Contents |
|---|---|
| `092b72a` | Vendor `wlr-screencopy-unstable-v1` and generate the binding |
| `504796e` | Bind the manager as an optional global; extract `bindSingleton`; destroy it in `destroyGlobals` |
| `3ff3deb` | The reduced-resolution box blur kernel |
| `754b7a9` | `captureRegion`, `newCaptureBuffer`, `normaliseCapture` |
| `8dbfcf5` | Bilinear sampling beside `paintImage` |
| `c68e3d1` | `Style.Backdrop` composited beneath `rootFill` through the rounded mask |
| `c57f81e` | `OpacityMinBlurred`; `opacityAlpha` takes the blur state |
| `6c86f45` | `theme.blur-behind` / `theme.blur-radius`, settings rows, preset radius |
| `749da42` | Capture at the top of `openAux`, before any surface exists |
| `95ad950` | Architecture and roadmap amendments |
| `876ca72` | **Post-rebase defect fix**: keep a panel's own alpha once it has a backdrop |
| `169dea8` | Tracker state |

Tracker: `sysc-255` (in_progress, carries the figures and every correction), `sysc-256` (open, the live
gate). Both hang off `sysc-202`.

## Measured, and what the numbers settled

On archPC, go1.27.0, powersave governor, Ryzen 7 5700X, Audio region 1120x960:

| | measured | against |
|---|---|---|
| blur kernel | **4.94 ms** median | 4.60 ms predicted |
| screencopy readback | **5.67 ms** median (4.27–7.06) | D16.1's 15 ms stop threshold |
| allocation | 0.016 ms | — |
| per open | **~11 ms** | 16.67 ms frame |

Cost is linear at 4.6–5.1 ms/MPix across six panel sizes. Both of the plan's stop-and-re-review gates are
cleared. The readback is **not** the small term D16.1 assumed: it is about the size of the blur itself, so
the honest per-open figure is ~11 ms, not ~5.

## Five design corrections the live work forced

Each was found by measurement, not by reading, and each is already applied:

1. **This Niri offers `xrgb8888` only** for a region capture, never `argb8888`. The design recorded that a
   capture needs no conversion because shm buffers are `argb8888` — true of buffers the shell *paints*,
   false of buffers it is *offered*. An x-format carries no alpha, so `normaliseCapture` fills the fourth
   byte with `0xff`.
2. **A screencopy target's `wl_shm` pool must hold exactly one buffer.** `newGeneration` always builds
   `slotCount`, so its pool is a multiple of one buffer and the compositor answers with `invalid_buffer` —
   a *protocol error*, which kills the connection rather than losing a backdrop. Pool size is the entire
   cause: memfd against tmpfs, and one buffer against two, change nothing. Hence `newCaptureBuffer`.
3. **The backdrop must go through the rounded mask**, not the raw rectangle, or a panel's deliberately
   transparent corners fill in and it reads as a square.
4. **The shell, not the wayland package, assembles a panel's `Style`.** The plan expected to hand the image
   to a painter where the surface unit's Style is assembled; there is no such site. It returns through a
   new one-way `HostCallbacks.Backdrop`.
5. **The panel-opacity bound sat at `OpacityMin` in three places** — the validator, the config loader and
   the settings row. Any one left at 80 makes the blurred floor unreachable from configuration.

Additionally, **`capture_output_region` is output-relative, not global**. D3 assumed this and never
verified it. It matters because this output sits at logical `2560,0`: a global-space region is refused with
nil, an output-relative one returns real content. Probed directly; `Placement` needs no translation.

## The defect the rebase introduced, and why it will recur

`main`'s control-centre work added `AttachedPanelStyle()`, which resolves a bar-attached panel's root at
the **bar's** opacity so the two read as one joined ground. The bar is opaque, so an attached panel
carrying a backdrop painted an opaque root straight over its own blur. Its comment names the condition it
was written under: "without a captured backdrop the two surfaces are one joined ground."

`876ca72` moves the choice into `PanelHost.rootStyle`, which keeps the panel's own alpha whenever a
backdrop is present, with a regression test. **Any future change to how a panel picks its root alpha must
keep that condition**, or the blur silently disappears again with every test still passing — the renderer
already asserts, in `TestOpaqueRootHidesTheBackdrop`, that an opaque root hides the backdrop entirely.

## Scope question the owner still owns

Design D13 excludes the bar, toasts, the OSD and tooltips. Every `PanelID` — all ten — routes through
`spawnPanelLocked` → `panelSpec`, which is the only `AuxSpec` that sets `BlurRegion`, so **every panel**
receives a backdrop when `blur-behind` is on.

D13 is **silent** on three floating overlay surfaces that are not panels and not in its exclusion list:
the running-apps menu (`runningapps_menu.go`), the tray menu (`traymenuhost.go`) and the tray drawer
(`traydrawer.go`). They are visually panel-like. Decide whether they blur before the parity slice
re-bases their chrome; the wiring is three fields on their specs, not a design change.

## Hazards this session paid for

- **Another agent deploys to the same binary path.** A Codex session works
  `feature/network-panel` in `/home/nomadx/sysc-shell` and installs `~/.local/bin/sysc-shell`. At 14:55 it
  replaced the blur build with one from `7d4ceebd8528` carrying **zero** blur code, and every live
  observation after that — mine and the owner's — was of a shell without the feature. Before trusting any
  live observation, check provenance:
  ```bash
  go version -m ~/.local/bin/sysc-shell | head -3     # mod line names the commit
  strings ~/.local/bin/sysc-shell | grep -c zwlr_screencopy_manager_v1
  ```
- **Do not verify blur by diffing screenshots against a baseline.** Three attempts were invalid: the
  screen changes between captures for unrelated reasons (a clock widget, a cursor, wallpaper motion), so
  the difference bounding box becomes the whole screen and every panel measures as "changed". Verify with
  instrumentation inside the shell, or with the owner's eyes on a binary whose provenance is confirmed.
- The five standing hazards from the 2026-09-12 handover still apply unchanged: never `go test ./...` or
  `-race`; the `commit-msg` substring list (`both` contains `bot`); `bd` only from
  `/home/nomadx/sysc-shell`, or `BEADS_DB` from a worktree; `bd export` truncates unless
  `export_hashes` is cleared first; and gopls reports false errors in worktrees.

## What the tranche still owes

Nothing below has been started. **No bd issue names smoothness, stacking, token conformance or the
template panel** — create them as you begin each slice, under `sysc-202` where they belong.

| Order | Slice | Plan | Tasks | Notes |
|---|---|---|---|---|
| 1 | Rendering smoothness | `2026-09-11-rendering-smoothness.md` | 6 | Closes the rest of `sysc-202`. Blur measured one of its four named cases; animation frame time, image-heavy grids and CPU/power remain. Also keeps the architecture document's unkept "the bar milestone adds rectangle damage" promise. |
| 2 | Noctalia v4 parity | `2026-09-11-noctalia-parity.md` | 13 | Executes three designs: the parity ladders, token conformance as Task 1 (committed **red on purpose** against 105 enumerated sites), component parity as Tasks 5A/5B. Now unblocked: it deferred its opacity floor to blur D8, which has landed. Two runtime traps are called out in the plan — `textRoleCount` is derived, and the 1.45 floor is not a constant. |
| 3 | Media service, widget and page | `2026-09-11-media-service.md` | 10 | `sysc-156`. The page waits on the control-centre spine `sysc-253` (open); service and widget do not. |
| 4 | Surface stacking | `2026-09-11-surface-stacking.md` | 6 | Follows media: its only consumer is the media card. Task 4 depended on the blur plan's bilinear path, which landed as `blendMaskImage` — **the plan names `paintImageSmooth`, which no longer exists**. Task 5 is a real consumer gate with a revert branch. |
| 5 | Template panel | none | — | Not designed. Four committed documents already name it as a planned consumer. |

Also in flight, not this tranche and owned by another session: **network panel** `sysc-157` on
`feature/network-panel` at `7d4ceeb`, Tasks 10–13 outstanding, commissioned by
`2026-09-13-network-panel-execution-handover.md`. Do not touch that branch.

## What blur itself still owes

`sysc-256` is the live gate and is **partly** exercised. Confirmed on archPC (3440x1440, scale 1.0) and
archThink (1920x1080, **scale 1.25** — the first fractional-scale exercise of this path): both painted with
blur on, no panic, no leaked layer surface, panels open and close, and no self-blur, which is the
observation D16.2 asked for.

Still unrun, and all four need a binary whose provenance is confirmed:

1. A panel over a **maximised window** — design D1 calls this the common case; only wallpaper and terminal
   backgrounds have been seen.
2. `blur-behind` **off** proven pixel-identical to before the slice.
3. The **60-minute idle** run showing no continuous redraw.
4. The **two-output** case, which neither machine can run: each has a single output. Record it as
   unrunnable rather than claiming it.

Only after those does the default flip from off.

## Deployment state as left

| | archPC | archThink (laptop) |
|---|---|---|
| binary | **foreign** — Codex's `7d4ceeb` build, no blur code | branch HEAD, sha `4db52e95…` |
| config | `blur-behind: true`, radius 24, `panel-opacity: 65` | same |
| bar | unchanged | recorder, timer, notes and world-clock removed from the bar *and* `plugins.enabled` |

Backups for both: `~/.local/bin/sysc-shell.bak-blur-<stamp>` and
`~/.config/sysc-shell/config.json.bak-blur-<stamp>`. archPC's config asks for blur that its current binary
cannot provide; that is harmless — the axes are simply unknown fields the running build never reads — but
restoring the blur build there means coordinating with the session that owns the other deployment.

## Start here

Rendering smoothness, Task 1. It is the last of `sysc-202`, it is self-contained, and it does not touch
the panel chrome the parity slice is about to re-base. Create its bd issue first, under `sysc-202`.
