# Bar visual parity implementation plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Default bar paints per-widget capsules (padded, `SurfaceContainer` fill, radius 12) and a workspace pill row whose focused dot uses theme `Accent`.

**Architecture:** New `ui.KindCapsule` is a one-child (or empty-dot) node. Layout adds padding and fills the content band; Paint `FillRounded` then the child. `ThemeFromTokens` finally maps `SurfaceContainer`. Workspace projection emits per-output dots; `buildWidgets` wraps every bar item.

**Tech Stack:** Go, existing `internal/ui` tree, `internal/render.FillRounded`, `internal/theme.Tokens`. No new dependency, no CGO, no icon font change.

**Design:** [2026-08-31-bar-visual-parity-design.md](2026-08-31-bar-visual-parity-design.md)

**Epic:** `sysc-43`

Do not implement until the owner approves the design. Do not touch `sysc-41` panel paint, `sysc-42` settings teardown, or the scale-1.25 truncation bug. Do not add launcher/tray/MPRIS widgets.

---

### Task 1: KindCapsule — measure and layout

**Files:**
- Modify: `internal/ui/tree.go` (append `KindCapsule` to the iota; add `ToneAccent`)
- Modify: `internal/ui/layout.go` (`measureNode` + `Layout` case)
- Test: `internal/ui/layout_test.go`

**Step 1: Write the failing test**

```go
func TestLayoutArrangesCapsuleAroundText(t *testing.T) {
	t.Parallel()
	root := &Node{Kind: KindRow, Padding: 4, Children: []*Node{{
		Kind: KindCapsule, Padding: 8,
		Children: []*Node{{Kind: KindText, Text: "11:37"}},
	}}}
	if err := Layout(root, Rect{W: 400, H: 40}, fakeMeasure); err != nil {
		t.Fatal(err)
	}
	cap := root.Children[0]
	// "11:37" is 5 glyphs * 8 = 40 wide, 16 tall; plus 16 padding.
	if cap.Bounds.W != 56 {
		t.Fatalf("capsule width = %d, want 56", cap.Bounds.W)
	}
	if cap.Bounds.H != 32 { // content band = 40 - 2*row padding
		t.Fatalf("capsule height = %d, want the content band", cap.Bounds.H)
	}
	child := cap.Children[0]
	if child.Bounds.W != 40 || child.Bounds.H != 16 {
		t.Fatalf("child bounds = %+v", child.Bounds)
	}
}

func TestCapsuleWithZeroWidthChildMeasuresZero(t *testing.T) {
	t.Parallel()
	root := &Node{Kind: KindRow, Children: []*Node{{
		Kind: KindCapsule, Padding: 8,
		Children: []*Node{{Kind: KindText, Text: ""}},
	}}}
	if err := Layout(root, Rect{W: 400, H: 40}, fakeMeasure); err != nil {
		t.Fatal(err)
	}
	if root.Children[0].Bounds.W != 0 {
		t.Fatal("empty title must not leave an empty pill")
	}
}

func TestEmptyCapsuleWithWidthIsASquareDot(t *testing.T) {
	t.Parallel()
	root := &Node{Kind: KindRow, Padding: 0, Children: []*Node{{
		Kind: KindCapsule, Width: 8,
	}}}
	if err := Layout(root, Rect{W: 400, H: 40}, fakeMeasure); err != nil {
		t.Fatal(err)
	}
	got := root.Children[0].Bounds
	if got.W != 8 || got.H != 8 {
		t.Fatalf("dot bounds = %+v, want 8x8", got)
	}
}
```

**Step 2: Run test to verify it fails**

```bash
go test ./internal/ui -run 'TestLayoutArrangesCapsuleAroundText|TestCapsuleWithZeroWidthChildMeasuresZero|TestEmptyCapsuleWithWidthIsASquareDot' -count=1
```

Expected: FAIL (`unsupported kind` or missing `KindCapsule`).

**Step 3: Minimal implementation**

Append `KindCapsule` after `KindVirtualList`. `ToneAccent` after `ToneError`.

`measureNode`:

- `KindCapsule` with no children: if `Width <= 0` return `0, 0, nil`; else return `n.Width, n.Width, nil`.
- With children: require exactly one; measure it with `contentHeight - 2*n.Padding` (clamp ≥ 0); if child width is 0 return `0, 0, nil`; else return `childW + 2*n.Padding, contentHeight, nil`.

`Layout` default path today just assigns `Bounds`. Add a `KindCapsule` case: assign capsule `Bounds`, then if it has a child, centre the child in the padded inner rect; if that child is `KindRow`/`KindColumn`, call `Layout` / `LayoutColumn` on the inner box.

**Step 4: Run test to verify it passes**

Same command. Expected: PASS. Also `go test ./internal/ui -count=1`.

**Step 5: Commit**

```bash
git commit -m "feat: KindCapsule measures padding and square dots"
```

---

### Task 2: Paint KindCapsule

**Files:**
- Modify: `internal/render/paint.go` (`ProofStyle.Capsule`; `paintNode` case)
- Test: `internal/render/paint_test.go`

**Step 1: Write the failing test**

Sample a pixel inside the capsule that is not on the text glyphs: it must equal `style.Capsule`, not `style.Background`. A `ToneAccent` empty 8×8 capsule pixel must equal `style.Accent`.

Reuse `newTestCanvas`, `mustTestFace`, `pixelAt` from the existing file. Set `style.Capsule` to a colour distinct from `Background` (e.g. `{R: 0x18, G: 0x1a, B: 0x1d, A: 0xff}`).

**Step 2: Run test to verify it fails**

```bash
go test ./internal/render -run TestPaintCapsuleFill -count=1
```

Expected: FAIL (capsule pixels are background, or `unsupported kind`).

**Step 3: Minimal implementation**

Add `Capsule Color` to `ProofStyle`. In `paintNode`, `KindCapsule`: choose fill (`ToneAccent` → `style.Accent`, else `style.Capsule`), `FillRounded` at `style.Scale120.PhysicalRect(n.Bounds)` with radius `min(physical Radius, half short side)`, then paint children the way `KindRow` does. Do not clear outside the capsule.

**Step 4: PASS** `go test ./internal/render -count=1`

**Step 5: Commit** `feat: paint KindCapsule with SurfaceContainer fill`

---

### Task 3: Map SurfaceContainer onto Theme.Capsule

**Files:**
- Modify: `internal/shell/theme.go` (`Theme.Capsule`, `CapsulePadding`; `DefaultTheme`; `ThemeFromTokens`)
- Modify: `internal/shell/bar.go` (`ProofStyle.Capsule` copy; `style.Radius` already from theme)
- Test: `internal/shell/theme_test.go` (`TestTokensResolveToBarTheme`)

**Step 1: Extend `TestTokensResolveToBarTheme`**

Include `SurfaceContainer: "#181a1d"` on the fixture tokens. Assert `th.Capsule == parseColor(tok.SurfaceContainer, Color{})`. Assert `DefaultTheme().CapsulePadding == 8`.

**Step 2: FAIL** `go test ./internal/shell -run TestTokensResolveToBarTheme -count=1`

**Step 3:** Map the field. Copy `theme.Capsule` into `ProofStyle.Capsule` in `NewBar`. Do not change `Muted` ← `OnSurfaceVariant`.

**Step 4:** PASS `go test ./internal/shell -run 'TestTokensResolveToBarTheme|TestDefaultTheme' -count=1`

**Step 5: Commit** `feat: theme capsule fill from SurfaceContainer`

---

### Task 4: Wrap every bar widget in a capsule

**Files:**
- Modify: `internal/shell/widget.go` (`buildWidgets`)
- Test: `internal/shell/bar_test.go` (existing arrangement tests)

**Step 1: Failing assertion**

In an existing test that builds a default bar (or a small helper), after layout the clock node's `Kind` is `KindCapsule`, its child is `KindText`, and `Padding == theme.CapsulePadding`. Workspace and window-title too.

If current tests read `left[0].node.Text` for workspace, they will break once the text lives on the child — update those assertions in Task 5 with the dots. For this task, wrap clock and window-title first **or** wrap all three but keep workspace as a capsule around the old `KindText` so existing text assertions still work via a helper `func widgetText(n *ui.Node) string` that returns the first `KindText` descendant. Prefer the helper in this task so Task 5 can replace the inner text without retouching every test.

**Step 2: FAIL** because nodes are still bare `KindText`.

**Step 3:** `buildWidgets` wraps each constructed node:

```go
func wrapCapsule(inner *ui.Node, pad int) *ui.Node {
	return &ui.Node{Kind: ui.KindCapsule, Padding: pad, Children: []*ui.Node{inner}}
}
```

`format` must write `inner.Text`, not the capsule. Closure already captures `node` in weather/battery — capture the inner node, store the capsule as `textWidget.node`.

`applyLocked` today compares `w.node.Text`. After wrap that field is empty. Change it to compare/write the inner text node (the single child, or a small `leafText(n *ui.Node) *ui.Node`). This is required in this task or clocks never update.

**Step 4:** `go test ./internal/shell -count=1` PASS.

**Step 5: Commit** `feat: wrap bar widgets in capsules`

---

### Task 5: Workspace dots from the Niri snapshot

**Files:**
- Modify: `internal/shell/projection.go` (`workspaceDot`, fill from snapshot)
- Modify: `internal/shell/widget.go` (workspace `refresh`)
- Modify: `internal/shell/bar.go` (`applyLocked` refresh path)
- Modify: `internal/shell/barview` / `barView` in `widget.go`
- Test: `internal/shell/projection_test.go`, `internal/shell/widget_test.go`

**Step 1: Failing tests**

Projection: snapshot with two workspaces on `DP-9` (index 1 occupied+focused, index 2 empty) and one on `HDMI-A-9`. `projectOutputs()["DP-9"].Dots` has length 2; `[0].Focused` true; `[1].Occupied` false. HDMI is unaffected.

Widget: `buildWidgets([]config.Item{{ID: "workspace"}})` then `refresh` with a view of three dots; inner row has three empty capsules, first `ToneAccent`, widths 8 / 8 / 6 (empty smaller per D6).

**Step 2: FAIL** (`Dots` does not exist).

**Step 3:**

```go
type workspaceDot struct {
	Index    int
	Occupied bool
	Focused  bool
}
```

On `outputState` add `Dots []workspaceDot`. Keep `Workspace string` as today for the focused label tests.

Occupied: `w.HasActiveWindow` or any `niri.Window` with `HasWorkspace && WorkspaceID == w.ID`.

`barView` carries `Dots []workspaceDot` from that output's projection (registry already passes per-output state into `apply` — thread `Dots` the same way as `Workspace`).

`textWidget`:

```go
type textWidget struct {
	node    *ui.Node
	format  func(barView) string
	refresh func(barView) bool // optional; true if tree changed
	tooltip string
}
```

`applyLocked`: if `refresh != nil`, `changed = refresh(view) || changed`; else existing text path via the leaf.

Workspace `refresh` rebuilds the inner `KindRow` children when the dot signature changes (index, occupied, focused). Signature can be a small loop; do not put it in `Text`.

**Step 4:** `go test ./internal/shell -count=1` PASS. `go test ./internal/ui ./internal/render -count=1` still PASS.

**Step 5: Commit** `feat: workspace widget paints theme-accent dots`

---

### Task 6: Registry wiring and the live eyeball

**Files:**
- Modify: `internal/shell/registry.go` only if `apply`/`barView` construction does not yet copy `Dots` (it will not until this task if Task 5 stubbed a direct `Bar.apply`).
- Test: `internal/shell/tranche3a_test.go` — workspace still non-zero width; long title still cannot squeeze it to zero. Update any `node.Text` assertions to the leaf helper.

**Step 1:** Run `go test ./...` on the branch. Fix any `Kind` / `.Text` fallout. Add no new widgets.

**Step 2:** Commit `fix: thread workspace dots through the registry` if a registry change was needed; otherwise skip.

**Step 3: Live grim (owner machine, not a `sysc-3` pass)**

Rebuild `~/.local/bin/sysc-shell` from the branch. `grim` the top 80px of `eDP-1`. Confirm: inner pills, padded clock/title, focused workspace is an accent disc, slab still `Surface`. Do not claim the live Niri gate. Record the grim path in the commit message if you add it under `docs/plans/assets/`.

**Step 4:** `bd close` the child issues; leave `sysc-3` open.

---

## Review boundary

One reviewed slice. Tasks 1–5 are unit-complete. Task 6 is wiring plus an owner grim. Do not start M5 widgets on this branch.
