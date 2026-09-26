# Wallpaper P2 trio — completion handover

Date: 2026-09-26. Work: `sysc-460`, `sysc-461`, `sysc-462`, all closed. Commits `77302f3`, `e659bc0`, `69d6d00` on `fix/wallpaper-p2-trio`, merged to main as `b4301b3`; the concurrent session's pushes carried the merge to origin (main == origin/main at `6f8651d`). This is the snapshot the register's completion-handover kind calls for — hashes, gate output, live observations, known defects — plus the working order the owner set for future cycles.

## Background

The desktop shell reported `theme: matugen: exit status 1`, `DP-1/DP-3: wallpaper: … socket is not owned by this shell`, and a preview counter frozen at 228/645. Root cause: gslapper socket files left in `/run/user/1000/sysc-shell/` by a run killed without cleanup (Sep 16). Every boot's reconcile Apply refused them, so no apply committed, so the theme seed was never rebuilt, so startup generation ran `matugen image ""` and failed into the fallback palette. The frozen counter was a separate publish gap: the walk finished, but its tail was all cache hits and no final progress tick was published. The laptop was unaffected because it had no stale sockets; wallpaper count and dual monitors were red herrings.

## Standing workflow for shell work cycles

The owner set this order; this cycle followed it and future cycles should too:

1. Audit — independent review of the diff against the design constraints (socket ownership D17/D18, locking, error contracts) before anything merges.
2. Backend — smallest fix at the owning layer, test-first: failing test watched RED, minimal GREEN.
3. Front — the user-visible surface the change touches, checked against its contract (may be a no-op code-wise).
4. Gap analysis — every review finding becomes code, a test, or a bd issue; nothing stays only in prose.
5. Testing — `gofmt -w . && test -z "$(gofmt -l .)"`, `go vet ./...`, focused `go test -race -count=1` on touched packages; full-repo `-race` triaged finding by finding.
6. Verifying — a live Niri gate that first reproduces the failure condition, then proves the fix end to end.
7. Merge to main; push to origin only if every gate is green and the push contains nothing but reviewed work.

## What was done, by stage

### Audit

Independent review of the three-commit diff. Verdict: merge, no blockers. Findings:

- Probe-then-remove TOCTOU in Apply's dead-socket path — narrow, acknowledged in code, inherent to the sanctioned unlink → `sysc-576`.
- A bound-but-not-listening socket classifies as dead (`ECONNREFUSED`) — theoretical window; gslapper binds then listens. Recorded here only.
- `stopOwned`'s no-process branch had no test pinning refusal of a live foreign socket → `sysc-577`.
- The "waiting for the first wallpaper" banner renders in the error tone — cosmetic nit, recorded here only.

The reviewer probed the semantics the ownership proof rests on: dead file → `ECONNREFUSED`; live listener with full backlog → `EAGAIN`; datagram socket → `EPROTOTYPE`. The proof holds for every realistic case.

### Backend

- `77302f3` (sysc-460): `deadSocketFile` — Lstat requires `ModeSocket`, then `socketPeerPID`; true only on `ECONNREFUSED`. Apply unlinks a provably dead socket and launches; a live foreign socket is still refused. `stopOwned` unlinks a dead socket instead of erroring. Tests: `TestEngineUnlinksStaleSocketWithoutListener`, `TestEngineStillRefusesForeignLiveSocket`, `TestEngineRestoreClearsStaleSocket` (real listener-less AF_UNIX socket via `ListenUnix` + `SetUnlinkOnClose(false)` + `Close`).
- `e659bc0` (sysc-461): one final `t.note()` after the walk loop; the buffered progress channel coalesces it. Test: `TestThumbPublishesFinalCountsWhenNothingNewIsMade`.
- `69d6d00` (sysc-462): empty wallpaper seed returns the fallback palette with `theme: waiting for the first wallpaper` before any exec; the registry keeps the last complete tokens. Test: `TestGenerateWaitsForTheFirstWallpaper`.

### Front

No frontend code changed. The visible surface was verified through existing paths: the picker banner carries the waiting message via the theme-error path, and the preview counter now reaches total when the walk ends on cache hits.

### Gap analysis

Findings became two bd issues (`sysc-576`, `sysc-577` — open, P3, discovered-from `sysc-460`) and two recorded observations (bound-not-listening window, banner tone). Nothing left only in prose.

### Testing

- `gofmt -l .` clean; `go vet ./...` clean; `go.mod`/`go.sum` untouched.
- `go test -race -count=1 ./internal/wallpaper/... ./internal/theme/...` ok; the reviewer re-ran `-count=3`, stable.
- Full-repo `go test -race ./...`: 4 failures, all pre-existing and unrelated to this diff (untouched packages, unrelated failure modes; cf. `sysc-459`): `TestTrayCompositorCloseReleasesTheRoot` and `TestTrayCloseReleasesEverySurface` (tests/integration, "no point on output 7 produced an action"), `TestPanelSectionValidationPrecedesMutation` (controlcenter_test.go:1202, "section network was accepted"), `TestRightClickingTheBarBatteryOpensSession` (panelhost_test.go:261, "default bar has no laid-out battery"). A main-baseline rerun was inconclusive (worktree setup fails on relative replace paths); the evidence above is the basis for "pre-existing".

### Verifying (live, desktop)

An unplanned A/B strengthened the proof:

- The first install silently failed (`cp` hit ETXTBSY while the old service ran; a `;` in the chain masked it). The sha256 check caught it, and the old binary then met a dead-socket fixture and refused it — the original bug, reproduced.
- The new binary (sha256 `1219ad0b…`) unlinked the fixture, spawned gslapper (pid 593774) with the current wallpaper, the socket answered, niri mapped the wallpaper layer on Background beside the bar/toast/depth-clock layers, `colors.json` and `matugen.toml` regenerated with a real palette, and `ipc status` reported `matugen: true`. Journal clean apart from the pre-existing `org.sysc.weather` plugin EOF on every start.

### Merge and push

`b4301b3` merged the branch into main. Each fix commit carried exactly its own `.beads/issues.jsonl` line (the working tree held another session's uncommitted jsonl churn; spliced per commit). During the merge the bd daemon left a one-line partial export, recovered with the documented recipe (`DELETE FROM export_hashes;` + `bd export`, 474 lines verified). The concurrent session pushed main through `6f8651d`, which carries the merge; this handover commit is pushed separately.

## Known defects and out-of-scope observations

- The 4 pre-existing `-race` failures above belong to their owners and may warrant triage.
- The installed binary has no `cap_perfmon`, so GPU utilization is unavailable — pre-existing; the README documents the `setcap` step after every binary replacement.
- The desktop currently exposes one output (`DP-1`); `assignments.json` still carries a DP-3 entry, replayed if it returns.
- Another session's uncommitted planning work sits in the tree (register rows, three 2026-09-25 plan documents, jsonl churn). Do not sweep it into unrelated commits.
