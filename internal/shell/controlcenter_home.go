package shell

import (
	"fmt"
	"strings"
	"time"

	"github.com/Nomadcxx/sysc-shell/internal/services"
	"github.com/Nomadcxx/sysc-shell/internal/theme"
	"github.com/Nomadcxx/sysc-shell/internal/ui"
)

// The Home page is one row table that fits its viewport without scrolling
// (B1, 2026-10-08). Every width is derived from the body width and the gap;
// TestControlCentreHomeFitsTheViewportWithoutScrolling holds the sum.
const (
	ccHomeGap      = theme.MarginM
	ccHomeTopH     = 104
	ccHomeToggleH  = 44
	ccHomeSplitH   = 196
	ccHomeWeatherH = 98
	ccHomeSliderH  = 40
	// ccHomeLeftW keeps the clock and system cards at the width the system
	// gauges were sized for.
	ccHomeLeftW = 355
)

// ccHomeHeight is the page height with the given number of slider rows.
func ccHomeHeight(sliderRows int) int {
	return ccHomeTopH + ccHomeToggleH + ccHomeSplitH + sliderRows*ccHomeSliderH + (2+sliderRows)*ccHomeGap
}

// ccHomeRightW is the right column: whatever the body leaves after the left
// column and the gap, so the column always ends on the body edge.
func ccHomeRightW(body int) int {
	return max(body-ccHomeLeftW-ccHomeGap, 0)
}

func ccHome(r *Registry, h *PanelHost) *ui.Node {
	m := h.metrics()
	body := ccBodyWidth(h)
	identity := ccIdentity{}
	snap := services.Snapshot{}
	now := time.Time{}
	reading := services.Reading{}
	caffeine, dnd := false, false
	var audio services.AudioState
	var brightness services.BrightnessState
	var media services.MediaState
	var displays []services.DisplayInfo
	var wallsStatus string
	audioOK, brightnessOK := false, false
	if r != nil {
		identity = r.controlIdentity
		snap, now, reading = r.sample, r.now, r.reading
		caffeine = r.inhibitWanted
		wallsStatus = wallsServiceStatus(r.wallsSnapshot)
		if r.notify != nil {
			_, dnd = r.notify.dndState(now)
		}
		if r.audio != nil {
			audio, audioOK = r.audio.CachedState()
		}
		if r.brightness != nil {
			brightness, brightnessOK = r.brightness.CachedState()
			displays = r.brightness.CachedDisplays()
		}
		media = r.mediaState
	}

	half := (body - ccHomeGap) / 2
	top := &ui.Node{Kind: ui.KindRow, Height: ccHomeTopH, Gap: ccHomeGap, Children: []*ui.Node{
		ccHomeIdentity(r, h, identity, half),
		ccHomeMediaSlot(media, body-half-ccHomeGap),
	}}
	toggles := ccHomeToggles(r, body, caffeine, wallsStatus)
	split := &ui.Node{Kind: ui.KindRow, Height: ccHomeSplitH, Gap: ccHomeGap, Children: []*ui.Node{
		{Kind: ui.KindColumn, Width: ccHomeLeftW, Height: ccHomeSplitH, Gap: ccHomeGap, Children: []*ui.Node{
			ccHomeWeather(m, now, reading),
			ccHomeSystem(m, snap),
		}},
		ccHomeTiles(h, snap, audio, audioOK, dnd, ccHomeRightW(body)),
	}}
	sliders := ccHomeSliders(m, audio, audioOK, brightness, brightnessOK, displays, body)
	children := append([]*ui.Node{top, toggles, split}, sliders...)
	return &ui.Node{Kind: ui.KindColumn, Height: ccHomeHeight(len(sliders)), Gap: ccHomeGap, Children: children}
}

func ccHomeIdentity(r *Registry, h *PanelHost, identity ccIdentity, width int) *ui.Node {
	m := h.metrics()
	rows := []*ui.Node{
		{Kind: ui.KindText, Text: ccText(identity.Name), TextRole: theme.RoleTitle},
		{Kind: ui.KindText, Text: ccText(identity.Account), TextRole: theme.RoleCaption},
		{Kind: ui.KindText, Text: "Uptime " + ccText(identity.Uptime), TextRole: theme.RoleCaption},
	}
	if h != nil && h.errLabel != "" {
		rows = append([]*ui.Node{{Kind: ui.KindText, Text: h.errLabel, Tone: ui.ToneError}}, rows...)
	}
	card := monitorCard(m, []*ui.Node{{Kind: ui.KindRow, Gap: theme.MarginL, CenterY: true, Children: []*ui.Node{
		ccAvatar(r, h, identity),
		{Kind: ui.KindColumn, Gap: theme.MarginXS, Children: rows},
	}}})
	card.Width, card.Height = width, ccHomeTopH
	return card
}

// ccHomeMediaSlot is the top row's media half. Task 5 replaces the tile with
// the media card.
func ccHomeMediaSlot(media services.MediaState, width int) *ui.Node {
	title := "Nothing playing"
	if media.Available {
		title = ccText(media.Title)
	}
	tile := ccQuickTile(width, mediaGlyph(media.Status), "Now playing", title, "section:media", false)
	tile.Name, tile.Height = "Now playing", ccHomeTopH
	tile.Children[0].Children[2].MaxWidth = width - 2*theme.MarginL // the title ellipsises in the tile
	if !media.Available {
		ccDisable(tile)
	}
	return tile
}

func ccHomeToggles(r *Registry, body int, caffeine bool, wallsStatus string) *ui.Node {
	w := (body - 3*ccHomeGap) / 4
	caffeineButton := ccHomePill(w, "coffee", "Caffeine", "cc:caffeine", caffeine)
	var holding []string
	if r != nil {
		for _, inh := range r.externalInhibitors {
			if name := ccText(inh.App); name != ccDash {
				holding = append(holding, name)
			}
		}
	}
	if len(holding) > 0 {
		caffeineButton.Tooltip = "Idle held: " + strings.Join(holding, ", ")
	}
	screensaver := ccHomePill(w, "schedule", "Screensaver", "settings-section:Screensaver", false)
	screensaver.Tooltip = "Screensaver: " + ccText(wallsStatus)
	if caffeine && r != nil && r.wallsSnapshot.Running() {
		screensaver.Tooltip += ". Caffeine does not pause the screensaver."
	}
	return &ui.Node{Kind: ui.KindRow, Height: ccHomeToggleH, Gap: ccHomeGap, Children: []*ui.Node{
		caffeineButton,
		ccHomePill(w, "wallpaper", "Wallpaper", "cc:wallpaper", false),
		ccHomePill(w, "terminal", "Terminal Art", "cc:terminal-art", false),
		screensaver,
	}}
}

func ccHomePill(width int, icon, label, action string, selected bool) *ui.Node {
	n := ccQuickAccessButton(width, icon, label, action, selected)
	n.Height = ccHomeToggleH
	return n
}

func ccHomeWeather(m theme.Metrics, now time.Time, reading services.Reading) *ui.Node {
	summary, tone := ccWeatherSummary(reading)
	card := monitorCard(m, []*ui.Node{
		monitorCardTitle(ccClock(now), 0),
		{Kind: ui.KindText, Text: ccDate(now), TextRole: theme.RoleCaption},
		{Kind: ui.KindText, Text: summary, Tone: tone},
	})
	card.Height = ccHomeWeatherH
	// The clock and date own the leading edge, so the scene sits against the
	// trailing one and the scrim fades from the text side.
	return weatherCardWithEffect(card, reading, weatherHomeEffectKey, 1)
}

func ccHomeSystem(m theme.Metrics, snap services.Snapshot) *ui.Node {
	gpuSelector, gpuOK := selectGPU(snap)
	slotWidth := (ccHomeLeftW - 2*m.CardPadding - 3*theme.MarginM) / 4
	row := &ui.Node{Kind: ui.KindRow, Gap: theme.MarginM, CenterY: true, Children: []*ui.Node{
		ccResourceGroup(snap, "cpu", "CPU", services.Selector{Source: services.SourceCPU}, true, slotWidth),
		ccResourceGroup(snap, "memory", "Memory", services.Selector{Source: services.SourceMemory}, true, slotWidth),
		ccResourceGroup(snap, "temperature", "Temp", services.Selector{Source: services.SourceCPU, Subject: "temperature"}, true, slotWidth),
		ccResourceGroup(snap, "gpu", "GPU", gpuSelector, gpuOK, slotWidth),
	}}
	card := monitorCard(m, []*ui.Node{row})
	card.Name, card.Role = "System", "group"
	card.Height = ccHomeSplitH - ccHomeWeatherH - ccHomeGap
	return card
}

func ccHomeTiles(h *PanelHost, snap services.Snapshot, audio services.AudioState, audioOK, dnd bool, width int) *ui.Node {
	tileW := (width - ccHomeGap) / 2
	tileH := (ccHomeSplitH - ccHomeGap) / 2
	battery := ccDash
	if b := snap.Battery; b != nil && b.Present && b.ChargeValid {
		battery = fmt.Sprintf("%.0f%%", b.Charge*100)
	}
	profile := ccDash
	if h != nil && h.profileActive != "" {
		profile = powerProfileLabel(h.profileActive)
	}
	mute := ccQuickTile(tileW, "volume_off", "Mute", ccPercent(audio.Level, audioOK), "cc:mute", audio.Muted)
	if !audioOK {
		ccDisable(mute)
	}
	dndTile := ccQuickTile(tileW, "do_not_disturb_on", "DND", ccOnOff(dnd), "cc:dnd", dnd)
	dndTile.Name = "Do not disturb"
	nextProfile := hProfileNext(h)
	profileTile := ccQuickTile(tileW, "balance", "Profile", profile, "cc:profile:"+nextProfile, false)
	profileTile.Name = "Power profile"
	if nextProfile == "" {
		ccDisable(profileTile)
	}
	// Battery is a readout in the same filled tile as its neighbours: one
	// tile style for every state on the page.
	batteryTile := ccQuickTile(tileW, "battery_full", "Battery", battery, "", false)
	batteryTile.Role, batteryTile.Focusable = "group", false
	for _, t := range []*ui.Node{mute, dndTile, profileTile, batteryTile} {
		t.Height = tileH
	}
	return &ui.Node{Kind: ui.KindColumn, Width: width, Height: ccHomeSplitH, Gap: ccHomeGap, Children: []*ui.Node{
		{Kind: ui.KindRow, Height: tileH, Gap: ccHomeGap, Children: []*ui.Node{mute, dndTile}},
		{Kind: ui.KindRow, Height: tileH, Gap: ccHomeGap, Children: []*ui.Node{profileTile, batteryTile}},
	}}
}

// ccHomeSliders is the volume row and the brightness rows. Task 6 replaces
// the brightness half with ccBrightnessRows.
func ccHomeSliders(m theme.Metrics, audio services.AudioState, audioOK bool, brightness services.BrightnessState, brightnessOK bool, displays []services.DisplayInfo, body int) []*ui.Node {
	volume := ccHomeSlider(m, "volume_up", "Volume", "cc:volume", audio.Level, audioOK, body)
	return []*ui.Node{volume, ccHomeSlider(m, "brightness_high", "Brightness", "cc:brightness", brightness.Level, brightnessOK, body)}
}

// ccHomeSliderLabelW is the slider label column. A fixed column that
// ellipsises keeps the control width independent of font scale.
const ccHomeSliderLabelW = 96

// ccHomeSlider is ccSlider at the Home row height, with the control sized to
// what its row leaves: icon, label column, value and three gaps.
func ccHomeSlider(m theme.Metrics, icon, label, action string, value int, ok bool, width int) *ui.Node {
	n := ccSlider(m, icon, label, action, value, ok)
	n.Height, n.Width = ccHomeSliderH, width
	row := n.Children[0]
	row.Children[1].Width, row.Children[1].MaxWidth = ccHomeSliderLabelW, ccHomeSliderLabelW
	row.Children[2].Width = max(width-2*m.CardPadding-m.IconNormal-ccHomeSliderLabelW-ccValueW-3*theme.MarginM, 48)
	return n
}
