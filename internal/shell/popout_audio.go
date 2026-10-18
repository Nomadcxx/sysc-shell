package shell

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/Nomadcxx/sysc-shell/internal/icons"
	"github.com/Nomadcxx/sysc-shell/internal/services"
	"github.com/Nomadcxx/sysc-shell/internal/theme"
	"github.com/Nomadcxx/sysc-shell/internal/ui"
)

func audioTree(r *Registry, h *PanelHost) *ui.Node {
	if h.audioTab == "" {
		h.audioTab = "volumes"
	}
	m := h.metrics()
	header := audioHeaderCard(h, m)
	body := audioVolumesTree(r, h)
	if h.audioTab == "devices" {
		body = audioDevicesTree(r, h)
	}
	if errText := audioPanelError(h.errLabel, audioMixerSnapshot(r)); errText != "" {
		body.Children = append([]*ui.Node{{Kind: ui.KindText, Text: errText, Tone: ui.ToneError}}, body.Children...)
	}
	panelH := h.place.Panel.H
	if panelH <= 0 {
		panelH = panelTargetSize(PanelAudio).H
	}
	viewportH := max(panelH-2*m.PanelPadding-header.Height-theme.MarginL, 0)
	return &ui.Node{Kind: ui.KindColumn, Gap: theme.MarginL, Padding: m.PanelPadding, Children: []*ui.Node{
		header,
		{Kind: ui.KindScroll, Height: viewportH, Children: []*ui.Node{body}},
	}}
}

func audioHeaderCard(h *PanelHost, m theme.Metrics) *ui.Node {
	well := m.StandardControl
	closeSz := m.CompactControl
	title := &ui.Node{Kind: ui.KindText, Text: "Audio", TextRole: theme.RoleHeadline, Name: "Audio", Role: "heading"}
	closeBtn := &ui.Node{
		Kind: ui.KindButton, Action: "audio-close", Name: "Close", Role: "button",
		Focusable: true, Width: closeSz, Height: closeSz, Shape: ui.ShapeCircle,
		Children: []*ui.Node{{Kind: ui.KindIcon, Icon: "close", IconSize: m.IconNormal}},
	}
	icon := &ui.Node{
		Kind: ui.KindCapsule, Width: well, Height: well, Shape: ui.ShapeMedium,
		Fill:     ui.FillContainerHighest,
		Children: []*ui.Node{{Kind: ui.KindIcon, Icon: "graphic_eq", IconSize: m.IconNormal}},
	}
	leading := &ui.Node{Kind: ui.KindRow, Gap: theme.MarginL, Height: well, Children: []*ui.Node{icon, title}}
	top := &ui.Node{Kind: ui.KindRow, Gap: theme.MarginL, Height: well, PinEnd: true, Children: []*ui.Node{
		leading, closeBtn,
	}}
	tabs := &ui.Node{
		Kind: ui.KindSegmented, Key: "audio-tab", Gap: theme.MarginXXS, Height: well,
		Children: []*ui.Node{
			audioSegment(h, "audio-tab:volumes", "Volumes", h.audioTab != "devices"),
			audioSegment(h, "audio-tab:devices", "Devices", h.audioTab == "devices"),
		},
	}
	return &ui.Node{
		Kind: ui.KindCapsule, Padding: m.CardPadding, Fill: ui.FillContainerHigh, Shape: ui.ShapeCard,
		Height:   2*m.CardPadding + 2*well + theme.MarginL, // token-exempt: derived from the card inset, the well and the column gap below; the scan matches the leading 2 of this expression, not a hardcoded height
		Children: []*ui.Node{{Kind: ui.KindColumn, Gap: theme.MarginL, Children: []*ui.Node{top, tabs}}},
	}
}

func audioSegment(h *PanelHost, action, label string, selected bool) *ui.Node {
	n := &ui.Node{
		Kind: ui.KindButton, Action: action, Name: label, Role: "tab",
		Focusable: true, Height: h.metrics().CompactControl,
		Children: []*ui.Node{{Kind: ui.KindText, Text: label}},
	}
	if selected {
		n.State |= ui.StateSelected
	}
	return n
}

func audioVolumesTree(r *Registry, h *PanelHost) *ui.Node {
	snap, stale, unavailable := audioMixerState(r)
	discardStaleAudioRouteMenu(h, snap)
	rows := []*ui.Node{
		audioVolumeCard(audioApplyPending(h, audioDefaultNode(snap.Sinks, "Output")), "Output", nil, h, nil),
		audioVolumeCard(audioApplyPending(h, audioDefaultNode(snap.Sources, "Input")), "Input", nil, h, nil),
		{Kind: ui.KindText, Text: fmt.Sprintf("Applications %d", len(snap.Streams)), TextRole: theme.RoleLabel, Height: 28}, // token-exempt: the applications label band, a measured text row with no counterpart in the density row
	}
	if unavailable {
		audioDisable(rows[0])
		audioDisable(rows[1])
	}
	if stale {
		audioMarkStale(rows[0])
		audioMarkStale(rows[1])
	}
	if len(snap.Streams) == 0 {
		rows = append(rows, &ui.Node{
			Kind: ui.KindText, Text: "No applications playing audio",
			TextRole: theme.RoleBody, CenterX: true,
		})
	} else {
		for _, stream := range snap.Streams {
			n := audioApplyPending(h, stream)
			img := audioStreamImage(r, n)
			row := audioVolumeCard(n, "Application", img, h, snap.Sinks)
			if unavailable {
				audioDisable(row)
			}
			rows = append(rows, row)
		}
	}
	return &ui.Node{Kind: ui.KindColumn, Gap: theme.MarginL, Children: rows}
}

func audioDevicesTree(r *Registry, h *PanelHost) *ui.Node {
	m := h.metrics()
	snap, _, unavailable := audioMixerState(r)
	out := []*ui.Node{
		{Kind: ui.KindText, Text: "Output device", TextRole: theme.RoleLabel},
	}
	if len(snap.Sinks) == 0 {
		reason := "not available"
		if unavailable {
			reason = "audio unavailable"
		}
		out = append(out, &ui.Node{Kind: ui.KindText, Text: reason, TextRole: theme.RoleBody})
	} else {
		for _, n := range snap.Sinks {
			out = append(out, audioDeviceRow(n, m))
		}
	}
	out = append(out, &ui.Node{Kind: ui.KindText, Text: "Input device", TextRole: theme.RoleLabel})
	if len(snap.Sources) == 0 {
		reason := "not available"
		if unavailable {
			reason = "audio unavailable"
		}
		out = append(out, &ui.Node{Kind: ui.KindText, Text: reason, TextRole: theme.RoleBody})
	} else {
		for _, n := range snap.Sources {
			out = append(out, audioDeviceRow(n, m))
		}
	}
	return &ui.Node{Kind: ui.KindColumn, Gap: theme.MarginM, Children: out}
}

func audioDeviceRow(n services.AudioNode, m theme.Metrics) *ui.Node {
	// KindRow paints no fill; KindButton centres its children. A capsule
	// around a two-child row gets the selected well and pinRowEnd puts the
	// check on the trailing edge (D6).
	inner := []*ui.Node{{Kind: ui.KindText, Text: n.Description, TextRole: theme.RoleBody}}
	if n.Default {
		inner = append(inner, &ui.Node{Kind: ui.KindIcon, Icon: "check", IconSize: m.IconNormal})
	}
	cap := &ui.Node{
		Kind: ui.KindCapsule, Action: fmt.Sprintf("audio-dev:%d", n.ID),
		Name: n.Description, Role: "button", Focusable: true,
		Height: 44, Padding: theme.MarginL, Shape: ui.ShapeMedium, // token-exempt: the device row band, a measured height that no density-row field equals
		Children: []*ui.Node{{Kind: ui.KindRow, Gap: theme.MarginL, Children: inner}},
	}
	if n.Default {
		cap.Fill = ui.FillSoft
	}
	return cap
}

// audioValueColumnW is the trailing percentage column. It is named because
// three places share it: the node below, the width the card assigns it, and
// the subtraction that leaves the middle column its remaining pixels. Only
// the first is visible to the conformance scan, so a number here would drift
// out of step with the other two on the first edit that missed them.
const audioValueColumnW = 44

func audioVolumeRow(n services.AudioNode, role string, icon *ui.Image, m theme.Metrics) *ui.Node {
	identIcon := audioRoleIcon(role, n.Muted)
	ident := &ui.Node{
		Kind: ui.KindCapsule, Width: m.CompactControl, Height: m.CompactControl, Shape: ui.ShapeCircle,
		Fill:     ui.FillContainerHighest,
		Children: []*ui.Node{{Kind: ui.KindIcon, Icon: identIcon, IconSize: m.IconNormal}},
	}
	if icon != nil {
		ident.Children = []*ui.Node{{Kind: ui.KindImage, Image: icon, ImageSize: m.CompactControl}}
	}
	name := n.Description
	if name == "" {
		name = n.Name
	}
	nameNode := &ui.Node{Kind: ui.KindText, Text: name, TextRole: theme.RoleBody}
	if role == "Output" || role == "Input" {
		nameNode = &ui.Node{
			Kind: ui.KindButton, Action: "audio-tab:devices", Name: name, Role: "button",
			Focusable: true, Fill: ui.FillNone,
			Children: []*ui.Node{{Kind: ui.KindText, Text: name, TextRole: theme.RoleBody}},
		}
	}
	value := "—"
	if n.ID != 0 {
		value = fmt.Sprintf("%d%%", n.Level)
	}
	slider := &ui.Node{
		Kind: ui.KindSlider, Value: float64(n.Level), Min: 0, Max: 100, Step: 5,
		Action: fmt.Sprintf("audio-vol:%d", n.ID), Focusable: n.ID != 0,
		Name: role + " volume", Role: "slider",
	}
	if n.ID == 0 {
		slider.State |= ui.StateDisabled
		slider.Absent = true
	}
	mid := &ui.Node{Kind: ui.KindColumn, Gap: theme.MarginS, Children: []*ui.Node{
		{Kind: ui.KindText, Text: role, TextRole: theme.RoleLabel},
		nameNode,
		slider,
	}}
	muteIcon := "volume_up"
	if n.Muted || identIcon == "mic_off" {
		muteIcon = "volume_off"
	}
	if role == "Input" {
		muteIcon = identIcon
	}
	mute := &ui.Node{
		Kind: ui.KindButton, Action: fmt.Sprintf("audio-mute:%d", n.ID),
		Name: "Mute " + role, Role: "button", Focusable: n.ID != 0,
		Width: m.CompactControl, Height: m.CompactControl, Shape: ui.ShapeCircle,
		Children: []*ui.Node{{Kind: ui.KindIcon, Icon: muteIcon, IconSize: m.IconNormal}},
	}
	if n.Muted {
		mute.State |= ui.StateSelected
	}
	if n.ID == 0 {
		mute.State |= ui.StateDisabled
	}
	return &ui.Node{Kind: ui.KindRow, Gap: theme.MarginL, Height: 68, Children: []*ui.Node{ // token-exempt: TestAudioDensityContract pins this band at 68 so a compact row still holds the role, the name and the slider
		ident, mid,
		{Kind: ui.KindText, Text: value, Width: audioValueColumnW, Tabular: true, MinWidthText: "100%", TextRole: theme.RoleBody},
		mute,
	}}
}

func audioVolumeCard(n services.AudioNode, role string, icon *ui.Image, h *PanelHost, outputs []services.AudioNode) *ui.Node {
	m := h.metrics()
	row := audioVolumeRow(n, role, icon, m)
	panelW := h.place.Panel.W
	if panelW <= 0 {
		panelW = panelTargetSize(PanelAudio).W
	}
	innerW := max(panelW-2*m.PanelPadding-2*m.CardPadding, 0)
	// Identity, value and mute are fixed; the middle column receives every
	// remaining pixel so the slider grows with the panel.
	valueW := audioValueColumn(h)
	row.Children[1].Width = max(innerW-m.CompactControl-valueW-m.CompactControl-3*row.Gap, 0)
	row.Children[2].Width = valueW
	children := []*ui.Node{row}
	cardH := row.Height + 2*m.CardPadding
	if role == "Application" {
		route := audioRouteControl(h, n, outputs)
		route.Height = m.StandardControl
		children = append(children, route)
		cardH += theme.MarginS + route.Height
	}
	return &ui.Node{
		Kind: ui.KindCapsule, Height: cardH,
		Padding: m.CardPadding, Fill: ui.FillContainerHigh, Shape: ui.ShapeCard,
		Children: []*ui.Node{{Kind: ui.KindColumn, Gap: theme.MarginS, Children: children}},
	}
}

// audioValueColumn is the value column's width: audioValueColumnW, or "100%"
// as measured at the panel's scale when that is wider. A fractional scale
// rounds the measured text up (45 at 1.25), and the fixed column then pushed
// the mute button off the row.
func audioValueColumn(h *PanelHost) int {
	// The tree is built at open, before anything else has loaded the text
	// engine, and measureText's fallback estimate would always fit.
	if err := h.ensureText(); err != nil {
		return audioValueColumnW
	}
	w, _ := h.measureText()("100%", ui.TextAttrs{Role: theme.RoleBody, Tabular: true})
	return max(audioValueColumnW, w)
}

func audioRoleIcon(role string, muted bool) string {
	switch role {
	case "Input":
		if muted {
			return "mic_off"
		}
		return "mic"
	case "Application":
		return "graphic_eq"
	default:
		return "headphones"
	}
}

func audioDefaultNode(nodes []services.AudioNode, role string) services.AudioNode {
	for _, n := range nodes {
		if n.Default {
			return n
		}
	}
	if len(nodes) > 0 {
		return nodes[0]
	}
	return services.AudioNode{Description: role}
}

func audioMixerState(r *Registry) (services.AudioSnapshot, bool, bool) {
	if r == nil || r.audio == nil {
		return services.AudioSnapshot{}, true, true
	}
	if !r.audio.Available() {
		return r.audio.Mixer(), !r.audio.MixerReady(), true
	}
	snap := r.audio.Mixer()
	stale := !r.audio.MixerReady()
	return snap, stale, false
}

func audioMixerSnapshot(r *Registry) services.AudioSnapshot {
	if r == nil || r.audio == nil {
		return services.AudioSnapshot{}
	}
	return r.audio.Mixer()
}

func audioPanelError(controlError string, snap services.AudioSnapshot) string {
	if controlError != "" {
		return controlError
	}
	return snap.Error
}

func audioApplyPending(h *PanelHost, n services.AudioNode) services.AudioNode {
	if h == nil || n.ID == 0 {
		return n
	}
	if h.pendingVol != nil {
		if v, ok := h.pendingVol[n.ID]; ok {
			n.Level = v
		}
	}
	if h.pendingMute != nil {
		if v, ok := h.pendingMute[n.ID]; ok {
			n.Muted = v
		}
	}
	return n
}

func audioStreamImage(r *Registry, n services.AudioNode) *ui.Image {
	if r == nil || r.trayIcons == nil {
		return nil
	}
	name := n.Icon
	if name == "" {
		name = strings.ToLower(n.Name)
	}
	if name == "" {
		return nil
	}
	key := icons.Square(name, 32)
	if img, ok := r.trayIcons.Lookup(key); ok {
		return img
	}
	_, _, _ = r.trayIcons.Request(key)
	return nil
}

func audioDisable(n *ui.Node) {
	if n == nil {
		return
	}
	n.State |= ui.StateDisabled
	for _, c := range n.Children {
		audioDisable(c)
	}
}

func audioMarkStale(n *ui.Node) {
	var walk func(*ui.Node)
	walk = func(x *ui.Node) {
		if x == nil {
			return
		}
		if x.Kind == ui.KindSlider {
			x.Absent = true
			x.Value = 0
		}
		if strings.HasSuffix(x.Text, "%") {
			x.Text = "—"
		}
		for _, c := range x.Children {
			walk(c)
		}
	}
	walk(n)
}

func (h *PanelHost) applyAudioControl(r *Registry, n *ui.Node) bool {
	if n == nil || r == nil {
		return false
	}
	action := n.Action
	switch {
	case action == "audio-tab:volumes":
		h.audioTab = "volumes"
		r.rebuildPanel(h)
		return true
	case action == "audio-tab:devices":
		h.audioTab = "devices"
		r.rebuildPanel(h)
		return true
	}
	if id, ok := audioActionID(action, "audio-vol:"); ok {
		level := int(n.Value)
		if h.pendingVol == nil {
			h.pendingVol = map[int]int{}
		}
		h.pendingVol[id] = level
		h.pendingAt = time.Now()
		r.scheduleControl(h, func() error {
			if r.audio == nil {
				return fmt.Errorf("audio unavailable")
			}
			return r.audio.SetNodeVolume(id, level)
		})
		r.rebuildPanel(h)
		return true
	}
	if id, ok := audioActionID(action, "audio-mute:"); ok {
		muted := n.State.Has(ui.StateSelected)
		on := !muted
		if h.pendingMute == nil {
			h.pendingMute = map[int]bool{}
		}
		h.pendingMute[id] = on
		h.pendingAt = time.Now()
		r.scheduleControl(h, func() error {
			if r.audio == nil {
				return fmt.Errorf("audio unavailable")
			}
			return r.audio.SetNodeMute(id, on)
		})
		r.rebuildPanel(h)
		return true
	}
	if id, ok := audioActionID(action, "audio-dev:"); ok {
		r.scheduleControl(h, func() error {
			if r.audio == nil {
				return fmt.Errorf("audio unavailable")
			}
			return r.audio.SetDefault(id)
		})
		return true
	}
	return false
}

func audioRoutableSinks(sinks []services.AudioNode) []services.AudioNode {
	var out []services.AudioNode
	for _, sink := range sinks {
		if sink.Routeable {
			out = append(out, sink)
		}
	}
	return out
}

func audioRouteControl(h *PanelHost, stream services.AudioNode, sinks []services.AudioNode) *ui.Node {
	action := fmt.Sprintf("audio-route:%d", stream.ID)
	choices := audioRoutableSinks(sinks)
	if !stream.Routeable || len(choices) == 0 {
		if h != nil && h.menuPath == action && h.menu != nil && h.menu.Opened() {
			clearAudioRouteMenu(h)
		}
		label := "Output routing unavailable"
		if stream.Routeable {
			label = "No available outputs"
		}
		return &ui.Node{Kind: ui.KindText, Text: label, Name: label, Role: "status", State: ui.StateDisabled}
	}
	if h != nil && h.menuPath == action && h.menu != nil && h.menu.Opened() {
		if !sameAudioRouteSinks(h.audioRouteSinks, choices) {
			clearAudioRouteMenu(h)
		} else {
			n := h.menu.Node()
			n.Text = audioRouteLabel(stream, sinks)
			n.Action = action
			n.Name = audioRouteName(stream)
			n.Role = "combobox"
			n.Height = h.metrics().StandardControl
			return n
		}
	}
	options := make([]string, len(choices))
	index := 0
	for i, sink := range choices {
		options[i] = audioSinkLabel(sink)
		if sink.ID == stream.OutputID {
			index = i
		}
	}
	n := NewMenu(options, index).Node()
	n.Text = audioRouteLabel(stream, sinks)
	n.Action = action
	n.Name = audioRouteName(stream)
	n.Role = "combobox"
	n.Height = h.metrics().StandardControl
	return n
}

func audioRouteLabel(stream services.AudioNode, sinks []services.AudioNode) string {
	for _, sink := range sinks {
		if sink.ID == stream.OutputID {
			return "Output: " + audioSinkLabel(sink)
		}
	}
	return "Output: Unknown"
}

func audioRouteName(stream services.AudioNode) string {
	name := stream.Description
	if name == "" {
		name = stream.Name
	}
	if name == "" {
		name = "application"
	}
	return "Output for " + name
}

func audioSinkLabel(sink services.AudioNode) string {
	if sink.Description != "" {
		return sink.Description
	}
	if sink.Name != "" {
		return sink.Name
	}
	return fmt.Sprintf("Output %d", sink.ID)
}

func sameAudioRouteSinks(a, b []services.AudioNode) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i].ID != b[i].ID || audioSinkLabel(a[i]) != audioSinkLabel(b[i]) {
			return false
		}
	}
	return true
}

func discardStaleAudioRouteMenu(h *PanelHost, snap services.AudioSnapshot) {
	if h == nil || h.menu == nil || !h.menu.Opened() || !strings.HasPrefix(h.menuPath, "audio-route:") {
		return
	}
	id, ok := audioActionID(h.menuPath, "audio-route:")
	if ok {
		for _, stream := range snap.Streams {
			if stream.ID == id && stream.Routeable && sameAudioRouteSinks(h.audioRouteSinks, audioRoutableSinks(snap.Sinks)) {
				return
			}
		}
	}
	clearAudioRouteMenu(h)
}

func clearAudioRouteMenu(h *PanelHost) {
	if h.menu != nil && strings.HasPrefix(h.menuPath, "audio-route:") {
		h.menu.Cancel()
		h.menu = nil
		h.menuPath = ""
		h.audioRouteSinks = nil
	}
}

func (h *PanelHost) activateAudioRoute(r *Registry, n *ui.Node) bool {
	streamID, ok := audioActionID(n.Action, "audio-route:")
	if !ok {
		return false
	}
	if h.menu != nil && h.menuPath == n.Action && h.menu.Opened() {
		if !h.menu.PickAt(n, h.hoverX, h.hoverY) {
			return true
		}
		h.menu.Select()
		h.applyAudioRouteMenu(r, n.Action)
		r.rebuildPanel(h)
		return true
	}
	snap := audioMixerSnapshot(r)
	var stream services.AudioNode
	for _, candidate := range snap.Streams {
		if candidate.ID == streamID {
			stream = candidate
			break
		}
	}
	choices := audioRoutableSinks(snap.Sinks)
	if !stream.Routeable || len(choices) == 0 {
		return true
	}
	options := make([]string, len(choices))
	index := 0
	for i, sink := range choices {
		options[i] = audioSinkLabel(sink)
		if sink.ID == stream.OutputID {
			index = i
		}
	}
	if h.menu != nil {
		h.menu.Cancel()
	}
	h.audioRouteSinks = choices
	h.menu = NewMenu(options, index)
	h.menu.Open()
	h.menuPath = n.Action
	r.rebuildPanel(h)
	return true
}

func (h *PanelHost) applyAudioRouteMenu(r *Registry, path string) {
	streamID, ok := audioActionID(path, "audio-route:")
	if !ok || h.menu == nil || h.menu.Opened() {
		return
	}
	index := h.menu.Index()
	if index < 0 || index >= len(h.audioRouteSinks) {
		clearAudioRouteMenu(h)
		return
	}
	sinkID := h.audioRouteSinks[index].ID
	h.menu = nil
	h.menuPath = ""
	h.audioRouteSinks = nil
	r.scheduleControl(h, func() error {
		if r.audio == nil {
			return fmt.Errorf("audio unavailable")
		}
		return r.audio.MoveStream(streamID, sinkID)
	})
}

func audioActionID(action, prefix string) (int, bool) {
	rest, ok := strings.CutPrefix(action, prefix)
	if !ok {
		return 0, false
	}
	id, err := strconv.Atoi(rest)
	return id, err == nil
}

func (r *Registry) scheduleControl(h *PanelHost, run func() error) {
	r.scheduleControlResult(h, func() (bool, error) { return true, run() })
}

// scheduleControlResult applies the error and rebuilds the host only when the
// result still applies. A skipped stale operation leaves existing feedback in
// place but still invalidates the surface after its worker finishes.
func (r *Registry) scheduleControlResult(h *PanelHost, run func() (bool, error)) {
	go func() {
		apply, err := run()
		r.mu.Lock()
		if r.panelHosts[h.id] != h {
			r.mu.Unlock()
			return
		}
		if apply {
			if err != nil {
				h.errLabel = err.Error()
			} else {
				h.errLabel = ""
			}
			r.rebuildPanel(h)
		}
		output, id := h.output, panelSurfaceID(h.id)
		// The invalidation send can block, so it runs with Registry.mu free.
		r.mu.Unlock()
		r.publishSurface(output, id)
	}()
}
