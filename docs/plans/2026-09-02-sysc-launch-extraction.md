# sysc-launch Repository Extraction Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Publish the launcher engine and diagnostic CLI as
`github.com/Nomadcxx/sysc-launch@v0.1.0`, then replace
`sysc-shell/internal/launcher` with that pinned module without changing shell or
Milestone 5 behavior.

**Architecture:** Filter the existing launcher-only history into the empty
repository, make its API presentation-neutral, and add a small command under
`cmd/sysc-launch`. Integrate both local modules through a temporary `go.work`,
publish the external tag, then verify `sysc-shell` against the immutable tag
with no local replacement.

**Tech Stack:** Go 1.26.4, `git filter-repo`, XDG desktop entries,
`junegunn/fzf`, Niri CLI activation, gob history, GitHub CLI.

---

### Task 1: Create the history-preserving repository

**Files:**
- Create repository: `/home/nomadx/sysc-launch`
- Move from shell history: `internal/launcher/*.go` to module root

**Step 1: Recheck both source tips and target**

Run:

```bash
git -C /home/nomadx/sysc-shell status --short --branch
git -C /home/nomadx/sysc-shell show-ref \
  refs/heads/safety/pre-launcher-main-20260902 \
  refs/heads/safety/launcher-v1-20260902
gh repo view Nomadcxx/sysc-launch \
  --json isEmpty,url,visibility,defaultBranchRef
```

Expected: shell clean; both safety refs present; target public and empty.

**Step 2: Clone and filter**

Verify `/home/nomadx/sysc-launch` does not exist, then run:

```bash
git clone --no-local --single-branch --branch main \
  /home/nomadx/sysc-shell /home/nomadx/sysc-launch
git -C /home/nomadx/sysc-launch filter-repo \
  --path internal/launcher/ \
  --path-rename internal/launcher/: \
  --force
git -C /home/nomadx/sysc-launch branch -M main
git -C /home/nomadx/sysc-launch remote add origin \
  https://github.com/Nomadcxx/sysc-launch.git
```

Expected: only launcher files and their narrowed history remain; filtering has
removed the clone's original `origin`.

**Step 3: Verify preserved authorship**

Run:

```bash
git -C /home/nomadx/sysc-launch log \
  --format='%h %an <%ae> %s'
git -C /home/nomadx/sysc-launch log --format='%(trailers)'
```

Expected: launcher implementation commits remain authored by
`Nomadcxx <noovie@gmail.com>` and contain no attribution trailers.

### Task 2: Establish the public module and remove the shell UI seam

**Files:**
- Create: `/home/nomadx/sysc-launch/go.mod`
- Modify: `/home/nomadx/sysc-launch/entry.go`
- Modify: `/home/nomadx/sysc-launch/entry_test.go`
- Modify: `/home/nomadx/sysc-launch/prefix.go`
- Modify: `/home/nomadx/sysc-launch/prefix_test.go`

**Step 1: Initialize the module at the pinned versions**

Run:

```bash
cd /home/nomadx/sysc-launch
go mod init github.com/Nomadcxx/sysc-launch
go get github.com/go-freedesktop/desktopentry@v0.1.0
go get github.com/junegunn/fzf@v0.74.3
```

Set the `go` directive to `1.26.4`.

**Step 2: Verify the inherited UI dependency fails**

Run:

```bash
go test ./...
```

Expected: FAIL because the filtered module cannot import
`github.com/Nomadcxx/sysc-shell/internal/ui`.

**Step 3: Rewrite the icon tests first**

Replace the `Icon.Paint` tests with table tests proving:

- application entries retain `IconName`
- provider overview rows copy `Provider.Glyph` into presentation-neutral
  metadata
- `Result` contains no painter or shell node

Run the focused tests and confirm they fail against the inherited types.

**Step 4: Remove the UI seam**

Delete `Icon`, `Icon.Paint`, and `IconSlotSize`. Keep `PlaceholderGlyph`.
Remove `Result.Icon`. In `overviewRows`, set the overview entry's `IconName`
from the provider glyph. Remove every `sysc-shell/internal/ui` import.

**Step 5: Verify and commit**

Run:

```bash
go mod tidy
go test -count=1 ./...
go test -race -count=1 ./...
rg 'sysc-shell/internal|internal/ui' .
```

Expected: tests pass; search has no matches.

Commit:

```bash
git add .
git commit -m "refactor: expose presentation-neutral launcher core"
```

### Task 3: Export the supported service and history API

**Files:**
- Modify: `/home/nomadx/sysc-launch/history.go`
- Modify: `/home/nomadx/sysc-launch/history_test.go`
- Modify: `/home/nomadx/sysc-launch/service.go`
- Modify: `/home/nomadx/sysc-launch/service_test.go`

**Step 1: Write external-package compile tests**

Add `api_test.go` using `package launcher_test`. It must construct
`launcher.ServiceConfig` with injected scan, runner, environment, lookup,
clock, ranking, and logger functions; open a history at a temporary path; and
create and close a service.

Expected initial failure: unexported `history`, `rankFunc`, `runFunc`,
`getenvFunc`, `lookPathFunc`, or `logFunc` leaks through the public API.

**Step 2: Export the minimum API**

- Rename `history` to `History`.
- Add `OpenHistory(path string, logf func(string, ...any)) *History`.
- Keep an internal clock-taking loader for deterministic tests.
- Keep `DefaultHistory` but make it use
  `$XDG_STATE_HOME/sysc-launch/history.gob`.
- Change every `ServiceConfig` field to an ordinary public function type.
- Keep scan and rank implementation details unexported.

Do not add interfaces or option types.

**Step 3: Pin independent history paths**

Add table tests for XDG and HOME fallback paths. They must expect:

```text
$XDG_STATE_HOME/sysc-launch/history.gob
$HOME/.local/state/sysc-launch/history.gob
```

**Step 4: Verify and commit**

Run:

```bash
go test -count=1 ./...
go test -race -count=1 ./...
go vet ./...
```

Commit:

```bash
git add api_test.go history.go history_test.go service.go service_test.go
git commit -m "feat: publish launcher service and history API"
```

### Task 4: Add the diagnostic query command

**Files:**
- Create: `/home/nomadx/sysc-launch/cmd/sysc-launch/main.go`
- Create: `/home/nomadx/sysc-launch/cmd/sysc-launch/main_test.go`

**Step 1: Write failing command tests**

Test a `run(args, stdout, stderr, serviceFactory)` function with an injected
service. Cover:

- `query` emits one JSON array containing `Entry` and `Score`
- an optional query is passed unchanged
- too many arguments return usage error and nonzero status
- no initial snapshot before the timeout returns a bounded error

Use buffers and channels; do not launch subprocesses.

**Step 2: Implement the minimum command**

`query [QUERY]` creates the service, waits for its initial snapshot, submits
the requested query, waits for the next result, and encodes JSON to stdout.
All waits use a five-second context. Logs and usage go to stderr.

**Step 3: Verify and commit**

Run:

```bash
go test -count=1 ./cmd/sysc-launch
go test -race -count=1 ./cmd/sysc-launch
```

Commit:

```bash
git add cmd/sysc-launch
git commit -m "feat: add launcher query command"
```

### Task 5: Add the diagnostic launch command

**Files:**
- Modify: `/home/nomadx/sysc-launch/cmd/sysc-launch/main.go`
- Modify: `/home/nomadx/sysc-launch/cmd/sysc-launch/main_test.go`

**Step 1: Write failing tests**

Cover:

- `launch DESKTOP_ID` activates the entry after the initial snapshot
- `launch DESKTOP_ID ACTION_ID` forwards the desktop action
- missing ID and extra arguments return usage errors
- activation failure is written to stderr and returns nonzero

Use the existing injectable runner path; never require a live Niri process in
unit tests.

**Step 2: Implement and verify**

Add only the `launch` switch branch. Reuse the same initial-snapshot wait and
five-second bound as `query`.

Run:

```bash
go test -count=1 ./...
go test -race -count=1 ./...
go vet ./...
go build ./cmd/sysc-launch
```

**Step 3: Commit**

```bash
git add cmd/sysc-launch
git commit -m "feat: add launcher activation command"
```

### Task 6: Document and license sysc-launch

**Files:**
- Create: `/home/nomadx/sysc-launch/README.md`
- Create: `/home/nomadx/sysc-launch/LICENSE`

**Step 1: Write README**

Document:

- Niri-first scope and Go requirement
- library import and minimal `Service` example
- `query` and `launch` CLI usage with JSON behavior
- XDG scanning, fzf ranking, history paths, and privacy note
- desktop action and terminal behavior
- relationship to `sysc-shell`
- build and test commands

Do not add AI, Cursor, or assistant attribution.

**Step 2: Add licensing**

Add the complete GPL-3.0-only license text. Keep the Elephant attribution in
`history.go`.

**Step 3: Verify and commit**

Run:

```bash
go test -count=1 ./...
go test -race -count=1 ./...
go vet ./...
go build ./...
git diff --check
rg -i 'generated with|generated by|co-authored-by:.*(cursor|ai|assistant)' .
```

Commit:

```bash
git add README.md LICENSE
git commit -m "docs: document and license sysc-launch"
```

### Task 7: Integrate the two local modules without a replace directive

**Files:**
- Create worktree:
  `/home/nomadx/sysc-shell/.worktrees/extract/sysc-launch`
- Modify in worktree: `go.mod`, `go.sum`
- Modify in worktree: `internal/shell/registry.go`
- Modify in worktree: `internal/shell/panelhost.go`
- Modify in worktree: `internal/shell/popout_launcher.go`
- Modify in worktree: `internal/shell/popout_launcher_test.go`
- Delete in worktree: `internal/launcher/`
- Modify in worktree: `README.md`
- Modify in worktree: `docs/plans/README.md`
- Temporary only: `/tmp/sysc-launch-integration.work`

**Step 1: Create the shell extraction worktree**

Verify the parent exists, then run:

```bash
git -C /home/nomadx/sysc-shell worktree add \
  /home/nomadx/sysc-shell/.worktrees/extract/sysc-launch \
  -b extract/sysc-launch main
```

Do not run `bd` inside the worktree without
`BEADS_DB=/home/nomadx/sysc-shell/.beads/beads.db`.

**Step 2: Write the failing shell integration**

Change launcher imports to `github.com/Nomadcxx/sysc-launch`, require
`v0.1.0`, and remove `internal/launcher`. Add a shell test proving the history
path passed to `OpenHistory` remains:

```text
$XDG_STATE_HOME/sysc-shell/launcher/history.gob
```

Run with `GOWORK=off`; expect failure because `v0.1.0` is not published yet.

**Step 3: Create a temporary workspace**

Run:

```bash
tmpdir="$(mktemp -d)"
(cd "$tmpdir" && GOWORK=off go work init \
  /home/nomadx/sysc-launch \
  /home/nomadx/sysc-shell/.worktrees/extract/sysc-launch)
mv "$tmpdir/go.work" /tmp/sysc-launch-integration.work
rmdir "$tmpdir"
```

Run subsequent local integration commands with:

```bash
GOWORK=/tmp/sysc-launch-integration.work
```

Never add a `replace` directive or copy the workspace file into either repo.

**Step 4: Adapt the shell boundary**

- Replace all internal launcher imports with the external module.
- Build the existing shell history path locally and call `OpenHistory`.
- Keep all panel, UI, paint, Wayland, IPC, and hotkey code in `sysc-shell`.
- Delete `internal/launcher`.
- Add `sysc-launch` to the root README technology list.
- Register the sibling repository in `docs/plans/README.md`.

**Step 5: Verify local integration**

Run:

```bash
GOWORK=/tmp/sysc-launch-integration.work go test -count=1 ./...
GOWORK=/tmp/sysc-launch-integration.work go test -race -count=1 ./...
GOWORK=/tmp/sysc-launch-integration.work go vet ./...
GOWORK=/tmp/sysc-launch-integration.work go build ./...
rg 'internal/launcher|replace ' .
```

Expected: all commands pass; search finds only historical documentation
references, no Go import or module replacement.

Do not commit the shell worktree yet; its required tag does not exist.

### Task 8: Publish sysc-launch v0.1.0

**Files:**
- Remote: `https://github.com/Nomadcxx/sysc-launch`

**Step 1: Final repository review**

Run:

```bash
git -C /home/nomadx/sysc-launch status --short --branch
git -C /home/nomadx/sysc-launch log --oneline --decorate
git -C /home/nomadx/sysc-launch log --format='%(trailers)'
go -C /home/nomadx/sysc-launch test -count=1 ./...
go -C /home/nomadx/sysc-launch test -race -count=1 ./...
go -C /home/nomadx/sysc-launch vet ./...
go -C /home/nomadx/sysc-launch build ./...
```

Expected: clean tree, no attribution trailers, all checks pass.

**Step 2: Push main and tag**

Run:

```bash
git -C /home/nomadx/sysc-launch push -u origin main
git -C /home/nomadx/sysc-launch tag -a v0.1.0 \
  -m "sysc-launch v0.1.0"
git -C /home/nomadx/sysc-launch push origin v0.1.0
```

Do not force-push.

**Step 3: Verify remote release**

Run:

```bash
git ls-remote --heads --tags \
  https://github.com/Nomadcxx/sysc-launch.git
GOWORK=off go list -m \
  github.com/Nomadcxx/sysc-launch@v0.1.0
```

Expected: remote main and `v0.1.0` exist and the module resolves.

### Task 9: Pin the release and finish the shell extraction

**Files:**
- Commit the Task 7 shell worktree changes
- Modify: `.beads/issues.jsonl` from the primary checkout

**Step 1: Verify without the workspace**

From the shell extraction worktree:

```bash
GOWORK=off go mod tidy
GOWORK=off go test -count=1 ./...
GOWORK=off go test -race -count=1 ./...
GOWORK=off go vet ./...
GOWORK=off go build ./...
git diff --check
```

Expected: all checks pass by downloading `sysc-launch@v0.1.0`.

**Step 2: Prove M5 remains intact**

Run:

```bash
git diff --exit-code safety/pre-launcher-main-20260902 -- \
  internal/notifyclient internal/trayclient \
  internal/shell/notifications.go internal/shell/tray.go \
  internal/shell/toasthost.go
git diff --name-status --diff-filter=D \
  safety/pre-launcher-main-20260902 --
```

The second command may list only `internal/launcher`; no M5 file may be
deleted.

**Step 3: Commit shell integration**

```bash
git add -A
git commit -m "refactor(launcher): consume sysc-launch v0.1.0"
```

**Step 4: Review and merge to main**

Run a focused code review on the external-module boundary. Then merge
`extract/sysc-launch` into local `main` with a merge commit and rerun the full
normal/race/vet/build gate from committed main.

Do not push `sysc-shell`.

**Step 5: Close tracking**

From `/home/nomadx/sysc-shell`:

```bash
bd close sysc-99 --reason \
  "Published sysc-launch v0.1.0 and pinned sysc-shell to it; both full race suites pass."
bd sync --flush-only
```

Commit `.beads/issues.jsonl` with the final shell integration or in an
immediately following tracking commit.

**Step 6: Cleanup**

Remove `/tmp/sysc-launch-integration.work`. Keep both safety refs until the
owner confirms the remote shell update or explicitly asks to remove them.
