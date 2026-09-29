# Theming templates — completion handover (2026-09-28)

Snapshot of the tranche that replaced the stub `.tpl` files with app-correct
templates on `feature/theming-templates` (seventeen commits over `origin/main`
`1289d1a`). bd: `sysc-630`, `sysc-626`, `sysc-627`, `sysc-628`, `sysc-629` all
closed; no open work came out of this tranche.

## What shipped

- **Terminal token block** (`internal/theme/terminal.go`): 22 derived keys —
  background/foreground/cursor/cursor_text/selection pair plus ANSI-16 —
  ported from Noctalia `fixed_palette.cpp`, hues clamped to 4.5:1 against the
  terminal background; merged into `Tokens.Export()`.
- **Apply mechanism** (`internal/theming/apply.go`, `enabled.go`): sidecar
  file + one managed directive line per app (`EnsureDirective`/
  `RemoveDirective`, `create=false` for files the app owns), managed blocks
  (`ManageBlock`/`RemoveBlock`) for starship and btop.conf style targets,
  atomic temp+rename writes, marker guard on every owned file.
  `templateTarget.sidecar` is a list so qt writes the qt5ct and qt6ct colour
  files in one pass.
- **Thirteen complete templates** (catalog is 13 names): niri, alacritty,
  foot, ghostty, kitty, wezterm, starship, btop, cava, helix, kcolorscheme,
  qt, scroll. Each has a golden render test and an apply/idempotence/restore
  mechanism test.
- **Deletions** (disposition D4): gtk3/gtk4/emacs templates, their write and
  apply branches, the `gtk-theme-name` helpers, and their settings entries.
  A colour-only drop-in is not a finished theme.
- **Gate**: `theming.Complete()` still guards every toggle; a stub cannot
  reach a user's config.

## Live verification

| app | result |
|-----|--------|
| alacritty | ran on Niri with sidecar + `[general] import`; clean exit |
| foot | ran; theme applied |
| ghostty | `ghostty +validate-config` clean; ran |
| kitty | ran with `include themes/sysc-shell.conf` |
| btop | ran under a pty (the `script -qec` route fails to size the terminal) |
| cava | 1.0.0 ran to timeout with the sidecar theme |
| wezterm, starship, helix, kcolorscheme, qt, scroll | binaries absent on this machine — test-only |
| niri | shipped earlier; include-file model unchanged |

A concurrent scratchpad boot of the branch binary is refused by the
single-instance guard while the owner's shell runs
(`cmd/sysc-shell/main.go:291`), so no panel smoke was taken from this
branch; `niri msg -j layers` confirms the deployed shell's surfaces are
healthy (`sysc-shell:bar`, `sysc-shell-toast`, wallpaper layers).

## Gate output

- `gofmt -l .` — empty.
- `go vet ./...` — clean.
- `go test -race -count=1 ./...` — `internal/shell` and `tests/integration`
  fail identically on the `origin/main` baseline (four battery/network shell
  failures, tray integration suite, load-sensitive reveal/race noise in
  `gate4b_test.go`); every other package passes, including
  `internal/theming`, `internal/settings`, `internal/config`, `internal/theme`.
- `git diff --exit-code -- go.mod go.sum` — clean; no new dependencies.

## Unresolved / for whoever merges

- The six not-installed apps are verified only by their golden and mechanism
  tests; a human with those binaries should toggle them once before believing
  the wiring claims.
- KDE colour-scheme activation is deliberately a single managed
  `ColorSchemeName` line in `kdeglobals`; Noctalia's C++ service merges the
  whole document and signals D-Bus. If the desktop ignores the sidecar on
  theme change, the missing piece is that D-Bus signal, not the file.
- Deploy only through `scripts/deploy` from a merged `main`, never by hand.
