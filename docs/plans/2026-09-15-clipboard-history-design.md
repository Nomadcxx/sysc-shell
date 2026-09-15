# Clipboard History Design

This document records the approved design for `sysc-205`, the clipboard-history
slice of Milestone 7. It targets the observable behaviour of Noctalia v4 and
DMS; it does not preserve either project's configuration, plugin, QML, or
command-line interface.

## Scope and success criteria

`sysc-clipboard` is a standalone, first-party Go daemon. It owns the Wayland
clipboard selection, history reduction, encrypted storage, restore actions,
and a small local client protocol. `sysc-shell` is its first presenter and
does not own clipboard bytes or the storage files. Other clients can connect
later without changing the daemon's history model.

The slice is complete when:

- regular Wayland `CLIPBOARD` changes are captured as exact bytes with their
  selected MIME type;
- history survives a daemon restart in encrypted storage when a key is
  available, while a key or storage failure is visible as a volatile state;
- multiple same-user clients receive a coherent snapshot and ordered changes;
- restoring a history item makes the daemon the Wayland selection owner with
  the original bytes and MIME type;
- the shell's bar has a clipboard glyph, and both left- and right-click open or
  toggle the same keyboard-accessible history panel;
- the panel supports search, text and image previews, restore, pin/unpin,
  delete, clear-unpinned, clear-all confirmation, and explicit unavailable
  states;
- bounds, malformed clients, corrupt storage, missing Wayland capabilities,
  and disconnected clients cannot cause data loss, unbounded memory, or a
  stuck Wayland dispatch loop.

`PRIMARY_SELECTION`, virtual-keyboard auto-paste into the previously focused
application, and shell configuration compatibility are outside this slice.
Selecting an item restores the clipboard; it does not synthesize a keyboard
shortcut.

## Prior art and adopted lessons

The local prior-art review covered:

- Noctalia's `src/wayland/clipboard_service.*` and
  `src/shell/clipboard/*`: data-control protocol fallback, asynchronous offer
  reads, exact MIME/payload handling, encrypted per-entry persistence, bounded
  previews, pin ordering, and re-adoption of a live selection after its owner
  exits;
- DMS's `Modals/Clipboard/*`: centered modal presentation, focused search,
  virtualized list rows, image/text previews, keyboard navigation, clear
  confirmation, and accessible action labels;
- `golang.design/x/clipboard` v0.9.0: useful cross-backend clipboard
  transport, but no history, persistence, pinning, or daemon protocol;
- Copyzen: useful bbolt history ordering, byte-faithful entries, deduplication,
  pinning, and thumbnail ideas, but its recorder and picker depend on
  `wl-clipboard` and fuzzel rather than one long-lived Wayland owner;
- cliphist: mature external-command history behaviour, but the command-pipe
  boundary and serialized representation are a poor fit for exact MIME/image
  restore and for a reusable daemon.

The shell's existing inventory confirms the panel geometry and interaction
target in `docs/plans/2026-08-30-panels-and-controls-prior-art.md`; the
connectivity note confirms that a presenter widget routes to a peer service
and does not embed service state in the bar.

## Architecture

The system has three ownership layers:

```text
Wayland data-control globals
          |
          v
sysc-clipboard daemon
  Wayland owner goroutine  --->  bounded offer/source FD workers
          |                                  |
          v                                  v
  history reducer  <------  immutable capture results
          |
          +--> encrypted manifest and payload files
          +--> snapshot/delta broadcaster --> same-UID clients
                                             |
                                             v
                                  sysc-shell clipboard projection
                                  bar glyph + history panel
```

The daemon's Wayland connection and every Wayland proxy are owned by one
goroutine. It may hand ordinary file-descriptor I/O to bounded workers, but no
worker may call a Wayland request or touch a Wayland proxy. The history reducer
is the only writer of the in-memory history and the only component allowed to
turn a client command into a history mutation. A restore is a reducer command
followed by a command to the Wayland owner; the shell never receives a raw
payload.

The daemon is normally started by a per-user systemd service. The shell does
not launch or supervise it. A missing or restarting daemon makes the clipboard
widget/panel unavailable and does not prevent the rest of the shell from
starting.

The new repository exposes a small `client` package alongside
`cmd/sysc-clipboard`. The client package contains only the versioned socket
protocol and reconnecting transport; it does not expose daemon internals or
payload reads.

## History model and invariants

Each entry has:

| Field | Contract |
|---|---|
| `id` | Opaque random identifier; clients never address entries by list index. |
| `kind` | `text` or `image`; unsupported offers are ignored. |
| `mime` | MIME used for capture and restore. Text aliases are canonicalized only for comparison. Image MIME is retained exactly. |
| `offered_mime` | Bounded list of advertised types needed to restore common text aliases. |
| `size` | Exact payload byte count. |
| `sha256` | Hash of the exact stored bytes, used for integrity and deduplication. |
| `captured_at` | UTC wall-clock timestamp for display and persistence. |
| `preview` | Bounded, UTF-8-safe text preview; empty for images. |
| `pinned` | Whether normal eviction may remove the entry. |

The reducer maintains one ordered list: pinned entries form a contiguous block
at the front, newest pinned first, followed by newest unpinned entries. A new
capture is inserted at the front of the unpinned region. Capturing the same
payload again moves the matching unpinned entry to that position; it does not
create a duplicate. Text payloads with equivalent text MIME aliases compare as
the same entry. Image payloads require both exact bytes and exact MIME to
match. A capture matching a pinned entry is treated as the pinned entry's
existing selection and does not create an unpinned echo.

The hard limits are:

- 100 total entries;
- 256 MiB total stored payload;
- 4 MiB for one text entry;
- 32 MiB for one image entry;
- 200 MiB for the live-selection backup used for re-adoption;
- 200 bytes for a stored text preview.

Pinned entries are not selected as normal eviction victims, but they still
consume the hard total-entry and payload budgets. If pinning or retaining a
new capture would exceed a hard bound after all unpinned victims are removed,
the operation fails without changing the existing history. This keeps pinning
useful without turning it into an unbounded storage escape hatch.

The daemon may drop payload bytes from its resident cache after durable write;
the manifest metadata and encrypted payload remain the source of truth. A
missing or invalid payload makes only that entry unavailable and removes it
from the next published snapshot. A stale offer read is discarded when its
selection generation no longer matches the current Wayland selection.

## Encrypted persistence

The default state root is `$XDG_STATE_HOME/sysc-clipboard`, falling back to
`$HOME/.local/state/sysc-clipboard`. It contains:

```text
manifest.enc
entries/<opaque-id>.enc
```

The directory and files are private (`0700`/`0600`). The manifest contains a
format version and only validated metadata. Each payload is encrypted
separately with AES-256-GCM from the Go standard library. The manifest and
payloads use distinct authenticated purposes and include the entry ID as
associated data, so a payload cannot be silently swapped between entries.

The daemon obtains its 32-byte data key from the Secret Service by default.
An explicit `--key-file` fallback is available for headless sessions; the file
must be a regular `0600` file containing exactly 32 bytes, and a newly created
file is generated with private permissions. There is no plaintext storage or
automatic insecure fallback. If neither key source is usable, capture and
restore continue in memory where possible and the public persistence state is
`unavailable`/`volatile`.

Writes are staged and atomically renamed. The active manifest is not replaced
until all new payload writes and the new manifest have succeeded. A failed
write leaves the previous durable state intact and marks newly captured state
volatile. Orphaned encrypted payload files are harmless and are garbage
collected only after a successful manifest commit.

At startup:

- a valid manifest is loaded and each entry is validated against the limits;
- an invalid manifest is renamed to a timestamped `.corrupt` file and is
  preserved for recovery rather than overwritten;
- a corrupt or missing individual payload removes only that item from the
  usable history;
- a missing key or locked Secret Service does not overwrite existing files;
- recovery and later successful writes publish the persistence state to
  clients.

## Local protocol

The socket is `$XDG_RUNTIME_DIR/sysc-clipboard/control.v1.sock`. Its containing
directory is `0700`, the socket is `0600`, and the daemon checks Linux
`SO_PEERCRED` before accepting a client. Only the daemon's UID is authorized.
The filesystem mode is defense in depth, not the authorization decision.

The stream uses a four-byte big-endian length followed by one JSON object. A
frame is at most 1 MiB; zero-length, oversized, malformed, or deeply nested
JSON is rejected and the client is disconnected. Every object carries
`version: 1` and `type`. IDs, strings, and array lengths are bounded before
they reach the reducer.

Handshake and state flow:

1. The client sends `hello` with protocol version 1.
2. The daemon replies with `hello` containing its version and capability/state
   flags, then sends one complete `snapshot` with a monotonically increasing
   `revision`.
3. Mutations produce an acknowledgement and a subsequent ordered `delta`.
4. A client that detects a revision gap or receives `resync_required` sends
   `resync` and receives a fresh snapshot. Each client has a bounded outgoing
   queue; a slow client is coalesced to resynchronization rather than causing
   daemon memory growth.

The v1 commands are:

| Command | Effect |
|---|---|
| `restore {id}` | Make the entry the current regular clipboard selection. |
| `pin {id, pinned}` | Pin or unpin by opaque ID, subject to hard bounds. |
| `delete {id}` | Delete one entry and its encrypted payload after the next durable commit. |
| `clear {scope: "unpinned"\|"all"}` | Clear the selected scope; the panel confirms destructive actions. |
| `thumbnail {id, max_px}` | Return a bounded daemon-generated PNG thumbnail, never the raw payload. |
| `resync` | Return a complete snapshot at the current revision. |

Snapshot entries contain metadata and previews only. There is deliberately no
raw-payload stream in v1: restore and thumbnail work stay inside the daemon,
which keeps future clients from accidentally becoming alternate storage
owners.

Errors are structured (`code`, bounded human message, and optional command
ID), including `unsupported`, `not_found`, `limit`, `unavailable`,
`persistence`, `protocol`, and `conflict`. A failed command does not advance
the history revision.

## Wayland capture and restore

The daemon generates Go bindings for the two data-control protocols from the
upstream XML files stored with their licence notices:

1. bind `ext_data_control_manager_v1` when advertised, at the minimum of the
   server version and the implemented version;
2. otherwise bind `zwlr_data_control_manager_v1` at the implemented maximum;
3. create one data device for the first usable `wl_seat`;
4. ignore primary-selection events and expose the absence of either manager or
   seat as an explicit capability failure.

On a new regular selection the Wayland owner records an offer generation and
its advertised MIME types. It prefers UTF-8/plain-text formats for text and
common image formats for images, requests the selected format through the
offer FD, and gives the FD to a worker that reads at most the applicable limit
plus one byte. The reducer accepts the result only if it reached EOF, passed
the kind/size rules, and still belongs to the current generation. Empty text
does not enter history.

The daemon keeps the latest complete live selection as a separately bounded
backup. When the current external owner disappears, it reoffers that backup
through a new data source when it fits the 200 MiB ceiling. Re-adoption does
not create a duplicate history entry. A selection too large to back up is
allowed to disappear; it must not make the daemon retain unbounded bytes.

Restore loads the exact payload off the Wayland owner goroutine, creates a
data source, advertises the retained MIME (plus safe text aliases where
appropriate), and streams the bytes on source-send FD callbacks. The source
and immutable payload stay alive until the compositor cancels them. The owner
does not block on disk reads or FD writes. The resulting self-selection is
recognized by the normal deduplication rules.

If the Wayland connection or data-control manager is unavailable, the daemon
can still serve loaded history and persistence status. Capture and restore
return a structured `unavailable` error until the connection is usable again.

## Shell presenter

`sysc-shell` adds a `clipboard` bar item backed by the daemon's metadata
projection. The default bar includes a `content_paste` Material glyph; its
tooltip reports the history count and unavailable/persistence state. The item
uses one `panel:clipboard` action, and both left and right button releases
toggle the same panel. No history bytes are retained in the bar widget.

The panel is a floating, exclusive-keyboard `720x560` surface using the
existing `PanelHost`, `PanelSet`, placement, focus, scrolling, and theme
machinery. It is centered like the reference modals, clamps to the output, and
does not introduce a second panel framework. Opening resets the query and
focuses the search field. The list is virtualized and keyed by daemon IDs so a
concurrent delta cannot turn a visible row's old index into a different item.

Each row exposes an accessible restore action, a text preview or image
thumbnail, size/time metadata, and a pin indicator. The selected item has a
preview area with restore, pin/unpin, and delete actions. Search is a
case-insensitive bounded substring over the daemon preview/MIME label.
Keyboard navigation covers Up/Down, Home/End, Page Up/Down, Enter to restore,
Delete with confirmation, and a documented pin toggle. Clear-unpinned and
clear-all are separately labelled and confirmed. Empty history, no matches,
daemon disconnect, Wayland unavailability, and persistence failure are
rendered as useful states rather than blank panels.

The shell client reconnects with bounded backoff, applies only snapshots and
in-order deltas, and requests `resync` after a gap. It invalidates the bar and
open panel only when the projected state changes. Closing the panel releases
thumbnail data; the daemon remains the sole durable owner.

## Verification gates

The daemon must leave focused runnable checks for:

- history ordering, text-alias deduplication, exact image identity, pinning,
  all hard bounds, and stale-capture rejection;
- length-prefixed protocol framing, peer-UID authorization, bounded queues,
  snapshot/delta revision recovery, and command validation;
- AES-GCM round trips, private permissions, atomic-write rollback, corrupt
  manifest preservation, and isolated corrupt-payload removal;
- fake data-control offer/source state transitions, cancellation, EOF/limit
  handling, and restore MIME advertisement.

The shell must check the clipboard widget's action and both button paths, the
panel's ID-based projection and unavailable states, and the 720x560 tree at
the real layout size. Repository gates are `gofmt`, `go vet`, `go test -race`,
and `go build` in both repositories, followed by a local pinned-module probe
before the daemon tag is consumed.

The live Niri gate uses the environment from `AGENTS.md` and confirms: daemon
startup, text capture, image capture/thumbnail, restore, duplicate suppression,
pin and clear semantics, daemon restart with encrypted history, shell left and
right click, a second simultaneous client, daemon disconnect/reconnect, and
absence of plaintext payload files. The available machine has one output, so
the two-output portion remains explicitly unrunnable there.
