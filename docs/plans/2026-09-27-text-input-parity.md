# Text Input Parity Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Every text field in sysc-shell — plugin fields included — edits like a QML `TextInput`: real keyboard layouts, caret and word motion, selection, system clipboard, undo, fields that keep their width and scroll to the caret, and pointer caret placement.

**Architecture:** Keys resolve once at the platform edge (`internal/platform/wayland`) into `Event.Sym/Text/Mods`, first through the existing US table, finally through pinned pure-Go `xkb-go`. `ui.Field` becomes the pure editing engine behind `HandleKey`. The shell routes focused-field keys through it, owns scroll/selection view state, and forwards clipboard requests to the platform through a new request channel.

**Tech Stack:** Go 1.26, `github.com/Nomadcxx/sysc-wayland` v0.2.2 (generated `wl_data_device*`), `github.com/thegrumpylion/xkb-go` v0.1.0 (new), `github.com/rivo/uniseg` v0.4.7 (promoted from indirect).

**Spec:** `docs/plans/2026-09-27-text-input-parity-design.md` (`sysc-623`). Read it before Task 1.

## Global Constraints

- Build and test only from your own worktree off `origin/main`, never from `/home/nomadx/sysc-shell` itself (its `main` is behind and holds other sessions' changes).
- Every repo-wide Go command is capped: `GOMAXPROCS=4 go test -p 2 ...`. An uncapped `./...` is blocked by a hook.
- Commit messages must not contain the words the global hook rejects, including substrings: `bot` (so not "both", "bottom"), `agent`, `cursor` (write "caret"), `llm`, `codex`, `claude`. No attribution trailers.
- No plugin protocol change and no protocol minor bump (spec D10).
- No cgo. The only new module is `github.com/thegrumpylion/xkb-go` pinned at `v0.1.0`; record any fork by commit in the design doc.
- Blocking I/O never runs on the platform owner goroutine (spec D5).
- Phase 4 (Tasks 13–14) starts only after `sysc-591` Tasks 5–6 (key-repeat retargeting in `internal/platform/wayland/keyboard.go`) are on `origin/main`. Check with `bd show sysc-591` and `git log origin/main -- internal/platform/wayland/keyboard.go`.
- Deploying to a machine follows the deploy rule: `origin/main` must be an ancestor of the build, the deployed `vcs.revision` must be an ancestor of HEAD, keep a rollback copy, report `vcs.revision`/`vcs.modified`, and redeploy a clean `origin/main` build after testing a branch.

## Review Focus

1. **A plugin reseeds a field while the user has a selection.** Expect the selection to collapse to the reseeded caret and undo history to clear, not a stale selection over new text. Test: Task 7 `TestSyncFromNewTextClearsSelectionAndHistory`.
2. **Typing into a settings field whose builder writes the caret at the end.** Expect Left/Right to keep working rather than the caret snapping back to the end each keystroke. Test: Task 7 `TestSyncFromSameTextKeepsFieldCaret`.
3. **A paste bigger than a single-line field, containing newlines and NUL.** Expect one line, no NUL, one undo step, the field scrolled to the caret. Test: Task 12 `TestPasteIntoSingleLineFieldFlattensAndIsOneUndoStep`.
4. **Caps Lock on under a real keymap.** `xkb-go` v0.1.0 returns lowercase for Caps+A (probed 2026-09-27); expect "A", and "a" with Caps+Shift. Test: Task 13 `TestCapsLockUppercasesLetters`.
5. **A key held through a modifier change.** Expect repeats to type the new level (Shift pressed mid-hold turns `aaa` into `AAA`), not the first press's text. Test: Task 14 `TestRepeatResolvesTextAtDeliveryTime`.

## Fidelity notes — where this plan departs from the design's wording

1. **`Event.Mods` is `ui.Mods`, not a separate `wayland.Mods`.** `internal/platform/wayland` already imports `internal/ui`, so one type serves both; D1's "wayland.Mods converts to it" collapses.
2. **Scroll state lives on `ui.Field`, not `retainedEditor`.** Only plugin fields have a `retainedEditor`; searches, the password, the Bluetooth PIN, menu filters and settings entries own a bare `ui.Field`. `Field.ScrollX/ScrollY` serve all of them.
3. **Only the focused field paints its selection.** D8's "an unfocused field keeps its selection but paints it in the outline tone" is dropped: the painter has no focus state today and the render pass marks only the focused copy. The `Field` keeps the selection, so it reappears on refocus.
4. **The IME cursor rectangle is deferred.** D7 says the `text-input-v3` cursor rectangle "reports the scrolled caret", but nothing sets a cursor rectangle today (`internal/platform/wayland/textinput.go` has no `SetCursorRectangle` call). Adding it is a new shell→platform channel; Task 15 files it as a follow-up.
5. **Modifier masks decode by fixed real-modifier bits**, not `Keymap.ModGetIndex`, in both the fallback and the xkb path. `xkb-go` v0.1.0 returns −1 from `ModGetIndex` for every name on a keymap parsed from text (probed on `de` and `us(intl)`); real modifiers occupy fixed bits by XKB definition (Shift 0, Lock 1, Control 2, Mod1 3, Mod4 6, Mod5 7).
6. **A text field with `Width == 0` fills the row only where a column would** — as the last child, or the first child of a `PinEnd` row — otherwise it takes 120 logical px. D6 said "fills the row's remaining width"; filling from a middle position would starve later siblings.
8. **Pointer caret placement is single-line only.** D9 covers every field; multiline click, drag and multi-click keep today's focus-only behaviour in this slice (Task 10), because multiline hit testing needs per-line measurement the painter does not expose yet. Keyboard editing covers multiline fully.
7. **`PanelHost.keyPress(r, key uint32)` stays** as a wrapper that resolves through the fallback, so the dozens of existing tests calling it keep working. The live path is the new `keyEvent(r, e)`.

## File Map

| File | Responsibility |
|---|---|
| `internal/ui/keys.go` (new) | `Mods`, `KeyInput`, keysym constants, `ModsFromMask`, `FallbackKey` |
| `internal/ui/textfield.go` | `Field` state: anchor, scroll, history; `SyncFrom`/`SyncTo`/`Node` carry selection |
| `internal/ui/textedit.go` (new) | grapheme and word boundaries, motion, deletion, selection operations |
| `internal/ui/textundo.go` (new) | undo/redo history and coalescing |
| `internal/ui/textkey.go` (new) | `FieldResult`, `Field.HandleKey` key table |
| `internal/ui/textscroll.go` (new) | `KeepCaretVisible` |
| `internal/ui/tree.go` | render-only `ScrollX`, `ScrollY`, `SelStart`, `SelEnd`, `Editing` on `Node` |
| `internal/ui/layout.go`, `internal/ui/column.go` | text field width rule |
| `internal/render/paint.go` | scrolled text, selection fill, `FieldTextRect` shared with hit testing |
| `internal/platform/wayland/keyboard.go` | modifier tracking; one `keyEvent` builder for press, release and repeat |
| `internal/platform/wayland/client.go` | `Event.Sym/Text/Mods`, `EventPaste`, `Callbacks.Selection`, keymap and modifiers handlers |
| `internal/platform/wayland/selection.go` (new) | `wl_data_device` binding, copy source, paste reads |
| `internal/platform/wayland/wake.go` | selection requests and paste results cross to the owner |
| `internal/platform/wayland/keymap.go` (new) | xkb keymap, state, compose, Caps Lock workaround |
| `internal/shell/panelhost.go` | `fieldFor`, `keyEvent`, `h.mods`, editor view pass, pointer caret, paste |
| `internal/shell/selection.go` (new) | `Registry` selection request channel |
| `cmd/sysc-shell/main.go` | wire `Callbacks.Selection` |

---

## Phase 1 — Layout and scroll

### Task 1: A text field's width is its own

**Files:**
- Modify: `internal/ui/layout.go` (`measureNode` case `KindTextField`, and the row loop's `default:` case)
- Test: `internal/ui/textfield_layout_test.go` (new)

**Interfaces:**
- Produces: `const MinTextFieldWidth = 120` in `internal/ui/layout.go`.

- [ ] **Step 1: Write the failing tests**

```go
package ui

import "testing"

func fixedMeasure(s string, _ TextAttrs) (int, int) { return len(s) * 8, 16 }

func fieldRow(pinEnd bool, kids ...*Node) *Node {
	return &Node{Kind: KindRow, Gap: 8, PinEnd: pinEnd, Children: kids}
}

// A field declared 100 wide holds 100 whatever it contains: growth pushed the
// github-notifications "Mark all read" pill across the panel.
func TestTextFieldKeepsDeclaredWidthAsTextGrows(t *testing.T) {
	for _, text := range []string{"", "short", "a query far longer than the field could ever show"} {
		field := &Node{Kind: KindTextField, Text: text, Width: 100, Height: 40, Focusable: true, Name: "q", Role: "textbox"}
		root := fieldRow(false, field, &Node{Kind: KindText, Text: "tail"})
		if err := Layout(root, Rect{W: 400, H: 40}, fixedMeasure); err != nil {
			t.Fatal(err)
		}
		if field.Bounds.W != 100 {
			t.Fatalf("text %q: width %d, want 100", text, field.Bounds.W)
		}
	}
}

// With no declared width a trailing field fills what the row has left.
func TestTrailingTextFieldWithoutWidthFillsTheRow(t *testing.T) {
	field := &Node{Kind: KindTextField, Height: 40, Focusable: true, Name: "q", Role: "textbox"}
	root := fieldRow(false, &Node{Kind: KindText, Text: "Find"}, field)
	if err := Layout(root, Rect{W: 400, H: 40}, fixedMeasure); err != nil {
		t.Fatal(err)
	}
	if want := 400 - 32 - 8; field.Bounds.W != want {
		t.Fatalf("width %d, want %d", field.Bounds.W, want)
	}
}

// A leading field in a pinned row fills up to the pinned trailing control.
func TestLeadingFieldInPinnedRowFillsToThePin(t *testing.T) {
	field := &Node{Kind: KindTextField, Height: 40, Focusable: true, Name: "q", Role: "textbox"}
	pill := &Node{Kind: KindButton, Text: "Go", Action: "go", Focusable: true, Name: "Go", Role: "button"}
	root := fieldRow(true, field, pill)
	if err := Layout(root, Rect{W: 400, H: 40}, fixedMeasure); err != nil {
		t.Fatal(err)
	}
	if field.Bounds.X+field.Bounds.W+8 != pill.Bounds.X {
		t.Fatalf("field ends at %d, pill starts at %d", field.Bounds.X+field.Bounds.W, pill.Bounds.X)
	}
}

// A middle field with no width takes the minimum, not the row.
func TestMiddleTextFieldWithoutWidthTakesTheMinimum(t *testing.T) {
	field := &Node{Kind: KindTextField, Height: 40, Focusable: true, Name: "q", Role: "textbox"}
	root := fieldRow(false, &Node{Kind: KindText, Text: "a"}, field, &Node{Kind: KindText, Text: "b"})
	if err := Layout(root, Rect{W: 400, H: 40}, fixedMeasure); err != nil {
		t.Fatal(err)
	}
	if field.Bounds.W != MinTextFieldWidth {
		t.Fatalf("width %d, want %d", field.Bounds.W, MinTextFieldWidth)
	}
}
```

- [ ] **Step 2: Run to verify they fail**

Run: `GOWORK=off go test ./internal/ui -run 'TextField|LeadingField' -count=1`
Expected: FAIL — `MinTextFieldWidth` undefined; after defining it, the first test fails with a width above 100.

- [ ] **Step 3: Implement**

In `measureNode`, replace the `case KindTextField:` body's width logic:

```go
	case KindTextField:
		// A field's width is declared, never measured from its text: sizing
		// to the text made a field grow as the user typed and push siblings.
		// The height still fits one padded line.
		sample := DisplayText(n) + DisplayPreedit(n)
		if sample == "" {
			sample = " "
		}
		_, h := measure(sample, TextAttrsOf(n))
		w := n.Width
		if w <= 0 {
			w = MinTextFieldWidth
		}
		return w, max(n.Height, h+2*n.Padding), nil
```

Add near the top of `layout.go`:

```go
// MinTextFieldWidth is what a text field with no declared width takes when it
// is not in a position to fill its row.
const MinTextFieldWidth = 120
```

In the row loop's `default:` case, before `if w > remain {`:

```go
			// A field with no declared width fills the row where a column
			// would: as the last child, or the leading child of a pinned row.
			if child.Kind == KindTextField && child.Width <= 0 &&
				(i == len(root.Children)-1 || (root.PinEnd && len(root.Children) == 2 && i == 0)) {
				w = max(remain, MinTextFieldWidth)
			}
```

- [ ] **Step 4: Run the package**

Run: `GOWORK=off go test ./internal/ui -count=1`
Expected: PASS. If a pre-existing test asserted text-driven field growth, update its expectation to the declared width and say so in the commit body.

- [ ] **Step 5: Run the dependent packages**

Run: `GOMAXPROCS=4 GOWORK=off go test -p 2 -count=1 ./internal/render ./internal/plugin ./plugin/... ./internal/shell`
Expected: PASS except the four `internal/shell` failures already on `origin/main` (`TestPanelSectionValidationPrecedesMutation`, `TestRightClickingTheBarBatteryOpensSession`, `TestRightClickingBatteryCapsulePaddingOpensSession`, `TestABatteryWidgetOpensTheSessionPanel`). Confirm any other failure also fails on a clean `origin/main` worktree before calling it pre-existing.

- [ ] **Step 6: Commit**

```bash
git add internal/ui/layout.go internal/ui/textfield_layout_test.go
git commit -m "fix(ui): a text field keeps its declared width as its text grows"
```

### Task 2: Scroll arithmetic and render-only field view state

**Files:**
- Create: `internal/ui/textscroll.go`, `internal/ui/textscroll_test.go`
- Modify: `internal/ui/tree.go` (after the `Cursor int` field), `internal/ui/textfield.go` (`Field` struct)

**Interfaces:**
- Produces:
  - `func KeepCaretVisible(scroll, caretX, textW, viewW, margin int) int`
  - `Node` render-only fields: `ScrollX int`, `ScrollY int`, `SelStart int`, `SelEnd int`, `Editing bool`
  - `Field` fields: `ScrollX int`, `ScrollY int`

- [ ] **Step 1: Write the failing tests**

```go
package ui

import "testing"

func TestKeepCaretVisible(t *testing.T) {
	cases := []struct {
		name                                   string
		scroll, caretX, textW, viewW, margin, want int
	}{
		{"fits: never scrolls", 40, 10, 80, 100, 8, 0},
		{"caret past the right edge", 0, 150, 300, 100, 8, 58},
		{"caret before the left edge", 120, 60, 300, 100, 8, 52},
		{"caret inside the window stays put", 50, 100, 300, 100, 8, 50},
		{"never past the end", 250, 300, 300, 100, 8, 200},
		{"never negative", 5, 2, 300, 100, 8, 0},
		{"zero view", 30, 10, 300, 0, 8, 0},
	}
	for _, tc := range cases {
		if got := KeepCaretVisible(tc.scroll, tc.caretX, tc.textW, tc.viewW, tc.margin); got != tc.want {
			t.Errorf("%s: got %d, want %d", tc.name, got, tc.want)
		}
	}
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `GOWORK=off go test ./internal/ui -run TestKeepCaretVisible -count=1`
Expected: FAIL — undefined `KeepCaretVisible`.

- [ ] **Step 3: Implement**

`internal/ui/textscroll.go`:

```go
package ui

// KeepCaretVisible returns the horizontal scroll, in logical px, that keeps a
// caret at caretX inside a view viewW wide, margin clear of either edge. It
// never scrolls past the end of the text, so no dead space follows the last
// glyph, and never below zero. Pure: the host measures, this decides.
func KeepCaretVisible(scroll, caretX, textW, viewW, margin int) int {
	if viewW <= 0 || textW <= viewW {
		return 0
	}
	margin = min(max(margin, 0), viewW/2)
	if caretX-scroll > viewW-margin {
		scroll = caretX - (viewW - margin)
	}
	if caretX-scroll < margin {
		scroll = caretX - margin
	}
	return min(max(scroll, 0), textW-viewW)
}
```

In `internal/ui/tree.go`, after `Cursor int`:

```go
	// ScrollX and ScrollY are the render-time scroll of a focused text field:
	// logical px along a single line, whole lines in a multiline field. The
	// host sets them on the paint copy; plugins never do.
	ScrollX int
	ScrollY int
	// SelStart and SelEnd bound the selected bytes of Text. Equal means no
	// selection. Render-only, like Cursor.
	SelStart int
	SelEnd   int
	// Editing marks the one text field that has keyboard focus on this paint
	// copy: it scrolls to its caret and paints its selection instead of
	// ellipsizing from the start.
	Editing bool
```

In `Field`, after `Masked bool`:

```go
	// ScrollX and ScrollY are view state kept with the editor so a rebuild
	// does not jump the view: px for a single line, lines for multiline.
	ScrollX int
	ScrollY int
```

- [ ] **Step 4: Run**

Run: `GOWORK=off go test ./internal/ui -count=1`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/ui/textscroll.go internal/ui/textscroll_test.go internal/ui/tree.go internal/ui/textfield.go
git commit -m "feat(ui): caret-following scroll arithmetic and field view state"
```

### Task 3: Focused fields scroll to the caret on screen

**Files:**
- Modify: `internal/render/paint.go` (`paintTextField`, new `FieldTextRect`)
- Modify: `internal/shell/panelhost.go` (extract `fieldFor` from `editField`; new `applyEditorView`; call it in `render`)
- Test: `internal/render/paint_textfield_scroll_test.go` (new), `internal/shell/texteditview_test.go` (new)

**Interfaces:**
- Consumes: `KeepCaretVisible`, `Node.ScrollX/ScrollY/Editing` (Task 2).
- Produces:
  - `func FieldTextRect(n *ui.Node) ui.Rect` in `internal/render` — the logical text box inside a field after padding, the search glyph and the clear glyph. Single source for painter and hit testing.
  - `func (h *PanelHost) fieldFor(n *ui.Node) *ui.Field` — the retained `ui.Field` backing a text-field node, created on first use, synced with `SyncFrom`. Returns nil for a non-field.
  - `func (h *PanelHost) applyEditorView(root *ui.Node)` — on the paint copy, marks the focused field `Editing`, sets `ScrollX/ScrollY`, `SelStart/SelEnd`.

- [ ] **Step 1: Write the failing render test**

```go
package render

import (
	"testing"

	"github.com/Nomadcxx/sysc-shell/internal/ui"
)

// An editing field shows the end of a long value, not its start: the caret
// must stay on screen as the user types past the right edge.
func TestEditingFieldPaintsScrolledText(t *testing.T) {
	t.Parallel()
	const w, h = 160, 40
	style := capsuleStyle()
	style.Body = ui.Rect{W: w, H: h}
	long := "abcdefghijklmnopqrstuvwxyz0123456789"
	paint := func(editing bool, scroll int) *Canvas {
		c := newTestCanvas(t, w, h)
		root := &ui.Node{Kind: ui.KindRow, Children: []*ui.Node{{
			Kind: ui.KindTextField, Text: long, Cursor: len(long), Padding: 10,
			Bounds: ui.Rect{W: w, H: h}, Editing: editing, ScrollX: scroll,
		}}}
		if err := Paint(c, root, NewTextRenderer(mustTestFace(t)), style); err != nil {
			t.Fatal(err)
		}
		return c
	}
	rest, scrolled := paint(false, 0), paint(true, 200)
	// The caret is the accent column; unscrolled it is off the right edge (not
	// painted in view), scrolled it is inside the text box.
	box := FieldTextRect(&ui.Node{Kind: ui.KindTextField, Padding: 10, Bounds: ui.Rect{W: w, H: h}})
	if countColor(t, scrolled, box, style.accent()) == 0 {
		t.Fatal("scrolled editing field painted no caret inside its box")
	}
	if countColor(t, rest, box, style.accent()) != 0 {
		t.Fatal("unscrolled field painted its end-of-text caret inside the box")
	}
}
```

If `countColor` does not exist in `internal/render` test helpers, add it next to `inked` in the same test file:

```go
func countColor(t *testing.T, c *Canvas, r ui.Rect, want Color) int {
	t.Helper()
	n := 0
	for y := r.Y; y < r.Y+r.H; y++ {
		for x := r.X; x < r.X+r.W; x++ {
			if pixelAt(t, c, x, y) == want {
				n++
			}
		}
	}
	return n
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `GOWORK=off go test ./internal/render -run TestEditingFieldPaintsScrolledText -count=1`
Expected: FAIL — `FieldTextRect` undefined.

- [ ] **Step 3: Implement `FieldTextRect` and the scrolled paint**

In `paint.go`, add:

```go
// FieldTextRect is the logical box a text field's text occupies: inside its
// padding, after a Search field's leading glyph and before its clear glyph.
// Painting and pointer hit testing both read it, so a click lands on the
// glyph the user sees.
func FieldTextRect(n *ui.Node) ui.Rect {
	if n == nil {
		return ui.Rect{}
	}
	mark := 0
	if n.Name == "Search" && !n.Multiline {
		mark = searchGlyphInset + searchGlyphSize + searchGlyphGap - n.Padding
	}
	trail := 0
	if clear := SearchClearBox(n); clear.W > 0 {
		trail = clearGlyphInset + clearGlyphSize + clearGlyphGap - n.Padding
	}
	return ui.Rect{
		X: n.Bounds.X + n.Padding + mark,
		Y: n.Bounds.Y + n.Padding,
		W: max(n.Bounds.W-2*n.Padding-mark-trail, 0),
		H: n.Bounds.H - 2*n.Padding,
	}
}
```

In `paintTextField`, replace the inline `inner := ui.Rect{...}` computation (and the `mark`/`trail` arithmetic that only fed it — keep the `paintSearchGlyph`/`paintClearGlyph` calls) with `inner := FieldTextRect(n)`. Then, in the single-line path after `phys = centreLine(phys, text, style, n)` and the `c.restrict = phys` clip, shift the text origin when editing and paint unclipped-by-ellipsis:

```go
	// An editing field is a window onto its line: the text slides left by
	// ScrollX and is clipped by the box rather than ellipsized, so the caret
	// the user is typing at stays on screen.
	origin := phys
	if n.Editing && !n.Multiline {
		origin.X -= style.Scale120.Physical(n.ScrollX)
		origin.W += style.Scale120.Physical(n.ScrollX) + phys.W
	}
```

Pass `origin` instead of `phys` to the `paintText` call for `shown`, to the preedit box (`pre := origin`), and to the caret rect (`caret := ui.Rect{X: origin.X + prefixW, ...}`). Widening `origin.W` means `text.Truncate` never trims an editing field; the `c.restrict` clip set from `phys` still bounds what reaches the canvas.

For multiline, in `paintMultilineField`, skip the first `n.ScrollY` lines when `n.Editing`: start the loop's `box.Y` at `phys.Y + (i-n.ScrollY)*lineH` and `continue` for `i < n.ScrollY`; compute the caret line's y the same way.

- [ ] **Step 4: Run the render package**

Run: `GOWORK=off go test ./internal/render -count=1`
Expected: PASS, including the existing `TestPaintSearchFieldDrawsALeadingMark` and `TestPaintTextFieldIsAStadiumOnCapsule`.

- [ ] **Step 5: Write the failing shell test**

```go
package shell

import "testing"

// Typing past the right edge of the clipboard search scrolls the painted
// copy so the caret stays in view.
func TestFocusedSearchScrollsToCaretOnPaint(t *testing.T) {
	r := newPanelRegistry(t)
	r.mu.Lock()
	r.clipboard = clipboardTestRegistry(nil).clipboard
	r.mu.Unlock()
	r.BindClipboard(&clipboardRecorder{})
	if err := r.OpenPanel(PanelClipboard, 7, Trigger{OutW: 1920, OutH: 1080}); err != nil {
		t.Fatal(err)
	}
	requests := drainAux(t, r, 2)
	h := r.panelHosts[PanelClipboard]
	if err := requests[1].Open.Callbacks.Configure(720, 560, 120); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 120; i++ {
		h.keyPress(r, 17) // KEY_W
	}
	r.mu.Lock()
	root := copyNode(h.root)
	h.applyEditorView(root)
	field := findEditing(root)
	r.mu.Unlock()
	if field == nil {
		t.Fatal("no editing field on the paint copy")
	}
	if field.ScrollX <= 0 {
		t.Fatalf("ScrollX = %d after 120 characters, want > 0", field.ScrollX)
	}
}

func findEditing(n *ui.Node) *ui.Node {
	if n == nil {
		return nil
	}
	if n.Kind == ui.KindTextField && n.Editing {
		return n
	}
	for _, c := range n.Children {
		if f := findEditing(c); f != nil {
			return f
		}
	}
	return nil
}
```

(Add `"github.com/Nomadcxx/sysc-shell/internal/ui"` to the imports.)

- [ ] **Step 6: Run to verify it fails**

Run: `GOWORK=off go test ./internal/shell -run TestFocusedSearchScrollsToCaretOnPaint -count=1`
Expected: FAIL — `applyEditorView` undefined.

- [ ] **Step 7: Implement `fieldFor` and `applyEditorView`**

Extract the field-selection block of `editField` (everything from `var f *ui.Field` through the final `else { ... h.fields[path] ... }`) into:

```go
// fieldFor is the retained editor behind a text-field node: the Bluetooth
// PIN, the network password, a panel search, a plugin field's retained
// editor, or a settings entry. It is created on first use and synced from
// the node, so every path that edits or paints a field shares one state.
func (h *PanelHost) fieldFor(n *ui.Node) *ui.Field {
	if n == nil || n.Kind != ui.KindTextField {
		return nil
	}
	// ... the moved block, unchanged, ending with `return f`
}
```

`editField` becomes: menu-filter branch unchanged; `n := h.focused()`; `f := h.fieldFor(n)`; `if f == nil { return false }`; `fn(f)`; `f.SyncTo(n)`; then its existing change tail unchanged.

Add:

```go
// applyEditorView marks the focused field on a paint copy and gives it the
// scroll that keeps its caret visible. It runs after layout, so bounds are
// real, and measures with the same text metrics the painter uses.
func (h *PanelHost) applyEditorView(root *ui.Node) {
	focused := h.focused()
	if focused == nil || focused.Kind != ui.KindTextField {
		return
	}
	key := focused.StableKey()
	f := h.fieldFor(focused)
	n := findByStableKey(root, key)
	if f == nil || n == nil {
		return
	}
	n.Editing = true
	n.SelStart, n.SelEnd = f.Selection()
	box := render.FieldTextRect(n)
	measure := h.measureText()
	attrs := ui.TextAttrsOf(n)
	if n.Multiline {
		lines := strings.Count(ui.DisplayPrefix(n, n.Cursor), "\n")
		_, lineH := measure(" ", attrs)
		visible := max(box.H/max(lineH, 1), 1)
		switch {
		case lines < f.ScrollY:
			f.ScrollY = lines
		case lines >= f.ScrollY+visible:
			f.ScrollY = lines - visible + 1
		}
		n.ScrollY = f.ScrollY
		return
	}
	caretX, _ := measure(ui.DisplayPrefix(n, n.Cursor)+ui.DisplayPreedit(n), attrs)
	textW, _ := measure(ui.DisplayText(n)+ui.DisplayPreedit(n), attrs)
	f.ScrollX = ui.KeepCaretVisible(f.ScrollX, caretX, textW, box.W, 8)
	n.ScrollX = f.ScrollX
}

func findByStableKey(n *ui.Node, key string) *ui.Node {
	if n == nil || key == "" {
		return nil
	}
	if n.StableKey() == key {
		return n
	}
	for _, c := range n.Children {
		if m := findByStableKey(c, key); m != nil {
			return m
		}
	}
	return nil
}
```

`f.Selection()` arrives in Task 6. Until then, stub it at the bottom of `internal/ui/textfield.go` so this task compiles:

```go
// Selection returns the selected byte range, start <= end. Task 6 replaces
// this with the anchor-aware version.
func (f *Field) Selection() (start, end int) { return f.Cursor, f.Cursor }
```

In `render`, immediately after `root := copyNode(h.root)` and the page offset restore, add `h.applyEditorView(root)`.

- [ ] **Step 8: Run**

Run: `GOMAXPROCS=4 GOWORK=off go test -p 2 -count=1 ./internal/ui ./internal/render ./internal/shell`
Expected: PASS apart from the four pre-existing `internal/shell` failures.

- [ ] **Step 9: Commit**

```bash
git add internal/render internal/shell internal/ui
git commit -m "feat(shell): a focused text field scrolls to keep its caret visible"
```

---

## Phase 2 — Editing engine

### Task 4: Key vocabulary and the fallback resolver

**Files:**
- Create: `internal/ui/keys.go`, `internal/ui/keys_test.go`

**Interfaces:**
- Produces (exact):

```go
type Mods uint8
const (
	ModShift Mods = 1 << iota
	ModCtrl
	ModAlt
	ModSuper
	ModCapsLock
)
func (m Mods) Has(x Mods) bool
func ModsFromMask(depressed, latched, locked uint32) Mods
type KeyInput struct {
	Code   uint32 // evdev code, for callers that still switch on it
	Sym    uint32 // X11 keysym
	Text   string // what the key types; "" for non-printing keys
	Mods   Mods
	Serial uint32 // the wl_keyboard serial; clipboard ownership needs it
}
func FallbackKey(code uint32, mods Mods) KeyInput
const (
	SymBackSpace  uint32 = 0xff08
	SymTab        uint32 = 0xff09
	SymReturn     uint32 = 0xff0d
	SymEscape     uint32 = 0xff1b
	SymHome       uint32 = 0xff50
	SymLeft       uint32 = 0xff51
	SymUp         uint32 = 0xff52
	SymRight      uint32 = 0xff53
	SymDown       uint32 = 0xff54
	SymPageUp     uint32 = 0xff55
	SymPageDown   uint32 = 0xff56
	SymEnd        uint32 = 0xff57
	SymKPEnter    uint32 = 0xff8d
	SymISOLeftTab uint32 = 0xfe20
	SymDelete     uint32 = 0xffff
)
```

- [ ] **Step 1: Write the failing tests**

```go
package ui

import "testing"

func TestModsFromMaskUsesRealModifierBits(t *testing.T) {
	cases := []struct {
		dep, lat, lock uint32
		want           Mods
	}{
		{0, 0, 0, 0},
		{1 << 0, 0, 0, ModShift},
		{1 << 2, 0, 0, ModCtrl},
		{1 << 3, 0, 0, ModAlt},
		{1 << 6, 0, 0, ModSuper},
		{0, 0, 1 << 1, ModCapsLock},
		{0, 1 << 0, 0, ModShift},       // a latched Shift counts
		{1 << 7, 0, 0, 0},              // Mod5 is AltGr's level, not Alt
		{1<<0 | 1<<2, 0, 1 << 4, ModShift | ModCtrl}, // Mod2 (NumLock) is ignored
	}
	for _, tc := range cases {
		if got := ModsFromMask(tc.dep, tc.lat, tc.lock); got != tc.want {
			t.Errorf("mask %#x/%#x/%#x = %#x, want %#x", tc.dep, tc.lat, tc.lock, got, tc.want)
		}
	}
}

func TestFallbackKey(t *testing.T) {
	cases := []struct {
		name string
		code uint32
		mods Mods
		sym  uint32
		text string
	}{
		{"a", 30, 0, 'a', "a"},
		{"Shift+a", 30, ModShift, 'A', "A"},
		{"Caps+a", 30, ModCapsLock, 'A', "A"},
		{"Caps+Shift+a", 30, ModCapsLock | ModShift, 'a', "a"},
		{"Caps+1 is not a letter", 2, ModCapsLock, '1', "1"},
		{"Shift+1", 2, ModShift, '!', "!"},
		{"space", 57, 0, ' ', " "},
		{"BackSpace", 14, 0, SymBackSpace, ""},
		{"Delete", 111, 0, SymDelete, ""},
		{"Left", 105, 0, SymLeft, ""},
		{"Home", 102, 0, SymHome, ""},
		{"Return", 28, 0, SymReturn, ""},
		{"Shift+Tab", 15, ModShift, SymISOLeftTab, ""},
		{"Ctrl+a keeps sym and text", 30, ModCtrl, 'a', "a"},
	}
	for _, tc := range cases {
		got := FallbackKey(tc.code, tc.mods)
		if got.Sym != tc.sym || got.Text != tc.text || got.Code != tc.code || got.Mods != tc.mods {
			t.Errorf("%s: %+v, want sym %#x text %q", tc.name, got, tc.sym, tc.text)
		}
	}
}
```

- [ ] **Step 2: Run to verify they fail**

Run: `GOWORK=off go test ./internal/ui -run 'ModsFromMask|FallbackKey' -count=1`
Expected: FAIL — undefined names.

- [ ] **Step 3: Implement `internal/ui/keys.go`**

```go
package ui

import (
	"unicode"
	"unicode/utf8"
)

// Mods is the modifier state a key was resolved under. AltGr is not here:
// it selects a keymap level, so its effect is already in Sym and Text.
type Mods uint8

const (
	ModShift Mods = 1 << iota
	ModCtrl
	ModAlt
	ModSuper
	ModCapsLock
)

func (m Mods) Has(x Mods) bool { return m&x != 0 }

// Real modifier bits are fixed by XKB: Shift 0, Lock 1, Control 2, Mod1 3,
// Mod2 4, Mod3 5, Mod4 6, Mod5 7. Every keymap maps Alt to Mod1 and Super to
// Mod4 in practice, and decoding by bit needs no keymap at all.
const (
	realShift = 1 << 0
	realLock  = 1 << 1
	realCtrl  = 1 << 2
	realMod1  = 1 << 3
	realMod4  = 1 << 6
)

// ModsFromMask decodes wl_keyboard.modifiers. Depressed and latched count as
// held; only Lock is read from the locked mask.
func ModsFromMask(depressed, latched, locked uint32) Mods {
	held := depressed | latched
	var m Mods
	if held&realShift != 0 {
		m |= ModShift
	}
	if held&realCtrl != 0 {
		m |= ModCtrl
	}
	if held&realMod1 != 0 {
		m |= ModAlt
	}
	if held&realMod4 != 0 {
		m |= ModSuper
	}
	if locked&realLock != 0 {
		m |= ModCapsLock
	}
	return m
}

// KeyInput is one resolved key: its evdev code, the keysym and text the
// active layout gives it, and the modifiers it was pressed under.
type KeyInput struct {
	Code   uint32
	Sym    uint32
	Text   string
	Mods   Mods
	Serial uint32
}

// X11 keysyms (keysymdef.h) the shell matches on. Printable ASCII keysyms
// equal their code points.
const (
	SymBackSpace  uint32 = 0xff08
	SymTab        uint32 = 0xff09
	SymReturn     uint32 = 0xff0d
	SymEscape     uint32 = 0xff1b
	SymHome       uint32 = 0xff50
	SymLeft       uint32 = 0xff51
	SymUp         uint32 = 0xff52
	SymRight      uint32 = 0xff53
	SymDown       uint32 = 0xff54
	SymPageUp     uint32 = 0xff55
	SymPageDown   uint32 = 0xff56
	SymEnd        uint32 = 0xff57
	SymKPEnter    uint32 = 0xff8d
	SymISOLeftTab uint32 = 0xfe20
	SymDelete     uint32 = 0xffff
)

var fallbackSyms = map[uint32]uint32{
	1: SymEscape, 14: SymBackSpace, 15: SymTab, 28: SymReturn, 96: SymKPEnter,
	102: SymHome, 103: SymUp, 104: SymPageUp, 105: SymLeft, 106: SymRight,
	107: SymEnd, 108: SymDown, 109: SymPageDown, 111: SymDelete,
}

// FallbackKey resolves an evdev code through the US table. It is the whole
// resolver when the compositor sent no usable keymap, and what the shell's
// raw-code test entry point uses.
func FallbackKey(code uint32, mods Mods) KeyInput {
	k := KeyInput{Code: code, Mods: mods}
	if sym, ok := fallbackSyms[code]; ok {
		k.Sym = sym
		if sym == SymTab && mods.Has(ModShift) {
			k.Sym = SymISOLeftTab
		}
		return k
	}
	plain, _ := EvdevText(code, false)
	letter := len(plain) == 1 && plain[0] >= 'a' && plain[0] <= 'z'
	shift := mods.Has(ModShift)
	if letter && mods.Has(ModCapsLock) {
		shift = !shift
	}
	text, ok := EvdevText(code, shift)
	if !ok {
		return k
	}
	k.Text = text
	if r, size := utf8.DecodeRuneInString(text); size == len(text) && r < unicode.MaxASCII {
		k.Sym = uint32(r)
	}
	return k
}
```

- [ ] **Step 4: Run**

Run: `GOWORK=off go test ./internal/ui -count=1`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/ui/keys.go internal/ui/keys_test.go
git commit -m "feat(ui): key vocabulary and the US fallback resolver"
```

### Task 5: Platform key events carry Sym, Text and Mods

**Files:**
- Modify: `internal/platform/wayland/client.go` (`Event` struct; keyboard wiring near `keyboard.SetKeyHandler`; pointer button delivery)
- Modify: `internal/platform/wayland/keyboard.go` (`owner` modifier state, `keyEvent` builder used by `deliverKey` and `fireRepeat`)
- Test: `internal/platform/wayland/keyevent_test.go` (new)

**Interfaces:**
- Consumes: `ui.Mods`, `ui.ModsFromMask`, `ui.FallbackKey` (Task 4).
- Produces:
  - `Event` fields `Sym uint32`, `Text string`, `Mods ui.Mods` on every key press, release and repeat; `Mods` also on pointer press/release.
  - `func (o *owner) setModifiers(depressed, latched, locked, group uint32)`
  - `func (o *owner) keyEvent(kind EventKind, key, serial uint32) Event`
  - owner field `mods ui.Mods` (the resolver seam Task 14 replaces).

- [ ] **Step 1: Write the failing tests**

```go
package wayland

import (
	"testing"
	"time"

	"github.com/Nomadcxx/sysc-shell/internal/ui"
	"github.com/Nomadcxx/sysc-wayland/client"
)

func TestKeyEventsCarryResolvedSymTextAndMods(t *testing.T) {
	rh := newRepeatHarness(t, 25, 600)
	rh.o.setModifiers(1<<0, 0, 0, 0) // Shift held
	rh.o.deliverKey(5, 30, uint32(client.KeyboardKeyStatePressed))
	got := (*rh.seen)[len(*rh.seen)-1]
	if got.Sym != 'A' || got.Text != "A" || !got.Mods.Has(ui.ModShift) {
		t.Fatalf("Shift+a = %+v", got)
	}
}

// A repeat is resolved when it fires, so a modifier change mid-hold shows.
func TestRepeatUsesModifiersHeldAtDelivery(t *testing.T) {
	rh := newRepeatHarness(t, 25, 600)
	rh.o.deliverKey(5, 30, uint32(client.KeyboardKeyStatePressed))
	rh.o.setModifiers(1<<0, 0, 0, 0)
	rh.advance(600 * time.Millisecond)
	got := (*rh.seen)[len(*rh.seen)-1]
	if got.Text != "A" {
		t.Fatalf("repeat after Shift = %q, want A", got.Text)
	}
}
```

- [ ] **Step 2: Run to verify they fail**

Run: `GOWORK=off go test ./internal/platform/wayland -run 'KeyEventsCarry|RepeatUsesModifiers' -count=1`
Expected: FAIL — `setModifiers` undefined, `Event` has no `Sym`.

- [ ] **Step 3: Implement**

Add to `Event` (after `Key uint32`):

```go
	// Sym, Text and Mods are the key resolved through the active layout:
	// the keysym, the UTF-8 it types ("" for non-printing keys and while a
	// compose sequence is open), and the modifiers held. Set on every key
	// press, release and repeat; Mods is also set on pointer buttons.
	Sym  uint32
	Text string
	Mods ui.Mods
```

In `keyboard.go`, add the field `mods ui.Mods` to the owner (next to `repeat keyRepeat` in the `owner` struct in `client.go`) and:

```go
// setModifiers records wl_keyboard.modifiers. group selects the layout and is
// used once a keymap is loaded (Task 14).
func (o *owner) setModifiers(depressed, latched, locked, group uint32) {
	o.mods = ui.ModsFromMask(depressed, latched, locked)
}

// keyEvent resolves a key at delivery time. Real presses and synthesised
// repeats both come through here, so a repeat types what the keys held now
// would type.
func (o *owner) keyEvent(kind EventKind, key, serial uint32) Event {
	k := ui.FallbackKey(key, o.mods)
	return Event{Kind: kind, Key: key, Serial: serial, Sym: k.Sym, Text: k.Text, Mods: k.Mods}
}
```

In `deliverKey`, replace `Event{Kind: kind, Key: key, Serial: serial}` with `o.keyEvent(kind, key, serial)`. In `fireRepeat`, replace its `Event{...}` with `o.keyEvent(EventKeyPress, o.repeat.key, o.repeat.serial)`.

In `client.go`, next to `keyboard.SetRepeatInfoHandler`, add:

```go
		keyboard.SetModifiersHandler(func(e client.KeyboardModifiersEvent) {
			o.setModifiers(e.ModsDepressed, e.ModsLatched, e.ModsLocked, e.Group)
		})
```

Where pointer press/release `Event`s are built (search `EventPointerPress` in `client.go`), add `Mods: o.mods`.

- [ ] **Step 4: Run**

Run: `GOWORK=off go test ./internal/platform/wayland -count=1`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/platform/wayland
git commit -m "feat(wayland): key events carry keysym, text and modifiers"
```

### Task 6: Field selection, motion and deletion

**Files:**
- Create: `internal/ui/textedit.go`, `internal/ui/textedit_test.go`
- Modify: `internal/ui/textfield.go` (`Anchor`, `goalCol` fields; `Insert`/`Backspace` replace the selection; remove the Task 3 `Selection` stub)
- Modify: `go.mod` (promote `github.com/rivo/uniseg` to a direct requirement)

**Interfaces:**
- Produces on `*Field`:

```go
Anchor int // selection anchor; == Cursor means no selection
func (f *Field) Selection() (start, end int)
func (f *Field) HasSelection() bool
func (f *Field) SelectedText() string
func (f *Field) SelectAll()
func (f *Field) SetCaret(pos int, extend bool)
func (f *Field) SelectWordAt(pos int)
func (f *Field) SelectLineAt(pos int)
func (f *Field) MoveGrapheme(dir int, extend bool)
func (f *Field) MoveWord(dir int, extend bool)
func (f *Field) MoveLineEdge(end bool, extend bool)
func (f *Field) MoveTextEdge(end bool, extend bool)
func (f *Field) MoveVertical(dir int, extend bool) bool // multiline only; false when single-line
func (f *Field) DeleteGrapheme(dir int) bool
func (f *Field) DeleteWord(dir int) bool
func (f *Field) DeleteToLineEdge(end bool) bool
func (f *Field) DeleteSelection() bool
```

`dir` is −1 (back/left/up) or +1. Every mutating method returns whether `Text` changed.

- [ ] **Step 1: Write the failing tests**

```go
package ui

import "testing"

func field(text string, cursor, anchor int) *Field {
	return &Field{Text: text, Cursor: cursor, Anchor: anchor}
}

func TestGraphemeMotionNeverSplitsAClusterOrAnEmoji(t *testing.T) {
	s := "éx👩‍💻y" // e + combining acute, x, woman technologist (ZWJ), y
	f := field(s, 0, 0)
	var stops []int
	for i := 0; i < 5; i++ {
		f.MoveGrapheme(+1, false)
		stops = append(stops, f.Cursor)
	}
	want := []int{3, 4, 4 + len("👩‍💻"), len(s), len(s)}
	for i := range want {
		if stops[i] != want[i] {
			t.Fatalf("stops %v, want %v", stops, want)
		}
	}
}

func TestWordMotionAndDeletion(t *testing.T) {
	f := field("alpha beta_2  gamma", 19, 19)
	f.MoveWord(-1, false)
	if f.Cursor != 14 {
		t.Fatalf("word left from end = %d, want 14", f.Cursor)
	}
	f.MoveWord(-1, false)
	if f.Cursor != 6 {
		t.Fatalf("second word left = %d, want 6 (beta_2 is one word)", f.Cursor)
	}
	f.MoveWord(+1, false)
	if f.Cursor != 12 {
		t.Fatalf("word right = %d, want 12", f.Cursor)
	}
	g := field("alpha beta", 10, 10)
	if !g.DeleteWord(-1) || g.Text != "alpha " {
		t.Fatalf("delete word back = %q", g.Text)
	}
}

func TestShiftMotionExtendsAndPlainMotionCollapses(t *testing.T) {
	f := field("hello world", 0, 0)
	f.MoveWord(+1, true)
	if s, e := f.Selection(); s != 0 || e != 5 || f.SelectedText() != "hello" {
		t.Fatalf("selection %d..%d %q", s, e, f.SelectedText())
	}
	f.MoveGrapheme(-1, false)
	if f.HasSelection() || f.Cursor != 0 {
		t.Fatalf("left with a selection should collapse to its start, got cursor %d anchor %d", f.Cursor, f.Anchor)
	}
}

func TestInsertAndBackspaceReplaceTheSelection(t *testing.T) {
	f := field("hello world", 11, 6)
	f.Insert("there")
	if f.Text != "hello there" || f.HasSelection() {
		t.Fatalf("insert over selection = %q", f.Text)
	}
	f.SelectAll()
	f.Backspace()
	if f.Text != "" {
		t.Fatalf("backspace over select-all = %q", f.Text)
	}
}

func TestLineEdgesAndVerticalMotionInMultiline(t *testing.T) {
	f := &Field{Text: "abc\nde\nfghij", Cursor: 2, Anchor: 2, Multiline: true, goalCol: -1}
	if !f.MoveVertical(+1, false) || f.Cursor != 6 {
		t.Fatalf("down from col 2 = %d, want 6 (end of short line)", f.Cursor)
	}
	if !f.MoveVertical(+1, false) || f.Cursor != 9 {
		t.Fatalf("down again keeps goal col 2 = %d, want 9", f.Cursor)
	}
	f.MoveLineEdge(false, false)
	if f.Cursor != 7 {
		t.Fatalf("line start = %d, want 7", f.Cursor)
	}
	if !f.DeleteToLineEdge(true) || f.Text != "abc\nde\n" {
		t.Fatalf("delete to line end = %q", f.Text)
	}
	single := field("abc", 1, 1)
	if single.MoveVertical(+1, false) {
		t.Fatal("single-line field handled Down")
	}
}

func TestSelectWordAndLineAt(t *testing.T) {
	f := &Field{Text: "one two\nthree", Multiline: true}
	f.SelectWordAt(5)
	if f.SelectedText() != "two" {
		t.Fatalf("word at 5 = %q", f.SelectedText())
	}
	f.SelectLineAt(10)
	if f.SelectedText() != "three" {
		t.Fatalf("line at 10 = %q", f.SelectedText())
	}
}
```

- [ ] **Step 2: Run to verify they fail**

Run: `GOWORK=off go test ./internal/ui -run 'Grapheme|WordMotion|ShiftMotion|ReplaceTheSelection|LineEdges|SelectWordAndLine' -count=1`
Expected: FAIL — undefined methods.

- [ ] **Step 3: Promote uniseg**

Run: `GOWORK=off go get github.com/rivo/uniseg@v0.4.7` then confirm `go.mod` lists it in the direct `require` block.

- [ ] **Step 4: Implement `internal/ui/textedit.go`**

```go
package ui

import (
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/rivo/uniseg"
)

// Positions are byte offsets into Text, always on a grapheme-cluster
// boundary, so a caret never lands inside an accent or a joined emoji.

func nextGrapheme(s string, i int) int {
	if i >= len(s) {
		return len(s)
	}
	cluster, _, _, _ := uniseg.FirstGraphemeClusterInString(s[i:], -1)
	return i + len(cluster)
}

func prevGrapheme(s string, i int) int {
	if i <= 0 {
		return 0
	}
	last := 0
	for pos := 0; pos < i; {
		last = pos
		pos = nextGrapheme(s, pos)
	}
	return last
}

func isWordRune(r rune) bool { return r == '_' || unicode.IsLetter(r) || unicode.IsDigit(r) }

func runeAt(s string, i int) rune     { r, _ := utf8.DecodeRuneInString(s[i:]); return r }
func runeBefore(s string, i int) rune { r, _ := utf8.DecodeLastRuneInString(s[:i]); return r }

func nextWord(s string, i int, masked bool) int {
	if masked {
		return len(s)
	}
	for i < len(s) && !isWordRune(runeAt(s, i)) {
		i = nextGrapheme(s, i)
	}
	for i < len(s) && isWordRune(runeAt(s, i)) {
		i = nextGrapheme(s, i)
	}
	return i
}

func prevWord(s string, i int, masked bool) int {
	if masked {
		return 0
	}
	for i > 0 && !isWordRune(runeBefore(s, i)) {
		i = prevGrapheme(s, i)
	}
	for i > 0 && isWordRune(runeBefore(s, i)) {
		i = prevGrapheme(s, i)
	}
	return i
}

func lineStart(s string, i int) int { return strings.LastIndexByte(s[:i], '\n') + 1 }

func lineEnd(s string, i int) int {
	if j := strings.IndexByte(s[i:], '\n'); j >= 0 {
		return i + j
	}
	return len(s)
}

func (f *Field) Selection() (start, end int) {
	f.clamp()
	return min(f.Cursor, f.Anchor), max(f.Cursor, f.Anchor)
}

func (f *Field) HasSelection() bool { s, e := f.Selection(); return s != e }

func (f *Field) SelectedText() string { s, e := f.Selection(); return f.Text[s:e] }

func (f *Field) SelectAll() { f.Anchor, f.Cursor, f.goalCol = 0, len(f.Text), -1 }

// SetCaret moves the caret to pos (snapped back to a grapheme boundary),
// extending the selection when extend is set.
func (f *Field) SetCaret(pos int, extend bool) {
	pos = min(max(pos, 0), len(f.Text))
	if pos < len(f.Text) {
		pos = prevGrapheme(f.Text, nextGrapheme(f.Text, pos))
	}
	f.place(pos, extend)
}

func (f *Field) place(pos int, extend bool) {
	f.Cursor = pos
	if !extend {
		f.Anchor = pos
	}
	f.goalCol = -1
}

func (f *Field) SelectWordAt(pos int) {
	pos = min(max(pos, 0), len(f.Text))
	start, end := pos, pos
	if pos < len(f.Text) && isWordRune(runeAt(f.Text, pos)) || pos > 0 && isWordRune(runeBefore(f.Text, pos)) {
		start, end = prevWord(f.Text, pos, f.Masked), nextWord(f.Text, pos, f.Masked)
		if pos < len(f.Text) && !isWordRune(runeAt(f.Text, pos)) {
			end = pos
		}
	}
	f.Anchor, f.Cursor, f.goalCol = start, end, -1
}

func (f *Field) SelectLineAt(pos int) {
	pos = min(max(pos, 0), len(f.Text))
	f.Anchor, f.Cursor, f.goalCol = lineStart(f.Text, pos), lineEnd(f.Text, pos), -1
}

func (f *Field) MoveGrapheme(dir int, extend bool) {
	f.clamp()
	if !extend && f.HasSelection() {
		s, e := f.Selection()
		if dir < 0 {
			f.place(s, false)
		} else {
			f.place(e, false)
		}
		return
	}
	if dir < 0 {
		f.place(prevGrapheme(f.Text, f.Cursor), extend)
	} else {
		f.place(nextGrapheme(f.Text, f.Cursor), extend)
	}
}

func (f *Field) MoveWord(dir int, extend bool) {
	f.clamp()
	if dir < 0 {
		f.place(prevWord(f.Text, f.Cursor, f.Masked), extend)
	} else {
		f.place(nextWord(f.Text, f.Cursor, f.Masked), extend)
	}
}

func (f *Field) MoveLineEdge(end bool, extend bool) {
	f.clamp()
	if end {
		f.place(lineEnd(f.Text, f.Cursor), extend)
	} else {
		f.place(lineStart(f.Text, f.Cursor), extend)
	}
}

func (f *Field) MoveTextEdge(end bool, extend bool) {
	if end {
		f.place(len(f.Text), extend)
	} else {
		f.place(0, extend)
	}
}

// MoveVertical moves one line in a multiline field, keeping the column the
// motion started from so a short line in between does not lose it.
func (f *Field) MoveVertical(dir int, extend bool) bool {
	if !f.Multiline {
		return false
	}
	f.clamp()
	start := lineStart(f.Text, f.Cursor)
	col := f.goalCol
	if col < 0 {
		col = utf8.RuneCountInString(f.Text[start:f.Cursor])
	}
	var target int
	if dir < 0 {
		if start == 0 {
			return true
		}
		target = lineStart(f.Text, start-1)
	} else {
		end := lineEnd(f.Text, f.Cursor)
		if end == len(f.Text) {
			return true
		}
		target = end + 1
	}
	stop := lineEnd(f.Text, target)
	pos := target
	for n := 0; n < col && pos < stop; n++ {
		pos = nextGrapheme(f.Text, pos)
	}
	f.Cursor = pos
	if !extend {
		f.Anchor = pos
	}
	f.goalCol = col
	return true
}

func (f *Field) DeleteSelection() bool {
	s, e := f.Selection()
	if s == e {
		return false
	}
	f.Text = f.Text[:s] + f.Text[e:]
	f.Cursor, f.Anchor, f.goalCol = s, s, -1
	return true
}

func (f *Field) deleteRange(s, e int) bool {
	if s == e {
		return false
	}
	f.Text = f.Text[:s] + f.Text[e:]
	f.Cursor, f.Anchor, f.goalCol = s, s, -1
	return true
}

func (f *Field) DeleteGrapheme(dir int) bool {
	f.clamp()
	if f.DeleteSelection() {
		return true
	}
	if dir < 0 {
		return f.deleteRange(prevGrapheme(f.Text, f.Cursor), f.Cursor)
	}
	return f.deleteRange(f.Cursor, nextGrapheme(f.Text, f.Cursor))
}

func (f *Field) DeleteWord(dir int) bool {
	f.clamp()
	if f.DeleteSelection() {
		return true
	}
	if dir < 0 {
		return f.deleteRange(prevWord(f.Text, f.Cursor, f.Masked), f.Cursor)
	}
	return f.deleteRange(f.Cursor, nextWord(f.Text, f.Cursor, f.Masked))
}

func (f *Field) DeleteToLineEdge(end bool) bool {
	f.clamp()
	if end {
		return f.deleteRange(f.Cursor, lineEnd(f.Text, f.Cursor))
	}
	return f.deleteRange(lineStart(f.Text, f.Cursor), f.Cursor)
}
```

In `textfield.go`: add `Anchor int` and `goalCol int` to `Field`; `NewField` sets `Anchor: len(s), goalCol: -1`; `clamp` also clamps `Anchor` to `[0, len(Text)]`; `Insert` becomes:

```go
func (f *Field) Insert(s string) bool {
	if f == nil {
		return false
	}
	if s == "\n" && (!f.Multiline || f.SubmitOnEnter) {
		return false
	}
	f.PreeditText = ""
	f.clamp()
	f.DeleteSelection()
	f.Text = f.Text[:f.Cursor] + s + f.Text[f.Cursor:]
	f.Cursor += len(s)
	f.Anchor, f.goalCol = f.Cursor, -1
	return true
}
```

`Backspace` becomes `func (f *Field) Backspace() { if f != nil { f.DeleteGrapheme(-1) } }`. `Move(runes)` (used by `menu.go`) and `Clear`, `DeleteSurrounding` also set `f.Anchor = f.Cursor` at the end. Delete the Task 3 `Selection` stub.

- [ ] **Step 5: Run**

Run: `GOWORK=off go test ./internal/ui -count=1`
Expected: PASS, including the pre-existing `textfield_test.go` and `textfield_mask_test.go`.

- [ ] **Step 6: Commit**

```bash
git add go.mod go.sum internal/ui
git commit -m "feat(ui): grapheme-aware selection, motion and deletion on Field"
```

### Task 7: Undo, redo, and sync that keeps the editor's state

**Files:**
- Create: `internal/ui/textundo.go`, `internal/ui/textundo_test.go`
- Modify: `internal/ui/textfield.go` (`history` field; `SyncFrom`, `SyncTo`, `Node`, `Commit` record undo)

**Interfaces:**
- Consumes: Task 6 operations.
- Produces:

```go
type editKind uint8 // editNone, editInsert, editDeleteBack, editDeleteForward, editOther
func (f *Field) record(kind editKind, typed string) // snapshot before a mutation
func (f *Field) Undo() bool
func (f *Field) Redo() bool
func (f *Field) BreakUndo() // caret moved by other means: the next edit starts a new step
```

`SyncFrom` rule: new text → adopt text and caret, collapse the selection, clear history and scroll; same text → keep the field's caret, anchor and history, adopt only `Preedit`. `SyncTo` and `Node` also write `SelStart/SelEnd`.

- [ ] **Step 1: Write the failing tests**

```go
package ui

import "testing"

func typeString(f *Field, s string) {
	for _, r := range s {
		f.record(editInsert, string(r))
		f.Insert(string(r))
	}
}

func TestTypingCoalescesUntilWhitespaceFollowsAWord(t *testing.T) {
	f := NewField("")
	typeString(f, "hello world")
	if !f.Undo() || f.Text != "hello " {
		t.Fatalf("first undo = %q, want %q", f.Text, "hello ")
	}
	if !f.Undo() || f.Text != "" {
		t.Fatalf("second undo = %q, want empty", f.Text)
	}
	if f.Undo() {
		t.Fatal("undo past the start reported a change")
	}
	if !f.Redo() || f.Text != "hello " {
		t.Fatalf("redo = %q", f.Text)
	}
}

func TestCaretMoveBreaksCoalescing(t *testing.T) {
	f := NewField("")
	typeString(f, "ab")
	f.MoveGrapheme(-1, false)
	f.BreakUndo()
	typeString(f, "X")
	if !f.Undo() || f.Text != "ab" {
		t.Fatalf("undo after a caret move = %q, want ab", f.Text)
	}
}

func TestNewEditClearsRedo(t *testing.T) {
	f := NewField("")
	typeString(f, "ab")
	f.Undo()
	typeString(f, "c")
	if f.Redo() {
		t.Fatal("redo survived a new edit")
	}
}

func TestHistoryIsCapped(t *testing.T) {
	f := NewField("")
	for i := 0; i < 250; i++ {
		f.record(editOther, "")
		f.Insert("x ")
	}
	steps := 0
	for f.Undo() {
		steps++
	}
	if steps != undoLimit {
		t.Fatalf("undo steps = %d, want %d", steps, undoLimit)
	}
}

// Review focus 1: a reseed with new text collapses selection and history.
func TestSyncFromNewTextClearsSelectionAndHistory(t *testing.T) {
	f := NewField("")
	typeString(f, "abc")
	f.SelectAll()
	f.SyncFrom(&Node{Kind: KindTextField, Text: "server value", Cursor: 3})
	if f.HasSelection() || f.Cursor != 3 || f.Undo() {
		t.Fatalf("after reseed: cursor %d anchor %d", f.Cursor, f.Anchor)
	}
}

// Review focus 2: a builder that writes the caret at the end must not move a
// caret the user placed.
func TestSyncFromSameTextKeepsFieldCaret(t *testing.T) {
	f := NewField("abcdef")
	f.SetCaret(2, false)
	f.SyncFrom(&Node{Kind: KindTextField, Text: "abcdef", Cursor: 6})
	if f.Cursor != 2 {
		t.Fatalf("cursor = %d, want 2", f.Cursor)
	}
}

func TestSyncToWritesSelection(t *testing.T) {
	f := field("hello", 4, 1)
	n := &Node{Kind: KindTextField}
	f.SyncTo(n)
	if n.SelStart != 1 || n.SelEnd != 4 || n.Cursor != 4 {
		t.Fatalf("node %+v", n)
	}
}
```

- [ ] **Step 2: Run to verify they fail**

Run: `GOWORK=off go test ./internal/ui -run 'Coalesc|BreaksCoalescing|ClearsRedo|HistoryIsCapped|SyncFrom|SyncToWrites' -count=1`
Expected: FAIL — undefined `record`, `Undo`, `undoLimit`.

- [ ] **Step 3: Implement `internal/ui/textundo.go`**

```go
package ui

import "unicode"

const undoLimit = 100

type editKind uint8

const (
	editNone editKind = iota
	editInsert
	editDeleteBack
	editDeleteForward
	editOther
)

type fieldSnap struct {
	text           string
	cursor, anchor int
}

type undoHistory struct {
	undo, redo []fieldSnap
	last       editKind
	lastSpace  bool // the previous coalesced insert was whitespace
}

// record snapshots the field before a mutation of the given kind, unless the
// mutation continues the current coalesced step: typed graphemes coalesce
// until a whitespace grapheme follows a non-whitespace one; same-direction
// deletes coalesce; anything else is its own step.
func (f *Field) record(kind editKind, typed string) {
	h := &f.history
	space := typed != "" && isSpace(typed)
	coalesce := kind != editOther && kind == h.last &&
		!(kind == editInsert && space && !h.lastSpace)
	h.last, h.lastSpace = kind, space
	if kind == editOther {
		h.last = editNone
	}
	h.redo = nil
	if coalesce {
		return
	}
	h.undo = append(h.undo, fieldSnap{f.Text, f.Cursor, f.Anchor})
	if len(h.undo) > undoLimit {
		h.undo = h.undo[len(h.undo)-undoLimit:]
	}
}

func isSpace(s string) bool {
	for _, r := range s {
		if !unicode.IsSpace(r) {
			return false
		}
	}
	return true
}

// BreakUndo ends the current coalesced step without recording anything.
func (f *Field) BreakUndo() { f.history.last = editNone }

func (f *Field) Undo() bool {
	h := &f.history
	if len(h.undo) == 0 {
		return false
	}
	snap := h.undo[len(h.undo)-1]
	h.undo = h.undo[:len(h.undo)-1]
	h.redo = append(h.redo, fieldSnap{f.Text, f.Cursor, f.Anchor})
	f.restore(snap)
	return true
}

func (f *Field) Redo() bool {
	h := &f.history
	if len(h.redo) == 0 {
		return false
	}
	snap := h.redo[len(h.redo)-1]
	h.redo = h.redo[:len(h.redo)-1]
	h.undo = append(h.undo, fieldSnap{f.Text, f.Cursor, f.Anchor})
	f.restore(snap)
	return true
}

func (f *Field) restore(s fieldSnap) {
	f.Text, f.Cursor, f.Anchor, f.PreeditText, f.goalCol = s.text, s.cursor, s.anchor, "", -1
	f.history.last = editNone
}
```

In `textfield.go`: add `history undoHistory` to `Field`. Replace `SyncFrom`, `SyncTo`, and extend `Node`:

```go
// SyncFrom adopts a node's value. New text is a reseed — from a plugin or a
// programmatic set — so the caret follows the node and selection, history
// and scroll reset. The same text means the node was rebuilt from this
// field, and the field's own caret, selection and history stand.
func (f *Field) SyncFrom(n *Node) {
	if f == nil || n == nil {
		return
	}
	f.PreeditText = n.Preedit
	if n.Text == f.Text {
		return
	}
	f.Text, f.Cursor = n.Text, n.Cursor
	f.clamp()
	f.Anchor, f.goalCol, f.ScrollX, f.ScrollY = f.Cursor, -1, 0, 0
	f.history = undoHistory{}
}

func (f *Field) SyncTo(n *Node) {
	if f == nil || n == nil {
		return
	}
	n.Text, n.Preedit, n.Cursor = f.Text, f.PreeditText, f.Cursor
	n.SelStart, n.SelEnd = f.Selection()
}
```

In `Node(name)`, add `SelStart`/`SelEnd` from `f.Selection()` to the returned node. `Commit(s)` becomes `f.record(editOther, ""); f.Insert(s)`.

- [ ] **Step 4: Run**

Run: `GOWORK=off go test ./internal/ui -count=1`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/ui
git commit -m "feat(ui): coalesced undo and a sync rule that keeps the editor's caret"
```

### Task 8: `Field.HandleKey` — the key table

**Files:**
- Create: `internal/ui/textkey.go`, `internal/ui/textkey_test.go`

**Interfaces:**
- Consumes: Tasks 4, 6, 7.
- Produces:

```go
type FieldResult struct {
	Handled, Changed bool
	Copy   string // put this on the system clipboard
	Paste  bool   // ask for the clipboard's text (arrives as an insert later)
	Submit bool
}
func (f *Field) HandleKey(k KeyInput) FieldResult
```

- [ ] **Step 1: Write the failing table test**

```go
package ui

import "testing"

func key(sym uint32, text string, mods Mods) KeyInput {
	return KeyInput{Sym: sym, Text: text, Mods: mods}
}

func TestHandleKeyTable(t *testing.T) {
	type want struct {
		text           string
		cursor, anchor int
		res            FieldResult
	}
	cases := []struct {
		name      string
		f         *Field
		k         KeyInput
		want      want
	}{
		{"type", field("ac", 1, 1), key('b', "b", 0), want{"abc", 2, 2, FieldResult{Handled: true, Changed: true}}},
		{"type over selection", field("hello", 5, 0), key('x', "x", 0), want{"x", 1, 1, FieldResult{Handled: true, Changed: true}}},
		{"ctrl+letter never types", field("ab", 2, 2), key('q', "q", ModCtrl), want{"ab", 2, 2, FieldResult{}}},
		{"alt+letter never types", field("ab", 2, 2), key('q', "q", ModAlt), want{"ab", 2, 2, FieldResult{}}},
		{"left", field("abc", 2, 2), key(SymLeft, "", 0), want{"abc", 1, 1, FieldResult{Handled: true}}},
		{"shift+left extends", field("abc", 2, 2), key(SymLeft, "", ModShift), want{"abc", 1, 2, FieldResult{Handled: true}}},
		{"ctrl+left word", field("ab cd", 5, 5), key(SymLeft, "", ModCtrl), want{"ab cd", 3, 3, FieldResult{Handled: true}}},
		{"home", field("abc", 2, 2), key(SymHome, "", 0), want{"abc", 0, 0, FieldResult{Handled: true}}},
		{"end", field("abc", 0, 0), key(SymEnd, "", 0), want{"abc", 3, 3, FieldResult{Handled: true}}},
		{"ctrl+e is end", field("abc", 0, 0), key('e', "e", ModCtrl), want{"abc", 3, 3, FieldResult{Handled: true}}},
		{"backspace", field("abc", 3, 3), key(SymBackSpace, "", 0), want{"ab", 2, 2, FieldResult{Handled: true, Changed: true}}},
		{"delete", field("abc", 0, 0), key(SymDelete, "", 0), want{"bc", 0, 0, FieldResult{Handled: true, Changed: true}}},
		{"ctrl+backspace word", field("ab cd", 5, 5), key(SymBackSpace, "", ModCtrl), want{"ab ", 3, 3, FieldResult{Handled: true, Changed: true}}},
		{"ctrl+w word", field("ab cd", 5, 5), key('w', "w", ModCtrl), want{"ab ", 3, 3, FieldResult{Handled: true, Changed: true}}},
		{"ctrl+delete word", field("ab cd", 0, 0), key(SymDelete, "", ModCtrl), want{" cd", 0, 0, FieldResult{Handled: true, Changed: true}}},
		{"ctrl+u to start", field("ab cd", 3, 3), key('u', "u", ModCtrl), want{"cd", 0, 0, FieldResult{Handled: true, Changed: true}}},
		{"ctrl+k to end", field("ab cd", 2, 2), key('k', "k", ModCtrl), want{"ab", 2, 2, FieldResult{Handled: true, Changed: true}}},
		{"ctrl+a select all", field("abc", 1, 1), key('a', "a", ModCtrl), want{"abc", 3, 0, FieldResult{Handled: true}}},
		{"ctrl+c copies", field("hello", 4, 1), key('c', "c", ModCtrl), want{"hello", 4, 1, FieldResult{Handled: true, Copy: "ell"}}},
		{"ctrl+c with nothing selected", field("hello", 4, 4), key('c', "c", ModCtrl), want{"hello", 4, 4, FieldResult{Handled: true}}},
		{"ctrl+x cuts", field("hello", 4, 1), key('x', "x", ModCtrl), want{"ho", 1, 1, FieldResult{Handled: true, Changed: true, Copy: "ell"}}},
		{"ctrl+v asks for paste", field("ab", 1, 1), key('v', "v", ModCtrl), want{"ab", 1, 1, FieldResult{Handled: true, Paste: true}}},
		{"shifted ctrl+A still selects all", field("abc", 1, 1), key('A', "A", ModCtrl|ModShift), want{"abc", 3, 0, FieldResult{Handled: true}}},
		{"enter submits single line", field("ab", 2, 2), key(SymReturn, "", 0), want{"ab", 2, 2, FieldResult{Handled: true, Submit: true}}},
		{"up not handled single line", field("ab", 2, 2), key(SymUp, "", 0), want{"ab", 2, 2, FieldResult{}}},
		{"tab not handled", field("ab", 2, 2), key(SymTab, "", 0), want{"ab", 2, 2, FieldResult{}}},
		{"escape not handled", field("ab", 2, 2), key(SymEscape, "", 0), want{"ab", 2, 2, FieldResult{}}},
		{"masked refuses copy", &Field{Text: "secret", Cursor: 6, Anchor: 0, Masked: true}, key('c', "c", ModCtrl), want{"secret", 6, 0, FieldResult{Handled: true}}},
		{"masked refuses cut", &Field{Text: "secret", Cursor: 6, Anchor: 0, Masked: true}, key('x', "x", ModCtrl), want{"secret", 6, 0, FieldResult{Handled: true}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tc.f.goalCol = -1
			got := tc.f.HandleKey(tc.k)
			if got != tc.want.res || tc.f.Text != tc.want.text || tc.f.Cursor != tc.want.cursor || tc.f.Anchor != tc.want.anchor {
				t.Fatalf("got %+v text %q cursor %d anchor %d; want %+v", got, tc.f.Text, tc.f.Cursor, tc.f.Anchor, tc.want)
			}
		})
	}
}

func TestHandleKeyMultilineEnterAndVertical(t *testing.T) {
	f := &Field{Text: "ab\ncd", Cursor: 1, Anchor: 1, Multiline: true, goalCol: -1}
	if r := f.HandleKey(key(SymDown, "", 0)); !r.Handled || f.Cursor != 4 {
		t.Fatalf("down = %+v cursor %d", r, f.Cursor)
	}
	if r := f.HandleKey(key(SymReturn, "", 0)); !r.Changed || f.Text != "ab\nc\nd" {
		t.Fatalf("enter in multiline = %+v %q", r, f.Text)
	}
	s := &Field{Text: "ab", Cursor: 2, Anchor: 2, Multiline: true, SubmitOnEnter: true, goalCol: -1}
	if r := s.HandleKey(key(SymReturn, "", 0)); !r.Submit {
		t.Fatalf("submit-on-enter multiline = %+v", r)
	}
}

func TestHandleKeyUndoRedo(t *testing.T) {
	f := NewField("")
	for _, r := range "ab" {
		f.HandleKey(key(uint32(r), string(r), 0))
	}
	if r := f.HandleKey(key('z', "z", ModCtrl)); !r.Changed || f.Text != "" {
		t.Fatalf("undo = %+v %q", r, f.Text)
	}
	if r := f.HandleKey(key('Z', "Z", ModCtrl|ModShift)); !r.Changed || f.Text != "ab" {
		t.Fatalf("redo = %+v %q", r, f.Text)
	}
	f.HandleKey(key('z', "z", ModCtrl))
	if r := f.HandleKey(key('y', "y", ModCtrl)); !r.Changed || f.Text != "ab" {
		t.Fatalf("ctrl+y redo = %+v %q", r, f.Text)
	}
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `GOWORK=off go test ./internal/ui -run HandleKey -count=1`
Expected: FAIL — `HandleKey` undefined.

- [ ] **Step 3: Implement `internal/ui/textkey.go`**

```go
package ui

// FieldResult reports what one key did to a field. Copy and Paste are
// requests for the host to carry to the system clipboard.
type FieldResult struct {
	Handled, Changed bool
	Copy             string
	Paste            bool
	Submit           bool
}

// lowerSym folds an ASCII capital keysym so Ctrl+Shift+z matches 'z'.
func lowerSym(sym uint32) uint32 {
	if sym >= 'A' && sym <= 'Z' {
		return sym + ('a' - 'A')
	}
	return sym
}

// HandleKey applies one resolved key: GUI-first bindings plus the readline
// keys that do not conflict with them. Keys it does not handle (Tab, Esc,
// Up and Down in a single-line field, unbound shortcuts) are left to the
// host's navigation. Pure: no clocks, no I/O.
func (f *Field) HandleKey(k KeyInput) FieldResult {
	f.clamp()
	shift, ctrl := k.Mods.Has(ModShift), k.Mods.Has(ModCtrl)
	moved := func() FieldResult { f.BreakUndo(); return FieldResult{Handled: true} }
	edit := func(kind editKind, typed string, apply func() bool) FieldResult {
		before := fieldSnap{f.Text, f.Cursor, f.Anchor}
		f.record(kind, typed)
		if !apply() {
			// Nothing changed: drop the snapshot record just pushed, if any.
			if n := len(f.history.undo); n > 0 && f.history.undo[n-1] == before {
				f.history.undo = f.history.undo[:n-1]
			}
			return FieldResult{Handled: true}
		}
		return FieldResult{Handled: true, Changed: true}
	}

	if ctrl && !k.Mods.Has(ModAlt) {
		switch lowerSym(k.Sym) {
		case 'a':
			f.SelectAll()
			return moved()
		case 'e':
			f.MoveLineEdge(true, shift)
			return moved()
		case 'c':
			if f.Masked {
				return FieldResult{Handled: true}
			}
			return FieldResult{Handled: true, Copy: f.SelectedText()}
		case 'x':
			if f.Masked || !f.HasSelection() {
				return FieldResult{Handled: true}
			}
			cut := f.SelectedText()
			r := edit(editOther, "", f.DeleteSelection)
			r.Copy = cut
			return r
		case 'v':
			return FieldResult{Handled: true, Paste: true}
		case 'z':
			if shift {
				return FieldResult{Handled: true, Changed: f.Redo()}
			}
			return FieldResult{Handled: true, Changed: f.Undo()}
		case 'y':
			return FieldResult{Handled: true, Changed: f.Redo()}
		case 'w':
			return edit(editOther, "", func() bool { return f.DeleteWord(-1) })
		case 'u':
			return edit(editOther, "", func() bool { return f.DeleteToLineEdge(false) })
		case 'k':
			return edit(editOther, "", func() bool { return f.DeleteToLineEdge(true) })
		}
	}

	switch k.Sym {
	case SymLeft, SymRight:
		dir := 1
		if k.Sym == SymLeft {
			dir = -1
		}
		if ctrl {
			f.MoveWord(dir, shift)
		} else {
			f.MoveGrapheme(dir, shift)
		}
		return moved()
	case SymHome, SymEnd:
		if ctrl {
			f.MoveTextEdge(k.Sym == SymEnd, shift)
		} else {
			f.MoveLineEdge(k.Sym == SymEnd, shift)
		}
		return moved()
	case SymUp, SymDown:
		dir := 1
		if k.Sym == SymUp {
			dir = -1
		}
		if !f.MoveVertical(dir, shift) {
			return FieldResult{}
		}
		return moved()
	case SymBackSpace:
		if ctrl {
			return edit(editOther, "", func() bool { return f.DeleteWord(-1) })
		}
		return edit(editDeleteBack, "", func() bool { return f.DeleteGrapheme(-1) })
	case SymDelete:
		if ctrl {
			return edit(editOther, "", func() bool { return f.DeleteWord(+1) })
		}
		return edit(editDeleteForward, "", func() bool { return f.DeleteGrapheme(+1) })
	case SymReturn, SymKPEnter:
		if f.Multiline && !f.SubmitOnEnter {
			return edit(editOther, "", func() bool { return f.Insert("\n") })
		}
		return FieldResult{Handled: true, Submit: true}
	}

	if k.Text != "" && !ctrl && !k.Mods.Has(ModAlt) && !k.Mods.Has(ModSuper) {
		return edit(editInsert, k.Text, func() bool { return f.Insert(k.Text) })
	}
	return FieldResult{}
}
```

- [ ] **Step 4: Run**

Run: `GOWORK=off go test ./internal/ui -count=1`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/ui/textkey.go internal/ui/textkey_test.go
git commit -m "feat(ui): HandleKey, GUI-first field keys with readline extras"
```

### Task 9: The panel host routes keys through `HandleKey`

**Files:**
- Modify: `internal/shell/panelhost.go` (`handle`, `keyPress`, `editField`, fields `shift/ctrl/alt` → `mods ui.Mods`, `panelShortcutKey`)
- Modify: `internal/shell/bareditor.go:390` (`h.alt` → `h.mods.Has(ui.ModAlt)`)
- Modify: `internal/shell/menu.go` (filter keys through `HandleKey`)
- Test: `internal/shell/textedit_test.go` (new)

**Interfaces:**
- Consumes: `ui.KeyInput`, `ui.FallbackKey`, `Field.HandleKey`, `FieldResult` (Tasks 4, 8); `wayland.Event.Sym/Text/Mods` (Task 5); `fieldFor` (Task 3).
- Produces:
  - `func (h *PanelHost) keyEvent(r *Registry, e wayland.Event) bool` — the live key path.
  - `func (h *PanelHost) keyPress(r *Registry, key uint32) bool` — kept; resolves `ui.FallbackKey(key, h.mods)` and calls `h.keyInput`.
  - `func (h *PanelHost) keyInput(r *Registry, k ui.KeyInput) bool` — shared body.
  - `func (h *PanelHost) fieldKey(r *Registry, k ui.KeyInput) (FieldResult, bool)` — applies a key to the focused field; the bool is "a field was focused".
  - `func (h *PanelHost) fieldChanged(r *Registry, n *ui.Node, f *ui.Field) bool` — `editField`'s existing change tail, extracted.
  - `panelShortcutKey(sym uint32) string`.
  - `h.selection` hooks used by Task 12: `copyRequest func(text string, serial uint32)`, `pasteRequest func(serial uint32)` (nil until Task 12 sets them).

- [ ] **Step 1: Write the failing tests**

```go
package shell

import (
	"testing"

	"github.com/Nomadcxx/sysc-shell/internal/ui"
)

func openClipboardSearch(t *testing.T) (*Registry, *PanelHost) {
	t.Helper()
	r := newPanelRegistry(t)
	r.mu.Lock()
	r.clipboard = clipboardTestRegistry(nil).clipboard
	r.mu.Unlock()
	r.BindClipboard(&clipboardRecorder{})
	if err := r.OpenPanel(PanelClipboard, 7, Trigger{OutW: 1920, OutH: 1080}); err != nil {
		t.Fatal(err)
	}
	requests := drainAux(t, r, 2)
	h := r.panelHosts[PanelClipboard]
	if err := requests[1].Open.Callbacks.Configure(720, 560, 120); err != nil {
		t.Fatal(err)
	}
	return r, h
}

func typeKeys(r *Registry, h *PanelHost, ks ...ui.KeyInput) {
	for _, k := range ks {
		h.keyInput(r, k)
	}
}

func txt(s string) ui.KeyInput { return ui.KeyInput{Sym: uint32(s[0]), Text: s} }

func TestArrowsMoveTheCaretInsteadOfLeavingTheField(t *testing.T) {
	r, h := openClipboardSearch(t)
	typeKeys(r, h, txt("a"), txt("c"), ui.KeyInput{Sym: ui.SymLeft}, txt("b"))
	if h.query != "abc" {
		t.Fatalf("query = %q, want abc", h.query)
	}
	if got := h.focused(); got == nil || got.Name != "Search" {
		t.Fatalf("focus left the field: %+v", got)
	}
}

func TestCtrlShortcutsEditTheFocusedField(t *testing.T) {
	r, h := openClipboardSearch(t)
	typeKeys(r, h, txt("o"), txt("n"), txt("e"), txt(" "), txt("t"), txt("w"), txt("o"),
		ui.KeyInput{Sym: 'w', Text: "w", Mods: ui.ModCtrl})
	if h.query != "one " {
		t.Fatalf("after Ctrl+W query = %q", h.query)
	}
	typeKeys(r, h, ui.KeyInput{Sym: 'z', Text: "z", Mods: ui.ModCtrl})
	if h.query != "one two" {
		t.Fatalf("after Ctrl+Z query = %q", h.query)
	}
}

func TestTabStillLeavesTheField(t *testing.T) {
	r, h := openClipboardSearch(t)
	typeKeys(r, h, ui.KeyInput{Sym: ui.SymTab, Code: keyTab})
	if got := h.focused(); got != nil && got.Name == "Search" {
		t.Fatal("Tab stayed in the search field")
	}
}

func TestCaretOnlyMoveKeepsTheQuery(t *testing.T) {
	r, h := openClipboardSearch(t)
	typeKeys(r, h, txt("a"), txt("b"))
	if !h.keyInput(r, ui.KeyInput{Sym: ui.SymLeft}) || h.query != "ab" {
		t.Fatalf("Left: query %q", h.query)
	}
	if f := h.fieldFor(h.focused()); f.Cursor != 1 {
		t.Fatalf("caret %d, want 1", f.Cursor)
	}
}
```

If `keyTab` is not already a constant in `internal/shell`, use its value `15` (KEY_TAB) as the test does elsewhere (search `keyTab` in `internal/shell/*.go`).

- [ ] **Step 2: Run to verify they fail**

Run: `GOWORK=off go test ./internal/shell -run 'ArrowsMoveTheCaret|CtrlShortcutsEdit|TabStillLeaves|CaretOnlyMoveKeeps' -count=1`
Expected: FAIL — `keyInput` undefined.

- [ ] **Step 3: Implement**

1. Replace the `shift`, `ctrl`, `alt` bool fields on `PanelHost` with `mods ui.Mods`. In `handle`: `case wayland.EventKeyPress: h.mods = e.Mods; return h.keyEvent(r, e)`; `case wayland.EventKeyRelease: h.mods = e.Mods; return false`. Delete the press-side `case keyLeftShift ...: h.shift = true` arms in `keyPress` and the release switch.

2. Add:

```go
// keyEvent is the live key path: the platform has already resolved the key
// through the active layout.
func (h *PanelHost) keyEvent(r *Registry, e wayland.Event) bool {
	return h.keyInput(r, ui.KeyInput{Code: e.Key, Sym: e.Sym, Text: e.Text, Mods: e.Mods, Serial: e.Serial})
}

// keyPress resolves a raw evdev code through the US fallback. Tests and any
// caller holding only a code use it.
func (h *PanelHost) keyPress(r *Registry, key uint32) bool {
	return h.keyInput(r, ui.FallbackKey(key, h.mods))
}
```

3. Rename the current body of `keyPress(r, key)` to `keyInput(r *Registry, k ui.KeyInput) bool`, with `key := k.Code` at its top so the unchanged panel-specific branches (`wallpaperKeyPress(r, key)`, `launcherKeyPress`, `clipboardKeyPress`, `barKeyPress`, the final `switch key`) keep working. Replace the two text blocks — the menu-filter block and the `if key == keyBackspace { ... }` / `if ch, ok := ui.EvdevText(...)` block — with a single field step placed where the Backspace block was:

```go
	if res, focused := h.fieldKey(r, k); focused && res.Handled {
		return true
	}
```

For the open-menu branch, route the filter through the same engine: replace its Backspace/`EvdevText` lines with

```go
			if h.menu.Filtering() {
				handled := false
				if h.menu.Edit(func(f *ui.Field) { handled = f.HandleKey(k).Handled }) && handled {
					r.rebuildPanel(h)
					return true
				}
			}
```

Replace the shortcut modifier collection with `h.mods.Has(ui.ModAlt)` etc., and call `panelShortcutKey(k.Sym)`.

4. Add `fieldKey` and extract `fieldChanged`:

```go
// fieldKey applies one key to the focused text field. Text changes run the
// field's change path (query, plugin change event, setting); caret and
// selection moves only repaint.
func (h *PanelHost) fieldKey(r *Registry, k ui.KeyInput) (ui.FieldResult, bool) {
	n := h.focused()
	f := h.fieldFor(n)
	if f == nil {
		return ui.FieldResult{}, false
	}
	res := f.HandleKey(k)
	if !res.Handled {
		return res, true
	}
	f.SyncTo(n)
	if res.Copy != "" && h.copyRequest != nil {
		h.copyRequest(res.Copy, k.Serial)
	}
	if res.Paste && h.pasteRequest != nil {
		h.pasteRequest(k.Serial)
	}
	if res.Submit {
		h.activate(r)
		return res, true
	}
	if res.Changed {
		h.fieldChanged(r, n, f)
	}
	return res, true
}
```

`keyEvent` sets `KeyInput.Serial` from `e.Serial`; `FallbackKey` leaves it zero.

Move everything after `f.SyncTo(n)` in `editField` into `func (h *PanelHost) fieldChanged(r *Registry, n *ui.Node, f *ui.Field) bool`, and have `editField` call it.

Before this change, Enter on a focused field reached `h.activate(r)` through the final `switch key` (`case keySpace, keyEnter`). Keep that behaviour exact: `res.Submit` calls `h.activate(r)`. Space is now typed text in a focused field and still activates other controls, because `fieldKey` reports `focused == false` for non-fields.

5. `panelShortcutKey`:

```go
func panelShortcutKey(sym uint32) string {
	if sym == ui.SymHome {
		return "home"
	}
	sym = uint32(unicode.ToLower(rune(sym)))
	if sym >= 'a' && sym <= 'z' || sym >= '0' && sym <= '9' {
		return string(rune(sym))
	}
	return ""
}
```

6. `bareditor.go:390`: `if !h.mods.Has(ui.ModAlt) || ...`.

7. Remove `ui.EvdevText` calls from `internal/shell` (`grep -rn EvdevText internal/shell` must print nothing).

- [ ] **Step 4: Run**

Run: `GOMAXPROCS=4 GOWORK=off go test -p 2 -count=1 ./internal/ui ./internal/shell`
Expected: PASS apart from the four pre-existing failures. Fix any existing test that pressed Left/Right on a focused field expecting roving focus to move: that was the defect.

- [ ] **Step 5: Commit**

```bash
git add internal/ui internal/shell
git commit -m "feat(shell): focused fields handle caret, word and shortcut keys"
```

### Task 10: Selection paint and pointer caret placement

**Files:**
- Modify: `internal/render/paint.go` (`paintTextField` selection fill; `FieldOffsetAt`)
- Modify: `internal/shell/panelhost.go` (`EventPointerPress`/`Motion`/`Release` for fields; press counter)
- Test: `internal/render/paint_textfield_select_test.go` (new), `internal/shell/textpointer_test.go` (new)

**Interfaces:**
- Consumes: `FieldTextRect`, `Node.SelStart/SelEnd/Editing/ScrollX` (Tasks 2–3), `Field.SetCaret/SelectWordAt/SelectLineAt` (Task 6), `Event.Mods` (Task 5).
- Produces:
  - `func FieldOffsetAt(n *ui.Node, x int, measure ui.MeasureText) int` in `internal/render` — byte offset of the grapheme boundary nearest logical `x` on a single-line field, accounting for `FieldTextRect` and `ScrollX`.
  - `PanelHost` fields `fieldDrag string` (stable key of the field being drag-selected), `clickAt time.Time`, `clickX, clickY int`, `clickKey string`, `clicks int`.
  - `const multiClickInterval = 400 * time.Millisecond`, `const multiClickSlop = 4`.

- [ ] **Step 1: Write the failing render test**

```go
package render

import (
	"testing"

	"github.com/Nomadcxx/sysc-shell/internal/ui"
)

func TestEditingFieldFillsItsSelectionBehindTheText(t *testing.T) {
	t.Parallel()
	const w, h = 200, 40
	style := capsuleStyle()
	style.Body = ui.Rect{W: w, H: h}
	paint := func(sel bool) *Canvas {
		c := newTestCanvas(t, w, h)
		n := &ui.Node{Kind: ui.KindTextField, Text: "hello world", Cursor: 11, Padding: 10,
			Bounds: ui.Rect{W: w, H: h}, Editing: true}
		if sel {
			n.SelStart, n.SelEnd = 0, 11
		}
		if err := Paint(c, &ui.Node{Kind: ui.KindRow, Children: []*ui.Node{n}}, NewTextRenderer(mustTestFace(t)), style); err != nil {
			t.Fatal(err)
		}
		return c
	}
	box := FieldTextRect(&ui.Node{Kind: ui.KindTextField, Padding: 10, Bounds: ui.Rect{W: w, H: h}})
	if countColor(t, paint(false), box, style.Capsule) <= countColor(t, paint(true), box, style.Capsule) {
		t.Fatal("selection did not cover the well behind the text")
	}
}

func TestFieldOffsetAtSnapsToTheNearestBoundary(t *testing.T) {
	measure := func(s string, _ ui.TextAttrs) (int, int) { return len(s) * 10, 16 }
	n := &ui.Node{Kind: ui.KindTextField, Text: "abcdef", Padding: 10, Bounds: ui.Rect{W: 200, H: 40}}
	box := FieldTextRect(n)
	cases := []struct{ x, want int }{
		{box.X - 5, 0}, {box.X + 14, 1}, {box.X + 16, 2}, {box.X + 500, 6},
	}
	for _, tc := range cases {
		if got := FieldOffsetAt(n, tc.x, measure); got != tc.want {
			t.Errorf("x=%d: %d, want %d", tc.x, got, tc.want)
		}
	}
	n.Editing, n.ScrollX = true, 20
	if got := FieldOffsetAt(n, box.X+1, measure); got != 2 {
		t.Errorf("scrolled by 20, x at the box start = %d, want 2", got)
	}
}
```

- [ ] **Step 2: Run to verify they fail**

Run: `GOWORK=off go test ./internal/render -run 'FillsItsSelection|FieldOffsetAt' -count=1`
Expected: FAIL.

- [ ] **Step 3: Implement the render half**

In `paintTextField`, single-line path, after computing `origin` (Task 3) and before painting `shown`:

```go
	if n.Editing && n.SelStart < n.SelEnd && text != nil {
		spec := textSpec(style, n)
		startW, _, _ := text.Measure(ui.DisplayPrefix(n, n.SelStart), spec, n.Tabular)
		endW, _, _ := text.Measure(ui.DisplayPrefix(n, n.SelEnd), spec, n.Tabular)
		sel := style.accent()
		sel.A = uint8(uint32(sel.A) * 40 / 100)
		fillRect(c, ui.Rect{X: origin.X + startW, Y: phys.Y, W: endW - startW, H: phys.H}, sel)
	}
```

For multiline, fill per line the same way inside `paintMultilineField`'s loop, clamping each line's span to its `[off, end]`.

Add:

```go
// FieldOffsetAt is the byte offset of the grapheme boundary nearest logical
// x in a single-line field, through the same text box and scroll the painter
// uses.
func FieldOffsetAt(n *ui.Node, x int, measure ui.MeasureText) int {
	box := FieldTextRect(n)
	rel := x - box.X
	if n.Editing {
		rel += n.ScrollX
	}
	attrs := ui.TextAttrsOf(n)
	best, bestD := 0, rel
	if bestD < 0 {
		bestD = -bestD
	}
	for pos := 0; pos < len(n.Text); {
		pos = ui.NextGraphemeBoundary(n.Text, pos)
		w, _ := measure(ui.DisplayPrefix(n, pos), attrs)
		d := w - rel
		if d < 0 {
			d = -d
		}
		if d < bestD {
			best, bestD = pos, d
		}
	}
	return best
}
```

Export the boundary helper from `internal/ui/textedit.go`: `func NextGraphemeBoundary(s string, i int) int { return nextGrapheme(s, i) }`.

- [ ] **Step 4: Run the render tests**

Run: `GOWORK=off go test ./internal/render ./internal/ui -count=1`
Expected: PASS.

- [ ] **Step 5: Write the failing shell pointer test**

```go
package shell

import (
	"testing"
	"time"

	"github.com/Nomadcxx/sysc-shell/internal/platform/wayland"
	"github.com/Nomadcxx/sysc-shell/internal/render"
	"github.com/Nomadcxx/sysc-shell/internal/ui"
)

func pressAt(r *Registry, h *PanelHost, x, y int, mods ui.Mods) {
	handle := h.handle(r)
	handle(wayland.Event{Kind: wayland.EventPointerPress, X: float64(x), Y: float64(y), Button: 0x110, Mods: mods})
	handle(wayland.Event{Kind: wayland.EventPointerRelease, X: float64(x), Y: float64(y), Button: 0x110, Mods: mods})
}

func TestClickPlacesCaretAndDoubleClickSelectsAWord(t *testing.T) {
	r, h := openClipboardSearch(t)
	typeKeys(r, h, txt("o"), txt("n"), txt("e"), txt(" "), txt("t"), txt("w"), txt("o"))
	r.mu.Lock()
	n := h.focused()
	box := render.FieldTextRect(n)
	r.mu.Unlock()
	y := box.Y + box.H/2
	pressAt(r, h, box.X+1, y, 0)
	r.mu.Lock()
	f := h.fieldFor(h.focused())
	if f.Cursor != 0 {
		r.mu.Unlock()
		t.Fatalf("click at the start put the caret at %d", f.Cursor)
	}
	r.mu.Unlock()
	h.clickAt = time.Now()
	pressAt(r, h, box.X+1, y, 0)
	r.mu.Lock()
	defer r.mu.Unlock()
	if got := h.fieldFor(h.focused()).SelectedText(); got != "one" {
		t.Fatalf("double click selected %q, want one", got)
	}
}
```

Button `0x110` is `BTN_LEFT`; if the host compares against a named constant, use that constant instead.

- [ ] **Step 6: Run to verify it fails**

Run: `GOWORK=off go test ./internal/shell -run TestClickPlacesCaret -count=1`
Expected: FAIL — the caret stays at the end.

- [ ] **Step 7: Implement the pointer half**

In `EventPointerPress`, inside `if n := h.hitFocusable(...); n != nil {`, after `h.setFocus(n)`:

```go
				if n.Kind == ui.KindTextField && !n.Multiline {
					f := h.fieldFor(n)
					key := n.StableKey()
					now := time.Now()
					if key == h.clickKey && now.Sub(h.clickAt) <= multiClickInterval &&
						abs(h.hoverX-h.clickX) <= multiClickSlop && abs(h.hoverY-h.clickY) <= multiClickSlop {
						h.clicks++
					} else {
						h.clicks = 1
					}
					h.clickKey, h.clickAt, h.clickX, h.clickY = key, now, h.hoverX, h.hoverY
					view := copyNode(n)
					view.Editing, view.ScrollX = true, f.ScrollX
					pos := render.FieldOffsetAt(view, h.hoverX, h.measureText())
					switch h.clicks {
					case 1:
						f.SetCaret(pos, e.Mods.Has(ui.ModShift))
						h.fieldDrag = key
					case 2:
						f.SelectWordAt(pos)
					default:
						f.SelectLineAt(pos)
					}
					f.BreakUndo()
					f.SyncTo(n)
				}
```

(`abs` for ints: add `func abs(v int) int { if v < 0 { return -v }; return v }` if the package has none.)

In `EventPointerMotion`, before the hover resolution, when `h.fieldDrag != ""` and the focused node's stable key matches: compute `pos` the same way and call `f.SetCaret(pos, true)`, `f.SyncTo(n)`, return `true`. In `EventPointerRelease` and `EventPointerLeave`: `h.fieldDrag = ""`.

Multiline fields keep today's click behaviour (focus only) in this task; note it in the commit body.

- [ ] **Step 8: Run**

Run: `GOMAXPROCS=4 GOWORK=off go test -p 2 -count=1 ./internal/render ./internal/shell`
Expected: PASS apart from the four pre-existing failures.

- [ ] **Step 9: Commit**

```bash
git add internal/render internal/shell internal/ui
git commit -m "feat(shell): click places the caret; drag and double-click select"
```

---

## Phase 3 — Clipboard

### Task 11: The platform owns the system clipboard

**Files:**
- Create: `internal/platform/wayland/selection.go`, `internal/platform/wayland/selection_test.go`
- Modify: `internal/platform/wayland/client.go` (`Callbacks.Selection`, `EventPaste`, owner fields, bind after the seat), `internal/platform/wayland/wake.go` (bridge `Selection`, queue paste results)

**Interfaces:**
- Produces:

```go
// In client.go
const EventPaste EventKind = ... // appended to the EventKind list
// Event gains: Paste string — the clipboard text for EventPaste.
type SelectionRequest struct {
	Copy   string // non-empty: own the selection with this text
	Paste  bool   // read the current selection and deliver EventPaste
	Serial uint32 // the input event that asked
}
// Callbacks gains:
Selection <-chan SelectionRequest

// In selection.go
var textMimes = []string{"text/plain;charset=utf-8", "text/plain", "UTF8_STRING", "TEXT", "STRING"}
func pickTextMime(offered []string) (string, bool)
func readPaste(r io.Reader, limit int64, timeout time.Duration) (string, error)
func sanitizePaste(s string) string // drops NUL
const pasteLimit = 1 << 20
const pasteTimeout = time.Second
```

- [ ] **Step 1: Write the failing pure tests**

```go
package wayland

import (
	"errors"
	"os"
	"strings"
	"testing"
	"time"
)

func TestPickTextMimePrefersUTF8(t *testing.T) {
	if got, ok := pickTextMime([]string{"image/png", "STRING", "text/plain;charset=utf-8"}); !ok || got != "text/plain;charset=utf-8" {
		t.Fatalf("got %q %v", got, ok)
	}
	if _, ok := pickTextMime([]string{"image/png"}); ok {
		t.Fatal("picked a text mime from an image-only offer")
	}
}

func TestReadPasteCapsAndTimesOut(t *testing.T) {
	r, w, _ := os.Pipe()
	go func() { w.WriteString(strings.Repeat("x", 100)); w.Close() }()
	got, err := readPaste(r, 10, time.Second)
	if err != nil || got != strings.Repeat("x", 10) {
		t.Fatalf("capped read = %q, %v", got, err)
	}
	r2, w2, _ := os.Pipe()
	defer w2.Close()
	if _, err := readPaste(r2, 10, 20*time.Millisecond); !errors.Is(err, os.ErrDeadlineExceeded) {
		t.Fatalf("stalled writer: err = %v, want deadline exceeded", err)
	}
}

func TestSanitizePasteDropsNUL(t *testing.T) {
	if got := sanitizePaste("a\x00b\nc"); got != "ab\nc" {
		t.Fatalf("got %q", got)
	}
}
```

- [ ] **Step 2: Run to verify they fail**

Run: `GOWORK=off go test ./internal/platform/wayland -run 'PickTextMime|ReadPaste|SanitizePaste' -count=1`
Expected: FAIL — undefined.

- [ ] **Step 3: Implement `selection.go`**

```go
package wayland

import (
	"io"
	"log"
	"os"
	"strings"
	"syscall"
	"time"

	"github.com/Nomadcxx/sysc-wayland/client"
)

const (
	pasteLimit   = 1 << 20
	pasteTimeout = time.Second
)

var textMimes = []string{"text/plain;charset=utf-8", "text/plain", "UTF8_STRING", "TEXT", "STRING"}

func pickTextMime(offered []string) (string, bool) {
	for _, want := range textMimes {
		for _, got := range offered {
			if got == want {
				return want, true
			}
		}
	}
	return "", false
}

// readPaste reads at most limit bytes, giving up after timeout. It runs off
// the owner goroutine.
func readPaste(r *os.File, limit int64, timeout time.Duration) (string, error) {
	defer r.Close()
	_ = r.SetReadDeadline(time.Now().Add(timeout))
	b, err := io.ReadAll(io.LimitReader(r, limit))
	if err != nil {
		return "", err
	}
	return string(b), nil
}

func sanitizePaste(s string) string { return strings.ReplaceAll(s, "\x00", "") }

// selectionState is the owner's view of the seat's clipboard.
type selectionState struct {
	manager *client.DataDeviceManager
	device  *client.DataDevice
	source  *client.DataSource
	offer   *client.DataOffer
	mimes   map[*client.DataOffer][]string
	warned  map[string]bool
}

func (o *owner) warnSelectionOnce(kind string, err error) {
	if o.sel.warned == nil {
		o.sel.warned = map[string]bool{}
	}
	if !o.sel.warned[kind] {
		o.sel.warned[kind] = true
		log.Printf("wayland: clipboard %s: %v", kind, err)
	}
}

// bindSelection binds wl_data_device_manager (v3) and the seat's data
// device. Absent manager: copy and paste are silently unavailable.
func (o *owner) bindSelection(ctx *client.Context) error {
	entry, ok := o.rs.singletons["wl_data_device_manager"]
	if !ok {
		return nil
	}
	o.sel.manager = client.NewDataDeviceManager(ctx)
	if err := o.registry.Bind(entry.global, "wl_data_device_manager", min(entry.version, 3), o.sel.manager); err != nil {
		return err
	}
	dev, err := o.sel.manager.GetDataDevice(o.seat)
	if err != nil {
		return err
	}
	o.sel.device = dev
	o.sel.mimes = map[*client.DataOffer][]string{}
	dev.SetDataOfferHandler(func(e client.DataDeviceDataOfferEvent) {
		offer := e.Id
		o.sel.mimes[offer] = nil
		offer.SetOfferHandler(func(m client.DataOfferOfferEvent) {
			o.sel.mimes[offer] = append(o.sel.mimes[offer], m.MimeType)
		})
	})
	dev.SetSelectionHandler(func(e client.DataDeviceSelectionEvent) {
		if o.sel.offer != nil && o.sel.offer != e.Id {
			_ = o.sel.offer.Destroy()
			delete(o.sel.mimes, o.sel.offer)
		}
		o.sel.offer = e.Id
	})
	return nil
}

// handleSelection serves one request from the shell on the owner goroutine.
func (o *owner) handleSelection(req SelectionRequest, results chan<- pasteResult) {
	if o.sel.device == nil {
		return
	}
	switch {
	case req.Copy != "":
		src, err := o.sel.manager.CreateDataSource()
		if err != nil {
			o.warnSelectionOnce("source", err)
			return
		}
		for _, m := range textMimes {
			_ = src.Offer(m)
		}
		text := req.Copy
		src.SetSendHandler(func(e client.DataSourceSendEvent) {
			f := os.NewFile(uintptr(e.Fd), "wl-selection-send")
			go func() { _, _ = io.WriteString(f, text); f.Close() }()
		})
		src.SetCancelledHandler(func(client.DataSourceCancelledEvent) {
			_ = src.Destroy()
			if o.sel.source == src {
				o.sel.source = nil
			}
		})
		if err := o.sel.device.SetSelection(src, req.Serial); err != nil {
			o.warnSelectionOnce("set", err)
			return
		}
		o.sel.source = src
	case req.Paste:
		if o.sel.offer == nil {
			return
		}
		mime, ok := pickTextMime(o.sel.mimes[o.sel.offer])
		if !ok {
			return
		}
		var fds [2]int
		if err := syscall.Pipe2(fds[:], syscall.O_CLOEXEC); err != nil {
			o.warnSelectionOnce("pipe", err)
			return
		}
		if err := o.sel.offer.Receive(mime, fds[1]); err != nil {
			syscall.Close(fds[0])
			syscall.Close(fds[1])
			o.warnSelectionOnce("receive", err)
			return
		}
		syscall.Close(fds[1])
		o.flush() // the receive request must reach the compositor before the read
		unit := o.keyFocus.unit
		go func() {
			text, err := readPaste(os.NewFile(uintptr(fds[0]), "wl-selection-recv"), pasteLimit, pasteTimeout)
			results <- pasteResult{unit: unit, text: sanitizePaste(text), err: err}
		}()
	}
}

type pasteResult struct {
	unit *surfaceUnit
	text string
	err  error
}

// deliverPaste runs on the owner goroutine. A result for a surface that no
// longer has keyboard focus is dropped: the user has moved on.
func (o *owner) deliverPaste(p pasteResult) {
	if p.err != nil {
		o.warnSelectionOnce("read", p.err)
		return
	}
	if p.text == "" || p.unit == nil || o.keyFocus.unit != p.unit {
		return
	}
	o.deliverUnit(o.keyFocus.host, p.unit, Event{Kind: EventPaste, Paste: p.text})
}
```

If the owner has no `flush` method, use whatever `dispatchAll`/`handleAux` use to push requests (search `Flush(` in `client.go`), and write that name here instead. `o.sel` is a new `sel selectionState` field on `owner`.

In `client.go`: add `EventPaste` to the `EventKind` constants, `Paste string` to `Event`, `SelectionRequest`, and `Selection <-chan SelectionRequest` to `Callbacks` (optional — nil is allowed, like `Tooltips`). Call `o.bindSelection(ctx)` right after `bindOptionalInput`.

In `wake.go`: `bridge` gains a `selection <-chan SelectionRequest` parameter and a `pastes <-chan pasteResult` parameter; queue each into `w.selection []SelectionRequest` / `w.pastes []pasteResult` under `w.mu` and `w.signal()`. Add `takeSelection()` and `takePastes()` mirroring `takeAux`. In the owner loop, after `takeAux`, drain both: `for _, req := range wake.takeSelection() { o.handleSelection(req, pasteCh) }` and `for _, p := range wake.takePastes() { o.deliverPaste(p) }`, where `pasteCh := make(chan pasteResult, 4)` is created next to `wake` and passed to `bridge`.

- [ ] **Step 4: Run**

Run: `GOWORK=off go test ./internal/platform/wayland -count=1`
Expected: PASS.

- [ ] **Step 5: Write a paste-delivery test**

```go
// A paste that returns after focus moved is dropped, not typed elsewhere.
func TestPasteForAnUnfocusedSurfaceIsDropped(t *testing.T) {
	rh := newRepeatHarness(t, 0, 0)
	unit := rh.o.keyFocus.unit
	rh.o.leaveKeyboard()
	rh.o.deliverPaste(pasteResult{unit: unit, text: "hi"})
	for _, e := range *rh.seen {
		if e.Kind == EventPaste {
			t.Fatal("paste delivered to a surface without keyboard focus")
		}
	}
	rh.o.enterKeyboard(rh.o.keyFocus.host, unit)
}
```

(Enter keyboard again with the harness's host; if `keyFocus.host` is empty after leave, keep the host from before `leaveKeyboard` in a local.)

- [ ] **Step 6: Run and commit**

Run: `GOWORK=off go test ./internal/platform/wayland -count=1` — PASS.

```bash
git add internal/platform/wayland
git commit -m "feat(wayland): system clipboard through wl_data_device"
```

### Task 12: Fields copy, cut and paste

**Files:**
- Create: `internal/shell/selection.go`, `internal/shell/selection_test.go`
- Modify: `internal/shell/panelhost.go` (`handle` routes `EventPaste`; wire `copyRequest`/`pasteRequest`), `cmd/sysc-shell/main.go` (`Selection: registry.Selections()`)

**Interfaces:**
- Consumes: `SelectionRequest`, `EventPaste`, `Event.Paste` (Task 11); `fieldKey`, `copyRequest`, `pasteRequest` (Task 9).
- Produces: `func (r *Registry) Selections() <-chan wayland.SelectionRequest`; `func (h *PanelHost) applyPaste(r *Registry, text string) bool`.

- [ ] **Step 1: Write the failing tests**

```go
package shell

import (
	"testing"

	"github.com/Nomadcxx/sysc-shell/internal/platform/wayland"
	"github.com/Nomadcxx/sysc-shell/internal/ui"
)

func TestCopyAndPasteRequestsReachThePlatform(t *testing.T) {
	r, h := openClipboardSearch(t)
	typeKeys(r, h, txt("h"), txt("i"), ui.KeyInput{Sym: 'a', Text: "a", Mods: ui.ModCtrl, Serial: 9},
		ui.KeyInput{Sym: 'c', Text: "c", Mods: ui.ModCtrl, Serial: 10},
		ui.KeyInput{Sym: 'v', Text: "v", Mods: ui.ModCtrl, Serial: 11})
	got := []wayland.SelectionRequest{<-r.Selections(), <-r.Selections()}
	if got[0].Copy != "hi" || got[0].Serial != 10 || !got[1].Paste || got[1].Serial != 11 {
		t.Fatalf("requests %+v", got)
	}
}

// Review focus 3.
func TestPasteIntoSingleLineFieldFlattensAndIsOneUndoStep(t *testing.T) {
	r, h := openClipboardSearch(t)
	typeKeys(r, h, txt("x"))
	h.handle(r)(wayland.Event{Kind: wayland.EventPaste, Paste: "line one\nline\x00 two\r\nend"})
	if h.query != "xline one line two end" {
		t.Fatalf("query after paste = %q", h.query)
	}
	typeKeys(r, h, ui.KeyInput{Sym: 'z', Text: "z", Mods: ui.ModCtrl})
	if h.query != "x" {
		t.Fatalf("one undo after paste = %q, want x", h.query)
	}
}
```

- [ ] **Step 2: Run to verify they fail**

Run: `GOWORK=off go test ./internal/shell -run 'CopyAndPasteRequests|PasteIntoSingleLine' -count=1`
Expected: FAIL — `Selections` undefined.

- [ ] **Step 3: Implement**

`internal/shell/selection.go`:

```go
package shell

import (
	"strings"

	"github.com/Nomadcxx/sysc-shell/internal/platform/wayland"
	"github.com/Nomadcxx/sysc-shell/internal/ui"
)

// Selections is the shell's system-clipboard request stream. The platform
// owner drains it; a full buffer drops the request rather than block the
// registry lock.
func (r *Registry) Selections() <-chan wayland.SelectionRequest { return r.selections }

func (r *Registry) requestSelection(req wayland.SelectionRequest) {
	select {
	case r.selections <- req:
	default:
	}
}

// flattenPaste makes clipboard text fit a single-line field: each line break
// becomes one space.
func flattenPaste(s string) string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.ReplaceAll(s, "\r", "\n")
	return strings.ReplaceAll(s, "\n", " ")
}

// applyPaste inserts clipboard text into the focused field as one undo step.
func (h *PanelHost) applyPaste(r *Registry, text string) bool {
	return h.editField(r, func(f *ui.Field) {
		if !f.Multiline || f.SubmitOnEnter {
			text = flattenPaste(text)
		}
		f.BreakUndo()
		f.Commit(text)
	})
}
```

`Registry` gains `selections chan wayland.SelectionRequest`, made with capacity 8 in the constructor used by both production and `newPanelRegistry`. When a `PanelHost` is created (search `&PanelHost{` in `internal/shell`), set:

```go
	h.copyRequest = func(text string, serial uint32) { r.requestSelection(wayland.SelectionRequest{Copy: text, Serial: serial}) }
	h.pasteRequest = func(serial uint32) { r.requestSelection(wayland.SelectionRequest{Paste: true, Serial: serial}) }
```

In `handle`: `case wayland.EventPaste: return h.applyPaste(r, e.Paste)`. The NUL is already gone (platform `sanitizePaste`); the test's `\x00` exercises the shell path too, so also strip it in `applyPaste` with `strings.ReplaceAll(text, "\x00", "")` — defence at the boundary the shell owns.

In `cmd/sysc-shell/main.go`, add `Selection: registry.Selections(),` next to `Tooltips:`.

- [ ] **Step 4: Run**

Run: `GOMAXPROCS=4 GOWORK=off go test -p 2 -count=1 ./internal/shell ./cmd/sysc-shell`
Expected: PASS apart from the four pre-existing failures.

- [ ] **Step 5: Commit**

```bash
git add internal/shell cmd/sysc-shell
git commit -m "feat(shell): copy, cut and paste in every text field"
```

---

## Phase 4 — Keymaps (after `sysc-591` Tasks 5–6 merge)

### Task 13: The xkb resolver

**Files:**
- Create: `internal/platform/wayland/keymap.go`, `internal/platform/wayland/keymap_test.go`
- Create: `internal/platform/wayland/testdata/keymap-de.xkb`, `testdata/keymap-us-intl.xkb`, `testdata/Compose.test`
- Modify: `go.mod` (`github.com/thegrumpylion/xkb-go v0.1.0`)

**Interfaces:**
- Produces:

```go
type keymapResolver struct { /* xkb keymap, state, compose state */ }
func newKeymapResolver(text []byte, composeFile string) (*keymapResolver, error)
func (k *keymapResolver) setMask(depressed, latched, locked, group uint32)
func (k *keymapResolver) resolve(code uint32, mods ui.Mods) (sym uint32, text string)
func (k *keymapResolver) resetCompose()
```

`composeFile == ""` loads the compose table from the locale (`LC_ALL`, `LC_CTYPE`, `LANG`, then `C`). A compose failure leaves compose off, not the keymap.

- [ ] **Step 1: Generate the fixtures**

```bash
xkbcli compile-keymap --layout de > internal/platform/wayland/testdata/keymap-de.xkb
xkbcli compile-keymap --layout us --variant intl > internal/platform/wayland/testdata/keymap-us-intl.xkb
cat > internal/platform/wayland/testdata/Compose.test <<'EOF'
<dead_acute> <e> : "é" eacute
<dead_acute> <a> : "á" aacute
EOF
```

- [ ] **Step 2: Add the dependency**

Run: `GOWORK=off go get github.com/thegrumpylion/xkb-go@v0.1.0`

- [ ] **Step 3: Write the failing tests**

```go
package wayland

import (
	"os"
	"testing"

	"github.com/Nomadcxx/sysc-shell/internal/ui"
)

func loadResolver(t *testing.T, name string) *keymapResolver {
	t.Helper()
	raw, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatal(err)
	}
	k, err := newKeymapResolver(raw, "testdata/Compose.test")
	if err != nil {
		t.Fatal(err)
	}
	return k
}

const (
	evY, evQ, evE, evA, ev2, evApostrophe, evEqual = 21, 16, 18, 30, 3, 40, 13
	maskShift, maskLock, maskCtrl, maskMod5          = 1 << 0, 1 << 1, 1 << 2, 1 << 7
)

func TestGermanLayoutResolves(t *testing.T) {
	k := loadResolver(t, "keymap-de.xkb")
	cases := []struct {
		name          string
		dep, locked   uint32
		code          uint32
		wantText      string
	}{
		{"Y types z", 0, 0, evY, "z"},
		{"Shift+Y types Z", maskShift, 0, evY, "Z"},
		{"AltGr+Q types @", maskMod5, 0, evQ, "@"},
		{"AltGr+E types €", maskMod5, 0, evE, "€"},
		{"ä", 0, 0, evApostrophe, "ä"},
		{"Shift+2 types quote", maskShift, 0, ev2, "\""},
	}
	for _, tc := range cases {
		k.setMask(tc.dep, 0, tc.locked, 0)
		if _, got := k.resolve(tc.code, ui.ModsFromMask(tc.dep, 0, tc.locked)); got != tc.wantText {
			t.Errorf("%s: %q", tc.name, got)
		}
	}
}

// Review focus 4: xkb-go v0.1.0 ignores Lock for alphabetic keys.
func TestCapsLockUppercasesLetters(t *testing.T) {
	k := loadResolver(t, "keymap-de.xkb")
	k.setMask(0, 0, maskLock, 0)
	if _, got := k.resolve(evA, ui.ModCapsLock); got != "A" {
		t.Fatalf("Caps+a = %q", got)
	}
	k.setMask(maskShift, 0, maskLock, 0)
	if _, got := k.resolve(evA, ui.ModCapsLock|ui.ModShift); got != "a" {
		t.Fatalf("Caps+Shift+a = %q", got)
	}
	k.setMask(0, 0, maskLock, 0)
	if _, got := k.resolve(ev2, ui.ModCapsLock); got != "2" {
		t.Fatalf("Caps+2 = %q", got)
	}
}

func TestDeadKeyComposes(t *testing.T) {
	k := loadResolver(t, "keymap-us-intl.xkb")
	k.setMask(0, 0, 0, 0)
	if sym, got := k.resolve(evApostrophe, 0); got != "" || sym != 0xfe51 {
		t.Fatalf("dead_acute = %#x %q, want dead_acute and no text", sym, got)
	}
	if _, got := k.resolve(evE, 0); got != "é" {
		t.Fatalf("dead_acute e = %q", got)
	}
}

func TestControlCharactersNeverReachText(t *testing.T) {
	k := loadResolver(t, "keymap-de.xkb")
	k.setMask(maskCtrl, 0, 0, 0)
	if sym, got := k.resolve(evA, ui.ModCtrl); sym != 'a' || got != "a" {
		t.Fatalf("Ctrl+a = %#x %q", sym, got)
	}
	for _, r := range func() string { _, s := k.resolve(28, ui.ModCtrl); return s }() {
		if r < 0x20 || r == 0x7f {
			t.Fatalf("control character %U in text", r)
		}
	}
}

func TestMalformedKeymapIsAnError(t *testing.T) {
	if _, err := newKeymapResolver([]byte("xkb_keymap { nonsense"), ""); err == nil {
		t.Fatal("malformed keymap parsed")
	}
}
```

- [ ] **Step 4: Run to verify they fail**

Run: `GOWORK=off go test ./internal/platform/wayland -run 'German|CapsLock|DeadKey|ControlCharacters|MalformedKeymap' -count=1`
Expected: FAIL — `newKeymapResolver` undefined.

- [ ] **Step 5: Implement `keymap.go`**

```go
package wayland

import (
	"context"
	"os"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/Nomadcxx/sysc-shell/internal/ui"
	xkb "github.com/thegrumpylion/xkb-go"
)

// keymapResolver turns evdev codes into keysyms and text through the
// compositor's keymap. xkb-go is pinned at v0.1.0; two of its defects are
// worked around here and recorded in the design doc:
//   - Keymap.ModGetIndex returns -1 for every name on a parsed keymap, so
//     modifier masks are passed through by fixed real-modifier bit.
//   - Caps Lock does not select the capital level of alphabetic keys, so
//     letters are cased here.
type keymapResolver struct {
	state   *xkb.State
	compose *xkb.ComposeState
}

func newKeymapResolver(text []byte, composeFile string) (*keymapResolver, error) {
	ctx := xkb.NewContext(context.Background(), xkb.ContextNoFlags)
	km, err := ctx.NewKeymapFromString(text, xkb.KeymapFormatTextV1)
	if err != nil {
		return nil, err
	}
	k := &keymapResolver{state: km.NewState()}
	var table *xkb.ComposeTable
	if composeFile != "" {
		table, err = ctx.NewComposeTableFromFile(composeFile, "C", xkb.ComposeCompileNoFlags)
	} else {
		table, err = ctx.NewComposeTableFromLocale(composeLocale(), xkb.ComposeCompileNoFlags)
	}
	if err == nil && table != nil {
		k.compose = table.NewState(xkb.ComposeStateNoFlags)
	}
	return k, nil
}

func composeLocale() string {
	for _, v := range []string{"LC_ALL", "LC_CTYPE", "LANG"} {
		if s := os.Getenv(v); s != "" {
			return s
		}
	}
	return "C"
}

func (k *keymapResolver) setMask(depressed, latched, locked, group uint32) {
	k.state.UpdateMask(xkb.ModMask(depressed), xkb.ModMask(latched), xkb.ModMask(locked), 0, 0, xkb.Group(group))
}

func (k *keymapResolver) resetCompose() {
	if k.compose != nil {
		k.compose.Reset()
	}
}

// resolve returns the keysym and text for an evdev code under the current
// mask. Text is "" for non-printing keys, while a compose sequence is open,
// and never carries a control character.
func (k *keymapResolver) resolve(code uint32, mods ui.Mods) (uint32, string) {
	kc := xkb.Keycode(code + 8)
	sym := k.state.KeyGetOneSym(kc)
	text := k.state.KeyGetUTF8(kc)
	if k.compose != nil && !mods.Has(ui.ModCtrl) {
		if k.compose.Feed(sym) == xkb.ComposeFeedAccepted {
			switch k.compose.GetStatus() {
			case xkb.ComposeComposing:
				return uint32(sym), ""
			case xkb.ComposeComposed:
				text = k.compose.GetUTF8()
				if s := k.compose.GetOneSym(); s != xkb.KeyNoSymbol {
					sym = s
				}
				k.compose.Reset()
			case xkb.ComposeCancelled:
				k.compose.Reset()
				return uint32(sym), ""
			}
		}
	}
	return uint32(sym), stripControl(caseForLock(text, mods))
}

// caseForLock applies Caps Lock to a single cased letter: upper with Lock
// alone, lower with Lock and Shift. xkb-go v0.1.0 leaves this undone.
func caseForLock(text string, mods ui.Mods) string {
	if !mods.Has(ui.ModCapsLock) {
		return text
	}
	r, size := utf8.DecodeRuneInString(text)
	if size != len(text) || !unicode.IsLetter(r) || unicode.ToUpper(r) == unicode.ToLower(r) {
		return text
	}
	if mods.Has(ui.ModShift) {
		return string(unicode.ToLower(r))
	}
	return string(unicode.ToUpper(r))
}

func stripControl(s string) string {
	return strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f {
			return -1
		}
		return r
	}, s)
}
```

Keep `sym` as xkb returned it: shortcut matching lower-cases it in `ui.HandleKey`.

If `TestCapsLockUppercasesLetters` shows xkb-go already uppercasing (a later version fixed it), `caseForLock` still produces the right answer — it is idempotent for Lock alone; for Lock+Shift it relies on xkb returning the shifted (capital) letter, which it does.

- [ ] **Step 6: Run**

Run: `GOWORK=off go test ./internal/platform/wayland -count=1`
Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add go.mod go.sum internal/platform/wayland/keymap.go internal/platform/wayland/keymap_test.go internal/platform/wayland/testdata
git commit -m "feat(wayland): resolve keys through the compositor keymap with xkb-go"
```

### Task 14: The owner uses the keymap

**Files:**
- Modify: `internal/platform/wayland/keyboard.go` (`keyEvent`, `setModifiers`, `leaveKeyboard`), `internal/platform/wayland/client.go` (keymap handler)
- Test: `internal/platform/wayland/keymapowner_test.go` (new)

**Interfaces:**
- Consumes: `keymapResolver` (Task 13), `keyEvent`/`setModifiers` (Task 5).
- Produces: owner field `keymap *keymapResolver` (nil = fallback); `func (o *owner) loadKeymap(format uint32, fd int, size uint32)`.

- [ ] **Step 1: Write the failing tests**

```go
package wayland

import (
	"os"
	"testing"
	"time"

	"github.com/Nomadcxx/sysc-wayland/client"
)

func withKeymap(t *testing.T, rh *repeatHarness, name string) {
	t.Helper()
	raw, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatal(err)
	}
	k, err := newKeymapResolver(raw, "testdata/Compose.test")
	if err != nil {
		t.Fatal(err)
	}
	rh.o.keymap = k
}

func TestOwnerTypesThroughTheKeymap(t *testing.T) {
	rh := newRepeatHarness(t, 25, 600)
	withKeymap(t, rh, "keymap-de.xkb")
	rh.o.setModifiers(0, 0, 0, 0)
	rh.o.deliverKey(5, evY, uint32(client.KeyboardKeyStatePressed))
	if got := (*rh.seen)[len(*rh.seen)-1]; got.Text != "z" {
		t.Fatalf("de Y = %q", got.Text)
	}
}

// Review focus 5.
func TestRepeatResolvesTextAtDeliveryTime(t *testing.T) {
	rh := newRepeatHarness(t, 25, 600)
	withKeymap(t, rh, "keymap-de.xkb")
	rh.o.setModifiers(0, 0, 0, 0)
	rh.o.deliverKey(5, evA, uint32(client.KeyboardKeyStatePressed))
	rh.o.setModifiers(maskShift, 0, 0, 0)
	rh.advance(600 * time.Millisecond)
	if got := (*rh.seen)[len(*rh.seen)-1]; got.Text != "A" {
		t.Fatalf("repeat after Shift = %q, want A", got.Text)
	}
}

func TestUnsupportedKeymapFormatKeepsTheFallback(t *testing.T) {
	rh := newRepeatHarness(t, 25, 600)
	rh.o.loadKeymap(0 /* no_keymap */, -1, 0)
	if rh.o.keymap != nil {
		t.Fatal("no_keymap installed a resolver")
	}
	rh.o.deliverKey(5, evA, uint32(client.KeyboardKeyStatePressed))
	if got := (*rh.seen)[len(*rh.seen)-1]; got.Text != "a" {
		t.Fatalf("fallback a = %q", got.Text)
	}
}

func TestLeavingFocusCancelsAnOpenCompose(t *testing.T) {
	rh := newRepeatHarness(t, 25, 600)
	withKeymap(t, rh, "keymap-us-intl.xkb")
	host, unit := rh.o.keyFocus.host, rh.o.keyFocus.unit
	rh.o.deliverKey(5, evApostrophe, uint32(client.KeyboardKeyStatePressed))
	rh.o.leaveKeyboard()
	rh.o.enterKeyboard(host, unit)
	rh.o.deliverKey(6, evE, uint32(client.KeyboardKeyStatePressed))
	if got := (*rh.seen)[len(*rh.seen)-1]; got.Text != "e" {
		t.Fatalf("after refocus e = %q, want plain e", got.Text)
	}
}
```

- [ ] **Step 2: Run to verify they fail**

Run: `GOWORK=off go test ./internal/platform/wayland -run 'ThroughTheKeymap|ResolvesTextAtDelivery|UnsupportedKeymapFormat|CancelsAnOpenCompose' -count=1`
Expected: FAIL — `o.keymap` undefined.

- [ ] **Step 3: Implement**

Owner field `keymap *keymapResolver` and `keymapWarned bool`. In `keyboard.go`:

```go
// loadKeymap installs the compositor's keymap. Anything but a parseable
// xkb_v1 keymap keeps the US fallback, logged once per keymap event.
func (o *owner) loadKeymap(format uint32, fd int, size uint32) {
	if fd >= 0 {
		defer syscall.Close(fd)
	}
	o.keymap = nil
	if format != uint32(client.KeyboardKeymapFormatXkbV1) || fd < 0 || size == 0 {
		return
	}
	data, err := syscall.Mmap(fd, 0, int(size), syscall.PROT_READ, syscall.MAP_PRIVATE)
	if err != nil {
		log.Printf("wayland: keymap mmap: %v; using the US fallback", err)
		return
	}
	defer syscall.Munmap(data)
	text := bytes.TrimRight(data, "\x00")
	k, err := newKeymapResolver(append([]byte(nil), text...), "")
	if err != nil {
		log.Printf("wayland: keymap parse: %v; using the US fallback", err)
		return
	}
	o.keymap = k
}
```

`setModifiers` also calls `o.keymap.setMask(depressed, latched, locked, group)` when `o.keymap != nil`. `keyEvent` becomes:

```go
func (o *owner) keyEvent(kind EventKind, key, serial uint32) Event {
	e := Event{Kind: kind, Key: key, Serial: serial, Mods: o.mods}
	if o.keymap == nil {
		k := ui.FallbackKey(key, o.mods)
		e.Sym, e.Text = k.Sym, k.Text
		return e
	}
	if kind == EventKeyRelease {
		// A release must not feed compose; only its keysym matters.
		e.Sym, _ = o.keymap.peek(key)
		return e
	}
	e.Sym, e.Text = o.keymap.resolve(key, o.mods)
	return e
}
```

Add to `keymap.go`:

```go
// peek returns the keysym for a code without touching compose state.
func (k *keymapResolver) peek(code uint32) (uint32, string) {
	kc := xkb.Keycode(code + 8)
	return uint32(k.state.KeyGetOneSym(kc)), ""
}
```

`leaveKeyboard` calls `o.keymap.resetCompose()` when non-nil. In `client.go` next to the modifiers handler:

```go
		keyboard.SetKeymapHandler(func(e client.KeyboardKeymapEvent) {
			o.loadKeymap(e.Format, e.Fd, e.Size)
		})
```

If the generated constant is named differently than `client.KeyboardKeymapFormatXkbV1`, use the generated name (search `KeymapFormat` in sysc-wayland's `client/client.go`); its value is 1.

- [ ] **Step 4: Run**

Run: `GOMAXPROCS=4 GOWORK=off go test -p 2 -count=1 ./internal/platform/wayland ./internal/shell ./internal/ui`
Expected: PASS apart from the four pre-existing `internal/shell` failures.

- [ ] **Step 5: Commit**

```bash
git add internal/platform/wayland
git commit -m "feat(wayland): type through the compositor keymap with a US fallback"
```

### Task 15: Gates, live check, and tracking

**Files:**
- [ ] **Step 1: Full capped gate**

Run: `GOMAXPROCS=4 GOWORK=off go vet ./... && GOMAXPROCS=4 GOWORK=off go test -p 2 -count=1 ./...`
Expected: PASS apart from failures that also fail on a clean `origin/main` worktree. List each such failure with the command that proved it pre-existing.

- [ ] **Step 2: Plugin repo compatibility**

In a `sysc-plugins` worktree off its `origin/main`, point `go.mod` at this branch's commit (`GOWORK=off go get github.com/Nomadcxx/sysc-shell@<commit>`) and run `GOMAXPROCS=4 GOWORK=off go test -p 2 -count=1 ./...`. Fit lints may change because fields no longer grow; a plugin whose field now overflows is a real layout bug to report, not to paper over. Do not commit the plugin repo's `go.mod` change as part of this task.

- [ ] **Step 3: Live gate on Niri (laptop, per the deploy rule)**

Build from this worktree after merging `origin/main`, confirm `git merge-base --is-ancestor origin/main HEAD`, check the deployed `vcs.revision` is an ancestor, keep a rollback copy, deploy, restart, and check `journalctl --user -u sysc-shell` for `closing surface` or `panic`. Then, with the owner at the keyboard:

1. Launcher search: type past the right edge — the caret stays visible; Home/End, Ctrl+Left/Right, Shift+arrows, Ctrl+A, Ctrl+Backspace, Ctrl+Z all behave per spec D3.
2. `niri msg action switch-layout` to `de`: Y types z, AltGr+Q types @. Switch to `us(intl)`: `'` then `e` types é.
3. Copy from the github-notifications panel search, paste into a terminal; copy from the terminal, paste into the search. A multi-line paste arrives on one line.
4. Double-click selects a word, triple-click selects all; a click places the caret.
5. Hold Backspace: it repeats; press Shift mid-hold on a letter: repeats switch case.
6. Network password field: Ctrl+C copies nothing.

Afterwards redeploy a clean `origin/main` build per the rule, and report `vcs.revision` and `vcs.modified` for every binary deployed.

- [ ] **Step 4: Tracking**

Run from the primary checkout, not a worktree:

```bash
cd /home/nomadx/sysc-shell
bd create "text-input-v3 cursor rectangle follows the scrolled caret" -t task -p 3 --deps discovered-from:sysc-623 -d "Nothing sets the IME cursor rectangle; text input parity (sysc-623) deferred it. Needs a shell-to-platform channel for the focused field's caret rect."
bd create "xkb-go v0.1.0: ModGetIndex -1 on parsed keymaps; Caps Lock ignored for alphabetic keys" -t task -p 3 --deps discovered-from:sysc-623 -d "Worked around in internal/platform/wayland/keymap.go. File upstream and drop the workarounds when fixed."
bd close sysc-171 --reason "Key repeat shipped in f755eea; focus-move repair is sysc-591 Tasks 5-6."
bd close sysc-623 --reason "Text input parity landed; live gate passed on the laptop."
```

Commit `.beads/issues.jsonl` only if the primary checkout's JSONL holds no other session's uncommitted changes; otherwise leave it for the next `chore(tracking)` commit and say so.

The plan's register row landed with the plan itself; nothing to add here.
