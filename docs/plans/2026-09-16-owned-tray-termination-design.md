# Owned tray termination design

Date: 2026-09-16. Parent commission: `sysc-309`.

## Goal

Make the tray Close action terminate an owned tray application through
`sysc-tray`, with identity checks and an acknowledgement that waits for the
real item removal.

## Existing seams

- `internal/shell/tray.go`, `traymenuhost.go`, and `trayactions.go` project
  full-generation `tray.ItemKey` values and send activate, scroll, and menu
  commands through one bounded client.
- `sysc-tray@v0.1.0-rc.1` exposes item/menu state and menu commands, but no
  terminate command or ownership capability.
- The shell currently removes a tray row only after `KindItemRemoved` and
  closes a menu when its exact key disappears. Those generation rules already
  protect stale clicks.

## Decisions

### D1. The shell sends an item key, never a process identity

Add an upstream service-owned terminate operation, represented in the shell
protocol by `CommandTerminate` carrying only the full `ItemKey`. Add an
additive item capability such as `CloseSupported` so the shell can omit Close
for items the service cannot own. If capability information is unavailable,
the service returns a clear unsupported error; the shell does not guess.

The shell appends one reserved Close row to the tray menu when the item
advertises support. Its action uses a shell-reserved action string, not an
application DBusMenu ID. It sends through the existing command queue and
shows a pending state without removing the item. The menu and drawer retain
their normal root/lifecycle ownership.

### D2. sysc-tray validates ownership at operation time

The service records the tray item's D-Bus owner, same-UID status, PID, and
`/proc` start-time identity when it can establish them. On every terminate
request it rechecks:

1. the item key still names the current service generation;
2. the D-Bus owner still maps to the recorded process;
3. the process has the same UID and start time;
4. the lifecycle authority still permits graceful termination.

The service rejects stale, recycled, cross-UID, ambiguous, or unsupported
identities before signaling. It invokes its own lifecycle authority, sends a
bounded graceful termination request, and waits for the owner/item delta.
The operation may return accepted while the process exits. It does not use a
shell-supplied PID. A timeout or ignored request returns busy/unavailable and
leaves the item visible; it does not fall back to hidden removal or an
unbounded kill escalation.

The exact service implementation, peer credential check, process identity
reader, and lifecycle authority belong in `/home/nomadx/sysc-tray`. Publish a
tagged compatible release and update the shell module only after its protocol
and daemon tests pass.

### D3. State arrival is the shell's success signal

The shell treats a successful command reply as accepted work. It reports Close
success only after a matching `KindItemRemoved` or an equivalent service
delta for the same generation arrives. A stale reply retriggers the existing
projection rule; a disconnect or generation change cancels the pending
operation. A failed reply restores the menu and leaves the item visible.

The shell never edits tray preferences to make the row disappear. A hidden
preference change is not a termination result. An item from another tray
application remains untouched.

## Data flow

```text
tray DBus item -> sysc-tray ownership record
Close menu row -> CommandTerminate(ItemKey)
                 -> service identity/lifecycle checks
                 -> owner exit -> ItemRemoved delta
                 -> shell clears pending state and reprojects surfaces
```

The service performs D-Bus, `/proc`, and process operations outside shell
locks. The shell only queues a bounded protocol command and applies immutable
messages.

## Focused proof

The executable plans will cover:

- upstream protocol validation and presenter dispatch for supported,
  unsupported, stale, recycled, cross-UID, busy, and successful termination;
- upstream daemon tests that verify graceful termination and item delta after
  process exit without a hidden-only removal;
- `internal/shell/trayactions_test.go` and `traywiring_test.go`: reserved Close
  action sends the exact live key, waits for the matching removal, and leaves
  the row on rejection or timeout;
- reconnect and generation tests proving an old result cannot close a new
  application with the same owner/path;
- a live smoke test with one disposable owned tray fixture and one unrelated
  tray item.

## Live gate

The receiving machine must not be used to terminate an unrelated desktop tray
application. Qualify with a disposable same-UID fixture if one is available,
then inspect `niri msg -j layers` before and after the drawer/menu path. If no
safe fixture exists, record the upstream protocol and shell fixture results
and leave the real-process gate open. Do not claim termination from a hidden
row.

## Boundary

This design does not add `os.Kill` or PID parsing to `sysc-shell`, signal an
arbitrary process, alter unrelated tray menu behavior, or claim a Close result
from a preference update.
