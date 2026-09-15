# Clipboard history completion handover

Date: 2026-09-16.

Design: `2026-09-15-clipboard-history-design.md`.
Plan: `2026-09-15-clipboard-history.md`.
Tracker: `sysc-205`, `sysc-290`, `sysc-291`, `sysc-292`.

## Delivered

- `sysc-clipboard` is a standalone Go daemon. It owns Wayland data-control
  capture and restore, bounded history reduction, AES-GCM persistence, the
  private versioned client socket, and daemon-generated thumbnails.
- `sysc-shell` projects metadata only. The default right bar lane carries the
  `content_paste` glyph and opens the same `720x560` floating,
  exclusive-keyboard panel from either button release.
- The panel has focused search, ID-keyed virtualized rows, text and image
  previews, restore, pin/unpin, delete, clear-unpinned, confirmed clear-all,
  keyboard navigation, reconnect/error states, and bounded thumbnail lifetime.
- A live render check found the panel's text-row `content_copy` glyph missing
  from the embedded Material subset. The authoring inventory, Go inventory,
  font artifact, and regression check now agree.

## Commits

| Repository | Commit | Scope |
|---|---|---|
| `sysc-clipboard` | `ea2e979` (`v0.1.0`) | Qualified daemon release. |
| `sysc-shell` | `1083838` | Clipboard bar projection. |
| `sysc-shell` | `6ec1118` | Clipboard history panel. |
| `sysc-shell` | `e7e9b04` | Reconnect and daemon-error handling. |
| `sysc-shell` | `3b9e43a` | IPC reachability and Material subset correction. |

## Automated evidence

The daemon worktree was clean at `ea2e979`. Fresh bounded checks passed:

```text
GOMAXPROCS=4 go vet -p 2 ./...
GOMAXPROCS=4 go build -p 2 ./...
per-package GOMAXPROCS=4 go test -p 1 -race -count=1 ./...  [7 packages]
```

The shell worktree passed `gofmt`, `go vet -p 2 ./...`,
`go build -p 2 ./cmd/sysc-shell`, `go test -p 2 -count=1 ./...`,
`git diff --check`, and the `go.mod`/`go.sum` diff check. Clipboard-specific
race checks passed for `internal/config`, `internal/ipc`, `internal/render`,
and `internal/shell`.

The machine safety hook rejected the all-package `-race` command because it
has previously exhausted this host's zram-only swap. The shell package race
run was therefore kept separate and remains red on an existing media-relay
setup race: `NewRegistry` starts `relayMedia` before tests finish populating
`reg.bars`. The same `TestViewLockedServesCachedAudio` failure reproduces at
baseline `a985317`; this is tracked as `sysc-310` and is outside clipboard
ownership.

The regenerated Material artifact is 22,308 bytes, 98 glyphs, with SHA-256:

```text
032a4b3c238449bdfa2ce7cab7dd14a608397db7e1c58bca4cc02f47f5807781
```

## Live Niri evidence

The qualified run used Niri with `DP-3` at logical x=0 and `DP-1` at logical
x=2560, both scale 1.0. Bars mapped on both outputs. Fresh IPC smoke testing
closed the clipboard panel, observed no panel layer, opened it again, and
observed `sysc-shell-panel` on `DP-1` at `Overlay` with `Exclusive` keyboard
interactivity. The final close removed the panel layer.

The history run passed text capture, text-alias deduplication, restore,
pin/unpin, delete, clear, daemon disconnect/reconnect, encrypted restart
recovery, image capture and thumbnail generation, MIME-preserving PNG restore,
and a second simultaneous client. The restored PNG hash was
`0539ccf3351a4428c123f669bf3443c12d669d12f890bda3722fe97932b43cc8`.

The runtime socket was `0600`. The inspected state root contained only
`manifest.enc` and per-entry `.enc` files; no plaintext payload files were
present.

## Open qualification items

- `sysc-311` records the live pointer limitation: installed `ydotool` attempts
  did not reliably reach the bar input region. Unit tests cover button codes
  272 and 273, and IPC/keyboard panel paths passed. A real pointer or working
  uinput path still needs to exercise the physical left/right click route.
- `sysc-310` records the unrelated shell media-relay race described above.
- `PRIMARY_SELECTION`, auto-paste, configuration migration, system-wide
  installation, and arbitrary compositor support remain outside v1.

Leave this snapshot unchanged. Put later corrections and live observations in
Beads or a new document.
