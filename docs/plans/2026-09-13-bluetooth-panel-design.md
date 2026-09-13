# Bluetooth panel, widget, and control-centre page design

Date: 2026-09-13. Owner-approved in brainstorming on 2026-09-13.

This design covers `sysc-155`: a Registry-owned BlueZ service, the
`bluetooth` bar widget, a standalone `PanelBluetooth`, and the control-centre
Bluetooth page. The two surfaces share one complete device-management body.

The implementation starts after the in-flight network branch lands. Both
features change Registry wiring, panel dispatch, control-centre navigation,
and the embedded Material glyph inventory.

Sources read as behaviour and architecture references only:

- `docs/plans/2026-09-06-connectivity-and-media-prior-art.md`
- `docs/plans/2026-09-11-network-panel-design.md`
- `internal/services/audio.go`, `internal/shell/registry.go`, and
  `internal/shell/popout_audio.go`
- `internal/shell/panel.go`, `panelhost.go`,
  `popout_controlcenter.go`, and `controlcenter_pages.go`
- `internal/render/materialfont.go` and
  `internal/render/icons/material/build.py`
- `/home/nomadx/noctalia/src/dbus/bluetooth/bluetooth_service.{h,cpp}`
- `/home/nomadx/noctalia/src/dbus/bluetooth/bluetooth_agent.{h,cpp}`
- `/home/nomadx/noctalia/src/shell/control_center/tabs/bluetooth_tab.cpp`
- `/home/nomadx/noctalia/src/shell/bar/widgets/bluetooth_widget.cpp`
- `/home/nomadx/Documents/GitHub/DankMaterialShell/Modules/ControlCenter/Details/BluetoothDetail.qml`

## Goal and scope

In scope:

- One event-driven BlueZ client on the system bus, owned for the Registry's
  lifetime.
- Adapter power and discovery controls.
- Device discovery, pairing, trust, connect, disconnect, and forget.
- BlueZ's complete `KeyboardDisplay` pairing-agent flow.
- A thin `bluetooth` bar widget.
- A 460x560 standalone Bluetooth panel.
- A functional control-centre Bluetooth destination.
- One shared body for adapter controls, pairing prompts, device sections, and
  inline device details.

Out of scope:

- File transfer, OBEX, tethering, PAN configuration, audio profiles, codecs,
  GATT characteristic browsing, and device renaming.
- A background reconnection policy. BlueZ and each device profile own
  reconnection; this shell only sets BlueZ's `Trusted` property.
- Multiple-adapter selection UI. The service selects one adapter
  deterministically until a second real adapter creates a product need.
- A subprocess backend around `bluetoothctl`, `busctl`, or Blueman.
- A new modal, list-row primitive, or general D-Bus framework.

## Approaches considered

The approved approach uses one Registry service and one shared body. The bar,
standalone panel, and control-centre page read the same cached snapshot. This
matches the current ownership model and gives incoming pairing requests one
place to land.

A control-centre-only service was rejected. It would make Bluetooth state
disappear whenever the page closed and leave the bar widget and standalone
panel needing a second owner.

Wrapping `bluetoothctl` or delegating to Blueman was rejected. Neither option
can provide a reliable in-process pairing prompt, immutable snapshots, or
signal-driven device state. The shell would also lose ownership of error and
credential handling.

## Decisions

### D1: Registry owns one BlueZ service

`internal/services/bluetooth.go` owns a private system-bus connection for the
Registry's lifetime. A panel or widget never constructs it. The service
connects once, reads `GetManagedObjects`, installs signal matches, and keeps a
plain Go snapshot current.

The public surface stays small:

```go
func (b *Bluetooth) Available() bool
func (b *Bluetooth) CachedState() BluetoothState
func (b *Bluetooth) Changes() <-chan BluetoothState
func (b *Bluetooth) SetPowered(bool) error
func (b *Bluetooth) StartDiscovery() error
func (b *Bluetooth) StopDiscovery() error
func (b *Bluetooth) Pair(DeviceID) error
func (b *Bluetooth) Connect(DeviceID) error
func (b *Bluetooth) Disconnect(DeviceID) error
func (b *Bluetooth) SetTrusted(DeviceID, bool) error
func (b *Bluetooth) Forget(DeviceID) error
func (b *Bluetooth) Respond(PromptID, PairingResponse) error
func (b *Bluetooth) CancelPrompt(PromptID) error
func (b *Bluetooth) Close() error
```

The signatures are a contract sketch rather than permission to add an
interface around the service. `CachedState` performs no D-Bus work. UI code can
call it while it holds `Registry.mu`.

The service continuously subscribes because the bar needs Bluetooth state even
when no panel is visible. It does not copy the interval-shaped `leaseSet` from
polled services.

### D2: Use BlueZ directly through the existing Go D-Bus dependency

The network branch already selects `github.com/godbus/dbus/v5 v5.2.2`. BlueZ's
client surface here is four stable interfaces:

- `org.freedesktop.DBus.ObjectManager`
- `org.freedesktop.DBus.Properties`
- `org.bluez.Adapter1` and `org.bluez.Device1`
- `org.bluez.AgentManager1` and the exported `org.bluez.Agent1`

No cached Go BlueZ binding covers both ObjectManager discovery and the agent
export. Writing the narrow calls directly on `godbus/v5` adds no dependency and
keeps the trust boundary visible.

If Bluetooth lands before the network branch despite the ordering gate, it pins
the same `godbus/dbus/v5 v5.2.2` version explicitly.

### D3: ObjectManager and signals are the state source

The service calls `GetManagedObjects` at `/`, then consumes:

- `InterfacesAdded`
- `InterfacesRemoved`
- `PropertiesChanged`
- `NameOwnerChanged` for `org.bluez`

It parses `Adapter1`, `Device1`, and optional `Battery1` properties. Invalidated
properties clear the cached value instead of leaving stale battery or RSSI
data. A cap-one change channel coalesces bursts from discovery.

The snapshot contains no `dbus.Variant` or object wrapper. It carries adapter
availability, power, pairability, discovery, pairing-agent readiness, the
active prompt, and copied device values: ID, alias, address, BlueZ icon hint,
paired, trusted, connected, current action, optional battery percentage, and
optional RSSI.

`DeviceID` is an opaque service value. Consumers do not construct object paths.
Every mutating method resolves the ID against the current snapshot before it
calls BlueZ, so stale UI actions fail without targeting another object.

The service chooses the lexicographically first `Adapter1` path and rebuilds
the device projection if that adapter disappears. The implementation marks the
ceiling in code:

```go
// ponytail: one adapter matches the shipped host; add adapter selection when a second real consumer needs it.
```

### D4: Cached state and D-Bus calls never hold the Registry lock together

The BlueZ signal loop owns D-Bus event reduction. It publishes immutable state
copies. A Registry relay reads the change channel, takes `Registry.mu`, updates
the cached view, rebuilds visible Bluetooth content, and invalidates the bars.

UI handlers use the existing off-owner control path. They record a local
per-device action such as pairing, connecting, or disconnecting, release
`Registry.mu`, call BlueZ, then rebuild with either the signalled state or the
returned error. Duplicate actions for one device fail fast.

The local action field supplies the connecting state because `Device1` exposes
`Connected` but no `Connecting` property. BlueZ property signals remain the
source of the final state.

No code waits for D-Bus while it holds the service state lock, `Registry.mu`,
or the Wayland dispatch goroutine.

### D5: The shell registers a full `KeyboardDisplay` agent

The service exports one object at a shell-owned object path, registers it with
`AgentManager1.RegisterAgent`, then calls `RequestDefaultAgent`. It implements:

- `RequestPinCode`
- `DisplayPinCode`
- `RequestPasskey`
- `DisplayPasskey`
- `RequestConfirmation`
- `RequestAuthorization`
- `AuthorizeService`
- `Cancel`
- `Release`

PIN input accepts 1 to 16 characters. Passkey input accepts a number from 0 to
999999 and displays it as six digits. Confirmation and authorization default
to rejection. Service authorization names the device and shows the UUID; the
first implementation does not carry a speculative service-name catalogue.

Blocking requests occupy one prompt slot. A concurrent blocking request gets
`org.bluez.Error.Rejected`. Each prompt has an opaque generation ID, and a UI
response must match it. This prevents a late click from answering a newer
request. The slot replies exactly once on submit, rejection, timeout, BlueZ
loss, panel dismissal, or shell shutdown.

Display methods update the visible code and entered-digit count, acknowledge
the D-Bus notification, and remain visible until BlueZ cancels or the pairing
operation completes. `Cancel` and `Release` clear the prompt.

A blocking prompt waits for at most 60 seconds. The duration is a package
constant, not configuration. A timed-out request returns
`org.bluez.Error.Canceled` and clears the UI.

When a prompt arrives with no Bluetooth body visible, the Registry opens
`PanelBluetooth` on the focused output. If the control-centre Bluetooth page is
already visible, the request stays there. Incoming requests can therefore be
answered without a new modal or automatic authorization.

The shell does not kill or reconfigure Blueman. If `RequestDefaultAgent` fails
because another agent owns the role or policy rejects the request, device
status and connect controls remain available. Pair controls disable and the UI
states that pairing prompts are unavailable.

`Close` stops discovery started by this connection, rejects any pending
request, unregisters the agent, removes signal matches, and closes the owned
bus connection.

### D6: Pairing, trust, and removal fail safely

An unpaired row exposes Pair. A paired disconnected row exposes Connect. A
connected row exposes Disconnect. The row does not hide these actions behind a
whole-row gesture.

Successful pairing sets `Trusted=true`. If trust fails after BlueZ has paired
the device, the service keeps the paired device and reports the trust failure;
it does not remove a valid pairing as rollback. The detail toggle lets the user
change `Trusted` later. Its caption explains that trust allows service
authorization and normal reconnect behaviour without claiming that the shell
runs a reconnection engine.

Forget calls `Adapter1.RemoveDevice`. The inline detail first changes to a
confirmation row, so one stray click cannot remove keys and trust state. No
existing device is forgotten during a live gate without the owner's explicit
choice.

Closing the panel while a blocking prompt is visible rejects that prompt. A
display-only code closes with the pairing operation. The service clears PIN and
passkey input after submit, cancel, timeout, host replacement, and shutdown.
Credentials never enter logs, error labels, retained trees after dismissal, or
process arguments.

### D7: BlueZ restarts do not require a shell restart

When `org.bluez` loses its owner, the service marks itself unavailable, clears
device and adapter state, rejects pending prompts, and disables controls. It
keeps the system-bus connection and `NameOwnerChanged` match.

When BlueZ returns, the service calls `GetManagedObjects`, restores the signal
matches and agent registration, then publishes the rebuilt snapshot. It does
not poll for recovery.

Method errors use their BlueZ message when it is safe and useful. The UI keeps
the last valid cached state, clears the local action, and shows an inline error.
A retry button appears only for an operation the user can repeat safely.

### D8: One shared device-management body serves both surfaces

`bluetoothBody` builds the adapter controls, pairing card, device sections, and
expanded detail from a cached `BluetoothState`. It accepts the available body
width and host state, but owns no surface.

`PanelBluetooth` wraps that body in its own title, close button, placement, and
460x560 geometry. The control centre keeps its 700x564 attached chrome, rail,
page header, and viewport. Its Bluetooth page returns the same body function.
Both hosts route that body's actions through the same Registry handler and
off-owner service calls. There is no second list implementation, duplicated
control path, or embedding of one panel surface inside another.

The current panel machinery permits one process-wide interactive root. Opening
the control centre closes the standalone panel and vice versa, which also makes
one pairing prompt and one discovery session sufficient.

### D9: Adapter and discovery controls follow the visible body

The top card contains a Bluetooth power toggle and a Scan/Stop control. Power
off disables discovery and device actions while retaining the paired-device
projection BlueZ still provides.

Opening either Bluetooth body starts discovery when the adapter is powered.
Leaving the control-centre section, closing the panel, replacing the root, or
powering off stops only the discovery session started by this bus connection.
BlueZ reference-counts discovery requests from different clients, so this does
not cancel another application's scan.

The user can stop and restart scanning while the body remains open. A manual
stop lasts until that body closes; the next open starts a fresh scan. Discovery
method failures leave the view usable and report the error inline.

### D10: Device sections and rows are stable under radio noise

The body presents three sections:

1. Connected
2. Paired, excluding connected devices
3. Available, excluding paired devices

Empty sections stay out of the tree. A no-results message appears only after a
scan has produced no available devices. Adapter-off and BlueZ-unavailable
states get explicit messages.

Each row shows a device-type icon, alias with address fallback, optional
battery, optional RSSI, current action, and its explicit Pair, Connect, or
Disconnect control. Connected and Paired sort by case-folded alias, then
address. Available devices sort by coarse RSSI band, then alias and address, so
small signal changes do not move a row under the pointer.

BlueZ icon hints map onto the smallest useful Material set:

| BlueZ hint | Material icon |
|---|---|
| headset/headphones | existing `headphones` |
| computer | existing `desktop_windows` |
| keyboard | `keyboard` |
| mouse | `mouse` |
| phone | `smartphone` |
| speaker/audio card | `speaker` |
| anything else | `devices_other` |

An inline Details affordance expands address, battery, RSSI, the Trusted
toggle, and Forget confirmation. It uses existing rows, buttons, toggles, and
text. The design adds no disclosure or modal node kind.

### D11: Pairing prompts live at the top of the body

The active pairing card precedes device sections and resets the body viewport
to the top when it appears. It shows one of:

- PIN entry
- passkey entry with six-digit numeric input
- displayed PIN or passkey with entered-digit progress
- numeric confirmation
- incoming device authorization
- service authorization with UUID

The input cases offer Submit and Cancel. Authorization cases offer Allow and
Reject. Display-only cases explain that the code must be entered or checked on
the other device. All actions carry accessible names, and every decision stays
visible in text as well as colour or icon state. Codes use tabular text.

### D12: The bar widget stays thin

The configuration ID is `bluetooth`. Its glyph has three settled states:

| State | Glyph |
|---|---|
| unavailable or powered off | `bluetooth_disabled` |
| powered, no connected device | existing `bluetooth` |
| one or more connected devices | `bluetooth_connected` |

Discovery does not animate the bar or add a badge. The panel itself already
shows that state.

Left-click toggles `PanelBluetooth`. Right-click opens
`PanelControlCenter` with section `bluetooth`, using the section-aware IPC path
already in `PanelHost`. Adapter power remains an explicit toggle in the body;
the bar carries no hidden disconnect-all gesture.

The Material subset adds seven names:

`bluetooth_disabled`, `bluetooth_connected`, `keyboard`, `mouse`,
`smartphone`, `speaker`, and `devices_other`.

The implementation unions these with the network branch's glyph additions and
regenerates the font from the pinned upstream file. It updates
`materialIcons` and `build.py` together.

### D13: Control-centre dependency remains real

`sysc-155` continues to depend on the control-centre spine. The service and bar
could exist without it, but this approved tranche includes the functional
control-centre page. Removing that dependency would let bd report the whole
issue ready while one required host remained unavailable.

The implementation changes the Bluetooth `ccSection` to enabled only when its
page dispatch and shared body are wired. It does not create a temporary page or
close the issue after the standalone panel alone works.

`sysc-155` keeps all four deliverables in one issue. Splitting service, widget,
and two hosts would add tracking work without creating independent landing
units because they share Registry and glyph conflicts. The issue gains a
dependency on the in-flight network work as the integration ordering gate.

### D14: Focused tests cover logic; the laptop covers BlueZ

Automated checks cover:

- managed-object parsing, property invalidation, device removal, and BlueZ
  loss/recovery reduction;
- deterministic adapter selection;
- section classification and stable RSSI-band sorting;
- bar glyph selection;
- one action per device and stale `DeviceID` rejection;
- PIN/passkey validation, prompt generation matching, single-slot rejection,
  reply-once, timeout, cancel, and fail-closed shutdown;
- shared-body structure in the standalone and control-centre hosts;
- discovery start/stop on open, section change, root replacement, and close;
- left-click standalone routing and right-click section routing;
- trust and Forget confirmation actions.

Pure parsers and reducers accept property maps in tests. Pairing-slot tests call
the exported Agent1 methods directly. The implementation does not introduce a
single-implementation bus interface only to count calls. The live gate covers
the narrow D-Bus registration and method wrappers.

Code-touching commits run the repository gate from `AGENTS.md`:

```text
gofmt -w .
test -z "$(gofmt -l .)"
go vet ./...
go test -race -count=1 ./...
git diff --exit-code -- go.mod go.sum
```

The live Niri gate checks the three bar states, both click routes, identical
state in both hosts, discovery lifetime, connect and disconnect on an approved
paired device, and the full pairing/trust/reconnect/Forget flow on an approved
disposable device. It also checks the existing Blueman default-agent collision
and uses `niri msg -j layers` for mapped-surface evidence.

The gate does not restart BlueZ, forget existing devices, or disturb an active
connection just to manufacture a failure. Deterministic tests cover daemon
loss. The completion handoff records any pairing method that available hardware
cannot exercise.

## Stop condition

Stop when one Registry service drives the bar, standalone panel, and enabled
control-centre page; every BlueZ agent request receives one bounded response;
the automated gate passes; and the safe live matrix has recorded evidence on
`sysc-155`. Do not pull Bluetooth profiles, OBEX, GATT controls, or
multiple-adapter UI into this tranche.
