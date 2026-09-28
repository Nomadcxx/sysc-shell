# Theming Templates Tranche Implementation Plan

## Goal

Replace the stub application templates in `internal/theming/templates/` with output each application can load. The work covers terminal palette tokens (`sysc-626`), the sidecar and managed-directive mechanism plus terminal templates (`sysc-627`), non-terminal templates (`sysc-628`), and template dispositions (`sysc-629`). Design decisions D1–D5 live in [the design](2026-09-28-theming-templates-design.md). Beads holds issue status.

## Rendering gate

The GH #7 completeness gate (`sysc-630`) must protect users before template writes begin. `completeTemplates` and `Complete` gate both the settings toggles and the apply path. Mechanism tests use `markTemplatesComplete` to exercise templates before their live verification finishes.

## Terminal palette

`internal/theme` exports 22 derived terminal roles through `Tokens.Export()`: background and foreground, cursor and selection pairs, and ANSI-16 colours. Derive the roles from the palette helpers and the D1 mapping from Noctalia's `fixed_palette.cpp`. Table tests cover both light and dark tokens, valid hex output, foreground contrast of at least 4.5:1, and ANSI contrast of at least 3:1 against the terminal background. Keep Niri's existing rendering unchanged.

## Apply mechanism

`internal/theming` owns guarded writes for generated sidecars, managed directive lines in user configuration, and managed blocks for formats such as Starship. Track directive ownership so disabling removes only a line inserted by the shell; preserve an identical pre-existing user line. Write config symlink targets without detaching their links. Use temporary files and rename for atomic replacement. Refuse unknown or modified user content unless the caller explicitly confirms an overwrite; retain the original config in `.bak` before confirmed replacement. Preserve Niri handling and Kitty's reload signal. Keep application wire formats inside each target's directive configuration rather than adding a general config parser. For WezTerm, insert before a final `return config` and refuse other return shapes.

The target table maps each template to its sidecar paths and directives. Alacritty uses a single-line import, Foot an include in `[main]`, Ghostty a theme key, Kitty an include, and Cava a theme key in `[color]`. Qt writes both qt5ct and qt6ct sidecars and their `General` paths. Starship places its palette and rendered block in `starship.toml`.

## Terminal templates

Port the Alacritty, Foot, Ghostty, Kitty, WezTerm, and Starship bodies from the matching Noctalia templates into Go `text/template` syntax using D1 token names. Each template needs a deterministic render test for its application-specific colour keys and an apply test for its sidecar or managed block. Run an installed application against its generated config and record unavailable binaries as unverified.

## Non-terminal templates

Port Btop, Cava, Helix, KDE colour scheme, and Qt 5/6. Add a render test and an apply test for directive placement, user-config preservation, idempotence, and disable behavior. Verify installed applications live; record unavailable binaries as unverified. Defer Scroll because it is a separate compositor and requires its own approved design.

## Template dispositions

Remove GTK 3, GTK 4, and Emacs templates and their settings entries. A colour-only GTK stylesheet leaves the icon, font, and pointer settings disconnected; the Emacs stub lacks load-path wiring. Keep Qt as a named colour-scheme sidecar for both qt5ct and qt6ct.

## Completion evidence

Run `gofmt`, `go vet ./...`, the race-enabled Go test suite, and `git diff --exit-code -- go.mod go.sum`. Exercise the completed toggles on Niri and check `niri msg -j layers` for unexpected surfaces. Record command output, live observations, and per-application verification limits in the completion handover. Deploy only through `scripts/deploy` after merge.
