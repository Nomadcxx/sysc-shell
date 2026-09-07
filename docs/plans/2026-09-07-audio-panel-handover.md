# Standalone audio panel — handover to the implementation plan

Date: 2026-09-07. Written for whoever picks up planning after the design.

Design: `docs/plans/2026-09-07-audio-panel-design.md` (committed, `d8bc02c`).
Mock: `docs/plans/assets/2026-09-07-audio-panel/` — `volumes.png`,
`devices.png`, `wingtip.png`, and the HTML/CSS they render from.

## Your job

Turn the design's seven slices into a `docs/plans/2026-09-07-audio-panel.md`
implementation plan and register the work in bd. Invoke the writing-plans
skill; do not start coding.

## State when this was handed over

- Design committed. **The mock has not been approved by the owner yet.**
  Three questions were put to them and are unanswered:
  1. 560 px panel width, which front-elides device names on the Volumes rows
     (Devices shows them in full). Wider, or keep?
  2. Devices rows as selected wells with a trailing `check`, versus the
     prior art's radio buttons.
  3. `render.AttachedMask` landing in this slice with `sysc-154` depending on
     it, versus the reverse.
  Check the answers before you write the plan. Question 3 changes the slice
  order; questions 1 and 2 change the pixel contract and D6.
- Nothing is registered in bd. No issue exists for this work.
- No code has been written. `internal/services/audio.go` and
  `internal/shell/` are untouched.

## What the design already settled, so you do not relitigate it

- The deep audio UI lives in its own panel, not in the control centre. The
  owner chose this over routing to `sysc-154`, which means the work does not
  block on the spine.
- Reads come from `pw-dump`, writes from `wpctl`. `pw-mon` was rejected as an
  orphan-process risk; `wpctl status` was rejected for having no format
  contract and no icon name.
- The control centre's Audio page is not being changed. It stays the thin
  slider `sysc-158` designed; its rail entry opens `PanelAudio`.

## Traps this repository will spring on you

- **Repo-wide Go builds hard-lock this box.** zram-only swap plus 16-way
  linking. Cap `-p 4` and `GOMAXPROCS=4`. Never `./...` with `-race`.
- **`go test ./internal/shell` logs you out.** It really runs
  `loginctl terminate-session self`. Shadow `loginctl` and `systemctl` with
  stubs earlier on `PATH` before running it.
- **The commit-msg hook rejects AI attribution by naive substring.** It sits
  at `~/.git-hooks/commit-msg` via `core.hooksPath`, so it applies
  everywhere. Ordinary words trip it — "b-o-t-h" and the name of the design
  skill used here are both confirmed hits. No `Co-Authored-By` trailer.
- **The beads pre-commit hook auto-stages `.beads/issues.jsonl`.** It did so
  on `d8bc02c`, sweeping in unrelated pre-existing changes. Expect it.
- **Commits from a worktree need `BEADS_DB` set** or the hook blocks them.
- **The Material glyph subset is hand-curated.** A name absent from both
  `internal/render/icons/material/build.py` and `materialfont.go` shapes to
  nothing and paints an invisible control. The design adds four names; they
  must go into both lists, and a test asserts the lists agree.

## Facts worth carrying into the plan, already verified

- `wpctl`, `pw-dump` and `pw-mon` are all present at `/usr/bin` on this box.
- `pw-dump` is ~312 KB of JSON here, with 11 audio nodes.
- PipeWire stores linear volume; `wpctl` displays the cube root. Node 45 reads
  `channelVolumes 0.1355` and `wpctl` shows `0.51`. Reading the raw value
  would paint 14 % where the rest of the shell paints 51 %.
- `pw-dump` carries `application.icon-name` / `device.icon-name` per node;
  `wpctl status` does not.
- `ui.KindSlider` (track 4, knob 14), `KindSegmented`, `KindScroll`,
  `KindImage` and `KindIcon` all ship. No new node kinds are needed.
- `internal/render/mask.go` is convex-only. `AttachedMask` does not exist.
- `internal/shell/widget.go:184` already binds `network` to the throughput
  metric, so the new widget's config type name is `volume`.
- The panel and the bar must paint the same token, `SurfaceContainerLow`, or
  the fused joint shows a seam.

## Known gap in the design

Density and font-scale behaviour is **unverified**. The pixel contract is
standard density at fontScale 1 only. At `DensityCompact`, or fontScale 125 %,
the 44 px value column and the 68 px row height need rechecking, and the
560 px width may no longer hold the Devices names. Close this in the plan —
either as an explicit slice or as an acceptance check on slices 5 and 6.

## Suggested bd shape

```
<new> render.AttachedMask concave-corner primitive   (sysc-154 gains a dep)
<new> Material glyph subset: mic, mic_off, graphic_eq, headphones
<new> services.Audio: pw-dump enumeration and SetDefault
  └── <new> PanelAudio chrome: identity, wing-tips, header, tabs, IPC
        ├── <new> Volumes tab
        └── <new> Devices tab
<new> volume bar widget                              (deps: service, panel)
```

The first three have no blockers and can run in parallel.
