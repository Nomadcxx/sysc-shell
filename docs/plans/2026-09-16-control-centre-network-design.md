# Control Centre Network design

Date: 2026-09-16. Parent commission: `sysc-309`.

## Goal

Enable the existing Network rail destination and route it to the
NetworkManager-backed service and body already used by `PanelNetwork`.

## Existing seams

- `internal/shell/popout_controlcenter.go` lists Network as disabled and
  dispatches no `network` page from `ccPage`.
- `internal/shell/popout_network.go` already builds the status-first header,
  Wi-Fi/Ethernet tabs, access-point rows, password card, and error states.
- `internal/services/network.go` owns one NetworkManager-backed service,
  cached snapshots, access points, signal relay, lease, and credential
  lifetime. `Registry.setNetwork` installs and relays that one service.
- `internal/shell/panelhost.go` already handles network actions, off-owner
  writes through `scheduleControl`, shared close behavior, and panel sizing.

## Decisions

### D1. The Network rail is enabled and the page reuses `networkTree`

Set the Network section enabled and add `case "network": return networkTree(r,
h)` to the Control Centre page dispatch. The same body serves both
`PanelNetwork` and the Control Centre, with the host ID deciding which surface
closes. The body keeps the existing status-first composition, default Wi-Fi
tab, access-point ordering, credential card, and theme/density metrics.

When the service is unavailable, the enabled route renders a truthful
unavailable state. The rail does not disappear or claim that a network is
connected.

### D2. One service and one state path serve both surfaces

The Control Centre uses `Registry.network` and its cached values. It does not
construct a second NetworkManager connection, device model, secret store, or
signal loop. The Control Centre host takes the smallest required network
metrics lease for its status figures and releases it with the host. The
Network service's existing process-wide lease remains the owner of its
NetworkManager watch.

`publishNetworkSnapshot` rebuilds an open Control Centre when its selected
section is Network, as it already does for the standalone Network panel. The
relay copies cached state under `Registry.mu` and performs bus work in the
service goroutine.

### D3. Page transitions preserve credential and focus invariants

Selecting Network can request the existing scan through `scheduleControl`.
Leaving Network clears the host's password buffer and cancels any pending
secret request. Entering it seeds the existing Wi-Fi tab and restores focus
through the normal control-centre rebuild. A page change never leaks a second
panel surface or retains a password in an old host.

The generic `network-close` action closes the owning surface, whether that is
`PanelNetwork` or `PanelControlCenter`. Radio changes, access-point activation,
and secret submission keep the current off-owner write path and error label.

### D4. Existing issue ownership remains intact

Network state, credential handling, and panel behavior stay under
`sysc-157`, `sysc-254`, and `sysc-268`. This design adds the missing consumer
route and lifecycle wiring only. It does not duplicate those issues or add a
second connectivity service.

## Data flow

```text
NetworkManager -> services.Network cache/Changes
               -> Registry.network
               -> PanelNetwork networkTree
               -> Control Centre networkTree when section == network
```

All reads during tree construction use cached state. All mutations run after
the handler releases the Registry lock. A reconnect refreshes both open
surfaces and the bar Wi-Fi widget through the existing relay.

## Focused proof

The executable plan will cover:

- `internal/shell/controlcenter_test.go`: Network rail enabled, page dispatch,
  shared status/body projection, and error/unavailable state;
- `internal/shell/popout_network_test.go`: the same fake service drives the
  standalone and Control Centre trees, Wi-Fi/Ethernet tabs remain coherent,
  and `network-close` closes the correct host;
- `internal/shell/registry_test.go`: network updates rebuild an open Control
  Centre without starting another service or lease loop;
- focus and credential tests: leaving the page clears the password, reconnect
  does not retain it, and no extra surface remains mapped;
- off-owner action tests: scan, radio, activation, and secret submit remain
  scheduled rather than executed while `Registry.mu` is held.

## Live gate

On Niri `DP-1`, inspect the Network rail, open and close the Control Centre
page, and compare `niri msg -j layers` before and after. Read status and page
routing only. Do not toggle Wi-Fi, forget a connection, or retain a real
credential to produce evidence.

## Boundary

This design does not add NetworkManager features, change credential policy,
move the standalone panel, or introduce a general page router.
