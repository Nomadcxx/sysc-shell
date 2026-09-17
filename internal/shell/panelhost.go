package shell

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"sync"
	"time"

	launcher "github.com/Nomadcxx/sysc-launch"
	"github.com/Nomadcxx/sysc-notify/protocol"
	"github.com/Nomadcxx/sysc-shell/internal/config"
	"github.com/Nomadcxx/sysc-shell/internal/platform/wayland"
	"github.com/Nomadcxx/sysc-shell/internal/platform/wayland/layershell"
	"github.com/Nomadcxx/sysc-shell/internal/render"
	"github.com/Nomadcxx/sysc-shell/internal/services"
	"github.com/Nomadcxx/sysc-shell/internal/settings"
	"github.com/Nomadcxx/sysc-shell/internal/theme"
	"github.com/Nomadcxx/sysc-shell/internal/ui"
	"github.com/Nomadcxx/sysc-shell/internal/wallpaper"
	v1 "github.com/Nomadcxx/sysc-shell/plugin/v1"
)

const (
	keyboardExclusive = uint32(layershell.ZwlrLayerSurfaceV1KeyboardInteractivityExclusive)
	keyboardNone      = uint32(layershell.ZwlrLayerSurfaceV1KeyboardInteractivityNone)
	layerOverlay      = layershell.ZwlrLayerShellV1LayerOverlay

	keyEsc       = 1
	keyBackspace = 14
	keyTab       = 15
	keyEnter     = 28
	keyLeftShift = 42
	keySpace     = 57
	keyHome      = 102
	keyUp        = 103
	keyPageUp    = 104
	keyLeft      = 105
	keyRight     = 106
	keyEnd       = 107
	keyDown      = 108
	keyPageDown  = 109
	keyDelete    = 111

	btnLeft  = 272
	btnRight = 273

	// shieldQuietFor drops the press that mapped the overlay, so the click
	// that opened a panel cannot dismiss it through the fullscreen shield.
	shieldQuietFor = 400 * time.Millisecond
)

// Trigger is the placement hint for opening a panel on one output.
type Trigger struct {
	BarEdge    string
	BarZone    int
	Align      string
	OutW, OutH int
	AnchorX    int
}

// PanelHost is one open panel: two surfaces' callbacks, content tree, focus,
// leases, and reveal state.
type PanelHost struct {
	id          PanelID
	output      uint32
	place       Placement
	root        *ui.Node
	focus       []*ui.Node
	roving      ui.Roving
	leases      []*services.Lease
	shieldQuiet time.Time
	// anim is this surface's one clock: every transition it runs shares it, so
	// frames are scheduled from a single place.
	anim     *animator
	stopAnim chan struct{}
	stopOnce sync.Once
	theme    Theme
	// themeFrom is the palette this surface is fading out of. It is the theme
	// as it was rendering when the change arrived, not the last published one,
	// so a reload during a fade continues from what is on screen.
	themeFrom  Theme
	text       *render.TextRenderer
	fontFamily string
	// backdrop is the blurred capture of whatever sat behind this panel,
	// taken before either of its surfaces existed. Nil means no blur, and the
	// panel then paints over whatever the compositor shows, as it always has.
	backdrop *ui.Image
	logicalW int
	logicalH int
	scale120 int
	shift    bool
	pressed  string
	// pointer is the resolved hover/press state, kept as stable keys so it
	// survives the tree rebuilds that replace every node.
	pointer            interaction
	drag               ui.Drag
	lastAction         string
	hoverX, hoverY     int
	monthDelta         int
	errLabel           string
	menu               *Menu
	menuPath           string
	menus              map[string]*Menu
	sliderDrag         *ui.Node
	scrollDrag         *ui.Node
	set                *settings.Registry
	draft              config.Config
	query              string
	section            string
	pageDirection      int
	mediaLease         *services.Lease
	mediaSeekPending   *int64
	mediaSeekTrack     string
	networkTab         string
	weatherView        string
	pendingSSID        string
	password           *ui.Field
	bluetoothInput     *ui.Field
	bluetoothPromptID  services.PromptID
	bluetoothDetails   services.DeviceID
	bluetoothForget    services.DeviceID
	bluetoothRetry     string
	bluetoothDiscovery bool
	search             *ui.Field
	fields             map[string]*ui.Field
	editors            map[string]*retainedEditor

	launcherResults []launcher.Result
	launcherSel     int
	launcherScroll  int
	launcherMenuID  string
	launcherActions []launcher.Action

	wallpaperSnap    wallpaper.Snapshot
	wallpaperDir     string
	wallpaperFilter  wallpaper.Filter
	wallpaperOutput  string
	wallpaperSel     int
	wallpaperFocused bool
	// wallpaperMenu names the open chrome dropdown ("folder" or "palette"),
	// or is empty when none is. One field rather than a flag each keeps them
	// mutually exclusive: two lists open at once would each claim a slice of
	// the panel the grid was sized against.
	wallpaperMenu string
	// wallpaperPaletteSource and wallpaperPaletteSeed mirror cfg.ThemeGen so
	// the theme combobox can show what is pinned. They are snapshotted when
	// the tree is built, under Registry.mu, like wallpaperSnap.
	wallpaperPaletteSource string
	wallpaperPaletteSeed   string
	// wallpaperThemeErr mirrors Registry.themeErr for the picker's banners.
	wallpaperThemeErr string

	notifyFilter string
	notifyExpand string
	notifyMenu   bool

	profiles      []string
	profileActive string
	profilesOK    bool

	audioTab    string
	mixerLease  *services.MixerLease
	pendingVol  map[int]int
	pendingMute map[int]bool
	pendingAt   time.Time

	monitorPage      string
	processFilter    string
	processSort      string
	processDesc      bool
	processStatus    string
	processStatusErr error
	processSelected  services.ProcessIdentity

	clipboardSelectedID       string
	clipboardConfirmScope     string
	clipboardDeleteConfirmID  string
	clipboardThumbnails       map[string]*ui.Image
	clipboardThumbnailRequest map[string]struct{}
}

func parsePanelName(name string) (PanelID, error) {
	switch name {
	case "clock":
		return PanelClock, nil
	case "system-monitor":
		return PanelMonitor, nil
	case "session", "power":
		return PanelSession, nil
	case "settings":
		return PanelSettings, nil
	case "launcher":
		return PanelLauncher, nil
	case "plugin":
		return PanelPlugin, nil
	case "notifications":
		return PanelNotifications, nil
	case "wallpaper":
		return PanelWallpaper, nil
	case "audio":
		return PanelAudio, nil
	case "control-center":
		return PanelControlCenter, nil
	case "network":
		return PanelNetwork, nil
	case "bluetooth":
		return PanelBluetooth, nil
	case "weather":
		return PanelWeather, nil
	case "clipboard":
		return PanelClipboard, nil
	default:
		return 0, fmt.Errorf("unknown panel")
	}
}

func (r *Registry) AuxRequests() <-chan wayland.AuxRequest { return r.aux }

func (r *Registry) TogglePanelByName(name string) error {
	return r.HandlePanelByName("toggle", name, "")
}

func (r *Registry) OpenPanelByName(name string) error {
	return r.HandlePanelByName("open", name, "")
}

func (r *Registry) ClosePanelByName(name string) error {
	return r.HandlePanelByName("close", name, "")
}

// HandlePanelByName validates a requested section before it changes the root.
func (r *Registry) HandlePanelByName(action, name, section string) error {
	id, err := parsePanelName(name)
	if err != nil {
		return err
	}
	section, err = panelSection(id, section)
	if err != nil {
		return err
	}
	if action == "close" {
		r.ClosePanel(id)
		return nil
	}
	if action != "open" && action != "toggle" {
		return fmt.Errorf("unknown panel action")
	}

	out, trig := r.focusedTrigger()
	r.mu.Lock()
	defer r.mu.Unlock()
	if where, ok := r.panels.Output(id); ok && where == out && r.roots.owns(panelRoot(id)) {
		if action == "toggle" {
			r.closePanelLocked(id)
			return nil
		}
		return r.selectPanelSectionLocked(id, section)
	}
	if err := r.openPanelRootLocked(id, out, trig); err != nil {
		return err
	}
	return r.selectPanelSectionLocked(id, section)
}

func panelSection(id PanelID, requested string) (string, error) {
	if requested == "" {
		switch id {
		case PanelControlCenter:
			return "home", nil
		case PanelSettings:
			return "Bar", nil
		default:
			return "", nil
		}
	}
	switch id {
	case PanelControlCenter:
		section, ok := ccSectionFor(requested)
		if !ok {
			return "", fmt.Errorf("unknown section %q", requested)
		}
		if !section.Enabled {
			return "", fmt.Errorf("section %q is unavailable", requested)
		}
		return requested, nil
	case PanelSettings:
		for _, section := range settingsSections {
			if section == requested {
				return requested, nil
			}
		}
	}
	return "", fmt.Errorf("unknown section %q", requested)
}

func (r *Registry) selectPanelSectionLocked(id PanelID, section string) error {
	if section == "" {
		return nil
	}
	h := r.panelHosts[id]
	if h == nil {
		return fmt.Errorf("panel %q is not open", id)
	}
	if h.section == section {
		return nil
	}
	if id == PanelControlCenter {
		h.selectControlCentreSection(r, section)
		r.publishSurface(h.output, panelSurfaceID(id))
		return nil
	}
	h.section = section
	r.rebuildPanel(h)
	r.publishSurface(h.output, panelSurfaceID(id))
	return nil
}

func (r *Registry) focusedTrigger() (uint32, Trigger) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.focusedTriggerLocked()
}

func (r *Registry) focusedTriggerLocked() (uint32, Trigger) {
	var global uint32
	var connector string
	if r.focused != "" {
		for g, bar := range r.bars {
			if bar.connector() == r.focused {
				global, connector = g, r.focused
				break
			}
		}
	}
	if global == 0 {
		for g, bar := range r.bars {
			global, connector = g, bar.connector()
			break
		}
	}
	return global, r.triggerLocked(global, connector)
}

func (r *Registry) triggerFor(global uint32) (uint32, Trigger) {
	r.mu.Lock()
	defer r.mu.Unlock()
	connector := ""
	if bar, ok := r.bars[global]; ok {
		connector = bar.connector()
	}
	return global, r.triggerLocked(global, connector)
}

func (r *Registry) triggerLocked(global uint32, connector string) Trigger {
	policy := r.cfg.ForConnector(connector)
	trig := Trigger{BarEdge: policy.Edge, BarZone: policy.Height - policy.Gap, Align: "center"}
	if bar, ok := r.bars[global]; ok {
		w, h := bar.configuredSize()
		if w > 0 {
			trig.OutW = w
		}
		if h > 0 {
			trig.BarZone = h
		}
		// The screen, not the bar. Without it every panel was placed as if the
		// output were 1080 logical tall, and a taller panel than the output
		// could hold overran the bottom edge on a scaled laptop.
		if ow, oh := bar.outputSize(); oh > 0 {
			trig.OutW, trig.OutH = ow, oh
			if w > 0 {
				trig.OutW = w
			}
		}
	}
	return trig
}

func (r *Registry) OpenPanel(id PanelID, output uint32, trig Trigger) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if where, ok := r.panels.Output(id); ok && where == output && r.roots.owns(panelRoot(id)) {
		return nil
	}
	return r.openPanelRootLocked(id, output, trig)
}

// openPanelRootLocked publishes the panel as the process-wide interactive
// root. Whatever chain was open is released first, so opening an unrelated
// panel closes the previous one and this panel on any other output.
func (r *Registry) openPanelRootLocked(id PanelID, output uint32, trig Trigger) error {
	generation := r.roots.openRoot(panelRoot(id))
	if r.panels.open == nil {
		r.panels.open = make(map[PanelID]uint32)
	}
	r.panels.open[id] = output
	if err := r.spawnPanelLocked(id, output, trig); err != nil {
		r.panels.Close(id)
		r.roots.closeRoot(generation)
		return err
	}
	if id == PanelNotifications {
		r.setCenterOpen(true)
		if ids := r.markCenterSeen(); len(ids) > 0 {
			r.sendNotify(protocol.Command{Kind: protocol.CommandHistoryMarkSeen, IDs: ids})
		}
	}
	r.roots.onClose(generation, func() {
		r.panels.Close(id)
		r.teardownPanelLocked(id)
		// A root that goes away takes any visible tooltip with it.
		r.dwell.leave()
		if id == PanelPlugin && r.plugins != nil {
			ids := r.plugins.snapshotPanelViewIDs()
			go r.plugins.dropPanelViews(ids)
		}
	})
	return nil
}

func (r *Registry) ClosePanel(id PanelID) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.closePanelLocked(id)
}

// closePanelLocked closes the panel through the root chain when it owns the
// chain, so every release runs exactly once and in one order.
func (r *Registry) closePanelLocked(id PanelID) {
	if _, generation, ok := r.roots.current(); ok && r.roots.owns(panelRoot(id)) {
		r.roots.closeRoot(generation)
		return
	}
	r.panels.Close(id)
	r.teardownPanelLocked(id)
	if id == PanelPlugin && r.plugins != nil {
		ids := r.plugins.snapshotPanelViewIDs()
		go r.plugins.dropPanelViews(ids)
	}
}

func (r *Registry) TogglePanel(id PanelID, output uint32, trig Trigger) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if where, ok := r.panels.Output(id); ok && where == output {
		r.closePanelLocked(id)
		return nil
	}
	// A panel asked for on a different output is a fresh root there; opening
	// releases the chain that held the old instance.
	return r.openPanelRootLocked(id, output, trig)
}

func (r *Registry) DropAux(output uint32, surfaceID string) {
	if r.DropTrayAux(output, surfaceID) {
		return
	}
	if surfaceID == runningAppMenuSurfaceID || surfaceID == runningAppMenuShieldID {
		r.mu.Lock()
		if h := r.runningMenu; h != nil && h.open_ && h.output == output {
			h.closeLocked()
		}
		r.mu.Unlock()
		return
	}
	id, ok := panelIDFromAux(surfaceID)
	if !ok {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	h := r.panelHosts[id]
	if h == nil || h.output != output {
		return
	}
	r.closePanelLocked(id)
}

func panelIDFromAux(surfaceID string) (PanelID, bool) {
	name, ok := strings.CutPrefix(surfaceID, "panel:")
	if !ok {
		name, ok = strings.CutPrefix(surfaceID, "shield:")
		if !ok {
			return 0, false
		}
	}
	switch name {
	case "clock":
		return PanelClock, true
	case "system-monitor":
		return PanelMonitor, true
	case "session", "power":
		return PanelSession, true
	case "settings":
		return PanelSettings, true
	case "launcher":
		return PanelLauncher, true
	case "plugin":
		return PanelPlugin, true
	case "notifications":
		return PanelNotifications, true
	case "wallpaper":
		return PanelWallpaper, true
	case "audio":
		return PanelAudio, true
	case "control-center":
		return PanelControlCenter, true
	case "network":
		return PanelNetwork, true
	case "bluetooth":
		return PanelBluetooth, true
	case "weather":
		return PanelWeather, true
	case "clipboard":
		return PanelClipboard, true
	default:
		return 0, false
	}
}

func (r *Registry) spawnPanelLocked(id PanelID, output uint32, trig Trigger) error {
	outW, outH := trig.OutW, trig.OutH
	if outW <= 0 {
		outW = 1920
	}
	if outH <= 0 {
		outH = 1080
	}
	size := panelTargetSize(id)
	if id == PanelPlugin && r.plugins != nil {
		size = r.plugins.panelSize()
	}
	if id == PanelAudio {
		size = audioPanelSize(outW, outH)
	}
	gap := r.cfg.Panels.Gap
	if id == PanelPlugin || id == PanelAudio || id == PanelControlCenter {
		gap = 0
	}
	place := Placement{
		BarEdge: trig.BarEdge,
		Output:  ui.Rect{W: outW, H: outH},
		BarZone: trig.BarZone,
		Gap:     gap,
		Padding: r.cfg.Panels.Padding,
		Panel:   size,
		Align:   trig.Align,
		AnchorX: trig.AnchorX,
	}
	if id == PanelSettings && place.Align == "" {
		place.Align = "center"
	}
	if id == PanelSession || id == PanelNotifications {
		place.Align = "right"
	}
	if id == PanelLauncher || id == PanelWallpaper {
		place.CenterY = true
	}
	if id == PanelClipboard {
		// Clipboard history is a true modal: centre it against the whole output,
		// not the bar-free region used by attached/floating pickers.
		place.BarZone = 0
		place.Gap = 0
		place.CenterY = true
		place.Align = "center"
	}

	h := &PanelHost{
		id:          id,
		output:      output,
		place:       place,
		stopAnim:    make(chan struct{}),
		shieldQuiet: time.Now().Add(shieldQuietFor),
		theme:       r.panelThemeFor(output),
		fontFamily:  r.panelFontFamily(output),
	}
	if bar, ok := r.bars[output]; ok {
		h.scale120 = bar.scale120()
	}
	if id == PanelSettings {
		h.set = settings.DefaultFor(r.cfg)
		h.draft = r.cfg
		h.section = "Bar"
		h.search = ui.NewField("")
		h.menus = map[string]*Menu{}
		h.fields = map[string]*ui.Field{}
	}
	if id == PanelLauncher {
		h.search = ui.NewField("")
		svc := r.launcherServiceLocked()
		svc.Open()
		svc.Query("")
	}
	if id == PanelWallpaper {
		h.search = ui.NewField("")
		h.wallpaperFilter = wallpaper.FilterAll
		h.wallpaperOutput = wallpaper.AllOutputs
		if svc := r.wallpaperServiceLocked(); svc != nil {
			h.wallpaperSnap = svc.Snapshot()
			h.wallpaperDir = firstRoot(h.wallpaperSnap)
		}
	}
	if id == PanelAudio {
		h.audioTab = "volumes"
	}
	if id == PanelMonitor {
		h.monitorPage = monitorPageProcesses
		h.processFilter = "all"
		h.processSort, h.processDesc = "cpu", true
		h.search = ui.NewField("")
	}
	if id == PanelControlCenter {
		h.section = "home"
	}
	if id == PanelClipboard {
		h.search = ui.NewField("")
		h.clipboardThumbnails = make(map[string]*ui.Image)
		h.clipboardThumbnailRequest = make(map[string]struct{})
	}
	h.root = r.panelTree(h)
	if id == PanelNotifications {
		_ = h.ensureText()
		h.place.Panel.H = notificationsSurfaceHeight(h)
		// The notification tree derives its scroll viewport from the panel
		// height. Rebuild it after fitting the surface so the viewport does not
		// retain the initial 300 px fallback and clip the last visible card.
		h.root = r.panelTree(h)
	}
	h.focus = ui.Focusables(h.root)
	h.roving = ui.Roving{Count: len(h.focus)}
	if id == PanelWallpaper {
		// The picker opens on its search box rather than on the Close button
		// that happens to be first in the tree.
		h.focusByName("Search")
		h.wallpaperFocused = true
	}
	if id == PanelAudio {
		h.focusByName("Volumes")
	}
	if id == PanelClipboard {
		h.focusByName("Search")
	}
	w, hgt := h.place.FittedSize()
	h.place.Panel.W, h.place.Panel.H = w, hgt
	margins := h.place.Margins()
	if err := r.acquirePanelLeases(h); err != nil {
		return err
	}

	r.panelHosts[id] = h

	r.sendAux(wayland.AuxRequest{Output: output, Open: r.shieldSpec(h)})
	r.sendAux(wayland.AuxRequest{Output: output, Open: r.panelSpec(h, margins)})

	if id == PanelSession || id == PanelControlCenter {
		r.scheduleLoadProfiles(h)
	}
	if id == PanelNetwork && r.network != nil && r.network.Available() {
		network := r.network
		r.scheduleControl(h, network.Scan)
	}
	if id == PanelBluetooth {
		r.startBluetoothDiscoveryLocked(h)
	}

	h.anim = newAnimator(nil, r.cfg.Accessibility.ReducedMotion, h.theme.Motion)
	h.anim.Target(panelSurfaceID(id), animVisible, 1)
	r.scheduleSurfaceFrames(h)
	return nil
}

func (r *Registry) acquirePanelLeases(h *PanelHost) error {
	switch h.id {
	case PanelClock:
		lease, err := r.clock.Acquire(time.Second)
		if err != nil {
			return err
		}
		h.leases = []*services.Lease{lease}
	case PanelMonitor:
		connector := ""
		if bar, ok := r.bars[h.output]; ok {
			connector = bar.connector()
		}
		for _, sel := range monitorSelectors(r.cfg.ForConnector(connector)) {
			lease, err := r.metrics.Acquire(sel, time.Second)
			if err != nil {
				releaseAll(h.leases)
				h.leases = nil
				return err
			}
			h.leases = append(h.leases, lease)
		}
		lease, err := r.metrics.Acquire(services.Selector{Source: services.SourceProcess}, time.Second)
		if err != nil {
			releaseAll(h.leases)
			h.leases = nil
			return err
		}
		h.leases = append(h.leases, lease)
	case PanelSession:
		lease, err := r.metrics.Acquire(services.Selector{Source: services.SourceBattery}, time.Second)
		if err != nil {
			return err
		}
		h.leases = []*services.Lease{lease}
	case PanelAudio:
		if r.audio == nil {
			h.errLabel = "audio unavailable"
			return nil
		}
		lease, err := r.audio.MixerAcquire()
		if err != nil {
			h.errLabel = err.Error()
			return nil
		}
		h.mixerLease = lease
	case PanelNetwork:
		lease, err := r.metrics.Acquire(services.Selector{Source: services.SourceNetwork}, time.Second)
		if err != nil {
			return err
		}
		h.leases = []*services.Lease{lease}
	case PanelWeather:
		lease, err := r.weather.Acquire(r.cfg.Weather.Interval)
		if err != nil {
			// An unconfigured weather block has no interval to lease at; the
			// panel still opens and renders its placeholder.
			h.errLabel = "weather unavailable"
			return nil
		}
		h.leases = []*services.Lease{lease}
	case PanelControlCenter:
		for _, sel := range []services.Selector{
			{Source: services.SourceCPU},
			{Source: services.SourceMemory},
			{Source: services.SourceBattery},
		} {
			lease, err := r.metrics.Acquire(sel, time.Second)
			if err != nil {
				releaseAll(h.leases)
				h.leases = nil
				return err
			}
			h.leases = append(h.leases, lease)
		}
		lease, err := r.clock.Acquire(time.Second)
		if err != nil {
			releaseAll(h.leases)
			h.leases = nil
			return err
		}
		h.leases = append(h.leases, lease)
		if r.cfg.Weather.Interval > 0 {
			lease, err := r.weather.Acquire(r.cfg.Weather.Interval)
			if err != nil {
				releaseAll(h.leases)
				h.leases = nil
				return err
			}
			h.leases = append(h.leases, lease)
		}
	}
	return nil
}

func monitorSelectors(bar config.Bar) []services.Selector {
	out := []services.Selector{
		{Source: services.SourceCPU},
		{Source: services.SourceMemory},
		{Source: services.SourceGPU},
	}
	seenFS, seenBlock, seenNet := false, false, false
	for _, item := range append(append(append([]config.Item{}, bar.Left...), bar.Center...), bar.Right...) {
		sel, ok := metricSelector(item)
		if !ok {
			continue
		}
		switch sel.Source {
		case services.SourceFilesystem:
			if seenFS {
				continue
			}
			seenFS = true
		case services.SourceBlock:
			if seenBlock {
				continue
			}
			seenBlock = true
		case services.SourceNetwork:
			if seenNet {
				continue
			}
			seenNet = true
		default:
			continue
		}
		out = append(out, sel)
	}
	return out
}

func placeholderTree() *ui.Node {
	btn := func(text, action string) *ui.Node {
		return &ui.Node{
			Kind: ui.KindButton, Text: text, Action: action,
			Name: text, Role: "button", Focusable: true,
		}
	}
	return &ui.Node{Kind: ui.KindColumn, Gap: theme.MarginM, Padding: theme.MarginL, Children: []*ui.Node{
		{Kind: ui.KindText, Text: "Panel"},
		btn("Lock", "lock"),
		btn("Two", "two"),
		btn("Three", "three"),
	}}
}

func panelSurfaceID(id PanelID) string  { return "panel:" + id.String() }
func shieldSurfaceID(id PanelID) string { return "shield:" + id.String() }

// blur-exempt: the shield paints nothing. It is a transparent, fullscreen input
// catcher, so it has no ground for a backdrop to sit under, and it opens before
// the panel it guards -- a capture here would photograph the screen a second
// time for no one to look at.
func (r *Registry) shieldSpec(h *PanelHost) *wayland.AuxSpec {
	return &wayland.AuxSpec{
		ID:            shieldSurfaceID(h.id),
		Namespace:     "sysc-shell-shield",
		Layer:         layerOverlay,
		Anchor:        uint32(layershell.ZwlrLayerSurfaceV1AnchorTop | layershell.ZwlrLayerSurfaceV1AnchorBottom | layershell.ZwlrLayerSurfaceV1AnchorLeft | layershell.ZwlrLayerSurfaceV1AnchorRight),
		ExclusiveZone: -1,
		Keyboard:      keyboardNone,
		Callbacks: wayland.HostCallbacks{
			Configure: func(int, int, int) error { return nil },
			Render:    func([]byte, int, int, int) error { return nil },
			Handle: func(e wayland.Event) bool {
				if e.Kind == wayland.EventPointerPress {
					if time.Now().Before(h.shieldQuiet) {
						return false
					}
					r.ClosePanel(h.id)
					return true
				}
				return false
			},
		},
	}
}

// rootStyle picks the alpha this panel's root fill paints at.
//
// An attached panel normally resolves its root at the *bar's* opacity, because
// with nothing captured behind it the panel and the bar read as one joined
// ground and the detached panel alpha would composite a different colour.
//
// A backdrop makes them separate grounds again. The bar's alpha is fully opaque
// whenever the bar is, so keeping it would paint straight over the blur and
// throw the capture away -- which is exactly what the renderer's
// TestOpaqueRootHidesTheBackdrop asserts an opaque root does.
func (h *PanelHost) rootStyle(t Theme) render.Style {
	if !h.place.CenterY && h.backdrop == nil {
		return t.AttachedPanelStyle()
	}
	return t.PanelStyle()
}

func (r *Registry) panelSpec(h *PanelHost, m Margins) *wayland.AuxSpec {
	anchor := uint32(layershell.ZwlrLayerSurfaceV1AnchorTop | layershell.ZwlrLayerSurfaceV1AnchorLeft)
	if h.place.BarEdge == "bottom" {
		anchor = uint32(layershell.ZwlrLayerSurfaceV1AnchorBottom | layershell.ZwlrLayerSurfaceV1AnchorLeft)
	}
	// Where the panel's body will land on the output, in the same logical
	// coordinates the capture takes. It is computed before the fillet shifts
	// the surface left, because the body sits inset by exactly that much inside
	// the surface, so the two cancel.
	region := ui.Rect{X: m.Left, Y: m.Top, W: h.place.Panel.W, H: h.place.Panel.H}
	if h.place.BarEdge == "bottom" {
		region.Y = h.place.Output.H - m.Bottom - h.place.Panel.H
	}
	fillet := h.filletMargin()
	if fillet > 0 {
		m.Left -= fillet
	}
	opaque := h.theme.BackgroundOpaque()
	if fillet > 0 {
		// ponytail: omit the hint for fillet-expanded surfaces; add a body-aware
		// opaque-region API only if compositor profiling shows this matters.
		opaque = false
	}
	// Nil disables the capture entirely, so a shell with blur off pays none of
	// its cost rather than capturing and discarding.
	var blurRegion *ui.Rect
	if r.cfg.Theme.BlurBehind {
		blurRegion = &region
	}
	return &wayland.AuxSpec{
		ID:            panelSurfaceID(h.id),
		Namespace:     "sysc-shell-panel",
		Layer:         layerOverlay,
		Anchor:        anchor,
		MarginTop:     int32(m.Top),
		MarginBottom:  int32(m.Bottom),
		MarginLeft:    int32(m.Left),
		MarginRight:   int32(m.Right),
		Width:         int32(h.place.Panel.W + 2*fillet),
		Height:        int32(h.place.Panel.H),
		ExclusiveZone: -1,
		Keyboard:      keyboardExclusive,
		BlurRegion:    blurRegion,
		BlurRadius:    r.cfg.Theme.BlurRadius,
		Callbacks: wayland.HostCallbacks{
			// Delivered from the Wayland goroutine before the surface exists,
			// so it takes the registry lock exactly as Render does.
			Backdrop: func(img *ui.Image) {
				r.mu.Lock()
				defer r.mu.Unlock()
				h.backdrop = img
			},
			OpaqueBackground: opaque,
			Radius:           h.theme.Radius,
			Configure:        h.configureLocking(r),
			Render:           h.renderLocking(r),
			Handle:           h.handle(r),
			WantIME: func() bool {
				n := h.focused()
				return n != nil && n.Kind == ui.KindTextField
			},
			IBeamAt: func(x, y float64) bool {
				n := h.hitFocusable(int(math.Floor(x)), int(math.Floor(y)))
				return n != nil && n.Kind == ui.KindTextField
			},
		},
	}
}

// filletMargin is the per-side room the concave bar joint needs. It clamps to
// the gap between this panel's edge and the bar's, because a wedge wider than
// that margin paints past the bar it is meant to join. Floating panels
// (CenterY) do not attach, so they take no margin.
func (h *PanelHost) filletMargin() int {
	if h == nil || h.place.BarEdge == "" || h.place.CenterY {
		return 0
	}
	room := h.place.Padding - BarGap
	if h.id == PanelControlCenter {
		m := h.place.Margins()
		left := m.Left - BarGap
		right := h.place.Output.W - BarGap - (m.Left + h.place.Panel.W)
		room = min(left, right)
	}
	if room <= 0 {
		return 0
	}
	return min(h.theme.Fillet, room)
}

// panelFontFamily resolves the font of the output the panel opens on. A panel
// is per-output, so a connector with its own bar font must not open a panel in
// the global family.
func (r *Registry) panelFontFamily(output uint32) string {
	connector := ""
	if bar, ok := r.bars[output]; ok {
		connector = bar.connector()
	}
	return r.cfg.ForConnector(connector).FontFamily
}

func (h *PanelHost) ensureText() error {
	if h.text != nil {
		return nil
	}
	fonts, err := render.NewSystemFontMap(h.fontFamily, render.DefaultFontCacheDir())
	if err != nil {
		return err
	}
	h.text = render.NewTextRendererWithFontMap(fonts)
	return nil
}

// The configure and render callbacks take the registry lock, like handle
// already does. Panel geometry and the panel tree are written from relay
// goroutines — the launcher's result relay rebuilds an open panel, which
// re-lays it out at this geometry — so the compositor's callbacks cannot read
// or write it unlocked. rebuildPanel already runs under the lock and calls
// configure directly, which is why the locking wrapper is separate.
func (h *PanelHost) configureLocking(r *Registry) func(int, int, int) error {
	return func(w, height, scale120 int) error {
		r.mu.Lock()
		defer r.mu.Unlock()
		return h.configure(w, height, scale120)
	}
}

func (h *PanelHost) renderLocking(r *Registry) func([]byte, int, int, int) error {
	return func(pixels []byte, width, height, stride int) error {
		r.mu.Lock()
		defer r.mu.Unlock()
		return h.render(pixels, width, height, stride)
	}
}

func (h *PanelHost) configure(w, height, scale120 int) error {
	h.logicalW, h.logicalH, h.scale120 = w, height, scale120
	if err := h.ensureText(); err != nil {
		return err
	}
	box := ui.Rect{W: w, H: height}
	if margin := h.filletMargin(); margin > 0 && w >= h.place.Panel.W+2*margin {
		box = ui.Rect{X: margin, W: w - 2*margin, H: height}
	}
	if h.root != nil && h.root.Kind == ui.KindRow {
		return ui.Layout(h.root, box, h.measureText())
	}
	return ui.LayoutColumn(h.root, box, h.measureText())
}

func (h *PanelHost) measureText() ui.MeasureText {
	scale := ui.Scale120(h.scale120)
	if !scale.Valid() {
		scale = ui.ScaleUnit
	}
	style := h.theme.PanelStyle()
	style.Scale120 = scale
	return func(s string, attrs ui.TextAttrs) (int, int) {
		spec := render.SpecFor(style, attrs)
		if h.text != nil && spec.Size > 0 {
			mw, mh, err := h.text.Measure(s, spec, attrs.Tabular)
			if err == nil {
				return scale.Logical(mw), scale.Logical(mh)
			}
		}
		return len(s) * 8, 16
	}
}

func (h *PanelHost) panelReveal() (opacity float64, offsetY, fillet int) {
	if h == nil || h.anim == nil || !h.anim.has(panelSurfaceID(h.id), animVisible) {
		if h == nil {
			return 1, 0, 0
		}
		return 1, 0, h.theme.Fillet
	}
	key := panelSurfaceID(h.id)
	opacity = h.anim.PanelOpacity(key)
	offsetY = h.anim.PanelSlide(key)
	if h.place.BarEdge == "top" {
		offsetY = -offsetY
	}
	fillet = int(math.Round(float64(h.theme.Fillet) * opacity))
	return opacity, offsetY, fillet
}

func (h *PanelHost) render(pixels []byte, width, height, stride int) error {
	if err := h.ensureText(); err != nil {
		return err
	}
	c, err := render.NewCanvas(pixels, width, height, stride)
	if err != nil {
		return err
	}
	scale := ui.Scale120(h.scale120)
	if !scale.Valid() {
		scale = ui.ScaleUnit
	}
	body := ui.Rect{W: h.logicalW, H: h.logicalH}
	if body.W <= 0 || body.H <= 0 {
		body = ui.Rect{W: h.place.Panel.W, H: h.place.Panel.H}
	}
	if margin := h.filletMargin(); margin > 0 && body.W >= h.place.Panel.W+2*margin {
		body = ui.Rect{X: margin, W: h.place.Panel.W, H: body.H}
	}
	// Resolve the pointer state onto the tree that is about to be painted. The
	// painter consumes an immutable mask; nothing downstream mutates state.
	h.pointer.apply(h.root, h.anim)

	paintTheme := h.paintTheme()
	style := h.rootStyle(paintTheme)
	// Only a panel draws its own rim; the bar, toasts and tray surfaces
	// sit directly on the shared surface and leave it zero. A fused audio
	// panel paints no rim: it and the bar share Style.Background, and a
	// stroke would read as a seam.
	if h.id != PanelAudio {
		style.Rim = paintTheme.Outline
	}
	style.Scale120 = scale
	style.Body = body
	opacity, offsetY, fillet := h.panelReveal()
	style.Fillet = fillet
	if !h.place.CenterY {
		style.AttachEdge = h.place.BarEdge
	}
	style.Backdrop = h.backdrop
	page, viewport, pageProgress, pageOffset := h.controlCentrePageVisual()
	if page != nil && pageOffset != 0 {
		offsetNodeY(page, pageOffset)
	}
	err = render.Paint(c, h.root, h.text, style)
	if page != nil && pageOffset != 0 {
		offsetNodeY(page, -pageOffset)
	}
	if err != nil {
		return err
	}
	if viewport != nil && pageProgress < 1 {
		wash := style.RootFill()
		wash.A = uint8(math.Round(float64(wash.A) * (1 - pageProgress)))
		c.FillRounded(scale.PhysicalRect(viewport.Bounds), 0, wash)
	}
	if h.roving.Count > 0 {
		n := h.focus[h.roving.Index()]
		if n != nil && n.Bounds.W > 0 {
			// The ring follows the node's own silhouette rather than boxing a
			// stadium in square corners, and stays independent of hover: a
			// focused control that is not hovered still shows it.
			ring := scale.PhysicalRect(n.Bounds)
			radius := min(scale.Physical(h.theme.Radius), min(ring.W, ring.H)/2)
			if n.Kind == ui.KindTextField {
				// A field is a stadium, and it carries the focus itself: the
				// comment here used to say it painted its own focused well,
				// but nothing ever told the painter which field had focus, so
				// a focused search field looked exactly like an idle one.
				radius = min(ring.W, ring.H) / 2
			}
			c.StrokeRounded(ring, radius, max(scale.Physical(2), 2), h.theme.Accent)
		}
	}
	c.ApplySurfaceTransform(opacity, scale.Physical(offsetY))
	return nil
}

func (h *PanelHost) handle(r *Registry) func(wayland.Event) bool {
	return func(e wayland.Event) bool {
		r.mu.Lock()
		defer r.mu.Unlock()
		switch e.Kind {
		case wayland.EventKeyPress:
			return h.keyPress(r, e.Key)
		case wayland.EventIME:
			return h.applyIME(r, e)
		case wayland.EventPointerAxis:
			return h.scrollAxis(r, e)
		case wayland.EventKeyRelease:
			if e.Key == keyLeftShift {
				h.shift = false
			}
			return false
		case wayland.EventPointerEnter, wayland.EventPointerMotion:
			h.hoverX, h.hoverY = int(math.Floor(e.X)), int(math.Floor(e.Y))
			if h.drag.Source != nil {
				h.drag.Move(e.X, e.Y)
				return h.drag.Active()
			}
			if h.sliderDrag != nil {
				ui.SliderAt(h.sliderDrag, h.hoverX)
				return true
			}
			if h.scrollDrag != nil {
				ui.ScrollSetFromY(h.scrollDrag, h.hoverY)
				if h.logicalW > 0 {
					_ = h.configure(h.logicalW, h.logicalH, h.scale120)
				}
				return true
			}
			// Only a change of resolved target repaints. Movement inside the
			// control the pointer is already on resolves to the same key and
			// costs nothing.
			return h.pointerChanged(r, h.pointer.setHover(hoverKeyAt(h.root, h.hoverX, h.hoverY)))
		case wayland.EventPointerLeave:
			h.pressed = ""
			h.sliderDrag = nil
			h.scrollDrag = nil
			return h.pointerChanged(r, h.pointer.clear())
		case wayland.EventPointerPress:
			h.hoverX, h.hoverY = int(math.Floor(e.X)), int(math.Floor(e.Y))
			// The clear glyph sits inside the field's own bounds, so it is
			// resolved before any panel's hit testing gets a look at the point.
			if h.searchClearPress(r, e) {
				return true
			}
			if h.id == PanelWallpaper && h.wallpaperPointerPress(r, e) {
				return true
			}
			if h.id == PanelLauncher && h.launcherPointerPress(r, e) {
				return true
			}
			if s := scrollTrackAt(h.root, h.hoverX, h.hoverY); s != nil {
				h.scrollDrag = s
				ui.ScrollSetFromY(s, h.hoverY)
				if h.logicalW > 0 {
					_ = h.configure(h.logicalW, h.logicalH, h.scale120)
				}
				return true
			}
			if n := h.hitFocusable(h.hoverX, h.hoverY); n != nil {
				h.pressed = n.StableKey()
				h.pointerChanged(r, h.pointer.setPress(n.StableKey()))
				h.setFocus(n)
				if n.Kind == ui.KindDragSource {
					h.drag.Begin(n, e.X, e.Y)
				}
				if n.Kind == ui.KindSlider {
					ui.SliderAt(n, h.hoverX)
					h.sliderDrag = n
				}
				return true
			}
			return false
		case wayland.EventPointerRelease:
			h.hoverX, h.hoverY = int(math.Floor(e.X)), int(math.Floor(e.Y))
			if h.sliderDrag != nil {
				n := h.sliderDrag
				ui.SliderAt(n, h.hoverX)
				h.sliderDrag = nil
				h.pressed = ""
				if strings.HasPrefix(n.Action, "plugin-set:") {
					return r.handlePluginManager(h, n)
				}
				if strings.HasPrefix(n.Action, "audio-") {
					return h.applyAudioControl(r, n)
				}
				if h.id == PanelControlCenter && h.activateControlCentre(r, n) {
					return true
				}
				h.applySetting(r, n)
				return true
			}
			if h.scrollDrag != nil {
				h.scrollDrag = nil
				return true
			}
			if h.drag.Active() {
				zone := ui.FindDropZone(h.root, &h.drag)
				payload, ok := h.drag.Drop(zone)
				h.drag.Cancel()
				if ok && zone != nil {
					return r.deliverPluginText(zone.Action, payload, v1.EventDrop)
				}
				return true
			}
			n := h.hitFocusable(h.hoverX, h.hoverY)
			pressed := h.pressed
			h.pressed = ""
			cleared := h.pointerChanged(r, h.pointer.setPress(""))
			if n != nil && pressed != "" && n.StableKey() == pressed {
				return h.activate(r)
			}
			return cleared
		}
		return false
	}
}

func (h *PanelHost) keyPress(r *Registry, key uint32) bool {
	if h.menu != nil && h.menu.Opened() {
		if !h.menu.Handle(key) {
			return false
		}
		if !h.menu.Opened() && key != keyEsc {
			if h.id == PanelLauncher {
				h.applyLauncherMenu(r)
			} else if strings.HasPrefix(h.menuPath, "plugin-set:") {
				_ = r.handlePluginManager(h, &ui.Node{
					Kind: ui.KindMenu, Action: h.menuPath, Text: h.menu.Value(),
				})
			} else {
				h.applyMenu(r, h.menuPath)
			}
		}
		r.rebuildPanel(h)
		return true
	}
	if key == keyBackspace {
		return h.editField(r, func(f *ui.Field) { f.Backspace() })
	}
	// Space is both printable text and the accept key. Only a focused text
	// field consumes it as text, so fall through rather than return when the
	// edit does not land -- otherwise no control is ever activatable by
	// keyboard, because every accept press is swallowed here.
	if ch, ok := ui.EvdevText(key, h.shift); ok {
		if h.editField(r, func(f *ui.Field) { f.Insert(ch) }) {
			return true
		}
	}
	if h.id == PanelWallpaper && h.wallpaperKeyPress(r, key) {
		return true
	}
	if h.id == PanelLauncher && h.launcherKeyPress(r, key) {
		return true
	}
	if h.id == PanelClipboard && h.clipboardKeyPress(r, key) {
		return true
	}
	switch key {
	case keyLeftShift:
		h.shift = true
		return false
	case keyEsc:
		if h.id == PanelSettings && h.query != "" {
			h.query = ""
			h.search = ui.NewField("")
			r.rebuildPanel(h)
			return true
		}
		r.closePanelLocked(h.id)
		return true
	case keyTab:
		if h.shift {
			h.roving.Prev()
		} else {
			h.roving.Next()
		}
		h.afterFocusChange(r)
		return true
	case keyLeft, keyUp:
		if h.adjustSlider(r, keyLeft) {
			return true
		}
		h.roving.Prev()
		h.afterFocusChange(r)
		return true
	case keyRight, keyDown:
		if h.adjustSlider(r, keyRight) {
			return true
		}
		h.roving.Next()
		h.afterFocusChange(r)
		return true
	case keyHome, keyEnd:
		if h.adjustSlider(r, key) {
			return true
		}
		if key == keyHome {
			return h.scrollTo(0)
		}
		return h.scrollTo(1 << 30)
	case keyPageUp:
		return h.scrollBy(-max(h.logicalH, 1))
	case keyPageDown:
		return h.scrollBy(max(h.logicalH, 1))
	case keySpace, keyEnter:
		return h.activate(r)
	}
	return false
}

func (h *PanelHost) scrollAxis(r *Registry, e wayland.Event) bool {
	delta := 0
	switch {
	case e.AxisDiscrete != 0:
		delta = int(e.AxisDiscrete) * 40
	case e.AxisValue120 != 0:
		delta = int(e.AxisValue120) * 40 / 120
	default:
		delta = int(e.AxisValue)
	}
	if delta == 0 {
		return false
	}
	if h.id == PanelLauncher {
		rows := delta / launcherSlotHeight
		if rows == 0 {
			if delta > 0 {
				rows = 1
			} else {
				rows = -1
			}
		}
		h.launcherMoveSel(r, rows)
		return true
	}
	return h.scrollBy(delta)
}

func (h *PanelHost) scrollBy(delta int) bool {
	s := scrollAt(h.root, h.hoverX, h.hoverY)
	if s == nil {
		return false
	}
	ui.ScrollBy(s, delta)
	if h.id == PanelLauncher {
		h.launcherScroll = s.ScrollOffset
	}
	if h.logicalW > 0 {
		_ = h.configure(h.logicalW, h.logicalH, h.scale120)
	}
	return true
}

func (h *PanelHost) scrollTo(off int) bool {
	s := findScroll(h.root)
	if s == nil {
		return false
	}
	s.ScrollOffset = off
	ui.ScrollBy(s, 0)
	if h.logicalW > 0 {
		_ = h.configure(h.logicalW, h.logicalH, h.scale120)
	}
	return true
}

func findScroll(n *ui.Node) *ui.Node {
	if n == nil {
		return nil
	}
	if n.Kind == ui.KindScroll || n.Kind == ui.KindVirtualList {
		return n
	}
	for _, c := range n.Children {
		if got := findScroll(c); got != nil {
			return got
		}
	}
	return nil
}

// scrollAt is the scrollable region the wheel acts on: the deepest one under
// the pointer, or the first in the tree when the pointer is over none.
//
// The fallback is what findScroll alone used to do, and on a panel with one
// scrollable the two agree. They stop agreeing as soon as a panel has two --
// the wallpaper picker has a folder list above its tile grid -- because tree
// order then hands every wheel event to whichever happens to be built first,
// and no amount of clicking in the other one can change that.
//
// Deepest wins because scrollables nest: a list inside a scrolled region is
// the one the pointer is really over.
func scrollAt(root *ui.Node, x, y int) *ui.Node {
	if hit := deepestScrollAt(root, x, y); hit != nil {
		return hit
	}
	return findScroll(root)
}

// scrollTrackAt is the scrollable whose scrollbar track the pointer is on.
// Like scrollAt, this has to consider every scrollable rather than the first:
// a track belongs to one region, and testing only one of two means the other
// region's bar cannot be dragged at all.
func scrollTrackAt(root *ui.Node, x, y int) *ui.Node {
	var hit *ui.Node
	forEachScroll(root, func(s *ui.Node) {
		if hit == nil && ui.ScrollTrack(s).Contains(x, y) {
			hit = s
		}
	})
	return hit
}

func forEachScroll(n *ui.Node, fn func(*ui.Node)) {
	if n == nil {
		return
	}
	if n.Kind == ui.KindScroll || n.Kind == ui.KindVirtualList {
		fn(n)
	}
	for _, c := range n.Children {
		forEachScroll(c, fn)
	}
}

func deepestScrollAt(n *ui.Node, x, y int) *ui.Node {
	if n == nil || !n.Bounds.Contains(x, y) {
		return nil
	}
	for _, c := range n.Children {
		if got := deepestScrollAt(c, x, y); got != nil {
			return got
		}
	}
	if n.Kind == ui.KindScroll || n.Kind == ui.KindVirtualList {
		return n
	}
	return nil
}

func (h *PanelHost) applyIME(r *Registry, e wayland.Event) bool {
	return h.editField(r, func(f *ui.Field) {
		f.DeleteSurrounding(int(e.IMEDeleteBefore), int(e.IMEDeleteAfter))
		if e.IMECommit != "" {
			f.Commit(e.IMECommit)
		}
		f.Preedit(e.IMEPreedit)
	})
}

// searchClearPress empties a search well when the press lands on the trailing
// clear glyph paintTextField draws in it. It is here rather than in one
// panel's own handler because the glyph is painted by shared chrome: the
// launcher, the settings search and the wallpaper picker all name their field
// "Search", so wiring it to one would leave a live affordance dead in the
// other two.
func (h *PanelHost) searchClearPress(r *Registry, e wayland.Event) bool {
	if e.Button != btnLeft {
		return false
	}
	n := searchClearAt(h.root, h.hoverX, h.hoverY)
	if n == nil {
		return false
	}
	// editField edits whatever is focused, and a press on the well is a press
	// on the field whether or not it already held focus.
	h.setFocus(n)
	return h.editField(r, func(f *ui.Field) { f.Clear() })
}

// searchClearAt finds the field whose clear glyph covers the point. A text
// field is a leaf, so the glyph leaves nothing in the tree to hit; render owns
// where it was drawn and answers for it here too.
func searchClearAt(n *ui.Node, x, y int) *ui.Node {
	if n == nil {
		return nil
	}
	if render.SearchClearBox(n).Contains(x, y) {
		return n
	}
	for _, c := range n.Children {
		if got := searchClearAt(c, x, y); got != nil {
			return got
		}
	}
	return nil
}

func (h *PanelHost) editField(r *Registry, fn func(*ui.Field)) bool {
	n := h.focused()
	if n == nil || n.Kind != ui.KindTextField {
		return false
	}
	var f *ui.Field
	if n.Action == "bluetooth-prompt-input" && bluetoothBodyVisible(h) {
		if h.bluetoothInput == nil {
			h.bluetoothInput = ui.NewField("")
			h.bluetoothInput.Masked = true
		}
		h.bluetoothInput.SyncFrom(n)
		f = h.bluetoothInput
	} else if h.id == PanelNetwork && n.Name == "Password" {
		if h.password == nil {
			h.password = ui.NewField("")
			h.password.Masked = true
		}
		h.password.SyncFrom(n)
		f = h.password
	} else if n.Name == "Search" {
		if h.search == nil {
			h.search = ui.NewField("")
		}
		h.search.SyncFrom(n)
		f = h.search
	} else if _, ok := parsePluginAction(n.Action); ok {
		k := n.StableKey()
		if h.editors == nil {
			h.editors = map[string]*retainedEditor{}
		}
		slot := h.editors[k]
		if slot == nil {
			slot = &retainedEditor{field: &ui.Field{
				Text: n.Text, PreeditText: n.Preedit, Cursor: n.Cursor,
				Multiline: n.Multiline, SubmitOnEnter: n.SubmitOnEnter,
			}, reseed: n.Reseed}
			h.editors[k] = slot
		}
		slot.field.SyncFrom(n)
		f = slot.field
	} else if store := pluginSettingStoreKey(n.Action); store != "" {
		if h.fields == nil {
			h.fields = map[string]*ui.Field{}
		}
		f = h.fields[store]
		if f == nil {
			f = &ui.Field{Text: n.Text, PreeditText: n.Preedit, Cursor: n.Cursor}
			h.fields[store] = f
		} else {
			f.SyncFrom(n)
		}
	} else {
		path, _ := strings.CutPrefix(n.Action, "set:")
		if h.fields == nil {
			h.fields = map[string]*ui.Field{}
		}
		f = h.fields[path]
		if f == nil {
			f = &ui.Field{Text: n.Text, PreeditText: n.Preedit, Cursor: n.Cursor}
			h.fields[path] = f
		} else {
			f.SyncFrom(n)
		}
	}
	fn(f)
	f.SyncTo(n)
	if _, ok := parsePluginAction(n.Action); ok {
		r.deliverPluginText(n.Action, n.Text, v1.EventChange)
		return true
	}
	if n.Action == "bluetooth-prompt-input" {
		r.rebuildPanel(h)
		return true
	}
	if n.Name == "Search" {
		h.query = f.Text
		if h.id == PanelLauncher {
			h.launcherSel = 0
			h.launcherScroll = 0
			r.launcherServiceLocked().Query(h.query)
		}
		idx := h.roving.Index()
		r.rebuildPanel(h)
		h.roving.Set(idx)
		return true
	}
	if strings.HasPrefix(n.Action, "plugin-set:") {
		return r.handlePluginManager(h, n)
	}
	h.applySetting(r, n)
	return true
}

// metrics is the density row a tree builds against. The receiver may be nil:
// a few unit tests compose a subtree without a host, and a panel that cannot
// name its density should still lay out on the default row rather than on
// zeroes.
func (h *PanelHost) metrics() theme.Metrics {
	if h == nil {
		m, _ := theme.MetricsFor(theme.DensityDefault)
		return m
	}
	return h.theme.Metrics
}

func (h *PanelHost) focused() *ui.Node {
	if h.roving.Count == 0 {
		return nil
	}
	return h.focus[h.roving.Index()]
}

func (h *PanelHost) adjustSlider(r *Registry, key uint32) bool {
	n := h.focused()
	if n == nil || n.Kind != ui.KindSlider {
		return false
	}
	if !ui.ControlKey(n, key) {
		return false
	}
	if strings.HasPrefix(n.Action, "plugin-set:") {
		return r.handlePluginManager(h, n)
	}
	if strings.HasPrefix(n.Action, "audio-") {
		return h.applyAudioControl(r, n)
	}
	if h.id == PanelControlCenter && h.activateControlCentre(r, n) {
		return true
	}
	h.applySetting(r, n)
	return true
}

func (h *PanelHost) activate(r *Registry) bool {
	n := h.focused()
	if n == nil || n.State.Has(ui.StateDisabled) {
		return false
	}
	if h.id == PanelClipboard {
		return h.activateClipboard(r, n)
	}
	switch n.Action {
	case "plugin-close", "plugin-retry", "plugin-disable":
		if r.plugins != nil {
			r.plugins.retryOrDisable(n.Action)
		}
		return true
	}
	if _, ok := parsePluginAction(n.Action); ok {
		if n.Kind == ui.KindTextField {
			return r.deliverPluginText(n.Action, n.Text, v1.EventSubmit)
		}
		return r.handlePluginBar(n.Action, wayland.Event{Kind: wayland.EventPointerRelease, Button: 272})
	}
	if strings.HasPrefix(n.Action, "plugin-set:") {
		switch n.Kind {
		case ui.KindToggle:
			ui.Activate(n)
			return r.handlePluginManager(h, n)
		case ui.KindText:
			for _, f := range h.focus {
				if f != nil && f.Kind == ui.KindToggle && f.Action == n.Action {
					ui.Activate(f)
					return r.handlePluginManager(h, f)
				}
			}
			return false
		case ui.KindMenu:
			store := pluginSettingStoreKey(n.Action)
			if m := h.menus[store]; m != nil {
				h.menu = m
				h.menuPath = n.Action
				if !m.Opened() {
					m.Open()
					r.rebuildPanel(h)
					return true
				}
				m.PickAt(n, h.hoverX, h.hoverY)
				m.Select()
				n.Text = m.Value()
				return r.handlePluginManager(h, n)
			}
			return false
		default:
			return r.handlePluginManager(h, n)
		}
	}
	if r.handlePluginManager(h, n) {
		return true
	}
	if strings.HasPrefix(n.Action, "bluetooth-") && bluetoothBodyVisible(h) {
		return h.activateBluetooth(r, n)
	}
	if h.id == PanelLauncher {
		return h.activateLauncher(r, n)
	}
	if h.id == PanelControlCenter && h.activateControlCentre(r, n) {
		return true
	}
	if n.Kind == ui.KindToggle {
		changed := ui.Activate(n)
		h.applySetting(r, n)
		return changed
	}
	if n.Kind == ui.KindMenu {
		path, _ := strings.CutPrefix(n.Action, "set:")
		if m := h.menus[path]; m != nil {
			h.menu = m
			h.menuPath = path
			if !m.Opened() {
				m.Open()
				r.rebuildPanel(h)
				return true
			}
			m.PickAt(n, h.hoverX, h.hoverY)
			m.Select()
			h.applyMenu(r, path)
			r.rebuildPanel(h)
			return true
		}
		if h.menu != nil && !h.menu.Opened() {
			h.menu.Open()
			return true
		}
		return false
	}
	if strings.HasPrefix(n.Action, "wallpaper") && h.wallpaperAction(r, n) {
		return true
	}
	if strings.HasPrefix(n.Action, "audio-") && h.applyAudioControl(r, n) {
		return true
	}
	// applyNetworkControl returns false for "network-close", which the shared
	// close path below handles.
	if strings.HasPrefix(n.Action, "network-") && h.applyNetworkControl(r, n) {
		return true
	}
	if h.id == PanelMonitor && h.activateMonitor(r, n) {
		return true
	}
	if strings.HasPrefix(n.Action, "weather-view:") {
		h.weatherView = strings.TrimPrefix(n.Action, "weather-view:")
		r.rebuildPanel(h)
		return true
	}
	if n.Action == "audio-close" || n.Action == "network-close" || n.Action == "weather-close" {
		r.closePanelLocked(h.id)
		return true
	}
	if strings.HasPrefix(n.Action, "notify:") {
		return h.activateNotify(r, n)
	}
	if strings.HasPrefix(n.Action, "section:") {
		section := strings.TrimPrefix(n.Action, "section:")
		if h.id == PanelControlCenter {
			return h.selectControlCentreSection(r, section)
		}
		h.section = section
		r.rebuildPanel(h)
		return true
	}
	if path, ok := strings.CutPrefix(n.Action, "goto:"); ok {
		if e := h.set.ByPath(path); e != nil {
			h.section = e.Section
			h.query = ""
			h.search = ui.NewField("")
			r.rebuildPanel(h)
			h.focusByName(e.Label)
		}
		return true
	}
	if path, ok := strings.CutPrefix(n.Action, "reset:"); ok {
		if e := h.set.ByPath(path); e != nil && e.Default != nil {
			h.commitSetting(r, e, e.Default(h.draft))
			r.rebuildPanel(h)
		}
		return true
	}
	if name, ok := strings.CutPrefix(n.Action, "profile:"); ok {
		r.setSessionProfile(h, name)
		return true
	}
	h.lastAction = n.Action
	switch n.Action {
	case "cal-prev":
		h.monthDelta--
		r.rebuildPanel(h)
	case "cal-next":
		h.monthDelta++
		r.rebuildPanel(h)
	case "session-lock", "session-logout", "session-suspend", "session-reboot", "session-poweroff":
		r.runSessionAction(h, n.Action)
	}
	return true
}

func (h *PanelHost) activateNotify(r *Registry, n *ui.Node) bool {
	action := n.Action
	h.lastAction = action
	if rest, ok := strings.CutPrefix(action, "notify:center:"); ok {
		switch {
		case rest == "clear":
			r.clearVisible(h)
		case rest == "settings":
			trig := Trigger{BarEdge: h.place.BarEdge, BarZone: h.place.BarZone, OutW: h.place.Output.W, OutH: h.place.Output.H}
			if where, ok := r.panels.Output(PanelSettings); ok && where == h.output {
				r.closePanelLocked(PanelSettings)
			} else {
				_ = r.openPanelRootLocked(PanelSettings, h.output, trig)
			}
		case rest == "close":
			r.closePanelLocked(h.id)
		case rest == "dnd":
			_, on := r.notify.dndState(r.clockNow())
			r.notify.setDND(!on)
			if r.toasts != nil {
				r.toasts.recompute()
			}
			r.rebuildPanel(h)
		case rest == "schedule":
			h.notifyMenu = !h.notifyMenu
			r.rebuildPanel(h)
		case strings.HasPrefix(rest, "filter:"):
			h.notifyFilter = strings.TrimPrefix(rest, "filter:")
			r.rebuildPanel(h)
		case strings.HasPrefix(rest, "expand:"):
			key := strings.TrimPrefix(rest, "expand:")
			if h.notifyExpand == key {
				h.notifyExpand = ""
			} else {
				h.notifyExpand = key
			}
			r.rebuildPanel(h)
		case strings.HasPrefix(rest, "dismiss-group:"):
			key := strings.TrimPrefix(rest, "dismiss-group:")
			for _, id := range r.notify.idsForGroup(key, h.notifyFilter, r.clockNow()) {
				r.sendNotify(protocol.Command{Kind: protocol.CommandDismiss, ID: id})
			}
		case strings.HasPrefix(rest, "preset:"):
			id := strings.TrimPrefix(rest, "preset:")
			now := r.clockNow()
			if d, untilOff, ok := dndPresetDuration(id, now); ok {
				if untilOff {
					r.notify.setDND(true)
				} else {
					r.notify.setDNDPreset(now, d)
				}
				if r.toasts != nil {
					r.toasts.recompute()
				}
			}
			h.notifyMenu = false
			r.rebuildPanel(h)
		}
		return true
	}
	id, parts, ok := parseCardAction(action)
	if !ok || len(parts) == 0 {
		return true
	}
	switch parts[0] {
	case "dismiss":
		r.sendNotify(protocol.Command{Kind: protocol.CommandDismiss, ID: id})
	case "remove":
		r.sendNotify(protocol.Command{Kind: protocol.CommandHistoryRemove, IDs: []uint32{id}})
	case "default":
		r.sendNotify(protocol.Command{Kind: protocol.CommandAction, ID: id, ActionKey: "default"})
	case "action":
		if len(parts) == 2 {
			r.sendNotify(protocol.Command{Kind: protocol.CommandAction, ID: id, ActionKey: parts[1]})
		}
	}
	return true
}

// clearVisible clears exactly what the open filter shows. With the tabs gone
// there is no other unambiguous target: a Clear that emptied the whole store
// while the user was looking at Yesterday would delete what they cannot see.
func (r *Registry) clearVisible(h *PanelHost) {
	now := r.clockNow()
	filter := "all"
	if h != nil && h.notifyFilter != "" {
		filter = h.notifyFilter
	}

	r.notify.mu.Lock()
	var dismiss, remove []uint32
	for _, n := range r.notify.active {
		if historyFilter(filter, n.Timestamp, now) {
			dismiss = append(dismiss, n.ID)
		}
	}
	for _, e := range r.notify.history {
		if historyFilter(filter, e.Timestamp, now) {
			remove = append(remove, e.ID)
		}
	}
	r.notify.mu.Unlock()

	for _, id := range dismiss {
		r.sendNotify(protocol.Command{Kind: protocol.CommandDismiss, ID: id})
	}
	if len(remove) > 0 {
		r.sendNotify(protocol.Command{Kind: protocol.CommandHistoryRemove, IDs: remove})
	}
}

func (h *PanelHost) afterFocusChange(r *Registry) {
	if h.id == PanelMonitor {
		r.rebuildPanel(h)
		if revealFocusedProcess(h) && h.logicalW > 0 {
			_ = h.configure(h.logicalW, h.logicalH, h.scale120)
		}
	}
	if h.id == PanelClipboard {
		h.clipboardSelectionFromFocus()
		r.rebuildPanel(h)
	}
}

func (r *Registry) rebuildPanel(h *PanelHost) {
	idx := h.roving.Index()
	focusedKey := ""
	if h.id == PanelClipboard {
		focusedKey = h.focused().StableKey()
	}
	h.root = r.panelTree(h)
	if h.id == PanelPlugin {
		if h.editors == nil {
			h.editors = map[string]*retainedEditor{}
		}
		overlayEditors(h.root, h.editors)
	}
	h.focus = ui.Focusables(h.root)
	h.roving.Count = len(h.focus)
	h.roving.Set(idx)
	if focusedKey != "" {
		for i, n := range h.focus {
			if n != nil && n.StableKey() == focusedKey {
				h.roving.Set(i)
				break
			}
		}
	}
	if h.id == PanelNotifications {
		r.syncNotificationsSize(h)
	}
	if h.logicalW > 0 {
		_ = h.configure(h.logicalW, h.logicalH, h.scale120)
	}
}

func (r *Registry) panelTree(h *PanelHost) *ui.Node {
	switch h.id {
	case PanelClock:
		now := r.now
		if now.IsZero() {
			now = time.Now()
		}
		return clockTree(now, h.monthDelta, h.theme)
	case PanelMonitor:
		connector := ""
		if bar, ok := r.bars[h.output]; ok {
			connector = bar.connector()
		}
		return monitorPanelTree(h, monitorSelectors(r.cfg.ForConnector(connector)), r.sample, r.historyLocked(), readMachineFacts())
	case PanelSession:
		return sessionTree(h, r.sample, r.cfg.Session.Locker)
	case PanelSettings:
		return settingsTree(r, h)
	case PanelLauncher:
		return launcherTree(r, h)
	case PanelWallpaper:
		return wallpaperTree(r, h)
	case PanelPlugin:
		if r.plugins != nil {
			return r.plugins.panelTree(h)
		}
		return pluginPanelError("starting", false)
	case PanelNotifications:
		return r.centerTreeFor(h)
	case PanelAudio:
		return audioTree(r, h)
	case PanelControlCenter:
		return controlCentreTree(r, h)
	case PanelNetwork:
		return networkTree(r, h)
	case PanelBluetooth:
		return bluetoothTree(r, h)
	case PanelWeather:
		return weatherTree(r, h)
	case PanelClipboard:
		return clipboardTree(r, h)
	default:
		return placeholderTree()
	}
}

func panelTargetSize(id PanelID) ui.Rect {
	switch id {
	case PanelClock:
		return ui.Rect{W: 360, H: 420}
	case PanelMonitor:
		return ui.Rect{W: 640, H: 720}
	case PanelSettings:
		// Width is unchanged on purpose: the narrowest-width acceptance check
		// lays this panel out at its target, and holding width leaves that
		// premise intact while the plain column takes the vertical room that
		// descriptions and group headings need. FittedSize clamps on a short
		// output.
		return ui.Rect{W: 900, H: 760}
	case PanelLauncher:
		// 700 is DMS spotlight's own height. FittedSize caps this to the
		// output before placement, so a short screen clamps rather than
		// overflowing -- the lesson the wallpaper picker paid for.
		return ui.Rect{W: 560, H: 700}
	case PanelSession:
		return ui.Rect{W: 420, H: 360}
	case PanelPlugin:
		// Fallback when no plugin view has declared a size yet.
		return ui.Rect{W: 320, H: 280}
	case PanelNotifications:
		return ui.Rect{W: 416, H: 300}
	case PanelWallpaper:
		// The plugin picker's size, not native Noctalia's 980x700. A short
		// output clamps it through Placement.FittedSize (D2).
		return ui.Rect{W: 980, H: 1100}
	case PanelAudio:
		return audioPanelSize(1920, 1080)
	case PanelControlCenter:
		return ui.Rect{W: 700, H: 564}
	case PanelNetwork:
		// Fixed rather than a fraction of the output: the content is a list of
		// SSID rows, whose comfortable width does not scale with the screen.
		return ui.Rect{W: 460, H: 560}
	case PanelBluetooth:
		return ui.Rect{W: 460, H: 560}
	case PanelWeather:
		// The network and Bluetooth sibling size: a fixed panel whose content
		// does not scale with the screen.
		return ui.Rect{W: 460, H: 560}
	case PanelClipboard:
		return ui.Rect{W: 720, H: 560}
	default:
		return ui.Rect{W: 280, H: 200}
	}
}

func audioPanelSize(outputW, outputH int) ui.Rect {
	return ui.Rect{
		W: min(max(outputW/3, 720), 1120),
		H: min(max(2*outputH/3, 640), 992),
	}
}

// monitorSurfaceHeight is the tree's intrinsic height plus two radii of
// empty chrome so the rounded bottom clears the last row. One radius still
// clipped Uptime on the 1.25 laptop.
func monitorSurfaceHeight(root *ui.Node, width, radius int, measure ui.MeasureText) int {
	fallback := panelTargetSize(PanelMonitor).H
	if root == nil || measure == nil {
		return fallback
	}
	ht, err := ui.ContentHeight(root, width, measure)
	if err != nil || ht <= 0 {
		return fallback
	}
	if radius < 0 {
		radius = 0
	}
	return ht + 2*radius
}

func notificationsSurfaceHeight(h *PanelHost) int {
	root := h.root
	if root != nil {
		copyRoot := *root
		copyRoot.Children = append([]*ui.Node(nil), root.Children...)
		for i, child := range copyRoot.Children {
			if child == nil || child.Kind != ui.KindScroll {
				continue
			}
			// Measure the body as ordinary content before assigning its viewport.
			// A scroll node's Height is the viewport, not the height of its cards.
			copyRoot.Children[i] = &ui.Node{
				Kind: ui.KindColumn, Gap: child.Gap, Padding: child.Padding, Children: child.Children,
			}
			break
		}
		root = &copyRoot
	}
	ht := monitorSurfaceHeight(root, h.place.Panel.W, h.theme.Radius, h.measureText())
	maxH := min(h.place.Output.H*8/10, 648)
	return max(300, min(ht, maxH))
}

func (r *Registry) syncNotificationsSize(h *PanelHost) {
	_ = h.ensureText()
	h.place.Panel.H = notificationsSurfaceHeight(h)
	w, hgt := h.place.FittedSize()
	h.place.Panel.W, h.place.Panel.H = w, hgt
}

func (h *PanelHost) applySetting(r *Registry, n *ui.Node) {
	if h.set == nil || n == nil {
		return
	}
	path, ok := strings.CutPrefix(n.Action, "set:")
	if !ok {
		return
	}
	e := h.set.ByPath(path)
	if e == nil {
		return
	}
	var v string
	switch n.Kind {
	case ui.KindToggle:
		v = "false"
		if n.Value != 0 {
			v = "true"
		}
	case ui.KindSlider:
		v = strconv.Itoa(int(n.Value))
	case ui.KindTextField:
		v = n.Text
	case ui.KindMenu:
		v = n.Text
	}
	h.commitSetting(r, e, v)
}

// commitSetting applies one value to the draft, rebuilds the registry from it,
// and writes. The rebuild is what lets an entry's options depend on another
// setting: the seed picker follows the theme source, and a registry built once
// at open would keep offering the previous source's vocabulary for as long as
// the panel stayed up.
func (h *PanelHost) commitSetting(r *Registry, e *settings.Entry, v string) {
	if err := e.Set(&h.draft, v); err != nil {
		h.errLabel = err.Error()
		r.rebuildPanel(h)
		return
	}
	h.set = settings.DefaultFor(h.draft)
	h.persistDraft(r)
}

func (h *PanelHost) applyMenu(r *Registry, path string) {
	if h.set == nil || path == "" {
		return
	}
	e := h.set.ByPath(path)
	m := h.menus[path]
	if e == nil || m == nil {
		return
	}
	h.commitSetting(r, e, m.Value())
}

func (h *PanelHost) focusByName(name string) {
	for i, n := range h.focus {
		if n != nil && n.Name == name {
			h.roving.Set(i)
			return
		}
	}
}

func (h *PanelHost) setFocus(n *ui.Node) {
	for i, f := range h.focus {
		if f == n {
			h.roving.Set(i)
			return
		}
	}
}

func (h *PanelHost) hitFocusable(x, y int) *ui.Node {
	for i := len(h.focus) - 1; i >= 0; i-- {
		n := h.focus[i]
		if n.Bounds.Contains(x, y) {
			return n
		}
	}
	return nil
}

// paintTheme is the palette this frame paints with: the published theme once a
// change has settled, or a blend of the outgoing and incoming palettes while
// one is in flight.
func (h *PanelHost) paintTheme() Theme {
	key := panelSurfaceID(h.id)
	if h.anim == nil || !h.anim.has(key, animTheme) {
		return h.theme
	}
	p := h.anim.Value(key, animTheme)
	if p >= 1 {
		return h.theme
	}
	return h.themeFrom.LerpColors(h.theme, p)
}

// retheme moves this surface onto a new palette, crossfading from whatever it
// is currently rendering. A reload that lands mid-fade therefore continues from
// the colours on screen rather than snapping back to the palette it was leaving.
func (h *PanelHost) retheme(next Theme) {
	if h.theme == next {
		return
	}
	h.themeFrom = h.paintTheme()
	h.theme = next
	if h.anim == nil {
		return
	}
	key := panelSurfaceID(h.id)
	// Restart from zero so the blend runs the whole way from what is rendering.
	h.anim.Reset(key, animTheme)
	h.anim.Target(key, animTheme, 1)
}

func (h *PanelHost) stopAnimation() {
	h.stopOnce.Do(func() { close(h.stopAnim) })
}

// scheduleSurfaceFrames drives this surface's one clock. It publishes a frame
// per tick while any value on the surface is unsettled and returns as soon as
// they all are, so an idle shell schedules nothing. A loop already running is
// left alone: a second target change joins the clock rather than starting a
// second ticker.
func (r *Registry) startSurfaceFrames(h *PanelHost) {
	if h.anim == nil || h.anim.running ||
		(h.anim.Settled() && !mediaPageFramesWantedLocked(r, h)) {
		return
	}
	h.anim.running = true
	go r.surfaceFrameLoop(h)
}

// scheduleSurfaceFrames starts the clock and, when there is nothing to animate,
// publishes the one frame the surface still needs. Reduced motion takes that
// second path.
func (r *Registry) scheduleSurfaceFrames(h *PanelHost) {
	r.startSurfaceFrames(h)
	if h.anim == nil || h.anim.Settled() {
		r.publishSurface(h.output, panelSurfaceID(h.id))
	}
}

// pointerChanged aims the surface clock at a newly resolved pointer state and
// starts frames if that put anything in flight. It reports whether the caller
// should invalidate, so an unchanged target stays silent.
func (h *PanelHost) pointerChanged(r *Registry, changed bool) bool {
	if !changed {
		return false
	}
	h.pointer.apply(h.root, h.anim)
	r.startSurfaceFrames(h)
	return true
}

func (r *Registry) surfaceFrameLoop(h *PanelHost) {
	defer func() {
		r.mu.Lock()
		h.anim.running = false
		r.mu.Unlock()
	}()
	// Resolve the cap once, under the lock: a theme reload can replace the
	// animator, and the call expression below runs unlocked.
	r.mu.Lock()
	frameCap := h.anim.frameCap()
	r.mu.Unlock()
	animateSurface(h.stopAnim, func() bool {
		r.mu.Lock()
		defer r.mu.Unlock()
		return h.anim.Settled() && !mediaPageFramesWantedLocked(r, h)
	}, func() {
		r.mu.Lock()
		if r.panelHosts[h.id] == h && mediaBodyVisible(h) {
			r.rebuildPanel(h)
		}
		out := h.output
		r.mu.Unlock()
		r.publishSurface(out, panelSurfaceID(h.id))
	}, frameCap)
}

func (r *Registry) teardownPanelLocked(id PanelID) {
	if id == PanelNotifications {
		r.setCenterOpen(false)
	}
	if id == PanelNetwork && r.network != nil {
		r.network.CancelSecret()
	}
	h := r.panelHosts[id]
	if h == nil {
		return
	}
	if bluetoothBodyVisible(h) {
		r.stopBluetoothDiscoveryLocked(h)
		r.cancelBluetoothPromptLocked(h)
	}
	if h.mediaLease != nil {
		r.leaveMediaBodyLocked(h)
	}
	h.stopAnimation()
	h.drag.Cancel()
	if id == PanelNetwork {
		h.clearNetworkSecret()
	}
	if id == PanelClipboard {
		h.clipboardThumbnails = nil
		h.clipboardThumbnailRequest = nil
		h.clipboardSelectedID = ""
		h.clipboardConfirmScope = ""
		h.clipboardDeleteConfirmID = ""
	}
	delete(r.panelHosts, id)
	r.sendAux(wayland.AuxRequest{Output: h.output, ID: panelSurfaceID(id)})
	r.sendAux(wayland.AuxRequest{Output: h.output, ID: shieldSurfaceID(id)})
	releaseAll(h.leases)
	h.leases = nil
	if h.mixerLease != nil {
		lease := h.mixerLease
		h.mixerLease = nil
		// ponytail: only the audio panel owns this potentially blocking poller.
		// If more panel resources gain blocking teardown, return cleanup work
		// from this locked method and drain it through one shared path.
		go lease.Release()
	}
	h.editors = nil
}

func (r *Registry) sendAux(req wayland.AuxRequest) {
	select {
	case r.aux <- req:
	case <-r.closed:
	}
}

func (r *Registry) publishSurface(global uint32, surfaceID string) {
	select {
	case r.invalidations <- wayland.Invalidation{Global: global, SurfaceID: surfaceID}:
	case <-r.closed:
	default:
		// Drop when the owner is behind rather than stalling the caller.
	}
}

func (r *Registry) runSessionAction(h *PanelHost, action string) {
	argv := sessionArgv(action, r.cfg.Session.Locker)
	if err := r.runArgv(argv); err != nil {
		h.errLabel = err.Error()
		r.rebuildPanel(h)
		return
	}
	r.closePanelLocked(h.id)
}

func (r *Registry) closeAllPanelsLocked() {
	r.roots.release()
	ids := make([]PanelID, 0, len(r.panelHosts))
	for id := range r.panelHosts {
		ids = append(ids, id)
	}
	for _, id := range ids {
		r.panels.Close(id)
		r.teardownPanelLocked(id)
	}
}

type retainedEditor struct {
	field  *ui.Field
	reseed uint64
}

func overlayEditors(root *ui.Node, eds map[string]*retainedEditor) {
	if eds == nil {
		return
	}
	seen := map[string]bool{}
	var walk func(*ui.Node)
	walk = func(n *ui.Node) {
		if n == nil {
			return
		}
		if n.Kind == ui.KindTextField {
			k := n.StableKey()
			if k != "" {
				seen[k] = true
				slot := eds[k]
				if slot == nil || n.Reseed > slot.reseed {
					eds[k] = &retainedEditor{
						field: &ui.Field{
							Text: n.Text, PreeditText: n.Preedit, Cursor: n.Cursor,
							Multiline: n.Multiline, SubmitOnEnter: n.SubmitOnEnter,
						},
						reseed: n.Reseed,
					}
				} else {
					slot.field.Multiline = n.Multiline
					slot.field.SubmitOnEnter = n.SubmitOnEnter
					slot.field.SyncTo(n)
				}
			}
		}
		for _, c := range n.Children {
			walk(c)
		}
	}
	walk(root)
	for k := range eds {
		if !seen[k] {
			delete(eds, k)
		}
	}
}
