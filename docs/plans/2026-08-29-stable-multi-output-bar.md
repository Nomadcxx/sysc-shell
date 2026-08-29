# Stable Multi-Output Bar Correction Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Correct per-output configuration ownership, live model replacement, and rounded-body rendering
before merging Milestone 2.

**Architecture:** `owner` holds the accepted `config.Config`; each `OutputHost` holds its resolved bar
policy and opacity. The shell registry prepares replacement models before reload commits. The renderer
clears each buffer and paints the configured rounded body instead of the whole layer surface.

**Tech Stack:** Go, standard library, `wl_shm`, generated Wayland protocol bindings, Niri IPC.

---

### Task 1: Prove per-host policy ownership

**Files:**
- Modify: `internal/platform/wayland/host.go`
- Modify: `internal/platform/wayland/client.go`
- Test: `internal/platform/wayland/policy_test.go`

**Step 1: Write the failing tests**

Add tests that construct two hosts with different `config.Bar` values and assert that each host derives
its own surface height, gap and radius. Add a readiness-policy test that expects `enabled: false` to leave
the host idle without requesting shell callbacks.

**Step 2: Verify RED**

Run:

```bash
go test ./internal/platform/wayland -run 'TestHostPolicy|TestDisabledHost' -count=1
```

Expected: FAIL because geometry comes from shared `owner.options` and readiness creates every bar.

**Step 3: Implement the minimum ownership change**

- Replace `Run`'s `Options` argument with the validated initial `config.Config`.
- Store the accepted config on `owner`.
- Store `config.Bar` and background opacity on `OutputHost`.
- Resolve and store policy during readiness.
- Make surface height, regions and role requests read the host policy.
- Leave disabled hosts in `hostIdle`.

Keep the 48/4/44 reference geometry unchanged.

**Step 4: Verify GREEN**

Run the focused command from Step 2, then:

```bash
go test ./internal/platform/wayland ./cmd/sysc-shell -count=1
```

Expected: PASS.

**Step 5: Commit**

```bash
git add internal/platform/wayland/host.go internal/platform/wayland/client.go \
  internal/platform/wayland/policy_test.go cmd/sysc-shell/main.go
git commit -m "fix(wayland): keep policy on each output"
```

### Task 2: Prepare and commit shell models on reload

**Files:**
- Modify: `internal/shell/registry.go`
- Modify: `internal/platform/wayland/client.go`
- Modify: `cmd/sysc-shell/main.go`
- Test: `internal/shell/registry_test.go`
- Test: `internal/platform/wayland/policy_test.go`

**Step 1: Write the failing tests**

Add a registry test that prepares a new accent and item layout for two existing connectors, proves the
live bars remain unchanged before commit, invokes commit, and proves both bars changed together while
workspace labels survived.

Add owner-policy tests for:

- a hotplugged connector using the latest accepted override;
- an enabled host becoming disabled;
- a disabled host becoming enabled;
- a rejected candidate retaining the previous host policies and callbacks.

**Step 2: Verify RED**

Run:

```bash
go test ./internal/shell ./internal/platform/wayland \
  -run 'TestPrepareConfig|TestReloadPolicy|TestHotplugPolicy' -count=1
```

Expected: FAIL because `SetConfig` only changes configuration for future bars and reload mutates shared
options.

**Step 3: Implement staged replacement**

- Add a concrete prepared-update type containing callbacks for enabled connectors and a commit closure.
- Make the registry build every candidate bar before returning the prepared update.
- Preserve workspace labels while building replacements.
- Apply prepared callbacks and policies on the owner goroutine.
- Commit registry models and `owner.cfg` after all host operations succeed.
- Treat Wayland request failure during application as fatal; reject parse, resolution and model-build
  errors without changing live state.
- Remove `Callbacks.OnConfig`, shared geometry mutation, and stale registry comments.

**Step 4: Verify GREEN**

Run the focused command from Step 2, then:

```bash
go test -race -count=1 ./internal/shell ./internal/platform/wayland ./cmd/sysc-shell
```

Expected: PASS.

**Step 5: Commit**

```bash
git add internal/shell/registry.go internal/shell/registry_test.go \
  internal/platform/wayland/client.go internal/platform/wayland/policy_test.go cmd/sysc-shell/main.go
git commit -m "fix(config): replace all live bars on reload"
```

### Task 3: Paint the rounded body and transparent gap

**Files:**
- Modify: `internal/render/canvas.go`
- Modify: `internal/render/paint.go`
- Modify: `internal/render/paint_test.go`
- Modify: `internal/shell/proof.go`
- Modify: `internal/platform/wayland/client.go`
- Modify: `internal/platform/wayland/regions.go`
- Test: `internal/platform/wayland/regions_test.go`

**Step 1: Write the failing tests**

Add pixel tests that assert:

- the top gap and corner pixels stay transparent;
- pixels inside the rounded 40px body use the configured background;
- repainting a smaller body clears pixels from the previous frame;
- a 1.5 scale maps body and radius to physical pixels;
- a translucent background produces no opaque region.

**Step 2: Verify RED**

Run:

```bash
go test ./internal/render ./internal/platform/wayland -run 'TestPaint.*Body|Test.*Opaque' -count=1
```

Expected: FAIL because `Paint` fills the complete canvas and `applyRegions` assumes an opaque background.

**Step 3: Implement rounded-body painting**

- Record the logical body and radius in the render style during configure.
- Clear the buffer before each paint.
- Add one clipped rounded-rectangle fill using integer-safe scanlines and standard-library math.
- Convert body and radius through `scale120` once at the render boundary.
- Derive the opaque-region flag from the validated background alpha.
- Keep the input region equal to the complete layer surface.

Do not add a rendering dependency or a generalized drawing API.

**Step 4: Verify GREEN**

Run the focused command from Step 2, then:

```bash
go test -race -count=1 ./internal/render ./internal/shell ./internal/platform/wayland
```

Expected: PASS.

**Step 5: Commit**

```bash
git add internal/render/canvas.go internal/render/paint.go internal/render/paint_test.go \
  internal/shell/proof.go internal/platform/wayland/client.go \
  internal/platform/wayland/regions.go internal/platform/wayland/regions_test.go
git commit -m "fix(render): paint the rounded bar body"
```

### Task 4: Repair milestone documentation

**Files:**
- Modify: `tests/integration/README.md`
- Modify: `docs/plans/2026-08-29-stable-multi-output-bar-progress-handover.md`

**Step 1: Remove stale instructions**

Delete the obsolete architectural-proof procedure that uses the removed `--output` flag. Keep the
13-check Milestone 2 matrix and the 48/4/44 geometry record.

**Step 2: Update the handover**

Point it to the authoritative design and plan, record the corrected ownership and rendering behavior,
and retain the unqualified-live-gate warning. Do not record machine-specific claims.

**Step 3: Check the documentation**

Run:

```bash
rg -n -- '--output' tests/integration/README.md
rg -n '48|44|40|Live' tests/integration/README.md \
  docs/plans/2026-08-29-stable-multi-output-bar-progress-handover.md
```

Expected: the first command returns no matches; the second finds the reference geometry and live-gate
status.

**Step 4: Commit**

```bash
git add tests/integration/README.md \
  docs/plans/2026-08-29-stable-multi-output-bar-progress-handover.md
git commit -m "docs: correct the stable bar handover"
```

### Task 5: Run the merge gate

**Files:**
- Review: all branch changes against `main`

**Step 1: Run the full automated gate**

```bash
go mod tidy -diff
go test -race -count=1 ./...
go vet ./...
go build -o /tmp/sysc-shell-milestone2 ./cmd/sysc-shell
gofmt -l .
git diff --check
```

Expected: each command exits zero and formatting prints no paths.

**Step 2: Review scope and contracts**

Confirm the diff adds no widget system, plugin API, GPU backend, compositor mutation, new dependency, or
non-Linux code. Confirm the 48/4/44 contract and the one-goroutine Wayland rule remain intact.

**Step 3: Commit any gate-only documentation update**

Record only reusable commands and milestone state. Keep machine observations outside Git.

**Step 4: Merge and verify**

Merge `milestone/stable-bar` into `main`, rerun the full automated gate on merged `main`, and push after
the merged gate passes. Keep the live Niri matrix recorded as outstanding until a local console session
can run it.
