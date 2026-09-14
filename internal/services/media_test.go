package services

import (
	"sync"
	"testing"
	"time"
)

// fakeBus is the seam. A fake bus beats a fake session bus: it is
// deterministic, needs no running player, and cannot be affected by whatever
// happens to be playing on the developer's desktop.
type fakeBus struct {
	names  []string
	props  map[string]map[string]any
	calls  []string
	nameCh chan nameChange
}

func newFakeBus(names ...string) *fakeBus {
	return &fakeBus{names: names, props: map[string]map[string]any{}, nameCh: make(chan nameChange, 8)}
}

func (f *fakeBus) ListNames() ([]string, error)   { return f.names, nil }
func (f *fakeBus) NameChanges() <-chan nameChange { return f.nameCh }

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

// waitFor polls a predicate with a bounded deadline and fails the test when it
// does not hold in time. The service's watch loop is asynchronous, so tests
// that drive it through the fake's channel need this instead of a sleep.
func waitFor(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatal("condition did not hold in time")
}

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

// fakeClock is the injected clock. A sleeping test is slow and flaky; moving a
// clock is neither.
type fakeClock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *fakeClock) move(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
}

// mediaClocks tracks the injected clock of each service a test built through
// newMediaAt, so advance can be a method on Media as the plan writes it.
var (
	mediaClocksMu sync.Mutex
	mediaClocks   = map[*Media]*fakeClock{}
)

func newMediaAt(t *testing.T, status PlaybackStatus, positionUS int64, rate float64) *Media {
	t.Helper()
	m := NewMedia(newFakeBus("org.mpris.MediaPlayer2.x"))
	t.Cleanup(func() {
		mediaClocksMu.Lock()
		delete(mediaClocks, m)
		mediaClocksMu.Unlock()
		m.Close()
	})
	clock := &fakeClock{now: time.Unix(0, 0)}
	m.now = clock.Now
	mediaClocksMu.Lock()
	mediaClocks[m] = clock
	mediaClocksMu.Unlock()

	m.mu.Lock()
	p := &mediaPlayer{Player: Player{Bus: "org.mpris.MediaPlayer2.x"}}
	p.status = status
	p.rate = rate
	p.positionUS = positionUS
	p.positionAt = clock.Now()
	m.players[p.Bus] = p
	m.reselectLocked()
	m.mu.Unlock()
	return m
}

func (m *Media) advance(d time.Duration) {
	mediaClocksMu.Lock()
	clock := mediaClocks[m]
	mediaClocksMu.Unlock()
	clock.move(d)
}
