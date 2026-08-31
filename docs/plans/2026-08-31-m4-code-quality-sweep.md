# M4 Code Quality Sweep

Date: 2026-08-31
Issue: `sysc-37`
Commit: `01cae98`
Scope: landed Milestone 4 (panels, controls, settings, OSD, theming). M3 widget packages are out of
scope except where M4 calls them.

This is an engineering pass: correctness under the written contracts, races, leaked work, silent
failure, and tests that do not prove what they claim. It is not a ponytail cut list. Nothing was
applied.

## Ranked findings

### 1. External volume/brightness cannot raise the OSD (correctness)

`relayAudioOSD` / `relayBrightnessOSD` range `Changes()`. Those channels only receive while the
service is leased. `OSDStep` acquires, steps, shows, and releases. After that the poller is stopped.
A `wpctl set-volume` from another client never reaches `Show`.

The Task 14 test passes because it calls `Acquire()` itself. Production has no equivalent standing
lease.

Fix: hold one process-lifetime lease per available service (or lease for the OSD timeout plus a
quiet period), and keep the relay. Do not start a second sampler.

See `sysc-38`.

### 2. Template apply does not theme other apps (correctness)

`ApplyEnabled` writes niri and gtk to the right trees. Every other catalog name lands in
`~/.config/sysc-shell/themes/<name>.conf`. Alacritty, kitty, foot, helix, and the rest never see
the file. Kitty is not signalled. Apply errors are assigned to `_`.

The catalog tests prove embed, niri include, gtk name policy, and “do not overwrite foreign
files”. They do not prove an app config path.

See `sysc-39`.

### 3. OSD is a bar without the specified chrome (contract)

`osd.render` fills a rounded rect and a 6 px level strip. Task 10 required glyph + label + level
bar and 4A reveal motion gated by reduced-motion. Reduced-motion is implemented as “one
invalidation”. Motion for the OSD is not.

Same issue: `sysc-38`.

### 4. Settings schema is a snapshot of defaults (correctness)

`settings.Default()` calls `addWidgetEntries(config.Default())`. Opening settings on a live
registry still uses that snapshot, not `r.cfg`. Widget options for a bar that differs from
defaults never appear. Bar item lists are not entries at all.

The settings panel also never sets `KindVirtualList` / `Node.Item`. The keyboard acceptance test
flips the scroll node’s kind in memory. That does not prove a virtual list in the product tree.

See `sysc-40`.

### 5. `OSD.Show` can block with the registry mutex held (reliability)

`Show` locks `Registry.mu` then `sendAux`. `sendAux` waits on a full `aux` channel (cap 8) until
`closed`. A hide timer, `Close`, and a panel open can contend on that lock while the owner is not
draining aux. Unblock by sending without the lock, or make aux send fail-open the way
`publishSurface` already does when closed.

### 6. `SetMute` uses `wpctl set-volume … mute` (risk)

PipeWire’s `wpctl` mute verb is `set-mute`. The fake test script only implements `get-volume`.
Live mute binds may no-op or error. Confirm on a machine with `wpctl` before changing the call;
if it fails, switch to `set-mute`.

### 7. IPC `status` is thinner than specified (gap)

4A asked for version, open panels, and matugen presence. 4B asked for template enables. The
handler returns version plus two booleans. Not a crash, but operators cannot probe the shell the
design described.

## Inspected, not a defect

- Two surfaces per panel, Exclusive only on the panel, OSD without a shield: matches D1/D7.
- Reload does not tear down aux: `TestReloadKeepsOpenPanel`.
- Reveal goroutine stops on close: `TestClosingDuringRevealStopsTicker`.
- Relays exit on `Registry.closed`.
- Atomic config write: unique temp, 0600, sync, rename, cleanup.
- `runningAsTest()` skipping live template apply during `go test` is the right ceiling; it is not
  a substitute for injecting `$HOME` in unit tests.
- `sysc-wayland v0.2.0` without `replace`.
- Virtual-list layout code itself (`VisibleRange`, `Node.Item`) looks sound; the miss is the
  missing consumer.

## Tests that over-claim

| Test | What it actually proves |
|---|---|
| `TestAcceptOsdOnEachOutputExternalChange` | OSD opens when **the test** holds an audio lease. |
| `TestAcceptKeyboardOnlyAllControls` virtual-list branch | PageDown on a `KindScroll` after `Kind` is overwritten. |
| `TestAcceptNiriTemplateLiveApply` | Include injection in a temp dir, not `ApplyEnabled` against `$HOME`. |

## Not applied

No deletions, path changes, or lease changes in this commit. The three follow-ups are `sysc-38`,
`sysc-39`, `sysc-40`.
