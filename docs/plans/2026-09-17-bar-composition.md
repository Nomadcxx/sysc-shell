# Bar composition implementation plan

Date: 2026-09-17. Issue: `sysc-323`. Sub-project B of Milestone 9.

Executes [`2026-09-15-bar-composition-design.md`](2026-09-15-bar-composition-design.md).
Its decisions D1 to D9 are settled and are not reopened here. This document is
the execution order, the exact anchors, and the gate.

## Where this builds

**On `milestone/m9-settings-foundation`, not on `main`.** Sub-project A is
seventeen commits on that branch plus the polish commits that follow it, and it
is unmerged. B depends on A at the interface level, not merely in the tracker:
the inspector is built from entries synthesised at runtime over one specific
`config.Item`, which is exactly what A's typed accessors made expressible and
what the retired `Get`/`Set` switch pair could not do.

Branch B from A's tip rather than from `main`. If A is merged to `main` first,
rebase B onto the merge rather than starting from `main` directly.

## Anchors, verified 2026-09-17 against A's tip

Every one of these was grepped by name on the branch this plan builds on, not
read from the design. Re-verify by name before relying on any of them; do not
trust the line numbers.

| What | Where | Note |
|---|---|---|
| `resolveItem` | `internal/config/load.go:824` | The instance rejection is a loop over three misplaced fields at `:831`, `{"plugin", …}, {"entry", …}, {"instance", …}` |
| The rejection message | `internal/config/load.go:837` | `"is accepted only on a plugin placement, not on %q"` |
| One-level cap | `internal/config/load.go:853` | Already refuses a group inside a group |
| Empty group refusal | `internal/config/load.go:846` | Already refuses `len(items) == 0` |
| `flattenItems` | `internal/config/load.go:657` | Appends `item.Items...` and drops the wrapper |
| `consistentInstances` | `internal/config/load.go:416` | |
| `applyBar` | `internal/config/load.go:669` | |
| `knownItems` | `internal/config/config.go:254` | 24 entries: 22 widgets plus `group` and `plugin` |
| `eachItem` | `internal/settings/registry.go:1095` | The every-instance write D4 fixes |
| `bar.items.*` entries | `internal/settings/registry.go:81`, `:88`, `:95` | Retired by D9 |
| `TestRegistryExposesBarItemLists` | `internal/settings/registry_test.go:94` | Retired by D9 |
| `connectorsLocked` | `internal/shell/popout_wallpaper.go:1167` | |
| Drag API | `internal/ui/drag.go` | `Begin:22 Move:29 Cancel:39 Active:46 Accepts:48 Hits:63 Drop:76 FindDropZone:85` |
| Outer-zone-wins walk | `internal/ui/drag.go:95` | Tests the node before descending, exactly as D-drop-model states |
| Node drag fields | `internal/ui/tree.go:259` | `DragType`, `Payload`, `Accept` |
| `KindDragSource`, `KindDropZone` | `internal/ui/tree.go:35`, `:36` | |

**Two design statements corrected by this pass.** The design says groups have
"no id and no options of its own" — true — but it does not record that the
loader *already* refuses both an empty group and a nested group. D5's two
invariants therefore have loader backing today; the editor must not be allowed
to construct what the loader would refuse, and the tests should assert the
editor refuses at the drop rather than relying on the load-time error.

## Glyphs: confirmed against the pinned subset

The design lists six unconfirmed ligature names. Checked against
`materialIcons` in `internal/render/materialfont.go`, which is the exact
inventory the subset carries:

- **Present:** `add`, `close`, `delete`, `settings`, `chevron_left`,
  `chevron_right`, `folder_open`.
- **Absent:** `drag_indicator`, `tune`, `more_vert`, `edit`, `call_split`,
  `link_off`, `workspaces`.

A name the subset lacks shapes to nothing and paints an invisible control, so
Task 0 settles this before any chrome is written. The subset can be re-cut:
`internal/render/icons/material/build.py` verifies the pinned upstream's
SHA-256 before reading it, the upstream is reachable, and `fontTools` is
installed. Adding a glyph means the builder, `materialIcons` in
`materialfont.go`, and `materialInventory` in its test, which drives
rasterisation coverage.

## Task order

Fourteen tasks. Each is test-first: write the assertion, watch it fail for the
stated reason, make it pass. A task that cannot be made to fail first is a task
whose test proves nothing — say so rather than proceeding.

Tasks 1 to 6 are `internal/config` and need no host. Tasks 7 to 13 are
`internal/shell`. Task 0 and Task 14 bracket them.

### Task 0: Settle the glyphs

Decide, for each control the chrome needs — chip grip, chip options, group
dissolve, add widget, remove widget, move left, move right — either a name the
subset already carries or a name to cut into it. Cut the ones chosen, in one
commit that touches the builder, `materialIcons`, and `materialInventory`.

Gate: `timeout 90s env GOMAXPROCS=2 go test -count=1 ./internal/render -run Material`.

### Task 1: Accept `instance` on a built-in item

Narrow the misplaced-field loop in `resolveItem` to `plugin` and `entry`.

Test first: a document with `instance` on a `clock` currently fails to load
with the plugin-placement message; assert it loads and the instance survives.
Assert `plugin` and `entry` on a non-plugin item are still refused, with the
same message — the loop keeps two members and must not stop rejecting them.

Record in the commit that this is a schema change under `DisallowUnknownFields`
in the direction D2 already accepted: a document written by a shell carrying
this change will not load on one without it.

### Task 2: Accept an id on a group

A group takes an instance id like any other item. Assert a group with an id
loads, a group without one still loads, and the one-level cap and the
empty-group refusal are both unchanged.

### Task 3: `flattenItems` stops hiding the wrapper

Emit the group itself as well as its members.

The design names the risk precisely: `requireWeatherWhenUsed` is the other
caller and is indifferent to the wrapper. Test first that a `weather` widget
nested inside a group is still found, then make the change, then assert
`consistentInstances` can now see a group id.

### Task 4: `consistentInstances` covers groups

Duplicate group ids are refused. Unique built-in ids pass. The case that
function's own comment records — the same id on two outputs is one placement
and stays legal — is asserted rather than assumed, because Task 3 widens what
the function sees and that case is the one most likely to break.

### Task 5: Lane mutations as pure functions

`Move`, `Insert`, `Remove`, `Group`, `Ungroup` over `[]Item` in
`internal/config`, returning a new slice or an error. D1.

Table tests, no `PanelHost`: move within a lane, move across lanes, insert,
remove, group creation, ungroup, a refused group-inside-group, a pruned empty
group, and a pruned group whose placement goes with it.

These sit beside `resolveItem` because they must agree with it. The test that
proves they do: every mutation result is put through `config.Write` and
`config.Load`. This is the round-trip pattern that found three live defects in
A within a minute of being written, and B changes the item lists themselves, so
it needs it more than A did.

### Task 6: Ids minted lazily

An id appears only when something addresses the widget — a reorder, a grouping,
or an option write. D3.

Assert a configuration that is loaded and written back unchanged grows no ids,
and that each mutation mints one only for the items it actually addresses.

### Task 7: Per-instance option writes

Fix `eachItem`: an option write addresses one instance, not every widget of
that type. D4.

This is a live defect independent of the editor — a user with a time widget and
a date widget cannot give them different formats today — so it carries its own
regression test naming that case.

### Task 8: Display names for the vocabulary

The 22 widget ids are configuration tokens and several read poorly as labels.
Write the map, one label per id, and assert every id in `knownItems` except
`group` and `plugin` has one. The assertion is the point: a widget added later
without a label would otherwise render as a raw token.

### Task 9: Drop target resolution as a pure function

Chip bounds plus drop point in, insertion index or join target out. No pointer
events. Per the drop model: a drop on a chip's inner half joins or creates a
group, a drop between chips inserts at that index.

Each lane has exactly one `KindDropZone` and groups are not zones, because
`FindDropZone` tests a node before descending and an outer zone would always
win. Assert that: a drop over a group resolves through the lane's zone.

### Task 10: The lane strip renders

Three lanes, chips from the draft, per the chrome in the design. Card chrome on
lanes is the one named exception to the foundation's no-cards rule, because a
drop target has to be a visible surface.

Lane height derives from `StandardControl` plus twice `CardPadding`, never a
literal. Every column carries a width. **A's clipping defect was a node with no
width inside a row that right-pins**, and B composes chips and lanes into the
same pane, so this is where that lesson is spent.

Chips carry accessible names and roles.

### Task 11: Keyboard parity

`Alt`+Left/Right within a lane, `Alt`+Up/Down between lanes, explicit group and
ungroup commands. D6.

Every command calls Task 5's functions, so the test needs no synthesised
pointer events, and pointer and keyboard rules cannot drift. Assert a group
command and the equivalent drag produce the same draft.

### Task 12: The inspector

Opens beneath the strip for the selected chip, built from entries synthesised
at runtime for that `Item`. This is the task A exists for.

Option rows follow the foundation's row anatomy unchanged — label over caption,
no cards.

### Task 13: Per-output lanes

Output selector above the strip, reusing `connectorsLocked()`. `Shared` edits
`cfg.Bar`; a connector edits that `OutputOverride.Bar`. D7.

The granularity is the loader's, not a choice: `applyBar` inherits or replaces
a lane whole. So each lane reads as inherited or overridden and carries a reset
to shared, and the test asserts that reordering one widget on one output forks
that whole lane and that later shared changes stop reaching it.

**Two-output behaviour cannot be verified on either machine in reach.** Both
have exactly one output. Assert what is assertable in tests and say plainly
that live two-output behaviour is unexercised. Do not claim it works.

### Task 14: Retire the string entries

Remove `bar.items.left`, `bar.items.center`, `bar.items.right` and
`TestRegistryExposesBarItemLists`. D9.

Last, not first: until Task 13 lands, those three strings are the only way to
reach lane arrangement at all, and removing them earlier leaves the tree with
no path to a bar layout.

## Gate

The repository-wide race gate in `AGENTS.md` is unrunnable here — a full `-race`
build hard-locks this workstation on zram-only swap, and
`.cursor/hooks/deny-go-race.py` refuses both `-race` and `./...` outright. The
substitute, one package per invocation:

```bash
timeout 90s env GOMAXPROCS=2 go test -count=1 <pkg> -run <Name>
```

Required at every commit: `gofmt -w . && test -z "$(gofmt -l .)"` and
`go vet ./...`. A whole-tree vet is safe with `-p 2` and `GOMAXPROCS=2`.

Packages this work touches, each run on its own: `internal/config`,
`internal/settings`, `internal/shell`, `internal/render`, `internal/ui`.

## Live gate

The agent shell inherits none of the compositor environment:

```bash
export NIRI_SOCKET=$(ls /run/user/1000/niri.wayland-*.sock | head -1)
export WAYLAND_DISPLAY=wayland-1
export XDG_RUNTIME_DIR=/run/user/1000
```

The shell has no argument parsing — `--help` starts it rather than printing
usage, and there is no client mode. Drive a running instance over its IPC
socket at `$XDG_RUNTIME_DIR/sysc-shell/ipc.v1.sock` with a newline-terminated
JSON object:

```json
{"id":1,"method":"panel.open","params":{"panel":"settings","section":"Bar"}}
```

**Check it on the laptop.** `ssh -p 7777 nomadx@192.168.0.64`, which is `eDP-1`
at 1536x864 logical, scale 1.25 — the only machine in reach that is not
3440x1440 at scale 1.0. It caught a clipping defect that every test and the
first live check missed. A strip that looks right on the workstation is not
evidence. `rsync` is not installed there; use
`tar czf - … | ssh … tar xzf -`.

## Not verified, carried forward

- Whether a plugin-supplied widget entry needs different chip treatment from a
  built-in one. The placement model is the same; the label source is not.
- The interaction between a lane override and a plugin placement's instance id,
  which has not been walked case by case.
- Two-output behaviour, per Task 13.
