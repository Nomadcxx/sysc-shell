# Connectivity and Media: where the service, the widget, and the page each live

Date: 2026-09-06. Status: research note, uncommitted and unregistered.

Scope: `sysc-157` (NetworkManager), `sysc-155` (BlueZ), `sysc-156` (MPRIS), and
the standalone bar widgets currently pooled in `sysc-103`. Written while the
`sysc-158` design is in flight; it does not modify that design.

## The question

Each of `sysc-155/156/157` is worded as one slice: "service **and** page",
gated on `sysc-154` (the control-centre spine). The bar widgets for the same
three domains have no issue of their own — they sit inside `sysc-103`, a P2
catch-all ("clipboard, bluetooth, wifi, notifications and volume"), which is
already stale: `internal/shell/notifywidget.go` landed.

So today the tracker says the Wi-Fi bar widget is blocked on the control
centre. Nothing in either reference shell justifies that.

## What both references actually do

### Noctalia v5 (`/home/nomadx/noctalia`)

The service is a peer of the shell, not a part of it:

```
src/dbus/network/     inetwork_service.h  (abstract)   118
                      network_manager_service.cpp     2388
                      wpa_supplicant_service.cpp       506
                      iwd_service.cpp                  424
                      network_secret_agent.cpp         218
                      network_glyphs.cpp                60
src/dbus/bluetooth/   bluetooth_service.cpp            924
                      bluetooth_agent.cpp              301
src/dbus/mpris/       mpris_service.cpp               2773
                      mpris_art.cpp                    176
```

Consumers of each service header, outside `src/dbus`:

| Service | UI consumers |
|---|---|
| `inetwork_service` | `bar/widgets/network_widget`, `control_center/tabs/network_tab`, `control_center/shortcut_registry`, plus `app/application_{services,events,ipc,ui}` |
| `bluetooth_service` | `bar/widgets/bluetooth_widget`, `control_center/tabs/bluetooth_tab`, `shortcut_registry`, same `app/*` |
| `mpris_service` | `bar/widgets/media_widget`, `bar/widgets/plugin_widget`, `control_center/{control_center_panel, tabs/media_tab, tabs/audio_tab, tabs/home_tab}`, `desktop/widgets/desktop_media_player_widget`, `osd/media_osd`, `lockscreen/lock_surface`, `shortcut_registry` — **eight** |

The widget includes the service header. The tab includes the service header.
**Neither includes the other.** Media proves the point: eight independent
consumers, no consumer-to-consumer edge anywhere.

Shared *presentation* helpers live with the service, not in either consumer:
`network_glyphs.h` (which glyph a signal strength and link type mean) and
`mpris_art.h` (album-art fetch and decode) are both included by widget *and*
tab. That is what keeps the bar icon and the page icon in agreement without
coupling them.

Sizes show the division of labour, not a shrunken copy:

| Domain | bar widget | control-centre tab |
|---|---|---|
| network | 410 | 973 |
| media | 312 | 1003 |

### What the Noctalia widget is *for*

`src/shell/bar/widget_gesture_defaults.cpp` is the whole answer, declaratively:

```
bluetooth  Left: panel-toggle control-center bluetooth   Right: bluetooth-toggle
network    Left: panel-toggle control-center network     Right: network-toggle
media      Left: panel-toggle control-center media       Right: media toggle
                                                          Back/Forward: media previous/next
                                                          Scroll: media next/previous
```

The bar widget owns **no popup of its own**. It is a status glyph plus direct
actions; the deep UI exists once, in the control centre, and the widget is a
route to it. Only clipboard, launcher, session and wallpaper keep private
panels. Even the dead-zone right-click opens the control centre.

### DankMaterialShell (`/home/nomadx/Documents/GitHub/DankMaterialShell`)

Services are `pragma Singleton` QML in `Services/`, read by anything:

```
NetworkService.qml            236   facade
  NetworkManagerService.qml   516   D-Bus backend, via DMS_SOCKET
  LegacyNetworkService.qml   1093   nmcli backend
BluetoothService.qml          485
MprisController.qml            60
VpnService.qml                246
```

DMS goes *further* than Noctalia on widget minimalism: `Modules/DankBar/Widgets/`
has **no** Bluetooth widget and **no** Wi-Fi widget. One
`ControlCenterButton.qml` (266 lines) paints network + bluetooth + audio +
battery glyphs in a single capsule and opens the control centre. Standalone
widgets exist only where the domain has a job the control centre does not do:
`NetworkMonitor` (throughput), `Vpn` (+ its own `VpnPopout`), `Media`.

The control centre itself is pill-plus-inline-detail rather than a tab rail:
`ControlCenter/Widgets/*Pill.qml` → `ControlCenter/Details/*Detail.qml`
(`NetworkDetail` 453, `BluetoothDetail` 524), hosted by `Components/DetailHost.qml`
over a user-rearrangeable `Models/WidgetModel.qml` grid.

### The one seam both projects invented independently

Network is a **façade over swappable backends** in both:

- Noctalia: `INetworkService` abstract base — "UI code should use this type so
  it works with any backend" — implemented by NetworkManager, wpa_supplicant
  and iwd, each with its own secret agent.
- DMS: `NetworkService.qml` picks `NetworkManagerService` or
  `LegacyNetworkService` at runtime from capability detection, and re-points
  `activeService` if capability appears later.

Two projects, different languages, same seam. Treat it as a requirement of the
domain, not a coincidence. Neither Bluetooth nor MPRIS has it — BlueZ and the
MPRIS spec are each the only game in town.

## How this lands on `sysc-shell`

The house pattern already matches the references. `internal/services/audio.go`
is `Available() / State() / Changes() <-chan / Acquire() *Lease`, owned once by
the `Registry`, fanned out to a keybind action path (`registry.go:260-283`) and
an OSD relay (`relayAudioOSD`, `registry.go:434`) — two consumers, no bar
widget yet, and the control-centre Audio page will be a third. That is exactly
Noctalia's `mpris_service` shape at smaller scale.

So nothing new is needed architecturally. What is wrong is the **slicing in the
tracker**, in three specific ways.

### 1. Service and page are one issue; they should be two

`sysc-157` reads "Network service and page" and depends on `sysc-154`. That
makes the NetworkManager backend look like a part of the control centre. It is
not: it is `internal/services/network.go`, a peer of `audio.go`, and it has no
dependency on the spine at all. Only the *page* does.

Proposed shape per domain:

```
<new> internal/services/<domain>.go        deps: its own approved design
  ├── sysc-157/155/156  control-centre page   deps: service, sysc-154
  └── <new> bar widget                        deps: service
```

The widget issue then does not depend on `sysc-154`, which is the whole point —
`bd ready` starts telling the truth about what a second pair of hands could
pick up while the spine is being built.

### 2. Widget scope is not "the page, but smaller"

From the gesture table above, the standalone widget is: one glyph, an optional
label, a direct action on right-click/scroll, and left-click routes to the
control-centre section. No device list, no AP picker, no pairing flow — those
exist once, in the page. Write the widget issues to that scope explicitly or
they will grow a second device picker.

### 3. `network` is already taken

`internal/shell/widget.go:184` groups `"network"` with `cpu`, `memory`,
`filesystem`, `block` — it is the throughput metric, DMS's `NetworkMonitor`.
The connectivity widget needs a different type name (`wifi`, or `connectivity`
if wired belongs in the same capsule). Deciding this late means a config
migration.

## One item that is time-critical

The in-flight design's D6 adds an IPC `section` param
(`panel.open {"panel":"control-center","section":"audio"}`). Noctalia's widget
gestures are the *same* string
(`panel-toggle control-center network`) — one grammar serving IPC, keybinds and
bar clicks alike.

`sysc-shell` cannot express it yet. Bar actions are flat strings dispatched in
`bindBarPanelActionsLocked` (`registry.go:584-616`) into
`TogglePanel(PanelID, out, trig)`, which takes no section. The notifications
right-click case shows what the workaround costs: open the panel, take the
registry lock, poke `h.notifyMenu`, rebuild.

If `TogglePanel` grows the section parameter while D6 is being written, the
three widgets later cost a glyph and a case each. If it does not, each widget
repeats the notifications workaround. This is worth raising with the session
holding `sysc-158` now, not after it closes.

## Reading list for the three designs

- Network ownership and secret agent: `noctalia/src/dbus/network/inetwork_service.h`
  (the contract), `network_secret_agent.cpp` (218 lines — the trust boundary),
  `network_glyphs.cpp` (shared icon vocabulary).
- Bluetooth pairing and trust: `noctalia/src/dbus/bluetooth/bluetooth_agent.cpp`
  (301 lines), and DMS `Modules/ControlCenter/Details/BluetoothDetail.qml`
  (524) for the interaction shape.
- MPRIS: `noctalia/src/dbus/mpris/mpris_service.cpp` for discovery and
  active-player selection, `mpris_art.cpp` for art off the paint path, and the
  eight-consumer list above as the argument for putting position interpolation
  in the service rather than in each consumer.
