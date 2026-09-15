package shell

import (
	"strings"
	"testing"
	"time"

	"github.com/Nomadcxx/sysc-shell/internal/platform/wayland"
	"github.com/Nomadcxx/sysc-shell/internal/services"
	"github.com/Nomadcxx/sysc-shell/internal/ui"
)

func mediaTestRegistry(t *testing.T, state services.MediaState, players []services.Player) (*Registry, *PanelHost) {
	t.Helper()
	media := services.NewUnavailableMedia()
	r := &Registry{
		closed:        make(chan struct{}),
		aux:           make(chan wayland.AuxRequest, 8),
		invalidations: make(chan wayland.Invalidation, 8),
		media:         media,
		mediaState:    state,
		mediaPlayers:  players,
		panelHosts:    make(map[PanelID]*PanelHost),
		notify:        newNotifyState(),
		now:           time.Now(),
	}
	h := &PanelHost{
		id: PanelControlCenter, section: "home", output: 1,
		theme: DefaultTheme(), stopAnim: make(chan struct{}),
	}
	r.panelHosts[h.id] = h
	t.Cleanup(func() {
		media.Close()
		close(r.closed)
	})
	return r, h
}

func mediaTreeHasText(n *ui.Node, want string) bool {
	if n == nil {
		return false
	}
	if n.Kind == ui.KindText && strings.Contains(n.Text, want) {
		return true
	}
	for _, child := range n.Children {
		if mediaTreeHasText(child, want) {
			return true
		}
	}
	return false
}

func TestControlCentreMediaPageIsNoLongerDisabled(t *testing.T) {
	section, ok := ccSectionFor("media")
	if !ok || !section.Enabled {
		t.Fatal("media remains disabled in the control-centre rail")
	}
	state := services.MediaState{
		Available: true, Player: "org.mpris.MediaPlayer2.player", Identity: "Player",
		Title: "Track", Artist: "Artist", Album: "Album", Status: services.PlaybackPlaying,
		CanPrev: true, CanPlay: true, CanNext: true, CanSeek: true, LengthUS: 180_000_000,
	}
	r, h := mediaTestRegistry(t, state, []services.Player{{Bus: state.Player, Identity: state.Identity, Active: true}})
	r.mu.Lock()
	h.section = "media"
	page := mediaBody(r, h)
	r.mu.Unlock()
	if page == nil || !mediaTreeHasText(page, "Track") || findAction(page, "media:playpause") == nil {
		t.Fatalf("media page does not contain the now-playing and transport controls: %#v", page)
	}
	if len(page.Children) != 3 {
		t.Fatalf("media page has %d cards, want now-playing, transport, and position/players", len(page.Children))
	}
	art := findNode(page, func(n *ui.Node) bool {
		return n.Width == mediaArtBox && n.Height == mediaArtBox && n.Shape == ui.ShapeMedium
	})
	if art == nil || art.Kind != ui.KindCapsule {
		t.Fatalf("missing-art node = %+v, want a painted placeholder capsule", art)
	}
}

func TestMediaPageFramesWhilePlayingOrSeeking(t *testing.T) {
	playing := &Registry{mediaState: services.MediaState{Status: services.PlaybackPlaying}}
	h := &PanelHost{id: PanelControlCenter, section: "media"}
	if !mediaPageFramesWantedLocked(playing, h) {
		t.Fatal("playing media page did not request frames")
	}
	playing.mediaState.Status = services.PlaybackPaused
	if mediaPageFramesWantedLocked(playing, h) {
		t.Fatal("paused media page requested frames without a seek")
	}
	pending := int64(2_000_000)
	h.mediaSeekPending = &pending
	if !mediaPageFramesWantedLocked(playing, h) {
		t.Fatal("media seek did not request frames")
	}
}

func TestMediaBodyLeaseTracksTheSection(t *testing.T) {
	r, h := mediaTestRegistry(t, services.MediaState{}, nil)
	r.mu.Lock()
	if !h.selectControlCentreSection(r, "media") {
		r.mu.Unlock()
		t.Fatal("media section was not selectable")
	}
	if h.mediaLease == nil || !r.media.Running() {
		r.mu.Unlock()
		t.Fatal("entering Media did not acquire its service lease")
	}
	if !h.selectControlCentreSection(r, "home") {
		r.mu.Unlock()
		t.Fatal("home section was not selectable")
	}
	if h.mediaLease != nil {
		r.mu.Unlock()
		t.Fatal("leaving Media retained its service lease")
	}
	r.mu.Unlock()
	deadline := time.Now().Add(time.Second)
	for r.media.Running() && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if r.media.Running() {
		t.Fatal("leaving Media retained its service lease")
	}
	r.mu.Lock()
	if !h.selectControlCentreSection(r, "media") {
		r.mu.Unlock()
		t.Fatal("media section could not be re-entered")
	}
	r.closeAllPanelsLocked()
	r.mu.Unlock()
	deadline = time.Now().Add(time.Second)
	for r.media.Running() && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if r.media.Running() {
		t.Fatal("closing the control centre retained the Media lease")
	}
}

func TestMediaBodyTransportWritesThroughTheControlSeam(t *testing.T) {
	r, h := mediaTestRegistry(t, services.MediaState{Available: true, CanPlay: true}, nil)
	r.mu.Lock()
	if !h.selectControlCentreSection(r, "media") {
		r.mu.Unlock()
		t.Fatal("media section was not selectable")
	}
	if !h.activateControlCentre(r, &ui.Node{Action: "media:playpause"}) {
		r.mu.Unlock()
		t.Fatal("media play/pause action was not handled")
	}
	r.mu.Unlock()
	select {
	case <-r.invalidations:
	case <-time.After(time.Second):
		t.Fatal("media action did not complete through scheduleControl")
	}
}

func TestMediaBodyPlayerClickPrefers(t *testing.T) {
	const bus = "org.mpris.MediaPlayer2.player"
	state := services.MediaState{Available: true, Player: bus, Identity: "Player"}
	r, h := mediaTestRegistry(t, state, []services.Player{{Bus: bus, Identity: "Player", Active: true}})
	r.mu.Lock()
	h.section = "media"
	page := ccPage(r, h)
	row := findAction(page, "media:player:"+bus)
	r.mu.Unlock()
	if row == nil || !row.State.Has(ui.StateSelected) {
		t.Fatalf("active player row = %#v, want selected", row)
	}
}

func TestMediaSeekWritesOnRelease(t *testing.T) {
	state := services.MediaState{Available: true, Player: "org.mpris.MediaPlayer2.player", CanSeek: true, LengthUS: 60_000_000, Title: "Track"}
	r, h := mediaTestRegistry(t, state, nil)
	r.mu.Lock()
	h.section = "media"
	if !h.activateControlCentre(r, &ui.Node{Action: "media:seek:2000000", Value: 2_000_000}) {
		r.mu.Unlock()
		t.Fatal("media seek action was not handled")
	}
	r.mu.Unlock()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		r.mu.Lock()
		pending := h.mediaSeekPending
		r.mu.Unlock()
		if pending == nil {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("media seek remained pending after the scheduled write")
}

func TestHomeShowsNowPlayingTile(t *testing.T) {
	state := services.MediaState{Available: true, Title: "Track", Status: services.PlaybackPlaying}
	r, h := mediaTestRegistry(t, state, nil)
	r.mu.Lock()
	page := ccHome(r, h)
	r.mediaState = services.MediaState{}
	empty := ccHome(r, h)
	r.mu.Unlock()
	tile := findByName(page, "Now playing")
	if tile == nil || !mediaTreeHasText(tile, "Track") || findNode(tile, func(n *ui.Node) bool { return n.Kind == ui.KindIcon && n.Icon == "pause" }) == nil {
		t.Fatalf("now-playing tile = %#v", tile)
	}
	emptyTile := findByName(empty, "Now playing")
	if emptyTile == nil || !emptyTile.State.Has(ui.StateDisabled) || !mediaTreeHasText(emptyTile, "Nothing playing") {
		t.Fatalf("empty now-playing tile = %#v", emptyTile)
	}
}

func TestPanelSectionMediaIPCUnblocked(t *testing.T) {
	if got, err := panelSection(PanelControlCenter, "media"); err != nil || got != "media" {
		t.Fatalf("panelSection media = %q, %v", got, err)
	}
}
