# Milestone 3 Audit Defect Corrections Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Correct the five defects filed by the merged Milestone 3 implementation audit.

**Architecture:** Keep each correction in the owner that already holds the violated invariant. The Niri
pump resolves its paired channels into one terminal result, tooltip generations follow the existing bar
retirement rules, tooltip paint reads accepted config, and dwell callbacks prove they still own the
current hover before sending.

**Tech Stack:** Go standard library, existing Wayland client bindings, existing render/config packages,
and the repository's `go test` gate.

Design source:
`docs/plans/2026-08-31-milestone-3-implementation-audit-report.md`, findings 1 through 5.

---

### Task 1: Make terminal Niri failure authoritative

**Files:**

- Modify: `cmd/sysc-shell/main.go:44-65`
- Test: `cmd/sysc-shell/main_test.go`

**Step 1: Write the failing test**

Add a package test that closes the snapshot channel while a terminal error remains buffered. Call a
small `pumpNiri` helper and assert that it returns that error. Add a second assertion that snapshots
received before closure reach the supplied update function.

**Step 2: Verify the failure**

Run: `go test ./cmd/sysc-shell -run TestPumpNiri -count=1`

Expected: FAIL because `pumpNiri` does not exist.

**Step 3: Implement the smallest pump**

Move the anonymous select loop into:

```go
func pumpNiri(
    snapshots <-chan niri.Snapshot,
    errs <-chan error,
    update func(niri.Snapshot),
) error
```

Set a closed channel to `nil` and keep reading the other channel. Return the first non-nil terminal
error; return nil after both channels close. Keep cancellation and `streamFailed` publication in
`run`.

**Step 4: Verify the correction**

Run: `go test ./cmd/sysc-shell -run TestPumpNiri -count=1`

Expected: PASS.

**Step 5: Commit**

Commit message: `fix(niri): preserve terminal stream errors`

### Task 2: Reject stale dwell callbacks

**Files:**

- Modify: `internal/shell/tooltip.go:20-99`
- Test: `internal/shell/tooltip_test.go`

**Step 1: Write the failing test**

Capture a dwell generation, invalidate it with `leave`, invoke the production callback path with the
old generation, and assert that no show request reaches `requests()`.

**Step 2: Verify the failure**

Run: `go test ./internal/shell -run TestStaleDwellCallback -count=1`

Expected: FAIL because dwell callbacks have no ownership token.

**Step 3: Implement generation ownership**

Add one monotonic generation field. Increment it on `enter`, `leave`, and `stop`. Have
`time.AfterFunc` call an unexported method that checks the captured generation under the mutex before
setting `shown` or sending.

**Step 4: Verify the correction**

Run: `go test ./internal/shell -run 'Test.*Dwell|TestMovingToAnotherWidget' -count=1`

Expected: PASS.

**Step 5: Commit**

Commit message: `fix(shell): reject stale tooltip dwells`

### Task 3: Retire tooltip buffers on release

**Files:**

- Modify: `internal/platform/wayland/tooltip.go:69-311`
- Test: `internal/platform/wayland/tooltip_test.go`

**Step 1: Write the failing lifecycle checks**

Create empty generations with `fd: -1`. Prove that reconfiguration retains an attached tooltip
generation until `released()`, then frees it. Prove that surface teardown marks current and retired
generations destroyed before freeing them.

**Step 2: Verify the failure**

Run: `go test ./internal/platform/wayland -run TestTooltipBuffer -count=1`

Expected: FAIL because tooltip surfaces have no retiring list or release path.

**Step 3: Reuse the bar retirement protocol**

Add `retiring []*generation` to `tooltipSurface`. Install a release handler on each new buffer.
Reconfiguration moves the current generation to `retiring`; a sweep destroys generations after
`retirement.freeable()` becomes true. Hide destroys the viewport, role, and surface, marks every
generation destroyed, then frees the storage.

**Step 4: Verify the correction**

Run: `go test ./internal/platform/wayland -run 'TestTooltip|TestRetirement' -count=1`

Expected: PASS.

**Step 5: Commit**

Commit message: `fix(wayland): retire tooltip buffers safely`

### Task 4: Paint tooltips from accepted config

**Files:**

- Modify: `internal/platform/wayland/client.go:148-176`
- Modify: `internal/platform/wayland/tooltip.go:226-278`
- Test: `internal/platform/wayland/tooltip_test.go`

**Step 1: Write the failing policy test**

Give the owner a config with non-default background, foreground, radius, font size, and font family.
Assert that tooltip style and font selection return those accepted values.

**Step 2: Verify the failure**

Run: `go test ./internal/platform/wayland -run TestTooltipUsesAcceptedTheme -count=1`

Expected: FAIL because tooltip paint uses constants.

**Step 3: Read current config at paint and measure time**

Resolve the triggering connector's bar policy through `Config.ForConnector`. Parse the validated theme
colours into `render.Color`, use `Theme.Radius` and `Bar.FontSize`, and build the cached text renderer
from `Bar.FontFamily`. Cache the family beside the renderer so a reload or output override selects the
right map.

**Step 4: Verify the correction**

Run: `go test ./internal/platform/wayland -run TestTooltip -count=1`

Expected: PASS.

**Step 5: Commit**

Commit message: `fix(wayland): apply theme to tooltips`

### Task 5: Remove the unused panicking selector query

**Files:**

- Modify: `internal/services/metrics.go:246-251`

**Step 1: Prove that no consumer exists**

Run: `rg -n '\.Leased\(' --glob '*.go' .`

Expected: no callers.

**Step 2: Delete `Metrics.Leased`**

Keep `SourceLeased`, which the sampler uses. Add no replacement API.

**Step 3: Run the focused package gate**

Run: `go test ./internal/services -count=1`

Expected: PASS.

**Step 4: Run the repository gate**

Run:

```text
go mod tidy -diff
go test ./...
go test -race -count=1 ./...
go vet ./...
go build -o /tmp/sysc-shell-m3-fixes ./cmd/sysc-shell
gofmt -l .
git diff --check
```

Expected: every command exits 0; tidy, vet, formatting, and diff checks print no findings.

**Step 5: Close tracking and commit**

Close `sysc-31` through `sysc-35`, flush beads from the primary checkout, and commit
`.beads/issues.jsonl` with the final correction.

Commit message: `fix: close milestone 3 audit defects`
