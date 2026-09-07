# Standalone audio panel — completion handover

Date: 2026-09-07. Snapshot of the gate output and live observations for the
`feature/audio-panel` branch. Leave as written; corrections go to bd.

## Shipped

Commits `ca2b996`..`18470ab` on `feature/audio-panel` (plan:
`2026-09-07-audio-panel.md`, design: `2026-09-07-audio-panel-design.md`):

- `ca2b996` — `render.AttachedMask` concave-corner primitive (plan Task 1,
  sysc-216).
- `5fce137` — Material subset gains `mic`, `mic_off`, `graphic_eq`,
  `headphones`; both lists agree, asserted by test (Task 2, sysc-209).
- `604502a` — `services.Audio` `pw-dump` enumeration, cubic volume,
  defaults, `SetDefault`/`SetNodeVolume`/`SetNodeMute` (Task 3, sysc-210).
- `18470ab` — `PanelAudio` chrome, Volumes and Devices tabs, the off-owner
  `scheduleControl` seam, the `volume` bar widget, bar seams, IPC and config
  wiring (Tasks 4–7, sysc-227/212/213/214).

## Gate output

`PATH=~/.cache/sysc-stubs:$PATH GOMAXPROCS=4 go test -p 4 ./internal/shell
./internal/ui ./internal/ipc ./internal/services ./internal/render
./internal/config` — all `ok` (shell 9.6s, services 6.1s, render 0.18s).
`gofmt` clean on touched packages; `go vet` clean; `go.mod`/`go.sum`
untouched. Repo-wide `-race` remains unrunnable on this box (zram-only
swap), per the plan's global constraint.

One unreproducible observation: a single concurrent test run panicked inside
`fontscan` (`scoredFootprints.Less`, sort.go) reached from
`pluginhost.refreshPanel` — code this branch does not touch. Three clean
re-runs; not pursued.

## Audit

A post-implementation audit against the plan and D1–D13 found **no blocking
defects**. Recorded deviations, all low severity:

- D6 "SecondaryContainer well" renders as `ui.FillSoft` — no
  `SecondaryContainer` token exists in the codebase; closest available fill.
- D12 failure semantics: a failed `wpctl` write sets `errLabel` but leaves
  the pending slider value until the next 1 Hz sample reconciles it, rather
  than restoring the last known value immediately.
- Task 7 Step 2: `barView.Audio` is populated for every bar, not gated on
  the bar carrying a `volume` widget — a per-rebuild `wpctl` exec whose
  value is ignored on bars without the widget.
- The mask test samples column 2, not the plan's column 0: column 0 is
  antialiased and the plan's literal assertion cannot hold there.

## Live Niri observations (DP-1, 3440×1440)

Redeployed to `~/.local/bin/sysc-shell` at 14:34, service active, bar
painted. `volume` is present in the deployed `~/.config/sysc-shell/config.json`.

- Panel opens over IPC (`panel.open audio`): `niri msg -j layers` shows
  `sysc-shell-panel`; `panel.close` unmaps it and the shield layer with it.
- Cubic volume reconciles live: default sink node 45 reads
  `channelVolumes 0.135549` in `pw-dump`, cube root 0.51, and `wpctl
  get-volume @DEFAULT_AUDIO_SINK@` reads 0.51 — the panel paints 51 %.
- Pointer-driven items (scroll step, right-click mute, slider drag, device
  switch, mock fidelity against `docs/plans/assets/2026-09-07-audio-panel/`)
  are owner-verified: no compositor input injection from the agent shell.
- Failure path, Escape/Tab focus, compact density and fontScale 125 % are
  verified by unit coverage (`TestAudioDensityContract`,
  `TestEmptyApplicationsSectionIsNotAbsent`, focus tests), per the plan's
  allowance for item 7.

## Tracker

sysc-209/210/212/213/214 closed with evidence; duplicates sysc-208/211/215
closed pointing at sysc-216/227; sysc-227 closed; epic sysc-207 closed.
`sysc-154` keeps its dependency on the shipped mask primitive.
