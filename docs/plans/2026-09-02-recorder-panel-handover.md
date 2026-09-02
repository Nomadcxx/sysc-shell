# Recorder panel live handover

Date: 2026-09-02
Kind: completion-handover
Branch: `milestone/plugin-host`
Tip: `1e47624 feat(plugin): open the recorder panel and drive it from the command`

This is a snapshot of automated output and live observations. It does not track remaining work.

## Commits

| Commit | Task |
|---|---|
| `d2baa0a` / `da99136` | 1 — recorder glyphs in the catalogue |
| `c650f79` | 2 — catalogue icons on buttons |
| `256755f` | 3 — declare recorder panel and `include_settings` |
| `3b2f1b9` | 4 — camera / Record / Stop bar pill |
| `417aefd` | 5 — panel header tree and elapsed |
| `83820eb` / `5dc23fc` | 6 — toggle panel and compose settings |
| `42e0a1a` / `cf50644` | 7 — real setting controls and apply |
| `1e47624` | 8 — wire the recorder command |

## Automated gate

Run from `1e47624` on 2026-09-02 (archPC worktree):

```
gofmt -l .                        (no output)
go vet ./...                      (clean)
go test -race -count=1 ./...      all packages ok
go test ./tests/integration -run PluginRecorderGate
                                  ok  tests/integration  8.807s
```

`PluginRecorderGate` still covers focused-output argv, exact flags, two-output projection, save notification, crash, hung stop, zero-byte artifact, stderr flood, rejected settings, exact-PID adoption, ambiguous matches, missing dependency, and host disable against a fake `gpu-screen-recorder`.

## Live results

Environment: Niri on the trusted laptop (`ssh -p 7777 nomadx@192.168.0.64`, host `archThink`). One output `eDP-1`, physical 1920×1080 @ 60.010 Hz, scale 1.25, logical 1536×864, transform Normal. `gpu-screen-recorder` 6.0.1.

Gate binaries were built from the worktree on archPC and copied to `/home/nomadx/scratchpad/recorder-panel-gate/` on the laptop. The mapped process was that scratchpad `sysc-shell`, not `/home/nomadx/.local/bin/sysc-shell`. Isolated `XDG_CONFIG_HOME` / `XDG_STATE_HOME` under the scratchpad held `config.json` and `org.sysc.screen-recorder` (manifest + worktree plugin binary). The daily shell (`pid` previously 263075 / 162149) was stopped by exact pid for the gate and restarted afterward.

`/dev/uinput` is root-only on the laptop (`ydotoold` Permission denied; no passwordless sudo). Bar pointer clicks were not injectable. Record / Stop / camera / replay / save were driven by writing one NDJSON `input.event` line into the live plugin's stdin (`/proc/<plugin-pid>/fd/0`), which is the same activate path the bar hit-test uses. Settings changes used `settings.changed` inject plus editing the gate `config.json` and `SIGHUP` where noted.

### Ran

| Check | Observation |
|---|---|
| Build + map worktree shell | `niri msg -j layers` showed `sysc-shell:bar` and `sysc-shell-toast` on `eDP-1`. Plugin pid started under the scratchpad plugin dir. |
| Pill shows camera | `grim` bar crop: camera control plus Record and Stop (hide_inactive false). |
| Record then Stop | Inject `node=record` → GSR `-w eDP-1 -k h264 … -o …/recording_20260902_114507.mp4`; inject `stop` reaped GSR. File 539817 bytes, ffprobe duration 6.040s. |
| Codec / directory store | Gate `config.json` + `settings.changed` set `video_codec=hevc`; next Record argv contained `-k hevc` and wrote `recording_20260902_114554.mp4` (544337 bytes, 4.920s). |
| Replay start / save | `replay_enabled` via settings; inject `replay` → GSR with `-r 10 -replay-storage ram -o/-ro` directories; inject `save` → `replay_20260902_114609.mp4` (705960 bytes, 7.040s). |
| `hide_inactive` idle | Fresh shell with `hide_inactive: true`: bar crop showed camera only (no Record/Stop text). |
| `hide_inactive` recording | Inject Record: bar crop showed camera + Record + Stop; Stop cleared GSR. |
| Camera → panel.open | Inject `node=camera` called `panel.open`. Shell log: `closing surface panel:plugin: ui: child 7: ui: child 1 of kind 11 does not fit in 320x20` (twice). `niri msg -j layers` never showed a panel surface. Tracked as `sysc-139`. |
| Shell exit while recording | SIGTERM of the gate shell reaped the plugin but left GSR pid 280774 until an explicit kill of that pid. Tracked as `sysc-140`. |

### Not run / incomplete

- True left-click on the bar (no uinput). Activate path was stdin inject, not seat pointer.
- Panel UI edits (directory/codec toggles, Settings → Plugins visual parity while the panel is open). Panel never stayed mapped (`sysc-139`).
- Escape / shield close of the panel (panel never stayed open).
- Disable via Settings → Plugins / `SetPluginEnabled`. Writing `plugins.enabled=[]` and `SIGHUP` did not stop the plugin or GSR. Host disable remains covered by `PluginRecorderGate` only.
- Notification delivery through `sysc-notifyd`. Absent (Milestone 5 services still plan-only), as in the M6E handover.
- Two-output recording. Laptop has one connector.

### Calibration / notes

- Plan bd id `sysc-136` is not present in this repository's beads graph (`bd show sysc-136` → not found). The live feature issue is `sysc-138`. No replacement id was created; `sysc-136` was not closed.
- `sysc-130` (adopt dest path) left open.
- `sysc-73` left open.
- Live-discovered: `sysc-139` (panel TextField vs 320-wide default), `sysc-140` (orphan GSR after shell SIGTERM).

## Verdict

The worktree recorder pill maps on live Niri, and the same activate path records, stops, switches codec, and saves replay against GSR 6.0.1. `hide_inactive` matches D7 on the bar. The attached panel does not stay open: configure still uses the 320×280 plugin fallback, so `include_settings` TextFields fail layout (`sysc-139`). Seat clicks, panel UI, shield/Escape, and Settings disable were not proven live.
