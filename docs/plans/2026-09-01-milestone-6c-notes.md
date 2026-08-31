# Milestone 6C Notes Reference Plugin Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Add retained multiline editing and ship a file-backed Notes plugin with safe autosave and external-change reconciliation.

**Architecture:** The shell owns the live editor buffer, selection, preedit, focus, and painting. Stable node identity preserves edits across view patches; an explicit reseed revision replaces the buffer after the plugin resolves an external change. The plugin owns note files and uses standard-library polling and atomic writes.

**Tech Stack:** Go standard library filesystem and Unicode support, M6B protocol/view host, existing text shaping and IME path.

**Design:** `docs/plans/2026-09-01-milestone-6-plugin-host-design.md`

---

### Task 1: Add multiline field behavior to retained UI

**Files:**
- Modify: `internal/ui/tree.go`
- Modify: `internal/ui/textfield.go`
- Modify: `internal/ui/layout.go`
- Modify: `internal/ui/column.go`
- Modify: `internal/render/paint.go`
- Test: `internal/ui/textfield_test.go`
- Test: `internal/render/paint_test.go`

**Step 1:** Add failing tests for newline insertion, UTF-8 cursor movement, backspace across a newline, wrapped measurement, vertical growth, viewport scrolling to the caret, IME preedit, submit without newline when configured, and clipping long content.

**Step 2:** Extend the existing `Field`; do not create a second editor model. Mark multiline and submit policy on the node, and keep buffer mutation in `Field`.

**Step 3:** Paint line runs through the existing text renderer. Add no syntax highlighting, rich text, undo stack, or general document model.

**Step 4:** Run `go test -race -count=1 ./internal/ui ./internal/render -run 'TextField|Multiline|IME|Paint' -v`.

**Step 5:** Commit `feat(ui): support multiline text fields`.

### Task 2: Preserve and reseed host-owned editor state

**Files:**
- Modify: `plugin/v1/node.go`
- Modify: `plugin/v1/message.go`
- Modify: `internal/plugin/view.go`
- Modify: `internal/shell/panelhost.go`
- Test: `internal/plugin/view_test.go`
- Test: `internal/shell/panelhost_test.go`

**Step 1:** Write tests that a same-key snapshot preserves the buffer, a changed sibling patch preserves it, a higher explicit reseed revision replaces it, a stale reseed does nothing, and close drops the retained editor generation.

**Step 2:** Add tests that text-change events coalesce to the newest value while submit, focus, and blur stay ordered. Verify IME state never enters plugin JSON.

**Step 3:** Add only `multiline`, `submit_on_enter`, and `reseed` to the wire input node. Keep mutable field state in `PanelHost` keyed by view generation and node ID.

**Step 4:** Run `go test -race -count=1 ./internal/plugin ./internal/shell -run 'Editor|Reseed|TextEvent|IME' -v`.

**Step 5:** Commit `feat(plugin): retain plugin editor state`.

### Task 3: Implement safe note-file ownership

**Files:**
- Create: `plugins/reference/notes/manifest.json`
- Create: `plugins/reference/notes/store.go`
- Test: `plugins/reference/notes/store_test.go`

**Step 1:** Write tests for configured directory expansion, extension validation, list ordering, scratchpad creation, unique timestamp names, filename sanitization, symlink/path escape rejection, pin persistence, atomic save, rename collision, delete, and preservation after write failure.

**Step 2:** Add external-change tests for clean buffer reload, dirty buffer conflict, unchanged mtime/content, deleted current file, and a file changing during an atomic save.

**Step 3:** Implement with `os.ReadDir`, `os.Stat`, temporary-file write, `Sync`, close, and rename. Poll metadata once per second while the panel is open; add no filesystem watcher dependency.

**Step 4:** Run `go test -race -count=1 ./plugins/reference/notes -run 'Store|External|Atomic|Path' -v`.

**Step 5:** Commit `feat(plugin): add safe Notes storage`.

### Task 4: Build the Notes service and views

**Files:**
- Create: `plugins/reference/notes/notes.go`
- Create: `plugins/reference/notes/view.go`
- Test: `plugins/reference/notes/notes_test.go`
- Test: `plugins/reference/notes/view_test.go`
- Create: `cmd/sysc-plugin-notes/main.go`

**Step 1:** Write state tests for home/list/editor transitions, create, rename, pin, delete confirmation/cancel, dirty state, debounced autosave, flush on close, save error, and external-change conflict choice.

**Step 2:** Write view tests for the bar opener, full-height panel, scrollable note list, stable keys, multiline focused editor, word/character count, status line, accessible controls, and error content.

**Step 3:** Implement one process owner. Autosave after a short idle debounce and on close. On dirty external change, preserve the local buffer and present Reload or Keep Local; never overwrite without a user choice.

**Step 4:** Run `go test -race -count=1 ./plugins/reference/notes ./cmd/sysc-plugin-notes` and `go build ./cmd/sysc-plugin-notes`.

**Step 5:** Commit `feat(plugin): add Notes reference plugin`.

### Task 5: Prove editor and filesystem failure behavior

**Files:**
- Create: `tests/integration/plugin_notes_gate_test.go`
- Modify: `tests/integration/README.md`

**Step 1:** Add an integration test that creates, edits with IME-style events, autosaves, renames, pins, confirms deletion, and reopens a note through a temporary plugin directory.

**Step 2:** Change the open file externally during clean and dirty editor states. Verify clean reseed and dirty conflict behavior. Make the directory read-only and verify content remains in memory with a visible save error.

**Step 3:** Run `go test -race -count=1 ./...` and `go vet ./...`.

**Step 4:** Close `sysc-71` with the gate evidence, flush beads, and commit `test(plugin): prove Notes data safety` with `.beads/issues.jsonl`.
