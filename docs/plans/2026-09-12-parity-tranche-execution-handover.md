# Parity tranche execution handover

Date: 2026-09-12.

This handover commissions execution of the 2026-09-11 parity tranche: seven designs and five plans,
all on `main`, none started. It records where to begin and why, the hazards that will bite a cold
start, and the claims in those documents that are **not** yet verified.

The tranche answers two requests. A style guide, so development stops re-deriving chrome — that is the
parity, token-conformance and component-parity designs. And expanded rendering — that is backdrop blur,
rendering smoothness and surface stacking, with a media slice carried along because stacking's consumer
turned out to live there.

## Receiving state

Nine commits, `17e24fb` through `d64c5ef`, with `bb88d0e` from a concurrent session interleaved and no
collision:

| Commit | Contents |
|---|---|
| `17e24fb` | Six designs, register rows |
| `a7bad57` | Blur plan, 10 tasks |
| `6bbe26b` | Smoothness plan, 6 tasks |
| `ed52746` | Stacking plan, 6 tasks |
| `810f5c9` | Parity plan (13 tasks) and media plan (10 tasks) |
| `2dd478a` | Evidence audit; **corrected two designs** |
| `810eebf` | Composition modules sourced; component parity folded into the parity plan |
| `d64c5ef` | Consumer gates amended for the planned template panel |

Tracker, verified 2026-09-12:

| Issue | Status | Relevance |
|---|---|---|
| `sysc-202` | open, epic | Blur and smoothness both hang here. Closing each retires it. |
| `sysc-104` | **in_progress** | The nested-surface contrast floor. Parity Task 7 records its resolution at 1.30:1. Someone may be touching it. |
| `sysc-156` | open | Media. Its design D8 re-slices it into service, widget and page. |
| `sysc-253` | open, P1 | Control-centre spine. The media page and the stacked media card both wait on it. |
| `sysc-142` | closed | Superseded theme parity. **Its Task 13 live gate never ran**; the parity design re-inherits it. |
| `sysc-54` | **in_progress** | Unblocked by the contrast change; the concurrent surface-polish pass owns it. |

**No bd issue names any tranche work.** Not blur, smoothness, stacking, conformance, the parity re-base
or the template. AGENTS.md makes bd the only tracker and `bd ready` the answer to "what can I work on" —
so running it today surfaces none of this. Each plan creates its own tasks at execution time; do that as
you start a slice rather than pre-filling the graph.

## Start here: blur, Tasks 1 to 3

Work `2026-09-11-panel-backdrop-blur.md` Tasks 1, 2 and 3, then **stop at Task 3 Step 5** and report the
measurement before going further.

Four reasons, in order of weight.

**It resolves the largest unknown first.** Blur D16.1 records the screencopy readback as the single
biggest unmeasured risk in the tranche — it is a GPU-to-CPU copy, and on some drivers it dominates. Task
3 Step 5 separately checks the blur kernel against the 4.6 ms the design predicts. Both are written as
explicit stop-and-re-review gates. If either comes back wrong the design changes shape, and you want that
before the parity plan re-bases every ladder on top of it.

**It unblocks parity.** Parity D9 defers the opacity floor to blur D8. Parity cannot reach its target
panel opacity until blur lands, because the 80 floor exists only because the shell cannot blur.

**It is safe from a cold start.** Tasks 1 to 3 are mostly new files: vendor the XML, generate a binding,
write a pure blur kernel whose full test bodies are already in the plan. Almost no existing code changes.

**It stays clear of the concurrent session.** The surface-polish pass is working in `internal/shell`; blur
Tasks 1 to 3 touch `protocols/`, `internal/platform/wayland/` and `internal/render/`.

### What not to start with

- **The conformance gate (parity Task 1) standalone.** It lands *red* across `internal/shell` against 105
  sites, by design, and the re-base drives it green. The concurrent pass is working in that package right
  now. Its own design D5 puts it inside the parity plan for this reason; do not lift it out.
- **Parity before blur.** It is the largest change and depends on blur for the floor.
- **Stacking.** It now follows media, and media waits on `sysc-253`.
- **Media.** It needs the spine, and it is the only slice with an external dependency to pin.

## Hazards

These are load-bearing. Each has already cost time in this repository.

- **Never run `go test ./...` or any `-race` build.** A repo-wide race build exhausts memory on this
  machine. Run named tests in one package.
- **The `commit-msg` hook rejects substrings, case-insensitively**: `claude`, `anthropic`, `chatgpt`,
  `openai`, `copilot`, `cursor`, `cody`, `tabnine`, `codex`, `gemini`, `bard`, `gpt-[0-9]`, `llm`,
  `ai assistant`, `bot`, `agent`. Ordinary English trips it — `both` contains `bot`. Screen before
  committing:
  ```bash
  grep -oiE "(claude|anthropic|chatgpt|openai|copilot|cursor|cody|tabnine|codex|gemini|bard|gpt-[0-9]|llm|ai assistant|bot|agent)" msg.txt && echo BANNED || echo clean
  ```
  Do not add a `Co-Authored-By` trailer; it is rejected twice over. Never use `--no-verify`.
- **Every commit carries `.beads/issues.jsonl`** whether you stage it or not — the repository's own
  `pre-commit` hook runs `bd sync --flush-only` and stages it. An explicit path list is not the guarantee
  it appears to be. This is intended behaviour, not a fault.
- **`bd` runs only from `/home/nomadx/sysc-shell`.** From a worktree it needs
  `BEADS_DB=/home/nomadx/sysc-shell/.beads/beads.db`.
- **`bd export` overwrites the tracked JSONL with only what it thinks changed.** Check `wc -l` and
  `git diff` before committing it.
- **`go test ./internal/shell` is safe to run directly.** `runArgvDefault` (`popout_session.go:270`)
  refuses under `testing.Testing()`. Older notes told you to shadow `loginctl` on `PATH`; that is
  obsolete, and building the stub risks masking a real `exec.LookPath` failure.
- **Live Niri needs its environment derived**, and never `pkill -f` a name you also typed:
  ```bash
  export NIRI_SOCKET=$(ls /run/user/1000/niri.wayland-*.sock | head -1)
  export WAYLAND_DISPLAY=wayland-1
  export XDG_RUNTIME_DIR=/run/user/1000
  ```

## Corrections already applied — do not re-introduce

The evidence audit (`2dd478a`) found the original designs had sourced the global ladders correctly and
the component layer not at all. Two errors were corrected in place, and each is marked where it sits:

- **Padding is not 14.** The parity design first claimed "v4's `panelPadding`/`cardPadding` of 14 each".
  Neither constant exists in v4 — both came from **v5**, a different generation. Padding is a margin
  ladder rung per surface: `marginM` 9 inside a card, `marginL` 13 for a panel inset, `marginS` 6 dense.
- **Stacking's consumer is the media card, not the weather card.** The weather card layers a GPU
  `ShaderEffect` over its own rendered content for rain and snow; this shell has no shader stage and that
  card needs no stack.

Two further things to leave alone:

- **`textRoleCount` is derived** — `int(theme.RoleMono)+1`. Parity Task 4 adds `RoleDisplay`; adding it
  without re-deriving that constant indexes past a fixed-size array **while the existing guard test still
  passes**. The plan says so; heed it.
- **The 1.45 contrast floor is not a constant.** `derive()` carries only 4.5 and 3.0; nested separation
  emerges from ladder steps. The change targets two assertions at `theme_test.go:252` and `:255`.

## What is unverified

Do not treat these as settled:

1. **The screencopy readback cost.** Measured nowhere. Blur Task 6 Step 5 measures it live and says stop
   if it exceeds roughly 15 ms.
2. **Capture-before-map ordering.** Read from `spawnPanelLocked` and `openAux`, never observed against a
   live compositor. Blur Task 7 asserts it through a test hook.
3. **The inherited live gate.** `sysc-142` closed without running its ten configurations. The parity
   design re-inherits them, and re-basing every ladder makes them more necessary, not less.
4. **Parity at scale 1.0.** All reference calibration came from a 2× capture. This machine's primary
   output is 3440×1440 at scale 1.0, where no 2× rounding applies.
5. **1.30:1 on a generated palette.** Calibrated on one authored scheme in one mode.

Two divergences are known and deliberately unreconciled: v4's standard panel is 440 wide against our 640,
416 and 700; and v4 settings panes use **no cards** where ours wrap rows in card chrome.

## Coordination with the surface polish pass

`2026-09-11-shell-surface-polish-execution-handover.md` already draws the boundary: that pass owns the
accepted monitor table's visual finish, and "the 2026-09-11 parity tranche now owns the later token,
control-geometry, opacity, and frame-pacing changes."

Respect it in both directions. Do not re-base tokens underneath that pass while it is in flight, and do
not let it harden geometry the parity re-base is about to move. Blur Tasks 1 to 3 avoid the overlap
entirely.

## Tracking and scope

Create bd tasks as you start each slice, under `sysc-202` for blur and smoothness. Record the two blur
measurements in the commit bodies — they are the evidence that retires the epic, and `sysc-202` says in
its own text that it "needs measurements, not a design-first feature epic."

Execution order: **blur → smoothness → parity → media → stacking**, with the template panel following.
Stacking moved behind media when its consumer was corrected.

## Stop

Stop after blur Task 3 Step 5 and report the kernel measurement against the predicted 4.6 ms, before any
compositing work is built on it. If it holds, continue to Task 6 and stop again at its readback
measurement. Those two numbers decide whether the rest of the tranche rests on a sound cost model.
