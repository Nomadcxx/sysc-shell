# Theming Templates Tranche Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make every non-niri template toggle actually theme its app: terminal palette tokens
(`sysc-626`), the sidecar+directive apply mechanism and terminal cluster (`sysc-627`), non-terminal
cluster (`sysc-628`), dispositions (`sysc-629`). Design: `2026-09-28-theming-templates-design.md`
(D1–D5 there; this plan does not restate decisions).

**Architecture:** `internal/theme` gains derived `terminal_*` roles carried by `Tokens.Export()`.
`internal/theming` gains `ApplySidecar`/`EnsureDirective`/`RemoveDirective` beside the existing
`ApplyWrite` guard; `applyOnce`'s default branch becomes a per-template table of
(sidecar path, directive file, key). Every template body is a port of the matching Noctalia file
(`/home/nomadx/noctalia/assets/templates/<app>/`), Go `text/template` syntax, rendered with D1 keys.

**Tech Stack:** Go, `text/template`, `os`/`path/filepath` only — no new dependencies.

**Worktree:** create `.worktrees/theming-templates` from `main`, branch `feature/theming-templates`.
Run bd from the primary checkout. Gates per commit: `gofmt -w . && test -z "$(gofmt -l .)"`,
`go vet ./...`, `go test -race -count=1 ./internal/theming/... ./internal/theme/...`
(package-scoped; full suite at Tasks 9 and 16).

## Task 0 — land the GH#7 gate (sysc-630) — prerequisite, blocks everything

- [ ] Read the dirty diff in `/home/nomadx/sysc-shell/.worktrees/fix-audit-tranche` for
      `internal/theming/catalog.go` (`completeTemplates` + `Complete`),
      `internal/settings/registry.go` (`addTemplateEntries` filter),
      `internal/theming/enabled.go` + `enabled_test.go`. Do not touch that worktree otherwise.
- [ ] Port those hunks onto a fresh checkout of `main`; add `markTemplatesComplete(t, names...)`
      test helper if the diff lacks it (it restores the map entry via `t.Cleanup`).
- [ ] `go test -race -count=1 ./internal/theming/... ./internal/settings/...`; commit
      `theming: gate template toggles on completed rendering`; `bd close sysc-630`.

## Task 1 — terminal token set (sysc-626)

- [ ] Failing table test `internal/theme/terminal_test.go`: for seeded light/dark `Tokens`,
      assert all 22 D1 keys exist in `Export()`, match `#[0-9a-f]{6}`,
      `contrast(TerminalForeground, TerminalBackground) >= 4.5`, each ANSI colour vs
      TerminalBackground `>= 3.0` (`terminalContrastFloor` const).
- [ ] Implement in `internal/theme/theme.go`: `terminal_*` roles + derivation per design D1, porting
      `noctalia/src/theme/fixed_palette.cpp:173-184` and its ANSI role map with existing blend/
      contrast helpers; `bd update sysc-626 --status in_progress`.
- [ ] Green; commit; close sysc-626.

## Task 2 — sidecar + directive mechanism (part of sysc-627)

- [ ] Failing tests `internal/theming/apply_test.go`: `ApplySidecar` creates dirs, writes marker
      header, temp+rename; `EnsureDirective` on absent file (creates with line), clean file
      (appends), own previous line (updates in place, idempotent), user-written key line (refuses,
      file untouched); `RemoveDirective` removes only sysc-shell-written line and an empty created
      file.
- [ ] Implement the three functions in `internal/theming/apply.go`; refactor
      `internal/theming/enabled.go` default branch to a `templateTargets` table
      `{tpl → sidecar(pathFn), directive{file, format, key, value}}` (formats: `key = value`,
      `key=value`, lua `config.color_scheme = "x"`, alacritty `import = ["…"]` single-line,
      `include=path`); keep niri and kitty `signalKitty` behaviour; cava writes its colour keys as
      directives into the existing `~/.config/cava/config`.
- [ ] Green; commit.

## Tasks 3–8 — terminal cluster templates (sysc-627)

Ports: `3 alacritty`, `4 foot`, `5 ghostty`, `6 kitty`, `7 wezterm`, `8 starship`. Per task, same
shape:

- [ ] Render the Noctalia body with D1 keys into `internal/theming/templates/<app>.tpl`; update the
      Task 2 table row if the app needs a directive variant.
- [ ] Failing golden test `Test<App>TemplateRendersCompleteOutput` in
      `internal/theming/catalog_test.go`: deterministic tokens; assert all 16 `colorN`/`palette N`
      (or app equivalent), background/foreground/cursor/selection lines present.
- [ ] Live check if the app is installed (`command -v`): point it at the generated config, screenshot
      or note "not verified"; record in the commit message.
- [ ] Green; commit per app. After Task 8: flip `completeTemplates` for the six, registry test via
      `markTemplatesComplete`; `bd close sysc-627 --reason …`.

## Tasks 9–14 — non-terminal cluster (sysc-628)

Ports: `9 btop`, `10 cava`, `11 helix`, `12 kcolorscheme`, `13 qt (5ct/6ct)`, `14 scroll`
(scroll: confirm sidecar/include form from `noctalia/assets/templates/scroll/apply.sh` first).
Same per-task shape as Tasks 3–8 (golden test names `Test<App>Template…`, table row, live check).
After Task 14: full `go test -race -count=1 ./...`; flip `completeTemplates` for the cluster;
`bd close sysc-628`.

## Task 15 — dispositions (sysc-629)

- [ ] Per design D4: delete `gtk3.tpl`, `gtk4.tpl`, `emacs.tpl`; remove their `writeTarget` cases,
      the `applyOnce` gtk branch, `ApplyGtkThemeName`/`UnapplyGtkThemeName` if now uncalled, the
      `gtk3`/`gtk4`/`emacs` names from the settings Templates list, their `templateTarget` rows and tests; update
      settings docs table.
- [ ] `go vet ./... && go test -race -count=1 ./...`; commit; close sysc-629.

## Task 16 — package gates and completion snapshot

- [ ] `gofmt -l .` empty; `go vet ./...`; `go test -race -count=1 ./...`;
      `git diff --exit-code -- go.mod go.sum`; live Niri run with all complete toggles on:
      `niri msg -j layers` unchanged, no error-log refusals beyond unmanaged-user-file cases.
- [ ] Write `2026-09-28-theming-templates-completion-handover.md` (gate output, live observations,
      per-app verified/not-verified), register row, retire the execution handover per AGENTS
      (its remaining work is all in bd), commit `.beads/issues.jsonl` alongside.
