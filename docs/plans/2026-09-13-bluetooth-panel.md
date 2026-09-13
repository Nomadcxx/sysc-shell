# Bluetooth Panel Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Ship `sysc-155`: one Registry-owned BlueZ client driving the `bluetooth` bar widget, standalone `PanelBluetooth`, and a functional control-centre Bluetooth page.

**Architecture:** BlueZ state is reduced from ObjectManager and property/name-owner signals into immutable Go snapshots. The Registry owns the service and relays snapshots to both hosts. The standalone panel and control-centre page call one shared `bluetoothBody`; neither host creates a service or maintains a second device list. Pairing uses one shell-owned `KeyboardDisplay` agent with one bounded prompt slot.

**Tech Stack:** Go, `github.com/godbus/dbus/v5 v5.2.2`, the existing retained UI, `PanelHost`, `scheduleControl`, Niri IPC, and the system BlueZ service.

---

## Scope and ordering

The approved contract is [2026-09-13-bluetooth-panel-design.md](2026-09-13-bluetooth-panel-design.md). Work starts only after `sysc-157` lands in the primary checkout and is rebased onto that result. The control-centre host is a required consumer, not optional polish: `sysc-155` remains open until the standalone panel, bar, and enabled control-centre page all use the same state, actions, prompt, discovery lifetime, and errors.

Do not add a Bluetooth subprocess, a second service for the control centre, a generic D-Bus abstraction, a new UI node kind, profile controls, OBEX, GATT, or multi-adapter UI. Keep `sysc-154`/`sysc-253` as existing control-centre prerequisites; do not make the backend depend on a panel being open.

Run `bd` only from `/home/nomadx/sysc-shell`. Status belongs in bd, not this document. Do not add Markdown task checkboxes.

## Task 0: Reconcile the post-network tree

**Files:** None.

1. Confirm `sysc-157` is landed, inspect `git status`, and rebase the dedicated Bluetooth worktree onto current `main`.
2. Confirm `godbus/dbus/v5 v5.2.2` is available from the network branch. Add the pin only if it is absent; do not add a second dependency.
3. Claim `sysc-155` with `bd update sysc-155 --status in_progress` from the primary checkout.
4. Run the existing focused service, shell, and control-centre tests before editing. Record any baseline failure in bd.

## Task 1: Define Bluetooth snapshots and the pure reducer

**Files:**

- Create: `internal/services/bluetooth.go`
- Create: `internal/services/bluetooth_test.go`

Write the smallest pure model for adapter state, device state, prompt state, actions, and opaque `DeviceID`/prompt generation. Add managed-object reduction for `Adapter1`, `Device1`, optional `Battery1`, property invalidation, interface removal, deterministic first-adapter selection, three device sections, and stable RSSI-band sorting.

The test must cover adapter selection, paired/connected/available classification, invalidated battery/RSSI values, device removal, BlueZ loss, restart reconstruction, and stale device IDs. No D-Bus call belongs in these tests.

Run: `go test ./internal/services -run Bluetooth -count=1`.

## Task 2: Implement the single-slot pairing agent logic

**Files:**

- Modify: `internal/services/bluetooth.go`
- Modify: `internal/services/bluetooth_test.go`

Implement the exported `KeyboardDisplay` methods and the one prompt slot: PIN/passkey validation, display-only requests, confirmation and authorization, generation matching, duplicate rejection, reply-once behavior, 60-second cancellation, BlueZ-loss rejection, and fail-closed shutdown. Clear credential input on every terminal path and never include it in logs or error labels.

Call the Agent1 methods directly in tests. Cover every request kind, malformed input, a second simultaneous request, timeout, cancel, late response, and shutdown.

Run: `go test ./internal/services -run 'Bluetooth|Pairing|Agent' -count=1`.

## Task 3: Connect the service to BlueZ

**Files:**

- Modify: `internal/services/bluetooth.go`
- Modify: `internal/services/bluetooth_test.go`
- Modify: `go.mod`, `go.sum` only if the existing pin is missing

Use one system-bus connection with ObjectManager discovery, Properties reads, `InterfacesAdded`, `InterfacesRemoved`, `PropertiesChanged`, and `NameOwnerChanged` subscriptions. Register the shell agent and request the default-agent role. Implement power, discovery, pair, connect, disconnect, trust, forget, response, cancellation, restart recovery, and close.

Keep D-Bus calls off the Registry lock and Wayland owner. Resolve every mutating `DeviceID` against the current snapshot before calling BlueZ. On daemon loss, publish unavailable state and reject pending prompts; on return, rebuild from `GetManagedObjects` without polling.

Use a narrow internal bus seam only where it makes signal and method tests possible; do not expose a general D-Bus package. Test with fakes for parsing/reduction and reserve the live bus for the final gate.

Run: `go test ./internal/services -run Bluetooth -count=1` and `go vet ./internal/services`.

## Task 4: Give the Registry one service and one relay

**Files:**

- Modify: `internal/shell/registry.go`
- Modify: `internal/shell/registry_close_test.go`
- Create or modify: `internal/shell/bluetooth_registry_test.go`

Construct one `services.Bluetooth` for the Registry lifetime, relay immutable changes under `Registry.mu`, invalidate visible bars and panels, and close the service during `Registry.Close`. Add the existing off-owner command path for Bluetooth actions and transient per-device errors/actions without holding `Registry.mu` across D-Bus calls.

Test service replacement/close, relay invalidation, action deduplication, stale IDs, and that a D-Bus failure leaves the last valid snapshot visible with an inline error.

## Task 5: Add the icon/config surface and thin bar widget

**Files:**

- Modify: `internal/render/materialfont.go`
- Modify: `internal/render/icons/material/build.py`
- Modify: `internal/config/config.go`
- Create: `internal/shell/bluetoothwidget.go`
- Create: `internal/shell/bluetoothwidget_test.go`
- Modify: the default bar construction file selected by the post-network tree

Union the Bluetooth glyphs with the network branch inventory: `bluetooth_disabled`, `bluetooth_connected`, `keyboard`, `mouse`, `smartphone`, `speaker`, and `devices_other`. Keep the generated font, name map, and build input in sync.

Register configuration ID `bluetooth`. The widget exposes unavailable/off, powered-idle, and connected glyph states; left-click toggles `PanelBluetooth`; right-click opens `PanelControlCenter` with section `bluetooth`. It never owns a device list or hidden disconnect action.

Test glyph selection, config presence, left-click routing, right-click section routing, and the no-device/connected states.

## Task 6: Build the shared Bluetooth body

**Files:**

- Create: `internal/shell/bluetoothbody.go`
- Create: `internal/shell/bluetoothbody_test.go`

Build `bluetoothBody` from the cached snapshot and host width. It owns the adapter power/scan controls, pairing card, Connected/Paired/Available sections, explicit Pair/Connect/Disconnect actions, inline details, Trusted toggle, confirmed Forget row, empty/unavailable/error states, and accessible names.

The body must accept host state and action callbacks but must not own a service, D-Bus object, discovery lease, or standalone-panel chrome. Tests must prove both hosts receive the same section rows and action names from the same snapshot, including prompt insertion and credential clearing.

## Task 7: Add the standalone panel and discovery lifecycle

**Files:**

- Create: `internal/shell/popout_bluetooth.go`
- Modify: `internal/shell/panel.go`
- Modify: `internal/shell/panelhost.go`
- Create or modify: `internal/shell/popout_bluetooth_test.go`

Register `PanelBluetooth` at 460x560 with the existing root/IPC machinery. Wrap `bluetoothBody` with only the standalone title, close control, placement, and panel geometry. Opening starts this client’s discovery session when powered; closing, root replacement, power-off, or leaving the body stops only that session.

When a prompt arrives without a visible Bluetooth host, open this panel on the focused output. If the control centre Bluetooth page is visible, keep the prompt there. Test panel identity, IPC dispatch, geometry, root replacement, prompt auto-open, and discovery start/stop.

## Task 8: Make the control-centre Bluetooth page functional

**Files:**

- Modify: `internal/shell/popout_controlcenter.go`
- Modify: `internal/shell/controlcenter_pages.go`
- Modify: `internal/shell/controlcenter_test.go`
- Modify: `internal/shell/panelhost.go` if section/root transitions require it

Enable the Bluetooth rail destination only when its page dispatch and shared body are wired. Route the page to the same `bluetoothBody`, Registry action handler, prompt state, discovery lifecycle, and error projection used by `PanelBluetooth`. Preserve the control-centre’s 700x564 attached chrome, rail, header, and viewport; do not embed the standalone panel.

Test that right-click from the bar opens the control centre directly on Bluetooth, the rail can enter and leave Bluetooth, the page shows the same snapshot as the standalone panel, actions reach the same Registry methods, and leaving the section stops this shell’s scan. This task is mandatory for `sysc-155` completion.

## Task 9: Complete cross-host action and pairing behavior

**Files:**

- Modify: the Bluetooth service, Registry, body, standalone panel, and control-centre files as needed
- Extend: corresponding focused tests

Exercise the full shared path: power, scan, Pair, Connect, Disconnect, Trusted, confirmed Forget, PIN/passkey/confirmation/authorization prompts, error retry, BlueZ restart, and root switching between the two hosts. Ensure only one visible prompt and one discovery session exist, and no action waits under `Registry.mu`.

Run focused package tests plus `go test -race -count=1 ./internal/services ./internal/shell`.

## Task 10: Repository and live gates

**Files:**

- Create: `docs/plans/2026-09-13-bluetooth-panel-completion-handover.md` only after implementation is complete
- Modify: `.beads/issues.jsonl` with the final bd state in the implementation commit

Run the repository gates required by `AGENTS.md`: `gofmt -w .`, `test -z "$(gofmt -l .)"`, `go vet ./...`, `go test -race -count=1 ./...`, and `git diff --exit-code -- go.mod go.sum` (use the repository’s documented per-package substitute if the machine cannot run the full race gate).

For the live gate, export the Niri environment, deploy one exact build, and use `niri msg -j layers` plus the safe BlueZ matrix. Verify bar states, both click routes, identical state in both hosts, discovery lifetime, connect/disconnect on an approved paired device, and the full pairing/trust/reconnect/Forget flow only on an approved disposable device. Do not restart BlueZ, forget existing devices, or disturb an active connection to manufacture a failure.

Close `sysc-155` only when the service, bar, standalone panel, and control-centre page are all functional and the completion handover records automated and live evidence.
