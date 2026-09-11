# Surface stacking Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add one container kind whose children share its box instead of flowing, so a card can put content over a background image, and build the control-centre weather card on it.

**Architecture:** Paint already walks children forward and `Hit` already walks them in reverse, so "last child is topmost" is true in each today and needs no change. The only missing piece is a layout that hands every child the same content box, plus a measurement rule that takes the maximum where a column sums. Paint needs no new code at all — `KindStack` joins the existing container case.

**Tech Stack:** Go 1.26.4, no new dependencies.

**Spec:** `docs/plans/2026-09-11-surface-stacking-design.md`

## Global Constraints

- **Never run `go test ./...` or any `-race` build.** Run named tests in one package: `go test ./internal/ui -run TestStack`.
- `go test ./internal/shell` is safe to run directly — `runArgvDefault` (`popout_session.go:270`) refuses under `testing.Testing()`.
- Go only. No CGO, no new module.
- **`kindcoverage_test.go` requires every `Kind` to be measurable and paintable.** A kind that exists without both fails the package. Task 1 is therefore end-to-end by necessity, not by preference.
- AGENTS.md: "Add UI primitives only for an approved shell component. Do not build a general application toolkit." Task 5 is the consumer gate — if the weather card does not need this, delete the kind rather than keep it.
- **Depends on the blur plan's Task 4**, which adds `paintImageSmooth`. Do not start Task 4 here until that has landed on `main`.
- **The `commit-msg` hook rejects these substrings, case-insensitively:** `claude`, `anthropic`, `chatgpt`, `openai`, `copilot`, `cursor`, `cody`, `tabnine`, `codex`, `gemini`, `bard`, `gpt-[0-9]`, `llm`, `ai assistant`, `bot`, `agent`. Ordinary words trip it — `both` contains `bot`. Screen every message:
  ```bash
  grep -oiE "(claude|anthropic|chatgpt|openai|copilot|cursor|cody|tabnine|codex|gemini|bard|gpt-[0-9]|llm|ai assistant|bot|agent)" msg.txt && echo BANNED || echo clean
  ```
  No `Co-Authored-By` trailer. Never `--no-verify`.

## File Structure

| Path | Responsibility |
|---|---|
| `internal/ui/tree.go` | `KindStack` in the `Kind` const block |
| `internal/ui/layout.go` | `measureNode` case (max), placement inside `Layout` |
| `internal/ui/column.go` | `columnChildHeight` case (max, explicit height wins) |
| `internal/render/paint.go:322` | `KindStack` joins the existing container case |
| `internal/ui/kindcoverage_test.go` | `sampleNode` entry |
| `internal/shell/popout_*.go` | The weather card, Task 5 |

---

### Task 1: The kind, end to end

`kindcoverage_test.go` fails the package if a kind is not both measurable and paintable, so measure, layout and paint land together. Paint is one line — the kind joins a case that already does exactly the right thing.

**Files:**
- Modify: `internal/ui/tree.go` (`Kind` const block)
- Modify: `internal/ui/layout.go` (`measureNode`, `Layout`)
- Modify: `internal/ui/column.go` (`columnChildHeight`)
- Modify: `internal/render/paint.go:322`
- Modify: `internal/ui/kindcoverage_test.go`
- Test: `internal/ui/stack_test.go`

**Interfaces:**
- Consumes: `Node.Children`, `Node.Padding`, `Node.Bounds`.
- Produces: `ui.KindStack`.

- [ ] **Step 1: Write the failing tests**

Create `internal/ui/stack_test.go`:

```go
package ui

import "testing"

func stackOf(children ...*Node) *Node {
	return &Node{Kind: KindStack, Children: children}
}

func TestStackGivesEveryChildTheSameBox(t *testing.T) {
	t.Parallel()
	// The point of the kind: children occupy one another's space rather than
	// successive space.
	a := &Node{Kind: KindText, Text: "a"}
	b := &Node{Kind: KindText, Text: "b"}
	n := stackOf(a, b)
	n.Padding = 4
	n.Bounds = Rect{X: 10, Y: 20, W: 100, H: 50}

	if err := layoutStackChildren(n, fixedMeasure); err != nil {
		t.Fatal(err)
	}
	want := Rect{X: 14, Y: 24, W: 92, H: 42}
	if a.Bounds != want {
		t.Errorf("first child = %+v, want %+v", a.Bounds, want)
	}
	if b.Bounds != want {
		t.Errorf("second child = %+v, want %+v", b.Bounds, want)
	}
}

func TestStackMeasuresAsTheMaximumNotTheSum(t *testing.T) {
	t.Parallel()
	// A column sums its children. A stack must not, or every stacked card
	// reserves the height of all its layers added together.
	short := &Node{Kind: KindText, Text: "x"}
	tall := &Node{Kind: KindText, Text: "x", Height: 40}
	n := stackOf(short, tall)

	h, err := columnChildHeight(n, 100, fixedMeasure)
	if err != nil {
		t.Fatal(err)
	}
	if h < 40 {
		t.Errorf("height = %d, want at least the tallest child's 40", h)
	}
	if h >= 40+10 {
		t.Errorf("height = %d; that looks like a sum, not a maximum", h)
	}
}

func TestStackWithNoChildrenIsHarmless(t *testing.T) {
	t.Parallel()
	// The other containers degrade rather than error on an empty child list.
	n := stackOf()
	if err := layoutStackChildren(n, fixedMeasure); err != nil {
		t.Errorf("empty stack errored: %v", err)
	}
	if _, err := columnChildHeight(n, 100, fixedMeasure); err != nil {
		t.Errorf("empty stack failed to measure: %v", err)
	}
}

func TestStackRejectsANilChild(t *testing.T) {
	t.Parallel()
	// Every other container names the index. A nil child must not panic.
	n := stackOf(nil)
	if err := layoutStackChildren(n, fixedMeasure); err == nil {
		t.Error("a nil child was accepted")
	}
}
```

`fixedMeasure` is whatever measure stub `internal/ui`'s existing tests use — read `layout_test.go` and use that name rather than adding one.

- [ ] **Step 2: Run and watch them fail**

Run: `go test ./internal/ui -run TestStack -v`
Expected: FAIL — `undefined: KindStack`.

- [ ] **Step 3: Add the kind**

In `internal/ui/tree.go`, in the `Kind` const block, at the **end** so existing values do not shift:

```go
	// KindStack lays every child into its own content box rather than flowing
	// them. Children paint in order, so the last is on top, and Hit already
	// walks children in reverse, so the topmost is hit first. It exists for a
	// card with a background image behind its content.
	KindStack
```

- [ ] **Step 4: Add layout and measurement**

In `internal/ui/layout.go`:

```go
// layoutStackChildren lays every child into the stack's content box. They
// overlap by design; paint order decides what is visible and Hit's reverse
// walk decides what is clicked.
func layoutStackChildren(n *Node, measure MeasureText) error {
	if len(n.Children) == 0 {
		return nil
	}
	inner := Rect{
		X: n.Bounds.X + n.Padding,
		Y: n.Bounds.Y + n.Padding,
		W: max(n.Bounds.W-2*n.Padding, 0),
		H: max(n.Bounds.H-2*n.Padding, 0),
	}
	for i, child := range n.Children {
		if child == nil {
			return fmt.Errorf("ui: stack child %d is nil", i)
		}
		switch child.Kind {
		case KindColumn:
			if err := LayoutColumn(child, inner, measure); err != nil {
				return err
			}
		case KindRow:
			if err := Layout(child, inner, measure); err != nil {
				return err
			}
		default:
			child.Bounds = inner
		}
	}
	return nil
}
```

Add the measure case to `measureNode`, beside `KindRow`:

```go
	case KindStack:
		// The maximum, not the sum: children share one box. An explicit size
		// is a reserved box, the way it is for a capsule and a text field --
		// measuring the children instead let the two paths disagree.
		if n.Width > 0 && n.Height > 0 {
			return n.Width, n.Height, nil
		}
		w, h := 2*n.Padding, 2*n.Padding
		for i, child := range n.Children {
			if child == nil {
				return 0, 0, fmt.Errorf("stack child %d is nil", i)
			}
			cw, ch, err := measureNode(child, contentHeight, measure)
			if err != nil {
				return 0, 0, err
			}
			w = max(w, cw+2*n.Padding)
			h = max(h, ch+2*n.Padding)
		}
		if n.Width > 0 {
			w = n.Width
		}
		if n.Height > 0 {
			h = n.Height
		}
		return w, h, nil
```

And to `columnChildHeight` in `internal/ui/column.go`:

```go
	case KindStack:
		if n.Height > 0 {
			return n.Height, nil
		}
		tallest := 0
		for i, child := range n.Children {
			if child == nil {
				return 0, fmt.Errorf("stack child %d is nil", i)
			}
			h, err := columnChildHeight(child, width-2*n.Padding, measure)
			if err != nil {
				return 0, err
			}
			tallest = max(tallest, h)
		}
		return tallest + 2*n.Padding, nil
```

Call `layoutStackChildren` from `placeColumnChild` and from `Layout`'s child switch wherever a `KindColumn` child is currently laid out, so a stack works in a column and in a row.

- [ ] **Step 5: Add paint — one line**

In `internal/render/paint.go`, add the kind to the existing container case:

```go
	// Segmented rows own allocation, not chrome: each segment paints itself.
	// A stack's children overlap; painting them in order is exactly right,
	// because the last child is the topmost one.
	case ui.KindColumn, ui.KindDropZone, ui.KindSegmented, ui.KindStack:
```

- [ ] **Step 6: Add coverage**

In `internal/ui/kindcoverage_test.go`, extend `sampleNode`:

```go
	case KindRow, KindColumn, KindMenu, KindCapsule, KindDropZone, KindStack:
		n.Children = []*Node{{Kind: KindText, Text: "c"}}
```

- [ ] **Step 7: Run everything in the affected packages**

Run: `go test ./internal/ui -run 'TestStack|TestKind' -v && go test ./internal/render -run TestPaint`
Expected: PASS, including `kindcoverage`.

- [ ] **Step 8: Commit**

```bash
git add internal/ui/ internal/render/paint.go
git commit -m "feat(ui): add a stack container whose children share its box"
```

---

### Task 2: Overlap behaves correctly for hit testing

No production change expected — this pins behaviour the design claims already holds, so a later refactor cannot silently break it.

**Files:**
- Test: `internal/ui/stack_test.go`

- [ ] **Step 1: Write the test**

```go
func TestStackHitReturnsTheTopmostChild(t *testing.T) {
	t.Parallel()
	// Hit walks children in reverse, so the last child -- the one painted on
	// top -- must win. Without this, a scrim laid over an image would swallow
	// nothing and the wrong node would answer the click.
	under := &Node{Kind: KindButton, Text: "under", Action: "under", Bounds: Rect{W: 50, H: 50}}
	over := &Node{Kind: KindButton, Text: "over", Action: "over", Bounds: Rect{W: 50, H: 50}}
	root := &Node{Kind: KindRow, Bounds: Rect{W: 50, H: 50}, Children: []*Node{
		{Kind: KindStack, Bounds: Rect{W: 50, H: 50}, Children: []*Node{under, over}},
	}}

	action, ok := Hit(root, 25, 25)
	if !ok {
		t.Fatal("no hit inside the stack")
	}
	if action != "over" {
		t.Errorf("hit = %q, want the topmost child %q", action, "over")
	}
}
```

- [ ] **Step 2: Run it**

Run: `go test ./internal/ui -run TestStackHit -v`
Expected: PASS immediately. If it fails, `Hit`'s traversal is not what the design measured — stop and re-read `layout.go:523` before changing anything.

- [ ] **Step 3: Commit**

```bash
git add internal/ui/stack_test.go
git commit -m "test(ui): pin topmost-wins hit testing for overlapping children"
```

---

### Task 3: A scrim child reads over a background

**Files:**
- Test: `internal/render/paint_test.go`

**Interfaces:**
- Consumes: `ui.FillScrim`, `KindStack`.

- [ ] **Step 1: Write the test**

```go
func TestStackScrimDarkensWhatIsBeneathIt(t *testing.T) {
	t.Parallel()
	// The scrim is an ordinary child rather than a property, so its presence,
	// extent and order are visible in the tree. A card with a bright
	// background needs it or the text over it is unreadable.
	bright := &ui.Node{Kind: ui.KindCapsule, Fill: ui.FillContainerHighest, Bounds: ui.Rect{W: 20, H: 20}}
	scrim := &ui.Node{Kind: ui.KindCapsule, Fill: ui.FillScrim, Bounds: ui.Rect{W: 20, H: 20}}

	withScrim := paintStackTo(t, bright, scrim)
	without := paintStackTo(t, bright)

	if withScrim == without {
		t.Fatal("the scrim child changed nothing")
	}
}
```

`paintStackTo` builds a canvas, paints a `KindStack` holding the given children, and returns a comparable summary. Use the helper `paint_test.go` already has for rendering a tree to a canvas rather than adding a new one.

- [ ] **Step 2: Run it**

Run: `go test ./internal/render -run TestStackScrim -v`
Expected: PASS if `FillScrim` resolves to a translucent wash; FAIL if it paints opaque, which would mean the scrim hides rather than dims. If it fails, check `fillPair` for `FillScrim` before changing the test.

- [ ] **Step 3: Commit**

```bash
git add internal/render/paint_test.go
git commit -m "test(render): confirm a scrim child dims what sits beneath it"
```

---

### Task 4: Background images sample smoothly

**Depends on the blur plan's Task 4.** Do not start until `paintImageSmooth` is on `main`.

**Files:**
- Modify: `internal/render/paint.go` (`ui.KindImage` case)
- Test: `internal/render/paint_test.go`

- [ ] **Step 1: Write the failing test**

```go
func TestBackgroundImageInAStackSamplesSmoothly(t *testing.T) {
	t.Parallel()
	// A card background is one decoded image scaled to whatever the card
	// measures. Nearest sampling bands visibly across a large flat area --
	// which is the opposite of an icon, where the worker already produced the
	// exact requested size and resampling would be a second, worse scaler.
	src := &ui.Image{Width: 2, Height: 1, Stride: 8, Pix: []byte{
		0, 0, 0, 0xff,
		0xff, 0xff, 0xff, 0xff,
	}}
	img := &ui.Node{Kind: ui.KindImage, Image: src, Background: true, Bounds: ui.Rect{W: 32, H: 4}}
	out := paintStackTo(t, img)
	if !hasIntermediateValues(out) {
		t.Error("the background banded; it is not using the smooth path")
	}
}
```

- [ ] **Step 2: Run and watch it fail**

Run: `go test ./internal/render -run TestBackgroundImage -v`
Expected: FAIL — `Background` undefined on `ui.Node`.

- [ ] **Step 3: Add the flag and route it**

Add to `ui.Node`, beside the other image fields:

```go
	// Background marks an image that fills a container rather than standing in
	// for an icon. It selects bilinear sampling: an icon is produced at the
	// size the node asked for, a background is scaled to whatever the card
	// measures.
	Background bool
```

In `paintNode`'s `ui.KindImage` case, choose the sampler:

```go
	if n.Background {
		paintImageSmooth(c, style.Scale120.PhysicalRect(n.Bounds), n.Image)
	} else {
		paintImage(c, style.Scale120.PhysicalRect(n.Bounds), n.Image)
	}
```

- [ ] **Step 4: Run and watch it pass**

Run: `go test ./internal/render -run 'TestBackgroundImage|TestPaintImage' -v`
Expected: PASS, and the existing icon tests still assert nearest sampling.

- [ ] **Step 5: Commit**

```bash
git add internal/ui/tree.go internal/render/paint.go internal/render/paint_test.go
git commit -m "feat(render): sample container backgrounds bilinearly"
```

---

### Task 5: The consumer gate — the weather card

The design is explicit: "One consumer, or this does not ship." If the weather card can be built acceptably without a full-bleed image, **revert Tasks 1 to 4** rather than leaving an unused primitive behind.

**Files:**
- Modify: the control-centre weather card in `internal/shell/`
- Test: `internal/shell/surfacerole_test.go`

- [ ] **Step 1: Find the current weather card**

```bash
grep -rn "weather" internal/shell/*.go | grep -v _test | grep -iE "card|home|control"
```

- [ ] **Step 2: Decide, and record the decision**

Build it as a `KindStack` of background image, scrim, then content. If the card has no image to show — no weather artwork is shipped and none is being added by this plan — then it does **not** need a stack, and the honest outcome is to revert.

Write the decision into the commit body either way. "We kept it because it might be useful later" is precisely what AGENTS.md forbids.

- [ ] **Step 3: If building, write the failing test**

```go
func TestWeatherCardStacksContentOverItsBackground(t *testing.T) {
	t.Parallel()
	_, h := panelAtDensity(t, PanelControlCenter, theme.DensityStandard)
	var stacks int
	walkNodes(h.root, func(n *ui.Node) {
		if n.Kind == ui.KindStack {
			stacks++
			if len(n.Children) < 2 {
				t.Error("a stack with one child should be a plain container")
			}
		}
	})
	if stacks == 0 {
		t.Fatal("the weather card is not built on a stack")
	}
}
```

- [ ] **Step 4: Run, implement, run**

Run: `go test ./internal/shell -run TestWeatherCard -v`

- [ ] **Step 5: Commit**

```bash
git add internal/shell/
git commit -m "feat(shell): build the weather card on a stacked background"
```

---

### Task 6: Register the outcome

- [ ] **Step 1: Record in bd**

```bash
cd /home/nomadx/sysc-shell
bd create "Surface stacking: weather card consumer" --deps discovered-from:sysc-253
bd export -o .beads/issues.jsonl
wc -l .beads/issues.jsonl
git diff --stat .beads/issues.jsonl
```

Check the line count and diff before committing — `bd export` overwrites the tracked JSONL with only what it thinks changed.

- [ ] **Step 2: Commit**

```bash
git add .beads/issues.jsonl
git commit -m "chore: track the stacking consumer"
```

---

## Self-Review

**Spec coverage.** D1 `KindStack`, children share the content box → Task 1. D2 measure as maximum, explicit height reserved → Task 1 Step 4, asserted in two tests. D3 scrim as a child → Task 3. D4 bilinear backgrounds → Task 4, with the blur-plan dependency stated in Global Constraints and again in the task header. D5 one consumer or delete → Task 5, written as a real gate with a revert branch. D6 damage interaction → deliberately absent: rectangle damage is unadopted by any surface at the end of the smoothness plan, so a stack contributes nothing and there is nothing to implement yet. D7 testing → Tasks 1 to 4. D8 tracker → Task 6. D9 risks: the `kindcoverage` constraint is stated up front and forces Task 1 to be end-to-end; the "layout code that assumes siblings are disjoint" risk is partly answered by Task 2, which pins hit testing.

**Placeholders.** None. Task 5 Step 2 is a decision point with both branches specified, not a deferral. Two tasks say "use the existing helper" and name the file to read.

**Type consistency.** `layoutStackChildren(n *Node, measure MeasureText) error` is used with that signature in Task 1's tests and its callers. `columnChildHeight(n, width, measure) (int, error)` matches the existing function it extends. `KindStack` is added once and referenced in `measureNode`, `columnChildHeight`, `paintNode`, `sampleNode` and three test files. `Node.Background` is added in Task 4 and read only in Task 4's paint site.
