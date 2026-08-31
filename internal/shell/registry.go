package shell

import (
	"fmt"
	"sync"
	"time"

	"github.com/Nomadcxx/sysc-shell/internal/config"
	"github.com/Nomadcxx/sysc-shell/internal/platform/niri"
	"github.com/Nomadcxx/sysc-shell/internal/platform/wayland"
	"github.com/Nomadcxx/sysc-shell/internal/services"
	"github.com/Nomadcxx/sysc-shell/internal/theme"
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
	// closed unblocks a pending publish at shutdown.
	closed     chan struct{}
	closeOnce  sync.Once
	dwell      *dwell
	configPath string
	reloads    chan<- struct{}
	audio      *services.Audio
	brightness *services.Brightness
	osd        *OSDManager
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
		audio:         services.NewAudio(0, ""),
		brightness:    services.NewBrightness("", "", 0),
	}
	r.tokens = r.generateTheme(cfg)
	r.osd = newOSDManager(r, 0)
	return r
}

func (r *Registry) OSD() *OSDManager { return r.osd }

func (r *Registry) AudioAvailable() bool {
	return r != nil && r.audio != nil && r.audio.Available()
}

func (r *Registry) BrightnessAvailable() bool {
	return r != nil && r.brightness != nil && r.brightness.Available()
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

func (r *Registry) generateTheme(cfg config.Config) theme.Tokens {
	tok, _ := r.themeGen.Generate(
		theme.Source{Kind: cfg.ThemeGen.Source, Seed: cfg.ThemeGen.Seed},
		theme.Options{
			Mode:         cfg.ThemeGen.Mode,
			Scheme:       cfg.ThemeGen.Scheme,
			HighContrast: cfg.Accessibility.HighContrast,
		},
	)
	return tok
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
	r.mu.Unlock()

	return r.bindHost(global, bar, callbacks), nil
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
				for _, bar := range bars {
					bar.apply(r.viewLocked(bar.connector()))
				}
				r.cfg = cfg
				r.tokens = tok
				r.bars = bars
				r.leases = leases
				r.mu.Unlock()

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
	r.mu.Unlock()

	releaseAll(leases)
}

// Close releases every bar and service. It is safe to call twice.
func (r *Registry) Close() {
	// Unblocks any publish waiting on a full channel, so shutdown cannot hang.
	r.closeOnce.Do(func() { close(r.closed) })

	r.mu.Lock()
	if r.osd != nil {
		r.osd.hideLocked()
	}
	r.closeAllPanelsLocked()
	var leases []*services.Lease
	for global, held := range r.leases {
		leases = append(leases, held...)
		delete(r.leases, global)
	}
	r.bars = make(map[uint32]*Bar)
	r.mu.Unlock()

	releaseAll(leases)
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
	r.mu.Unlock()

	r.publish(changed)
	if monitorOK {
		r.publishSurface(monitorOut, panelSurfaceID(PanelMonitor))
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
	return barView{
		Now:       r.now,
		Workspace: state.Workspace,
		Title:     state.Title,
		Metrics:   r.sample,
		History:   r.historyLocked(),
		Weather:   r.reading,
	}
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
func allItems(policy config.Bar) []config.Item {
	out := make([]config.Item, 0, len(policy.Left)+len(policy.Center)+len(policy.Right))
	out = append(out, policy.Left...)
	out = append(out, policy.Center...)
	return append(out, policy.Right...)
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
		if text, bounds, ok := bar.hoverTooltip(); ok {
			r.dwell.enter(global, bounds, text)
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
