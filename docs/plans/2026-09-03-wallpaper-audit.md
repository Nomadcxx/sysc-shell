# Milestone 7 Wallpaper — Plan Audit

Date: 2026-09-03. Subject: `docs/plans/2026-09-03-wallpaper.md` against the
shipped `docs/plans/2026-09-03-wallpaper-design.md` and landed `main`.

**Verdict: ready to implement after a 5-line correction pass.** Plan is sound;
shape mirrors the launcher panel and the existing gSlapper on the box. Most
issues are bd state, hygiene, and two pin-points in tests.

## Cross-checked against landed `main`

- `PanelID` enum (`internal/shell/panel.go:9-15`), `parsePanelName`
  (`internal/shell/panelhost.go:110`), `panelTargetSize`
  (`internal/shell/panelhost.go:1201`), `CenterY` branch
  (`internal/shell/panelhost.go:348-349`) — all present, plan slots cleanly
  beside the launcher.
- `theme.Source{Kind, Seed}` (`internal/theme/generate.go`),
  `cfg.ThemeGen.Source` already defaults to `"wallpaper"`
  (`internal/config/config.go:178`). Wiring apply → `cfg.ThemeGen.Seed` →
  `generateTheme` is a 5-line add to the service call site, not a new theme
  code path.
- IPC `knownPanels` (`internal/ipc/server.go:21`), launcher relay shape
  (`internal/shell/popout_launcher.go:60-78`), `KindVirtualList`
  (`internal/ui/tree.go:22`), `KindImage` (`internal/ui/tree.go:23`),
  `ui.NewField` (`internal/ui/textfield.go:17`) — all match the plan's
  stated APIs.
- gSlapper on disk: `-I`, `--no-save-state`, `-r`, `-p`/`-s`,
  `--transition-type`, `--cache-size`, `--help-output`, positional
  `<output> <path>`. Plan's `launchArgs` description matches the binary;
  `-r FPS` (not `--fps-cap`) is the short form. `STATUS: playing image
  <path>` confirmed via `gSlapper/docs-site/.../ipc-control.mdx`. Path can
  contain spaces → tests must use `strings.SplitN(line, " ", 4)`.
- `go build ./...` clean.

## Must-fix before Task 0

1. **bd state mismatch in the plan itself.** Plan Task 0 Step 1 says
   "confirm `sysc-147` is in progress" but `sysc-147` is **closed** (the
   plan-write task). The implementation ticket is `sysc-149` and it is
   **blocked** on `sysc-146` (design), which is still **open**. Fix: close
   `sysc-146` (the design + plan both exist on `main`), then claim
   `sysc-149 --status in_progress`. Two `bd update` lines at the start of
   the worktree, not part of the code commits.
2. **`main` is dirty** with untracked
   `docs/plans/2026-09-03-notification-centre-design.md` plus six modified
   files from the M5A notifications slice. The M7 worktree must branch from
   a clean `main`. Either land the notification-centre work first, or stash
   those changes before creating the M7 worktree (do not commit them into
   M7).
3. **Stale AGENTS.md line.** AGENTS.md says "one output (DP-1,
   3440×1440)" but the design and the live machine have DP-1 + DP-3. The
   design records this and overrides it for M7. AGENTS.md needs the same
   one-line correction in the same M7 slice (docs-only commit).

## Test pin-points the plan under-specifies

4. **Task 3 `ParseStatus`** must split on exactly four tokens
   (`SplitN(" ", 4)`) or a path with a space fails the test. The plan's
   example uses `/wallpapers/space name.png` but doesn't pin the split.
5. **Task 4 `launchArgs`** should pin `-r 30` (the short form), not
   `--fps-cap`, to match what the plan actually emits. Both work; one is
   what the test will compare against.
6. **Task 9 engine stub** is described as "a tiny `Command` stub (inject
   `exec` like a func field)". Spell that out in the failing test: a
   `spawn func(argv []string, dir string) (Process, error)` field on the
   engine, where `Process` is `Wait() error` + `Stop() error`. Otherwise
   the test is hand-wavy and the real impl lands without a falsifiable
   test first.

## Scope guards

7. **No `*` wildcard** is correctly pinned in Task 4. Good.
8. **No `pkill` by binary name** is correctly pinned by the AGENTS.md rule
   + design D17. Plan never names `pkill`. Good.
9. **Fan-out per connector** (D14): plan Task 2 tests pin partial All, but
   the panel chrome "All / DP-1 / DP-3" select should drive
   `Apply("all", path)`, not `Apply("DP-1", path)`. Make sure `Apply` takes
   the output-select token and the service expands it via the current
   connector list. Worth a one-line note in Task 2.
10. **Theme seed write-back**: the service should write
    `cfg.ThemeGen.Source/Seed` and trigger `r.generateTheme(r.cfg)`. The
    plan mentions this in Task 11 but doesn't name the seam. The cleanest
    seam is a `cfgHook func(Source, Seed)` field on `Service` that the
    registry sets to `r.setWallpaperSeed`. Test with a recorder.

## Worktree plan

Branch name: `milestone/wallpaper`. Branch from a clean `main`. `commit-msg`
hook will reject messages with `agent`, `cursor`, `codex`, `llm`, `both`,
`Hallmark` — keep messages terse and concrete. Fix #3 in a separate
docs-only commit on `milestone/wallpaper` before any product code lands.
