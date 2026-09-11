# Rendering smoothness — Design

Date: 2026-09-11. Status lives in bd.

Approach owner-approved 2026-09-11 in brainstorming. The decisions below are
pending owner review.

Fourth document of the parity tranche. It closes the **remainder of Milestone 8**.
`sysc-202` names four cases: "animation frame time, large blurred panels,
image-heavy grids, or unacceptable CPU/power". The backdrop blur design measured
the second. This design addresses the other three, so the epic can retire on
evidence rather than stay open indefinitely.

It also keeps a promise the architecture document made and never kept. At L193:
"The proof starts with full-surface damage. **The bar milestone adds rectangle
damage after tests cover old and new bounds.**" Milestone 2 shipped; rectangle
damage did not, and no bd issue tracks it.

Sources (read, not imported):

- `internal/platform/wayland/host.go:39` — `HostCallbacks`; `:48` `Render`
- `internal/platform/wayland/client.go:796` — `renderJob`; `:808` the damage call
- `internal/platform/wayland/tooltip.go:243` — the only other damage call
- `internal/render/schedule.go:44` — `Scheduler.dirty`, one coalesced flag
- `internal/shell/animation.go:27` — `animTick`; `:316` the ticker
- `internal/shell/popout_wallpaper.go:1044` — `applyWallpaperThumb`
- `docs/plans/2026-08-26-sysc-shell-design.md` — L19, L172–196, L353, L372
- `docs/plans/2026-09-11-panel-backdrop-blur-design.md` — the blur cost model
- `noctalia/src/core/frame_rate_limiter.h` — behaviour reference only
- bd `sysc-202`, `sysc-5`

## Observed state

| Property | Today | Evidence |
|---|---|---|
| Damage | Whole buffer, always | Two calls, both `DamageBuffer(0, 0, w, h)` |
| Dirty tracking | One boolean per surface | `Scheduler.dirty` |
| Animation cadence | Fixed 16 ms while unsettled | `animTick = 16 * time.Millisecond` |
| Thumbnail decode | Full tree rebuild + full repaint, per thumbnail | `applyWallpaperThumb` calls `rebuildPanel` then `publishSurface` |
| Blit cost | ~9.7 ms/MiB, linear | Measured 2026-09-11 for the blur design |

The wallpaper picker is 980×1100, so one surface is 4.1 MiB and one repaint is
roughly 40 ms. A library of several hundred wallpapers therefore pays a full
relayout plus a ~40 ms blit **per decoded thumbnail**. That is `sysc-202`'s
image-heavy-grid case, and it is arithmetic over measured constants rather than
a suspicion.

## Goal and scope

In:

- Rectangle damage, with dirty rectangles accumulated from the tree.
- A frame-pacing cap for animation-driven repaints.
- Coalescing the wallpaper picker's per-thumbnail rebuild storm.
- The measurements that retire `sysc-202`.

Out:

- EGL/OpenGL ES. This design is the evidence `wl_shm` suffices, not a step
  toward a second renderer. Architecture L195 stands: "No renderer interface
  will exist while `wl_shm` is the only implementation."
- Blur. Owned by its own design; this one must not regress it.
- Any change to layout, chrome, or tokens.

## Decisions

### D1 — Damage travels back through `HostCallbacks.Render`

`Render` is a struct field, not an interface method:

```go
Render func(pixels []byte, width, height, stride int) error
```

It gains a sibling rather than changing shape:

```go
// Damaged reports the buffer rectangles the last Render touched. An empty
// slice means the whole buffer, which is what every surface reports until it
// tracks its own dirty regions.
Damaged func() []ui.Rect
```

`renderJob` calls `Render`, then asks `Damaged`, then submits one
`DamageBuffer` per rectangle, falling back to the full buffer when the slice is
empty or the callback is nil.

Rejected: changing `Render` to return `([]ui.Rect, error)`. Every implementor
and every test double changes at once, for a capability most surfaces will not
use on day one. A nil sibling callback is the migration.

Rejected: the scheduler computing damage. It "knows nothing about Wayland"
(`schedule.go:33`) and nothing about the tree either; giving it geometry would
break the one property that makes it testable.

### D2 — Dirty rectangles come from the tree, not the scheduler

`Scheduler.dirty` stays exactly as it is: one coalesced boolean answering "is a
redraw owed". It is the right shape for that question and this design does not
touch it.

Dirty *geometry* is separate state, owned by the surface that builds the tree. A
node whose paint-affecting fields change contributes its **old and new bounds** —
both, because a node that moved must repair the region it left. This is
precisely the condition architecture L193 attached to the promise: "after tests
cover old and new bounds."

Where a surface cannot cheaply say what changed, it reports nothing and gets
today's full-buffer damage. Correct-but-slow remains the default; rectangle
damage is an optimisation a surface opts into.

### D3 — Frame pacing caps animation repaints

`animTick` is a fixed 16 ms, so every unsettled value repaints its whole surface
at 60 Hz regardless of how much of it moved, or how expensive that surface is.

The cadence becomes a bounded cap rather than a constant: a repaint is skipped
when too little time has passed since the last one, and a skipped tick re-arms
so the animation still advances and still settles. Noctalia reached the same
design for the same reason — its `FrameRateLimiter` caps at 33 ms because
repainting a large blurred surface at refresh rate "burns GPU/CPU for no visible
gain."

This matters more after blur, not less. A blurred panel's backdrop is cached and
static, but the *composite* still blits the full surface each tick.

The cap is a theme-resolved value, not a second constant table, so the parity
design's motion re-base reaches it. Reduced motion is unaffected: it settles
immediately and schedules nothing.

### D4 — The picker coalesces thumbnail arrivals

`applyWallpaperThumb` currently rebuilds the entire panel tree and republishes
the surface for every single decoded thumbnail.

Two changes, in order of value:

1. **Coalesce.** Thumbnails arrive in bursts from a paced worker. Arrivals
   within a short window collapse into one rebuild and one publish.
2. **Do not rebuild the tree for a raster.** The virtual list's `Item` builder
   already looks the thumbnail up at layout time (`wallpaperThumbFor`), so a
   decoded raster only needs the surface repainted, not the tree rebuilt.

Coalescing alone removes most of the cost and carries almost no risk. The second
is the larger win and the larger change; the plan may stage them.

### D5 — Damage is an optimisation, never a correctness boundary

Any surface that cannot prove what changed damages everything. A missed
rectangle shows as stale pixels, which is the worst class of rendering bug
because it looks like corrupted state rather than a paint fault.

Consequently: the first consumer is **one** surface, chosen for having a large
buffer and small changes, and every other surface keeps full-buffer damage until
its own tests cover its bounds.

### D6 — Architecture amendment

- L193: record that rectangle damage exists, which surfaces use it, and that
  full-buffer damage remains the default and the fallback.
- L372, open gates: "Measure shared-memory rendering before deciding whether to
  add EGL/OpenGL ES" — this design plus the blur design together close it for
  every case `sysc-202` names.
- L19 is unchanged. Pacing *reduces* frame requests; it introduces no loop.

### D7 — Testing

Per-package named tests only. **Do not run `go test ./...` or `-race`** — a
repo-wide race build exhausts memory on this machine.

- A node that moves contributes both its old and its new bounds.
- A surface reporting no rectangles gets one full-buffer damage call.
- Rectangles are in buffer pixels and survive a non-1 `Scale120`.
- Pacing: a skipped tick still re-arms; a settled value schedules nothing;
  reduced motion settles without scheduling.
- Thumbnail arrivals inside one window produce one publish, not N.
- A paced animation still reaches its target value exactly.

### D8 — Tracker

Hangs off `sysc-202`, alongside the blur slice. Closing both retires the epic.
Note `sysc-5` (M2 live gate) carries the 60-minute idle run, which is the
natural place to observe pacing on real hardware.

### D9 — Open risks

1. **Stale pixels are the failure mode**, and they are silent. D5's
   one-surface-at-a-time rule is the containment.
2. Damage rectangles interact with the blur backdrop: a panel with a cached
   backdrop must damage the composited result, not the backdrop's own bounds.
3. The wallpaper rebuild change touches a path that already had a
   "never decode here" hazard documented in `wallpaperThumbFor`. The comment
   should survive the edit.
4. Pacing changes animation timing subtly. A cap slower than a token's duration
   makes a short transition visibly steppy; the cap must stay below the shortest
   token it governs.
