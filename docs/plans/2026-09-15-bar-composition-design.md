# Bar composition design

Date: 2026-09-15. Owner-approved in brainstorming on 2026-09-15.

This design covers sub-project B of Milestone 9: adding, removing, reordering,
grouping, and configuring bar widgets from the settings surface.

It consumes the schema and chrome established by
[the settings foundation design](2026-09-15-settings-foundation-design.md) and
does not restate them. Where that document left the three `bar.items.*` string
entries untouched, this one replaces them.

Sources read as behaviour and architecture references only:

- `internal/config/config.go`, `load.go`, `write.go`
- `internal/shell/widget.go`, `bar.go`, `panelhost.go`, `popout_wallpaper.go`
- `internal/settings/registry.go`
- `internal/ui/drag.go`, `tree.go`, `internal/render/paint.go`
- `/home/nomadx/noctalia/src/shell/settings/bar_widget_editor.{h,cpp}`,
  `settings_bar_management.h`
- `/home/nomadx/Documents/GitHub/DankMaterialShell/Modules/Settings/`
  `WidgetsTabSection.qml`, `WidgetSelectionPopup.qml`, `DankBarTab.qml`

## Goal and scope

In scope:

- Add, remove, and reorder widgets across the three bar lanes.
- Create, edit, and dissolve capsule groups.
- Per-widget options through an inspector.
- Stable identity for built-in widget instances and for groups.
- Per-output lane editing.
- Keyboard parity for every one of the above.

Out of scope:

- Bar edge, auto-hide, exclusive zone, and geometry, which are sub-project C.
- Direct manipulation on the live bar.
- Multiple named bars.
- Per-item output overrides, which the configuration model does not express.

## The problem

Bar arrangement is reachable today only as three comma-separated strings:
`bar.items.left`, `bar.items.center`, and `bar.items.right` in the settings
registry. There is no add, no remove, no reorder, and no per-instance option.

Two defects make this worse than it looks.

**Widgets have no identity.** `buildWidgets` switches on `item.ID` and captures
options by closure. `config.Item.Instance` exists but `resolveItem` rejects it
on any non-plugin item, with "is accepted only on a plugin placement". Two
clocks on one bar are therefore indistinguishable.

**Per-widget options are already broken.** The settings registry reaches items
through `eachItem(c, "clock", ...)`, which applies a change to *every* clock in
the configuration. A user with a time and a date widget cannot give them
different formats from the interface. This is a live defect, not a limitation
introduced by this work.

Groups compound it: a group is `Item{ID: "group", Items: [...]}` with no id and
no options of its own, and `flattenItems` discards the wrapper entirely, so no
validation rule has ever seen a group as an addressable thing.

## What the shell already has

The drag stack is complete and has never been used by a shell surface:

- `ui.Drag` with `Begin`, `Move`, `Accepts`, `Hits`, `Drop`, and
  `FindDropZone`, an 8-pixel movement threshold and 8-pixel drop slop.
- `Node.DragType`, `Node.Payload`, and `Node.Accept`.
- `KindDragSource` painting on the button path and `KindDropZone` on the column
  path in `internal/render/paint.go`.
- `PanelHost` beginning a drag on a `KindDragSource` press and advancing it on
  pointer motion.

`Bar` has none of this. `Bar.Handle` resolves pointer events to an action string
through `hitLocked` and tracks hover and press as stable keys; it has no drag
state at all. That asymmetry is the whole reason editing happens in the settings
surface rather than on the bar.

`connectorsLocked()` already lists the connectors that currently have a bar.

## Approaches considered

The approved approach puts lane mutations in `internal/config` as pure functions
over `[]Item`, called by both the pointer and keyboard paths.

Implementing mutations inside the settings panel against `PanelHost.draft` was
rejected. The invariants that matter here — the one-level nesting cap, empty
group pruning, lazy id minting, instance uniqueness — would be written inside
pointer-event handling and could not be tested without a live host.

A separate `internal/bareditor` package was rejected as a third home for bar
rules that already live in `internal/config` beside the validation they must
agree with.

Editing on the live bar was rejected for this sub-project. It would require new
drag plumbing on `Bar`, an explicit edit mode, and a resolution for the fact
that every widget already owns a click action, against a surface roughly one
control tall.

## Decisions

### D1: Lane mutations are pure functions in `internal/config`

`Move`, `Insert`, `Remove`, `Group`, and `Ungroup` operate on `[]Item` and
return a new slice or an error. Drag and keyboard both call them.

The rules are written once and unit-tested without a `PanelHost`, beside
`resolveItem`, whose validation they have to stay consistent with.

### D2: Identity extends to built-in widgets and to groups

`wireItem` accepts `instance` on non-plugin items, and `resolveItem`'s blanket
rejection narrows to `plugin` and `entry` only.

Groups take ids too. They are draggable, inspectable, and addressable, and a
structurally anonymous group cannot be any of those.

The loader uses `DisallowUnknownFields`, so this is a schema change: a document
written by a shell with this change will not load on one without it. The project
makes no cross-version configuration compatibility promise, and this is recorded
rather than worked around.

### D3: Ids are minted lazily

An instance id appears only when something addresses the widget: a reorder, a
grouping, or a per-widget option change.

Existing documents keep working untouched, and a configuration file grows ids
only for widgets the user actually customised, which keeps diffs readable.

### D4: Per-widget options stay typed on `config.Item`

`Format`, `MaxWidth`, `Display`, `Interval`, `Path`, `Device`, `Interface`,
`Direction`, `ShowCondition`, `Label`, and `WarnBelow` stay where they are.

`resolveItem` already validates each option against its widget id and reports
the exact field path. An instance-keyed `map[string]any`, as plugins use, would
be uniform with plugin widgets but would discard that validation. Identity is
added beside the typed fields, not in place of them.

Fixing `eachItem` follows from identity: an option write addresses one instance.

### D5: Full group editing

Dropping a widget onto another creates a group containing both. A group can be
dissolved. Members can be dragged in and out.

Two invariants:

- **The one-level cap is enforced at the drop.** `resolveItem` refuses a group
  inside a group, so a drop that would nest one is refused outright, never
  silently flattened.
- **An empty group is pruned.** Dragging the last member out removes the group
  and its placement. This is Noctalia's rule and transfers directly.

Noctalia stores a lane token plus a separate group-style list. This design keeps
the existing inline `Items` nesting instead: it is simpler, it is what the
loader and the bar renderer already understand, and it needs no second store.

### D6: Keyboard parity through commands

A focused chip takes `Alt`+Left/Right to move within a lane and `Alt`+Up/Down to
move between lanes, plus explicit group and ungroup commands. Move controls
appear on the focused chip so the commands are discoverable.

Every command calls D1's mutation functions, so pointer and keyboard rules
cannot drift, and the tests need no synthesised pointer events.

### D7: Per-output lanes, at lane granularity

An output selector sits above the lane strip, reusing `connectorsLocked()`.
`Shared` edits `cfg.Bar`; a connector edits that `OutputOverride.Bar`.

The granularity is fixed by the loader and is not a choice this design makes.
`applyBar` starts from the resolved base bar (`bar := cfg.Bar`) and each lane
goes through `items(w.Items.Left, out.Left, ...)`, which returns the base when
the wire field is absent. **A lane is inherited whole or overridden whole.**

The consequence must be visible in the interface: reordering one widget on one
output forks that entire lane for that output, and later changes to the shared
lane stop reaching it. Each lane therefore reads as inherited or overridden and
carries a reset to shared. A per-widget override affordance would be a lie.

### D8: `flattenItems` stops hiding groups

It currently appends `item.Items...` and skips the wrapper, so
`consistentInstances` has never seen a group. With D2 giving groups ids, that
would let duplicate group ids pass silently.

Only one of its two callers is affected. `requireWeatherWhenUsed` inspects
`item.ID == "weather"` and is indifferent to the wrapper; its behaviour must be
proven unchanged.

### D9: The `bar.items.*` entries are removed

The three comma-separated string entries are replaced by the lane editor. The
settings foundation deliberately left them alone so this sub-project inherits
one unambiguous starting point.

## The drop model

`FindDropZone` tests a node before descending into it:

```go
if n.Kind == KindDropZone && d.Hits(n) { found = n; return }
for _, c := range n.Children { walk(c) }
```

An outer lane zone therefore always wins over a nested group zone, which would
make a group drop target unreachable.

**Groups are consequently not drop zones.** Each lane has exactly one zone, and
it resolves the target itself from chip bounds: a drop on a chip's inner half
joins or creates a group, and a drop between chips inserts at that index.

This keeps a primitive shared with the plugin view unchanged for the sake of one
consumer, which is the same restraint the settings foundation showed in refusing
to extend `KindVirtualList`.

Target resolution is a pure function of chip bounds and drop point, returning an
insertion index or a join target. The pointer path is a thin caller.

## The editor chrome

The Bar section gains a Layout group above its geometry rows.

One named exception to the foundation's no-cards rule: a drop target has to be a
visible surface, so lanes carry card chrome. Option rows inside the inspector
follow the foundation unchanged.

One lane:

```
KindColumn  gap MarginS
|- KindText  lane name, RoleCaption, muted
'- KindDropZone  Accept ["bar-widget"]
   Fill FillContainerHigh, Shape ShapeCard, Padding CardPadding
   '- KindRow gap MarginS   -- the chips
```

Lane height derives from `StandardControl` plus twice `CardPadding`, never a
literal, so the strip follows the density ladder like every other surface.

A chip is a `KindDragSource` carrying `DragType: "bar-widget"`, the instance id
as `Payload`, `ShapeStadium`, the widget's display name, and a trailing options
glyph. It is focusable and carries an accessible name and role.

A group renders as a bounded run of chips inside the lane, `FillContainerHighest`
at `ShapeSmall`, with a dissolve control. This mirrors the live bar, where a
group is one capsule holding uncapsuled members.

The inspector opens beneath the strip for the selected chip, built from entries
synthesised at runtime for that `Item` from `resolveItem`'s own option matrix.
The settings foundation's typed accessor entries are what make this possible; a
switch statement cannot enumerate an arbitrary instance's fields.

The add control is a chip at each lane's end opening an in-panel searchable list
of the known item vocabulary plus installed plugin entries, expanding in place
as `Menu` does, because no popup-over-panel surface exists.

## Testing

Mutation functions in `internal/config` take table tests: move within and across
lanes, insert, remove, group creation, ungroup, a refused group-inside-group, a
pruned empty group and its placement, and ids minted only on addressing.

`consistentInstances` gains coverage for duplicate group ids, unique built-in
ids, and the deliberate case its own comment records: the same id on two outputs
is one placement and stays legal.

`flattenItems`'s change is proven not to regress `requireWeatherWhenUsed`: a
weather widget nested in a group is still found.

Load and round-trip: `instance` accepted on a built-in, documents without ids
still load, a document with ids survives `Write` and reload.

In `internal/shell`: three lanes render with chips; drop resolution through the
pure function; `Alt`+arrow reorders the draft; group and ungroup commands
produce the same result as the equivalent drags; an output selection shows each
lane as inherited or overridden; reset to shared clears a lane override; chips
carry accessible names and roles.

Retired with D9: `TestRegistryExposesBarItemLists`, which pins the three
comma-separated entries the lane editor replaces.

Gate: the per-package substitute with `-p` and `GOMAXPROCS` capped, never the
repository-wide `-race` run, which hard-locks this machine. The implementation
plan states the exact commands.

## Not verified

- The six candidate Material ligature names are not yet confirmed against the
  pinned Material Symbols source.
- Display names for the known item vocabulary are not written. The ids are
  configuration tokens and several read poorly as labels.
- Whether any plugin-supplied widget entry needs a different chip treatment from
  a built-in one. The placement model is the same; the label source is not.
- The interaction between a lane override and a plugin placement's instance id
  has not been walked case by case.

## File and anchor note

Every path and behaviour above was read against `main` on 2026-09-15. Anchors
drift; re-verify before relying on any of them.
