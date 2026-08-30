# Tray Foundation Implementation Plan — Milestone 5, Tranche 5B

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Ship Milestone 5 tray presentation: dial sysc-tray, render normal/attention/overlay icons in the bar, forward activate/secondary/scroll, open a keyboard-accessible DBusMenu as an xdg_popup (or D5 fallback), and an overflow drawer.

**Architecture:** Shell is a presentation client on the tray Unix socket. Items keyed by unique owner + object path. Named icons go through 5A `internal/icons`. Menus are requested on demand. Overflow uses M4 panel machinery.

**Tech Stack:** Go 1.26 stdlib, sysc-wayland xdg-shell (already generated), tagged `sysc-tray`, 5A icons, M4 panels.

**Design:** [2026-08-30-tray-foundation-design.md](2026-08-30-tray-foundation-design.md)
**Research:** [2026-08-30-notifications-and-tray-research.md](2026-08-30-notifications-and-tray-research.md)

---

## Prerequisites

1. 5A merged (at least `internal/icons` + M4 panels). Tagged `sysc-tray` with socket + item snapshot + menu on demand.
2. Live-test D5 before writing popup product code (Task 1).
3. No AI-tool substrings in commits.

---

### Task 1: Live-test xdg_popup parented to the bar layer surface

**Files:** none in tree until the result is recorded in the 5B design Risks section.

**Step 1:** On Niri, from a throwaway binary or `tests/integration`, create the bar layer surface. Create
a separate popup `wl_surface` and `xdg_surface`, call `xdg_surface.get_popup` with a null parent, then call
`zwlr_layer_surface_v1.get_popup` on the bar before the popup's initial commit. Use the triggering pointer
serial for `xdg_popup.grab`. Run the probe from bars on two outputs.

Expected: the popup maps at the triggering bar on each output, gets keyboard after the bar switches to
OnDemand, unmaps on Escape or outside click, restores the bar to keyboard None, and returns focus to the
last window. Do not commit connector names, host names, screenshots, or measurements.

**Step 2:** If it fails, set a flag `popupFromLayer = false` in the design Risks (one-line amendment) and implement Task 6 as M4 panel + KindMenu. Do not invent a third host.

**Commit:** `docs: record niri xdg_popup-from-layer result`

---

### Task 2: Freeze tray snapshot types

**Files:**
- Create: `internal/ipc/trayproto/types.go`
- Test: `internal/ipc/trayproto/types_test.go`

```go
type Item struct {
    Owner, Path string
    Title, Status, IconName, AttentionName, OverlayName, IconThemePath, MenuPath string
    NeedsAttention bool
    Icon, Attention, Overlay *Pixmap
}
type Pixmap struct{ W, H int; ARGB []byte }
type Snapshot struct {
    Type  string `json:"type"`
    Items []Item `json:"items"`
}
type Event struct {
    Type string `json:"type"` // item-added|item-removed|item-updated|menu
    Item *Item  `json:"item,omitempty"`
    Menu *Menu  `json:"menu,omitempty"`
}
type Menu struct {
    Revision int
    Entries  []MenuEntry
}
type MenuEntry struct {
    ID int32
    Label, IconName string
    Enabled, Visible, Separator, HasSubmenu, Checkmark, Radio bool
    ToggleState int32
    Children []MenuEntry
}
type Command struct {
    Type, Owner, Path string
    Delta int
    Orientation string // vertical|horizontal
    MenuID int32
    Event string // clicked|hovered
}
```

**Commit:** `test: add tray snapshot types`

---

### Task 3: Tray Unix client

**Files:**
- Create: `internal/ipc/trayclient/client.go`
- Test: `internal/ipc/trayclient/client_test.go` — unix listener, snapshot, item-updated, reconnect.

Same peer-cred and backoff as notifyclient. Path `$XDG_RUNTIME_DIR/sysc-tray/ipc.v1.sock`. `Send` for activate / secondary-activate / scroll / menu.open / menu.select.

**Commit:** `feat: dial sysc-tray snapshot socket`

---

### Task 4: Icon compose (attention + overlay)

**Files:**
- Create: `internal/shell/trayicon.go`
- Test: `internal/shell/trayicon_test.go`

```go
func Source(item trayproto.Item, lookup func(string, int) (image.Image, error), size int) image.Image
```

Priority: attention pixmap if NeedsAttention and non-empty, else icon pixmap, else lookup(attentionName|iconName). Overlay pixmap or lookup(overlayName) composited bottom-right at size/2. Nil lookup → 1-letter placeholder is **not** required; empty image + accessible Name is enough.

**Commit:** `feat: compose tray attention and overlay icons`

---

### Task 5: Tray bar widget

**Files:**
- Create: `internal/shell/widget_tray.go`
- Modify: config item id `tray` allowed in bar sections
- Test: `internal/shell/widget_tray_test.go` — N items, overflow when width insufficient, left-click sends activate, wheel sends scroll, right-click requests menu.

Row of `KindButton` nodes, size = bar inner height. Chevron when `hidden > 0`. `Handle` on icon: ButtonLeft → activate, ButtonRight → menu.open, ButtonMiddle → secondary-activate, axis → scroll. Name/Role on each.

**Commit:** `feat: render status notifier icons in the bar`

---

### Task 6: DBusMenu surface

**Files:**
- Create: `internal/platform/wayland/popup.go` (if Task 1 passed) **or** `internal/shell/popout_traymenu.go` (fallback)
- Test: protocol request order, owner-goroutine confinement, keyboard Next/Prev/Enter/Escape;
  `menu.select` on Enter; closing on outside click, Escape, output removal, popup failure, and service loss;
  bar keyboard restoration on every close path.

xdg_popup path: queue an owner-goroutine command that creates the popup surface and positioner, calls
`xdg_surface.get_popup(nil, positioner)`, assigns it with `barLayer.GetPopup(popup)`, grabs with the saved
input serial, and performs the initial commit in protocol order. Positioner gravity opposes the bar edge;
constraint adjustment is slide|flip. Switch the bar layer keyboard to OnDemand while open and restore
None on every close path. Roving focus traverses `MenuEntry`; submenu content replaces the list (stack +
Back), with no recursive popup in v1.

Fallback path: panel id `tray-menu`, Exclusive, KindMenu, anchored to the icon (4A placement). Same command wiring.

One menu process-wide (5B-3).

**Commit:** `feat: open keyboard-accessible tray DBusMenu`

---

### Task 7: Overflow drawer

**Files:**
- Create: `internal/shell/popout_traydrawer.go`
- Test: items that did not fit appear here; activate from drawer; Escape closes.

Panel `tray-drawer`, Exclusive, bar-anchored to the chevron. Grid/row of the same icon buttons as Task 5.

**Commit:** `feat: add tray overflow drawer`

---

### Task 8: Wiring + gate

**Files:**
- Modify: `cmd/sysc-shell/main.go` — trayclient goroutine
- Test: fake-compositor — owner replacement (same path, new unique owner) drops stale menu and shows one icon; malformed pixmap omits that icon only; reconnect restores item set without duplicates; bar exclusive zone unchanged.

Live checklist: real app (nm-applet or similar) registers; left-click; right-click menu; wheel; kill shell, items remain, restart shell they reappear; kill tray service, bar empties, restart service they return.

**Commit:** `feat: wire sysc-tray client and tray gate tests`

---

## Done when

- Registration, property updates, activate, scroll, menus, owner replacement, shell restart, malformed pixmap isolation all have tests.
- Live checklist ticked.
- Shell does not import D-Bus or claim StatusNotifierWatcher.

## Skipped

XEmbed. Hidden/pin lists. Tooltip surfaces (Name/Role only). Watcher implementation in-process. Nested xdg_popup submenus (in-menu stack instead).
