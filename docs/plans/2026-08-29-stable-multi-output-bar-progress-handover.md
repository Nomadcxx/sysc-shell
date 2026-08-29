# Stable Multi-Output Bar Progress Handover

Date: 2026-08-29
Status: Design approved. Plan written. Implementation started, 3 of 16 tasks complete.
For: the orchestrating agent continuing Milestone 2.

## What this session produced

| Artifact | Path | State |
|---|---|---|
| Design | `docs/plans/2026-08-29-stable-multi-output-bar-design.md` | Owner-approved, on `main` |
| Implementation plan | `docs/plans/2026-08-29-stable-multi-output-bar.md` | Written, on `main`, 16 tasks |
| Implementation | branch `milestone/stable-bar` | Tasks 1-3 done, tree green |

## Repository state

| Item | Value |
|---|---|
| `main` HEAD | `296b0eb` — design and plan commit |
| Milestone branch | `milestone/stable-bar` |
| Worktree | `~/.config/superpowers/worktrees/sysc-shell/milestone/stable-bar` |
| Branch commits | 4, listed below |
| Remote | `main` is **28 commits ahead of `origin/main` and unpushed** |
| `proof-v0` tag | still not created |

```text
5773ced feat(wayland): accumulate per-output metadata and readiness
b481d54 refactor(wayland): move per-surface state onto OutputHost
45ec455 feat(wayland): add registry-keyed output host set
74706ff feat(wayland): add OutputHost with per-surface state and close budget
```

Automated gate on the branch: `go build ./...`, `go vet ./...` and `go test -race -count=1 ./...` all
pass, including every pre-existing proof test. Single-output behavior is unchanged, which is what Task 1
required.

## Design decisions worth knowing before continuing

Three questions the handover left open were answered from prior art rather than by asking the owner. The
sources are cited with file and line in the design.

**Bar geometry.** DankMaterialShell's `DankBarWindow.qml` resolves the owner's 48/44/4 baseline:
`Theme.barHeight` is 48 and is a token that never reaches Wayland; `effectiveBarThickness` is 40;
`effectiveSpacing` is 4; `exclusiveZone` is `thickness + spacing` = 44; and `implicitHeight` exceeds the
exclusive zone by a shadow buffer the input mask excludes. Noctalia states the same rule canonically in
`src/shell/bar/bar_reserved_zone.h`. The adopted derivation is `body = height - 2*gap` and
`surface = gap + body`, giving 40/44 from 48/4 — the same result as DMS's three-term formula. The gap
lives **inside** the surface with a zero layer margin, so the screen edge stays clickable.

**Centring.** The centre is pinned to the absolute centre of the content band, computed without reference
to the side widths (`DankBarContent.qml:377`). Sides truncate first; the centre truncates only when it
alone exceeds the band.

**Configuration format.** Noctalia v4 (`~/.config/noctalia/settings.json`) and DMS both use JSON, so the
shell uses `encoding/json`. No new dependency, and the owner never needed to be asked about a parser.

## Completed tasks

**Task 1 — `OutputHost`.** `internal/platform/wayland/host.go`. Holds every per-surface field that used
to sit on `owner`: surface, layer role, fractional-scale, viewport, `surfaceState`, scheduler, buffer
generations, frame callback, cleanup stack, and the close-retry budget. `client.go` threads a
`*OutputHost` through `createSurface`, `onConfigure`, `onPreferredScale`, `reconfigure`,
`onBufferRelease`, `sweepRetired` and `renderJob`. `owner` keeps only shared state.

**Task 2 — `hostSet`.** `internal/platform/wayland/hosts.go`. Keyed by `wl_registry` global name, with
arrival ordering so render and shutdown order are deterministic. Tests cover create-once-per-global and
reconnect-under-a-new-global producing no duplicate.

**Task 3 — output metadata.** `applyGeometry`, `applyMode`, `applyName`, `applyDone` on the host.
`applyDone` reports the ready transition exactly once, and readiness requires **both** a `done` and a
non-empty name.

## Remaining tasks

Tasks 4 through 16 of the plan, unstarted. In dependency order: bind outputs in the registry handler and
create one bar per ready output (4); hotplug, unplug and bounded layer-close recovery (5); per-host scale
isolation tests (6); pointer focus across surfaces (7); input and opaque regions (8); theme tokens (9);
three-section layout (10); system fonts, fallback and truncation (11); configuration load and validation
(12); transactional SIGHUP reload (13); the bar model and command rewrite (14); isolation, shutdown and
regression tests (15); live matrix and milestone handoff (16).

Task 14 is the largest and touches the most files. Tasks 9 through 12 are independent of each other and
of the Wayland work, so they can proceed in separate worktrees once Task 4 fixes the `OutputHost` API, as
the orchestration plan allows.

## Deviations from the plan, and defects found in it

These were found while executing and are worth carrying forward. The plan file on `main` has **not** been
amended for the first two; the executor should expect them.

1. **Task 1 Step 5 forward-references Task 2.** It says to add `hosts *hostSet` to `owner`, but
   `hostSet` does not exist until Task 2. Task 2 is purely additive and depends only on Task 1's earlier
   steps, so it was executed first. Keep that order.
2. **Task 3's `attachOutputHandlers` forward-references Task 4's `hostBecameReady`.** The plan works
   around this with a temporary stub. The stub was skipped: only the pure accumulators and their tests
   were implemented, and the handler wiring belongs with Task 4, which is where it is used. Task 3's
   `TestTransformedOutputUsesSwappedConfigureDimensions` was pulled forward from Task 15, since it tests
   the accumulators directly.
3. **`hostSet` read methods tolerate a nil receiver.** The pre-existing
   `TestLifecycleShutdownReportsCleanupFailure` builds an `owner{}` literal with no host set, and
   `shutdown` now iterates hosts. A nil set semantically has no hosts, so `each`, `get`, `byConnector`
   and `len` return empty rather than panicking. Mutation still requires a real set.
4. **Task 1's `owner` keeps a `selected *OutputHost`.** Task 1 must not change single-output behavior, so
   hosts are created for every bound output while only the selected one gets a surface. Task 4 should
   delete `selected`, `Options.Output`, `chooseOutput`, and the `--output` flag together.

## Environment limits hit this session

- **No live Niri.** `NIRI_SOCKET` and `WAYLAND_DISPLAY` were unset, so nothing in the live matrix ran and
  the two proof checks deferred on 2026-08-28 — a physical pointer click and a non-1 scale on a spare
  output — remain open. They are live matrix checks 10 and 6 in the design.
- **The commit hook matches naive substrings.** `ERROR: Commit message contains AI/agent attribution!`
  fires on any message containing `bot`, `ai` or `agent` as a substring, so the ordinary word "both" is
  rejected. Bisect a rejected body with `git commit --allow-empty -m test -m "<paragraph>"`.

## Recommended next step

Execute Task 4 in this worktree. It is the pivot: it deletes the single-output selection path and creates
a bar per ready output, and Tasks 5 through 8 all build directly on the `hostBecameReady` and `createBar`
entry points it introduces. Do not start Tasks 9 through 12 in parallel worktrees until Task 4 has landed
and the `OutputHost` API is fixed.

Six owner-review items in the design remain open and should be settled before the code they govern is
written: the layer-close retry budget (R1, Task 5), shadow-band timing (R2, Task 8), fixture composition
(R3, Task 14), the unverified output-transform contract (R4, live check 6), the configuration item
vocabulary (R5, Task 12), and ignoring integer `wl_output.scale` (R6).
