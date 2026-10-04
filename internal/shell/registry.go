package shell

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	launcher "github.com/Nomadcxx/sysc-launch"
	"github.com/Nomadcxx/sysc-shell/internal/config"
	"github.com/Nomadcxx/sysc-shell/internal/icons"
	"github.com/Nomadcxx/sysc-shell/internal/notifyclient"
	"github.com/Nomadcxx/sysc-shell/internal/platform/niri"
	"github.com/Nomadcxx/sysc-shell/internal/platform/wayland"
	"github.com/Nomadcxx/sysc-shell/internal/plugin/store"
	"github.com/Nomadcxx/sysc-shell/internal/services"
	"github.com/Nomadcxx/sysc-shell/internal/settings"
	"github.com/Nomadcxx/sysc-shell/internal/theme"
	"github.com/Nomadcxx/sysc-shell/internal/theming"
	"github.com/Nomadcxx/sysc-shell/internal/trayclient"
	"github.com/Nomadcxx/sysc-shell/internal/ui"
	"github.com/Nomadcxx/sysc-shell/internal/wallpaper"
	"github.com/Nomadcxx/sysc-shell/internal/walls"
	tray "github.com/Nomadcxx/sysc-tray/protocol"
)

// Registry owns every bar, the services they consume, and the state they read.
//
// Bars are keyed by wl_registry global name. A connector is an attribute: two
// globals may briefly share one during a reconnect, and they must stay
// distinct instances with distinct service leases.
//
// Niri state is keyed by connector and held whether or not a host exists,
// because a Niri event may name an output whose wl_output has not been
// announced yet or has already been removed. A host is never created or
// destroyed from a Niri event.
type Registry struct {
	mu           sync.Mutex
	cfg          config.Config
	outputs      map[string]outputState
	bars         map[uint32]*Bar
	leases       map[uint32][]*services.Lease
	niriSnapshot niri.Snapshot
	now          time.Time
	focused      string
	layouts      niri.KeyboardLayouts
	layoutsSeen  bool
	// caps is what the compositor last said it can do. The zero value, no
	// blur, is also the answer for a compositor without the protocol.
	caps wayland.Capabilities
	// capsKnown is set by the first report, which is logged even when it
	// matches the zero value.
	capsKnown bool

	clock        *services.Clock
	metrics      *services.Metrics
	weather      *services.Weather
	sample       services.Snapshot
	reading      services.Reading
	mediaState   services.MediaState
	mediaPlayers []services.Player
	// controlIdentity is captured outside Registry.mu so the control centre
	// never reads /proc or user databases from the Wayland owner.
	controlIdentity ccIdentity
	// machineFacts is refreshed before lock-held panel rebuilds. It is a value
	// copy so monitor trees can consume it without doing I/O under Registry.mu.
	machineFacts        machineFacts
	controlAvatarFailed map[icons.Key]struct{}

	tokens theme.Tokens
	// themeErr is why the published palette is not the requested one, empty
	// when it is. Surfaced by the picker; never fatal.
	themeErr string
	// previewing keeps the preview session open until hide so its committed
	// generation reason can be restored. previewTheme is the separate in-memory
	// presentation used by every surface; commits clear it without persisting it.
	// These fields are protected by Registry.mu.
	previewing     bool
	previewPrevErr string
	previewTheme   *themePreviewState
	previewRequest uint64
	// templateRefusals names templates whose files the shell refused to write
	// because their current bytes are not the shell's last render. The
	// settings Templates section surfaces them with an overwrite action.
	// templateForce marks templates the user explicitly overrode a refusal
	// for; the next apply that succeeds consumes the entry.
	//
	// generateTheme runs off the Wayland owner (wallpaper applies, config
	// reload) and deliberately outside Registry.mu, while the panel handler
	// writes templateForce under it. A leaf mutex guards the pair; nothing
	// takes Registry.mu while holding it.
	templateMu       sync.Mutex
	templateRefusals map[string]string
	templateForce    map[string]bool
	themeGen         theme.Generator
	// paletteStore holds the user's saved palettes. Set once in NewRegistry and
	// never reassigned outside tests, so it is read without Registry.mu.
	paletteStore *theme.Store
	// paletteImportDir is where the import field starts (the Downloads
	// folder, shown with ~), resolved once at construction so building the
	// page reads no file.
	paletteImportDir string
	// palettes is the last listing of paletteStore. Registry.mu; replaced
	// whole by refreshPalettes.
	palettes []theme.PaletteInfo
	// paletteRefreshMu serialises a listing with its swap, so a listing taken
	// before a save never lands after one taken after it. Taken before
	// Registry.mu, never while holding it.
	paletteRefreshMu sync.Mutex
	// paletteLister reads the store; tests replace it to hold a listing open.
	paletteLister func(*theme.Store) []theme.PaletteInfo
	// themeGenMu serializes generation across goroutines (sysc-780). Committed
	// generation shares one cache path; previews use temporary paths but still
	// keep matugen single-flight. Leaf lock: nothing takes Registry.mu while it
	// is held, and painting takes r.mu only after generation releases it.
	themeGenMu sync.Mutex

	// invalidations carries one entry per bar whose rendered text changed.
	// The Wayland owner receives from it; the registry owns it and never
	// closes it.
	invalidations chan wayland.Invalidation
	aux           chan wayland.AuxRequest
	// selections carries text fields' copy and paste requests to the
	// platform's clipboard.
	selections chan wayland.SelectionRequest
	panels     PanelSet
	panelHosts map[PanelID]*PanelHost
	// panelShields records which panel host opened each output's shared shield,
	// even if that host closes while another panel keeps the shield alive.
	panelShields map[uint32]*PanelHost
	// panelOrder breaks ties between open panels when their shared shield
	// dismisses the newest one first.
	panelOrder uint64
	// roots is the one interactive root the process allows at a time. Open
	// panels share one root; other modal surfaces still replace the whole group.
	roots rootChain
	// closed unblocks a pending publish at shutdown.
	closed     chan struct{}
	closeOnce  sync.Once
	dwell      *dwell
	tooltips   *tooltipHost
	configPath string
	// writeDelay is how long a stream of edits settles before it reaches the
	// file. Zero takes settingsWriteDelay; tests shorten it.
	writeDelay           time.Duration
	reloads              chan<- struct{}
	audio                *services.Audio
	brightness           *services.Brightness
	lockKeys             *services.LockKeys
	network              *services.Network
	media                *services.Media
	mediaRelayCancel     chan struct{}
	bluetooth            *services.Bluetooth
	bluetoothState       services.BluetoothState
	bluetoothRelayCancel chan struct{}
	// Bluetooth discovery operations are serialized off the owner so a root
	// replacement cannot race its stop with the next body's start.
	bluetoothOpsMu   sync.Mutex
	bluetoothOpsTail chan struct{}
	osd              *OSDManager
	audioLease       *services.Lease
	brightLease      *services.Lease
	networkLease     *services.Lease
	// runArgv launches a session action. Tests replace it per Registry.
	runArgv func([]string) error
	// locker tracks the session-lock process; lockerSpawn is the test seam.
	locker         *lockerManager
	lockerSpawn    lockerSpawnFn
	lockerRunning  bool // state cache for lock-held readers; guarded by mu
	lockerAcquired bool
	// lookPath finds a binary on PATH. Tests replace it per Registry.
	lookPath func(string) (string, error)
	// animClock is the clock a panel animator samples. Tests freeze it to
	// watch the reveal's pacing without racing the wall clock; production
	// leaves it nil and the animator runs on time.Now.
	animClock func() time.Time
	// runArgvOutput captures stdout of powerprofilesctl list. Tests replace it.
	runArgvOutput func([]string) (string, error)
	// startInhibit creates the process-backed caffeine hold. Tests replace it.
	startInhibit    func() (io.Closer, error)
	inhibit         io.Closer
	inhibitWanted   bool
	inhibitStarting bool
	// idleSvc is the display-power policy. Nil means no idle service is
	// wired, which is the test and disabled configuration.
	idleSvc *services.IdleService
	// screenSaver owns the org.freedesktop.ScreenSaver name. Nil when the
	// name belongs to another process, which is a degraded but valid state.
	screenSaver *services.ScreenSaverService
	// externalInhibitors is the latest published screensaver inhibitor list.
	// These hold the shell's idle timers only; logind sleep blocking is
	// untouched, which is the per-application behavior the protocol asks for.
	externalInhibitors []services.ScreenSaverInhibitor

	running      []runningAppSlot
	runningIndex []runningAppEntry
	// usernames resolves process owners for the system monitor. It carries
	// its own lock; see usernameCache.
	usernames      *usernameCache
	runningMenu    *runningAppMenuHost
	windowSwitcher *windowSwitcherHost
	// niriSend is the FocusWindow/CloseWindow seam. Tests replace it; nil
	// sends niri.Action on $NIRI_SOCKET off this goroutine.
	niriSend func(any) error
	// killPID SIGTERMs one client pid. Tests replace it; nil uses os.FindProcess.
	killPID func(int) error
	// signalProcess validates PID identity and delivers TERM/KILL off Registry.mu.
	signalProcess func(services.ProcessIdentity, syscall.Signal) error

	// notify is the service-owned notification projection.
	notify *notifyState
	// batteryWarning is the shell-owned desired-state reducer for the one
	// keyed battery producer notification.
	batteryWarning *batteryWarning

	// clipboard is the daemon-owned metadata projection. Payload bytes never
	// enter Registry or a bar view.
	clipboard       clipboardProjection
	clipboardSender clipboardCommandSender

	// tray is the service-owned tray projection.
	tray                 *trayState
	trayCh               chan trayclient.Message
	traySender           trayCommandSender
	trayMenu             *trayMenuHost
	trayDrawer           *trayDrawerHost
	trayReplies          *trayReplyTracker
	trayCloses           *trayCloseTracker
	trayIcons            *icons.Worker
	wallpaperSvc         *wallpaper.Service
	wallsService         wallsController
	wallsSnapshot        walls.Snapshot
	wallpaperThumbs      *icons.Worker
	wallpaperThumbCancel context.CancelFunc
	mediaArt             *mediaArtWorker
	trayIconCancel       context.CancelFunc
	pendingTrayMenu      pendingTrayMenu

	// plugins hosts one process per enabled plugin. Nil until BindPlugins.
	plugins *pluginHost
	// pluginStore runs its own goroutine. Tree builders read only this immutable
	// snapshot; Store.State is never called while Registry.mu is held.
	pluginStore         *store.Store
	pluginStoreSnapshot store.State
	pluginStoreReadmes  map[string]string

	// notifyCh carries client messages; main pumps it. Nil in tests that drive
	// applyNotify directly.
	notifyCh chan notifyclient.Message
	// notifySender is the client's Send seam. Nil in tests that do not
	// assert commands.
	notifySender   notifyCommandSender
	producerSender notifyProducerSender
	// pluginNotifySeq makes each plugin toast a unique producer key; the
	// service replaces live notifications that share a key.
	pluginNotifySeq atomic.Uint32
	// niriScreenshot sends a screenshot action and waits for its file. Tests
	// replace it; nil uses niri.Screenshot on $NIRI_SOCKET.
	niriScreenshot func(ctx context.Context, action any, path string) error
	// screenshotDir names the directory captures are saved to. Tests replace
	// it; nil is screenshot.Dir.
	screenshotDir func() string
	// selector is the open region selector, if any. Registry.mu.
	selector *regionSelector
	// toasts hosts one toast stack per output, created when wiring binds it.
	toasts *toastHost
	// depthClocks contains one click-through wallpaper clock per accepted mask.
	depthClocks     *depthClockHost
	depthClockLease *services.Lease
	// launcherSvc is created on the first launcher open; nil until then.
	launcherSvc *launcher.Service
	// launcherMu guards the query generation, its current plan,
	// and the batch of provider rows being assembled into one snapshot.
	// Lock order is Registry.mu then launcherMu. The service goroutine takes
	// launcherMu only; it never takes Registry.mu while ranking.
	launcherMu    sync.Mutex
	launcherGen   uint64
	launcherPlan  launcherPlan
	launcherBatch launcherBatch
	launcherSnaps chan launcherSnap
	// launcherRankWait, when set, runs on the service goroutine after a rank
	// and before that rank is stamped. Tests hold a publish there.
	launcherRankWait func(query string)
	// onLauncherDequeued runs on the relay after a snapshot is received and
	// before Registry.mu is taken.
	onLauncherDequeued func()
	// afterLauncherSnap runs on the relay after a received snapshot has been
	// accepted or ignored. Registry.mu.
	afterLauncherSnap func()
}

func listPalettes(st *theme.Store) []theme.PaletteInfo {
	if st == nil {
		return nil
	}
	return st.List()
}

// refreshPalettes re-reads the palettes directory outside Registry.mu and
// swaps the snapshot under it. Every Registry.Palette* mutation calls it, and
// so do opening Settings and entering the Palettes section, so a hand-edited
// file shows up the next time the page is entered. Must not be called with
// Registry.mu held.
func (r *Registry) refreshPalettes() {
	r.paletteRefreshMu.Lock()
	defer r.paletteRefreshMu.Unlock()
	list := r.paletteLister(r.paletteStore)
	r.mu.Lock()
	r.palettes = list
	r.mu.Unlock()
}

// settingsFor builds the settings registry for cfg with the saved palettes.
// It must not be called with Registry.mu held; use settingsForLocked there.
func (r *Registry) settingsFor(cfg config.Config) *settings.Registry {
	r.mu.Lock()
	list := r.palettes
	r.mu.Unlock()
	return settings.DefaultFor(cfg, settings.WithCustomPalettes(settings.CustomPalettesFrom(list)))
}

// settingsForLocked is settingsFor for callers that already hold Registry.mu.
// It reads only the snapshot, so it is cheap enough for every settings edit.
func (r *Registry) settingsForLocked(cfg config.Config) *settings.Registry {
	return settings.DefaultFor(cfg, settings.WithCustomPalettes(settings.CustomPalettesFrom(r.palettes)))
}

// paletteDir is where saved palettes live: beside the config file main
// persists to (config.DefaultPath). Empty when there is no config directory.
func paletteDir() string {
	path := config.DefaultPath()
	if path == "" {
		return ""
	}
	return filepath.Join(filepath.Dir(path), "palettes")
}

func NewRegistry(cfg config.Config) *Registry {
	gen := theme.Generator{}
	var palettes *theme.Store
	if dir := paletteDir(); dir != "" {
		palettes = &theme.Store{Dir: dir}
		// Set before the first Generate below, and never written again, so
		// generateOnlyWith may copy r.themeGen without Registry.mu.
		gen.Custom = palettes
	}
	r := &Registry{
		cfg:     cfg,
		outputs: make(map[string]outputState),
		bars:    make(map[uint32]*Bar),
		leases:  make(map[uint32][]*services.Lease),
		clock:   services.NewClock(),
		metrics: services.NewMetrics(),
		weather: services.NewWeather(
			cfg.Weather.Latitude, cfg.Weather.Longitude, weatherUnit(cfg.Weather.Unit)),
		themeGen:         gen,
		paletteStore:     palettes,
		paletteLister:    listPalettes,
		paletteImportDir: paletteImportDir(),
		templateForce:    map[string]bool{},
		invalidations:    make(chan wayland.Invalidation, 8),
		aux:              make(chan wayland.AuxRequest, 8),
		selections:       make(chan wayland.SelectionRequest, 8),
		panelHosts:       make(map[PanelID]*PanelHost),
		panelShields:     make(map[uint32]*PanelHost),
		closed:           make(chan struct{}),
		dwell:            newDwell(defaultDwell),
		runArgv:          runArgvDefault,
		lookPath:         exec.LookPath,
		runArgvOutput:    runArgvOutputDefault,
		startInhibit:     startInhibitDefault,
		signalProcess:    signalProcessDefault,
		notify:           newNotifyState(),
		batteryWarning:   newBatteryWarning(),
		clipboard:        newClipboardProjection(),
		tray:             newTrayState(),
		trayCh:           make(chan trayclient.Message, 32),
		// Intrinsic state, not a binding: a message can settle a close before
		// anything is bound, and a nil tracker would drop it.
		trayCloses:      newTrayCloseTracker(),
		notifyCh:        make(chan notifyclient.Message, 32),
		controlIdentity: readCCIdentity(),
		machineFacts:    readMachineFacts(),
	}
	r.depthClocks = newDepthClockHost(r, nil)
	r.weather.SetCity(cfg.Weather.City)
	// Construction is single-threaded, so the first snapshot needs no lock.
	r.palettes = listPalettes(palettes)
	r.tokens, r.themeErr = tokensAndReason(r.generateTheme(cfg))
	r.osd = newOSDManager(r, 0)
	r.tooltips = newTooltipHost(r, nil)
	go r.relayTooltips(r.dwell)
	// DND toggles often run under Registry.mu; Show takes it, so publish from
	// a separate goroutine after the setter returns.
	r.notify.onDND = func(on bool) { go r.OSD().Show(OSDView{Kind: osdDND, On: on}) }
	r.setAudio(services.NewAudio(0, ""))
	// DDC probing opens real i2c buses; keep unit and shell tests on sysfs
	// only so a test run never puts traffic on a live monitor.
	if runningAsTest() {
		r.setBrightness(services.NewBrightness("", "", 0))
	} else {
		r.setBrightness(services.NewBrightnessDDC("", "", 0))
	}
	if !runningAsTest() {
		r.lockKeys = services.NewLockKeys("", 0)
		r.lockKeys.Start()
		go r.relayLockKeysOSD(r.lockKeys)
	}
	// The media service opens a session-bus connection, which no unit test
	// should need. Tests get the inert service and install their own over the
	// fake, the same way the network service below is skipped.
	if runningAsTest() {
		r.setMedia(services.NewUnavailableMedia())
	} else {
		r.setMedia(services.NewSessionMedia())
	}
	if r.media != nil && (cfg.Media.Preferred != "" || len(cfg.Media.Blacklist) > 0) {
		r.media.Configure(cfg.Media.Preferred, cfg.Media.Blacklist)
	}
	// The network service opens a system-bus connection, which no unit test
	// should need. It is skipped under test for the same reason the wallpaper
	// service below is: a test that reaches the developer's real session is a
	// test that fails on a build machine. Tests install their own service.
	if !runningAsTest() {
		r.setNetwork(services.NewSystemNetwork())
		r.setBluetooth(services.NewSystemBluetooth())
	}
	// The wallpaper service starts with the registry, not with the picker: an
	// output's wallpaper has to come back at login whether or not anyone opens
	// the panel (D20). It is skipped under test, where starting it would read
	// the developer's real assignment file and launch real engines; those
	// tests install their own service.
	if !runningAsTest() {
		r.mu.Lock()
		r.wallpaperStartLocked()
		// The launcher scans XDG for .desktop files. Doing that when the panel
		// first opens makes the very first Mod+D of a session the slow one, on
		// a control whose whole job is to be instant. It is skipped under test
		// for the same reason as the wallpaper service: it would read the
		// developer's real applications.
		r.launcherServiceLocked()
		r.mu.Unlock()
	}
	return r
}

func (r *Registry) OSD() *OSDManager { return r.osd }

func (r *Registry) setAudio(a *services.Audio) {
	if r.audioLease != nil {
		r.audioLease.Release()
		r.audioLease = nil
	}
	if r.audio != nil {
		r.audio.Close()
	}
	r.audio = a
	if a != nil && a.Available() {
		if l, err := a.Acquire(); err == nil {
			r.audioLease = l
		}
	}
	go r.relayAudioOSD(a)
	go r.relayMixer(a)
}

// setMedia installs the media service and relays its cached snapshots into the
// retained bar and control-centre trees. The widget and page acquire leases
// for the service's bus watch; the relay itself never keeps the service alive.
// SetIdleService installs the idle policy service. Its channels go into
// wayland.Callbacks by the caller; main wires both before Run.
func (r *Registry) SetIdleService(s *services.IdleService) {
	r.idleSvc = s
	// The initial publish matters: nothing else calls the setters until a
	// media, inhibit or config change happens, and the timeouts must be
	// armed from the configuration already loaded.
	r.pushIdleInputs()
}

// pushIdleInputsLocked re-publishes every input the idle policy reads.
// Caller holds r.mu.
func (r *Registry) pushIdleInputsLocked() {
	if r.idleSvc == nil {
		return
	}
	r.idleSvc.SetConfig(services.IdleSettings{
		BlankAc:        r.cfg.Idle.BlankAc,
		BlankBattery:   r.cfg.Idle.BlankBattery,
		SuspendAc:      r.cfg.Idle.SuspendAc,
		SuspendBattery: r.cfg.Idle.SuspendBattery,
		LockAc:         r.cfg.Idle.Lock,
		LockBattery:    r.cfg.Idle.Lock,
		MediaExempt:    r.cfg.Idle.MediaExempt,
	})
	r.idleSvc.SetMediaPlaying(r.mediaState.Status == services.PlaybackPlaying)
	r.idleSvc.SetInhibited(r.inhibitWanted || len(r.externalInhibitors) > 0)
}

// SetScreenSaver installs the bus-name owner and takes its first inhibitor
// snapshot.
func (r *Registry) SetScreenSaver(ss *services.ScreenSaverService) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.screenSaver = ss
	if ss != nil {
		r.externalInhibitors = ss.ListInhibitors()
	}
	r.pushIdleInputsLocked()
}

// SetExternalInhibitors is the screensaver service's change callback. It runs
// on that service's D-Bus signal pump goroutine.
func (r *Registry) SetExternalInhibitors(list []services.ScreenSaverInhibitor) {
	r.mu.Lock()
	r.externalInhibitors = list
	r.pushIdleInputsLocked()
	out, open := r.rebuildControlCentreLocked()
	r.mu.Unlock()
	if open {
		r.publishSurface(out, panelSurfaceID(PanelControlCenter))
	}
}

func (r *Registry) pushIdleInputs() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.pushIdleInputsLocked()
}

func (r *Registry) setMedia(m *services.Media) {
	if r.media == m {
		return
	}
	if r.mediaRelayCancel != nil {
		close(r.mediaRelayCancel)
		r.mediaRelayCancel = nil
	}
	if r.media != nil {
		r.media.Close()
	}
	r.media = m
	if m == nil {
		r.mediaState = services.MediaState{}
		r.mediaPlayers = nil
		return
	}
	r.mediaState = m.CachedState()
	r.mediaPlayers = m.Players()
	cancel := make(chan struct{})
	r.mediaRelayCancel = cancel
	go r.relayMedia(m, cancel)
	r.pushIdleInputs()
}

func (r *Registry) relayMedia(media *services.Media, cancel <-chan struct{}) {
	if media == nil {
		return
	}
	r.publishMediaSnapshot(media, media.CachedState())
	prev := media.CachedState()
	for {
		select {
		case <-r.closed:
			return
		case <-cancel:
			return
		case state := <-media.Changes():
			r.publishMediaSnapshot(media, state)
			if view, show := mediaOSD(prev, state); show {
				r.OSD().Show(view)
			}
			prev = state
		}
	}
}

// mediaArtFor returns the shared bounded art worker. Registry.mu is held.
func (r *Registry) mediaArtFor() *mediaArtWorker {
	if r.mediaArt == nil {
		r.mediaArt = newMediaArtWorker(r.applyMediaArt)
	}
	return r.mediaArt
}

// applyMediaArt reapplies the retained bar and page once a decoded cover
// arrives. Consumers ask only for Lookup results while building; decoding
// stays off-owner.
func (r *Registry) applyMediaArt(key icons.Key, image *ui.Image) {
	if image == nil {
		return
	}
	r.mu.Lock()
	keyName, ok := mediaArtRequestName(r.mediaState.ArtKey)
	if !ok || keyName == "" || key.Name != keyName {
		r.mu.Unlock()
		return
	}
	changed := make([]uint32, 0, len(r.bars))
	for global, bar := range r.bars {
		if bar.apply(r.viewLocked(bar.connector())) {
			changed = append(changed, global)
		}
	}
	h := r.panelHosts[PanelControlCenter]
	open := h != nil && h.section == "media"
	var out uint32
	if open {
		out = h.output
		r.rebuildPanel(h)
	}
	r.mu.Unlock()
	r.publish(changed)
	if open {
		r.publishSurface(out, panelSurfaceID(PanelControlCenter))
	}
}

func (r *Registry) publishMediaSnapshot(media *services.Media, state services.MediaState) {
	players := media.Players()
	r.mu.Lock()
	if r.media != media {
		r.mu.Unlock()
		return
	}
	r.mediaState = state
	r.mediaPlayers = players
	r.pushIdleInputsLocked()
	changed := make([]uint32, 0, len(r.bars))
	for global, bar := range r.bars {
		if bar.apply(r.viewLocked(bar.connector())) {
			changed = append(changed, global)
		}
	}
	out, open := r.rebuildControlCentreLocked()
	if h := r.panelHosts[PanelControlCenter]; h != nil && mediaBodyVisible(h) {
		r.startSurfaceFrames(h)
	}
	r.mu.Unlock()

	r.publish(changed)
	if open {
		r.publishSurface(out, panelSurfaceID(PanelControlCenter))
	}
}

func (r *Registry) relayMixer(audio *services.Audio) {
	if audio == nil {
		return
	}
	ch := audio.MixerChanges()
	if ch == nil {
		return
	}
	for {
		select {
		case <-r.closed:
			return
		case _, ok := <-ch:
			if !ok {
				return
			}
			r.mu.Lock()
			h := r.panelHosts[PanelAudio]
			if h == nil {
				r.mu.Unlock()
				continue
			}
			if snap := audio.Mixer(); !h.pendingAt.IsZero() && snap.At.After(h.pendingAt) {
				h.pendingVol = nil
				h.pendingMute = nil
				h.pendingAt = time.Time{}
			}
			r.rebuildPanel(h)
			out := h.output
			r.mu.Unlock()
			r.publishSurface(out, panelSurfaceID(PanelAudio))
		}
	}
}

// setNetwork swaps the network service, releasing any lease the old one held.
// The lease is what starts the D-Bus subscription, so taking it here is what
// makes the bar glyph live.
// toggleWirelessAsync flips the radio from the bar's right-click.
//
// The write is I/O over D-Bus, so it runs on its own goroutine: this handler
// is on the path that takes Registry.mu and the Wayland owner, and blocking
// either on a bus round trip stalls every bar on the machine.
func (r *Registry) toggleWirelessAsync() {
	r.mu.Lock()
	n := r.network
	r.mu.Unlock()
	if n == nil || !n.Available() {
		return
	}
	want := !n.CachedState().WirelessEnabled
	go func() { _ = n.SetWirelessEnabled(want) }()
}

func (r *Registry) setNetwork(n *services.Network) {
	if r.networkLease != nil {
		r.networkLease.Release()
		r.networkLease = nil
	}
	if r.network != nil {
		r.network.Close()
	}
	r.network = n
	if n != nil && n.Available() {
		if l, err := n.Acquire(); err == nil {
			r.networkLease = l
		}
		go r.relayNetwork(n)
	}
}

// setBluetooth installs the one process-wide BlueZ service. It has no lease:
// Bluetooth state is useful to the bar even while no panel is open, so the
// service subscribes for the Registry lifetime.
func (r *Registry) setBluetooth(bluetooth *services.Bluetooth) {
	if r.bluetooth == bluetooth {
		return
	}
	if r.bluetoothRelayCancel != nil {
		close(r.bluetoothRelayCancel)
		r.bluetoothRelayCancel = nil
	}
	if r.bluetooth != nil && r.bluetooth != bluetooth {
		_ = r.bluetooth.Close()
	}
	r.bluetooth = bluetooth
	if bluetooth == nil {
		r.bluetoothState = services.BluetoothState{}
		return
	}
	r.bluetoothState = bluetooth.CachedState()
	cancel := make(chan struct{})
	r.bluetoothRelayCancel = cancel
	go r.relayBluetooth(bluetooth, cancel)
}

func (r *Registry) relayBluetooth(bluetooth *services.Bluetooth, cancel <-chan struct{}) {
	if bluetooth == nil {
		return
	}
	r.publishBluetoothSnapshot(bluetooth, bluetooth.CachedState())
	for {
		select {
		case <-r.closed:
			return
		case <-cancel:
			return
		case state := <-bluetooth.Changes():
			r.publishBluetoothSnapshot(bluetooth, state)
		}
	}
}

// publishBluetoothSnapshot is the only bridge from the service into retained
// shell state. Cached snapshots are copied before any host is rebuilt; the bus
// is never touched while Registry.mu is held.
func (r *Registry) publishBluetoothSnapshot(bluetooth *services.Bluetooth, state services.BluetoothState) {
	r.mu.Lock()
	if r.bluetooth != bluetooth {
		r.mu.Unlock()
		return
	}
	r.bluetoothState = state
	if !state.Available || !state.Adapter.Powered {
		for _, id := range []PanelID{PanelBluetooth, PanelControlCenter} {
			if h := r.panelHosts[id]; h != nil {
				h.bluetoothDiscovery = false
			}
		}
	}
	if state.Prompt != nil && !r.bluetoothHostVisibleLocked() {
		if output, trigger := r.focusedTriggerLocked(); output != 0 {
			_ = r.openPanelRootLocked(PanelBluetooth, output, trigger)
		}
	}
	changed := make([]uint32, 0, len(r.bars))
	for global, bar := range r.bars {
		if bar.apply(r.viewLocked(bar.connector())) {
			changed = append(changed, global)
		}
	}
	var surfaces []wayland.Invalidation
	for _, id := range []PanelID{PanelBluetooth, PanelControlCenter} {
		if h := r.panelHosts[id]; h != nil {
			r.rebuildPanel(h)
			surfaces = append(surfaces, wayland.Invalidation{Global: h.output, SurfaceID: panelSurfaceID(id)})
		}
	}
	r.mu.Unlock()

	r.publish(changed)
	for _, invalidation := range surfaces {
		r.publishSurface(invalidation.Global, invalidation.SurfaceID)
	}
}

// relayNetwork is the one bridge from the pushed service into retained shell
// state. Bus reads stay off Registry.mu; the lock only applies cached state to
// bars and the open panel.
func (r *Registry) relayNetwork(network *services.Network) {
	if network == nil {
		return
	}
	network.State()
	network.AccessPoints()
	r.publishNetworkSnapshot(network)

	for {
		select {
		case <-r.closed:
			return
		case _, ok := <-network.Changes():
			if !ok {
				return
			}
			network.AccessPoints()
			r.publishNetworkSnapshot(network)
		case req, ok := <-network.SecretRequests():
			if !ok {
				return
			}
			r.presentNetworkSecret(network, req)
		case <-network.SecretExpiry():
			r.expireNetworkSecret(network)
		}
	}
}

func (r *Registry) publishNetworkSnapshot(network *services.Network) {
	r.mu.Lock()
	if r.network != network {
		r.mu.Unlock()
		return
	}
	changed := make([]uint32, 0, len(r.bars))
	for global, bar := range r.bars {
		if bar.apply(r.viewLocked(bar.connector())) {
			changed = append(changed, global)
		}
	}
	h := r.panelHosts[PanelNetwork]
	var panelOut uint32
	if h != nil {
		r.rebuildPanel(h)
		panelOut = h.output
	}
	r.mu.Unlock()

	r.publish(changed)
	if h != nil {
		r.publishSurface(panelOut, panelSurfaceID(PanelNetwork))
	}
}

func (r *Registry) presentNetworkSecret(network *services.Network, req services.SecretRequest) {
	r.mu.Lock()
	if r.network != network {
		r.mu.Unlock()
		network.CancelSecret()
		return
	}
	h := r.panelHosts[PanelNetwork]
	if h == nil {
		r.mu.Unlock()
		network.CancelSecret()
		return
	}
	h.clearNetworkSecret()
	h.networkTab = "wifi"
	h.pendingSSID = req.SSID
	h.password = ui.NewField("")
	h.password.Masked = true
	h.errLabel = ""
	r.rebuildPanel(h)
	h.focusByName("Password")
	out := h.output
	r.mu.Unlock()
	r.publishSurface(out, panelSurfaceID(PanelNetwork))
}

// expireNetworkSecret drops the password card once a prompt outlived its
// deadline. The service has already answered NetworkManager with a cancel;
// this keeps the panel from offering to submit a secret nobody waits for.
func (r *Registry) expireNetworkSecret(network *services.Network) {
	r.mu.Lock()
	if r.network != network {
		r.mu.Unlock()
		return
	}
	h := r.panelHosts[PanelNetwork]
	if h == nil || h.pendingSSID == "" {
		r.mu.Unlock()
		return
	}
	h.clearNetworkSecret()
	r.rebuildPanel(h)
	out := h.output
	r.mu.Unlock()
	r.publishSurface(out, panelSurfaceID(PanelNetwork))
}

func (r *Registry) setBrightness(b *services.Brightness) {
	if r.brightLease != nil {
		r.brightLease.Release()
		r.brightLease = nil
	}
	if r.brightness != nil {
		r.brightness.Close()
	}
	r.brightness = b
	if b != nil && b.Available() {
		if l, err := b.Acquire(); err == nil {
			r.brightLease = l
		}
	}
	go r.relayBrightnessOSD(b)
}

func (r *Registry) AudioAvailable() bool {
	return r != nil && r.audio != nil && r.audio.Available()
}

func (r *Registry) BrightnessAvailable() bool {
	return r != nil && r.brightness != nil && r.brightness.Available()
}

func (r *Registry) Status() map[string]any {
	if r == nil {
		return map[string]any{"version": "sysc-shell"}
	}
	r.mu.Lock()
	audio := r.audio != nil && r.audio.Available()
	bright := r.brightness != nil && r.brightness.Available()
	var panels []string
	for id := range r.panelHosts {
		panels = append(panels, id.String())
	}
	cfg := r.cfg
	inhibitors := make([]string, 0, len(r.externalInhibitors))
	for _, in := range r.externalInhibitors {
		inhibitors = append(inhibitors, in.App)
	}
	r.mu.Unlock()
	templates := map[string]bool{}
	for _, name := range theming.Catalog().Names() {
		templates[name] = cfg.TemplateEnabled(name)
	}
	_, err := exec.LookPath("matugen")
	return map[string]any{
		"version":         "sysc-shell",
		"audio":           audio,
		"brightness":      bright,
		"panels":          panels,
		"matugen":         err == nil,
		"templates":       templates,
		"idle_inhibitors": inhibitors,
	}
}

func (r *Registry) OSDStep(kind, action string) error {
	switch kind {
	case "audio":
		return r.stepAudio(action)
	case "brightness":
		return r.stepBrightness(action)
	default:
		return fmt.Errorf("unknown kind")
	}
}

// stepAudioAsync runs stepAudio off the calling goroutine. The bar input
// handlers run on the Wayland owner, which must never exec.
// ponytail: wpctl volume steps commute, but concurrent mute toggles can
// coalesce; add a serialized worker channel if ordering ever matters.
func (r *Registry) stepAudioAsync(action string) {
	go func() { _ = r.stepAudio(action) }()
}

func (r *Registry) stepAudio(action string) error {
	if r.audio == nil || !r.audio.Available() {
		return fmt.Errorf("audio unavailable")
	}
	lease, err := r.audio.Acquire()
	if err != nil {
		return err
	}
	defer lease.Release()
	switch action {
	case "up":
		err = r.audio.Step(5)
	case "down":
		err = r.audio.Step(-5)
	case "mute":
		st := r.audio.State()
		err = r.audio.SetMute(!st.Muted)
	default:
		return fmt.Errorf("unknown action")
	}
	if err != nil {
		return err
	}
	st := r.audio.State()
	r.OSD().Show(OSDView{Kind: osdAudio, Level: st.Level, Muted: st.Muted})
	return nil
}

func (r *Registry) stepBrightness(action string) error {
	if r.brightness == nil || !r.brightness.Available() {
		return fmt.Errorf("brightness unavailable")
	}
	lease, err := r.brightness.Acquire()
	if err != nil {
		return err
	}
	defer lease.Release()
	switch action {
	case "up":
		err = r.brightness.Step(5)
	case "down":
		err = r.brightness.Step(-5)
	default:
		return fmt.Errorf("unknown action")
	}
	if err != nil {
		return err
	}
	r.OSD().Show(OSDView{Kind: osdBrightness, Level: r.brightness.Level()})
	return nil
}

// BindPersist sets the file and reload signal used when settings write a
// candidate. Empty path skips the write (tests). The channel is the same one
// SIGHUP uses.
func (r *Registry) BindPersist(path string, reloads chan<- struct{}) {
	r.mu.Lock()
	r.configPath = path
	r.reloads = reloads
	r.mu.Unlock()
}

// Tokens is the palette the registry generated at construction or last reload.
func (r *Registry) Tokens() theme.Tokens {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.tokens
}

// ReducedMotion reports the accessibility preference from the live config.
func (r *Registry) ReducedMotion() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.cfg.Accessibility.ReducedMotion
}

// generateTheme returns the palette for cfg, or the palette already published
// when the candidate is incomplete.
//
// A partial palette is rejected whole rather than merged: a surface painted
// with some new roles and some old ones is worse than one painted entirely in
// the previous theme, and the mix is hard to attribute afterwards. Templates
// are only written for a palette that survives that check, so an external
// consumer never sees one the shell itself refused.
// panelTheme resolves a popout's theme from the effective presentation
// configuration and palette, which may be a no-commit preview.
//
// Panels used to build ThemeFromTokens(r.tokens, 12), which rebuilds the
// default composition and pins the radius, so the palette was the only axis
// that reached a popout: density, radius, font scale and preset all stopped at
// the bar. Resolving from the effective config is what makes one theme serve
// every surface.
func (r *Registry) panelTheme() Theme {
	cfg, tokens := r.effectiveThemeLocked()
	t, err := ResolveTheme(cfg, cfg.Bar, tokens)
	if err != nil {
		return DefaultTheme()
	}
	return t.WithCompositor(r.caps.Blur)
}

// resolveOutputTheme is one output's theme, with the compositor's blur applied
// so the bar and the panels that join it agree about its ground.
func resolveOutputTheme(cfg config.Config, connector string, tok theme.Tokens, blur bool) (Theme, error) {
	t, err := ResolveTheme(cfg, cfg.ForConnector(connector), tok)
	return t.WithCompositor(blur), err
}

func (r *Registry) panelThemeFor(output uint32) Theme {
	cfg, tokens := r.effectiveThemeLocked()
	return r.panelThemeForState(output, cfg, tokens)
}

func (r *Registry) effectiveThemeLocked() (config.Config, theme.Tokens) {
	if r.previewTheme != nil {
		return r.previewTheme.cfg, r.previewTheme.tokens
	}
	return r.cfg, r.tokens
}

// invalidateThemePreviewLocked discards pending work before a committed theme
// is published. A visible preview keeps its session open until hide; a first
// preview that has not painted is simply canceled.
func (r *Registry) invalidateThemePreviewLocked() {
	r.previewRequest++
	if r.previewTheme == nil {
		r.previewing = false
		r.previewPrevErr = ""
	}
	r.previewTheme = nil
}

// panelThemeForState resolves one output against the supplied palette. The
// retheme path passes preview candidates here without publishing them in r.
func (r *Registry) panelThemeForState(output uint32, cfg config.Config, tokens theme.Tokens) Theme {
	connector := ""
	if bar, ok := r.bars[output]; ok {
		connector = bar.connector()
	}
	t, err := resolveOutputTheme(cfg, connector, tokens, r.caps.Blur)
	if err != nil {
		return DefaultTheme()
	}
	return t
}

// generateTheme returns the palette for cfg and why it is not the requested
// one, when it is not.
//
// The error used to be dropped on both failure paths. Keeping the published
// palette is still right -- swapping a working theme for the compiled fallback
// is a regression nobody asked for -- but saying nothing meant a theme that
// would not generate was indistinguishable from one that generated to the same
// colours: the wallpaper changed, the shell did not, and there was nowhere to
// look. Callers surface this; they must not treat it as fatal.
func (r *Registry) generateTheme(cfg config.Config) (theme.Tokens, error) {
	tok, err := r.generateOnly(cfg)
	if err != nil {
		return r.lastCompleteTokens(cfg.Accessibility.HighContrast), err
	}
	if !runningAsTest() {
		outcomes, err := theming.ApplyEnabled(os.Getenv("HOME"), cfg.TemplateEnabled, tok, r.consumeTemplateForce)
		// A nil outcomes map means the apply did not run: it was queued
		// behind a live one (or had nothing to do). Sweeping now would eat
		// the overwrite the queued pass is about to consume; the goroutine
		// that eventually runs the job reports its outcomes instead.
		r.recordTemplateOutcomes(outcomes, true)
		if err != nil {
			return tok, fmt.Errorf("theme: external templates: %w", err)
		}
	}
	return tok, nil
}

func (r *Registry) recordTemplateOutcomes(outcomes map[string]error, clearResolvedForces bool) {
	if outcomes == nil {
		return
	}
	refusals := map[string]string{}
	for name, err := range outcomes {
		if errors.Is(err, theming.ErrUserModified) {
			refusals[name] = err.Error()
		}
	}
	r.templateMu.Lock()
	r.templateRefusals = refusals
	if clearResolvedForces {
		for name := range r.templateForce {
			if _, refused := refusals[name]; !refused {
				delete(r.templateForce, name)
			}
		}
	}
	r.templateMu.Unlock()
}

// generateOnly produces palette tokens for cfg without touching published
// state or writing application templates. Preview uses the same generation
// path with an isolated cache. On failure, the caller decides the failure
// floor: generateTheme keeps the published palette, preview refuses to paint.
func (r *Registry) generateOnly(cfg config.Config) (theme.Tokens, error) {
	return r.generateOnlyWith(cfg, r.themeGen)
}

// generatePreviewOnly isolates generator files from the persistent cache. A
// preview is disposable state, so even matugen's config and template belong in
// a temporary directory that is removed before the preview is published.
func (r *Registry) generatePreviewOnly(cfg config.Config) (tokens theme.Tokens, err error) {
	dir, err := os.MkdirTemp("", "sysc-shell-theme-preview-")
	if err != nil {
		return theme.Tokens{}, fmt.Errorf("theme: preview cache: %w", err)
	}
	defer func() {
		if cleanupErr := os.RemoveAll(dir); cleanupErr != nil {
			err = errors.Join(err, fmt.Errorf("theme: remove preview cache: %w", cleanupErr))
		}
	}()

	gen := r.themeGen
	gen.CacheDir = dir
	return r.generateOnlyWith(cfg, gen)
}

func (r *Registry) generateOnlyWith(cfg config.Config, gen theme.Generator) (theme.Tokens, error) {
	r.themeGenMu.Lock()
	defer r.themeGenMu.Unlock()
	tok, err := gen.Generate(
		theme.Source{Kind: cfg.ThemeGen.Source, Seed: cfg.ThemeGen.Seed},
		theme.Options{
			Mode:         cfg.ThemeGen.Mode,
			Scheme:       cfg.ThemeGen.Scheme,
			HighContrast: cfg.Accessibility.HighContrast,
		},
	)
	if err != nil {
		return tok, err
	}
	if err := tok.Complete(); err != nil {
		return tok, fmt.Errorf("theme: generated palette is incomplete: %w", err)
	}
	return tok, nil
}

// consumeTemplateForce reports and clears one template's overwrite request.
func (r *Registry) consumeTemplateForce(name string) bool {
	r.templateMu.Lock()
	defer r.templateMu.Unlock()
	forced := r.templateForce[name]
	delete(r.templateForce, name)
	return forced
}

// tokensAndReason flattens generateTheme for the construction path, which has
// no surface to report to yet and only needs the reason recorded.
func tokensAndReason(tok theme.Tokens, err error) (theme.Tokens, string) {
	if err != nil {
		return tok, err.Error()
	}
	return tok, ""
}

// lastCompleteTokens is the palette to fall back on when a generated one is
// rejected: whatever is currently published, or the compiled-in fallback
// before anything has been.
func (r *Registry) lastCompleteTokens(highContrast bool) theme.Tokens {
	r.mu.Lock()
	current := r.tokens
	r.mu.Unlock()
	if current.Complete() == nil {
		return current
	}
	return theme.FallbackFor(highContrast)
}

// surfaceTheme is the effective palette every auxiliary surface paints with,
// plus the bar's geometry so a panel and the bar agree about spacing and text
// size.
//
// Callers hold Registry.mu, because the tokens are replaced by a reload.
// surfaceTheme is the theme a non-bar surface adopts. It resolves the effective
// configuration rather than rebuilding the default composition around a
// radius, which is what lets a density, motion or opacity change reach an
// already-open panel, toast, tray surface or OSD on reload.
func (r *Registry) surfaceTheme() Theme {
	cfg, _ := r.effectiveThemeLocked()
	return withBarGeometry(r.panelTheme(), cfg.Bar)
}

func runningAsTest() bool {
	return strings.HasSuffix(os.Args[0], ".test")
}

// relayAudioOSD takes the service as an argument rather than reading the
// field: replacing the service writes that field, and a relay started for the
// previous service would otherwise read it concurrently. The replaced service
// is closed before the field changes, so its channel ends this loop.
func (r *Registry) relayAudioOSD(audio *services.Audio) {
	if audio == nil {
		return
	}
	ch := audio.Changes()
	for {
		select {
		case <-r.closed:
			return
		case st, ok := <-ch:
			if !ok {
				return
			}
			r.OSD().Show(OSDView{Kind: osdAudio, Level: st.Level, Muted: st.Muted})
			r.mu.Lock()
			out, open := r.rebuildControlCentreLocked()
			r.mu.Unlock()
			if open {
				r.publishSurface(out, panelSurfaceID(PanelControlCenter))
			}
		}
	}
}

// relayBrightnessOSD takes the service as an argument for the same reason as
// relayAudioOSD.
func (r *Registry) relayBrightnessOSD(brightness *services.Brightness) {
	if brightness == nil {
		return
	}
	ch := brightness.Changes()
	for {
		select {
		case <-r.closed:
			return
		case st, ok := <-ch:
			if !ok {
				return
			}
			r.OSD().Show(OSDView{Kind: osdBrightness, Level: st.Level})
			r.mu.Lock()
			out, open := r.rebuildControlCentreLocked()
			r.mu.Unlock()
			if open {
				r.publishSurface(out, panelSurfaceID(PanelControlCenter))
			}
		}
	}
}

// Clock is the shared clock service. The process pumps its updates into
// UpdateClock.
func (r *Registry) Clock() *services.Clock { return r.clock }

// Metrics is the shared sampling service. The process pumps its updates into
// UpdateMetrics.
func (r *Registry) Metrics() *services.Metrics { return r.metrics }

// Weather is the shared weather service. The process pumps its updates into
// UpdateWeather.
func (r *Registry) Weather() *services.Weather { return r.weather }

// relayTooltips hands the dwell's requests to the tooltip host in order. It
// runs off the Wayland owner, like the other relays, and ends with the
// registry. The host is read per request, so a test can install its own.
func (r *Registry) relayTooltips(d *dwell) {
	for {
		select {
		case req := <-d.requests():
			if req.empty() {
				r.tooltips.hide()
			} else {
				r.tooltips.show(req)
			}
		case <-r.closed:
			return
		}
	}
}

// Invalidations is the channel the Wayland owner receives from. The registry
// owns it and never closes it.
func (r *Registry) Invalidations() <-chan wayland.Invalidation { return r.invalidations }

// publish sends one invalidation per changed global.
//
// The send blocks rather than dropping. A dropped invalidation is a bar that
// never repaints, which is exactly the defect this tranche must not ship. The
// owner's bridge goroutine drains this channel continuously into an unbounded
// queue, so blocking is bounded; Close unblocks a pending send at shutdown.
//
// Callers must not hold r.mu: the send can block, and the owner must stay free
// to make progress.
func (r *Registry) publish(globals []uint32) {
	for _, global := range globals {
		select {
		case r.invalidations <- wayland.Invalidation{Global: global}:
		case <-r.closed:
			return
		}
	}
}

// NewHost builds the hooks for one output's bar and acquires its services.
func (r *Registry) NewHost(global uint32, connector string) (wayland.HostCallbacks, error) {
	r.mu.Lock()
	cfg, tok := r.effectiveThemeLocked()
	r.mu.Unlock()

	bar, leases, callbacks, err := r.buildBar(cfg, connector, tok)
	if err != nil {
		return wayland.HostCallbacks{}, err
	}

	toastOutputs, plugins := r.adoptBar(global, connector, bar, leases)

	r.SyncToastOutputs(toastOutputs)
	if plugins != nil {
		plugins.outputLost(global)
		plugins.syncBars()
	}
	// An output that comes back gets its wallpaper back (D20). This is the
	// arrival seam; DropHost is the departure one.
	r.wallpaperOutputConnected(connector)
	return r.bindHost(global, bar, callbacks), nil
}

// adoptBar installs a freshly built bar as the one for its output and reports
// what the caller must then publish outside the lock.
//
// The unlock is deferred rather than written at the end because NewHost runs
// inside the wl_output.done handler: the Wayland dispatch loop recovers a
// panic raised there into an error, so a panic in bar.apply -- a widget whose
// state seam is nil, say -- would otherwise leave this mutex held forever.
// Registry.Close then blocks on it during shutdown and the process stops
// responding to SIGTERM, which turns a legible startup crash into a shell
// that never appears and has to be killed.
func (r *Registry) adoptBar(
	global uint32, connector string, bar *Bar, leases []*services.Lease,
) (map[string]uint32, *pluginHost) {
	r.mu.Lock()
	defer r.mu.Unlock()

	// NewHost builds outside Registry.mu. Re-resolve against the effective
	// theme before adoption so a preview or commit that arrived during the
	// build cannot install a bar with stale colors.
	cfg, tokens := r.effectiveThemeLocked()
	if next, err := resolveOutputTheme(cfg, connector, tokens, r.caps.Blur); err == nil {
		bar.retheme(next)
	}
	r.attachRunningIconsAtLocked(bar.scale120())
	if bar.mediaWidget {
		r.mediaArtFor()
	}
	bar.apply(r.viewLocked(connector))
	r.bars[global] = bar
	r.leases[global] = leases
	// Route this bar's invalidations -- animation frames included -- to the
	// owner through the registry. Without this the frame loop writes a
	// private channel nothing reads and every settling frame is dropped.
	bar.onPublish = func() { r.publish([]uint32{global}) }
	r.bindBarTrayLocked(global, connector, bar)
	r.bindBarPluginLocked(bar)
	r.bindBarPanelActionsLocked(global, bar)
	// Seeded, not published: the owner configures this surface next and paints
	// it once. An invalidation here would be a second frame for a first paint,
	// and this call is on the owner goroutine, which drains that channel.
	r.syncTrayLocked()
	return r.outputGlobalsLocked(), r.plugins
}

// bindBarTrayLocked gives one bar its tray input seam. The global and the
// connector are captured here rather than carried through the bar, because a
// bar is identified by its global and a connector is only an attribute.
func (r *Registry) bindBarTrayLocked(global uint32, connector string, bar *Bar) {
	bar.setTrayHandler(func(
		key tray.ItemKey, arranged trayArrangement, anchor ui.Rect, event wayland.Event,
	) bool {
		return r.handleTrayBar(global, connector, key, arranged, anchor, event)
	})
}

func (r *Registry) bindBarPluginLocked(bar *Bar) {
	bar.setPluginHandler(func(action string, event wayland.Event) bool {
		// The handler runs without the bar lock, so reading the action's
		// centre here is safe and gives the plugin's panel an anchor under
		// the widget that was clicked.
		return r.handlePluginBar(action, event, bar.actionCenterX(action))
	})
}

// bindBarPanelActionsLocked gives one bar its panel-toggle seam. The handler
// is called without the bar lock, matching the tray path, because TogglePanel
// takes the registry lock and then the bar lock.
func (r *Registry) bindBarPanelActionsLocked(global uint32, bar *Bar) {
	bar.setActionHandler(func(action string, button uint32) bool {
		if key, ok := runningAppKey(action); ok {
			return r.handleRunningAppClick(global, key, button)
		}
		if id, ok := workspaceID(action); ok {
			return r.handleWorkspacePillClick(id, button)
		}
		out, trig := r.triggerFor(global)
		switch {
		case action == panelLauncherAction && (button == 0 || button == buttonLeft):
			return r.TogglePanel(PanelLauncher, out, trig) == nil
		case action == panelMonitorAction && (button == 0 || button == buttonLeft || button == buttonRight):
			return r.TogglePanel(PanelMonitor, out, trig) == nil
		case action == panelSessionAction && button == buttonRight:
			return r.TogglePanel(PanelSession, out, trig) == nil
		case action == panelWallpaperAction && (button == 0 || button == buttonLeft || button == buttonRight):
			return r.TogglePanel(PanelWallpaper, out, trig) == nil
		case action == panelTerminalArtAction && (button == 0 || button == buttonLeft || button == buttonRight):
			return r.TogglePanel(PanelTerminalArt, out, trig) == nil
		case action == panelControlCenterAction && button == buttonRight:
			trig.AnchorX = bar.actionCenterX(panelControlCenterAction)
			return r.TogglePanel(PanelControlCenter, out, trig) == nil
		case action == panelNotificationsAction && (button == 0 || button == buttonLeft):
			return r.TogglePanel(PanelNotifications, out, trig) == nil
		case action == panelClipboardAction && (button == 0 || button == buttonLeft || button == buttonRight):
			return r.TogglePanel(PanelClipboard, out, trig) == nil
		case action == panelNotificationsAction && button == buttonMiddle:
			r.toggleNotifyDND()
			return true
		case action == panelNotificationsAction && button == buttonRight:
			if err := r.OpenPanel(PanelNotifications, out, trig); err != nil {
				return true
			}
			r.mu.Lock()
			if h := r.panelHosts[PanelNotifications]; h != nil {
				h.notifyMenu = true
				r.rebuildPanel(h)
			}
			r.mu.Unlock()
			return true
		case action == panelAudioAction && (button == 0 || button == buttonLeft):
			trig.AnchorX = bar.actionCenterX(panelAudioAction)
			return r.TogglePanel(PanelAudio, out, trig) == nil
		case action == panelWifiAction && (button == 0 || button == buttonLeft):
			trig.AnchorX = bar.actionCenterX(panelWifiAction)
			return r.TogglePanel(PanelNetwork, out, trig) == nil
		case action == panelWifiAction && button == buttonRight:
			r.toggleWirelessAsync()
			return true
		case action == panelBluetoothAction && (button == 0 || button == buttonLeft):
			trig.AnchorX = bar.actionCenterX(panelBluetoothAction)
			return r.TogglePanel(PanelBluetooth, out, trig) == nil
		case action == panelWeatherAction && (button == 0 || button == buttonLeft || button == buttonRight):
			trig.AnchorX = bar.actionCenterX(panelWeatherAction)
			return r.TogglePanel(PanelWeather, out, trig) == nil
		case action == panelBluetoothAction && button == buttonRight:
			trig.AnchorX = bar.actionCenterX(panelBluetoothAction)
			if err := r.OpenPanel(PanelControlCenter, out, trig); err != nil {
				return false
			}
			r.mu.Lock()
			err := r.selectPanelSectionLocked(PanelControlCenter, "bluetooth")
			r.mu.Unlock()
			return err == nil
		case action == panelAudioAction && button == buttonRight:
			r.stepAudioAsync("mute")
			return true
		case action == panelMediaAction && (button == 0 || button == buttonLeft):
			// The control centre is the one player picker (D7): the widget
			// routes there and the spine lands on the Media section.
			trig.AnchorX = bar.actionCenterX(panelMediaAction)
			if err := r.OpenPanel(PanelControlCenter, out, trig); err != nil {
				return false
			}
			r.mu.Lock()
			err := r.selectPanelSectionLocked(PanelControlCenter, "media")
			r.mu.Unlock()
			return err == nil
		case action == panelMediaAction && (button == buttonMiddle || button == buttonRight):
			r.mu.Lock()
			m := r.media
			r.mu.Unlock()
			if m != nil {
				// Bus I/O, so off the handler path entirely.
				go func() { _ = m.PlayPause() }()
			}
			return true
		}
		return false
	})
	bar.setAxisHandler(func(action string, delta int) bool {
		switch action {
		case panelAudioAction:
			if delta > 0 {
				r.stepAudioAsync("up")
			} else if delta < 0 {
				r.stepAudioAsync("down")
			}
			return true
		case panelMediaAction:
			// Scroll moves next and previous (D7): up is forward.
			r.mu.Lock()
			m := r.media
			r.mu.Unlock()
			if m == nil {
				return true
			}
			switch {
			case delta > 0:
				go func() { _ = m.Next() }()
			case delta < 0:
				go func() { _ = m.Previous() }()
			}
			return true
		}
		return false
	})
}

func (r *Registry) toggleNotifyDND() {
	r.mu.Lock()
	_, on := r.notify.dndState(r.now)
	r.notify.setDND(!on)
	if r.toasts != nil {
		r.toasts.recompute()
	}
	var changed []uint32
	for global, bar := range r.bars {
		if bar.apply(r.viewLocked(bar.connector())) {
			changed = append(changed, global)
		}
	}
	r.mu.Unlock()
	r.publish(changed)
}

// RepaintAll invalidates every live output's surfaces once. Resume uses it:
// after a sleep cycle shell pixels, cursor planes and damage state are stale,
// and one forced frame per output is cheaper than reasoning about which
// surfaces survived. While the Wayland owner is suspended its bridge queues
// the invalidations, so this call never blocks on the socket's silence.
func (r *Registry) RepaintAll() {
	r.mu.Lock()
	globals := make([]uint32, 0, len(r.bars))
	for global := range r.bars {
		globals = append(globals, global)
	}
	r.mu.Unlock()
	r.publish(globals)
}

// outputGlobalsLocked maps each live connector to its wl_registry global. Two
// globals may briefly share a connector during a reconnect; the newest wins,
// because that is the one whose surfaces exist.
func (r *Registry) outputGlobalsLocked() map[string]uint32 {
	globals := make(map[string]uint32, len(r.bars))
	for global, bar := range r.bars {
		if existing, ok := globals[bar.connector()]; !ok || global > existing {
			globals[bar.connector()] = global
		}
	}
	return globals
}

// PrepareConfig builds every enabled host's replacement bar and acquires its
// services before the caller changes live host policy.
//
// Acquiring here, and releasing the outgoing leases only in Commit, is what
// keeps a service in continuous use from stopping: its consumer count never
// reaches zero, so it is never restarted. A failure at any point releases
// exactly what this call acquired.
func (r *Registry) PrepareConfig(cfg config.Config, identities []wayland.HostIdentity) (wayland.PreparedConfig, error) {
	tok, genErr := r.generateTheme(cfg)
	bars := make(map[uint32]*Bar, len(identities))
	leases := make(map[uint32][]*services.Lease, len(identities))
	callbacks := make(map[uint32]wayland.HostCallbacks, len(identities))

	for _, identity := range identities {
		bar, held, hooks, err := r.buildBar(cfg, identity.Connector, tok)
		if err != nil {
			for _, acquired := range leases {
				releaseAll(acquired)
			}
			return wayland.PreparedConfig{}, err
		}
		bars[identity.Global] = bar
		leases[identity.Global] = held
		callbacks[identity.Global] = r.bindHost(identity.Global, bar, hooks)
	}

	// once guards against Commit and Rollback each running, and against
	// either running twice.
	var once sync.Once

	return wayland.PreparedConfig{
		Hosts: callbacks,
		Commit: func() {
			once.Do(func() {
				r.mu.Lock()
				outgoing := r.leases
				outgoingBars := r.bars
				depthClockFontChanged := r.cfg.Bar.FontFamily != cfg.Bar.FontFamily
				depthClockVisualChanged := r.cfg.Wallpaper.Scale != cfg.Wallpaper.Scale ||
					r.tokens != tok || depthClockFontChanged ||
					r.cfg.Bar.FontSize != cfg.Bar.FontSize
				mediaConfigChanged := r.cfg.Media.Preferred != cfg.Media.Preferred ||
					!slices.Equal(r.cfg.Media.Blacklist, cfg.Media.Blacklist)
				var media *services.Media
				var depthEffects depthClockEffects
				// Coordinates, unit and city are the request, not a lease
				// parameter, so the service has to be told. Each call is a
				// no-op unless its value changed, which is the common case
				// for an unrelated reload.
				r.weather.Reconfigure(
					cfg.Weather.Latitude, cfg.Weather.Longitude, weatherUnit(cfg.Weather.Unit))
				r.weather.SetCity(cfg.Weather.City)
				r.dwell.leave()
				// The open menu and drawer were placed against the outgoing
				// geometry and hold a root; a candidate replaces both.
				r.closeTrayLocked()
				for _, bar := range bars {
					bar.apply(r.viewLocked(bar.connector()))
				}
				r.cfg = cfg
				r.pushIdleInputsLocked()
				// An open settings panel holds its own draft, and a change
				// arriving from outside it would otherwise be reverted by the
				// next control write, which puts that draft back whole. The
				// registry is rebuilt with it because an entry's options can
				// depend on another setting.
				if h := r.panelHosts[PanelSettings]; h != nil {
					h.draft = cfg
					h.set = r.settingsForLocked(cfg)
				}
				r.refreshMonitorLeasesLocked(r.panelHosts[PanelMonitor])
				media = r.media
				r.tokens = tok
				r.themeErr = ""
				if genErr != nil {
					r.themeErr = genErr.Error()
				}
				r.invalidateThemePreviewLocked()
				if r.previewing {
					r.previewPrevErr = r.themeErr
				}
				surfacePubs := r.retheThemeOpenSurfacesLocked(r.cfg, r.tokens)
				if depthClockVisualChanged && r.depthClocks != nil {
					depthEffects = r.depthClocks.reconfigureLocked(depthClockFontChanged)
				}
				r.bars = bars
				r.leases = leases
				for global, bar := range r.bars {
					r.bindBarTrayLocked(global, bar.connector(), bar)
					r.bindBarPluginLocked(bar)
					r.bindBarPanelActionsLocked(global, bar)
				}
				// Seeded only: every replacement bar is reconfigured and
				// repainted by the owner as part of adopting the candidate.
				r.syncTrayLocked()
				toastOutputs := r.outputGlobalsLocked()
				plugins := r.plugins
				r.mu.Unlock()
				if mediaConfigChanged && media != nil {
					media.Configure(cfg.Media.Preferred, cfg.Media.Blacklist)
				}
				if r.depthClocks != nil {
					r.depthClocks.emit(depthEffects)
				}
				for _, p := range surfacePubs {
					r.publishSurface(p.Global, p.SurfaceID)
				}
				for _, bar := range outgoingBars {
					bar.stopAnimation()
				}

				r.SyncToastOutputs(toastOutputs)
				if plugins != nil {
					plugins.syncBars()
				}

				// Released only after the replacement set holds its own, so
				// the count never touches zero for a service still in use.
				for _, held := range outgoing {
					releaseAll(held)
				}
			})
		},
		Rollback: func() {
			once.Do(func() {
				for _, bar := range bars {
					bar.stopAnimation()
				}
				for _, held := range leases {
					releaseAll(held)
				}
			})
		},
	}, nil
}

// DropHost releases a bar and its service leases after its surface is
// destroyed. Only the named global is affected, so a stale global sharing a
// connector with a reconnected one cannot remove it.
func (r *Registry) DropHost(global uint32) {
	r.mu.Lock()
	leases := r.leases[global]
	gone := ""
	bar := r.bars[global]
	if bar != nil {
		gone = bar.connector()
	}
	delete(r.bars, global)
	delete(r.leases, global)
	r.trayOutputLostLocked(global)
	r.tooltips.outputLost(global)
	toastOutputs := r.outputGlobalsLocked()
	plugins := r.plugins
	r.mu.Unlock()
	if bar != nil {
		bar.stopAnimation()
	}

	if gone != "" && !slices.Contains(r.connectorsSnapshot(), gone) {
		r.wallpaperOutputGone(gone)
	}
	r.SyncToastOutputs(toastOutputs)
	if plugins != nil {
		plugins.outputLost(global)
		plugins.syncBars()
	}
	releaseAll(leases)
}

// Close releases every bar and service. It is safe to call twice.
// closeLockGrace bounds how long Close waits for r.mu at shutdown.
const closeLockGrace = 2 * time.Second

// lockWithin acquires r.mu, giving up after d.
//
// sysc-wayland recovers a panic in an event handler into an error rather than
// crashing. A handler that panicked between a manual Lock and its Unlock has
// therefore left r.mu held for the life of the process. Shutdown must not
// depend on an invariant a panic has already broken: Close used to block here
// for good, so the error that caused the panic was never reported and the
// service sat in futex_do_wait until it was killed.
func (r *Registry) lockWithin(d time.Duration) bool {
	deadline := time.Now().Add(d)
	for {
		if r.mu.TryLock() {
			return true
		}
		if time.Now().After(deadline) {
			return false
		}
		time.Sleep(time.Millisecond)
	}
}

func (r *Registry) Close() {
	// Unblocks any publish waiting on a full channel, so shutdown cannot hang.
	r.closeOnce.Do(func() { close(r.closed) })

	// Best effort: everything under the lock is cleanup, and giving it up is
	// better than never reporting why we are shutting down at all.
	locked := r.lockWithin(closeLockGrace)
	if !locked {
		log.Printf("shell: closing without r.mu; a handler panic left it held")
	}
	var osdAux []wayland.AuxRequest
	var leases []*services.Lease
	var bars []*Bar
	var audioLease, brightLease, networkLease *services.Lease
	var bluetooth *services.Bluetooth
	var inhibit io.Closer
	var wallpaperSvc *wallpaper.Service
	var wallsSvc wallsController
	var wallpaperThumbCancel context.CancelFunc
	var mediaArt *mediaArtWorker
	var depthEffects depthClockEffects
	if locked {
		if r.toasts != nil {
			r.toasts.stopLeaseRenew()
			r.toasts.stopSlideAnimation()
		}
		if r.osd != nil {
			osdAux = r.osd.prepareHide()
		}
		r.closeTrayLocked()
		if r.runningMenu != nil {
			r.runningMenu.closeLocked()
		}
		r.stopTrayIconsLocked()
		r.closeAllPanelsLocked()
		if r.depthClocks != nil {
			depthEffects = r.depthClocks.closeLocked()
		}
		for global, held := range r.leases {
			leases = append(leases, held...)
			delete(r.leases, global)
		}
		for _, bar := range r.bars {
			bars = append(bars, bar)
		}
		r.bars = make(map[uint32]*Bar)
		audioLease = r.audioLease
		r.audioLease = nil
		brightLease = r.brightLease
		r.brightLease = nil
		networkLease = r.networkLease
		r.networkLease = nil
		wallpaperSvc = r.wallpaperSvc
		r.wallpaperSvc = nil
		wallsSvc = r.wallsService
		r.wallsService = nil
		wallpaperThumbCancel = r.wallpaperThumbCancel
		r.wallpaperThumbCancel = nil
		mediaArt = r.mediaArt
		r.mediaArt = nil
		bluetooth = r.bluetooth
		r.bluetooth = nil
		if r.bluetoothRelayCancel != nil {
			close(r.bluetoothRelayCancel)
			r.bluetoothRelayCancel = nil
		}
		inhibit = r.inhibit
		r.inhibit = nil
		r.inhibitWanted = false
		r.pushIdleInputsLocked()
		r.mu.Unlock()
	}
	for _, bar := range bars {
		bar.stopAnimation()
	}

	for _, req := range osdAux {
		r.sendAux(req)
	}
	if r.depthClocks != nil {
		r.depthClocks.emit(depthEffects)
	}
	if audioLease != nil {
		audioLease.Release()
	}
	if brightLease != nil {
		brightLease.Release()
	}
	if networkLease != nil {
		networkLease.Release()
	}
	if inhibit != nil {
		_ = inhibit.Close()
	}
	releaseAll(leases)
	var launcherSvc *launcher.Service
	if r.lockWithin(closeLockGrace) {
		launcherSvc = r.launcherSvc
		r.launcherSvc = nil
		r.mu.Unlock()
	}
	if launcherSvc != nil {
		launcherSvc.Close()
	}
	if wallpaperThumbCancel != nil {
		wallpaperThumbCancel()
	}
	if mediaArt != nil {
		mediaArt.Close()
	}
	if wallpaperSvc != nil {
		wallpaperSvc.Close()
	}
	if wallsSvc != nil {
		if err := wallsSvc.Close(); err != nil {
			log.Printf("shell: close sysc-walls service: %v", err)
		}
	}
	r.dwell.stop()
	r.clock.Close()
	r.metrics.Close()
	r.weather.Close()
	if r.audio != nil {
		r.audio.Close()
	}
	if r.brightness != nil {
		r.brightness.Close()
	}
	if r.lockKeys != nil {
		r.lockKeys.Close()
	}
	if r.network != nil {
		r.network.Close()
	}
	if r.media != nil {
		r.media.Close()
	}
	if bluetooth != nil {
		_ = bluetooth.Close()
	}
	if r.plugins != nil {
		r.plugins.Close()
		r.plugins = nil
	}
}

// UpdateClock applies a shared time snapshot to every bar and reports the
// globals whose text actually changed.
//
// One tick reaches every bar from one snapshot; a bar whose rendered text is
// unchanged is not reported, so no frame is submitted for it.
func (r *Registry) UpdateClock(now time.Time) []uint32 {
	r.mu.Lock()
	r.now = now
	expired := r.notify.expireDND(now)
	if expired && r.toasts != nil {
		r.toasts.recompute()
	}
	// An open notification centre freezes its DND glyph in the built tree;
	// rebuild it when a timed preset lifts (gh #56).
	var dndCentreOut uint32
	dndCentreOpen := false
	if expired {
		if h := r.panelHosts[PanelNotifications]; h != nil {
			r.rebuildPanel(h)
			dndCentreOut, dndCentreOpen = h.output, true
		}
	}
	var changed []uint32
	for global, bar := range r.bars {
		if bar.apply(r.viewLocked(bar.connector())) {
			changed = append(changed, global)
		}
	}
	var depthEffects depthClockEffects
	if r.depthClocks != nil {
		depthEffects = r.depthClocks.updateClockLocked(now)
	}
	controlOut, controlOK := r.rebuildControlCentreLocked()
	r.mu.Unlock()

	r.publish(changed)
	if r.depthClocks != nil {
		r.depthClocks.emit(depthEffects)
	}
	if controlOK {
		r.publishSurface(controlOut, panelSurfaceID(PanelControlCenter))
	}
	if dndCentreOpen {
		r.publishSurface(dndCentreOut, panelSurfaceID(PanelNotifications))
	}
	return changed
}

// UpdateMetrics applies a sampling pass to every bar and reports the globals
// whose rendering actually changed.
func (r *Registry) UpdateMetrics(snap services.Snapshot) []uint32 {
	facts := readMachineFacts()
	r.updateRootDevice(snap, resolveDevicePath)
	r.mu.Lock()
	r.sample = snap
	r.machineFacts = facts
	batteryThreshold, batteryConfigured := batteryWarningThreshold(r.cfg.Bar)
	var changed []uint32
	for global, bar := range r.bars {
		if bar.apply(r.viewLocked(bar.connector())) {
			changed = append(changed, global)
		}
	}
	monitorOut, monitorOK := uint32(0), false
	if h := r.panelHosts[PanelMonitor]; h != nil {
		r.syncRateSubjectsLocked(h, snap, monitorLeaseInterval(r.cfg.Monitor))
		r.rebuildPanel(h)
		monitorOut, monitorOK = h.output, true
	}
	sessionOut, sessionOK := uint32(0), false
	if h := r.panelHosts[PanelSession]; h != nil {
		r.rebuildPanel(h)
		sessionOut, sessionOK = h.output, true
	}
	networkOut, networkOK := uint32(0), false
	if h := r.panelHosts[PanelNetwork]; h != nil {
		r.rebuildPanel(h)
		networkOut, networkOK = h.output, true
	}
	if h := r.panelHosts[PanelControlCenter]; h != nil {
		r.syncRateSubjectsLocked(h, snap, time.Second)
	}
	controlOut, controlOK := r.rebuildControlCentreLocked()
	r.mu.Unlock()

	if batteryConfigured && r.batteryWarning != nil {
		r.dispatchBatteryWarning(r.batteryWarning.observe(snap, batteryThreshold))
	}
	r.publish(changed)
	if monitorOK {
		r.publishSurface(monitorOut, panelSurfaceID(PanelMonitor))
	}
	if sessionOK {
		r.publishSurface(sessionOut, panelSurfaceID(PanelSession))
	}
	if networkOK {
		r.publishSurface(networkOut, panelSurfaceID(PanelNetwork))
	}
	if controlOK {
		r.publishSurface(controlOut, panelSurfaceID(PanelControlCenter))
	}
	return changed
}

// UpdateWeather applies a reading to every bar and reports the globals whose
// text actually changed.
func (r *Registry) UpdateWeather(reading services.Reading) []uint32 {
	r.mu.Lock()
	r.reading = reading
	var changed []uint32
	for global, bar := range r.bars {
		if bar.apply(r.viewLocked(bar.connector())) {
			changed = append(changed, global)
		}
	}
	controlOut, controlOK := r.rebuildControlCentreLocked()
	weatherOut, weatherOK := r.rebuildWeatherPanelLocked()
	r.mu.Unlock()

	r.publish(changed)
	if controlOK {
		r.publishSurface(controlOut, panelSurfaceID(PanelControlCenter))
	}
	if weatherOK {
		r.publishSurface(weatherOut, panelSurfaceID(PanelWeather))
	}
	return changed
}

func (r *Registry) rebuildWeatherPanelLocked() (uint32, bool) {
	h := r.panelHosts[PanelWeather]
	if h == nil {
		return 0, false
	}
	r.rebuildPanel(h)
	return h.output, true
}

func (r *Registry) rebuildControlCentreLocked() (uint32, bool) {
	h := r.panelHosts[PanelControlCenter]
	if h == nil {
		return 0, false
	}
	r.rebuildPanel(h)
	return h.output, true
}

// UpdateNiri projects a snapshot into per-connector text and reports the
// globals whose text actually changed.
func (r *Registry) UpdateNiri(s niri.Snapshot) []uint32 {
	next := projectOutputs(s)

	r.mu.Lock()
	// Replaced wholesale, not merged: a connector absent from the projection
	// has no workspace state any more, and keeping its last value would render
	// a stale workspace or title on a host that reconnects under that name.
	r.outputs = next
	r.niriSnapshot = cloneNiriSnapshot(s)
	r.focused = s.FocusedOutput
	r.ensureRunningIndexLocked()
	r.running = groupRunningApps(s.Windows, r.runningIndex)
	if h := r.runningMenu; h != nil && h.open_ && !runningSlotPresent(r.running, h.slot.Key) {
		h.closeLocked()
	}
	changed := r.applyRunningIconsLocked()
	layoutView, showLayout := layoutOSD(r.layouts, s.Layouts, r.layoutsSeen)
	r.layouts = niri.KeyboardLayouts{Names: slices.Clone(s.Layouts.Names), Current: s.Layouts.Current}
	if len(s.Layouts.Names) > 0 {
		r.layoutsSeen = true
	}
	switcherUpdated := false
	var switcherOutput uint32
	if r.windowSwitcher != nil {
		switcherOutput = r.windowSwitcher.output
		switcherUpdated = r.windowSwitcher.refreshLocked(r.niriSnapshot)
	}
	r.mu.Unlock()

	r.publish(changed)
	if switcherUpdated {
		r.publishSurface(switcherOutput, windowSwitcherSurfaceID)
	}
	if showLayout {
		r.OSD().Show(layoutView)
	}
	return changed
}

// viewLocked assembles one bar's immutable input: the process-wide clock
// snapshot plus this connector's Niri projection.
func (r *Registry) viewLocked(connector string) barView {
	state, ok := r.outputs[connector]
	if !ok {
		state = outputState{Workspace: noWorkspace}
	}
	view := barView{
		Now:       r.now,
		Workspace: state.Workspace,
		Title:     state.Title,
		Pills:     state.Pills,
		Metrics:   r.sample,
		History:   r.historyLocked(),
		Weather:   r.reading,
		Clipboard: r.clipboard.clone(),
		Unread:    r.notify.unread(),
		Running:   r.running,
	}
	_, view.DND = r.notify.dndState(r.now)
	if r.audio != nil {
		view.Audio, _ = r.audio.CachedState()
	}
	if r.network != nil {
		view.Network = r.network.CachedState()
	}
	if r.media != nil {
		view.Media = r.mediaState
		if r.mediaArt != nil && r.mediaState.ArtKey != "" {
			if name, ok := mediaArtRequestName(r.mediaState.ArtKey); ok {
				key := icons.Key{Name: name, W: mediaArtBox, H: mediaArtBox}
				if image, cached := r.mediaArt.Lookup(key); cached {
					view.MediaArt = image
				} else {
					_, _ = r.mediaArt.Request(r.mediaState.ArtKey, mediaArtBox)
				}
			}
		}
	}
	view.Bluetooth = r.bluetoothState
	if r.plugins != nil {
		view.Plugins = r.plugins.frames(connector)
	}
	for _, item := range allItems(r.cfg.ForConnector(connector)) {
		if item.ID == "plugin" && !slices.Contains(r.cfg.Plugins.Enabled, item.Plugin) {
			if view.PluginsOff == nil {
				view.PluginsOff = map[string]bool{}
			}
			view.PluginsOff[item.Plugin] = true
		}
	}
	return view
}

// historyLocked collects the samples every leased selector holds. The service
// keeps a ring only while something leases it, so an unused ring cannot be
// copied here.
func (r *Registry) historyLocked() map[services.Selector][]float64 {
	return r.metrics.Histories()
}

func (r *Registry) ensureRunningIndexLocked() {
	if r.runningIndex != nil || runningAsTest() {
		return
	}
	// ponytail: second XDG walk vs the launcher catalogue; share the index if both stay hot.
	r.runningIndex = loadRunningAppEntries(xdgApplicationDirs())
}

// buildBar creates one bar and acquires the services its items need. A failure
// releases whatever was already acquired, so a rejected build leaks nothing.
func (r *Registry) buildBar(cfg config.Config, connector string, tok theme.Tokens) (
	*Bar, []*services.Lease, wayland.HostCallbacks, error,
) {
	policy := cfg.ForConnector(connector)
	r.mu.Lock()
	blur := r.caps.Blur
	r.mu.Unlock()
	th, err := resolveOutputTheme(cfg, connector, tok, blur)
	if err != nil {
		return nil, nil, wayland.HostCallbacks{}, err
	}
	bar, err := NewWithTheme(th, policy, connector)
	if err != nil {
		return nil, nil, wayland.HostCallbacks{}, err
	}

	var leases []*services.Lease
	for _, boundary := range clockBoundaries(policy.Left, policy.Center, policy.Right) {
		lease, err := r.clock.Acquire(boundary)
		if err != nil {
			releaseAll(leases)
			return nil, nil, wayland.HostCallbacks{}, err
		}
		leases = append(leases, lease)
	}
	for _, item := range allItems(policy) {
		sel, ok := metricSelector(item)
		if !ok {
			continue
		}
		lease, err := r.metrics.Acquire(sel, item.Interval)
		if err != nil {
			releaseAll(leases)
			return nil, nil, wayland.HostCallbacks{}, err
		}
		leases = append(leases, lease)
	}
	for _, item := range allItems(policy) {
		if item.ID != "weather" {
			continue
		}
		lease, err := r.weather.Acquire(cfg.Weather.Interval)
		if err != nil {
			releaseAll(leases)
			return nil, nil, wayland.HostCallbacks{}, err
		}
		leases = append(leases, lease)
	}
	if r.audio != nil {
		for _, item := range allItems(policy) {
			if item.ID != "volume" {
				continue
			}
			lease, err := r.audio.Acquire()
			if err != nil {
				releaseAll(leases)
				return nil, nil, wayland.HostCallbacks{}, err
			}
			leases = append(leases, lease)
			break
		}
	}
	if r.media != nil {
		for _, item := range allItems(policy) {
			if item.ID != "media" {
				continue
			}
			lease, err := r.media.Acquire()
			if err != nil {
				releaseAll(leases)
				return nil, nil, wayland.HostCallbacks{}, err
			}
			leases = append(leases, lease)
			break
		}
	}

	return bar, leases, wayland.HostCallbacks{
		Configure:  bar.Configure,
		OutputSize: bar.setOutputSize,
		Render:     bar.Render,
		Handle:     bar.Handle,
		BlurShape:  bar.blurShape,
		// The hint is fixed for the bar's life, but SetCapabilities can make a
		// frosted bar translucent later; claim opaque only if it stays so.
		OpaqueBackground: th.WithCompositor(true).BackgroundOpaque(),
	}, nil
}

// allItems is every configured item across the three sections.
// allItems flattens every section, descending one level into a group. A group
// is chrome: its members are the widgets that need service leases, so a
// selector nested in one must still be acquired or the group renders
// placeholders forever.
func allItems(policy config.Bar) []config.Item {
	out := make([]config.Item, 0, len(policy.Left)+len(policy.Center)+len(policy.Right))
	for _, section := range [][]config.Item{policy.Left, policy.Center, policy.Right} {
		for _, item := range section {
			if item.ID == "group" {
				out = append(out, item.Items...)
				continue
			}
			out = append(out, item)
		}
	}
	return out
}

// weatherUnit maps the validated configuration string to the service unit.
func weatherUnit(name string) services.Unit {
	if name == "fahrenheit" {
		return services.UnitFahrenheit
	}
	return services.UnitCelsius
}

func (r *Registry) bindHost(global uint32, bar *Bar, hooks wayland.HostCallbacks) wayland.HostCallbacks {
	innerHandle := hooks.Handle
	hooks.Handle = func(event wayland.Event) bool {
		changed := innerHandle(event)
		r.drivePointerTooltip(global, bar, event)
		return changed
	}
	innerOutputSize := hooks.OutputSize
	hooks.OutputSize = func(width, height int) {
		if innerOutputSize != nil {
			innerOutputSize(width, height)
		}
		if r.depthClocks != nil {
			r.depthClocks.outputSize(bar.connector(), global, width, height)
		}
	}
	innerConfigure := hooks.Configure
	hooks.Configure = func(width, height, scale120 int) error {
		prev := bar.scale120()
		if err := innerConfigure(width, height, scale120); err != nil {
			return err
		}
		if bar.scale120() == prev {
			return nil
		}
		r.reprojectTray()
		r.reprojectRunningApps()
		return nil
	}
	return hooks
}

func (r *Registry) drivePointerTooltip(global uint32, bar *Bar, event wayland.Event) {
	switch event.Kind {
	case wayland.EventPointerLeave:
		r.dwell.leave()
	case wayland.EventPointerEnter, wayland.EventPointerMotion:
		if text, root, bounds, ok := bar.hoverTooltip(); ok {
			if root != nil {
				r.dwell.enterRoot(global, bounds, root)
			} else {
				r.dwell.enter(global, bounds, text)
			}
		} else {
			r.dwell.leave()
		}
	}
}

func releaseAll(leases []*services.Lease) {
	for _, lease := range leases {
		lease.Release()
	}
}

// retheThemeOpenSurfacesLocked moves auxiliary surfaces onto the supplied
// theme and returns visible surfaces that need repainting after r.mu is released.
//
// Caller holds r.mu.
func (r *Registry) retheThemeOpenSurfacesLocked(cfg config.Config, tokens theme.Tokens) []wayland.Invalidation {
	var pubs []wayland.Invalidation
	for _, h := range r.panelHosts {
		if h == nil {
			continue
		}
		next := r.panelThemeForState(h.output, cfg, tokens)
		h.retheme(withPanelRadius(next, h))
		r.startSurfaceFrames(h)
	}
	if r.toasts != nil {
		r.toasts.restyleLocked()
	}
	if h := r.windowSwitcher; h != nil {
		next := withBarGeometry(r.panelThemeForState(0, cfg, tokens), cfg.Bar)
		h.retheme(next)
		if h.open_ {
			pubs = append(pubs, wayland.Invalidation{Global: h.output, SurfaceID: windowSwitcherSurfaceID})
		}
	}
	if r.osd != nil {
		pubs = append(pubs, r.osd.retheme(r.panelThemeForState(0, cfg, tokens))...)
	}
	return pubs
}

// withPanelRadius keeps a panel's own corner radius, which is fixed rather than
// themed, while everything else follows the published palette.
func withPanelRadius(t Theme, h *PanelHost) Theme {
	t.Radius = h.theme.Radius
	t.CardRadius = h.theme.Shapes.Card
	return t
}

// SetCapabilities records the compositor's optional effects. The platform
// calls it on the Wayland goroutine before the first bar is built, and again
// when the answer changes.
func (r *Registry) SetCapabilities(c wayland.Capabilities) {
	r.mu.Lock()
	changed := r.caps.Blur != c.Blur
	if changed || !r.capsKnown {
		log.Print(blurLogLine(c.Blur))
	}
	r.caps, r.capsKnown = c, true
	if !changed {
		r.mu.Unlock()
		return
	}
	// The bar restyles in place: WithCompositor re-derives its ground and
	// pills from the configured values, so no reload or remap is needed.
	for _, bar := range r.bars {
		bar.retheme(bar.themeSnapshot().WithCompositor(c.Blur))
	}
	cfg, tokens := r.effectiveThemeLocked()
	surfacePubs := r.retheThemeOpenSurfacesLocked(cfg, tokens)
	outputs := r.outputGlobalsLocked()
	r.mu.Unlock()

	for _, global := range outputs {
		r.publishSurface(global, "")
	}
	for _, p := range surfacePubs {
		r.publishSurface(p.Global, p.SurfaceID)
	}
}

// blurLogLine says what the compositor's blur answer means for the shell.
func blurLogLine(blur bool) string {
	if blur {
		return "shell: the compositor blurs behind surfaces; frosted bars and panels blur"
	}
	return "shell: the compositor offers no blur (ext-background-effect-v1, Niri 26.04 or later); frosted bars paint solid"
}

// blurAvailableLocked reports whether the compositor blurs behind regions.
func (r *Registry) blurAvailableLocked() bool { return r.caps.Blur }
