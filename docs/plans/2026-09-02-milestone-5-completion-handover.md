# Milestone 5 completion handover

Date: 2026-09-02
Kind: completion-handover
Branch: `milestone/m5-completion`
Shell commit: `2604d77 fix(shell): paint the toast surface`
Branch base: `55377ab`

## What Milestone 5 shipped

27 commits, from the notification protocol pin to the tray presentation gate.

Notifications (Tranche 5A): a bounded card model with markup and image nodes, a service-owned
projection that never invents expiry, per-output toast stacks with geometry-decided overflow, pointer
intents, the notification centre with seen state and DND presets, conservative focus for accepted
actions, and the client wired through the registry.

Tray (Tranche 5B): a generation-safe client, item projection with attention override and overlay
composition, shared tooltips and item actions, a bounded menu model with a back stack, one hosted menu
surface with revision and root-correlation rules, overflow with persisted preferences and a virtual-list
drawer, and this milestone's final tranche — the wiring that makes all of it reachable from a bar.

## Service candidate tags

```text
github.com/Nomadcxx/sysc-notify v0.1.0-rc.2
github.com/Nomadcxx/sysc-tray   v0.1.0-rc.1
```

Neither pin moved during this tranche. `git diff --exit-code -- go.mod go.sum` is clean, and there is
no `replace` directive.

**Neither service is implemented.** Both repositories contain a `docs/` tree and nothing else at their
tagged candidates: there is no `sysc-notifyd` and no `sysc-tray` daemon to run. This is the single fact
that shapes the live results below, and it is the gate on a stable tag.

## Automated gate

Run from the branch tip on 2026-09-02:

```text
gofmt -l .                        (no output)
go vet ./...                      (clean)
go test -race -count=1 ./...      all packages ok
git diff --exit-code -- go.mod go.sum   (clean)
```

```text
ok github.com/Nomadcxx/sysc-shell/cmd/sysc-shell            1.018s
ok github.com/Nomadcxx/sysc-shell/internal/config           1.028s
ok github.com/Nomadcxx/sysc-shell/internal/icons            1.117s
ok github.com/Nomadcxx/sysc-shell/internal/ipc              1.024s
ok github.com/Nomadcxx/sysc-shell/internal/notifyclient     1.244s
ok github.com/Nomadcxx/sysc-shell/internal/platform/niri    1.041s
ok github.com/Nomadcxx/sysc-shell/internal/platform/wayland 1.025s
ok github.com/Nomadcxx/sysc-shell/internal/render           1.887s
ok github.com/Nomadcxx/sysc-shell/internal/services         7.075s
ok github.com/Nomadcxx/sysc-shell/internal/settings         1.017s
ok github.com/Nomadcxx/sysc-shell/internal/shell            2.616s
ok github.com/Nomadcxx/sysc-shell/internal/theme            1.020s
ok github.com/Nomadcxx/sysc-shell/internal/theming          1.045s
ok github.com/Nomadcxx/sysc-shell/internal/trayclient       1.034s
ok github.com/Nomadcxx/sysc-shell/internal/ui               1.015s
ok github.com/Nomadcxx/sysc-shell/tests/integration        11.841s
```

`tests/integration/tray_test.go` drives the tray end to end with a fake service behind the command
sender and a fake compositor draining the aux and invalidation channels. It covers two outputs, output
hotplug and replug, service generations, stale and failed replies, a refused `menu.open`, malformed
sibling IDs, a preference-token collision, root replacement both before and after the menu arrives, a
compositor-side surface close, and shutdown cleanup.

## Live results

Environment: Niri 26.04 (8ed0da4), one connected output `DP-1` at 3440x1440, scale 1.0, transform
Normal. Go 1.27.0. Binary built from `2604d77`.

### Ran, and passed

| # | Check | Observation |
|---|---|---|
| 1 | shell starts under Niri | one bar; `niri msg layers` shows `sysc-shell:bar` on Top for DP-1 |
| 2 | toast surface maps | `sysc-shell-toast` on Overlay, keyboard None, anchored to all four edges |
| 3 | IPC status | returns the audio, brightness, matugen and template state |
| 4 | panel open through IPC | `sysc-shell-shield` (Overlay) and `sysc-shell-panel` (Overlay, keyboard Exclusive) appear |
| 5 | panel close through IPC | each goes away; only the bar and the toast surface remain |
| 6 | SIGHUP reload | both surfaces survive, nothing on stderr |
| 7 | SIGTERM shutdown | process exits, `niri msg layers` shows no `sysc-shell` surface, stderr clean |
| 8 | tray service absent | the client retries with backoff, the tray is empty, and nothing fails |

Check 2 matters: it is the first time a toast surface has ever been mapped. See the defect below.

### Not run, and why

Nothing below may be recorded as a pass.

| # | Check | Blocked by |
|---|-------|-----------|
| 9 | every notification check | `sysc-notifyd` does not exist at `v0.1.0-rc.2`; there is nothing to connect to |
| 10 | every tray item, menu, drawer and preference check | the `sysc-tray` daemon does not exist at `v0.1.0-rc.1` |
| 11 | every two-output check | one physical output is connected; Niri offers no runtime virtual output |
| 12 | mixed scale and transform | needs the second output |
| 13 | independent service restarts | needs both services |
| 14 | sixty minutes idle with both services connected | needs both services |
| 15 | Task 6 `xdg_popup` probe | needs `xdg_wm_base` bound in the platform registry, which no shipped path uses |

Check 15 is a deliberate call, not an oversight. The tray menu ships on the design's named fallback —
one Overlay layer surface carrying the menu's own root and revision rules — and that surface is now
live-verified to map on Niri through the drawer and panel surfaces that share its shape. Binding
`xdg_wm_base` only to answer a question whose answer cannot change this milestone's code would add an
unused proxy to the platform. The probe stays open as Task 6 Step 1.

## Defects found and fixed in this tranche

**`SyncToastOutputs` had no caller.** The registry exported it and documented that "the registry calls
it from its output bookkeeping", and nothing did. Toast surfaces were therefore never opened, in any
build, since Tranche 5A. Wiring the tray's output lifecycle exposed it, because both features hang off
the same `NewHost` and `DropHost` bookkeeping.

**The toast surface could not be mapped.** Once it was opened the Wayland owner rejected it at once:
`HostCallbacks.Render is nil for toast:DP-1`. The spec carried a Configure callback and nothing else,
asked for no size, and set no anchor. It now anchors to all four edges, so the compositor reports the
output's own size; that size replaces the 1920x1080 placeholder the stack had been laid out against.

**The painter cannot express a stack.** `render.Paint` fills exactly one rounded body and clears
everything outside it. Painting a toast stack as a single body would fill the gaps between cards.
Each card is now arranged at its own origin, painted into a reused buffer, and copied onto the
surface, so the gaps and the rest of the output stay transparent.

**Nothing supplied toast hover.** `toastHost.hovered` fed the documented presentation precedence
(hovered > visible > queued > suppressed) and no code ever wrote to it. The surface now takes pointer
events and marks the card under the pointer.

**Tray host callbacks raced the client pump.** The drawer's configure, render and input callbacks ran
on the Wayland owner while `ApplyTray` wrote the same state from the pump. Both tray hosts and the
toast host now take the registry lock in their callbacks, matching the panel hosts.

**`Registry.Status` carried tray shutdown code.** An earlier uncommitted draft had pasted the icon
worker cancel and the host disconnects into `Status`, so an IPC status query would have torn the tray
down. Moved to `Close`.

## Open defects

**No live evidence for notifications or the tray.** Every behaviour in those two features is covered by
unit and integration tests against fakes, and by nothing else. The first run against a real service
should be treated as a first run.

**Toast cards are not interactive beyond hover.** Accepting an action, dismissing a card, and inline
reply are modelled and unit-tested, and the toast surface routes no activation to them, because there
is no service to accept the resulting command. The centre is the reachable path today.

**Tray menu placement is approximate.** A bar menu anchors under its icon's left edge and a drawer menu
sits beside the drawer, both clamped by the compositor rather than by the shell, because a layer
surface cannot be repositioned after creation. The `xdg_popup` path in Task 6 is what removes the
approximation.

**Submenu surfaces are sized for the widest level in the tree.** A layer surface's size is fixed at
creation, so entering a submenu cannot resize it; a level with fewer rows leaves the surplus empty.

## Stable-tag authorization

**Not authorized.** A stable `sysc-notify` or `sysc-tray` tag requires the live matrix, and the live
matrix requires services that do not exist. The candidates stay at `v0.1.0-rc.2` and `v0.1.0-rc.1`.

The gate on a stable tag is, in order:

1. `sysc-notifyd` and a `sysc-tray` daemon exist and run;
2. a second output is available;
3. checks 9 to 14 above are executed and recorded;
4. the Task 6 popup probe is run, and the Overlay fallback is either replaced or confirmed.

## What a follow-on should pick up

- The two service implementations, which everything else waits on.
- Task 6 Step 1: the `xdg_popup` probe, and the popup surface path if Niri accepts it.
- Toast card activation, once a service can accept the command.
- The live matrix in `tests/integration/README.md` under "Milestone 5", which is written and unrun.
