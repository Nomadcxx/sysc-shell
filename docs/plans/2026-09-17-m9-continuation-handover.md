# Milestone 9 continuation handover

Date: 2026-09-17.

Sub-project A is implemented and closed. This hands over two jobs, in order:

1. **Polish what A left rough**, including the items nobody has flagged yet.
2. **Write and execute the next implementation plan** in the milestone.

Read this, then [`2026-09-17-m9-execution-handover.md`](2026-09-17-m9-execution-handover.md),
which still holds the settled decisions, the sub-project table and most of the
machine traps. Everything there is still true unless contradicted below.

## Where the work is

The implementation is **not on `main`**. Seventeen commits sit on
`milestone/m9-settings-foundation`, in the worktree at
`/home/nomadx/.config/superpowers/worktrees/sysc-shell/milestone/m9-settings-foundation`.

It was branched from `main` at `00901b9`. `main` has moved since — the weather
branch landed, and `main` is now past `59741f6`. **The branch has not been
merged or rebased**, and the merge will not be clean: the owner had
uncommitted work in `internal/shell/panelhost.go` and
`internal/shell/controlcenter_pages.go`, which are two of the files A rewrites.
Check `git status` on `main` before attempting it, and expect conflicts in
`panelhost.go` around the settings apply path regardless.

The owner's laptop (`ssh -p 7777 nomadx@192.168.0.64`) is **running this
build**, installed over `~/.local/bin/sysc-shell`. Their previous binary is
kept at `~/.local/bin/sysc-shell.pre-m9`, and a source copy of the branch is at
`~/m9-test`. To revert:

```bash
install -m755 ~/.local/bin/sysc-shell.pre-m9 ~/.local/bin/sysc-shell
systemctl --user restart sysc-shell
```

That laptop is `eDP-1`, 1536x864 logical at **scale 1.25**. It is the only
machine in reach that is not 3440x1440 at scale 1.0, and it earned its keep:
it caught a clipping defect that every test and the first live check missed.
**Use it.** A pane that looks right on the workstation is not evidence.

## What A actually shipped

Beyond the thirteen planned tasks, three things worth knowing:

- **`bar.edge` used to offer an edge the loader refuses outright.** The shipped
  pane could write a configuration the shell then declined to start from. The
  entry now offers only what loads. A round-trip test writes every enum option
  and reads it back through `config.Load`; it is the guard on every vocabulary
  `internal/settings` restates, and it found this, the reverse-domain plugin id
  rule, and the source/seed pairing within a minute of being written.
- **`sysc-107` was two defects.** The missing stock picker was the visible
  half. The other was that changing `appearance.source` alone left a seed the
  new source cannot read, so the shell refused its own configuration.
- **The design's claim that `Plugins.Enabled` was unreachable is wrong.** The
  Plugins section already renders the plugin host's own view, whose per-plugin
  switch is keyed on what is *installed* rather than on what configuration
  names. No second switch was added, and the coverage invariant deliberately
  omits the `plugins.` prefix and says why. Tray, Outputs, Weather and
  Wallpaper were checked the same way and genuinely had no surface.

## Job one: polish

### Flagged by the owner, and done

All three are fixed on the branch and deployed to the laptop. They are here so
the next reader knows what the fix was, not as outstanding work.

- Rail tabs carry tooltips. They draw glyphs only, so the name was reachable by
  screen reader and by nothing else.
- Input fields were a hardcoded 200 pixels whatever the surface. They now fill
  the control column.
- The plugin view's switches stretched the whole surface, because the plugin
  host's card column carries no width of its own. It now gets the same bounded,
  scrolling column a section gets.

### Flagged by the owner, and not done

**The plugin cards are still the plugin host's own composition** — a meta line,
a switch, a Retry button, then per-setting rows — which is not the row anatomy
the rest of the pane now uses. Bounding the column stopped it eating the
screen; it did not make it read like settings. Giving plugin settings the
label-over-caption row treatment means editing `internal/shell/popout_plugins.go`
and `pluginSettingRow`, not the settings pane, so it wants its own issue and
its own decision about how much of `Entry`'s anatomy a plugin-declared setting
can borrow.

### Not flagged, and worth fixing

Found while building it. None of these are tracked yet.

1. **The hex pattern exists twice.** `hexPattern` in
   `internal/settings/registry.go` and `settingsHexPattern` in
   `internal/shell/popout_settings.go`, both restating `config`'s unexported
   `colorPattern`. Three copies of one rule. The setter and the field must
   agree or a value marks itself valid and is then refused, so this should be
   one exported rule.
2. **The font picker is not searchable.** D8 says "searchable picker"; what
   ships enumerates `fontscan.SystemFonts`, dedupes and sorts, then hands the
   list to the ordinary `Menu`. On a machine with two hundred families that is
   a wall. The filtering half was never built.
3. **Font families are shown normalized.** `dejavusans`, not `DejaVu Sans`.
   D10 allowed this explicitly and called deriving a display form its own
   decision. It still is, and it looks wrong in the pane.
4. **`settingsResetWidth` and `settingsSearchWidth` are literals** (64 and
   260). They are exactly the kind of hardcoded chrome dimension `sysc-265` is
   shrinking, added by this work.
5. **Weather unit and interval do not set `Configured`.** `config.Write` only
   emits a weather block when `Configured` is true, so changing the unit on an
   unconfigured weather is silently dropped at the write. Setting a place sets
   the flag. The alternative — having unit set it — pins the location to
   0,0, which is worse, so this needs a real decision rather than a patch.
6. **Tray and Displays are empty until configuration already names something.**
   Both are keyed on what the configuration carries, so a user with no tray
   preferences and no output override sees two empty sections with no way to
   create the first entry. Tray's answer is the live tray host, which enumerates
   items; Displays' answer belongs to sub-project C. Until then the sections
   should at least say why they are empty.
7. **A described row measures its own height.** Every other row takes
   `Metrics.StandardControl`. A fixed height cropped the caption, so described
   rows opt out, which means the density ladder governs the text but not the
   row box. It is honest but it is not what the design says.
8. **`expandTilde` was added to `internal/shell`.** Check whether the wallpaper
   or template code already had one before keeping a second.
9. **No test asserts the control-centre shortcut lists every section.** It
   builds from `settingsSections`, so it cannot drift, but nothing says so.

### Verified, and deliberately deferred

- **Two-output behaviour is untested.** Both machines in reach have exactly one
  output. Do not claim per-output behaviour works; nothing has exercised it.
- **A's control vocabulary question is now a retrofit.** The previous handover
  wanted the list, string-map and keybind editor exclusion settled *before* A
  shipped, so it would be a known extension point. A shipped. That is not
  fatal — `Kind` grew three members cleanly this session, which is evidence the
  seam works — but the question is now answered against existing code.

## Job two: the next plan

`bd ready` is authoritative. As of this handover, with A closed, both
`sysc-323` (B, bar composition) and `sysc-324` (D, surfaces and behaviour) are
ready, and `sysc-321` (C1, bar edge and reserve space) was never blocked.

**Write B's plan first.** Bar widget arrangement is the owner's stated driver
for the whole milestone — it is reachable today only as three comma-separated
strings — and A exists to make B possible. Its design is committed at
[`2026-09-15-bar-composition-design.md`](2026-09-15-bar-composition-design.md)
and its decisions are settled; do not reopen them.

A now gives B what it was waiting for: `Entry` carries typed accessors, so an
entry can be constructed at runtime over one specific `config.Item`, which the
switch pair could not express. That is the whole reason B was gated on A.

Two things B should inherit from this session:

- **The round-trip test pattern.** Write every option through the entry, then
  `config.Write` and `config.Load` it back. It found three live defects in
  A within a minute. B changes the item lists themselves, so it needs this more
  than A did.
- **The layout ownership lesson.** A's clipping defect was a node with no
  width inside a row that right-pins. B composes chips and lanes into the same
  pane. Give every column a width, and check it on the laptop.

`sysc-321` (C1) remains the quick, visible win if a smaller slice is wanted
between larger ones. `sysc-322` (C2) is still gated on `sysc-314`; check that
before touching `ui.ArrangeBar`.

## Machine traps learned this session

These are in addition to the previous handover's list, which all still holds —
the `commit-msg` substring match caught `both` in a commit message during this
session, exactly as documented.

**The bd duplicate-id trap fired again, and cost an hour.** A re-import minted
five new issues rather than reconciling rows: three byte-identical copies of an
in-progress issue, and two carrying closures that belonged to `sysc-107` and
`sysc-320`, whose originals reverted to open. The repair is to restore the
closures on the canonical ids and delete the copies, but note:

- **Deleting a duplicate drops the dependency edges it collected.** `bd delete`
  reported six orphaned issues. Check the graph against the committed JSONL
  afterwards; here the real edges survived and `sysc-323`/`sysc-324` correctly
  became ready once A closed.

**JSONL truncation fires on read commands, not only writes.** This is new and
it is the sharper version of the known trap. A rebuilt 295-row file was
truncated to 6 rows by `bd ready`, `bd show` and `bd blocked` run between the
rebuild and the commit — and the truncated file was committed. **Rebuild the
file and commit it in the same command, with no `bd` invocation between them**,
then verify with `git show HEAD:.beads/issues.jsonl | wc -l`.

**`sysc-shell` has no argument parsing.** `--help` starts the shell rather than
printing usage. There is no client mode; drive a running instance over its IPC
socket at `$XDG_RUNTIME_DIR/sysc-shell/ipc.v1.sock` with a newline-terminated
JSON object, for example
`{"id":1,"method":"panel.open","params":{"panel":"settings","section":"Wallpaper"}}`.
That is how the live gate opened the panel at a section.

**`rsync` is not installed on the laptop.** Use `tar czf - … | ssh … tar xzf -`.

**The font subset can be re-cut.** `internal/render/icons/material/build.py`
verifies the pinned upstream's SHA-256 before reading it, the upstream is
reachable, and `fontTools` is installed. Thirteen names were added this way.
Adding a glyph means the builder, `materialIcons` in `materialfont.go`, and
`materialInventory` in its test, which drives rasterisation coverage.

## Gate

Unchanged, and still not `AGENTS.md`'s repository-wide race gate, which cannot
run here:

```bash
timeout 90s env GOMAXPROCS=2 go test -count=1 <pkg> -run <Name>
```

`gofmt -w . && test -z "$(gofmt -l .)"` and `go vet ./...` are required. A
whole-tree `go vet` is safe with `-p 2` and `GOMAXPROCS=2`.

At the close of this session: `gofmt` clean, `go vet ./...` clean, `go.mod` and
`go.sum` untouched, and `internal/settings`, `shell`, `config`, `render`, `ui`
and `theme` each passing on their own.
