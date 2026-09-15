package shell

import (
	"slices"
	"testing"

	"github.com/Nomadcxx/sysc-shell/internal/config"
	"github.com/Nomadcxx/sysc-shell/internal/icons"
	"github.com/Nomadcxx/sysc-shell/internal/services"
	"github.com/Nomadcxx/sysc-shell/internal/ui"
)

func TestMediaIsAKnownBarItem(t *testing.T) {
	t.Parallel()
	if !slices.Contains(config.KnownItemIDs(), "media") {
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
	// A bar with nothing playing should not reserve a gap. Test the wrapped
	// widget, because the capsule is what the bar lays out and paints.
	metrics := DefaultTheme().Metrics
	w := buildWidgets([]config.Item{{ID: "media"}}, metrics.CapsulePadding, metrics)[0]
	if !w.refresh(barView{}) {
		t.Fatal("the widget refresh did not report its absence")
	}
	if !w.node.Absent {
		t.Error("the media capsule remained visible with no player")
	}
}

func TestMediaWidgetSwapsGlyphWithStatus(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		status services.PlaybackStatus
		icon   string
	}{
		{services.PlaybackPlaying, "pause"},
		{services.PlaybackPaused, "play_arrow"},
		{services.PlaybackStopped, "music_note"},
	} {
		row := buildMediaWidget().node
		refreshMediaWidget(row, barView{Media: services.MediaState{Available: true, Status: tc.status}})
		if got := row.Children[0].Icon; got != tc.icon {
			t.Errorf("status %d icon = %q, want %q", tc.status, got, tc.icon)
		}
	}
}

func TestMediaWidgetCarriesResolvedArtAndFallsBack(t *testing.T) {
	image := &ui.Image{Width: 16, Height: 16, Stride: 64, Pix: make([]byte, 16*64)}
	row := buildMediaWidget().node
	state := services.MediaState{Available: true, Status: services.PlaybackPlaying, ArtKey: "https://example.test/cover.png"}
	refreshMediaWidget(row, barView{Media: state, MediaArt: image})
	if art := row.Children[0]; art.Kind != ui.KindImage || art.Image != image {
		t.Fatalf("media leading node = %+v, want the resolved cover art", art)
	}

	refreshMediaWidget(row, barView{Media: state})
	if fallback := row.Children[0]; fallback.Kind != ui.KindIcon || fallback.Icon != "pause" {
		t.Fatalf("media fallback node = %+v, want the playing glyph", fallback)
	}
}

func TestRegistrySharesCachedMediaArtWithTheBar(t *testing.T) {
	const artURL = "https://example.test/cover.png"
	image := &ui.Image{Width: mediaArtBox, Height: mediaArtBox, Stride: mediaArtBox * 4,
		Pix: make([]byte, mediaArtBox*mediaArtBox*4)}
	name, ok := mediaArtRequestName(artURL)
	if !ok {
		t.Fatal("test art URL was rejected")
	}
	worker := newMediaArtWorker(nil)
	worker.cache[icons.Key{Name: name, W: mediaArtBox, H: mediaArtBox}] = image

	media := services.NewUnavailableMedia()
	metrics := services.NewMetrics()
	r := &Registry{
		outputs:  make(map[string]outputState),
		metrics:  metrics,
		notify:   newNotifyState(),
		media:    media,
		mediaArt: worker,
		mediaState: services.MediaState{
			Available: true, ArtKey: artURL, Title: "Track",
		},
	}
	t.Cleanup(func() {
		worker.Close()
		media.Close()
		metrics.Close()
	})

	widget := buildMediaWidget()
	bar := &Bar{right: []textWidget{widget}}
	r.bars = map[uint32]*Bar{1: bar}
	if !bar.apply(r.viewLocked("")) {
		t.Fatal("cached art did not update the bar widget")
	}
	if got := widget.node.Children[0]; got.Kind != ui.KindImage || got.Image != image {
		t.Fatalf("bar art = %+v, want the shared cached raster", got)
	}
}

func TestMediaWidgetSetsMarqueeConfig(t *testing.T) {
	t.Parallel()
	widgets := buildWidgets([]config.Item{{ID: "media", MaxWidth: 120}}, 8, standardMetrics())
	if len(widgets) != 1 || widgets[0].inner == nil || len(widgets[0].inner.Children) != 2 {
		t.Fatalf("media widget = %+v", widgets)
	}
	title := widgets[0].inner.Children[1]
	if !title.Marquee || title.MaxWidth != 120 || title.Key != "media-title" {
		t.Fatalf("media title = %+v, want marquee, max width 120, stable key", title)
	}
}
