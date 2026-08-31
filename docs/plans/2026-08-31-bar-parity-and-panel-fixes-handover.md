# Handover: bar visual parity, panel fixes, and what they uncovered

Date: 2026-08-31. Session worked on `feature/bar-visual-parity`, which branches from
`fix/live-empty-panels`, which branches from `main` at `514ab9e`.

Nothing is merged to `main`. Both branches carry unmerged work.

## Where the code is

| Branch | Worktree | Contains |
|---|---|---|
| `fix/live-empty-panels` | `~/.config/superpowers/worktrees/sysc-shell/fix/live-empty-panels` | 6 commits: sysc-41 and part of sysc-42 |
| `feature/bar-visual-parity` | `~/.config/superpowers/worktrees/sysc-shell/feature/bar-visual-parity` | those 6 plus 10 more: the parity tranche |

Head is `a458bfc`. `go build ./...`, `go vet ./...`, `go test ./...` and
`go test -race ./internal/shell ./internal/ui ./internal/render` are all green at that commit.

Run `bd` from `/home/nomadx/sysc-shell`, never from a worktree. A commit from a worktree needs
`BEADS_DB=/home/nomadx/sysc-shell/.beads/beads.db`.

The commit-msg hook rejects a message containing `bot`, `agent`, `llm`, `cursor`, `codex` and
similar as substrings, so ordinary words like "both" fail. Check before committing:

```bash
grep -oiE "(claude|anthropic|chatgpt|openai|copilot|cursor|cody|tabnine|codex|gemini|bard|gpt-[0-9]|llm|ai assistant|bot|agent)" msg.txt && echo BANNED || echo clean
```

## The laptop

`ssh -p 7777 nomadx@192.168.0.64`, Niri on `eDP-1`, 1920x1080 physical, scale 1.25, logical 1536x864.
It currently runs a build of `3312cd7` at `~/.local/bin/sysc-shell`; the pre-session binary is kept at
`~/.local/bin/sysc-shell.pre-fix`.

`sysc-shell` needs both a Wayland display and a Niri socket:

```bash
export XDG_RUNTIME_DIR=/run/user/1000 WAYLAND_DISPLAY=wayland-1
export NIRI_SOCKET=$(ls -t /run/user/1000/niri.wayland-1.*.sock | head -1)
setsid nohup ~/.local/bin/sysc-shell >/tmp/sysc-shell.log 2>&1 </dev/null &
```

Capture with `grim -g "0,0 1536x56" out.png`. A fresh start shows no clock until the first minute
boundary, so wait ~64s before judging a capture. DMS is `systemctl --user start dms.service`;
Noctalia v5 is installed and started with `noctalia`. Only run one shell at a time.

## What landed

**sysc-41, panels painted empty chrome.** `PanelHost` had no text renderer and `configure` measured
with a stub; nothing ever called `render.Paint`. Panels now measure and paint for real, in the font of
the output they open on. `measureNode` and `columnChildHeight` were each missing kinds, which is what
produced `unsupported kind 7`; a table test now measures every kind in the iota through each path and
a `kindCount` sentinel makes it fail when a kind is appended. An auxiliary surface that fails to
configure now closes itself instead of failing the owner.

**Bar visual parity, sysc-43.** `KindCapsule`, its paint with a fill that supplies its own foreground,
the capsule palette mapped from tokens, every bar widget wrapped, workspaces as numbered pills, and
the bar-section arrangement fixed so capsule contents are laid out at all. Then the scale truncation
fix, the clock grouping and status roster, the palette lift, and group items.

## Corrections a successor should not have to rediscover

**sysc-42 is not fixed.** The teardown ordering fix (`c7bcc7c`) is correct and insufficient. The real
cause is **sysc-51**, in pinned `sysc-wayland v0.1.1`: `Pointer.Dispatch` opcode 0 handles
`wl_pointer.enter` for an unknown or zombie surface by calling `RegisterWithID` with a client-range
id, which panics, and the dispatch loop makes it fatal. Destroying an aux surface always races a
queued enter, so no shell-side change can close it. `Super+M` still kills the shell. The same broken
fallback is generated at ten sites. This blocks the M4 live gate and gets worse in M5, where toasts
and tray menus are short-lived surfaces under the pointer.

**The capsule contrast diagnosis was wrong at first.** Our bar-to-capsule ratio was 1.08:1 and DMS's
is 1.14:1, so the ratio was never the problem. The palette sat so near black that the step was
imperceptible. `efefda5` lifts `theme.Fallback` to the reference luminance and a contrast test now
pins minimums. Note the shell was running on the fallback palette, not a generated one.

**Two plan expectations are obsolete.** Task 5's widths of 8, 8 and 6 died with the numbered-pill
amendment: focus drives width, not occupancy. A workspace rename is now invisible to the bar, so a
test that renamed one to prove invalidation had to change its premise.

## Closed after the handover was first written

sysc-52, sysc-53, sysc-55 and sysc-56 were finished in the same session and are closed.

- **Battery glyphs.** Every level drew the same silhouette because the window subpath wound the same
  direction as the body, so under nonzero winding it filled instead of cutting a hole. Reversed in all
  fifteen sources. A rasterisation test now covers each band, asserts more charge never draws less ink,
  and checks the levels survive bar size. The old test only checked codepoint mapping, which is why an
  asset that discarded the distinction passed.
- **Metric icons.** cpu, memory, disk and network glyphs authored and prefixed onto each value. The
  width floor includes the icon so a field cannot widen as its value grows, and a failed source keeps
  its icon beside the placeholder.
- **Icon size.** The artwork does not fill its 24 pixel box, so mapping that box to 800 font units left
  the ink below cap height. It now maps to 980.
- **Group centring.** A capsule grows its layout box around the inner band's centre rather than
  downward, so members centre on the visible pill.
- **Tooltips.** Every status widget names itself and a group exposes its members, so a hover finds the
  member rather than the group. Tooltip labels also clipped, for the same reason the bar did: measured
  at the logical size, shaped at the physical one.

Note the icon face is a shared singleton and `font.Face` is not safe for concurrent use, so the icon
tests do not run in parallel. This is the third time that constraint has bitten in this session; a
shared text renderer was tried and reverted for the same reason.

## Open, in the order worth doing

1. **sysc-51**, P0. Upstream `sysc-wayland` fix plus a tag, then repin. Everything live is unstable
   until this lands.
2. **sysc-54**, group the sysmon widgets. The group item and the default grouping landed; what remains
   is judging it live now that icons exist. Superseded parts of the original issue are recorded there.
3. **sysc-52 note kept for history**, battery glyphs. All seven level codepoints `0xE008`..`0xE00E` are the same solid
   silhouette, verified by rasterising the band at 96pt and at 17px. `BatteryIconRune` correctly maps
   73% to level 5; the asset does not encode it. `iconfont_test` only checks codepoint mapping, so it
   passes. Add a test that rasterises each level and asserts they differ. Check the charging band and
   the critical glyph too, which were not verified.
3. **sysc-53**, no icons on cpu, memory, filesystem, block, network. Same font pass as sysc-52.
4. **sysc-31 to sysc-35**, the M3 audit defects. sysc-32, 33 and 34 are also M5 prerequisites.
5. **sysc-5**, the deferred M2 live gate: two outputs, hotplug, mixed scales, 60-minute idle.

## Still not parity, and why

The bar is much closer but is not there. Remaining, beyond the issues above: our capsules sit one
surface layer deeper than the reference because every widget is wrapped and the workspace pills live
inside a wrapper, and the widget roster is far thinner. DMS shows clipboard, bluetooth, wifi,
notifications and volume; none exist as widgets. That is new widget work, not chrome, and no issue
covers it yet.

## Reference captures

Twelve live captures with a catalogue in
`docs/plans/assets/2026-08-31-bar-visual-parity/refs/README.md`, committed on `main` at `5c1d85f`.
DMS and Noctalia, bar and panels, plus three deliberately triggered notification toasts. The catalogue
records which bar elements are plugin-supplied and therefore not parity targets, where the design
already decided against a reference, which panels reuse nodes we have, and which need vocabulary or
upstream metrics we do not.

That file also holds the M5 observations worth keeping: the Noctalia notification centre is a live
picture of the Tranche 5A spec, normal toasts carry an accent countdown on the top edge while critical
ones swap it for an error border and never expire, and a `value` progress hint rendered nothing, so
5A's independent value bar is ahead of the reference rather than behind it.

## Milestone 5

`sysc-20` is open and its plans are complete but not merged. They live on `redesign/milestone-5`,
which is stale relative to `main` and needs rebasing. `sysc-notify` and `sysc-tray` are documentation
only: no `go.mod`, no Go file, no tag. Both shell tranches begin with
`go get ...@v0.1.0-rc.1` and local `replace` is forbidden, so roughly 17 service tasks across two
repositories gate all M5 product code. That work is independent of everything above and is the long
pole.

Shell-side M5 prerequisites that do not exist yet: `AuxUpdate`, and a process-wide root coordinator.
`OpenPanel` currently tracks panels in a map and never closes an unrelated root.
