# M10 Control Centre Network Route Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Qualify the Control Centre Network destination using the existing NetworkManager service, cached state, and `networkTree` body.

**Architecture:** Keep NetworkManager I/O in `services.Network`; reuse the single `Registry.network` projection and route the Control Centre page to the existing tree. Do not add another connection, credential store, or signal loop.

**Tech Stack:** Go, existing NetworkManager service, retained UI tree, Niri.

---

### Task 1: Verify route and shared-state behavior

**Files:**
- Tests: `internal/shell/controlcenter_test.go`, `internal/shell/popout_network_test.go`, `internal/shell/registry_test.go`
- Implementation under review: `internal/shell/popout_controlcenter.go`, `internal/shell/controlcenter_pages.go`, `internal/shell/popout_network.go`

**Step 1: Run the focused route and Network checks**

Run: `go test -count=1 -run 'Network|ControlCenter' ./internal/shell`

Expected: PASS. The Network rail is enabled, the section selects the existing Network body, missing service state renders safely, and Network writes remain off `Registry.mu`.

**Step 2: If a behavior is missing, add a failing test at its existing owner seam**

Use the Control Centre tests for rail and selection behavior, Network tests for the shared body and credential lifecycle, and registry tests for snapshot rebuilds. Confirm the new test fails before changing implementation.

**Step 3: Re-run the focused checks**

Run: `go test -count=1 -run 'Network|ControlCenter' ./internal/shell`

Expected: PASS without a second NetworkManager service or credential path.

### Task 2: Inspect the route on Niri

**Files:**
- Evidence source: Niri on `DP-1`, 3440×1440 at scale 1.0.

**Step 1: Record `niri msg -j layers` before opening Control Centre**

**Step 2: Open the existing Network section and inspect its current service state**

Confirm the route appears and an unavailable NetworkManager state remains truthful. Read status only: do not toggle the radio, forget a network, or enter a credential.

**Step 3: Close Control Centre and compare `niri msg -j layers`**

Expected: the page closes through its owning surface and leaves no extra mapped layer.

### Task 3: Record the slice evidence

Record the focused test command, live result, and NetworkManager availability in the M10 completion handover. Keep the separately owned credential and forget-affordance gates under `sysc-268` and `sysc-254`.

