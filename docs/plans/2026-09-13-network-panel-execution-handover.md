# Network panel continuation handover

Date: 2026-09-13.

Continue `sysc-157` in the existing `feature/network-panel` worktree. The
previous session stopped at the NetworkManager credential export after a rate
limit. Do not restart the design or replay the completed tasks.

Read the approved
[`2026-09-11-network-panel-design.md`](2026-09-11-network-panel-design.md), then
Tasks 10 to 12 of
[`2026-09-11-network-panel.md`](2026-09-11-network-panel.md). `bd show
sysc-157` owns live status. This handover records the code checkpoint and the
safe continuation order.

## Owner decisions to preserve

The bar item is `wifi`; `network` already names the throughput metric. A left
click opens one 460x560 `PanelNetwork` with Wi-Fi and Ethernet tabs. A right
click toggles the Wi-Fi radio. The panel uses the owner-approved Direction B
composition: connection status first, then tabs and the access-point list.
The command centre is a separate product and may consume `services.Network`
later.

Use `github.com/Wifx/gonetworkmanager/v2 v2.2.0` for the NetworkManager client.
Hand-write only the secret export with `godbus/v5`. NetworkManager and polkit
own privilege; do not add `sudo` or pass a secret through a process argument.
The credential holder accepts one prompt at a time and answers every prompt
once. A passphrase must not reach `errLabel`, the retained node tree, logs, or
command arguments.

The glyph inventory has nine additions, not eight. `wifi_off` keeps radio-off
distinct from enabled-but-disconnected. Main now carries this correction in
`85a0df3`. Main also carries the corrected dependency ordering in `80d6a8c`:
write the importing backend before `go mod tidy`, or tidy removes the unused
pin.

## Recovered checkpoint

The Claude session is
`2f962c9f-580f-478b-a044-7100a34a32e2`. Its transcript lives at
`/home/nomadx/.claude/projects/-home-nomadx-sysc-shell/2f962c9f-580f-478b-a044-7100a34a32e2.jsonl`.
The approved three-variant mock is
`https://claude.ai/code/artifact/c85bd466-d929-47bd-8c9f-ae9f41b56a49`.

Worktree: `/home/nomadx/sysc-shell/.worktrees/feature/network-panel`

Branch HEAD: `cfe692e` (`feat(ui): masked text fields`)

The committed work covers the service contract and backend, lease lifecycle,
Wi-Fi widget and glyphs, panel identity and anchoring, Direction B header,
Wi-Fi and Ethernet tabs, cached access-point list, off-lock writes, and masked
text fields. These commits form the implementation checkpoint:

| Plan work | Commit |
|---|---|
| State types and signal bands | `6ebc7f3` |
| Service and lease lifecycle | `ec69632` |
| Pinned NetworkManager backend | `eea1ce6` |
| Wi-Fi widget and nine glyphs | `83913d6` |
| Panel identity, geometry, and trigger | `57b56ea` |
| Status-first header | `1d2e41e` |
| Tabs, access points, and wired tab | `3179960` |
| Off-lock write proof | `50b6113` |
| Masked text fields | `cfe692e` |

Task 10 has an uncommitted checkpoint:

| File | State |
|---|---|
| `internal/services/network.go` | Adds and initializes the secret slot and request channel. |
| `internal/services/networksecrets.go` | New single-slot state, SecretAgent methods, SSID extraction, and registration helper. |
| `internal/services/networksecrets_test.go` | New slot tests for collision, cancel, submit, repeat answers, and pending state. |

The three files pass `gofmt`. Six focused credential tests pass, and `go vet
./internal/services` passes. `go mod tidy -diff` reports one expected change:
move `github.com/godbus/dbus/v5 v5.2.2` from indirect to direct. A full services
run in the Codex sandbox reached an unrelated `httptest` case and failed when
the sandbox denied a loopback listener. The earlier session had the package
green before Task 10.

Task 10 is not complete. `registerSecretExport` has no caller, so the running
shell never publishes or registers the object. Finish the service lifetime
wiring, run tidy, verify the focused tests and the services package outside a
socket-restricted sandbox, then commit the three dirty files and module change.
Use a hook-safe commit message; the machine hook rejects the substring
`agent`.

## Integrate current main before shell work

After the Task 10 commit, rebase the branch onto current `main`. At this
snapshot the branch is 11 commits ahead of its merge base and 28 commits behind
main. Main has since landed the control centre, surface polish, and the two
network-plan corrections.

The merge is substantive. Both sides changed the Material inventory and font,
`internal/render/paint.go`, `internal/shell/panel.go`, `panelhost.go`,
`registry.go`, `widget.go`, and `internal/ui/tree.go`. Preserve both features.
For the binary Material font conflict, union main's current glyph inventory
with the nine network names and regenerate the subset from
`~/.cache/sysc-shell/fonts/MaterialSymbolsRounded-upstream.ttf`. Do not select
either binary wholesale. Keep main's antialiased surface work and the branch's
masked text display path. Add network enum, dispatch, and registry cases to
main's current control-centre cases instead of replacing their switches.

Git should recognize the patch-equivalent plan corrections now on main and
drop branch commits `1c9fa08` and `b234465` during the rebase. Inspect any
conflict rather than keeping duplicate documentation commits.

## Finish the credential slice

Task 11 builds the password card and connects the service request channel to
the panel host. The card replaces the access-point list, names the SSID, masks
the field, supports reveal, and provides Connect and Cancel. Clear the real
field value on submit, cancel, panel close, and host replacement. Closing or
replacing the panel must call `CancelSecret`, because leaving `GetSecrets`
waiting makes NetworkManager appear hung.

Task 12 is the live Niri gate. Resolve the `nm-applet` secret-holder collision
before trusting the password flow, then unblock Wi-Fi with `rfkill`. Exercise
left and right bar clicks, both tabs, stable band sorting, saved-profile
activation, a new secured network, cancel, radio-off, panel close, and shell
logs. Record results and defects on `sysc-157`; close it only when the live gate
passes.

Two visible gaps need deliberate treatment. `sysc-254` records that
`Network.Forget` has no UI affordance; do not invent a gesture in this slice.
The header currently paints permanent dashes for Down and Up even though the
existing metrics service already owns network rates. Before closing
`sysc-157`, either wire those two figures through the existing cached metrics
path or record the accepted deferral in bd. Do not add throughput fields to
`services.NetworkState` without a reason; the metric owner already exists.

## Stop condition

Stop when Tasks 10 to 12 pass after the rebase, the live panel joins and
cancels secured networks without leaking a credential, and `sysc-157` contains
the live evidence. Remove this handover and its register row once the branch
lands or bd contains every remaining item.
