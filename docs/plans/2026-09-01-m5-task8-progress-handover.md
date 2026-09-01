# Milestone 5 Task 8 progress handover

Date: 2026-09-01
Kind: progress-handover

## Working state

- Branch: `milestone/m5-completion`
- Worktree: `/home/nomadx/sysc-shell/.worktrees/milestone/m5-completion`
- Committed head: `437ef33 feat(shell): add tray overflow preferences`
- Base before Task 7: `a763165 feat(shell): host one tray menu surface with revision rules`
- Do not continue on `main`. Use the existing worktree.

Task 7 is committed. It adds strict atomic tray preferences, stable tokens, collision handling,
geometry overflow, pinned and saved ordering, the virtual-list drawer, accessible controls, root cleanup,
and deterministic service order. The required gate passed immediately before the commit:

```text
go test -race -count=1 ./internal/shell/ ./internal/config/
ok github.com/Nomadcxx/sysc-shell/internal/shell 2.345s
ok github.com/Nomadcxx/sysc-shell/internal/config 1.018s
```

`sysc-94` is closed. `sysc-95` was created and claimed for Task 8 in the primary beads database. Tracker
export can wait until the implementation is ready to commit.

## Uncommitted Task 8 work

Five files contain an incomplete 409-line wiring attempt:

```text
internal/shell/bar.go
internal/shell/registry.go
internal/shell/tray.go
internal/shell/trayicon.go
internal/ui/tree.go
```

The attempt adds dynamic tray nodes to bars, `ui.Node.Tooltip`, named and pixmap icon projection, registry
client/host fields, menu request correlation, drawer opening, item activation, scroll commands, and an
icon worker. Keep or replace these edits based on a quick local review. They are not committed and they
do not compile yet.

Current check:

```text
go test -count=1 ./internal/shell ./cmd/sysc-shell
internal/shell/registry.go:84:18: undefined: context
internal/shell/tray.go:231:35: undefined: ui
internal/shell/tray.go:255:54: undefined: ui
```

The first repair is mechanical: import `context` in `registry.go` and `internal/ui` in `tray.go`. Compile
again before deciding whether the draft has deeper ownership or deadlock problems. Pay particular
attention to lock order between `Registry.mu`, `Bar.mu`, tray state, menu/drawer callbacks, and the icon
worker callback.

## Task 8 still required

The current draft does not finish Task 8. Complete these behaviors before its commit:

1. Wire `trayclient.New` in `cmd/sysc-shell/main.go`, pump `Registry.TrayMessages`, and stop the client with
   the process context.
2. Finish bar tray nodes, real activation, secondary activation, vertical scroll, tooltips, drawer open,
   scale-aware icon refresh, and invalidation on snapshot/delta/reconnect.
3. Give `trayMenuHost.spec()` real configure, render, and input callbacks. The existing host tracks menu
   revisions and roots but paints nothing. Wire `menu.open`, `menu.select`, refresh, about-to-show, and
   close commands.
4. Attach a menu opened from the drawer as the drawer root's child. A bar menu may own a fresh root.
   Close the tooltip first. Clear pending menu requests on item loss, output loss, service loss, root
   replacement, and shutdown.
5. Update drawer rows with resolved item images and refresh the open drawer when tray state changes.
6. Cover output hotplug and cleanup in `DropHost`, `DropAux`, configuration commit, and `Registry.Close`.
7. Add `tests/integration/tray_test.go` and update `tests/integration/README.md` for two outputs,
   generations, stale replies, menu failure, malformed siblings, preference collision, root replacement,
   hotplug, reconnect, and cleanup.

Run the plan gate after Task 8:

```sh
gofmt -w .
test -z "$(gofmt -l .)"
go vet ./...
go test -race -count=1 ./...
git diff --exit-code -- go.mod go.sum
```

Commit Task 8 as `test(shell): qualify tray presentation` only after those checks pass. Avoid `bot` and
`agent` in commit messages because the local hook rejects those substrings.

## Live gates and merge

No Niri result has been recorded. The owner offered a Niri laptop over SSH; the known target is
`ssh -p 7777 nomadx@192.168.0.64` once the laptop session is available. Ask the owner to log into Niri
before connecting.

Run all pending live work on two outputs:

- notification gate: toasts on each output, center, DND, inline reply;
- Task 6 popup probe: valid 1x1 `xdg_popup` through layer-shell `get_popup`; keep the Overlay fallback if
  Niri rejects the protocol-valid sequence and record the trace;
- combined gate: center/drawer root replacement, menu from drawer, tooltip closure, reply replacing a
  menu, independent service restarts, shell restart, hotplug, mixed scale/transform, and 60 minutes idle.

Do not report a pass for anything the laptop did not run.

After the live matrix, create and register the Milestone 5 completion handover with the shell commit,
service candidate tags, command output, observations, defects, and stable-tag authorization. Close
`sysc-64`, commit tracker state with the implementation, merge `milestone/m5-completion` into `main`, and
run a post-merge test. The owner authorized completion and merge; do not pause for a merge-strategy
choice unless the branches conflict.
