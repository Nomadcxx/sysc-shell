# M10 Integration and Exit Gate Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Review the seven registered M10 designs as one shell, record every required check and live result, and close the M10 gate only when its evidence is complete.

**Architecture:** Keep each slice with its existing Beads owner and consume its service and projection. This plan coordinates integration and evidence; it does not replace focused slice plans or introduce duplicate services. The metrics and System Monitor evidence comes from their owner’s work and is not modified here.

**Tech Stack:** Go, Beads, Niri, existing `sysc-notify` and `sysc-tray` protocols.

---

### Task 1: Check the design, plan, and issue inventory

**Files:**
- Read: `docs/roadmap.md`, `docs/plans/README.md`, `docs/plans/2026-09-16-shell-polish-design.md`
- Read: the six focused M10 designs and their registered executable plans
- Evidence: `bd show sysc-309`, `bd show sysc-317`, `bd ready`, and `bd blocked` from `/home/nomadx/sysc-shell`

**Step 1: Verify that each design has a registered executable plan and a focused check**

Expected: every design points to a plan with exact files, tests, and a commit boundary. Keep missing documents and open owners visible; do not infer completion from a closed parent issue.

**Step 2: Reconcile each nine-area handover proof to its owner**

Check Control Centre, System Monitor, bar, Network page, notifications, tray, launcher, workspaces, and centre composition. For metrics/System Monitor, consume the separate owner’s evidence without changing its code or plan.

### Task 2: Run the cross-slice and repository gates

**Files:**
- Review: `internal/shell/registry.go`, `internal/shell/bar.go`, `internal/shell/popout_controlcenter.go`, `internal/shell/controlcenter_pages.go`, `internal/shell/tray.go`, `internal/shell/traymenuhost.go`
- Gate outputs: `go.mod`, `go.sum`, and the final working-tree diff

**Step 1: Run the focused checks named by each slice plan**

Expected: PASS for the changed behavior. Keep unrelated failures attributed to their existing issue unless evidence ties them to M10.

**Step 2: Run the repository gates**

Run: `gofmt -l .`

Run: `go vet ./...`

Run: `go test -race -count=1 ./...`

Run: `git diff --exit-code -- go.mod go.sum`

Run: `git diff --check`

Record the exact result of each command. A machine restriction, pre-existing race, unavailable package, or failed check is recorded as blocked or failed, never as passed.

**Step 3: Review integration invariants**

Confirm valid zero values remain distinct from unavailable data; no duplicate collector or NetworkManager path was added; tray Close success follows the matching removal delta; Network actions stay off `Registry.mu`; and centre, bar, and tray callbacks do not move I/O onto the Wayland dispatch loop.

### Task 3: Complete the live Niri evidence

**Files:**
- Evidence source: Niri on `DP-1`, 3440×1440 at scale 1.0.

**Step 1: Capture `niri msg -j layers` before affected surfaces are opened**

**Step 2: Inspect each available affected surface and record the result**

Use the existing slice plans for their specific actions. Keep checks read-only where they involve NetworkManager or notification credentials. Run a tray termination only against a disposable item owned by the test; otherwise record that fixture gate as unrun.

**Step 3: Capture `niri msg -j layers` after the surfaces close**

Record unavailable second-output, laptop, hardware, and disposable-fixture checks with their cause.

### Task 4: Write the completion handover and update Beads

**Files:**
- Create: `docs/plans/YYYY-MM-DD-m10-shell-polish-completion-handover.md`
- Modify: `docs/plans/README.md`
- Tracking source: `.beads/issues.jsonl` through `bd` in the primary checkout

**Step 1: Write a completion snapshot with commands, focused checks, live observations, and omissions**

Do not patch the receiving handover. Register the new completion handover in the same documentation change.

**Step 2: Close `sysc-317` and `sysc-309` only after every acceptance condition is evidenced**

Use `bd close` with a reason pointing to the completion handover. Commit `.beads/issues.jsonl` alongside the evidence it describes. If any required slice remains open or a required gate is incomplete, leave the M10 issues open.

