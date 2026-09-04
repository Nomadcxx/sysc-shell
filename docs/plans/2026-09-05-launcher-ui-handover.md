# Launcher UI Handover

Date: 2026-09-05.

For a **receiving agent**. The application launcher (`Mod+D`) is now the
daily-driver launcher on both machines, replacing fuzzel on the laptop and
DMS spotlight on the desktop. It works, and it is not good enough yet.

Three defects are already diagnosed below, with root causes confirmed in the
source. Fix those first. The wider UI audit is the second half.

## Paths

| Thing | Absolute path |
|---|---|
| This handover | `/home/nomadx/sysc-shell/docs/plans/2026-09-05-launcher-ui-handover.md` |
| Panel chrome | `/home/nomadx/sysc-shell/internal/shell/popout_launcher.go` |
| Panel tests | `/home/nomadx/sysc-shell/internal/shell/popout_launcher_test.go` |
| Panel size + key routing | `/home/nomadx/sysc-shell/internal/shell/panelhost.go` |
| Keyboard events | `/home/nomadx/sysc-shell/internal/platform/wayland/keyboard.go` |
| Ranking dependency | `$(go env GOMODCACHE)/github.com/!nomadcxx/sysc-launch@v0.1.0` |
| Live screenshot (desktop) | `docs/plans/assets/2026-09-04-launcher-desktop.png` |

The ranking library is `github.com/Nomadcxx/sysc-launch v0.1.0`, a separate
repo at `/home/nomadx/sysc-launch`. Two of the three defects live there, but
**all three can be fixed inside sysc-shell** — see each item.

## Read before touching anything

`internal/ui` is a retained proof tree, not a browser. There is no flexbox and
no `Grow`. A child that does not fit is a **layout error that fails the whole
surface and closes the panel** — this is not theoretical, it closed the
wallpaper picker twice during its gate. Sizes are computed, not negotiated.

Icons are two different things. `KindIcon` is the embedded Material subset —
19 hand-curated glyphs; naming anything else fails the surface at render
time. Application icons are `KindImage` rasters resolved through
`r.trayIcons` from the user's icon theme, which is a different path with a
different failure mode (a miss returns nil and falls back to a letter
capsule). Do not confuse them.

## The three defects

### 1. The list stops around "E" — a result cap, not a render bug

**This is the worst one and the least obvious.** The list is not truncated by
the virtual list or the scroll offset. `sysc-launch/score.go` has:

```go
const resultLimit = 50
...
if len(results) > resultLimit {
    results = results[:resultLimit]
}
```

With an empty query every entry scores 0 (`entryScore` returns `0, true`), so
the sort collapses to alphabetical by name, and the first 50 survive. On this
desktop there are **482** `.desktop` entries. Fifty of them alphabetically
ends in the E's. Everything after is not scrolled past — it was never in the
list.

The cap is correct for a *search*, where 50 matches is already more than
anyone reads. It is wrong for the *browse* case, which is what an empty query
is.

**Fix it in sysc-shell, not by releasing the dependency.**
`launcher.ServiceConfig.Rank` is injectable:

```go
Rank func(entries []Entry, query string, boost func(query, identifier string) int) []Result
```

Supply one from the shell. Reuse the library's scoring shape (score desc,
then lowercase name, then name, then ID — copy it exactly so search order does
not drift) and cap only when the query is non-empty. Verify against a real
machine: open the picker, press End, and confirm you land on a Z entry.

Do not "fix" this by raising the constant to a bigger number. A browse list
that silently drops entries at any threshold is the same bug with a longer
fuse.

### 2. Holding Down does not repeat

`internal/platform/wayland/keyboard.go` handles
`client.KeyboardKeyStateRepeated`, but niri never sends it. The compositor
sends `wl_keyboard.repeat_info(rate, delay)` once and then a single press;
**the client owns the repeat timer**. There is no `repeat_info` handler in the
tree and no timer, so one press is one row.

The mouse wheel works, which is why this reads as an arrow-key bug rather
than a missing protocol feature.

This is a Wayland-layer fix, not a launcher fix, and it will fix held keys in
every panel at once — including Backspace in the search fields, which has the
same problem today and nobody has reported yet.

Watch out for:
- Repeat must stop on key release, on `wl_keyboard.leave`, and when the
  surface is destroyed. A timer outliving its surface delivers keys into a
  freed host.
- The timer fires off the Wayland owner goroutine. Events must be delivered
  the way `deliverKey` already does, on the owner, or you race the font map
  the way `sysc-162` describes.
- `rate` is keys per second and `delay` is milliseconds; `rate == 0` means
  repeat is disabled and must be honoured.

### 3. Frequently used should be at the top

Partly working already — do not rewrite it before you measure it. The boost
is wired: `Registry.launcherServiceLocked` passes `History`, `rank` adds
`min(boost(...), usageBoostCap)` with `usageBoostCap = 25`, and
`History.usageLocked` aggregates across every stored query when the query is
empty, which is the browse case. In the captured screenshot, Brave sorts above
"Add/Remove Software", so the boost is demonstrably applied.

What makes it feel broken is defect 1: above the fold you get a couple of
boosted entries and then 48 alphabetical strangers. Fix the cap first, then
re-judge this with fresh eyes.

If it still reads wrong after that, the thing to examine is the decay:

```go
base := 10 - int(h.now().Sub(lastUsed).Hours()/24)
return max(base*amount, 1) / max(delta, 1)
```

Anything older than ten days collapses to a boost of 1 regardless of how
often it was used, so a daily driver used 200 times ranks level with something
opened once last month. That is a frecency curve worth revisiting — but it is
a **behaviour change in a shared library**, so agree it before shipping it.

The history file is `~/.local/state/sysc-shell/launcher/history.gob`
(deliberately separate from sysc-launch's own path so the two consumers do not
merge usage data).

## The wider audit

After the three defects, audit the chrome itself. It has never had a design
pass — it was built to work, and the prior art was not studied.

Current shape, from `launcherTree`: a 560x500 panel, 12px padding, a 44px
search field, then a virtual list of 60px rows with an 8px gap. Each row is a
capsule holding a 40px icon slot, a bold name, and the `.desktop` Comment.
About six and a half rows are visible at a time.

Worth forming an opinion on, with evidence rather than taste:

- **Six visible rows on a 500px panel.** The row is 60px tall to fit a
  two-line name+comment. Whether the comment earns that much vertical space
  is the central question of this layout — most entries' comments are
  generic ("Access the Internet"), and halving the row doubles what you can
  see without scrolling.
- **No keyboard hint anywhere.** Nothing says Enter launches or that Up/Down
  move. The wallpaper picker has the same gap.
- **The letter-capsule fallback.** Apps without a themed icon get a coloured
  capsule with their first letter. Check how common that actually is on these
  two machines before deciding whether it deserves better.
- **`launcherGlyph` takes the first rune of the name**, so entries starting
  with a bracket, digit, or non-Latin script produce a poor initial.
- **No categories, no recent/frequent section header, no empty-state
  guidance** beyond the words "No results".
- **The panel is fixed at 560x500** while the wallpaper picker learned the
  hard way to fit the output — see `panelTargetSize` and the `Trigger.OutH`
  fix in `f4dc4da`. 500 fits everywhere, so this is latent, not broken.

Prior art to study rather than guess at: DMS spotlight (`/usr/bin/dms`, the
machine's previous launcher), fuzzel (still bound to `Mod+Space` on the
laptop), and `/home/nomadx/noctalia-gslapper` for the house chrome idiom. The
wallpaper picker's parity method is worth copying: read the prior art's own
string table and map every capability, rather than eyeballing screenshots.

## How to test

The shell runs from a systemd user unit on both machines. Redeploy is:

```bash
go build -o ~/.local/bin/sysc-shell.staged ./cmd/sysc-shell
mv ~/.local/bin/sysc-shell.staged ~/.local/bin/sysc-shell   # rename, not cp
systemctl --user restart sysc-shell.service
```

`mv` matters: a cross-filesystem copy over the running binary gets `ETXTBSY`.
Logs are `journalctl --user -u sysc-shell`.

Open it without a keypress:

```bash
sysc-shell ipc panel.open '{"panel":"launcher"}'
grim /tmp/shot.png
sysc-shell ipc panel.close '{"panel":"launcher"}'
```

The laptop is `ssh -p 7777 nomadx@192.168.0.64`, single output, 1536x864
logical at scale 1.25 — the short scaled case that found four defects the
desktop structurally could not. Test both.

**Do not use `wtype` to drive the panel.** Attaching a virtual keyboard drops
the panel's exclusive grab and dismisses it, and the keystrokes then land in
whatever terminal is underneath. Defect 2 needs a human at the machine, or a
unit test around the repeat timer plus one human confirmation.

## Build constraints on this machine

These are not preferences. Ignoring them logs the user out or locks the box.

- **Never** combine `./...` with `-race`; a hook blocks it, and repo-wide
  builds hard-lock this machine (zram-only swap, 16-way linking). Cap it:
  `GOMAXPROCS=2 go test -count=1 -p 1 ./internal/<pkg>`, ideally wrapped in
  `systemd-run --user --scope -p MemoryMax=6G -p MemorySwapMax=0`.
- `go test ./internal/shell` runs `loginctl terminate-session self` for real.
  Shadow `loginctl` on `PATH` before running it interactively.
- Never `pgrep -f` / `pkill -f` a pattern that appears in your own command
  line. It matches your own shell and kills your session; this happened.
- Commit messages are rejected on a naive case-insensitive substring match
  including `bot` and `agent` — so "both" and "bottom" are rejected. Precheck
  every message before committing.

## Tracker

`sysc-166` is the open brightness bug found alongside this work; unrelated,
do not fold it in. File the launcher defects as their own records with
`bd create -t bug`, and note that `bd` must run from
`/home/nomadx/sysc-shell` with `BEADS_DB` set if you work in a worktree.
