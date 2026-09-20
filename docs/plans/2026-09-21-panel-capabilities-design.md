# Panel Capabilities Design — `panel.resize` and `view.focus`

Date: 2026-09-21
Status: Owner-approved 2026-09-21
Branch: docs-only on `main` (implementation later, alongside or after `feature/wire-minor-5`).
Commissioned by: the KDE Connect modern-panel infrastructure plan
(`~/.commandcode/plans/kdeconnect-modern-panel-infrastructure.md`, Design B).

Supersedes nothing. Adds the two host calls the gap-fill needs and nothing else.

## Scope

- `panel.resize` — a plugin panel asks the host to change its surface size.
- `view.focus` — a plugin panel asks the host to focus one of its nodes.

No new wire node kinds, no new rendering, no Wayland protocol work beyond one field on an
existing update path. Excluded: per-view resize (a plugin has at most one open panel —
`h.panel` is singular), resize of bar/tooltip views (fixed slots by design), focus of another
plugin's views.

## Verified ground truth (tree at `348fd12` + in-flight minor-4 work)

- `hostedView` already carries `Width`/`Height` (`pluginhost.go:45-46`), set at `openPanel`
  from the manifest panel spec (`:625`, `:645`); the plugin tree is laid out at
  `Bounds: ui.Rect{W: v.Width, H: v.Height}` (`:322`). A plugin panel's size today is whatever
  the manifest declared, read back through `panelSize()` (`:792-799`, fallback 320×280).
- `AuxUpdate` carries only `Keyboard` and `InputRects` (`aux_surface.go:44-53`);
  `updateAux` → `planAuxUpdate` → `applyAuxPolicy` touches policy only (`:204-224`).
  `layer.SetSize` fires once, at open (`applyAuxGeometry`, `:163-165`). **No live layer
  resize exists anywhere** — `syncNotificationsSize` (`panelhost.go:2345-2351`) only re-derives
  `place.Panel` in memory and waits for the next configure.
- The configure chain is live: the aux surface's `SetConfigureHandler` → `onConfigure` →
  `h.configure(w, h, scale)` → `place.FittedSize()` → `place.Panel.W/H` (`panelhost.go:716`)
  → `rebuildPanel` re-lays-out and re-renders (`:2111-2146`).
- Calls dispatch on `call.Call` in `Dispatcher.dispatch` (`hostcall.go:122-150`) with
  capability gates; `CallEnv` carries host functions (`:27-30`) wired in one place —
  `pluginhost.go:211-216`. Params are bounded by hand (`boundNotify`, `:244`).
- Focus: `PanelHost.setFocus` (`panelhost.go:2468`), `hitFocusable` (`:2477`),
  `focusByName` (`:2459`) used at open for wallpaper/audio/clipboard; `ui.Focusables`
  flattens the tree in traversal order (`ui/focus.go:4`). The plugin panel spec already
  holds `Keyboard: keyboardExclusive` (`panelhost.go:980`), so no keyboard-interactivity
  change is needed. Plugin node identity is stamped at `applyResult`:
  `stampPluginActions(res.Root, res.ViewID)` (`pluginhost.go:376`) produces actions of the
  form `plugin:<viewID>:<nodeID>` (parsed by `parsePluginAction`, `pluginwidget.go:15-38`).

## Decisions

| # | Decision | Rejected alternative |
|---|---|---|
| D1 | `panel.resize` params are `{width, height}` only — the call always targets the calling plugin's open panel. | Naming the panel entry. `h.panel` is singular per plugin host; an entry name would validate a thing that cannot differ. |
| D2 | Bounds are 64–4096 logical px per axis, checked synchronously in the dispatcher (`boundPanelResize` beside `boundNotify`). | Reusing `MaxExtent` (8192). A panel is a popup, not a surface; the tighter bound is the contract. |
| D3 | The call updates `h.panel.Width/Height` under `h.mu`, calls `refreshPanel()` (tree re-lays-out at the new bounds at once), then queues an `AuxRequest{Update}` carrying the new size. The reply reports the bounds as *requested*; the compositor's configure completes it. | Blocking the reply on the configure round-trip. The aux channel is fire-and-forget today; making the reply synchronous couples the dispatcher to the Wayland goroutine for no plugin-visible benefit — the plugin cannot act on the difference. |
| D4 | `AuxUpdate` gains `Width`/`Height` (pointers, nil = unchanged); `updateAux` calls `layer.SetSize` + `surface.Commit()` when either is set, before the policy application. The existing configure handler does the rest. | A new AuxRequest verb. The update path exists, is tested (`aux_test.go`), and is exactly "change policy on an already-open surface". |
| D5 | The `wp_viewport` opened with the aux surface stays untouched. | Resizing through the viewport. `SetSize` on the layer surface is the layer-shell-native lever; the viewport is a scale/crop aid, and mixing both size sources is a defect generator. |
| D6 | `view.focus` params are `{view, node}`; the host requires `view` to be the calling plugin's currently open panel view and the node to be one of the panel's focusables, matched by the stamped action `plugin:<view>:<node>`. | Matching by `Name`. Names are accessibility labels, not addresses; the stamp is already the node's identity on this host. |
| D7 | Focus failure (unknown view, view not open, node absent or not focusable) is a `failReply` naming the node — never a plugin crash or a silent no-op. | Best-effort silent focus. The plugin needs to know its dialog did not take focus. |
| D8 | Both calls gate on `CapPanels`. | A new capability. They are panel-surface operations; a third capability would churn every manifest for nothing. |
| D9 | No `view.resize` wire message. | Telling the plugin its new bounds. Plugin trees are size-agnostic — the host re-lays-out the retained tree; the plugin learns nothing and needs nothing. |

## Wire additions (`plugin/v1/message.go`)

```go
CallPanelResize CallKind = "panel.resize"   // params: PanelResizeParams{Width, Height int}
CallViewFocus   CallKind = "view.focus"     // params: ViewFocusParams{View, Node string}
```

Both params structs validate in the dispatcher (bounds for resize; non-empty strings for
focus), mirroring `boundNotify`.

## Host flow

### `panel.resize`

1. `Dispatcher.dispatch` case `CallPanelResize` under `CapPanels` → `d.panelResize(ctx, call)`.
2. `boundPanelResize` checks 64–4096 per axis; failure is a `failReply`.
3. `CallEnv.PanelResize` → `pluginhost.go` handler: under `h.mu`, set
   `h.panel.Width/Height`; unlock; `h.refreshPanel()` (tree re-lays-out at the new bounds —
   `:322` reads the hostedView size); queue
   `h.r.aux <- wayland.AuxRequest{Output: global, ID: panelSurfaceID(PanelPlugin),
   Update: &wayland.AuxUpdate{Width: &w, Height: &hgt}}`.
4. Wayland owner: `updateAux` → `layer.SetSize(w, h)` + commit → compositor configure →
   `onConfigure` → `place.Panel` re-derived → `rebuildPanel` renders at the confirmed size.
   The fillet margin arithmetic (`place.Panel.W + 2*fillet`, `panelhost.go:977`) applies at
   the next spec build; the update path sends the body size, consistent with what
   `applyAuxGeometry` sets at open.

### `view.focus`

1. Case `CallViewFocus` under `CapPanels` → `d.viewFocus(ctx, call)`.
2. `CallEnv.ViewFocus` → handler: resolve `h.views[view]`; require `Kind == ViewPanel` and
   that it is the plugin's open panel (`h.panel.ID == view`); require the panel host to be
   live. Walk `h.focus` (the panel's `ui.Focusables`) for a node whose `Action` equals
   `plugin:<view>:<node>`; miss → `failReply`.
3. `setFocus(n)` + publish — the same path `focusByName` uses at open. No Wayland work: the
   panel already holds `keyboardExclusive`.

## Files

| File | Change |
|---|---|
| `plugin/v1/message.go` | Two `CallKind` constants, two params structs |
| `plugin/v1/message_test.go` | Param round-trip/strictness tests |
| `internal/plugin/hostcall.go` | Two dispatch cases, `boundPanelResize`, two `CallEnv` function fields |
| `internal/plugin/hostcall_test.go` | Capability-gate + bounds reject tests |
| `internal/shell/pluginhost.go` | `CallEnv` wiring (`:211` block), resize handler, focus handler |
| `internal/shell/pluginhost_test.go` | Resize updates hostedView + queues aux update; focus routes to the stamped node and fails loudly on a miss |
| `internal/platform/wayland/aux_surface.go` | `AuxUpdate.Width/Height`, `SetSize`+commit in `updateAux` |
| `internal/platform/wayland/aux_test.go` | Update-resize test beside `TestAuxUpdateRaisesKeyboardInteractivityInPlace` |

`internal/shell/panelhost.go` changes nothing: configure, `rebuildPanel`, and `setFocus`
already do the work.

## Automated evidence

- Dispatcher: resize under `CapPanels` granted/denied; bounds rejects (63, 4097, zero);
  focus rejects (unknown view, view not open, unknown node).
- Wayland: `updateAux` with size calls `SetSize` with the right values and commits; nil size
  leaves the surface alone (the existing keyboard test is the shape to follow).
- pluginhost: after a resize call, `panelSize()` returns the new rect and an aux update was
  queued; after a focus call, the panel's focused node is the stamped one.
- End-to-end gate: a fake plugin calls `panel.resize` then `view.focus`; assert the reply
  sequence and the resulting panel bounds.

Full gates per commit: `gofmt -w . && test -z "$(gofmt -l .)"`, `go vet ./...`,
`go test -race -count=1 ./...`, clean `git diff -- go.mod go.sum`.

## Dependencies and assumptions

- Independent of Design A's wire minor (no node-kind changes); may land before, with, or
  after it. The gap-fill consumes both.
- Assumes one open panel per plugin (true today: `h.panel` is a single pointer, replaced on
  `openPanel`). A multi-panel future amends D1, not this document's structure.
- The commissioning plan's touch-point list matches this document; its "AuxUpdate gaining a
  size field" and "no live layer resize exists today" claims verified as written.
