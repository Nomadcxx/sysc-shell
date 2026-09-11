# Media service, widget and page — Design

Date: 2026-09-11. Status lives in bd.

Approach owner-approved 2026-09-11 in brainstorming. The decisions below are
pending owner review.

Sixth document of the parity tranche, and the one feature slice in it. An MPRIS
service as a peer of `internal/services/audio.go`, a thin `media` bar widget, and
the control-centre Media page that replaces its disabled destination.

`sysc-156` states the requirement: "Requires a separately approved discovery,
active-player, metadata/art, command, and position-update design." This is that
design.

Prior art is `docs/plans/2026-09-06-connectivity-and-media-prior-art.md`, which
already did the comparison work and whose guidance this design follows rather
than repeats. Behaviour and architecture reference only; no QML or C++ imported.

Sources (read, not imported):

- `docs/plans/2026-09-06-connectivity-and-media-prior-art.md` — the division of
  service, widget and page, and the MPRIS reading list
- `docs/plans/2026-09-11-network-panel-design.md` — D1 service contract shape,
  D2 the pinned-binding rung order
- `internal/services/audio.go` — the house service shape
- `internal/config/config.go:239` — `knownItems`
- bd `sysc-156`, `sysc-154`/`sysc-253`
- `noctalia/src/dbus/mpris/mpris_service.cpp` — discovery and active-player
  selection; `mpris_art.cpp` — art off the paint path

## Goal and scope

In:

- `internal/services/media.go`: discovery, active-player selection, metadata,
  album art, position, and commands.
- The `media` bar widget: one glyph, optional label, gestures.
- The control-centre Media page.

Out:

- Exposing this shell *as* an MPRIS player. We are a controller, not a source.
- The audio spectrum. It needs PipeWire capture, which is a different service
  and a different design.
- Album art as a full-bleed card background. That needs the stacking design and
  arrives with it, not here.
- Lock-screen and OSD media consumers. Later, and they are why D5 puts position
  interpolation in the service.

## Decisions

### D1 — Service ownership follows the house shape

`internal/services/media.go`, constructed and owned by `Registry` beside the
existing services, never by a panel or widget. The public surface copies the
shape the network design settled, so consumers cannot tell a pushed service from
a polled one:

```go
func (m *Media) Available() bool
func (m *Media) State() MediaState
func (m *Media) CachedState() MediaState
func (m *Media) Players() []Player
func (m *Media) Changes() <-chan MediaState
func (m *Media) Acquire() (*Lease, error)
```

`CachedState` exists for the same reason as `audio.go`'s and `network.go`'s:
callers holding `Registry.mu` or running on the Wayland owner must never
trigger I/O.

### D2 — Hand-rolled on `godbus/dbus/v5`, and why that differs from network

AGENTS.md fixes the rung order: "existing project code, Go standard library,
native Linux service, pinned dependency, then new code." The network design took
a binding because "it covers everything this design asks of the client side."
Evaluated 2026-09-11, no MPRIS binding does:

| Candidate | Verdict |
|---|---|
| `github.com/leberKleber/go-mpris` v1.1.0, MIT | A real client — commands, `Position`, `SeekTo`, metadata with `MPRISArtURL()`, the `Seeked` signal. **No player discovery**, which is the first capability `sysc-156` names. Last updated November 2022. |
| `github.com/go-music-players/mpris` v0.1.0, MIT | A **server** library for exposing your own app as a player. Wrong direction entirely. |

So the rung order is satisfied by moving past a dependency that does not cover
the requirement, not by preferring new code. The second fact that makes this
comfortable: MPRIS is two small stable interfaces — `org.mpris.MediaPlayer2` and
`org.mpris.MediaPlayer2.Player` — a handful of properties, five commands, and
one signal. NetworkManager is not remotely that, which is why the opposite call
was right there.

`godbus/dbus/v5` is the only dependency, and the in-flight network work
(`sysc-157`) already pins it. If media lands first it adds the pin and records
the reason; if network lands first it inherits it.

### D3 — Discovery and active-player selection

Players appear as bus names prefixed `org.mpris.MediaPlayer2.`. The service
lists names at start, then subscribes to `NameOwnerChanged` to add and drop
players as they come and go. No polling.

Active-player selection is the part that misbehaves if left implicit. The rule,
in order: the player the user last interacted with through this shell; otherwise
the most recently playing; otherwise the first by bus name so the choice is at
least stable. A player that disappears releases the selection immediately rather
than leaving commands pointed at a dead name.

Configuration gets a preferred-player hint and a blacklist. Noctalia and DMS
both grew these because browsers register per-tab players that nobody wants to
control from a bar.

### D4 — Metadata and art stay off the paint path

`PropertiesChanged` on the Player interface carries metadata, playback status
and volume. The service decodes it into a plain snapshot; consumers never touch
a D-Bus type.

Album art arrives as `mpris:artUrl`, usually a `file://` path, sometimes remote.
Fetching or decoding it on the paint path would stall a frame, which is the
mistake `wallpaperThumbFor` documents having already paid for elsewhere in this
tree. Art resolves through the existing async icon-worker pattern: the snapshot
carries an identifier, a decoded raster appears later, and the surface
republishes. A remote URL is fetched with a bound and a timeout, or dropped.

### D5 — Position interpolation lives in the service

MPRIS reports `Position` on request and emits `Seeked` only on discontinuities,
so a progress bar that wants smooth movement must interpolate from the last
known position and the playback rate.

That interpolation belongs to the service, not to each consumer. The prior art
makes the argument with a count: Noctalia's `mpris_service` has **eight**
independent consumers — bar widget, plugin widget, four control-centre
surfaces, a desktop widget, an OSD, and the lock screen. Interpolating in each
means eight timers and eight subtly different answers.

The service therefore exposes a position that is correct when asked, without
running a timer of its own when nobody is watching. A consumer that wants a
moving bar drives it from the existing per-surface animator, which already
owns that surface's cadence and settles.

### D6 — Commands

`PlayPause`, `Next`, `Previous`, `Stop`, and `SetPosition`. Each is a method
call on the active player, issued off the Wayland owner through the established
`scheduleControl` seam so a UI handler never blocks on D-Bus.

A command against a player that vanished mid-flight fails quietly and triggers
re-selection. It does not surface an error toast: the user pressed next on
something that stopped existing, and the correct repair is to update the display.

### D7 — The bar widget is `media`, and its scope is small

`media` is **free**. `knownItems` holds `clock`, `workspace`, `window-title`,
`cpu`, `memory`, `temperature`, `gpu`, `filesystem`, `block`, `network`,
`weather`, `battery`, `notifications`, `running-apps`, `wordmark`, `launcher`,
`wallpaper`, `volume`, `group` and `plugin`, and no `"media"` string exists in
the tree. This was checked deliberately: prior art §3 records that `network` was
already taken by the throughput metric, and discovering that late costs a config
migration.

Scope, per prior art §2 — the widget is "not the page, but smaller": one glyph,
an optional scrolling title, left-click routing to the control-centre Media
section, right-click or middle-click toggling play, and scroll for next and
previous. **No player picker, no seek bar, no volume.** `volume` is already its
own widget and stays separate.

### D8 — The page, and why it is a second issue

Per prior art §1, the service and the page are different work with different
dependencies. The service is a peer of `audio.go` and depends on nothing in the
control centre; only the page depends on the spine (`sysc-253`). Filing them as
one issue makes the backend look like part of the control centre and makes
`bd ready` lie about what can be picked up in parallel.

So: a service issue with no spine dependency, a page issue depending on the
service and the spine, and a widget issue depending only on the service.

### D9 — Testing

Per-package named tests only. **Do not run `go test ./...` or `-race`.**

- A fake bus, not a real session bus. The network design's D14 reached the same
  conclusion: "a fake backend in tests beats a fake system bus."
- Discovery: a name appearing and vanishing adds and drops a player.
- Selection: last-interacted wins; a vanished player releases selection; the
  fallback ordering is stable.
- Metadata: a malformed or partial `Metadata` map degrades to a usable snapshot
  rather than failing the service.
- Art: a missing, unreadable, or oversized `artUrl` leaves the snapshot valid
  and the surface paintable.
- Position: interpolation matches a known rate; `Seeked` resets it; a paused
  player does not advance.
- Commands against a dead player fail quietly and re-select.
- The widget carries no device or player list.

### D10 — Tracker

`sysc-156`, re-sliced per D8 into service, widget and page. The service issue
drops its dependency on the control-centre spine.

### D11 — Open risks

1. **Browsers register noisy per-tab players.** D3's selection rule and
   blacklist are the mitigation, and neither can be validated without real
   players on real hardware.
2. Art URLs are attacker-adjacent: a player names a path this shell then reads.
   Bounds, timeouts and a decode size cap are required, and remote fetching may
   be worth refusing entirely in a first slice.
3. `godbus/dbus/v5` is not yet in `go.mod`. Whichever of media or network lands
   first carries the pin, and the network design records a `GOPROXY=off`
   resolution trap worth reading before adding it.
4. Prior art flags a time-critical item this design inherits: `TogglePanel`
   takes no section parameter, so a widget routing to the Media section of the
   control centre has no clean way to ask for it. If the spine work adds that
   parameter, this widget costs a glyph and a case; if not, it repeats the
   notifications workaround.
