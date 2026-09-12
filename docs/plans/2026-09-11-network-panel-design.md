# Network panel — Design

Date: 2026-09-11. Status: approved 2026-09-11 — backend, credential path, and
composition all settled in brainstorming. Composition is **Direction B,
"status first"**, chosen from a three-variant study (D7). Live probes were
offered and declined; D18 records what is therefore unverified.

A first-party `PanelNetwork` — Wi-Fi and Ethernet as two tabs on one surface —
plus the thin bar widget that owns it, over a new event-driven NetworkManager
service.

Composition target: the Noctalia network panel (`noctalia-wifi.png`,
`noctalia-ethernet.png`, analysed 2026-09-11). Behaviour and visual reference
only. No QML, C++ or configuration is imported.

Sources (read, not imported):

- `internal/services/audio.go` — the shipped lease/poll/`wpctl` service whose
  public shape this service copies
- `internal/services/leases.go` — `leaseSet`, interval-oriented (see D3)
- `internal/shell/popout_audio.go` — `KindSegmented` tabs, `scheduleControl`
- `internal/shell/panel.go`, `panelhost.go`, `registry.go`, `widget.go`
- `internal/ui/tree.go`, `internal/ui/textfield.go` — `ui.Field` has no mask
- `internal/render/materialfont.go` — the 25-name hand-cut subset
- `internal/render/icons/material/build.py`, `SOURCE.md` — pinned provenance
- `noctalia/src/dbus/network/` — `inetwork_service.h`, `network_types.h`,
  `network_glyphs.cpp`, `network_secret_agent.h`
- `noctalia/src/shell/control_center/tabs/network_tab.cpp` — password card
- `docs/plans/2026-09-06-connectivity-and-media-prior-art.md` — the slicing note
- `docs/plans/2026-09-07-audio-panel-design.md` — the panel this one mirrors
- Variant study, three directions in live tokens, published 2026-09-11:
  `https://claude.ai/code/artifact/c85bd466-d929-47bd-8c9f-ae9f41b56a49`
  (Direction B approved; A and C retained there as the rejected alternatives)

## Goal and scope

In:

- `services.Network`: an event-driven NetworkManager client over the system
  bus, a peer of `services.Audio`, owned once by `Registry`.
- A registered NetworkManager secret agent, so joining a secured network can
  prompt inside our panel.
- `PanelNetwork`, public IPC name `network`, opened by a bar widget or by IPC.
- Two tabs: **Wi-Fi** (default) and **Ethernet**. Neither leaks into the other.
- One `wifi` bar widget: signal glyph, left-click toggles the panel,
  right-click toggles the radio.
- Password entry: a masked `ui.Field`, a reveal toggle, and the renderer
  support both require.
- Nine Material glyphs added to the embedded subset.

Out:

- VPN. Noctalia's tab carries VPN rows; this panel does not. `NetworkState`
  reserves no VPN fields — adding them later is a service change, not a
  migration.
- Hidden SSIDs, WPA-Enterprise/802.1X, and per-connection IP configuration.
  The secret agent answers PSK requests only (D5).
- The external-IP lookup Noctalia's tab shows. It is a network call on a
  paint-adjacent path and belongs to no tracked issue.
- Throughput plotting. `network` already means the rate metric (D10).
- Any control-centre page. The command centre is a separate product and may
  later become a consumer of this service; it is not a dependency in either
  direction (D17).

## Decisions

### D1 — Service ownership and contract

`internal/services/network.go`, constructed and owned by `Registry` beside the
existing services (`registry.go:159`, `r.setAudio(...)`), never by a panel or a
widget. The public surface copies the house shape so consumers cannot tell a
polled service from a pushed one:

```go
func (n *Network) Available() bool
func (n *Network) State() NetworkState
func (n *Network) CachedState() NetworkState
func (n *Network) AccessPoints() []AccessPoint
func (n *Network) Changes() <-chan NetworkState
func (n *Network) Acquire() (*Lease, error)
```

`CachedState` exists for the same reason as `audio.go`'s: callers holding
`Registry.mu` or the Wayland owner must never trigger I/O.

Types port `noctalia/src/dbus/network/network_types.h`:

```go
type Connectivity uint8 // Unknown, None, Wired, Wireless

type NetworkState struct {
	Kind            Connectivity
	Connected       bool
	Resolving       bool // activating, not yet connected
	WirelessEnabled bool
	Scanning        bool
	SSID            string
	IPv4            string
	Interface       string
	Strength        uint8 // 0..100, Wi-Fi only
}

type AccessPoint struct {
	Path       string
	DevicePath string
	SSID       string
	Strength   uint8
	Secured    bool
	Active     bool
	Saved      bool // a saved profile exists for this SSID
}
```

`Saved` is ours, not Noctalia's: the panel must distinguish "tap to connect"
from "tap to be asked for a password" before it calls anything, and Noctalia
answers that with a separate `hasSavedConnection(ssid)` call per row.

The implementation behind this contract is the pinned binding in D2, not
hand-written D-Bus method calls.

### D2 — A pinned binding behind a test seam

The NetworkManager client is `github.com/Wifx/gonetworkmanager/v2 v2.2.0`
(MIT), pinned by version. It is not hand-written D-Bus code.

`AGENTS.md:15` fixes the order: "existing project code, Go standard library,
native Linux service, **pinned dependency, then new code**." Writing our own
client is the last rung, and a maintained binding that covers the whole client
surface sits above it. `AGENTS.md:24` requires the pin and the reason to be
recorded, which is what this decision is.

It covers everything this design asks of the client side: `GetAllAccessPoints`
and `RequestScan` on `DeviceWireless`, `AccessPoint.GetPropertyStrength` and
the SSID/flags properties, `Settings.ListConnections` for `Saved`,
`ActivateConnection` and `AddAndActivateConnection`,
`SetPropertyWirelessEnabled` for the radio, `Connection.Delete` for forget,
and `DeviceWired` plus `IP4Config` for the Ethernet tab and D9's status block.

Resolved dependency set: `gonetworkmanager/v2 v2.2.0` and
`godbus/dbus/v5 v5.2.2`. It also pulls `golang.org/x/sys`, which this tree
already carries at a higher version. Two facts a builder needs:

- The binding's own `go.mod` names `github.com/google/uuid v1.3.0`, which is
  **not** in the local module cache (v1.6.0 is). Because it is a `go 1.12`
  module its full graph is loaded, so `go mod tidy` under `GOPROXY=off` fails
  with a missing `go.sum` entry until `uuid` is required explicitly at a cached
  version. `tidy` then prunes it again, because no compiled file imports it.
- The binding documents testing against NetworkManager 1.40. This machine runs
  **1.58.1**. A spike built offline and enumerated devices, states and saved
  profiles correctly against 1.58.1, so the gap is recorded rather than feared.

The **unexported** `backend` interface inside `services` survives, now wrapping
the binding rather than raw calls. It is still not the façade the prior art
describes — Noctalia and DMS each grew a swappable-backend seam for iwd and
wpa_supplicant, and we are building neither. It earns its place for one
concrete reason: **a fake backend in tests beats a fake system bus** (D14).
If that justification ever stops being true, the interface should be deleted,
not populated.

### D3 — Event-driven, and `Acquire` means liveness

Verified on this machine: `Device.Wireless.AccessPoints`, `.LastScan`, and
`Device.Managed` are all annotated `emits-change`, and NM exposes the standard
`org.freedesktop.DBus.Properties.PropertiesChanged` signal. The service
therefore subscribes; it does not poll.

`leaseSet` computes the finest interval across consumers, which is meaningless
for a pushed service. `Acquire` keeps its lifecycle role only — the first lease
subscribes, the last release unsubscribes — and the interval is ignored. This
divergence is stated in the file, because silently reusing an interval-shaped
helper for a non-interval purpose is how the next reader is misled.

Signal delivery runs on its own goroutine and coalesces into a cap-1 `changes`
channel, exactly as `audio.go` does, so a burst of scan updates cannot outrun
one paint.

### D4 — The secret agent

We export `/org/freedesktop/NetworkManager/SecretAgent` implementing
`org.freedesktop.NetworkManager.SecretAgent` (`GetSecrets`,
`CancelGetSecrets`, `SaveSecrets`, `DeleteSecrets`), then call
`AgentManager.Register("one.archpcx.sysc-shell")`. Verified present on this
system: `.Register(s)` and `.RegisterWithCapabilities(su)`. Registration needs
no elevated privilege; it is scoped to this user's sessions.

**This is the one piece the binding does not provide**, and the only part of
the service written against `godbus` directly. `gonetworkmanager` exports no
agent: it carries no `AgentManager` or `SecretAgent` code and no D-Bus object
export at all, and its `Connection.GetSecrets` is the client-side *settings*
call, whose own documentation says the user "will never be prompted for secrets
as a result of this request." Reading a stored secret and being asked for one
are opposite directions; only the second is a trust boundary.

Join flow:

1. The user taps a secured access point. The service calls
   `AddAndActivateConnection` (unsaved) or `ActivateConnection` (saved).
2. NM calls our agent's `GetSecrets`.
3. The service raises a pending-secret request; the panel swaps in the
   password card.
4. Submit replies to the deferred call. NM proceeds. The passphrase is stored
   by NM, not by us.

**Single-slot**, copying Noctalia: one prompt in flight. A concurrent
`GetSecrets` is answered `NoSecrets` so NM falls back to its own store rather
than queueing a second prompt behind the first.

**Every prompt must reply.** Closing the panel, cancelling, or replacing the
panel host mid-prompt must answer the deferred call with `UserCanceled`. A
dangling reply hangs NM's activation until its own timeout, which presents to
the user as a join that silently does nothing.

### D5 — Credential handling

Three rules, written here because each is a place a passphrase leaks by
accident rather than by decision:

- The passphrase never enters `h.errLabel`, the retained node tree, or any log
  line. `errLabel` is painted; a credential that reaches it is on screen.
- The passphrase never becomes a process argument. This is the reason the
  `nmcli` backend was rejected for this scope: `nmcli ... password <psk>` is
  readable from `/proc/<pid>/cmdline` by any process on the machine.
- The field is cleared on submit, on cancel, and on panel close — not merely
  hidden.

The agent answers PSK requests only. An 802.1X request is answered `NoSecrets`,
which routes the user to whatever full-featured agent is registered.

### D6 — Writes, and what polkit allows

Mutations follow `scheduleControl` (`popout_audio.go:440`): run off
`Registry.mu` in a goroutine, re-lock, set `errLabel`, rebuild, publish.

Verified with `pkaction` on this machine:

| Action | `implicit active` |
|---|---|
| `org.freedesktop.NetworkManager.enable-disable-wifi` | yes |
| `org.freedesktop.NetworkManager.network-control` | yes |
| `org.freedesktop.NetworkManager.settings.modify.own` | `auth_self_keep` for `any`; active not confirmed (D18) |

So the radio toggle and activation need no prompt. Creating or forgetting a
saved profile may prompt, and the session runs a polkit agent (`polkit-mate-aut`)
to service it. We never invoke `sudo`: elevation is orthogonal to the secret
problem — NM asks a registered agent for the passphrase regardless of our uid —
and a bar panel that prompts for a root password is a worse boundary than an
unprivileged agent.

A polkit denial is an error surfaced in `errLabel`, never a hang.

### D7 — Panel identity and geometry

`PanelNetwork` joins the `PanelID` enum (`panel.go:9`), with a `panelTargetSize`
case and a `panelTree` case beside `PanelAudio`.

Geometry is a fixed `ui.Rect{W: 460, H: 560}` — the notifications panel's
family (416×300) rather than the audio panel's computed third-of-width, because
the content is a list of SSID rows whose comfortable width does not scale with
the output. Confirmed against the variant study at those exact dimensions, so
this is the pixel contract rather than a placeholder.

Composition is **Direction B**. Two alternatives were built and rejected.
Direction A put title, radio and tabs in one header card — closest to
`audioHeaderCard`, and the lowest-risk option — rejected because it answers
"what networks are near me?" before "what am I connected to?". Direction C used
a single-row header with bar-band meters and denser rows, fitting ten networks
where A fits six — rejected because those four extra rows do not pay for the
lost connection detail. The study renders all three in the live palette at
460x560 and is the reference for anything this document leaves unstated.

The panel anchors under its bar glyph through the pattern already shipped for
audio (`registry.go:663`): `trig.AnchorX = bar.actionCenterX(panelWifiAction)`
before `TogglePanel`.

### D8 — Tabs

`ui.KindSegmented` with two `Role: "tab"` segments, driven by a `networkTab`
string on `PanelHost` and `network-tab:<name>` actions — the exact shape of
`audioSegment` (`popout_audio.go:71`). No new node kind.

Wi-Fi is the default: `if h.networkTab == "" { h.networkTab = "wifi" }`, seeded
on open beside the existing `PanelSettings` seeding.

### D9 — The Wi-Fi tab

Top to bottom, per Direction B:

- A header card holding a **status block** above the tabs: the state glyph in a
  `ShapeMedium` well, the active SSID as a title with the interface and signal
  beneath it as a caption, a `KindToggle` for the radio, and the close button.
  Below a one-pixel `outline-variant` rule, three labelled figures in equal
  columns — IPv4, Down, Up — set `Tabular` so they do not jitter as rates
  change. Every figure is a dash when absent, never a zero, per the standing
  rule that an absent metric is not zero.
- The tabs sit under the status block, inside the same card (D8).
- **The connected network stays in the list, but only the status block carries
  the filled highlight**; the active row is marked with a trailing `check`
  alone. The status block and the list would otherwise present the same fact
  twice with equal weight. Removing the active network from the list instead
  was rejected: the list is a picture of what is in range, and one that omits
  the strongest nearby network is a lie about the radio environment. Noctalia
  fills both; we do not.
- The access-point list in a `KindScroll`. Each row is a glyph chosen by signal
  band, the SSID, a `lock` glyph when `Secured`, and a trailing `check` when
  `Active`.
- Rows sort by **signal band, not raw percent**. Noctalia's `wifiSignalBand`
  exists precisely because the raw value jitters on every scan and would
  reorder the list under the user's finger. We port the same five bands
  (≥80, ≥60, ≥35, ≥15, else) and sort on them, ties broken by SSID.
- The password card replaces the list when a secret is pending, carrying the
  SSID in its title, a masked field, a reveal toggle, Connect and Cancel.

States the tab must render, because each is reachable on a real machine and
three were observed here: radio off; hardware absent; soft-blocked; scanning
with no results yet; scan complete with an empty list.

### D10 — The bar widget is `wifi`, not `network`

`network` is already bound in `knownItems` (`config.go:239`) as a rate source
with `rx`/`tx` directions — the throughput metric. The connectivity widget
takes **`wifi`**. Deciding this now is the whole point of the prior art's
warning: choosing it after release would be a config migration.

Gestures, following the notifications and audio widgets:

| Gesture | Effect |
|---|---|
| Left | Toggle `PanelNetwork` |
| Right | Toggle the radio |

The glyph is `network_glyphs.cpp`'s logic ported: wired when the active
connection is wired, otherwise a signal-band Wi-Fi glyph, with distinct glyphs
for radio-off and for enabled-but-disconnected. `barView` gains a
`Network services.NetworkState` field beside `Audio` (`widget.go:39`).

### D11 — Glyphs

The embedded subset is an exact 25-name inventory
(`materialfont.go:23`); a name the font does not carry "would shape to nothing
and paint an invisible control". Nine names are added and the subset re-cut:

`signal_wifi_0_bar`, `network_wifi_1_bar`, `network_wifi_2_bar`,
`network_wifi_3_bar`, `signal_wifi_4_bar`, `wifi_off`, `lan`, and
`visibility` / `visibility_off` for the reveal toggle.

`wifi_off` is the ninth and was added during implementation. The five bands
describe signal strength, and none of them can say "the radio is off" — which
D10 requires to read differently from "on with no association". Eight names
could not express the widget this design asks for.

`build.py` verifies the pinned upstream SHA-256 before reading it. The pinned
file — 15,090,976 bytes, `c4416e02…` — was found in `/tmp`, which is volatile,
and has been copied to
`~/.cache/sysc-shell/fonts/MaterialSymbolsRounded-upstream.ttf`. The re-cut is
therefore offline. The copy in `~/.local/share/fonts` is a **different** cut
and fails the hash check.

`materialIcons` and `build.py`'s `ICONS` are kept in step by hand and asserted
by test; both change in the same commit.

### D12 — Masking

`ui.Field` carries `Text`, `Preedit`, `Cursor`, `Multiline`, `SubmitOnEnter`
and nothing else. Masking is new, in three places:

- `Field.Masked bool`, propagated by `Field.Node` onto the node.
- `Node.Masked bool` in `internal/ui/tree.go`.
- A branch in `paint.go:285` (`ui.KindTextField`) rendering a fixed bullet per
  rune rather than the rune.

Masking is a **render-time** substitution: `Field.Text` stays the real value so
editing, cursor movement and submit are unchanged, and exactly one code path
knows about the disguise. Width must be measured on the masked string, or the
caret drifts from the glyphs.

### D13 — Error surface

One `errLabel` string on the panel host, as audio has. A failed activation, a
polkit denial, a missing device, and a rejected scan all land there. Per D5 it
never carries the passphrase.

### D14 — Testing

Service tests run against a fake `backend` (D2). We do **not** spin a private
bus: the `audio_test.go` trick of shadowing a binary on `PATH` has no D-Bus
equivalent, and a test that needs `NetworkManager` running is a test that fails
on a build machine.

Covered: band sorting stability under jittering strength; `Saved` driving the
connect-versus-prompt decision; the single-slot rejection; cancel-replies-once;
the state machine across radio-off, scanning, and empty-scan.

Panel tests follow the existing popout tests, building the tree from a fixed
state. Note `go test ./internal/shell` terminates the login session on this
machine unless `loginctl` is shadowed — see the standing note before running it.

### D15 — Staging

Three slices, so `bd ready` tells the truth:

1. `services.Network` (read-only state and scan) plus the `wifi` bar widget and
   its glyph. No panel.
2. `PanelNetwork`: both tabs, the AP list, the radio toggle, and connecting to
   a **saved** profile. No password path.
3. The secret agent, the password card, masking (D12), and the reveal toggle.

Slice 2 is useful on its own, and slice 3 is the only slice that carries the
trust boundary — keeping it separate keeps that review small.

### D16 — Tracker

`sysc-157` reads "Control centre Network service and page" and depends on the
spine. Both halves are now wrong: the command centre is a separate product
(D17), and the service was never a part of it. Proposed re-slicing, to be
applied in bd before implementation and not silently:

- `sysc-157` is rewritten to the panel, or closed and replaced by three issues
  matching D15's slices.
- The spine dependency (`sysc-154`/`sysc-253`) is dropped.
- The duplicate spine pair is resolved; `sysc-157` currently depends on both.

### D17 — Relationship to the command centre

The command centre is a separate product, in development. It may later consume
`services.Network` — the service is owned by `Registry` and fanned out, so an
additional consumer costs nothing. It is not a dependency in either direction,
and this panel does not wait on it.

### D18 — Open risks

- **`nm-applet` is running** and probably holds a registered secret agent.
  Multiple agents may register, and which one NM asks is not something
  inspection can settle here: D-Bus returned `Access denied` for introspection
  of its `SecretAgent` path. Our prompt may collide with, or be pre-empted by,
  its GTK dialog. This is the one unresolved design risk; a live probe was
  offered and declined. Settle it at the start of slice 3, before the password
  card is built on an assumption.
- **Wi-Fi is soft-blocked on this machine.** `phy0` reports
  `Soft blocked: yes`, NM reports `WirelessEnabled false` with
  `WirelessHardwareEnabled true`, and `wlan0` sits at `wifi:unavailable` while
  `Managed: true`. The hardware is an RTL8821CE at
  `/org/freedesktop/NetworkManager/Devices/3`. Every Wi-Fi path therefore needs
  an `rfkill unblock wifi` before it can be exercised; the radio-off and
  unavailable states are the ones testable as the machine stands.
- **`settings.modify.own` for an active session is unconfirmed** (D6), so
  whether forgetting a network prompts is unknown.
- **The binding is lightly maintained.** Its own documentation states that it
  has no automated tests, that testing is manual and best-effort, and that
  there is no active development workforce behind it. That is the standing cost
  of D2's pin, and part of why the seam exists: a defect behind our own
  interface can be worked around without forking. Re-read its CHANGELOG before
  moving the pin.
- Two **Wi-Fi** profiles (`LukeAP`, `Orac 15A`) sit among the 14 saved
  connections on this machine, so slice 2's activation path can be tested
  without ever typing a passphrase.

## Verified during design

`godbus/dbus/v5` v5.1.0 and v5.2.2 are in the local module cache. A spike built
offline with `GOPROXY=off` and read `WirelessEnabled: false` from the system
bus, so the dependency and the read path are proven on this machine rather than
assumed.

`gonetworkmanager/v2 v2.2.0` is in the cache too. A second spike built offline
against NetworkManager **1.58.1** — eighteen minor versions past the 1.40 the
binding documents — and reported the truth about this machine:

```
NM 1.58.1  wirelessEnabled=false
  enp7s0   NmDeviceTypeEthernet NmDeviceStateActivated
  wlan0    NmDeviceTypeWifi     NmDeviceStateUnavailable
saved profiles: 14
```

Device enumeration, device typing, state, the radio flag and the settings list
all work at 1.58.1. `wlan0` reporting `Unavailable` is the soft block in D18,
not a binding defect.

Searched and rejected as the client: writing our own D-Bus calls (the charter's
last rung, D2). `BellerophonMobile/gonetworkmanager` is archived and redirects
to the Wifx fork.
