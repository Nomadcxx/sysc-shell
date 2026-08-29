# Built-in Widget Foundation Audit Report

Date: 2026-08-30
Auditor: independent audit agent (standard technical audit + Hallmark design audit)
Commissioned by: `docs/plans/2026-08-30-built-in-widget-foundation-audit-handover.md`

## Scope and state

- Branch: `milestone/widget-foundation`
- Worktree: `/home/nomadx/.config/superpowers/worktrees/sysc-shell/milestone/widget-foundation`
- Commits audited: `391787b` (design) and `0a9587d` (plan), read against the charter
  `2026-08-30-built-in-widget-foundation-execution-handover.md`. Branch head at audit time: `c1e3b4e`
  (the audit commission itself).
- Milestone 2 contracts were read from worktree
  `/home/nomadx/.config/superpowers/worktrees/sysc-shell/milestone/stable-bar` at `57b49f0`.
- No document under audit was edited. The audit worktree is unchanged. Live-test artifacts (two
  temporary terminal windows, scratch files) were removed; `niri msg -j windows` confirms `[]`.

## Headline: the design's most consequential unverified claim is now verified live

The design session had no windows open, so assumption 4 — that Niri emits
`WorkspaceActiveWindowChanged` for a focus move *within* one workspace — was unverified. This audit
opened a live event stream on the running compositor (socket
`/run/user/1000/niri.wayland-1.3830.sock`, niri `26.04 (8ed0da4)`), spawned two `foot` windows on
workspace 3, and moved focus between them with `niri msg action focus-window`.

Result: every in-workspace focus move emitted
`WorkspaceActiveWindowChanged{"workspace_id":3,"active_window_id":N}` (ids 21→22→21, exactly one
event per move), each followed by `WindowFocusChanged{"id":N}`. The design's join — consume
`WorkspaceActiveWindowChanged`, ignore `WindowFocusChanged` — is the correct one, and live matrix
item 4's contingency (consuming `WindowFocusChanged` as a second trigger) is unnecessary on this
machine. Assumption 4 moves from **not verified** to **verified**.

## Audit 1 — standard technical audit

### External facts re-derived

Every factual claim about something outside the two documents was re-derived. Results:

| Claim | How checked | Result |
|---|---|---|
| Event set incl. `WorkspaceActiveWindowChanged`, `WindowOpenedOrChanged`, `WindowClosed`, `WindowFocusChanged`, `WindowLayoutsChanged`, `WindowFocusTimestampChanged`, urgency/overview/keyboard/cast/config events | `strings` on `/usr/bin/niri` (each name occurs 3–4×) plus a live event-stream connect | **Held** |
| `Workspace` wire fields `{id, idx, name, output, is_urgent, is_active, is_focused, active_window_id}`; `active_window_id` nullable | Live capture of the initial burst, e.g. `{"id":3,...,"active_window_id":null}` | **Held**, exact |
| `Window`'s ten fields `{id, title, app_id, pid, workspace_id, is_focused, is_floating, is_urgent, layout, focus_timestamp}` | Live events from the two spawned windows, e.g. `"focus_timestamp":{"secs":127218,"nanos":28542737}` | **Held**, exact |
| Reply envelope `{"Ok":"Handled"}`; initial complete snapshot (workspaces, windows, keyboard, overview, config, casts) | Live connect | **Held** |
| Noctalia: `BarInstance` `std::uint32_t outputName`; `ClockWidget(wl_output*, Options)`; `m_lastPrimaryText` compare-before-setText; `ActiveWindow` `maxWidth` default 260 with 145 in the live configuration; Niri backend ignores `active_window_id` and consumes `WindowFocusChanged`; `focusedWindowId()` takes no output; `if (next == m_workspaces) return false;` | grep of `/home/nomadx/noctalia`: `bar_instance.h:36`, `clock_widget.cpp:29/200`, `active_window_widget.h:36`, `niri_workspace_backend.cpp:314/362/384`; `~/.config/noctalia/settings.json:152,164` (`"maxWidth": 145`) | **Held**, all |
| `followFocusedScreen` exposed as a per-widget option | Found in live `~/.config/noctalia/settings.json:96` (`"followFocusedScreen": false`); not located by grep in the C++ source snapshot | Held with caveat — evidence is the live configuration, not the cited source tree |
| DMS at `892b8ae`: empty-title zero-width; `Math.min(implicitWidth, …250)` + `ElideRight`; `maxNormalWidth`; `isNiri`/`isHyprland` branch inside one widget; NiriService consumes `WorkspaceActiveWindowChanged` and never `WindowFocusChanged`; `Clock.qml` declares `precision: SystemClock.Seconds` while rendering `HH:mm`; modernClock's conditional precision; DisplayService/WallpaperCyclingService declare `Minutes` | git head = `892b8ae` ✓; `FocusedApp.qml:53/54/72/90/179/219`; `Services/NiriService.qml:244`; `Clock.qml:194/227`; `ModernClock.qml:100` (verbatim quote); `DisplayService.qml:408`, `WallpaperCyclingService.qml:363` | **Held** at the cited commit. Caveat: the *installed* `/usr/share/quickshell/dms/Services/*.qml` copies contain no `SystemClock` at all (packaging drift vs the repo); the design cites the repo commit, so the claim stands |
| dankPomodoroTimer `Timer { interval: 1000 }` and `Timer { interval: 100 }` | `~/.config/DankMaterialShell/plugins/.repos/0026f1eba8dedaec/DankPomodoroTimer/DankPomodoroWidget.qml:57,150` | **Held** |
| `time.Format` never returns an error | Signature is `func (Time) string`; empirically `"HH:MM"` renders literally forever | **Held** |
| `time.Truncate` operates on absolute time, so a 24 h truncation hits UTC midnight, not local midnight | Ran a probe: UTC+5 zone, 17:30 local → `Truncate(24*time.Hour)` = 05:00 local | **Held** — the design's "cap the boundary at one minute" rationale is correct |
| Go timers run on a suspend-excluding monotonic clock | Go runtime fact (CLOCK_MONOTONIC on Linux); not suspend-tested in this session; the design labels it reasoning and lists it in the live matrix | Held as inference; live matrix item 10 settles it |
| Every Milestone 2 signature the plan names or modifies | Read `milestone/stable-bar@57b49f0`: `state.apply(line []byte) (bool, error)`; `state.activate(id, focused)`; `client.go:108 send(snapshots, s.snapshot())`; `MeasureText` type; `measureNode`'s `KindText` body; `Layout(root, Rect, MeasureText)`; `ui.ScaleUnit`; `render.ProofStyle` (including the pre-existing `AccentOn: theme.Error`); `theme.Valid()`; `ThemeFrom(cfg, cfg.Bar)`; `NewWithTheme(theme, policy)` two-arity; `New()` zero-arity; `Configure(logicalWidth, logicalHeight, scale120)`; `Render`; `Handle`; `wayland.HostCallbacks` with `validate`; `preparedOwnerConfig{cfg, hosts, commit}`; `owner.prepareConfig` with its nil-Commit check and the two error paths after `cb.PrepareConfig` (omitted host, `app.validate`); `config.ForConnector`; `pathErr`; `Parse`; `Default`; `config.Theme.Accent string`; `startFakeNiri` variadic; `Stream(ctx, path)`; `cmd/sysc-shell/main.go`'s `run(ctx)` naming `NIRI_SOCKET` in its error and the niri pump Task 13 extends; `hostSet` already keyed `map[uint32]*OutputHost` | **Held**, all |
| The lossy invalidation the design discloses | `registry.go:32` `make(chan wayland.Invalidation, 8)` with a `default:` drop at `:145`, consumed via `main.go:83` → `client.go:869` wake bridge | **Held** — the design describes the current state accurately |

### Findings

Severity key: **verified** = reproduced or confirmed against code; **suspected** = reasoned, not
executed.

| # | Sev | Where | Defect | Consequence | Fix |
|---|---|---|---|---|---|
| 1 | Major (verified) | Plan Task 8 Step 3 | Deletes `projectWorkspaces` from `registry.go`; its caller `Registry.UpdateNiri` (M2 `registry.go:126`) is not rewritten until Task 11 | The tree is red after Task 8's commit; Task 8's own Step 4 (`go test ./internal/shell/`) cannot compile — the plan fails its own checkpoint | Fold the `UpdateNiri` rewrite into Task 8, or merge Tasks 8–11 into one commit boundary with an explicit note |
| 2 | Major (verified) | Plan Task 10 Step 3 | Deletes `Proof.SetWorkspace`, which the current `Registry.UpdateNiri` calls (M2 `registry.go:139`), again not until Task 11 | Same red-tree window, Tasks 10→11; Task 10's Step 4 cannot compile | Same consolidation; alternatively Task 10 keeps a thin `SetWorkspace` shim until Task 11 |
| 3 | Minor (verified) | Plan Task 10 | Presents `sections()`/`widgets()` as new accessors; `func (p *Proof) sections()` already exists (`proof.go:132`, becoming `b *Bar` after Task 9) | Literal execution is a duplicate method declaration | Say "replace the existing `sections()`" |
| 4 | Minor (verified) | Plan Task 3 Step 3 | "There are five" `return true, nil` sites, then lists six blocks | Cosmetic; the code instruction ("replace every") is unambiguous | Correct the count to six |
| 5 | Minor (verified) | Design evidence table vs plan Task 14 | The design claims "long multi-script title with MaxWidth set"; the planned fixture is `strings.Repeat("Fixture Title Segment ", 40)` — pure ASCII | Per-rune fallback during truncation gets no new test even though titles are the first unbounded user text; the design oversells its own evidence row | Extend the fixture with one non-Latin run beyond the cap, or downscope the design row to match the plan |
| 6 | Minor (verified) | Charter requirement vs plan | "focus, title, workspace, close, and move events invalidate only affected output bars": **move** has no invalidation-level test — only decoder-level replace-by-id (Task 1). A window moving workspaces (or a workspace moving outputs) exercises `WindowOpenedOrChanged` with a changed `workspace_id`, and nothing asserts the *changed globals* for that case | The one hole in the twelve-behavior coverage, exactly where the handover predicted | One table row: feed a moved window through `UpdateNiri` and assert the returned globals |
| 7 | Minor (suspected) | Plan Task 11 `UpdateNiri` | Merges `next` into `r.outputs` and never clears a connector absent from the projection | A reconnecting host for a previously seen connector can briefly render a stale workspace/title until the next snapshot; stale keys accumulate | Replace wholesale (`r.outputs = next`) unless a merge semantic is actually needed — none was found |

### Concurrency — verified, no defect

- Total lock order registry→bar holds in `NewHost`, `UpdateClock`, `UpdateNiri`, and `Commit`.
  `Bar.Render` takes only `Bar.mu`, and the Wayland goroutine never reaches the registry lock, so no
  cycle can close.
- `Clock`: `Release` waits on `done` outside the lock; `rearm` is capacity-1 and a stale signal is
  harmless; the run loop recomputes `finestLocked()` each iteration, so releasing a shorter lease
  coarsens on the next tick; `send()` is correct for a single producer; `Clock.Close` and
  `Lease.Release` are idempotent and nil-safe.
- Robustness note, not a defect: `Acquire` after `Close` would restart the service. Unreachable
  through the Registry, but a `closed` flag would be defensive.

### Task 7/8 parked state

The deliberate `PrepareConfig` stub between Tasks 11 and 12 returns a named error and compiles —
safe. The genuinely unsafe seams are findings 1–2, not the parking.

### Failure-policy coherence

The unknown-id split is principled, not a rationalisation. `WindowClosed` for an unknown id: the
desired post-state already holds, so nothing can diverge — erroring would stop the shell over a
no-op. `WorkspaceActiveWindowChanged` for an unknown workspace: state with nowhere to be recorded
and no resync path in this tranche, matching the existing `WorkspaceActivated` strictness. Given
stream ordering (`WorkspacesChanged` always precedes), neither choice can stop the shell on a
benign compositor event.

### Coverage against the charter's twelve required behaviors

All twelve map to named tests except **move** (finding 6). In particular, "an accepted reload does
not restart a service still in use" is covered including the boundary-change case
(`TestAnAcceptedReloadDoesNotRestartAServiceStillInUse` asserts `Starts()==1`, backed by Task 4's
rearm test).

### Stop conditions

Checked individually: none applies. No second Wayland goroutine; widget instances keyed by global;
one Niri connection and one clock timer for the process; no widget schema, renderer interface,
plugin protocol, or injection container; no `sysc-metrics` import or replacement; no SVG decoder or
asset pipeline; `Rollback` preserves the last accepted widget and service set after a failed
reload; every new goroutine provably stops under cancellation; no new dependency.

### Verdict

**Proceed with named corrections.** Findings 1–2 must be fixed in the plan text before execution
(they are reorder/merge edits, not design changes). Findings 3–7 are cheap plan edits. Nothing here
touches a design decision; D1–D8 survive scrutiny, and the prior-art rationale for D3 held up under
source inspection.

## Audit 2 — Hallmark design audit

Run per `references/verbs/audit.md` against the visual and interaction decisions the two documents
fix. The design surface is a Wayland bar rendered into `wl_shm` buffers, not a web page.

**Non-applicable categories, reported once:** hero sections, calls to action, feature grids,
card-in-card, navs, footers, bento and other page macrostructures, aurora or blob backgrounds,
floating orbs, scroll animation, autoplay, LCP, Lottie, and Three.js do not exist on a 44-pixel
bar. There is no `design.md` and no Hallmark stamp in these files, so the stamp-versus-page and
design-system-drift checks do not apply. The tranche is read-only, so there are no hover, press,
or focus states to grade, and none were invented as findings.

**Verified surface:** palette (`internal/shell/theme.go` `DefaultTheme`: background `#101418`,
foreground `#e8ecf0`, accent `#0080ff`, muted `#303438`, error `#ff4040`); geometry (nominal 48,
gap 4, body 40, exclusive zone 44, radius 12, padding 8, spacing 6 — charter-fixed); one family at
size 14 with per-rune system fallback; defaults `workspace` + `window-title` (max-width 260) left,
`clock "15:04"` centre, `clock "Mon 2 Jan"` right; `"-"` fallback; zero-width empty title;
cluster-aware `…` ellipsis truncation (`internal/render/truncate.go`). Measured contrast: fg/bg
15.6:1, accent/bg 4.87:1, error/bg 5.34:1, muted/bg 1.47:1.

### Punch list

| Sev | Tell | Where | Fix |
|---|---|---|---|
| Major | Figure rendering unaddressed | Design D6 and the clock node in `buildWidgets`; nothing requests tabular figures | A proportional face makes the centre clock's width jitter as digits change (`15:04` → `15:19`). Shape clock runs with `tnum` (go-text/typesetting supports OpenType features), or reserve the maximum width the layout string can need |
| Minor | Token discipline | Design D5/D8; `defaultTitleMaxWidth = 260` lives in `internal/config` | 260 is a pixel constant with design weight, and the project rule is that no component carries an independent pixel constant. A config default is defensible because per-instance options need it, but lift the *default value* into the theme token set beside the other geometry |
| Minor | Palette provenance | `theme.go` default accent `#0080ff` | The canonical default-attractor blue. The code comment records it as an owner-supplied baseline, which makes it a decision rather than an accident — but no Tranche 3A text widget ever paints in accent or error, so the question reopens at 3B. Contrast holds at size 14 for every token that could carry text |
| Minor | Muted token constraint | `muted #303438` at 1.47:1 against the background | Acceptable as a decorative track fill; would fail as text colour at size 14. No consumer in this tranche — record the constraint where the token lives |

**Graded, no finding:** hierarchy (one colour, one size across widgets) is correct restraint for a
read-only 44 px bar, not an absence of design; the two clock instances in different sections read
as intentional because their formats differ in grain (`15:04` vs `Mon 2 Jan`), not as a workaround;
`"-"` is a fine stable fallback; the `…` ellipsis is the right truncation affordance given the
tranche ships no tooltip or marquee; the default composition (workspace + title left, time centre,
date right) is balanced and conventional for the genre.

**Count: 0 critical · 1 major · 3 minor.**

**Blocked by the charter's fixed geometry:** the inherited palette itself, the 48/44/40/12/8/6
geometry, and the single-14px typography are owner-supplied baselines fixed by the charter. The
major figure-rendering finding and the muted-token caveat can both be addressed inside Tranche 3A;
palette replacement or any hierarchy beyond one size would require reopening the owner's theme
decision and cannot be acted on inside this tranche.

## Claims that could not be verified, and what would settle them

| Claim | Settling evidence |
|---|---|
| `SystemClock.precision` exposes `Hours` as well as `Seconds`/`Minutes` | Quickshell source or docs; only `Seconds`/`Minutes` uses were located. Trivial — it supports a supporting sentence |
| Timer lateness across suspend/resume | One suspend/resume cycle with a minute-boundary clock running (live matrix item 10) |
| `Theme.Valid()` rejects Height 4 / Gap 4 specifically (Task 12's broken-candidate test) | Running Task 12; plausible from the body-height arithmetic but not executed here |
| `followFocusedScreen` defined in Noctalia source (not just its live configuration) | A wider grep of the Noctalia settings-schema code; the live config proves the option exists |

## Decisions judged wrong

None of D1–D8. Each was engaged with, and each survived. The two post-approval corrections
(`WindowClosed` no-op; `[]uint32` change returns) are sound.

## Live Niri and hardware

- Assumption 4 was tested live and **held** (see headline). Reproduction: connect to
  `$NIRI_SOCKET` with `"EventStream"`, spawn two windows on one workspace, and alternate
  `niri msg action focus-window --id <a|b>`.
- Everything else live (two-output rendering, reconnect, reload, socket loss, suspend) remains
  gated on the live matrix per the plan; no live claim is made from the automated checks.

## Recommended action for the owner

1. Apply findings 1–2 to the plan (task-boundary reorder/merge).
2. Apply findings 3–7 as plan-text edits.
3. Record assumption 4 as verified in the design's assumptions list.
4. Consider the Hallmark major (tabular figures) before Task 10 fixes the clock node shape.
5. Then execution may start; Task 0's gates remain as written.
