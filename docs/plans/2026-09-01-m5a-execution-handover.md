# Milestone 5 Execution Handover

Status: entrance gates closed; 5A Tasks 1–3 and half of Task 4 landed; 5A Tasks 4–10 and all of 5B remain
Branch: `milestone/m5a-notifications` (head `49a5fba`, 7 commits ahead of `main`)

Plans: [5A notifications](2026-08-30-notifications-foundation.md) · [5B tray](2026-08-30-tray-foundation.md)
Design: [notifications and tray integration](2026-08-31-notifications-and-tray-integration-design.md)
Decision: [icon raster decision](2026-09-01-m5-icon-raster-decision.md)

---

## 1. Where Milestone 5 stands

### Entrance gates: all closed

| Gate | State |
|---|---|
| M5 shell prerequisites (`sysc-60`) | Merged to `main` in `bc1efa7`: `AuxUpdate`, process-wide root chain |
| M3 tooltip and Niri defects (`sysc-31`–`34`) | Merged to `main` in `7b1a5e4` |
| `sysc-notify` release candidate (`sysc-61`) | `v0.1.0-rc.1` SSH-signed, pushed |
| `sysc-tray` release candidate (`sysc-62`) | `v0.1.0-rc.1` SSH-signed, pushed |
| `sysc-51`, `sysc-57` | Closed at the maintainer's direction as no longer relevant |

Both services are code-complete against their own v0.1 plans and pass their full gates. The shell
resolves both tags through the public module proxy; there is no `replace` directive anywhere.

### Services

| Repository | Branch | Tag | State |
|---|---|---|---|
| `sysc-notify` | `redesign/v0.1` (`5a3e015`) | `v0.1.0-rc.1` | Tasks 1–8 complete; stable tag pending 5A qualification |
| `sysc-tray` | `redesign/v0.1` (`29e494e`) | `v0.1.0-rc.1` | Tasks 1–8 complete; stable tag pending 5B qualification |

Each service's completion record lives in its own repository under `docs/plans/`.

### Tranche 5A: notifications

| Task | State | Commit |
|---|---|---|
| 1 Pin and fixture the notify protocol | done | `ee451d2` |
| 2 Generation-safe notify client | done | `5977f71` |
| 3 Shared raster node and bounded icon path | done | `a354912`, `28af57a` |
| 4 Body markup and card tree | markup done, card tree **not started** | `275af66` |
| 5 Projection and aggregate presentation state | not started | — |
| 6 Toast geometry and auxiliary hosts | not started | — |
| 7 Pointer, swipe, actions, inline reply | not started | — |
| 8 Center, seen state, badge, DND presets | not started | — |
| 9 Conservative process focus | not started | — |
| 10 Wiring and qualification | not started | — |

`a50c964` additionally fixes a pre-existing intermittent race: two `t.Parallel()` tests shared the
package-level `runArgv` hook. It is now a `Registry` field (`sysc-89`).

### Tranche 5B: tray

Nothing started. All nine tasks remain. Its stated prerequisites are otherwise satisfied.

---

## 2. Remaining 5A work

### Task 4 — card tree (half done)

`internal/shell/notifytext.go` and its test are complete: the freedesktop subset parses into bounded
styled runs, with fallback to plain text on anything malformed. Bounds are `MaxRuns` 256,
`MaxMarkupDepth` 16, `MaxLinkBytes` 2 KiB.

Still to write: `internal/shell/notifycard.go` and `notifycard_test.go`.

- Build retained `ui.Node` trees from imported `protocol.Notification` and `protocol.HistoryEntry`.
- History builders omit actions and inline reply.
- Cover six action pairs, the default action, urgency, countdown, and a value bar that is independent of
  the card's own state.
- Link nodes exist only where the opener capability passed qualification. `ParseBody` already takes this
  as its `allowLinks` argument; the caller decides.
- Gate: `go test -count=1 ./internal/shell/ -run 'Notify(Text|Card)' -v`
- Commit: `feat(shell): build bounded notification cards`

### Task 5 — projection and presentation state

Create `internal/shell/notifications.go` and `presentation.go`.

- One connection generation plus per-output projections under `Registry.mu`. Only the Wayland owner
  applies messages.
- Precedence is `hovered > visible > queued > suppressed`. Queued requires **every** configured output to
  queue the record.
- Publish state changes plus two-second renewals; stop renewals and report suppressed on terminal paths.
- **Do not implement expiry, and do not remove a card until the service delta arrives.** The service owns
  lifetime; the shell must not guess.
- Table tests: snapshots, add/replace/close, hotplug, DND, center-open, zero outputs, queue state, hover
  on one copy.
- Gate: `go test -race -count=1 ./internal/shell/ -run 'Notification|Presentation' -v`
- Commit: `feat(shell): project service-owned notifications`

### Task 6 — toast geometry and auxiliary hosts

Create `internal/shell/toastlayout.go` and `toasthost.go`; extend `internal/platform/wayland/aux_test.go`.

- One Overlay aux unit per configured output, keyboard `None`, `exclusive_zone -1`.
- 360-pixel card width; clamp to the output; handle geometry overflow by queueing.
- Recompute visible/queued state after every geometry or record change.
- Use `AuxUpdate` for the input region — the union of card rectangles. **Never make the full output
  clickable.** `AuxUpdate` landed in `bc1efa7` and takes `SetInputRegion` plus `InputRects`; an empty
  rect slice means the surface takes no pointer input, which is not the same as leaving it unset.
- Cover all configured corners, top/bottom/left/right bars, queue promotion, replacement in place,
  reduced motion, output loss, full cleanup.
- Gate: `go test -race -count=1 ./internal/shell/ ./internal/platform/wayland/`
- Commit: `feat(shell): host toast stacks per output`

### Task 7 — pointer, swipe, actions, inline reply

Create `internal/shell/notifyactions.go`; modify `toasthost.go` and `root.go`.

- Send intent with request IDs and wait for service replies and deltas. Never act locally on the
  assumption a command succeeded.
- 35 percent swipe commits, less cancels.
- Inline reply closes the tooltip and any unrelated root, joins the process-wide root coordinator,
  raises keyboard from `None` to `OnDemand` via `AuxUpdate`, enables text-input, and releases keyboard,
  serial, text-input, and request state on submit, cancel, record close, root replacement, output loss,
  service loss, or shutdown.
- The root chain in `internal/shell/root.go` already provides this: `openRoot`, `attach`, `onClose`,
  `closeRoot`, `closeChild`, with generation checks that make a late close a no-op.
- Cover command `busy` and stale replies.
- Gate: `go test -race -count=1 ./internal/shell/ -run 'NotifyAction|InlineReply|Swipe' -v`
- Commit: `feat(shell): interact with notification cards`

### Task 8 — centre, seen state, badge, DND

Create `internal/shell/popout_notifications.go` and `dnd.go`; modify `bar.go` and `internal/config/config.go`.

- The centre is one root. Opening it sends `history.mark-seen` for presented entries and suppresses toasts.
- DND off/on/preset, plus `active.dismiss-all` and `history.clear` as **separate** commands.
- One generation-safe timer for preset expiry.
- Persist DND through the existing config path, atomically.
- Cover grouping, newest-first order, virtual-list bounds, history without actions, unread badge,
  keyboard traversal, and accessible names and roles.
- Gate: `go test -race -count=1 ./internal/shell/ ./internal/config/`
- Commit: `feat(shell): add notification center and dnd`

### Task 9 — conservative process focus

Create `internal/shell/notifymatch.go`.

- Match the bounded sender lineage (`protocol.Process`, capped at 16 entries) against the current Niri
  projection.
- Focus **only** one still-live process instance, and only after the service accepted the action.
- Two matches means no focus. A stale start time means no focus.
- Focus failure is a diagnostic, never an action failure.
- Gate: `go test -count=1 ./internal/shell/ -run NotifyMatch -v`
- Commit: `feat(shell): focus unambiguous notification sender`

### Task 10 — wiring and qualification

Modify `cmd/sysc-shell/main.go` and `internal/shell/registry.go`; create `tests/integration/notifications_test.go`.

- Wire client messages and commands into the registry owner, image worker, aux requests, root
  coordinator, reconnect, and shutdown. Service loss closes centre, toasts, and reply, and releases input.
- Fake-service and fake-compositor tests: sequence recovery, lease loss, two outputs, hotplug,
  markup and image failures, root replacement, shutdown.
- Automated gate, all of which must pass:

```bash
gofmt -w . && test -z "$(gofmt -l .)"
go vet ./...
go test -race -count=1 ./...
git diff --exit-code -- go.mod go.sum
```

- Then the live two-output Niri gate (section 4).
- Commit: `test(shell): qualify notification presentation`

---

## 3. Remaining 5B work

All nine tasks. 5B reuses 5A's client pattern, `KindImage`, and the icon worker; it must not grow a
second icon path.

### Task 1 — pin and fixture the tray protocol

`go get github.com/Nomadcxx/sysc-tray@v0.1.0-rc.1`, then `internal/trayclient/fixtures_test.go` covering
hello, snapshot, every item and menu delta, normal/attention/overlay icons, pixmaps, tooltip,
generations, revisions, commands, replies, unknown fields, and limits. Round-trip only through imported
types — no copied wire structs.

Mirror `internal/notifyclient/fixtures_test.go`, which does exactly this for notify.
Commit: `build: pin tray protocol candidate`

### Task 2 — generation-safe tray client

`internal/trayclient/client.go` and `socket.go`, same one-reader/one-writer ownership as notify,
**without extracting a cross-service abstraction** — the plan is explicit that the duplication is
intended. Real-socket tests for path and peer validation, handshake, snapshot N plus N+1 deltas, gap
resnapshot, malformed frames, presenter replacement, reconnect, 64-command backpressure, request IDs,
item generations, menu revisions, and unknown outcomes. Return `busy` when full; never replay effects.
Commit: `feat(tray): connect to tray service`

### Task 3 — item projection and icon composition

`internal/shell/tray.go` and `trayicon.go`. Items stored under `Registry.mu`, one `KindImage` node per
visible item, projected independently on every output. Fallback order is named icon, then pixmap, then
placeholder. Overlay composes last at half size — `internal/icons` already does this in `compose`.

**Adjust for the raster-only decision:** the plan's test list says "named SVG/raster". SVG resolves to
nothing in M5, so that case asserts the pixmap or placeholder fallback instead. See the decision document.

Commit: `feat(shell): project tray items`

### Task 4 — shared tooltip and item actions

Flattened bounded title and description go on `ui.Node.Tooltip`. **Create no new tooltip host** — M3's
tooltip is the owner, and it now takes its palette from the shell via `wayland.TooltipStyle`. Route input
through retained press/release matching. Correlate replies with request ID, item generation, output, and
current root generation. Cover activate, secondary activate, both scroll orientations, logical
coordinates, `busy`, stale reply, and no retry after an unknown result.
Commit: `feat(shell): interact with tray items`

### Task 5 — bounded menu model and back stack

`internal/shell/traymenu.go`. Validate imported trees iteratively — depth 8, 512 nodes, duplicate IDs,
malformed siblings. Build **one visible list at a time**; a submenu replaces it and pushes parent state;
Escape returns through the stack before closing. **Do not create recursive popup surfaces.** Cover
initial focus, keyboard traversal, activation, separators, disabled/checked/radio entries, and
accessible names and roles.
Commit: `feat(shell): build bounded tray menus`

### Task 6 — popup surface, revisions, root correlation

**This task begins with a live experiment, before any product code**, and cannot be done off the laptop.

Step 1 requires live-testing Niri with a 1×1 `xdg_popup`: create its own `wl_surface` and `xdg_surface`,
call `get_popup` with the protocol-required positioner, assign it to the triggering layer surface through
`zwlr_layer_surface_v1.get_popup`, grab with the pointer serial, then commit. **Record the protocol
trace.** If Niri rejects that valid sequence, select the documented Overlay auxiliary fallback and record
the compositor evidence.

Only then: `internal/shell/traymenuhost.go`, plus changes to `internal/platform/wayland/popup.go` and
`internal/shell/root.go`. One popup `surfaceUnit` in the current root chain, parented to the triggering
bar or drawer layer surface, grabbed with the saved serial. It may be a root from a bar icon or an
attached child of its drawer — `rootChain.attach` exists for exactly this. Close the tooltip first.
Release keyboard, serial, requests, and root ownership on every terminal path.

Property-only updates apply when focus survives. While interaction is active keep only the newest
structural revision and apply it on idle. A stale selection invokes nothing, requests a refresh, and
leaves the menu usable.
Commit: `feat(shell): host revision-safe tray popups`

### Task 7 — overflow and preferences

`internal/shell/traydrawer.go` and `trayprefs.go`; modify `internal/config/config.go`. Reuse tray item
nodes in a virtual-list drawer root. Persist by non-generic SNI ID, then non-generic title. **If two live
items share a token, apply neither preference and show both in default order.** Item generations never
enter persisted tokens. Cover geometry overflow, pinned-first bar order, saved order, hidden exclusion
and recovery, show/hide, pin/unpin, move earlier/later, atomic reload, token collision, and keyboard
accessibility.
Commit: `feat(shell): add tray overflow preferences`

### Task 8 — wiring and failure recovery

Wire tray client messages, commands, image work, tooltips, menus, drawer, root coordinator, output
lifecycle, reconnect, and shutdown. Service loss removes projections and closes tooltip, menu, and
drawer; **item loss affects only that item's roots.** Fake-service and fake-compositor tests for two
outputs, hotplug, generations, stale replies, popup failure, malformed siblings, preference collision,
root replacement, and cleanup. Same automated gate as 5A Task 10, then the live tray matrix.
Commit: `test(shell): qualify tray presentation`

### Task 9 — combined gate

Create `docs/plans/2026-08-31-milestone-5-completion-handover.md`. Run notify and tray together for the
combined live matrix (section 4), rerun the automated gate from a clean checkout, and record the shell
commit, service candidate tags, test output, live observations, defects, and stable-tag authorization.
Commit: `docs: record milestone 5 qualification`

---

## 4. Live qualification — needs the laptop

None of this can be done from a headless session. `ssh -p 7777 nomadx@192.168.0.64` was the route used
in earlier sessions.

**5A two-output Niri gate** (Task 10 Step 4) — the design's matrix.

**5B tray matrix** (Task 8 Step 4): normal and attention icons, overlay order, theme raster and pixmap
fallback, tooltip updates, activate, secondary activate, scroll, keyboard and pointer menu traversal,
submenu back stack, deferred structural updates, stale revision rejection, overflow drawer, hidden
recovery, pin and order preferences, item replacement, every restart direction, malformed siblings, and
60 minutes idle. Record exact app fixtures and restart direction.

**5B Task 6 Step 1** is the Niri `xdg_popup` protocol experiment described above. It gates the whole
popup task and must happen before that product code is written.

**Combined matrix** (Task 9 Step 1): centre plus drawer root replacement, tray menu from drawer, tooltip
closure before roots, inline reply replacing a menu, both services restarting independently, shell
restart, output hotplug, mixed scale and transform, and 60 minutes idle.

After qualification, both services get stable `v0.1.0` tags. A wire change means a new `rc.2` — the first
tag never moves.

---

## 5. Deviations and stale plan text

**Icons are raster-only.** No SVG rasterizer is pinned; an SVG-only theme yields a placeholder. Recorded
in the icon raster decision. This touches 5B Task 3's test list and one line of the `sysc-tray`
qualification matrix. The qualification record must state it rather than quietly pass.

**`KindImage` has no `kindCount` entry.** 5A Task 3 said to extend the exhaustive coverage table if it
had landed. It has not — that table is on the unmerged bar-parity branch. When they meet, add `KindImage`
to it; do not weaken the sentinel.

**5B's prerequisite about `sysc-51` is stale.** The plan says "`sysc-51` is fixed upstream and the shell
pins the fixed `sysc-wayland` tag." It was not fixed — it was closed as no longer relevant at the
maintainer's direction, and the shell still pins `sysc-wayland v0.2.0`. Treat that prerequisite as
withdrawn, not satisfied.

**`sysc-65` is closed as invalid**, so no bead tracks the combined gate. 5B Task 9 still describes the
work; it needs tracking elsewhere or not at all, by preference.

**Command correlation lives only in the envelope** in the tray protocol, not duplicated inside `Command`
as that plan sketched. Recorded in the tray completion document.

---

## 6. Gates and conventions

From a shell worktree:

```bash
gofmt -w . && test -z "$(gofmt -l .)"
go vet ./...
go test -race -count=1 ./...
git diff --exit-code -- go.mod go.sum
```

All clean at `49a5fba`.

- Run `-race -count=8` on `./internal/shell/` when touching that package. Its races surface roughly one
  run in eight; a single green run proves little.
- Commits from a worktree need `BEADS_DB=~/sysc-shell/.beads/beads.db` or the pre-commit hook blocks them.
- The commit-message hook rejects AI attribution by naive substring match; ordinary conventional commits
  pass.
- Never add a local `replace` directive. Both plans reject it, and release commits must not contain one.

## 7. Where the landed pieces live

- `internal/notifyclient` — socket transport and the generation rule. Publishes immutable
  `Message{Generation, Kind, …}`. A structural error ends the generation rather than skipping a message,
  because a projection built across a gap is wrong in a way the shell cannot see.
- `internal/icons` — the one shared resolver and bounded decode worker, used by both tranches. Bounds:
  32 pending jobs, 8 MiB per file, 4096-pixel source edge, 256-entry/32-MiB cache.
- `internal/ui` `KindImage` and `internal/render/image.go` — measured in both row and column paths; the
  painter drops a raster whose pixels do not match its declared geometry rather than painting part of it.
- `internal/shell/root.go` — the process-wide interactive root chain, with generation-checked closes.
- `internal/shell/notifytext.go` — body markup.
- `internal/platform/wayland/aux.go` — `AuxUpdate` for in-place keyboard and input-region policy.
