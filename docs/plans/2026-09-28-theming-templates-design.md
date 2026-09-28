# Theming templates design — app-correct output and application model

Commissioned by `2026-09-28-theming-templates-execution-handover.md`.
Work: bd sysc-626 (tokens), sysc-627 (terminal cluster), sysc-628 (non-terminal), sysc-629
(dispositions), sysc-630 (land the uncommitted `Complete()` gate first).
Prior art (port, do not reinvent): Noctalia v5 at `/home/nomadx/noctalia` —
`assets/templates/<app>/<body>` + `apply.sh` (2318 lines, same app list),
token shapes in `src/theme/tokens.h`, derivations in `src/theme/fixed_palette.cpp`.

## The problem the handover understates

A rendered template must *actually theme the app* from the toggle alone. In the current
write-target-clobber model that is only true for a user who has **no** config for that app
(`ApplyWrite` correctly refuses an unmarked user file). For users with an existing config the
toggle silently no-ops — exactly the half-theme GH#7 was filed against. And several of our
writeTargets are sidecars the app never auto-selects (btop `themes/`, helix `themes/`,
kcolorscheme, qt5ct `colors/`, emacs), so even a fresh write needs an activation the shell does
not perform.

Noctalia solved this: **sidecar theme file + one managed directive in the user config**
(`import` for alacritty, `theme =`/`colorscheme` for ghostty/kitty/wezterm,
`color_theme =` for btop, `theme =` for helix, `ColorSchemeName=` in kdeglobals,
`color_scheme_path=` in qt5ct). Port that model.

## D1 — terminal token set (sysc-626)

Terminal block mirrors Noctalia `tokens.h` keys, implemented as derived export values
(`internal/theme/terminal.go`, merged by `Tokens.Export()`) rather than matugen wire roles —
every palette source carries the block with no second table. Exported keys: `.TerminalBackground` `.TerminalForeground` `.TerminalCursor`
`.TerminalCursorText` `.TerminalSelectionForeground` `.TerminalSelectionBackground`,
`.TerminalNormal{Black,Red,Green,Yellow,Blue,Magenta,Cyan,White}` and the same with `Bright`.

Derivation (port of `fixed_palette.cpp:173-184` + ANSI mapping, using existing blend/contrast
helpers in `internal/theme`):

| token | rule |
|---|---|
| TerminalBackground | `surface_container` |
| TerminalForeground | `on_surface` |
| TerminalCursor | TerminalForeground; TerminalCursorText = TerminalBackground |
| TerminalSelection{Background,Foreground} | `surface_variant` / `on_surface_variant` |
| ANSI 0, 7, 8 | Anchors, not clamped: 0 = `surface_variant` (= selection background), 7 = `on_surface` (= foreground), 8 = `outline` |
| ANSI 1-6, 9-14 | Noctalia's role map (red=error, green=primary, yellow=secondary, blue=tertiary, magenta=primary_fixed_dim, cyan=secondary_fixed_dim; bright = same), each passed through `EnsureContrast` against TerminalBackground at ≥4.5:1 (`TerminalContrast` const) |

Contrast floor is a named const with a `ponytail:` comment; it is the calibration knob.

## D2 — application model

New mechanism in `internal/theming/apply.go` (minimal, guarded like `ApplyWrite`):

1. `ApplySidecar(path, content)` — always-safe sidecar write (temp+rename, marker header kept).
2. `EnsureDirective(path, key, value, fmt)` / `RemoveDirective` — append-or-update **one**
   single-line directive in the user's config file; only a line matching the key that sysc-shell
   wrote (or an absent file) is touched; a multi-line/preformatted user value → refuse, log,
   no-op. Record ownership so disable removes only the occurrence the shell inserted; an identical
   pre-existing user line stays. Writes follow a config symlink to preserve the link.
   This replaces clobbering main configs.

the default branch of `applyOnce`/`writeTarget()` in `internal/theming/enabled.go` becomes a table of (sidecar path, directive target+key) per template. The marker
guard, temp+rename swap and `Complete()` gating stay untouched.

## D3 — per-app table (this tranche)

Formats come from the Noctalia body files; the job is token mapping + Go `text/template` syntax.

| tpl | sidecar (write) | directive (managed) | noctalia body |
|---|---|---|---|
| alacritty | `~/.config/alacritty/themes/sysc-shell.toml` | `import` line in `alacritty.toml` | `alacritty.toml` 68L |
| foot | `~/.config/foot/themes/sysc-shell` | `include=` line in `foot.ini` | `foot` 22L |
| ghostty | `~/.config/ghostty/themes/sysc-shell` | `theme =` line in `config` | 22L |
| kitty | `~/.config/kitty/themes/sysc-shell.conf` | `colorscheme` line in `kitty.conf` | 32L |
| wezterm | `~/.config/wezterm/colors/sysc-shell.toml` | `config.color_scheme =` line in `wezterm.lua` | 84L |
| starship | `~/.config/starship.toml` palette table (managed `[palettes]` block between directives) | `palette =` line | 41L |
| btop | `~/.config/btop/themes/sysc-shell.theme` | `color_theme =` line in `btop.conf` | 39L |
| cava | `~/.config/cava/config` directive keys (color section lines) | same file | 15L |
| helix | `~/.config/helix/themes/sysc-shell.toml` | `theme =` line in `config.toml` | 161L |
| kcolorscheme | `~/.local/share/color-schemes/sysc-shell.colors` | `ColorSchemeName=` in `kdeglobals` | 146L |
| qt | `~/.config/qt6ct/colors/sysc-shell.conf` (+qt5ct dir if present) | `color_scheme_path=` in `qt5ct.conf`/`qt6ct.conf` | 8L |
| niri | unchanged (`sysc-shell.kdl` include, real today) | — | done |

Scroll is a separate Wayland compositor and is deferred until it has a separate
approved design consistent with the Niri-first product constraint.

WezTerm supports the managed assignment only when `wezterm.lua` ends in `return config`; the
assignment is inserted immediately before it. Configs returning a table are left untouched.

## D4 — dispositions (sysc-629)

Rule from the handover stands: if an app cannot be themed by the D2/D3 model, delete the template
and its routing; do not ship a half-theme.

- **gtk3/gtk4: DELETE.** A colour-only `gtk.css` is not a named GTK theme; the
  `~/.config/gtk-N.0/gtk.css` overlay would clobber the user's own CSS, and a full named theme is
  a different product (Noctalia ships a 154-line applyer for it — not ported here). Remove the
  templates, `writeTarget` cases and the `applyOnce` gtk branch incl.
  `ApplyGtkThemeName/UnapplyGtkThemeName`; drop `gtk3`/`gtk4` from the settings Templates list (`internal/settings/registry.go`). Revisit if
  a real requirement appears.
- **emacs: DELETE.** Activation is a `(load-theme)` elisp evaluation in the running instance
  (Noctalia uses emacsclient eval); outside the D2 directive model. Remove the template, its `writeTarget` case and its settings Templates entry.
- **qt/kde keep** via the D3 directives: the directive only touches files that exist (absent user
  config → skip, sidecar still written).

## D5 — correctness gates

Per template: (1) golden render test (deterministic token set incl. D1; assert every D1 token and
structural markers, e.g. `color0..color15`, appear); (2) sidecar+directive mechanism test in
`apply_test.go` (fresh file, absent file, unmanaged user value refused); (3) live check: open the
real app with the generated file, note "not verified" when the app is absent.
`completeTemplates` flips per cluster only after its golden tests pass (gate lands first via
sysc-630, ported from the dirty diff in `.worktrees/fix-audit-tranche`).

## Out of scope

Reload/signal orchestration beyond existing `signalKitty`; watcher; export formats
(kvantum/xcursor/wallpaper); cava 24-colour gradients; hyprland/sway/labwc; multi-line managed
`import` array forms; runtime colour preview.
