# Stable Multi-Output Bar Implementation Handover

Date: 2026-08-29
Status: Design approved. Plan written. **All 16 plan tasks implemented. Automated gate green. Live gate
not run.**
For: the orchestrating agent reviewing Milestone 2.

## Summary

Milestone 2 is code-complete against its plan and passes every automated check. It has **not** been run
against a live compositor, so the milestone's exit gate is not met and the work is not qualified.

| Artifact | Path | State |
|---|---|---|
| Design | `docs/plans/2026-08-29-stable-multi-output-bar-design.md` | Owner-approved, on `main` |
| Plan | `docs/plans/2026-08-29-stable-multi-output-bar.md` | 16 tasks, on `main` |
| Implementation | branch `milestone/stable-bar` | 18 commits, green |
| Live gate | `tests/integration/README.md` | Written, **not executed** |

## Repository state

| Item | Value |
|---|---|
| `main` HEAD | `296b0eb` — design and plan |
| Branch | `milestone/stable-bar`, 18 commits ahead of `main` |
| Worktree | `~/.config/superpowers/worktrees/sysc-shell/milestone/stable-bar` |
| Remote | `main` is **28 commits ahead of `origin/main`, unpushed**; the branch is local only |
| `proof-v0` tag | still not created |

Automated gate, all exiting zero on the branch:

```bash
go mod tidy -diff        # silent
go test -race -count=1 ./...
go vet ./...
go build ./cmd/sysc-shell
gofmt -l .               # silent
```

## What was built

**Ownership.** One process, one `wl_display`, one owner goroutine. Per-surface state moved off `owner`
into `OutputHost` (`host.go`), held in a `hostSet` keyed by `wl_registry` global name (`hosts.go`).
Shared globals, seat, pointer, config and poll state stay on `owner`.

**Discovery.** `wl_output` binds inside the registry global handler, so an output present at startup and
one hotplugged later travel one path. A host is ready only with both a `done` and a non-empty name;
`applyDone` reports that transition exactly once and creates the bar. `global_remove` tears the host down.

**Close recovery.** `zwlr_layer_surface_v1.closed` destroys the role but keeps the host and its
`wl_output`. Recreation is bounded: three attempts, five seconds apart, reset after sixty seconds mapped.

**Scale.** Logical size, `scale120` and buffer pixels stay three distinct values per host. Because
`wp_fractional_scale_v1` is per surface, a scale change reallocates only its own host's buffers.

**Pointer.** `pointerFocus` identifies the entered surface and keeps the latest logical coordinates and
input serial, so a press with no intervening motion acts where the pointer entered. Out-of-order leaves
are discarded; losing the pointer capability resets focus. `Event` coordinates are `float64`, floored
once at the hit-test boundary.

**Regions.** Input is the whole surface, so the transparent gap stays clickable and the screen edge is not
a dead strip. Opaque is the body minus its corner boxes, and empty for a translucent background.

**Geometry.** `body = height − 2·gap`, `surface = gap + body`, `exclusive = surface`. The default 48/4
tokens give body 40 and surface 44. The nominal 48 never reaches Wayland — verified against
DankMaterialShell and Noctalia, cited with file and line in the design.

**Layout.** `ui.ArrangeBar` pins the centre to the absolute centre of the content band, computed without
reference to the side widths. Sides truncate first; only a centre wider than the band truncates. `ui.Hit`
is recursive.

**Text.** `fontscan`-backed `FontMap` with a bounded face cache and per-rune fallback that never returns
nil. `Truncate` cuts on shaping cluster boundaries, accumulates in 26.6 fixed point so rounding happens
once, and renders empty when even the ellipsis will not fit.

**Configuration.** JSON on `encoding/json` — no new dependency. Pointer wire fields so an absent field
inherits its default. Every validation failure names the exact field path. Per-output overrides match by
connector while runtime identity stays the registry global.

**Reload.** `SIGHUP` signals; the owner goroutine re-reads and validates. `config.Resolve` resolves every
connected output *before* the config pointer swaps, so a candidate valid as a document but unresolvable
for one output is rejected whole. Geometry and anchor changes are ordinary layer-surface requests; only a
bar appearing or disappearing rebuilds the role.

**Bar.** Built from resolved tokens and configured item ids. Fixture: `shell-name` left, `workspace`
centre, `meter` + `toggle` right. No new node kinds. Workspace state is keyed by connector and held
whether or not a host exists, so one output's change redraws only that output.

## Deviations from the plan

1. **Task order.** Task 1 Step 5 forward-references Task 2's `hostSet`; Task 2 ran first.
2. **Task 3 stub skipped.** Its `attachOutputHandlers` forward-references Task 4's `hostBecameReady`; only
   the pure accumulators landed in Task 3 and the wiring landed with Task 4, where it is used.
3. **`hostSet` read methods tolerate a nil receiver.** `TestLifecycleShutdownReportsCleanupFailure` builds
   an `owner{}` literal with no set. A nil set has no hosts; mutation still requires a real set.
4. **Task 14 is an integration, not a painter rewrite.** The plan specified new `BarView`, `BarColors`,
   `PaintBar`, `Canvas.FillRounded` and `DrawMask`. The existing painter already walks absolute node
   bounds, so the three arranged sections flatten into its child list and it needed only truncation. The
   named types were not created. **A rounded-corner painter does not exist**, so the opaque region
   currently excludes corners the painter still fills square — see Known defects.
5. **Two obsolete tests replaced.** `TestParseOptions*` covered the removed `--output` flag;
   `TestRegistrySelectsOutputByName` covered removed selection. Replaced with a `NIRI_SOCKET` guard test
   and a connector-recording test.
6. **`ThemeFrom` takes a resolved bar policy**, not just the config. A registry test caught the original
   signature dropping per-output geometry overrides.

## Known defects and gaps

- **Rounded corners are declared but not painted.** `theme.Radius` defaults to 12 and shapes the opaque
  region, but the painter fills the body square. On a live run the corners will be opaque squares where
  the opaque region promised transparency. Either paint the radius or set the default radius to 0 before
  the live gate. **This is the first thing to fix.**
- **`Options.Height/Gap/Radius` are mutated per host during reload** in `applyConfig`, then read by
  `applyGeometryRequests`. With differing per-output overrides the last host's values persist on `owner`.
  It works because each host is handled immediately after its assignment, but it is a latent bug: the
  geometry belongs on the host, not on shared options.
- **`Proof.Layout`, `Root`-era helpers and `ProofStyle` still carry proof-era naming.** The type is now a
  per-output bar; the rename was not part of any task.
- **No test asserts a full `createBar` → configure → map sequence**, because that needs a compositor.
  Coverage stops at the pure state machines.
- **`activeWorkspace` and `Proof.UpdateNiri` are now dead** in the production path; `Registry` owns the
  projection. They remain for the older proof tests.

## Environment limits

- **No live Niri this session.** `NIRI_SOCKET` and `WAYLAND_DISPLAY` were unset, so none of the 13 live
  checks ran. Checks 6 (transform) and 10 (physical pointer) also remain the two proof checks deferred on
  2026-08-28.
- **The output-transform contract is unverified.** The design asserts the configure size is already
  post-transform and that `set_buffer_transform` must not be called. No rotated output has ever been
  exercised on this project. Live check 6 decides it.
- **The commit hook matches naive substrings.** `bot`, `ai` or `agent` anywhere in a message is rejected,
  so the ordinary word "both" fails. Bisect with `git commit --allow-empty -m test -m "<paragraph>"`.

## Recommended next steps

1. Fix the rounded-corner mismatch — paint the radius, or default it to 0.
2. Move bar geometry from `Options` onto `OutputHost` so per-output overrides cannot alias.
3. Run the live matrix in `tests/integration/README.md` on a machine with two outputs, one unpluggable and
   one rotatable. Record observations in the milestone handoff.
4. Only then consider `bar-v0`, and settle the six owner-review items in the design — R1 (retry budget) and
   R5 (item vocabulary) are now implemented as recommended and want confirmation; R2, R3, R4 and R6 stand
   as written.

Do not treat this branch as qualified. Every claim above is from unit tests, `vet` and a build; none is
from a compositor.
