# Media service execution handover

Date: 2026-09-15.

Commissions **`sysc-156`** — the MPRIS media service, bar widget and control-centre page — as the next
slice of the parity tranche. The contract is
[2026-09-11-media-service-design.md](2026-09-11-media-service-design.md) (D1–D11) and the executable plan
is [2026-09-11-media-service.md](2026-09-11-media-service.md) (ten TDD tasks). Read both. This document
records only what changed underneath them since they were written, and what that changes for you.

## Why this slice, and why now

The tranche order in `2026-09-13-parity-tranche-continuation-handover.md` is smoothness, parity, media,
stacking, template. Slots 1 and 2 are done:

- **Rendering smoothness** — Tasks 1–4 merged. Task 5 dropped at its heading. Task 6 (a documentation
  amendment) is owed and is *not* a dependency of anything here.
- **Noctalia v4 parity** — complete. Task 10 and Task 11 Step 2 landed 2026-09-15.

Surface stacking must follow media, not precede it: its only consumer is the media card, and its Task 5
is a consumer gate with a revert branch.

## Receiving state

Verify before you start; do not assume.

- `main` is at `8565b7b`, and **`origin/main` is behind it at `41eaf68`**. A parity branch
  (`feature/parity-task10-11`, three commits) is pushed and awaiting a merge into `main`. Confirm the
  merge landed before branching, or you will base on a tree missing the conformance widening.
- No media code exists anywhere. `internal/services/media*` and `internal/shell/media*` do not exist and
  nothing imports MPRIS. This is a clean start, not a resumption.

## Three plan and design claims that are now stale

Each was verified against the tree on 2026-09-15. Each would have cost you time.

### 1. The `godbus` pin is already done — D11.3 is discharged

D11.3 says "`godbus/dbus/v5` is not yet in `go.mod`. Whichever of media or network lands first carries
the pin." Network landed first. `go.mod:18` already requires `github.com/godbus/dbus/v5 v5.2.2`, the
module is in the download cache, and it is already imported by `internal/services/networksecrets.go` and
`internal/services/bluetooth.go`.

The plan's Global Constraints anticipated this exactly: *"`sysc-157` may land `godbus` first. If `go.mod`
already requires it, do not re-pin — just use it."* That is now the live case. **Skip the pinning half of
Task 1 and the `GOPROXY=off` resolution dance.**

### 2. A section-routing seam exists — D11.4 is mostly discharged

D11.4 calls it "a time-critical item": `TogglePanel` takes no section parameter, so a widget routing to
the Media section of the control centre "has no clean way to ask for it", and would otherwise "repeat the
notifications workaround".

`TogglePanel(id, output, trig)` (`panelhost.go:428`) still takes no section. **But a section path now
exists and is in production use.** `selectPanelSectionLocked(id, section)` is at `panelhost.go:283`, with
callers at `panelhost.go:244,249` on a path that takes `(id, section, action)`, and the Bluetooth widget
already routes a right-click into a named control-centre section at `registry.go:944`:

```go
case action == panelBluetoothAction && button == buttonRight:
        trig.AnchorX = bar.actionCenterX(panelBluetoothAction)
        if err := r.OpenPanel(PanelControlCenter, out, trig); err != nil {
                return false
        }
        r.mu.Lock()
        err := r.selectPanelSectionLocked(PanelControlCenter, "bluetooth")
        r.mu.Unlock()
        return err == nil
```

**Follow that precedent in Task 8.** Do not invent a seam and do not repeat the notifications workaround.
Note the shape of the shipped convention while you are there: left-click opens the dedicated panel, and
right-click opens the control-centre page for the same domain. Media has no dedicated panel in this
slice, so decide deliberately what left-click does rather than inheriting it by accident.

### 3. `knownItems` still lacks `"media"`

Verified at `internal/config/config.go:239`: the map carries `clock`, `workspace`, `window-title`, the
metric ids, `weather`, `battery`, `notifications`, `running-apps`, `wordmark`, `launcher`, `wallpaper`,
`volume`, `wifi`, `bluetooth` and `group`. There is no `media`. The design's "verified free against
`knownItems`" still holds, and Task 8's addition is still required.

## What is blocked and what is not

- **Tasks 1–8 are unblocked.** The service and the widget do not depend on the control-centre spine.
- **Task 9 (the page) waits on `sysc-253`**, which is open. The plan deliberately leaves Task 9 light and
  says so rather than inventing a seam that may not match. Do not write speculative page steps; stop at
  Task 8 and report if the spine has not landed.
- **Task 10 re-slices the tracker**, splitting `sysc-156` into service, widget and page so the service
  stops depending on the spine. Do that split when you reach it — `sysc-156` currently declares
  dependencies on `sysc-154`, `sysc-201` and `sysc-253`, which is what makes the whole issue look blocked
  when two thirds of it is not.

## Hazards this project has already paid for

- **Never `go test ./...` and never `-race`.** Repo-wide builds lock this machine. Cap with
  `GOMAXPROCS=4 -p 2` and run named tests in one package.
- **Tests use a fake bus, never a real session bus.**
- **The `commit-msg` hook rejects substrings case-insensitively**: `claude`, `anthropic`, `chatgpt`,
  `openai`, `copilot`, `cursor`, `cody`, `tabnine`, `codex`, `gemini`, `bard`, `gpt-[0-9]`, `llm`,
  `ai assistant`, `bot`, `agent`. Ordinary words trip it — `both` contains `bot`, `Hallmark` contains
  `llm`. It also rejects `co-authored-by:` with an `@anthropic.com`-style address, `generated with`,
  `generated by`, and the robot emoji. **No attribution trailer.** Screen every message against the real
  pattern before committing; never `--no-verify`.
- **`bd` from the primary checkout**, or export `BEADS_DB=/home/nomadx/sysc-shell/.beads/beads.db` from a
  worktree. `pre-commit` runs `bd sync --flush-only` and stages the result on every commit, so verify the
  committed blob afterwards, never the working file before.
- **gopls reports false errors in worktrees** (`undefined: Registry`, `use of internal package not
  allowed`). Trust `go build`.
- **Art URLs are attacker-adjacent** (D11.2): a player names a path this shell then reads. Keep the
  bounds, timeouts and decode cap, and prefer refusing remote fetches outright in this slice.

## The live gate is runnable

The laptop answers on `ssh -p 7777 nomadx@192.168.0.64` (`archThink`), runs Niri, and has
`sysc-shell.service` active. `go.mod` pins `sysc-notify v0.1.0-rc.3`, which that host **cannot resolve**,
so build on archPC and copy the binary:

```bash
GOMAXPROCS=4 go build -p 2 -o /tmp/sysc-shell ./cmd/sysc-shell
scp -P 7777 /tmp/sysc-shell nomadx@192.168.0.64:/tmp/sysc-shell.new
ssh -p 7777 nomadx@192.168.0.64 'install -m755 /tmp/sysc-shell.new ~/.local/bin/sysc-shell && systemctl --user restart sysc-shell.service'
```

Back the existing binary up first, compare checksums after the copy, and verify from the journal rather
than from `systemctl is-active`: **a handler panic reads as `active` while painting nothing.** The
laptop's clock runs behind archPC's, so journal timestamps will not line up across the two machines.

One hazard recorded by an earlier session and worth repeating: a second agent deploying over
`~/.local/bin/sysc-shell` concurrently. Confirm the provenance of whatever is installed before drawing a
conclusion from it.

## Start here

1. Confirm the parity merge has landed on `main`, then branch for the slice.
2. Task 1, skipping the dependency pin — it is already satisfied.
3. Stop at Task 8. Report rather than guess if `sysc-253` has not landed.
