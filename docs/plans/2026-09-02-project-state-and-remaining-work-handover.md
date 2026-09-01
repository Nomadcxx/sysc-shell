# Project state and remaining work

Date: 2026-09-02
Kind: project-handover
Head: `3159318` on `main`, pushed
Supersedes nothing; complements the per-milestone handovers registered beside it.

This is the whole-project picture: what exists, what is in flight, what is left, and the conventions
and traps that cost earlier sessions time. Read the milestone sections for detail and the
**Conventions** and **Traps** sections before touching anything.

---

## 1. Where the project is

`sysc-shell` is a Wayland shell for Niri: one bar per output, panels, OSDs, notifications, a system
tray, a launcher, and a plugin host. Nine milestones are planned; five are complete, one is in flight,
two are not started, and one is a standing qualification milestone.

| Milestone | Subject | State |
|---|---|---|
| M0 | Project foundation | complete |
| M1 | Architectural proof | complete |
| M2 | Stable bar on every output | code complete; **live gate never run** (`sysc-5`) |
| M3 | Built-in widget foundation | complete |
| M4 | Panels and standard controls | code complete as of `715cc5f`; **live gate unrun** (`sysc-106`) |
| M5 | Notifications and tray | shell side complete; **services do not exist**, live matrix unrun (`sysc-97`) |
| M6 | External widget and plugin host | **in flight**, see section 5 |
| M7 | Shell breadth | launcher slice landed; the rest **not started** |
| M8 | Rendering qualification | **not started**, and correctly deferred |

The pattern worth seeing: every milestone from M2 onward is *code* complete and *live* unqualified.
Three separate live gates are open for the same underlying reason — one physical output on the
development machine, and, for M5, services that were never written.

---

## 2. Conventions

**Work is tracked in bd, not in documents.** `bd ready` and `bd blocked` are authoritative. A status
written into a document header drifts, and one already did. Run `bd` from `/home/nomadx/sysc-shell`,
never from a worktree: the SQLite database is gitignored and lives only in the primary checkout, and a
worktree run creates a second empty one.

**Committing from a worktree needs the database path**, or bd's pre-commit hook aborts every commit:

```bash
BEADS_DB=/home/nomadx/sysc-shell/.beads/beads.db git commit -F msg.txt
```

**The commit-msg hook rejects AI attribution by naive substring match.** It lives at
`~/.git-hooks/commit-msg` via `core.hooksPath`, so it applies to every repository on this machine.
Ordinary English trips it: `both`, `Hallmark`, and anything containing `agent`, `cursor`, `codex`,
`bard`, `cody` or `llm`. Screen before committing:

```bash
grep -oiE "(claude|anthropic|chatgpt|openai|copilot|cursor|cody|tabnine|codex|gemini|bard|gpt-[0-9]|llm|ai assistant|bot|agent)" msg.txt && echo BANNED || echo clean
```

**The gate before any commit that touches code:**

```bash
gofmt -w . && test -z "$(gofmt -l .)"
go vet ./...
go test -race -count=1 ./...
git diff --exit-code -- go.mod go.sum
```

**Milestone branches** live under `~/.config/superpowers/worktrees/<repo>/<branch>` or
`.worktrees/<branch>` inside the repository. Remove a merge worktree when done; several were left
behind and their work was lost for weeks (section 8).

---

## 3. Environment

A live Niri session runs on this machine, but the agent shell inherits none of its environment:

```bash
export NIRI_SOCKET=$(ls /run/user/1000/niri.wayland-*.sock | head -1)
export WAYLAND_DISPLAY=wayland-1
export XDG_RUNTIME_DIR=/run/user/1000
```

`niri msg -j layers` is the best live assertion this project has: it names every mapped layer surface,
its layer and its keyboard interactivity, so a gate can check that `sysc-shell:bar`,
`sysc-shell-toast`, `sysc-shell-panel`, `sysc-shell-shield`, `sysc-shell-tray-menu` and
`sysc-shell-tray-drawer` appear and disappear without reading pixels. `niri msg action
screenshot-screen` writes to `~/Pictures/Screenshots` when pixels are needed; delete the capture
afterwards.

`ydotool` is installed with `ydotoold` running, which is how pointer-dependent bugs get reproduced:

```bash
YDOTOOL_SOCKET=/run/user/1000/.ydotool_socket ydotool mousemove -a -x 1700 -y 700
```

**Never `pkill -f` a binary name you also typed in the command** — the pattern matches the agent's own
shell and kills the tool call. Kill by pid from `pgrep -f 'scratchpad/<name>'`.

**One output only.** `DP-1`, 3440x1440, scale 1.0, transform Normal. Niri offers no runtime virtual
output, so every two-output check in every milestone is unrunnable here. This single fact blocks
`sysc-5`, part of `sysc-106`, and part of `sysc-97`.

---

## 4. Dependencies and sibling repositories

| Module | Pin | State |
|---|---|---|
| `sysc-wayland` | `v0.2.1` | healthy; released 2026-09-02 with the object-argument fix |
| `sysc-metrics` | `v0.2.0` | healthy; supplies more than the catalogue long claimed (section 7) |
| `sysc-notify` | `v0.1.0-rc.2` | **documentation only — no Go code, no daemon** |
| `sysc-tray` | `v0.1.0-rc.1` | **documentation only — no Go code, no daemon** |
| `sysc-launch` | unpinned | extraction in flight (`sysc-99`), repo exists at `/home/nomadx/sysc-launch` |

`replace` directives are forbidden. `git diff --exit-code -- go.mod go.sum` is part of every gate.

The two missing services are the single largest blocker in the project. Their v0.1 plans exist:

- `~/.config/superpowers/worktrees/sysc-notify/redesign/v0.1/docs/plans/2026-08-31-sysc-notify-v0.1.md` — 9 tasks
- `~/.config/superpowers/worktrees/sysc-tray/redesign/v0.1/docs/plans/2026-08-31-sysc-tray-v0.1.md` — 8 tasks

Roughly 17 tasks and two release candidates. Until they exist, no notification or tray behaviour has
ever been observed against a real service; all of it is covered by unit and integration tests against
fakes and by nothing else.

---

## 5. Milestone 6: in flight

**Being worked on now, in another session. Do not merge `milestone/plugin-host` without checking with
whoever holds it.**

`sysc-77` is in progress. The branch `milestone/plugin-host` is five commits ahead of `main` and
**seventy-seven behind**: it branched before M5 landed, so merging it as-is would delete the tray
integration suite and the M5 README section. It needs `main` merged into it, not the other way round.

Landed on that branch (`sysc-76` records it): the `plugin/v1` wire contract, manifest validation and
discovery, configuration and persistent state, process supervision and handshake, and view conversion.

Open:

| Issue | Subject |
|---|---|
| `sysc-77` | M6A host kernel, manager and Timer — in progress |
| `sysc-76` | M6A Tasks 6–9: attach plugin views to shell owners |
| `sysc-69` | duplicate of `sysc-77`'s subject; reconcile or close one |
| `sysc-70` | M6B incremental views and World Clock |
| `sysc-71` | M6C Notes |
| `sysc-72` | M6D Weather |
| `sysc-73` | M6E Screen Recorder |

Plans: `docs/plans/2026-09-01-milestone-6{,a,b,c,d,e,f}-*.md`, all on `main`.

`internal/plugins/` exists on disk as an empty untracked directory. It is a leftover, not a stub.

---

## 6. Milestone 7: partly landed, mostly not started

The roadmap defines M7 as **shell breadth** — vertical feature slices, each owning its service, state
projection, components, surfaces, actions, tests and failure behaviour. Candidate order from
`docs/roadmap.md`:

1. **launcher and application search** — landed; see below
2. OSDs and richer notification history
3. clipboard history
4. control centre: network, Bluetooth, audio, brightness, power actions
5. media controls
6. wallpaper control through gSlapper's existing control socket
7. desktop widgets and richer plugin surfaces

Only slice 1 exists. Slices 2–7 have **no design, no plan and no issues**. Two of them need D-Bus
designs before entering a branch: MPRIS player discovery and position handling for media controls, and
AT-SPI if screen-reader export is ever adopted.

The Wayland protocols the remaining slices need are all present on Niri 26.04 and need no negotiation
design: `ext_data_control_manager_v1` and `zwlr_data_control_manager_v1` for clipboard,
`zwp_primary_selection_device_manager_v1`, `zwlr_screencopy_manager_v1`, `xdg_activation_v1`,
`ext_foreign_toplevel_list_v1`, `zwlr_foreign_toplevel_manager_v1`, `ext_idle_notifier_v1`,
`zwp_idle_inhibit_manager_v1`. Two are worth adopting before their slice needs them:
`wp_cursor_shape_manager_v1` (already bound) and `zwlr_output_manager_v1` for any output configuration
feature.

The roadmap decides that the shell does **not** own gSlapper's lifecycle, so the wallpaper slice stays
a socket client rather than growing process supervision.

### The launcher slice

Landed and merged. `sysc-90` executed `docs/plans/2026-09-01-launcher.md`. Open follow-ups:

| Issue | Subject | Note |
|---|---|---|
| `sysc-99` | extract the core to `sysc-launch` | in progress; repo exists, needs tag and pin |
| `sysc-93` | M7 live Niri gate | was blocked on `sysc-41`; **now unblocked**, panels paint |
| `sysc-86` | real theme icons | see below |
| `sysc-87` | `xdg_activation_v1` token binding |  |
| `sysc-88` | adopt the interactive-root coordinator |  |
| `sysc-79`–`85` | calculator, emoji, windows/Niri-focus providers; bar glyph button; app grid and category chips; searchable desktop actions; fsnotify rescan |  |

`sysc-86` is worth a note. The launcher's `□` placeholder is a **deliberate v1 stub, not a defect**:
`launcher.Icon` is a deferred closure precisely so a theme-icon slice can replace it without changing
`Entry` or `Result`, and the 40px slot is already reserved so nothing reflows. The machinery it waits
on now exists — `internal/icons` (resolver plus bounded worker) and `ui.KindImage`, both from M5's
tray. `Registry.trayImagesLocked` is a working example of the exact pattern. But it is not a
one-liner: `launcherRow` currently renders no icon node at all, so the work is a registry-side icon
worker, a resolve path for `Entry.IconName`, a `KindImage` node in the row, and panel invalidation when
a decode lands.

---

## 7. Milestone 8: not started, correctly

Keep `wl_shm` while it meets the measured budgets. Add EGL/OpenGL ES only for a **named failing case**:
animation frame time, large blurred panels, image-heavy grids, or unacceptable CPU or power use.

Nothing has been measured, so nothing justifies it yet. If GPU work starts: retain the UI tree and
layout engine, add the second renderer beside the working shared-memory one, compare pixel output and
damage behaviour, keep the software renderer for tests and recovery, and document the driver, scale and
output combinations used to qualify it. The milestone expands no shell features.

**The prerequisite nobody has done is measurement.** M3's tranches each list baselines to record — idle
CPU and wakeups over 60 minutes, CPU during ticks and bursts, RSS after an hour, submitted and skipped
frame counts, layout and paint duration, allocations per update, binary size. Those baselines were
never captured. M8 cannot begin honestly without them, and they are also what would justify *not*
doing M8.

---

## 8. Remaining work, in the order worth doing

### Tier 1 — gates that unblock judgement

1. **`sysc-97` — the M5 live matrix.** Blocked on the two services. This is the long pole for the whole
   project: it gates a stable `sysc-notify` and `sysc-tray` tag, which gates M5's epic (`sysc-20`).
   Roughly 17 tasks across two repositories. Start here if capacity allows.
2. **`sysc-5`, `sysc-106`, `sysc-93`** — the M2, M4 and M7 live gates. `sysc-93` is newly unblocked
   and cheap: the launcher now paints. `sysc-5` and half of `sysc-106` need a second output.
3. **`sysc-96` — the tray `xdg_popup` probe.** Deferred deliberately. Tray menus ship on the design's
   named Overlay fallback, which is live-verified to map. The probe needs `xdg_wm_base` bound in
   `internal/platform/wayland`, which no shipped path uses. Its answer only decides whether to replace
   a working fallback.

### Tier 2 — visible quality

4. **`sysc-102` — the system monitor panel.** Cards landed 2026-09-02: every metric visible at once,
   titled, with units, plus a Resources card carrying load average, memory, swap and filesystems.
   Remaining: the reference is a 2x3 grid (needs `sysc-105`), and its GPU and CPU-temperature cards
   need data no release supplies. The System card of static facts — CPU and GPU model, distro, kernel,
   host, uptime — is a one-shot read of `/proc/cpuinfo`, `/etc/os-release` and `uname` plus plumbing
   `UptimeSnapshot`, and is not blocked.
5. **`sysc-104` — card fill contrast.** Measured 1.17:1 between the fallback capsule `0x282c33` and the
   panel background `0x1d2025`. Two surfaces that close cannot be told apart, which is why the monitor
   panel's cards read as one dark box. This is a palette decision on `Theme.Capsule` and the generated
   `SurfaceContainer` mapping, so it is **owner taste, not a correction**. `sysc-54` turns on it:
   grouping was chosen flat because nested surfaces are indistinguishable.
6. **`sysc-105` — the two-up grid.** `measureNode` returns `contentHeight` as a capsule's height, which
   is right in the bar where that is the band and wrong inside a column-nested row where
   `columnChildHeight` passes `1<<20`. Either give the capsule an explicit height, or make its height
   its child plus padding and have the bar stretch it — the second changes bar geometry that landed on
   2026-09-02 and needs a live look.
7. **`sysc-58` — icon advance.** Glyph ink spans about 1150 font units against an advance of 900, so an
   icon sits tight against its value. Widening the advance to 1260 once corrupted the whole bar, which
   is either a mask or blend path that cannot handle an advance wider than the em, or something else
   entirely. Visible on every bar right now.
8. **`sysc-103` — the widget roster.** The reference bars carry clipboard, bluetooth, wifi,
   notifications and volume. None exist here. This is new widget work rather than chrome, and it
   overlaps M7 slices 3 and 4 — decide whether it is bar work or a breadth slice before starting.
9. **`sysc-54` — group the sysmon widgets.** The group item and default grouping landed; what remains
   is judging it live now that icons and capsules exist.

### Tier 3 — completion and cleanup

10. **`sysc-98`** — route toast card activation to the notification service. Needs a service.
11. **`sysc-35`** — `Metrics.Leased` has no callers and dereferences without a nil guard. Delete it
    unless a consumer appears.
12. **`sysc-69`** — appears to duplicate `sysc-77`. Reconcile or close.

---

## 9. Traps and corrections a successor should not rediscover

**`bd export` destroys the tracked JSONL.** It emits only what it thinks changed and writes *that* over
the whole file: 93 lines became 1 on 2026-09-02. The cursor is the `export_hashes` table in
`beads.db`, not the file, so re-running never recovers. Always:

```bash
sqlite3 .beads/beads.db "DELETE FROM export_hashes;" && bd export -o .beads/issues.jsonl
```

Then check `wc -l` and that `git diff` names only the IDs you touched.

**A closed issue is not evidence the code is on `main`.** `sysc-53` and `sysc-55` were recorded closed
against code that existed only on an unmerged branch for weeks. `sysc-51` was closed while its own note
said the fix was still required. Check the code, not the tracker, before trusting a "closed".

**Four branches were finished and never merged**, which is how a P0 stayed live for weeks. Three are
now merged (`fix/live-empty-panels`, `feature/bar-visual-parity`, and the M5 work). Remaining:
`milestone/plugin-host` (in flight, section 5) and `audit/milestone-5` (2 doc commits, 224 behind,
probably not worth chasing). **When a branch is finished, merge it or record why not.**

**The reference catalogue understated what `sysc-metrics` supplies.** Corrected 2026-09-02 in
`docs/plans/assets/2026-08-31-bar-visual-parity/refs/README.md`: load averages are
`CPUSnapshot.Load1/5/15`, swap is `MemorySnapshot.Swap`, disk capacity is the filesystem snapshot, and
uptime has its own snapshot. `services.Snapshot` already carries the CPU, memory and filesystem
snapshots whole. Only GPU usage and CPU temperature are genuinely absent upstream.

**A fraction graphs against full scale, a rate against its own maximum.** `normalise` divides a series
by its own maximum, which made steady 31 per cent memory paint as a solid full-height block reading as
a machine out of memory. Fixed for CPU, memory and filesystem on 2026-09-02.

**Host callbacks run on the Wayland owner; relays run elsewhere.** Panel, tray, toast and drawer
`configure`, `render` and `handle` all take `Registry.mu`, because the client pumps and the launcher
result relay write the same state. `rebuildPanel` already holds the lock and calls the unlocked form —
keep that split when adding a host.

**`sysc-shell` panels measure through the real font renderer.** Before `715cc5f` `PanelHost.configure`
used `len(s)*8, 16` and `render` never called `render.Paint`, so every panel mapped as empty chrome for
the whole of M4 and M5.

**`SyncToastOutputs` had no caller** until 2026-09-02, so toast surfaces were never opened in any
build. Wiring the tray's output lifecycle exposed it. Check that a new exported lifecycle hook is
actually called.

---

## 10. What landed on 2026-09-02

For continuity, the session that produced this document:

- M5 completed and merged; `docs/plans/2026-09-02-milestone-5-completion-handover.md` records it.
- `sysc-wayland v0.2.1` released and repinned: an `object` event argument no longer binds a proxy to a
  client-range id, which had been killing the Wayland connection on a `wl_pointer.leave` naming a
  just-destroyed surface. Verified with 80 panel cycles under the pointer.
- `fix/live-empty-panels` merged: panels paint.
- `feature/bar-visual-parity` merged: capsules, workspace pills, grouped widgets, icon glyphs.
- The system monitor panel rebuilt as titled cards with a Resources card.
- Closed: `sysc-41`, `42`, `43`–`49`, `51`, `64`, `95`, `100`, `101`.
