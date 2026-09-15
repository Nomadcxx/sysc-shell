package shell

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/Nomadcxx/sysc-shell/internal/icons"
	"github.com/Nomadcxx/sysc-shell/internal/services"
	"github.com/Nomadcxx/sysc-shell/internal/theme"
	"github.com/Nomadcxx/sysc-shell/internal/ui"
	"github.com/Nomadcxx/sysc-shell/internal/wallpaper"
)

const mediaSeekAction = "media:seek:"

// mediaBody is the one Media tree used by the control centre. It reads the
// Registry's retained snapshot and player list; all service writes stay in the
// activation seam below.
func mediaBody(r *Registry, h *PanelHost) *ui.Node {
	m := DefaultTheme().Metrics
	if h != nil {
		if hm := h.metrics(); hm.StandardControl > 0 {
			m = hm
		}
	}
	var state services.MediaState
	var players []services.Player
	if r != nil {
		state, players = mediaBodyStateLocked(r)
	}
	if h != nil {
		track := state.Player + "\x00" + state.Title
		if h.mediaSeekTrack != "" && h.mediaSeekTrack != track {
			h.mediaSeekPending = nil
		}
		h.mediaSeekTrack = track
	}

	if !state.Available {
		return &ui.Node{Kind: ui.KindColumn, Gap: theme.MarginL, Children: []*ui.Node{
			mediaNotice(m, "Nothing playing", "Start a player to control it here"),
		}}
	}

	position := state.PositionUS
	if h != nil && h.mediaSeekPending != nil {
		position = *h.mediaSeekPending
	}
	children := []*ui.Node{
		mediaNowPlaying(r, h, state, m),
		mediaTransport(state, m),
		mediaPositionPlayers(state, position, players, m),
	}
	return &ui.Node{Kind: ui.KindColumn, Gap: theme.MarginL, Children: children}
}

func mediaNotice(m theme.Metrics, title, detail string) *ui.Node {
	return monitorCard(m, []*ui.Node{
		{Kind: ui.KindIcon, Icon: "music_note", IconSize: m.IconLarge},
		{Kind: ui.KindText, Text: title, TextRole: theme.RoleTitle},
		{Kind: ui.KindText, Text: detail, TextRole: theme.RoleCaption},
	})
}

func mediaNowPlaying(r *Registry, h *PanelHost, state services.MediaState, m theme.Metrics) *ui.Node {
	var art *ui.Image
	if r != nil && state.ArtKey != "" {
		if name, ok := mediaArtRequestName(state.ArtKey); ok {
			worker := r.mediaArtFor()
			key := icons.Key{Name: name, W: mediaArtBox, H: mediaArtBox}
			if image, ok := worker.Lookup(key); ok {
				art = image
			} else {
				_, _ = worker.Request(state.ArtKey, mediaArtBox)
			}
		}
	}
	if art == nil && r != nil && h != nil {
		art = mediaWallpaperArtLocked(r, h)
	}
	background := &ui.Node{
		Kind: ui.KindImage, Width: mediaArtBox, Height: mediaArtBox,
		ImageSize: mediaArtBox, Image: art, Background: true, Shape: ui.ShapeCard,
	}
	foregroundChildren := []*ui.Node{
		{Kind: ui.KindColumn, Gap: theme.MarginXXS, Children: []*ui.Node{
			{Kind: ui.KindText, Text: ccText(state.Title), TextRole: theme.RoleTitle},
			{Kind: ui.KindText, Text: ccText(state.Artist), TextRole: theme.RoleCaption},
			{Kind: ui.KindText, Text: ccText(state.Album), TextRole: theme.RoleCaption},
			{Kind: ui.KindText, Text: ccText(state.Identity), TextRole: theme.RoleLabel},
		}},
	}
	if art == nil {
		foregroundChildren = append([]*ui.Node{{
			Kind: ui.KindCapsule, Width: mediaArtBox, Height: mediaArtBox,
			Fill: ui.FillContainerHighest, Shape: ui.ShapeMedium,
			Children: []*ui.Node{{Kind: ui.KindIcon, Icon: "music_note", IconSize: m.IconNormal}},
		}}, foregroundChildren...)
	}
	return &ui.Node{
		// The card chrome stays outside the stack; its padding moves into the
		// foreground layer so the background can fill the card edge to edge.
		Kind: ui.KindCapsule, Fill: ui.FillContainerHigh, Shape: ui.ShapeCard,
		Children: []*ui.Node{{Kind: ui.KindStack, Children: []*ui.Node{
			background,
			{Kind: ui.KindCapsule, Fill: ui.FillScrim, Shape: ui.ShapeCard},
			{Kind: ui.KindColumn, Padding: m.CardPadding, Opacity: 80, Children: []*ui.Node{{
				Kind: ui.KindRow, Gap: theme.MarginL, Children: foregroundChildren,
			}}},
		}}},
	}
}

// mediaWallpaperArtLocked supplies the cached still assigned to the bar's
// output when the player has no cover art. Registry.mu is held by the media
// page builder; decoding remains on the thumbnail worker.
func mediaWallpaperArtLocked(r *Registry, h *PanelHost) *ui.Image {
	bar := r.bars[h.output]
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
	key := icons.Key{Name: source, W: mediaArtBox, H: mediaArtBox}
	worker := r.wallpaperThumbsLocked()
	if image, ok := worker.Lookup(key); ok {
		return image
	}
	_, _, _ = worker.Request(key)
	return nil
}

func mediaTransport(state services.MediaState, m theme.Metrics) *ui.Node {
	playIcon := "play_arrow"
	if state.Status == services.PlaybackPlaying {
		playIcon = "pause"
	}
	buttons := []*ui.Node{
		mediaButton("skip_previous", "media:prev", "Previous", state.CanPrev),
		mediaButton(playIcon, "media:playpause", "Play or pause", state.CanPlay || state.CanPause),
		mediaButton("skip_next", "media:next", "Next", state.CanNext),
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
	return monitorCard(m, []*ui.Node{
		monitorCardTitle("Transport", 0),
		{Kind: ui.KindRow, Gap: theme.MarginM, Children: buttons},
	})
}

func mediaButton(icon, action, name string, enabled bool) *ui.Node {
	n := centreIconButton(icon, action, name)
	if !enabled {
		ccDisable(n)
	}
	return n
}

func mediaPositionPlayers(state services.MediaState, position int64, players []services.Player, m theme.Metrics) *ui.Node {
	var control *ui.Node
	if state.CanSeek && state.LengthUS > 0 {
		control = &ui.Node{
			Kind: ui.KindSlider, Action: mediaSeekAction + strconv.FormatInt(position, 10),
			Name: "Position", Role: "slider", Focusable: true, Width: ccSliderW,
			Value: float64(position), Min: 0, Max: float64(state.LengthUS), Step: 1_000_000,
		}
	} else {
		value := 0.0
		if state.LengthUS > 0 {
			value = float64(position) / float64(state.LengthUS)
		}
		control = &ui.Node{Kind: ui.KindMeter, Width: ccSliderW, Value: value, Absent: state.LengthUS <= 0}
	}
	rows := []*ui.Node{
		monitorCardTitle("Position", 0),
		{Kind: ui.KindRow, Gap: theme.MarginM, Children: []*ui.Node{
			control,
			{Kind: ui.KindText, Text: mediaTime(position) + " / " + mediaTime(state.LengthUS),
				MinWidthText: "00:00 / 00:00", Tabular: true},
		}},
		monitorCardTitle("Players", 0),
	}
	rows = append(rows, mediaPlayerRows(state, players, m)...)
	return monitorCard(m, rows)
}

func mediaPlayerRows(state services.MediaState, players []services.Player, m theme.Metrics) []*ui.Node {
	rows := make([]*ui.Node, 0, len(players))
	for _, player := range players {
		name := strings.TrimSpace(player.Identity)
		if name == "" {
			name = player.Bus
		}
		selected := player.Active || player.Bus == state.Player
		rowChildren := []*ui.Node{{Kind: ui.KindText, Text: name}}
		if selected {
			rowChildren = append(rowChildren, &ui.Node{Kind: ui.KindIcon, Icon: "check", IconSize: m.IconNormal})
		}
		row := &ui.Node{
			Kind: ui.KindButton, Action: "media:player:" + player.Bus, Name: name,
			Role: "button", Focusable: true, Height: m.StandardControl,
			Shape: ui.ShapeSmall, PinEnd: true,
			Children: []*ui.Node{{Kind: ui.KindRow, Gap: theme.MarginM, PinEnd: true,
				Children: rowChildren}},
		}
		if selected {
			row.State |= ui.StateSelected
		}
		rows = append(rows, row)
	}
	if len(rows) == 0 {
		rows = append(rows, &ui.Node{Kind: ui.KindText, Text: "No players found", TextRole: theme.RoleCaption})
	}
	return rows
}

func mediaTime(us int64) string {
	if us < 0 {
		us = 0
	}
	if us == 0 {
		return "00:00"
	}
	seconds := us / 1_000_000
	return fmt.Sprintf("%02d:%02d", seconds/60, seconds%60)
}

func mediaGlyph(status services.PlaybackStatus) string {
	switch status {
	case services.PlaybackPlaying:
		return "pause"
	case services.PlaybackPaused:
		return "play_arrow"
	default:
		return "music_note"
	}
}

func mediaBodyVisible(h *PanelHost) bool {
	return h != nil && h.id == PanelControlCenter && h.section == "media"
}

// mediaBodyStateLocked keeps the retained snapshot authoritative for the page
// while taking the interpolated position from the service's memory-only cache.
// The relay remains the sole bridge for metadata and player-set changes.
func mediaBodyStateLocked(r *Registry) (services.MediaState, []services.Player) {
	if r == nil {
		return services.MediaState{}, nil
	}
	state, players := r.mediaState, r.mediaPlayers
	if r.media != nil {
		state.PositionUS = r.media.CachedState().PositionUS
	}
	return state, players
}

func mediaPageFramesWantedLocked(r *Registry, h *PanelHost) bool {
	if r == nil || !mediaBodyVisible(h) {
		return false
	}
	return r.mediaState.Status == services.PlaybackPlaying || h.mediaSeekPending != nil
}

// startMediaBodyLocked gives the page its own service lease. The bar has a
// separate lease, so opening the page never changes the bar's ownership.
func (r *Registry) startMediaBodyLocked(h *PanelHost) {
	if r == nil || !mediaBodyVisible(h) || h.mediaLease != nil || r.media == nil {
		return
	}
	lease, err := r.media.Acquire()
	if err != nil {
		h.errLabel = err.Error()
		return
	}
	h.mediaLease = lease
}

func (r *Registry) leaveMediaBodyLocked(h *PanelHost) {
	if h == nil || h.mediaLease == nil {
		return
	}
	lease := h.mediaLease
	h.mediaLease = nil
	// ponytail: releasing a media watch waits for an in-flight bus enumeration;
	// keep that bounded wait off the Wayland owner and let a new lease keep the
	// service alive if the page is entered again before it finishes.
	go lease.Release()
}

func (h *PanelHost) activateMedia(r *Registry, n *ui.Node) bool {
	if h == nil || r == nil || n == nil || h.id != PanelControlCenter || h.section != "media" {
		return false
	}
	media := r.media
	if media == nil {
		return false
	}
	switch {
	case n.Action == "media:playpause":
		r.scheduleControl(h, media.PlayPause)
	case n.Action == "media:next":
		r.scheduleControl(h, media.Next)
	case n.Action == "media:prev":
		r.scheduleControl(h, media.Previous)
	case n.Action == "media:loop":
		r.scheduleControl(h, media.ToggleLoop)
	case n.Action == "media:shuffle":
		r.scheduleControl(h, media.ToggleShuffle)
	case strings.HasPrefix(n.Action, mediaSeekAction):
		value := int64(n.Value)
		if rest := strings.TrimPrefix(n.Action, mediaSeekAction); rest != "" {
			if parsed, err := strconv.ParseInt(rest, 10, 64); err == nil && n.Kind != ui.KindSlider {
				value = parsed
			}
		}
		h.mediaSeekPending = &value
		r.rebuildPanel(h)
		r.startSurfaceFrames(h)
		r.scheduleControl(h, func() error {
			err := media.SetPosition(value)
			r.mu.Lock()
			if r.panelHosts[h.id] == h && h.mediaSeekPending != nil && *h.mediaSeekPending == value {
				h.mediaSeekPending = nil
			}
			r.mu.Unlock()
			return err
		})
	case strings.HasPrefix(n.Action, "media:player:"):
		bus := strings.TrimPrefix(n.Action, "media:player:")
		if bus == "" {
			return false
		}
		media.Prefer(bus)
	default:
		return false
	}
	return true
}
