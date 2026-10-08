package shell

import (
	"os"
	"strconv"
	"strings"

	"github.com/Nomadcxx/sysc-shell/internal/icons"
	"github.com/Nomadcxx/sysc-shell/internal/services"
	"github.com/Nomadcxx/sysc-shell/internal/ui"
	"github.com/Nomadcxx/sysc-shell/internal/wallpaper"
)

// mediaControl maps one media action to the service write it makes. It is the
// single action vocabulary the Media page, the Home card and the bar strip
// share, so the three cannot drift. seekTo is set for a seek, so the caller
// can hold the position as pending until the write lands. Bus I/O happens in
// run, which callers schedule off the Wayland owner.
func mediaControl(media *services.Media, n *ui.Node) (run func() error, seekTo *int64, ok bool) {
	if media == nil || n == nil {
		return nil, nil, false
	}
	switch {
	case n.Action == "media:playpause":
		return media.PlayPause, nil, true
	case n.Action == "media:next":
		return media.Next, nil, true
	case n.Action == "media:prev":
		return media.Previous, nil, true
	case n.Action == "media:loop":
		return media.ToggleLoop, nil, true
	case n.Action == "media:shuffle":
		return media.ToggleShuffle, nil, true
	case strings.HasPrefix(n.Action, mediaSeekAction):
		value := int64(n.Value)
		if rest := strings.TrimPrefix(n.Action, mediaSeekAction); rest != "" {
			if parsed, err := strconv.ParseInt(rest, 10, 64); err == nil && n.Kind != ui.KindSlider {
				value = parsed
			}
		}
		return func() error { return media.SetPosition(value) }, &value, true
	case strings.HasPrefix(n.Action, "media:player:"):
		bus := strings.TrimPrefix(n.Action, "media:player:")
		if bus == "" {
			return nil, nil, false
		}
		return func() error { media.Prefer(bus); return nil }, nil, true
	}
	return nil, nil, false
}

// mediaPlayDisabled reports a player that can neither start nor pause, such
// as a stopped stream: its play button must not look live.
func mediaPlayDisabled(state services.MediaState) bool {
	return !state.CanPlay && !state.CanPause
}

// mediaTransportButtons is the transport row every media surface draws:
// previous, play/pause and next, then repeat and shuffle when modes is set
// and the player supports them. Unsupported controls are disabled, not
// hidden, so the row keeps its shape across players.
func mediaTransportButtons(state services.MediaState, modes bool) []*ui.Node {
	playIcon := "play_arrow"
	if state.Status == services.PlaybackPlaying {
		playIcon = "pause"
	}
	buttons := []*ui.Node{
		mediaButton("skip_previous", "media:prev", "Previous", state.CanPrev),
		mediaButton(playIcon, "media:playpause", "Play or pause", !mediaPlayDisabled(state)),
		mediaButton("skip_next", "media:next", "Next", state.CanNext),
	}
	if !modes {
		return buttons
	}
	if state.CanLoop {
		loopIcon := "repeat"
		if state.LoopStatus == "Track" {
			loopIcon = "repeat_one"
		}
		loop := mediaButton(loopIcon, "media:loop", "Repeat", true)
		if state.LoopStatus != "None" {
			loop.State |= ui.StateSelected
			loop.Fill = ui.FillAccent
		}
		buttons = append(buttons, loop)
	}
	if state.CanShuffle {
		shuffle := mediaButton("shuffle", "media:shuffle", "Shuffle", true)
		if state.Shuffle {
			shuffle.State |= ui.StateSelected
			shuffle.Fill = ui.FillAccent
		}
		buttons = append(buttons, shuffle)
	}
	return buttons
}

// mediaArtImageLocked is the cover art at box, or the cached still of the
// wallpaper on output when the player has none. Decoding stays on the
// workers; a miss requests the image and returns nil until it lands.
// Registry.mu is held.
func mediaArtImageLocked(r *Registry, output uint32, artKey string, box int) *ui.Image {
	if r == nil {
		return nil
	}
	if artKey != "" {
		if name, ok := mediaArtRequestName(artKey); ok {
			worker := r.mediaArtFor()
			if image, ok := worker.Lookup(icons.Key{Name: name, W: box, H: box}); ok {
				return image
			}
			_, _ = worker.Request(artKey, box)
		}
	}
	return mediaWallpaperArtLocked(r, output, box)
}

// mediaWallpaperArtLocked supplies the cached still assigned to output when
// the player has no cover art.
func mediaWallpaperArtLocked(r *Registry, output uint32, box int) *ui.Image {
	bar := r.bars[output]
	if bar == nil || r.wallpaperSvc == nil {
		return nil
	}
	assignment, ok := r.wallpaperSvc.Snapshot().Assignments[bar.connector()]
	if !ok || assignment.Path == "" {
		return nil
	}
	source := wallpaper.CachedStillPath(assignment.Path)
	if source == "" {
		return nil
	}
	if _, err := os.Stat(source); err != nil {
		return nil
	}
	key := icons.Key{Name: source, W: box, H: box}
	worker := r.wallpaperThumbsLocked()
	if image, ok := worker.Lookup(key); ok {
		return image
	}
	_, _, _ = worker.Request(key)
	return nil
}
