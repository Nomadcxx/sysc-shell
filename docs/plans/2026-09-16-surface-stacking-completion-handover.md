# Surface stacking completion handover

Date: 2026-09-16.

Plan: `docs/plans/2026-09-11-surface-stacking.md`.

The surface-stacking slice landed on `main` in merge commit `07c8544`.
`sysc-307` is closed.

## Delivered

- `ui.KindStack` measures the tallest child, gives every child the same padded
  content box, paints children in order, and preserves topmost hit testing.
- Background images use centred aspect-crop mapping with bilinear sampling.
  Icons retain the nearest-neighbour path.
- The control-centre Media card now composes background image, scrim, and
  80-percent foreground content in a real stack.
- Missing album art falls back to the assigned cached wallpaper thumbnail.
  Thumbnail completion rebuilds and invalidates an open Media page.
- Blur is enabled in every preset by default. Explicit blur opt-outs and blur
  radius values survive config writes and preset rebases.

## Proof

The feature worktree passed:

```text
go test -p 2 ./internal/ui -run 'TestStack|TestKind' -v
go test -p 2 ./internal/render -run 'TestStackScrim|TestBackgroundImage|TestPaintImage' -v
go test -p 2 ./internal/shell -run TestMediaCard -v
go test -p 2 ./internal/shell
go vet -p 2 ./internal/ui ./internal/render ./internal/shell ./internal/config ./internal/theme ./internal/icons
go build -p 2 ./...
gofmt scan, git diff --check, and the go.mod/go.sum diff check
```

All commands exited successfully. The tracker export was checked and only the
`sysc-307` record changed.

## Bare-metal Niri observation

The one-output bare-metal gate used `DP-1` at 3440x1440, scale 1.0. The test
process opened the Media control-centre section through IPC. `sysc-shell-panel`
and its shield mapped on `DP-1`, and the captured surface is at
`/tmp/sysc-shell-surface-stacking.png`. Closing the section returned the layer
list to its baseline with no panel surface left behind.

The laptop gate was omitted because the owner moved back to bare metal. This
machine has no second output, so the two-output case remains unqualified.
