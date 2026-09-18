# Milestone 9 bar composition handover

Date: 2026-09-17.

Sub-project B is implemented and running on the owner's laptop. It is **not
merged**. This hands over three jobs, in order:

1. **Merge the work**, which is deferred and blocked on the owner's own
   in-flight changes.
2. **Close the defects the owner found live**, listed below with what is known
   about each.
3. **Write and execute the next plan** in the milestone.

Read this, then [`2026-09-17-m9-continuation-handover.md`](2026-09-17-m9-continuation-handover.md)
and [`2026-09-17-m9-execution-handover.md`](2026-09-17-m9-execution-handover.md),
which still hold the settled decisions, the sub-project table and the machine
traps. Everything there is still true unless contradicted here.

## Where the work is

`milestone/m9-bar-composition`, in the worktree at
`/home/nomadx/.config/superpowers/worktrees/sysc-shell/milestone/m9-settings-foundation`.
The worktree's directory name is A's; the branch checked out in it is B's.

It branched from `milestone/m9-settings-foundation` at `1747099`, so it carries
**A, A's polish, and B** — 17 + 2 + 15 commits. Merging this one branch lands
the whole of sub-projects A and B.

Executes [`2026-09-17-bar-composition.md`](2026-09-17-bar-composition.md).
All fourteen tasks are done.

### The merge, and why it is deferred

`main` carries **15 uncommitted files** of the owner's metrics and GPU work,
and a Codex session was actively editing them. Five paths collide with this
branch:

```
.beads/issues.jsonl
internal/shell/controlcenter_pages.go
internal/shell/controlcenter_test.go
internal/shell/panelhost.go
internal/shell/registry.go
```

`panelhost.go` is the sharp one: B rewrites large parts of it and the owner's
work modifies it too. A plain `git merge` refuses to run rather than overwrite
local changes, which is the safe outcome. **Do not stash the owner's work to
get around this.** The branch is committed and loses nothing by waiting.

The owner's plan is to land the metrics/GPU work first, then merge this. Once
`git status` on `main` is clean:

```bash
git merge milestone/m9-bar-composition
```

Expect conflicts in `panelhost.go` around the settings apply path and the
`PanelHost` struct, which B adds five fields to (`alt`, `barAdding`,
`barSelected`, `barOutput`, `barDropHint`, `settingsScroll`).

## What B shipped

The Bar section now opens on a lane editor: three vertical lanes, one row per
widget, with drag, keyboard commands, an inspector, and per-output editing.
The three `bar.items.*` comma-separated strings are gone.

Five things are worth knowing beyond the task list.

- **`config.Write` never emitted `instance` for anything but a plugin
  placement.** The whole identity model would have been inert: the editor mints
  an id, the write drops it, the next load hands back anonymous widgets.
- **`resolveItem` has a branch that replaces the accumulated item wholesale.**
  `item = resolved`, for the seven metric ids, discarded anything set before the
  switch — a nested `cpu` silently lost its id while its group kept one.
  Identity is applied after the switch so a later branch cannot drop it.
- **A drag source now measures and lays out exactly as a button.**
  `render/paint.go` already drew the two through one path while layout measured
  a drag source from its own text alone, so a drag source carrying children
  rendered them with no box.
- **Lanes are vertical stacks, not the horizontal strips the design drew.**
  The default bar does not fit a single row — the right lane alone carries six
  placements including a group of four, against 782 available pixels — and
  `internal/ui` has neither a wrapping row nor a horizontal scroll, so a strip
  could only have clipped. Both reference shells list widgets vertically.
- **`consistentInstances` grew three rules**, not one: a group id may never
  repeat, an id may not name two widget types, and plugin placements keep their
  existing plugin/entry rule. The deliberate cross-output case is now asserted.

## What the owner found live, and what it cost

Every one of these passed the full test suite before the owner saw it. That is
the most important thing in this document.

**Three separate times, green tests hid a live defect.** The pattern is the
same each time: the test asserted the thing the code did rather than the thing
the user needed.

1. **Chips painted as empty pills.** `layoutButtonContent` descends into a
   single row or column child; with two or more it measures each and never
   descends. The chip nested a row, so its grip, label and glyph got no box.
   The chrome-fit conformance test could not catch it: it asserts a box for
   *interactive* nodes, and a label is not interactive.
2. **Remove and group were unreachable.** They lived only in the inspector,
   which renders beneath three stacked lanes: against the default bar the
   viewport ends at y=627 and those controls sat at y=985. Fallout from making
   lanes vertical. They now sit on the chip itself.
3. **Drop resolution used the wrong axis.** `resolveDrop` decided on x, which
   was right for horizontal lanes and meaningless for vertical ones — every row
   shares an x range, so every drop resolved against the first row. Dragging up
   appeared to work, dragging down did nothing, and a group could never be
   dropped into. **The tests kept passing because their fixture was three chips
   at x = 0, 110 and 220** — they went on proving a rule the surface no longer
   used.

Also fixed from live use: the scroll position jumped to the top on every edit,
because `rebuildPanel` replaces the tree and a fresh `KindScroll` starts at
zero; and dragging felt like heavy load because every pointer motion repainted
the whole surface while the paint path reads no drag state at all, so each
repaint redrew identical pixels.

**The lesson for whoever continues.** A test over the node tree proves the tree
has the shape you wrote. It does not prove the surface is usable, reachable, or
resolving on the axis it draws. Check the laptop before believing any of it.

## Still open, and raised by the owner

None of these are tracked in `bd` yet. **Nothing from this session is** — see
the tracker note at the end.

### Reported and not fixed

1. **Settings input fields have an odd height.** At mini density
   `Metrics.InputHeight` is 24, and a 24-pixel control reads as a thin bar. The
   control-sizing pass stopped dropdowns and toggles stretching, which may have
   improved the proportion on its own, so this wants the owner's eye before the
   density ladder is touched — that metric is shared by every surface.
2. **The editor is still rough.** The owner's words. The tooltip, labelling and
   grouping-colour pass helped; it is not finished. Specific candidates: no
   insertion indicator when a drop would insert rather than join (only the join
   target is marked), the add list is 22 unfiltered rows, and a group's members
   cannot be reordered by drag, only by `Alt`+arrow.

### Verified, and deliberately not claimed

- **Two-output behaviour is untested.** Both machines in reach have one output.
  D7's fork-the-whole-lane logic is covered by tests and by nothing else.
- **`bd` holds nothing from this session.** No issue was opened, updated or
  closed for any of B. `sysc-323` is still open and still reads as unstarted.
  The five issues filed during A's polish (`sysc-330` through `sysc-334`) are
  still open and untouched.
- **The blur report was investigated and closed as working.** The owner tested
  with a high-contrast wallpaper and confirmed it. Recorded because the
  investigation produced one wrong answer first: a translucency measurement was
  taken with the control centre closed, so it measured terminals updating. When
  repeated with the panel confirmed mapped, zero panel pixels changed between
  frames and the panel background showed the smooth gradient of the frozen
  blurred backdrop `panelhost.go` documents. **Verify the surface is actually
  mapped before measuring it.**

## Job three: the next plan

`bd ready` is authoritative, and it disagrees with the previous handover.
`sysc-321`, `sysc-323` and `sysc-324` all depend on the epic `sysc-319`, which
is **open**, so none of them read as ready. Close the epic or change the edges
before trusting `bd ready` for M9.

Two sub-projects remain, and their designs are committed:

- **C1, `sysc-321`** — bar edge and reserve space,
  [`2026-09-17-bar-geometry-design.md`](2026-09-17-bar-geometry-design.md).
  Independent of everything, including A and B. The quick visible win: the
  policy already reaches the platform, six placement branches already handle a
  lower bar, and the reload path already re-anchors a mapped surface. **Write
  this plan next** unless the owner wants D first.
- **D, `sysc-324`** — surfaces and behaviour,
  [`2026-09-17-surfaces-and-behaviour-design.md`](2026-09-17-surfaces-and-behaviour-design.md).
  Its sections are settings sections, so A unblocks it the same way A unblocked
  B. Open check carried forward: confirm whether urgency styling was already
  settled by M10's `sysc-315` before implementing that part.

**C2, `sysc-322`** stays gated on `sysc-314`, which is the metrics/GPU work the
owner is landing right now. Check it before touching `ui.ArrangeBar`.

Both plans should inherit what B learned: round-trip every mutation through
`Write` and `Load`, give every column a width, and **check it on the laptop
before calling it done**.

## Machine traps, in addition to the earlier lists

**The `commit-msg` substring match fired four times this session**, on `both`
every time, in ordinary sentences. Screen every message before committing:

```bash
grep -qiE "(claude|anthropic|chatgpt|openai|copilot|cursor|cody|tabnine|codex|gemini|bard|gpt-[0-9]|llm|ai assistant|bot|agent)" msg.txt && echo BANNED
```

**Committing from the worktree needs `BEADS_DB`**, exactly as recorded:

```bash
env BEADS_DB=/home/nomadx/sysc-shell/.beads/beads.db git commit -F msg.txt
```

**The JSONL truncation trap fired on `bd create`** and dropped 298 rows to 1.
The repair recipe works, with one addition the earlier handover did not have:
**splice the raw lines**. A full export strips the `comments` array from the
eleven issues that carry them, so the rebuild must keep HEAD's line for every
id not touched — and it must keep the *line*, not a re-serialised row, because
`bd` writes `id` first and `json.dumps(sort_keys=True)` rewrites all 298
untouched rows into a useless diff.

**One package per `go test` invocation, and the rule has teeth beyond `-race`.**
`go test ./internal/shell ./internal/settings ./internal/plugin` in one command
blocked at near-zero load for over four minutes. Each package runs in 13s, 0.01s
and 0.8s on its own.

**Driving the shell remotely.** `wtype` injects into the compositor's focused
surface, which is whatever terminal has focus, **not** the layer-shell panel —
20 Tab presses went into the owner's Codex session. Panels open over IPC:

```bash
python3 - <<'EOF'
import socket, json
s = socket.socket(socket.AF_UNIX, socket.SOCK_STREAM); s.settimeout(5)
s.connect("/run/user/1000/sysc-shell/ipc.v1.sock")
s.sendall((json.dumps({"id": 1, "method": "panel.open",
    "params": {"panel": "settings", "section": "Bar"}}) + "\n").encode())
print(s.recv(4096).decode())
EOF
```

`nc` is not installed on the laptop. Panel names are `control-center`, not
`controlcenter`. `grim` and `slurp` are installed; `rsync` is not, so use
`tar czf - … | ssh … tar xzf -`.

**Deploying to the laptop**, which is `ssh -p 7777 nomadx@192.168.0.64`,
`eDP-1`, 1920x1080 at scale 1.25:

```bash
tar czf - sysc-shell | ssh -p 7777 nomadx@192.168.0.64 \
  'cat > /tmp/s.tgz && cd /tmp && tar xzf s.tgz &&
   install -m755 /tmp/sysc-shell ~/.local/bin/sysc-shell &&
   systemctl --user restart sysc-shell'
```

**The schema change is one-way.** A configuration carrying instance ids will
not load on a shell without D2. The laptop's pre-B configuration is kept at
`~/.config/sysc-shell/config.json.pre-m9b`; roll it back alongside the binary
if reverting to `~/.local/bin/sysc-shell.pre-m9`.

**The icon subset can be re-cut and it reproduces byte for byte.** Four names
were added this session (`drag_indicator`, `tune`, `link`, `link_off`). The
upstream is at the URL in `SOURCE.md` and verified against its pinned SHA-256
before it is read. Adding a glyph means `build.py`, `materialIcons` in
`materialfont.go`, and `materialInventory` in its test, which drives the
rasterisation coverage that catches a name the font lacks. `SOURCE.md` was
stale before this session and is now correct.

## Gate

Unchanged, and still not `AGENTS.md`'s repository-wide race gate:

```bash
timeout 90s env GOMAXPROCS=2 go test -count=1 <pkg> -run <Name>
```

`gofmt -w . && test -z "$(gofmt -l .)"` and `go vet ./...` are required; a
whole-tree vet is safe with `-p 2` and `GOMAXPROCS=2`.

At the close of this session: `gofmt` clean, `go vet ./...` clean, `go.mod` and
`go.sum` untouched, and `internal/config`, `settings`, `shell`, `ui`, `render`
and `wallpaper` each passing on their own.
