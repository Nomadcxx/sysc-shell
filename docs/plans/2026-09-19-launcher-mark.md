> **For implementation:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

# Launcher mark implementation plan

Date: 2026-09-19. Design: `2026-09-16-bar-weather-launcher-workspaces-design.md`
(D2). Issue: `sysc-82`. Parent commission: `sysc-309`.

## Goal

Replace the bar launcher's `ghost` glyph with the supplied nested-gates PNG,
embedded as a project-owned render asset and decoded through the existing
bounded PNG path into an immutable `ui.Image` at the bar mark size.

## Scope

This plan executes D2 only. D1 (weather row) and D3 (workspace shapes) remain
unplanned and get their own plan when they are executed. No launcher search
behaviour changes; launcher ownership stays `sysc-82`.

## Invariants

- Source bytes are copied, never regenerated: SHA-256
  `02f3a6246c193b06701e8d99d7cfbcb5b57136db943d2a67aca8740891827d3f`,
  1024×1024 8-bit RGBA, alpha bounds `768×768+128+128`.
- The transparent margin is part of the asset contract; the visible mark is
  smaller than its box.
- The launcher node keeps `panel:launcher`, name `Open launcher`, button role,
  far-left placement, and existing hit testing.
- Decode happens off `Registry.mu`; the node carries immutable image data.
- No `ghost`, no SYSC wordmark, no generated vector, no screenshot substitute.

## Task 1: Embed the supplied PNG as a render asset

Files: `internal/render/icons/launcher/sysc-aperture.png` (new, byte copy),
`internal/render/icons/launcher/SOURCE.md` (new), `internal/render/launcher.go`
(new), `internal/render/launcher_test.go` (new).

Failing check first: `TestLauncherMarkAsset` in `internal/render/launcher_test.go`
asserts the embedded bytes hash to the source SHA-256, decode to 1024×1024
RGBA, have non-transparent bounds `image.Rect(128, 128, 896, 896)`, and contain
at least one pixel with alpha > 0.

Implementation: copy the source file byte-for-byte; embed it with
`//go:embed icons/launcher/sysc-aperture.png`; expose `LauncherPNG() []byte`.
`SOURCE.md` records the source path, hash, dimensions, alpha bounds, and the
copy step, mirroring `icons/wordmark/SOURCE.md`.

Commit boundary: `feat(render): embed supplied launcher mark asset`.

## Task 2: Build the launcher as an image node

Files: `internal/shell/widget.go`, `internal/shell/widget_test.go`.

Failing check first: rename `TestLauncherWidgetUsesGhostAndOpensLauncher` to
`TestLauncherWidgetUsesSuppliedMarkAndOpensLauncher` and assert the launcher
node is `ui.KindImage` with a non-nil `Image`, `ImageSize == launcherMarkHeight`,
`Action == panelLauncherAction`, `Name == "Open launcher"`, `Role == "button"`.

Implementation: decode once at package level with
`icons.DecodeRaster(render.LauncherPNG(), launcherMarkHeight, launcherMarkHeight)`
so the work runs at init, off `Registry.mu`; replace the ghost text node in
`buildWidgetsWithClockFloor` with the image node. The image box owns its centre
at native and fractional scale.

Commit boundary: `feat(shell): paint the supplied launcher mark`.

## Task 3: Affected-package gate

Run from the worktree:

```
timeout 240s env GOMAXPROCS=2 go test -p 2 -count=1 ./internal/render ./internal/shell ./internal/ui ./internal/config
go build ./cmd/sysc-shell
gofmt -l internal/render/launcher.go internal/render/launcher_test.go internal/shell/widget.go internal/shell/widget_test.go
go vet -p 2 ./...
git diff --check
git diff --exit-code -- go.mod go.sum
```

The repository-wide `go test ./...` and race forms are machine-blocked on this
workstation; record that limitation rather than claiming the blocked gate.

## Task 4: Live DP-1 gate

```
export NIRI_SOCKET=$(ls /run/user/1000/niri.wayland-*.sock | head -1)
export WAYLAND_DISPLAY=wayland-1
export XDG_RUNTIME_DIR=/run/user/1000
```

Run the built shell, assert `niri msg -j layers` shows the bar on DP-1, capture
the bar at 3440×1440 scale 1.0, and confirm the nested-gates mark is visible at
the far left and the launcher panel still opens from it. Kill by pid from
`pgrep -f 'scratchpad/<name>'`. No second output exists on this machine, so no
multi-output claim belongs to this ticket.

## Task 5: Land and close

Merge the worktree branch to `main`, close `sysc-82` citing the asset hash, the
focused checks, and the live result, and commit `.beads/issues.jsonl` in the
same commit as the tracker update.
