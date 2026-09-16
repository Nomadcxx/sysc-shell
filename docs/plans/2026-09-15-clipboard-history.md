# Clipboard History Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Ship a first-party sysc-clipboard daemon and a sysc-shell presenter with byte-faithful Wayland clipboard history, encrypted persistence, restore/pin/delete/clear actions, thumbnails, and a keyboard-accessible panel opened by either bar button.

**Architecture:** sysc-clipboard owns the Wayland data-control connection, bounded history reducer, encrypted state, and private versioned Unix-socket protocol. sysc-shell imports only the daemon client package, keeps a metadata projection in Registry, and renders the existing PanelHost and ui.Node tree. The daemon Wayland owner is one goroutine; socket readers, persistence, thumbnail decoding, and source/offer FD transfers are bounded ordinary-I/O workers.

**Tech Stack:** Go 1.26+, standard-library crypto, encoding/json, image, net, and os packages; pinned github.com/Nomadcxx/sysc-wayland v0.2.2; pinned github.com/godbus/dbus/v5 for Secret Service; existing sysc-shell bar, panel, theme, focus, scrolling, and image primitives.

---

## Scope and execution rules

- Run bd only from /home/nomadx/sysc-shell. Product changes use dedicated worktrees.
- The empty sysc-clipboard remote needs an initial repository commit before its worktree can be created.
- Commit this plan and its register row before product code. Commit .beads/issues.jsonl with the plan and issue-state changes.
- Keep PRIMARY_SELECTION, virtual-keyboard auto-paste, raw payload streaming, shell configuration compatibility, and arbitrary compositor support out of v1.
- Do not add a shell-side payload cache or a second panel framework.
- Every non-trivial reducer, framing, persistence, client projection, and layout rule gets a focused runnable test before implementation. Wayland behavior gets fake-proxy tests plus the live gate.
- Final repository gates are gofmt, go vet ./..., go test -race -count=1 ./..., and go build ./.... The intentional daemon module pin is isolated to its release task.

## Fixed public contract

The daemon module exposes protocol and client; daemon internals remain private. Protocol Entry is metadata only: opaque ID, text or image kind, capture MIME, bounded offered MIME list, exact size, SHA-256, UTC capture time, bounded text preview, and pin state. Snapshot carries a revision, metadata entries, persistence state durable/volatile/unavailable, and Wayland state ready/unavailable. Messages carry version 1 and one of hello, snapshot, delta, acknowledgement, error, restore, pin, delete, clear, thumbnail, or resync.

Frames use a four-byte big-endian length followed by JSON. MaxFrame is 1 MiB. Zero-length, oversized, malformed, deeply nested, or over-bound messages disconnect the peer. There is no raw payload stream.

### Task 1: Initialize the daemon module and protocol sources

**Files:**
- Create in /home/nomadx/sysc-clipboard: go.mod, NOTICE, protocols/ext-data-control-v1.xml, protocols/wlr-data-control-unstable-v1.xml, internal/wayland/generate.go.
- Generate: internal/wayland/ext_data_control.go and internal/wayland/wlr_data_control.go.
- Test: internal/wayland/generate_test.go or a package compile check.

**Steps:**

1. Write go.mod with Go 1.26, sysc-wayland v0.2.2, godbus/dbus/v5 at a pinned version, and no other runtime dependency.
2. Copy the exact upstream XML sources named by the approved design, preserve copyright blocks, and record their provenance in NOTICE.
3. Write a failing compile check for both generated packages and their interface-name constants.
4. Run go test ./internal/wayland and observe failure because generated bindings are absent.
5. Add go:generate commands using the pinned sysc-wayland scanner. Check generated files in so normal builds do not need network access.
6. Run the focused test, go vet ./..., and go test ./....
7. Commit chore: initialize clipboard daemon.

### Task 2: Build protocol/framing and the pure history reducer with TDD

**Files:**
- Create /home/nomadx/sysc-clipboard/protocol/protocol.go.
- Create /home/nomadx/sysc-clipboard/protocol/frame.go, protocol/protocol_test.go, and protocol/frame_test.go.
- Create /home/nomadx/sysc-clipboard/internal/history/history.go and internal/history/history_test.go.

**Steps:**

1. Write table tests for valid and invalid versions/types, bounded IDs/MIME/preview fields, zero/oversized/malformed frames, and frames split across reads.
2. Run go test ./protocol -run TestFrame and observe the missing implementation failure.
3. Implement io.ReadFull framing, the 1 MiB frame limit, JSON validation, and a WriteFrame limit check.
4. Run the protocol tests and keep them green.
5. Write history table tests for newest-first order; text MIME aliases text/plain, text/plain;charset=utf-8, and UTF8_STRING deduplication; exact image MIME and byte identity; pinned newest-first prefix; unpinned eviction at 100 entries and 256 MiB; text/image per-entry limits; UTF-8-safe 200-byte preview; duplicate pinned captures; and stale generation rejection.
6. Run go test ./internal/history -run TestHistory and observe the missing reducer failure.
7. Implement the reducer with immutable records, text-only comparison canonicalization, exact image identity, pinned-prefix insertion, unpinned-only eviction, and all-or-nothing limit failure.
8. Add metadata snapshots and ordered Change values. Return copied bytes or immutable records; never expose a mutable internal payload.
9. Run go test ./protocol ./internal/history.
10. Commit feat: add clipboard protocol and history.

### Task 3: Add encrypted persistence and key loading

**Files:**
- Create /home/nomadx/sysc-clipboard/internal/store/key.go and key_test.go.
- Create /home/nomadx/sysc-clipboard/internal/store/store.go and store_test.go.
- Modify history only for a narrow load/commit hook if required.

**Steps:**

1. Write tests for AES-256-GCM round trips, wrong-key/authentication errors, entry-ID associated-data mismatch, private 0700/0600 permissions, a valid 32-byte key file, rejection of symlink/non-regular/wrong-permission/wrong-length key files, corrupt-manifest preservation, isolated corrupt-payload removal, and atomic rollback on staged-write failure.
2. Run go test ./internal/store and observe missing implementation failures.
3. Resolve the state root from XDG_STATE_HOME/sysc-clipboard, then HOME/.local/state/sysc-clipboard. Create only private directories.
4. Implement Secret Service lookup/create of one per-user key item over the session bus. An unavailable or locked service is unavailable; no plaintext or automatic insecure fallback.
5. Implement explicit key-file loading/creation with O_EXCL, 0600, exactly 32 bytes, and symlink checks.
6. Encrypt the manifest and each payload separately with distinct purpose prefixes and entry-ID associated data. Stage, sync, rename payloads, then rename the manifest; preserve the old manifest on failure.
7. Load only validated metadata. Preserve an invalid manifest as a timestamped corrupt file. Drop only entries whose individual payload fails authentication.
8. Run go test -race ./internal/store ./internal/history and commit feat: encrypt clipboard state.

### Task 4: Implement the reducer service and private client server

**Files:**
- Create /home/nomadx/sysc-clipboard/internal/daemon/service.go and service_test.go.
- Create /home/nomadx/sysc-clipboard/internal/daemon/server.go and server_test.go.

**Steps:**

1. Write tests for startup snapshot, capture add delta, duplicate move delta, restore/pin/delete/clear effects, failed-command revision stability, volatile persistence, and a slow subscriber coalescing to one resync marker.
2. Run the focused tests and observe missing service failures.
3. Implement one reducer goroutine receiving capture results and validated commands. It owns revisions and is the only writer of history. Persist after mutations; failed writes publish volatile without replacing the old durable manifest.
4. Serve XDG_RUNTIME_DIR/sysc-clipboard/control.v1.sock with private parent/socket modes, same-UID stale-socket handling, SO_PEERCRED authorization, hello/version handshake, initial snapshot, bounded per-client output, and disconnect on malformed input.
5. Publish ordered deltas. On output overflow discard pending deltas and enqueue one current snapshot/resync-required message.
6. Generate bounded PNG thumbnails inside the daemon using standard image decoders and nearest-neighbor scaling. Return structured unsupported or limit errors.
7. Run go test -race ./internal/daemon ./internal/store ./internal/history, go vet ./..., and go test ./....
8. Commit feat: serve clipboard history clients.

### Task 5: Add the one-goroutine Wayland capture/restore owner

**Files:**
- Create /home/nomadx/sysc-clipboard/internal/wayland/owner.go and owner_test.go.
- Create /home/nomadx/sysc-clipboard/internal/wayland/fd.go and fd_test.go when the FD helper is not covered by owner tests.
- Modify generated files only by regeneration.

**Steps:**

1. Write fake-proxy tests for ext-before-wlr manager preference, server-version clamping, first-seat binding, ignored primary-selection events, MIME capture, text/image MIME choice, EOF-required bounded reads, stale-generation discard, restore MIME advertisements, source cancellation lifetime, and re-adoption without a duplicate.
2. Run the focused tests and observe missing owner failures.
3. Discover globals during the initial roundtrip, bind the preferred manager and first wl_seat, create one data device, and expose ready/unavailable without disabling loaded history.
4. Run dispatch on one goroutine. Use a short read deadline to service a bounded command channel without another goroutine touching a proxy.
5. On regular selection, record generation/MIME types, receive through a pipe, close the local write end, and hand only the read end to a bounded worker. Read at most the kind limit plus one byte and require EOF.
6. On restore, create a fresh source, offer retained MIME plus safe text aliases, set selection, and write source-send FDs from bounded workers. Keep source state until cancellation and never block dispatch on disk or FD writes.
7. Keep a separate live-selection backup capped at 200 MiB. Re-adopt it after an external owner disappears; reject oversized backups.
8. Run owner tests, go test -race ./internal/wayland ./internal/history, and go vet ./....
9. Commit feat: own the Wayland clipboard.

### Task 6: Publish client, command, service unit, and qualified release

**Files:**
- Create /home/nomadx/sysc-clipboard/client/client.go, client/socket.go, and client/client_test.go.
- Create /home/nomadx/sysc-clipboard/cmd/sysc-clipboard/main.go and contrib/sysc-clipboard.service.
- Modify /home/nomadx/sysc-clipboard/README.md.

**Steps:**

1. Write temporary-Unix-server tests for handshake, snapshot application, ordered deltas, gap-triggered resync, bounded reconnect backoff, unavailable state, bounded command queue, and malformed server messages.
2. Run the focused tests and observe the missing client failure.
3. Implement Client.Run, Updates, and non-blocking Restore, Pin, Delete, Clear, Thumbnail, and Resync. Store metadata only; apply snapshots atomically; reject out-of-order deltas.
4. Add CLI flags for key-file, state-dir, and diagnostic check. Normal execution loads persistence, starts the service and Wayland owner, and stops on SIGTERM/SIGINT.
5. Add a per-user systemd unit and document that sysc-shell does not launch the daemon.
6. Run gofmt, go vet ./..., go test -race -count=1 ./..., and go build ./cmd/sysc-clipboard.
7. Tag and push qualified daemon v0.1.0. Record its exact commit in Beads before changing sysc-shell go.mod.

### Task 7: Pin the daemon client and add the bar projection

**Files:**
- Modify sysc-shell go.mod and go.sum to add github.com/Nomadcxx/sysc-clipboard v0.1.0.
- Modify cmd/sysc-shell/main.go for one clipboard client pump.
- Modify internal/config/config.go and config_test.go for a known clipboard item in the default right lane.
- Modify internal/shell/widget.go and widget_test.go for the content_paste icon and action.
- Create internal/shell/clipboard.go and clipboard_test.go; modify registry.go, bar_test.go, and integration tests.

**Steps:**

1. Write failing tests for known/default clipboard configuration, KindIcon/content_paste, accessible name, metadata-only action, and both button codes.
2. Run focused tests and observe missing item/projection failures.
3. Add a process-wide immutable clipboard projection carrying copied client metadata and connection/persistence/Wayland status. Invalidate bars only for projection changes.
4. Pump client updates in main. A missing daemon is non-fatal and projects unavailable state.
5. Render the glyph and count/status tooltip. Both button release codes 272 and 273 call TogglePanel(PanelClipboard).
6. Run affected shell tests and verify the module pin resolves to the qualified daemon commit.
7. Commit feat: add clipboard bar projection.

### Task 8: Add the 720x560 panel and actions

**Files:**
- Modify internal/shell/panel.go for PanelClipboard, parsing, aux mapping, and panel:clipboard.
- Modify internal/shell/panelhost.go for initialization, target size, tree dispatch, pointer/keyboard action routing, and rebuild retention.
- Create internal/shell/popout_clipboard.go and popout_clipboard_test.go.
- Modify internal/shell/registry.go, blurcoverage_test.go, gate_test.go, and panel integration tests.

**Steps:**

1. Write failing 720x560 tree/layout tests for focused search, ID-keyed virtualized rows, text/image previews, selected-row restore/pin/delete, clear controls and confirmation, accessible labels, empty/no-match/unavailable/volatile states, and no raw payload in shell nodes.
2. Write failing interaction tests for left/right bar clicks, Enter restore, Delete confirmation, pin toggle, clear cancel/confirm, substring search, Home/End/Page navigation, and concurrent snapshot replacement preserving the selected ID.
3. Run the tests and observe missing panel/tree failures.
4. Append PanelClipboard after PanelBluetooth to preserve numeric IDs; add clipboard to all name/aux switches; target 720x560, centred/floating, exclusive keyboard.
5. Build the tree from existing text-field, virtual-list/scroll, image, button, icon, theme, shape, and focus primitives. Search only bounded metadata. Rows use daemon IDs as stable keys.
6. Route all actions through the daemon client. Keep clear-all behind confirmation. Release thumbnail data when the panel closes.
7. Rebuild only on projection/query/selection changes, preserve stable focus, and publish panel invalidation outside Registry.mu.
8. Run focused panel tests, go test -race ./internal/shell ./internal/ui, go vet ./..., and gofmt.
9. Commit feat: add clipboard history panel.

### Task 9: Complete reconnect handling and repository gates

**Files:**
- Modify internal/shell/registry.go, internal/shell/clipboard.go, and cmd/sysc-shell/main.go.
- Modify/add clipboard, integration, and main tests.

**Steps:**

1. Write failing tests for disconnect/reconnect, unchanged-state coalescing, status tooltip transitions, unavailable panel rendering, and Registry.Close pump cleanup.
2. Implement bounded updates and shutdown. Do not let a stale reconnect overwrite a newer valid snapshot.
3. Run all affected shell tests and the complete shell test suite.
4. Run daemon gates: gofmt -w ., go vet ./..., go test -race -count=1 ./..., and go build ./....
5. Run shell gates: gofmt -w ., go vet ./..., go test -race -count=1 ./..., go build ./cmd/sysc-shell, and git diff --exit-code -- go.mod go.sum after the intentional pin is committed.
6. Review for plaintext payloads, unbounded reads/queues, proxy use outside the Wayland owner, unlocked panel callbacks, compatibility creep, and unrelated .commandcode/.cursor changes.
7. Commit test: gate clipboard integration.

### Task 10: Qualify live Niri behavior and close the tracked gates

**Files:**
- Modify relevant Beads records.
- Create docs/plans/2026-09-15-clipboard-history-completion-handover.md after evidence exists.
- Modify docs/plans/README.md with the completion-handover row in that same commit.

**Steps:**

1. Start/restart the qualified per-user daemon and export the AGENTS.md Niri environment.
2. Exercise text capture, text-alias deduplication, image capture/thumbnail, MIME-preserving restore, pin/unpin, delete, clear-unpinned, confirmed clear-all, encrypted restart recovery, and missing-key volatile mode.
3. Exercise shell left/right click, search, keyboard navigation, panel close/reopen, daemon disconnect/reconnect, and a second simultaneous client. Check niri msg -j layers before and after and verify no plaintext payload files.
4. Record the one-output limitation; do not claim a two-output result on this machine.
5. Record failures as Beads issues before closing gates. Close daemon, shell, and live issues only with evidence.
6. Add the immutable completion handover and register row, then commit docs: record clipboard qualification.

## Stop condition

Stop after the live gate and completion handover. Do not add PRIMARY selection, auto-paste, configuration migration, system-wide installation, or unrelated M7 breadth.
