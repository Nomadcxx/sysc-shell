# Milestone 9 execution handover

Date: 2026-09-17.

This commissions the implementation of Milestone 9 sub-project A, and hands over
enough context to carry the milestone forward afterwards — including the design
work that deliberately remains unwritten and why.

Read this, then `docs/plans/2026-09-15-settings-foundation-design.md` and
`docs/plans/2026-09-17-settings-foundation.md`. Nothing else is required before
your first commit.

## What you are being asked to do

1. **Implement sub-project A** from `2026-09-17-settings-foundation.md`, issue
   `sysc-320`. Thirteen TDD tasks. This is the whole of your first assignment.
2. **Then continue the milestone**, which means writing plans for B, C and D
   from their committed designs, and doing the remaining design work described
   at the end of this document.

Do not start sub-project E. Its shape is an open product question, set out
below.

## Why Milestone 9 exists

The shell models twelve configuration domains and exposes five. Concretely:
`Weather`, `Wallpaper`, `Tray`, `Outputs`, and `Plugins.Enabled` are parsed,
validated, and unreachable from any user interface.

The owner's stated driver was narrower and sharper: **bar widget arrangement is
reachable only as three comma-separated strings** (`bar.items.left`,
`bar.items.center`, `bar.items.right`) in a settings pane that is otherwise a
flat property grid. There is no add, no remove, no reorder, and no per-instance
option. The bar is also fixed to the top edge, because `Bar.Edge` is parsed and
validated and then never reaches the surface that anchors the bar.

Milestone 9 is therefore "user control of the shell": make what the shell
already does reachable, and make the bar the user's to shape. Parity with DMS
and Noctalia v4 is the quality bar, not a porting exercise.

## The five sub-projects

| | Sub-project | Design | Plan | Issue |
|---|---|---|---|---|
| A | Settings foundation | `2026-09-15-settings-foundation-design.md` | `2026-09-17-settings-foundation.md` | `sysc-320` |
| B | Bar composition | `2026-09-15-bar-composition-design.md` | — | `sysc-323` |
| C | Bar geometry and placement | `2026-09-17-bar-geometry-design.md` | — | `sysc-321`, `sysc-322` |
| D | Surfaces and behaviour | `2026-09-17-surfaces-and-behaviour-design.md` | — | `sysc-324` |
| E | New subsystems | not designed — see the end | — | — |

Epic: `sysc-319`. Clipboard history is `sysc-205`, already landed, and is not
part of E.

**A is the spine.** B's widget inspector needs entries synthesised at runtime
for one specific `config.Item`, which the current switch pair cannot express;
D's sections are settings sections. Both are blocked on A in the tracker, and
that is not bureaucratic — it is a real interface dependency.

## Sequencing

- **A** first. Nothing blocks it.
- **C1** (`sysc-321`, bar edge and reserve space) is independent of everything,
  including A. If you want a quick, visible win between larger slices, it is
  the one: the bar's policy already reaches the platform, six placement
  branches already handle a lower bar, and the reload path already re-anchors a
  mapped surface.
- **B** and **D** after A.
- **C2** (`sysc-322`, vertical axis and overflow) is gated on **`sysc-314`**,
  Milestone 10's centre-composition work, because both change `ui.ArrangeBar`.
  That function carries a documented collision total order, and three
  concurrent rewrites of one invariant is how the invariant dies. Check
  `sysc-314` before touching it.

**Milestone 10 is in flight and touches the bar.** The boundary was checked,
not assumed: no M10 design references bar edges, exclusive zones, auto-hide, or
`supportedEdges`, and every "anchor" in the M10 set means the wordmark's centre,
not layer-shell anchoring. M10 owns what the bar *contains*; M9 sub-project C
owns where the bar *is*. The single overlap is `ArrangeBar`.

## Machine traps — read before your first command

These will cost you hours or an outage if you learn them the hard way.

**Never run `go test` with `-race` or with `./...`.** A repository-wide race
build hard-locks this workstation — zram-only swap against 16-way linking — and
`.cursor/hooks/deny-go-race.py` refuses both forms outright. `AGENTS.md` still
prescribes the full-tree race gate; it is unrunnable here. Use:

```bash
timeout 90s env GOMAXPROCS=2 go test -count=1 <pkg> -run <Name>
```

One package per invocation. `gofmt` and `go vet ./...` are fine and required.

**Do not install a `loginctl` shim.** Older plans prescribe one so that
`go test ./internal/shell` does not log you out. It is obsolete:
`internal/shell/popout_session.go` guards with `testing.Testing()` and checks
`argv[0]`. Verified 2026-09-17. Shadowing `loginctl` now only hides real
behaviour.

**The `commit-msg` hook rejects AI attribution by naive substring match**, and
ordinary English trips it. The pattern includes `bot`, `agent`, `cursor`,
`codex`, `llm`, `bard`, `cody`. So **`both`, `bottom` and `precursor` all
fail** — which matters constantly in this milestone, because sub-project C is
about the *lower* bar edge and you cannot use the obvious word. Screen every
message before committing:

```bash
grep -qiE "(claude|anthropic|chatgpt|openai|copilot|cursor|cody|tabnine|codex|gemini|bard|gpt-[0-9]|llm|ai assistant|bot|agent)" msg.txt && echo BANNED
```

It also rejects `Co-Authored-By:` lines addressed to those domains, and
`generated with`/`generated by`. Add no attribution trailer.

**The bd `pre-commit` hook stages the tracker into every commit.** It runs
`bd sync --flush-only` then an unconditional `git add .beads/issues.jsonl`. A
commit that explicitly stages three unrelated paths therefore lands four files,
and whatever tracker state is pending — including another person's in-flight
work — rides along under your message. For a commit that must not carry tracker
state, use `git commit --no-verify` and screen the message yourself.

**`bd` auto-flush truncates the tracked JSONL, and a full export drops
comments.** Both happened during this session. Every `bd` write may rewrite
`.beads/issues.jsonl` with only recently-changed issues — it went to 1 line
against 292 at HEAD, then to 12 lines. Always check before staging it:

```bash
wc -l .beads/issues.jsonl
git show HEAD:.beads/issues.jsonl | wc -l
```

The repair is *not* just re-exporting: the cursor lives in the `export_hashes`
table, so a plain `bd export` emits nothing, and a full export **strips the
`comments` array** from the eleven issues that carry them. The recipe that
works:

```bash
sqlite3 .beads/beads.db "DELETE FROM export_hashes;"
bd export -o /tmp/full.jsonl
git show HEAD:.beads/issues.jsonl > /tmp/head.jsonl
# then keep HEAD's row for every id you did not touch, and take the fresh
# export's row only for the ids you did; verify line count, comment count,
# duplicate ids, and that every row parses, before installing it.
```

**Never `git checkout -- .beads/issues.jsonl`.** A `post-checkout` hook
re-imports it and mints *new* ids for rows it cannot reconcile. This has
created duplicate issues twice. To restore it, write it out of the object store
instead: `git show HEAD:.beads/issues.jsonl > .beads/issues.jsonl`.

**Verify anchors before trusting them.** Line numbers in every committed design
and plan drift. Between 2026-09-15 and 2026-09-17, `panel.go`'s edge branches
moved from 93/98 to 101/108/113, `exclusiveBarZone` from 118 to 133, the bar
root from 501 to 504 — all with identical content. Grep by *name*, never
re-read a line range. There is a memory note about exactly this class of
failure.

**The owner commits to `main` concurrently.** During this session a commit
landed between one commit and the `git reset --soft HEAD~1` meant to amend it,
so `HEAD~1` resolved to the wrong commit and republished the owner's work under
the wrong message. Nothing was lost, but if you rewrite history, check
`git rev-parse HEAD` and `git diff --cached --name-only` *inside the same
command* that rewrites, and abort if either moved.

## Decisions that are settled

Do not reopen these. Each was decided with the owner, and the reasoning is in
the design documents.

**A.** Typed accessor entries replace the `Get`/`Set` switch pair. Live apply,
debounced, with per-entry reset. Plain grouped column, **no cards** — settings
becomes the one surface in the shell that is not card-composed, deliberately.
One full panel plus a control-centre *shortcut*, not two settings trees.
Validated hex fields instead of a colour picker, because `internal/ui` carries
no colour type and `ui.Fill` is a closed semantic enum; adding one would be the
node tree's first non-semantic colour, against `sysc-265`'s direction.

**B.** Editing happens in the settings pane, not on the live bar — `Bar.Handle`
has no drag state at all, while `PanelHost` already has drag wired end to end
and unused. Instance ids extend to built-in widgets **and groups**, minted
lazily. Typed options stay on `config.Item` so `resolveItem`'s per-id
validation survives. Full group editing. Per-output lanes at **lane**
granularity, because `applyBar` inherits or replaces a lane whole and no
per-item override exists.

**C.** All four edges in the model. `ArrangeBar` becomes axis-aware rather than
forked. C owns overflow (`sysc-313`): whole items drop in declaration order
behind a visible indicator — never silent clipping, never refusal at config
load, because plugin widgets come and go at runtime. Auto-hide, floating
margins and per-output geometry are **out of scope**, which is what preserves
the deliberate "gap inside the surface, zero layer margin, screen edge stays
clickable" invariant.

**D.** Notification settings are **presentation only**. The shell connects to
`sysc-notify` as `RolePresenter`; retention, capacity, eviction and filters
belong to that service, and a shell-side copy would be a second source of truth.
Do-not-disturb persists as a preference and never as a deadline.

## What is not verified

Carried forward honestly from each design. Check these during planning or
implementation; do not assume them away.

**A.** Nine candidate Material ligature names are unconfirmed against the
pinned Material Symbols source — a name the font lacks shapes to nothing and
paints an invisible control. Per-connector `Outputs` overrides have never had an
interface, so the Displays section's editing model may need its own decision.

**B.** Six ligatures unconfirmed. Display names for the 22-item widget
vocabulary are unwritten, and several ids read poorly as labels. Whether a
plugin-supplied widget needs different chip treatment from a built-in one.
Whether a lane override interacts badly with a plugin placement's instance id.

**C.** Whether Niri honours an exclusive zone of **zero** on a surface that
still has a size, or treats it as un-reserving — the live gate answers this
before the configurable reserve is claimed to work. Whether concave fillets
mean anything on a vertical bar edge. How the density ladder reads on a side
bar, given every control size derives from a base widget sized for a horizontal
band. Whether any widget's text needs rotation.

**D.** Which behavioural options the launcher service actually exposes — the
shell's own file carries only chrome constants, so the surface may live in the
pinned `sysc-launch` library and may need extending there. Whether toast
placement should share the OSD's nine-position vocabulary (attractive, possibly
wrong: a toast stack grows, an OSD does not). **Whether urgency styling was
already settled by Milestone 10's notification child, `sysc-315`** — confirm
before implementing that part of D.

## The design work that remains, and its shape

Sub-project E is listed on the roadmap as night light, idle behaviour,
screenshot, hooks, keybinds and dock. **Do not write one "sub-project E
design".** That is six subsystems, each needing its own service, surfaces and
failure behaviour, exactly as the Bluetooth and network slices each did. One
document covering all six would be written far ahead of its code and would
drift before execution — this project has a standing memory note about plan
documents drifting from the code, and it was earned.

The valuable design work, in order:

1. **Decompose E.** What are the slices, what does each own, what order, and
   which are even wanted. Two are genuine product questions rather than
   sequencing ones: a **dock** is a significant addition, and **keybinds** may
   belong to Niri's own configuration rather than the shell's. `AGENTS.md` bans
   a lock screen outright, so nothing in E may grow one.
2. **Answer the control-vocabulary question.** A's design excluded list
   editors, string-map editors and keybind capture on the explicit grounds that
   *"they have no consumer in A. They belong to B and E and should enter with
   one."* E is where those consumers arrive. Walk E's slices against that
   exclusion and settle whether A's vocabulary needs extending — ideally before
   A ships, so it is a known extension point rather than a retrofit.
3. **Then design E's first slice**, and plan B, C and D.

That second item is the reason the owner chose to keep designing rather than
implement immediately, and it is worth honouring.

## Conventions

- Every design, plan and handover gets a row in `docs/plans/README.md` **in the
  same commit that adds the document.** A document not in the register is one
  the project loses; this rule exists because Milestone 2's 16-task plan was
  executed and never committed and no longer exists on any branch.
- Naming is `YYYY-MM-DD-<topic>[-<kind>].md`. No kind suffix means the
  implementation plan.
- **Status lives in bd, never in a document header.** A design states its
  decisions; `bd ready` and `bd blocked` state whether work is done, in flight
  or gated.
- Commit `.beads/issues.jsonl` with the work it describes — subject to the
  truncation check above.
- Land design and plan documents on `main` as docs-only commits, not on a
  feature branch.
- Work from a worktree, and run `bd` from `/home/nomadx/sysc-shell`, never from
  a worktree — the SQLite database exists only in the primary checkout.

## Live gate

The agent shell inherits none of the compositor environment:

```bash
export NIRI_SOCKET=$(ls /run/user/1000/niri.wayland-*.sock | head -1)
export WAYLAND_DISPLAY=wayland-1
export XDG_RUNTIME_DIR=/run/user/1000
```

This machine has one output, `DP-1` at 3440x1440 and scale 1.0, so any
two-output check is unrunnable here and must be deferred rather than claimed.
`niri msg -j layers` is the live assertion for mapped surfaces. Never `pkill -f`
a binary name you also typed in the command — it matches your own shell. Kill by
pid from `pgrep -f 'scratchpad/<name>'`.
