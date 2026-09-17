# Notifications, Battery, and Urgency Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement the plan task-by-task.

**Goal:** Add semantic urgency styling to shell notification cards and publish one hysteretic battery warning through the qualified `sysc-notify` presenter producer.

**Architecture:** `sysc-notify` remains the only notification record and FDO owner. It adds an optional, capability-gated keyed producer command to the existing authenticated presenter socket; the state owner assigns IDs, replaces by key, and closes idempotently. `sysc-shell` keeps a pure battery reducer beside its notification integration, uses the base bar threshold, and sends producer commands outside `Registry.mu`; old services remain usable because the shell sends producer commands only after capability negotiation.

**Tech Stack:** Go 1.26, existing length-bounded JSON Unix-socket protocol, existing `sysc-notify` state owner and presenter server, existing `sysc-shell` metrics relay and retained UI tree, Go table tests and race tests. No new dependency.

---

### Task 1: Extend the producer wire contract

**Repository:** `/home/nomadx/sysc-notify`

**Files:**
- Modify: `protocol/types.go`
- Modify: `protocol/validate.go`
- Test: `protocol/types_test.go`
- Test: `protocol/validate_test.go`

**Step 1: Write the failing protocol tests**

Add tests for:

- protocol minor `2` and capability `battery-producer`;
- `producer.publish` and `producer.close` commands round-tripping through strict JSON;
- a publish request carrying a bounded key, text, urgency, persistent expiry, and optional 0..100 value;
- rejection of an empty or oversized key, invalid UTF-8/text, invalid urgency, invalid value, missing producer payload, and expiry below `-1`;
- a successful producer reply carrying a non-zero service-assigned ID;
- an older minor still validating while a future major remains rejected.

Run from the service worktree:

```bash
go test ./protocol/ -run 'Producer|Hello' -count=1
```

Expected: FAIL because the producer types and commands do not exist.

**Step 2: Implement the minimum additive contract**

Add:

```go
const (
    ProtocolMinor            uint16 = 2
    CapabilityBatteryProducer       = "battery-producer"
    MaxProducerKeyBytes             = 128
)

const (
    CommandProducerPublish CommandKind = "producer.publish"
    CommandProducerClose   CommandKind = "producer.close"
)

type ProducerRequest struct {
    Key             string  `json:"key"`
    AppName         string  `json:"app_name,omitempty"`
    Summary         string  `json:"summary,omitempty"`
    Body            string  `json:"body,omitempty"`
    Urgency         Urgency `json:"urgency"`
    ExpireTimeoutMS int32   `json:"expire_timeout_ms"`
    Value           *int32  `json:"value,omitempty"`
}
```

Add `Producer *ProducerRequest` to `protocol.Command` and `ID uint32` plus `Replaced bool` to `protocol.Reply`. Validate the producer payload at the trust boundary. Accept lower compatible minors and reject a peer minor above the service minor. Keep all existing command and reply encodings valid.

**Step 3: Run the focused protocol tests**

```bash
go test ./protocol/ -run 'Producer|Hello' -count=1
go test -race ./protocol/ -count=1
```

Expected: PASS.

**Step 4: Commit**

```bash
git add protocol/
git commit -m "feat(protocol): add battery producer commands"
```

### Task 2: Make the state owner own keyed producer records

**Repository:** `/home/nomadx/sysc-notify`

**Files:**
- Modify: `internal/state/owner.go`
- Test: `internal/state/owner_test.go`

**Step 1: Write the failing state tests**

Test that:

- publishing a producer request assigns a non-zero ID, creates a persistent active notification, and marks the candidate transient;
- publishing the same key replaces the original ID and emits one replace delta rather than a second active record;
- closing an existing key removes the record and emits no history entry;
- closing an absent key succeeds without a delta;
- a normal close, expiry, dismissal, or capacity eviction removes the key mapping so a later publish gets a fresh ID.

Run:

```bash
go test ./internal/state/ -run 'Producer' -count=1
```

Expected: FAIL because `state.Command` has no producer operations.

**Step 2: Implement the state path**

Add producer publish and close commands to `state.CommandKind`. Keep a `producerIDs map[string]uint32` and a producer key on each active record. Convert a publish request into the existing `notify.Candidate` path with `Transient: true`, `ExpireTimeout` from the request, and no actions or image. Reuse `add` so capacity, validation, IDs, replacement, and deltas stay in one owner. Delete the key when its record closes. Make producer close idempotent and return the normal state result with the assigned ID and replacement bit.

**Step 3: Run the state tests and race check**

```bash
go test ./internal/state/ -run 'Producer' -count=1
go test -race ./internal/state/ -count=1
```

Expected: PASS.

**Step 4: Commit**

```bash
git add internal/state/
git commit -m "feat(state): own keyed producer records"
```

### Task 3: Gate producer commands at the authenticated presenter

**Repository:** `/home/nomadx/sysc-notify`

**Files:**
- Modify: `internal/presenter/server.go`
- Modify: `internal/presenter/connection.go`
- Test: `internal/presenter/server_test.go`
- Test: `internal/presenter/connection_test.go`

**Step 1: Write the failing presenter tests**

Test with the real Unix socket that:

- the service hello advertises `battery-producer`;
- a peer that requests the capability can publish and close and receives the assigned ID;
- a peer that omits the capability receives an unavailable reply for a producer command and cannot mutate state;
- existing peers that request only the two required presentation capabilities still handshake and receive snapshots;
- same-UID and existing malformed-command rules still apply.

Run:

```bash
go test ./internal/presenter/ -run 'Producer|Capability|Handshake' -count=1
```

Expected: FAIL because the connection does not remember negotiated producer capability and cannot dispatch producer state commands.

**Step 2: Implement negotiation and authorization**

Keep `notification-state` and `presentation-lifetime` as required capabilities. Advertise `battery-producer` as an additive service capability, and store whether the peer requested it on the connection. Pass that bit into command execution. Return `ErrorUnavailable` without calling the state owner when a producer command arrives from an unqualified peer. Map successful state results to `Reply{OK: true, ID: result.ID, Replaced: result.Replaced}`.

**Step 3: Run the presenter and full service checks**

```bash
go test ./internal/presenter/ ./internal/state/ -count=1
go test -race ./internal/presenter/ ./internal/state/ -count=1
```

Expected: PASS.

**Step 4: Commit**

```bash
git add internal/presenter/
git commit -m "feat(presenter): authorize battery producers"
```

### Task 4: Qualify the service release before shell integration

**Repository:** `/home/nomadx/sysc-notify`

**Files:**
- No shell files in this task

**Step 1: Run the service gate**

```bash
gofmt -w protocol internal/presenter internal/state
test -z "$(gofmt -l protocol internal/presenter internal/state)"
go vet ./...
go test -race -count=1 ./...
```

Expected: PASS with no formatting or vet output.

**Step 2: Tag the qualified candidate**

```bash
git tag -a v0.1.0-rc.4 -m "sysc-notify producer capability"
```

Do not push or change the existing `v0.1.0-rc.3` tag. The shell pin moves only after the service gate passes. The rollback path remains `v0.1.0-rc.3`: an old service has no producer capability, so the shell keeps normal notifications and suppresses battery producer commands.

### Task 5: Teach the shell client capability negotiation and producer sends

**Repository:** `/home/nomadx/sysc-shell`

**Files:**
- Modify: `go.mod`
- Modify: `go.sum`
- Modify: `internal/notifyclient/client.go`
- Test: `internal/notifyclient/client_test.go`
- Test: `internal/notifyclient/fixtures_test.go`

**Step 1: Write the failing client tests**

Test that:

- the hello advertises `battery-producer` while retaining existing capabilities;
- a service hello with the capability lets `SendProducer` queue a command;
- a service hello without the capability makes `SendProducer` return `ErrUnsupported` without queueing;
- disconnect clears the capability and producer sends cannot proceed;
- the client includes service capabilities on the snapshot message so the shell projection can gate the reducer.

Run with a temporary workspace pointing the module import at `/tmp/sysc-notify-sysc-315`:

```bash
go work init . /tmp/sysc-notify-sysc-315
go test ./internal/notifyclient/ -run 'Producer|Capability' -count=1
```

Expected: FAIL because the client has no producer capability state or producer send API.

**Step 2: Implement the compatibility seam**

Pin `github.com/Nomadcxx/sysc-notify` to `v0.1.0-rc.4`. Store service capabilities per connection, add `Supports`, `ErrUnsupported`, and `SendProducer`, and include a copied capability list on the first snapshot message. Keep `Send` available for existing state-control commands. `SendProducer` validates the command, checks the negotiated capability, and then reuses the bounded command queue.

**Step 3: Run the focused client tests**

```bash
go test ./internal/notifyclient/ -run 'Producer|Capability' -count=1
go test -race ./internal/notifyclient/ -count=1
```

Expected: PASS.

### Task 6: Add the pure hysteretic reducer and wire it to metrics/messages

**Repository:** `/home/nomadx/sysc-shell`

**Files:**
- Create: `internal/shell/batterywarning.go`
- Test: `internal/shell/batterywarning_test.go`
- Modify: `internal/shell/registry.go`
- Modify: `internal/shell/notifications.go`
- Test: `internal/shell/notifications_test.go`
- Test: `internal/shell/registry_test.go`

**Step 1: Write the reducer tests**

Cover one behavior per test or table row:

- valid discharging input at the inclusive threshold emits one publish action;
- repeated low samples emit no second publish;
- charge between threshold and threshold+5 holds the warning;
- charge above the hysteresis band, charging, or full emits one close action;
- absent, invalid, NaN, infinite, or out-of-range charge emits no publish or recovery command;
- a send error leaves the warning retryable on the next valid sample;
- an early reply racing the send bookkeeping is consumed once;
- disconnect marks the service state unknown and a qualified reconnect reasserts or closes once according to the latest valid state;
- an unqualified service never receives producer commands.

Run:

```bash
go test ./internal/shell/ -run 'BatteryWarning|Producer' -count=1
```

Expected: FAIL because the reducer and wiring do not exist.

**Step 2: Implement the reducer**

Use a small mutex-protected state machine with `clear`, `warning-pending`, and `warning-live` semantics. Reserve an operation before sending so repeated samples cannot enqueue duplicates. Track a bounded early-reply map by request ID to cover a reply arriving before the caller records the returned request ID. A disconnect discards unknown in-flight outcomes and forces one reconciliation on the next qualified snapshot. Invalid readings preserve the last known warning state and never act as recovery.

Build the command with the fixed key `sysc-shell:battery-low`, app name `sysc-shell`, summary `Battery low`, a rounded 0..100 value in the body and `Value`, `UrgencyCritical`, and persistent expiry. Use the first battery item found in the base bar, including nested group items. Do not read output overrides for the producer threshold.

**Step 3: Wire the reducer without holding `Registry.mu`**

Initialize the reducer in `NewRegistry`. After `UpdateMetrics` releases `Registry.mu`, pass the snapshot to the reducer and dispatch any returned producer action. In `applyNotify`, update the projection first, then pass snapshot, reply, and disconnect events to the reducer; dispatch returned actions after the registry lock is free. Store service capabilities from snapshot messages in `notifyState`. Keep existing notification command sends unchanged.

**Step 4: Run focused shell checks**

```bash
gofmt -w internal/shell/batterywarning.go internal/shell/batterywarning_test.go internal/shell/registry.go internal/shell/notifications.go internal/shell/notifications_test.go internal/shell/registry_test.go
go test ./internal/shell/ ./internal/notifyclient/ -run 'BatteryWarning|Producer|Notifications|Registry' -count=1
```

Expected: PASS.

### Task 7: Apply urgency roles while preserving card and bar geometry

**Repository:** `/home/nomadx/sysc-shell`

**Files:**
- Modify: `internal/shell/notifycard.go`
- Test: `internal/shell/notifycard_test.go`
- Test: `internal/shell/notifywidget_test.go`

**Step 1: Write the failing UI tests**

Assert that low summaries and meaningful identity/body text use `ToneSubtle`, normal uses `ToneNormal`, and critical uses `ToneError` plus the existing accessible accent stripe and outline. Assert that grouping/history paths use the same urgency helper. Keep the existing unread/read layout-width test as the geometry guard.

Run:

```bash
go test ./internal/shell/ -run 'NotifyCard|NotifyWidget|Urgency|Unread' -count=1
```

Expected: FAIL for low-role assertions.

**Step 2: Implement one urgency helper**

Extend `toneFor` to cover low, normal, and critical. Apply its result to summary, app/time identity, and body text nodes. Keep fixed card padding, action controls, timeout/value meters, unread icon box, critical stripe, and outline unchanged.

**Step 3: Run the UI checks**

```bash
go test ./internal/shell/ -run 'NotifyCard|NotifyWidget|Urgency|Unread' -count=1
```

Expected: PASS.

### Task 8: Complete cross-repository verification and handoff

**Repositories:** both isolated worktrees

**Step 1: Run service proof before the final shell pin proof**

```bash
cd /tmp/sysc-notify-sysc-315
go vet ./...
go test -race -count=1 ./...
```

Expected: PASS.

**Step 2: Run the shell proof with the temporary workspace**

```bash
cd /tmp/sysc-shell-sysc-315
go test ./internal/notifyclient/ ./internal/shell/ -count=1
go vet ./...
go test -race -count=1 ./...
test -z "$(gofmt -l .)"
git diff --exit-code -- go.mod go.sum
```

Expected: PASS. Keep any pre-existing baseline failure separate and name its package and command; do not attribute it to sysc-315 without a reproducing delta.

**Step 3: Inspect the final changes and record the release boundary**

Confirm the service commit/tag precedes the shell pin, the old `v0.1.0-rc.3` service path still handshakes without producer sends, and no shell-only notification appears when a producer send fails. Run the controlled producer fixture only; do not force a battery threshold or add records to the live desktop history.

**Step 4: Commit the shell implementation**

```bash
git add go.mod go.sum internal/notifyclient internal/shell
git commit -m "feat(shell): publish hysteretic battery warning"
```

Close `sysc-315` from `/home/nomadx/sysc-shell` only after the service and shell evidence is attached to the issue. Leave the M10 parent and unrelated dirty checkout changes untouched.

