# Tray Foundation Implementation Plan: Milestone 5, Tranche 5B

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Present service-owned tray items, menus, overflow, and preferences on every output.

**Architecture:** The shell imports the tagged `sysc-tray/protocol` package and applies immutable
snapshots on its Wayland owner. 5B reuses 5A's image path, M3's tooltip, and M4's auxiliary/root
machinery. The tray service owns D-Bus items and menu revisions; the shell owns pixels and interaction.

**Tech Stack:** Go 1.26, pinned `sysc-tray v0.1.0-rc.1`, existing Wayland bindings and shell UI.

**Design:** `docs/plans/2026-08-30-tray-foundation-design.md`

---

## Prerequisites

- 5A's protocol-client pattern, `KindImage`, cache, and worker have landed.
- M3/M4 tooltip, auxiliary update, input routing, virtual list, and root coordinator have landed.
- `sysc-tray v0.1.0-rc.1` exists. Start clean and reject local `replace` directives.

---

### Task 1: Pin and fixture the tray protocol

**Files:**
- Modify: `go.mod`, `go.sum`
- Create: `internal/trayclient/fixtures_test.go`

**Step 1:** Run `go get github.com/Nomadcxx/sysc-tray@v0.1.0-rc.1`.

**Step 2:** Add fixtures for hello, snapshot, every item/menu delta, normal/attention/overlay icons,
pixmaps, tooltip, generations, revisions, commands, replies, unknown fields, and limits. Round-trip only
through imported protocol types.

**Step 3:** Run `go test ./internal/trayclient/ -run Fixture -v`. Expected: PASS.

**Step 4:** Commit `build: pin tray protocol candidate`.

---

### Task 2: Generation-safe tray client

**Files:**
- Create: `internal/trayclient/client.go`, `internal/trayclient/socket.go`
- Test: `internal/trayclient/client_test.go`

**Step 1:** Write failing real-socket tests for path/peer validation, handshake, snapshot N plus N+1
deltas, gap resnapshot, malformed frames, presenter replacement, reconnect, 64-command backpressure,
request IDs, item generations, menu revisions, and unknown outcomes.

**Step 2:** Implement the same one-reader/one-writer ownership pattern as notify without extracting a
cross-service abstraction. Publish immutable generation-tagged messages. Return `busy` when full and
never replay effects.

**Step 3:** Run `go test -race -count=1 ./internal/trayclient/`.

**Step 4:** Commit `feat(tray): connect to tray service`.

---

### Task 3: Item projection and icon composition

**Files:**
- Create: `internal/shell/tray.go`, `internal/shell/trayicon.go`
- Test: `internal/shell/tray_test.go`, `internal/shell/trayicon_test.go`

**Step 1:** Write failing tests for snapshot/deltas, owner replacement, stale generation, normal status,
attention replacement, overlay-last half-size composition, named SVG/raster, pixmap fallback, cache
keys, malformed candidate isolation, and output-independent projection.

**Step 2:** Store imported item records under `Registry.mu`. Build one `KindImage` node per visible
item. Reuse 5A lookup, worker, and cache. Fallback named icon to pixmap to placeholder. Project the same
item independently on every output.

**Step 3:** Run `go test -race -count=1 ./internal/shell/ -run 'Tray(Item|Icon)' -v`.

**Step 4:** Commit `feat(shell): project tray items`.

---

### Task 4: Shared tooltip and item actions

**Files:**
- Modify: `internal/shell/tray.go`, `internal/shell/tooltip.go`
- Test: `internal/shell/tray_test.go`, `internal/shell/tooltip_test.go`

**Step 1:** Write failing tests for bounded title/description flattening, dynamic tooltip update, item
loss close, activate, secondary activate, vertical/horizontal scroll, logical coordinates, command
`busy`, stale reply, and no retry after unknown result.

**Step 2:** Put flattened text on `ui.Node.Tooltip`; create no new tooltip host. Route input through
retained press/release matching and imported commands. Correlate replies with request ID, item
generation, output, and current root generation.

**Step 3:** Run `go test -race -count=1 ./internal/shell/ -run 'Tray|Tooltip' -v`.

**Step 4:** Commit `feat(shell): interact with tray items`.

---

### Task 5: Bounded menu model and back stack

**Files:**
- Create: `internal/shell/traymenu.go`
- Test: `internal/shell/traymenu_test.go`

**Step 1:** Write failing tests for depth 8, 512 nodes, duplicate IDs, malformed siblings, initial focus,
keyboard traversal, activation, separators, disabled/checked/radio entries, submenu push/back, and
accessible names/roles.

**Step 2:** Validate imported menu trees iteratively. Build one visible list at a time; submenus replace
it and push the parent state. Escape returns through the stack before closing. Do not create recursive
popup surfaces.

**Step 3:** Run `go test -count=1 ./internal/shell/ -run TrayMenu -v`.

**Step 4:** Commit `feat(shell): build bounded tray menus`.

---

### Task 6: Popup surface, revisions, and root correlation

**Files:**
- Create: `internal/shell/traymenuhost.go`
- Modify: `internal/platform/wayland/popup.go`
- Modify: `internal/shell/root.go`
- Test: `internal/shell/traymenuhost_test.go`, `internal/shell/root_test.go`
- Test: `internal/platform/wayland/aux_test.go`

**Step 1:** Before product code, live-test Niri with a 1x1 xdg_popup. Create its own `wl_surface` and
`xdg_surface`, call `get_popup` with the protocol-required positioner, assign it to the triggering layer
surface through `zwlr_layer_surface_v1.get_popup`, grab with the pointer serial, then commit. Record the
protocol trace. If Niri rejects this valid sequence, select the documented Overlay auxiliary fallback
and record the compositor evidence.

**Step 2:** Write failing tests for protocol order, saved serial, output/item/revision/root correlation, keyboard
OnDemand, input-region update, outside close, item/output/service loss, root replacement, property-only
update, deferred structural update while active, focused-ID restoration, and stale selection.

**Step 3:** Open one popup `surfaceUnit` in the current root chain. Parent it to the triggering bar or
drawer layer surface, position against the relevant edge, and grab with the saved serial. It may be a
root from a bar icon or an attached child of its drawer. Close tooltip first. Save correlation fields.
Release keyboard, serial, requests, and root ownership on every terminal path. Use the recorded fallback
only if Step 1 required it.

**Step 4:** Apply property-only updates when focus survives. Keep only the newest structural revision
while interaction is active; apply it on idle. A stale selection invokes nothing, requests refresh, and
keeps the menu usable.

**Step 5:** Run `go test -race -count=1 ./internal/shell/ ./internal/platform/wayland/`.

**Step 6:** Commit `feat(shell): host revision-safe tray popups`.

---

### Task 7: Overflow and preferences

**Files:**
- Create: `internal/shell/traydrawer.go`, `internal/shell/trayprefs.go`
- Modify: `internal/config/config.go`
- Test: `internal/shell/traydrawer_test.go`, `internal/shell/trayprefs_test.go`

**Step 1:** Write failing tests for geometry overflow, pinned-first bar order, ordinary saved order,
hidden exclusion, recoverable hidden section, show/hide, pin/unpin, move earlier/later, atomic reload,
stable token selection, generic IDs, token collision, and keyboard accessibility.

**Step 2:** Reuse tray item nodes in a virtual-list drawer root. Persist preferences by non-generic SNI
ID, then non-generic title. If two live items share a token, apply neither preference and show both in
default order. Item generations never enter persisted tokens.

**Step 3:** Run `go test -race -count=1 ./internal/shell/ ./internal/config/`.

**Step 4:** Commit `feat(shell): add tray overflow preferences`.

---

### Task 8: Wiring and failure recovery

**Files:**
- Modify: `cmd/sysc-shell/main.go`, `internal/shell/registry.go`
- Create: `tests/integration/tray_test.go`
- Modify: `tests/integration/README.md`

**Step 1:** Wire tray client messages, commands, image work, tooltips, menus, drawer, root coordinator,
output lifecycle, reconnect, and shutdown. Service loss removes projections and closes tooltip/menu/
drawer; item loss affects only that item's roots.

**Step 2:** Add fake-service and fake-compositor tests for two outputs, hotplug, generations, stale
replies, popup failure, malformed siblings, preference collision, root replacement, and cleanup.

**Step 3:** Run:

```bash
gofmt -w .
test -z "$(gofmt -l .)"
go vet ./...
go test -race -count=1 ./...
git diff --exit-code -- go.mod go.sum
```

Expected: PASS; module changes contain the pinned tray candidate and no replacement.

**Step 4:** Execute the design's two-output Niri matrix. Record exact app fixtures and restart direction.
Any wire change requires a new service candidate and pin.

**Step 5:** Commit `test(shell): qualify tray presentation`.

---

### Task 9: Milestone 5 combined gate

**Files:**
- Create: `docs/plans/2026-08-31-milestone-5-completion-handover.md`
- Modify: `tests/integration/README.md`

**Step 1:** Run notify and tray together for the combined live matrix: center plus drawer root
replacement, tray menu from drawer, tooltip closure before roots, inline reply replacing a menu, both
services restarting independently, shell restart, output hotplug, mixed scale/transform, and 60 minutes
idle.

**Step 2:** Rerun `go test -race -count=1 ./...`, `go vet ./...`, and the fake-compositor suite from
a clean checkout.

**Step 3:** Record shell commit, service candidate tags, test output, live observations, defects, and
stable-tag authorization in the completion handover.

**Step 4:** Commit `docs: record milestone 5 qualification`.
