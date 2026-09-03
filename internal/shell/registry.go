package shell

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	launcher "github.com/Nomadcxx/sysc-launch"
	"github.com/Nomadcxx/sysc-shell/internal/config"
	"github.com/Nomadcxx/sysc-shell/internal/icons"
	"github.com/Nomadcxx/sysc-shell/internal/notifyclient"
	"github.com/Nomadcxx/sysc-shell/internal/platform/niri"
	"github.com/Nomadcxx/sysc-shell/internal/platform/wayland"
	"github.com/Nomadcxx/sysc-shell/internal/services"
	"github.com/Nomadcxx/sysc-shell/internal/theme"
	"github.com/Nomadcxx/sysc-shell/internal/theming"
	"github.com/Nomadcxx/sysc-shell/internal/trayclient"
	"github.com/Nomadcxx/sysc-shell/internal/ui"
	"github.com/Nomadcxx/sysc-shell/internal/wallpaper"
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
	mu      sync.Mutex
	cfg     config.Config
	outputs map[string]outputState
	bars    map[uint32]*Bar
	leases  map[uint32][]*services.Lease
	now     time.Time
	focused string

	clock   *services.Clock
	metrics *services.Metrics
	weather *services.Weather
	sample  services.Snapshot
	reading services.Reading

	tokens   theme.Tokens
	themeGen theme.Generator

	// invalidations carries one entry per bar whose rendered text changed.
	// The Wayland owner receives from it; the registry owns it and never
	// closes it.
	invalidations chan wayland.Invalidation
	aux           chan wayland.AuxRequest
	panels        PanelSet
	panelHosts    map[PanelID]*PanelHost
	// roots is the one interactive root the process allows at a time.
	roots rootChain
	// closed unblocks a pending publish at shutdown.
	closed      chan struct{}
	closeOnce   sync.Once
	dwell       *dwell
	configPath  string
	reloads     chan<- struct{}
	audio       *services.Audio
	brightness  *services.Brightness
	osd         *OSDManager
	audioLease  *services.Lease
	brightLease *services.Lease
	// runArgv launches a session action. Tests replace it per Registry.
	runArgv func([]string) error
	// lookPath finds a binary on PATH. Tests replace it per Registry.
	lookPath func(string) (string, error)
	// runArgvOutput captures stdout of powerprofilesctl list. Tests replace it.
	runArgvOutput func([]string) (string, error)

	// notify is the service-owned notification projection.
	notify *notifyState

	// tray is the service-owned tray projection.
	tray            *trayState
	trayCh          chan trayclient.Message
	traySender      trayCommandSender
	trayMenu        *trayMenuHost
	trayDrawer      *trayDrawerHost
	trayReplies     *trayReplyTracker
	trayIcons       *icons.Worker
	wallpaperSvc    *wallpaper.Service
	trayIconCancel  context.CancelFunc
	pendingTrayMenu pendingTrayMenu

	// plugins hosts one process per enabled plugin. Nil until BindPlugins.
	plugins *pluginHost

	// notifyCh carries client messages; main pumps it. Nil in tests that drive
	// applyNotify directly.
	notifyCh chan notifyclient.Message
	// notifySender is the client's Send seam. Nil in tests that do not
	// assert commands.
	notifySender notifyCommandSender
	// toasts hosts one toast stack per output, created when wiring binds it.
	toasts *toastHost
	// launcherSvc is created on the first launcher open; nil until then.
	launcherSvc *launcher.Service
}

func NewRegistry(cfg config.Config) *Registry {
	gen := theme.Generator{}
	r := &Registry{
		cfg:     cfg,
		outputs: make(map[string]outputState),
		bars:    make(map[uint32]*Bar),
		leases:  make(map[uint32][]*services.Lease),
		clock:   services.NewClock(),
		metrics: services.NewMetrics(),
		weather: services.NewWeather(
			cfg.Weather.Latitude, cfg.Weather.Longitude, weatherUnit(cfg.Weather.Unit)),
		themeGen:      gen,
		invalidations: make(chan wayland.Invalidation, 8),
		aux:           make(chan wayland.AuxRequest, 8),
		panelHosts:    make(map[PanelID]*PanelHost),
		closed:        make(chan struct{}),
		dwell:         newDwell(defaultDwell),
		runArgv:       runArgvDefault,
		lookPath:      exec.LookPath,
		runArgvOutput: runArgvOutputDefault,
		notify:        newNotifyState(),
		tray:          newTrayState(),
		trayCh:        make(chan trayclient.Message, 32),
		notifyCh:      make(chan notifyclient.Message, 32),
	}
	r.tokens = r.generateTheme(cfg)
	r.osd = newOSDManager(r, 0)
	r.setAudio(services.NewAudio(0, ""))
	r.setBrightness(services.NewBrightness("", "", 0))
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
	r.mu.Unlock()
	templates := map[string]bool{}
	for _, name := range theming.Catalog().Names() {
		templates[name] = cfg.TemplateEnabled(name)
	}
	_, err := exec.LookPath("matugen")
	return map[string]any{
		"version":    "sysc-shell",
		"audio":      audio,
		"brightness": bright,
		"panels":     panels,
		"matugen":    err == nil,
		"templates":  templates,
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
	r.OSD().Show(OSDView{Kind: "audio", Level: st.Level, Muted: st.Muted})
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
	r.OSD().Show(OSDView{Kind: "brightness", Level: r.brightness.Level()})
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
func (r *Registry) generateTheme(cfg config.Config) theme.Tokens {
	tok, _ := r.themeGen.Generate(
		theme.Source{Kind: cfg.ThemeGen.Source, Seed: cfg.ThemeGen.Seed},
		theme.Options{
			Mode:         cfg.ThemeGen.Mode,
			Scheme:       cfg.ThemeGen.Scheme,
			HighContrast: cfg.Accessibility.HighContrast,
		},
	)
	if err := tok.Complete(); err != nil {
		return r.lastCompleteTokens()
	}
	if !runningAsTest() {
		_ = theming.ApplyEnabled(os.Getenv("HOME"), cfg.TemplateEnabled, tok)
	}
	return tok
}

// lastCompleteTokens is the palette to fall back on when a generated one is
// rejected: whatever is currently published, or the compiled-in fallback
// before anything has been.
func (r *Registry) lastCompleteTokens() theme.Tokens {
	r.mu.Lock()
	current := r.tokens
	r.mu.Unlock()
	if current.Complete() == nil {
		return current
	}
	return theme.Fallback
}

// surfaceTheme is the palette every auxiliary surface paints with: the
// generated tokens, with the bar's geometry so a panel and the bar agree about
// spacing and text size.
//
// Callers hold Registry.mu, because the tokens are replaced by a reload.
func (r *Registry) surfaceTheme() Theme {
	return withBarGeometry(ThemeFromTokens(r.tokens, r.cfg.Theme.Radius), r.cfg.Bar)
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
			r.OSD().Show(OSDView{Kind: "audio", Level: st.Level, Muted: st.Muted})
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
			r.OSD().Show(OSDView{Kind: "brightness", Level: st.Level})
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

// Tooltips is the channel the Wayland owner receives hover requests from.
func (r *Registry) Tooltips() <-chan wayland.TooltipRequest { return r.dwell.requests() }

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
	cfg := r.cfg
	tok := r.tokens
	r.mu.Unlock()

	bar, leases, callbacks, err := r.buildBar(cfg, connector, tok)
	if err != nil {
		return wayland.HostCallbacks{}, err
	}

	r.mu.Lock()
	bar.apply(r.viewLocked(connector))
	r.bars[global] = bar
	r.leases[global] = leases
	r.bindBarTrayLocked(global, connector, bar)
	r.bindBarPluginLocked(bar)
	r.bindBarPanelActionsLocked(global, bar)
	// Seeded, not published: the owner configures this surface next and paints
	// it once. An invalidation here would be a second frame for a first paint,
	// and this call is on the owner goroutine, which drains that channel.
	r.syncTrayLocked()
	toastOutputs := r.outputGlobalsLocked()
	plugins := r.plugins
	r.mu.Unlock()

	r.SyncToastOutputs(toastOutputs)
	if plugins != nil {
		plugins.syncBars()
	}
	return r.bindHost(global, bar, callbacks), nil
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
		return r.handlePluginBar(action, event)
	})
}

// bindBarPanelActionsLocked gives one bar its panel-toggle seam. The handler
// is called without the bar lock, matching the tray path, because TogglePanel
// takes the registry lock and then the bar lock.
func (r *Registry) bindBarPanelActionsLocked(global uint32, bar *Bar) {
	bar.setActionHandler(func(action string, button uint32) bool {
		out, trig := r.triggerFor(global)
		switch {
		case action == panelMonitorAction && (button == 0 || button == buttonLeft || button == buttonRight):
			return r.TogglePanel(PanelMonitor, out, trig) == nil
		case action == panelSessionAction && button == buttonRight:
			return r.TogglePanel(PanelSession, out, trig) == nil
		case action == panelNotificationsAction && (button == 0 || button == buttonLeft):
			return r.TogglePanel(PanelNotifications, out, trig) == nil
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
	tok := r.generateTheme(cfg)
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
				// Coordinates and unit are the request, not a lease parameter,
				// so the service has to be told. It is a no-op unless they
				// changed, which is the common case for an unrelated reload.
				r.weather.Reconfigure(
					cfg.Weather.Latitude, cfg.Weather.Longitude, weatherUnit(cfg.Weather.Unit))
				r.dwell.leave()
				// The open menu and drawer were placed against the outgoing
				// geometry and hold a root; a candidate replaces both.
				r.closeTrayLocked()
				for _, bar := range bars {
					bar.apply(r.viewLocked(bar.connector()))
				}
				r.cfg = cfg
				r.tokens = tok
				r.retheThemeOpenSurfacesLocked()
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
	delete(r.bars, global)
	delete(r.leases, global)
	r.trayOutputLostLocked(global)
	toastOutputs := r.outputGlobalsLocked()
	plugins := r.plugins
	r.mu.Unlock()

	r.SyncToastOutputs(toastOutputs)
	if plugins != nil {
		plugins.syncBars()
	}
	releaseAll(leases)
}

// Close releases every bar and service. It is safe to call twice.
func (r *Registry) Close() {
	// Unblocks any publish waiting on a full channel, so shutdown cannot hang.
	r.closeOnce.Do(func() { close(r.closed) })

	r.mu.Lock()
	if r.toasts != nil {
		r.toasts.stopLeaseRenew()
	}
	var osdAux []wayland.AuxRequest
	if r.osd != nil {
		osdAux = r.osd.prepareHide()
	}
	r.closeTrayLocked()
	r.stopTrayIconsLocked()
	r.closeAllPanelsLocked()
	var leases []*services.Lease
	for global, held := range r.leases {
		leases = append(leases, held...)
		delete(r.leases, global)
	}
	r.bars = make(map[uint32]*Bar)
	audioLease := r.audioLease
	r.audioLease = nil
	brightLease := r.brightLease
	r.brightLease = nil
	r.mu.Unlock()

	for _, req := range osdAux {
		r.sendAux(req)
	}
	if audioLease != nil {
		audioLease.Release()
	}
	if brightLease != nil {
		brightLease.Release()
	}
	releaseAll(leases)
	r.mu.Lock()
	launcherSvc := r.launcherSvc
	r.launcherSvc = nil
	r.mu.Unlock()
	if launcherSvc != nil {
		launcherSvc.Close()
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
	var changed []uint32
	for global, bar := range r.bars {
		if bar.apply(r.viewLocked(bar.connector())) {
			changed = append(changed, global)
		}
	}
	r.mu.Unlock()

	r.publish(changed)
	return changed
}

// UpdateMetrics applies a sampling pass to every bar and reports the globals
// whose rendering actually changed.
func (r *Registry) UpdateMetrics(snap services.Snapshot) []uint32 {
	r.mu.Lock()
	r.sample = snap
	var changed []uint32
	for global, bar := range r.bars {
		if bar.apply(r.viewLocked(bar.connector())) {
			changed = append(changed, global)
		}
	}
	monitorOut, monitorOK := uint32(0), false
	if h := r.panelHosts[PanelMonitor]; h != nil {
		r.rebuildPanel(h)
		monitorOut, monitorOK = h.output, true
	}
	sessionOut, sessionOK := uint32(0), false
	if h := r.panelHosts[PanelSession]; h != nil {
		r.rebuildPanel(h)
		sessionOut, sessionOK = h.output, true
	}
	r.mu.Unlock()

	r.publish(changed)
	if monitorOK {
		r.publishSurface(monitorOut, panelSurfaceID(PanelMonitor))
	}
	if sessionOK {
		r.publishSurface(sessionOut, panelSurfaceID(PanelSession))
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
	r.mu.Unlock()

	r.publish(changed)
	return changed
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
	r.focused = s.FocusedOutput

	var changed []uint32
	for global, bar := range r.bars {
		if bar.apply(r.viewLocked(bar.connector())) {
			changed = append(changed, global)
		}
	}
	r.mu.Unlock()

	r.publish(changed)
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
		Unread:    r.notify.unread(),
	}
	_, view.DND = r.notify.dndState(r.now)
	if r.plugins != nil {
		view.Plugins = r.plugins.frames(connector)
	}
	return view
}

// historyLocked collects the samples every leased selector holds. The service
// keeps a ring only while something leases it, so an unused ring cannot be
// copied here.
func (r *Registry) historyLocked() map[services.Selector][]float64 {
	return r.metrics.Histories()
}

// buildBar creates one bar and acquires the services its items need. A failure
// releases whatever was already acquired, so a rejected build leaks nothing.
func (r *Registry) buildBar(cfg config.Config, connector string, tok theme.Tokens) (
	*Bar, []*services.Lease, wayland.HostCallbacks, error,
) {
	policy := cfg.ForConnector(connector)
	th := withBarGeometry(ThemeFromTokens(tok, cfg.Theme.Radius), policy)
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

	return bar, leases, wayland.HostCallbacks{
		Configure:        bar.Configure,
		Render:           bar.Render,
		Handle:           bar.Handle,
		OpaqueBackground: th.BackgroundOpaque(),
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
	inner := hooks.Handle
	hooks.Handle = func(event wayland.Event) bool {
		changed := inner(event)
		r.drivePointerTooltip(global, bar, event)
		return changed
	}
	return hooks
}

func (r *Registry) drivePointerTooltip(global uint32, bar *Bar, event wayland.Event) {
	switch event.Kind {
	case wayland.EventPointerLeave:
		r.dwell.leave()
	case wayland.EventPointerEnter, wayland.EventPointerMotion:
		if text, root, bounds, ok := bar.hoverTooltip(); ok {
			style := wayland.TooltipStyle{
				Background: bar.theme.Background,
				Foreground: bar.theme.Foreground,
			}
			if root != nil {
				r.dwell.enterRoot(global, bounds, root, style)
			} else {
				r.dwell.enter(global, bounds, text, style)
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

// retheThemeOpenSurfacesLocked moves every open panel onto the newly published
// palette, crossfading from what each is currently rendering. Panels used to
// keep the theme they were spawned with, so a reload left an open surface in
// the previous palette until it was closed and reopened.
//
// Caller holds r.mu.
func (r *Registry) retheThemeOpenSurfacesLocked() {
	next := r.surfaceTheme()
	for _, h := range r.panelHosts {
		if h == nil {
			continue
		}
		h.retheme(withPanelRadius(next, h))
		r.startSurfaceFrames(h)
	}
}

// withPanelRadius keeps a panel's own corner radius, which is fixed rather than
// themed, while everything else follows the published palette.
func withPanelRadius(t Theme, h *PanelHost) Theme {
	t.Radius = h.theme.Radius
	t.CardRadius = h.theme.CardRadius
	return t
}
