# Running-apps pill implementation audit handover

Date: 2026-09-06
Kind: audit-handover
Commission: `sysc-189`
Report to write: `/home/nomadx/sysc-shell/docs/plans/2026-09-06-running-apps-pill-audit-report.md`

This is a snapshot of one implementation session (2026-09-05) plus the
uncommitted follow-up still in the working tree (2026-09-06). It commissions an
independent audit of that work against the approved design, the prior-art note,
the plan's live gate, and the owner's live UI feedback. Do not patch this file
later; put corrections in the report and in bd.

State at writing lives in bd, not in a status header. Query `bd show sysc-175`,
`sysc-181`, `sysc-182`, `sysc-189` from `/home/nomadx/sysc-shell` with
`BEADS_DB=/home/nomadx/sysc-shell/.beads/beads.db`.

---

## Assignment

Audit the running-apps bar pill as it exists on `main` at `07c1916`, plus the
**uncommitted** `sysc-181` working-tree diff on the same checkout. Produce one
report at the path above. Register that report in `docs/plans/README.md` in the
same commit as the report.

Classify findings as **Blocking**, **Significant**, or **Minor**. End with one
verdict:

- **Proceed** — ship after ordinary review comments.
- **Proceed after corrections** — named code, test, beads, or document fixes
  close the findings.
- **Redesign required** — a product, ownership, or platform assumption fails.

You may edit design/plan documents only to correct a verified factual error
(wrong type, wrong wire shape, a decision that the owner already reversed in
session). You may write Go only if a Blocking finding has a one-line or
single-function fix and a named test already exists or is the smallest thing
that fails. Prefer recording the fix in the report and leaving implementation
to a later claim of `sysc-189` or a discovered-from issue.

Do not start a dock, pins, SVG rasterizer, launcher `Service` wiring, or any
Milestone 7 cluster that is not this pill.

---

## Repository and workspace

| Item | Value |
|---|---|
| Primary checkout | `/home/nomadx/sysc-shell` |
| Branch | `main` at `07c1916`, tracking `origin/main` (not ahead) |
| Pill tip that landed | `bf87d4d` (`fix(shell): hang running-app menus under the icon`) is an ancestor of `main` |
| Feature worktree | `/home/nomadx/sysc-shell/.worktrees/feature/running-apps-pill` still checked out at `bf87d4d` |
| Feature branch | `feature/running-apps-pill` |
| Uncommitted pill work | `sysc-181` (Physical size + CatmullRom + Configure reproject). **Not in HEAD.** |
| Transcript | `/home/nomadx/.cursor/projects/home-nomadx-sysc-shell/agent-transcripts/7dee7b19-e431-4bae-a9d9-23cc438ecc78/7dee7b19-e431-4bae-a9d9-23cc438ecc78.jsonl` |

Run `bd` only from the primary checkout. A worktree `bd` creates a second
SQLite file and lies.

Unrelated dirty files on this checkout have come and gone across the day
(keyrepeat, launcher rank, wallpaper `adoptBar`). At handover time the
uncommitted set that belongs to this pill is:

```
internal/icons/worker.go
internal/shell/registry.go
internal/shell/runningapps.go
internal/shell/runningapps_click_test.go
internal/shell/runningapps_widget_test.go
internal/shell/tray.go
```

`internal/shell/registry.go` also contains later-`main` structure (`adoptBar`,
wallpaper panel action). The 181 stitch is `attachRunningIconsAtLocked` inside
`adoptBar`, `applyRunningIconsLocked` in `UpdateNiri`, and the `Configure`
wrapper in `bindHost`. Audit that merge; do not revert `adoptBar`.

Do not commit unless the owner asks. Do not mix `.cursor/` into a pill commit.

---

## Documents to read first

In order:

1. Prior art: `docs/plans/2026-09-05-running-apps-pill-prior-art.md`
2. Design D1–D15: `docs/plans/2026-09-05-running-apps-pill-design.md`
   (D8/D11 and D12 were amended in session; the file on disk is the amended
   text.)
3. Plan: `docs/plans/2026-09-05-running-apps-pill.md`
4. Pre-implementation plan audit:
   `docs/plans/2026-09-05-running-apps-pill-audit-report.md`
   Findings 2 and 5 were superseded by the owner (no `sysc-launch` at runtime).
   Leave that report as written.
5. This handover.
6. Current source listed under [Code map](#code-map).

Compare product intent with DMS RunningApps compact chrome and Noctalia
Taskbar/Dock *interaction*, not Dock *chrome*. The pill is not a dock.

---

## What the session was

Owner request (2026-09-05 03:48): a dynamic bar pill of icons for launched UI
apps (Steam, Firefox, Brave, …), with right-click, after a detailed DMS then
Noctalia prior-art pass.

The session then: researched prior art, wrote design and plan, absorbed a plan
audit, amended ownership so the bar works with the launcher unused, implemented
the plan TDD, deployed live, crashed, recovered, took a punch list of live UI
defects into beads, fixed most of them, fast-forwarded local `main` (later
merged through the theme tranche and pushed), then implemented `sysc-181` in
the working tree and closed beads in a way that **did not survive** a later
beads rewrite. Details below.

---

## Owner product decisions (fixed unless they contradict each other)

These were spoken in session and written into the design. Do not reopen them
in the audit unless two of them collide or a live check proves the code does
the opposite.

| ID | Decision | Session origin |
|---|---|---|
| D1 | Bar capsule, not a dock. No pins, magnification, autohide, badges. | Opening request + prior art |
| D2 | One icon per application (desktop id). `steam_app_*` folds into Steam only if no matching `.desktop`. | Opening request |
| D3 | Session-wide list on every bar. | DMS RunningApps default |
| D4 | Default **right, first** (before cpu/memory). Left stays workspace + window-title. | Owner placement |
| D5 | Compact: 18 px icon in 24 px tile, no title column. | DMS compact |
| D6 | Left-click focus-or-cycle. No sticky last-focused id while unfocused. | DMS Dock / Noctalia `groupClickAction: cycle` |
| D7 | Right-click is `.desktop` `Actions=` then Close all. **Steam is not special.** Spotify on this machine ships none. | Owner correction 03:57–03:59 |
| D8 / D11 | Shell owns XDG identity and `niri msg action spawn`. Do **not** use `launcher.Service` / `sysc-launch` at runtime. The launcher widget is optional. | Owner 04:36–04:38 |
| D9 | Niri windows are the model. `focus_timestamp` is `{secs,nanos}` or null. | Plan audit finding 1 |
| D10 | Focus/close are short-lived JSON `Action` requests on `$NIRI_SOCKET`. | Design |
| D12 **amended** | **No per-tile focus chrome.** Idle and focused tiles look the same. Window-title names the focused app. | Owner 06:17, after a 24×24 chip still looked wrong |
| D13 | Overlay popup, not a `KindMenu` in the bar tree. Same family as `trayMenuHost`. | Design |
| D14 | Empty capsule is absent. | Design |
| D15 | Compositor and lookup failures are non-fatal. | Design |

Further owner UI decisions that are **not** numbered in the original D1–D15
but are binding:

- Dropdown hangs **below the clicked icon**, not the right edge of the output
  (running-apps **and** tray). Reuse `trayMenuUnderBar`.
- Overlay must dismiss: Escape, click-outside shield, press+release to choose
  a row, exclusive keyboard.
- Menu rows are **not** `KindButton` stadiums and **not** `KindMenu` (panel
  combobox). Replacement chosen in session: **`KindCapsule` + `KindText`**,
  idle `FillNone` on a card painted at capsule colour, hover/keyboard
  `FillSoft`, `Radius: 6`, Close all after `KindSeparator` with `ToneError`.
  Width hugs labels (about 140–220), not `trayMenuWidth` 280.
- Live gate is a **spec review (D1–D15) plus prior-art UI parity**, not table
  tests alone. `niri msg -j layers` and grim are the live assertions.

---

## User feedback → what changed, and what may still need changing

This is the section the auditor should spend the most time on. Each row is
owner-visible behaviour, not an implementer preference.

### Feedback that was actioned

| Owner said | What landed | Residual risk |
|---|---|---|
| Right-click is not Steam-only; other apps have `Actions=` (Spotify was the counter-example and ships none here). | D7: desktop-file `Actions=` + Close all. | Confirm a non-Steam app with `Actions=` (Firefox) on the live bar. Steam was used in early live; Firefox was not open at the 181 grim. |
| Bar must work if the user does not use sysc-launcher. | Shell `go-freedesktop` index; spawn is `niri msg action spawn --`. | Duplicate XDG walk vs an open launcher is a recorded ceiling. Do not "fix" it by calling `launcher.Service`. |
| Menu at the **right of the screen**, not under the icon. | `sysc-177` closed. `trayMenuUnderBar` + tile `Bounds`. Named test `TestRunningAppsMenuIsPlacedUnderItsIcon`. | Empty `Rect` fallback; multi-output coordinates (DP-1 is at x=2560). |
| Rows unselectable; no dismiss; overlay eats bar clicks. | `sysc-178` closed. Exclusive keyboard, press+release, Escape, fullscreen `shield:running-app-menu`. | Live Escape / click-away / second right-click toggle still owner-eyes. |
| No gap; solid blue blob on first row. | First pass added gap + `FillSoft`; then Hallmark redesign dropped stadiums. `sysc-179` closed as superseded by capsule rows. | Owner may still dislike row chrome. Compare grim of the open menu, not the table test. |
| Focus chrome is a tiny stadium behind the icon; "keep it but make it wrap the whole icon". | First pass: 24×24 rounded chip (`sysc-180` original acceptance). | **Superseded the same hour.** |
| "Remove the blue focus element… it is not centering the icon." | D12 rewritten: no per-tile accent. `FillNone`, 3 px pad, 18 px icon in 24 px cell. `sysc-180` closed. | If a later agent "restores" a focus chip, that fights this owner call. |
| Menu still looks bad after gap/wash; Hallmark the menu; do not use `KindButton` stadiums without naming a replacement. | `KindCapsule`+`KindText`, hug width, pointer motion highlight, Close all after separator. | Aesthetic judgment is still owner. Auditor should grim an open Steam/Firefox menu. |
| Icons look soft / lost resolution. | Working-tree `sysc-181`: request `Physical(18)` per bar after configure; reproject tray + running-apps on scale change; `decodeRaster` `CatmullRom` instead of `ApproxBiLinear`; reuse `trayIcons`. | **Uncommitted.** Beads marked 181 closed. Both outputs here are scale 1.0, so Physical does not change the pixel size; CatmullRom is the 1× sharpness change. CatmullRom affects **every** consumer of `decodeRaster` (tray, launcher, notifications). |
| Left-click does nothing on the live bar. | Table tests already passed. Overlay eating clicks was the stated cause (178). `TestRunningAppsHandleClickCycles` added in the 181 working tree (press+release on laid-out tile → `FocusWindow`). | **No seat pointer was injected.** Plan Task 9: "If a pointer is not available, stop and leave the gate open." Owner later said "close 182". Beads then **re-opened** 182 at 08:44. Auditor must decide whether 182 is actually done. |
| Submit to `main` if safe; check unactioned beads. | `bf87d4d` is on `main` (via theme merge `41a06bc`) and on `origin/main`. 181/182 were called out as still open at that moment. | 181 code never committed. 175 was closed then recreated `in_progress`. |
| "Do 181, close 182, close 175." | 181 implemented in WT; 181/182/175 closed in bd at 07:35. | 175 and 182 exist again (`created_at` 08:44) from a later beads rewrite (`07c1916` and friends). Closing them again without the live pointer check and without committing 181 repeats the drift. |

### Feedback that was not fully proven live

- Steam **and** Firefox icons on the bar (Task 9). At the 181 grim, four **kitty** windows → one kitty icon. Steam/Firefox were not open.
- Left-click cycle on the seat (Task 9 step 3). Handle-path unit test only.
- Right-click Steam Store/Library/Friends then Close all, and an app with no `Actions=` showing Close all only, after the capsule-row redesign.
- Closing the last window of an app removes its icon (Task 9 step 5) — grouping tests exist; live not re-checked after the 181 deploy.
- Owner visual sign-off that CatmullRom icons are actually crisp enough. Grim showed a real raster (not a letter) for kitty.

### Feedback that must not be "fixed" back

- Do not put per-tile Primary/accent chrome back. D12 as amended.
- Do not route identity or spawn through `launcher.Service`.
- Do not treat Steam as a special menu source (tray SNI is a different object).
- Do not build dock chrome to chase "UI parity." Parity is RunningApps compact + Taskbar/Dock *click/menu* behaviour.

---

## Commits that are the pill (on `main`)

```
78a96ef docs: running-apps bar pill design and plan
b672510 feat(niri): project window focus from the event stream
1251d0d feat(niri): send FocusWindow and CloseWindow on the IPC socket
b58d1fc feat(shell): group Niri windows into running-app slots
0c1c830 feat(shell): match running-app ids to desktop entries
1900893 feat(shell): pick the next window for a running-app slot
adbc838 feat(shell): running-app menu is desktop actions plus Close all
68d1ac7 feat(config): running-apps is a bar item, first on the default right
00ce134 feat(shell): paint the running-apps capsule from grouped slots
0278d84 feat(shell): running-apps focus, cycle, and desktop-action menu
bf87d4d fix(shell): hang running-app menus under the icon
```

`41a06bc` later merged `main` (including those commits) into the theme tranche.
HEAD `07c1916` is launcher-UI beads, not pill code.

Also in that range, not listed above: `WindowFocusChanged` with `"id":null`
must not kill the shell (`TestWindowFocusChangedNullClearsFocus` in
`internal/platform/niri`). That was the live crash after first deploy
("whatever happened it just killed the bar").

---

## Code map

| Area | Files |
|---|---|
| Niri window focus + null id | `internal/platform/niri/events.go`, `events_test.go` |
| FocusWindow / CloseWindow | `internal/platform/niri/action.go`, `action_test.go` |
| Grouping, identity, tiles, icons | `internal/shell/runningapps.go` |
| Overlay menu host | `internal/shell/runningapps_menu.go` |
| Clicks / overlay tests | `internal/shell/runningapps_click_test.go` |
| Capsule widget tests | `internal/shell/runningapps_widget_test.go` |
| Config item + default right | `internal/config/config.go` (`knownItems`, `Default().Bar.Right[0]`) |
| Bar hit / action | `internal/shell/bar.go` (`Handle`, `hitLocked`), `registry.go` (`bindBarPanelActionsLocked`) |
| Icon worker | `internal/icons/worker.go` (`decodeRaster`), `internal/icons/theme.go` |
| Tray size (do not regress) | `internal/shell/tray.go` (`trayIconPixelSize`, `syncTrayLocked`) |
| Placement helper | `trayMenuUnderBar` (tray menu host / running-apps menu) |

Working-tree 181 behaviour (not in HEAD):

- `runningAppIconPixelSize` / `attachRunningIconsAtLocked(scale120)` / `applyRunningIconsLocked` (per-bar attach then apply so two scales do not share one upscaled raster on the tree).
- `bindHost` wraps `Configure`: if `bar.scale120()` changed, `reprojectTray` then `reprojectRunningApps`.
- `decodeRaster` uses `xdraw.CatmullRom` (wallpaper thumbs already did; icons did not).

---

## Tests that exist (scoped runner only)

This machine OOMs on a full-tree test and on the race detector. Auditor
verification, one package, named test:

```bash
timeout 90s env GOMAXPROCS=2 go test -count=1 ./internal/platform/niri -run 'TestWindowsChangedReplacesTheWholeSet|TestWindowFocusChanged'
timeout 90s env GOMAXPROCS=2 go test -count=1 ./internal/config -run 'TestParse|TestDefault'
timeout 90s env GOMAXPROCS=2 go test -count=1 ./internal/shell -run 'TestRunningApps'
timeout 90s env GOMAXPROCS=2 go test -count=1 ./internal/icons -run 'TestWorker'
timeout 90s env GOMAXPROCS=2 go test -count=1 ./internal/shell -run 'TestTrayIconSizeFollowsTheOutputScale'
```

Named pill tests to know about:

- `TestRunningAppsCapsule` — 24×24 `FillNone` tiles, gap 4, empty hides.
- `TestRunningAppsIconUsesACachedRaster` / `TestRunningAppsIconArrivesAfterPaint`
- `TestRunningAppsIconSizeFollowsTheOutputScale` / `TestRunningAppsIconFollowsConfigureScale` (**working tree only**)
- `TestRunningAppsClick` — left cycle via `onAction`, right menu labels, spawn argv, Close all, slot-gone closes menu.
- `TestRunningAppsHandleClickCycles` — **working tree only**; `Handle` press+release on bounds.
- `TestRunningAppsMenuIsPlacedUnderItsIcon`
- `TestRunningAppsMenuTakesExclusiveKeyboard` / `EscapeCloses` / `ChoosesOnPressRelease` / pointer motion `FillSoft`

`TestRunningAppsClick` calling `onAction` directly is why 182 could pass tables
while the live bar did nothing (overlay or hit-test). The Handle test is the
one that actually goes through `ui.Hit`.

---

## Live observations (2026-09-05, archPC)

Environment at the 181 deploy:

- Niri, `WAYLAND_DISPLAY=wayland-1`, `$NIRI_SOCKET=/run/user/1000/niri.wayland-1.4746.sock`
- **Two** outputs, both scale 1.0: `DP-1` 3440×1440 at logical x=2560; `DP-3` 2560×1440 at x=0.
  `AGENTS.md` still says one output (`DP-1` only). Two-output is now the live machine.
- Binary: `~/.local/bin/sysc-shell` via `systemctl --user restart sysc-shell.service` after `go build` to `.staged` then `mv`.
- `niri msg -j layers`: `sysc-shell:bar` Top on DP-1 and DP-3; `sysc-shell-toast` Overlay on both.
- Windows: four `kitty` → one running-apps icon, first on the right, before cpu/memory. No per-tile accent.
- Grim: `/tmp/sysc-181/dp1-bar-right.png` (not in-tree). Kitty theme icon visible, not a letter.
- Journal: fontconfig noise only; no repeat of the null-focus panic after the niri fix.
- Notify/tray: must be started as their own units (`sysc-notify-dev`, `sysc-tray-dev`) if the daily user session expects them. Do not `pkill -f` a name that also appears in the agent command.

Start notify/tray was an owner request after the first bar death ("launch everything we should be launching"). The pill itself does not start those daemons.

---

## Beads at writing (2026-09-06 morning)

Query live; this table is a snapshot.

| ID | Snapshot | Note for the auditor |
|---|---|---|
| **sysc-175** | `in_progress` | Recreated `created_at` 08:44 after the 07:35 close. Parent of the live punch list. Plan says close only after Task 9 live gate. |
| **sysc-177** | closed | Menu under icon. |
| **sysc-178** | closed | Dismiss / exclusive keyboard / shield. |
| **sysc-179** | closed | Stadium/blob rows; superseded by capsule redesign. |
| **sysc-180** | closed | Focus chrome removed from D12. |
| **sysc-181** | closed 07:35 | Code **not in HEAD**. Closing beads without the commit is the defect to call. |
| **sysc-182** | **open**, created 08:44 | Recreated after close. Live seat click still unproven. |
| **sysc-189** | open | This audit. |
| sysc-173 | open P2 | SVG icons. Same family, **not** this pill (design out of scope). |
| sysc-86 / sysc-117 | open P2 | Launcher theme icons. Do not fold into 175. |

`bd export` in this repo can truncate `.beads/issues.jsonl` to a handful of
lines. Recover with
`sqlite3 .beads/beads.db "DELETE FROM export_hashes;" && bd export -o .beads/issues.jsonl`
then `wc -l` (expect ~170+) and `git diff` before any commit. The 07:35 close
of 175/182 did not stick; a later beads commit rewrote those ids.

---

## Spec checklist for the auditor (D1–D15)

Walk each decision against current source **and** a live grim if the bar is
up. Mark verified / contradicted / unproven.

| D | What to prove | Likely files |
|---|---|---|
| D1 | One `KindCapsule` of tiles; no dock surface | `runningapps.go` `refreshRunningApps` |
| D2 | Group by desktop id; steam fold; unmatched letter | grouping tests + `lookupRunningApp` |
| D3 | Same `barView.Running` on every bar | `UpdateNiri` / `viewLocked` |
| D4 | `Default().Bar.Right[0] == running-apps`; left workspace+title | `internal/config/config.go` |
| D5 | 18 in 24, pad 3, gap 4, no title in the pill | `runningAppTile` constants |
| D6 | Unfocused → MRU; focused → next wrap | `nextFocusID` |
| D7 | `Actions=` then Close all; empty Actions → Close all only | `runningAppMenu` |
| D8 | Spawn argv is niri spawn, not `Service.Activate` | `TestRunningAppsClick` spawn assertions |
| D9 | `{secs,nanos}` and `WindowFocusChanged`; null id safe | niri tests |
| D10 | JSON Action, not `niri msg` for focus/close | `internal/platform/niri/action.go` |
| D11 | Shell XDG index; Hidden out; NoDisplay in | `runningapps.go` scan |
| D12 | No per-tile FillAccent / focus stadium | `runningAppTileNode` `FillNone` |
| D13 | Overlay aux; not a bar child; slot-gone closes | `runningapps_menu.go` |
| D14 | Zero slots → no children, not hit | `TestRunningAppsCapsule` empty |
| D15 | Failed focus/close logs, no panic | send path |

Prior-art parity (owner: "UI parity as a minimum"):

- Compact shared capsule, hide when empty, session-wide, 24/18 geometry: DMS RunningApps.
- One icon per app, focus-or-cycle, desktop Actions= then Close all: Noctalia grouped taskbar / dock interaction.
- **Not** dock: no pins, magnification, autohide, badges, separate dock surface.

---

## Hazards to scrutinise (implementer-known)

1. **Beads vs git vs working tree disagree** on 175/181/182. The audit's first
   job is to say what is actually shipped.
2. **`sysc-181` uncommitted** on a `registry.go` that also contains
   `adoptBar` (startup-hang fix: defer unlock because a panic in `bar.apply`
   during `wl_output.done` held `Registry.mu` and `Close` ignored SIGTERM).
   A sloppy rebase could drop either fix.
3. **`Configure` wrap publishes twice** (tray then running-apps) into an
   8-deep invalidation channel. Fine at two outputs; can block a test that
   nobody drains if output count grows.
4. **Shared `runningAppSlot.Image`** mutated per bar then copied into that
   bar's tree. Relies on apply order. A later shared refresh that attaches
   once would regress mixed scale. This machine is 1.0/1.0 so it would not
   show.
5. **CatmullRom is process-wide.** Tray and launcher icons change with it.
   Owner asked for bar-size crispness; they did not ask to retune wallpaper
   or notification rasters. Wallpaper already used CatmullRom; icons did not.
6. **Default `Bar.scale120` is `ScaleUnit` (120), not 0.** The 181 ticket text
   ("request at 18 if scale is 0") is slightly wrong on this codebase; the
   real bug is "never re-request after a non-1× configure."
7. **Existing configs** without `running-apps` do not gain it until they reset
   to `Default()` or add the item. Only `Default()` changed.
8. **Letter fallback** for SVG-only names is in-scope (sysc-173 is not). A
   live Steam/Firefox miss that is SVG-only is not a 175 regression.

---

## What the implementing session did *not* do

- Commit `sysc-181`.
- Push from this handover (origin already has `bf87d4d` via later merges).
- Owner-seat left-click, Steam+Firefox live pair, post-redesign menu grim.
- `xdg_popup` parenting (still Overlay, same as tray).
- SVG (`sysc-173` / `sysc-86` / `sysc-117`).
- Delete `feature/running-apps-pill` or its worktree.
- Update `AGENTS.md` for two outputs.

---

## Audit method

1. `git status`, `git log --oneline 78a96ef^..bf87d4d`, `git diff` of the six
   181 files. State clearly: HEAD vs working tree.
2. `bd show` 175, 177–182, 189. Do not close 175/182 from this audit unless
   the owner has since proven Task 9 live; recommend instead.
3. Walk D1–D15 and the feedback table. Cite file:line.
4. Run only the scoped named tests above (and any one new named test you add).
5. If the daily shell is mapped: `niri msg -j layers`, `niri msg -j windows`,
   grim the top 56 px of DP-1. Do not `pkill -f sysc-shell`. Restart by
   systemd unit or exact pid from `pgrep -a -f '/home/nomadx/.local/bin/sysc-shell'`.
6. Write the report. Register it. Commit report + JSONL only if the owner
   asks, together.

---

## Suggested report shape

Mirror `docs/plans/2026-09-05-running-apps-pill-audit-report.md`:

- What was verified (no action).
- Findings, severity, file:line, fix.
- Beads recommendation for 175, 181 (reopen until the diff lands?), 182, 189.
- Verdict.

---

## Session narrative (compressed, for context only)

1. Prior art → design D1–D15 → plan → plan audit (wrong `focus_timestamp`
   wire; `sysc-launch` assumed).
2. Owner: menus are desktop `Actions=` for every app; shell must lift without
   the launcher. D8/D11 amended. Findings 2 and 5 superseded.
3. TDD implementation of the ten plan tasks on `feature/running-apps-pill`.
4. Live deploy. `WindowFocusChanged` `id:null` killed the bar. Fixed with a
   named test. Owner asked to start notify/tray too.
5. Live punch list → beads 177–182 under 175 (owner asked for a todo list;
   bd is the tracker).
6. 177 placement, 178 dismiss, 179/menu Hallmark, 180 then D12 drop focus
   chrome, capsule rows instead of KindButton.
7. Fast-forward local main at `bf87d4d`; later landed on origin through
   unrelated merges. 181/182 still open at that decision.
8. 181 implemented + Handle click test; 181/182/175 closed in bd; 181 not
   committed. Later beads rewrite left 175 in_progress and 182 open.

That is the work to audit.
