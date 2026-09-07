# Notification centre polish — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make the notification centre readable — stratified surfaces, one filter row, a merged list, per-card remove, and concave fillets joining the panel to the bar.

**Architecture:** Three slices in dependency order. Slice 1 adds one command to `sysc-notify` and bumps the pin. Slice 2 adds the shared chrome primitives every panel inherits. Slice 3 rebuilds the centre's tree using them. The projection, grouping, DND policy and toast chrome are untouched throughout.

**Tech Stack:** Go, no CGO. `internal/ui` retained tree, `internal/render` software painter, `internal/theme` token tables. `github.com/Nomadcxx/sysc-notify` over a presenter socket. fontTools for the icon subset.

**Spec:** `docs/plans/2026-09-07-notification-centre-polish-design.md`

## Global Constraints

- Go only. No C++, Rust, Lua, Qt, QML. CGO needs prior approval (`AGENTS.md`).
- Wayland types stay in `internal/platform/wayland`; Niri wire types in `internal/platform/niri`.
- Stop at the first working rung: existing project code, stdlib, native service, pinned dependency, then new code.
- One focused runnable check per non-trivial unit. Table tests for pure layout and protocol code.
- bd is the only tracker. No markdown task lists, no session todos. Claim with `bd update <id> --status in_progress`.
- Commit `.beads/issues.jsonl` in the same commit as the code it describes.
- Run `bd` from `/home/nomadx/sysc-shell`, never a worktree. Committing from a worktree needs `BEADS_DB=/home/nomadx/sysc-shell/.beads/beads.db`.
- `Panels.Gap` is 0, `Panels.Padding` is 8, `BarGap` is 4. `PanelNotifications` is 416x300.
- Icon names must exist in `materialIcons` (`internal/render/materialfont.go`) or the control paints nothing.

### The test gate this machine can actually run

`AGENTS.md` prescribes `go test -race -count=1 ./...` for every code-touching commit. **That gate is unrunnable on this workstation and is denied by a hook.** The box has zram-only swap and links 16-way; the race detector over the full tree has hard-locked it twice, and `.cursor/hooks/deny-go-race.py` now refuses any `go test` carrying `-race`, plus any `go test ... ./...`.

Use the per-package, non-race form everywhere in this plan:

```bash
timeout 90s env GOMAXPROCS=2 go test -count=1 <pkg> -run <Name>
```

Run the packages you touched, one invocation each — never a tree-wide sweep. If a race genuinely needs proving, ask the owner before attempting it; it is a deliberate machine-level decision, not something to work around.

`go vet ./...` and `gofmt` are fine and still required.

### Two further machine traps

- **`go test ./internal/shell` runs `loginctl terminate-session self` for real** and will log you out. Put a no-op `loginctl` on `PATH` first:
  ```bash
  mkdir -p /tmp/shim && printf '#!/bin/sh\nexit 0\n' > /tmp/shim/loginctl && chmod +x /tmp/shim/loginctl
  ```
  Every `internal/shell` command below assumes `PATH=/tmp/shim:$PATH`.
- The machine `commit-msg` hook rejects messages containing `agent`, `cursor`, `codex`, `llm`, `both`, `Hallmark` **as substrings**. "both" inside an ordinary sentence is rejected. Screen every message.

---

# Slice 1 — `history.remove` (bd `sysc-153`)

Worktree: `~/.config/superpowers/worktrees/sysc-notify/redesign/v0.1`, branch `redesign/v0.1`, whose tip is tag `v0.1.0-rc.2`. `origin/main` is docs only and is **not** what this module compiles.

Tasks 1–4 happen in that repository. Task 5 returns here.

### Task 1: Protocol command kind

**Files:**
- Modify: `protocol/types.go:185-187` (the `CommandKind` block)
- Modify: `protocol/validate.go:264`
- Test: `protocol/validate_test.go`

**Interfaces:**
- Produces: `protocol.CommandHistoryRemove CommandKind = "history.remove"`, carried on the existing `Command.IDs []uint32` field.

- [ ] **Step 1: Write the failing test**

```go
func TestValidateHistoryRemoveRequiresIDs(t *testing.T) {
	if err := ValidateCommand(Command{Kind: CommandHistoryRemove}); err == nil {
		t.Fatal("empty id list accepted")
	}
	if err := ValidateCommand(Command{Kind: CommandHistoryRemove, IDs: []uint32{7}}); err != nil {
		t.Fatalf("valid remove rejected: %v", err)
	}
}
```

- [ ] **Step 2: Run it and confirm it fails**

```bash
cd ~/.config/superpowers/worktrees/sysc-notify/redesign/v0.1
timeout 90s env GOMAXPROCS=2 go test -count=1 ./protocol -run HistoryRemove -v
```

Expected: FAIL — `undefined: CommandHistoryRemove`.

- [ ] **Step 3: Add the kind**

In `protocol/types.go`, beside `CommandHistoryClear`:

```go
	CommandHistoryRemove     CommandKind = "history.remove"
```

- [ ] **Step 4: Add validation**

Line 264 accepts `CommandHistoryClear, CommandDismissAll` with no id. `history.remove` is the opposite — it requires a non-empty list, so it gets its own case:

```go
	case CommandHistoryRemove:
		if len(c.IDs) == 0 {
			return fmt.Errorf("protocol: %s needs at least one id", c.Kind)
		}
```

Read the surrounding cases first and match their error wording and whatever id-bound check they apply. Do not introduce a second style.

- [ ] **Step 5: Run and confirm it passes**

```bash
timeout 90s env GOMAXPROCS=2 go test -count=1 ./protocol -run HistoryRemove -v
```

Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add protocol/types.go protocol/validate.go protocol/validate_test.go
git commit -m "feat(protocol): define history.remove command"
```

### Task 2: `Store.Remove`

**Files:**
- Modify: `internal/history/store.go` (beside `Add`, line 100)
- Test: `internal/history/store_test.go`

**Interfaces:**
- Produces: `func (s *Store) Remove(ids []uint32) ([]uint32, error)` — returns the ids actually removed. Unknown ids are skipped, not an error; an all-unknown list returns an empty slice and nil.

Skipping unknown ids is deliberate: two shells open on one history can each remove the same entry, and the loser must not see a failure for work already done.

- [ ] **Step 1: Write the failing test**

```go
func TestStoreRemoveDropsKnownIDsOnly(t *testing.T) {
	s := newTestStore(t)
	now := time.Now()
	for _, id := range []uint32{1, 2, 3} {
		if _, _, err := s.Add(protocol.HistoryEntry{ID: id, AppName: "t"}, now); err != nil {
			t.Fatalf("seed %d: %v", id, err)
		}
	}

	got, err := s.Remove([]uint32{2, 99})
	if err != nil {
		t.Fatalf("remove: %v", err)
	}
	if len(got) != 1 || got[0] != 2 {
		t.Fatalf("removed = %v, want [2]", got)
	}

	for _, e := range s.Entries() {
		if e.ID == 2 {
			t.Fatal("entry 2 survived removal")
		}
	}
	if len(s.Entries()) != 2 {
		t.Fatalf("entries = %d, want 2", len(s.Entries()))
	}
}
```

`newTestStore` stands in for whatever constructor this file's existing tests already use to build a `Store` over a temp directory. Read the file first and use the real one.

- [ ] **Step 2: Run it and confirm it fails**

```bash
timeout 90s env GOMAXPROCS=2 go test -count=1 ./internal/history -run StoreRemove -v
```

Expected: FAIL — `s.Remove undefined`.

- [ ] **Step 3: Implement**

```go
// Remove drops the named entries and returns the ids that were actually
// present. An unknown id is skipped rather than refused: two shells may remove
// the same entry, and the loser must not see a failure for work already done.
func (s *Store) Remove(ids []uint32) ([]uint32, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	drop := make(map[uint32]struct{}, len(ids))
	for _, id := range ids {
		drop[id] = struct{}{}
	}
	kept := make([]protocol.HistoryEntry, 0, len(s.entries))
	removed := make([]uint32, 0, len(ids))
	for _, e := range s.entries {
		if _, ok := drop[e.ID]; ok {
			removed = append(removed, e.ID)
			continue
		}
		kept = append(kept, e)
	}
	if len(removed) == 0 {
		return nil, nil
	}
	if err := s.commit(kept); err != nil {
		return nil, err
	}
	return removed, nil
}
```

Check `Add` for the exact mutex and slice field names, and whether `commit` already handles the entry assignment and orphaned-image cleanup. Mirror it — do not duplicate cleanup that `commit` performs.

- [ ] **Step 4: Run and confirm it passes**

```bash
timeout 90s env GOMAXPROCS=2 go test -count=1 ./internal/history -run StoreRemove -v
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/history/store.go internal/history/store_test.go
git commit -m "feat(history): remove named entries"
```

### Task 3: Owner command and delta

**Files:**
- Modify: `internal/state/owner.go:65` (command kind block), and the `Do` switch near line 261
- Test: `internal/state/owner_test.go`

**Interfaces:**
- Consumes: `history.Store.Remove` (Task 2).
- Produces: `state.HistoryRemove`, reading `Command.IDs`.

The delta already exists: `owner.go:376` publishes `protocol.Delta{Kind: protocol.DeltaHistoryRemoved, ID: removedID}` for retention eviction. Reuse that exact publication, one delta per removed id.

- [ ] **Step 1: Write the failing test**

```go
func TestHistoryRemovePublishesOneDeltaPerEntry(t *testing.T) {
	owner, deltas := newTestOwner(t)
	seedHistory(t, owner, 1, 2, 3)

	if _, err := owner.Do(context.Background(), Command{Kind: HistoryRemove, IDs: []uint32{1, 3}}); err != nil {
		t.Fatalf("remove: %v", err)
	}

	var removed []uint32
	for _, d := range drain(deltas) {
		if d.Kind == protocol.DeltaHistoryRemoved {
			removed = append(removed, d.ID)
		}
	}
	if !slices.Equal(removed, []uint32{1, 3}) {
		t.Fatalf("deltas = %v, want [1 3]", removed)
	}
}
```

`newTestOwner`, `seedHistory` and `drain` stand in for this file's real fixtures. Read `owner_test.go` first and use those.

- [ ] **Step 2: Run it and confirm it fails**

```bash
timeout 90s env GOMAXPROCS=2 go test -count=1 ./internal/state -run HistoryRemove -v
```

Expected: FAIL — `undefined: HistoryRemove`.

- [ ] **Step 3: Add the kind and handler**

In the command kind block beside `HistoryClear`:

```go
	HistoryRemove
```

In `Do`, beside the `HistoryClear` case:

```go
	case HistoryRemove:
		removed, err := s.history.Remove(cmd.IDs)
		if err != nil {
			return Result{}, err
		}
		for _, id := range removed {
			s.publishDelta(protocol.Delta{Kind: protocol.DeltaHistoryRemoved, ID: id})
		}
```

Match the surrounding cases for how they name the store field and what they put in `Result`.

- [ ] **Step 4: Run and confirm it passes**

```bash
timeout 90s env GOMAXPROCS=2 go test -count=1 ./internal/state -run HistoryRemove -v
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/state/owner.go internal/state/owner_test.go
git commit -m "feat(state): own history removal"
```

### Task 4: Presenter dispatch and the tag

**Files:**
- Modify: `internal/presenter/connection.go:206` (the `executeCommand` switch)
- Test: `internal/presenter/connection_test.go`

**Interfaces:**
- Consumes: `protocol.CommandHistoryRemove` (Task 1), `state.HistoryRemove` (Task 3).
- Produces: tag `v0.1.0-rc.3`, which Task 5 pins.

- [ ] **Step 1: Write the failing test**

```go
func TestExecuteHistoryRemoveReachesState(t *testing.T) {
	owner, _ := newTestOwner(t)
	seedHistory(t, owner, 5)

	reply := executeCommand(owner, 0, protocol.Command{
		Kind: protocol.CommandHistoryRemove,
		IDs:  []uint32{5},
	})
	if !reply.OK {
		t.Fatalf("reply = %+v, want OK", reply)
	}
}
```

- [ ] **Step 2: Run it and confirm it fails**

```bash
timeout 90s env GOMAXPROCS=2 go test -count=1 ./internal/presenter -run HistoryRemove -v
```

Expected: FAIL — an unmapped kind leaves `stateCommand.Kind` at its zero value, so the reply carries an error.

- [ ] **Step 3: Add the dispatch case**

Beside `case protocol.CommandHistoryClear:` at line 206:

```go
	case protocol.CommandHistoryRemove:
		stateCommand.Kind = state.HistoryRemove
```

`executeCommand` already copies `command.IDs` into `stateCommand` at the top; nothing further is needed.

- [ ] **Step 4: Run the gate**

```bash
gofmt -w . && test -z "$(gofmt -l .)"
go vet ./...
for p in ./protocol ./internal/history ./internal/state ./internal/presenter; do
  timeout 90s env GOMAXPROCS=2 go test -count=1 "$p" || break
done
```

Expected: all pass.

- [ ] **Step 5: Commit and tag**

```bash
git add internal/presenter/connection.go internal/presenter/connection_test.go
git commit -m "feat(presenter): dispatch history.remove"
git tag v0.1.0-rc.3
```

**Stop before pushing.** Pushing publishes the tag. Confirm with the owner, then:

```bash
git push origin redesign/v0.1 --tags
```

### Task 5: Pin bump and capability flip

**Files:** (back in `/home/nomadx/sysc-shell`)
- Modify: `go.mod:16`, `go.sum`
- Modify: `internal/shell/notifycard.go:263-265`
- Modify: `docs/plans/README.md` (sibling-repositories row for `sysc-notify`)
- Test: `internal/shell/notifycard_test.go`

**Interfaces:**
- Produces: `historyRemoveSupported() == true`, which Task 16 relies on to paint the card remove button.

- [ ] **Step 1: Write the failing test**

```go
func TestHistoryRemoveSupportedOnCurrentPin(t *testing.T) {
	if !historyRemoveSupported() {
		t.Fatal("pin carries history.remove but the shell reports it unsupported")
	}
}
```

- [ ] **Step 2: Run it and confirm it fails**

```bash
PATH=/tmp/shim:$PATH timeout 90s env GOMAXPROCS=2 go test -count=1 ./internal/shell -run HistoryRemoveSupported -v
```

Expected: FAIL.

- [ ] **Step 3: Bump the pin**

```bash
go get github.com/Nomadcxx/sysc-notify@v0.1.0-rc.3
go mod tidy
```

- [ ] **Step 4: Flip the flag**

Replace the function and its comment at `notifycard.go:263-265`:

```go
// historyRemoveSupported reports whether the pinned sysc-notify accepts
// history.remove. True from v0.1.0-rc.3. The flag stays so a rollback to an
// older pin disables the control rather than painting one the service rejects.
func historyRemoveSupported() bool { return true }
```

- [ ] **Step 5: Run and confirm it passes**

```bash
PATH=/tmp/shim:$PATH timeout 90s env GOMAXPROCS=2 go test -count=1 ./internal/shell -run HistoryRemoveSupported -v
```

Expected: PASS.

- [ ] **Step 6: Update the register and close the issue**

In `docs/plans/README.md`, the sibling-repositories row for `/home/nomadx/sysc-notify`: change the pin to `v0.1.0-rc.3` and the commit to the new tag's hash.

```bash
bd close sysc-153 --reason "history.remove landed in v0.1.0-rc.3; shell pinned and capability flipped"
bd export -o .beads/issues.jsonl
```

- [ ] **Step 7: Commit**

```bash
git add go.mod go.sum internal/shell/notifycard.go internal/shell/notifycard_test.go docs/plans/README.md .beads/issues.jsonl
git commit -m "feat(notify): pin rc.3 and enable per-entry removal"
```

---

# Slice 2 — Shared chrome primitives (bd `sysc-198`)

Everything here is reachable by panels beyond the notification centre. Land it separately so a regression is attributable.

### Task 6: Material subset gains `delete` and `schedule`

**Files:**
- Modify: `internal/render/icons/material/build.py` (the `ICONS` list, line 35)
- Modify: `internal/render/icons/material/material-symbols-rounded.ttf` (regenerated)
- Modify: `internal/render/icons/material/SOURCE.md` (inventory, glyph count, byte size, output hash)
- Modify: `internal/render/materialfont.go:23-29` (`materialIcons`)
- Test: `internal/render/materialfont_test.go`

**Interfaces:**
- Produces: `render.ValidMaterialIcon("delete")` and `("schedule")` return true; both rasterise through `RasterMaterialIcon`.

- [ ] **Step 1: Write the failing test**

```go
func TestSubsetCarriesCentreGlyphs(t *testing.T) {
	for _, name := range []string{"delete", "schedule"} {
		if !ValidMaterialIcon(name) {
			t.Fatalf("%q is not in the inventory", name)
		}
	}
}
```

Then extend it to rasterise each name at size 20 through whatever `TextRenderer` fixture `materialfont_test.go` already builds. Read the file first; do not add a second renderer fixture.

- [ ] **Step 2: Run it and confirm it fails**

```bash
timeout 90s env GOMAXPROCS=2 go test -count=1 ./internal/render -run CentreGlyphs -v
```

Expected: FAIL — `"delete" is not in the inventory`.

- [ ] **Step 3: Fetch the pinned upstream font**

The local `~/.local/share/fonts/MaterialSymbolsRounded.ttf` hashes `4b959703…` and is a **different cut**; `build.py` will reject it. Fetch the pinned one:

```bash
curl -L -o /tmp/MaterialSymbolsRounded.ttf \
  'https://raw.githubusercontent.com/google/material-design-icons/84ccef280841abfac506afc4ad4a2782f6d0a1d0/variablefont/MaterialSymbolsRounded%5BFILL%2CGRAD%2Copsz%2Cwght%5D.ttf'
sha256sum /tmp/MaterialSymbolsRounded.ttf
```

Expected: `c4416e02739ed6865e3218c19dcd62c5a88fb97b8bcc445f24ae8017d11cc2d0`.

If it differs, **stop**. Do not pass a force flag, do not edit `SOURCE_SHA256`. A mismatch means the download is wrong, not the pin.

- [ ] **Step 4: Add both names and rebuild**

Append to `ICONS` in `build.py`:

```python
    "delete",
    "schedule",
```

Then:

```bash
python3 internal/render/icons/material/build.py /tmp/MaterialSymbolsRounded.ttf
sha256sum internal/render/icons/material/material-symbols-rounded.ttf
```

Extend `materialIcons` in `internal/render/materialfont.go`:

```go
	"volume_up": {}, "volume_off": {}, "brightness_high": {},
	"delete": {}, "schedule": {},
```

- [ ] **Step 5: Update `SOURCE.md`**

Three things change, all reviewed by eye: the `### Inventory` block gains `delete schedule`; the "Result:" line's byte and glyph counts; the reproduction hash at the bottom. Take every value from the Step 4 command output — do not estimate.

- [ ] **Step 6: Run and confirm it passes**

```bash
timeout 90s env GOMAXPROCS=2 go test -count=1 ./internal/render -run CentreGlyphs -v
timeout 90s env GOMAXPROCS=2 go test -count=1 ./internal/render
```

Expected: PASS, including the existing test asserting `materialIcons` matches the font's real glyph set.

- [ ] **Step 7: Commit**

```bash
git add internal/render/icons/material/ internal/render/materialfont.go internal/render/materialfont_test.go
git commit -m "feat(render): add delete and schedule to the icon subset"
```

### Task 7: `paintIcon` honours `Tone`

**Files:**
- Modify: `internal/render/paint.go:902-923` (`paintIcon`)
- Test: `internal/render/paint_test.go`

**Interfaces:**
- Produces: a `KindIcon` node with `Tone: ui.ToneAccent` paints in the accent. `ToneNormal` (the zero value) is unchanged.

- [ ] **Step 1: Write the failing test**

```go
func TestIconTonePicksTheAccent(t *testing.T) {
	style := testStyle()
	style.Foreground = Color{R: 255, G: 255, B: 255, A: 255}
	style.Accent = Color{R: 255, G: 0, B: 0, A: 255}

	paint := func(tone ui.Tone) Color {
		c := newTestCanvas(t, 32, 32)
		n := &ui.Node{Kind: ui.KindIcon, Icon: "settings", IconSize: 20, Tone: tone,
			Bounds: ui.Rect{W: 32, H: 32}}
		if err := paintIcon(c, n, testText(t), style); err != nil {
			t.Fatalf("paint: %v", err)
		}
		return brightestPixel(c)
	}

	if got := paint(ui.ToneAccent); got.R == got.G {
		t.Fatalf("accent icon painted neutral: %v", got)
	}
	if got := paint(ui.ToneNormal); got.R != got.G || got.G != got.B {
		t.Fatalf("normal icon is no longer the foreground: %v", got)
	}
}
```

`testStyle`, `newTestCanvas`, `testText` and `brightestPixel` stand in for this file's real fixtures. Read `paint_test.go` and use those rather than adding new ones.

- [ ] **Step 2: Run it and confirm it fails**

```bash
timeout 90s env GOMAXPROCS=2 go test -count=1 ./internal/render -run IconTone -v
```

Expected: FAIL — the accent icon paints neutral, because `paintIcon` always blends `style.Foreground`.

- [ ] **Step 3: Implement**

The final blend in `paintIcon` becomes:

```go
	blendMask(c, mask.Alpha, x, y, textColor(style, n.Tone))
```

The doc comment's second sentence no longer describes the code, so it changes too:

```go
// paintIcon draws one named glyph from the embedded Material subset, centred in
// the node's box. The glyph takes the colour its Tone names, resolved the same
// way text is: ToneNormal is the foreground it inherited from the chrome around
// it, and an icon still takes no fill of its own.
```

- [ ] **Step 4: Run and confirm it passes**

```bash
timeout 90s env GOMAXPROCS=2 go test -count=1 ./internal/render -run IconTone -v
timeout 90s env GOMAXPROCS=2 go test -count=1 ./internal/render
```

Expected: PASS. Every existing icon leaves `Tone` unset, so no golden image moves.

- [ ] **Step 5: Commit**

```bash
git add internal/render/paint.go internal/render/paint_test.go
git commit -m "feat(render): let an icon take its tone"
```

### Task 8: `Node.PinEnd`

**Files:**
- Modify: `internal/ui/tree.go` (the `Node` struct, after `CenterX`)
- Modify: `internal/ui/column.go:221-231` (`pinRowEnd`)
- Test: `internal/ui/column_test.go`

**Interfaces:**
- Produces: a two-child `KindRow` inside a column with `PinEnd: true` right-pins its last child regardless of the first child's kind. Rows without the flag behave exactly as today, including the existing `KindText`-first case.

- [ ] **Step 1: Write the failing test**

```go
func TestPinEndPinsPastANonTextFirstChild(t *testing.T) {
	trailing := &Node{Kind: KindButton, Width: 20, Height: 20}
	row := &Node{Kind: KindRow, PinEnd: true, Children: []*Node{
		{Kind: KindColumn, Children: []*Node{{Kind: KindText, Text: "app"}}},
		trailing,
	}}
	col := &Node{Kind: KindColumn, Children: []*Node{row}}

	layoutColumn(t, col, Rect{W: 200, H: 60})

	if got := trailing.Bounds.X + trailing.Bounds.W; got != 200 {
		t.Fatalf("right edge = %d, want 200", got)
	}
}

func TestRowWithoutPinEndIsUnchanged(t *testing.T) {
	trailing := &Node{Kind: KindButton, Width: 20, Height: 20}
	row := &Node{Kind: KindRow, Children: []*Node{
		{Kind: KindColumn, Children: []*Node{{Kind: KindText, Text: "app"}}},
		trailing,
	}}
	col := &Node{Kind: KindColumn, Children: []*Node{row}}

	layoutColumn(t, col, Rect{W: 200, H: 60})

	if got := trailing.Bounds.X + trailing.Bounds.W; got == 200 {
		t.Fatal("row pinned without opting in")
	}
}
```

`layoutColumn` stands in for this file's real layout driver. Read `column_test.go` and use it.

- [ ] **Step 2: Run and confirm the first fails**

```bash
timeout 90s env GOMAXPROCS=2 go test -count=1 ./internal/ui -run PinEnd -v
```

Expected: `TestPinEndPinsPastANonTextFirstChild` FAILs — `PinEnd` undefined.

- [ ] **Step 3: Add the field**

In `tree.go`, immediately after `CenterX`:

```go
	// PinEnd right-pins the last child of a two-child row to the row's inner
	// right edge. Without it, only a row whose first child is KindText pins:
	// that narrow case predates this flag and stays, because the callers
	// relying on it never set one.
	PinEnd bool
```

- [ ] **Step 4: Widen the guard**

`pinRowEnd`'s third condition becomes an opt-in or the legacy kind:

```go
	if first == nil || last == nil {
		return
	}
	if !n.PinEnd && first.Kind != KindText {
		return
	}
```

- [ ] **Step 5: Run and confirm both pass**

```bash
timeout 90s env GOMAXPROCS=2 go test -count=1 ./internal/ui -run "PinEnd|Row" -v
timeout 90s env GOMAXPROCS=2 go test -count=1 ./internal/ui
```

Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/ui/tree.go internal/ui/column.go internal/ui/column_test.go
git commit -m "feat(ui): opt a row into end pinning"
```

### Task 9: Concave fillet geometry

**Files:**
- Modify: `internal/render/canvas.go` (beside `roundedInset`, line 120)
- Test: `internal/render/canvas_test.go`

**Interfaces:**
- Produces: `func filletExtent(y, fillet int) int` — how far, in pixels, the bar-coloured wedge reaches outward from the body's side edge at row `y` measured from the attach edge. `filletExtent(0, f) == f`; `filletExtent(f, f) == 0`; rows outside the band return 0.

Pure geometry, proven on its own before anything paints with it.

- [ ] **Step 1: Write the failing test**

```go
func TestFilletExtentSweepsFromBarToPanel(t *testing.T) {
	const f = 8
	if got := filletExtent(0, f); got != f {
		t.Fatalf("row 0 extent = %d, want %d (flush with the bar)", got, f)
	}
	if got := filletExtent(f, f); got != 0 {
		t.Fatalf("row f extent = %d, want 0 (met the panel edge)", got)
	}
	prev := f + 1
	for y := 0; y <= f; y++ {
		got := filletExtent(y, f)
		if got > prev {
			t.Fatalf("extent grew at row %d: %d after %d", y, got, prev)
		}
		prev = got
	}
	if got := filletExtent(f+1, f); got != 0 {
		t.Fatalf("row past the band = %d, want 0", got)
	}
	if got := filletExtent(3, 0); got != 0 {
		t.Fatalf("zero fillet = %d, want 0", got)
	}
	if got := filletExtent(-1, f); got != 0 {
		t.Fatalf("negative row = %d, want 0", got)
	}
}
```

- [ ] **Step 2: Run it and confirm it fails**

```bash
timeout 90s env GOMAXPROCS=2 go test -count=1 ./internal/render -run FilletExtent -v
```

Expected: FAIL — `undefined: filletExtent`.

- [ ] **Step 3: Implement**

```go
// filletExtent is how far the bar-coloured wedge reaches outward from the panel
// body's side edge, at row y measured from the attached edge.
//
// The wedge is the region inside a circle of radius fillet centred on the bar's
// edge at the body corner, so the curve leaves the bar horizontally and meets
// the panel side vertically: the bar appears to sweep into the panel rather
// than to sit on top of it. The half-pixel term matches roundedInset, so a
// fillet and a corner quantise the same way.
func filletExtent(y, fillet int) int {
	if fillet <= 0 || y < 0 || y > fillet {
		return 0
	}
	r := float64(fillet)
	dy := float64(y) + 0.5
	return max(0, int(math.Ceil(math.Sqrt(max(0, r*r-dy*dy))-0.5)))
}
```

- [ ] **Step 4: Run and confirm it passes**

```bash
timeout 90s env GOMAXPROCS=2 go test -count=1 ./internal/render -run FilletExtent -v
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/render/canvas.go internal/render/canvas_test.go
git commit -m "feat(render): compute the bar-to-panel fillet"
```

### Task 10: Paint the fillet

**Files:**
- Modify: `internal/render/style.go` (the `Style` struct, after `AttachEdge`)
- Modify: `internal/render/canvas.go:149-171` (`clearOutsideRoundedRect`), plus a new `fillAttachFillets`
- Modify: `internal/render/paint.go:155-200` (`Paint`)
- Modify: `internal/shell/panelhost.go:617-618, 729`
- Test: `internal/render/paint_test.go`

**Interfaces:**
- Consumes: `filletExtent` (Task 9).
- Produces: `Style.Fillet int` and `Style.FilletFill Color`. A zero `Fillet` leaves every existing surface painting exactly as it does now.

The panel surface must be wider than its body for the wedge to have pixels. `style.Body` is already documented as the painted body *inside* the surface, so this uses the seam as designed rather than widening it.

- [ ] **Step 1: Write the failing test**

```go
func TestFilletPaintsBarColourOutsideTheBody(t *testing.T) {
	style := testStyle()
	style.AttachEdge = "top"
	style.Fillet = 8
	style.FilletFill = Color{R: 0, G: 0, B: 255, A: 255}
	style.Body = ui.Rect{X: 8, Y: 0, W: 100, H: 60}

	c := newTestCanvas(t, 116, 60)
	if err := Paint(c, &ui.Node{Kind: ui.KindColumn}, testText(t), style); err != nil {
		t.Fatalf("paint: %v", err)
	}

	// Row 0 sits against the bar: the wedge reaches the surface edge.
	if got := pixelAt(c, 0, 0); got.B != 255 {
		t.Fatalf("top-left corner = %v, want the fillet fill", got)
	}
	// Row 8 has met the panel edge: outside the body is transparent again.
	if got := pixelAt(c, 0, 8); got.A != 0 {
		t.Fatalf("row 8 outside the body = %v, want transparent", got)
	}
}

func TestZeroFilletLeavesTheSurfaceUnchanged(t *testing.T) {
	style := testStyle()
	style.AttachEdge = "top"
	style.Body = ui.Rect{X: 8, Y: 0, W: 100, H: 60}

	c := newTestCanvas(t, 116, 60)
	if err := Paint(c, &ui.Node{Kind: ui.KindColumn}, testText(t), style); err != nil {
		t.Fatalf("paint: %v", err)
	}
	if got := pixelAt(c, 0, 0); got.A != 0 {
		t.Fatalf("corner = %v, want transparent with no fillet", got)
	}
}
```

Reuse `paint_test.go`'s existing fixtures and pixel helper.

- [ ] **Step 2: Run and confirm it fails**

```bash
timeout 90s env GOMAXPROCS=2 go test -count=1 ./internal/render -run Fillet -v
```

Expected: FAIL — `style.Fillet undefined`.

- [ ] **Step 3: Add the Style fields and the theme that feeds them**

After `AttachEdge` in `render/style.go`:

```go
	// Fillet is the radius, in logical pixels, of the concave wedges joining
	// this surface to the bar at AttachEdge. Zero draws none.
	Fillet int
	// FilletFill paints those wedges. It is the *bar's* fill, not this
	// surface's: the two carry different alphas, and painting the wedge with
	// rootFill leaves a visible seam wherever surface opacity is below 100.
	FilletFill Color
```

`Style` is renderer-ready and never resolves itself — a surface hands one in. So the value has to come from the theme. In `internal/shell/theme.go`, beside `BarGap` in the `Theme` struct:

```go
	// Fillet is the radius of the concave wedges joining a panel to the bar.
	// It lives beside BarGap because it is the same kind of decision: how the
	// shell's surfaces meet, not a density row.
	Fillet int
```

Give it a default in the same literal that sets `BarGap` (start at `12`; it clamps per Step 5), and populate both `Style` fields where panels build theirs. `PanelStyle` is the panel's root style and the **bar's** background is what the wedge must paint, so:

```go
	st.Fillet = t.Fillet
	st.FilletFill = t.Style().rootFill()
```

Read `theme.go:510-530` first — `StyleFor`, `PanelStyle` and `OverlayStyle` are the three seams, and only the bar-attached ones should carry a non-zero `Fillet`. A toast or the OSD floats free and must keep zero, or it will paint wedges against nothing.

- [ ] **Step 4: Paint the wedges and preserve them**

Add to `canvas.go`:

```go
// fillAttachFillets paints the two concave wedges joining a panel to the bar,
// one outside each side edge of the body, tapering over the fillet band.
func fillAttachFillets(c *Canvas, r ui.Rect, fillet int, attachEdge string, col Color) {
	if fillet <= 0 || col.A == 0 || r.W <= 0 || r.H <= 0 {
		return
	}
	if attachEdge != "top" && attachEdge != "bottom" {
		return
	}
	for y := 0; y <= fillet && y < r.H; y++ {
		ext := filletExtent(y, fillet)
		if ext <= 0 {
			continue
		}
		row := r.Y + y
		if attachEdge == "bottom" {
			row = r.Y + r.H - 1 - y
		}
		fillRect(c, ui.Rect{X: r.X - ext, Y: row, W: ext, H: 1}, col)
		fillRect(c, ui.Rect{X: r.X + r.W, Y: row, W: ext, H: 1}, col)
	}
}
```

In `Paint`, after `squareAttachedEdge` and before children paint:

```go
	fillet := style.Scale120.Physical(style.Fillet)
	fillAttachFillets(c, box, fillet, style.AttachEdge, style.FilletFill)
```

`clearOutsideRoundedRect` runs last and would erase them, so it takes the band and skips it. Signature:

```go
func clearOutsideRoundedRect(c *Canvas, r ui.Rect, radius, fillet int, attachEdge string) {
```

Inside the per-row loop, before computing `x0`/`x1`:

```go
		ext := 0
		if fillet > 0 {
			ly := y - r.Y
			if attachEdge == "bottom" {
				ly = r.Y + r.H - 1 - y
			}
			ext = filletExtent(ly, fillet)
		}
```

then widen the kept span:

```go
		x0 := max(0, min(c.Width, r.X+inset-ext))
		x1 := max(x0, min(c.Width, r.X+r.W-inset+ext))
```

Update the one call site in `Paint` to pass `fillet`.

- [ ] **Step 5: Widen the panel surface**

The panel surface is currently exactly the panel rect. Give it the fillet margin and offset the body into it.

At the surface-size call (`panelhost.go:617-618`):

```go
		Width:  int32(h.place.Panel.W + 2*h.filletMargin()),
		Height: int32(h.place.Panel.H),
```

At the body rect (line 729):

```go
		body = ui.Rect{X: h.filletMargin(), W: h.place.Panel.W, H: h.place.Panel.H}
```

And the margin, clamped per design D10 — the panel is inset `Panels.Padding` from the output edge while the bar body is inset `BarGap`, so a wedge wider than the difference overruns the bar:

```go
// filletMargin is the per-side room the concave bar joint needs. It clamps to
// the gap between this panel's edge and the bar's, because a wedge wider than
// that margin paints past the bar it is meant to join.
func (h *PanelHost) filletMargin() int {
	if h == nil || h.place.BarEdge == "" {
		return 0
	}
	room := h.r.cfg.Panels.Padding - BarGap
	if room < 0 {
		return 0
	}
	return min(h.theme.Fillet, room)
}
```

Read `panelhost.go` around each site first: the surface call and the body rect appear in more than one code path, and **every path that sets one must set the other** or the panel paints offset from its surface.

- [ ] **Step 6: Run the gate**

```bash
gofmt -w . && test -z "$(gofmt -l .)"
go vet ./...
timeout 90s env GOMAXPROCS=2 go test -count=1 ./internal/render
timeout 90s env GOMAXPROCS=2 go test -count=1 ./internal/ui
PATH=/tmp/shim:$PATH timeout 90s env GOMAXPROCS=2 go test -count=1 ./internal/shell
```

Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add internal/render/style.go internal/render/canvas.go internal/render/paint.go internal/render/paint_test.go internal/shell/panelhost.go
git commit -m "feat(render): join a panel to the bar with concave fillets"
```

- [ ] **Step 8: Close the slice**

```bash
bd close sysc-198 --reason "icon subset, PinEnd, icon tone and the bar fillet landed"
bd export -o .beads/issues.jsonl
git add .beads/issues.jsonl
git commit -m "chore(beads): close shared chrome primitives"
```

---

# Slice 3 — Centre layout (bd `sysc-199`)

All `internal/shell`. Shadow `loginctl` before every test run in this slice.

### Task 11: Four filter buckets

**Files:**
- Modify: `internal/shell/notifytime.go:23-42` (`historyFilter`)
- Modify: `internal/shell/popout_notifications.go:270-278` (`historyChips`)
- Test: `internal/shell/notifytime_test.go`

**Interfaces:**
- Produces: `historyFilter` accepts exactly `all`, `today`, `yesterday`, `earlier`. `1h`, `7d` and `older` return false.

`earlier` replaces `older` and means "before yesterday", not "older than seven days" — with the 7d bucket gone there is nothing between them to fall through.

- [ ] **Step 1: Write the failing test**

```go
func TestHistoryFilterFourBuckets(t *testing.T) {
	now := time.Date(2026, 9, 7, 15, 0, 0, 0, time.Local)
	cases := []struct {
		name   string
		ts     time.Time
		bucket string
		want   bool
	}{
		{"today midday", now.Add(-2 * time.Hour), "today", true},
		{"today at local midnight", time.Date(2026, 9, 7, 0, 0, 0, 0, time.Local), "today", true},
		{"yesterday just before midnight", time.Date(2026, 9, 6, 23, 59, 59, 0, time.Local), "yesterday", true},
		{"yesterday is not today", now.AddDate(0, 0, -1), "today", false},
		{"two days back is earlier", now.AddDate(0, 0, -2), "earlier", true},
		{"yesterday is not earlier", now.AddDate(0, 0, -1), "earlier", false},
		{"ten days back is earlier", now.AddDate(0, 0, -10), "earlier", true},
		{"all takes everything", now.AddDate(0, 0, -30), "all", true},
	}
	for _, c := range cases {
		if got := historyFilter(c.bucket, c.ts, now); got != c.want {
			t.Fatalf("%s: historyFilter(%q) = %v, want %v", c.name, c.bucket, got, c.want)
		}
	}
	for _, retired := range []string{"1h", "7d", "older"} {
		if historyFilter(retired, now, now) {
			t.Fatalf("retired bucket %q still matches", retired)
		}
	}
}
```

- [ ] **Step 2: Run and confirm it fails**

```bash
PATH=/tmp/shim:$PATH timeout 90s env GOMAXPROCS=2 go test -count=1 ./internal/shell -run HistoryFilterFour -v
```

Expected: FAIL — `earlier` returns false and `1h` returns true.

- [ ] **Step 3: Implement**

```go
// historyFilter reports whether ts falls in the named bucket. The set is the
// four the centre's segmented row offers; anything else answers false, so a
// stale action string filters everything out rather than showing everything.
func historyFilter(bucket string, ts, now time.Time) bool {
	ts = ts.In(now.Location())
	switch bucket {
	case "all":
		return true
	case "today":
		return sameLocalDay(ts, now)
	case "yesterday":
		return sameLocalDay(ts, now.AddDate(0, 0, -1))
	case "earlier":
		return !sameLocalDay(ts, now) && !sameLocalDay(ts, now.AddDate(0, 0, -1)) && ts.Before(now)
	}
	return false
}
```

- [ ] **Step 4: Shrink the chip table**

`historyChips` in `popout_notifications.go`:

```go
var historyChips = []struct{ id, label string }{
	{"all", "All"},
	{"today", "Today"},
	{"yesterday", "Yesterday"},
	{"earlier", "Earlier"},
}
```

- [ ] **Step 5: Fix the existing test**

`TestHistoryFilter` in `notifytime_test.go` enumerates the old six chips and will fail. Update its `chips` slice and its cases to the new four. **Do not delete the test.**

- [ ] **Step 6: Run and confirm it passes**

```bash
PATH=/tmp/shim:$PATH timeout 90s env GOMAXPROCS=2 go test -count=1 ./internal/shell -run HistoryFilter -v
```

Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add internal/shell/notifytime.go internal/shell/popout_notifications.go internal/shell/notifytime_test.go
git commit -m "feat(notify): reduce the centre to four time buckets"
```

### Task 12: Bucket counts

**Files:**
- Modify: `internal/shell/popout_notifications.go` (new function beside `historyChips`)
- Test: `internal/shell/popout_notifications_test.go`

**Interfaces:**
- Produces: `func bucketCount(bucket string, active []protocol.Notification, history []protocol.HistoryEntry, now time.Time) int`.

Active entries count too: the merged list shows them, so a segment reading `Today (2)` above three visible rows is a defect.

- [ ] **Step 1: Write the failing test**

```go
func TestBucketCountSpansActiveAndHistory(t *testing.T) {
	now := time.Date(2026, 9, 7, 15, 0, 0, 0, time.Local)
	active := []protocol.Notification{{ID: 1, Timestamp: now.Add(-time.Minute)}}
	history := []protocol.HistoryEntry{
		{ID: 2, Timestamp: now.Add(-3 * time.Hour)},
		{ID: 3, Timestamp: now.AddDate(0, 0, -1)},
		{ID: 4, Timestamp: now.AddDate(0, 0, -5)},
	}
	for bucket, want := range map[string]int{"all": 4, "today": 2, "yesterday": 1, "earlier": 1} {
		if got := bucketCount(bucket, active, history, now); got != want {
			t.Fatalf("bucketCount(%q) = %d, want %d", bucket, got, want)
		}
	}
}
```

- [ ] **Step 2: Run and confirm it fails**

```bash
PATH=/tmp/shim:$PATH timeout 90s env GOMAXPROCS=2 go test -count=1 ./internal/shell -run BucketCount -v
```

Expected: FAIL — `undefined: bucketCount`.

- [ ] **Step 3: Implement**

```go
// bucketCount is how many entries one filter segment would show. Active
// notifications are counted with the closed ones because the merged list shows
// them together: a segment whose number disagrees with its list is a defect.
func bucketCount(bucket string, active []protocol.Notification, history []protocol.HistoryEntry, now time.Time) int {
	n := 0
	for _, a := range active {
		if historyFilter(bucket, a.Timestamp, now) {
			n++
		}
	}
	for _, e := range history {
		if historyFilter(bucket, e.Timestamp, now) {
			n++
		}
	}
	return n
}
```

- [ ] **Step 4: Run and confirm it passes**

```bash
PATH=/tmp/shim:$PATH timeout 90s env GOMAXPROCS=2 go test -count=1 ./internal/shell -run BucketCount -v
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/shell/popout_notifications.go internal/shell/popout_notifications_test.go
git commit -m "feat(notify): count entries per filter bucket"
```

### Task 13: Header

**Files:**
- Modify: `internal/shell/popout_notifications.go:160-190`
- Modify: `internal/shell/panelhost.go:1388-1436` (`activateNotify`), and the `PanelHost` struct (`notifyTab` removal)
- Test: `internal/shell/popout_notifications_test.go`

**Interfaces:**
- Consumes: `delete`/`schedule` glyphs (Task 6), icon `Tone` (Task 7), `Node.PinEnd` (Task 8).
- Produces: actions `notify:center:dnd`, `:schedule`, `:clear`, `:settings`, `:close`. Retires `:dismiss-all`, `:clear-history`, `:tab:0`, `:tab:1`.

- [ ] **Step 1: Write the failing test**

```go
func TestCentreHeaderCarriesFiveCircularButtons(t *testing.T) {
	r := newTestRegistry(t)
	tree := r.centerTreeFor(nil)

	for _, action := range []string{
		"notify:center:dnd", "notify:center:schedule", "notify:center:clear",
		"notify:center:settings", "notify:center:close",
	} {
		n := findAction(t, tree, action)
		if n.Shape != ui.ShapeCircle {
			t.Fatalf("%s shape = %v, want circle", action, n.Shape)
		}
		if n.Fill != ui.FillContainerHighest {
			t.Fatalf("%s fill = %v, want ContainerHighest", action, n.Fill)
		}
		if n.Name == "" || n.Role != "button" {
			t.Fatalf("%s is not addressable: name=%q role=%q", action, n.Name, n.Role)
		}
	}
	for _, gone := range []string{"notify:center:tab:0", "notify:center:tab:1", "notify:center:clear-history"} {
		if hasAction(tree, gone) {
			t.Fatalf("retired action %s is still in the tree", gone)
		}
	}
}
```

`newTestRegistry`, `findAction` and `hasAction` stand in for this file's real fixtures. Read it first.

- [ ] **Step 2: Run and confirm it fails**

```bash
PATH=/tmp/shim:$PATH timeout 90s env GOMAXPROCS=2 go test -count=1 ./internal/shell -run CentreHeader -v
```

Expected: FAIL — the actions do not exist.

- [ ] **Step 3: Build the header**

Add beside the other card constants:

```go
	centreIconSize = 20
	centreIconPad  = 6
```

Replace `headerBtns` and the tab row with:

```go
// centreIconButton is one circular control in the centre's header. The glyph
// carries no fill of its own; the button around it resolves one.
func centreIconButton(icon, action, name string) *ui.Node {
	return &ui.Node{
		Kind: ui.KindButton, Action: action, Name: name, Role: "button",
		Focusable: true, Shape: ui.ShapeCircle, Fill: ui.FillContainerHighest,
		Padding:  centreIconPad,
		Children: []*ui.Node{{Kind: ui.KindIcon, Icon: icon, IconSize: centreIconSize}},
	}
}

func centreHeaderRow(dnd bool) *ui.Node {
	title := &ui.Node{Kind: ui.KindRow, Gap: cardGap, Children: []*ui.Node{
		{Kind: ui.KindIcon, Icon: "notifications", IconSize: centreIconSize, Tone: ui.ToneAccent},
		{Kind: ui.KindText, Text: "Notifications", TextRole: theme.RoleHeadline},
	}}
	dndIcon := "notifications"
	if dnd {
		dndIcon = "do_not_disturb_on"
	}
	controls := &ui.Node{Kind: ui.KindRow, Gap: cardGap, Children: []*ui.Node{
		centreIconButton(dndIcon, "notify:center:dnd", "Do not disturb"),
		centreIconButton("schedule", "notify:center:schedule", "Schedule"),
		centreIconButton("delete", "notify:center:clear", "Clear"),
		centreIconButton("settings", "notify:center:settings", "Settings"),
		centreIconButton("close", "notify:center:close", "Close"),
	}}
	return &ui.Node{Kind: ui.KindRow, Gap: cardGap, PinEnd: true,
		Children: []*ui.Node{title, controls}}
}
```

Delete the `sched, _ := render.IconByName("schedule")` line and the `clearAction` tab switch; both are unreachable now. Drop the `render` import if nothing else in the file uses it.

- [ ] **Step 4: Rewire dispatch**

In `activateNotify`, replace the `dismiss-all`, `clear-history` and `tab:` cases:

```go
		case rest == "clear":
			r.clearVisible(h)
		case rest == "settings":
			r.togglePanel(PanelSettings, h.output, h.trigger)
		case rest == "close":
			r.closePanel(h)
```

`togglePanel` and `closePanel` are placeholders for whatever `panelhost.go` already exposes — read it and reuse those; the settings and close paths must not open-code a second way to move panels.

`clearVisible` arrives in Task 16. Until then, have it send `protocol.CommandHistoryClear` so the tree compiles and the button does something honest.

Delete `notifyTab` from the `PanelHost` struct, and the `strconv` import if it becomes unused.

- [ ] **Step 5: Run and confirm it passes**

```bash
PATH=/tmp/shim:$PATH timeout 90s env GOMAXPROCS=2 go test -count=1 ./internal/shell -run CentreHeader -v
```

Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/shell/popout_notifications.go internal/shell/panelhost.go internal/shell/popout_notifications_test.go
git commit -m "feat(notify): give the centre a headline header"
```

### Task 14: Segmented filter row

**Files:**
- Modify: `internal/shell/popout_notifications.go:280-296` (replacing `historyChipRow`)
- Test: `internal/shell/popout_notifications_test.go`

**Interfaces:**
- Consumes: `bucketCount` (Task 12).
- Produces: `func centreFilterRow(active []protocol.Notification, history []protocol.HistoryEntry, filter string, now time.Time, width int) *ui.Node`.

- [ ] **Step 1: Write the failing test**

```go
func TestFilterRowIsOneSegmentedControl(t *testing.T) {
	now := time.Date(2026, 9, 7, 15, 0, 0, 0, time.Local)
	history := []protocol.HistoryEntry{{ID: 1, Timestamp: now.Add(-time.Hour)}}

	row := centreFilterRow(nil, history, "today", now, 392)
	if row.Kind != ui.KindSegmented {
		t.Fatalf("kind = %v, want segmented", row.Kind)
	}
	if len(row.Children) != 4 {
		t.Fatalf("segments = %d, want 4", len(row.Children))
	}
	if !row.Children[1].State.Has(ui.StateSelected) {
		t.Fatal("Today is not marked selected")
	}
	if row.Children[0].State.Has(ui.StateSelected) {
		t.Fatal("All is selected while the filter is today")
	}
	if got, want := row.Children[1].Children[0].Text, "Today (1)"; got != want {
		t.Fatalf("label = %q, want %q", got, want)
	}
}
```

- [ ] **Step 2: Run and confirm it fails**

```bash
PATH=/tmp/shim:$PATH timeout 90s env GOMAXPROCS=2 go test -count=1 ./internal/shell -run FilterRow -v
```

Expected: FAIL — `undefined: centreFilterRow`.

- [ ] **Step 3: Implement**

```go
// centreFilterRow is the one control selecting what the list shows. It replaced
// a tab row plus a six-chip row: two controls for one question.
func centreFilterRow(active []protocol.Notification, history []protocol.HistoryEntry, filter string, now time.Time, width int) *ui.Node {
	segments := make([]*ui.Node, 0, len(historyChips))
	for _, c := range historyChips {
		seg := &ui.Node{
			Kind: ui.KindButton, Action: "notify:center:filter:" + c.id,
			Name: c.label, Role: "tab", Focusable: true, Padding: 4,
			Children: []*ui.Node{{Kind: ui.KindText,
				Text: fmt.Sprintf("%s (%d)", c.label, bucketCount(c.id, active, history, now))}},
		}
		if filter == c.id {
			seg.State |= ui.StateSelected
		}
		segments = append(segments, seg)
	}
	return &ui.Node{Kind: ui.KindSegmented, Key: "notify-filter", Gap: 2,
		Width: width, Children: segments}
}
```

Delete `historyChipRow` and its `showOlder` probe: the `older` bucket it guarded no longer exists.

- [ ] **Step 4: Run and confirm it passes**

```bash
PATH=/tmp/shim:$PATH timeout 90s env GOMAXPROCS=2 go test -count=1 ./internal/shell -run FilterRow -v
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/shell/popout_notifications.go internal/shell/popout_notifications_test.go
git commit -m "feat(notify): select the centre list with one segmented row"
```

### Task 15: Merged list and the surface ladder

**Files:**
- Modify: `internal/shell/popout_notifications.go:128-232` (`centerTreeFor`)
- Modify: `internal/shell/notifycard.go:78-96` (`wrapNotifyCard`)
- Test: `internal/shell/popout_notifications_test.go`

**Interfaces:**
- Consumes: `centreHeaderRow` (Task 13), `centreFilterRow` (Task 14).
- Produces: the centre tree — header block at `FillContainerHigh`, then a scroll whose children are section labels and cards.

- [ ] **Step 1: Write the failing test**

```go
func TestMergedListPinsLiveAboveClosed(t *testing.T) {
	now := time.Date(2026, 9, 7, 15, 0, 0, 0, time.Local)
	r := newTestRegistry(t)
	r.now = now
	seedActive(t, r, protocol.Notification{ID: 1, AppName: "mail", Timestamp: now.Add(-time.Minute)})
	seedHistory(t, r, protocol.HistoryEntry{ID: 2, AppName: "mail", Timestamp: now.Add(-2 * time.Hour)})

	if got := sectionLabels(t, r.centerTreeFor(nil)); !slices.Equal(got, []string{"LIVE", "EARLIER"}) {
		t.Fatalf("labels = %v, want [LIVE EARLIER]", got)
	}
}

func TestEmptySectionEmitsNoLabel(t *testing.T) {
	now := time.Date(2026, 9, 7, 15, 0, 0, 0, time.Local)
	r := newTestRegistry(t)
	r.now = now
	seedHistory(t, r, protocol.HistoryEntry{ID: 2, Timestamp: now.Add(-2 * time.Hour)})

	if got := sectionLabels(t, r.centerTreeFor(nil)); !slices.Equal(got, []string{"EARLIER"}) {
		t.Fatalf("labels = %v, want [EARLIER] only", got)
	}
}

func TestCardsSitOnTheHighContainer(t *testing.T) {
	r := newTestRegistry(t)
	seedHistory(t, r, protocol.HistoryEntry{ID: 2, Timestamp: r.clockNow()})

	if card := firstCapsule(t, r.centerTreeFor(nil)); card.Fill != ui.FillContainerHigh {
		t.Fatalf("card fill = %v, want ContainerHigh", card.Fill)
	}
}
```

`sectionLabels`, `firstCapsule`, `seedActive` and `seedHistory` stand in for this file's real helpers. Read it and use those, adding only what genuinely does not exist.

- [ ] **Step 2: Run and confirm it fails**

```bash
PATH=/tmp/shim:$PATH timeout 90s env GOMAXPROCS=2 go test -count=1 ./internal/shell -run "MergedList|EmptySection|HighContainer" -v
```

Expected: FAIL — no section labels exist and cards are `FillNone`.

- [ ] **Step 3: Rebuild the tree**

```go
func centreSectionLabel(text string) *ui.Node {
	return &ui.Node{Kind: ui.KindText, Text: text,
		TextRole: theme.RoleCaption, Tone: ui.ToneAccent}
}
```

In `centerTreeFor`, replace the header/menu/tab/chip assembly with a header block, and the two body branches with one merged pass:

```go
	header := &ui.Node{
		Kind: ui.KindCapsule, Fill: ui.FillContainerHigh, Shape: ui.ShapeLarge,
		Padding: cardPadding, Children: []*ui.Node{
			{Kind: ui.KindColumn, Gap: cardGap, Children: []*ui.Node{
				centreHeaderRow(dnd),
				centreFilterRow(active, history, filter, now, innerW),
			}},
		},
	}
	children := []*ui.Node{header}
	if showMenu {
		children = append(children, dndPresetColumn())
	}

	sort.Slice(history, func(i, j int) bool { return history[i].Timestamp.After(history[j].Timestamp) })

	var live, closed []*ui.Node
	for _, g := range activeGroups(active) {
		if !historyFilter(filter, g.members[0].Timestamp, now) {
			continue
		}
		raster := r.lookupNotifyIcon(g.members[0].AppIcon)
		live = append(live, ActiveGroupCard(g, now, expand == g.key, raster, r.linksAllowed()))
	}
	for _, e := range history {
		if !historyFilter(filter, e.Timestamp, now) {
			continue
		}
		closed = append(closed, HistoryCard(e, now, r.lookupNotifyIcon(e.AppIcon), r.linksAllowed()))
	}

	body := []*ui.Node{}
	if len(live) > 0 {
		body = append(body, centreSectionLabel("LIVE"))
		body = append(body, live...)
	}
	if len(closed) > 0 {
		body = append(body, centreSectionLabel("EARLIER"))
		body = append(body, closed...)
	}
	if len(body) == 0 {
		body = append(body, &ui.Node{Kind: ui.KindText, Text: "Nothing to see here"})
	}
```

`innerW` is the panel width less twice `cardPadding`; derive it from the same `h`-or-default source `surfaceH` already uses, and keep the existing `listH` reservation.

- [ ] **Step 4: Lift the cards onto the ladder**

In `wrapNotifyCard`, the capsule's fill changes:

```go
	cap := &ui.Node{
		Kind: ui.KindCapsule, Fill: ui.FillContainerHigh, Padding: cardPadding,
		Shape: ui.ShapeCard, Action: inner.Action, Children: []*ui.Node{body},
	}
```

Nothing else in that function changes; the critical stroke and left chip stay.

- [ ] **Step 5: Run and confirm it passes**

```bash
PATH=/tmp/shim:$PATH timeout 90s env GOMAXPROCS=2 go test -count=1 ./internal/shell -run "MergedList|EmptySection|HighContainer" -v
```

Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/shell/popout_notifications.go internal/shell/notifycard.go internal/shell/popout_notifications_test.go
git commit -m "feat(notify): merge the centre list and stratify its surfaces"
```

### Task 16: Per-card remove and the visible-set clear

**Files:**
- Modify: `internal/shell/notifycard.go` (`HistoryCard`, `ActiveGroupCard`)
- Modify: `internal/shell/panelhost.go` (`activateNotify`, and `clearVisible` from Task 13)
- Test: `internal/shell/notifycard_test.go`, `internal/shell/popout_notifications_test.go`

**Interfaces:**
- Consumes: `historyRemoveSupported()` (Task 5), `Node.PinEnd` (Task 8), `delete` glyph (Task 6).
- Produces: `notify:<id>:dismiss` on a live card, `notify:<id>:remove` on a closed one, and `(*Registry).clearVisible(*PanelHost)`.

- [ ] **Step 1: Write the failing test**

```go
func TestHistoryCardCarriesARemoveControl(t *testing.T) {
	now := time.Now()
	card := HistoryCard(protocol.HistoryEntry{ID: 9, AppName: "mail", Summary: "s", Timestamp: now}, now, nil, false)

	n := findAction(t, card, "notify:9:remove")
	if n.Shape != ui.ShapeCircle {
		t.Fatalf("remove shape = %v, want circle", n.Shape)
	}
	if n.Name == "" || n.Role != "button" {
		t.Fatalf("remove is not addressable: name=%q role=%q", n.Name, n.Role)
	}
}

func TestActiveCardRemoveDismisses(t *testing.T) {
	now := time.Now()
	g := activeGroup{key: "mail", members: []protocol.Notification{
		{ID: 4, AppName: "mail", Summary: "s", Timestamp: now},
	}}
	card := ActiveGroupCard(g, now, false, nil, false)

	if !hasAction(card, "notify:4:dismiss") {
		t.Fatal("live card has no remove control")
	}
}
```

- [ ] **Step 2: Run and confirm it fails**

```bash
PATH=/tmp/shim:$PATH timeout 90s env GOMAXPROCS=2 go test -count=1 ./internal/shell -run "RemoveControl|RemoveDismisses" -v
```

Expected: FAIL — no remove action exists.

- [ ] **Step 3: Add the control**

```go
// centreRemoveButton is the per-entry remove control. HistoryCard omits it
// when the pinned service has no command behind it: a painted control the
// service rejects is worse than an absent one.
func centreRemoveButton(action, name string) *ui.Node {
	return &ui.Node{
		Kind: ui.KindButton, Action: action, Name: name, Role: "button",
		Focusable: true, Shape: ui.ShapeCircle, Fill: ui.FillContainerHighest,
		Padding:  centreIconPad,
		Children: []*ui.Node{{Kind: ui.KindIcon, Icon: "delete", IconSize: centreIconSize}},
	}
}
```

In `HistoryCard`, wrap the inner tree so the button pins right — this is what `Node.PinEnd` was added for:

```go
	inner := notificationTree(e.ID, e.AppName, e.Summary, e.Body, e.Urgency, raster, nil, allowLinks, now, e.Timestamp)
	if historyRemoveSupported() {
		inner = &ui.Node{Kind: ui.KindRow, Gap: cardGap, PinEnd: true, Children: []*ui.Node{
			inner,
			centreRemoveButton(fmt.Sprintf("notify:%d:remove", e.ID), "Remove"),
		}}
	}
	return cardColumn(wrapNotifyCard(inner, e.Urgency == protocol.UrgencyCritical))
```

In `ActiveGroupCard`, wrap `head` the same way with `fmt.Sprintf("notify:%d:dismiss", latest.ID)` and the name `"Dismiss"`. A live card needs no capability guard — `notification.dismiss` has always existed.

`notificationTree` itself is **not** touched: per design D7 the card's internals stay as shipped.

- [ ] **Step 4: Dispatch remove and the visible-set clear**

In `activateNotify`'s card-action switch, beside `case "dismiss":`:

```go
	case "remove":
		r.sendNotify(protocol.Command{Kind: protocol.CommandHistoryRemove, IDs: []uint32{id}})
```

Replace the Task 13 placeholder:

```go
// clearVisible clears exactly what the open filter shows. With the tabs gone
// there is no other unambiguous target: a Clear that emptied the whole store
// while the user was looking at Yesterday would delete what they cannot see.
func (r *Registry) clearVisible(h *PanelHost) {
	now := r.clockNow()
	filter := "all"
	if h != nil && h.notifyFilter != "" {
		filter = h.notifyFilter
	}

	r.notify.mu.Lock()
	var dismiss, remove []uint32
	for _, n := range r.notify.active {
		if historyFilter(filter, n.Timestamp, now) {
			dismiss = append(dismiss, n.ID)
		}
	}
	for _, e := range r.notify.history {
		if historyFilter(filter, e.Timestamp, now) {
			remove = append(remove, e.ID)
		}
	}
	r.notify.mu.Unlock()

	for _, id := range dismiss {
		r.sendNotify(protocol.Command{Kind: protocol.CommandDismiss, ID: id})
	}
	if len(remove) > 0 {
		r.sendNotify(protocol.Command{Kind: protocol.CommandHistoryRemove, IDs: remove})
	}
}
```

Check whether `sendNotify` takes `Registry.mu`. If it does, the sends must stay outside the `notify.mu` critical section exactly as written, and `AGENTS.md`'s panel-lock note applies.

- [ ] **Step 5: Test the clear target**

```go
func TestClearVisibleSpansOnlyTheOpenFilter(t *testing.T) {
	now := time.Date(2026, 9, 7, 15, 0, 0, 0, time.Local)
	r, sent := newTestRegistryRecordingCommands(t)
	r.now = now
	seedHistory(t, r,
		protocol.HistoryEntry{ID: 1, Timestamp: now.Add(-time.Hour)},
		protocol.HistoryEntry{ID: 2, Timestamp: now.AddDate(0, 0, -5)},
	)

	r.clearVisible(&PanelHost{notifyFilter: "today"})

	for _, c := range *sent {
		if c.Kind == protocol.CommandHistoryRemove && slices.Contains(c.IDs, uint32(2)) {
			t.Fatal("cleared an entry outside the open filter")
		}
	}
}
```

`newTestRegistryRecordingCommands` stands in for whatever command-capturing fixture the package already has; if none exists, add the smallest one that records `sendNotify` calls.

- [ ] **Step 6: Run the gate**

```bash
gofmt -w . && test -z "$(gofmt -l .)"
go vet ./...
timeout 90s env GOMAXPROCS=2 go test -count=1 ./internal/render
timeout 90s env GOMAXPROCS=2 go test -count=1 ./internal/ui
PATH=/tmp/shim:$PATH timeout 90s env GOMAXPROCS=2 go test -count=1 ./internal/shell
git diff --exit-code -- go.mod go.sum
```

Expected: all pass.

- [ ] **Step 7: Commit**

```bash
git add internal/shell/notifycard.go internal/shell/panelhost.go internal/shell/notifycard_test.go internal/shell/popout_notifications_test.go
git commit -m "feat(notify): remove one entry and clear what is on screen"
```

### Task 17: Live Niri gate and close-out

**Files:**
- Modify: `.beads/issues.jsonl`

- [ ] **Step 1: Build against the live compositor**

```bash
export NIRI_SOCKET=$(ls /run/user/1000/niri.wayland-*.sock | head -1)
export WAYLAND_DISPLAY=wayland-1
export XDG_RUNTIME_DIR=/run/user/1000
go build -o /tmp/claude-1000/scratch/sysc-shell ./cmd/sysc-shell
```

`sysc-shell` runs from systemd on this machine. Redeploy is `mv` plus `systemctl restart` — do not spawn a second copy beside the running one. **Never `pkill -f sysc-shell`**: that pattern matches the shell the command is typed in. Kill by pid from `pgrep -f 'scratch/sysc-shell'`.

- [ ] **Step 2: Assert the surface**

```bash
niri msg -j layers
```

Open the centre from the bar bell and check against the reference: header block separated from the plate, cards separated from the header, one filter row carrying counts, LIVE above EARLIER, a remove control on every card, and the fillets joining panel to bar with no seam.

A handler panic presents as a **blank shell** — the service reads active with nothing painted, because `sysc-wayland` recovers panics into an error. If the panel opens empty, read the journal before assuming a layout bug.

- [ ] **Step 3: Close the slices**

```bash
bd close sysc-199 --reason "centre header, filter row, merged list, ladder and per-entry removal landed; live Niri gate passed"
bd close sysc-197 --reason "design executed across sysc-153, sysc-198, sysc-199"
bd export -o .beads/issues.jsonl
```

Verify the export did not truncate: `wc -l .beads/issues.jsonl` and `git diff` before committing. If it shrank:

```bash
sqlite3 .beads/beads.db "DELETE FROM export_hashes;" && bd export -o .beads/issues.jsonl
```

- [ ] **Step 4: Commit**

```bash
git add .beads/issues.jsonl
git commit -m "chore(beads): close the notification centre polish slices"
```
