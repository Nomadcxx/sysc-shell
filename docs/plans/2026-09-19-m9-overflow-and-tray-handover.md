# M9 handover: bar edge landed, overflow half-done, tray daemon found stopped

Date: 2026-09-19.

Three jobs came out of this session, in the order the next one should take them:

1. **Fix the tray.** The daemon was never running on the laptop. It runs now and
   still paints nothing. Unfinished, and first.
2. **Finish the overflow slice.** The clipping half is done and green; the
   indicator chrome was never started, and an elastic item can still be granted
   a sliver.
3. **Then the rest of M9**, which is unchanged apart from one design premise
   that has gone stale.

Read this with [`2026-09-18-bar-edge-and-reserve.md`](2026-09-18-bar-edge-and-reserve.md)
and [`2026-09-19-bar-overflow.md`](2026-09-19-bar-overflow.md), which carry the
decisions and the task lists. Everything there is still true.

## Job one: the tray

### What was wrong, and what is now fixed

`sysc-tray` is **a released dependency, not unpushed local work** —
`github.com/Nomadcxx/sysc-tray v0.1.0-rc.1` in `go.mod`, consumed by
`internal/trayclient`. Nothing is missing from the repository.

It is a **separate daemon**. `sysc-shell` never spawns it; `trayclient` connects
to a presenter socket under `/run/user/1000/sysc-tray/`. The shell is a client
and paints whatever that socket reports.

On the laptop the binary was installed and **had never been started**:

```
~/.local/bin/sysc-tray   7600731 bytes, dated Sep 7 21:38
pgrep -x sysc-tray       NOT RUNNING
/run/user/1000/sysc-tray/  No such file or directory
```

No socket, so `trayclient` had nothing to connect to, and no
`org.kde.StatusNotifierWatcher` on the bus because that watcher lives inside
`sysc-tray`.

A unit now exists at `~/.config/systemd/user/sysc-tray.service`, enabled and
started, matching the shape of `sysc-notify.service`:

```ini
[Unit]
Description=sysc tray presenter

[Service]
Type=simple
ExecStart=%h/.local/bin/sysc-tray
Restart=on-failure
RestartSec=2s

[Install]
WantedBy=default.target
```

After that: service active, `presenter.v1.sock` present,
`org.kde.StatusNotifierWatcher` owned by `sysc-tray`, and two items registered:

```
as 2 ":1.27/org/ayatana/NotificationItem/nm_applet" ":1.40614/org/blueman/sni"
```

### What is still broken

**The bar paints no tray icons.** The region between the centre and the right
section stays empty across two `sysc-shell` restarts taken *after* the daemon
was up, and the shell logs no tray or presenter lines at all.

Two candidates. Neither was verified, and the first is much cheaper:

1. **The items are `Passive`.** `arrangeTray` in `internal/shell/trayprefs.go`
   skips `tray.StatusPassive` before anything else:

   ```go
   if item.Status == tray.StatusPassive {
       continue
   }
   ```

   `nm_applet` and `blueman` both commonly publish Passive when they have
   nothing to report. **If this is the cause there is no bug at all**, and an
   app that publishes Active would appear. Check this first: read each
   registered item's `Status` property off the watcher before touching code.
2. **Protocol skew.** The laptop's `sysc-tray` binary is dated **Sep 7**; the
   `sysc-shell` binary on that laptop is the sub-project B build from
   **Sep 17–18**, talking to `presenter.v1.sock`. A version mismatch would
   produce exactly this silence. Compare the `sysc-tray` version the laptop
   binary reports against the `v0.1.0-rc.1` in `go.mod`.

### The question this handover cannot answer

**Why the tray works on the desktop is not established.** The evidence is
genuinely confusing and should not be guessed at:

- Both machines have `~/.local/bin/sysc-tray`.
- **Neither machine has a `sysc-tray.service`** (before this session) — checked
  with `systemctl --user list-unit-files | grep -i sysc`.
- **Neither `niri` config has a `spawn-at-startup` for it.** The laptop's config
  has a *commented-out* `sysc-shell` spawn at line 129, left from before it
  moved to systemd.
- The desktop has only `sysc-shell` and `sysc-walls` as user units; the laptop
  has `sysc-shell`, `sysc-notify`, `sysc-clipboard` and `sysc-walls`. **The
  laptop is the better-provisioned machine**, which makes "the desktop has
  something the laptop lacks" the wrong shape of assumption.

So something outside both mechanisms starts it on the desktop, or it is started
by hand there. **This could not be checked from this session**: the desktop's
compositor session is not up — `sysc-shell` is inactive here and there is no
Niri socket in `/run/user/1000` — so there was nothing running to inspect.

The owner's hypothesis, that the desktop runs an **older binary**, is worth
testing directly and is consistent with candidate 2 above: if the desktop's
`sysc-shell` predates a presenter protocol change, the pair there would match
while the laptop's pair does not. Compare the two `sysc-tray` binaries by
checksum and date, and the two `sysc-shell` builds by what they were built from.

**Do this next, in this order**, on the laptop with the compositor up:

1. Read the `Status` of both registered items. If both are `Passive`, launch
   something that publishes an Active item and confirm the tray populates. That
   likely closes the whole question.
2. If they are Active and still absent, compare the `sysc-tray` protocol version
   against what the shell expects, and rebuild the laptop's `sysc-shell` from
   current `main` so both sides are known.
3. Only then look at `trayclient` itself.

## Job two: finish the overflow slice

`sysc-313`, on branch `milestone/m9-bar-overflow`, worktree
`/home/nomadx/.config/superpowers/worktrees/sysc-shell/milestone/m9-bar-overflow`,
branched from `main` at `c8fe579`. **Four commits, unmerged, full gate green.**

```
fb20c05 docs(plans): record the elastic item decision
7bf110d feat(ui): let elastic bar items shrink instead of vanishing
a5d1a2d feat(ui): place bar items whole or not at all
0dee4e8 docs(plans): plan the bar overflow slice
```

### What it does

`placeSection` no longer grants a fraction of an item. Fixed items take their
natural width, elastic items share the remainder, and only when the fixed items
alone do not fit does a section drop whole items from the far end. Dropped items
get the zero `Rect`. `ArrangeBar` returns `BarOverflow{Left, Center, Right}`;
`Bar.Overflow()` exposes the last layout's counts.

Two pieces of it are easy to miss:

- **`elastic()` descends into a capsule.** `capsuled()` in
  `internal/shell/widget.go` wraps every bar widget in a `KindCapsule`, so a
  `MaxWidth` check on the placed node reads *every* widget as fixed — including
  the titles the rule exists to protect.
- **One partial grant survives on purpose.** A centre wider than the whole band
  has nowhere to be dropped into, so `placeTruncating` gives it the band and the
  painter ellipsizes inside. Documented at the call site.

### What is left

1. **The indicator chrome was never started.** This is the larger remaining
   piece. `sysc-313`'s bug had two symptoms — a label cut mid-glyph *and* items
   vanishing with no diagnostic. **Only the clipping is fixed.** The count is
   recorded and nothing draws it, so from the user's seat the vanishing half
   looks exactly as it did before. `sysc-313` must stay open until this lands.
   Task T4 in the overflow plan stops deliberately at this line, because the
   chrome is bar composition's surface and wants the owner's eye.
2. **The elastic sliver floor.** Nothing stops an elastic item being granted two
   pixels when the remainder is tiny. A floor was considered and not added — the
   owner chose option 1 (shrink) over option 3 (shrink to a floor, then drop).
   **Untested on hardware**: proving it needs the *left* section squeezed, and
   the session restored the laptop instead of starting another config cycle.
3. **The reserve is plumbed but unused.** `placeSection` takes a reserve and
   every caller passes zero. It exists so the indicator's extent can be taken
   before selection; wire it when the chrome lands.

### Live gate results, already run

On the laptop, `eDP-1`, 1920x1080 at scale 1.25:

- **Regression clean.** At full width the bar is identical to before. This is
  the most important result: neither C1 nor the overflow work moves a bar that
  fits.
- **Overflow correct.** With 21 right-side items instead of 7, the surplus is
  dropped whole — no label cut mid-glyph, no zero-width slivers, survivors
  flush to the edge.
- **No indicator**, as expected, per the gap above.

## Job three: the rest of M9

- **`sysc-322`, C2's vertical axis.** Its real gate `sysc-314` **has landed**
  (`c6a289a`, closed in `3ceffe7`), so it is technically unblocked. But see the
  stale premise below before planning it.
- **`sysc-324`, sub-project D.** Ready in `bd`, but its sections are settings
  sections, so it wants sub-project B merged first rather than stacking another
  branch on an unmerged one.
- **`sysc-323`, sub-project B.** Implemented, unmerged, on
  `milestone/m9-bar-composition`. `main` is clean now, so **the merge that was
  blocked all session is available**. Expect conflicts in `panelhost.go`.

### A design premise that has gone stale

The bar geometry design rejected forking `ArrangeBar`:

> Forking a vertical twin of `ArrangeBar` was rejected … Duplicating that rule
> into a second function would leave two copies of one invariant, and the copy
> drifts.

**`sysc-314` forked it anyway.** `internal/ui/bar.go` now dispatches on
`wordmarkIndex` into `arrangeAnchoredWordmark` and `arrangeBarSections`, with a
comment saying "a second anchored composition needs its own layout contract".
The total-order doc comment above `ArrangeBar` still describes one rule that has
two implementations.

D3's "generalise one function to main and cross axis" therefore no longer
describes the code. **The fork's resolution is an open decision** and was
deliberately deferred. The options put to the owner were: unify the two paths
then generalise once; generalise each separately; or support the vertical axis
only for the sections path. None was chosen.

Overflow was unaffected because every placement in both paths funnels through
one `placeSection` — thirteen call sites.

## What landed on `main` this session

```
c8fe579 docs(plans): register the bar edge slice and close it
06d6e2e Merge branch 'milestone/m9-bar-geometry'
91c25ec chore(beads): record the bar geometry slice and what it found
8346d0b chore(beads): reconcile the tracker with the lane editor work
```

**C1 (`sysc-321`) is merged and closed.** D1 composes the layer anchor from
`h.policy.Edge` through pure `barAnchor`/`barSize` functions; `supportedEdges`
admits the lower edge; D6 puts `Body`/`Extent`/`ExclusiveZone` on `config.Bar`
so the platform and `Theme.Geometry` stop repeating one expression; the
exclusive zone gains a `reserve` that may be stated as zero.

Its live gate answered the design's first open question: **Niri honours an
exclusive zone of zero on a surface that still has a size** — it un-reserves
without unmapping, the focused tile grew by exactly the 44px extent, and the bar
went on painting over the strip. Two things stay **unclaimed**: live re-anchoring
from a configuration write alone (it needed a restart), and two-output
behaviour.

### Tracker state

`.beads/issues.jsonl` was found **truncated to 1 line against 306 at HEAD** and
repaired by splicing raw lines, preserving all 11 comment arrays. Opened or
changed this session:

| id | state | what |
|---|---|---|
| `sysc-321` | closed | C1, merged |
| `sysc-323` | in_progress | B implemented, unmerged |
| `sysc-338` | open | settings inputs read as a thin bar at mini density |
| `sysc-339` | open | lane editor polish: insertion indicator, 22-row add list, group drag |
| `sysc-340` | open | clock and date paint empty pills until the next minute boundary |
| `sysc-313` | open | overflow: clipping fixed, indicator chrome outstanding |

`sysc-319`, the M9 epic, is still open and leaves `sysc-322` in neither
`bd ready` nor `bd blocked`. Close it or fix the edges so C2's state is legible.

## Traps this session confirmed or added

- **Run the real commit hook, do not re-implement it.**
  `bash ~/.git-hooks/commit-msg msg.txt` is the arbiter. A hand-written grep
  chain let a "Both" through, because `grep && echo BANNED || echo clean` still
  exits 0 and the `&&`-joined commit ran anyway.
- **"bottom" contains "bot"** and is rejected. M9 is about bar edges, so this
  fires constantly. Write "the lower edge".
- **`niri msg -j layers` reports neither anchor nor exclusive zone.** Measure the
  zone differentially from `niri msg -j focused-window`'s `tile_size`, which
  cancels Niri's own gaps, and read the anchor from a `grim` screenshot.
- **`grim` over ssh needs `XDG_RUNTIME_DIR=/run/user/1000` and
  `WAYLAND_DISPLAY=wayland-1`**, or it fails with `failed to create display`.
- **A `main`-built binary rejects the laptop's config**, which carries instance
  ids from sub-project B. Swap to `~/.config/sysc-shell/config.json.pre-m9b`
  when deploying a non-B build, and restore both binary and config after.
- **`systemctl --user reset-failed`** is needed after a bad config
  restart-loops into `start-limit-hit`; a plain restart will not clear it.
- **A clock `format` must be a Go layout** built on the reference instant.
  `"EEEE dd MMMM yyyy HH:mm:ss"` is rejected; `"Monday 02 January 2006 15:04:05"`
  is fine.
- **There is no `tray` bar item.** `knownItems` has no such id, and adding one
  fails config validation. The tray is laid out automatically between the centre
  and right sections via `trayAvailableLocked` and `arrangeTray`.

## Mistakes worth not repeating

Recorded because each cost real time and two of them took the owner's bar down.

- **The laptop's shell was broken twice**, both times by editing a live config
  before reading what the code accepted: once with an invalid clock layout, once
  with a `tray` item id that does not exist. The validator caught both and named
  the exact field. **Read `knownItems` and the validator before editing a live
  configuration**, not after.
- **A regression was reported that did not exist.** The claim was that whole-item
  dropping made the window title vanish where it used to ellipsize. Instrumenting
  it showed the title's capsule was **already granted `W=0` on `main`** at that
  width — the test passed only because a zero-width capsule still lays out its
  child, so the inner node it asserts on had bounds inside an invisible box. The
  fixture was widened from 600 to 1400 and the reason left in the test. **Two
  guesses were made before measuring; the measurement overturned both.**
- **The tray was reported absent when the binary was merely unstarted.** The
  first conclusion, that nothing was publishing tray items, was drawn from a bus
  query without checking whether the daemon that owns the watcher was running.
