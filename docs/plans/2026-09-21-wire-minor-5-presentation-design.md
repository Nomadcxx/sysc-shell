# Wire Minor 5 — Presentation Primitives Design

Date: 2026-09-21
Status: Owner-approved 2026-09-21.
Branch: docs-only on `main` (implementation later on `feature/wire-minor-5`).
Commissioned by: the KDE Connect modern-panel infrastructure plan
(`~/.commandcode/plans/kdeconnect-modern-panel-infrastructure.md`, Design A).

Supersedes nothing. Minor 2 carried the presentation fields (fills, radius, bold, size tiers,
disabled, alignment), minor 3 the gauge vocabulary, minor 4 the parity surface (tooltips, shapes,
values, absent, graph, separator). The next minor is **5**; the commissioning plan called it
"minor 3" before the gauge history was confirmed.

## Scope

One tranche of wire vocabulary, the smallest set the KDE Connect gap-fill needs:

- an `image` kind carrying a host-decoded raster;
- an explicit container stroke on row, column, and button;
- explicit children on `button`;
- one new catalogue glyph (`send`).

It ships no new rendering: every primitive maps onto a painter the host already runs. It ships no
host calls (Design B), no animation channel (Design C), no sliders/toggles/menus/segmented/graph on
the wire, no warning/success theme roles, no image-from-bytes transport, and no virtual lists.

## Decisions

| # | Decision | Rejected alternative |
|---|---|---|
| D1 | `image` is a new leaf kind validated only in panel views. | Allowing it in bar views. Bar slots are fixed-width and rebuilt per frame; a decode job per bar revision is waste no consumer needs. |
| D2 | The image path is an absolute filesystem path, decoded by the host. | Image-from-bytes transport. A plugin that can name a path can read the file itself; bytes on the wire would triple the framing cost for no capability gain. |
| D3 | The decode rides a new `icons.FileResolver` plus the existing `icons.Worker`, so caps (8 MiB / 4096 px) and the bounded cache are inherited, not reimplemented. | A bespoke decode path in `shell`. `mediaArtWorker` exists because album art mixes catalogue names and remote URLs; plugin images are paths only, which is exactly what a resolver already abstracts. |
| D4 | The converter writes the path into a new host-side `ui.Node.ImagePath`; the decode registrar walks the retained tree. | A side table keyed by node id. Image nodes are not interactive, so `ID` is optional and may be empty; `Key` is optional too. A node field survives every rebuild by construction. |
| D5 | Stroke rides the existing `Stroke`/`StrokeFill` node fields, painted by `paintChrome` for capsules and buttons alike. | A new border primitive. `paint.go:1098-1103` already paints exactly this; the wire work is validation and mapping only. |
| D6 | The wire field is `stroke_fill` from the existing fill vocabulary, defaulting to the outline (rim) tone when absent. | The commissioning plan's `stroke_tone`. The fill vocabulary (`knownFills`) is what `capsuleFill` resolves; a second tone name would need its own table for one consumer. |
| D7 | Button children are non-interactive leaves only; the button stays the single hit target and focusable. | Nested interactive children. `ui.Hit` routes by node action; two actions under one pointer would make activation ambiguous for both pointer and keyboard. |
| D8 | Explicit button children replace the synthesized icon/text children; the existing synthesis is unchanged when no children are sent. | Merging both. Two sources of the same children is a validator headache for zero capability. |
| D9 | `send` is the only new glyph; `check`, `link`, and `close` are already in the material subset. | Adding all four as the plan first assumed. Three of the four exist (`materialfont.go:33-37`); the subset grows only deliberately. |
| D10 | Compatibility is lockstep: `framing.go` decodes strictly (`DisallowUnknownFields`), so an older host rejects a view carrying minor-5 fields rather than degrading. No negotiated-minor feature detect in this tranche. | A `Client.Minor()` helper so plugins branch at runtime. No plugin branches today (the pomodoro audit proved the manifest minor is decorative); the KDE Connect plugin ships with the shell. Add the helper when a second, independently-versioned plugin needs it. |

## Wire vocabulary

All fields are optional and additive; a tree that sets none of them validates exactly as it did at
minor 4. New constants follow the existing naming: `KindImage NodeKind = "image"`.

### `image` kind

```
{ "kind": "image", "path": "/home/x/Pictures/a.png",
  "image_size": 96 }                      // square
{ "kind": "image", "path": "...", "image_w": 320, "image_h": 180,
  "background": true, "shape": "card" }   // cover-fill box
```

Fields: `path` (string), `image_size` (square edge), `image_w`/`image_h` (explicit box),
`background` (cover-fill instead of fit), plus the existing `shape`/`radius` for the round mask.

Validator (`minorFive`), each check with its reject message:

- `image` is legal only in `ViewPanel` — a bar or tooltip view carrying it is rejected.
- `path` must be non-empty, absolute (leading `/`), at most 4096 bytes. The host reads the file;
  a relative path would resolve against the shell's cwd, which the plugin cannot know.
- Exactly one box form: `image_size` alone, or `image_w` and `image_h` together (both positive,
  each ≤ `MaxExtent`). One of a pair alone is the `imageBox` fallback trap — reject it rather than
  silently squaring it.
- `background` requires the explicit `image_w`/`image_h` box: a cover-fill needs a container shape
  to fill, and a square edge underspecifies it.
- No children (leaf; the generic container check already rejects this).
- `shape` follows the existing `fillAllowed` rule (containers and buttons today; `image` joins the
  allowed set) — the mask is the album-art treatment.

Trust model, stated once: the plugin already runs with the user's privileges and can read any file
it can name; the wire adds display, not read, power. The worker caps bound the cost of a hostile
or accidental path (8 MiB read, 4096 px source), and a decode failure paints the reserved box.

### Container stroke

`stroke` (int, logical px) and `stroke_fill` (name from `knownFills`) on `row`, `column`, and
`button`. Validator: `stroke` in 0..`MaxStroke` (new constant, 8 — the consumer is a 1 px rim);
`stroke_fill` must be a known fill when set; both only on kinds where `fillAllowed` holds. Absent
`stroke_fill` with a set `stroke` resolves to `ui.FillOutline` — the rim cards already draw — so a
plugin asks for a border with one field.

### Button children

The generic "takes no children" check gains an exception: `button` may carry children. Additional
rule: a button's children may not be interactive kinds (`button`, `text_input`, `drag_source`) —
one hit target per control. `MaxChildren` still bounds the subtree. Everything else is unchanged:
the button's `ID` becomes the action, the button is the focusable, children paint inside its hit
target.

### `send` glyph

`internal/render/icons/material/build.py` `ICONS` list, `materialfont.go`'s accepted-name set, and
`materialfont_test.go`'s name list, in one commit — the same three-file flow as the previous
catalogue additions.

## Host mapping

### Converter (`internal/plugin/view.go`)

- `v1.KindImage` → `ui.KindImage`, mapping `image_size` → `ImageSize`, `image_w`/`image_h` →
  `ImageW`/`ImageH`, `background` → `Background`, `path` → the new `ui.Node.ImagePath`. The painter
  already handles all four paint modes: reserved box when `Image` is nil, rounded mask via
  `Shape`/`Radius`, cover-fill when `Background` (paint.go:319-337).
- `stroke`/`stroke_fill` → `ui.Node.Stroke`/`StrokeFill`. For row/column the stroke lands on the
  capsule the `card()` wrapper builds (that is the node the painter paints); for button it lands on
  the button node itself — `paintButton` delegates to `paintChrome`, which paints `Stroke`.
- Button children: when the wire node carries children, convert each and use them as the button's
  children; otherwise the existing icon/text synthesis runs unchanged.

### Decode flow (`internal/shell/pluginhost.go`, `internal/icons`)

- `icons` gains `FileResolver`: `Resolve(name, size)` returns the name itself when it is an
  absolute path, else fails. `icons.NewWorker(FileResolver{}, publish)` then gives plugin images the
  worker's queueing, in-flight collapse, 8 MiB / 4096 px caps, and bounded cache (256 entries /
  32 MiB) with no new decode code.
- The plugin host owns one such worker. When a panel view is applied (`applyResult` → the retained
  `hostedView.Root`), a walk collects `KindImage` nodes with a non-nil `ImagePath` and a nil
  `Image`; for each, `Key{Name: path, W, H}` from the node's box: a cache hit sets `Image`
  immediately, a miss queues a decode.
- The worker's publish callback walks the retained views under `h.mu`, sets `Image` on the matching
  nodes, and calls `refreshPanel()` (panels) or `refreshPluginBars()` (bars, for completeness) —
  the same invalidation a view revision rides. A nil image never reflows: the box was reserved at
  layout time.

### Version advertisement

`internal/plugin/supervisor.go` `Supported` gains `{Major: 1, Minor: 5}`; `plugin/v1/node.go`'s
doc comment records what minor five carried. The manifest `minor` stays decorative (pomodoro audit
§2); the strict decoder is the real compatibility edge, and D10 records the consequence.

## Files

| File | Change |
|---|---|
| `plugin/v1/node.go` | `KindImage`, `Path`/`ImageSize`/`ImageW`/`ImageH`/`Background`/`Stroke`/`StrokeFill` fields, `minorFive` validator, button-children exception, doc comment |
| `plugin/v1/node_test.go` | Accept/reject matrices for every new field |
| `internal/plugin/view.go` | Converter cases: image, stroke (wrapper vs button), button children |
| `internal/plugin/view_test.go` | Converter mapping tests |
| `internal/ui/tree.go` | `ImagePath` field (host-side, documented like `Image`) |
| `internal/icons/theme.go` | `FileResolver` |
| `internal/icons/worker_test.go` | `FileResolver` resolve tests |
| `internal/shell/pluginhost.go` | Worker instance, decode registrar walk, publish write-back |
| `internal/shell/pluginhost_test.go` | End-to-end: plant a PNG in a temp dir, assert decode, write-back, and refresh |
| `internal/plugin/supervisor.go` | `Supported` gains `{1, 5}` |
| `internal/render/icons/material/build.py`, `materialfont.go`, `materialfont_test.go` | `send` |

`internal/render/` changes nothing: image paint, stroke paint, and button-child painting all exist.

## Automated evidence

- Validator: one accept and one reject per rule above, in the existing matrix style.
- Converter: image field mapping; stroke on wrapper vs button; explicit children replace synthesis;
  synthesis still fires without children.
- Decode end-to-end: gate test writes a small PNG to `t.TempDir()`, publishes a panel view carrying
  it, asserts the retained node gains a non-nil `Image` after the worker publishes and that
  `refreshPanel` ran; a second test asserts a nonexistent path leaves the box painted and no error
  surfaced.
- Stroke: a paint-level test asserting `StrokeRounded` runs for a stroked capsule and button
  (mirror the existing chrome tests).
- Button children: a hit test asserting activation routes to the button's action when a child is
  struck.
- Glyph: `RasterMaterialIcon("send", 24)` succeeds (the `materialfont_test.go` pattern).

Full gates per commit: `gofmt -w . && test -z "$(gofmt -l .)"`, `go vet ./...`,
`go test -race -count=1 ./...`, clean `git diff -- go.mod go.sum`.

## Dependencies and assumptions

- Verified against the tree at `348fd12` (`feat/plugin-material-icons`): `ui.KindImage` and its
  paint modes exist; `Stroke`/`StrokeFill` paint on capsules and buttons; `icons.Worker` caps and
  cache are as cited; `hostedView.Root` is retained and `refreshPanel` is the panel invalidation
  path; `check`/`link`/`close` are in the material subset and `send` is not.
- Assumes the KDE Connect gap-fill (Plan D) is the only first consumer; if a second arrives with
  different needs (bytes transport, bar images), that is a new decision, not an amendment.
- The commissioning plan's remaining Design A citations (media-art precedent, `send`-only glyph)
  match this document; its "wire minor 3" naming is corrected to minor 5 here and in the plan.
