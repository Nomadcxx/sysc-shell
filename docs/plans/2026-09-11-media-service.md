# Media service, widget and page Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** An MPRIS service that is a peer of `internal/services/audio.go`, a thin `media` bar widget, and the control-centre Media page that replaces its disabled destination.

**Architecture:** One service owns discovery, active-player selection, metadata, art and position; consumers read immutable snapshots and never touch a D-Bus type. Hand-rolled on `godbus/dbus/v5` because no MPRIS binding covers discovery. Art resolves through the existing async worker pattern so decoding never lands on the paint path, and position interpolates in the service rather than in each of its eventual consumers.

**Tech Stack:** Go 1.26.4, `github.com/godbus/dbus/v5 v5.2.2` (MIT).

**Spec:** `docs/plans/2026-09-11-media-service-design.md`

## Global Constraints

- **Never run `go test ./...` or any `-race` build.** Run named tests in one package: `go test ./internal/services -run TestMedia`.
- `go test ./internal/shell` is safe to run directly — `runArgvDefault` (`popout_session.go:270`) refuses under `testing.Testing()`.
- **Tests use a fake bus, never a real session bus.** The network design reached the same conclusion: a fake backend beats a fake system bus.
- Go only. No CGO. One new module: `github.com/godbus/dbus/v5 v5.2.2`, which is **already in the local module cache** (`~/go/pkg/mod/cache/download/github.com/godbus/dbus/v5/@v/v5.2.2.zip`), so `GOPROXY=off` resolves it. The network design records a `go mod tidy` trap under `GOPROXY=off`; if resolution fails, read that design's D2 before improvising.
- `sysc-157` (network) may land `godbus` first. If `go.mod` already requires it, do not re-pin — just use it.
- Services never import `internal/shell` or Wayland. A panel never calls D-Bus directly; writes go through `scheduleControl`.
- **The `commit-msg` hook rejects these substrings, case-insensitively:** `claude`, `anthropic`, `chatgpt`, `openai`, `copilot`, `cursor`, `cody`, `tabnine`, `codex`, `gemini`, `bard`, `gpt-[0-9]`, `llm`, `ai assistant`, `bot`, `agent`. Ordinary words trip it — `both` contains `bot`. Screen every message:
  ```bash
  grep -oiE "(claude|anthropic|chatgpt|openai|copilot|cursor|cody|tabnine|codex|gemini|bard|gpt-[0-9]|llm|ai assistant|bot|agent)" msg.txt && echo BANNED || echo clean
  ```
  No `Co-Authored-By` trailer. Never `--no-verify`.

## File Structure

| Path | Responsibility |
|---|---|
| `internal/services/media.go` | The service: state, lifetime, snapshot |
| `internal/services/media_discovery.go` | Bus-name discovery and active-player selection |
| `internal/services/media_bus.go` | The `bus` interface and its godbus implementation — the test seam |
| `internal/services/clock.go:39` | `Lease` gains a `media` field |
| `internal/shell/registry.go:159,182` | Construction and `setMedia` |
| `internal/shell/mediawidget.go` | The `media` bar widget |
| `internal/config/config.go:239` | `knownItems` gains `"media"` |
| `internal/shell/popout_*` | The control-centre Media page |

---

### Task 1: The bus seam and the service skeleton

**Files:**
- Create: `internal/services/media_bus.go`
- Create: `internal/services/media.go`
- Test: `internal/services/media_test.go`

**Interfaces:**
- Consumes: nothing.
- Produces: `type MediaState struct`, `type Player struct`, `func NewMedia(b bus) *Media`, and `Available() bool`, `State() MediaState`, `CachedState() MediaState`, `Players() []Player`, `Changes() <-chan MediaState`, `Acquire() (*Lease, error)`, `Close()`.

- [ ] **Step 1: Write the failing test**

Create `internal/services/media_test.go`:

```go
package services

import "testing"

// fakeBus is the seam. A fake bus beats a fake session bus: it is
// deterministic, needs no running player, and cannot be affected by whatever
// happens to be playing on the developer's desktop.
type fakeBus struct {
	names   []string
	props   map[string]map[string]any
	calls   []string
	nameCh  chan nameChange
}

func newFakeBus(names ...string) *fakeBus {
	return &fakeBus{names: names, props: map[string]map[string]any{}, nameCh: make(chan nameChange, 8)}
}

func (f *fakeBus) ListNames() ([]string, error)          { return f.names, nil }
func (f *fakeBus) NameChanges() <-chan nameChange        { return f.nameCh }
func (f *fakeBus) Get(busName, iface, prop string) (any, error) {
	if m, ok := f.props[busName]; ok {
		return m[prop], nil
	}
	return nil, nil
}
func (f *fakeBus) Call(busName, method string, args ...any) error {
	f.calls = append(f.calls, busName+"."+method)
	return nil
}
func (f *fakeBus) Close() {}

func TestMediaWithNoPlayersIsUnavailable(t *testing.T) {
	t.Parallel()
	m := NewMedia(newFakeBus())
	t.Cleanup(m.Close)
	if m.Available() {
		t.Error("no players on the bus, but the service reports available")
	}
	if got := m.State(); got.Title != "" {
		t.Errorf("state = %+v, want zero", got)
	}
}

func TestMediaCachedStateNeverTouchesTheBus(t *testing.T) {
	t.Parallel()
	// CachedState exists for callers holding Registry.mu or running on the
	// Wayland owner. It must answer from memory, exactly as audio.go's does.
	b := newFakeBus("org.mpris.MediaPlayer2.spotify")
	m := NewMedia(b)
	t.Cleanup(m.Close)
	before := len(b.calls)
	_ = m.CachedState()
	if len(b.calls) != before {
		t.Error("CachedState issued a bus call")
	}
}

func TestMediaIgnoresNonPlayerNames(t *testing.T) {
	t.Parallel()
	// Only names under the MPRIS prefix are players. A bus carries hundreds of
	// unrelated names.
	m := NewMedia(newFakeBus("org.freedesktop.Notifications", "org.gnome.Shell"))
	t.Cleanup(m.Close)
	if len(m.Players()) != 0 {
		t.Errorf("players = %v, want none", m.Players())
	}
}
```

- [ ] **Step 2: Run and watch it fail**

Run: `go test ./internal/services -run TestMedia -v`
Expected: FAIL — `undefined: NewMedia`.

- [ ] **Step 3: Add the dependency**

```bash
GOFLAGS=-mod=mod go get github.com/godbus/dbus/v5@v5.2.2
go mod tidy
git diff --stat go.mod go.sum
```

If `go.mod` already requires it from the network slice, skip this step entirely.

- [ ] **Step 4: Define the seam**

Create `internal/services/media_bus.go`:

```go
package services

// bus is the D-Bus surface this service needs, and nothing more.
//
// It exists so tests can drive discovery, metadata and commands without a
// session bus. It is not a swappable-backend abstraction: there is exactly one
// real implementation and there will not be a second. If the test seam ever
// stops being the justification, delete the interface rather than populate it.
type bus interface {
	ListNames() ([]string, error)
	NameChanges() <-chan nameChange
	Get(busName, iface, prop string) (any, error)
	Call(busName, method string, args ...any) error
	Close()
}

// nameChange is one bus name appearing or vanishing.
type nameChange struct {
	Name    string
	Acquired bool
}
```

The godbus implementation lands in the same file: connect to the session bus, `AddMatch` on `NameOwnerChanged`, and translate signals into `nameChange`.

- [ ] **Step 5: Write the service skeleton**

Create `internal/services/media.go` with the house contract, copying `audio.go`'s shape — `Changes()` returning a channel, `State()`/`CachedState()`, `Available()`, `Acquire()`/`release()`, `Close()`, and the `startLocked`/`stopIfUnusedLocked` lifetime so the service stops when nothing holds a lease.

```go
// PlaybackStatus is the player's transport state.
type PlaybackStatus uint8

const (
	PlaybackStopped PlaybackStatus = iota
	PlaybackPaused
	PlaybackPlaying
)

// MediaState is one immutable snapshot. Consumers never see a D-Bus type.
type MediaState struct {
	Available  bool
	Player     string // bus name of the active player
	Identity   string // human name, from org.mpris.MediaPlayer2.Identity
	Title      string
	Artist     string
	Album      string
	ArtKey     string // identifier for the async art worker, never a decoded image
	Status     PlaybackStatus
	PositionUS int64
	LengthUS   int64
	Rate       float64
	CanNext    bool
	CanPrev    bool
	CanPlay    bool
}

// Player is one discovered player.
type Player struct {
	Bus      string
	Identity string
	Active   bool
}
```

- [ ] **Step 6: Run and watch the tests pass**

Run: `go test ./internal/services -run TestMedia -v`
Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add go.mod go.sum internal/services/media.go internal/services/media_bus.go internal/services/media_test.go
git commit -m "feat(services): add the media service skeleton and its bus seam"
```

---

### Task 2: Discovery

**Files:**
- Create: `internal/services/media_discovery.go`
- Test: `internal/services/media_test.go`

**Interfaces:**
- Consumes: Task 1's `bus`.
- Produces: discovery wired into `Players()`.

- [ ] **Step 1: Write the failing tests**

```go
func TestMediaDiscoversPlayersAtStart(t *testing.T) {
	t.Parallel()
	m := NewMedia(newFakeBus(
		"org.mpris.MediaPlayer2.spotify",
		"org.freedesktop.Notifications",
		"org.mpris.MediaPlayer2.firefox.instance_1_5",
	))
	t.Cleanup(m.Close)
	if got := len(m.Players()); got != 2 {
		t.Fatalf("players = %d, want 2", got)
	}
}

func TestMediaAddsAndDropsPlayersOnNameChanges(t *testing.T) {
	t.Parallel()
	// No polling: the session bus tells us. A player that vanishes must go
	// immediately, or commands point at a dead name.
	b := newFakeBus()
	m := NewMedia(b)
	t.Cleanup(m.Close)

	b.nameCh <- nameChange{Name: "org.mpris.MediaPlayer2.vlc", Acquired: true}
	waitFor(t, func() bool { return len(m.Players()) == 1 })

	b.nameCh <- nameChange{Name: "org.mpris.MediaPlayer2.vlc", Acquired: false}
	waitFor(t, func() bool { return len(m.Players()) == 0 })
}
```

`waitFor` polls a predicate with a bounded deadline and calls `t.Fatal` on timeout. If `internal/services` already has such a helper, use it.

- [ ] **Step 2: Run and watch them fail**

Run: `go test ./internal/services -run TestMediaDiscover -v`
Expected: FAIL.

- [ ] **Step 3: Implement discovery**

List names at start, filter on the `org.mpris.MediaPlayer2.` prefix, then consume `NameChanges()` in the service's run loop. Adding and removing both republish a snapshot on `Changes()`.

- [ ] **Step 4: Run and watch them pass**

Run: `go test ./internal/services -run TestMedia -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/services/
git commit -m "feat(services): discover media players from the session bus"
```

---

### Task 3: Active-player selection

**Files:**
- Modify: `internal/services/media_discovery.go`
- Test: `internal/services/media_test.go`

**Interfaces:**
- Produces: `func (m *Media) Prefer(busName string)`.

- [ ] **Step 1: Write the failing tests**

```go
func TestMediaPrefersTheLastInteractedPlayer(t *testing.T) {
	t.Parallel()
	m := NewMedia(newFakeBus("org.mpris.MediaPlayer2.a", "org.mpris.MediaPlayer2.b"))
	t.Cleanup(m.Close)
	m.Prefer("org.mpris.MediaPlayer2.b")
	if got := m.State().Player; got != "org.mpris.MediaPlayer2.b" {
		t.Errorf("active = %q, want the preferred player", got)
	}
}

func TestMediaSelectionIsStableWithoutAPreference(t *testing.T) {
	t.Parallel()
	// With nothing else to go on, the fallback must be deterministic, or the
	// bar widget flips between players between snapshots.
	b := newFakeBus("org.mpris.MediaPlayer2.z", "org.mpris.MediaPlayer2.a")
	m := NewMedia(b)
	t.Cleanup(m.Close)
	first := m.State().Player
	for i := 0; i < 5; i++ {
		if got := m.State().Player; got != first {
			t.Fatalf("selection changed between reads: %q then %q", first, got)
		}
	}
}

func TestMediaReleasesSelectionWhenThePlayerVanishes(t *testing.T) {
	t.Parallel()
	b := newFakeBus("org.mpris.MediaPlayer2.gone")
	m := NewMedia(b)
	t.Cleanup(m.Close)
	m.Prefer("org.mpris.MediaPlayer2.gone")
	b.nameCh <- nameChange{Name: "org.mpris.MediaPlayer2.gone", Acquired: false}
	waitFor(t, func() bool { return m.State().Player == "" })
}
```

- [ ] **Step 2: Run, implement, run**

Selection order, per design D3: last interacted through this shell; else the most recently playing; else the first by bus name. A blacklist from configuration filters the candidate set before any of that, because browsers register per-tab players nobody wants to control from a bar.

Run: `go test ./internal/services -run TestMedia -v`
Expected: PASS.

- [ ] **Step 3: Commit**

```bash
git add internal/services/
git commit -m "feat(services): choose one active media player deterministically"
```

---

### Task 4: Metadata, and art off the paint path

**Files:**
- Modify: `internal/services/media.go`
- Test: `internal/services/media_test.go`

- [ ] **Step 1: Write the failing tests**

```go
func TestMediaDecodesMetadata(t *testing.T) {
	t.Parallel()
	b := newFakeBus("org.mpris.MediaPlayer2.x")
	b.props["org.mpris.MediaPlayer2.x"] = map[string]any{
		"Metadata": map[string]any{
			"xesam:title":  "Ambush",
			"xesam:artist": []string{"Sepultura"},
			"xesam:album":  "Roots",
			"mpris:artUrl": "file:///tmp/art.png",
			"mpris:length": int64(215_000_000),
		},
	}
	m := NewMedia(b)
	t.Cleanup(m.Close)
	st := m.State()
	if st.Title != "Ambush" || st.Artist != "Sepultura" || st.Album != "Roots" {
		t.Errorf("metadata = %+v", st)
	}
	if st.LengthUS != 215_000_000 {
		t.Errorf("length = %d", st.LengthUS)
	}
}

func TestMediaSnapshotCarriesNoDecodedImage(t *testing.T) {
	t.Parallel()
	// Fetching or decoding art on the paint path stalls a frame. The snapshot
	// carries an identifier; the async worker produces the raster later. The
	// wallpaper picker already documents having paid for this mistake.
	b := newFakeBus("org.mpris.MediaPlayer2.x")
	b.props["org.mpris.MediaPlayer2.x"] = map[string]any{
		"Metadata": map[string]any{"mpris:artUrl": "file:///tmp/art.png"},
	}
	m := NewMedia(b)
	t.Cleanup(m.Close)
	if m.State().ArtKey == "" {
		t.Error("no art key recorded")
	}
}

func TestMediaSurvivesMalformedMetadata(t *testing.T) {
	t.Parallel()
	// A player is free to send nonsense. A partial map must degrade to a
	// usable snapshot, never fail the service.
	b := newFakeBus("org.mpris.MediaPlayer2.x")
	b.props["org.mpris.MediaPlayer2.x"] = map[string]any{
		"Metadata": map[string]any{
			"xesam:title":  42,
			"xesam:artist": "not a list",
			"mpris:length": "not a number",
		},
	}
	m := NewMedia(b)
	t.Cleanup(m.Close)
	st := m.State() // must not panic
	if st.LengthUS != 0 {
		t.Errorf("garbage length produced %d", st.LengthUS)
	}
}
```

- [ ] **Step 2: Run, implement, run**

Every field read is a checked type assertion with a zero-value fallback. `xesam:artist` is a list in the specification and a bare string in practice — accept each shape.

**Art URLs are attacker-adjacent** (design D11.2): a player names a path this shell then reads. Bound the size, apply a timeout, and in this first slice **refuse non-`file://` schemes** rather than fetching remote URLs.

Run: `go test ./internal/services -run TestMedia -v`
Expected: PASS.

- [ ] **Step 3: Commit**

```bash
git add internal/services/
git commit -m "feat(services): decode player metadata and defer art resolution"
```

---

### Task 5: Position interpolation

**Files:**
- Modify: `internal/services/media.go`
- Test: `internal/services/media_test.go`

- [ ] **Step 1: Write the failing tests**

```go
func TestMediaInterpolatesPositionWhilePlaying(t *testing.T) {
	t.Parallel()
	// MPRIS reports Position on request and emits Seeked only on
	// discontinuities. Interpolation lives here, not in each consumer:
	// Noctalia's service has eight, and eight timers would give eight
	// slightly different answers.
	m := newMediaAt(t, PlaybackPlaying, 1_000_000, 1.0)
	m.advance(2 * time.Second)
	if got := m.State().PositionUS; got < 2_900_000 || got > 3_100_000 {
		t.Errorf("position = %d, want about 3000000", got)
	}
}

func TestMediaPausedPositionDoesNotAdvance(t *testing.T) {
	t.Parallel()
	m := newMediaAt(t, PlaybackPaused, 1_000_000, 1.0)
	m.advance(5 * time.Second)
	if got := m.State().PositionUS; got != 1_000_000 {
		t.Errorf("paused position moved to %d", got)
	}
}

func TestMediaSeekedResetsTheBaseline(t *testing.T) {
	t.Parallel()
	m := newMediaAt(t, PlaybackPlaying, 1_000_000, 1.0)
	m.advance(2 * time.Second)
	m.onSeeked(10_000_000)
	if got := m.State().PositionUS; got < 9_900_000 || got > 10_100_000 {
		t.Errorf("position after seek = %d, want about 10000000", got)
	}
}

func TestMediaRunningSlowTrackRate(t *testing.T) {
	t.Parallel()
	m := newMediaAt(t, PlaybackPlaying, 0, 0.5)
	m.advance(4 * time.Second)
	if got := m.State().PositionUS; got < 1_900_000 || got > 2_100_000 {
		t.Errorf("position at half rate = %d, want about 2000000", got)
	}
}
```

`newMediaAt` builds a service with an injected clock; `advance` moves that clock. Inject the clock as a field defaulting to `time.Now` rather than sleeping in tests — a sleeping test is slow and flaky.

- [ ] **Step 2: Run, implement, run**

Position is computed from the last known value, the baseline timestamp and the rate. **No timer**: the service does not tick while nobody is watching. A consumer wanting a moving bar drives it from the existing per-surface animator, which already owns that surface's cadence and settles.

Run: `go test ./internal/services -run TestMedia -v`
Expected: PASS.

- [ ] **Step 3: Commit**

```bash
git add internal/services/
git commit -m "feat(services): interpolate playback position in the service"
```

---

### Task 6: Commands

**Files:**
- Modify: `internal/services/media.go`
- Test: `internal/services/media_test.go`

**Interfaces:**
- Produces: `PlayPause() error`, `Next() error`, `Previous() error`, `Stop() error`, `SetPosition(us int64) error`.

- [ ] **Step 1: Write the failing tests**

```go
func TestMediaCommandsTargetTheActivePlayer(t *testing.T) {
	t.Parallel()
	b := newFakeBus("org.mpris.MediaPlayer2.a", "org.mpris.MediaPlayer2.b")
	m := NewMedia(b)
	t.Cleanup(m.Close)
	m.Prefer("org.mpris.MediaPlayer2.b")
	if err := m.Next(); err != nil {
		t.Fatal(err)
	}
	if len(b.calls) != 1 || b.calls[0] != "org.mpris.MediaPlayer2.b.Next" {
		t.Errorf("calls = %v", b.calls)
	}
}

func TestMediaCommandWithNoPlayerIsQuiet(t *testing.T) {
	t.Parallel()
	// The user pressed next on something that stopped existing. The repair is
	// to update the display, not to raise a toast.
	m := NewMedia(newFakeBus())
	t.Cleanup(m.Close)
	if err := m.Next(); err != nil {
		t.Errorf("command with no player returned %v, want nil", err)
	}
}
```

- [ ] **Step 2: Run, implement, run**

Run: `go test ./internal/services -run TestMedia -v`
Expected: PASS.

- [ ] **Step 3: Commit**

```bash
git add internal/services/
git commit -m "feat(services): send transport commands to the active player"
```

---

### Task 7: Wire the service into the registry

**Files:**
- Modify: `internal/services/clock.go:39` (`Lease`)
- Modify: `internal/shell/registry.go:159,182`
- Test: `internal/shell/registry_test.go`

- [ ] **Step 1: Write the failing test**

```go
func TestRegistryOwnsTheMediaService(t *testing.T) {
	t.Parallel()
	reg := NewRegistry(config.Default())
	t.Cleanup(reg.Close)
	if reg.media == nil {
		t.Fatal("the registry did not construct a media service")
	}
}

func TestMediaServiceStopsWhenItsLastLeaseGoes(t *testing.T) {
	t.Parallel()
	// Consumer-counted lifetime, exactly as audio has. A service nobody is
	// watching must not keep a bus connection open.
	reg := NewRegistry(config.Default())
	t.Cleanup(reg.Close)
	l, err := reg.media.Acquire()
	if err != nil {
		t.Fatal(err)
	}
	l.Release()
	if reg.media.Available() && reg.media.running() {
		t.Error("the service kept running with no leases")
	}
}
```

- [ ] **Step 2: Run, implement, run**

Add a `media *Media` field to `Lease` beside the existing services, construct the service at `registry.go:159` beside `r.setAudio(...)`, and add `setMedia` mirroring `setAudio` at `:182`.

Do **not** add an OSD relay. Audio has one because volume changes deserve an OSD; a track change does not, and adding one now would be a feature this design did not approve.

Run: `go test ./internal/shell -run 'TestRegistry|TestMedia' -v`
Expected: PASS.

- [ ] **Step 3: Commit**

```bash
git add internal/services/clock.go internal/shell/registry.go internal/shell/registry_test.go
git commit -m "feat(shell): own the media service from the registry"
```

---

### Task 8: The `media` bar widget

**Files:**
- Create: `internal/shell/mediawidget.go`
- Modify: `internal/config/config.go:239` (`knownItems`)
- Modify: `internal/shell/widget.go`
- Test: `internal/shell/mediawidget_test.go`

- [ ] **Step 1: Write the failing tests**

```go
func TestMediaIsAKnownBarItem(t *testing.T) {
	t.Parallel()
	if _, ok := knownItems["media"]; !ok {
		t.Fatal(`"media" is not a known bar item`)
	}
}

func TestMediaWidgetCarriesNoPlayerPicker(t *testing.T) {
	t.Parallel()
	// The widget is not the page but smaller: one glyph, an optional title,
	// and gestures. A device or player list exists once, in the page. Without
	// this the widget grows a second picker.
	w := buildMediaWidget()
	var rows int
	walkNodes(w.node, func(n *ui.Node) {
		if n.Kind == ui.KindVirtualList || n.Kind == ui.KindScroll {
			rows++
		}
	})
	if rows != 0 {
		t.Errorf("the widget contains %d list nodes; it must carry no picker", rows)
	}
}

func TestMediaWidgetIsAbsentWithNoPlayer(t *testing.T) {
	t.Parallel()
	// A bar with nothing playing should not reserve a gap.
	w := buildMediaWidget()
	if !w.refresh(barView{}) && !w.node.Absent {
		t.Error("the widget did not mark itself absent with no player")
	}
}
```

- [ ] **Step 2: Run, implement, run**

Follow `buildVolumeWidget`'s shape at `widget.go`'s `case "volume":`. Scope per design D7: one glyph, optional scrolling title, left-click routes to the control-centre Media section, right or middle click toggles play, scroll moves next and previous. **No seek bar, no volume** — `volume` is already its own widget.

Run: `go test ./internal/shell -run TestMedia -v && go test ./internal/config -run TestKnownItems`
Expected: PASS.

- [ ] **Step 3: Commit**

```bash
git add internal/shell/mediawidget.go internal/shell/widget.go internal/config/config.go internal/shell/mediawidget_test.go
git commit -m "feat(shell): add the media bar widget"
```

---

### Task 9: The control-centre Media page

**Files:**
- Modify: the control-centre page dispatch in `internal/shell/`
- Test: `internal/shell/surfacerole_test.go`

**Depends on `sysc-253`**, the control-centre spine. Do not start until it has landed.

- [ ] **Step 1: Write the failing test**

```go
func TestControlCentreMediaPageIsNoLongerDisabled(t *testing.T) {
	t.Parallel()
	_, h := panelAtDensity(t, PanelControlCenter, theme.DensityStandard)
	// Navigate to the media section and assert the page builds real content
	// rather than the disabled placeholder.
}
```

- [ ] **Step 2: Build the page**

Now-playing header, transport controls, a position bar driven by the surface's existing animator, and the player list — the one place a picker belongs.

Album art as a full-bleed card background needs the surface-stacking slice. Until that lands, show art as an ordinary bounded image.

- [ ] **Step 3: Commit**

```bash
git add internal/shell/
git commit -m "feat(shell): replace the disabled media destination with a live page"
```

---

### Task 10: Re-slice the tracker

- [ ] **Step 1: Split `sysc-156`**

Per design D8 and the prior art's first finding, the service is a peer of `audio.go` and depends on nothing in the control centre; only the page does. Filing them as one issue makes `bd ready` lie about what can be picked up in parallel.

```bash
cd /home/nomadx/sysc-shell
bd create "Media service: MPRIS discovery, metadata, position, commands" --deps discovered-from:sysc-156
bd create "Media bar widget" --deps discovered-from:sysc-156
bd update sysc-156 --status open
bd export -o .beads/issues.jsonl
wc -l .beads/issues.jsonl
git diff --stat .beads/issues.jsonl
```

Leave `sysc-156` as the page issue, keeping its dependency on the spine. The two new issues do **not** depend on `sysc-253`.

- [ ] **Step 2: Commit**

```bash
git add .beads/issues.jsonl
git commit -m "chore: split the media work into service, widget and page"
```

---

## Self-Review

**Spec coverage.** D1 house service contract → Task 1. D2 hand-rolled on godbus, with the binding evaluation as its justification → Task 1 Steps 3 and 4. D3 discovery and active-player selection → Tasks 2 and 3. D4 metadata and art off the paint path → Task 4, including the security bound from D11.2. D5 position interpolation in the service → Task 5, with the eight-consumer argument quoted. D6 commands, quiet failure, re-selection → Task 6. D7 the `media` widget and its scope → Task 8, with a test that fails if a picker appears. D8 page as a separate issue → Tasks 9 and 10. D9 fake bus → Task 1's `fakeBus`. D10 tracker → Task 10. D11 risks: browser noise is mitigated by D3's blacklist (Task 3) and is explicitly unvalidatable without hardware; art URLs are bounded in Task 4; the godbus pin is checked against the cache in Global Constraints; the `TogglePanel` section parameter is left to the spine work, and Task 8's left-click routing is where it will be felt.

**Placeholders.** None in Tasks 1 to 8. Task 9 is deliberately lighter because it depends on `sysc-253`, which is still in flight — its detailed steps should be written once the spine's page seam exists, and the task says so rather than inventing a seam that may not match.

**Type consistency.** `bus` and `nameChange` are defined in Task 1 and used in Tasks 2, 3 and 6. `MediaState` fields set in Task 4 are read in Tasks 5 and 8. `Prefer(busName string)` is introduced in Task 3 and used in Task 6's test. `Acquire() (*Lease, error)` matches the existing services' signature, and `Lease` gains its `media` field in Task 7.
