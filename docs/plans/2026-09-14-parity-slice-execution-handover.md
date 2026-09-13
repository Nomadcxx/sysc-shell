# Parity slice execution handover

Date: 2026-09-14.

Backdrop blur and rendering smoothness are merged and pushed. The Noctalia v4 parity slice is six tasks
in, on a pushed branch, with a seventh part-written and uncommitted. This handover discharges everything
before it and commissions the rest. Read `2026-09-13-parity-tranche-continuation-handover.md` for the
tranche's origin; do not re-derive it here.

## Receiving state

`main` is `43da674`, pushed. It carries both merged slices:

| Commit | Contents |
|---|---|
| `127f7aa` | Merge of `feature/panel-backdrop-blur` — the whole blur slice, defaults off |
| `49c6a92` | Merge of rendering smoothness Tasks 1–4 and its plan amendment |
| `493ac6b` | Merge of rendering smoothness Task 6 |

**A concurrent session is committing to `main`.** Four documentation commits landed there while this
slice was being worked — `d121923`, `d4b6ab7`, `cd895b0`, `7b2f138` — defining a panel list row shape,
its insets, and a bluetooth panel and service, with their register rows. They touch `docs/` only and add
no code, so **the 169-site census below is unaffected**. But those documents design new panels, and a
new panel carries new literal geometry: expect the gate's count to grow once they are built, exactly as
the unmerged `feature/network-panel` branch already adds 18 sites to it. Re-take the census before
trusting the number, with the rule the gate itself uses.

`feature/noctalia-parity` is **pushed** at `30b5f06`, branched from `493ac6b`. It is 6 ahead of `main`
and 5 behind it, those five being the documentation commits above and this handover. Rebasing is
optional: none of them touches a file this slice edits.

| Commit | Task | Contents |
|---|---|---|
| `e6bfdf9` | 1 | The literal-geometry gate, committed **red on purpose** |
| `32a9167` | 2 | Spacing ladder → 1, 2, 4, 6, 9, 13, 18 |
| `ccbcbb7` | 3 | Independent input radius axis |
| `d756992` | 4 | Type ladder re-based, `RoleDisplay` added |
| `d8a0154` | 5 | Five odd-height density rows |
| `30b5f06` | 5A | Control sizes derived from one base dimension |

Tracker: `sysc-265` (open, carries the corrected census and every finding), `sysc-262` (closed, the
smoothness slice), `sysc-202` (open — see *What is blocked*).

**Package state on the branch:** `internal/theme`, `internal/config`, `internal/settings` green.
`internal/shell` fails **exactly one** test, `TestSurfaceSourcesCarryNoLegacyVisuals` — Task 1's gate,
which Tasks 9 and 10 drive to zero. Any other failure means something regressed.

## Task 6 is part-written and uncommitted

The worktree at `/home/nomadx/.config/superpowers/worktrees/sysc-shell/feature/noctalia-parity` has
uncommitted motion work. It is nearly finished.

Applied already:

- `BaseMotion` re-based: Shorter 75, Short 150, Medium 300, Long 450, ExtraLong 750. `FrameCap` stays
  33 and deliberately did **not** move — it bounds blits, not transitions.
- `profile_test.go`: `TestMotionDurationsMatchTheReference` (checks all six tokens, not the plan's two)
  and `TestFrameCapStaysBelowTheShortestToken` added; the `AtSpeed` table re-computed to
  1200 / 300 / 240 / 75.
- `animation_test.go`: the catalogue table re-based to 75 / 150 / 150 / 150 / 300 / 300 / 150.
- `theme_test.go:462`: `Durations.Medium` 180 → 300.

**Step 2 needs no code change.** The task asks for press and hover on `Shorter`/`Short`, selection on
`Medium`, panel enter/exit on `Medium`/`Short` — which is already exactly what `duration()`
(`animation.go`) resolves. Only the values underneath it moved. Verify this rather than editing it.

### Three failures remain, and they are two different kinds

1. **`theme_test.go:553` — mechanical.** `got.Motion.Durations.Medium != 144*time.Millisecond` must
   become `240`. The compact preset runs `MotionSpeed` 125, so Medium 300 scales to 240. Change the
   message with it.

2. **`panelhost_test.go:209` — not understood, do not tune.** `countSurfaceInvalidations(reg, 200ms)`
   returns 4 against a `want >= 5`. The window is a fixed 200 ms and the frame cap paces at 33 ms, so a
   longer reveal should produce *more* frames, not fewer. That contradiction is unexplained. Find out why
   before touching the threshold — lowering it would turn a real signal into an intermittent flake.

3. **`controlcenter_test.go:302` — not understood, do not tune.** The test advances the clock by
   `Durations.Medium / 2` (now 150 ms), retargets mid-transition, and expects the rendered value to be
   unchanged; it reports "jumped from 1 to 0". At 150 ms into a 300 ms transition the value should still
   be in flight, so a settled `1` collapsing to `0` suggests the animator forgot the key or the page
   wrapper changed identity, not that a number needs updating.

Both timing failures appeared **only** when durations nearly doubled, so they are genuinely caused by
this task. Treat them as findings.

## What the slice still owes

Seven tasks. Nothing below is started.

| Order | Task | Notes |
|---|---|---|
| 1 | **6** (finish) | The three failures above. |
| 2 | **5B** | Mostly done already: Task 5 set `PanelPadding` 13 and `CardPadding` 9 at every density. What remains is a new `CardGap` field (also `marginM` 9) and its test. |
| 3 | **7** | Contrast floor 1.45 → 1.30, outline split. The two assertions have **moved to `theme_test.go:262` and `:265`**, not the `:252`/`:255` the plan cites. The API its test needs all exists: `FallbackFor` (`fallback.go:132`), `ContrastRatio` (`contrast.go:61`), `mustColor` (`palettes.go:389`), `OutlineVariant` (`palettes.go:360`). `derive()` is at `palettes.go:236` with `text := 4.5`, `nonText := 3.0` and steps 0.45/0.72/1.0 exactly as described; the outline derivation is at `:303–304`. Records the resolution of `sysc-104`. |
| 4 | **8** | Map the reference's sixteen colour roles. Delete none. |
| 5 | **9** | **The large one.** Drive the gate to green: 169 literal sites. `controlcenter_pages.go` alone carries 62. |
| 6 | **10** | Widen the three behavioural checks past two panels. Note `TestSurfaceCardPaddingFollowsDensity` has already had its density probe removed — card padding no longer varies by density, by design. |
| 7 | **11** | Steps 1–2 (migration note, settings list) are doable. **Step 3 is not** — see below. |

## What is blocked, and why

**The live gates cannot be run from here.** `sysc-256` (blur), `sysc-5`, and parity Task 11 Step 3 all
need eyes on a running compositor. As of 2026-09-13:

- archPC sits at its **login greeter**. No Niri, no `wayland-*` socket, `sysc-shell.service` inactive,
  and the unit is `Requisite=graphical-session.target` so a manual start fails by design.
- The laptop (`ssh laptop`, 192.168.0.64) refuses key auth: `Permission denied (publickey,password)`.
  This session reaches archPC *from* that laptop, so the live shell is on the unreachable machine.

Either log in on archPC and run them there, or set up key auth to the laptop. A sampler for the
60-minute idle observation is written and ready; re-create it if the scratchpad is gone.

**`sysc-202` stays open.** Three of its four named cases are measured (blur, animation frame time,
image-heavy grids). Only CPU/power is unmeasured. One archPC run recorded **10h 11min of CPU over 23h
2min wall — about 44% of a core sustained** — against 3.3% for another 22-hour run on the same binary
era. That spread is the failing case this epic names. It is recorded on `sysc-202` and is not
attributable to a commit.

## Corrections already applied — do not re-introduce

- **The gate's rule reports every literal on a line**, via `FindAllStringSubmatch`, and exempts a zero
  **per match** rather than per line. The plan's `FindString` plus a whole-line `": 0"` test misses both
  cases: 38 lines here carry two literals, and one line reads `Min: 0, Max: 100, Step: 5, Width: 360`.
- **The census is 169, not 105.** The design's figure was measured against an older tree.
- **Task 4's addressability test lives in `internal/render`**, not beside the role definition. `render`
  imports `theme`, so the plan's placement is an import cycle that could never compile.
- **`textRoleCount` is derived** from the last role. A role added past it indexes out of range at paint
  *while the count guard still passes*, because the guard agrees with the stale constant.
- **The superseded density name folds onto its replacement in `MetricsFor`.** No "wire alias" mechanism
  existed to keep. `Densities()` does not offer it, and the presets now name `DensityDefault` — leaving
  them on the old name made every default composition produce a value the settings enum no longer lists.
- **`BarPadding` shrank with the band.** A 31 px bar holding a 25 px pill has 6 px for the two insets
  together. The plan does not mention this.
- **`BaseWidget` varies per row.** The plan quotes a single 33, but its own proportion test requires
  compact to be smaller than spacious, which one constant cannot satisfy.
- **`CompactControl` = `TabHeight`, `StandardControl` = `InputHeight`.** The latter falls 40 → 36 at the
  default row and moves on every row. Measured first: the failures are height assertions, not fits.
- **Two tests named for a DMS content band were renamed.** Architecture decision 8 makes DMS a behaviour
  reference, not a compatibility contract, and this re-base supersedes constants that were observed
  rather than measured.
- **`FrameCap` is not scaled by motion speed** (smoothness slice) and did not move with the motion
  re-base. Scaled, it fell below the 16 ms tick at 400% and paced nothing.

## Hazards this session paid for

- **The `commit-msg` hook rejects substrings, case-insensitively.** `both` contains `bot` — it blocked
  three messages here. Screen every one before committing:
  ```bash
  grep -oiE "(claude|anthropic|chatgpt|openai|copilot|cursor|cody|tabnine|codex|gemini|bard|gpt-[0-9]|llm|ai assistant|bot|agent)" msg.txt && echo BANNED || echo clean
  ```
  No `Co-Authored-By`. Never `--no-verify`.
- **Guard every commit on the failing set by name.** Asserting "exactly these tests fail" before
  committing caught three real regressions that eyeballing would have waved through, including a whole-
  struct table test and a half-completed rename.
- **Whole-struct comparisons are invisible to the obvious grep.** `TestPresetTablesMatchTheDesign` and
  `TestProfileDensityTable` compare with a bare `!=`, so **every new field breaks them**. Searching for
  `reflect.DeepEqual` or `NumField` finds neither.
- **Measure before adopting.** Patching one value, running, and reverting settled three decisions that
  would otherwise have been guesses — the bar-height shrink, the padding fix for the session overflow,
  and the control-size shrink. Back the file up with `cp`; do not `git checkout` a file holding
  uncommitted work.
- **Never `git checkout -- .beads/issues.jsonl`.** The `post-checkout` hook re-imports it and mints new
  IDs; that forked `sysc-259`, `sysc-260` and `sysc-261` here. Restore with
  `git show HEAD:.beads/issues.jsonl > .beads/issues.jsonl` instead.
- **`bd daemon stop` and `bd daemon status` START a daemon.** The flags are `--stop` and `--status`. The
  database is pre-0.17.5 and every daemon dies immediately with `LEGACY DATABASE DETECTED`.
- **Committing from a worktree needs `BEADS_DB=/home/nomadx/sysc-shell/.beads/beads.db`.**
- **gopls reports false errors in worktrees** — `undefined: Registry`, `use of internal package not
  allowed`, and once a plausible-looking `FrameCap undefined`. Trust `go build`; never edit code for it.
- **Never `go test ./...` or any `-race` build.** Cap with `GOMAXPROCS=4` and `-p 2`.

## Plan defects found, and where they are recorded

The rendering smoothness plan was amended in place (`49c6a92`) with an Amendments section. The parity
plan has **not** been amended yet — do it, or the next executor pays again:

- Task 3's file list omits the preset rebase and validation wiring.
- Task 4's file list omits `internal/shell/theme_test.go` and `internal/config/config_test.go`, and
  places a test where it cannot compile.
- Task 5's "keep a wire alias" names a mechanism that does not exist, and omits the `BarPadding`
  constraint.
- Task 5A quotes one base dimension that its own test contradicts.
- Every line number in the document is stale.

## Start here

Finish Task 6: the mechanical fix at `theme_test.go:553`, then investigate the two timing failures
rather than adjusting their thresholds. Then 5B, which is nearly free, before the larger re-bases.
