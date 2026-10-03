package shell

import (
	"context"
	"errors"
	"fmt"
	"log"
	"math"
	"net/url"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"

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
	// writeTimer is the pending settings write, if a field or slider has been
	// moved and has not settled yet.
	writeTimer *time.Timer
	id         PanelID
	output     uint32
	// openOrder selects the newest panel for outside dismissal.
	openOrder uint64
	// preferredX is the panel's original anchor before collision reflow.
	preferredX int
	place      Placement
	// rect is the placed panel body in output coordinates, excluding joints.
	rect   ui.Rect
	root   *ui.Node
	focus  []*ui.Node
	roving ui.Roving
	leases []*services.Lease
	// subjectLeases hold the Control Centre's per-interface and per-device rate
	// rings. They are resolved from the first snapshot that names a subject and
	// kept until that subject disappears, so the chart does not hop.
	subjectLeases                                     []*services.Lease
	ccIface, ccDevice                                 string
	ccRootSource, ccRootDevice, ccRootSelectionSource string
	// monitorInterval is the interval the system monitor's leases were taken
	// at, so a reload that changes monitor.refresh can tell to re-lease.
	monitorInterval time.Duration
	// monitorIconRebuild is set while a coalesced rebuild for arriving icons
	// is scheduled.
	monitorIconRebuild bool
	shieldQuiet        time.Time
	// anim is this surface's one clock: every transition it runs shares it, so
	// frames are scheduled from a single place.
	anim        *animator
	stopAnim    chan struct{}
	stopOnce    sync.Once
	badgeCounts map[string]int
	theme       Theme
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
	// paper is the sticky-note fill a floating plugin surface paints as its
	// ground; FillNone keeps the theme's panel ground.
	paper    ui.Fill
	logicalW int
	logicalH int
	scale120 int
	// mods is the modifier state the platform resolved with the latest key.
	mods ui.Mods
	// inputSerial is the serial of the latest key or pointer press this
	// surface handled, carried into a clipboard request so the platform can
	// claim the selection with it.
	inputSerial uint32
	// fieldDrag is the editor a primary press in a single-line field is
	// drag-selecting; nil when none is. The retained field, not a key, names
	// it: a panel search carries neither Key nor Action.
	fieldDrag *ui.Field
	// clickField, clickAt, clickX, clickY and clicks count presses on one
	// field for double- and triple-click selection.
	clickField     *ui.Field
	clickAt        time.Time
	clickX, clickY int
	clicks         int
	// barAdding names the lane whose add-a-widget list is open, empty when
	// none is. The list expands in place the way Menu does, because no
	// popup-over-panel surface exists.
	barAdding string
	// barDropHint names the row the pointer is currently over, so a drag
	// repaints when the answer changes and stays quiet when it does not.
	barDropHint string
	// settingsScroll retains the settings body's scroll offset across a
	// rebuild. Every edit in the lane editor rebuilds the tree, and a fresh
	// tree starts at the top, so without this a drag or a remove threw the
	// user back to the first row and lost their place.
	settingsScroll int
	// settingsPage is the open section's page (settings redesign D1). Empty
	// means the section's first.
	settingsPage string
	// barPreview is the last bar preview that resolved, kept so a draft that
	// is mid-edit does not blank the Appearance page's preview.
	barPreview *ui.Image
	// barImages caches rendered bar pictures by the draft, width and scale
	// they were painted from. Building one scans system fonts; the
	// Appearance page cost 38 ms a rebuild on the Wayland owner without it.
	barImages map[string]*ui.Image
	// settingsScrollTop makes the next rebuild open the body at the top: a
	// page or section switch. rebuildPanel otherwise carries the old offset.
	settingsScrollTop bool
	// settingsTreeScale is the scale the settings tree was built at, so a
	// configure at another scale rebuilds it (its measured widths and the bar
	// pictures depend on it).
	settingsTreeScale int
	// pluginStoreTreeScale is the same for the plugin store, whose cards,
	// chip rows and wrapped text are measured as they are built.
	pluginStoreTreeScale int
	// pluginName resolves a plugin ID to its catalogue name for the bar
	// editor's rows (settings redesign D9). Set by barLaneStripFor; nil
	// falls back to the plugin ID.
	pluginName func(id string) string
	pressed    string
	// pointer is the resolved hover/press state, kept as stable keys so it
	// survives the tree rebuilds that replace every node.
	pointer        interaction
	drag           ui.Drag
	lastAction     string
	hoverX, hoverY int
	monthDelta     int
	errLabel       string
	menu           *Menu
	menuPath       string
	menus          map[string]*Menu
	sliderDrag     *ui.Node
	scrollDrag     *ui.Node
	set            *settings.Registry
	draft          config.Config
	// barSelected is the chip the lane editor has selected, as the address
	// barRefAction encodes. barOutput is the output whose lanes are being
	// edited, empty for the shared bar (D7).
	barSelected        string
	barOutput          string
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
	// launcherAttempt stamps the user's current launcher interaction; an
	// activation completion applies only while its stamp is still current.
	// launcherPendingAttempt is the stamp an in-flight activation captured,
	// so a Notes capture superseded before its provider ran is dropped.
	launcherAttempt        uint64
	launcherPendingAttempt uint64
	// launcherAwaiting is set when a query is sent and cleared only by a
	// snapshot stamped with that query's launcherQueryGen. Until then the
	// rows on screen belong to the previous query, so activating one would
	// run something the field no longer asks for. A snapshot already queued
	// for the previous query must not clear the flag (gh #77, gh #90).
	launcherAwaiting bool
	launcherQueryGen uint64

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

	pluginManagerTab string
	// palettes is the Palettes page state (custom palettes P12). Registry.mu.
	palettes                    paletteUI
	pluginManagerSourceWarning  bool
	pluginManagerRemoveConfirm  string
	pluginManagerError          string
	pluginManagerExpanded       map[string]bool
	pluginUpdateAllNeedsConsent []string
	pluginStoreReviewKey        string

	pluginStoreQuery          BrowseQuery
	pluginStoreSelected       string
	pluginStoreDetail         string
	pluginStoreConsent        *pluginStorePinnedConsent
	pluginStoreRemoveConfirm  bool
	pluginStoreDetailErr      string
	pluginStorePending        *pluginStorePendingOp
	pluginStoreDetailScroll   int
	pluginStoreScroll         int
	pluginStoreColumns        int
	pluginStoreRowHeight      int
	pluginStoreGridHeight     int
	pluginStoreConfigured     bool
	pluginStoreCategoriesOpen bool

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
	// monitorOptions swaps the info card's facts for the view options.
	monitorOptions bool
	// processExpanded holds the group keys the user opened, and
	// processCollapsed the section keys the user closed. Both are keyed by
	// application or executable, so they survive recycled PIDs.
	processExpanded  map[string]bool
	processCollapsed map[string]bool

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
	case "plugin-store":
		return PanelPluginStore, nil
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

	var facts machineFacts
	if id == PanelMonitor {
		facts = readMachineFacts()
	}
	out, trig := r.focusedTrigger()
	r.mu.Lock()
	defer r.mu.Unlock()
	if id == PanelMonitor {
		r.machineFacts = facts
	}
	if where, ok := r.panels.Output(id); ok && where == out && r.panelOpenLocked(id) {
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

// openSettingsAtLocked opens the settings panel at one section, through the
// addressing that already exists: panelSection validates the name and
// selectPanelSectionLocked applies it, which is what IPC section addressing
// uses. No second route is added.
func (r *Registry) openSettingsAtLocked(output uint32, requested string) bool {
	section, err := panelSection(PanelSettings, requested)
	if err != nil {
		return true
	}
	if r.panelHosts[PanelSettings] == nil {
		// The output's size, so Settings takes its responsive size (D10)
		// rather than one for a 1920x1080 output that is not there.
		connector := ""
		if bar, ok := r.bars[output]; ok {
			connector = bar.connector()
		}
		if err := r.openPanelRootLocked(PanelSettings, output, r.triggerLocked(output, connector)); err != nil {
			return true
		}
	}
	_ = r.selectPanelSectionLocked(PanelSettings, section)
	return true
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
	case PanelMonitor:
		if _, _, ok := parseProcessOrder(requested); ok {
			return requested, nil
		}
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
		if _, _, ok := settingsAddress(requested); ok {
			return requested, nil
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
	if id == PanelMonitor {
		// The section is a sort order, not a page: apply it every time,
		// so reopening at the same order after a click still resets it.
		key, desc, _ := parseProcessOrder(section)
		h.monitorPage, h.processSort, h.processDesc = monitorPageProcesses, key, desc
		r.rebuildPanel(h)
		r.publishSurface(h.output, panelSurfaceID(id))
		return nil
	}
	page := ""
	if id == PanelSettings {
		var ok bool
		requested := section
		if section, page, ok = settingsAddress(requested); !ok {
			return fmt.Errorf("unknown section %q", requested)
		}
		if h.section == section && settingsCurrentPage(h, section) == page {
			return nil
		}
		h.settingsScrollTop = true
	} else if h.section == section {
		return nil
	}
	if id == PanelControlCenter {
		h.selectControlCentreSection(r, section)
		r.publishSurface(h.output, panelSurfaceID(id))
		return nil
	}
	if id == PanelSettings {
		r.settingsSectionChangingLocked(h, section)
	}
	h.section, h.settingsPage = section, page
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
	trig := Trigger{BarEdge: policy.Edge, BarZone: policy.Extent(), Align: "center"}
	if bar, ok := r.bars[global]; ok {
		w, h := bar.configuredSize()
		if w > 0 {
			trig.OutW = w
		}
		// Panels meet the body; an attached bar's overhang lies past it.
		if h -= policy.Overhang(); h > 0 {
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
	var facts machineFacts
	if id == PanelMonitor {
		facts = readMachineFacts()
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if id == PanelMonitor {
		r.machineFacts = facts
	}
	if where, ok := r.panels.Output(id); ok && where == output && r.panelOpenLocked(id) {
		return nil
	}
	return r.openPanelRootLocked(id, output, trig)
}

// openPanelRootLocked opens a new panel or adds it to the current panel group.
// A different modal root still replaces the whole group.
func (r *Registry) openPanelRootLocked(id PanelID, output uint32, trig Trigger) error {
	if where, ok := r.panels.Output(id); ok {
		if where == output && r.panelOpenLocked(id) {
			return nil
		}
		r.closePanelLocked(id)
	}

	owner, generation, open := r.roots.current()
	panelGroupOpen := open && owner == panelGroupRoot()
	if !panelGroupOpen {
		generation = r.roots.openRoot(panelGroupRoot())
		r.roots.onClose(generation, func() { r.closePanelGroupLocked() })
	}
	if r.panels.open == nil {
		r.panels.open = make(map[PanelID]uint32)
	}
	r.panels.open[id] = output
	if err := r.spawnPanelLocked(id, output, trig, generation); err != nil {
		r.panels.Close(id)
		if !panelGroupOpen {
			r.roots.closeRoot(generation)
		}
		return err
	}
	if id == PanelNotifications {
		r.setCenterOpen(true)
		// The badge clears when the daemon confirms with a history-seen
		// delta; a command that never left keeps the entries unread.
		if ids := r.notify.unseenIDs(); len(ids) > 0 {
			if err := r.sendNotify(protocol.Command{Kind: protocol.CommandHistoryMarkSeen, IDs: ids}); err != nil {
				fmt.Fprintf(os.Stderr, "sysc-shell: mark notifications seen: %v\n", err)
			}
		}
	}
	return nil
}

// panelOpenLocked reports whether id belongs to the current panel group.
// Caller holds Registry.mu.
func (r *Registry) panelOpenLocked(id PanelID) bool {
	return r.roots.owns(panelGroupRoot()) && r.panelHosts[id] != nil
}

// closePanelGroupLocked runs as the panel group's root cleanup. The panel
// hosts are the authoritative list; each output shield closes with its last
// panel in teardownPanelLocked.
func (r *Registry) closePanelGroupLocked() {
	hadPluginPanel := r.panelHosts[PanelPlugin] != nil
	ids := make([]PanelID, 0, len(r.panelHosts))
	for id := range r.panelHosts {
		ids = append(ids, id)
	}
	for _, id := range ids {
		r.panels.Close(id)
		r.teardownPanelLocked(id)
	}
	for id := range r.panels.open {
		r.panels.Close(id)
	}
	if hadPluginPanel && r.plugins != nil {
		ids := r.plugins.snapshotPanelViewIDs()
		go r.plugins.dropPanelViews(ids)
	}
	// A root that goes away takes any visible tooltip with it.
	r.dwell.leave()
}

func (r *Registry) ClosePanel(id PanelID) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.closePanelLocked(id)
}

// closePanelLocked closes one panel and releases the group root when it was
// the last member.
func (r *Registry) closePanelLocked(id PanelID) {
	h := r.panelHosts[id]
	if h != nil {
		// A hint over a panel that is going would stay on screen until the
		// pointer next moved somewhere that drives the dwell.
		r.dwell.leave()
	}
	output, hasOutput := r.panels.Output(id)
	if h != nil {
		output, hasOutput = h.output, true
	}
	r.panels.Close(id)
	r.teardownPanelLocked(id)
	if hasOutput {
		r.restorePanelPositionsLocked(output)
	}
	if len(r.panelHosts) > 0 && r.dwell != nil {
		r.dwell.leave()
	}
	if id == PanelPlugin && r.plugins != nil {
		ids := r.plugins.snapshotPanelViewIDs()
		go r.plugins.dropPanelViews(ids)
	}
	if r.roots.owns(panelGroupRoot()) && len(r.panelHosts) == 0 {
		_, generation, ok := r.roots.current()
		if ok {
			r.roots.closeRoot(generation)
		}
	}
}

func (r *Registry) closePanelsOnOutputLocked(output uint32) {
	ids := r.panelIDsOnOutputLocked(output)
	hadPluginPanel := false
	for _, id := range ids {
		hadPluginPanel = hadPluginPanel || id == PanelPlugin
		r.panels.Close(id)
		r.teardownPanelLocked(id)
	}
	if len(r.panelHosts) > 0 && r.dwell != nil {
		r.dwell.leave()
	}
	if hadPluginPanel && r.plugins != nil {
		views := r.plugins.snapshotPanelViewIDs()
		go r.plugins.dropPanelViews(views)
	}
	if r.roots.owns(panelGroupRoot()) && len(r.panelHosts) == 0 {
		_, generation, ok := r.roots.current()
		if ok {
			r.roots.closeRoot(generation)
		}
	}
}

func (r *Registry) TogglePanel(id PanelID, output uint32, trig Trigger) error {
	var facts machineFacts
	if id == PanelMonitor {
		facts = readMachineFacts()
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if id == PanelMonitor {
		r.machineFacts = facts
	}
	if where, ok := r.panels.Output(id); ok && where == output {
		r.closePanelLocked(id)
		return nil
	}
	// Moving one panel keeps any other members of the panel group open.
	return r.openPanelRootLocked(id, output, trig)
}

func (r *Registry) DropAux(output uint32, surfaceID string) {
	if r.DropTrayAux(output, surfaceID) {
		return
	}
	if r.dropSelectorAux(output, surfaceID) {
		return
	}
	r.mu.Lock()
	plugins := r.plugins
	r.mu.Unlock()
	if plugins != nil && plugins.dropFloatingAux(output, surfaceID) {
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
	if surfaceID == windowSwitcherSurfaceID {
		r.mu.Lock()
		if h := r.windowSwitcher; h != nil && h.open_ && h.output == output {
			h.closeLocked()
		}
		r.mu.Unlock()
		return
	}
	if strings.HasPrefix(surfaceID, "tooltip:") {
		r.mu.Lock()
		r.tooltips.drop(output, surfaceID)
		r.mu.Unlock()
		return
	}
	if connector, ok := strings.CutPrefix(surfaceID, "toast:"); ok {
		r.mu.Lock()
		if r.toasts != nil {
			r.toasts.drop(connector)
		}
		r.mu.Unlock()
		return
	}
	if escaped, ok := strings.CutPrefix(surfaceID, "depth-clock:"); ok {
		if connector, err := url.PathUnescape(escaped); err == nil {
			r.mu.Lock()
			h := r.depthClocks
			var effects depthClockEffects
			if h != nil {
				effects = h.dropLocked(connector, output)
			}
			r.mu.Unlock()
			if h != nil {
				h.emit(effects)
			}
		}
		return
	}
	if digits, ok := strings.CutPrefix(surfaceID, "osd:"); ok {
		if g, err := strconv.ParseUint(digits, 10, 32); err == nil {
			r.mu.Lock()
			delete(r.osd.open, uint32(g))
			r.mu.Unlock()
		}
		return
	}
	if digits, ok := strings.CutPrefix(surfaceID, panelShieldPrefix); ok {
		shieldOutput, err := strconv.ParseUint(digits, 10, 32)
		if err != nil || uint32(shieldOutput) != output {
			return
		}
		r.mu.Lock()
		defer r.mu.Unlock()
		r.closePanelsOnOutputLocked(output)
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

func (r *Registry) dropPanelAux(host *PanelHost) {
	if host == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.panelHosts[host.id] == host {
		r.closePanelLocked(host.id)
	}
}

func (r *Registry) dropPanelShield(output uint32, owner *PanelHost) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if owner != nil && r.panelShields[output] == owner {
		r.closePanelsOnOutputLocked(output)
	}
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

const panelShieldPrefix = "shield:output:"

func panelShieldSurfaceID(output uint32) string {
	return panelShieldPrefix + strconv.FormatUint(uint64(output), 10)
}

// openPanelHostsLocked returns every other open panel on this output. Caller
// holds r.mu.
func (r *Registry) openPanelHostsLocked(output uint32, except *PanelID) []*PanelHost {
	var hosts []*PanelHost
	for id, h := range r.panelHosts {
		if (except != nil && id == *except) || h == nil || h.output != output || h.rect.W <= 0 || h.rect.H <= 0 {
			continue
		}
		hosts = append(hosts, h)
	}
	return hosts
}

func (r *Registry) restorePanelPositionsLocked(output uint32) {
	hosts := r.openPanelHostsLocked(output, nil)
	sort.Slice(hosts, func(i, j int) bool { return hosts[i].openOrder < hosts[j].openOrder })
	placedHosts := make([]*PanelHost, 0, len(hosts))
	placedRects := make([]ui.Rect, 0, len(hosts))
	for _, h := range hosts {
		anchor := h.rect
		anchor.X = h.preferredX
		work := panelWorkArea(anchor, h.place, placedHosts)
		rect, nextRects := panelArrangement(anchor, work, placedRects,
			ui.Size{W: h.rect.W, H: h.rect.H})
		for i, placed := range placedHosts {
			r.updatePanelPlacementLocked(placed, nextRects[i])
		}
		r.updatePanelPlacementLocked(h, rect)
		placedHosts = append(placedHosts, h)
		placedRects = append(nextRects, rect)
	}
}

func panelWorkArea(base ui.Rect, place Placement, open []*PanelHost) ui.Rect {
	work := place.workArea(base)
	conflict, safeEdge := false, 0
	for _, h := range open {
		if base.Y >= h.rect.Y+h.rect.H || h.rect.Y >= base.Y+base.H {
			continue
		}
		conflict = true
		if h.place.Attached() && h.place.BarShape == "attached" {
			safeEdge = max(safeEdge, h.place.Fillet)
		}
	}
	if !conflict {
		return work
	}
	if place.Attached() && place.BarShape == "attached" {
		safeEdge = max(safeEdge, place.Fillet)
	}
	if safeEdge == 0 {
		return work
	}
	left := max(work.X, safeEdge)
	right := min(work.X+work.W, place.Output.W-safeEdge)
	work.X, work.W = left, max(right-left, 0)
	return work
}

// updatePanelPlacementLocked moves an existing panel without replacing its
// Wayland surface, which keeps its focus and event callbacks attached.
func (r *Registry) updatePanelPlacementLocked(h *PanelHost, rect ui.Rect) {
	if h == nil || h.rect == rect {
		return
	}
	h.rect = rect
	h.place.AnchorX = rect.X + rect.W/2
	spec := r.panelSpec(h, marginsFor(rect, h.place))
	width, height := uint32(max(spec.Width, 0)), uint32(max(spec.Height, 0))
	inputRects := spec.InputRects
	if inputRects == nil {
		inputRects = []ui.Rect{{W: int(width), H: int(height)}}
	}
	update := &wayland.AuxUpdate{
		MarginTop:      &spec.MarginTop,
		MarginBottom:   &spec.MarginBottom,
		MarginLeft:     &spec.MarginLeft,
		MarginRight:    &spec.MarginRight,
		Width:          &width,
		Height:         &height,
		SetInputRegion: true,
		InputRects:     inputRects,
	}
	r.sendAux(wayland.AuxRequest{Output: h.output, ID: panelSurfaceID(h.id), Update: update})
}

func (r *Registry) panelIDsOnOutputLocked(output uint32) []PanelID {
	var ids []PanelID
	for id, h := range r.panelHosts {
		if h != nil && h.output == output {
			ids = append(ids, id)
		}
	}
	return ids
}

func (r *Registry) hasPanelOnOutputLocked(output uint32) bool {
	return len(r.panelIDsOnOutputLocked(output)) > 0
}

func (r *Registry) newestPanelOnOutputLocked(output uint32) *PanelHost {
	var newest *PanelHost
	for _, h := range r.panelHosts {
		if h != nil && h.output == output && (newest == nil || h.openOrder > newest.openOrder) {
			newest = h
		}
	}
	return newest
}

func (r *Registry) spawnPanelLocked(id PanelID, output uint32, trig Trigger, generation uint64) error {
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
	if id == PanelSettings {
		size = settingsPanelSize(outW, outH)
	}
	gap := r.cfg.Panels.Gap
	if id == PanelPlugin || id == PanelAudio || id == PanelControlCenter || id == PanelPluginStore {
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
	if bar, ok := r.bars[output]; ok {
		bt := bar.themeSnapshot()
		place.BarShape, place.BarGap, place.BarRadius, place.Fillet = bt.BarShape, bt.BarGap, bt.Radius, bt.Fillet
	}
	// Settings, the launcher, the clipboard and the plugin store float over the desktop; every
	// other panel attaches to the bar.
	if id == PanelLauncher || id == PanelSettings || id == PanelClipboard || id == PanelPluginStore {
		place.CenterY = true
	}
	if _, hasBar := r.bars[output]; !place.CenterY && (!hasBar || r.panelThemeFor(output).BarStyle == "islands") {
		place.Detached = true
		place.Gap = theme.MarginS
		if !hasBar {
			place.BarZone = 0
		}
	}
	if id == PanelPluginStore {
		// The store is large enough to reach the bar, so it centres in the
		// space the bar leaves rather than across it.
		place.Gap = 0
		place.CenterY = true
		place.Align = "center"
	}
	if id == PanelClipboard {
		// Clipboard history is a true modal: centre it against the whole output,
		// not the bar-free region used by attached/floating pickers.
		place.BarZone = 0
		place.Gap = 0
		place.CenterY = true
		place.Align = "center"
	}
	if id == PanelPluginStore {
		w, hgt := place.FittedSize()
		place.Panel.W, place.Panel.H = w, hgt
	}

	// Tuck an attached panel one pixel under an opaque bar. Over a
	// translucent one the doubled row would paint a darker line instead.
	if place.Attached() && r.panelThemeFor(output).Surfaces.Bar == 0xff {
		place.Overlap = 1
	}

	openShield := !r.hasPanelOnOutputLocked(output)
	r.panelOrder++
	h := &PanelHost{
		id:          id,
		output:      output,
		openOrder:   r.panelOrder,
		place:       place,
		pointer:     interaction{stateLayer: true},
		stopAnim:    make(chan struct{}),
		shieldQuiet: time.Now().Add(shieldQuietFor),
		theme:       r.panelThemeFor(output),
		fontFamily:  r.panelFontFamily(output),
	}
	// Resolve the surface clock before the first tree is built. Effect phases
	// are keyed by the tree's stable nodes, so the first frame must target the
	// same animator entries as every later rebuild.
	h.anim = newAnimator(r.animClock, r.cfg.Accessibility.ReducedMotion, h.theme.Motion)
	h.anim.Target(panelSurfaceID(id), animVisible, 1)
	if bar, ok := r.bars[output]; ok {
		h.scale120 = bar.scale120()
	}
	if id == PanelSettings {
		h.set = r.settingsForLocked(r.cfg)
		r.refreshPalettesAsync(h)
		h.draft = r.cfg
		h.section = "Bar"
		h.search = ui.NewField("")
		h.menus = map[string]*Menu{}
		h.fields = map[string]*ui.Field{}
	}
	if id == PanelPluginStore {
		h.search = ui.NewField("")
		h.pluginStoreQuery.Sort = SortName
		h.menus = map[string]*Menu{}
	}
	if id == PanelLauncher {
		h.search = ui.NewField("")
		svc := r.launcherServiceLocked()
		svc.Open()
		r.launcherSendQuery(h, "")
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
		h.processSort, h.processDesc = "mem", true
		h.processExpanded, h.processCollapsed = map[string]bool{}, map[string]bool{}
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
	if id == PanelSession {
		_ = h.ensureText()
		h.place.Panel.H = r.sessionSurfaceHeight(h)
	}
	if id == PanelNotifications {
		_ = h.ensureText()
		h.place.Panel.H = notificationsSurfaceHeight(h)
		// The notification tree derives its scroll viewport from the panel
		// height. Rebuild it after fitting the surface so the viewport does not
		// retain the initial 300 px fallback and clip the last visible card.
		h.root = r.panelTree(h)
	}
	probe := copyNode(h.root)
	if err := h.resolveEffectMotionLocked(probe); err != nil {
		return err
	}
	resolveProgressMotion(h.anim, probe)
	resolveSpriteMotion(h.anim, probe)
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
	if id == PanelPluginStore {
		h.focusPluginStoreSelection()
	}
	w, hgt := h.place.FittedSize()
	h.place.Panel.W, h.place.Panel.H = w, hgt
	baseRect := h.place.panelRect()
	h.preferredX = baseRect.X
	openHosts := r.openPanelHostsLocked(output, &id)
	openRects := make([]ui.Rect, len(openHosts))
	for i, open := range openHosts {
		openRects[i] = open.rect
	}
	var placedRects []ui.Rect
	h.rect, placedRects = panelArrangement(baseRect, panelWorkArea(baseRect, h.place, openHosts),
		openRects, ui.Size{W: w, H: hgt})
	if h.rect.X != baseRect.X {
		h.place.AnchorX = h.rect.X + h.rect.W/2
	}
	margins := marginsFor(h.rect, h.place)
	if err := r.acquirePanelLeases(h); err != nil {
		return err
	}
	for i, open := range openHosts {
		r.updatePanelPlacementLocked(open, placedRects[i])
	}

	r.panelHosts[id] = h

	if openShield {
		if r.panelShields == nil {
			r.panelShields = make(map[uint32]*PanelHost)
		}
		r.panelShields[output] = h
		r.sendAux(wayland.AuxRequest{Output: output, Open: r.shieldSpec(h, generation)})
	}
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
		interval := monitorLeaseInterval(r.cfg.Monitor)
		for _, sel := range append(monitorLeaseSelectors(), services.Selector{Source: services.SourceProcess}) {
			lease, err := r.metrics.Acquire(sel, interval)
			if err != nil {
				releaseAll(h.leases)
				h.leases = nil
				return err
			}
			h.leases = append(h.leases, lease)
		}
		h.monitorInterval = interval
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
		for _, sel := range append(monitorLeaseSelectors(), services.Selector{Source: services.SourceBattery}) {
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

// refreshMonitorLeasesLocked re-leases an open system monitor at the configured
// refresh when it differs from the interval its leases hold. The new leases
// are taken before the old ones go, so no source stops in between; on a
// failure the monitor keeps sampling at the old interval.
func (r *Registry) refreshMonitorLeasesLocked(h *PanelHost) {
	if h == nil || r.metrics == nil || h.monitorInterval == monitorLeaseInterval(r.cfg.Monitor) {
		return
	}
	old, oldInterval := h.leases, h.monitorInterval
	h.leases = nil
	if err := r.acquirePanelLeases(h); err != nil {
		h.leases, h.monitorInterval = old, oldInterval
		return
	}
	releaseAll(old)
	// Forget the resolved subjects so the next sync re-leases their rate
	// rings at the new interval.
	h.ccIface, h.ccDevice = "", ""
	r.syncRateSubjectsLocked(h, r.sample, h.monitorInterval)
}

// monitorLeaseSelectors are the sources the Control Centre's Monitor page and
// the system monitor's System page chart. Battery is the Control Centre's
// alone, and processes the system monitor's.
func monitorLeaseSelectors() []services.Selector {
	return []services.Selector{
		{Source: services.SourceCPU},
		{Source: services.SourceMemory},
		{Source: services.SourceCPU, Subject: "temperature"},
		{Source: services.SourceGPU},
		{Source: services.SourceFilesystem, Subject: "/"},
		{Source: services.SourceNetwork},
		{Source: services.SourceBlock},
	}
}

// monitorLeaseInterval is the system monitor's sampling interval: the
// configured refresh, never below one second.
func monitorLeaseInterval(m config.Monitor) time.Duration {
	return time.Duration(max(m.Refresh, 1)) * time.Second
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

func panelSurfaceID(id PanelID) string { return "panel:" + id.String() }

// blur-exempt: the shield paints nothing. It is a transparent, fullscreen input
// catcher, so it has no ground for a backdrop to sit under, and it opens before
// the panel it guards -- a capture here would photograph the screen a second
// time for no one to look at.
func (r *Registry) shieldSpec(h *PanelHost, generation uint64) *wayland.AuxSpec {
	return &wayland.AuxSpec{
		ID:            panelShieldSurfaceID(h.output),
		Namespace:     "sysc-shell-shield",
		Layer:         layerOverlay,
		Anchor:        uint32(layershell.ZwlrLayerSurfaceV1AnchorTop | layershell.ZwlrLayerSurfaceV1AnchorBottom | layershell.ZwlrLayerSurfaceV1AnchorLeft | layershell.ZwlrLayerSurfaceV1AnchorRight),
		ExclusiveZone: -1,
		Keyboard:      keyboardNone,
		OnDrop:        func() { r.dropPanelShield(h.output, h) },
		Callbacks: wayland.HostCallbacks{
			Configure: func(int, int, int) error { return nil },
			Render:    func([]byte, int, int, int) error { return nil },
			Handle: func(e wayland.Event) bool {
				if e.Kind != wayland.EventPointerPress {
					return false
				}
				r.mu.Lock()
				defer r.mu.Unlock()
				if !r.roots.owns(panelGroupRoot()) || r.roots.gen() != generation ||
					time.Now().Before(h.shieldQuiet) {
					return false
				}
				// A press over an open panel's own body is not a click
				// outside, even when the compositor hands it to the shield.
				// Taking it as one closed Settings under a click on its rail,
				// and said nothing about why.
				for _, open := range r.panelHosts {
					if open.output == h.output && open.place.Rect().Contains(int(e.X), int(e.Y)) {
						log.Printf("shell: shield took a press inside %s at %.0f,%.0f; keeping the panel open", open.id, e.X, e.Y)
						return false
					}
				}
				newest := r.newestPanelOnOutputLocked(h.output)
				if newest == nil {
					return false
				}
				r.closePanelLocked(newest.id)
				return true
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
//
// Only a panel draws its own rim; the bar, toasts and tray surfaces sit
// directly on the shared surface and leave it zero. An attached panel paints
// none either: it and the bar are one ground, and a stroke would read as a
// seam between them.
func (h *PanelHost) rootStyle(t Theme) render.Style {
	var s render.Style
	switch {
	case h.place.Attached() && h.backdrop == nil:
		s = t.AttachedPanelStyle()
	case h.place.Attached():
		s = t.PanelStyle()
	default:
		s = t.PanelStyle()
		s.Rim = t.Outline
	}
	if h.id == PanelSettings {
		s.SurfaceOpacity = max(s.SurfaceOpacity, settingsOpacityFloor)
	}
	return s
}

// settingsPanelSize is settings redesign D10: 72 percent of the output's
// width and 88 percent of its height, capped at 1120x820.
func settingsPanelSize(outputW, outputH int) ui.Rect {
	return ui.Rect{W: min(1120, outputW*72/100), H: min(820, outputH*88/100)}
}

// settingsOpacityFloor is the least alpha the settings root paints at (D7):
// the pane is read for minutes at a time, over whatever is behind it.
const settingsOpacityFloor uint8 = 0xf0

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
	joints := h.place.Joints()
	m.Left -= joints.Left
	width, height := h.surfaceSize()
	opaque := h.theme.BackgroundOpaque()
	var input []ui.Rect
	if joints != (Joints{}) {
		// The joints and a flush panel's screen-edge wedge hang past the body
		// over whatever is beneath; only the body takes input.
		input = []ui.Rect{h.surfaceBody(width, height)}
		// ponytail: omit the hint for fillet-expanded surfaces; add a body-aware
		// opaque-region API only if compositor profiling shows this matters.
		opaque = false
	}
	// Nil disables the capture entirely, so a shell with blur off pays none of
	// its cost rather than capturing and discarding. A compositor that blurs
	// does the job itself, through BlurShape.
	var blurRegion *ui.Rect
	if r.cfg.Theme.BlurBehind && !r.caps.Blur {
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
		Width:         int32(width),
		Height:        int32(height),
		InputRects:    input,
		ExclusiveZone: -1,
		Keyboard:      keyboardExclusive,
		BlurRegion:    blurRegion,
		BlurRadius:    r.cfg.Theme.BlurRadius,
		OnDrop:        func() { r.dropPanelAux(h) },
		Callbacks: wayland.HostCallbacks{
			// Delivered from the Wayland goroutine before the surface exists,
			// so it takes the registry lock exactly as Render does.
			Backdrop: func(img *ui.Image) {
				r.mu.Lock()
				defer r.mu.Unlock()
				h.backdrop = img
			},
			BlurShape: func() []ui.Rect {
				r.mu.Lock()
				defer r.mu.Unlock()
				return h.blurShape(r)
			},
			OpaqueBackground: opaque,
			Radius:           h.theme.Radius,
			Configure:        h.configureLocking(r),
			Render:           h.renderLocking(r),
			Handle:           h.handle(r),
			WantIME: func() bool {
				// syncIME calls this from the Wayland goroutine outside any
				// handler, so the tree can be mid-rebuild under r.mu (GH #6).
				r.mu.Lock()
				defer r.mu.Unlock()
				n := h.focused()
				return n != nil && n.Kind == ui.KindTextField
			},
			IBeamAt: func(x, y float64) bool {
				r.mu.Lock()
				defer r.mu.Unlock()
				n := h.hitFocusable(int(math.Floor(x)), int(math.Floor(y)))
				return n != nil && n.Kind == ui.KindTextField
			},
		},
	}
}

// blurShape is the region the compositor blurs behind the panel, in surface
// coordinates: its whole silhouette, joints and screen-edge wedge included. An
// attached panel on a frosted bar blurs so it shares the bar's ground; any
// panel blurs when blur-behind is on and the compositor can. Caller holds
// r.mu.
func (h *PanelHost) blurShape(r *Registry) []ui.Rect {
	if !(h.place.Attached() && h.theme.Blur) && !(r.cfg.Theme.BlurBehind && r.caps.Blur) {
		return nil
	}
	w, hgt := h.logicalW, h.logicalH
	if w <= 0 || hgt <= 0 {
		w, hgt = h.surfaceSize()
	}
	shape := ui.SurfaceShape{Body: h.surfaceBody(w, hgt), Radius: h.theme.Radius}
	if h.place.Attached() {
		opacity, _ := h.panelReveal()
		j := h.place.Joints()
		shape.AttachEdge = h.place.BarEdge
		shape.JointLeft, shape.JointRight, shape.EdgeFillet = h.revealJoints(opacity)
		shape.EdgeLeft, shape.EdgeRight = j.FlushLeft, j.FlushRight
	}
	return ui.BlurStrips(shape)
}

// edgeExtent is how far a flush panel's surface reaches past its far edge,
// to hold the wedge that curves it into the screen's side.
func (h *PanelHost) edgeExtent(j Joints) int {
	if j.Flush() {
		return h.place.Fillet
	}
	return 0
}

// surfaceSize is the surface the placed panel needs: its body widened by the
// joints and lengthened by any screen-edge wedge.
func (h *PanelHost) surfaceSize() (w, hgt int) {
	j := h.place.Joints()
	return h.place.Panel.W + j.Left + j.Right, h.place.Panel.H + h.edgeExtent(j)
}

// surfaceBody is where the body sits in a surface of the given size: inset by
// the left joint, and below the screen-edge wedge when the bar is on the lower
// edge.
// driveTooltip arms the hover card for the node under the pointer, the way
// the bar does for its widgets. The card is placed on the output, so the
// node's surface rect is moved by where the panel body sits there.
func (h *PanelHost) driveTooltip(r *Registry) {
	text, anchor := tooltipAt(h.root, h.hoverX, h.hoverY)
	if text == "" {
		r.dwell.leave()
		return
	}
	w, hgt := h.logicalW, h.logicalH
	if w <= 0 || hgt <= 0 {
		w, hgt = h.surfaceSize()
	}
	body := h.surfaceBody(w, hgt)
	anchor.X += h.rect.X - body.X
	anchor.Y += h.rect.Y - body.Y
	r.dwell.enterOnOutput(h.output, anchor, text)
}

// tooltipAt is the hover text of the innermost node under x, y, and that
// node's bounds. The walk only descends into nodes containing the point, so a
// row scrolled out of its list is never found.
func tooltipAt(root *ui.Node, x, y int) (string, ui.Rect) {
	text, bounds := "", ui.Rect{}
	var walk func(*ui.Node)
	walk = func(n *ui.Node) {
		if n == nil || !n.Bounds.Contains(x, y) {
			return
		}
		if n.Tooltip != "" {
			text, bounds = n.Tooltip, n.Bounds
		}
		for _, c := range n.Children {
			walk(c)
		}
	}
	walk(root)
	return text, bounds
}

func (h *PanelHost) surfaceBody(w, hgt int) ui.Rect {
	j := h.place.Joints()
	edge := h.edgeExtent(j)
	body := ui.Rect{X: j.Left, W: max(0, w-j.Left-j.Right), H: max(0, hgt-edge)}
	if h.place.BarEdge == "bottom" {
		body.Y = edge
	}
	return body
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
		// Settings and the plugin store measure text as their trees are
		// built, first at the bar's scale or none. At another scale the tree
		// is rebuilt before it is laid out: laying out the stale one first
		// can fail to fit and close the surface.
		if built, measured := h.treeScale(); measured && built != scale120 && ui.Scale120(scale120).Valid() {
			h.logicalW, h.logicalH, h.scale120 = w, height, scale120
			if err := h.ensureText(); err != nil {
				return err
			}
			r.rebuildPanel(h)
		}
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
	var focusKey, focusName string
	var focusKind ui.Kind
	if h.id == PanelPluginStore && h.pluginStoreConfigured {
		if focused := h.focused(); focused != nil {
			focusKey, focusName, focusKind = focused.StableKey(), focused.Name, focused.Kind
		}
	}
	h.logicalW, h.logicalH, h.scale120 = w, height, scale120
	if err := h.ensureText(); err != nil {
		return err
	}
	box := h.surfaceBody(w, height)
	var err error
	if h.root != nil && h.root.Kind == ui.KindRow {
		err = ui.Layout(h.root, box, h.measureText())
	} else {
		err = ui.LayoutColumn(h.root, box, h.measureText())
	}
	if err != nil || h.id != PanelPluginStore {
		return err
	}
	h.focus = ui.Focusables(h.root)
	h.roving.Count = len(h.focus)
	if !h.pluginStoreConfigured {
		h.pluginStoreConfigured = true
		h.focusPluginStoreSelection()
		return nil
	}
	for i, n := range h.focus {
		if n == nil {
			continue
		}
		if focusKey != "" && n.StableKey() == focusKey || focusKey == "" && focusName != "" && n.Kind == focusKind && n.Name == focusName {
			h.roving.Set(i)
			return nil
		}
	}
	h.focusPluginStoreSelection()
	return nil
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

// panelReveal is the surface's reveal: its opacity and slide.
func (h *PanelHost) panelReveal() (opacity float64, offsetY int) {
	if h == nil || h.anim == nil || !h.anim.has(panelSurfaceID(h.id), animVisible) {
		return 1, 0
	}
	key := panelSurfaceID(h.id)
	opacity = h.anim.PanelOpacity(key)
	offsetY = h.anim.PanelSlide(key)
	if h.place.BarEdge == "top" {
		offsetY = -offsetY
	}
	return opacity, offsetY
}

// revealJoints scales each joint and the screen-edge wedge by the reveal's
// opacity, so they grow out of the bar with the panel rather than popping.
func (h *PanelHost) revealJoints(opacity float64) (left, right, edge int) {
	j := h.place.Joints()
	scale := func(v int) int { return int(math.Round(float64(v) * opacity)) }
	return scale(j.Left), scale(j.Right), scale(h.edgeExtent(j))
}

// markMenuRowHover resolves the open menu's option onto the render copy.
func markMenuRowHover(root *ui.Node, path string, m *Menu, x, y int) {
	if m == nil || path == "" {
		return
	}
	var walk func(*ui.Node)
	walk = func(n *ui.Node) {
		if n == nil {
			return
		}
		if n.Kind == ui.KindMenu && n.Action == path {
			if i := m.RowAt(n, x, y); i >= 0 && i < len(n.Children) && n.Children[i] != nil {
				n.Children[i].State |= ui.StateHovered
				n.Children[i].HoverProgress = 1
			}
			return
		}
		for _, child := range n.Children {
			walk(child)
		}
	}
	walk(root)
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
	w, hgt := h.logicalW, h.logicalH
	if w <= 0 || hgt <= 0 {
		w, hgt = h.surfaceSize()
	}
	body := h.surfaceBody(w, hgt)
	// The painter consumes a copy. Pointer state and effect phase are render
	// values, so neither resolver mutates the retained panel tree.
	page, viewport, pageProgress, pageOffset := h.controlCentrePageVisual()
	if page != nil && pageOffset != 0 {
		offsetNodeY(page, pageOffset)
	}
	root := copyNode(h.root)
	if page != nil && pageOffset != 0 {
		offsetNodeY(page, -pageOffset)
	}
	h.applyEditorView(root)
	h.pointer.apply(root, h.anim)
	if h.menu != nil && h.menu.Opened() && h.menuPath != "" {
		markMenuRowHover(root, h.menuPath, h.menu, h.hoverX, h.hoverY)
	}
	if err := h.resolveEffectMotionLocked(root); err != nil {
		return err
	}
	resolveProgressMotion(h.anim, root)
	h.applyBadgePop(root)
	resolveSpriteMotion(h.anim, root)

	paintTheme := h.paintTheme()
	style := h.rootStyle(paintTheme).WithPaper(h.paper)
	style.Scale120 = scale
	style.Body = body
	opacity, offsetY := h.panelReveal()
	if h.place.Attached() {
		style.AttachEdge = h.place.BarEdge
		j := h.place.Joints()
		style.JointLeft, style.JointRight, style.EdgeFillet = h.revealJoints(opacity)
		style.EdgeLeft, style.EdgeRight = j.FlushLeft, j.FlushRight
	}
	style.Backdrop = h.backdrop
	err = render.Paint(c, root, h.text, style)
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
				// A field carries the focus itself, so the ring takes the
				// well's own corners: a pill for a search field, a 12px page
				// for a multiline one, never a stadium as tall as the page.
				radius = render.FieldRadius(n, ring, scale)
			}
			c.StrokeRounded(ring, radius, max(scale.Physical(2), 2), h.theme.Accent)
		}
	}
	c.ApplySurfaceTransform(opacity, 0, scale.Physical(offsetY))
	return nil
}

func (h *PanelHost) handle(r *Registry) func(wayland.Event) bool {
	return func(e wayland.Event) bool {
		r.mu.Lock()
		defer r.mu.Unlock()
		switch e.Kind {
		case wayland.EventKeyPress, wayland.EventPointerPress:
			h.inputSerial = e.Serial
		}
		switch e.Kind {
		case wayland.EventKeyPress:
			h.mods = e.Mods
			return h.keyEvent(r, e)
		case wayland.EventIME:
			return h.applyIME(r, e)
		case wayland.EventPaste:
			return h.applyPaste(r, e.Paste)
		case wayland.EventPointerAxis:
			return h.scrollAxis(r, e)
		case wayland.EventKeyRelease:
			h.mods = e.Mods
			return false
		case wayland.EventPointerEnter, wayland.EventPointerMotion:
			h.hoverX, h.hoverY = int(math.Floor(e.X)), int(math.Floor(e.Y))
			if h.drag.Source != nil {
				h.drag.Move(e.X, e.Y)
				if !h.drag.Active() {
					return false
				}
				if h.id == PanelSettings {
					// Repaint only when the drop target actually changes.
					// Nothing in the paint path reads drag state, so a repaint
					// per motion event produced pixel-identical output at the
					// cost of a full software render of the surface -- which
					// is what made dragging feel like heavy load.
					return h.barDragHover()
				}
				return true
			}
			if h.sliderDrag != nil {
				ui.SliderAt(h.sliderDrag, h.hoverX)
				return true
			}
			if h.scrollDrag != nil {
				ui.ScrollSetFromY(h.scrollDrag, h.hoverY)
				h.notePluginStoreScroll(h.scrollDrag)
				if h.logicalW > 0 {
					_ = h.configure(h.logicalW, h.logicalH, h.scale120)
				}
				return true
			}
			if h.fieldDrag != nil {
				if n := h.focused(); h.fieldFor(n) == h.fieldDrag {
					h.fieldDrag.SetCaret(h.fieldOffsetAt(n, h.fieldDrag), true)
					h.fieldDrag.SyncTo(n)
					return true
				}
				h.fieldDrag = nil
			}
			h.driveTooltip(r)
			// Only a change of resolved target repaints. Movement inside the
			// control the pointer is already on resolves to the same key and
			// costs nothing.
			return h.pointerChanged(r, h.pointer.setHover(hoverKeyAt(h.root, h.hoverX, h.hoverY)))
		case wayland.EventPointerLeave:
			r.dwell.leave()
			h.pressed = ""
			h.sliderDrag = nil
			h.scrollDrag = nil
			h.fieldDrag = nil
			return h.pointerChanged(r, h.pointer.clear())
		case wayland.EventPointerPress:
			h.hoverX, h.hoverY = int(math.Floor(e.X)), int(math.Floor(e.Y))
			// A click acts on the control; its hint has done its job.
			r.dwell.leave()
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
				h.notePluginStoreScroll(s)
				if h.logicalW > 0 {
					_ = h.configure(h.logicalW, h.logicalH, h.scale120)
				}
				return true
			}
			if n := h.hitFocusable(h.hoverX, h.hoverY); n != nil {
				h.pressed = n.StableKey()
				if h.anim != nil && ui.Animated(n) && h.pressed != "" {
					h.anim.TargetRipple(h.pressed, h.hoverX, h.hoverY)
				}
				h.pointerChanged(r, h.pointer.setPress(n.StableKey()))
				h.setFocus(n)
				if n.Kind == ui.KindDragSource {
					h.drag.Begin(n, e.X, e.Y)
				}
				if n.Kind == ui.KindSlider {
					ui.SliderAt(n, h.hoverX)
					h.sliderDrag = n
				}
				if n.Kind == ui.KindTextField && !n.Multiline && (e.Button == btnLeft || e.Button == 0) {
					h.pressField(n, e.Mods)
				}
				return true
			}
			return false
		case wayland.EventPointerRelease:
			h.hoverX, h.hoverY = int(math.Floor(e.X)), int(math.Floor(e.Y))
			h.fieldDrag = nil
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
				h.afterPluginStoreScroll(r)
				return true
			}
			if h.drag.Active() {
				zone := ui.FindDropZone(h.root, &h.drag)
				dropX, dropY := int(h.drag.X), int(h.drag.Y)
				payload, ok := h.drag.Drop(zone)
				h.drag.Cancel()
				if ok && zone != nil {
					if strings.HasPrefix(zone.Action, "bar-lane:") {
						return h.barDrop(r, zone, payload, dropX, dropY)
					}
					return r.deliverPluginText(zone.Action, payload, v1.EventDrop)
				}
				return true
			}
			n := h.hitFocusable(h.hoverX, h.hoverY)
			pressed := h.pressed
			h.pressed = ""
			cleared := h.pointerChanged(r, h.pointer.setPress(""))
			// A press in a text field placed its caret; releasing it is not an
			// activation. Enter submits a field, never the pointer.
			if n != nil && pressed != "" && n.StableKey() == pressed && n.Kind != ui.KindTextField {
				return h.activate(r)
			}
			return cleared
		}
		return false
	}
}

// multiClickInterval and multiClickSlop bound a double or triple press: the
// same field, soon enough, and near enough to the previous press.
const (
	multiClickInterval = 400 * time.Millisecond
	multiClickSlop     = 4
)

// pressField places the caret of a single-line field under a primary press:
// one press sets it (Shift extends the selection) and starts a drag, two
// select the word there, three select the line. Multiline fields keep
// focus-only presses until the painter exposes per-line hit testing.
func (h *PanelHost) pressField(n *ui.Node, mods ui.Mods) {
	f := h.fieldFor(n)
	if f == nil {
		return
	}
	now := time.Now()
	if f == h.clickField && now.Sub(h.clickAt) <= multiClickInterval &&
		abs(h.hoverX-h.clickX) <= multiClickSlop && abs(h.hoverY-h.clickY) <= multiClickSlop {
		h.clicks++
	} else {
		h.clicks = 1
	}
	h.clickField, h.clickAt, h.clickX, h.clickY = f, now, h.hoverX, h.hoverY
	pos := h.fieldOffsetAt(n, f)
	switch h.clicks {
	case 1:
		f.SetCaret(pos, mods.Has(ui.ModShift))
		h.fieldDrag = f
	case 2:
		f.SelectWordAt(pos)
	default:
		f.SelectLineAt(pos)
	}
	f.BreakUndo()
	f.SyncTo(n)
}

func abs(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

// fieldOffsetAt is the text offset under the pointer in a single-line field,
// measured through the field's current scroll the way it was painted.
func (h *PanelHost) fieldOffsetAt(n *ui.Node, f *ui.Field) int {
	view := copyNode(n)
	view.Editing, view.ScrollX = true, f.ScrollX
	return render.FieldOffsetAt(view, h.hoverX, h.measureText())
}

// keyEvent is the live key path: the platform has already resolved the key
// through the active layout.
func (h *PanelHost) keyEvent(r *Registry, e wayland.Event) bool {
	return h.keyInput(r, ui.KeyInput{Code: e.Key, Sym: e.Sym, Text: e.Text, Mods: e.Mods, Serial: e.Serial})
}

// keyPress resolves a raw evdev code through the US fallback. Tests and any
// caller holding only a code use it.
func (h *PanelHost) keyPress(r *Registry, key uint32) bool {
	return h.keyInput(r, ui.FallbackKey(key, h.mods))
}

func (h *PanelHost) keyInput(r *Registry, k ui.KeyInput) bool {
	key := k.Code
	if h.menu != nil && h.menu.Opened() {
		if !h.menu.Handle(key) {
			// A picker's well takes the keys the menu itself does not: the
			// text of a family name and the keys that edit it, through the
			// same engine as every other field.
			if h.menu.Filtering() {
				var res ui.FieldResult
				if h.menu.Edit(func(f *ui.Field) { res = f.HandleKey(k) }) && res.Handled {
					r.requestClipboard(res, k.Serial)
					r.rebuildPanel(h)
					return true
				}
			}
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
	// A focused field takes its editing keys first. Space is text there and
	// the accept key everywhere else, because fieldKey reports no focus for a
	// control. Enter submits by falling through to the path it always took,
	// so a launcher still runs its selection and a form still activates.
	// The launcher is type-to-search: its well always has focus, and the list
	// keys (Up, Down, Page, Home, End, Enter) drive the results, not the caret.
	if h.id == PanelLauncher && h.launcherKeyPress(r, key) {
		return true
	}
	if res, focused := h.fieldKey(r, k); focused && res.Handled && !res.Submit {
		return true
	}
	if h.id == PanelPluginStore && h.pluginStoreKeyPress(r, key) {
		return true
	}
	if h.id == PanelWallpaper && h.wallpaperKeyPress(r, key) {
		return true
	}
	if h.id == PanelClipboard && h.clipboardKeyPress(r, key) {
		return true
	}
	if h.id == PanelSettings && h.barKeyPress(r, key) {
		return true
	}
	if h.id == PanelPlugin {
		// A bare key typed into a field is text, so a field keeps it; one
		// held with Ctrl or Alt that the field did not use above cannot be
		// text, and still reaches the plugin (Notes saves on Ctrl+S).
		typing := false
		if n := h.focused(); n != nil && n.Kind == ui.KindTextField {
			typing = !h.mods.Has(ui.ModCtrl) && !h.mods.Has(ui.ModAlt)
		}
		if !typing {
			mods := make([]string, 0, 3)
			if h.mods.Has(ui.ModAlt) {
				mods = append(mods, "alt")
			}
			if h.mods.Has(ui.ModCtrl) {
				mods = append(mods, "ctrl")
			}
			if h.mods.Has(ui.ModShift) {
				mods = append(mods, "shift")
			}
			if shortcut := panelShortcutKey(k.Sym); shortcut != "" && r.plugins != nil && r.plugins.deliverShortcut(shortcut, mods) {
				return true
			}
		}
	}
	switch key {
	case keyEsc:
		if h.id == PanelMonitor && h.processSelected != (services.ProcessIdentity{}) {
			h.processSelected = services.ProcessIdentity{}
			r.rebuildPanel(h)
			return true
		}
		if h.id == PanelSettings && h.query != "" {
			h.query = ""
			h.search = ui.NewField("")
			r.rebuildPanel(h)
			return true
		}
		r.closePanelLocked(h.id)
		return true
	case keyTab:
		if h.mods.Has(ui.ModShift) {
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
		off := 1 << 30
		if key == keyHome {
			off = 0
		}
		if !h.scrollTo(off) {
			return false
		}
		h.afterPluginStoreScroll(r)
		return true
	case keyPageUp:
		return h.scrollBy(-max(h.logicalH, 1))
	case keyPageDown:
		return h.scrollBy(max(h.logicalH, 1))
	case keySpace, keyEnter:
		if n := h.focused(); n != nil && h.anim != nil && ui.Animated(n) {
			if key := n.StableKey(); key != "" {
				h.anim.TargetRipple(key, n.Bounds.X+n.Bounds.W/2, n.Bounds.Y+n.Bounds.H/2)
				r.startSurfaceFrames(h)
			}
		}
		return h.activate(r)
	}
	return false
}

// fieldKey applies one key to the focused text field. Text changes run the
// field's change path (query, plugin change event, setting); caret and
// selection moves only repaint. The bool reports whether a field had focus.
func (h *PanelHost) fieldKey(r *Registry, k ui.KeyInput) (ui.FieldResult, bool) {
	n := h.focused()
	f := h.fieldFor(n)
	if f == nil {
		return ui.FieldResult{}, false
	}
	res := f.HandleKey(k)
	if !res.Handled {
		return res, true
	}
	f.SyncTo(n)
	r.requestClipboard(res, k.Serial)
	if res.Changed {
		h.fieldChanged(r, n, f)
	}
	return res, true
}

// panelShortcutKey names a keysym the way plugin shortcuts are declared:
// "home", a lowercase letter, or a digit. Anything else is not a shortcut.
func panelShortcutKey(sym uint32) string {
	if sym == ui.SymHome {
		return "home"
	}
	sym = uint32(unicode.ToLower(rune(sym)))
	if sym >= 'a' && sym <= 'z' || sym >= '0' && sym <= '9' {
		return string(rune(sym))
	}
	return ""
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
	if !h.scrollBy(delta) {
		return false
	}
	h.afterPluginStoreScroll(r)
	return true
}

func (h *PanelHost) scrollBy(delta int) bool {
	s := scrollAt(h.root, h.hoverX, h.hoverY)
	if s == nil {
		return false
	}
	ui.ScrollBy(s, delta)
	h.notePluginStoreScroll(s)
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
	h.notePluginStoreScroll(s)
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

// pressMenuFilter answers a press inside an open picker that landed on its
// filter well rather than on an option. The menu stays open and the well keeps
// the caret; letting the press fall through would close the menu on the value
// the cursor happened to rest on, so aiming at the well would commit a choice.
// A press on the well's clear glyph never reaches here: searchClearPress takes
// it on the press, before this runs on the release.
func (h *PanelHost) pressMenuFilter(r *Registry) bool {
	r.rebuildPanel(h)
	return true
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
	// An open picker owns text entry for as long as it is open. Its well is
	// deliberately not focusable — the menu node takes the presses — so it is
	// answered for here rather than through the focused node.
	if h.menu.Filtering() && h.menu.Opened() {
		if !h.menu.Edit(fn) {
			return false
		}
		r.rebuildPanel(h)
		return true
	}
	n := h.focused()
	f := h.fieldFor(n)
	if f == nil {
		return false
	}
	fn(f)
	f.SyncTo(n)
	return h.fieldChanged(r, n, f)
}

// fieldChanged carries a field's new text to whoever owns it: a plugin's
// change event, the Bluetooth prompt, a panel query, the plugin manager, or
// a setting.
func (h *PanelHost) fieldChanged(r *Registry, n *ui.Node, f *ui.Field) bool {
	if h.id == PanelSettings {
		if role, ok := strings.CutPrefix(n.Action, "palette-role:"); ok {
			r.paletteRoleEdited(h, role, f.Text)
			return true
		}
		switch n.Action {
		case "palette-edit-name":
			r.paletteNameEdited(h, f.Text)
			return true
		case "palette-name", "palette-import-path":
			return true // read when their button is pressed; never a setting
		}
	}
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
		if h.id == PanelPluginStore {
			h.pluginStoreQuery.Text = f.Text
			h.pluginStoreScroll = 0
		}
		if h.id == PanelLauncher {
			// A failed activation's error answers that attempt, not the
			// search the user has moved on to.
			h.errLabel = ""
			h.launcherAttempt++
			h.launcherSel = 0
			h.launcherScroll = 0
			r.launcherSendQuery(h, h.query)
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

// fieldFor is the retained editor behind a text-field node: the Bluetooth
// PIN, the network password, a panel search, a plugin field's retained
// editor, or a settings entry. It is created on first use and synced from
// the node, so every path that edits or paints a field shares one state.
func (h *PanelHost) fieldFor(n *ui.Node) *ui.Field {
	if n == nil || n.Kind != ui.KindTextField {
		return nil
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
			slot = &retainedEditor{field: seedField(n), reseed: n.Reseed}
			h.editors[k] = slot
		}
		slot.field.SyncFrom(n)
		slot.field.Masked = n.Masked
		f = slot.field
	} else if store := pluginSettingStoreKey(n.Action); store != "" {
		if h.fields == nil {
			h.fields = map[string]*ui.Field{}
		}
		f = h.fields[store]
		if f == nil {
			f = seedField(n)
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
			f = seedField(n)
			h.fields[path] = f
		} else {
			f.SyncFrom(n)
		}
	}
	return f
}

// applyEditorView marks the focused field on a paint copy and gives it the
// scroll that keeps its caret visible. It runs after layout, so bounds are
// real, and measures with the same text metrics the painter uses.
func (h *PanelHost) applyEditorView(root *ui.Node) {
	focused := h.focused()
	if focused == nil || focused.Kind != ui.KindTextField {
		return
	}
	f := h.fieldFor(focused)
	n := mirrorNode(h.root, root, focused)
	if f == nil || n == nil {
		return
	}
	n.Editing = true
	// The field owns caret and selection. A builder may have written the
	// caret at the end of the text; the painted one is where the user put it.
	f.SyncTo(n)
	box := render.FieldTextRect(n)
	measure := h.measureText()
	attrs := ui.TextAttrsOf(n)
	if n.Multiline {
		lines := strings.Count(ui.DisplayPrefix(n, n.Cursor), "\n")
		_, lineH := measure(" ", attrs)
		visible := max(box.H/max(lineH, 1), 1)
		switch {
		case lines < f.ScrollY:
			f.ScrollY = lines
		case lines >= f.ScrollY+visible:
			f.ScrollY = lines - visible + 1
		}
		n.ScrollY = f.ScrollY
		return
	}
	caretX, _ := measure(ui.DisplayPrefix(n, n.Cursor)+ui.DisplayPreedit(n), attrs)
	textW, _ := measure(ui.DisplayText(n)+ui.DisplayPreedit(n), attrs)
	f.ScrollX = ui.KeepCaretVisible(f.ScrollX, caretX, textW, box.W, 8)
	n.ScrollX = f.ScrollX
}

// mirrorNode finds target in live and returns the node at the same place in
// cp, a copyNode of live. Position, not key: a panel search carries neither a
// Key nor an Action, and copyNode keeps every child in order.
func mirrorNode(live, cp, target *ui.Node) *ui.Node {
	if live == nil || cp == nil {
		return nil
	}
	if live == target {
		return cp
	}
	if len(live.Children) != len(cp.Children) {
		return nil
	}
	for i, c := range live.Children {
		if m := mirrorNode(c, cp.Children[i], target); m != nil {
			return m
		}
	}
	return nil
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

// leaveTextField moves focus off a focused text field to the first control
// that is not one, and reports whether it did. A sticky's Esc uses it so the
// first press stops typing and only the second closes the note.
func (h *PanelHost) leaveTextField() bool {
	n := h.focused()
	if n == nil || n.Kind != ui.KindTextField {
		return false
	}
	for i, f := range h.focus {
		if f != nil && f.Kind != ui.KindTextField {
			h.roving.Set(i)
			return true
		}
	}
	return false
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
	if h.id == PanelPluginStore {
		return h.activatePluginStore(r, n)
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
		return r.handlePluginBar(n.Action, wayland.Event{Kind: wayland.EventPointerRelease, Button: 272}, 0)
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
				if !m.PickAt(n, h.hoverX, h.hoverY) {
					// A press inside the popup that is not on an option: the
					// filter well. Keep the menu open and let the well take it.
					return h.pressMenuFilter(r)
				}
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
	if r.handlePalettes(h, n) {
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
			if !m.PickAt(n, h.hoverX, h.hoverY) {
				return h.pressMenuFilter(r)
			}
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
	if strings.HasPrefix(n.Action, "bar-") && h.barActivate(r, n.Action) {
		return true
	}
	if name, ok := strings.CutPrefix(n.Action, "template-overwrite:"); ok {
		r.templateMu.Lock()
		if r.templateForce == nil {
			r.templateForce = map[string]bool{}
		}
		r.templateForce[name] = true
		r.templateMu.Unlock()
		// A config rewrite is the one path that re-runs every template
		// apply; the force flag is consumed there, off this goroutine.
		// Persist the draft, not the committed config: the refusal row is
		// shown inside the settings view, and overwriting must not throw
		// away the edits the user is looking at.
		if err := r.writeConfig(h.draft); err != nil {
			h.errLabel = err.Error()
		}
		r.rebuildPanel(h)
		return true
	}
	if strings.HasPrefix(n.Action, "section:") {
		section := strings.TrimPrefix(n.Action, "section:")
		if h.id == PanelControlCenter {
			return h.selectControlCentreSection(r, section)
		}
		if h.id == PanelSettings {
			r.settingsSectionChangingLocked(h, section)
		}
		h.section, h.settingsPage, h.settingsScrollTop = section, "", true
		r.rebuildPanel(h)
		return true
	}
	if page, ok := strings.CutPrefix(n.Action, "page:"); ok {
		h.settingsPage, h.settingsScrollTop = page, true
		r.rebuildPanel(h)
		return true
	}
	if rest, ok := strings.CutPrefix(n.Action, "step:"); ok {
		dir, path, found := strings.Cut(rest, ":")
		if e := h.set.ByPath(path); found && e != nil && e.Get != nil {
			value, err := strconv.Atoi(e.Get(h.draft))
			if err == nil {
				if dir == "up" {
					value++
				} else {
					value--
				}
				h.commitSetting(r, e, strconv.Itoa(value))
				r.rebuildPanel(h)
			}
		}
		return true
	}
	if rest, ok := strings.CutPrefix(n.Action, "pick:"); ok {
		path, value, found := strings.Cut(rest, "=")
		if e := h.set.ByPath(path); found && e != nil {
			h.commitSetting(r, e, value)
			r.rebuildPanel(h)
		}
		return true
	}
	if path, ok := strings.CutPrefix(n.Action, "browse:"); ok {
		e := h.set.ByPath(path)
		if e == nil {
			return true
		}
		// The browser is the menu the enum entries already use, filled with
		// where this path can go from where it is.
		if h.menus == nil {
			h.menus = map[string]*Menu{}
		}
		m := NewMenu(settingsBrowseOptions(e.Get(h.draft)), 0)
		m.Open()
		h.menus[path] = m
		h.menu = m
		h.menuPath = path
		r.rebuildPanel(h)
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
	case "plugin-calendar:open":
		if r.plugins != nil {
			_, _ = r.plugins.openPanel("org.sysc.calendar", v1.PanelParams{Entry: "panel"})
		}
	case "session-lock", "session-logout", "session-suspend", "session-display-off", "session-reboot", "session-poweroff":
		r.runSessionAction(h, n.Action)
	}
	return true
}

func (h *PanelHost) activateNotify(r *Registry, n *ui.Node) bool {
	action := n.Action
	h.lastAction = action
	if rest, ok := strings.CutPrefix(action, "notify:center:"); ok {
		switch {
		case rest == "clear", rest == "clear-all":
			r.clearAll()
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

// clearAll sends the service-owned scopes in order so active notifications
// dismissed by the first command cannot repopulate history after it is cleared.
func (r *Registry) clearAll() {
	r.sendNotify(protocol.Command{Kind: protocol.CommandDismissAll})
	r.sendNotify(protocol.Command{Kind: protocol.CommandHistoryClear})
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
	if h.id == PanelSettings {
		h.settingsScroll = settingsScrollOffset(h.root)
		if h.settingsScrollTop {
			h.settingsScroll, h.settingsScrollTop = 0, false
		}
	}
	focusedKey := ""
	focusedName := ""
	focusedKind := ui.Kind(0)
	if h.id == PanelClipboard || h.id == PanelPluginStore {
		if focused := h.focused(); focused != nil {
			focusedKey = focused.StableKey()
			focusedName = focused.Name
			focusedKind = focused.Kind
		}
	}
	h.root = r.panelTree(h)
	if h.id == PanelPlugin {
		if h.editors == nil {
			h.editors = map[string]*retainedEditor{}
		}
		overlayEditors(h.root, h.editors)
	}
	probe := copyNode(h.root)
	if err := h.resolveEffectMotionLocked(probe); err != nil {
		h.errLabel = err.Error()
	}
	resolveProgressMotion(h.anim, probe)
	h.noteBadges(h.root)
	resolveSpriteMotion(h.anim, probe)
	h.focus = ui.Focusables(h.root)
	h.roving.Count = len(h.focus)
	h.roving.Set(idx)
	restoredFocus := false
	if focusedKey != "" {
		for i, n := range h.focus {
			if n != nil && n.StableKey() == focusedKey {
				h.roving.Set(i)
				restoredFocus = true
				break
			}
		}
	}
	if h.id == PanelPluginStore && !restoredFocus && focusedKey == "" && focusedName != "" {
		for i, n := range h.focus {
			if n != nil && n.Kind == focusedKind && n.Name == focusedName {
				h.roving.Set(i)
				restoredFocus = true
				break
			}
		}
	}
	if h.id == PanelPluginStore && !restoredFocus {
		h.focusPluginStoreSelection()
	}
	if h.id == PanelNotifications {
		r.syncNotificationsSize(h)
	}
	if h.logicalW > 0 {
		_ = h.configure(h.logicalW, h.logicalH, h.scale120)
	}
	if h.stopAnim != nil {
		r.startSurfaceFrames(h)
	}
}

// resolveEffectMotionLocked targets the panel's effect phases on the supplied
// render tree. The caller owns Registry.mu; root is expected to be a copy of
// the retained tree so phase resolution cannot mutate the model being rebuilt.
func (h *PanelHost) resolveEffectMotionLocked(root *ui.Node) error {
	if h == nil || h.anim == nil {
		return nil
	}
	type effectTarget struct {
		node *ui.Node
		key  string
	}
	var targets []effectTarget
	var walk func(*ui.Node) error
	walk = func(n *ui.Node) error {
		if n == nil {
			return nil
		}
		if n.Kind == ui.KindEffect {
			key := n.StableKey()
			if key == "" {
				return fmt.Errorf("shell: effect node is missing a stable key")
			}
			targets = append(targets, effectTarget{node: n, key: key})
		}
		for _, child := range n.Children {
			if err := walk(child); err != nil {
				return err
			}
		}
		return nil
	}
	if err := walk(root); err != nil {
		return err
	}

	seen := make(map[string]bool, len(targets))
	for _, target := range targets {
		seen[target.key] = true
		h.anim.TargetLoop(target.key, animEffect, 0, 1, effectTrip, ui.GradientLoop)
		target.node.EffectPhase = h.anim.Value(target.key, animEffect)
	}
	for key := range h.anim.values {
		if key.channel == animEffect && !seen[key.node] {
			delete(h.anim.values, key)
		}
	}
	return nil
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
		return monitorPanelTree(h, r.monitorViewLocked(h))
	case PanelSession:
		return sessionTree(h, r.sample, r.cfg.Session.Locker)
	case PanelSettings:
		return settingsTree(r, h)
	case PanelLauncher:
		return launcherTree(r, h)
	case PanelWallpaper:
		return wallpaperTree(r, h)
	case PanelPluginStore:
		return pluginStoreTree(r, h)
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
		return ui.Rect{W: 800, H: 650}
	case PanelSettings:
		// The open path sizes it from the output (settings redesign D10);
		// this is that rule on the 1920x1080 fallback.
		return settingsPanelSize(1920, 1080)
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
	case PanelPluginStore:
		return ui.Rect{W: 1280, H: 820}
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

// sessionProfilesReserved stands in for the power profiles while the panel is
// measured: they load after it opens.
var sessionProfilesReserved = []string{"performance", "balanced", "power-saver"}

// sessionSurfaceHeight is the session panel's height for everything it can
// show: the battery card when the sample carries a battery, and the profile
// row, which arrives after the panel opens and would otherwise push the
// actions past a body sized without it (sysc-596). It never shrinks below the
// design's target.
func (r *Registry) sessionSurfaceHeight(h *PanelHost) int {
	target := panelTargetSize(PanelSession).H
	profiles, loaded := h.profiles, h.profilesOK
	if !loaded || len(profiles) == 0 {
		h.profiles, h.profilesOK = sessionProfilesReserved, true
	}
	root := sessionTree(h, r.sample, r.cfg.Session.Locker)
	h.profiles, h.profilesOK = profiles, loaded
	ht, err := ui.ContentHeight(root, h.place.Panel.W, h.measureText())
	if err != nil {
		return target
	}
	return max(target, ht)
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
	// A toggle or a menu can change what else applies: turning the bar off
	// dims the rest of Appearance. Sliders and fields stream, so they wait.
	if h.id == PanelSettings && (n.Kind == ui.KindToggle || n.Kind == ui.KindMenu) {
		r.rebuildPanel(h)
	}
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
	h.set = r.settingsForLocked(h.draft)
	// A toggle or a menu is a decision the user has finished making, so it
	// goes to the file at once. A slider or a field is a stream of them, and
	// writing per keystroke rewrote the whole document each time.
	switch e.Kind {
	case settings.KindInt, settings.KindString:
		h.deferDraft(r)
	default:
		h.persistDraft(r)
	}
}

// settingsWriteDelay is how long a stream of edits settles before it is
// written. Short enough that releasing a slider feels like it saved, long
// enough that typing a path is one write rather than one per character.
const settingsWriteDelay = 400 * time.Millisecond

// deferDraft arms the write, replacing any write already armed, so a run of
// edits collapses into the one that follows the last of them.
func (h *PanelHost) deferDraft(r *Registry) {
	if h.writeTimer != nil {
		h.writeTimer.Stop()
	}
	delay := settingsWriteDelay
	if r != nil && r.writeDelay > 0 {
		delay = r.writeDelay
	}
	h.writeTimer = time.AfterFunc(delay, func() {
		r.mu.Lock()
		if r.panelHosts[h.id] != h {
			// The host was replaced while the edit was settling; its draft is
			// no longer the one on screen.
			r.mu.Unlock()
			return
		}
		h.writeTimer = nil
		cfg := h.draft
		r.mu.Unlock()
		// scheduleControl runs the write off the Wayland owner, re-takes the
		// lock, discards the result if the host has gone, and rebuilds.
		r.scheduleControl(h, func() error { return r.writeConfig(cfg) })
	})
}

// flushDraft writes an edit that has not settled yet. Closing the panel is
// otherwise a way to lose the last thing typed into it.
func (h *PanelHost) flushDraft(r *Registry) {
	if h.writeTimer == nil {
		return
	}
	h.writeTimer.Stop()
	h.writeTimer = nil
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
	// A virtual list's rows enter the focus order through Item, as copies
	// that are never laid out, so none of them contains any point. Resolve
	// the point in the laid-out tree and answer with the focus entry that
	// carries the same key.
	if hit := laidOutFocusableAt(h.root, x, y); hit != nil {
		key := hit.StableKey()
		for _, n := range h.focus {
			if n != nil && key != "" && n.StableKey() == key {
				return n
			}
		}
	}
	return nil
}

// laidOutFocusableAt is the innermost focusable node under the point in a
// laid-out tree, descending into a virtual list's materialised rows. Every
// container's bounds clip, so a row scrolled out of its list is not hit
// through whatever is drawn over it.
func laidOutFocusableAt(n *ui.Node, x, y int) *ui.Node {
	if n == nil || !n.Bounds.Contains(x, y) {
		return nil
	}
	for i := len(n.Children) - 1; i >= 0; i-- {
		if hit := laidOutFocusableAt(n.Children[i], x, y); hit != nil {
			return hit
		}
	}
	if n.Focusable {
		return n
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
	if h.anim == nil || !h.anim.running.CompareAndSwap(false, true) {
		return
	}
	if h.anim.Settled() && !mediaPageFramesWantedLocked(r, h) {
		h.anim.running.Store(false)
		return
	}
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
		h.anim.running.Store(false)
		r.mu.Unlock()
	}()
	// The cap is resolved per tick rather than once: a surface carrying an
	// effect paces its drift far slower than its interactions, and which of
	// the two is in flight changes while this loop runs.
	frameCap := func() time.Duration {
		r.mu.Lock()
		defer r.mu.Unlock()
		if h.anim.SettledExceptEffects() {
			return effectFrameCap
		}
		return h.anim.frameCap()
	}
	r.mu.Lock()
	wake := h.anim.wake
	r.mu.Unlock()
	animateSurfaceResting(h.stopAnim, wake, func() bool {
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
	}, frameCap, func() (time.Duration, bool) {
		r.mu.Lock()
		defer r.mu.Unlock()
		if mediaPageFramesWantedLocked(r, h) {
			return 0, false
		}
		return h.anim.SpriteRest()
	})
}

func (r *Registry) teardownPanelLocked(id PanelID) {
	// Every close of the store passes here (Escape, close, the shield, another
	// panel taking its place), and each must stop the worker fetching media
	// for a panel no one can see.
	if id == PanelPluginStore && r.pluginStore != nil {
		r.pluginStore.Want(nil)
	}
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
	h.flushDraft(r)
	if id == PanelSettings && h.palettes.preview {
		h.palettes.preview = false
		go r.themePreviewHide()
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
	if id == PanelMonitor {
		// Owners are resolved for the panel's lifetime (D10).
		r.usernames = nil
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
	if !r.hasPanelOnOutputLocked(h.output) {
		r.sendAux(wayland.AuxRequest{Output: h.output, ID: panelShieldSurfaceID(h.output)})
		delete(r.panelShields, h.output)
	}
	releaseAll(h.leases)
	h.leases = nil
	releaseAll(h.subjectLeases)
	h.subjectLeases, h.ccIface, h.ccDevice = nil, "", ""
	h.ccRootSource, h.ccRootDevice, h.ccRootSelectionSource = "", "", ""
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

func (r *Registry) sendAuxWait(ctx context.Context, req wayland.AuxRequest) error {
	req.Reply = make(chan error, 1)
	select {
	case r.aux <- req:
	case <-r.closed:
		return errors.New("shell is closing")
	case <-ctx.Done():
		return ctx.Err()
	}
	select {
	case err := <-req.Reply:
		return err
	case <-r.closed:
		return errors.New("shell is closing")
	case <-ctx.Done():
		// Preserve request order: if open was already dequeued, this close
		// retires it after the owner finishes handling the open.
		select {
		case r.aux <- wayland.AuxRequest{Output: req.Output, ID: req.ID}:
		case <-r.closed:
		}
		return ctx.Err()
	}
}

func (r *Registry) publishSurface(global uint32, surfaceID string) {
	// Blocking is bounded: the owner's bridge goroutine drains this channel
	// into an unbounded queue without taking r.mu (see Registry.publish).
	// Dropping here is a surface that never repaints, which is the defect.
	select {
	case r.invalidations <- wayland.Invalidation{Global: global, SurfaceID: surfaceID}:
	case <-r.closed:
	}
}

func (r *Registry) runSessionAction(h *PanelHost, action string) {
	argv := sessionArgv(action, r.cfg.Session.Locker)
	run := r.runArgv
	// loginctl actions run under a 5-second timeout; holding Registry.mu
	// across them stalls every relay and the Wayland owner (GH #5). Launch
	// off the lock and commit the panel result after re-acquiring it, the
	// same prepare/commit shape scheduleControl uses.
	go func() {
		err := run(argv)
		r.mu.Lock()
		defer r.mu.Unlock()
		if r.panelHosts[h.id] != h {
			return
		}
		if err != nil {
			h.errLabel = err.Error()
			r.rebuildPanel(h)
			r.publishSurface(h.output, panelSurfaceID(h.id))
			return
		}
		r.closePanelLocked(h.id)
	}()
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

// seedField is a fresh editor holding a node's value with the caret where
// the node put it and nothing selected.
func seedField(n *ui.Node) *ui.Field {
	f := ui.NewField(n.Text)
	f.PreeditText = n.Preedit
	f.Multiline, f.SubmitOnEnter, f.Masked = n.Multiline, n.SubmitOnEnter, n.Masked
	f.SetCaret(n.Cursor, false)
	return f
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
					eds[k] = &retainedEditor{field: seedField(n), reseed: n.Reseed}
				} else {
					slot.field.Multiline = n.Multiline
					slot.field.SubmitOnEnter = n.SubmitOnEnter
					slot.field.Masked = n.Masked
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

// notePluginStoreScroll carries a scroll the pointer or a key made on the
// store's tree back to the host, which the next rebuild reads: otherwise any
// snapshot from the worker put the grid back at the top.
func (h *PanelHost) notePluginStoreScroll(s *ui.Node) {
	if h.id != PanelPluginStore || s == nil {
		return
	}
	if h.pluginStoreDetail != "" {
		h.pluginStoreDetailScroll = s.ScrollOffset
	} else if s.Key == "plugin-store-grid" {
		h.pluginStoreScroll = s.ScrollOffset
	}
}

// afterPluginStoreScroll rebuilds the grid after it scrolled, so the rows
// that came into view ask the worker for their screenshots.
func (h *PanelHost) afterPluginStoreScroll(r *Registry) {
	if r != nil && h.id == PanelPluginStore && h.pluginStoreDetail == "" {
		r.rebuildPanel(h)
	}
}

// treeScale is the scale the tree was built at, for a panel whose tree
// measures text as it is built; measured is false for every other panel.
func (h *PanelHost) treeScale() (built int, measured bool) {
	switch h.id {
	case PanelSettings:
		return h.settingsTreeScale, true
	case PanelPluginStore:
		return h.pluginStoreTreeScale, true
	}
	return 0, false
}
