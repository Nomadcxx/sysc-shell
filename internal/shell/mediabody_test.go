package shell

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Nomadcxx/sysc-shell/internal/icons"
	"github.com/Nomadcxx/sysc-shell/internal/platform/wayland"
	"github.com/Nomadcxx/sysc-shell/internal/services"
	"github.com/Nomadcxx/sysc-shell/internal/ui"
	"github.com/Nomadcxx/sysc-shell/internal/wallpaper"
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

func TestMediaCardUsesTheOutputWallpaperThumbnailWithoutCoverArt(t *testing.T) {
	cache := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", cache)
	source := filepath.Join(t.TempDir(), "wall.png")
	data := testMediaArtPNG(t)
	if err := os.WriteFile(source, data, 0o600); err != nil {
		t.Fatal(err)
	}
	preview := wallpaper.CachedStillPath(source)
	if preview == "" {
		t.Fatal("wallpaper preview path was empty")
	}
	if err := os.MkdirAll(filepath.Dir(preview), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(preview, data, 0o600); err != nil {
		t.Fatal(err)
	}

	r, h := mediaTestRegistry(t, services.MediaState{Available: true, Title: "Track"}, nil)
	r.bars = map[uint32]*Bar{1: {conn: "DP-1"}}
	svc := wallpaper.NewService(wallpaper.ServiceConfig{
		Engine:     mediaWallpaperEngine{},
		Connectors: []string{"DP-1"},
	})
	t.Cleanup(svc.Close)
	r.wallpaperSvc = svc
	svc.Enqueue(wallpaper.Command{Op: wallpaper.OpApply, Token: "DP-1", Path: source, Kind: wallpaper.KindImage})
	deadline := time.After(time.Second)
	for {
		select {
		case snap := <-svc.Updates():
			if _, ok := snap.Assignments["DP-1"]; ok {
				goto assigned
			}
		case <-deadline:
			t.Fatal("wallpaper assignment did not arrive")
		}
	}

assigned:
	r.mu.Lock()
	h.section = "media"
	page := mediaBody(r, h)
	worker := r.wallpaperThumbs
	r.mu.Unlock()
	if worker == nil {
		t.Fatal("media page did not start the wallpaper thumbnail worker")
	}
	key := icons.Key{Name: preview, W: mediaArtBox, H: mediaArtBox}
	var art *ui.Image
	deadline = time.After(time.Second)
	for art == nil {
		if got, ok := worker.Lookup(key); ok {
			art = got
			break
		}
		select {
		case <-deadline:
			t.Fatal("wallpaper thumbnail did not decode")
		default:
			time.Sleep(time.Millisecond)
		}
	}
	if bg := findNode(page, func(n *ui.Node) bool { return n.Kind == ui.KindImage && n.Background }); bg == nil {
		t.Fatal("media card has no background image node")
	}

	r.mu.Lock()
	page = mediaBody(r, h)
	r.mu.Unlock()
	bg := findNode(page, func(n *ui.Node) bool { return n.Kind == ui.KindImage && n.Background })
	if bg == nil || bg.Image != art {
		t.Fatalf("wallpaper fallback image = %p, want decoded thumbnail %p", bgImage(bg), art)
	}
}

func TestWallpaperThumbnailRebuildsAndPublishesTheOpenMediaPage(t *testing.T) {
	r, h := mediaTestRegistry(t, services.MediaState{Available: true, Title: "Track"}, nil)
	r.mu.Lock()
	h.section = "media"
	h.root = &ui.Node{Kind: ui.KindText, Text: "old"}
	r.mu.Unlock()

	r.applyWallpaperThumb(icons.Key{Name: "preview"}, &ui.Image{Width: 1, Height: 1, Stride: 4, Pix: []byte{0, 0, 0, 0xff}})
	r.mu.Lock()
	root := h.root
	r.mu.Unlock()
	if root == nil || root.Text == "old" {
		t.Fatal("open Media page was not rebuilt when a thumbnail arrived")
	}
	select {
	case inv := <-r.invalidations:
		if inv.SurfaceID != panelSurfaceID(PanelControlCenter) {
			t.Fatalf("thumbnail invalidation = %+v, want the Media page", inv)
		}
	case <-time.After(time.Second):
		t.Fatal("thumbnail arrival did not publish a Media page invalidation")
	}
}

type mediaWallpaperEngine struct{}

func (mediaWallpaperEngine) Apply(wallpaper.Job, wallpaper.Settings) (string, error) {
	return "", nil
}
func (mediaWallpaperEngine) Restore(string, string) error { return nil }
func (mediaWallpaperEngine) SetPaused(string, bool) error { return nil }
func (mediaWallpaperEngine) Capabilities() wallpaper.Capabilities {
	return wallpaper.Capabilities{Statics: []string{"stub"}}
}

func bgImage(n *ui.Node) *ui.Image {
	if n == nil {
		return nil
	}
	return n.Image
}

func TestMediaTransportShowsSupportedRepeatAndShuffle(t *testing.T) {
	state := services.MediaState{
		Available: true, CanPrev: true, CanPlay: true, CanNext: true,
		CanLoop: true, LoopStatus: "Track", CanShuffle: true, Shuffle: true,
	}
	r, h := mediaTestRegistry(t, state, nil)
	r.mu.Lock()
	h.section = "media"
	transport := mediaBody(r, h).Children[1]
	r.mu.Unlock()
	loop := findAction(transport, "media:loop")
	shuffle := findAction(transport, "media:shuffle")
	if loop == nil || shuffle == nil {
		t.Fatalf("transport = %#v, want repeat and shuffle controls", transport)
	}
	if findNode(loop, func(n *ui.Node) bool { return n.Kind == ui.KindIcon && n.Icon == "repeat_one" }) == nil {
		t.Fatalf("loop control = %#v, want repeat_one for Track", loop)
	}
	if !shuffle.State.Has(ui.StateSelected) {
		t.Fatal("shuffle control did not reflect its active state")
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

func TestMediaControlReleaseSurvivesSnapshotTreeRebuild(t *testing.T) {
	r, h := mediaTestRegistry(t, services.MediaState{Available: true, CanPlay: true}, nil)
	h.section = "media"

	makeTree := func() (*ui.Node, *ui.Node) {
		button := &ui.Node{
			Kind: ui.KindButton, Action: "media:playpause", Name: "Play or pause",
			Role: "button", Focusable: true, Bounds: ui.Rect{X: 10, Y: 10, W: 80, H: 40},
		}
		return &ui.Node{Kind: ui.KindColumn, Bounds: ui.Rect{W: 120, H: 80}, Children: []*ui.Node{button}}, button
	}

	root, button := makeTree()
	h.root, h.focus, h.roving.Count = root, []*ui.Node{button}, 1
	h.roving.Set(0)
	handle := h.handle(r)
	if !handle(wayland.Event{Kind: wayland.EventPointerPress, X: 20, Y: 20}) {
		t.Fatal("media press was not handled")
	}

	// Media snapshots rebuild the retained tree while the pointer is down.
	// The replacement is an equivalent control at the same location, not the
	// same Go pointer.
	root, button = makeTree()
	h.root, h.focus, h.roving.Count = root, []*ui.Node{button}, 1
	h.roving.Set(0)
	if !handle(wayland.Event{Kind: wayland.EventPointerRelease, X: 20, Y: 20}) {
		t.Fatal("media release was not handled")
	}
	select {
	case <-r.invalidations:
	case <-time.After(time.Second):
		t.Fatal("release on a rebuilt media control did not dispatch the command")
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

func TestMediaPlayerRowsMarkOnlyTheActivePlayer(t *testing.T) {
	const activeBus = "org.mpris.MediaPlayer2.active"
	const idleBus = "org.mpris.MediaPlayer2.idle"
	state := services.MediaState{Available: true, Player: activeBus, Identity: "Active"}
	players := []services.Player{
		{Bus: activeBus, Identity: "Active", Active: true},
		{Bus: idleBus, Identity: "Idle"},
	}
	r, h := mediaTestRegistry(t, state, players)
	r.mu.Lock()
	h.section = "media"
	page := ccPage(r, h)
	r.mu.Unlock()
	for _, tc := range []struct {
		bus  string
		want bool
	}{
		{activeBus, true},
		{idleBus, false},
	} {
		row := findAction(page, "media:player:"+tc.bus)
		if row == nil {
			t.Fatalf("missing player row for %s", tc.bus)
		}
		got := findNode(row, func(n *ui.Node) bool { return n.Kind == ui.KindIcon && n.Icon == "check" }) != nil
		if got != tc.want {
			t.Errorf("player %s checkmark = %v, want %v", tc.bus, got, tc.want)
		}
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
