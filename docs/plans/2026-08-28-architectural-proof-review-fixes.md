# Architectural Proof Review Fixes Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Correct the architectural-proof review blockers, rerun every acceptance gate, and merge the qualified branch into `main`.

**Architecture:** Preserve the existing proof boundaries. `Proof` supplies immutable render data, Wayland translates enter coordinates and owns synchronous input invalidation, Niri owns cancellable exact handshake validation, and `Run` reports ordered cleanup failures before closing the display.

**Tech Stack:** Go 1.26, `sysc-wayland` v0.1.1, Linux/Niri, Go race detector.

---

### Task 1: Stabilize module metadata

**Files:**
- Modify: `go.mod`
- Modify: `go.sum`

1. Run `go mod tidy -diff` and confirm it reports direct dependencies as indirect and missing `typesetting-utils` checksums.
2. Run `go mod tidy`.
3. Run `go mod tidy -diff`; expect no output and exit zero.

### Task 2: Make rendering immutable

**Files:**
- Modify: `internal/shell/proof.go`
- Modify: `internal/shell/proof_test.go`

1. Add a concurrent update/render regression test.
2. Run it with `go test -race ./internal/shell -run TestProofConcurrentUpdateAndRender -count=1`; expect a race failure.
3. Stop mutating retained text from the Niri goroutine. Under `Proof.mu`, copy the render style and a value-owned render tree containing current model values and arranged bounds; paint the copy after unlocking.
4. Rerun the focused race test; expect pass.

### Task 3: Preserve pointer-enter coordinates

**Files:**
- Modify: `internal/platform/wayland/client.go`
- Modify: `internal/shell/proof.go`
- Modify: `internal/shell/proof_test.go`

1. Add a test proving enter, press, and release can activate the button without motion.
2. Run the test and confirm it fails because enter is not represented.
3. Add `EventPointerEnter`, forward the enter coordinates for this surface, and cache them in `Proof.Handle` using the same logical-coordinate path as motion.
4. Rerun the focused test; expect pass.

### Task 4: Enforce a cancellable exact Niri handshake

**Files:**
- Modify: `internal/platform/niri/client.go`
- Modify: `internal/platform/niri/client_test.go`

1. Add tests rejecting `{"Ok":"Unexpected"}` and unblocking a stalled handshake after cancellation.
2. Run both tests and confirm the first is accepted and the second remains blocked.
3. Require the `Ok` value to equal `Handled`, and use `context.AfterFunc` to close the connection while the handshake read is pending.
4. Rerun both tests; expect pass.

### Task 5: Keep one click-invalidation path

**Files:**
- Modify: `internal/shell/proof.go`
- Modify: `internal/shell/proof_test.go`

1. Change the click test to require a synchronous `true` result without an asynchronous channel invalidation.
2. Run it and confirm the existing duplicate invalidation fails the assertion.
3. Remove the model-side click invalidation; the Wayland owner already invalidates when `Handle` reports a change.
4. Rerun the focused test; expect pass.

### Task 6: Report ordered shutdown failures

**Files:**
- Modify: `internal/platform/wayland/client.go`
- Modify: `internal/platform/wayland/lifecycle_test.go`

1. Add a test proving a cleanup-step failure is returned by shutdown.
2. Run it and confirm shutdown currently has no result to inspect.
3. Make `shutdown` return joined generation, callback, cleanup, final-roundtrip, and display-close errors. Make `Run` join that result into its return error, and close the display only after the final roundtrip.
4. Rerun the focused lifecycle tests; expect pass.

### Task 7: Qualify and merge

1. Run `go mod tidy -diff`.
2. Regenerate all four protocol packages and require `git diff --exit-code` before committing generated output.
3. Run `go test -race -count=1 ./...`, `go vet ./...`, and `go build -o /tmp/sysc-shell-review ./cmd/sysc-shell`.
4. Run the live Niri one-output, real-pointer, non-1-scale, idle-frame, ten-restart, SIGINT, and SIGTERM gates.
5. Commit the fixes, merge `milestone/architectural-proof` into `main`, and rerun the full automated gate on merged `main`.
