# Milestone 6E Screen Recorder handover

Date: 2026-09-02
Kind: completion-handover
Branch: `milestone/plugin-host`
Tip: `6a1f6bc fix(plugin): pass -o with -ro for GSR replay buffers`

This is a snapshot of automated output and live observations. It does not track remaining work.

## Commits

| Commit | Task |
|---|---|
| `7454ac7` | 0 — calibrate Screen Recorder to gpu-screen-recorder 6.0.1 |
| `ad22d0c` | 1 — define recorder configuration |
| `0878f91` | 2 — supervise recorder process |
| `f0891af` | 3 — control recording and replay |
| `4c5f97e` | 4 — expose recorder host context |
| `c2df869` | 5 — add Screen Recorder reference plugin |
| `98b049b` | 6 — prove recorder lifecycle |
| `f75197e` | live — accept a live recorder that never prints ready |
| `6a1f6bc` | live — pass `-o` with `-ro` for GSR replay buffers |

## Automated gate

Run from `6a1f6bc` on 2026-09-02:

```
gofmt -l .                        (no output)
go vet ./...                      (clean)
go test -race -count=1 ./...      all packages ok
```

`go test ./tests/integration -run PluginRecorderGate` builds the plugin against a fake `gpu-screen-recorder` on PATH. It covers focused-output argv, exact flags, two-output projection, save notification, crash, hung stop, zero-byte artifact, stderr flood, rejected settings, exact-PID adoption, ambiguous matches, a missing dependency that does not start, and host disable.

## Live results

Environment: Niri on the trusted laptop (`ssh -p 7777 nomadx@192.168.0.64`). One output `eDP-1`, physical 1920×1080 @ 60.010 Hz, scale 1.25, logical 1536×864, transform Normal. `gpu-screen-recorder` 6.0.1. A daily `sysc-shell` was already mapped (`pid 162149`, `/home/nomadx/.local/bin/sysc-shell`). This gate did not replace that process.

The live cycle drove `cmd/sysc-plugin-screen-recorder` over v1 against the real recorder, with `WAYLAND_DISPLAY=wayland-1` and `NIRI_SOCKET=/run/user/1000/niri.wayland-1.1275.sock`. Capture used `-w eDP-1` (focused connector, not portal).

### Ran

| Check | Observation |
|---|---|
| Record ten seconds, stop | `/tmp/sysc-m6e-recordings/recording_20260902_091623.mp4`, 870914 bytes, ffprobe duration 10.000s, notify summary `Recording saved` |
| Replay buffer ten seconds, SIGUSR1 save | `/tmp/sysc-m6e-recordings/replay_20260902_091645.mp4`, 885401 bytes, ffprobe duration 10.060s |
| Restart plugin during an active recording | GSR pid 164090 survived `SIGKILL` of the plugin; the restarted plugin adopted that exact exe+args and showed Recording; stop reaped pid 164090 |
| Plugin shutdown while recording | GSR pid 164115 exited after `host.shutdown`; `pidof gpu-screen-recorder` was empty |
| Leftover owned process | none after the gate. Test files under `/tmp/sysc-m6e-recordings` were removed after the sizes and durations above were recorded |

### Not run

- Enabling Screen Recorder inside the already-mapped daily shell (bar click, settings manager, SIGHUP).
- Notification delivery through `sysc-notifyd`. The plugin issued `notify` host calls; no notify daemon exists (Milestone 5 services are still plan-only).
- Two-output recording. The laptop has one connector.

### Calibration found on the live binary

`gpu-screen-recorder` 6.0.1 never prints `ready`. It logs `gsr info:` on stderr and records. A process that stays alive for 100ms is treated as ready (`f75197e`). Replay with `-r` refuses to start unless `-o` is also a directory; `-o` and `-ro` both receive the replay directory (`6a1f6bc`).

Adopted recordings do not restore the original `-o` path. Stop after adoption signals the owned PID (proven) but then verifies an empty dest and can mark Failed. That is why a later 88-byte `recording_20260902_091648.mp4` appeared during the adopt cycle. Tracked as `sysc-130`.

## Verdict

The plugin owns real `gpu-screen-recorder` 6.0.1 on live Niri: record, replay save, exact-PID adopt, and shutdown-without-pkill all held, and no owned recorder remained. The compositor bar path and notify daemon were not part of this run.
