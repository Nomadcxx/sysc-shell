# Notifications Foundation Implementation Plan: Milestone 5, Tranche 5A

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Present service-owned notifications as qualified toasts and a notification center on every
output.

**Architecture:** The shell imports the tagged `sysc-notify/protocol` package and projects immutable
snapshots on the Wayland owner. One toast surface per output reports aggregate presentation state back to
the service. M3/M4 provide auxiliary surfaces and root ownership; 5A adds one shared raster node.

**Tech Stack:** Go 1.26, pinned `sysc-notify v0.1.0-rc.1`, existing Wayland bindings, standard-library
image packages, and one explicitly selected and pinned bounded pure-Go SVG parser if required.

**Design:** `docs/plans/2026-08-30-notifications-foundation-design.md`

---

## Prerequisites

- M3 tooltip fixes `sysc-32`, `sysc-33`, and `sysc-34` have landed.
- `docs/plans/2026-09-01-milestone-5-shell-prerequisites.md` has landed.
- `sysc-51` is fixed upstream and the shell pins the fixed `sysc-wayland` tag.
- `sysc-notify v0.1.0-rc.1` exists and its protocol package is standard-library-only.
- Start from a clean worktree. Reject `go.mod` changes containing a local `replace`.

---

### Task 1: Pin and fixture the notify protocol

**Files:**
- Modify: `go.mod`, `go.sum`
- Create: `internal/notifyclient/fixtures_test.go`
- Test: `internal/notifyclient/fixtures_test.go`

**Step 1:** Add the candidate tag with `go get github.com/Nomadcxx/sysc-notify@v0.1.0-rc.1`.

**Step 2:** Write fixtures for hello, snapshot with active/history, every delta, every command reply,
unknown fields, and all bounds. Assert sequence, request ID, enums, seen state, lineage, images, actions,
and presentation states round-trip through the imported types.

**Step 3:** Run `go test ./internal/notifyclient/ -run Fixture -v`. Expected: PASS after importing the
tag; fail the task if copied wire structs remain.

**Step 4:** Commit `build: pin notification protocol candidate`.

---

### Task 2: Generation-safe notify client

**Files:**
- Create: `internal/notifyclient/client.go`, `internal/notifyclient/socket.go`
- Test: `internal/notifyclient/client_test.go`

**Step 1:** Write failing real-socket tests for path ownership/mode, `SO_PEERCRED`, handshake, snapshot
baseline, N+1 deltas, sequence gap, malformed frame, presenter replacement, reconnect, 64-command
backpressure, and request/reply correlation.

**Step 2:** Implement one reader and one serialized writer. Publish immutable
`Message{Generation, Snapshot|Delta|Reply|Disconnected}` values. Drop projected state on any structural
error and reconnect with capped cancellation-aware backoff. Return `busy` on a full command queue.
Never replay a command after an unknown outcome.

**Step 3:** Run `go test -race -count=1 ./internal/notifyclient/`. Expected: PASS.

**Step 4:** Commit `feat(notify): connect to notification service`.

---

### Task 3: Shared raster node and bounded icon path

**Files:**
- Modify: `internal/ui/tree.go`, `internal/ui/layout.go`, `internal/ui/column.go`, `internal/render/paint.go`
- Create: `internal/render/image.go`, `internal/icons/theme.go`, `internal/icons/worker.go`
- Test: `internal/ui/layout_test.go`, `internal/ui/column_test.go`, `internal/render/image_test.go`, `internal/icons/theme_test.go`, `internal/icons/worker_test.go`

**Step 1:** Write failing tests for `KindImage` measurement through both row and column paths,
premultiplied alpha paint, scale keys,
normal/overlay composition, theme inheritance, PNG/JPEG decode, bounded SVG raster, cancellation,
duplicate-job collapse, 32-job queue, 256-entry/32-MiB cache, and malformed fallback.
If the exhaustive `kindCount` coverage table has landed, add `KindImage` to it; do not weaken the
sentinel.

**Step 2:** Add one image node and painter plus one shared XDG icon-theme resolver; reuse a resolver if
one has landed rather than adding a notification-only path. Prefer SVG then raster. No SVG parser is
currently pinned: select and record one bounded pure-Go dependency before implementation, or stop if no
candidate can enforce the integration-design limits. One worker decodes/rasterizes outside the Wayland
owner and publishes immutable results.

**Step 3:** Run `go test -race -count=1 ./internal/render/ ./internal/icons/`. Expected: PASS.

**Step 4:** Commit `feat(render): add shared bounded image path`.

---

### Task 4: Body markup and card tree

**Files:**
- Create: `internal/shell/notifytext.go`, `internal/shell/notifycard.go`
- Test: `internal/shell/notifytext_test.go`, `internal/shell/notifycard_test.go`

**Step 1:** Write failing tests for bold, italic, underline, line breaks, entities, links, unsupported
tags, invalid fallback, depth 16, 256 runs, 2-KiB link targets, six action pairs, default action, reply,
urgency, countdown, and independent value bar.

**Step 2:** Parse the freedesktop subset into bounded styled runs. Build retained nodes from imported
protocol records. History builders omit actions and reply. Link nodes exist only when the opener
capability passed qualification.

**Step 3:** Run `go test -count=1 ./internal/shell/ -run 'Notify(Text|Card)' -v`. Expected: PASS.

**Step 4:** Commit `feat(shell): build bounded notification cards`.

---

### Task 5: Projection and aggregate presentation state

**Files:**
- Create: `internal/shell/notifications.go`, `internal/shell/presentation.go`
- Test: `internal/shell/notifications_test.go`, `internal/shell/presentation_test.go`

**Step 1:** Write failing table tests for snapshots, add/replace/close, hotplug, DND, center-open, zero
outputs, queue state, hover on one copy, and precedence `hovered > visible > queued > suppressed`.
Assert queued requires every configured output to queue the record.

**Step 2:** Store one connection generation and per-output projections under `Registry.mu`. Only the
Wayland owner applies messages. Publish state changes plus two-second renewals; stop renewals and report
suppressed on terminal paths. Do not implement expiry or remove a card until the service delta arrives.

**Step 3:** Run `go test -race -count=1 ./internal/shell/ -run 'Notification|Presentation' -v`.

**Step 4:** Commit `feat(shell): project service-owned notifications`.

---

### Task 6: Toast geometry and auxiliary hosts

**Files:**
- Create: `internal/shell/toastlayout.go`, `internal/shell/toasthost.go`
- Test: `internal/shell/toastlayout_test.go`, `internal/shell/toasthost_test.go`
- Test: `internal/platform/wayland/aux_test.go`

**Step 1:** Write failing tests for all configured corners, top/bottom/left/right bars, 360-pixel width,
output clamping, geometry overflow, queue promotion, card input-region union, replacement in place,
reduced motion, output loss, and full cleanup.

**Step 2:** Open one Overlay aux unit per configured output with keyboard None and `exclusive_zone -1`.
Render cards through existing shell callbacks. Recompute visible/queued state after every geometry or
record change. Use `AuxUpdate` for input regions; never make the full output clickable.

**Step 3:** Run `go test -race -count=1 ./internal/shell/ ./internal/platform/wayland/`.

**Step 4:** Commit `feat(shell): host toast stacks per output`.

---

### Task 7: Pointer, swipe, actions, and inline reply

**Files:**
- Modify: `internal/shell/toasthost.go`, `internal/shell/root.go`
- Create: `internal/shell/notifyactions.go`
- Test: `internal/shell/notifyactions_test.go`, `internal/shell/toasthost_test.go`

**Step 1:** Write failing tests for hover state, press/release matching, default and named actions,
dismiss, 35-percent swipe commit/cancel, command `busy`, stale reply, reply focus, text-input lifecycle,
keyboard None-to-OnDemand updates, and every close path.

**Step 2:** Send intent with request IDs and wait for service replies/deltas. Inline reply closes the
tooltip and unrelated root, joins the process-wide root coordinator, updates keyboard/input region,
enables text-input, and releases keyboard, serial, text-input, and request state on submit, cancel,
record close, root replacement, output loss, service loss, or shutdown.

**Step 3:** Run `go test -race -count=1 ./internal/shell/ -run 'NotifyAction|InlineReply|Swipe' -v`.

**Step 4:** Commit `feat(shell): interact with notification cards`.

---

### Task 8: Center, seen state, badge, and DND presets

**Files:**
- Create: `internal/shell/popout_notifications.go`, `internal/shell/dnd.go`
- Modify: `internal/shell/bar.go`, `internal/config/config.go`
- Test: `internal/shell/popout_notifications_test.go`, `internal/shell/dnd_test.go`

**Step 1:** Write failing tests for grouping, newest-first order, virtual-list bounds, history without
actions, mark-seen, unread badge, permanent DND, preset expiry, center suppression, separate clear
history and dismiss active commands, keyboard traversal, and accessible names/roles.

**Step 2:** Add the center as one root. Opening sends `history.mark-seen` for presented entries and suppresses toasts.
Provide DND off/on/preset controls, `active.dismiss-all`, and `history.clear`. Use one generation-safe
timer for presets. Persist DND config atomically through the existing config path.

**Step 3:** Run `go test -race -count=1 ./internal/shell/ ./internal/config/`.

**Step 4:** Commit `feat(shell): add notification center and dnd`.

---

### Task 9: Conservative process focus

**Files:**
- Create: `internal/shell/notifymatch.go`
- Test: `internal/shell/notifymatch_test.go`

**Step 1:** Write failing tests for one match, no match, two matches, stale process start time, vanished
window, replacement lineage, and service acceptance preceding focus.

**Step 2:** Match bounded lineage against the current Niri projection. Focus only one still-live process
instance after the service accepted the action. Treat focus failure as a diagnostic, not action failure.

**Step 3:** Run `go test -count=1 ./internal/shell/ -run NotifyMatch -v`.

**Step 4:** Commit `feat(shell): focus unambiguous notification sender`.

---

### Task 10: Wiring and qualification

**Files:**
- Modify: `cmd/sysc-shell/main.go`, `internal/shell/registry.go`
- Create: `tests/integration/notifications_test.go`
- Modify: `tests/integration/README.md`

**Step 1:** Wire client messages and commands into the registry owner, image worker, auxiliary requests,
root coordinator, reconnect, and shutdown. Service loss closes center/toasts/reply and releases input.

**Step 2:** Add fake-service and fake-compositor tests for sequence recovery, lease loss, two outputs,
hotplug, markup/image failures, root replacement, and shutdown.

**Step 3:** Run:

```bash
gofmt -w .
test -z "$(gofmt -l .)"
go vet ./...
go test -race -count=1 ./...
git diff --exit-code -- go.mod go.sum
```

Expected: PASS; the module diff contains the pinned candidate and no local replacement.

**Step 4:** Execute and record the design's two-output Niri gate. Any protocol change requires a new
service release candidate and updated pin.

**Step 5:** Commit `test(shell): qualify notification presentation`.
