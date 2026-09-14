package services

import "testing"

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
