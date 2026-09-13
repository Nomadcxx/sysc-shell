# Bluetooth Panel Execution Handover

Date: 2026-09-14.

Execute `sysc-155` from the approved design and committed implementation plan:

- `docs/plans/2026-09-13-bluetooth-panel-design.md`
- `docs/plans/2026-09-13-bluetooth-panel.md`

Use `superpowers:executing-plans` and follow the plan task by task. This file
does not repeat that task sequence. Track progress and discovered work in bd,
not in this handover.

## Receiving gate

Run `bd show sysc-155` and `bd blocked` from `/home/nomadx/sysc-shell` before
creating a Bluetooth worktree. At handover time, `sysc-155` is open and bd
reports `sysc-157` as its active blocker. The existing control-centre spine
dependencies, `sysc-154` and duplicate `sysc-253`, remain attached because the
Bluetooth tranche includes a functional control-centre page.

Do not start product code until `sysc-157` is on `main`. Bluetooth and network
touch the same Registry wiring, panel dispatch, control-centre navigation,
configuration, and Material glyph inventory. Planning against the network
branch is useful; implementing beside its dirty worktree would create an
avoidable merge.

At this snapshot, the network work is here:

```text
worktree: /home/nomadx/sysc-shell/.worktrees/feature/network-panel
branch:   feature/network-panel
HEAD:     ce3a0fe fix(ui): square network list rows
```

The branch is not on `main`. It has uncommitted row-inset work in
`internal/shell/popout_network.go`, `popout_network_test.go`, and
`surfacerole_test.go`. Those files belong to the network session. Do not edit,
commit, stash, or clean that worktree from the Bluetooth session.

## Start after the gate clears

From the primary checkout, confirm `sysc-157` is closed and its code is on
`main`; a closed bead alone is not proof. Then create the dedicated worktree:

```bash
cd /home/nomadx/sysc-shell
git worktree add .worktrees/feature/bluetooth-panel -b feature/bluetooth-panel main
bd update sysc-155 --status in_progress
```

Run later bd commands from the primary checkout. When a commit from the
worktree must include tracker state, point it at the primary database:

```bash
BEADS_DB=/home/nomadx/sysc-shell/.beads/beads.db git commit ...
```

Inspect `git status` before every commit. Preserve `.cursor/` and any unrelated
`.beads/issues.jsonl` changes in the primary checkout.

## Contracts that must survive implementation

The Registry owns one continuously subscribed BlueZ service. The bar,
standalone panel, and control-centre page consume immutable snapshots from that
service. Panels do not create a bus connection or own a second device model.

Use the existing `github.com/godbus/dbus/v5 v5.2.2` pin delivered by the
network tranche. Talk directly to BlueZ's ObjectManager, Properties, Adapter1,
Device1, AgentManager1, and Agent1 surfaces. Add no Bluetooth binding,
subprocess backend, general D-Bus framework, or new UI node kind.

Keep every D-Bus call off `Registry.mu` and the Wayland owner. The signal loop
publishes copied state; the Registry relay updates visible hosts and bars. A
mutating action resolves the opaque `DeviceID` against current state before it
calls BlueZ.

The pairing endpoint implements the complete `KeyboardDisplay` flow. One
blocking prompt may exist. Reject a second, match responses by generation,
reply once, time out after 60 seconds, fail closed, and clear PIN/passkey input
on every terminal path. Never place credentials in logs, argv, retained trees,
or error labels.

The deterministic first-adapter rule is intentional:

```go
// ponytail: one adapter matches the shipped host; add adapter selection when a second real consumer needs it.
```

Do not expand this tranche into profiles, codecs, OBEX, GATT, tethering,
renaming, reconnect policy, or multi-adapter UI.

## Control-centre boundary

The control centre is a required interactive consumer of Bluetooth. It uses
the same `bluetoothBody`, Registry handler, prompt, discovery session, transient
actions, and errors as `PanelBluetooth`. It keeps its own rail, header,
viewport, and 700x564 surface chrome; do not embed one panel inside the other.

Keep the Bluetooth rail destination disabled until its page dispatch and body
are wired. The `bluetooth` bar widget opens `PanelBluetooth` on left-click and
opens `PanelControlCenter` at section `bluetooth` on right-click. Switching
between the two replaces the one interactive root, leaving one prompt and one
shell-owned discovery session.

Do not close `sysc-155` after the service, widget, or standalone panel alone
works. Task 8 and the cross-host tests in Tasks 9 and 10 are part of the same
landing unit.

## Verification and handback

Use the focused red/green commands in the plan after each task. Before merge,
run the code-touching gates from `AGENTS.md` and the safe live Niri/BlueZ gate
from Task 10. Do not restart BlueZ, forget an existing device, or disturb an
active connection to manufacture evidence. Only pair and forget a disposable
device with owner approval.

When the gate passes, write
`docs/plans/2026-09-13-bluetooth-panel-completion-handover.md`, register it,
record the evidence on `sysc-155`, and close the bead in the same landing
commit. Leave this execution handover unchanged until the remaining-work
retirement rule in `AGENTS.md` applies.

## Known commits

```text
cd895b0 docs(bluetooth): define panel and service
f888538 docs(bluetooth): plan panel and service
```
