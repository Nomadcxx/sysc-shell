# Notifications Foundation Implementation Plan — Milestone 5, Tranche 5A

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Ship Milestone 5 notification presentation: dial sysc-notify, restore a snapshot after reconnect, render Overlay toasts (actions, images, progress, urgency, expiry, dismiss, swipe, inline-reply), a notification-center panel, DND, and unambiguous-only PID focus.

**Architecture:** Shell is a presentation client on the notify Unix socket (D3). One Overlay aux surface per output stacks cards (D7/D8). `internal/icons` decodes pixels and resolves icon names. History is notify-owned (D13); the shell only renders snapshot `history[]`. PID matching is shell-side against the cached Niri window list.

**Tech Stack:** Go 1.26 stdlib, `x/image` already in go.mod, sysc-wayland layer-shell, tagged `sysc-notify`, M4 aux surfaces + panel machinery + 4B text field.

**Design:** [2026-08-30-notifications-foundation-design.md](2026-08-30-notifications-foundation-design.md)
**Persist:** [2026-08-30-sysc-notify-persistence-design.md](2026-08-30-sysc-notify-persistence-design.md)
**Research:** [2026-08-30-notifications-and-tray-research.md](2026-08-30-notifications-and-tray-research.md)

---

## Prerequisites (verify before Task 1)

1. M2 live Niri gate passed and merged. M3 and M4 (4A+4B) merged. This plan executes from main containing those.
2. Merged tree has M4 aux (`Callbacks.Aux`, `AuxSpec`, `DropAux`), panel host, 4B text field / text-input-v3, `ui.Handle`, `icons` **not** yet present.
3. A reviewed sysc-notify implementation plan has implemented the service-owned socket contract and the
   persistence addendum. A tagged `sysc-notify` release exists with the version handshake,
   snapshot+deltas, `sender_pid`, and D13 `active[]`+`history[]` capability. Tasks 2–3 may land before
   that tag because they do not consume the wire contract; the completed tranche and Task 12 may not degrade.
4. Commit messages must not contain AI-tool substrings.

If the tagged socket framing differs from newline JSON, change Task 4 only.

---

### Task 1: Freeze the notify snapshot types

**Files:**
- Create: `internal/ipc/notifyproto/types.go`
- Test: `internal/ipc/notifyproto/types_test.go`

**Step 1: Failing test** — marshal/unmarshal a snapshot with one active notification (actions, image-data bounds, sender_pid, urgency, expire_timeout, value hint) and one history entry.

**Step 2: Types**

```go
package notifyproto

type Snapshot struct {
    Type    string         `json:"type"`
    Active  []Notification `json:"active"`
    History []HistoryEntry `json:"history"`
}

type Notification struct {
    ID            uint32            `json:"id"`
    AppName       string            `json:"app_name"`
    AppIcon       string            `json:"app_icon"`
    DesktopEntry  string            `json:"desktop_entry"`
    Summary       string            `json:"summary"`
    Body          string            `json:"body"`
    Actions       [][2]string       `json:"actions"` // key, label
    Urgency       uint8             `json:"urgency"`
    Category      string            `json:"category"`
    Transient     bool              `json:"transient"`
    ExpireTimeout int32             `json:"expire_timeout"` // spec: <0 default, 0 persistent
    Value         *float64          `json:"value,omitempty"` // 0–100
    Image         *RawImage         `json:"image,omitempty"`
    SenderPID     int               `json:"sender_pid"`
    SenderParents []int             `json:"sender_parents,omitempty"`
}

type RawImage struct {
    Width    int    `json:"width"`
    Height   int    `json:"height"`
    Stride   int    `json:"stride"`
    HasAlpha bool   `json:"has_alpha"`
    Bits     int    `json:"bits"`
    Channels int    `json:"channels"`
    Data     []byte `json:"data"`
}

type HistoryEntry struct {
    ID           uint32 `json:"id"`
    AppName      string `json:"app_name"`
    AppIcon      string `json:"app_icon"`
    DesktopEntry string `json:"desktop_entry"`
    Summary      string `json:"summary"`
    Body         string `json:"body"`
    Urgency      uint8  `json:"urgency"`
    Timestamp    string `json:"timestamp"`
    Image        string `json:"image,omitempty"`
}

type Event struct {
    Type string          `json:"type"` // added|replaced|closed|history-added|history-removed|history-cleared
    N    *Notification   `json:"notification,omitempty"`
    H    *HistoryEntry   `json:"history,omitempty"`
    ID   uint32          `json:"id,omitempty"`
    Reason uint32        `json:"reason,omitempty"`
}

type Command struct {
    Type   string `json:"type"` // action.invoke|dismiss|inline-reply|history.clear
    ID     uint32 `json:"id,omitempty"`
    Key    string `json:"key,omitempty"`
    Text   string `json:"text,omitempty"`
}
```

**Step 3:** `go test ./internal/ipc/notifyproto/ -count=1`

**Step 4:** Commit `test: add notify snapshot types`

---

### Task 2: Decode raw image-data and files

**Files:**
- Create: `internal/icons/decode.go`
- Test: `internal/icons/decode_test.go`

**Step 1:** Tests — 2×2 RGB, 2×2 RGBA, negative and over-limit dimensions, overflow in
width×channels and stride×height, short and trailing raw data, inconsistent alpha/channels, bits≠8,
channels=2, encoded file over the byte limit, image header over the dimension/pixel limit, truncated PNG,
and a PNG file round-trip.

```go
func DecodeRaw(width, height, stride int, hasAlpha bool, bits, channels int, data []byte) (image.Image, error) {
    if bits != 8 || (channels != 3 && channels != 4) {
        return nil, errFormat
    }
    row, ok := mul(width, channels)
    if !ok || width <= 0 || height <= 0 || overLimits(width, height) || stride < row || stride > maxStride {
        return nil, errFormat
    }
    need, ok := mul(stride, height)
    if !ok || need > maxDecodedBytes || len(data) != need {
        return nil, errFormat
    }
    // Reject hasAlpha/channels disagreement, then copy into image.RGBA.
}
```

Use a small checked integer multiply helper. `DecodeFile` first limits encoded bytes, then uses
`image.DecodeConfig` to enforce dimension, pixel-work, and decoded-memory limits before `image.Decode`.
The released service limits are the upper bound; the shell repeats the checks at its own trust boundary.

**Step 2:** `go test ./internal/icons/ -count=1`

**Step 3:** Commit `feat: decode notification image-data`

---

### Task 3: Freedesktop icon-theme lookup

**Files:**
- Create: `internal/icons/lookup.go`
- Test: `internal/icons/lookup_test.go` with fixture themes covering `Inherits`, fixed/scalable/threshold
  directory metadata, scale, hicolor fallback, traversal attempts, inheritance cycles, and a symlink escape.

```go
func Lookup(name string, size, scale int, roots []string, theme string) (string, error)
```

Reject empty names and traversal. For icon names, parse each theme's declared directories and size/scale
rules, walk an acyclic `Inherits` chain, append `hicolor`, and search pixmaps last. Resolve symlinks and
accept a candidate only when it remains under the root being searched. Explicit absolute image paths are
handled by the notification image-path policy, not by theme-name lookup. Prefer a drawable PNG at the
best declared size; an SVG-only match falls back until the approved rasterizer exists.

Do not call gsettings in the unit test; pass `theme` in. Production wrapper reads gsettings then gtk settings.ini then `"hicolor"`.
Cache `(source identity, size, scale, theme generation)` in a bounded LRU and invalidate it when theme or
root state changes.

**Commit:** `feat: resolve icon theme names`

---

### Task 4: Notify Unix client

**Files:**
- Create: `internal/ipc/notifyclient/client.go`
- Test: `internal/ipc/notifyclient/client_test.go` — `net.Listen("unix")`, write a snapshot, assert `Active`/`History`; write `added`; close server, assert reconnect handler gets a second snapshot.

```go
type Client struct {
    Path string
    OnSnapshot func(notifyproto.Snapshot)
    OnEvent    func(notifyproto.Event)
}

func (c *Client) Run(ctx context.Context) error // dial loop, peer uid == os.Getuid(), bufio.Scanner
func (c *Client) Send(cmd notifyproto.Command) error
```

Dial `$XDG_RUNTIME_DIR/sysc-notify/ipc.v1.sock` if Path empty. Missing socket: log, backoff 1s, no fatal. Bound line length 1 MiB; oversize → close.

**Commit:** `feat: dial sysc-notify snapshot socket`

---

### Task 5: Markup sanitize + expiry policy

**Files:**
- Create: `internal/shell/notifytext.go`, `internal/shell/notifyexpiry.go`
- Test: `internal/shell/notifytext_test.go`, `internal/shell/notifyexpiry_test.go`

Sanitize: `<br>`, `<br/>`, `<br />` → `\n`; `&lt;&gt;&amp;&quot;&apos;`; strip other tags; keep inner text.

Expiry: `timeoutMs(expireTimeout, urgency) int` — `<0` → 5000/5000/0 by urgency; `0` → 0 (persistent); `>0` → that value.

**Commit:** `feat: sanitize notification markup and expiry defaults`

---

### Task 6: Toast stack geometry (pure)

**Files:**
- Create: `internal/shell/toaststack.go`
- Test: `internal/shell/toaststack_test.go`

```go
type Stack struct {
    Edge     string // top|bottom
    OutputH  int
    Pad, Gap int
    CardH    []int // visible + queued
}

func (s Stack) Place() []int // y for each visible card; len may be < len(CardH)
func (s Stack) VisibleCount() int
```

Overflow: later cards not placed (queued). `top` stacks down from Pad; `bottom` stacks up from OutputH-Pad.

**Commit:** `feat: stack notification cards per output`

---

### Task 7: Toast host on aux surfaces

**Files:**
- Create: `internal/shell/notifyhost.go`
- Modify: `internal/shell/registry.go` — `AttachNotify(*notifyclient.Client)`, on snapshot/event rebuild cards, `AuxRequest` open `notify:<global>` Overlay None exclusive −1 when any visible card exists, close when zero.
- Test: `internal/shell/notifyhost_test.go` with fake aux (no compositor): added → open; closed last → close; DND true → no open.

Input region = union of card rects. Keyboard None. Position token default top-right, offset bar zone + pad 8.

Render: shadow + rounded card (M4 masks) + icon + texts + countdown + optional value bar + action buttons. Reduced-motion: no enter slide.

**Commit:** `feat: show notification toasts on overlay aux`

---

### Task 8: Pointer — default action, dismiss, swipe, hover-pause

**Files:**
- Modify: `internal/shell/notifyhost.go`
- Test: `internal/shell/notifyhost_pointer_test.go`

Left-click body → `action.invoke` key `default` if present else `dismiss`. Close button → dismiss. Drag dx > 0.35×cardW → dismiss. Pointer enter card → pause that card's timer; leave → resume remaining. Send `notifyclient.Command`.

**Commit:** `feat: dismiss and invoke notification toasts`

---

### Task 9: Inline-reply OnDemand

**Files:**
- Modify: `internal/shell/notifyhost.go`, wayland aux keyboard field
- Test: `internal/shell/notifyhost_reply_test.go`

If actions contain `inline-reply`, render 4B text field instead of that button. Focus field → set that aux `Keyboard` to OnDemand (re-apply layer keyboard interactivity). Submit → `inline-reply` command + dismiss. Blur/close → Keyboard None.

Live note: input region must include the field.

**Commit:** `feat: inline-reply on notification toasts`

---

### Task 10: PID matcher

**Files:**
- Create: `internal/shell/notifyfocus.go`
- Test: `internal/shell/notifyfocus_test.go`

```go
func MatchWindow(senderPID int, parents []int, windows []niri.Window) (id uint64, ok bool)
```

`ok` only when exactly one window pid is in the set {senderPID}+parents. Registry stores map[uint32]uint64 only when ok. Activating a notification with no mapping does nothing to focus. Activating with a mapping: `niri msg action focus-window --id` (or the existing niri client action). Ambiguous stored as absent.

**Commit:** `feat: focus unambiguous notification sender windows`

---

### Task 11: DND

**Files:**
- Modify: `internal/config/config.go` — `Session.DoNotDisturb bool`, `DoNotDisturbUntil int64`
- Modify: `internal/shell/notifyhost.go` — skip display when DND
- Test: `internal/shell/notifyhost_dnd_test.go`

Until>now counts as DND; a 1s ticker clears expired until. History events still applied.

**Commit:** `feat: suppress notification toasts during DND`

---

### Task 12: Notification center panel

**Files:**
- Create: `internal/shell/popout_notifications.go`
- Modify: panel ID set + IPC `panel.toggle` already knows `settings`; add `notifications`
- Test: `internal/shell/popout_notifications_test.go` — grouping, clear-all sends `history.clear`, keyboard traversal names/roles, DND toggle in header.

Centered, Exclusive, ~400×620, virtual list of history groups. Opening sets toast host `overlayOpen` (hide toasts). No action buttons on history rows.

**Commit:** `feat: add notifications center panel`

---

### Task 13: Wiring + fake-compositor gate

**Files:**
- Modify: `cmd/sysc-shell/main.go` — start notifyclient in a ctx goroutine
- Modify: `docs/niri-hotkeys.md` — Super+N → `sysc-shell ipc panel.toggle {"panel":"notifications"}`
- Test: fake-compositor — exclusive zone of bar unchanged while toasts visible; accessible names on action buttons; reduced-motion skips slide; malformed image still shows summary.

Live checklist (do not automate): reconnect restores toasts; `Notify` works with shell killed; ambiguous two-pid case does not focus; inline-reply OnDemand types; Overlay over fullscreen.

**Commit:** `feat: wire sysc-notify client and notification gate tests`

---

## sysc-notify repo (persistence) — do first if the tag lacks D13

Work in `/home/nomadx/sysc-notify`, not this worktree.

1. Write `history.json` 0600 + PNG sidecars as in the persist addendum.
2. Snapshot includes `history`.
3. Tests: restart, cap 100, transient skip, bad JSON load.
4. Tag a release; pin that tag in sysc-shell.

---

## Done when

- Replacement, actions, dismiss, expiry, hover-pause, overflow pause, swipe, inline-reply, DND, center, unambiguous-only focus, reconnect snapshot all have tests.
- Live checklist above ticked on Niri.
- No Wayland imports in sysc-notify.

## Skipped

Tray (5B). `xdg_activation_v1`. Sound. HTML body renderer. Per-app filters. Control-center tab. Shell-owned history file.
