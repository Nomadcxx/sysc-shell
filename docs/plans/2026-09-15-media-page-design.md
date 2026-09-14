# Media page, bar parity and D3 configuration — Design

Date: 2026-09-15. Status lives in bd.

Owner-commissioned 2026-09-15 after the media service and bar widget landed
(sysc-281, sysc-282) and the owner's review named what was missing for full
functionality and Noctalia v4/DMS parity: the control-centre Media page and
Home summary (sysc-156), the bar's state-swapped glyph and scrolling title
(sysc-284), and design D3's configuration surface (sysc-283).

Eighth document of the parity tranche's media line. Follows
`2026-09-11-media-service-design.md` (the service contract this page consumes)
and `2026-09-06-connectivity-and-media-prior-art.md` (the division of service,
widget and page). Behaviour and architecture reference only; no QML or C++
imported.

Sources (read, not imported):

- `internal/shell/popout_controlcenter.go`, `controlcenter_pages.go`,
  `bluetoothbody.go`, `panelhost.go` — the spine as it exists on main
- `internal/icons/worker.go`, `internal/shell/tray.go`,
  `popout_wallpaper.go` — the async image pipeline and its bounds
- `internal/shell/bar.go`, `animation.go`, `internal/render/paint.go`,
  `truncate.go` — the animator and text paint the marquee extends
- `internal/render/materialfont.go`, `icons/material/build.py` — the glyph
  procedure
- bd `sysc-156`, `sysc-253`, `sysc-283`, `sysc-284`
- Noctalia v4.7.7 bar media widget and media page (the standing parity target)

## Goal and scope

In:

- The control-centre Media page replacing the disabled destination, and a
  live Now-playing tile on the Home dashboard.
- Bar widget parity: state-swapped glyph, scrolling (marquee) title.
- Service additions: `CanSeek`, `Configure(preferred, blacklist)`, and the
  "most recently playing" middle rule.
- The live Niri/MPRIS gate the landed slice never ran.

Out:

- A standalone media panel. sysc-156 names the CC page and Home summary only;
  no design calls for a second host of the same body.
- Exposing this shell as an MPRIS source. We remain a controller.
- Full-bleed album art. `KindStack` is unlanded and its design reserves the
  consumer gate for the weather card; art renders as a bounded rounded image
  until stacking arrives.
- The audio spectrum and lyric views. Different services, different designs.

## Decisions

### D1 — One shared body; enabling the section is the spine change

`mediaBody(r *Registry, h *PanelHost) *ui.Node` in a new
`internal/shell/mediabody.go`, mirroring `bluetoothBody`: the Registry
supplies cached state, the body never constructs a service or performs a bus
call. The whole dispatch change is `Enabled: true` on the `media` entry of
`ccSections` (`popout_controlcenter.go:33`) plus `case "media": return
mediaBody(r, h)` in `ccPage`. Because the rail, `panelSection` IPC
(`panelhost.go:263-272`) and the bar widget's
`selectPanelSectionLocked(PanelControlCenter, "media")` all read the same
registry entry, every route opens at once. There is no second tree: the
comment discipline that forbids a second Bluetooth device tree forbids a
second media tree.

sysc-253's spine owns the section seam this page needs, and the seam exists
on `main`. Whether sysc-253 closes is a bd judgement for the owner; this
design records no status.

### D2 — Page composition, on the ladder

The page is a column at `theme.MarginL` inside the existing CC viewport
(`KindScroll`, body width 596 at the 700×564 panel). Three cards on the
`monitorCard` idiom — `KindCapsule`, `m.CardPadding`, `ui.FillContainerHigh`,
`ui.ShapeCard`:

1. **Now playing.** A row: rounded album art (`KindImage`, square, box
   reserved via `ImageSize` so a late or failed decode never reflows the
   card) and a column — title at `RoleTitle`, artist and album at
   `RoleCaption`, and the active player's `Identity` as a label line. Empty
   metadata renders a dash, not a zero, as `NetworkState`'s comment
   establishes. No player at all cannot reach this card: the page renders a
   single "Nothing playing" card instead.
2. **Transport.** One row of icon buttons in the `centreIconButton`
   vocabulary: previous, play/pause, next. Play/pause swaps `play_arrow` /
   `pause` with `Status`; each button is gated by `CanPrev` / `CanPlay` /
   `CanNext` and disabled through the `ccDisable` idiom. No stop button: the
   reference shells do not offer one on the page, and D6's command set keeps
   `Stop` available to IPC.
3. **Position and players.** A position row — `KindMeter` when merely
   displaying, `KindSlider` when seeking is possible (`CanSeek` and
   `LengthUS > 0`); `Min 0`, `Max LengthUS`, step one second; an `mm:ss`
   readout with `MinWidthText "00:00"` and `Tabular`. Below it, the player
   list — the one picker in the shell — rows at `m.StandardControl`,
   `PinEnd`, `ShapeSmall` per the panel-list-row designs, active row
   `StateSelected`, click = `Prefer`.

Every vertical measure derives from `Metrics` rungs; the only literal is the
art box, fixed at implementation against the reference capture and recorded
in the completion handover.

### D3 — Lock discipline

The body builds under `Registry.mu` on the Wayland owner and reads only
memory: `r.media.CachedState()` (which interpolates position from the clock —
no I/O) and `r.media.Players()`. Every transport write — `PlayPause`, `Next`,
`Previous`, `SetPosition` — runs through `r.scheduleControl` with a
completion that re-locks, rebuilds the host and republishes, exactly like
`applyAudioControl`. `Prefer` mutates service memory only and runs under
`Registry.mu` directly.

### D4 — The page holds the lease

`startMediaBodyLocked(h)` / `leaveMediaBodyLocked(h)` mirror the Bluetooth
hook pair. Entering the media section acquires a lease stored on the
`PanelHost` (`h.mediaLease`); leaving the section, and closing the CC host
from the `closeAllPanelsLocked` path, releases it. The service's
contract — "the widget and the page acquire for themselves, and the service
stops while nothing watches" — is finally exercised by its second consumer.
A check fails if leaving the page leaves the lease held.

### D5 — Service additions (sysc-283)

`MediaState` gains `CanSeek bool`; `probePlayer` decodes it beside the other
`Can*` properties. A new `Media.Configure(preferred string, blacklist
[]string)` re-runs selection and republishes; the Registry applies
`config` at construction and on reload. Selection order becomes:

1. the player last interacted with through this shell (`Prefer`), else
2. the configured preferred player, if present on the bus, else
3. the most recently playing player (a per-player timestamp recorded on the
   transition into `Playing`), else
4. the first player by bus name.

Blacklisted names never enter the player set at all: discovery skips them,
name events ignore them, and re-`Configure` reconciles the live set, so the
picker hides what the user rejected. With no configuration, behaviour is
exactly what shipped.

### D6 — Art worker

A third small `icons.Worker` (`r.mediaArt`, lazily started like
`wallpaperThumbsLocked`, own cancel context) so track art can never evict
tray or notification icons. The request key is the `ArtKey` — a `file://`
URL — parsed with `net/url`: non-`file` schemes and non-empty hosts are
refused (the service already refuses them; the worker re-checks), and
`URL.Path` supplies the percent-decoded path, which `icons.Resolve` takes
as-given. Per-job bounds: the worker's own 8 MiB `readBounded` cap and
4096 px dimension check, plus the timeout D11.2 mandates — a five-second
watchdog per job (the session-operations precedent); a timed-out job
publishes nil and negative-caches the key until the snapshot's `ArtKey`
changes, so a stuck path costs one attempt per track, never a repaint stall.
The theoretical abandoned-reader goroutine is bounded by the worker's
in-flight collapse and is recorded here rather than hidden.

### D7 — Marquee

New `Node` fields: `Marquee bool` (intent, set by the widget) and
`TextOffset int` (the per-frame scalar). New render primitive
`paintTextMarquee`: when the shaped advance fits the cell it paints as
today; otherwise it clips the cell and draws at `-TextOffset`, wrapping
with a gap of eight space-glyph advances measured on the painted face. The
bar gains a resolve step beside `resolveGradientMotionLocked`: it measures
the title's advance, and only when the text overflows **and** motion is
permitted does it write the marquee's offset onto the copied tree via a new
linear sweep animator mode (`animSweep`: 0→1 wrapping, never settling,
pinned to 0 under reduced motion — the gradient loop is ping-pong and would
scroll backwards) and start bar frames. When the title fits or motion is
reduced, the resolve clears `Marquee` on the copy so paint falls back to
the existing ellipsis truncation. Trip duration is
`(advance + gap) / 30 px·s⁻¹` — the one tunable in the design, to be
measured against the reference capture in the live gate and recorded. The
title node carries `MaxWidth` from `config.Item.MaxWidth` (window-title's
default) and a stable `Key` so animator identity survives rebuilds.
`window-title` is the named future consumer, not a change in this slice.

### D8 — Bar state glyph

`refreshMediaWidget` swaps the glyph with transport state: playing →
`pause`, paused → `play_arrow`, stopped or unknown → `music_note`. The
widget's gestures are unchanged (approved D7 of the landed design): left
click routes to the CC Media section, middle/right toggles play, scroll
steps next/previous.

### D9 — Home tile

`ccHome`'s right column gains a Now-playing `ccQuickTile`: the state glyph
of D8, the track title or "Nothing playing", action `section:media`,
disabled via `ccDisable` when `!Available`. It reads `r.media.CachedState()`
beside the existing cached reads at the top of `ccHome`, so the dashboard
summary sysc-156 asks for costs one tile and no new machinery.

### D10 — Activation seam

`media:` actions — `media:playpause`, `media:next`, `media:prev`,
`media:seek:<us>`, `media:player:<bus>` — dispatch in the shared panel
activation chain beside the `audio-` prefix handlers. A seek records its
pending value on the host during drag (`h.mediaSeekPending *int64`), shows
it in the readout, writes through `scheduleControl` on release, and is
cleared by a track change (a `Player` or `Title` change in the snapshot) or
by the completed write. The keyboard path is the existing `adjustSlider`
route, which already reaches prefixed actions.

### D11 — Live gate

Build and deploy the exact binary to the laptop; never overwrite another
session's `~/.local/bin/sysc-shell`, and kill by pid from
`pgrep -f 'scratchpad/<name>'`, never `pkill -f` a typed name. Assert mapped
surfaces with `niri msg -j layers`. Exercise: one real player plus a
browser's per-tab players; selection under interaction; the blacklist
hiding a named player; marquee only on overflow; seek on a seekable track;
art decode on a local cover; the tile and page agreeing with the bar. One
output (DP-1) is enough. Unrunnable items are recorded in the completion
handover, never assumed away.

### D12 — Tracker

sysc-156 closes on the page and tile; sysc-283 closes on Task 1; sysc-284
closes on Task 3. sysc-253's disposition is the owner's: the seam exists,
and this design claims nothing about the spine's remaining scope. Status
lives in bd, not here.

## Risks

1. **The TTF rebuild** needs the pinned upstream Material Symbols font; if
   the fetch is impossible, Task 2 blocks visibly rather than shipping a
   font of unknown provenance.
2. **`paintTextMarquee` is the slice's only new render capability.** It is
   guarded by one boolean and one field, its fallback is the existing
   truncation, and its wrap math is table-tested.
3. **Browser per-tab players** are the selection rule's real adversary; D5's
   blacklist is the mitigation and only the live gate can show it working.
4. **Concurrent `feature/parity-task10-11`** touches neighbouring parity
   surfaces; this branch re-bases onto `main` before its live gate, and
   merge order belongs to the integrator.
5. **Paint-path cost.** The position resolve and marquee measurement run per
   frame while media plays; both are memory reads and one text measurement,
   inside the render pass that already copies the tree.
