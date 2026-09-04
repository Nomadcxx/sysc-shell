# Running-apps pill plan audit report

Audits `2026-09-05-running-apps-pill-design.md` (D1–D15) and `2026-09-05-running-apps-pill.md` (9 TDD tasks). Findings are ordered by severity; the implementing agent should resolve 1–2 before execution and fold 3–8 into the plan edit.

**Supersession (same day, owner):** findings 2 and 5 assumed the pill would reuse `sysc-launch`. The owner then required the bar to work with the launcher widget unused. Design D8/D11 and plan Tasks 3b/7/8 now use a shell-owned `go-freedesktop` index and `niri msg action spawn`. Leave the original findings below as the audit snapshot.

## What was verified (no action needed)

- Every symbol the plan references exists: `Window`/`wireWindow`/`project` and the three named niri tests (`internal/platform/niri/events.go`), `knownItems`/`Default()` (`internal/config/config.go:226`/`:298`), `barView`/`buildWidgets` (`internal/shell/widget.go:16`/`:156`), the action-handler seam (`internal/shell/registry.go:515`), `trayMenuHost` Overlay host, `NewMenu` (`internal/shell/menu.go`), `launcherSpawn`/`openLauncherActions` (`internal/shell/popout_launcher.go:286`/`:329`), the icons resolver, `KindCapsule`/`FillAccent`.
- Both documents are registered in `docs/plans/README.md`; bd issue sysc-175 (open, P1) references them.
- Test-runner lines match repo practice (`timeout 90s env GOMAXPROCS=2 go test -count=1 <pkg> -run <Name>`; no `./...`, no `-race` — OOM and commit-msg hook constraints).
- All 8 plan commit messages pass the commit-msg banned-word screen.
- D1–D15 each map to at least one task; the plan's live gate matches the design's.
- Live probe on this machine (niri 26.04, one output DP-1): `niri msg -j windows` shows the focused window carries the maximum `focus_timestamp`, so Task 3's "MRU = max FocusTimestamp" direction is correct.

## Findings

### 1. BLOCKER — Task 1 decodes `focus_timestamp` with the wrong wire shape

The plan decodes `focus_timestamp` as `int64`. The live wire shape is an object:

```json
"focus_timestamp": {"secs": 166673, "nanos": 194678785}
```

(monotonic seconds+nanos of last focus). A plain int64 unmarshal errors on every focused window, and a `project()` error aborts the whole `WindowsChanged` handling (`internal/platform/niri/events.go:227-236`) — the window set would never publish on the live machine, while the table test (null fixture only) still passes.

**Fix:** decode `*struct{ Secs uint64; Nanos uint64 }` (or `json.RawMessage`) and convert to int64; add a fixture with a non-null timestamp to the Task 1 table test so the real shape is covered. Design D9 ("FocusTimestamp when present") survives unchanged; only the plan's decode spec is wrong.

### 2. MAJOR — no task builds the production identity lookup (D11)

`groupRunningApps(windows, lookup)` takes the lookup as a parameter and the tests inject a map, but no task wires the real one. The pinned `sysc-launch v0.1.0` `Entry` has no `StartupWMClass` field and the package exposes no app_id→Entry matcher (API is `Query`/`Results`/`Activate` only). D11 promises app_id → desktop-entry id / StartupWMClass / punctuation-stripped tail.

**Fix (choose one):**
- Add a task that builds the lookup from a launcher entry snapshot through the existing Registry service seam (`Query(app_id)` → `Results` is the existing seam; a direct desktop-file scan would violate the "no second desktop parser" rule), and specify the StartupWMClass/stripped-tail rules with their own table test; or
- Cut D11 down to exact/lowercase-id matching only and record StartupWMClass/stripped-tail as a follow-up bd issue.

As written, Tasks 7–8 consume slots nobody produces in production.

### 3. MINOR — steam fold rule deviates from D2

D2: fold `steam_app_*` into Steam only when no matching desktop file. Task 3 folds unconditionally (prefix → "steam" before consulting the lookup). Should try the game's own id first, then fold. The test table lacks the "steam_app with its own desktop file" case.

### 4. MINOR — D6's sticky last-focused memory is dropped

D6: "last focused member the projection remembers". Task 3's fallback chain ends at "last member with Focused, else members[0]" — a pure function of the current list, so once focus leaves a slot the memory is gone and the fallback degrades to `members[0]`. Either accept the degradation and note it against D6, or specify where the sticky last-focused id lives (registry/widget state). Task 4's "No member focused → MRU id" inherits the same ambiguity when no timestamps exist.

### 5. MINOR — Task 8 names the wrong spawn seam

The launcher action path is `launcherSpawn` → `Service.Activate(id, actionID)` (`internal/shell/popout_launcher.go:286`/`:329`), not `runArgv` directly; `runArgv` (`registry.go:76-77`) is the generic argv runner sysc-launch's Run config ultimately reaches. Name `launcherSpawn`/`Activate` so the implementer does not wire a parallel path.

### 6. PROCESS — bd steps missing from the tasks

Per AGENTS.md: claim with `bd update sysc-175 --status in_progress` before Task 1, commit `.beads/issues.jsonl` in the same commit as the code it describes, and close with `bd close sysc-175 --reason "..."` after the live gate. The plan's commits never touch issues.jsonl and Task 9 only says "record evidence in bd".

### 7. NIT — Task 7 placement ambiguity ("every output state or barView once")

`barView` is assembled per output in `registry.go:846` from `outputState`. A session-global slot list belongs on `barView` directly (one field, filled from the niri projection before the per-output loop), not on `outputState`. Say which, so the implementer does not duplicate the list per output.

### 8. NIT — Task 5 menu rows lack the row→action association

Menu rows return labels only. State the row-index → `slot.Actions` association so Task 8's spawn has the Argv without re-deriving it.

## Verdict

The plan is well-grounded (every referenced symbol exists), correctly registered, bd-linked, and follows the repo's test-runner and commit-message constraints. **Findings 1 and 2 must be fixed before execution**; 3–8 should be folded into the plan edit in the same pass.
