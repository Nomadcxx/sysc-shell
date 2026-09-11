# Control centre dashboard correction implementation plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to
> implement this plan task-by-task.

**Goal:** Correct the live control-centre Home dashboard, attached silhouette,
and unfinished interaction gate without changing its 700×564 surface or the
other page contracts.

**Architecture:** Rebase the existing control-centre branch onto `main`, then
reuse the generic image worker and radial gauge already in the tree. Fix the
opaque-region defect in the panel specification, antialias the existing fillet
recipe in the renderer, and connect the panel host to the animator outputs it
already owns. Keep one active page tree and one roving focus ring.

**Tech stack:** Go, retained `internal/ui` nodes, CPU `internal/render`, the
existing `internal/icons` worker, layer-shell auxiliary surfaces, Niri.

**Specs:**

- `docs/plans/2026-09-03-control-center-design.md`
- `docs/plans/2026-09-10-control-center-dashboard-correction-design.md`

**Tracker:** `sysc-154`. Status and discovered work stay in bd, not this file.

---

## Constraints and invariants

- Preserve the 700×564 panel and the 480px Home page. The Home block heights
  remain 96, 48, 184, and 116 with three 12px gaps.
- Add no dependency, UI node kind, runtime SVG loader, service backend, or
  second image decoder.
- Keep all filesystem and decode work off `Registry.mu` and the Wayland owner.
  A tree rebuild may perform cache lookup and enqueue bounded work only.
- `KindImage.Shape` controls clipping. An unset shape preserves the current
  rectangular image behavior.
- An absent metric is not zero: set `Absent`, preserve the dash value, and keep
  its space.
- A fillet-expanded surface contains transparent pixels outside its body and
  therefore cannot advertise its full bounds as opaque.
- Rail entries remain first in tree order. Disabled destinations remain inert
  and non-activating. Escape closes the root.
- One animator and one page tree exist per panel. A page change retargets that
  animator; it never mounts an outgoing tree.
- Use `/usr/bin/git` for plumbing in this checkout. `/home/nomadx/.local/bin/git`
  is a wrapper and may wait while compressing output.
- Run bd from `/home/nomadx/sysc-shell`, not from the worktree. Do not replace,
  unstage, or commit unrelated primary-checkout changes.
- Never run a repository-wide race test here. Use the capped, stubbed package
  gate in Task 5.

### Task 0: Rebase and prove the inherited baseline

**Files:** none intentionally changed.

1. From `/home/nomadx/sysc-shell`, inspect the tracker and both trees:

   ```bash
   bd show sysc-154
   /usr/bin/git status --short --branch
   /usr/bin/git -C .worktrees/feature/control-center status --short --branch
   ```

   Expected: `sysc-154` is in progress; the feature worktree is clean. Preserve
   all unrelated primary-checkout changes.

2. From the clean feature worktree, rebase onto the docs-bearing `main`:

   ```bash
   /usr/bin/git rebase main
   ```

   Resolve only conflicts belonging to the control-centre commits. The recent
   process-table and radial-gauge changes from `main` win outside that feature.

3. Build command stubs in a temporary directory. The shell package must never
   reach the real session commands during tests:

   ```bash
   mkdir -p /tmp/sysc-control-center-stubs
   printf '#!/bin/sh\nexit 0\n' > /tmp/sysc-control-center-stubs/loginctl
   printf '#!/bin/sh\nexit 0\n' > /tmp/sysc-control-center-stubs/systemctl
   printf '#!/bin/sh\nexec sleep 86400\n' > /tmp/sysc-control-center-stubs/systemd-inhibit
   chmod +x /tmp/sysc-control-center-stubs/*
   ```

4. Run the inherited focused gate before editing:

   ```bash
   PATH=/tmp/sysc-control-center-stubs:$PATH GOMAXPROCS=4 go test -p 4 \
     ./internal/shell ./internal/ui ./internal/ipc ./internal/services \
     ./internal/render ./internal/icons
   ```

   Expected: every package reports `ok`. If the rebase exposes a genuine
   baseline failure, record it with `bd create --deps discovered-from:sysc-154`
   before deciding whether it blocks this pass.

### Task 1: Circular account image with a stable fallback

**Files:**

- Modify: `internal/render/image.go`
- Modify: `internal/render/paint.go`
- Test: `internal/render/image_test.go`
- Modify: `internal/shell/controlcenter_pages.go`
- Modify: `internal/shell/registry.go`
- Modify: `internal/shell/tray.go`
- Test: `internal/shell/controlcenter_test.go`

1. Add a failing renderer test proving a `KindImage` with
   `ShapeCircle` leaves the four corner pixels untouched while painting the
   centre. Keep the existing rectangular image tests unchanged to prove an
   unset shape does not clip.

2. Run the focused failure:

   ```bash
   GOMAXPROCS=4 go test -p 4 ./internal/render -run 'Test.*Image.*Circle' -v
   ```

   Expected: FAIL because `paintImage` currently ignores `Node.Shape`.

3. Extend the image paint path at its owner. Resolve the physical radius from
   `Node.Shape` in the `KindImage` branch and multiply source alpha by the
   cached `RoundedMask` coverage. Keep the existing fast rectangular path when
   `Shape` and `Radius` are unset. Do not crop or decode in the renderer.

4. Run all image checks:

   ```bash
   GOMAXPROCS=4 go test -p 4 ./internal/render -run 'Test.*Image' -v
   ```

   Expected: PASS, including the old malformed-raster and clipping checks.

5. Add pure shell tests for candidate precedence. The helper returns the first
   existing regular file from:

   ```text
   <home>/.face.icon
   <home>/.face
   /var/lib/AccountsService/icons/<username>
   ```

   Pass the home directory and username into the candidate helper so tests use
   `t.TempDir`; do not read the root-only AccountsService account record.

6. Extend `ccIdentity` with the selected image path. `readCCIdentity` performs
   only the small standard-library path/stat selection while no registry lock
   exists. Decode remains with `internal/icons.Worker`.

7. Reuse the worker created by `BindTray`; production creates it even when the
   tray service is unavailable. At the 56px logical avatar size, use an
   `icons.Key` sized for `h.scale120`, return a cached image immediately, and
   otherwise enqueue once. Extend the worker callback to rebuild and invalidate
   an open control centre when that exact absolute-path key completes. A failed
   decode records that key as attempted so one-second metric rebuilds do not
   retry malformed input forever.

8. Compose the identity card as a 56px `ShapeCircle` image beside the existing
   three text lines. Until an image resolves, or when it fails, use a 56px
   circular container holding the existing `person` glyph. Both branches must
   have identical geometry.

9. Add one tree test for the image branch and one for the fallback. Run:

   ```bash
   PATH=/tmp/sysc-control-center-stubs:$PATH GOMAXPROCS=4 go test -p 4 \
     ./internal/shell ./internal/render ./internal/icons \
     -run 'Test(CCProfile|ControlCentre.*Avatar|.*Image.*Circle)' -v
   ```

   Expected: source precedence, circular image, and fallback all pass.

10. Commit only the files from this task:

   ```bash
   /usr/bin/git add internal/render/image.go internal/render/image_test.go \
     internal/render/paint.go internal/shell/controlcenter_pages.go \
     internal/shell/controlcenter_test.go internal/shell/registry.go \
     internal/shell/tray.go
   /usr/bin/git commit -m "fix(shell): show the account image on the dashboard"
   ```

### Task 2: Separate quick controls and reuse the radial gauges

**Files:**

- Modify: `internal/shell/controlcenter_pages.go`
- Test: `internal/shell/controlcenter_test.go`

1. Add a failing tree/layout test asserting the quick-access block contains two
   independent capsule/button controls in a row with `Gap: 8`, not one
   `KindSegmented`. Assert Caffeine alone carries selected state and Wallpaper
   remains an ordinary launch action.

2. Add a failing System-card test. Walk the Home tree and require exactly two
   `KindRadialGauge` nodes using `render.GaugeIconName("cpu")` and
   `render.GaugeIconName("memory")`. Each has a neighbouring tabular value.
   Cover sampled zero separately from an absent reading.

3. Run the failures:

   ```bash
   PATH=/tmp/sysc-control-center-stubs:$PATH GOMAXPROCS=4 go test -p 4 \
     ./internal/shell -run 'TestControlCentreHome(QuickAccess|RadialResources)' -v
   ```

4. Replace the segmented control with one 48px row containing two independent
   capsule buttons separated by 8px. Preserve the current actions, accessible
   names, and focusability. Do not change the block height.

5. Replace the CPU/memory text row with two equal groups. Each group contains a
   40px radial gauge, the project-owned glyph selected by
   `render.GaugeIconName`, and a visible tabular percentage beside it. Read the
   value once through `Snapshot.Fraction`; on failure set `Value: 0`,
   `Absent: true`, and text `—`. A valid zero sets `Absent: false` and text
   `0%`.

6. Run the Home and renderer gauge checks:

   ```bash
   PATH=/tmp/sysc-control-center-stubs:$PATH GOMAXPROCS=4 go test -p 4 \
     ./internal/shell ./internal/render \
     -run 'Test(ControlCentreHome|PaintRadial|Radial)' -v
   ```

   Expected: PASS; the Home root remains exactly 480px tall.

7. Commit:

   ```bash
   /usr/bin/git add internal/shell/controlcenter_pages.go \
     internal/shell/controlcenter_test.go
   /usr/bin/git commit -m "fix(shell): refine dashboard quick controls and resources"
   ```

### Task 3: Make the attached silhouette truthful and antialiased

**Files:**

- Modify: `internal/shell/panelhost.go`
- Test: `internal/shell/controlcenter_test.go`
- Modify: `internal/render/canvas.go`
- Test: `internal/render/canvas_test.go`
- Test: `internal/render/paint_test.go`

1. Add a failing `panelSpec` test for an opaque theme with a positive fillet
   margin. The returned callback must set `OpaqueBackground: false`. Retain a
   table row proving an opaque panel with no expanded margin still advertises
   its body as opaque.

2. Implement the narrow policy at `panelSpec`: when `fillet > 0`, disable the
   full-surface opaque hint. Mark the known ceiling:

   ```go
   // ponytail: omit the hint for fillet-expanded surfaces; add a body-aware
   // opaque-region API only if compositor profiling shows this matters.
   ```

3. Replace integer-only fillet spans with analytic per-pixel coverage. For a
   pixel centre `(x+0.5, y+0.5)` inside the radius band, derive circle coverage
   over a one-pixel transition, clamp it to `[0, 1]`, and blend the edge pixel
   through alpha rather than rounding the whole row to an opaque extent.
   `clearOutsideRoundedRect` must use the same curve so it does not erase the
   partial pixels just painted.

4. Replace `TestFilletExtentSweepsFromBarToPanel` with coverage-oriented table
   checks at physical radii corresponding to scales 1.0, 1.2, 1.25, and 1.5.
   Require at least one partial-alpha edge pixel, monotonic taper, full contact
   at the bar, and transparency beyond the curve. Keep the existing top/bottom
   symmetry check.

5. Run:

   ```bash
   GOMAXPROCS=4 go test -p 4 ./internal/render \
     -run 'Test(Fillet|ZeroFillet)' -v
   PATH=/tmp/sysc-control-center-stubs:$PATH GOMAXPROCS=4 go test -p 4 \
     ./internal/shell -run 'Test.*PanelSpec.*Fillet' -v
   ```

   Expected: partial coverage is present and the expanded panel has no opaque
   hint.

6. Commit:

   ```bash
   /usr/bin/git add internal/render/canvas.go internal/render/canvas_test.go \
     internal/render/paint_test.go internal/shell/panelhost.go \
     internal/shell/controlcenter_test.go
   /usr/bin/git commit -m "fix(render): smooth attached panel fillets"
   ```

### Task 4: Finish reveal, focus, Escape, and literal wordmark anchoring

**Files:**

- Modify: `internal/render/canvas.go`
- Test: `internal/render/canvas_test.go`
- Modify: `internal/shell/panelhost.go`
- Modify: `internal/shell/popout_controlcenter.go`
- Test: `internal/shell/controlcenter_test.go`

1. Re-run the rebased wordmark tests before editing. Recent `main` is
   authoritative for centred placement:

   ```bash
   PATH=/tmp/sysc-control-center-stubs:$PATH GOMAXPROCS=4 go test -p 4 \
     ./internal/shell -run 'Test.*Wordmark.*ControlCentre' -v
   ```

   Assert right-click uses the laid-out wordmark action centre. Left-click
   remains inert. IPC continues to use the focused output centre because it has
   no pointer anchor. If these pass, change no wordmark code.

2. Add canvas tests for applying a final surface opacity and a small signed
   vertical translation to already-premultiplied pixels. Multiplying alpha must
   multiply B, G, and R by the same factor; translation clears exposed rows and
   handles overlap without allocating a second frame buffer.

3. Connect `PanelHost.render` to the existing `animator.PanelOpacity` and
   `animator.PanelSlide` outputs after `render.Paint`. For a top bar, the panel
   begins toward the bar and settles down; reverse the direction for a bottom
   bar. Set `style.Fillet` from the same progress so the joint grows from zero
   to the resolved radius. Reduced motion keeps zero translation and no more
   than the existing 150ms fade.

4. Give the active page wrapper a stable key. On an enabled rail selection,
   compute the sign of the destination index relative to the current index,
   rebuild only the destination page, and retarget one `animVisible` value keyed
   to that wrapper. During paint, temporarily translate the page subtree by at
   most 8px and wash its viewport with the panel root fill at inverse progress;
   restore logical bounds before hit testing or the focus ring can observe them.
   A second selection retargets this value and still leaves one page tree.

5. Add focused tests proving:

   - `ui.Focusables` lists enabled rail entries before page controls;
   - a page swap preserves the activated rail slot;
   - Escape calls the existing root-close path;
   - only the selected page exists during a mid-transition retarget;
   - normal motion starts offset and transparent, then settles at full fillet;
   - reduced motion has zero spatial offset and settles within 150ms.

6. Run:

   ```bash
   PATH=/tmp/sysc-control-center-stubs:$PATH GOMAXPROCS=4 go test -p 4 \
     ./internal/shell ./internal/ui ./internal/render \
     -run 'Test(ControlCentre.*(Focus|Escape|Reveal|Retarget)|.*SurfaceTransform|.*Wordmark.*ControlCentre)' -v
   ```

   Expected: PASS.

7. Commit:

   ```bash
   /usr/bin/git add internal/render/canvas.go internal/render/canvas_test.go \
     internal/shell/panelhost.go internal/shell/popout_controlcenter.go \
     internal/shell/controlcenter_test.go
   /usr/bin/git commit -m "feat(shell): finish control centre reveal and focus"
   ```

### Task 5: Local verification

**Files:** none unless a task-owned regression fails.

1. Format and prove formatting:

   ```bash
   gofmt -w .
   test -z "$(gofmt -l .)"
   ```

2. Run the required capped package gate with command stubs:

   ```bash
   PATH=/tmp/sysc-control-center-stubs:$PATH GOMAXPROCS=4 go vet \
     ./internal/shell ./internal/ui ./internal/ipc ./internal/services \
     ./internal/render ./internal/icons ./cmd/sysc-shell
   PATH=/tmp/sysc-control-center-stubs:$PATH GOMAXPROCS=4 go test -p 4 \
     ./internal/shell ./internal/ui ./internal/ipc ./internal/services \
     ./internal/render ./internal/icons
   /usr/bin/git diff --exit-code -- go.mod go.sum
   /usr/bin/git diff --check
   ```

   Expected: every command exits zero. Do not widen this to a repo-wide race
   build on this machine.

3. Manually inspect the feature range for scope and ownership:

   ```bash
   /usr/bin/git diff --stat main...HEAD
   /usr/bin/git diff --check main...HEAD
   ```

   No NetworkManager, BlueZ, MPRIS, Settings redesign, or unrelated process-table
   work belongs in the range.

### Task 6: Laptop deployment and live scale-1.25 gate

**Files:** no repository changes unless live evidence exposes a defect.

1. Build a temporary Linux binary locally:

   ```bash
   GOMAXPROCS=4 go build -p 4 -o /tmp/sysc-shell.control-center ./cmd/sysc-shell
   ```

2. Copy it to the approved laptop and replace the old installed binary through
   a recoverable temporary path:

   ```bash
   scp -F /dev/null -P 7777 /tmp/sysc-shell.control-center \
     nomadx@192.168.0.64:/tmp/sysc-shell.control-center
   ssh -F /dev/null -p 7777 nomadx@192.168.0.64 \
     'cp ~/.local/bin/sysc-shell /tmp/sysc-shell.before-dashboard-correction && mv /tmp/sysc-shell.control-center ~/.local/bin/sysc-shell && systemctl --user restart sysc-shell.service && systemctl --user is-active sysc-shell.service'
   ```

   Expected: `active`. Keep `/tmp/sysc-shell.before-dashboard-correction` until
   the live gate passes.

3. On `eDP-1` at logical 1536×864 and scale 1.25, verify:

   1. Right-clicking the literal centred wordmark opens the panel from that
      centre; left-click does nothing.
   2. The identity card shows the account image when present, otherwise the
      circular person-glyph fallback without layout movement.
   3. Caffeine and Wallpaper are independent capsules with a visible 8px gap.
   4. CPU and memory are smooth gradient radial gauges with readable tabular
      values; missing values show dashes rather than zero.
   5. No black pixel or strip exists in the transparent side margins.
   6. Both panel-to-bar joints are smooth concave curves; lower corners remain
      convex.
   7. Tab walks enabled rail entries before page controls, Escape closes, page
      switches never show two page trees, and reduced motion has no spatial
      movement.

4. Capture the Home page using the temporary remote `grim` already installed
   for the preview:

   ```bash
   ssh -F /dev/null -p 7777 nomadx@192.168.0.64 \
     'env WAYLAND_DISPLAY=wayland-1 XDG_RUNTIME_DIR=/run/user/1000 /tmp/grim-sysc-preview -t png /tmp/sysc-control-center-dashboard-corrected.png'
   scp -F /dev/null -P 7777 \
     nomadx@192.168.0.64:/tmp/sysc-control-center-dashboard-corrected.png /tmp/
   ```

5. If the live gate passes, record the observation in `sysc-154`, close it with
   the shipped commit range, export carefully, and commit only the tracker
   change with the feature. If a defect remains, leave the issue in progress
   and fix only the responsible task layer.

## Stop condition

Stop when the focused local gate passes and the scale-1.25 laptop capture proves
the account fallback/image, 8px control gap, radial resources, clean transparent
margins, smooth bar joints, literal wordmark anchor, focus/Escape, and reduced
motion. Do not start the Media, Network, Bluetooth, Settings, or wider visual
redesign slices.
