# Milestone 6F Protocol Version One Qualification Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Freeze protocol version one, prove its resource and lifecycle bounds, qualify all five reference plugins on live Niri, and close the M6 exit gate.

**Architecture:** Committed JSON fixtures define compatibility independently of Go structs. Black-box fake plugins drive malformed, slow, crashed, flooded, and deep-tree behavior through real pipes. The final live gate checks the same packaged commands and manifests users receive.

**Tech Stack:** Go tests and helper processes, committed JSON/JSONL fixtures, race detector, Niri on the trusted laptop.

**Design:** `docs/plans/2026-09-01-milestone-6-plugin-host-design.md`

---

### Task 1: Write the version-one protocol reference

**Files:**
- Create: `docs/plugin-protocol-v1.md`
- Create: `plugin/v1/testdata/host-hello.jsonl`
- Create: `plugin/v1/testdata/timer-session.jsonl`
- Create: `plugin/v1/testdata/world-clock-patch.jsonl`
- Create: `plugin/v1/testdata/notes-reseed.jsonl`
- Create: `plugin/v1/testdata/host-calls.jsonl`
- Test: `plugin/v1/compatibility_test.go`

**Step 1:** Document manifest schema, handshake, capabilities, every message, node grammar, settings/state, revisions/resync, input semantics, lifecycle, limits, errors, and the trusted-process statement. Mark reserved fields and minor-version rules.

**Step 2:** Capture fixtures from the five reference clients, then review and commit them as stable text. Remove timestamps, PIDs, paths, and ordering that the protocol does not guarantee.

**Step 3:** Load each fixture through the host decoder and client decoder. Compare canonical semantic values, not a decode/re-encode byte round trip.

**Step 4:** Run `go test -race -count=1 ./plugin/v1 -run Compatibility -v`.

**Step 5:** Commit `docs(plugin): freeze protocol version one`.

### Task 2: Pin manifest and settings compatibility

**Files:**
- Create: `internal/plugin/testdata/manifests/valid-timer.json`
- Create: `internal/plugin/testdata/manifests/valid-recorder.json`
- Create: `internal/plugin/testdata/manifests/invalid-unknown-field.json`
- Create: `internal/plugin/testdata/manifests/incompatible-major.json`
- Test: `internal/plugin/compatibility_test.go`

**Step 1:** Copy the packaged Timer and Recorder manifests into stable fixtures. Add invalid and incompatible fixtures with exact expected diagnostic paths.

**Step 2:** Test current parsing and validation against every fixture, including defaulted settings and `visible_when` behavior.

**Step 3:** Run `go test -race -count=1 ./internal/plugin -run Compatibility -v`.

**Step 4:** Commit `test(plugin): pin manifest compatibility`.

### Task 3: Build black-box abuse fixtures

**Files:**
- Create: `cmd/sysc-plugin-fixture/main.go`
- Create: `tests/integration/plugin_abuse_gate_test.go`

**Step 1:** Give the Go fixture explicit modes for malformed JSON, 1 MiB plus one byte, handshake delay, blocked stdin, immediate crash, crash loop, stderr flood, valid update flood, snapshot flood, depth 17, 1,025 nodes, slow response, and ignored shutdown.

**Step 2:** Test each mode through the real supervisor pipes. Assert bounded memory-facing queues, timeout or failure state, placeholder UI, stderr truncation, process removal, and continued built-in plus healthy-plugin updates.

**Step 3:** Add a clean fixture mode and prove the abuse harness itself does not force failure.

**Step 4:** Run `go test -race -count=1 ./tests/integration -run PluginAbuse -v`.

**Step 5:** Commit `test(plugin): add protocol abuse fixtures`.

### Task 4: Prove lifecycle, output, and persistence recovery

**Files:**
- Create: `tests/integration/plugin_recovery_gate_test.go`
- Modify: `tests/integration/README.md`

**Step 1:** Test enable, shell restart, plugin restart, disable, config reload, manifest rescan, missing dependency recovery, output hotplug, panel open during crash, and state/settings persistence.

**Step 2:** Simulate two outputs. Assert separate view IDs and prepared trees, one plugin PID, one state namespace, correct triggering output, stale generation rejection, and no duplicate process after reload.

**Step 3:** Run `go test -race -count=10 ./tests/integration -run 'PluginRecovery|PluginOutput' -v`.

**Step 4:** Commit `test(plugin): prove lifecycle recovery`.

### Task 5: Verify packaging and the local manager

**Files:**
- Modify as required: packaging files already used by the repository
- Test: `tests/integration/plugin_package_gate_test.go`

**Step 1:** Build `sysc-shell` and the Timer, World Clock, Notes, Weather, and Screen Recorder commands from a clean checkout. Verify each packaged manifest resolves to a regular executable and contained assets.

**Step 2:** Install to a temporary XDG config/data/state root. Verify manager discovery, metadata, dependency state, settings, enable/disable, rescan, and absence of catalog/install/update/remove actions.

**Step 3:** Run `go test -race -count=1 ./tests/integration -run PluginPackage -v` and `go build ./...`.

**Step 4:** Commit `test(plugin): verify reference packaging`.

### Task 6: Run the complete automated gate

**Files:**
- Modify only for defects found by this gate

**Step 1:** Run `gofmt -w` on changed Go files and verify it leaves no diff on a second run.

**Step 2:** Run `go test -race -count=1 ./...`.

**Step 3:** Run `go test -race -count=10 ./plugin/v1 ./internal/plugin ./internal/shell ./tests/integration`.

**Step 4:** Run `go vet ./...` and `go build ./...`.

**Step 5:** Fix only failures inside M6 scope. Commit each root-cause fix with its regression test.

### Task 7: Qualify all reference plugins on live Niri

**Files:**
- Create: `docs/plans/2026-09-01-milestone-6-completion-handover.md`
- Modify: `docs/plans/README.md`

**Step 1:** On the trusted laptop, start the packaged shell with a temporary plugin/config/state root. Enable all five plugins through Settings and place their widgets on the bar.

**Step 2:** Exercise Timer completion, World Clock add/remove/reorder, Notes create/edit/autosave/external-change conflict, Weather tooltip/forecast/stale recovery, and the M6E recorder gate.

**Step 3:** Crash and flood the fixture plugin while the five references and built-in widgets remain active. Record redraw rate, layout overruns, restart count, RSS before/after, and failure placeholders.

**Step 4:** If a second physical output is available, repeat output-context and hot-unplug checks there. The automated two-output gate remains mandatory either way.

**Step 5:** Write the handoff with commit hashes, commands and output, measurements, artifacts, unresolved hardware behavior, and the exact exit-gate verdict. Register it in the same commit.

**Step 6:** Commit `docs: hand over milestone 6 qualification`.

### Task 8: Close the milestone gate

**Files:**
- Modify: `.beads/issues.jsonl`

**Step 1:** Confirm every M6 roadmap exit item has automated or live evidence in the completion handoff.

**Step 2:** Confirm `sysc-69` through `sysc-73` are closed with evidence-bearing reasons. Close `sysc-74`, then close `sysc-66`. Do not close the epic for a partial or environment-skipped live gate.

**Step 3:** Run `bd sync --flush-only`, `bd ready`, and `bd blocked`. Confirm no M6 status lives only in a document.

**Step 4:** Commit `.beads/issues.jsonl` with `chore: close milestone 6`.
