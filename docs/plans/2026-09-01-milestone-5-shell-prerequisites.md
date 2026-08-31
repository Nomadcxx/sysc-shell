# Milestone 5 Shell Prerequisites Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Add the two shell primitives that the notification and tray plans already depend on.

**Architecture:** Extend the existing auxiliary request path with in-place mutable surface policy, and
replace panel-local open bookkeeping with one process-wide interactive root chain. Keep both owners in
the shell/Wayland goroutine path; M5 adds no second surface host.

**Tech Stack:** Go 1.26, existing Wayland bindings and retained UI.

**Design:** `docs/plans/2026-08-31-notifications-and-tray-integration-design.md`

---

### Task 1: Update an auxiliary surface in place

**Files:**
- Modify: `internal/platform/wayland/aux.go`, `internal/platform/wayland/client.go`
- Test: `internal/platform/wayland/aux_test.go`

**Step 1:** Write failing tests that open one auxiliary surface, update keyboard interactivity from
None to OnDemand, replace a full input region with a bounded rectangle union and then an empty region,
and reject an update for a missing output or surface without disturbing siblings.

**Step 2:** Add `AuxUpdate` to the existing owner command. Copy submitted rectangles, validate them in
logical coordinates, apply keyboard and input-region changes to the existing `surfaceUnit`, and commit
once. Do not recreate the surface or add another dispatch goroutine.

**Step 3:** Run `go test -race -count=1 ./internal/platform/wayland/ -run Aux -v`.

**Step 4:** Commit `feat(wayland): update auxiliary surface policy`.

---

### Task 2: Coordinate one process-wide interactive root chain

**Files:**
- Create: `internal/shell/root.go`
- Modify: `internal/shell/panelhost.go`, `internal/shell/registry.go`, `internal/shell/tooltip.go`
- Test: `internal/shell/root_test.go`, `internal/shell/panelhost_test.go`

**Step 1:** Write failing tests that opening a different panel closes the old root, an attached child
closes with its owner, an unrelated root replaces the whole chain, and every close path releases
keyboard, text input, saved serials, tooltip visibility, and registered cleanup callbacks exactly once.

**Step 2:** Add one root generation and optional attached child under `Registry.mu`. Route existing
panel open/close through it without changing panel rendering. Root replacement runs the old chain's
cleanup before publishing the new owner; stale-generation close requests do nothing.

**Step 3:** Run `go test -race -count=1 ./internal/shell/ -run 'Root|Panel|Tooltip' -v`.

**Step 4:** Run `go test -race -count=1 ./internal/shell/ ./internal/platform/wayland/`.

**Step 5:** Commit `feat(shell): coordinate interactive roots`.
