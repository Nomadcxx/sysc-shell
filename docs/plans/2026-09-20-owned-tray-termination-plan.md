# Owned tray termination implementation plan

Date: 2026-09-20. Issue: `sysc-316`. Design: `2026-09-16-owned-tray-termination-design.md`.

Two phases. Phase A lands upstream in `sysc-tray` (worktree `redesign/v0.1`), is tagged, and only
then does Phase B touch the shell. The shell never learns a PID and never signals a process.

## Verified seams

- `sysc-tray` `protocol/types.go`: `CommandKind` set, `ItemKey{Owner,ObjectPath,Generation}`,
  `Item`, error codes `invalid|stale_item|stale_revision|unavailable|busy`. Menu node IDs and
  command menu IDs already reject negatives (`protocol/validate.go:231`, `internal/menu/validate.go:39`),
  so a reserved negative shell row ID cannot collide with an application node.
- `internal/presenter/connection.go` `readLoop` dispatches commands on the connection goroutine;
  `dispatch` routes by kind to the `Commands` interface implemented by `internal/app.App`.
  A bounded wait inside Terminate delays only that shell's next commands — acceptable for a rare
  Close action; noted as the ceiling.
- `internal/app/run.go` keeps `workers[key]` per live generation; `item.Proxy` publishes
  generation-tagged results and detects owner loss (`ownerLeft`), which flows to
  `state.Owner.Remove` and out as `KindItemRemoved`. Terminate needs no new delta kind.
- `internal/presenter/server.go` already reads `SO_PEERCRED` for the shell peer; item-owner
  identity is a different check (bus daemon `GetConnectionUnixUser`/`GetConnectionUnixProcessID`).
- Shell: `trayactions.go` sends commands with full `ItemKey` through `trayCommandSender`;
  `trayReplyTracker` consumes replies (only `ErrorStaleItem` retriggers). `tray.go` handles
  `KindItemRemoved` and already closes a menu when its exact key disappears. `traymenuhost.go`
  renders menu rows and sends `CommandMenuSelect` with the focused node ID.

## Phase A — sysc-tray (branch `redesign/v0.1`)

### A1. Protocol: terminate command and close capability

- `protocol/types.go`: add `CommandTerminate CommandKind = "terminate"`. Add `CloseSupported bool`
  to `Item` (`json:"close_supported,omitempty"`).
- `protocol/validate.go`: `CommandTerminate` requires a non-zero `Item` and forbids `X`, `Y`,
  `Delta`, `MenuRevision`, `MenuID`, `Serial`, `Params` (the command carries only the item key).
- Check: table tests in `protocol/types_test.go`/`validate.go` — terminate with coordinates or a
  menu field is invalid; terminate with a zero key is invalid; existing kinds still validate.

### A2. Ownership record

- New `internal/ownership` package. `Record{UID uint32, PID int, StartTime uint64}`.
- `Inspect(conn *dbus.Conn, owner string) (Record, error)`: bus calls
  `org.freedesktop.DBus.GetConnectionUnixUser` and `GetConnectionUnixProcessID`, then reads
  `/proc/<pid>/stat` field 22 (starttime). Fails when the name is gone, the call fails, or
  `/proc` is unreadable.
- `Verify(current Record, uid uint32) error`: rejects UID mismatch, PID change, starttime change
  (recycled PID), and zero records.
- Check: unit tests using the test process's own `/proc` entry for the happy path; a fabricated
  starttime mismatch and a UID mismatch for the rejection paths; a missing PID for the
  unavailable path.

### A3. Capability advertisement

- `internal/app`: when a worker registers (`workers[itemKey] = ...`), inspect ownership
  asynchronously; store the `Record` on the worker. On each published item, set
  `CloseSupported = err == nil && record.UID == service euid`. Identity failure leaves the field
  false; the item stays fully usable otherwise.
- Check: `internal/app/run_test.go` — an item whose owner identity resolves advertises close;
  one whose inspection fails does not, and its other properties still publish.

### A4. Terminate operation

- `internal/app`: `Terminate(key protocol.ItemKey) error` on `App`, added to the
  `presenter.Commands` interface and `dispatch`.
- Order of checks, each returning a typed protocol error before any signal:
  1. worker lookup → `stale_item`;
  2. re-inspect the owner's current identity and `Verify` against the recorded record →
     `unavailable` on inspection failure, `invalid` on mismatch (stale/recycled/cross-UID);
  3. lifecycle authority: same-UID owned item only — this check is the authority for v1.
- Signal `syscall.SIGTERM` to the recorded PID, then wait bounded (`terminateWait = 3s`, poll
  `/proc/<pid>` existence at 100ms) for the process to leave. Exit → `nil` reply; the normal
  owner-left path publishes the removal delta. Timeout → `busy`; the item stays.
- No escalation, no `SIGKILL`, no hidden removal.
- Check: `internal/app/terminate_test.go` — success against a real child that exits on SIGTERM
  (delta observed through the state owner); busy against a child that ignores SIGTERM (shortened
  wait via a package-level var); stale key rejected; recycled identity rejected; cross-UID
  rejected by table.

### A5. Release

- `gofmt`, `go vet`, `go test -race -count=1 ./...` in the sysc-tray worktree.
- Tag `v0.1.0-rc.2`, push, confirm the module proxy serves it.

## Phase B — sysc-shell (worktree for sysc-316)

### B1. Pin bump

- `go.mod` → `sysc-tray v0.1.0-rc.2`. `go mod tidy`; module-diff check stays clean afterwards.

### B2. Reserved Close row

- `traymenuhost.go`: when the item advertises `CloseSupported`, append one row with the reserved
  negative ID (`trayCloseMenuID = -1`) and label "Close". It is shell-owned: never sent as a
  `MenuSelect` target, never persisted, never styled as an application node.
- `selectFocused` intercepts the reserved ID before the menu-select path and calls the terminate
  sender instead.

### B3. Terminate send and pending state

- `trayactions.go`: `trayTerminate(sender, key, output)` sends `CommandTerminate` with the full
  live `ItemKey` only. The host records a pending-close for that exact key.
- Reply handling in `trayReplyTracker`: `OK` keeps pending until the delta; any error clears
  pending and restores the menu; `ErrorStaleItem` additionally retriggers the existing
  projection rule. Disconnect or generation change clears pending.

### B4. Success and failure paths

- Success is `KindItemRemoved` for the pending key: clear pending; the existing
  menu-closes-when-key-disappears rule does the rest. A hidden-preference change is never treated
  as termination.
- Failure, timeout, or a surviving item: pending cleared, row visible, menu usable.

### B5. Shell checks

- `trayactions_test.go`: reserved Close sends the exact live key and nothing else; rejection and
  timeout leave the row and restore the menu; success only after the matching removal.
- `traywiring_test.go`: a stale terminate reply for an old generation cannot close a new
  application reusing the owner/path; disconnect cancels pending.
- Gates: `gofmt -w . && test -z "$(gofmt -l .)"`, `go vet ./...`,
  `go test -race -count=1 ./...`, `git diff --exit-code -- go.mod go.sum`.

### B6. Live gate

- Disposable same-UID fixture tray item if available; otherwise record the upstream and shell
  fixture results and leave the real-process gate open. `niri msg -j layers` before and after the
  drawer/menu path. Never terminate an unrelated desktop tray application.

## Boundary

No `os.Kill`/PID parsing in `sysc-shell`; no arbitrary signalling; no preference edit as a
termination result; no unbounded kill escalation in the service.
