# Milestone 6A Plugin Host, Manager, and Timer Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Run one trusted plugin process through a bounded v1-draft protocol and use it to supply a Timer bar widget, tooltip, settings, and attached panel.

**Architecture:** Add a process-wide host under `internal/plugin`, public language-neutral wire types under `plugin/v1`, and thin shell adapters for existing bar, tooltip, settings, and panel owners. Reuse the M5 image path and interactive-root chain. Keep protocol parsing, validation, and view preparation away from the Wayland dispatch goroutine.

**Tech Stack:** Go 1.26 or later, Go standard library JSON and `os/exec`, existing retained UI and Wayland owner, existing M5 notification client.

**Design:** `docs/plans/2026-09-01-milestone-6-plugin-host-design.md`

---

### Task 0: Reconcile the plan with landed M5

**Files:**
- Read: `docs/roadmap.md`
- Read: `docs/plans/2026-09-01-milestone-6-plugin-host-design.md`
- Inspect: `internal/ui/tree.go`, `internal/render/paint.go`, `internal/shell/panelhost.go`, `internal/shell/registry.go`, `internal/platform/wayland/aux.go`

**Step 1:** Confirm `sysc-65` is closed with `bd show sysc-65`. Stop if the M5 exit gate has not passed.

**Step 2:** Run `go test -race -count=1 ./...`. Record any pre-existing failure in bd instead of weakening an M6 check.

**Step 3:** Locate the landed owners for `KindImage`, retained panel painting, the interactive-root chain, and bounded invalidation submission. Amend file paths in this plan if M5 moved those owners.

**Step 4:** Confirm the implementation still needs a separate wire tree. Reject any shortcut that decodes plugin JSON into `ui.Node`.

**Step 5:** Commit only a plan correction if reconciliation changed this document. Otherwise make no commit.

### Task 1: Define bounded protocol framing and wire types

**Files:**
- Create: `plugin/v1/message.go`
- Create: `plugin/v1/node.go`
- Create: `plugin/v1/framing.go`
- Test: `plugin/v1/framing_test.go`
- Test: `plugin/v1/node_test.go`

**Step 1:** Write table tests for a one-line `host.hello`/`plugin.hello` exchange, a valid snapshot, unknown message type rejection, a 1 MiB line limit, duplicate node IDs, depth 17, 1,025 nodes, 257 children, and text over 64 KiB.

**Step 2:** Run `go test -count=1 ./plugin/v1 -v`. Expect compile failure because the package does not exist.

**Step 3:** Add declarative message and node structs, strict decoding, a limited line reader, and tree validation. Keep the first node set to row, column, text, icon, progress, button, text input, and read-only tooltip content. Do not import `internal/ui`.

**Step 4:** Run `go test -race -count=1 ./plugin/v1 -v`. Expect all tests to pass.

**Step 5:** Commit `feat(plugin): define bounded v1 wire types`.

### Task 2: Validate manifests and discover local plugins

**Files:**
- Create: `internal/plugin/manifest.go`
- Create: `internal/plugin/discovery.go`
- Test: `internal/plugin/manifest_test.go`
- Test: `internal/plugin/discovery_test.go`

**Step 1:** Write table tests for the design's Timer manifest, unknown fields, bad IDs, duplicate IDs, path escape, non-executable entry, unknown capability, invalid panel geometry, invalid setting schema, more than 128 directories, and a missing `gpu-screen-recorder` dependency that remains visible.

**Step 2:** Run `go test -count=1 ./internal/plugin -run 'Manifest|Discover' -v`. Expect compile failure.

**Step 3:** Implement strict JSON manifest parsing with `json.Decoder.DisallowUnknownFields`, immediate-child scanning, duplicate rejection, relative-path containment, regular-executable checks, and `exec.LookPath` dependency reporting. A scan returns one immutable candidate set or an error; it does not start processes.

**Step 4:** Run `go test -race -count=1 ./internal/plugin -run 'Manifest|Discover' -v`.

**Step 5:** Commit `feat(plugin): validate and discover manifests`.

### Task 3: Store enabled plugins, settings, placements, and state

**Files:**
- Modify: `internal/config/config.go`
- Modify: `internal/config/load.go`
- Modify: `internal/config/write.go`
- Test: `internal/config/config_test.go`
- Test: `internal/config/write_test.go`
- Create: `internal/plugin/state.go`
- Test: `internal/plugin/state_test.go`

**Step 1:** Add failing config tests for enabled IDs, a bar placement containing plugin/entry/instance IDs, plugin-scoped settings, instance-scoped settings, duplicate instance IDs, a syntactically valid missing plugin that round-trips to a host placeholder, and round-trip preservation.

**Step 2:** Add failing state tests for plugin namespace containment, atomic replacement, 256 KiB values, 4 MiB total data, invalid keys, cancellation, and preservation after a rejected write.

**Step 3:** Implement the JSON config fields without changing built-in item syntax. Resolve state from `$XDG_STATE_HOME` with `$HOME/.local/state` as the fallback, then use temporary-file write, `Sync`, close, and rename. Use no database or new dependency.

**Step 4:** Run `go test -race -count=1 ./internal/config ./internal/plugin -run 'Config|Write|State|Placement' -v`.

**Step 5:** Commit `feat(plugin): persist configuration and state`.

### Task 4: Supervise one process and complete the handshake

**Files:**
- Create: `internal/plugin/supervisor.go`
- Create: `internal/plugin/runtime.go`
- Test: `internal/plugin/supervisor_test.go`
- Test: `internal/plugin/runtime_test.go`

**Step 1:** Use the current test binary as a helper process. Add tests for matching handshake, wrong identity, unsupported major version, update before hello, two-second timeout, context cancellation, one-second graceful shutdown then kill, 64 KiB stderr tail, and three starts in 60 seconds.

**Step 2:** Run `go test -count=1 ./internal/plugin -run 'Supervisor|Runtime|Handshake' -v`. Expect failure.

**Step 3:** Implement one runtime per enabled plugin with `exec.CommandContext`, explicit argument arrays, stdin/stdout/stderr pipes, capability intersection, structured failure state, and the restart budget. Keep process callbacks out of the shell mutex.

**Step 4:** Run `go test -race -count=1 ./internal/plugin -run 'Supervisor|Runtime|Handshake' -v`.

**Step 5:** Commit `feat(plugin): supervise plugin runtimes`.

### Task 5: Validate, convert, and prepare snapshot views

**Files:**
- Create: `internal/plugin/view.go`
- Create: `internal/plugin/prepare.go`
- Test: `internal/plugin/view_test.go`
- Test: `internal/plugin/prepare_test.go`
- Modify only if required by the approved nodes: `internal/ui/tree.go`, `internal/ui/layout.go`, `internal/ui/column.go`, `internal/render/paint.go`

**Step 1:** Write tests that convert each M6A wire node into a fresh shell-owned tree, reject protocol fields that have no shell owner, preserve semantic names and roles, and prove later mutation of decoded slices cannot change a prepared tree.

**Step 2:** Add a test that a blocking preparation job cannot run on the goroutine submitting a Wayland invalidation, and that one pending job per view replaces older work.

**Step 3:** Implement the smallest UI additions Timer needs. Use a fixed worker queue and immutable publication. Do not add a general component registry or accept wire-supplied bounds.

**Step 4:** Run `go test -race -count=1 ./internal/plugin ./internal/ui ./internal/render`.

**Step 5:** Commit `feat(plugin): prepare native plugin views`.

### Task 6: Attach plugin views and host calls to existing owners

**Files:**
- Create: `internal/shell/pluginhost.go`
- Create: `internal/shell/pluginwidget.go`
- Create: `internal/plugin/hostcall.go`
- Test: `internal/plugin/hostcall_test.go`
- Modify: `internal/shell/registry.go`
- Modify: `internal/shell/bar.go`
- Modify: `internal/shell/panelhost.go`
- Modify: `internal/shell/tooltip.go`
- Test: `internal/shell/pluginhost_test.go`
- Test: `internal/shell/pluginwidget_test.go`

**Step 1:** Write tests for one process-wide host, one bar view per output and placement, shared runtime state, panel opening on the triggering output, tooltip reuse, hot-unplug view closure, and fixed-size failure placeholders that leave built-ins intact.

**Step 2:** Add input tests for primary, middle, and secondary pointer buttons, activation, text change, submit, and panel close. Require accessible names on interactive nodes.

**Step 3:** Add host-call tests for settings/state namespace enforcement, declared-panel open/close, granted and denied notifications, bounded request count, deadline, cancellation, and failure replies. Reuse the M5 notification client.

**Step 4:** Extend `Registry` and `PanelHost`; do not add a second surface host. Route plugin panels through the landed interactive-root owner and prepared view updates through the landed bounded invalidation path.

**Step 5:** Run `go test -race -count=1 ./internal/plugin ./internal/shell -run 'Plugin|HostCall|Panel|Tooltip|Bar' -v`.

**Step 6:** Commit `feat(shell): host plugin views and calls`.

### Task 7: Add the installed-plugin manager

**Files:**
- Create: `internal/shell/popout_plugins.go`
- Modify: `internal/shell/popout_settings.go`
- Modify: `internal/settings/registry.go`
- Test: `internal/shell/popout_plugins_test.go`
- Test: `internal/settings/registry_test.go`

**Step 1:** Write tests for the plugin directory label, rescan, valid and rejected cards, metadata, dependency and capability display, enable/disable, retry, runtime state, bounded stderr, and generated global/instance settings.

**Step 2:** Add settings validation tests for bool, integer, float, string, select, color, file, folder, bounds, and `visible_when`. A rejected candidate must leave live config and process state unchanged.

**Step 3:** Add a Plugins section to the existing settings panel. Reuse its controls and scroll path. Do not add catalog, installation, update, removal, or custom plugin-rendered settings.

**Step 4:** Run `go test -race -count=1 ./internal/settings ./internal/shell -run 'Plugin|Setting' -v`.

**Step 5:** Commit `feat(settings): manage installed plugins`.

### Task 8: Ship the Timer reference plugin

**Files:**
- Create: `plugin/v1/client.go`
- Test: `plugin/v1/client_test.go`
- Create: `plugins/reference/timer/manifest.json`
- Create: `plugins/reference/timer/timer.go`
- Test: `plugins/reference/timer/timer_test.go`
- Create: `cmd/sysc-plugin-timer/main.go`

**Step 1:** Write pure Timer tests for duration parsing, start/pause/reset, elapsed monotonic time, no negative remaining value, restart state restoration, once-per-second updates, and one completion notification.

**Step 2:** Write client tests for handshake, settings/state calls, view snapshots, input dispatch, cancellation, and stdout containing protocol only.

**Step 3:** Implement one countdown owner and thin bar, tooltip, and panel projections. Use `time.Timer`/`time.Ticker`; do not create separate entry processes or import shell internals.

**Step 4:** Run `go test -race -count=1 ./plugin/v1 ./plugins/reference/timer ./cmd/sysc-plugin-timer` and `go build ./cmd/sysc-plugin-timer`.

**Step 5:** Commit `feat(plugin): add Timer reference plugin`.

### Task 9: Prove the M6A vertical slice

**Files:**
- Create: `tests/integration/plugin_host_gate_test.go`
- Modify: `tests/integration/README.md`

**Step 1:** Add an integration test that installs a temporary Timer manifest, enables it, opens two output views over one process, changes a setting, starts the timer from one view, observes the other view, opens and closes the panel, disables the plugin, and verifies process exit and node removal.

**Step 2:** Add malformed manifest, handshake timeout, crash, and oversized-line cases. Verify each failure leaves a built-in clock view usable.

**Step 3:** Run `go test -race -count=1 ./...` and `go vet ./...`.

**Step 4:** Record any deferred protocol change under `sysc-69`; do not call the draft wire stable.

**Step 5:** Close `sysc-69` with the gate evidence, flush beads, and commit `test(plugin): prove the M6A host slice` with `.beads/issues.jsonl`.
