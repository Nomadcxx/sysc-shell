# Milestone 6E Screen Recorder Reference Plugin Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Ship a Screen Recorder plugin that controls `gpu-screen-recorder`, reports status in the bar, handles replay buffers, and recovers exact owned processes.

**Architecture:** The plugin process owns recorder commands, settings, status, logs, and recovery. The shell supplies focused-output context and bounded notification calls. Tests replace the recorder with the Go test binary; production code never uses a command shell or broad process-name matching.

**Tech Stack:** Go standard library `os/exec`, Linux process inspection needed for exact adoption, M6D protocol host, M5 notification client, Niri output context.

**Design:** `docs/plans/2026-09-01-milestone-6-plugin-host-design.md`

---

### Task 0: Calibrate the installed recorder interface

**Files:**
- Inspect: `plugins/reference/recorder/` after Task 1 creates it
- Record changes in: this plan, if required

**Step 1:** On the trusted laptop run `ssh -p 7777 nomadx@192.168.0.64 '/usr/bin/gpu-screen-recorder --help'`. Capture version and supported arguments without starting a recording.

**Step 2:** Compare the installed flags with the Noctalia reference settings in the M6 design inputs. Remove settings the backend cannot honor; do not add compatibility shims for another recorder.

**Step 3:** Commit a docs-only correction before product code if the calibrated contract changes this plan.

**Calibrated backend (2026-09-02, trusted laptop `gpu-screen-recorder` 6.0.1):**

The installed `--help` accepts `-w focused|portal|<monitor>`, `-f`, `-k h264|hevc|av1|vp8|vp9`, `-q`, `-bm auto|qp|vbr|cbr`, `-ac aac|opus|flac`, `-ab`, `-a`, `-s WxH`, `-cursor yes|no`, `-cr limited|full`, `-r`, `-replay-storage ram|disk`, `-o`, `-ro`, `-c`, `-restore-portal-session`, `-ffmpeg-opts`, `-v`. It does not list `-ffmpeg-video-opts`.

Command construction therefore uses `-bm qp` with `-ffmpeg-opts qp=N` (codec-scaled like the Noctalia reference) rather than `-ffmpeg-video-opts`. Replay writes to `-ro <dir>`. Focused capture passes the connector name as `-w`, not the keyword `focused`. Save replay is SIGUSR1 to the owned PID; stop is SIGINT then SIGKILL after timeout, never `pkill`.

Settings the backend cannot honor, or that this shell has no v1 type for, stay out: Flatpak launch, glyph overrides, clipboard copy, control-center shortcuts, HDR codec aliases, and region capture.

---

### Task 1: Declare and validate recorder settings and dependency

**Files:**
- Create: `plugins/reference/recorder/manifest.json`
- Create: `plugins/reference/recorder/config.go`
- Test: `plugins/reference/recorder/config_test.go`

**Step 1:** Write tests for source, output directory, filename pattern, frame rate, video/audio codecs, quality, resolution, audio source/bitrate, cursor, color range, replay enable/duration/storage, inactive visibility, and invalid combinations.

**Step 2:** Add manifest dependency tests proving missing `gpu-screen-recorder` leaves the plugin visible, stopped, and actionable in Settings.

**Step 3:** Implement typed validation and command argument construction as a pure function returning `[]string`. Never return shell text.

**Step 4:** Run `go test -race -count=1 ./plugins/reference/recorder ./internal/plugin -run 'Recorder|Config|Dependency' -v`.

**Step 5:** Commit `feat(plugin): define recorder configuration`.

### Task 2: Own recorder process lifecycle by exact identity

**Files:**
- Create: `plugins/reference/recorder/process.go`
- Test: `plugins/reference/recorder/process_test.go`

**Step 1:** Use the current Go test binary as a fake recorder. Test start, startup failure, status, SIGINT stop, kill after timeout, exit result, stdout/stderr log tail, and no orphan after orderly plugin shutdown.

**Step 2:** Add Linux adoption tests around an injectable process scanner. Match executable identity and the plugin's exact output/replay arguments. Reject same-name processes with different arguments and more than one match.

**Step 3:** Implement with `exec.CommandContext`, explicit file descriptors, exact PID signalling, and bounded logs. Do not call `pkill`, `pgrep`, `sh -c`, or a global process-group kill.

**Step 4:** Run `go test -race -count=1 ./plugins/reference/recorder -run 'Process|Adopt|Shutdown|Log' -v`.

**Step 5:** Commit `feat(plugin): supervise recorder process`.

### Task 3: Implement recording and replay state machines

**Files:**
- Create: `plugins/reference/recorder/recorder.go`
- Test: `plugins/reference/recorder/recorder_test.go`

**Step 1:** Write table tests for unavailable, idle, recording, replay-active, stopping, failed, and adopted states. Cover record toggle, replay start/stop/save, record/replay exclusion, repeated input, process exit, zero-byte artifact, and retry.

**Step 2:** Add path tests for collision-free filenames, directory creation, configured patterns, replay claim, and preserving an existing file.

**Step 3:** Implement one serialized command loop. Keep filesystem verification and process waiting off the protocol reader. Track owned PID and exact command in persistent state only where recovery needs it.

**Step 4:** Run `go test -race -count=1 ./plugins/reference/recorder -run 'Recorder|Replay|Artifact|Filename' -v`.

**Step 5:** Commit `feat(plugin): control recording and replay`.

### Task 4: Supply focused-output context and qualify notification calls

**Files:**
- Modify: `plugin/v1/message.go`
- Modify: `internal/shell/pluginhost.go`
- Modify: `internal/plugin/runtime.go`
- Test: `internal/shell/pluginhost_test.go`
- Test: `internal/plugin/runtime_test.go`

**Step 1:** Add tests that a bar event includes its output, a global command uses Niri's focused output snapshot, stale output generations fail by name, and the plugin cannot request an undeclared connector.

**Step 2:** Add host-call tests for granted/denied notification capability, bounded title/body/action fields, timeout, M5 service failure, and a reply that cannot block the shell.

**Step 3:** Extend existing context and host-call messages. Reuse the M5 notification client and current Niri projection; do not add a recorder-specific shell service.

**Step 4:** Run `go test -race -count=1 ./internal/plugin ./internal/shell -run 'OutputContext|Notification|HostCall' -v`.

**Step 5:** Commit `feat(plugin): expose recorder host context`.

### Task 5: Build the recorder bar view and plugin command

**Files:**
- Create: `plugins/reference/recorder/view.go`
- Test: `plugins/reference/recorder/view_test.go`
- Create: `cmd/sysc-plugin-screen-recorder/main.go`

**Step 1:** Write tests for inactive, recording, replay, stopping, unavailable, and failed icon/tone/tooltip states; `hide_inactive`; primary record toggle; secondary replay toggle/save; middle save; and accessible status text.

**Step 2:** Write end-to-end client tests that settings changes rebuild the next command, process status patches all output views, saved artifacts notify, failures include the bounded recorder log, and disable sends orderly shutdown.

**Step 3:** Implement the thin view over the recorder state machine. Keep control-center entries absent.

**Step 4:** Run `go test -race -count=1 ./plugins/reference/recorder ./cmd/sysc-plugin-screen-recorder` and `go build ./cmd/sysc-plugin-screen-recorder`.

**Step 5:** Commit `feat(plugin): add Screen Recorder reference plugin`.

### Task 6: Prove recorder behavior with a fake backend

**Files:**
- Create: `tests/integration/plugin_recorder_gate_test.go`
- Modify: `tests/integration/README.md`

**Step 1:** Run the plugin against a fake executable that records argv, emits logs, creates an artifact, waits for signals, and supports replay-save. Prove output selection, exact args, state projection, notification, adoption, disable, and crash recovery.

**Step 2:** Add missing dependency, rejected config, hung stop, failed artifact, ambiguous adoption, and stderr flood cases.

**Step 3:** Run `go test -race -count=1 ./...` and `go vet ./...`.

**Step 4:** Commit `test(plugin): prove recorder lifecycle`.

### Task 7: Run the live recorder gate

**Files:**
- Create: `docs/plans/2026-09-01-milestone-6e-recorder-handover.md`
- Modify: `docs/plans/README.md`

**Step 1:** Build the shell and five reference commands from the M6 branch. Copy only those artifacts and manifests to a temporary directory on the trusted laptop.

**Step 2:** Under live Niri, enable Screen Recorder, record ten seconds on the focused output, stop, verify a non-empty playable file, start replay for at least ten seconds, save it, and verify the notification paths.

**Step 3:** Restart the plugin during an active test recording and verify exact adoption. Disable it and verify the owned recorder stops without signalling another process.

**Step 4:** Remove test artifacts after recording their names, sizes, and durations in the recorder handoff. Register the handoff in the same commit. Do not claim M6E complete if any owned process remains.

**Step 5:** Close `sysc-73` with the automated and live evidence, flush beads, and commit `test(plugin): record live recorder evidence` with the handoff and `.beads/issues.jsonl`.
