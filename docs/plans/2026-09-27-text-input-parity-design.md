# Text Input Parity — Design

Tracking: `sysc-623` (UI parity slice 4). Owner-approved approach 2026-09-27 ("resolve keys once at
the platform edge; `ui.Field` is the editing engine"). Found while fixing the github-notifications
panel in sysc-plugins, where a search field grew with its text and pushed its sibling, and a long
query ended in an ellipsis instead of following the caret.

## Problem

Every text field in the shell — panel searches, settings entries, the network password, the
Bluetooth PIN, menu filters, and every plugin `KindTextInput` — shares one model, `ui.Field`
(`internal/ui/textfield.go`), and one edit funnel, `PanelHost.editField`. Both are minimal:

| Area | Today |
|---|---|
| Width | `measureNode` (`internal/ui/layout.go`) sizes a field to `max(Width, text width)`, so it grows as the user types and pushes siblings |
| Overflow | `paintTextField` (`internal/render/paint.go`) ellipsizes; the caret can sit past the visible end |
| Caret | Left/Right move roving focus out of the field; Home/End scroll the panel; a click focuses but does not place the caret |
| Editing | Backspace only. No Delete, no word motion or deletion, no shortcuts |
| Selection, clipboard | None. `wl_data_device` is generated in `sysc-wayland` but never bound |
| Undo | None |
| Layouts | `ui.EvdevText` maps evdev codes through a fixed US table. `wl_keyboard.keymap` is ignored: other layouts type the wrong characters, AltGr and dead keys do nothing |
| Modifiers | `PanelHost` tracks Shift/Ctrl/Alt from its own key press and release events |
| IME | Works (`text-input-v3`, `EventIME`, `Field.Commit`/`Preedit`/`DeleteSurrounding`) |
| Key repeat | Implemented (`f755eea`); focus-move repair is `sysc-591` Task 5–6 |

Noctalia and DMS build on QML `TextInput`, which has all of the above. This slice closes that gap for
every field at once, including plugin fields, with no plugin protocol change.

## Decisions

### D1. Keys resolve once, at the platform edge, through a pinned pure-Go XKB

`internal/platform/wayland` reads `wl_keyboard.keymap` (format `xkb_v1`, mmapped from the fd) into an
`xkb-go` keymap and feeds `wl_keyboard.modifiers` to its state. Every key event it delivers — press,
release, and every synthesised repeat — carries the resolved result:

```go
// Added to wayland.Event. Key and Serial keep their meaning.
Sym  uint32 // XKB keysym after the current layout, level, and group
Text string // UTF-8 the key types, after compose; "" for non-printing keys and while composing
Mods Mods   // Shift, Ctrl, Alt (Mod1), Super (Mod4), CapsLock; AltGr is a level, not Alt
```

- Dependency: `github.com/thegrumpylion/xkb-go` pinned at `v0.1.0` (MIT, pure Go, keymap text V1,
  `State.UpdateMask`, `KeyGetUTF8`, `ComposeTable`/`ComposeState`). Chosen over libxkbcommon via cgo
  because AGENTS.md puts a pinned dependency above a C boundary and no measured requirement defeats
  the Go implementation yet; over an own parser because compose would be missing. If a defect in the
  library blocks a layout, patch it in a fork pinned by commit, and record the reason here.
- `Text` is computed from the xkb state at delivery time, so a repeat reflects the modifiers held
  now, not at the first press.
- Control characters (below U+0020 and U+007F) are stripped from `Text`: shortcuts are matched on
  `Sym` and `Mods`, never on the control byte xkb's Ctrl transform produces.
- Compose: the table loads from the locale (`LC_ALL`, `LC_CTYPE`, `LANG`, then `C`) once per keymap.
  A dead key yields `Text == ""` until the sequence completes; a cancelled sequence yields nothing.
- Fallback: no keymap, a format other than `xkb_v1`, or a parse error keeps today's behaviour —
  `Text` from the US evdev table using the Shift bit, `Sym` from a small evdev-to-keysym table for the
  navigation and editing keys. Logged once per keymap, never per key.

### D2. Modifier state comes from the event

`PanelHost` stops tracking Shift/Ctrl/Alt from its own press and release events. It reads `e.Mods`.
The owner also stamps `Mods` on pointer button events, so Shift+click (D9) needs no second source.
Before D1 lands (see Sequencing), the fallback decodes `wl_keyboard.modifiers` with the standard mod
indices (Shift 0, Lock 1, Control 2, Mod1 3, Mod4 6), which every keymap in practice uses.

### D3. `ui.Field` is the editing engine, behind one pure method

`ui.Field` gains a selection anchor, undo history (D4), and editing operations. Positions stay byte
offsets into `Text`, always on a grapheme-cluster boundary (`github.com/rivo/uniseg`, already in the
module graph via fzf, promoted to a direct dependency). A word is a maximal run of letters, digits
and `_`; anything else separates words.

One method interprets a resolved key:

```go
type Mods uint8 // ModShift, ModCtrl, ModAlt, ModSuper, ModCapsLock; wayland.Mods converts to it
type KeyInput struct { Sym uint32; Text string; Mods Mods }   // ui-level mirror of the event fields
type FieldResult struct {
    Handled, Changed bool
    Copy    string // non-empty: put this on the system clipboard
    Paste   bool   // request the clipboard's text (arrives later via EventPaste, D5)
    Submit  bool
}
func (f *Field) HandleKey(k KeyInput) FieldResult
```

Key table — GUI-first, with the readline keys that do not conflict:

| Keys | Effect |
|---|---|
| Left / Right | Move one grapheme; with a selection and no Shift, collapse to that edge |
| Ctrl+Left / Ctrl+Right | Move one word |
| Home / End, Ctrl+E | Line start / line end (Ctrl+E is end) |
| Ctrl+Home / Ctrl+End | Text start / end |
| Up / Down | Multiline only: previous / next line keeping a goal column; single-line fields do not handle them, so roving focus still works |
| any of the above + Shift | Extend the selection instead of moving the anchor |
| Backspace / Delete | Delete the selection, else one grapheme back / forward |
| Ctrl+Backspace, Ctrl+W / Ctrl+Delete | Delete one word back / forward |
| Ctrl+U / Ctrl+K | Delete to line start / line end |
| Ctrl+A | Select all |
| Ctrl+C / Ctrl+X / Ctrl+V | Copy / cut / paste |
| Ctrl+Z / Ctrl+Shift+Z, Ctrl+Y | Undo / redo |
| Enter | Multiline without `SubmitOnEnter`: newline. Otherwise `Submit`, which the host routes exactly as today |
| printable `Text`, no Ctrl/Alt/Super | Insert, replacing the selection |
| Tab, Esc, everything else | Not handled: the host's navigation keeps them |

Masked fields refuse Copy and Cut (handled, nothing copied), and treat their text as one word.
`HandleKey` has no clocks, no I/O, and no Wayland types, so the whole table is a table test.

`editField` calls `HandleKey` for the focused field before any panel navigation; `Handled` stops the
key there. The menu filter goes through the same funnel. Direct uses of `ui.EvdevText` in
`panelhost.go` go away.

### D4. Minimal undo, coalesced like GTK

Each edit pushes a `{Text, Cursor, Anchor}` snapshot; history holds 100 steps per field. Consecutive
single-grapheme inserts coalesce into one step until the caret moves by any other means, a
non-insert edit happens, or a whitespace grapheme follows a non-whitespace one. Consecutive
same-direction deletes coalesce the same way. An IME commit, a paste, and a cut are each one step.
`SyncFrom` with new text (a reseed from a plugin or a programmatic set) clears history; typing never
reseeds, so history survives rebuilds on the retained editor.

### D5. The system clipboard, through `wl_data_device`

The platform binds `wl_data_device_manager` (version 3) and one data device for the seat.

- Copy and cut: create a `wl_data_source` offering `text/plain;charset=utf-8`, `text/plain`,
  `UTF8_STRING`, `TEXT`, and `STRING`; call `set_selection` with the serial of the key event that
  asked (`Event.Serial`). Each `send` writes the text to the fd on a goroutine and closes it — never
  on the owner goroutine. `cancelled` destroys the source.
- Paste: keep the offer from the latest `wl_data_device.selection`. On request, pick the first of the
  same mime list the offer advertises, `receive` into a pipe, and read on a goroutine with a 1 s
  timeout and a 1 MiB cap. Deliver `EventPaste{Text}` to the unit that still has keyboard focus;
  drop it if focus moved. The shell applies it through `editField` as one insert step.
- Pasted text drops NUL bytes. Single-line fields replace each line break with a space.
- No offer, no text mime, a timeout, or an error pastes nothing and logs once per failure kind.
- Copies land in `sysc-clipboard`'s history the same way any application's copy does.

### D6. A field's width is its own, never its text's

`measureNode` for `KindTextField` returns `Width` when set, clamped to the space the row has left. With
`Width == 0` the field fills the row's remaining width, as a trailing column does, and never less
than 120 logical px. Height and `columnChildHeight` are unchanged. `ui.CheckFit` shares this code, so
plugin fit lints (`plugin/lint`) follow automatically. A plugin that relied on growth sees its field
hold its declared width, which was the intent of declaring it.

### D7. Focused single-line fields scroll to keep the caret in view

The retained editor (`retainedEditor` in `panelhost.go`) gains `ScrollX` in logical px. After layout
and before paint, the host measures the caret's x within the displayed text and updates it:

```go
// Pure, in internal/ui. margin keeps the caret off the very edge.
func KeepCaretVisible(scroll, caretX, textW, viewW, margin int) int
```

The result keeps the caret inside `[scroll+margin, scroll+viewW-margin]`, never scrolls past
`textW-viewW` (no dead space after the end), and never goes below zero. `ui.Node` gains a
render-only `ScrollX`, set the way `Cursor` is. `paintTextField` translates a focused field's text,
preedit, caret and selection by `-ScrollX` inside the existing clip and does not ellipsize; an
unfocused field paints from the start with the ellipsis it has today. Masked fields scroll over
bullets. The `text-input-v3` cursor rectangle reports the scrolled caret, so an IME's candidate window
follows it.

Multiline fields gain the vertical equivalent over hard lines (`ScrollY` in lines). Wrapping stays
out of scope.

### D8. Selection paints behind the text

`ui.Node` gains render-only `SelStart` and `SelEnd` (byte offsets, set like `Cursor`). The painter
fills the selected span's rectangle with the theme accent at 40% alpha, under the text, per line for
multiline. The caret still
paints. An unfocused field keeps its selection but paints it in the outline tone.

### D9. The pointer places the caret and selects

- A primary press in a field focuses it and puts the caret at the grapheme boundary nearest the
  pointer, measured with the text renderer after the field's padding, search glyph, and `ScrollX`.
- Shift+press extends the selection to that point.
- Dragging with the button held extends the selection; `KeepCaretVisible` scrolls when the pointer
  passes an edge.
- A double press selects a word; a triple press selects the line (the whole text in a single-line
  field). The shell has no multi-click detection today, so the panel host gains a press counter: a
  primary press on the same field within 400 ms and 4 logical px of the previous one increments it,
  anything else resets it to one.

### D10. Plugins inherit everything; the protocol does not change

Plugin `KindTextInput` nodes already become host-owned fields with retained editors. Caret,
selection, clipboard, undo, and scrolling stay host state; plugins still receive `EventChange` with
the full text and `EventSubmit`, and a plugin-supplied `Text` or `Reseed` still reseeds. No protocol
minor bump.

## Ownership

- `internal/ui`: `Field` operations, `HandleKey`, undo, word and grapheme boundaries,
  `KeepCaretVisible`, `measureNode` width rule, `Node.ScrollX`/`ScrollY`/`SelStart`/`SelEnd`. Pure.
- `internal/platform/wayland`: keymap and compose (`keymap.go`), modifier state, `Event.Sym`,
  `Text`, `Mods`, the fallback path, the data device and paste reads (`clipboard.go`), `EventPaste`.
- `internal/shell`: `editField` routing through `HandleKey`, clipboard requests, scroll update after
  layout, pointer caret placement and drag selection, removal of host-tracked modifiers.
- `internal/render`: scrolled field text, selection fill.

## Testing

- `internal/ui`: table tests for every row of the D3 key table, single-line and multiline, masked
  and plain, including grapheme cases (combining marks, a ZWJ emoji, CJK); undo coalescing
  boundaries; `KeepCaretVisible` edge cases; width rule (a field never widens with its text).
- `internal/platform/wayland`: keymap fixtures compiled with `xkbcli compile-keymap` for `us`,
  `de`, `fr`, and `us(intl)`, checked in as `testdata`, with expected `Sym`/`Text` for chosen
  keycodes under Shift, AltGr, and Caps Lock; a compose fixture (not the system locale) proving
  dead_acute + e → é; the fallback on a malformed keymap; repeat text following a modifier change.
  Clipboard: mime selection, the async read's timeout and cap, and drop-on-focus-change, driven
  through the existing fake-connection harness and real pipes.
- `internal/render`: pixel checks that a scrolled field shows the caret and not the start, and that a
  selection fills behind text.
- Live gate on Niri: type in a plugin search with `de` and `us(intl)` layouts including dead keys;
  copy from a field and paste into a terminal and back; a long query scrolls with the caret; undo;
  double-click word select; held-key repeat still works.

## Sequencing

Four phases, each shippable on its own:

1. **Layout and scroll** — D6, D7, and the paint half of D8's plumbing. Fixes the reported defects
   with no key changes.
2. **Editing engine** — D2 (fallback decoding), D3, D4, D8, D9, with `Event.Sym`/`Text`/`Mods`
   filled by the fallback path.
3. **Clipboard** — D5.
4. **Keymaps** — D1. Its edits to `keyboard.go` overlap `sysc-591` Tasks 5–6 (key-repeat retargeting),
   so this phase starts after those merge.

## Non-goals

Primary selection and middle-click paste. Rich text. Line wrapping in multiline fields. Drag and drop
of text. A right-click context menu. Spellcheck. Plugin-visible caret or selection.
